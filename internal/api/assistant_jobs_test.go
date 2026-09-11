package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	assistantjobs "github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
)

type jobTestTasks struct{ TaskService }

func (jobTestTasks) ClaimTask(context.Context, string, string, string) (*types.ClaimResponse, error) {
	return &types.ClaimResponse{Success: true}, nil
}
func (jobTestTasks) ReleaseTask(context.Context, string, string, string) error { return nil }
func (jobTestTasks) RenewClaim(context.Context, string, string, string) (*types.RenewClaimResponse, error) {
	return &types.RenewClaimResponse{}, nil
}
func jobTestContext(name string) context.Context {
	return context.WithValue(tenant.Into(context.Background(), tenant.Local), ctxAuthResult, &AuthResult{Type: "api_token", Name: name, Scope: "admin:*"})
}
func jobTestService(t *testing.T, url string) *AssistantService {
	t.Helper()
	t.Setenv("JOB_TEST_KEY", "test")
	s := NewAssistantService(AssistantServiceOptions{Enabled: true, Provider: "openrouter", BaseURL: url, APIKeyEnv: "JOB_TEST_KEY", Model: "test", Tasks: jobTestTasks{}, Brain: &mockBrainService{updateFunc: func(context.Context, string, types.UpdateEntryRequest) (*types.BrainEntry, error) {
		return &types.BrainEntry{}, nil
	}}})
	store, err := assistantjobs.Open(filepath.Join(t.TempDir(), "jobs.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	s.jobs = &conversationJobs{s: s, store: store, ctx: tenant.Into(context.Background(), tenant.Local), running: map[string]context.CancelFunc{}, wake: make(chan struct{}, 1), leaseRequest: func(_ context.Context, _ assistantjobs.Record, _ string, result any) error {
		if claim, ok := result.(*types.ClaimResponse); ok {
			claim.Success = true
		}
		return nil
	}}
	return s
}
func TestCoordinatorHasNoBrainTools(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Tools []toolSchema `json:"tools"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
		}
		for _, tool := range payload.Tools {
			seen = append(seen, tool.Function.Name)
		}
		fmt.Fprint(w, makeSSE(`{"choices":[{"delta":{"content":"Hello."}}]}`))
	}))
	defer server.Close()
	s := jobTestService(t, server.URL)
	_, err := s.Chat(jobTestContext("alice"), AssistantChatRequest{ConversationID: "chat", Message: "Hello"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(seen, ",") != "list_jobs,inspect_job,start_job,update_job,resume_job,cancel_job" {
		t.Fatal("unexpected coordinator tools", seen)
	}
	if _, err = s.Chat(jobTestContext("alice"), AssistantChatRequest{Message: "missing conversation"}); err == nil {
		t.Fatal("missing conversation accepted")
	}
}

func TestWorkerAcceptsContextWhileForegroundRemainsResponsive(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	var sawUpdate atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "foreground ping") {
			fmt.Fprint(w, makeSSE(`{"choices":[{"delta":{"content":"I'm here."}}]}`))
			return
		}
		n := calls.Add(1)
		if n == 1 {
			close(started)
			<-release
		} else if strings.Contains(string(body), "include last week") {
			sawUpdate.Store(true)
		}
		fmt.Fprint(w, makeSSE(`{"choices":[{"delta":{"content":"Worker result."}}]}`))
	}))
	defer server.Close()
	s := jobTestService(t, server.URL)
	owner, _ := jobOwner(jobTestContext("alice"))
	request, _ := json.Marshal(AssistantChatRequest{Message: "Check project progress"})
	r := assistantjobs.Record{Job: assistantjobs.Job{ID: "work", Conversation: "chat", TaskID: "task", TaskPath: "path", State: "queued"}, Owner: owner, Request: request}
	if err := s.jobs.store.Create(r); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); s.jobs.execute(s.jobs.ctx, r) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	foreground, cancel := context.WithCancel(jobTestContext("alice"))
	result, err := s.Chat(foreground, AssistantChatRequest{ConversationID: "chat", Message: "foreground ping"})
	cancel()
	if err != nil || result.Reply != "I'm here." {
		t.Fatal("foreground blocked", err)
	}
	if _, err = s.jobs.control(jobTestContext("alice"), owner, r, "update_job", "include last week"); err != nil {
		t.Fatal(err)
	}
	close(release)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not finish")
	}
	finished, err := s.jobs.store.Get(owner, r.ID)
	if err != nil || finished.State != "completed" || !sawUpdate.Load() || calls.Load() != 2 {
		t.Fatalf("context did not reach same worker: state=%s calls=%d update=%v error=%v", finished.State, calls.Load(), sawUpdate.Load(), err)
	}
	if finished.Acknowledged >= finished.Revision {
		t.Fatal("result was consumed while phone was away")
	}
	if !strings.Contains(string(finished.Messages), "include last week") {
		t.Fatal("worker context not persisted")
	}
	tools, err := s.jobs.tools(jobTestContext("bob"), AssistantChatRequest{ConversationID: "chat"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = tools[1].Handler(jobTestContext("bob"), s, "", json.RawMessage(`{"job_id":"work"}`))
	if err == nil {
		t.Fatal("other user accessed worker")
	}
	tools, _ = s.jobs.tools(jobTestContext("alice"), AssistantChatRequest{ConversationID: "other-chat"})
	_, err = tools[1].Handler(jobTestContext("alice"), s, "", json.RawMessage(`{"job_id":"work"}`))
	if err == nil {
		t.Fatal("other conversation accessed worker")
	}
}

func TestWorkerCancellationPersistsAndResumeKeepsJob(t *testing.T) {
	started := make(chan struct{})
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			_, _ = io.Copy(io.Discard, r.Body)
			close(started)
			<-r.Context().Done()
			return
		}
		fmt.Fprint(w, makeSSE(`{"choices":[{"delta":{"content":"Resumed answer."}}]}`))
	}))
	defer server.Close()
	s := jobTestService(t, server.URL)
	owner, _ := jobOwner(jobTestContext("alice"))
	request, _ := json.Marshal(AssistantChatRequest{Message: "Do work"})
	r := assistantjobs.Record{Job: assistantjobs.Job{ID: "same-job", Conversation: "chat", TaskID: "same-task", TaskPath: "path", State: "queued"}, Owner: owner, Request: request}
	if err := s.jobs.store.Create(r); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(s.jobs.ctx)
	s.jobs.running[r.ID] = cancel
	done := make(chan struct{})
	go func() { defer close(done); s.jobs.execute(ctx, r) }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("worker did not start")
	}
	if _, err := s.jobs.control(jobTestContext("alice"), owner, r, "cancel_job", ""); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancel did not stop worker")
	}
	r, _ = s.jobs.store.Get(owner, r.ID)
	if r.State != "cancelled" {
		t.Fatal("cancellation overwritten", r.State)
	}
	if _, err := s.jobs.control(jobTestContext("alice"), owner, r, "resume_job", "Continue this same lookup."); err != nil {
		t.Fatal(err)
	}
	r, _ = s.jobs.store.Get(owner, r.ID)
	s.jobs.execute(s.jobs.ctx, r)
	r, _ = s.jobs.store.Get(owner, r.ID)
	if r.State != "completed" || r.ID != "same-job" || r.TaskID != "same-task" || r.Result != "Resumed answer." {
		t.Fatalf("resume failed: %+v", r.Job)
	}
	all, _ := s.jobs.store.List(owner, "chat")
	if len(all) != 1 {
		t.Fatal("resume created duplicate")
	}
}

func TestWorkerClaimUsesCallerCredential(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tasks/assistant-jobs/task/claim" || r.Method != "POST" {
			t.Errorf("wrong claim route %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer delegated" {
			http.Error(w, "unauthorized", 401)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["runnerId"] != conversationRunnerID {
			t.Error("wrong runner claim")
		}
		fmt.Fprint(w, `{"success":true}`)
	}))
	defer server.Close()
	j := &conversationJobs{s: &AssistantService{mcpBaseURL: server.URL}}
	var result types.ClaimResponse
	if err := j.lease(context.Background(), assistantjobs.Record{Job: assistantjobs.Job{TaskID: "task"}, Credential: "delegated"}, "claim", &result); err != nil || !result.Success {
		t.Fatal("authenticated claim failed", err)
	}
	if err := j.lease(context.Background(), assistantjobs.Record{Job: assistantjobs.Job{TaskID: "task"}}, "claim", &result); err == nil {
		t.Fatal("missing credential bypassed authentication")
	}
}

func TestWorkerExitPreservesNewerControlState(t *testing.T) {
	for _, state := range []string{"queued", "cancelled", "paused"} {
		t.Run(state, func(t *testing.T) {
			r := assistantjobs.Record{Job: assistantjobs.Job{State: state, Revision: 9}, Pending: []string{"new context"}}
			if err := settleConversationJob(&r, agentLoopResult{}, context.Canceled); err != nil {
				t.Fatal(err)
			}
			if r.State != state || r.Revision != 9 || len(r.Pending) != 1 {
				t.Fatalf("old execution overwrote a newer control action: %+v", r.Job)
			}
		})
	}
}

func TestCancelDuringTaskCreationRemainsCancelled(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var mirrored atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			close(started)
			<-release
			fmt.Fprint(w, `{"id":"created-task","path":"projects/assistant-jobs/task/created-task.md"}`)
			return
		}
		var update types.UpdateEntryRequest
		_ = json.NewDecoder(r.Body).Decode(&update)
		mirrored.Store(update.Status != nil && *update.Status == "cancelled")
		fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	s := jobTestService(t, server.URL)
	s.mcpBaseURL = server.URL
	ctx := jobTestContext("alice")
	owner, _ := jobOwner(ctx)
	done := make(chan error, 1)
	go func() {
		_, err := s.jobs.create(ctx, owner, AssistantChatRequest{ConversationID: "chat"}, "Count", "Count entries", "")
		done <- err
	}()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("creation did not start")
	}
	jobs, err := s.jobs.store.List(owner, "chat")
	if err != nil || len(jobs) != 1 {
		t.Fatal("missing creating record", err)
	}
	if _, err = s.jobs.control(ctx, owner, jobs[0], "cancel_job", ""); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	r, err := s.jobs.store.Get(owner, jobs[0].ID)
	if err != nil || r.State != "cancelled" || r.TaskID != "created-task" || !mirrored.Load() {
		t.Fatalf("creation overwrote cancellation: %+v %v", r.Job, err)
	}
}

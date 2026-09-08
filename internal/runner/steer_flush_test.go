package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/bridge"
)

// stubSteerFlusher overrides the steer re-poke indirection for the test's
// duration and records the (port, sessionID) it was called with.
func stubSteerFlusher(t *testing.T, err error) *struct {
	called    bool
	port      int
	sessionID string
} {
	t.Helper()
	rec := &struct {
		called    bool
		port      int
		sessionID string
	}{}
	prev := steerFlusher
	steerFlusher = func(port int, sessionID string) error {
		rec.called = true
		rec.port = port
		rec.sessionID = sessionID
		return err
	}
	t.Cleanup(func() { steerFlusher = prev })
	return rec
}

// flushTestRunner builds a minimal TaskRunner around a mock process manager
// so tests can inspect the PendingSteer flag directly.
func flushTestRunner(t *testing.T) (*TaskRunner, *mockProcessMgr) {
	t.Helper()
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     testRunnerConfig(),
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})
	return tr, processMgr
}

func steerTask(port int, sessionID string, pending bool) RunningTask {
	return RunningTask{
		ID:           "task1",
		Path:         "projects/proj-a/task/task1.md",
		Title:        "Test Task",
		ProjectID:    "proj-a",
		PID:          100,
		StartedAt:    time.Now(),
		ExecutorType: "opencode",
		OpencodePort: port,
		SessionID:    sessionID,
		PendingSteer: pending,
	}
}

func TestFlushQueuedSteer_Success_ClearsFlag(t *testing.T) {
	tr, pm := flushTestRunner(t)
	rec := stubSteerFlusher(t, nil)

	task := steerTask(1234, "ses_abc", true)
	pm.Add(task.ID, task, newMockProcess(100))

	tr.flushQueuedSteer(task)

	if !rec.called {
		t.Fatal("steerFlusher should have been called")
	}
	if rec.port != 1234 || rec.sessionID != "ses_abc" {
		t.Errorf("steerFlusher called with (%d,%q), want (1234,ses_abc)", rec.port, rec.sessionID)
	}
	info := pm.Get(task.ID)
	if info == nil {
		t.Fatal("task should still be tracked")
	}
	if info.Task.PendingSteer {
		t.Error("PendingSteer should be cleared on successful flush")
	}
}

func TestFlushQueuedSteer_Error_LeavesFlagSet(t *testing.T) {
	tr, pm := flushTestRunner(t)
	rec := stubSteerFlusher(t, errors.New("boom"))

	task := steerTask(1234, "ses_abc", true)
	pm.Add(task.ID, task, newMockProcess(100))

	tr.flushQueuedSteer(task)

	if !rec.called {
		t.Fatal("steerFlusher should have been called")
	}
	info := pm.Get(task.ID)
	if info == nil {
		t.Fatal("task should still be tracked")
	}
	if !info.Task.PendingSteer {
		t.Error("PendingSteer should be LEFT set when flush errors")
	}
}

func TestFlushQueuedSteer_NoPort_SkipsFlusher(t *testing.T) {
	tr, pm := flushTestRunner(t)
	rec := stubSteerFlusher(t, nil)

	task := steerTask(0, "ses_abc", true)
	pm.Add(task.ID, task, newMockProcess(100))

	tr.flushQueuedSteer(task)

	if rec.called {
		t.Error("steerFlusher must not be called when port == 0")
	}
}

func TestFlushQueuedSteer_NoSession_SkipsFlusher(t *testing.T) {
	tr, pm := flushTestRunner(t)
	rec := stubSteerFlusher(t, nil)

	task := steerTask(1234, "", true)
	pm.Add(task.ID, task, newMockProcess(100))

	tr.flushQueuedSteer(task)

	if rec.called {
		t.Error("steerFlusher must not be called when sessionID is empty")
	}
}

// Integration: /session/status busy + transcript turn-ended + PendingSteer=true
// ⇒ steerFlusher fires and the flag clears (steer flush path). The idle timer
// must NOT advance this tick (flush gets a chance to restart a turn first).
func TestCheckIdleStatus_TurnEnded_PendingSteer_Flushes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ses_abc": map[string]interface{}{"type": "busy"},
		})
	}))
	defer server.Close()
	port := serverPort(t, server)

	body := transcriptJSON(t,
		msgJSON(t, "assistant", 2000, 3000),
	)
	stubSessionHistory(t, body, nil)
	rec := stubSteerFlusher(t, nil)

	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.IdleDetectionThreshold = 60000

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	proc := newMockProcess(100)
	task := steerTask(port, "ses_abc", true)
	task.CompleteOnIdle = true
	processMgr.Add(task.ID, task, proc)

	tr.checkIdleStatus(context.Background())

	if !rec.called {
		t.Fatal("steerFlusher should have been invoked on turn-ended edge with PendingSteer")
	}
	info := processMgr.Get(task.ID)
	if info == nil {
		t.Fatal("task should still be tracked")
	}
	if info.Task.PendingSteer {
		t.Error("PendingSteer should be cleared after successful flush")
	}
	// Idle timer must not advance this tick.
	if info.Task.IdleSince != "" {
		t.Errorf("IdleSince should not be set on the flush tick, got %q", info.Task.IdleSince)
	}
}

// Integration: same setup but PendingSteer=false ⇒ steerFlusher NOT invoked
// and Phase 2 behavior holds (idle timer advances).
func TestCheckIdleStatus_TurnEnded_NoPendingSteer_AdvancesIdle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ses_abc": map[string]interface{}{"type": "busy"},
		})
	}))
	defer server.Close()
	port := serverPort(t, server)

	body := transcriptJSON(t,
		msgJSON(t, "assistant", 2000, 3000),
	)
	stubSessionHistory(t, body, nil)
	rec := stubSteerFlusher(t, nil)

	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.IdleDetectionThreshold = 60000

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})

	proc := newMockProcess(100)
	task := steerTask(port, "ses_abc", false)
	task.CompleteOnIdle = true
	processMgr.Add(task.ID, task, proc)

	tr.checkIdleStatus(context.Background())

	if rec.called {
		t.Error("steerFlusher must not be invoked when PendingSteer is false")
	}
	info := processMgr.Get(task.ID)
	if info == nil {
		t.Fatal("task should still be tracked")
	}
	if info.Task.IdleSince == "" {
		t.Error("IdleSince should advance (Phase 2 behavior) when no pending steer")
	}
}

// proxyRequest steer-marking: a POST prompt_async proxied to a tracked task
// sets PendingSteer; a non-prompt_async request does not.
func TestProxyRequest_MarksPendingSteerOnPromptAsync(t *testing.T) {
	// httptest server standing in for the local OpenCode instance.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	port := serverPort(t, srv)

	pm := NewProcessManager(RunnerConfig{APITimeout: 5000})
	instanceID := "inst_test01"
	task := RunningTask{
		ID:           "task-1",
		Path:         "projects/p/task/t1.md",
		ProjectID:    "p",
		StartedAt:    time.Now(),
		ExecutorType: "opencode",
		InstanceID:   instanceID,
		OpencodePort: port,
	}
	if err := pm.Add(task.ID, task, newMockProcess(1)); err != nil {
		t.Fatalf("track task: %v", err)
	}

	tr := &TaskRunner{
		config:     RunnerConfig{APITimeout: 5000},
		processMgr: pm,
		logger:     log.New(io.Discard, "", 0),
	}
	bc := NewBridgeClient(tr)

	// prompt_async POST ⇒ flag set.
	f := bridge.Frame{
		Type:       bridge.FrameReq,
		Method:     http.MethodPost,
		Path:       "/session/ses_abc/prompt_async",
		InstanceID: instanceID,
		Body:       json.RawMessage(`{"parts":[{"type":"text","text":"steer"}]}`),
	}
	status, _, err := bc.proxyRequest(f)
	if err != nil {
		t.Fatalf("proxyRequest error: %v", err)
	}
	if status < 200 || status >= 300 {
		t.Fatalf("proxyRequest status = %d, want 2xx", status)
	}
	if info := pm.Get(task.ID); info == nil || !info.Task.PendingSteer {
		t.Error("PendingSteer should be set after a proxied prompt_async")
	}
}

func TestProxyRequest_NonPromptAsyncDoesNotMark(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	port := serverPort(t, srv)

	pm := NewProcessManager(RunnerConfig{APITimeout: 5000})
	instanceID := "inst_test02"
	task := RunningTask{
		ID:           "task-2",
		Path:         "projects/p/task/t2.md",
		ProjectID:    "p",
		StartedAt:    time.Now(),
		ExecutorType: "opencode",
		InstanceID:   instanceID,
		OpencodePort: port,
	}
	if err := pm.Add(task.ID, task, newMockProcess(1)); err != nil {
		t.Fatalf("track task: %v", err)
	}

	tr := &TaskRunner{
		config:     RunnerConfig{APITimeout: 5000},
		processMgr: pm,
		logger:     log.New(io.Discard, "", 0),
	}
	bc := NewBridgeClient(tr)

	f := bridge.Frame{
		Type:       bridge.FrameReq,
		Method:     http.MethodGet,
		Path:       "/session/status",
		InstanceID: instanceID,
	}
	if _, _, err := bc.proxyRequest(f); err != nil {
		t.Fatalf("proxyRequest error: %v", err)
	}
	if info := pm.Get(task.ID); info == nil || info.Task.PendingSteer {
		t.Error("PendingSteer must not be set for a non-prompt_async request")
	}
}

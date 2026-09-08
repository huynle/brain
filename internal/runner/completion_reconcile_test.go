package runner

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// Regression for supernote/w3d168as on runner_26e7c256 (2026-09-05): two
// attempts died on an opencode auth error and were correctly reset to
// pending. The third attempt succeeded — the agent ran brain_update
// {status: completed} and the process then exited 0 — but the runner still
// classified the exit as crashed, charged it as attempt 3/3, parked the task
// in blocked, and blocked the feature. A run whose task is already terminal
// in the API must be treated as a success: no attempt increment, no cap note,
// no parking, counter cleared.
func TestHandleTaskCompletion_AgentCompletedBeforeExit_NotParked(t *testing.T) {
	client := newMockClient()
	processMgr := newMockProcessMgr()
	tr := newTestRunner(client, newMockExecutor(), processMgr, newMockStateMgr())
	ctx := context.Background()

	// Attempts 1 and 2: genuine crashes (process gone, task still
	// in_progress), retried while under the cap.
	task := runningTaskForRetry(0, DefaultMaxTaskAttempts)
	task.StartedAt = time.Now()
	for i := 0; i < 2; i++ {
		status, attempt := tr.recordTaskFailure(ctx, task)
		if status != "pending" {
			t.Fatalf("attempt %d: status = %q, want pending", attempt, status)
		}
		task.AttemptCount = attempt
	}
	if task.AttemptCount != 2 {
		t.Fatalf("AttemptCount = %d, want 2 before the final run", task.AttemptCount)
	}

	// Attempt 3: the agent marks the task completed, then the driver exits 0.
	client.getEntryResult = map[string]*types.BrainEntry{
		task.Path: {Path: task.Path, Status: "completed"},
	}
	if err := processMgr.Add(task.ID, task, &fakeProcess{exited: true, exitCode: 0}); err != nil {
		t.Fatal(err)
	}
	client.mu.Lock()
	client.updateStatusCalls = nil
	client.appendCalls = nil
	client.metadataCalls = nil
	client.mu.Unlock()

	// The process manager's own status lookup failed, so CheckCompletion
	// said crashed. That is the classification under test.
	tr.handleTaskCompletion(ctx, task.ID, task, CompletionCrashed)

	client.mu.Lock()
	defer client.mu.Unlock()

	if len(client.updateStatusCalls) != 1 || client.updateStatusCalls[0].Status != "completed" {
		t.Fatalf("status updates = %+v, want exactly one update to completed", client.updateStatusCalls)
	}
	for _, c := range client.updateStatusCalls {
		if c.Status == "blocked" {
			t.Fatalf("task was parked in blocked: %+v", client.updateStatusCalls)
		}
	}
	for _, a := range client.appendCalls {
		if strings.Contains(a.Content, "Retry cap reached") {
			t.Fatalf("retry-cap note was appended to a successful run: %q", a.Content)
		}
	}
	for _, m := range client.metadataCalls {
		if v, ok := m.Fields["attempt_count"]; ok && v != 0 {
			t.Fatalf("attempt counter was incremented on a successful run: %+v", m.Fields)
		}
	}
	// The counter left by attempts 1–2 must be cleared so a later failure
	// starts from zero.
	cleared := false
	for _, m := range client.metadataCalls {
		if v, ok := m.Fields["attempt_count"]; ok && v == 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatalf("attempt counter was not cleared after success: %+v", client.metadataCalls)
	}

	tr.mu.RLock()
	completed, failed := tr.stats.Completed, tr.stats.Failed
	tr.mu.RUnlock()
	if completed != 1 || failed != 0 {
		t.Fatalf("stats completed=%d failed=%d, want 1/0", completed, failed)
	}
}

// A "validated" task is just as done as a "completed" one.
func TestHandleTaskCompletion_ValidatedBeforeExit_IsSuccess(t *testing.T) {
	client := newMockClient()
	processMgr := newMockProcessMgr()
	tr := newTestRunner(client, newMockExecutor(), processMgr, newMockStateMgr())

	task := runningTaskForRetry(2, DefaultMaxTaskAttempts)
	task.StartedAt = time.Now()
	client.getEntryResult = map[string]*types.BrainEntry{
		task.Path: {Path: task.Path, Status: "validated"},
	}
	_ = processMgr.Add(task.ID, task, &fakeProcess{exited: true, exitCode: 0})

	tr.handleTaskCompletion(context.Background(), task.ID, task, CompletionCrashed)

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.updateStatusCalls) != 1 || client.updateStatusCalls[0].Status != "completed" {
		t.Fatalf("status updates = %+v, want one update to completed", client.updateStatusCalls)
	}
}

// The other side of the same coin: when the task really is still
// in_progress after the process exits, crashed stays crashed and the cap
// still parks it. The fix must not launder genuine failures.
func TestHandleTaskCompletion_CrashedAndNotTerminal_StillHitsCap(t *testing.T) {
	client := newMockClient()
	processMgr := newMockProcessMgr()
	tr := newTestRunner(client, newMockExecutor(), processMgr, newMockStateMgr())

	task := runningTaskForRetry(2, DefaultMaxTaskAttempts)
	task.StartedAt = time.Now()
	client.getEntryResult = map[string]*types.BrainEntry{
		task.Path: {Path: task.Path, Status: "in_progress"},
	}
	_ = processMgr.Add(task.ID, task, &fakeProcess{exited: true, exitCode: 0})

	tr.handleTaskCompletion(context.Background(), task.ID, task, CompletionCrashed)

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.updateStatusCalls) != 1 || client.updateStatusCalls[0].Status != "blocked" {
		t.Fatalf("status updates = %+v, want one update to blocked", client.updateStatusCalls)
	}
	var sawAttempt3 bool
	for _, m := range client.metadataCalls {
		if v, ok := m.Fields["attempt_count"]; ok && v == 3 {
			sawAttempt3 = true
		}
	}
	if !sawAttempt3 {
		t.Fatalf("attempt counter was not incremented to 3: %+v", client.metadataCalls)
	}
}

// If the re-check itself fails, an unreachable API is not evidence of
// success: keep the process-exit classification and let the retry logic run.
func TestHandleTaskCompletion_RecheckErrorKeepsCrashClassification(t *testing.T) {
	client := newMockClient()
	processMgr := newMockProcessMgr()
	tr := newTestRunner(client, newMockExecutor(), processMgr, newMockStateMgr())

	task := runningTaskForRetry(0, DefaultMaxTaskAttempts)
	task.StartedAt = time.Now()
	client.getEntryErr = errors.New("api unreachable")
	_ = processMgr.Add(task.ID, task, &fakeProcess{exited: true, exitCode: 0})

	tr.handleTaskCompletion(context.Background(), task.ID, task, CompletionCrashed)

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.updateStatusCalls) != 1 || client.updateStatusCalls[0].Status != "pending" {
		t.Fatalf("status updates = %+v, want one reset to pending", client.updateStatusCalls)
	}
}

func TestReconcileCompletionWithAPI_OnlyTouchesFailureStatuses(t *testing.T) {
	client := newMockClient()
	tr := newRetryTestRunner(client)
	task := runningTaskForRetry(0, 3)
	client.getEntryResult = map[string]*types.BrainEntry{
		task.Path: {Path: task.Path, Status: "blocked"},
	}

	for _, s := range []CompletionStatus{CompletionCompleted, CompletionCancelled, CompletionBlocked, CompletionRunning} {
		if got := tr.reconcileCompletionWithAPI(context.Background(), task, s); got != s {
			t.Errorf("reconcile(%q) = %q, want unchanged", s, got)
		}
	}
	if got := tr.reconcileCompletionWithAPI(context.Background(), task, CompletionCrashed); got != CompletionBlocked {
		t.Errorf("reconcile(crashed) with API blocked = %q, want %q", got, CompletionBlocked)
	}
}

// The process manager's status lookup is a separate http.Client from the
// runner's APIClient and never sent the bearer token, so against a server
// with auth enabled it was always a 401 → "status unknown" → crashed. It
// must authenticate like the rest of the runner.
func TestCheckCompletion_TaskEntryLookupSendsBearerToken(t *testing.T) {
	const token = "runner-token"
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if gotAuth != "Bearer "+token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"completed"}`))
	}))
	defer srv.Close()

	pm := NewProcessManager(RunnerConfig{BrainAPIURL: srv.URL, APIToken: token, APITimeout: 2000})
	task := scriptTask(false)
	task.ExecutorType = "opencode"
	registerFake(pm, task, &fakeProcess{exited: true, exitCode: 0})

	if got := pm.CheckCompletion(task.ID, true); got != CompletionCompleted {
		t.Fatalf("CheckCompletion = %q, want %q (auth header sent: %q)", got, CompletionCompleted, gotAuth)
	}
}

func TestCheckCompletion_ValidatedIsCompleted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"validated"}`))
	}))
	defer srv.Close()

	pm := NewProcessManager(RunnerConfig{BrainAPIURL: srv.URL, APITimeout: 2000})
	task := scriptTask(false)
	registerFake(pm, task, &fakeProcess{exited: true, exitCode: 0})

	if got := pm.CheckCompletion(task.ID, true); got != CompletionCompleted {
		t.Fatalf("CheckCompletion = %q, want %q for a validated task", got, CompletionCompleted)
	}
}

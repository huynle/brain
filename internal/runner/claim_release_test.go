package runner

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// These tests pin the fix for brain task qqpzi2wt: a task whose agent had
// already written a terminal status was reset to "pending" by renewClaims
// (its claim was released server-side at completion, so the next renewal
// failed, and the failure branch wrote pending unconditionally). The
// dispatcher then re-claimed finished work — 131 sessions on one task.

type releaseHarness struct {
	client    *mockClient
	executor  *mockExecutor
	processes *mockProcessMgr
	tr        *TaskRunner
	events    *[]RunnerEvent
	eventMu   *sync.Mutex
}

func newReleaseHarness(t *testing.T, apiStatus string, lookupErr error) releaseHarness {
	t.Helper()
	client := newMockClient()
	executor := newMockExecutor()
	pm := newMockProcessMgr()
	tr := newTestRunner(client, executor, pm, newMockStateMgr())

	task := testRunningTask("task1")
	task.InstanceID = "inst-task1"
	if err := pm.Add("task1", task, newMockProcess(100)); err != nil {
		t.Fatal(err)
	}
	if lookupErr != nil {
		client.getEntryErr = lookupErr
	} else {
		client.getEntryResult = map[string]*types.BrainEntry{
			task.Path: {Path: task.Path, Status: apiStatus},
		}
	}

	var mu sync.Mutex
	events := &[]RunnerEvent{}
	tr.OnEvent(func(e RunnerEvent) {
		mu.Lock()
		defer mu.Unlock()
		*events = append(*events, e)
	})
	return releaseHarness{client: client, executor: executor, processes: pm, tr: tr, events: events, eventMu: &mu}
}

func (h releaseHarness) releasedReason(t *testing.T) string {
	t.Helper()
	h.eventMu.Lock()
	defer h.eventMu.Unlock()
	for _, e := range *h.events {
		if e.Type == EventTaskReleased && e.TaskID == "task1" {
			return e.Reason
		}
	}
	t.Fatal("no EventTaskReleased for task1")
	return ""
}

func (h releaseHarness) assertReaped(t *testing.T) {
	t.Helper()
	h.processes.mu.Lock()
	kills := append([]string(nil), h.processes.killCalls...)
	h.processes.mu.Unlock()
	if len(kills) != 1 || kills[0] != "task1" {
		t.Errorf("kill calls = %v, want [task1]", kills)
	}
	if h.processes.Get("task1") != nil {
		t.Error("task1 should have been removed from the process manager")
	}
	h.executor.mu.Lock()
	cleanups := len(h.executor.cleanupCalls)
	h.executor.mu.Unlock()
	if cleanups != 1 {
		t.Errorf("executor cleanup calls = %d, want 1 (serve must be reaped too)", cleanups)
	}
}

func TestRenewClaims_CompletedTaskIsReapedNotReset(t *testing.T) {
	h := newReleaseHarness(t, "completed", nil)
	h.client.renewErr = errors.New("claim not found")

	h.tr.renewClaims(context.Background())

	h.assertReaped(t)
	if calls := h.client.getUpdateStatusCalls(); len(calls) != 0 {
		t.Fatalf("a completed task must not be written back to pending, got %+v", calls)
	}
	if r := h.releasedReason(t); !strings.Contains(r, "already completed") {
		t.Errorf("release reason = %q, want it to say the task was already completed", r)
	}
	h.tr.mu.RLock()
	defer h.tr.mu.RUnlock()
	if h.tr.stats.Failed != 0 || h.tr.stats.Completed != 1 {
		t.Errorf("stats = failed %d / completed %d, want 0 / 1", h.tr.stats.Failed, h.tr.stats.Completed)
	}
}

func TestRenewClaims_BlockedTaskKeepsItsStatus(t *testing.T) {
	// An agent (or operator) that parked a task in blocked made a decision;
	// the runner reaping a leftover process must not undo it.
	h := newReleaseHarness(t, "blocked", nil)
	h.client.renewErr = errors.New("claim not found")

	h.tr.renewClaims(context.Background())

	h.assertReaped(t)
	if calls := h.client.getUpdateStatusCalls(); len(calls) != 0 {
		t.Fatalf("a blocked task must not be reset to pending, got %+v", calls)
	}
	h.tr.mu.RLock()
	defer h.tr.mu.RUnlock()
	if h.tr.stats.Failed != 0 || h.tr.stats.Completed != 0 {
		t.Errorf("stats = failed %d / completed %d, want 0 / 0", h.tr.stats.Failed, h.tr.stats.Completed)
	}
}

func TestRenewClaims_InProgressTaskStillResetToPending(t *testing.T) {
	// The legitimate case the branch exists for: the claim expired or was
	// force-released while the task really is still running elsewhere.
	h := newReleaseHarness(t, "in_progress", nil)
	h.client.renewErr = errors.New("claim expired")

	h.tr.renewClaims(context.Background())

	h.assertReaped(t)
	calls := h.client.getUpdateStatusCalls()
	if len(calls) != 1 || calls[0].Status != "pending" {
		t.Fatalf("status updates = %+v, want one pending", calls)
	}
	if r := h.releasedReason(t); r != "claim renewal failed" {
		t.Errorf("release reason = %q", r)
	}
}

func TestRenewClaims_StatusLookupFailureKeepsLegacyReset(t *testing.T) {
	// An unreachable API is not evidence the task finished; fall back to the
	// reset rather than leave an orphaned in_progress task stuck forever.
	h := newReleaseHarness(t, "", errors.New("api down"))
	h.client.renewErr = errors.New("claim not found")

	h.tr.renewClaims(context.Background())

	h.assertReaped(t)
	calls := h.client.getUpdateStatusCalls()
	if len(calls) != 1 || calls[0].Status != "pending" {
		t.Fatalf("status updates = %+v, want one pending", calls)
	}
}

func TestBridgeAbortTask_CompletedTaskIsNotResetToPending(t *testing.T) {
	h := newReleaseHarness(t, "completed", nil)
	bc := NewBridgeClient(h.tr)
	bc.ctx = context.Background()

	if err := bc.abortTask("task1"); err != nil {
		t.Fatalf("abortTask: %v", err)
	}

	h.assertReaped(t)
	if calls := h.client.getUpdateStatusCalls(); len(calls) != 0 {
		t.Fatalf("abort of a completed task must not write pending, got %+v", calls)
	}
	if r := h.releasedReason(t); !strings.Contains(r, "already completed") {
		t.Errorf("release reason = %q", r)
	}
}

package runner

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// stubSessionAborter overrides the /session/{id}/abort indirection for the
// test's duration and records the (port, sessionID) it was called with.
func stubSessionAborter(t *testing.T, err error) *struct {
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
	prev := sessionAborter
	sessionAborter = func(port int, sessionID string) error {
		rec.called = true
		rec.port = port
		rec.sessionID = sessionID
		return err
	}
	t.Cleanup(func() { sessionAborter = prev })
	return rec
}

// stubPendingPermissions overrides the pending-permission lookup so the stall
// path can be driven with a known count without a live bridge cache.
func stubPendingPermissions(t *testing.T, count int) {
	t.Helper()
	prev := pendingPermissionsForTask
	pendingPermissionsForTask = func(*TaskRunner, RunningTask) int { return count }
	t.Cleanup(func() { pendingPermissionsForTask = prev })
}

// permEvent builds a `data:`-prefixed SSE line carrying an OpenCode
// permission.* event with the id at properties.id.
func permEventLine(eventType, permID string) []byte {
	raw, _ := json.Marshal(map[string]interface{}{
		"type": eventType,
		"properties": map[string]interface{}{
			"id":   permID,
			"info": map[string]interface{}{"id": permID},
		},
	})
	return append([]byte("data: "), raw...)
}

// -----------------------------------------------------------------------------
// postAbort indirection
// -----------------------------------------------------------------------------

func TestPostAbort_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/session/ses_abc/abort" {
			t.Errorf("path = %q, want /session/ses_abc/abort", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	port := serverPort(t, srv)

	if err := postAbort(port, "ses_abc"); err != nil {
		t.Fatalf("postAbort returned error on 2xx: %v", err)
	}
}

func TestPostAbort_Non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	port := serverPort(t, srv)

	if err := postAbort(port, "ses_abc"); err == nil {
		t.Fatal("postAbort should return error on non-2xx")
	}
}

// -----------------------------------------------------------------------------
// BridgeClient pending-permission tracking
// -----------------------------------------------------------------------------

func TestBridgeClient_PendingPermissionCount(t *testing.T) {
	tr := &TaskRunner{
		config: RunnerConfig{APITimeout: 5000},
		logger: log.New(io.Discard, "", 0),
	}
	bc := NewBridgeClient(tr)
	const inst = "inst_perm01"

	if got := bc.PendingPermissionCount(inst); got != 0 {
		t.Fatalf("initial count = %d, want 0", got)
	}

	bc.handleEventLine(inst, permEventLine("permission.asked", "perm-1"))
	if got := bc.PendingPermissionCount(inst); got != 1 {
		t.Fatalf("after asked count = %d, want 1", got)
	}

	// A duplicate asked/updated for the same id doesn't double-count.
	bc.handleEventLine(inst, permEventLine("permission.updated", "perm-1"))
	if got := bc.PendingPermissionCount(inst); got != 1 {
		t.Fatalf("after updated (same id) count = %d, want 1", got)
	}

	bc.handleEventLine(inst, permEventLine("permission.replied", "perm-1"))
	if got := bc.PendingPermissionCount(inst); got != 0 {
		t.Fatalf("after replied count = %d, want 0", got)
	}

	// Unknown instance stays 0.
	if got := bc.PendingPermissionCount("nope"); got != 0 {
		t.Fatalf("unknown instance count = %d, want 0", got)
	}
}

// -----------------------------------------------------------------------------
// Stall detection + recovery integration via checkOpencodeIdleStatus
// -----------------------------------------------------------------------------

// stallTestRunner wires a TaskRunner with mocks plus a live (non-nil) bridge
// client so the abort-gating (B4) is satisfied. StallTimeout is set on the
// config.
func stallTestRunner(t *testing.T, stallTimeoutMs int) (*TaskRunner, *mockProcessMgr, *mockClient) {
	t.Helper()
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()

	cfg := testRunnerConfig()
	cfg.IdleDetectionThreshold = 60000
	cfg.StallTimeout = stallTimeoutMs

	tr := NewTaskRunner(TaskRunnerOptions{
		Projects:   []string{"proj-a"},
		Config:     cfg,
		Mode:       ExecutionModeHeadless,
		Client:     client,
		Executor:   executor,
		ProcessMgr: processMgr,
		StateMgr:   stateMgr,
	})
	tr.setBridgeClient(NewBridgeClient(tr))
	return tr, processMgr, client
}

// busyStatusServer returns a server reporting the session busy via
// /session/status.
func busyStatusServer(t *testing.T, sessionID string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			sessionID: map[string]interface{}{"type": "busy"},
		})
	}))
}

// Genuinely busy (turn NOT ended) + pending==0 + LastActivity ancient +
// StallTimeout>0 ⇒ recovery: abort invoked, marker appended, metadata set,
// StallRecovered set. A SECOND stall edge escalates to blocked.
func TestCheckIdleStatus_Stall_RecoversThenEscalates(t *testing.T) {
	server := busyStatusServer(t, "ses_abc")
	defer server.Close()
	port := serverPort(t, server)

	// NOT turn-ended: assistant message with completed==0.
	body := transcriptJSON(t, msgJSON(t, "assistant", 2000, 0))
	stubSessionHistory(t, body, nil)
	stubPendingPermissions(t, 0)
	abortRec := stubSessionAborter(t, nil)

	stallMs := 60000 // 60s
	tr, pm, client := stallTestRunner(t, stallMs)

	proc := newMockProcess(100)
	task := steerTask(port, "ses_abc", false)
	task.CompleteOnIdle = true
	// LastActivity well past the stall timeout so it's stale immediately.
	task.LastActivity = time.Now().UTC().Add(-2 * time.Duration(stallMs) * time.Millisecond)
	pm.Add(task.ID, task, proc)

	// FIRST stall edge → recovery.
	tr.checkIdleStatus(context.Background())

	if !abortRec.called {
		t.Fatal("sessionAborter should have been invoked on the first stall edge")
	}
	if abortRec.port != port || abortRec.sessionID != "ses_abc" {
		t.Errorf("abort called with (%d,%q), want (%d,ses_abc)", abortRec.port, abortRec.sessionID, port)
	}
	info := pm.Get(task.ID)
	if info == nil {
		t.Fatal("task should still be tracked after recovery")
	}
	if !info.Task.StallRecovered {
		t.Error("StallRecovered should be set after first recovery")
	}
	// Marker appended.
	foundMarker := false
	for _, a := range client.appendCalls {
		if a.TaskPath == task.Path && containsMarker(a.Content) {
			foundMarker = true
		}
	}
	if !foundMarker {
		t.Errorf("expected a stall marker append to %s, got %+v", task.Path, client.appendCalls)
	}
	// Metadata stalled=true set.
	foundMeta := false
	for _, m := range client.metadataWrites() {
		if m.Path == task.Path {
			if v, ok := m.Fields["stalled"]; ok && v == true {
				foundMeta = true
			}
		}
	}
	if !foundMeta {
		t.Errorf("expected stalled=true metadata write to %s, got %+v", task.Path, client.metadataWrites())
	}

	// SECOND stall edge: StallRecovered already true and still stale ⇒ escalate
	// to blocked. Re-fetch the tracked task (recovery advanced LastActivity, so
	// re-age it to simulate "recovery didn't clear it within one window").
	updated := pm.Get(task.ID).Task
	updated.LastActivity = time.Now().UTC().Add(-2 * time.Duration(stallMs) * time.Millisecond)
	pm.mu.Lock()
	pm.processes[task.ID].Task = updated
	pm.mu.Unlock()

	tr.checkIdleStatus(context.Background())

	// Escalation marks blocked.
	blocked := false
	for _, u := range client.getUpdateStatusCalls() {
		if u.TaskPath == task.Path && u.Status == "blocked" {
			blocked = true
		}
	}
	if !blocked {
		t.Errorf("expected task marked blocked on second stall edge, got %+v", client.getUpdateStatusCalls())
	}
	if pm.Get(task.ID) != nil {
		t.Error("task should have been removed from process manager after escalation")
	}
}

// Pending permissions > 0 ⇒ never abort, no marker (a real permission prompt is
// legitimate waiting).
func TestCheckIdleStatus_Stall_PendingPermissionsBlocksAbort(t *testing.T) {
	server := busyStatusServer(t, "ses_abc")
	defer server.Close()
	port := serverPort(t, server)

	body := transcriptJSON(t, msgJSON(t, "assistant", 2000, 0))
	stubSessionHistory(t, body, nil)
	stubPendingPermissions(t, 1) // a real prompt is outstanding
	abortRec := stubSessionAborter(t, nil)

	stallMs := 60000
	tr, pm, client := stallTestRunner(t, stallMs)

	proc := newMockProcess(100)
	task := steerTask(port, "ses_abc", false)
	task.LastActivity = time.Now().UTC().Add(-2 * time.Duration(stallMs) * time.Millisecond)
	pm.Add(task.ID, task, proc)

	tr.checkIdleStatus(context.Background())

	if abortRec.called {
		t.Error("sessionAborter must NOT be invoked when pending permissions > 0")
	}
	for _, a := range client.appendCalls {
		if containsMarker(a.Content) {
			t.Error("no stall marker should be appended when pending permissions > 0")
		}
	}
	info := pm.Get(task.ID)
	if info == nil {
		t.Fatal("task should still be tracked")
	}
	if info.Task.StallRecovered {
		t.Error("StallRecovered must not be set when pending permissions > 0")
	}
}

// Disabled: StallTimeout==0 ⇒ no stall path at all (no abort, no marker) even
// when LastActivity is ancient.
func TestCheckIdleStatus_Stall_DisabledNoop(t *testing.T) {
	server := busyStatusServer(t, "ses_abc")
	defer server.Close()
	port := serverPort(t, server)

	body := transcriptJSON(t, msgJSON(t, "assistant", 2000, 0))
	stubSessionHistory(t, body, nil)
	stubPendingPermissions(t, 0)
	abortRec := stubSessionAborter(t, nil)

	tr, pm, client := stallTestRunner(t, 0) // disabled

	proc := newMockProcess(100)
	task := steerTask(port, "ses_abc", false)
	task.LastActivity = time.Now().UTC().Add(-24 * time.Hour)
	pm.Add(task.ID, task, proc)

	tr.checkIdleStatus(context.Background())

	if abortRec.called {
		t.Error("sessionAborter must NOT be invoked when StallTimeout is disabled")
	}
	for _, a := range client.appendCalls {
		if containsMarker(a.Content) {
			t.Error("no stall marker should be appended when StallTimeout is disabled")
		}
	}
}

// First stall observation with a zero LastActivity seeds the stall clock and
// returns without aborting (can't judge staleness without a baseline).
func TestCheckIdleStatus_Stall_SeedsLastActivityWhenZero(t *testing.T) {
	server := busyStatusServer(t, "ses_abc")
	defer server.Close()
	port := serverPort(t, server)

	body := transcriptJSON(t, msgJSON(t, "assistant", 2000, 0))
	stubSessionHistory(t, body, nil)
	stubPendingPermissions(t, 0)
	abortRec := stubSessionAborter(t, nil)

	tr, pm, _ := stallTestRunner(t, 60000)

	proc := newMockProcess(100)
	task := steerTask(port, "ses_abc", false)
	task.LastActivity = time.Time{} // zero — no baseline yet
	pm.Add(task.ID, task, proc)

	tr.checkIdleStatus(context.Background())

	if abortRec.called {
		t.Error("sessionAborter must NOT be invoked on the seeding tick")
	}
	info := pm.Get(task.ID)
	if info == nil {
		t.Fatal("task should still be tracked")
	}
	if info.Task.LastActivity.IsZero() {
		t.Error("LastActivity should have been seeded on the first stall observation")
	}
}

// containsMarker reports whether a note body carries the runner-side stall
// marker literal (cross-referenced with service.StalledMarker).
func containsMarker(s string) bool {
	return strings.Contains(s, stalledNoteMarker)
}

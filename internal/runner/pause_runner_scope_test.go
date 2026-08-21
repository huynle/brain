package runner

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/huynle/brain-api/internal/sse"
	"github.com/huynle/brain-api/internal/types"
)

// Tests for the runner-scoped pause dial (PUT /runners/{runnerId}/pause).
//
// This is a different axis from the per-project dials covered by
// pause_server_origin_test.go: it pauses ONE runner regardless of which
// projects it serves, and its durable home is the runner registry row —
// not project_pause_state. The bug these tests lock down: the runner-scoped
// pause used to land in serverTasksPaused[""] (the *project* snapshot), which
// syncServerPauseState replaces wholesale on every poll tick from
// GetRunnerStatus. Since GetRunnerStatus only ever reports project pauses, a
// runner-scoped pause silently evaporated within one poll interval and the
// runner went right back to acking pushed dispatches.

func newRunnerScopePauseRunner(t *testing.T, client *mockClient) *TaskRunner {
	t.Helper()
	return NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"demo"}, Config: testRunnerConfig(), Mode: ExecutionModeHeadless,
		Client: client, Executor: newMockExecutor(), ProcessMgr: newMockProcessMgr(), StateMgr: newMockStateMgr(),
	})
}

// A runner-scoped pause must survive the periodic project-pause reconcile.
// GetRunnerStatus reports no paused projects (correctly — pausing a runner
// does not pause any project), so a reconcile that owns the runner dial too
// would wipe it.
func TestRunnerScopePause_SurvivesProjectReconcile(t *testing.T) {
	client := newMockClient()
	client.runnerStatus = &types.RunnerStatusResponse{Running: true}
	tr := newRunnerScopePauseRunner(t, client)
	// The API persisted the pause on the runner's registry row; no project
	// is paused, because pausing a runner is not a project-level change.
	client.runnerRecord = &types.RunnerInfo{RunnerID: tr.runnerID, Paused: true}

	tr.handleCommand(context.Background(), RunnerCommand{Type: CommandPause, Scope: "runner"})
	if !tr.IsRunnerPaused() {
		t.Fatal("IsRunnerPaused() = false, want true right after the SSE runner pause")
	}

	// One poll tick.
	tr.syncServerPauseState(context.Background())

	if !tr.IsRunnerPaused() {
		t.Error("IsRunnerPaused() = false after project reconcile, want true (runner dial is not project state)")
	}
}

// The dispatch gate must reject pushed leases while the runner is paused.
// This is the exact production symptom: the API answered {"action":"pause",
// "success":true} and the headless runner kept logging "dispatch acked;
// spawning task" on every scheduler tick.
func TestHandleDispatchCommand_RejectsWhenRunnerPaused(t *testing.T) {
	client := newMockClient()
	client.runnerStatus = &types.RunnerStatusResponse{Running: true}
	processMgr := newMockProcessMgr()
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"demo"}, Config: testRunnerConfig(), Mode: ExecutionModeHeadless,
		Client: client, Executor: newMockExecutor(), ProcessMgr: processMgr, StateMgr: newMockStateMgr(),
	})
	client.runnerRecord = &types.RunnerInfo{RunnerID: tr.runnerID, Paused: true}

	tr.handleCommand(context.Background(), RunnerCommand{Type: CommandPause, Scope: "runner"})
	// A poll tick lands between the pause and the dispatch, as it always
	// does in production.
	tr.syncServerPauseState(context.Background())

	task := testTask("task1", "demo")
	tr.handleCommand(context.Background(), RunnerCommand{
		Type: CommandDispatch, ProjectID: "demo", TaskID: "task1", LeaseID: "lease-1", Task: task,
	})

	rejects := client.getRejectCalls()
	if len(rejects) != 1 || rejects[0].Reason.Code != "runner_paused" {
		t.Fatalf("rejects = %+v, want exactly one runner_paused rejection", rejects)
	}
	if got := processMgr.Count(); got != 0 {
		t.Errorf("process count = %d after rejection, want 0 (reservation released)", got)
	}
}

// A paused runner must not run automation-generated work either. The
// automation carve-out exists so that "tasks paused, autos on" keeps
// automations flowing for a *project*; pausing the runner itself means
// "this machine does nothing".
func TestHandleDispatchCommand_RunnerPauseBeatsAutomationCarveOut(t *testing.T) {
	client := newMockClient()
	client.runnerStatus = &types.RunnerStatusResponse{Running: true}
	processMgr := newMockProcessMgr()
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"demo"}, Config: testRunnerConfig(), Mode: ExecutionModeHeadless,
		Client: client, Executor: newMockExecutor(), ProcessMgr: processMgr, StateMgr: newMockStateMgr(),
	})

	tr.handleCommand(context.Background(), RunnerCommand{Type: CommandPause, Scope: "runner"})

	task := testTask("task1", "demo")
	task.GeneratedBy = "automation:nightly"
	tr.handleCommand(context.Background(), RunnerCommand{
		Type: CommandDispatch, ProjectID: "demo", TaskID: "task1", LeaseID: "lease-1", Task: task,
	})

	rejects := client.getRejectCalls()
	if len(rejects) != 1 || rejects[0].Reason.Code != "runner_paused" {
		t.Fatalf("rejects = %+v, want exactly one runner_paused rejection for an automation task", rejects)
	}
}

// The matching resume clears the dial.
func TestRunnerScopeResume_ClearsPause(t *testing.T) {
	client := newMockClient()
	client.runnerStatus = &types.RunnerStatusResponse{Running: true}
	tr := newRunnerScopePauseRunner(t, client)

	tr.handleCommand(context.Background(), RunnerCommand{Type: CommandPause, Scope: "runner"})
	tr.handleCommand(context.Background(), RunnerCommand{Type: CommandResume, Scope: "runner"})

	if tr.IsRunnerPaused() {
		t.Error("IsRunnerPaused() = true after SSE runner resume, want false")
	}
}

// A runner-scoped pause must not leak into the project dials — the PWA
// reads those separately and a runner pause should leave "demo" itself
// unpaused for other runners.
func TestRunnerScopePause_DoesNotTouchProjectDials(t *testing.T) {
	client := newMockClient()
	client.runnerStatus = &types.RunnerStatusResponse{Running: true}
	tr := newRunnerScopePauseRunner(t, client)

	tr.handleCommand(context.Background(), RunnerCommand{Type: CommandPause, Scope: "runner"})

	tr.pauseMu.RLock()
	serverTasks := len(tr.serverTasksPaused)
	serverAutos := len(tr.serverAutosPaused)
	tr.pauseMu.RUnlock()

	if serverTasks != 0 || serverAutos != 0 {
		t.Errorf("server pause maps = tasks:%d autos:%d, want both empty (runner scope is not project state)",
			serverTasks, serverAutos)
	}
}

// A missed resume must heal: the runner re-reads its own registry record on
// each reconcile, so if the server says the runner is no longer paused the
// local dial clears even when the SSE resume was lost across a reconnect.
func TestSyncServerPauseState_HealsMissedRunnerResume(t *testing.T) {
	client := newMockClient()
	client.runnerStatus = &types.RunnerStatusResponse{Running: true}
	tr := newRunnerScopePauseRunner(t, client)

	tr.handleCommand(context.Background(), RunnerCommand{Type: CommandPause, Scope: "runner"})
	if !tr.IsRunnerPaused() {
		t.Fatal("IsRunnerPaused() = false, want true after SSE runner pause")
	}

	// Server-side the operator resumed the runner; the SSE event was lost.
	client.mu.Lock()
	client.runnerRecord = &types.RunnerInfo{RunnerID: tr.runnerID, Paused: false}
	client.mu.Unlock()

	tr.syncServerPauseState(context.Background())

	if tr.IsRunnerPaused() {
		t.Error("IsRunnerPaused() = true after reconcile, want false (missed resume must heal)")
	}
}

// The inverse: a pause the runner never heard about (SSE event lost, or the
// runner restarted after the pause) must be picked up from its registry row.
func TestSyncServerPauseState_PicksUpMissedRunnerPause(t *testing.T) {
	client := newMockClient()
	client.runnerStatus = &types.RunnerStatusResponse{Running: true}
	tr := newRunnerScopePauseRunner(t, client)
	client.mu.Lock()
	client.runnerRecord = &types.RunnerInfo{RunnerID: tr.runnerID, Paused: true}
	client.mu.Unlock()

	tr.syncServerPauseState(context.Background())

	if !tr.IsRunnerPaused() {
		t.Error("IsRunnerPaused() = false, want true from the runner's registry row")
	}
}

// A failed registry read must keep the previous dial — "unknown" is not
// "resumed". Same invariant the project snapshot already holds.
func TestSyncServerPauseState_KeepsRunnerPauseOnFetchError(t *testing.T) {
	client := newMockClient()
	client.runnerStatus = &types.RunnerStatusResponse{Running: true}
	tr := newRunnerScopePauseRunner(t, client)

	tr.handleCommand(context.Background(), RunnerCommand{Type: CommandPause, Scope: "runner"})

	client.mu.Lock()
	client.runnerRecordErr = errMockFailure
	client.mu.Unlock()

	tr.syncServerPauseState(context.Background())

	if !tr.IsRunnerPaused() {
		t.Error("IsRunnerPaused() = false after a failed reconcile, want true (snapshot must be kept)")
	}
}

// End-to-end over the wire format: the SSE envelope the API publishes for
// PUT /runners/{runnerId}/pause must arrive with Scope populated.
//
// The original bug lived exactly here. The handler published a nil payload,
// handleCommandEvent's envelope branch skips a null payload entirely, and the
// command reached applyPauseCommand with Scope="" and ProjectID="" — i.e.
// indistinguishable from a global *project* pause. It was filed in the
// project snapshot and reconciled away one poll tick later.
func TestSSEListener_RunnerPauseEnvelopeCarriesScope(t *testing.T) {
	wakeCh := make(chan struct{}, 10)
	commandCh := make(chan RunnerCommand, 10)
	listener := NewSSEListener("http://example.invalid", "", nil, wakeCh)
	listener.SetRunnerStream("runner_test", commandCh)

	// Byte-identical to what Hub.PublishRunnerCommand emits for the
	// runner-scoped pause (see api.runnerPauseScopePayload).
	data, err := json.Marshal(map[string]interface{}{
		"command": "pause",
		"payload": map[string]interface{}{"scope": "runner"},
	})
	if err != nil {
		t.Fatalf("marshal command envelope: %v", err)
	}

	listener.handleCommandEvent(sse.Event{Data: data}, "runner_test")

	select {
	case cmd := <-commandCh:
		if cmd.Type != CommandPause {
			t.Fatalf("command type = %q, want %q", cmd.Type, CommandPause)
		}
		if cmd.Scope != PauseScopeRunner {
			t.Fatalf("scope = %q, want %q — without it the runner files this as a project pause and reconciles it away", cmd.Scope, PauseScopeRunner)
		}
		if cmd.ProjectID != "" {
			t.Errorf("projectId = %q, want empty", cmd.ProjectID)
		}
	default:
		t.Fatal("expected a pause command from the envelope")
	}
}

// The runner-scoped pause must hold across the whole command path: parse the
// wire envelope, apply it, reconcile a poll tick, then reject a pushed lease.
func TestRunnerScopePause_EndToEndFromSSEEnvelope(t *testing.T) {
	client := newMockClient()
	client.runnerStatus = &types.RunnerStatusResponse{Running: true}
	processMgr := newMockProcessMgr()
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"demo"}, Config: testRunnerConfig(), Mode: ExecutionModeHeadless,
		Client: client, Executor: newMockExecutor(), ProcessMgr: processMgr, StateMgr: newMockStateMgr(),
	})
	client.runnerRecord = &types.RunnerInfo{RunnerID: tr.runnerID, Paused: true}

	commandCh := make(chan RunnerCommand, 10)
	listener := NewSSEListener("http://example.invalid", "", nil, make(chan struct{}, 10))
	listener.SetRunnerStream(tr.runnerID, commandCh)
	data, err := json.Marshal(map[string]interface{}{
		"command": "pause",
		"payload": map[string]interface{}{"scope": "runner"},
	})
	if err != nil {
		t.Fatalf("marshal command envelope: %v", err)
	}
	listener.handleCommandEvent(sse.Event{Data: data}, tr.runnerID)
	tr.handleCommand(context.Background(), <-commandCh)

	tr.syncServerPauseState(context.Background())

	tr.handleCommand(context.Background(), RunnerCommand{
		Type: CommandDispatch, ProjectID: "demo", TaskID: "task1", LeaseID: "lease-1",
		Task: testTask("task1", "demo"),
	})

	rejects := client.getRejectCalls()
	if len(rejects) != 1 || rejects[0].Reason.Code != "runner_paused" {
		t.Fatalf("rejects = %+v, want exactly one runner_paused rejection", rejects)
	}
}

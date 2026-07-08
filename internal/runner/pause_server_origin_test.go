package runner

import (
	"context"
	"errors"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

var errMockFailure = errors.New("mock failure")

// These tests cover the server-origin pause snapshot: SSE pause commands
// land in serverTasksPaused/serverAutosPaused, syncServerPauseState
// reconciles those maps against GetRunnerStatus each poll tick, and
// TUI-local pauses are never clobbered by that reconciliation.

func newPauseTestRunner(t *testing.T, client *mockClient) *TaskRunner {
	t.Helper()
	return NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"}, Config: testRunnerConfig(), Mode: ExecutionModeHeadless,
		Client: client, Executor: newMockExecutor(), ProcessMgr: newMockProcessMgr(), StateMgr: newMockStateMgr(),
	})
}

// A pause delivered via SSE whose matching resume event is lost (stream
// reconnect) must heal on the next reconcile — otherwise the runner
// rejects every dispatch as runner_paused until someone manually toggles
// pause again.
func TestSyncServerPauseState_HealsMissedResume(t *testing.T) {
	client := newMockClient()
	tr := newPauseTestRunner(t, client)

	tr.handleCommand(context.Background(), RunnerCommand{Type: CommandPause, Scope: "tasks"})
	if !tr.IsAllPaused() {
		t.Fatal("IsAllPaused() = false, want true after SSE global pause")
	}

	// Server-side the user has resumed, but the SSE resume event was lost.
	client.mu.Lock()
	client.runnerStatus = &types.RunnerStatusResponse{Running: true}
	client.mu.Unlock()

	tr.syncServerPauseState(context.Background())

	if tr.IsAllPaused() {
		t.Error("IsAllPaused() = true after reconcile, want false (missed resume must heal)")
	}
	if tr.IsPaused("proj-a") {
		t.Error("IsPaused(proj-a) = true after reconcile, want false")
	}
}

// The inverse direction: a pause the runner never heard about (missed SSE
// pause event) must be picked up from GetRunnerStatus.
func TestSyncServerPauseState_PicksUpMissedPause(t *testing.T) {
	client := newMockClient()
	client.runnerStatus = &types.RunnerStatusResponse{
		Running:                  true,
		PausedProjects:           []string{"proj-a"},
		AutomationPausedProjects: []string{"proj-b"},
	}
	tr := newPauseTestRunner(t, client)

	tr.syncServerPauseState(context.Background())

	if !tr.IsPaused("proj-a") {
		t.Error("IsPaused(proj-a) = false, want true from server snapshot")
	}
	if !tr.IsAutomationsPausedForProject("proj-b") {
		t.Error("IsAutomationsPausedForProject(proj-b) = false, want true from server snapshot")
	}
	if tr.IsAllPaused() {
		t.Error("IsAllPaused() = true, want false (only proj-a tasks are paused)")
	}
}

// Reconciliation replaces only the server-origin maps. A pause set locally
// (embedded TUI controller calls PauseProject directly; the server never
// learns about it) must survive a reconcile that reports nothing paused.
func TestSyncServerPauseState_DoesNotClobberLocalPause(t *testing.T) {
	client := newMockClient()
	client.runnerStatus = &types.RunnerStatusResponse{Running: true}
	tr := newPauseTestRunner(t, client)

	tr.PauseProject("proj-a")
	tr.PauseAll()

	tr.syncServerPauseState(context.Background())

	if !tr.IsPaused("proj-a") {
		t.Error("IsPaused(proj-a) = false, want true (local pause must survive reconcile)")
	}
	if !tr.IsAllPaused() {
		t.Error("IsAllPaused() = false, want true (local global pause must survive reconcile)")
	}
}

// An explicit SSE resume overrides a pause regardless of origin. A runner
// started with StartPaused is resumed from the PWA exactly this way, so the
// resume must clear the local flags too.
func TestApplyPauseCommand_ResumeClearsLocalPause(t *testing.T) {
	client := newMockClient()
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"}, Config: testRunnerConfig(), Mode: ExecutionModeHeadless,
		Client: client, Executor: newMockExecutor(), ProcessMgr: newMockProcessMgr(), StateMgr: newMockStateMgr(),
		StartPaused: true,
	})
	if !tr.IsAllPaused() || !tr.IsAutomationsPaused() {
		t.Fatal("StartPaused runner should begin fully paused")
	}

	tr.handleCommand(context.Background(), RunnerCommand{Type: CommandResume, Scope: "all"})

	if tr.IsAllPaused() {
		t.Error("IsAllPaused() = true after SSE resume, want false")
	}
	if tr.IsAutomationsPaused() {
		t.Error("IsAutomationsPaused() = true after SSE resume, want false")
	}
}

// A transient GetRunnerStatus failure must keep the previous snapshot —
// "unknown" is not "nothing paused".
func TestSyncServerPauseState_KeepsSnapshotOnFetchError(t *testing.T) {
	client := newMockClient()
	tr := newPauseTestRunner(t, client)

	tr.handleCommand(context.Background(), RunnerCommand{Type: CommandPause, ProjectID: "proj-a", Scope: "tasks"})
	if !tr.IsPaused("proj-a") {
		t.Fatal("IsPaused(proj-a) = false, want true after SSE pause")
	}

	client.mu.Lock()
	client.runnerStatusErr = errMockFailure
	client.mu.Unlock()

	tr.syncServerPauseState(context.Background())

	if !tr.IsPaused("proj-a") {
		t.Error("IsPaused(proj-a) = false after failed reconcile, want true (snapshot must be kept)")
	}
}

// The server-origin pause snapshot must gate dispatches: a dispatch that
// arrives while the snapshot says the project is paused is rejected with
// runner_paused and its slot reservation released.
func TestHandleDispatchCommand_RejectsWhenServerSnapshotPaused(t *testing.T) {
	client := newMockClient()
	client.runnerStatus = &types.RunnerStatusResponse{Running: true, PausedProjects: []string{"proj-a"}}
	processMgr := newMockProcessMgr()
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"}, Config: testRunnerConfig(), Mode: ExecutionModeHeadless,
		Client: client, Executor: newMockExecutor(), ProcessMgr: processMgr, StateMgr: newMockStateMgr(),
	})
	tr.syncServerPauseState(context.Background())

	task := testTask("task1", "proj-a")
	tr.handleCommand(context.Background(), RunnerCommand{
		Type: CommandDispatch, ProjectID: "proj-a", TaskID: "task1", LeaseID: "lease-1", Task: task,
	})

	rejects := client.getRejectCalls()
	if len(rejects) != 1 || rejects[0].Reason.Code != "runner_paused" {
		t.Fatalf("rejects = %+v, want exactly one runner_paused rejection", rejects)
	}
	if got := processMgr.Count(); got != 0 {
		t.Errorf("process count = %d after rejection, want 0 (reservation released)", got)
	}
}

// Regression: the aggregate `Paused` field on RunnerStatusResponse is
// `len(PausedProjects) > 0` on the API side — it is NOT a global-pause
// switch. Previously fetchServerPauseState promoted that aggregate into the
// serverTasksPaused[""] sentinel, which made serverPausedFor(projectID)
// return true for ANY project as long as SOME (unrelated) project was
// paused. Symptom seen in production 2026-07-07: 43 stale project pauses
// left `personal-productivity` receiving runner_paused rejections on every
// dispatch even though its own row was not paused. The runner must gate
// dispatches only on the explicit PausedProjects list.
func TestSyncServerPauseState_AggregatePausedDoesNotBlockOtherProjects(t *testing.T) {
	client := newMockClient()
	// Simulates the real API response: 1 project paused, so the aggregate
	// flag flips true. `proj-b` is NOT in the paused list.
	client.runnerStatus = &types.RunnerStatusResponse{
		Running:                  true,
		Paused:                   true,
		PausedProjects:           []string{"proj-a"},
		AutomationsPaused:        true,
		AutomationPausedProjects: []string{"proj-a"},
	}
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a", "proj-b"}, Config: testRunnerConfig(), Mode: ExecutionModeHeadless,
		Client: client, Executor: newMockExecutor(), ProcessMgr: newMockProcessMgr(), StateMgr: newMockStateMgr(),
	})

	tr.syncServerPauseState(context.Background())

	if !tr.IsPaused("proj-a") {
		t.Error("IsPaused(proj-a) = false, want true (explicitly in PausedProjects)")
	}
	if tr.IsPaused("proj-b") {
		t.Error("IsPaused(proj-b) = true, want false (aggregate Paused must not leak across projects)")
	}
	if !tr.IsAutomationsPausedForProject("proj-a") {
		t.Error("IsAutomationsPausedForProject(proj-a) = false, want true")
	}
	if tr.IsAutomationsPausedForProject("proj-b") {
		t.Error("IsAutomationsPausedForProject(proj-b) = true, want false (aggregate AutomationsPaused must not leak)")
	}
}

// Regression pair: the same aggregate-leak defect at the dispatch gate.
// A non-force dispatch for `proj-b` must succeed when only `proj-a` is
// paused, even though the API's derived `Paused` flag is true.
func TestHandleDispatchCommand_AggregatePausedDoesNotRejectOtherProject(t *testing.T) {
	client := newMockClient()
	client.runnerStatus = &types.RunnerStatusResponse{
		Running: true, Paused: true, PausedProjects: []string{"proj-a"},
	}
	client.readyTasks["proj-b"] = []types.ResolvedTask{*testTask("task-b", "proj-b")}
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a", "proj-b"}, Config: testRunnerConfig(), Mode: ExecutionModeHeadless,
		Client: client, Executor: executor, ProcessMgr: processMgr, StateMgr: newMockStateMgr(),
	})
	tr.syncServerPauseState(context.Background())

	task := testTask("task-b", "proj-b")
	tr.handleCommand(context.Background(), RunnerCommand{
		Type: CommandDispatch, ProjectID: "proj-b", TaskID: "task-b", LeaseID: "lease-b", Task: task,
	})

	if got := len(client.getRejectCalls()); got != 0 {
		t.Fatalf("must not reject dispatch for unpaused proj-b, got %d rejects: %+v",
			got, client.getRejectCalls())
	}
	if got := len(executor.getSpawnCalls()); got != 1 {
		t.Fatalf("must spawn task-b exactly once, got %d spawns", got)
	}
}

// Regression pair: automation dispatch for a project that is task-paused
// but whose automations are on must go through, even when the aggregate
// AutomationsPaused is true because ANOTHER project has automations paused.
func TestHandleDispatchCommand_AggregateAutomationsPausedAllowsCarveOut(t *testing.T) {
	client := newMockClient()
	// proj-a task-paused (autos on); proj-x automations paused (unrelated).
	// Aggregates: Paused=true, AutomationsPaused=true. But proj-a autos are
	// NOT in AutomationPausedProjects, so an automation task for proj-a
	// must still bypass the pause.
	client.runnerStatus = &types.RunnerStatusResponse{
		Running:                  true,
		Paused:                   true,
		PausedProjects:           []string{"proj-a"},
		AutomationsPaused:        true,
		AutomationPausedProjects: []string{"proj-x"},
	}
	task := testTask("auto-task", "proj-a")
	task.GeneratedBy = "automation:auto1234"
	client.readyTasks["proj-a"] = []types.ResolvedTask{*task}
	executor := newMockExecutor()
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"}, Config: testRunnerConfig(), Mode: ExecutionModeHeadless,
		Client: client, Executor: executor, ProcessMgr: newMockProcessMgr(), StateMgr: newMockStateMgr(),
	})
	tr.syncServerPauseState(context.Background())

	tr.handleCommand(context.Background(), RunnerCommand{
		Type: CommandDispatch, ProjectID: "proj-a", TaskID: "auto-task", LeaseID: "lease-1", Task: task,
	})

	if got := len(client.getRejectCalls()); got != 0 {
		t.Fatalf("automation dispatch must not be rejected when this project's autos are on; got %d rejects: %+v",
			got, client.getRejectCalls())
	}
	if got := len(executor.getSpawnCalls()); got != 1 {
		t.Fatalf("automation dispatch must spawn once, got %d", got)
	}
}

// A failed dispatch ack must release the local slot reservation. Before the
// fix the reservation leaked, permanently costing the runner one execution
// slot per failed ack.
func TestHandleDispatchCommand_AckFailureReleasesReservation(t *testing.T) {
	client := newMockClient()
	client.ackErr = errMockFailure
	processMgr := newMockProcessMgr()
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"}, Config: testRunnerConfig(), Mode: ExecutionModeHeadless,
		Client: client, Executor: newMockExecutor(), ProcessMgr: processMgr, StateMgr: newMockStateMgr(),
	})

	task := testTask("task1", "proj-a")
	tr.handleCommand(context.Background(), RunnerCommand{
		Type: CommandDispatch, ProjectID: "proj-a", TaskID: "task1", LeaseID: "lease-1", Task: task,
	})

	if got := processMgr.Count(); got != 0 {
		t.Errorf("process count = %d after failed ack, want 0 (reservation must be released)", got)
	}
	if got := len(client.getClaimCalls()); got != 0 {
		t.Errorf("claim calls = %d after failed ack, want 0 (spawn must be aborted)", got)
	}
}

func TestCommandChannelCapacity_ScalesWithMaxParallel(t *testing.T) {
	tests := []struct {
		maxParallel int
		want        int
	}{
		{0, 16},
		{1, 16},
		{8, 16},
		{9, 18},
		{32, 64},
	}
	for _, tt := range tests {
		if got := commandChannelCapacity(tt.maxParallel); got != tt.want {
			t.Errorf("commandChannelCapacity(%d) = %d, want %d", tt.maxParallel, got, tt.want)
		}
	}
}

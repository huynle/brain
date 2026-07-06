package runner

import (
	"context"
	"testing"
)

// TestHandleCommand_PauseWithProjectAndScope_TasksOnly confirms that
// a pause command carrying a projectId and scope="tasks" causes only
// the runner's task-pause cache for that project to be set, leaving
// the automation-pause cache untouched. This is what the PWA sends
// when the user clicks "pause tasks" for orion-ai.
func TestHandleCommand_PauseWithProjectAndScope_TasksOnly(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"orion-ai"}, Config: cfg, Mode: ExecutionModeHeadless,
		Client: client, Executor: executor, ProcessMgr: processMgr, StateMgr: stateMgr,
	})

	tr.handleCommand(context.Background(), RunnerCommand{
		Type:      CommandPause,
		ProjectID: "orion-ai",
		Scope:     "tasks",
	})

	if !tr.IsPaused("orion-ai") {
		t.Errorf("IsPaused(orion-ai) = false, want true after tasks-only pause command")
	}
	if tr.IsAutomationsPausedForProject("orion-ai") {
		t.Errorf("IsAutomationsPausedForProject(orion-ai) = true, want false (scope=tasks should not affect automations)")
	}
	// Other projects unaffected.
	if tr.IsPaused("other-project") {
		t.Errorf("IsPaused(other-project) = true, want false (per-project scope should not leak)")
	}
	if tr.IsAllPaused() {
		t.Errorf("IsAllPaused() = true, want false (per-project pause should not set global)")
	}
}

// TestHandleCommand_PauseWithProjectAndScope_AutomationsOnly confirms
// a pause command with scope="automations" sets only the automation
// pause for that project.
func TestHandleCommand_PauseWithProjectAndScope_AutomationsOnly(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"orion-ai"}, Config: cfg, Mode: ExecutionModeHeadless,
		Client: client, Executor: executor, ProcessMgr: processMgr, StateMgr: stateMgr,
	})

	tr.handleCommand(context.Background(), RunnerCommand{
		Type:      CommandPause,
		ProjectID: "orion-ai",
		Scope:     "automations",
	})

	if tr.IsPaused("orion-ai") {
		t.Errorf("IsPaused(orion-ai) = true, want false (scope=automations should not affect tasks)")
	}
	if !tr.IsAutomationsPausedForProject("orion-ai") {
		t.Errorf("IsAutomationsPausedForProject(orion-ai) = false, want true after automations-only pause command")
	}
}

// TestHandleCommand_PauseAll_NoProjectNoScope confirms that a bare
// pause command (no projectId, no scope) still triggers the legacy
// global-pause behavior (backwards compatibility with runner-level
// pause endpoint).
func TestHandleCommand_PauseAll_NoProjectNoScope(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"orion-ai"}, Config: cfg, Mode: ExecutionModeHeadless,
		Client: client, Executor: executor, ProcessMgr: processMgr, StateMgr: stateMgr,
	})

	tr.handleCommand(context.Background(), RunnerCommand{Type: CommandPause})

	if !tr.IsAllPaused() {
		t.Errorf("IsAllPaused() = false, want true after bare pause command")
	}
}

// TestHandleCommand_Resume_MirrorsPauseScope confirms resume commands
// respect the same project+scope semantics as pause. If you paused
// per-project per-scope, resume with the same fields undoes exactly
// that.
func TestHandleCommand_Resume_MirrorsPauseScope(t *testing.T) {
	client := newMockClient()
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"orion-ai"}, Config: cfg, Mode: ExecutionModeHeadless,
		Client: client, Executor: executor, ProcessMgr: processMgr, StateMgr: stateMgr,
	})

	// Pause tasks for orion-ai.
	tr.handleCommand(context.Background(), RunnerCommand{
		Type: CommandPause, ProjectID: "orion-ai", Scope: "tasks",
	})
	if !tr.IsPaused("orion-ai") {
		t.Fatal("precondition: paused for tasks")
	}

	// Resume with the same scope.
	tr.handleCommand(context.Background(), RunnerCommand{
		Type: CommandResume, ProjectID: "orion-ai", Scope: "tasks",
	})

	if tr.IsPaused("orion-ai") {
		t.Errorf("IsPaused(orion-ai) = true, want false after matching resume")
	}
}

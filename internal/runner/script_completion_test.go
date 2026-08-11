package runner

import (
	"os"
	"testing"
	"time"
)

// fakeProcess is a Process whose exit state the test controls exactly.
type fakeProcess struct {
	exited   bool
	exitCode int
}

func (p *fakeProcess) Pid() int             { return 4242 }
func (p *fakeProcess) Exited() bool         { return p.exited }
func (p *fakeProcess) ExitCode() int        { return p.exitCode }
func (p *fakeProcess) Kill(os.Signal) error { return nil }

// registerFake wires a running task + fake process into the manager, with
// no Brain API reachable — getTaskEntry fails, so CheckCompletion falls
// through to the exit-code logic, which is exactly the path under test.
func registerFake(pm *ProcessManager, task RunningTask, proc Process) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.processes[task.ID] = &ProcessInfo{Task: task, Proc: proc}
}

func newTestProcessManager() *ProcessManager {
	// A port nothing listens on: the task-file lookup must fail fast so the
	// test exercises exit-code handling rather than API status.
	return NewProcessManager(RunnerConfig{
		BrainAPIURL: "http://127.0.0.1:1",
		APITimeout:  50,
	})
}

func scriptTask(completeOnIdle bool) RunningTask {
	return RunningTask{
		ID:             "t1",
		Path:           "projects/p/task/t1.md",
		ProjectID:      "p",
		ExecutorType:   "script",
		Executor:       "script",
		CompleteOnIdle: completeOnIdle,
		StartedAt:      time.Now(),
	}
}

// The regression this guards: a script that failed was reported as
// completed whenever complete_on_idle was set, so a broken build or a
// conflicted merge silently completed its feature and let the checkout
// automation merge it.
func TestCheckCompletion_ScriptNonZeroExitIsCrashedDespiteCompleteOnIdle(t *testing.T) {
	pm := newTestProcessManager()
	registerFake(pm, scriptTask(true), &fakeProcess{exited: true, exitCode: 1})

	if got := pm.CheckCompletion("t1", true); got != CompletionCrashed {
		t.Errorf("CheckCompletion = %q, want %q for a script that exited 1", got, CompletionCrashed)
	}
}

func TestCheckCompletion_ScriptZeroExitCompletes(t *testing.T) {
	pm := newTestProcessManager()
	registerFake(pm, scriptTask(true), &fakeProcess{exited: true, exitCode: 0})

	if got := pm.CheckCompletion("t1", true); got != CompletionCompleted {
		t.Errorf("CheckCompletion = %q, want %q for a clean script exit", got, CompletionCompleted)
	}
}

// Without complete_on_idle the behaviour was already correct; pin it so the
// new branch cannot regress it.
func TestCheckCompletion_ScriptNonZeroExitWithoutCompleteOnIdle(t *testing.T) {
	pm := newTestProcessManager()
	registerFake(pm, scriptTask(false), &fakeProcess{exited: true, exitCode: 2})

	if got := pm.CheckCompletion("t1", true); got != CompletionCrashed {
		t.Errorf("CheckCompletion = %q, want %q", got, CompletionCrashed)
	}
}

// An agent task tracked by PID reports -1 because the real code is
// unknowable. That must keep meaning "completed" under complete_on_idle —
// the new script branch must not capture it.
func TestCheckCompletion_AgentUnknownExitStillCompletesOnIdle(t *testing.T) {
	pm := newTestProcessManager()
	task := RunningTask{
		ID:             "t1",
		Path:           "projects/p/task/t1.md",
		ProjectID:      "p",
		ExecutorType:   "opencode",
		Executor:       "opencode",
		CompleteOnIdle: true,
		StartedAt:      time.Now(),
	}
	registerFake(pm, task, &fakeProcess{exited: true, exitCode: -1})

	if got := pm.CheckCompletion("t1", true); got != CompletionCompleted {
		t.Errorf("CheckCompletion = %q, want %q for a PID-tracked agent", got, CompletionCompleted)
	}
}

// A script whose exit code is genuinely unknown (-1) must not be failed on
// that basis alone — complete_on_idle still decides.
func TestCheckCompletion_ScriptUnknownExitFallsBackToCompleteOnIdle(t *testing.T) {
	pm := newTestProcessManager()
	registerFake(pm, scriptTask(true), &fakeProcess{exited: true, exitCode: -1})

	if got := pm.CheckCompletion("t1", true); got != CompletionCompleted {
		t.Errorf("CheckCompletion = %q, want %q when the exit code is unknown", got, CompletionCompleted)
	}
}

// A still-running script is neither complete nor crashed.
func TestCheckCompletion_RunningScriptIsRunning(t *testing.T) {
	pm := newTestProcessManager()
	registerFake(pm, scriptTask(true), &fakeProcess{exited: false, exitCode: 0})

	if got := pm.CheckCompletion("t1", true); got != CompletionRunning {
		t.Errorf("CheckCompletion = %q, want %q", got, CompletionRunning)
	}
}

// The rule is now executor-agnostic: a KNOWN non-zero exit is a crash even
// for agent executors. An `opencode run` dying on an auth error used to be
// laundered into "completed" by complete_on_idle — the checkout agent never
// ran, yet the feature proceeded as if reviewed.
func TestCheckCompletion_AgentNonZeroExitIsCrashedDespiteCompleteOnIdle(t *testing.T) {
	pm := newTestProcessManager()
	task := RunningTask{
		ID:             "t1",
		Path:           "projects/p/task/t1.md",
		ProjectID:      "p",
		ExecutorType:   "opencode",
		Executor:       "opencode",
		CompleteOnIdle: true,
		StartedAt:      time.Now(),
	}
	registerFake(pm, task, &fakeProcess{exited: true, exitCode: 1})

	if got := pm.CheckCompletion("t1", true); got != CompletionCrashed {
		t.Errorf("CheckCompletion = %q, want %q for an agent that exited 1", got, CompletionCrashed)
	}
}

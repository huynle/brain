package runner

import (
	"testing"
	"time"
)

// stubSessionStatus overrides the steered-turn status probe for the test's
// duration. The probe is indirected (sessionStatusForPort) because the
// real one dials localhost, which tests can't rely on.
func stubSessionStatus(t *testing.T, status string) {
	t.Helper()
	prev := sessionStatusForPort
	sessionStatusForPort = func(int) string { return status }
	t.Cleanup(func() { sessionStatusForPort = prev })
}

func opencodeTask(port int) RunningTask {
	return RunningTask{
		ID:             "t-steer",
		Path:           "projects/p/task/t-steer.md",
		ProjectID:      "p",
		ExecutorType:   "opencode",
		CompleteOnIdle: true,
		OpencodePort:   port,
		StartedAt:      time.Now(),
	}
}

// The regression this guards: goal steering injects a prompt whose turn
// runs on the persistent serve process, but completion keyed on the
// `opencode run` driver exiting — so the runner completed the task and
// tore the serve process down while the steered turn was mid-flight.
// A clean driver exit with a busy session must hold as running.
func TestCheckCompletion_HoldsWhileSteeredTurnBusy(t *testing.T) {
	pm := newTestProcessManager()
	stubSessionStatus(t, "busy")
	registerFake(pm, opencodeTask(52768), &fakeProcess{exited: true, exitCode: 0})

	if got := pm.CheckCompletion("t-steer", false); got != CompletionRunning {
		t.Errorf("CheckCompletion = %q, want %q while session busy", got, CompletionRunning)
	}
	// The first busy observation stamps the hold start.
	pm.mu.Lock()
	since := pm.processes["t-steer"].Task.BusyHoldSince
	pm.mu.Unlock()
	if since.IsZero() {
		t.Error("BusyHoldSince not stamped on first busy observation")
	}
}

func TestCheckCompletion_CompletesOnceSessionIdle(t *testing.T) {
	pm := newTestProcessManager()
	stubSessionStatus(t, "idle")
	registerFake(pm, opencodeTask(52768), &fakeProcess{exited: true, exitCode: 0})

	if got := pm.CheckCompletion("t-steer", false); got != CompletionCompleted {
		t.Errorf("CheckCompletion = %q, want %q with idle session", got, CompletionCompleted)
	}
}

// A wedged session cannot pin the task forever: once the hold window
// lapses, completion proceeds even though the session still reports busy.
func TestCheckCompletion_HoldWindowBounded(t *testing.T) {
	pm := newTestProcessManager()
	stubSessionStatus(t, "busy")
	registerFake(pm, opencodeTask(52768), &fakeProcess{exited: true, exitCode: 0})

	// Backdate the hold start beyond the window.
	pm.mu.Lock()
	pm.processes["t-steer"].Task.BusyHoldSince = time.Now().Add(-steerHoldMax - time.Minute)
	pm.mu.Unlock()

	if got := pm.CheckCompletion("t-steer", false); got != CompletionCompleted {
		t.Errorf("CheckCompletion = %q, want %q after hold window lapsed", got, CompletionCompleted)
	}
}

// The hold is a success-path affordance only: a non-zero driver exit is a
// crash regardless of session state (a busy status there is a zombie, not
// steered work worth waiting on).
func TestCheckCompletion_NoHoldOnCrashedDriver(t *testing.T) {
	pm := newTestProcessManager()
	stubSessionStatus(t, "busy")
	registerFake(pm, opencodeTask(52768), &fakeProcess{exited: true, exitCode: 1})

	if got := pm.CheckCompletion("t-steer", false); got != CompletionCrashed {
		t.Errorf("CheckCompletion = %q, want %q for non-zero exit", got, CompletionCrashed)
	}
}

// Non-attachable tasks (no port) keep the old semantics untouched: the
// probe must not even be consulted.
func TestCheckCompletion_NoPortNoHold(t *testing.T) {
	pm := newTestProcessManager()
	prev := sessionStatusForPort
	sessionStatusForPort = func(int) string {
		t.Error("session status probed for a task with no attach port")
		return "busy"
	}
	t.Cleanup(func() { sessionStatusForPort = prev })
	registerFake(pm, opencodeTask(0), &fakeProcess{exited: true, exitCode: 0})

	if got := pm.CheckCompletion("t-steer", false); got != CompletionCompleted {
		t.Errorf("CheckCompletion = %q, want %q with no attach port", got, CompletionCompleted)
	}
}

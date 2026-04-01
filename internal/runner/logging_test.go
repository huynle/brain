package runner

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// captureLog sets up a slog logger that writes to a buffer and returns
// the buffer for inspection. It restores the default logger on cleanup.
func captureLog(t *testing.T, level slog.Level) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	old := slog.Default()
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(old) })
	return &buf
}

func TestRegisterEventLogger_TaskStarted(t *testing.T) {
	buf := captureLog(t, slog.LevelDebug)

	tr := newEventTestRunner(t)
	RegisterEventLogger(tr)

	tr.emitEvent(RunnerEvent{
		Type: EventTaskStarted,
		Task: &RunningTask{
			ID:        "task-123",
			ProjectID: "my-project",
			Title:     "Fix the bug",
			PID:       42,
		},
	})

	output := buf.String()
	if !strings.Contains(output, "task started") {
		t.Errorf("expected 'task started' in log output, got: %s", output)
	}
	if !strings.Contains(output, "task-123") {
		t.Errorf("expected task ID in log output, got: %s", output)
	}
	if !strings.Contains(output, "my-project") {
		t.Errorf("expected project ID in log output, got: %s", output)
	}
}

func TestRegisterEventLogger_TaskCompleted(t *testing.T) {
	buf := captureLog(t, slog.LevelDebug)

	tr := newEventTestRunner(t)
	RegisterEventLogger(tr)

	exitCode := 0
	tr.emitEvent(RunnerEvent{
		Type: EventTaskCompleted,
		Result: &TaskResult{
			TaskID:   "task-456",
			Status:   TaskResultCompleted,
			Duration: 5000,
			ExitCode: &exitCode,
		},
	})

	output := buf.String()
	if !strings.Contains(output, "task completed") {
		t.Errorf("expected 'task completed' in log output, got: %s", output)
	}
	if !strings.Contains(output, "task-456") {
		t.Errorf("expected task ID in log output, got: %s", output)
	}
}

func TestRegisterEventLogger_TaskFailed(t *testing.T) {
	buf := captureLog(t, slog.LevelDebug)

	tr := newEventTestRunner(t)
	RegisterEventLogger(tr)

	exitCode := 1
	tr.emitEvent(RunnerEvent{
		Type: EventTaskFailed,
		Result: &TaskResult{
			TaskID:   "task-789",
			Status:   TaskResultFailed,
			Duration: 3000,
			ExitCode: &exitCode,
		},
	})

	output := buf.String()
	if !strings.Contains(output, "task failed") {
		t.Errorf("expected 'task failed' in log output, got: %s", output)
	}
	// task failed should use slog.Warn level
	if !strings.Contains(output, "WARN") {
		t.Errorf("expected WARN level for task failed, got: %s", output)
	}
}

func TestRegisterEventLogger_TaskCancelled(t *testing.T) {
	buf := captureLog(t, slog.LevelDebug)

	tr := newEventTestRunner(t)
	RegisterEventLogger(tr)

	tr.emitEvent(RunnerEvent{
		Type:   EventTaskCancelled,
		TaskID: "task-cancel",
	})

	output := buf.String()
	if !strings.Contains(output, "task cancelled") {
		t.Errorf("expected 'task cancelled' in log output, got: %s", output)
	}
	if !strings.Contains(output, "task-cancel") {
		t.Errorf("expected task ID in log output, got: %s", output)
	}
}

func TestRegisterEventLogger_PollComplete_DebugLevel(t *testing.T) {
	// With Info level, poll_complete (Debug) should NOT appear
	buf := captureLog(t, slog.LevelInfo)

	tr := newEventTestRunner(t)
	RegisterEventLogger(tr)

	tr.emitEvent(RunnerEvent{
		Type:         EventPollComplete,
		ReadyCount:   3,
		RunningCount: 2,
	})

	output := buf.String()
	if strings.Contains(output, "poll complete") {
		t.Errorf("poll complete should not appear at Info level, got: %s", output)
	}

	// With Debug level, it should appear
	buf2 := captureLog(t, slog.LevelDebug)
	tr2 := newEventTestRunner(t)
	RegisterEventLogger(tr2)

	tr2.emitEvent(RunnerEvent{
		Type:         EventPollComplete,
		ReadyCount:   3,
		RunningCount: 2,
	})

	output2 := buf2.String()
	if !strings.Contains(output2, "poll complete") {
		t.Errorf("expected 'poll complete' at Debug level, got: %s", output2)
	}
}

func TestRegisterEventLogger_SessionDiscovered(t *testing.T) {
	buf := captureLog(t, slog.LevelDebug)

	tr := newEventTestRunner(t)
	RegisterEventLogger(tr)

	tr.emitEvent(RunnerEvent{
		Type:      EventSessionDiscovered,
		TaskPath:  "projects/test/task/abc.md",
		SessionID: "ses_12345",
	})

	output := buf.String()
	if !strings.Contains(output, "session discovered") {
		t.Errorf("expected 'session discovered' in log output, got: %s", output)
	}
	if !strings.Contains(output, "ses_12345") {
		t.Errorf("expected session ID in log output, got: %s", output)
	}
}

func TestRegisterEventLogger_ProjectPausedResumed(t *testing.T) {
	buf := captureLog(t, slog.LevelDebug)

	tr := newEventTestRunner(t)
	RegisterEventLogger(tr)

	tr.emitEvent(RunnerEvent{
		Type:      EventProjectPaused,
		ProjectID: "proj-1",
	})
	tr.emitEvent(RunnerEvent{
		Type:      EventProjectResumed,
		ProjectID: "proj-1",
	})

	output := buf.String()
	if !strings.Contains(output, "project paused") {
		t.Errorf("expected 'project paused' in log output, got: %s", output)
	}
	if !strings.Contains(output, "project resumed") {
		t.Errorf("expected 'project resumed' in log output, got: %s", output)
	}
}

func TestRegisterEventLogger_AllPausedResumed(t *testing.T) {
	buf := captureLog(t, slog.LevelDebug)

	tr := newEventTestRunner(t)
	RegisterEventLogger(tr)

	tr.emitEvent(RunnerEvent{Type: EventAllPaused})
	tr.emitEvent(RunnerEvent{Type: EventAllResumed})

	output := buf.String()
	if !strings.Contains(output, "all projects paused") {
		t.Errorf("expected 'all projects paused' in log output, got: %s", output)
	}
	if !strings.Contains(output, "all projects resumed") {
		t.Errorf("expected 'all projects resumed' in log output, got: %s", output)
	}
}

func TestRegisterEventLogger_Shutdown(t *testing.T) {
	buf := captureLog(t, slog.LevelDebug)

	tr := newEventTestRunner(t)
	RegisterEventLogger(tr)

	tr.emitEvent(RunnerEvent{
		Type:   EventShutdown,
		Reason: "context cancelled",
	})

	output := buf.String()
	if !strings.Contains(output, "runner shutdown") {
		t.Errorf("expected 'runner shutdown' in log output, got: %s", output)
	}
	if !strings.Contains(output, "context cancelled") {
		t.Errorf("expected shutdown reason in log output, got: %s", output)
	}
}

func TestRegisterEventLogger_RunnerStarted(t *testing.T) {
	buf := captureLog(t, slog.LevelDebug)

	tr := newEventTestRunner(t)
	RegisterEventLogger(tr)

	tr.emitEvent(RunnerEvent{
		Type:     EventRunnerStarted,
		Mode:     "multi",
		Projects: []string{"proj-a", "proj-b"},
	})

	output := buf.String()
	if !strings.Contains(output, "runner started") {
		t.Errorf("expected 'runner started' in log output, got: %s", output)
	}
	if !strings.Contains(output, "runner_id") {
		t.Errorf("expected runner_id in log output, got: %s", output)
	}
	if !strings.Contains(output, "multi") {
		t.Errorf("expected mode in log output, got: %s", output)
	}
	if !strings.Contains(output, "proj-a") {
		t.Errorf("expected project name in log output, got: %s", output)
	}
}

func TestRegisterEventLogger_TaskClaimed(t *testing.T) {
	buf := captureLog(t, slog.LevelDebug)

	tr := newEventTestRunner(t)
	RegisterEventLogger(tr)

	tr.emitEvent(RunnerEvent{
		Type:      EventTaskClaimed,
		TaskID:    "task-claimed",
		ProjectID: "my-project",
	})

	output := buf.String()
	if !strings.Contains(output, "task claimed") {
		t.Errorf("expected 'task claimed' in log output, got: %s", output)
	}
	if !strings.Contains(output, "runner_id") {
		t.Errorf("expected runner_id in log output, got: %s", output)
	}
	if !strings.Contains(output, "task-claimed") {
		t.Errorf("expected task ID in log output, got: %s", output)
	}
	if !strings.Contains(output, "my-project") {
		t.Errorf("expected project in log output, got: %s", output)
	}
}

func TestRegisterEventLogger_TaskClaimRejected(t *testing.T) {
	buf := captureLog(t, slog.LevelDebug)

	tr := newEventTestRunner(t)
	RegisterEventLogger(tr)

	tr.emitEvent(RunnerEvent{
		Type:      EventTaskClaimRejected,
		TaskID:    "task-reject",
		ProjectID: "my-project",
		ClaimedBy: "other-runner-xyz",
	})

	output := buf.String()
	if !strings.Contains(output, "task claim rejected") {
		t.Errorf("expected 'task claim rejected' in log output, got: %s", output)
	}
	if !strings.Contains(output, "WARN") {
		t.Errorf("expected WARN level for task claim rejected, got: %s", output)
	}
	if !strings.Contains(output, "runner_id") {
		t.Errorf("expected runner_id in log output, got: %s", output)
	}
	if !strings.Contains(output, "task-reject") {
		t.Errorf("expected task ID in log output, got: %s", output)
	}
	if !strings.Contains(output, "other-runner-xyz") {
		t.Errorf("expected claimed_by in log output, got: %s", output)
	}
}

func TestRegisterEventLogger_TaskStatusChanged(t *testing.T) {
	buf := captureLog(t, slog.LevelDebug)

	tr := newEventTestRunner(t)
	RegisterEventLogger(tr)

	tr.emitEvent(RunnerEvent{
		Type:       EventTaskStatusChanged,
		TaskID:     "task-status",
		ProjectID:  "my-project",
		FromStatus: "pending",
		ToStatus:   "in_progress",
	})

	output := buf.String()
	if !strings.Contains(output, "task status changed") {
		t.Errorf("expected 'task status changed' in log output, got: %s", output)
	}
	if !strings.Contains(output, "runner_id") {
		t.Errorf("expected runner_id in log output, got: %s", output)
	}
	if !strings.Contains(output, "task-status") {
		t.Errorf("expected task ID in log output, got: %s", output)
	}
	if !strings.Contains(output, "pending") {
		t.Errorf("expected from status in log output, got: %s", output)
	}
	if !strings.Contains(output, "in_progress") {
		t.Errorf("expected to status in log output, got: %s", output)
	}
}

func TestRegisterEventLogger_TaskReleased(t *testing.T) {
	buf := captureLog(t, slog.LevelDebug)

	tr := newEventTestRunner(t)
	RegisterEventLogger(tr)

	tr.emitEvent(RunnerEvent{
		Type:      EventTaskReleased,
		TaskID:    "task-released",
		ProjectID: "my-project",
	})

	output := buf.String()
	if !strings.Contains(output, "task released") {
		t.Errorf("expected 'task released' in log output, got: %s", output)
	}
	if !strings.Contains(output, "WARN") {
		t.Errorf("expected WARN level for task released, got: %s", output)
	}
	if !strings.Contains(output, "runner_id") {
		t.Errorf("expected runner_id in log output, got: %s", output)
	}
	if !strings.Contains(output, "task-released") {
		t.Errorf("expected task ID in log output, got: %s", output)
	}
}

func TestRegisterEventLogger_FeatureEnabledDisabled(t *testing.T) {
	buf := captureLog(t, slog.LevelDebug)

	tr := newEventTestRunner(t)
	RegisterEventLogger(tr)

	tr.emitEvent(RunnerEvent{
		Type:      EventFeatureEnabled,
		FeatureID: "feature-review",
	})
	tr.emitEvent(RunnerEvent{
		Type:      EventFeatureDisabled,
		FeatureID: "feature-inspector",
	})

	output := buf.String()
	if !strings.Contains(output, "feature enabled") {
		t.Errorf("expected 'feature enabled' in log output, got: %s", output)
	}
	if !strings.Contains(output, "feature-review") {
		t.Errorf("expected feature ID in log output, got: %s", output)
	}
	if !strings.Contains(output, "feature disabled") {
		t.Errorf("expected 'feature disabled' in log output, got: %s", output)
	}
	if !strings.Contains(output, "feature-inspector") {
		t.Errorf("expected feature ID in log output, got: %s", output)
	}
}

// TestRegisterEventLogger_RunnerID_AllEvents verifies that runner_id is auto-stamped
// in slog output for EVERY event type. The emitEvent() method on TaskRunner sets
// RunnerID from the runner's internal ID, so all events should include it.
func TestRegisterEventLogger_RunnerID_AllEvents(t *testing.T) {
	exitCode := 0

	events := []RunnerEvent{
		{Type: EventRunnerStarted, Mode: "single", Projects: []string{"p"}},
		{Type: EventTaskStarted, Task: &RunningTask{ID: "t1", ProjectID: "p", Title: "T", PID: 1}},
		{Type: EventTaskClaimed, TaskID: "t2", ProjectID: "p"},
		{Type: EventTaskClaimRejected, TaskID: "t3", ProjectID: "p", ClaimedBy: "other"},
		{Type: EventTaskStatusChanged, TaskID: "t4", ProjectID: "p", FromStatus: "a", ToStatus: "b"},
		{Type: EventTaskCompleted, Result: &TaskResult{TaskID: "t5", Status: TaskResultCompleted, ExitCode: &exitCode}},
		{Type: EventTaskFailed, Result: &TaskResult{TaskID: "t6", Status: TaskResultFailed, ExitCode: &exitCode}},
		{Type: EventTaskCancelled, TaskID: "t7"},
		{Type: EventTaskReleased, TaskID: "t8", ProjectID: "p"},
		{Type: EventPollComplete, ReadyCount: 1, RunningCount: 0},
		{Type: EventSessionDiscovered, TaskPath: "path", SessionID: "ses_1"},
		{Type: EventFeatureEnabled, FeatureID: "f1"},
		{Type: EventFeatureDisabled, FeatureID: "f2"},
		{Type: EventProjectPaused, ProjectID: "p"},
		{Type: EventProjectResumed, ProjectID: "p"},
		{Type: EventAllPaused},
		{Type: EventAllResumed},
		{Type: EventShutdown, Reason: "test"},
	}

	for _, ev := range events {
		t.Run(string(ev.Type), func(t *testing.T) {
			buf := captureLog(t, slog.LevelDebug)
			tr := newEventTestRunner(t)
			RegisterEventLogger(tr)

			tr.emitEvent(ev)

			output := buf.String()
			if !strings.Contains(output, "runner_id") {
				t.Errorf("expected runner_id in log output for %s, got: %s", ev.Type, output)
			}
			// Also verify the runner_id value is non-empty — it should contain
			// the auto-generated ID from NewTaskRunner.
			if !strings.Contains(output, tr.runnerID) {
				t.Errorf("expected runner_id value %q in log output for %s, got: %s", tr.runnerID, ev.Type, output)
			}
		})
	}
}

// newEventTestRunner creates a minimal TaskRunner suitable for event handler testing.
// It only initializes the fields needed for OnEvent/emitEvent.
func newEventTestRunner(t *testing.T) *TaskRunner {
	t.Helper()
	return NewTaskRunner(TaskRunnerOptions{
		ProjectID: "test",
		Config: RunnerConfig{
			BrainAPIURL:  "http://localhost:3333",
			MaxParallel:  1,
			PollInterval: 60,
		},
	})
}

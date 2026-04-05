package runner

import (
	"strings"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// ToEvent() Conversion Tests (Route T - data transformation with conditionals)
// =============================================================================

func TestRunnerEvent_ToEvent_TaskStarted(t *testing.T) {
	re := RunnerEvent{
		Type:     EventTaskStarted,
		RunnerID: "runner-1",
		Task: &RunningTask{
			ID:        "task-abc",
			Path:      "projects/myproj/task/abc.md",
			Title:     "My Task",
			ProjectID: "myproj",
			FeatureID: "feat-auth",
		},
	}

	evt := re.ToEvent()

	if evt.Type != types.EventTaskStarted {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventTaskStarted)
	}
	if evt.Source != types.EventSourceRunner {
		t.Errorf("Source = %q, want %q", evt.Source, types.EventSourceRunner)
	}
	if evt.RunnerID != "runner-1" {
		t.Errorf("RunnerID = %q, want %q", evt.RunnerID, "runner-1")
	}
	if evt.TaskID != "task-abc" {
		t.Errorf("TaskID = %q, want %q", evt.TaskID, "task-abc")
	}
	if evt.TaskPath != "projects/myproj/task/abc.md" {
		t.Errorf("TaskPath = %q, want %q", evt.TaskPath, "projects/myproj/task/abc.md")
	}
	if evt.TaskTitle != "My Task" {
		t.Errorf("TaskTitle = %q, want %q", evt.TaskTitle, "My Task")
	}
	if evt.ProjectID != "myproj" {
		t.Errorf("ProjectID = %q, want %q", evt.ProjectID, "myproj")
	}
	if evt.FeatureID != "feat-auth" {
		t.Errorf("FeatureID = %q, want %q", evt.FeatureID, "feat-auth")
	}
}

func TestRunnerEvent_ToEvent_TaskCompleted(t *testing.T) {
	re := RunnerEvent{
		Type:     EventTaskCompleted,
		RunnerID: "runner-1",
		TaskID:   "task-xyz",
		Result: &TaskResult{
			TaskID:    "task-xyz",
			Status:    TaskResultCompleted,
			StartedAt: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
			Duration:  5000,
		},
	}

	evt := re.ToEvent()

	if evt.Type != types.EventTaskCompleted {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventTaskCompleted)
	}
	if evt.TaskID != "task-xyz" {
		t.Errorf("TaskID = %q, want %q", evt.TaskID, "task-xyz")
	}
	if evt.Source != types.EventSourceRunner {
		t.Errorf("Source = %q, want %q", evt.Source, types.EventSourceRunner)
	}
}

func TestRunnerEvent_ToEvent_TaskFailed(t *testing.T) {
	re := RunnerEvent{
		Type:     EventTaskFailed,
		RunnerID: "runner-1",
		TaskID:   "task-fail",
		Result: &TaskResult{
			TaskID: "task-fail",
			Status: TaskResultFailed,
		},
	}

	evt := re.ToEvent()

	if evt.Type != types.EventTaskFailed {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventTaskFailed)
	}
	if evt.TaskID != "task-fail" {
		t.Errorf("TaskID = %q, want %q", evt.TaskID, "task-fail")
	}
}

func TestRunnerEvent_ToEvent_TaskCancelled(t *testing.T) {
	re := RunnerEvent{
		Type:     EventTaskCancelled,
		RunnerID: "runner-1",
		TaskID:   "task-cancel",
		TaskPath: "projects/p/task/cancel.md",
	}

	evt := re.ToEvent()

	if evt.Type != types.EventTaskCancelled {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventTaskCancelled)
	}
	if evt.TaskID != "task-cancel" {
		t.Errorf("TaskID = %q, want %q", evt.TaskID, "task-cancel")
	}
	if evt.TaskPath != "projects/p/task/cancel.md" {
		t.Errorf("TaskPath = %q, want %q", evt.TaskPath, "projects/p/task/cancel.md")
	}
}

func TestRunnerEvent_ToEvent_TaskClaimed(t *testing.T) {
	re := RunnerEvent{
		Type:      EventTaskClaimed,
		RunnerID:  "runner-1",
		TaskID:    "task-claim",
		ProjectID: "myproj",
		TaskPath:  "projects/myproj/task/claim.md",
	}

	evt := re.ToEvent()

	if evt.Type != types.EventTaskClaimed {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventTaskClaimed)
	}
	if evt.TaskID != "task-claim" {
		t.Errorf("TaskID = %q, want %q", evt.TaskID, "task-claim")
	}
	if evt.ProjectID != "myproj" {
		t.Errorf("ProjectID = %q, want %q", evt.ProjectID, "myproj")
	}
}

func TestRunnerEvent_ToEvent_TaskClaimRejected(t *testing.T) {
	re := RunnerEvent{
		Type:      EventTaskClaimRejected,
		RunnerID:  "runner-1",
		TaskID:    "task-reject",
		ProjectID: "myproj",
		ClaimedBy: "runner-2",
	}

	evt := re.ToEvent()

	if evt.Type != types.EventTaskClaimRejected {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventTaskClaimRejected)
	}
	if evt.Metadata["claimed_by"] != "runner-2" {
		t.Errorf("Metadata[claimed_by] = %q, want %q", evt.Metadata["claimed_by"], "runner-2")
	}
}

func TestRunnerEvent_ToEvent_TaskStatusChanged(t *testing.T) {
	re := RunnerEvent{
		Type:       EventTaskStatusChanged,
		RunnerID:   "runner-1",
		TaskID:     "task-status",
		ProjectID:  "myproj",
		TaskPath:   "projects/myproj/task/status.md",
		FromStatus: "pending",
		ToStatus:   "in_progress",
	}

	evt := re.ToEvent()

	if evt.Type != types.EventTaskStatusChanged {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventTaskStatusChanged)
	}
	if evt.FromStatus != "pending" {
		t.Errorf("FromStatus = %q, want %q", evt.FromStatus, "pending")
	}
	if evt.ToStatus != "in_progress" {
		t.Errorf("ToStatus = %q, want %q", evt.ToStatus, "in_progress")
	}
}

func TestRunnerEvent_ToEvent_TaskReleased(t *testing.T) {
	re := RunnerEvent{
		Type:      EventTaskReleased,
		RunnerID:  "runner-1",
		TaskID:    "task-release",
		ProjectID: "myproj",
		Reason:    "spawn failed",
	}

	evt := re.ToEvent()

	if evt.Type != types.EventTaskReleased {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventTaskReleased)
	}
	if evt.Metadata["reason"] != "spawn failed" {
		t.Errorf("Metadata[reason] = %q, want %q", evt.Metadata["reason"], "spawn failed")
	}
}

func TestRunnerEvent_ToEvent_RunnerStarted(t *testing.T) {
	re := RunnerEvent{
		Type:     EventRunnerStarted,
		RunnerID: "runner-1",
		Projects: []string{"proj-a", "proj-b"},
		Mode:     "headless",
	}

	evt := re.ToEvent()

	if evt.Type != types.EventRunnerStarted {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventRunnerStarted)
	}
	if evt.Metadata["mode"] != "headless" {
		t.Errorf("Metadata[mode] = %q, want %q", evt.Metadata["mode"], "headless")
	}
	if evt.Metadata["projects"] != "proj-a,proj-b" {
		t.Errorf("Metadata[projects] = %q, want %q", evt.Metadata["projects"], "proj-a,proj-b")
	}
}

func TestRunnerEvent_ToEvent_Shutdown(t *testing.T) {
	re := RunnerEvent{
		Type:     EventShutdown,
		RunnerID: "runner-1",
		Reason:   "graceful shutdown",
	}

	evt := re.ToEvent()

	if evt.Type != types.EventRunnerStopped {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventRunnerStopped)
	}
	if evt.Metadata["reason"] != "graceful shutdown" {
		t.Errorf("Metadata[reason] = %q, want %q", evt.Metadata["reason"], "graceful shutdown")
	}
}

func TestRunnerEvent_ToEvent_ProjectPaused(t *testing.T) {
	re := RunnerEvent{
		Type:      EventProjectPaused,
		RunnerID:  "runner-1",
		ProjectID: "myproj",
	}

	evt := re.ToEvent()

	if evt.Type != types.EventProjectPaused {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventProjectPaused)
	}
	if evt.ProjectID != "myproj" {
		t.Errorf("ProjectID = %q, want %q", evt.ProjectID, "myproj")
	}
}

func TestRunnerEvent_ToEvent_ProjectResumed(t *testing.T) {
	re := RunnerEvent{
		Type:      EventProjectResumed,
		RunnerID:  "runner-1",
		ProjectID: "myproj",
	}

	evt := re.ToEvent()

	if evt.Type != types.EventProjectResumed {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventProjectResumed)
	}
	if evt.ProjectID != "myproj" {
		t.Errorf("ProjectID = %q, want %q", evt.ProjectID, "myproj")
	}
}

func TestRunnerEvent_ToEvent_FeatureEnabled(t *testing.T) {
	re := RunnerEvent{
		Type:      EventFeatureEnabled,
		RunnerID:  "runner-1",
		FeatureID: "feat-deploy",
	}

	evt := re.ToEvent()

	if evt.Type != types.EventFeatureEnabled {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventFeatureEnabled)
	}
	if evt.FeatureID != "feat-deploy" {
		t.Errorf("FeatureID = %q, want %q", evt.FeatureID, "feat-deploy")
	}
}

func TestRunnerEvent_ToEvent_FeatureDisabled(t *testing.T) {
	re := RunnerEvent{
		Type:      EventFeatureDisabled,
		RunnerID:  "runner-1",
		FeatureID: "feat-deploy",
	}

	evt := re.ToEvent()

	if evt.Type != types.EventFeatureDisabled {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventFeatureDisabled)
	}
	if evt.FeatureID != "feat-deploy" {
		t.Errorf("FeatureID = %q, want %q", evt.FeatureID, "feat-deploy")
	}
}

func TestRunnerEvent_ToEvent_PollComplete(t *testing.T) {
	re := RunnerEvent{
		Type:         EventPollComplete,
		RunnerID:     "runner-1",
		ReadyCount:   5,
		RunningCount: 2,
	}

	evt := re.ToEvent()

	if evt.Type != types.EventRunnerPollComplete {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventRunnerPollComplete)
	}
	if evt.Metadata["ready_count"] != "5" {
		t.Errorf("Metadata[ready_count] = %q, want %q", evt.Metadata["ready_count"], "5")
	}
	if evt.Metadata["running_count"] != "2" {
		t.Errorf("Metadata[running_count] = %q, want %q", evt.Metadata["running_count"], "2")
	}
}

func TestRunnerEvent_ToEvent_StateSaved(t *testing.T) {
	re := RunnerEvent{
		Type:     EventStateSaved,
		RunnerID: "runner-1",
		Path:     "/tmp/state.json",
	}

	evt := re.ToEvent()

	if evt.Type != types.EventRunnerStateSaved {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventRunnerStateSaved)
	}
	if evt.Metadata["path"] != "/tmp/state.json" {
		t.Errorf("Metadata[path] = %q, want %q", evt.Metadata["path"], "/tmp/state.json")
	}
}

func TestRunnerEvent_ToEvent_AllPaused(t *testing.T) {
	re := RunnerEvent{
		Type:     EventAllPaused,
		RunnerID: "runner-1",
	}

	evt := re.ToEvent()

	if evt.Type != types.EventRunnerAllPaused {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventRunnerAllPaused)
	}
}

func TestRunnerEvent_ToEvent_AllResumed(t *testing.T) {
	re := RunnerEvent{
		Type:     EventAllResumed,
		RunnerID: "runner-1",
	}

	evt := re.ToEvent()

	if evt.Type != types.EventRunnerAllResumed {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventRunnerAllResumed)
	}
}

func TestRunnerEvent_ToEvent_SessionDiscovered(t *testing.T) {
	re := RunnerEvent{
		Type:      EventSessionDiscovered,
		RunnerID:  "runner-1",
		TaskPath:  "projects/p/task/abc.md",
		SessionID: "ses_12345",
	}

	evt := re.ToEvent()

	if evt.Type != types.EventRunnerSessionDiscovered {
		t.Errorf("Type = %q, want %q", evt.Type, types.EventRunnerSessionDiscovered)
	}
	if evt.Metadata["session_id"] != "ses_12345" {
		t.Errorf("Metadata[session_id] = %q, want %q", evt.Metadata["session_id"], "ses_12345")
	}
}

// =============================================================================
// All 18 event types produce valid event IDs
// =============================================================================

func TestRunnerEvent_ToEvent_AllTypesHaveIDs(t *testing.T) {
	allTypes := []RunnerEventType{
		EventTaskStarted, EventTaskCompleted, EventTaskFailed,
		EventTaskCancelled, EventPollComplete, EventStateSaved,
		EventShutdown, EventProjectPaused, EventProjectResumed,
		EventAllPaused, EventAllResumed, EventFeatureEnabled,
		EventFeatureDisabled, EventSessionDiscovered, EventTaskClaimed,
		EventTaskClaimRejected, EventTaskStatusChanged, EventTaskReleased,
		EventRunnerStarted,
	}

	for _, et := range allTypes {
		re := RunnerEvent{Type: et, RunnerID: "runner-1"}
		evt := re.ToEvent()

		if !strings.HasPrefix(evt.ID, "evt_") {
			t.Errorf("event type %q: ID %q does not have evt_ prefix", et, evt.ID)
		}
		if evt.Source != types.EventSourceRunner {
			t.Errorf("event type %q: Source = %q, want %q", et, evt.Source, types.EventSourceRunner)
		}
		if evt.RunnerID != "runner-1" {
			t.Errorf("event type %q: RunnerID = %q, want %q", et, evt.RunnerID, "runner-1")
		}
		if evt.Timestamp.IsZero() {
			t.Errorf("event type %q: Timestamp is zero", et)
		}
		if evt.Type == "" {
			t.Errorf("event type %q: mapped Type is empty", et)
		}
	}
}

// =============================================================================
// Feature ID population from RunningTask
// =============================================================================

func TestRunnerEvent_ToEvent_FeatureIDFromTask(t *testing.T) {
	re := RunnerEvent{
		Type:     EventTaskStarted,
		RunnerID: "runner-1",
		Task: &RunningTask{
			ID:        "task-1",
			Path:      "projects/p/task/1.md",
			Title:     "Task 1",
			ProjectID: "p",
			FeatureID: "my-feature",
		},
	}

	evt := re.ToEvent()

	if evt.FeatureID != "my-feature" {
		t.Errorf("FeatureID = %q, want %q", evt.FeatureID, "my-feature")
	}
}

func TestRunnerEvent_ToEvent_FeatureIDFromEventField(t *testing.T) {
	// For events like feature_enabled/disabled, FeatureID is directly on RunnerEvent
	re := RunnerEvent{
		Type:      EventFeatureEnabled,
		RunnerID:  "runner-1",
		FeatureID: "feat-direct",
	}

	evt := re.ToEvent()

	if evt.FeatureID != "feat-direct" {
		t.Errorf("FeatureID = %q, want %q", evt.FeatureID, "feat-direct")
	}
}

// =============================================================================
// Lossless conversion — all RunnerEvent fields are mapped
// =============================================================================

func TestRunnerEvent_ToEvent_Lossless_TaskStartedWithAllFields(t *testing.T) {
	task := &RunningTask{
		ID:        "task-full",
		Path:      "projects/p/task/full.md",
		Title:     "Full Task",
		Priority:  "high",
		ProjectID: "p",
		FeatureID: "feat-x",
		PID:       12345,
		StartedAt: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC),
		Workdir:   "/tmp/workdir",
	}

	re := RunnerEvent{
		Type:     EventTaskStarted,
		RunnerID: "runner-1",
		Task:     task,
	}

	evt := re.ToEvent()

	// Core fields
	if evt.TaskID != "task-full" {
		t.Errorf("TaskID = %q, want %q", evt.TaskID, "task-full")
	}
	if evt.TaskPath != "projects/p/task/full.md" {
		t.Errorf("TaskPath = %q, want %q", evt.TaskPath, "projects/p/task/full.md")
	}
	if evt.TaskTitle != "Full Task" {
		t.Errorf("TaskTitle = %q, want %q", evt.TaskTitle, "Full Task")
	}
	if evt.ProjectID != "p" {
		t.Errorf("ProjectID = %q, want %q", evt.ProjectID, "p")
	}
	if evt.FeatureID != "feat-x" {
		t.Errorf("FeatureID = %q, want %q", evt.FeatureID, "feat-x")
	}
}

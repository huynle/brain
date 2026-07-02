package runner

import (
	"encoding/json"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// TestRunnerCommand_UnmarshalWithInlinedTask confirms the SSE dispatch
// payload can carry a full ResolvedTask on the "task" field, so the
// runner can avoid an HTTP round-trip in handleDispatchCommand.
//
// This inlining is what lets us delete GetReadyTasks from the dispatch
// hot path — the scheduler already has the resolved task when it
// creates the lease, so serializing it into the SSE command means the
// runner never has to fetch.
func TestRunnerCommand_UnmarshalWithInlinedTask(t *testing.T) {
	payload := `{
		"type": "dispatch",
		"taskId": "abc12345",
		"projectId": "orion-ai",
		"lease": {"id": "l-1", "expires_at": 12345},
		"task": {
			"id": "abc12345",
			"path": "projects/orion-ai/task/abc12345.md",
			"title": "Test task",
			"priority": "high",
			"status": "pending",
			"depends_on": [],
			"created": "2026-07-01T00:00:00Z",
			"projectId": "orion-ai",
			"workdir": "/work",
			"git_remote": "",
			"git_branch": "",
			"agent": "opencode",
			"model": "",
			"executor": "opencode",
			"direct_prompt": "do the thing"
		}
	}`

	var cmd RunnerCommand
	if err := json.Unmarshal([]byte(payload), &cmd); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if cmd.Task == nil {
		t.Fatal("cmd.Task is nil — expected inlined ResolvedTask")
	}
	if cmd.Task.ID != "abc12345" {
		t.Errorf("Task.ID = %q, want %q", cmd.Task.ID, "abc12345")
	}
	if cmd.Task.Title != "Test task" {
		t.Errorf("Task.Title = %q, want %q", cmd.Task.Title, "Test task")
	}
	if cmd.Task.DirectPrompt != "do the thing" {
		t.Errorf("Task.DirectPrompt = %q, want %q", cmd.Task.DirectPrompt, "do the thing")
	}
	if cmd.Task.Executor != "opencode" {
		t.Errorf("Task.Executor = %q, want %q", cmd.Task.Executor, "opencode")
	}
}

// TestRunnerCommand_UnmarshalWithoutInlinedTask confirms backwards
// compatibility: older API servers that don't inline the task should
// still produce a valid RunnerCommand with cmd.Task == nil (runner
// falls back to GetReadyTasks).
func TestRunnerCommand_UnmarshalWithoutInlinedTask(t *testing.T) {
	payload := `{
		"type": "dispatch",
		"taskId": "abc12345",
		"projectId": "orion-ai",
		"lease": {"id": "l-1", "expires_at": 12345}
	}`

	var cmd RunnerCommand
	if err := json.Unmarshal([]byte(payload), &cmd); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if cmd.TaskID != "abc12345" {
		t.Errorf("TaskID = %q, want %q", cmd.TaskID, "abc12345")
	}
	if cmd.Task != nil {
		t.Errorf("cmd.Task = %+v, want nil (payload omitted task field)", cmd.Task)
	}
}

// avoid unused import warning
var _ = types.ResolvedTask{}

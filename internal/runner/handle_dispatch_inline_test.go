package runner

import (
	"context"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// TestHandleDispatchCommand_UsesInlinedTaskWithoutFetch confirms that
// when the dispatch payload includes an inlined ResolvedTask, the
// runner processes the dispatch WITHOUT calling GetReadyTasks. This
// eliminates the HTTP round-trip that caused task_lookup_failed
// rejections under API load.
//
// Setup: mockClient.readyTasks is empty for the project, so a fetch
// would fail to find the task. If the runner ignores the inlined task
// and fetches anyway, we'd see a "task_not_found" rejection.
func TestHandleDispatchCommand_UsesInlinedTaskWithoutFetch(t *testing.T) {
	client := newMockClient()
	// Explicitly leave client.readyTasks empty. Any fetch will return
	// an empty list, which the current impl treats as task_not_found.
	client.readyTasks = map[string][]types.ResolvedTask{}

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	cfg.Capabilities = []string{"dispatch_push"}
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"},
		Config:   cfg, Mode: ExecutionModeHeadless,
		Client: client, Executor: executor,
		ProcessMgr: processMgr, StateMgr: stateMgr,
	})

	task := testTask("task1", "proj-a")
	task.Executor = "opencode"

	tr.handleCommand(context.Background(), RunnerCommand{
		Type:      CommandDispatch,
		ProjectID: "proj-a",
		TaskID:    "task1",
		LeaseID:   "lease-1",
		Task:      task,
	})

	// The runner should have ACKed (found the task via cmd.Task) and
	// NOT rejected with task_not_found.
	if got := len(client.getAckCalls()); got != 1 {
		t.Fatalf("ack calls = %d, want 1 (runner should have used inlined task)", got)
	}
	rejects := client.getRejectCalls()
	for _, r := range rejects {
		if r.Reason.Code == "task_not_found" || r.Reason.Code == "task_lookup_failed" {
			t.Fatalf("unexpected reject %q: %+v — runner should NOT have fetched", r.Reason.Code, r)
		}
	}
}

// TestHandleDispatchCommand_FallsBackToFetchWhenTaskMissing confirms
// backwards compatibility: when the dispatch payload does NOT include
// an inlined task (older API server), the runner falls back to
// GetReadyTasks so old wire formats still work.
func TestHandleDispatchCommand_FallsBackToFetchWhenTaskMissing(t *testing.T) {
	client := newMockClient()
	client.readyTasks["proj-a"] = []types.ResolvedTask{*testTask("task1", "proj-a")}
	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	stateMgr := newMockStateMgr()
	cfg := testRunnerConfig()
	cfg.Capabilities = []string{"dispatch_push"}
	tr := NewTaskRunner(TaskRunnerOptions{
		Projects: []string{"proj-a"},
		Config:   cfg, Mode: ExecutionModeHeadless,
		Client: client, Executor: executor,
		ProcessMgr: processMgr, StateMgr: stateMgr,
	})

	tr.handleCommand(context.Background(), RunnerCommand{
		Type:      CommandDispatch,
		ProjectID: "proj-a",
		TaskID:    "task1",
		LeaseID:   "lease-1",
		// Task field intentionally omitted — simulates legacy scheduler.
	})

	// Runner should have fetched and ACKed via the fallback path.
	if got := len(client.getAckCalls()); got != 1 {
		t.Fatalf("ack calls = %d, want 1 (fallback fetch should succeed)", got)
	}
}

// avoid unused
var _ = time.Second

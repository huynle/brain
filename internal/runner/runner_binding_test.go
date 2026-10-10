package runner

import (
	"context"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// A binding's generated task keeps generated_by on its parent, and the runner
// has no binding ID at all. So the parent's max_concurrent gate must govern a
// binding's task exactly as it governs the parent's own tasks.

func TestTaskRunner_Poll_BindingTaskIsGatedByParentMaxConcurrent(t *testing.T) {
	client := newMockClient()
	task := testTask("bind-task", "proj-a")
	task.GeneratedBy = "automation:parent01"
	client.nextTask["proj-a"] = task
	client.getEntryResult = map[string]*types.BrainEntry{
		"parent01": {ID: "parent01", Trigger: &types.TriggerConfig{MaxConcurrent: 1}},
	}

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	processMgr.Add("parent-run", RunningTask{ID: "parent-run", GeneratedBy: "automation:parent01"}, newMockProcess(100))
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)
	tr.PauseProject("proj-a")
	tr.poll(context.Background())

	if n := len(executor.getSpawnCalls()); n != 0 {
		t.Fatalf("binding task spawned %d time(s) at the parent's max_concurrent, want 0", n)
	}
}

func TestTaskRunner_Poll_BindingTaskRunsBelowParentMaxConcurrent(t *testing.T) {
	client := newMockClient()
	task := testTask("bind-task", "proj-a")
	task.GeneratedBy = "automation:parent01"
	client.nextTask["proj-a"] = task
	client.getEntryResult = map[string]*types.BrainEntry{
		"parent01": {ID: "parent01", Trigger: &types.TriggerConfig{MaxConcurrent: 2}},
	}

	executor := newMockExecutor()
	processMgr := newMockProcessMgr()
	processMgr.Add("parent-run", RunningTask{ID: "parent-run", GeneratedBy: "automation:parent01"}, newMockProcess(100))
	stateMgr := newMockStateMgr()

	tr := newTestRunner(client, executor, processMgr, stateMgr)
	tr.PauseProject("proj-a")
	tr.poll(context.Background())

	if n := len(executor.getSpawnCalls()); n != 1 {
		t.Fatalf("binding task spawned %d time(s) below the parent's max_concurrent, want 1", n)
	}
}

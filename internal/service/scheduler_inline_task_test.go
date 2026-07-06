package service

import (
	"context"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// TestSchedulerService_ScheduleProject_InlinesTaskInDispatchPayload
// confirms that the scheduler serializes the resolved task into the
// dispatch payload so the runner can process it without a follow-up
// HTTP fetch to GetReadyTasks.
//
// This is the observable half of a two-side fix: runner-side
// unmarshaling already accepts the "task" field; scheduler-side now
// emits it.
func TestSchedulerService_ScheduleProject_InlinesTaskInDispatchPayload(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{
		ID: "abc12345", ProjectID: "proj-a", Status: "pending",
		Classification: "ready", Executor: "opencode",
		Title: "Test task", DirectPrompt: "do the thing",
	}}
	store.runners = []types.RunnerInfo{{
		RunnerID: "runner-a", Status: types.RunnerStatusOnline,
		DispatchPush: true, Executors: []string{"opencode"},
		MaxParallel: 1, ActiveTasks: 0,
	}}

	svc := NewSchedulerService(store, nil, store)
	if _, err := svc.ScheduleProject(context.Background(), "proj-a"); err != nil {
		t.Fatalf("ScheduleProject: %v", err)
	}

	if len(store.commands) != 1 {
		t.Fatalf("commands = %d, want 1", len(store.commands))
	}
	payload, ok := store.commands[0].payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", store.commands[0].payload)
	}

	// The critical assertion: dispatch payload must include the full
	// resolved task under "task".
	taskAny, ok := payload["task"]
	if !ok {
		t.Fatalf("payload missing \"task\" field; got keys: %v", mapKeys(payload))
	}
	task, ok := taskAny.(types.ResolvedTask)
	if !ok {
		// Also accept *ResolvedTask.
		if tp, ptrOK := taskAny.(*types.ResolvedTask); ptrOK && tp != nil {
			task = *tp
		} else {
			t.Fatalf("payload.task type = %T, want types.ResolvedTask", taskAny)
		}
	}
	if task.ID != "abc12345" {
		t.Errorf("payload.task.ID = %q, want %q", task.ID, "abc12345")
	}
	if task.DirectPrompt != "do the thing" {
		t.Errorf("payload.task.DirectPrompt = %q, want %q", task.DirectPrompt, "do the thing")
	}
}

func mapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

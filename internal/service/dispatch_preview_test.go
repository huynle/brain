package service

import (
	"context"
	"github.com/huynle/brain-api/internal/types"
	"testing"
)

type previewTasks struct{ *fakeSchedulerStore }

func (p previewTasks) GetTasks(ctx context.Context, project string) (*types.TaskListResponse, error) {
	return &types.TaskListResponse{Tasks: p.tasks}, nil
}
func TestDispatchPreviewDoesNotClaimOrSend(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{ID: "task-1", ProjectID: "proj", Status: "pending", Classification: "ready", Executor: "opencode"}}
	store.runners = []types.RunnerInfo{{RunnerID: "r", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 2}}
	svc := NewSchedulerService(previewTasks{store}, nil, store)
	result, err := svc.DispatchPreview(context.Background(), "proj", "task-1", false)
	if err != nil {
		t.Fatal(err)
	}
	if result["dispatchable"] != true || result["reservation"] != false || len(store.leases) != 0 || len(store.commands) != 0 {
		t.Fatal(result, store.leases, store.commands)
	}
}

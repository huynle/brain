package service

import (
	"context"
	"errors"
	"testing"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

func TestStandaloneTaskAssignmentRoutesPullAndClaimToSelectedRunner(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	insertTaskNote(t, store, "task-one", "One", "pending", "high", "brain", map[string]any{})
	insertCandidateRunner(t, store, storage.RunnerRow{RunnerID: "runner-a", Executors: []string{"opencode"}})
	insertCandidateRunner(t, store, storage.RunnerRow{RunnerID: "runner-b", Executors: []string{"opencode"}})

	resp, err := svc.AssignTaskToRunner(context.Background(), "brain", "task-one", types.TaskAssignmentRequest{RunnerID: "runner-a", Intent: "assign"})
	if err != nil {
		t.Fatalf("AssignTaskToRunner: %v", err)
	}
	if resp.RunnerID != "runner-a" || resp.Scope != "task" {
		t.Fatalf("assignment = %+v", resp)
	}
	listed, err := svc.GetTasks(context.Background(), "brain")
	if err != nil || len(listed.Tasks) != 1 || listed.Tasks[0].AssignedRunnerID != "runner-a" || listed.Tasks[0].AssignmentScope != "task" {
		t.Fatalf("effective assignment = %+v, err %v", listed, err)
	}

	readyA, err := svc.GetReady(context.Background(), "brain", &api.TaskFilterOptions{RunnerID: "runner-a"})
	if err != nil || len(readyA) != 1 {
		t.Fatalf("runner-a ready = %+v, err %v", readyA, err)
	}
	readyB, err := svc.GetReady(context.Background(), "brain", &api.TaskFilterOptions{RunnerID: "runner-b"})
	if err != nil || len(readyB) != 0 {
		t.Fatalf("runner-b ready = %+v, err %v", readyB, err)
	}
	if _, err := svc.ClaimTask(context.Background(), "brain", "task-one", "runner-b"); !errors.Is(err, api.ErrConflict) {
		t.Fatalf("wrong-runner claim error = %v, want conflict", err)
	}
}

func TestAssignTaskToRunnerRejectsFeatureTaskAndIncompatibleRunner(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	insertTaskNote(t, store, "feature-task", "Feature", "pending", "high", "brain", map[string]any{"feature_id": "feature-one"})
	insertTaskNote(t, store, "gpu-task", "GPU", "pending", "high", "brain", map[string]any{"requires_capability": []string{"gpu"}})
	insertCandidateRunner(t, store, storage.RunnerRow{RunnerID: "cpu", Executors: []string{"opencode"}})

	if _, err := svc.AssignTaskToRunner(context.Background(), "brain", "feature-task", types.TaskAssignmentRequest{RunnerID: "cpu", Intent: "assign"}); !errors.Is(err, api.ErrConflict) {
		t.Fatalf("feature task assignment error = %v, want conflict", err)
	}
	if _, err := svc.AssignTaskToRunner(context.Background(), "brain", "gpu-task", types.TaskAssignmentRequest{RunnerID: "cpu", Intent: "assign", Force: true}); !errors.Is(err, api.ErrConflict) {
		t.Fatalf("incompatible forced assignment error = %v, want conflict", err)
	}
}

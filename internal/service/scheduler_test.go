package service

import (
	"context"
	"testing"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

func TestSchedulerDispatchesSoftAffinityPreferredRunner(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{ID: "task-1", ProjectID: "proj", Status: "pending", Classification: "ready", Executor: "opencode"}}
	store.runners = []types.RunnerInfo{
		{RunnerID: "runner-a", MachineID: "machine-a", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 2},
		{RunnerID: "runner-b", MachineID: "machine-b", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 2},
	}
	store.placement = types.ProjectPlacement{ProjectID: "proj", Affinity: types.PlacementAffinitySoft, PreferredMachines: []string{"machine-b"}}

	svc := NewSchedulerService(store, nil, store)
	result, err := svc.ScheduleProject(context.Background(), "proj")
	if err != nil {
		t.Fatalf("ScheduleProject failed: %v", err)
	}

	if result.Dispatched != 1 {
		t.Fatalf("Dispatched = %d, want 1", result.Dispatched)
	}
	if len(store.leases) != 1 || store.leases[0].AssignedRunnerID != "runner-b" {
		t.Fatalf("lease assigned runner = %#v, want runner-b", store.leases)
	}
	if len(store.commands) != 1 || store.commands[0].runnerID != "runner-b" || store.commands[0].command != "dispatch" {
		t.Fatalf("commands = %#v, want dispatch to runner-b", store.commands)
	}
}

func TestSchedulerRecordsNoCandidateForStrictAffinityWithoutChangingTaskStatus(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{ID: "task-1", ProjectID: "proj", Status: "pending", Classification: "ready", Executor: "opencode"}}
	store.runners = []types.RunnerInfo{{RunnerID: "runner-a", MachineID: "machine-a", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 2}}
	store.placement = types.ProjectPlacement{ProjectID: "proj", Affinity: types.PlacementAffinityStrict, PreferredMachines: []string{"machine-b"}}

	svc := NewSchedulerService(store, nil, store)
	result, err := svc.ScheduleProject(context.Background(), "proj")
	if err != nil {
		t.Fatalf("ScheduleProject failed: %v", err)
	}

	if result.Dispatched != 0 || result.Skipped != 1 {
		t.Fatalf("result = %#v, want 0 dispatched and 1 skipped", result)
	}
	if store.tasks[0].Status != "pending" {
		t.Fatalf("task status = %q, want pending", store.tasks[0].Status)
	}
	if len(store.reasons) == 0 {
		t.Fatal("expected placement reason for no candidate")
	}
}

func TestSchedulerFiltersCandidatesByExecutorCapabilityWorkspaceCapacityAndDraining(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{
		ID:                 "task-1",
		ProjectID:          "proj",
		Status:             "pending",
		Classification:     "ready",
		Executor:           "pi",
		RequiresCapability: []string{"gpu"},
	}}
	store.runners = []types.RunnerInfo{
		{RunnerID: "wrong-executor", MachineID: "machine-a", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, Capabilities: []string{"gpu"}, WorkspaceRoots: []string{"/work"}, MaxParallel: 2},
		{RunnerID: "missing-cap", MachineID: "machine-b", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"pi"}, WorkspaceRoots: []string{"/work"}, MaxParallel: 2},
		{RunnerID: "no-workspace", MachineID: "machine-c", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"pi"}, Capabilities: []string{"gpu"}, MaxParallel: 2},
		{RunnerID: "full", MachineID: "machine-d", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"pi"}, Capabilities: []string{"gpu"}, WorkspaceRoots: []string{"/work"}, MaxParallel: 1, ActiveTasks: 1},
		{RunnerID: "draining", MachineID: "machine-e", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"pi"}, Capabilities: []string{"gpu"}, WorkspaceRoots: []string{"/work"}, MaxParallel: 2, Draining: true},
		{RunnerID: "eligible", MachineID: "machine-f", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"pi"}, Capabilities: []string{"gpu"}, WorkspaceRoots: []string{"/work"}, MaxParallel: 2},
	}
	store.placement = types.ProjectPlacement{ProjectID: "proj", Affinity: types.PlacementAffinityNone, WorkspacePolicy: types.WorkspacePolicyWorktree}

	svc := NewSchedulerService(store, nil, store)
	result, err := svc.ScheduleProject(context.Background(), "proj")
	if err != nil {
		t.Fatalf("ScheduleProject failed: %v", err)
	}

	if result.Dispatched != 1 {
		t.Fatalf("Dispatched = %d, want 1", result.Dispatched)
	}
	if store.leases[0].AssignedRunnerID != "eligible" {
		t.Fatalf("assigned runner = %q, want eligible", store.leases[0].AssignedRunnerID)
	}
}

func TestSchedulerSkipsPausedProjectAndAutomationGeneratedTasks(t *testing.T) {
	store := newFakeSchedulerStore()
	store.paused = true
	store.automationPaused = true
	store.tasks = []types.ResolvedTask{
		{ID: "normal", ProjectID: "proj", Status: "pending", Classification: "ready"},
		{ID: "automation", ProjectID: "proj", Status: "pending", Classification: "ready", GeneratedBy: "automation:auto-1"},
	}
	store.runners = []types.RunnerInfo{{RunnerID: "runner", MachineID: "machine", Status: types.RunnerStatusOnline, DispatchPush: true, MaxParallel: 2}}
	store.placement = types.ProjectPlacement{ProjectID: "proj", Affinity: types.PlacementAffinityNone}

	svc := NewSchedulerService(store, store, store)
	result, err := svc.ScheduleProject(context.Background(), "proj")
	if err != nil {
		t.Fatalf("ScheduleProject paused failed: %v", err)
	}
	if result.Dispatched != 0 || result.Skipped != 2 {
		t.Fatalf("paused result = %#v, want all skipped", result)
	}

	store.paused = false
	result, err = svc.ScheduleProject(context.Background(), "proj")
	if err != nil {
		t.Fatalf("ScheduleProject automation paused failed: %v", err)
	}
	if result.Dispatched != 1 || len(store.leases) != 1 || store.leases[0].TaskID != "normal" {
		t.Fatalf("automation paused leases = %#v result = %#v, want only normal dispatched", store.leases, result)
	}
}

type fakeSchedulerStore struct {
	tasks            []types.ResolvedTask
	runners          []types.RunnerInfo
	placement        types.ProjectPlacement
	paused           bool
	automationPaused bool
	leases           []storage.DispatchLeaseCreate
	reasons          []storage.PlacementReasonRow
	commands         []fakeRunnerCommand
}

type fakeRunnerCommand struct {
	runnerID string
	command  string
	payload  any
}

func newFakeSchedulerStore() *fakeSchedulerStore {
	return &fakeSchedulerStore{placement: types.ProjectPlacement{ProjectID: "proj", Affinity: types.PlacementAffinityNone}}
}

func (f *fakeSchedulerStore) GetReady(ctx context.Context, projectID string, opts *api.TaskFilterOptions) ([]types.ResolvedTask, error) {
	return append([]types.ResolvedTask(nil), f.tasks...), nil
}

func (f *fakeSchedulerStore) ListRunners(ctx context.Context) ([]types.RunnerInfo, error) {
	return append([]types.RunnerInfo(nil), f.runners...), nil
}

func (f *fakeSchedulerStore) Get(ctx context.Context, projectID string) (*types.ProjectPlacement, error) {
	pl := f.placement
	pl.ProjectID = projectID
	return &pl, nil
}

func (f *fakeSchedulerStore) CreateDispatchLease(ctx context.Context, in storage.DispatchLeaseCreate) (*storage.DispatchLeaseRow, bool, error) {
	f.leases = append(f.leases, in)
	return &storage.DispatchLeaseRow{
		ProjectID:         in.ProjectID,
		TaskID:            in.TaskID,
		AssignedRunnerID:  in.AssignedRunnerID,
		AssignedMachineID: in.AssignedMachineID,
		State:             storage.DispatchLeaseStatePushed,
		PushedAt:          in.PushedAt,
		ExpiresAt:         in.ExpiresAt,
	}, true, nil
}

func (f *fakeSchedulerStore) RecordPlacementReason(ctx context.Context, row *storage.PlacementReasonRow) error {
	f.reasons = append(f.reasons, *row)
	return nil
}

func (f *fakeSchedulerStore) PublishRunnerCommand(runnerID string, command string, payload interface{}) {
	f.commands = append(f.commands, fakeRunnerCommand{runnerID: runnerID, command: command, payload: payload})
}

func (f *fakeSchedulerStore) IsPaused(projectID string) bool { return f.paused }

func (f *fakeSchedulerStore) IsAutomationsPaused() bool { return f.automationPaused }

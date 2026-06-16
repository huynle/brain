package service

import (
	"context"
	"testing"
	"time"

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

func TestSchedulerChoosesPreferredMachineBeforeLeastBusyRunner(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{ID: "task-1", ProjectID: "proj", Status: "pending", Classification: "ready", Executor: "opencode"}}
	store.runners = []types.RunnerInfo{
		{RunnerID: "idle-other-machine", MachineID: "machine-a", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 2, ActiveTasks: 0},
		{RunnerID: "busy-preferred", MachineID: "machine-b", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 100, ActiveTasks: 80},
		{RunnerID: "less-busy-preferred", MachineID: "machine-b", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 100, ActiveTasks: 60},
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
	if len(store.leases) != 1 || store.leases[0].AssignedRunnerID != "less-busy-preferred" {
		t.Fatalf("lease assigned runner = %#v, want less-busy-preferred", store.leases)
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

func TestSchedulerReservesRunnerCapacityWithinSinglePass(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{
		{ID: "task-1", ProjectID: "proj", Status: "pending", Classification: "ready", Executor: "opencode"},
		{ID: "task-2", ProjectID: "proj", Status: "pending", Classification: "ready", Executor: "opencode"},
	}
	store.runners = []types.RunnerInfo{{RunnerID: "runner", MachineID: "machine", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 1}}
	store.placement = types.ProjectPlacement{ProjectID: "proj", Affinity: types.PlacementAffinityNone}

	svc := NewSchedulerService(store, nil, store)
	result, err := svc.ScheduleProject(context.Background(), "proj")
	if err != nil {
		t.Fatalf("ScheduleProject failed: %v", err)
	}

	if result.Dispatched != 1 || result.Skipped != 1 {
		t.Fatalf("result = %#v, want 1 dispatched and 1 skipped", result)
	}
	if len(store.leases) != 1 || store.leases[0].TaskID != "task-1" {
		t.Fatalf("leases = %#v, want only task-1 leased", store.leases)
	}
	if len(store.commands) != 1 || store.commands[0].runnerID != "runner" {
		t.Fatalf("commands = %#v, want one dispatch to runner", store.commands)
	}
	if len(store.reasons) != 1 || store.reasons[0].TaskID != "task-2" {
		t.Fatalf("reasons = %#v, want no-candidate reason for task-2", store.reasons)
	}
}

func TestSchedulerFiltersCandidatesByProjectResources(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{ID: "task-1", ProjectID: "proj", Status: "pending", Classification: "ready", Executor: "opencode"}}
	store.runners = []types.RunnerInfo{
		{RunnerID: "missing-numeric", MachineID: "machine-a", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 2, Resources: map[string]interface{}{"gpu": 1, "arch": "arm64", "ssd": true}, Capacity: map[string]interface{}{"memory_gb": 16}},
		{RunnerID: "missing-bool", MachineID: "machine-b", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 2, Resources: map[string]interface{}{"gpu": 2, "arch": "arm64", "ssd": false}, Capacity: map[string]interface{}{"memory_gb": 32}},
		{RunnerID: "missing-string", MachineID: "machine-c", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 2, Resources: map[string]interface{}{"gpu": 2, "arch": "amd64", "ssd": true}, Capacity: map[string]interface{}{"memory_gb": 32}},
		{RunnerID: "eligible", MachineID: "machine-d", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 2, Resources: map[string]interface{}{"gpu": 2, "arch": "arm64", "ssd": true}, Capacity: map[string]interface{}{"memory_gb": 32}},
	}
	store.placement = types.ProjectPlacement{
		ProjectID: "proj",
		Affinity:  types.PlacementAffinityNone,
		Resources: map[string]any{"gpu": 2, "memory_gb": 32, "arch": "arm64", "ssd": true},
	}

	svc := NewSchedulerService(store, nil, store)
	result, err := svc.ScheduleProject(context.Background(), "proj")
	if err != nil {
		t.Fatalf("ScheduleProject failed: %v", err)
	}

	if result.Dispatched != 1 {
		t.Fatalf("Dispatched = %d, want 1", result.Dispatched)
	}
	if len(store.leases) != 1 || store.leases[0].AssignedRunnerID != "eligible" {
		t.Fatalf("leases = %#v, want eligible runner", store.leases)
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

func TestSchedulerLifecycleTickExpiresLeasesSchedulesProjectsAndUpdatesStatus(t *testing.T) {
	store := newFakeSchedulerStore()
	store.projects = []string{"alpha", "beta"}
	store.tasksByProject = map[string][]types.ResolvedTask{
		"alpha": {{ID: "task-alpha", ProjectID: "alpha", Status: "pending", Classification: "ready"}},
		"beta":  {{ID: "task-beta", ProjectID: "beta", Status: "pending", Classification: "ready"}},
	}
	store.runners = []types.RunnerInfo{{RunnerID: "runner", MachineID: "machine", Status: types.RunnerStatusOnline, DispatchPush: true, MaxParallel: 4}}
	store.placement = types.ProjectPlacement{Affinity: types.PlacementAffinityNone}

	svc := NewSchedulerService(store, nil, store)
	svc.nowUnixMS = func() int64 { return 1234 }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc.Start(ctx, time.Hour)

	if err := waitForCondition(200*time.Millisecond, func() bool {
		return len(store.scheduledProjects) == 2
	}); err != nil {
		t.Fatalf("scheduler did not schedule expected projects: %v", err)
	}
	cancel()

	status := svc.Status()
	if !status.Started || !status.Running {
		t.Fatalf("status started/running = %v/%v, want true/true", status.Started, status.Running)
	}
	if status.Interval != time.Hour.String() {
		t.Fatalf("interval = %v, want %v", status.Interval, time.Hour.String())
	}
	if status.TotalTicks != 1 {
		t.Fatalf("total ticks = %d, want 1", status.TotalTicks)
	}
	if status.LastTickAt == "" || status.LastSuccessAt == "" {
		t.Fatalf("last tick/success should be set: %#v", status)
	}
	if status.LastExpiredLeases != 3 {
		t.Fatalf("last expired leases = %d, want 3", status.LastExpiredLeases)
	}
	if got := store.expireCalls; got != 1 {
		t.Fatalf("expire calls = %d, want 1", got)
	}
	if got := store.expireAt; got != 1234 {
		t.Fatalf("expire timestamp = %d, want 1234", got)
	}
	if len(status.LastProjectResults) != 2 || status.LastProjectResults["alpha"].Dispatched != 1 || status.LastProjectResults["beta"].Dispatched != 1 {
		t.Fatalf("project results = %#v, want alpha and beta dispatched", status.LastProjectResults)
	}
}

func waitForCondition(timeout time.Duration, fn func() bool) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return nil
		}
		time.Sleep(5 * time.Millisecond)
	}
	return context.DeadlineExceeded
}

type fakeSchedulerStore struct {
	tasks             []types.ResolvedTask
	tasksByProject    map[string][]types.ResolvedTask
	projects          []string
	scheduledProjects []string
	expireCalls       int
	expireAt          int64
	runners           []types.RunnerInfo
	placement         types.ProjectPlacement
	paused            bool
	automationPaused  bool
	leases            []storage.DispatchLeaseCreate
	reasons           []storage.PlacementReasonRow
	commands          []fakeRunnerCommand
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
	f.scheduledProjects = append(f.scheduledProjects, projectID)
	if f.tasksByProject != nil {
		return append([]types.ResolvedTask(nil), f.tasksByProject[projectID]...), nil
	}
	return append([]types.ResolvedTask(nil), f.tasks...), nil
}

func (f *fakeSchedulerStore) ListProjects(ctx context.Context) ([]string, error) {
	return append([]string(nil), f.projects...), nil
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

func (f *fakeSchedulerStore) ExpireDispatchLeases(ctx context.Context, now int64) (int64, error) {
	f.expireCalls++
	f.expireAt = now
	return 3, nil
}

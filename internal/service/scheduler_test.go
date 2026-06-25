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

func TestSchedulerSkipsAutomationGeneratedTasksOnlyForPausedAutomationProject(t *testing.T) {
	store := newFakeSchedulerStore()
	store.automationPausedProjects = map[string]bool{"proj-a": true}
	store.tasksByProject = map[string][]types.ResolvedTask{
		"proj-a": {{ID: "automation-a", ProjectID: "proj-a", Status: "pending", Classification: "ready", GeneratedBy: "automation:auto-a"}},
		"proj-b": {{ID: "automation-b", ProjectID: "proj-b", Status: "pending", Classification: "ready", GeneratedBy: "automation:auto-b"}},
	}
	store.runners = []types.RunnerInfo{{RunnerID: "runner", MachineID: "machine", Status: types.RunnerStatusOnline, DispatchPush: true, MaxParallel: 2}}
	store.placement = types.ProjectPlacement{Affinity: types.PlacementAffinityNone}

	svc := NewSchedulerService(store, store, store)
	resultA, err := svc.ScheduleProject(context.Background(), "proj-a")
	if err != nil {
		t.Fatalf("ScheduleProject proj-a failed: %v", err)
	}
	resultB, err := svc.ScheduleProject(context.Background(), "proj-b")
	if err != nil {
		t.Fatalf("ScheduleProject proj-b failed: %v", err)
	}

	if resultA.Dispatched != 0 || resultA.Skipped != 1 {
		t.Fatalf("proj-a result = %#v, want automation skipped", resultA)
	}
	if resultB.Dispatched != 1 {
		t.Fatalf("proj-b result = %#v, want automation dispatched", resultB)
	}
	if len(store.leases) != 1 || store.leases[0].TaskID != "automation-b" {
		t.Fatalf("leases = %#v, want only automation-b", store.leases)
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
	tasks                    []types.ResolvedTask
	tasksByProject           map[string][]types.ResolvedTask
	projects                 []string
	scheduledProjects        []string
	expireCalls              int
	expireAt                 int64
	runners                  []types.RunnerInfo
	placement                types.ProjectPlacement
	paused                   bool
	automationPaused         bool
	automationPausedProjects map[string]bool
	leases                   []storage.DispatchLeaseCreate
	reasons                  []storage.PlacementReasonRow
	commands                 []fakeRunnerCommand
	// activeLeases simulates the persisted state for already_leased / force
	// scenarios. Keyed by "projectID/taskID".
	activeLeases  map[string]*storage.DispatchLeaseRow
	releasedKeys  []string
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
	key := in.ProjectID + "/" + in.TaskID
	if f.activeLeases != nil {
		if existing := f.activeLeases[key]; existing != nil {
			return existing, false, nil
		}
	}
	f.leases = append(f.leases, in)
	row := &storage.DispatchLeaseRow{
		ProjectID:         in.ProjectID,
		TaskID:            in.TaskID,
		LeaseID:           in.LeaseID,
		AssignedRunnerID:  in.AssignedRunnerID,
		AssignedMachineID: in.AssignedMachineID,
		State:             storage.DispatchLeaseStatePushed,
		PushedAt:          in.PushedAt,
		ExpiresAt:         in.ExpiresAt,
	}
	if f.activeLeases == nil {
		f.activeLeases = map[string]*storage.DispatchLeaseRow{}
	}
	f.activeLeases[key] = row
	return row, true, nil
}

func (f *fakeSchedulerStore) GetDispatchLeaseRow(ctx context.Context, projectID, taskID string) (*storage.DispatchLeaseRow, error) {
	if f.activeLeases == nil {
		return nil, nil
	}
	return f.activeLeases[projectID+"/"+taskID], nil
}

func (f *fakeSchedulerStore) ReleaseDispatchLease(ctx context.Context, projectID, taskID, runnerID string) (bool, error) {
	key := projectID + "/" + taskID
	if f.activeLeases == nil {
		return false, nil
	}
	existing, ok := f.activeLeases[key]
	if !ok || existing.AssignedRunnerID != runnerID {
		return false, nil
	}
	delete(f.activeLeases, key)
	f.releasedKeys = append(f.releasedKeys, key)
	return true, nil
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

func (f *fakeSchedulerStore) IsAutomationsPausedForProject(projectID string) bool {
	return f.automationPaused || f.automationPausedProjects[projectID]
}

func (f *fakeSchedulerStore) ExpireDispatchLeases(ctx context.Context, now int64) (int64, error) {
	f.expireCalls++
	f.expireAt = now
	return 3, nil
}

// =============================================================================
// RunTaskNow tests — manual ad-hoc dispatch from a UI ("x" key in PWA / TUI).
// =============================================================================

func TestRunTaskNow_DispatchesToEligibleRunner(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{
		ID: "task-1", ProjectID: "proj", Status: "pending",
		Classification: "ready", Executor: "opencode",
	}}
	store.runners = []types.RunnerInfo{
		{
			RunnerID: "runner-a", MachineID: "machine-a",
			Status: types.RunnerStatusOnline, DispatchPush: true,
			Executors: []string{"opencode"}, MaxParallel: 2,
		},
	}

	svc := NewSchedulerService(store, nil, store)
	resp, err := svc.RunTaskNow(context.Background(), "proj", "task-1", false)
	if err != nil {
		t.Fatalf("RunTaskNow failed: %v", err)
	}
	if !resp.Dispatched {
		t.Fatalf("Dispatched = false; reason=%q runnerId=%q", resp.Reason, resp.RunnerID)
	}
	if resp.RunnerID != "runner-a" {
		t.Fatalf("RunnerID = %q, want runner-a", resp.RunnerID)
	}
	if len(store.leases) != 1 || store.leases[0].TaskID != "task-1" {
		t.Fatalf("expected one lease for task-1, got %#v", store.leases)
	}
	if len(store.commands) != 1 || store.commands[0].command != "dispatch" || store.commands[0].runnerID != "runner-a" {
		t.Fatalf("expected dispatch command to runner-a, got %#v", store.commands)
	}
}

func TestRunTaskNow_NoOnlineRunnerReturnsReason(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{
		ID: "task-1", ProjectID: "proj", Status: "pending",
		Classification: "ready", Executor: "opencode",
	}}
	// No runners registered.

	svc := NewSchedulerService(store, nil, store)
	resp, err := svc.RunTaskNow(context.Background(), "proj", "task-1", false)
	if err != nil {
		t.Fatalf("RunTaskNow returned error: %v", err)
	}
	if resp.Dispatched {
		t.Fatal("Dispatched = true, want false (no runners)")
	}
	if resp.Reason == "" {
		t.Fatal("Reason should be set when no runner available")
	}
	if len(store.leases) != 0 {
		t.Fatalf("no lease should be created when no runner; got %#v", store.leases)
	}
	if len(store.commands) != 0 {
		t.Fatalf("no command should be published when no runner; got %#v", store.commands)
	}
}

func TestRunTaskNow_TaskNotFoundReturnsReason(t *testing.T) {
	store := newFakeSchedulerStore()
	// proj has no ready tasks at all.
	store.runners = []types.RunnerInfo{
		{
			RunnerID: "runner-a", MachineID: "machine-a",
			Status: types.RunnerStatusOnline, DispatchPush: true,
			Executors: []string{"opencode"}, MaxParallel: 2,
		},
	}

	svc := NewSchedulerService(store, nil, store)
	resp, err := svc.RunTaskNow(context.Background(), "proj", "missing-task", false)
	if err != nil {
		t.Fatalf("RunTaskNow returned error: %v", err)
	}
	if resp.Dispatched {
		t.Fatal("Dispatched = true, want false (task missing)")
	}
	if resp.Reason == "" {
		t.Fatal("Reason should be set when task not found")
	}
}

func TestRunTaskNow_PausedProjectIsBypassedWhenForce(t *testing.T) {
	// User answer: auto-bypass pause silently. RunTaskNow always passes
	// force=true through to the runner, so the runner side accepts dispatch
	// even when the project is paused. This test pins down that contract.
	store := newFakeSchedulerStore()
	store.paused = true
	store.tasks = []types.ResolvedTask{{
		ID: "task-1", ProjectID: "proj", Status: "pending",
		Classification: "ready", Executor: "opencode",
	}}
	store.runners = []types.RunnerInfo{
		{
			RunnerID: "runner-a", MachineID: "machine-a",
			Status: types.RunnerStatusOnline, DispatchPush: true,
			Executors: []string{"opencode"}, MaxParallel: 2,
		},
	}

	svc := NewSchedulerService(store, store, store)
	resp, err := svc.RunTaskNow(context.Background(), "proj", "task-1", true)
	if err != nil {
		t.Fatalf("RunTaskNow failed: %v", err)
	}
	if !resp.Dispatched {
		t.Fatalf("Dispatched = false on paused project with force=true; reason=%q", resp.Reason)
	}
	if len(store.commands) != 1 {
		t.Fatalf("expected dispatch command when force-running paused project, got %#v", store.commands)
	}
	cmd := store.commands[0]
	payload, ok := cmd.payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", cmd.payload)
	}
	if payload["force"] != true {
		t.Fatalf("dispatch payload force = %v, want true (so paused runner accepts)", payload["force"])
	}
}

func TestRunTaskNow_AllRunnersAtCapacityReturnsReason(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{
		ID: "task-1", ProjectID: "proj", Status: "pending",
		Classification: "ready", Executor: "opencode",
	}}
	store.runners = []types.RunnerInfo{
		{
			RunnerID: "runner-a", MachineID: "machine-a",
			Status: types.RunnerStatusOnline, DispatchPush: true,
			Executors: []string{"opencode"}, MaxParallel: 1, ActiveTasks: 1,
		},
	}

	svc := NewSchedulerService(store, nil, store)
	resp, err := svc.RunTaskNow(context.Background(), "proj", "task-1", false)
	if err != nil {
		t.Fatalf("RunTaskNow returned error: %v", err)
	}
	if resp.Dispatched {
		t.Fatal("Dispatched = true, want false (all runners at capacity)")
	}
	if resp.Reason == "" {
		t.Fatal("Reason should be set when no eligible runner")
	}
	if len(store.commands) != 0 {
		t.Fatalf("no command should be published, got %#v", store.commands)
	}
}

func TestRunTaskNow_AlreadyLeasedSurfacesRunnerAndState(t *testing.T) {
	// When an active dispatch lease blocks a manual run, the response must
	// tell the caller which runner currently owns the task and what state
	// the lease is in. The PWA toast surfaces this so users don't have to
	// dig through logs to figure out why "x" was a no-op.
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{
		ID: "task-1", ProjectID: "proj", Status: "pending",
		Classification: "ready", Executor: "opencode",
	}}
	store.runners = []types.RunnerInfo{
		{
			RunnerID: "runner-a", MachineID: "machine-a",
			Status: types.RunnerStatusOnline, DispatchPush: true,
			Executors: []string{"opencode"}, MaxParallel: 2,
		},
	}
	// Pre-seed an existing lease assigned to a different runner so we can
	// confirm the response surfaces the actual owner, not the new candidate.
	store.activeLeases = map[string]*storage.DispatchLeaseRow{
		"proj/task-1": {
			ProjectID:        "proj",
			TaskID:           "task-1",
			LeaseID:          "lease-existing",
			AssignedRunnerID: "runner-existing",
			State:            storage.DispatchLeaseStatePushed,
			PushedAt:         1000,
			ExpiresAt:        2000,
		},
	}

	svc := NewSchedulerService(store, nil, store)
	resp, err := svc.RunTaskNow(context.Background(), "proj", "task-1", false)
	if err != nil {
		t.Fatalf("RunTaskNow returned error: %v", err)
	}
	if resp.Dispatched {
		t.Fatal("Dispatched = true, want false (already leased)")
	}
	if resp.Reason != "already_leased" {
		t.Fatalf("Reason = %q, want already_leased", resp.Reason)
	}
	if resp.RunnerID != "runner-existing" {
		t.Fatalf("RunnerID = %q, want runner-existing (the lease owner)", resp.RunnerID)
	}
	if resp.LeaseID != "lease-existing" {
		t.Fatalf("LeaseID = %q, want lease-existing", resp.LeaseID)
	}
	if resp.LeaseState != storage.DispatchLeaseStatePushed {
		t.Fatalf("LeaseState = %q, want %q", resp.LeaseState, storage.DispatchLeaseStatePushed)
	}
	if resp.ExpiresAt == "" {
		t.Fatal("ExpiresAt should be populated when an active lease exists")
	}
	if len(store.commands) != 0 {
		t.Fatalf("no dispatch command should be published when blocked, got %#v", store.commands)
	}
}

func TestRunTaskNow_ForceReleasesStaleLeaseAndRedispatches(t *testing.T) {
	// force=true is the recovery path for a stuck/orphaned lease (e.g. the
	// owning runner crashed silently). It must release the existing lease
	// AND publish a fresh dispatch command — otherwise the user is stuck
	// waiting for the TTL to expire.
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{
		ID: "task-1", ProjectID: "proj", Status: "pending",
		Classification: "ready", Executor: "opencode",
	}}
	store.runners = []types.RunnerInfo{
		{
			RunnerID: "runner-a", MachineID: "machine-a",
			Status: types.RunnerStatusOnline, DispatchPush: true,
			Executors: []string{"opencode"}, MaxParallel: 2,
		},
	}
	store.activeLeases = map[string]*storage.DispatchLeaseRow{
		"proj/task-1": {
			ProjectID:        "proj",
			TaskID:           "task-1",
			LeaseID:          "lease-stale",
			AssignedRunnerID: "runner-stale",
			State:            storage.DispatchLeaseStatePushed,
			PushedAt:         1000,
			ExpiresAt:        2000,
		},
	}

	svc := NewSchedulerService(store, nil, store)
	resp, err := svc.RunTaskNow(context.Background(), "proj", "task-1", true)
	if err != nil {
		t.Fatalf("RunTaskNow(force=true) failed: %v", err)
	}
	if !resp.Dispatched {
		t.Fatalf("Dispatched = false with force=true; reason=%q detail=%q", resp.Reason, resp.Detail)
	}
	if resp.RunnerID != "runner-a" {
		t.Fatalf("RunnerID = %q, want runner-a (the new dispatch target)", resp.RunnerID)
	}
	if len(store.releasedKeys) != 1 || store.releasedKeys[0] != "proj/task-1" {
		t.Fatalf("expected one release of proj/task-1, got %#v", store.releasedKeys)
	}
	if len(store.commands) != 1 || store.commands[0].command != "dispatch" || store.commands[0].runnerID != "runner-a" {
		t.Fatalf("expected fresh dispatch command to runner-a, got %#v", store.commands)
	}
	// Force should still propagate to the runner payload so a paused
	// runner accepts the dispatch.
	payload, ok := store.commands[0].payload.(map[string]any)
	if !ok {
		t.Fatalf("payload type = %T, want map[string]any", store.commands[0].payload)
	}
	if payload["force"] != true {
		t.Fatalf("dispatch payload force = %v, want true", payload["force"])
	}
}

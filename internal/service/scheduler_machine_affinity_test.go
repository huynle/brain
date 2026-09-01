package service

import (
	"context"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// twoMachineRunners returns one online push-capable runner on each of
// machine-a and machine-b.
func twoMachineRunners() []types.RunnerInfo {
	return []types.RunnerInfo{
		{RunnerID: "runner-a", MachineID: "machine-a", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 2},
		{RunnerID: "runner-b", MachineID: "machine-b", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 2},
	}
}

// TestMachineAffinityPreferredWinsOverIdleRemoteRunner is the default path: a
// task stamped with an origin machine goes to a runner on that machine even
// when a completely idle runner exists elsewhere.
func TestMachineAffinityPreferredWinsOverIdleRemoteRunner(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{
		ID: "task-1", ProjectID: "proj", Status: "pending", Classification: "ready",
		Executor: "opencode", OriginMachineID: "machine-b",
		// MachineAffinity deliberately unset — this asserts the DEFAULT.
	}}
	store.runners = []types.RunnerInfo{
		{RunnerID: "runner-a", MachineID: "machine-a", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 10, ActiveTasks: 0},
		{RunnerID: "runner-b", MachineID: "machine-b", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 10, ActiveTasks: 5},
	}

	svc := NewSchedulerService(store, nil, store)
	if _, err := svc.ScheduleProject(context.Background(), "proj"); err != nil {
		t.Fatalf("ScheduleProject failed: %v", err)
	}
	if len(store.leases) != 1 || store.leases[0].AssignedRunnerID != "runner-b" {
		t.Fatalf("assigned runner = %#v, want runner-b (the origin machine)", store.leases)
	}
}

// TestMachineAffinityPreferredStillRunsElsewhere is the reason "preferred" is
// the default rather than "local": when the origin machine has no runner, the
// task must still run rather than starve with no visible cause.
func TestMachineAffinityPreferredStillRunsElsewhere(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{
		ID: "task-1", ProjectID: "proj", Status: "pending", Classification: "ready",
		Executor: "opencode", OriginMachineID: "machine-gone",
		MachineAffinity: types.MachineAffinityPreferred,
	}}
	store.runners = twoMachineRunners()

	svc := NewSchedulerService(store, nil, store)
	result, err := svc.ScheduleProject(context.Background(), "proj")
	if err != nil {
		t.Fatalf("ScheduleProject failed: %v", err)
	}
	if result.Dispatched != 1 {
		t.Fatalf("Dispatched = %d, want 1 — preferred must not block placement", result.Dispatched)
	}
}

// TestMachineAffinityLocalRefusesForeignMachine is the hard filter.
func TestMachineAffinityLocalRefusesForeignMachine(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{
		ID: "task-1", ProjectID: "proj", Status: "pending", Classification: "ready",
		Executor: "opencode", OriginMachineID: "machine-b",
		MachineAffinity: types.MachineAffinityLocal,
	}}
	// Only a runner on the WRONG machine is available.
	store.runners = []types.RunnerInfo{
		{RunnerID: "runner-a", MachineID: "machine-a", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 2},
	}

	svc := NewSchedulerService(store, nil, store)
	result, err := svc.ScheduleProject(context.Background(), "proj")
	if err != nil {
		t.Fatalf("ScheduleProject failed: %v", err)
	}
	if result.Dispatched != 0 {
		t.Fatalf("Dispatched = %d, want 0 — local affinity must not place off-machine", result.Dispatched)
	}
	if len(store.leases) != 0 {
		t.Fatalf("leases = %#v, want none", store.leases)
	}
}

// TestMachineAffinityLocalPlacesOnOriginMachine is the other half: the hard
// filter must not block the machine it is pinning to.
func TestMachineAffinityLocalPlacesOnOriginMachine(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{
		ID: "task-1", ProjectID: "proj", Status: "pending", Classification: "ready",
		Executor: "opencode", OriginMachineID: "machine-b",
		MachineAffinity: types.MachineAffinityLocal,
	}}
	store.runners = twoMachineRunners()

	svc := NewSchedulerService(store, nil, store)
	if _, err := svc.ScheduleProject(context.Background(), "proj"); err != nil {
		t.Fatalf("ScheduleProject failed: %v", err)
	}
	if len(store.leases) != 1 || store.leases[0].AssignedRunnerID != "runner-b" {
		t.Fatalf("assigned runner = %#v, want runner-b", store.leases)
	}
}

// TestMachineAffinityNoneIgnoresOrigin proves the escape hatch works even when
// an origin machine is stamped.
func TestMachineAffinityNoneIgnoresOrigin(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{
		ID: "task-1", ProjectID: "proj", Status: "pending", Classification: "ready",
		Executor: "opencode", OriginMachineID: "machine-b",
		MachineAffinity: types.MachineAffinityNone,
	}}
	store.runners = []types.RunnerInfo{
		{RunnerID: "runner-a", MachineID: "machine-a", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 10, ActiveTasks: 0},
		{RunnerID: "runner-b", MachineID: "machine-b", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 10, ActiveTasks: 5},
	}

	svc := NewSchedulerService(store, nil, store)
	if _, err := svc.ScheduleProject(context.Background(), "proj"); err != nil {
		t.Fatalf("ScheduleProject failed: %v", err)
	}
	// With affinity off, ordinary least-busy scoring picks the idle runner.
	if len(store.leases) != 1 || store.leases[0].AssignedRunnerID != "runner-a" {
		t.Fatalf("assigned runner = %#v, want runner-a (origin ignored)", store.leases)
	}
}

// TestMachineAffinityLocalWithoutOriginIsRefused: asking to be local to
// nothing must not silently degrade to "anywhere".
func TestMachineAffinityLocalWithoutOriginIsRefused(t *testing.T) {
	task := types.ResolvedTask{MachineAffinity: types.MachineAffinityLocal}
	reason, ok := machineAffinitySatisfied(task, "machine-a")
	if ok {
		t.Fatal("local affinity with no origin machine was accepted")
	}
	if reason != reasonMachineAffinityUnresolved {
		t.Fatalf("reason = %q, want %q", reason, reasonMachineAffinityUnresolved)
	}
}

// TestMachineAffinityMismatchReasonIsReported: an unplaceable task must carry
// a reason, or it presents as an unexplained stall.
func TestMachineAffinityMismatchReasonIsReported(t *testing.T) {
	task := types.ResolvedTask{OriginMachineID: "machine-b", MachineAffinity: types.MachineAffinityLocal}
	reason, ok := machineAffinitySatisfied(task, "machine-a")
	if ok {
		t.Fatal("foreign machine was accepted under local affinity")
	}
	if reason != reasonMachineAffinityMismatch {
		t.Fatalf("reason = %q, want %q", reason, reasonMachineAffinityMismatch)
	}

	// And it must reach the caller-visible reasons list.
	runner := types.RunnerInfo{
		RunnerID: "runner-a", MachineID: "machine-a", Status: types.RunnerStatusOnline,
		DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 2,
	}
	svc := &SchedulerService{}
	_, reasons := svc.selectCandidate(task, "proj", []types.RunnerInfo{runner}, &types.ProjectPlacement{}, nil)
	joined := strings.Join(reasons, "; ")
	if !strings.Contains(joined, reasonMachineAffinityMismatch) {
		t.Fatalf("reasons = %q, want one mentioning %q", joined, reasonMachineAffinityMismatch)
	}
}

// TestScoreMachineOriginBonus pins the ordering claim: task-level origin
// affinity outranks the project-level preferred-machine bonus.
func TestScoreMachineOriginBonus(t *testing.T) {
	placement := &types.ProjectPlacement{PreferredMachines: []string{"machine-a"}}
	task := types.ResolvedTask{OriginMachineID: "machine-b"}

	projectPreferred := scoreMachine("machine-a", placement, task)
	taskOrigin := scoreMachine("machine-b", placement, task)
	if taskOrigin <= projectPreferred {
		t.Fatalf("origin machine scored %d, project-preferred scored %d; origin must win",
			taskOrigin, projectPreferred)
	}

	// A runner reporting no machine id gets a synthetic "runner:<id>" key.
	// An empty origin must never match it.
	noOrigin := types.ResolvedTask{}
	if scoreMachine("runner:x", placement, noOrigin) != scoreMachine("machine-z", placement, noOrigin) {
		t.Fatal("empty origin machine matched a synthetic machine id")
	}
}

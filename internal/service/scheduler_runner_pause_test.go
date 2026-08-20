package service

import (
	"context"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// The push-dispatch scheduler must treat a paused runner as ineligible.
//
// Before this, `PUT /runners/{runnerId}/pause` only fired a fire-and-forget
// SSE command — nothing server-side recorded the pause — so ScheduleProject
// kept minting dispatch leases for the paused runner on every tick. The
// runner then acked and spawned them, which is exactly what was observed
// against the demo API: the pause endpoint returned success while the
// headless runner logged "dispatch acked; spawning task" every few seconds.
func TestSchedulerSkipsPausedRunner(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{ID: "task-1", ProjectID: "demo", Status: "pending", Classification: "ready", Executor: "opencode"}}
	store.runners = []types.RunnerInfo{
		{RunnerID: "runner-paused", MachineID: "machine-a", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 2, Paused: true},
	}
	store.placement = types.ProjectPlacement{ProjectID: "demo", Affinity: types.PlacementAffinityNone}

	svc := NewSchedulerService(store, nil, store)
	result, err := svc.ScheduleProject(context.Background(), "demo")
	if err != nil {
		t.Fatalf("ScheduleProject failed: %v", err)
	}

	if result.Dispatched != 0 {
		t.Errorf("Dispatched = %d, want 0 (the only runner is paused)", result.Dispatched)
	}
	if len(store.leases) != 0 {
		t.Errorf("leases = %#v, want none created for a paused runner", store.leases)
	}
	if len(store.commands) != 0 {
		t.Errorf("commands = %#v, want no dispatch pushed to a paused runner", store.commands)
	}
}

// A pause on one runner must not stall the project: an unpaused peer still
// gets the work.
func TestSchedulerDispatchesToUnpausedPeer(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{ID: "task-1", ProjectID: "demo", Status: "pending", Classification: "ready", Executor: "opencode"}}
	store.runners = []types.RunnerInfo{
		{RunnerID: "runner-paused", MachineID: "machine-a", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 2, Paused: true},
		{RunnerID: "runner-live", MachineID: "machine-b", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 2},
	}
	store.placement = types.ProjectPlacement{ProjectID: "demo", Affinity: types.PlacementAffinityNone}

	svc := NewSchedulerService(store, nil, store)
	result, err := svc.ScheduleProject(context.Background(), "demo")
	if err != nil {
		t.Fatalf("ScheduleProject failed: %v", err)
	}

	if result.Dispatched != 1 {
		t.Fatalf("Dispatched = %d, want 1", result.Dispatched)
	}
	if len(store.leases) != 1 || store.leases[0].AssignedRunnerID != "runner-live" {
		t.Fatalf("lease assigned runner = %#v, want runner-live", store.leases)
	}
}

// RunTaskNow is the user-explicit override, but it must not resurrect a
// paused runner: selectCandidate shares runnerEligibleForTask, so a paused
// runner is simply not a candidate and the caller gets a clear reason.
func TestRunTaskNowSkipsPausedRunner(t *testing.T) {
	store := newFakeSchedulerStore()
	store.tasks = []types.ResolvedTask{{ID: "task-1", ProjectID: "demo", Status: "pending", Classification: "ready", Executor: "opencode"}}
	store.runners = []types.RunnerInfo{
		{RunnerID: "runner-paused", MachineID: "machine-a", Status: types.RunnerStatusOnline, DispatchPush: true, Executors: []string{"opencode"}, MaxParallel: 2, Paused: true},
	}
	store.placement = types.ProjectPlacement{ProjectID: "demo", Affinity: types.PlacementAffinityNone}

	svc := NewSchedulerService(store, nil, store)
	resp, err := svc.RunTaskNow(context.Background(), "demo", "task-1", false)
	if err != nil {
		t.Fatalf("RunTaskNow failed: %v", err)
	}

	if resp.Dispatched {
		t.Errorf("Dispatched = true, want false (the only runner is paused)")
	}
	if resp.Reason != "no_eligible_runner" {
		t.Errorf("Reason = %q, want no_eligible_runner", resp.Reason)
	}
}

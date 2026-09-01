package service

import (
	"context"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// insertRunnerWithMachine registers an online runner on a named machine, or on
// no machine at all when machineID is empty.
func insertRunnerWithMachine(t *testing.T, store *storage.StorageLayer, runnerID, machineID string) {
	t.Helper()
	now := time.Now().UnixMilli()
	if err := store.UpsertRunner(context.Background(), &storage.RunnerRow{
		RunnerID:      runnerID,
		MachineID:     machineID,
		Hostname:      runnerID + "-host",
		Labels:        map[string]string{},
		Executors:     []string{"opencode"},
		MaxParallel:   1,
		RegisteredAt:  now,
		LastHeartbeat: now,
		Status:        string(types.RunnerStatusOnline),
	}); err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}
}

// TestGetNext_LocalAffinityHidesTaskFromForeignMachine is the pull-path mirror
// of the push-path filter. Enforcing on only one path means a polling runner
// on another machine quietly picks up a pinned task.
func TestGetNext_LocalAffinityHidesTaskFromForeignMachine(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertRunnerWithMachine(t, store, "runner-foreign", "machine-b")
	insertTaskNote(t, store, "pinned11", "Pinned Task", "pending", "high", "proj", map[string]interface{}{
		"origin_machine_id": "machine-a",
		"machine_affinity":  types.MachineAffinityLocal,
	})

	next, err := svc.GetNext(ctx, "proj", &api.TaskFilterOptions{RunnerID: "runner-foreign"})
	if err != nil {
		t.Fatalf("GetNext failed: %v", err)
	}
	if next != nil {
		t.Fatalf("GetNext returned %q; a machine_affinity=local task must be invisible to a runner on another machine", next.ID)
	}
}

func TestGetNext_LocalAffinityVisibleOnOriginMachine(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertRunnerWithMachine(t, store, "runner-home", "machine-a")
	insertTaskNote(t, store, "pinned11", "Pinned Task", "pending", "high", "proj", map[string]interface{}{
		"origin_machine_id": "machine-a",
		"machine_affinity":  types.MachineAffinityLocal,
	})

	next, err := svc.GetNext(ctx, "proj", &api.TaskFilterOptions{RunnerID: "runner-home"})
	if err != nil {
		t.Fatalf("GetNext failed: %v", err)
	}
	if next == nil || next.ID != "pinned11" {
		t.Fatalf("GetNext = %v, want pinned11 on its own machine", next)
	}
}

// TestGetNext_PreferredAffinityDoesNotFilterOnPullPath: only "local" is a hard
// filter. A preferred task must remain claimable anywhere, or the default
// would silently strand work.
func TestGetNext_PreferredAffinityDoesNotFilterOnPullPath(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertRunnerWithMachine(t, store, "runner-foreign", "machine-b")
	insertTaskNote(t, store, "roaming1", "Roaming Task", "pending", "high", "proj", map[string]interface{}{
		"origin_machine_id": "machine-a",
		// Unset affinity resolves to "preferred".
	})

	next, err := svc.GetNext(ctx, "proj", &api.TaskFilterOptions{RunnerID: "runner-foreign"})
	if err != nil {
		t.Fatalf("GetNext failed: %v", err)
	}
	if next == nil || next.ID != "roaming1" {
		t.Fatalf("GetNext = %v, want roaming1 — preferred must not hard-filter", next)
	}
}

// TestGetNext_LocalAffinityUsesLegacyMachineLabel: runners registered before
// the machine_id column exists carry it in the legacy _machine_id label.
// Reading row.MachineID directly would make such a runner never match its own
// tasks.
func TestGetNext_LocalAffinityUsesLegacyMachineLabel(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	now := time.Now().UnixMilli()
	if err := store.UpsertRunner(ctx, &storage.RunnerRow{
		RunnerID:      "runner-legacy",
		Labels:        map[string]string{machineIDLabel: "machine-a"},
		Executors:     []string{"opencode"},
		MaxParallel:   1,
		RegisteredAt:  now,
		LastHeartbeat: now,
		Status:        string(types.RunnerStatusOnline),
	}); err != nil {
		t.Fatalf("UpsertRunner failed: %v", err)
	}
	insertTaskNote(t, store, "pinned11", "Pinned Task", "pending", "high", "proj", map[string]interface{}{
		"origin_machine_id": "machine-a",
		"machine_affinity":  types.MachineAffinityLocal,
	})

	next, err := svc.GetNext(ctx, "proj", &api.TaskFilterOptions{RunnerID: "runner-legacy"})
	if err != nil {
		t.Fatalf("GetNext failed: %v", err)
	}
	if next == nil || next.ID != "pinned11" {
		t.Fatalf("GetNext = %v, want pinned11 — legacy _machine_id label must be honored", next)
	}
}

// TestResolvedTask_CarriesOriginFields proves the metadata -> BrainEntry ->
// ResolvedTask hops both survive. A miss in either one leaves the fields on
// disk but absent from every consumer.
func TestResolvedTask_CarriesOriginFields(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	insertTaskNote(t, store, "origin11", "Origin Task", "pending", "high", "proj", map[string]interface{}{
		"origin_machine_id": "machine_a1b2",
		"origin_client_id":  "mcp-c3d4",
		"origin_path":       "/Users/huy/projects/brain-api",
		"machine_affinity":  types.MachineAffinityLocal,
	})

	resp, err := svc.GetTasks(ctx, "proj")
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	if len(resp.Tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(resp.Tasks))
	}
	task := resp.Tasks[0]
	if task.OriginMachineID != "machine_a1b2" {
		t.Errorf("OriginMachineID = %q, want machine_a1b2", task.OriginMachineID)
	}
	if task.OriginClientID != "mcp-c3d4" {
		t.Errorf("OriginClientID = %q, want mcp-c3d4", task.OriginClientID)
	}
	if task.OriginPath != "/Users/huy/projects/brain-api" {
		t.Errorf("OriginPath = %q, want /Users/huy/projects/brain-api", task.OriginPath)
	}
	if task.MachineAffinity != types.MachineAffinityLocal {
		t.Errorf("MachineAffinity = %q, want local", task.MachineAffinity)
	}
}

// TestGetNext_UnregisteredRunnerCannotTakePinnedTask closes a fail-open hole:
// when the requesting runner has no row, every other eligibility filter is
// skipped, and machine affinity must not inherit that default — an
// unregistered runner has no machine id, so it can never be the origin.
func TestGetNext_UnregisteredRunnerCannotTakePinnedTask(t *testing.T) {
	svc, store, _ := newTestTaskService(t)
	ctx := context.Background()

	// Deliberately do NOT register the runner.
	insertTaskNote(t, store, "pinned11", "Pinned Task", "pending", "high", "proj", map[string]interface{}{
		"origin_machine_id": "machine-a",
		"machine_affinity":  types.MachineAffinityLocal,
	})
	insertTaskNote(t, store, "roaming1", "Roaming Task", "pending", "low", "proj", map[string]interface{}{})

	next, err := svc.GetNext(ctx, "proj", &api.TaskFilterOptions{RunnerID: "runner-unknown"})
	if err != nil {
		t.Fatalf("GetNext failed: %v", err)
	}
	// The pinned task is higher priority, so serving it would be the bug.
	if next == nil || next.ID != "roaming1" {
		t.Fatalf("GetNext = %v, want roaming1 — an unregistered runner must not receive a pinned task", next)
	}
}

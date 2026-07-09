package runner

import (
	"sync"
	"sync/atomic"
	"testing"
)

// TestProcessManager_ReserveSlot_RespectsMaxParallel confirms
// ReserveSlot fails when the running-count-plus-reservations equals
// maxParallel, preventing multiple dispatch workers from all passing
// a stale RunningCount check and overspending the runner's slots.
func TestProcessManager_ReserveSlot_RespectsMaxParallel(t *testing.T) {
	pm := NewProcessManager(RunnerConfig{})

	if !pm.ReserveSlot("task-1", 3) {
		t.Fatal("first reservation should succeed")
	}
	if !pm.ReserveSlot("task-2", 3) {
		t.Fatal("second reservation should succeed")
	}
	if !pm.ReserveSlot("task-3", 3) {
		t.Fatal("third reservation should succeed")
	}
	if pm.ReserveSlot("task-4", 3) {
		t.Fatal("fourth reservation should fail (max=3)")
	}
}

// TestProcessManager_ReserveSlot_ReleasesFreeSlot confirms
// ReleaseReservation frees a slot so subsequent ReserveSlot calls can
// succeed. This is the failure-path cleanup: when a dispatch reserves
// a slot then rejects (task_lookup_failed, executor_unsupported, etc.),
// the slot must be returned or the runner leaks capacity.
func TestProcessManager_ReserveSlot_ReleasesFreeSlot(t *testing.T) {
	pm := NewProcessManager(RunnerConfig{})

	pm.ReserveSlot("task-1", 2)
	pm.ReserveSlot("task-2", 2)
	if pm.ReserveSlot("task-3", 2) {
		t.Fatal("third reservation should fail (max=2)")
	}

	pm.ReleaseReservation("task-1")

	if !pm.ReserveSlot("task-3", 2) {
		t.Fatal("third reservation should succeed after releasing task-1")
	}
}

// TestProcessManager_ReserveSlot_IdempotentForSameTask prevents a
// runaway duplicate-dispatch scenario from consuming multiple slots
// for the same task. If two SSE dispatches arrive for the same task,
// only one reservation should succeed; the second must return true
// (already-reserved is not an error) without incrementing the count.
func TestProcessManager_ReserveSlot_IdempotentForSameTask(t *testing.T) {
	pm := NewProcessManager(RunnerConfig{})

	if !pm.ReserveSlot("task-1", 1) {
		t.Fatal("first reservation should succeed")
	}
	// Same task again — must not consume an extra slot.
	if !pm.ReserveSlot("task-1", 1) {
		t.Fatal("re-reservation for same task should succeed (idempotent)")
	}
	// Different task — should fail because slot is taken.
	if pm.ReserveSlot("task-2", 1) {
		t.Fatal("different task should fail; task-1 is holding the only slot")
	}
}

// TestProcessManager_ReserveSlot_ConcurrentCapPreserved is the race
// test. Fires N goroutines each trying to reserve a slot; verifies
// that at most maxParallel reservations succeed even when they all
// race against each other. This is what the fix targets: multiple
// dispatch workers hitting the check simultaneously.
func TestProcessManager_ReserveSlot_ConcurrentCapPreserved(t *testing.T) {
	pm := NewProcessManager(RunnerConfig{})

	const maxParallel = 3
	const attempts = 20

	var succeeded atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start // release all at once for max contention
			taskID := "task-" + string(rune('a'+i))
			if pm.ReserveSlot(taskID, maxParallel) {
				succeeded.Add(1)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if got := int(succeeded.Load()); got != maxParallel {
		t.Errorf("succeeded reservations = %d, want %d (capacity should be strict under concurrency)", got, maxParallel)
	}
}

// TestProcessManager_GetAll_ExcludesReservations confirms that
// unspawned slot reservations never leak out of GetAll. A reservation
// carries a zero-value Task, and GetAll consumers (completion checks,
// claim renewal, lease release, state persistence) all assume a live
// process — leaking one caused the poll loop to treat the placeholder
// as a crashed task and call UpdateTaskStatus with an empty task path
// (API 404 "Entry not found: ") mid-dispatch.
func TestProcessManager_GetAll_ExcludesReservations(t *testing.T) {
	pm := NewProcessManager(RunnerConfig{})

	pm.ReserveSlot("task-1", 3)
	if got := len(pm.GetAll()); got != 0 {
		t.Errorf("GetAll returned %d entries for a bare reservation, want 0", got)
	}

	// After the spawn upgrades the reservation, it appears.
	if err := pm.Add("task-1", RunningTask{ID: "task-1"}, newMockProcess(1234)); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := len(pm.GetAll()); got != 1 {
		t.Errorf("GetAll returned %d entries after Add, want 1", got)
	}
}

// TestProcessManager_CheckCompletion_ReservationIsRunning confirms a
// reserved-but-unspawned task reads as still in flight, not crashed,
// and does not nil-panic on the placeholder's nil Proc.
func TestProcessManager_CheckCompletion_ReservationIsRunning(t *testing.T) {
	pm := NewProcessManager(RunnerConfig{})

	pm.ReserveSlot("task-1", 3)
	if got := pm.CheckCompletion("task-1", true); got != CompletionRunning {
		t.Errorf("CheckCompletion for reservation = %v, want %v", got, CompletionRunning)
	}
}

// TestProcessManager_ToProcessStates_SkipsReservations confirms state
// persistence doesn't nil-panic on a reservation's nil Proc and doesn't
// persist the placeholder.
func TestProcessManager_ToProcessStates_SkipsReservations(t *testing.T) {
	pm := NewProcessManager(RunnerConfig{})

	pm.ReserveSlot("task-1", 3)
	if err := pm.Add("task-2", RunningTask{ID: "task-2"}, newMockProcess(99)); err != nil {
		t.Fatalf("Add: %v", err)
	}

	states := pm.ToProcessStates()
	if len(states) != 1 {
		t.Fatalf("ToProcessStates returned %d states, want 1 (reservation skipped)", len(states))
	}
	if states[0].TaskID != "task-2" {
		t.Errorf("persisted TaskID = %q, want %q", states[0].TaskID, "task-2")
	}
}

// TestProcessManager_Add_UpgradesReservation confirms that when a
// reservation exists, Add attaches the real Process to the placeholder
// without double-counting. This is the "spawn succeeded" path: the
// reservation held the slot during executor/workdir resolution, and
// now Add promotes it to a live process.
func TestProcessManager_Add_UpgradesReservation(t *testing.T) {
	pm := NewProcessManager(RunnerConfig{})

	pm.ReserveSlot("task-1", 3)
	if pm.RunningCount() != 1 {
		t.Errorf("RunningCount after reserve = %d, want 1 (reservation counts)", pm.RunningCount())
	}

	proc := newMockProcess(1234)
	if err := pm.Add("task-1", RunningTask{ID: "task-1"}, proc); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Still 1 — the Add upgraded the placeholder, not created a duplicate.
	if pm.RunningCount() != 1 {
		t.Errorf("RunningCount after Add = %d, want 1 (upgrade, not double)", pm.RunningCount())
	}

	// Get returns the real process now.
	info := pm.Get("task-1")
	if info == nil || info.Proc == nil {
		t.Fatalf("Get returned nil after Add-upgrade")
	}
	if info.Proc.Pid() != 1234 {
		t.Errorf("Get.Proc.Pid = %d, want 1234", info.Proc.Pid())
	}
}

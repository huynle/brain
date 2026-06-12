package storage

import (
	"context"
	"testing"
)

func testInstance(id, runnerID string) *InstanceRow {
	return &InstanceRow{
		InstanceID: id,
		RunnerID:   runnerID,
		Hostname:   "host1",
		Kind:       "task",
		ProjectID:  "proj",
		TaskID:     "task-1",
		Title:      "Test task",
		Workdir:    "/tmp/work",
		Port:       4096,
		PID:        1234,
		SessionIDs: []string{"ses_abc"},
		Status:     "busy",
		Executor:   "opencode",
		StartedAt:  1000,
		LastSeen:   2000,
	}
}

func TestInstances_UpsertAndGet(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	inst := testInstance("inst_01", "runner_a")
	if err := s.UpsertInstance(ctx, inst); err != nil {
		t.Fatalf("UpsertInstance failed: %v", err)
	}

	got, err := s.GetInstance(ctx, "inst_01")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected instance, got nil")
	}
	if got.RunnerID != "runner_a" || got.Port != 4096 || got.Status != "busy" {
		t.Errorf("unexpected instance: %+v", got)
	}
	if len(got.SessionIDs) != 1 || got.SessionIDs[0] != "ses_abc" {
		t.Errorf("unexpected session ids: %v", got.SessionIDs)
	}

	// Upsert again with changed status — should replace, not duplicate.
	inst.Status = "idle"
	inst.Port = 5000
	if err := s.UpsertInstance(ctx, inst); err != nil {
		t.Fatalf("second UpsertInstance failed: %v", err)
	}
	got, _ = s.GetInstance(ctx, "inst_01")
	if got.Status != "idle" || got.Port != 5000 {
		t.Errorf("upsert did not update: %+v", got)
	}

	all, err := s.ListAllInstances(ctx)
	if err != nil {
		t.Fatalf("ListAllInstances failed: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("expected 1 instance, got %d", len(all))
	}
}

func TestInstances_GetMissing(t *testing.T) {
	s := newTestStorage(t)

	got, err := s.GetInstance(context.Background(), "inst_missing")
	if err != nil {
		t.Fatalf("GetInstance failed: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for missing instance, got %+v", got)
	}
}

func TestInstances_ListByRunnerAndDelete(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	s.UpsertInstance(ctx, testInstance("inst_01", "runner_a"))
	s.UpsertInstance(ctx, testInstance("inst_02", "runner_a"))
	s.UpsertInstance(ctx, testInstance("inst_03", "runner_b"))

	listA, err := s.ListInstancesByRunner(ctx, "runner_a")
	if err != nil {
		t.Fatalf("ListInstancesByRunner failed: %v", err)
	}
	if len(listA) != 2 {
		t.Errorf("expected 2 instances for runner_a, got %d", len(listA))
	}

	// Deleting with the wrong runner scope must not remove the row.
	deleted, err := s.DeleteInstance(ctx, "runner_b", "inst_01")
	if err != nil {
		t.Fatalf("DeleteInstance failed: %v", err)
	}
	if deleted {
		t.Error("expected delete with wrong runner scope to be a no-op")
	}

	deleted, err = s.DeleteInstance(ctx, "runner_a", "inst_01")
	if err != nil {
		t.Fatalf("DeleteInstance failed: %v", err)
	}
	if !deleted {
		t.Error("expected delete to remove row")
	}

	listA, _ = s.ListInstancesByRunner(ctx, "runner_a")
	if len(listA) != 1 {
		t.Errorf("expected 1 instance after delete, got %d", len(listA))
	}
}

func TestInstances_DeleteByRunner(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	s.UpsertInstance(ctx, testInstance("inst_01", "runner_a"))
	s.UpsertInstance(ctx, testInstance("inst_02", "runner_a"))
	s.UpsertInstance(ctx, testInstance("inst_03", "runner_b"))

	n, err := s.DeleteInstancesByRunner(ctx, "runner_a")
	if err != nil {
		t.Fatalf("DeleteInstancesByRunner failed: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 deleted, got %d", n)
	}

	all, _ := s.ListAllInstances(ctx)
	if len(all) != 1 || all[0].InstanceID != "inst_03" {
		t.Errorf("unexpected remaining instances: %+v", all)
	}
}

func TestInstances_ReplaceForRunner(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	s.UpsertInstance(ctx, testInstance("inst_01", "runner_a"))
	s.UpsertInstance(ctx, testInstance("inst_02", "runner_a"))
	s.UpsertInstance(ctx, testInstance("inst_other", "runner_b"))

	// Reconcile runner_a to a different set: inst_02 (updated) + inst_04 (new).
	updated := *testInstance("inst_02", "runner_a")
	updated.Status = "idle"
	fresh := *testInstance("inst_04", "runner_a")
	if err := s.ReplaceInstancesForRunner(ctx, "runner_a", []InstanceRow{updated, fresh}); err != nil {
		t.Fatalf("ReplaceInstancesForRunner failed: %v", err)
	}

	listA, _ := s.ListInstancesByRunner(ctx, "runner_a")
	if len(listA) != 2 {
		t.Fatalf("expected 2 instances after replace, got %d", len(listA))
	}
	ids := map[string]string{}
	for _, inst := range listA {
		ids[inst.InstanceID] = inst.Status
	}
	if _, exists := ids["inst_01"]; exists {
		t.Error("inst_01 should have been removed by reconcile")
	}
	if ids["inst_02"] != "idle" {
		t.Errorf("inst_02 should be updated to idle, got %q", ids["inst_02"])
	}
	if _, exists := ids["inst_04"]; !exists {
		t.Error("inst_04 should have been inserted by reconcile")
	}

	// Other runners are untouched.
	listB, _ := s.ListInstancesByRunner(ctx, "runner_b")
	if len(listB) != 1 {
		t.Errorf("runner_b instances affected by reconcile: %+v", listB)
	}

	// Replace with empty set clears all rows.
	if err := s.ReplaceInstancesForRunner(ctx, "runner_a", nil); err != nil {
		t.Fatalf("ReplaceInstancesForRunner(empty) failed: %v", err)
	}
	listA, _ = s.ListInstancesByRunner(ctx, "runner_a")
	if len(listA) != 0 {
		t.Errorf("expected 0 instances after empty reconcile, got %d", len(listA))
	}
}

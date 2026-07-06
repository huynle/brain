package storage

import (
	"context"
	"testing"
)

// TestListPlacementReasonRows_LimitCapsResults confirms that when the
// caller supplies a positive limit, the query returns at most that many
// rows, chosen from the most recent placement decisions.
//
// Uses insertPlacementReasonRaw to bypass RecordPlacementReason's
// auto-prune so the test can exercise `limit` against >20 rows.
func TestListPlacementReasonRows_LimitCapsResults(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert 30 placement decisions for the same task, ascending created_at.
	for i := 1; i <= 30; i++ {
		if err := insertPlacementReasonRaw(ctx, s, &PlacementReasonRow{
			ProjectID: "proj",
			TaskID:    "task-1",
			Decision:  "no_candidate",
			Reason:    "at capacity",
			CreatedAt: int64(i),
		}); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	// Request the 10 most recent.
	rows, err := s.ListPlacementReasonRowsLimit(ctx, "proj", "task-1", 10)
	if err != nil {
		t.Fatalf("ListPlacementReasonRowsLimit: %v", err)
	}
	if len(rows) != 10 {
		t.Fatalf("got %d rows, want 10", len(rows))
	}
	// Should be the newest 10 (created_at 21..30) in ascending order.
	if rows[0].CreatedAt != 21 || rows[9].CreatedAt != 30 {
		t.Errorf("returned rows created_at range [%d..%d], want [21..30]", rows[0].CreatedAt, rows[9].CreatedAt)
	}
}

// TestListPlacementReasonRows_ZeroLimitReturnsAll preserves the
// existing unbounded behavior when limit <= 0 so callers that legitimately
// want the full history (e.g. task_placement_reasons() diagnostic tool)
// aren't broken.
func TestListPlacementReasonRows_ZeroLimitReturnsAll(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		if err := s.RecordPlacementReason(ctx, &PlacementReasonRow{
			ProjectID: "proj",
			TaskID:    "task-1",
			Decision:  "no_candidate",
			CreatedAt: int64(i),
		}); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	rows, err := s.ListPlacementReasonRowsLimit(ctx, "proj", "task-1", 0)
	if err != nil {
		t.Fatalf("ListPlacementReasonRowsLimit: %v", err)
	}
	if len(rows) != 5 {
		t.Fatalf("got %d rows, want 5 (all)", len(rows))
	}
}

// TestPrunePlacementReasonsForTask keeps the most recent N rows per
// task and deletes the rest.
//
// Note: RecordPlacementReason auto-prunes to PlacementReasonRetention
// (currently 20). To exercise PrunePlacementReasonsForTask against a
// larger row set, we insert directly via test-only helper
// insertPlacementReasonRaw to bypass the opportunistic prune.
func TestPrunePlacementReasonsForTask(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// 50 rows for task-1, 3 rows for task-2 (should be untouched).
	for i := 1; i <= 50; i++ {
		if err := insertPlacementReasonRaw(ctx, s, &PlacementReasonRow{
			ProjectID: "proj", TaskID: "task-1",
			Decision: "x", CreatedAt: int64(i),
		}); err != nil {
			t.Fatalf("record task-1 %d: %v", i, err)
		}
	}
	for i := 100; i <= 102; i++ {
		if err := insertPlacementReasonRaw(ctx, s, &PlacementReasonRow{
			ProjectID: "proj", TaskID: "task-2",
			Decision: "x", CreatedAt: int64(i),
		}); err != nil {
			t.Fatalf("record task-2 %d: %v", i, err)
		}
	}

	// Keep only the last 20 for task-1.
	deleted, err := s.PrunePlacementReasonsForTask(ctx, "proj", "task-1", 20)
	if err != nil {
		t.Fatalf("PrunePlacementReasonsForTask: %v", err)
	}
	if deleted != 30 {
		t.Errorf("deleted = %d, want 30", deleted)
	}

	remaining, err := s.ListPlacementReasonRowsLimit(ctx, "proj", "task-1", 0)
	if err != nil {
		t.Fatalf("list after prune: %v", err)
	}
	if len(remaining) != 20 {
		t.Errorf("remaining = %d, want 20", len(remaining))
	}
	// Should be the 20 most recent (31..50).
	if remaining[0].CreatedAt != 31 || remaining[19].CreatedAt != 50 {
		t.Errorf("kept created_at range [%d..%d], want [31..50]",
			remaining[0].CreatedAt, remaining[19].CreatedAt)
	}

	// task-2 rows untouched.
	other, err := s.ListPlacementReasonRowsLimit(ctx, "proj", "task-2", 0)
	if err != nil {
		t.Fatalf("list task-2: %v", err)
	}
	if len(other) != 3 {
		t.Errorf("task-2 rows = %d, want 3 (pruning scope leaked)", len(other))
	}
}

// TestPrunePlacementReasonsForTask_NoOpWhenBelowLimit confirms the
// pruner is a no-op when a task has fewer rows than the retention
// cap. Prevents extra writes on the common path.
func TestPrunePlacementReasonsForTask_NoOpWhenBelowLimit(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	for i := 1; i <= 5; i++ {
		if err := s.RecordPlacementReason(ctx, &PlacementReasonRow{
			ProjectID: "proj", TaskID: "task-1",
			Decision: "x", CreatedAt: int64(i),
		}); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	deleted, err := s.PrunePlacementReasonsForTask(ctx, "proj", "task-1", 20)
	if err != nil {
		t.Fatalf("PrunePlacementReasonsForTask: %v", err)
	}
	if deleted != 0 {
		t.Errorf("deleted = %d, want 0 (no-op)", deleted)
	}
}

// insertPlacementReasonRaw performs a raw INSERT without the
// opportunistic prune that RecordPlacementReason does. Test-only helper
// so we can build up row sets larger than PlacementReasonRetention to
// exercise the pruner directly.
func insertPlacementReasonRaw(ctx context.Context, s *StorageLayer, row *PlacementReasonRow) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO task_placement_reasons
		  (project_id, task_id, runner_id, machine_id, decision, reason, required_labels, runner_labels, missing_labels, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ProjectID, row.TaskID, row.RunnerID, row.MachineID, row.Decision, row.Reason,
		row.RequiredLabels, row.RunnerLabels, row.MissingLabels, row.CreatedAt,
	)
	return err
}

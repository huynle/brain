package storage

import (
	"context"
	"testing"
)

// TestRecordPlacementReason_KeepsPerTaskHistoryBounded confirms that
// repeated RecordPlacementReason calls for the same task keep the
// on-disk row count bounded via opportunistic pruning. This is the
// production wedge fix: previously task_placement_reasons grew to 894k
// rows because every failed placement decision was appended forever.
func TestRecordPlacementReason_KeepsPerTaskHistoryBounded(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert 100 placement decisions for one task.
	for i := 1; i <= 100; i++ {
		if err := s.RecordPlacementReason(ctx, &PlacementReasonRow{
			ProjectID: "proj", TaskID: "task-1",
			Decision: "no_candidate", Reason: "at capacity",
			CreatedAt: int64(i),
		}); err != nil {
			t.Fatalf("record %d: %v", i, err)
		}
	}

	rows, err := s.ListPlacementReasonRowsLimit(ctx, "proj", "task-1", 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Retention cap is PlacementReasonRetention (defined in the impl).
	// Anything more than the cap indicates unbounded growth.
	if len(rows) > PlacementReasonRetention {
		t.Fatalf("stored %d rows after 100 inserts, want at most %d (unbounded growth)",
			len(rows), PlacementReasonRetention)
	}
	// The retained rows should be the most recent ones.
	last := rows[len(rows)-1]
	if last.CreatedAt != 100 {
		t.Errorf("newest retained created_at = %d, want 100", last.CreatedAt)
	}
}

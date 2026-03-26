package storage

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// GetStats — basic counts
// ---------------------------------------------------------------------------

func TestGetStats_BasicCounts(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert 3 notes.
	_, err := s.InsertNote(ctx, sampleNote("projects/test/stats/a.md", "stsa0001", "Note A"))
	if err != nil {
		t.Fatalf("insert A: %v", err)
	}
	_, err = s.InsertNote(ctx, sampleNote("projects/test/stats/b.md", "stsb0002", "Note B"))
	if err != nil {
		t.Fatalf("insert B: %v", err)
	}
	_, err = s.InsertNote(ctx, sampleNote("projects/test/stats/c.md", "stsc0003", "Note C"))
	if err != nil {
		t.Fatalf("insert C: %v", err)
	}

	stats, err := s.GetStats(ctx, nil)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if stats.TotalNotes != 3 {
		t.Errorf("TotalNotes = %d, want 3", stats.TotalNotes)
	}
}

// ---------------------------------------------------------------------------
// GetStats — by-type grouping
// ---------------------------------------------------------------------------

func TestGetStats_ByType(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert notes with different types.
	noteA := sampleNote("projects/test/stats/plan.md", "stsp0001", "Plan")
	planType := "plan"
	noteA.Type = &planType
	_, err := s.InsertNote(ctx, noteA)
	if err != nil {
		t.Fatalf("insert plan: %v", err)
	}

	noteB := sampleNote("projects/test/stats/idea1.md", "stsi0001", "Idea 1")
	ideaType := "idea"
	noteB.Type = &ideaType
	_, err = s.InsertNote(ctx, noteB)
	if err != nil {
		t.Fatalf("insert idea1: %v", err)
	}

	noteC := sampleNote("projects/test/stats/idea2.md", "stsi0002", "Idea 2")
	noteC.Type = &ideaType
	_, err = s.InsertNote(ctx, noteC)
	if err != nil {
		t.Fatalf("insert idea2: %v", err)
	}

	// Insert a note with nil type.
	noteD := sampleNote("projects/test/stats/untyped.md", "stsu0001", "Untyped")
	noteD.Type = nil
	_, err = s.InsertNote(ctx, noteD)
	if err != nil {
		t.Fatalf("insert untyped: %v", err)
	}

	stats, err := s.GetStats(ctx, nil)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.ByType["plan"] != 1 {
		t.Errorf("ByType[plan] = %d, want 1", stats.ByType["plan"])
	}
	if stats.ByType["idea"] != 2 {
		t.Errorf("ByType[idea] = %d, want 2", stats.ByType["idea"])
	}
	if stats.ByType["untyped"] != 1 {
		t.Errorf("ByType[untyped] = %d, want 1", stats.ByType["untyped"])
	}
}

// ---------------------------------------------------------------------------
// GetStats — orphan detection
// ---------------------------------------------------------------------------

func TestGetStats_OrphanCount(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create 3 notes: A → B (link), C is orphan.
	a, err := s.InsertNote(ctx, sampleNote("projects/test/stats/linked-a.md", "stla0001", "A"))
	if err != nil {
		t.Fatalf("insert A: %v", err)
	}
	_, err = s.InsertNote(ctx, sampleNote("projects/test/stats/linked-b.md", "stlb0001", "B"))
	if err != nil {
		t.Fatalf("insert B: %v", err)
	}
	_, err = s.InsertNote(ctx, sampleNote("projects/test/stats/orphan-c.md", "stlc0001", "C"))
	if err != nil {
		t.Fatalf("insert C: %v", err)
	}

	// A → B link.
	err = s.SetLinks(ctx, a.Path, []LinkInput{
		{TargetPath: "projects/test/stats/linked-b.md", Href: "projects/test/stats/linked-b.md", Title: "B"},
	})
	if err != nil {
		t.Fatalf("set links: %v", err)
	}

	stats, err := s.GetStats(ctx, nil)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	// A and C are orphans (no incoming links). B has incoming from A.
	if stats.OrphanCount != 2 {
		t.Errorf("OrphanCount = %d, want 2", stats.OrphanCount)
	}
}

// ---------------------------------------------------------------------------
// GetStats — tracked count
// ---------------------------------------------------------------------------

func TestGetStats_TrackedCount(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert 3 notes, track 2 of them.
	_, err := s.InsertNote(ctx, sampleNote("projects/test/stats/t1.md", "stst0001", "T1"))
	if err != nil {
		t.Fatalf("insert T1: %v", err)
	}
	_, err = s.InsertNote(ctx, sampleNote("projects/test/stats/t2.md", "stst0002", "T2"))
	if err != nil {
		t.Fatalf("insert T2: %v", err)
	}
	_, err = s.InsertNote(ctx, sampleNote("projects/test/stats/t3.md", "stst0003", "T3"))
	if err != nil {
		t.Fatalf("insert T3: %v", err)
	}

	// Record access for 2 notes (creates entry_meta rows).
	if err := s.RecordAccess(ctx, "projects/test/stats/t1.md"); err != nil {
		t.Fatalf("RecordAccess T1: %v", err)
	}
	if err := s.RecordAccess(ctx, "projects/test/stats/t2.md"); err != nil {
		t.Fatalf("RecordAccess T2: %v", err)
	}

	stats, err := s.GetStats(ctx, nil)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if stats.TrackedCount != 2 {
		t.Errorf("TrackedCount = %d, want 2", stats.TrackedCount)
	}
}

// ---------------------------------------------------------------------------
// GetStats — stale count
// ---------------------------------------------------------------------------

func TestGetStats_StaleCount(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert 3 notes: one never verified, one verified long ago, one recently verified.
	_, err := s.InsertNote(ctx, sampleNote("projects/test/stats/never.md", "stsn0001", "Never Verified"))
	if err != nil {
		t.Fatalf("insert never: %v", err)
	}
	_, err = s.InsertNote(ctx, sampleNote("projects/test/stats/old.md", "stso0001", "Old Verified"))
	if err != nil {
		t.Fatalf("insert old: %v", err)
	}
	_, err = s.InsertNote(ctx, sampleNote("projects/test/stats/fresh.md", "stsf0001", "Fresh"))
	if err != nil {
		t.Fatalf("insert fresh: %v", err)
	}

	// Old verified: 60 days ago.
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO entry_meta (path, last_verified)
		VALUES (?, datetime('now', '-60 days'))
	`, "projects/test/stats/old.md")
	if err != nil {
		t.Fatalf("insert old entry_meta: %v", err)
	}

	// Fresh: verified now.
	if err := s.SetVerified(ctx, "projects/test/stats/fresh.md"); err != nil {
		t.Fatalf("SetVerified fresh: %v", err)
	}

	stats, err := s.GetStats(ctx, nil)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	// "never" and "old" are stale. "fresh" is not.
	if stats.StaleCount != 2 {
		t.Errorf("StaleCount = %d, want 2", stats.StaleCount)
	}
}

// ---------------------------------------------------------------------------
// GetStats — path prefix filter
// ---------------------------------------------------------------------------

func TestGetStats_PathPrefixFilter(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert notes in different paths.
	_, err := s.InsertNote(ctx, sampleNote("projects/alpha/note1.md", "alp10001", "Alpha 1"))
	if err != nil {
		t.Fatalf("insert alpha1: %v", err)
	}
	_, err = s.InsertNote(ctx, sampleNote("projects/alpha/note2.md", "alp20001", "Alpha 2"))
	if err != nil {
		t.Fatalf("insert alpha2: %v", err)
	}
	_, err = s.InsertNote(ctx, sampleNote("projects/beta/note1.md", "bet10001", "Beta 1"))
	if err != nil {
		t.Fatalf("insert beta1: %v", err)
	}

	// Track one alpha note.
	if err := s.RecordAccess(ctx, "projects/alpha/note1.md"); err != nil {
		t.Fatalf("RecordAccess alpha1: %v", err)
	}

	stats, err := s.GetStats(ctx, &StatsOptions{Path: "projects/alpha/"})
	if err != nil {
		t.Fatalf("GetStats(alpha) failed: %v", err)
	}

	if stats.TotalNotes != 2 {
		t.Errorf("TotalNotes = %d, want 2", stats.TotalNotes)
	}
	if stats.TrackedCount != 1 {
		t.Errorf("TrackedCount = %d, want 1", stats.TrackedCount)
	}
}

// ---------------------------------------------------------------------------
// GetStats — empty database
// ---------------------------------------------------------------------------

func TestGetStats_EmptyDB(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	stats, err := s.GetStats(ctx, nil)
	if err != nil {
		t.Fatalf("GetStats on empty DB failed: %v", err)
	}
	if stats.TotalNotes != 0 {
		t.Errorf("TotalNotes = %d, want 0", stats.TotalNotes)
	}
	if len(stats.ByType) != 0 {
		t.Errorf("ByType should be empty, got %v", stats.ByType)
	}
	if stats.OrphanCount != 0 {
		t.Errorf("OrphanCount = %d, want 0", stats.OrphanCount)
	}
	if stats.TrackedCount != 0 {
		t.Errorf("TrackedCount = %d, want 0", stats.TrackedCount)
	}
	if stats.StaleCount != 0 {
		t.Errorf("StaleCount = %d, want 0", stats.StaleCount)
	}
}

// ---------------------------------------------------------------------------
// GetStats — nil options (same as empty options)
// ---------------------------------------------------------------------------

func TestGetStats_NilOptions(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	_, err := s.InsertNote(ctx, sampleNote("projects/test/stats/nil-opt.md", "stno0001", "Nil Opt"))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	stats, err := s.GetStats(ctx, nil)
	if err != nil {
		t.Fatalf("GetStats(nil) failed: %v", err)
	}
	if stats.TotalNotes != 1 {
		t.Errorf("TotalNotes = %d, want 1", stats.TotalNotes)
	}
}

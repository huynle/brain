package storage

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// RecordAccess
// ---------------------------------------------------------------------------

func TestRecordAccess_FirstAccess(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	err := s.RecordAccess(ctx, "projects/test/note.md")
	if err != nil {
		t.Fatalf("RecordAccess failed: %v", err)
	}

	// Verify the entry was created with access_count=1.
	meta, err := s.GetAccessStats(ctx, "projects/test/note.md")
	if err != nil {
		t.Fatalf("GetAccessStats failed: %v", err)
	}
	if meta == nil {
		t.Fatal("expected entry_meta row, got nil")
	}
	if meta.AccessCount != 1 {
		t.Errorf("AccessCount = %d, want 1", meta.AccessCount)
	}
	if meta.LastAccessed == nil {
		t.Error("LastAccessed should not be nil after RecordAccess")
	}
	if meta.Path != "projects/test/note.md" {
		t.Errorf("Path = %q, want %q", meta.Path, "projects/test/note.md")
	}
}

func TestRecordAccess_Increment(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	path := "projects/test/note.md"

	// First access.
	if err := s.RecordAccess(ctx, path); err != nil {
		t.Fatalf("first RecordAccess failed: %v", err)
	}
	// Second access.
	if err := s.RecordAccess(ctx, path); err != nil {
		t.Fatalf("second RecordAccess failed: %v", err)
	}

	meta, err := s.GetAccessStats(ctx, path)
	if err != nil {
		t.Fatalf("GetAccessStats failed: %v", err)
	}
	if meta == nil {
		t.Fatal("expected entry_meta row, got nil")
	}
	if meta.AccessCount != 2 {
		t.Errorf("AccessCount = %d, want 2", meta.AccessCount)
	}
}

func TestRecordAccess_MultiplePaths(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	pathA := "projects/test/a.md"
	pathB := "projects/test/b.md"

	if err := s.RecordAccess(ctx, pathA); err != nil {
		t.Fatalf("RecordAccess(A) failed: %v", err)
	}
	if err := s.RecordAccess(ctx, pathB); err != nil {
		t.Fatalf("RecordAccess(B) failed: %v", err)
	}
	// Access A again.
	if err := s.RecordAccess(ctx, pathA); err != nil {
		t.Fatalf("RecordAccess(A) second failed: %v", err)
	}

	metaA, err := s.GetAccessStats(ctx, pathA)
	if err != nil {
		t.Fatalf("GetAccessStats(A) failed: %v", err)
	}
	if metaA.AccessCount != 2 {
		t.Errorf("A.AccessCount = %d, want 2", metaA.AccessCount)
	}

	metaB, err := s.GetAccessStats(ctx, pathB)
	if err != nil {
		t.Fatalf("GetAccessStats(B) failed: %v", err)
	}
	if metaB.AccessCount != 1 {
		t.Errorf("B.AccessCount = %d, want 1", metaB.AccessCount)
	}
}

// ---------------------------------------------------------------------------
// GetAccessStats
// ---------------------------------------------------------------------------

func TestGetAccessStats_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	meta, err := s.GetAccessStats(ctx, "nonexistent/path.md")
	if err != nil {
		t.Fatalf("GetAccessStats failed: %v", err)
	}
	if meta != nil {
		t.Errorf("expected nil for nonexistent path, got %+v", meta)
	}
}

// ---------------------------------------------------------------------------
// SetVerified
// ---------------------------------------------------------------------------

func TestSetVerified_FirstTime(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	path := "projects/test/note.md"
	err := s.SetVerified(ctx, path)
	if err != nil {
		t.Fatalf("SetVerified failed: %v", err)
	}

	meta, err := s.GetAccessStats(ctx, path)
	if err != nil {
		t.Fatalf("GetAccessStats failed: %v", err)
	}
	if meta == nil {
		t.Fatal("expected entry_meta row, got nil")
	}
	if meta.LastVerified == nil {
		t.Error("LastVerified should not be nil after SetVerified")
	}
}

func TestSetVerified_UpdateExisting(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	path := "projects/test/note.md"

	// Record access first (creates the entry_meta row).
	if err := s.RecordAccess(ctx, path); err != nil {
		t.Fatalf("RecordAccess failed: %v", err)
	}

	// Now verify.
	if err := s.SetVerified(ctx, path); err != nil {
		t.Fatalf("SetVerified failed: %v", err)
	}

	meta, err := s.GetAccessStats(ctx, path)
	if err != nil {
		t.Fatalf("GetAccessStats failed: %v", err)
	}
	if meta == nil {
		t.Fatal("expected entry_meta row, got nil")
	}
	// Access count should still be 1 (SetVerified should not reset it).
	if meta.AccessCount != 1 {
		t.Errorf("AccessCount = %d, want 1 (SetVerified should not reset)", meta.AccessCount)
	}
	if meta.LastVerified == nil {
		t.Error("LastVerified should not be nil after SetVerified")
	}
}

// ---------------------------------------------------------------------------
// GetStaleEntries
// ---------------------------------------------------------------------------

func TestGetStaleEntries_FindsUnverified(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert a note that has never been verified.
	_, err := s.InsertNote(ctx, sampleNote("projects/test/stale/a.md", "stla0001", "Stale A"))
	if err != nil {
		t.Fatalf("insert note: %v", err)
	}

	got, err := s.GetStaleEntries(ctx, 30, nil)
	if err != nil {
		t.Fatalf("GetStaleEntries failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetStaleEntries: got %d notes, want 1", len(got))
	}
	if got[0].Path != "projects/test/stale/a.md" {
		t.Errorf("Path = %q, want %q", got[0].Path, "projects/test/stale/a.md")
	}
}

func TestGetStaleEntries_FindsOldVerified(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert a note.
	_, err := s.InsertNote(ctx, sampleNote("projects/test/stale/old.md", "stlo0001", "Old Verified"))
	if err != nil {
		t.Fatalf("insert note: %v", err)
	}

	// Set verified to 60 days ago.
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO entry_meta (path, last_verified)
		VALUES (?, datetime('now', '-60 days'))
	`, "projects/test/stale/old.md")
	if err != nil {
		t.Fatalf("insert entry_meta: %v", err)
	}

	got, err := s.GetStaleEntries(ctx, 30, nil)
	if err != nil {
		t.Fatalf("GetStaleEntries failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetStaleEntries: got %d notes, want 1", len(got))
	}
	if got[0].Path != "projects/test/stale/old.md" {
		t.Errorf("Path = %q, want %q", got[0].Path, "projects/test/stale/old.md")
	}
}

func TestGetStaleEntries_ExcludesRecentlyVerified(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert a note.
	_, err := s.InsertNote(ctx, sampleNote("projects/test/stale/fresh.md", "stlf0001", "Fresh"))
	if err != nil {
		t.Fatalf("insert note: %v", err)
	}

	// Set verified to now (fresh).
	if err := s.SetVerified(ctx, "projects/test/stale/fresh.md"); err != nil {
		t.Fatalf("SetVerified failed: %v", err)
	}

	got, err := s.GetStaleEntries(ctx, 30, nil)
	if err != nil {
		t.Fatalf("GetStaleEntries failed: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("GetStaleEntries: got %d notes, want 0 (recently verified)", len(got))
	}
}

func TestGetStaleEntries_TypeFilter(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert two notes with different types, both unverified.
	noteA := sampleNote("projects/test/stale/plan.md", "stlp0001", "Plan Note")
	planType := "plan"
	noteA.Type = &planType
	_, err := s.InsertNote(ctx, noteA)
	if err != nil {
		t.Fatalf("insert plan note: %v", err)
	}

	noteB := sampleNote("projects/test/stale/idea.md", "stli0001", "Idea Note")
	ideaType := "idea"
	noteB.Type = &ideaType
	_, err = s.InsertNote(ctx, noteB)
	if err != nil {
		t.Fatalf("insert idea note: %v", err)
	}

	// Filter by type "idea".
	got, err := s.GetStaleEntries(ctx, 30, &StaleOptions{Type: "idea"})
	if err != nil {
		t.Fatalf("GetStaleEntries(type=idea) failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("GetStaleEntries(type=idea): got %d notes, want 1", len(got))
	}
	if got[0].Path != "projects/test/stale/idea.md" {
		t.Errorf("Path = %q, want %q", got[0].Path, "projects/test/stale/idea.md")
	}
}

func TestGetStaleEntries_Limit(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert 3 unverified notes.
	for i, name := range []string{"a", "b", "c"} {
		note := sampleNote("projects/test/stale/"+name+".md", "stl"+name+"000"+string(rune('1'+i)), "Note "+name)
		_, err := s.InsertNote(ctx, note)
		if err != nil {
			t.Fatalf("insert note %s: %v", name, err)
		}
	}

	got, err := s.GetStaleEntries(ctx, 30, &StaleOptions{Limit: 2})
	if err != nil {
		t.Fatalf("GetStaleEntries(limit=2) failed: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("GetStaleEntries(limit=2): got %d notes, want 2", len(got))
	}
}

func TestGetStaleEntries_DefaultLimit(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// With nil options, default limit should be 50.
	got, err := s.GetStaleEntries(ctx, 30, nil)
	if err != nil {
		t.Fatalf("GetStaleEntries(nil) failed: %v", err)
	}
	// Empty DB — should return empty slice.
	if got == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("GetStaleEntries on empty DB: got %d notes, want 0", len(got))
	}
}

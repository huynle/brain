package storage

import (
	"context"
	"sort"
	"testing"
)

// ---------------------------------------------------------------------------
// SetTags
// ---------------------------------------------------------------------------

func TestSetTags_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Insert a note to attach tags to.
	note := sampleNote("projects/test/plan/tagged.md", "tag12345", "Tagged Note")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	// Set tags.
	err = s.SetTags(ctx, "projects/test/plan/tagged.md", []string{"go", "tdd", "storage"})
	if err != nil {
		t.Fatalf("SetTags failed: %v", err)
	}

	// Verify tags were stored.
	tags, err := s.GetTags(ctx, "projects/test/plan/tagged.md")
	if err != nil {
		t.Fatalf("GetTags failed: %v", err)
	}
	sort.Strings(tags)
	want := []string{"go", "storage", "tdd"}
	if len(tags) != len(want) {
		t.Fatalf("got %d tags, want %d", len(tags), len(want))
	}
	for i, tag := range tags {
		if tag != want[i] {
			t.Errorf("tag[%d] = %q, want %q", i, tag, want[i])
		}
	}
}

func TestSetTags_ReplacesExisting(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/replace-tags.md", "rep12345", "Replace Tags")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	// Set initial tags.
	err = s.SetTags(ctx, "projects/test/plan/replace-tags.md", []string{"old1", "old2"})
	if err != nil {
		t.Fatalf("SetTags (initial) failed: %v", err)
	}

	// Replace with new tags.
	err = s.SetTags(ctx, "projects/test/plan/replace-tags.md", []string{"new1", "new2", "new3"})
	if err != nil {
		t.Fatalf("SetTags (replace) failed: %v", err)
	}

	tags, err := s.GetTags(ctx, "projects/test/plan/replace-tags.md")
	if err != nil {
		t.Fatalf("GetTags failed: %v", err)
	}
	sort.Strings(tags)
	want := []string{"new1", "new2", "new3"}
	if len(tags) != len(want) {
		t.Fatalf("got %d tags, want %d: %v", len(tags), len(want), tags)
	}
	for i, tag := range tags {
		if tag != want[i] {
			t.Errorf("tag[%d] = %q, want %q", i, tag, want[i])
		}
	}
}

func TestSetTags_ClearWithEmptySlice(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/clear-tags.md", "clr12345", "Clear Tags")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	// Set some tags first.
	err = s.SetTags(ctx, "projects/test/plan/clear-tags.md", []string{"a", "b"})
	if err != nil {
		t.Fatalf("SetTags failed: %v", err)
	}

	// Clear with empty slice.
	err = s.SetTags(ctx, "projects/test/plan/clear-tags.md", []string{})
	if err != nil {
		t.Fatalf("SetTags (clear) failed: %v", err)
	}

	tags, err := s.GetTags(ctx, "projects/test/plan/clear-tags.md")
	if err != nil {
		t.Fatalf("GetTags failed: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("expected 0 tags after clear, got %d: %v", len(tags), tags)
	}
}

func TestSetTags_ClearWithNilSlice(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/clear-nil.md", "nil12345", "Clear Nil")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	// Set some tags first.
	err = s.SetTags(ctx, "projects/test/plan/clear-nil.md", []string{"x"})
	if err != nil {
		t.Fatalf("SetTags failed: %v", err)
	}

	// Clear with nil slice.
	err = s.SetTags(ctx, "projects/test/plan/clear-nil.md", nil)
	if err != nil {
		t.Fatalf("SetTags (nil) failed: %v", err)
	}

	tags, err := s.GetTags(ctx, "projects/test/plan/clear-nil.md")
	if err != nil {
		t.Fatalf("GetTags failed: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("expected 0 tags after nil clear, got %d: %v", len(tags), tags)
	}
}

func TestSetTags_NoteNotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	err := s.SetTags(ctx, "nonexistent/path.md", []string{"tag"})
	if err == nil {
		t.Fatal("expected error for nonexistent note, got nil")
	}
}

// ---------------------------------------------------------------------------
// GetTags
// ---------------------------------------------------------------------------

func TestGetTags_WithTags(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/get-tags.md", "get12345", "Get Tags")
	inserted, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	// Insert tags directly to test GetTags in isolation.
	for _, tag := range []string{"alpha", "beta"} {
		_, err := s.DB().ExecContext(ctx, "INSERT INTO tags (note_id, tag) VALUES (?, ?)", inserted.ID, tag)
		if err != nil {
			t.Fatalf("insert tag %q failed: %v", tag, err)
		}
	}

	tags, err := s.GetTags(ctx, "projects/test/plan/get-tags.md")
	if err != nil {
		t.Fatalf("GetTags failed: %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("got %d tags, want 2", len(tags))
	}
	sort.Strings(tags)
	if tags[0] != "alpha" || tags[1] != "beta" {
		t.Errorf("tags = %v, want [alpha beta]", tags)
	}
}

func TestGetTags_NoTags(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/no-tags.md", "not12345", "No Tags")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	tags, err := s.GetTags(ctx, "projects/test/plan/no-tags.md")
	if err != nil {
		t.Fatalf("GetTags failed: %v", err)
	}
	if tags == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(tags) != 0 {
		t.Errorf("expected 0 tags, got %d: %v", len(tags), tags)
	}
}

func TestGetTags_NoteNotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	_, err := s.GetTags(ctx, "nonexistent/path.md")
	if err == nil {
		t.Fatal("expected error for nonexistent note, got nil")
	}
}

package storage

import (
	"context"
	"testing"
)

// ---------------------------------------------------------------------------
// SetLinks
// ---------------------------------------------------------------------------

func TestSetLinks_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/linked.md", "lnk12345", "Linked Note")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	links := []LinkInput{
		{TargetPath: "other/note.md", Href: "other/note.md", Title: "Other Note", Type: "markdown", Snippet: "see also"},
		{TargetPath: "another/note.md", Href: "another/note.md"},
	}
	err = s.SetLinks(ctx, "projects/test/plan/linked.md", links)
	if err != nil {
		t.Fatalf("SetLinks failed: %v", err)
	}

	got, err := s.GetLinks(ctx, "projects/test/plan/linked.md")
	if err != nil {
		t.Fatalf("GetLinks failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d links, want 2", len(got))
	}

	// First link should have all fields.
	if got[0].TargetPath != "other/note.md" {
		t.Errorf("link[0].TargetPath = %q, want %q", got[0].TargetPath, "other/note.md")
	}
	if got[0].Title != "Other Note" {
		t.Errorf("link[0].Title = %q, want %q", got[0].Title, "Other Note")
	}
	if got[0].Type != "markdown" {
		t.Errorf("link[0].Type = %q, want %q", got[0].Type, "markdown")
	}
	if got[0].Snippet != "see also" {
		t.Errorf("link[0].Snippet = %q, want %q", got[0].Snippet, "see also")
	}

	// Second link should have defaults for empty fields.
	if got[1].TargetPath != "another/note.md" {
		t.Errorf("link[1].TargetPath = %q, want %q", got[1].TargetPath, "another/note.md")
	}
	if got[1].Type != "markdown" {
		t.Errorf("link[1].Type = %q, want %q (default)", got[1].Type, "markdown")
	}
}

func TestSetLinks_ReplacesExisting(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/replace-links.md", "rpl12345", "Replace Links")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	// Set initial links.
	err = s.SetLinks(ctx, "projects/test/plan/replace-links.md", []LinkInput{
		{TargetPath: "old/link.md", Href: "old/link.md"},
	})
	if err != nil {
		t.Fatalf("SetLinks (initial) failed: %v", err)
	}

	// Replace with new links.
	err = s.SetLinks(ctx, "projects/test/plan/replace-links.md", []LinkInput{
		{TargetPath: "new/link1.md", Href: "new/link1.md"},
		{TargetPath: "new/link2.md", Href: "new/link2.md"},
	})
	if err != nil {
		t.Fatalf("SetLinks (replace) failed: %v", err)
	}

	got, err := s.GetLinks(ctx, "projects/test/plan/replace-links.md")
	if err != nil {
		t.Fatalf("GetLinks failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d links, want 2", len(got))
	}
	if got[0].TargetPath != "new/link1.md" {
		t.Errorf("link[0].TargetPath = %q, want %q", got[0].TargetPath, "new/link1.md")
	}
	if got[1].TargetPath != "new/link2.md" {
		t.Errorf("link[1].TargetPath = %q, want %q", got[1].TargetPath, "new/link2.md")
	}
}

func TestSetLinks_ClearWithEmptySlice(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/clear-links.md", "clr12345", "Clear Links")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	// Set some links first.
	err = s.SetLinks(ctx, "projects/test/plan/clear-links.md", []LinkInput{
		{TargetPath: "some/path.md", Href: "some/path.md"},
	})
	if err != nil {
		t.Fatalf("SetLinks failed: %v", err)
	}

	// Clear with empty slice.
	err = s.SetLinks(ctx, "projects/test/plan/clear-links.md", []LinkInput{})
	if err != nil {
		t.Fatalf("SetLinks (clear) failed: %v", err)
	}

	got, err := s.GetLinks(ctx, "projects/test/plan/clear-links.md")
	if err != nil {
		t.Fatalf("GetLinks failed: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected 0 links after clear, got %d", len(got))
	}
}

func TestSetLinks_NoteNotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	err := s.SetLinks(ctx, "nonexistent/path.md", []LinkInput{
		{TargetPath: "any.md", Href: "any.md"},
	})
	if err == nil {
		t.Fatal("expected error for nonexistent note, got nil")
	}
}

func TestSetLinks_TargetResolution_Exists(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create source note.
	source := sampleNote("projects/test/plan/source.md", "src12345", "Source")
	_, err := s.InsertNote(ctx, source)
	if err != nil {
		t.Fatalf("InsertNote (source) failed: %v", err)
	}

	// Create target note.
	target := sampleNote("projects/test/plan/target.md", "tgt12345", "Target")
	insertedTarget, err := s.InsertNote(ctx, target)
	if err != nil {
		t.Fatalf("InsertNote (target) failed: %v", err)
	}

	// Set link from source to target.
	err = s.SetLinks(ctx, "projects/test/plan/source.md", []LinkInput{
		{TargetPath: "projects/test/plan/target.md", Href: "projects/test/plan/target.md"},
	})
	if err != nil {
		t.Fatalf("SetLinks failed: %v", err)
	}

	got, err := s.GetLinks(ctx, "projects/test/plan/source.md")
	if err != nil {
		t.Fatalf("GetLinks failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d links, want 1", len(got))
	}

	// target_id should be resolved to the target note's ID.
	if got[0].TargetID == nil {
		t.Fatal("expected TargetID to be set (target exists), got nil")
	}
	if *got[0].TargetID != insertedTarget.ID {
		t.Errorf("TargetID = %d, want %d", *got[0].TargetID, insertedTarget.ID)
	}
}

func TestSetLinks_TargetResolution_NotExists(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	// Create source note only — no target.
	source := sampleNote("projects/test/plan/source-only.md", "sro12345", "Source Only")
	_, err := s.InsertNote(ctx, source)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	// Set link to a non-existent target.
	err = s.SetLinks(ctx, "projects/test/plan/source-only.md", []LinkInput{
		{TargetPath: "nonexistent/target.md", Href: "nonexistent/target.md"},
	})
	if err != nil {
		t.Fatalf("SetLinks failed: %v", err)
	}

	got, err := s.GetLinks(ctx, "projects/test/plan/source-only.md")
	if err != nil {
		t.Fatalf("GetLinks failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d links, want 1", len(got))
	}

	// target_id should be nil (target doesn't exist).
	if got[0].TargetID != nil {
		t.Errorf("expected TargetID to be nil (target doesn't exist), got %d", *got[0].TargetID)
	}
	// target_path should still be set.
	if got[0].TargetPath != "nonexistent/target.md" {
		t.Errorf("TargetPath = %q, want %q", got[0].TargetPath, "nonexistent/target.md")
	}
}

// ---------------------------------------------------------------------------
// GetLinks
// ---------------------------------------------------------------------------

func TestGetLinks_WithLinks(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/get-links.md", "gtl12345", "Get Links")
	inserted, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	// Insert links directly to test GetLinks in isolation.
	_, err = s.DB().ExecContext(ctx,
		"INSERT INTO links (source_id, target_path, href, title, type, snippet) VALUES (?, ?, ?, ?, ?, ?)",
		inserted.ID, "some/path.md", "some/path.md", "Some Note", "markdown", "snippet text",
	)
	if err != nil {
		t.Fatalf("insert link failed: %v", err)
	}

	got, err := s.GetLinks(ctx, "projects/test/plan/get-links.md")
	if err != nil {
		t.Fatalf("GetLinks failed: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d links, want 1", len(got))
	}
	if got[0].TargetPath != "some/path.md" {
		t.Errorf("TargetPath = %q, want %q", got[0].TargetPath, "some/path.md")
	}
	if got[0].Title != "Some Note" {
		t.Errorf("Title = %q, want %q", got[0].Title, "Some Note")
	}
}

func TestGetLinks_NoLinks(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/no-links.md", "nol12345", "No Links")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	got, err := s.GetLinks(ctx, "projects/test/plan/no-links.md")
	if err != nil {
		t.Fatalf("GetLinks failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 links, got %d", len(got))
	}
}

func TestGetLinks_NoteNotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	_, err := s.GetLinks(ctx, "nonexistent/path.md")
	if err == nil {
		t.Fatal("expected error for nonexistent note, got nil")
	}
}

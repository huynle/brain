package storage

import (
	"context"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// Helper: create a sample NoteRow for testing
// ---------------------------------------------------------------------------

func sampleNote(path, shortID, title string) *NoteRow {
	lead := "A lead paragraph"
	body := "The body content of the note"
	rawContent := "---\ntitle: " + title + "\n---\n" + body
	metadata := `{"tags":["test"]}`
	typ := "plan"
	status := "active"
	priority := "high"
	projectID := "my-project"
	featureID := "feat-1"
	created := "2025-01-01T00:00:00Z"
	modified := "2025-01-02T00:00:00Z"
	checksum := "abc123hash"

	return &NoteRow{
		Path:       path,
		ShortID:    shortID,
		Title:      title,
		Lead:       &lead,
		Body:       &body,
		RawContent: &rawContent,
		WordCount:  42,
		Checksum:   &checksum,
		Metadata:   metadata,
		Type:       &typ,
		Status:     &status,
		Priority:   &priority,
		ProjectID:  &projectID,
		FeatureID:  &featureID,
		Created:    &created,
		Modified:   &modified,
	}
}

// ---------------------------------------------------------------------------
// InsertNote
// ---------------------------------------------------------------------------

func TestNoteInsert_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/my-note.md", "abc12def", "My Note")
	got, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	// ID should be assigned
	if got.ID == 0 {
		t.Error("expected non-zero ID after insert")
	}

	// IndexedAt should be populated by SQLite DEFAULT
	if got.IndexedAt == "" {
		t.Error("expected non-empty IndexedAt after insert")
	}

	// All fields should round-trip
	if got.Path != note.Path {
		t.Errorf("Path = %q, want %q", got.Path, note.Path)
	}
	if got.ShortID != note.ShortID {
		t.Errorf("ShortID = %q, want %q", got.ShortID, note.ShortID)
	}
	if got.Title != note.Title {
		t.Errorf("Title = %q, want %q", got.Title, note.Title)
	}
	if got.WordCount != note.WordCount {
		t.Errorf("WordCount = %d, want %d", got.WordCount, note.WordCount)
	}
	if got.Metadata != note.Metadata {
		t.Errorf("Metadata = %q, want %q", got.Metadata, note.Metadata)
	}

	// Nullable fields
	if got.Lead == nil || *got.Lead != *note.Lead {
		t.Errorf("Lead = %v, want %v", got.Lead, note.Lead)
	}
	if got.Body == nil || *got.Body != *note.Body {
		t.Errorf("Body = %v, want %v", got.Body, note.Body)
	}
	if got.Type == nil || *got.Type != *note.Type {
		t.Errorf("Type = %v, want %v", got.Type, note.Type)
	}
	if got.Status == nil || *got.Status != *note.Status {
		t.Errorf("Status = %v, want %v", got.Status, note.Status)
	}
	if got.Priority == nil || *got.Priority != *note.Priority {
		t.Errorf("Priority = %v, want %v", got.Priority, note.Priority)
	}
	if got.ProjectID == nil || *got.ProjectID != *note.ProjectID {
		t.Errorf("ProjectID = %v, want %v", got.ProjectID, note.ProjectID)
	}
	if got.FeatureID == nil || *got.FeatureID != *note.FeatureID {
		t.Errorf("FeatureID = %v, want %v", got.FeatureID, note.FeatureID)
	}
	if got.Created == nil || *got.Created != *note.Created {
		t.Errorf("Created = %v, want %v", got.Created, note.Created)
	}
	if got.Modified == nil || *got.Modified != *note.Modified {
		t.Errorf("Modified = %v, want %v", got.Modified, note.Modified)
	}
}

func TestNoteInsert_DuplicatePath(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/dup.md", "abc12def", "First")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("first InsertNote failed: %v", err)
	}

	// Second insert with same path should fail
	note2 := sampleNote("projects/test/plan/dup.md", "xyz98765", "Second")
	_, err = s.InsertNote(ctx, note2)
	if err == nil {
		t.Fatal("expected error for duplicate path, got nil")
	}
	if !strings.Contains(err.Error(), "duplicate") && !strings.Contains(err.Error(), "UNIQUE") {
		t.Errorf("expected duplicate/UNIQUE error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// GetNoteByPath
// ---------------------------------------------------------------------------

func TestNoteGetByPath_Found(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/find-me.md", "abc12def", "Find Me")
	inserted, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	got, err := s.GetNoteByPath(ctx, "projects/test/plan/find-me.md")
	if err != nil {
		t.Fatalf("GetNoteByPath failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected note, got nil")
	}
	if got.ID != inserted.ID {
		t.Errorf("ID = %d, want %d", got.ID, inserted.ID)
	}
	if got.Title != "Find Me" {
		t.Errorf("Title = %q, want %q", got.Title, "Find Me")
	}
}

func TestNoteGetByPath_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	got, err := s.GetNoteByPath(ctx, "nonexistent/path.md")
	if err != nil {
		t.Fatalf("GetNoteByPath returned error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for not found, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// GetNoteByShortID
// ---------------------------------------------------------------------------

func TestNoteGetByShortID_Found(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/short.md", "uniq1234", "Short ID Note")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	got, err := s.GetNoteByShortID(ctx, "uniq1234")
	if err != nil {
		t.Fatalf("GetNoteByShortID failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected note, got nil")
	}
	if got.ShortID != "uniq1234" {
		t.Errorf("ShortID = %q, want %q", got.ShortID, "uniq1234")
	}
	if got.Title != "Short ID Note" {
		t.Errorf("Title = %q, want %q", got.Title, "Short ID Note")
	}
}

func TestNoteGetByShortID_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	got, err := s.GetNoteByShortID(ctx, "nonexist")
	if err != nil {
		t.Fatalf("GetNoteByShortID returned error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for not found, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// GetNoteByTitle
// ---------------------------------------------------------------------------

func TestNoteGetByTitle_Found(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/titled.md", "abc12def", "Exact Title Match")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	got, err := s.GetNoteByTitle(ctx, "Exact Title Match")
	if err != nil {
		t.Fatalf("GetNoteByTitle failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected note, got nil")
	}
	if got.Title != "Exact Title Match" {
		t.Errorf("Title = %q, want %q", got.Title, "Exact Title Match")
	}
}

func TestNoteGetByTitle_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	got, err := s.GetNoteByTitle(ctx, "No Such Title")
	if err != nil {
		t.Fatalf("GetNoteByTitle returned error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for not found, got %+v", got)
	}
}

// ---------------------------------------------------------------------------
// UpdateNote
// ---------------------------------------------------------------------------

func TestNoteUpdate_PartialUpdate(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/update.md", "abc12def", "Before Update")
	inserted, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	// Update only title and status
	updates := map[string]interface{}{
		"title":  "After Update",
		"status": "completed",
	}
	got, err := s.UpdateNote(ctx, "projects/test/plan/update.md", updates)
	if err != nil {
		t.Fatalf("UpdateNote failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected updated note, got nil")
	}

	// Updated fields
	if got.Title != "After Update" {
		t.Errorf("Title = %q, want %q", got.Title, "After Update")
	}
	if got.Status == nil || *got.Status != "completed" {
		t.Errorf("Status = %v, want %q", got.Status, "completed")
	}

	// Unchanged fields should be preserved
	if got.ShortID != inserted.ShortID {
		t.Errorf("ShortID changed: got %q, want %q", got.ShortID, inserted.ShortID)
	}
	if got.Lead == nil || *got.Lead != *inserted.Lead {
		t.Errorf("Lead changed unexpectedly")
	}

	// IndexedAt should be updated (newer than original)
	if got.IndexedAt == inserted.IndexedAt {
		// In fast tests this might be the same second, so just check it's non-empty
		if got.IndexedAt == "" {
			t.Error("IndexedAt should be non-empty after update")
		}
	}
}

func TestNoteUpdate_FullUpdate(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/full-update.md", "abc12def", "Original")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	updates := map[string]interface{}{
		"title":       "New Title",
		"lead":        "New lead",
		"body":        "New body",
		"raw_content": "New raw content",
		"word_count":  99,
		"checksum":    "newhash",
		"metadata":    `{"updated":true}`,
		"type":        "summary",
		"status":      "archived",
		"priority":    "low",
		"project_id":  "new-project",
		"feature_id":  "feat-2",
		"created":     "2025-06-01T00:00:00Z",
		"modified":    "2025-06-02T00:00:00Z",
	}
	got, err := s.UpdateNote(ctx, "projects/test/plan/full-update.md", updates)
	if err != nil {
		t.Fatalf("UpdateNote failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected updated note, got nil")
	}

	if got.Title != "New Title" {
		t.Errorf("Title = %q, want %q", got.Title, "New Title")
	}
	if got.WordCount != 99 {
		t.Errorf("WordCount = %d, want 99", got.WordCount)
	}
	if got.Metadata != `{"updated":true}` {
		t.Errorf("Metadata = %q, want %q", got.Metadata, `{"updated":true}`)
	}
	if got.Type == nil || *got.Type != "summary" {
		t.Errorf("Type = %v, want %q", got.Type, "summary")
	}
	if got.Priority == nil || *got.Priority != "low" {
		t.Errorf("Priority = %v, want %q", got.Priority, "low")
	}
}

func TestNoteUpdate_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	updates := map[string]interface{}{"title": "Ghost"}
	got, err := s.UpdateNote(ctx, "nonexistent/path.md", updates)
	if err != nil {
		t.Fatalf("UpdateNote returned error: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil for not found, got %+v", got)
	}
}

func TestNoteUpdate_InvalidFieldRejected(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/inject.md", "abc12def", "Inject Test")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	// Attempt to update with a disallowed field (SQL injection attempt)
	updates := map[string]interface{}{
		"title":                    "Safe",
		"id; DROP TABLE notes; --": "evil",
	}
	_, err = s.UpdateNote(ctx, "projects/test/plan/inject.md", updates)
	if err == nil {
		t.Fatal("expected error for invalid field name, got nil")
	}
	if !strings.Contains(err.Error(), "not allowed") {
		t.Errorf("expected 'not allowed' error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// DeleteNote
// ---------------------------------------------------------------------------

func TestNoteDelete_Success(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/delete-me.md", "abc12def", "Delete Me")
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	deleted, err := s.DeleteNote(ctx, "projects/test/plan/delete-me.md")
	if err != nil {
		t.Fatalf("DeleteNote failed: %v", err)
	}
	if !deleted {
		t.Error("expected deleted=true, got false")
	}

	// Verify it's gone
	got, err := s.GetNoteByPath(ctx, "projects/test/plan/delete-me.md")
	if err != nil {
		t.Fatalf("GetNoteByPath after delete failed: %v", err)
	}
	if got != nil {
		t.Error("expected nil after delete, got note")
	}
}

func TestNoteDelete_NotFound(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	deleted, err := s.DeleteNote(ctx, "nonexistent/path.md")
	if err != nil {
		t.Fatalf("DeleteNote returned error: %v", err)
	}
	if deleted {
		t.Error("expected deleted=false for not found, got true")
	}
}

// ---------------------------------------------------------------------------
// FTS5 populated after InsertNote
// ---------------------------------------------------------------------------

func TestNoteInsert_FTS5Populated(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note := sampleNote("projects/test/plan/fts.md", "fts12345", "Searchable Title")
	body := "This note has unique searchable body content xylophone"
	note.Body = &body
	_, err := s.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}

	// FTS5 should find by title
	var count int
	err = s.DB().QueryRow(
		"SELECT count(*) FROM notes_fts WHERE notes_fts MATCH 'Searchable'",
	).Scan(&count)
	if err != nil {
		t.Fatalf("FTS5 title search failed: %v", err)
	}
	if count != 1 {
		t.Errorf("FTS5 title match count = %d, want 1", count)
	}

	// FTS5 should find by body
	err = s.DB().QueryRow(
		"SELECT count(*) FROM notes_fts WHERE notes_fts MATCH 'xylophone'",
	).Scan(&count)
	if err != nil {
		t.Fatalf("FTS5 body search failed: %v", err)
	}
	if count != 1 {
		t.Errorf("FTS5 body match count = %d, want 1", count)
	}
}

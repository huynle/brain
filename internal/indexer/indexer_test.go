package indexer

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/glebarez/go-sqlite"
	"github.com/huynle/brain-api/internal/storage"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// newTestStorage creates an in-memory StorageLayer for testing.
func newTestStorage(t *testing.T) *storage.StorageLayer {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	s, err := storage.NewWithDB(db)
	if err != nil {
		t.Fatalf("NewWithDB failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// createBrainDir creates a temp directory with markdown files for testing.
// Returns the brain directory path.
func createBrainDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for relPath, content := range files {
		fullPath := filepath.Join(dir, relPath)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0o755); err != nil {
			t.Fatalf("mkdir %q: %v", filepath.Dir(fullPath), err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			t.Fatalf("write %q: %v", fullPath, err)
		}
	}
	return dir
}

// noteContent generates a simple markdown file with frontmatter.
func noteContent(title string, tags ...string) string {
	content := "---\ntitle: " + title + "\n"
	if len(tags) > 0 {
		content += "tags:\n"
		for _, tag := range tags {
			content += "  - " + tag + "\n"
		}
	}
	content += "---\n\nBody of " + title + ".\n"
	return content
}

// noteWithLink generates a markdown file with a link.
func noteWithLink(title, linkTarget, linkText string) string {
	return "---\ntitle: " + title + "\n---\n\nSee [" + linkText + "](" + linkTarget + ") for details.\n"
}

// countNotes returns the number of notes in the DB.
func countNotes(t *testing.T, s *storage.StorageLayer) int {
	t.Helper()
	var count int
	err := s.DB().QueryRow("SELECT COUNT(*) FROM notes").Scan(&count)
	if err != nil {
		t.Fatalf("count notes: %v", err)
	}
	return count
}

type recordingEmbeddingClient struct {
	inputs []string
}

func (c *recordingEmbeddingClient) Embed(_ context.Context, inputs []string) ([][]float32, error) {
	c.inputs = append(c.inputs, inputs...)
	embeddings := make([][]float32, len(inputs))
	for i := range embeddings {
		embeddings[i] = []float32{1, 2, 3}
	}
	return embeddings, nil
}

// ---------------------------------------------------------------------------
// RebuildAll tests
// ---------------------------------------------------------------------------

func TestRebuildAll_IndexesAllFiles(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md":         noteContent("Note One"),
		"note2.md":         noteContent("Note Two"),
		"sub/dir/note3.md": noteContent("Note Three"),
	})

	idx := NewIndexer(brainDir, store)
	result, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	if result.Added != 3 {
		t.Errorf("Added = %d, want 3", result.Added)
	}
	if result.Deleted != 0 {
		t.Errorf("Deleted = %d, want 0", result.Deleted)
	}
	if result.Duration <= 0 {
		t.Error("Duration should be > 0")
	}
	if countNotes(t, store) != 3 {
		t.Errorf("DB note count = %d, want 3", countNotes(t, store))
	}
}

func TestRebuildAll_ExcludesZkDirectory(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md":              noteContent("Note One"),
		".brain-data/config.md": noteContent("ZK Config"),
	})

	idx := NewIndexer(brainDir, store)
	result, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	if result.Added != 1 {
		t.Errorf("Added = %d, want 1 (should exclude .brain-data/)", result.Added)
	}
}

func TestRebuildAll_ClearsExistingData(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})

	idx := NewIndexer(brainDir, store)

	// First rebuild
	_, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("first RebuildAll failed: %v", err)
	}
	if countNotes(t, store) != 1 {
		t.Fatalf("after first rebuild: count = %d, want 1", countNotes(t, store))
	}

	// Remove the file, add a different one
	os.Remove(filepath.Join(brainDir, "note1.md"))
	os.WriteFile(filepath.Join(brainDir, "note2.md"), []byte(noteContent("Note Two")), 0o644)

	// Second rebuild should clear old data
	result, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("second RebuildAll failed: %v", err)
	}

	if result.Added != 1 {
		t.Errorf("Added = %d, want 1", result.Added)
	}
	if result.Deleted != 1 {
		t.Errorf("Deleted = %d, want 1 (previous note should be counted)", result.Deleted)
	}
	if countNotes(t, store) != 1 {
		t.Errorf("DB note count = %d, want 1", countNotes(t, store))
	}

	// Verify the correct note is in DB
	note, err := store.GetNoteByPath(context.Background(), "note2.md")
	if err != nil {
		t.Fatalf("GetNoteByPath failed: %v", err)
	}
	if note == nil {
		t.Fatal("note2.md not found in DB after rebuild")
	}
	if note.Title != "Note Two" {
		t.Errorf("Title = %q, want %q", note.Title, "Note Two")
	}
}

func TestRebuildAll_IndexesTags(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Tagged Note", "go", "testing"),
	})

	idx := NewIndexer(brainDir, store)
	_, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	tags, err := store.GetTags(context.Background(), "note1.md")
	if err != nil {
		t.Fatalf("GetTags failed: %v", err)
	}
	if len(tags) != 2 {
		t.Errorf("tag count = %d, want 2", len(tags))
	}
}

func TestRebuildAll_IndexesLinks(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteWithLink("Linker", "note2.md", "Note Two"),
		"note2.md": noteContent("Note Two"),
	})

	idx := NewIndexer(brainDir, store)
	_, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	links, err := store.GetLinks(context.Background(), "note1.md")
	if err != nil {
		t.Fatalf("GetLinks failed: %v", err)
	}
	if len(links) != 1 {
		t.Errorf("link count = %d, want 1", len(links))
	}
}

func TestRebuildAll_HandlesParseErrors(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"good.md": noteContent("Good Note"),
		"bad.md":  "---\ntitle: [invalid yaml\n---\nBody\n",
	})

	idx := NewIndexer(brainDir, store)
	result, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	// Good file should be indexed
	if result.Added != 1 {
		t.Errorf("Added = %d, want 1", result.Added)
	}
	// Bad file should be in errors
	if len(result.Errors) != 1 {
		t.Errorf("Errors count = %d, want 1", len(result.Errors))
	}
	if len(result.Errors) > 0 && result.Errors[0].Path != "bad.md" {
		t.Errorf("Error path = %q, want %q", result.Errors[0].Path, "bad.md")
	}
}

// ---------------------------------------------------------------------------
// IndexChanged tests
// ---------------------------------------------------------------------------

func TestIndexChanged_DetectsNewFiles(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})

	idx := NewIndexer(brainDir, store)

	// Initial rebuild
	_, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	// Add a new file
	os.WriteFile(filepath.Join(brainDir, "note2.md"), []byte(noteContent("Note Two")), 0o644)

	result, err := idx.IndexChanged()
	if err != nil {
		t.Fatalf("IndexChanged failed: %v", err)
	}

	if result.Added != 1 {
		t.Errorf("Added = %d, want 1", result.Added)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if countNotes(t, store) != 2 {
		t.Errorf("DB note count = %d, want 2", countNotes(t, store))
	}
}

func TestIndexChanged_DetectsModifiedFiles(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})

	idx := NewIndexer(brainDir, store)

	// Initial rebuild
	_, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	// Modify the file (different content = different checksum)
	os.WriteFile(filepath.Join(brainDir, "note1.md"), []byte(noteContent("Note One Updated")), 0o644)

	result, err := idx.IndexChanged()
	if err != nil {
		t.Fatalf("IndexChanged failed: %v", err)
	}

	if result.Updated != 1 {
		t.Errorf("Updated = %d, want 1", result.Updated)
	}

	// Verify the title was updated
	note, err := store.GetNoteByPath(context.Background(), "note1.md")
	if err != nil {
		t.Fatalf("GetNoteByPath failed: %v", err)
	}
	if note == nil {
		t.Fatal("note1.md not found after update")
	}
	if note.Title != "Note One Updated" {
		t.Errorf("Title = %q, want %q", note.Title, "Note One Updated")
	}
}

func TestIndexChanged_DetectsDeletedFiles(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
		"note2.md": noteContent("Note Two"),
	})

	idx := NewIndexer(brainDir, store)

	// Initial rebuild
	_, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	// Delete a file
	os.Remove(filepath.Join(brainDir, "note2.md"))

	result, err := idx.IndexChanged()
	if err != nil {
		t.Fatalf("IndexChanged failed: %v", err)
	}

	if result.Deleted != 1 {
		t.Errorf("Deleted = %d, want 1", result.Deleted)
	}
	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if countNotes(t, store) != 1 {
		t.Errorf("DB note count = %d, want 1", countNotes(t, store))
	}
}

func TestIndexChanged_SkipsUnchangedFiles(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})

	idx := NewIndexer(brainDir, store)

	// Initial rebuild
	_, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	// Run incremental with no changes
	result, err := idx.IndexChanged()
	if err != nil {
		t.Fatalf("IndexChanged failed: %v", err)
	}

	if result.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", result.Skipped)
	}
	if result.Added != 0 {
		t.Errorf("Added = %d, want 0", result.Added)
	}
	if result.Updated != 0 {
		t.Errorf("Updated = %d, want 0", result.Updated)
	}
	if result.Deleted != 0 {
		t.Errorf("Deleted = %d, want 0", result.Deleted)
	}
}

// ---------------------------------------------------------------------------
// IndexFile tests
// ---------------------------------------------------------------------------

func TestIndexFile_InsertsNewFile(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})

	idx := NewIndexer(brainDir, store)

	err := idx.IndexFile("note1.md")
	if err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}

	note, err := store.GetNoteByPath(context.Background(), "note1.md")
	if err != nil {
		t.Fatalf("GetNoteByPath failed: %v", err)
	}
	if note == nil {
		t.Fatal("note1.md not found after IndexFile")
	}
	if note.Title != "Note One" {
		t.Errorf("Title = %q, want %q", note.Title, "Note One")
	}
}

func TestIndexFile_UpdatesExistingFile(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})

	idx := NewIndexer(brainDir, store)

	// First index
	err := idx.IndexFile("note1.md")
	if err != nil {
		t.Fatalf("first IndexFile failed: %v", err)
	}

	// Modify file
	os.WriteFile(filepath.Join(brainDir, "note1.md"), []byte(noteContent("Note One Updated")), 0o644)

	// Re-index
	err = idx.IndexFile("note1.md")
	if err != nil {
		t.Fatalf("second IndexFile failed: %v", err)
	}

	note, err := store.GetNoteByPath(context.Background(), "note1.md")
	if err != nil {
		t.Fatalf("GetNoteByPath failed: %v", err)
	}
	if note == nil {
		t.Fatal("note1.md not found after re-index")
	}
	if note.Title != "Note One Updated" {
		t.Errorf("Title = %q, want %q", note.Title, "Note One Updated")
	}

	// Should still be only 1 note
	if countNotes(t, store) != 1 {
		t.Errorf("DB note count = %d, want 1", countNotes(t, store))
	}
}

// ---------------------------------------------------------------------------
// RemoveFile tests
// ---------------------------------------------------------------------------

func TestRemoveFile_DeletesFromDB(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})

	idx := NewIndexer(brainDir, store)

	// Index first
	err := idx.IndexFile("note1.md")
	if err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}
	if countNotes(t, store) != 1 {
		t.Fatalf("expected 1 note after index, got %d", countNotes(t, store))
	}

	// Remove
	err = idx.RemoveFile("note1.md")
	if err != nil {
		t.Fatalf("RemoveFile failed: %v", err)
	}

	if countNotes(t, store) != 0 {
		t.Errorf("DB note count = %d, want 0 after remove", countNotes(t, store))
	}
}

// ---------------------------------------------------------------------------
// GetHealth tests
// ---------------------------------------------------------------------------

func TestGetHealth_ReportsCorrectCounts(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
		"note2.md": noteContent("Note Two"),
	})

	idx := NewIndexer(brainDir, store)

	// Rebuild to index both files
	_, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	health, err := idx.GetHealth()
	if err != nil {
		t.Fatalf("GetHealth failed: %v", err)
	}

	if health.TotalFiles != 2 {
		t.Errorf("TotalFiles = %d, want 2", health.TotalFiles)
	}
	if health.TotalIndexed != 2 {
		t.Errorf("TotalIndexed = %d, want 2", health.TotalIndexed)
	}
	if health.StaleCount != 0 {
		t.Errorf("StaleCount = %d, want 0", health.StaleCount)
	}
}

func TestGetHealth_DetectsStaleEntries(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
		"note2.md": noteContent("Note Two"),
	})

	idx := NewIndexer(brainDir, store)

	// Rebuild to index both files
	_, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	// Delete one file from disk (but not from DB)
	os.Remove(filepath.Join(brainDir, "note2.md"))

	health, err := idx.GetHealth()
	if err != nil {
		t.Fatalf("GetHealth failed: %v", err)
	}

	if health.TotalFiles != 1 {
		t.Errorf("TotalFiles = %d, want 1", health.TotalFiles)
	}
	if health.TotalIndexed != 2 {
		t.Errorf("TotalIndexed = %d, want 2", health.TotalIndexed)
	}
	if health.StaleCount != 1 {
		t.Errorf("StaleCount = %d, want 1", health.StaleCount)
	}
}

// ---------------------------------------------------------------------------
// Embedding indexing tests
// ---------------------------------------------------------------------------

func TestIndexEmbeddingsWithOptions_IncludesReadyTextAttachmentDerivedContent(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": "---\ntitle: Note One\n---\n\nBody of note one.\n",
	})

	idx := NewIndexer(brainDir, store)
	if err := idx.IndexFile("note1.md"); err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}

	att, err := store.CreateAttachment(ctx, storage.AttachmentInput{
		Digest:    strings.Repeat("a", 64),
		Size:      42,
		MediaType: "text/plain",
	})
	if err != nil {
		t.Fatalf("CreateAttachment failed: %v", err)
	}
	if err := store.LinkAttachmentToEntry(ctx, "note1.md", att.ID, "inline"); err != nil {
		t.Fatalf("LinkAttachmentToEntry failed: %v", err)
	}
	if _, err := store.UpsertAttachmentDerived(ctx, storage.AttachmentDerivedInput{
		AttachmentID: att.ID,
		Kind:         "text",
		Status:       "ready",
		ContentType:  "text/plain",
		Text:         "Attachment text that should be semantically indexed.",
	}); err != nil {
		t.Fatalf("UpsertAttachmentDerived failed: %v", err)
	}

	client := &recordingEmbeddingClient{}
	result, err := idx.IndexEmbeddingsWithOptions(ctx, client, EmbeddingIndexOptions{})
	if err != nil {
		t.Fatalf("IndexEmbeddingsWithOptions failed: %v", err)
	}

	if result.Processed != 1 {
		t.Fatalf("Processed = %d, want 1", result.Processed)
	}
	if len(client.inputs) != 1 {
		t.Fatalf("embedding inputs = %d, want 1", len(client.inputs))
	}
	input := client.inputs[0]
	if !strings.Contains(input, "Body of note one.") {
		t.Fatalf("embedding input missing note body: %q", input)
	}
	if !strings.Contains(input, "Attachments:") {
		t.Fatalf("embedding input missing attachment separator: %q", input)
	}
	if !strings.Contains(input, "Attachment text that should be semantically indexed.") {
		t.Fatalf("embedding input missing derived attachment text: %q", input)
	}
}

func TestEmbeddingBackfillCandidatesAndHealth_UseReadyAttachmentDerivedUpdatedAtForStaleness(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"ready.md":   "---\ntitle: Ready Attachment\n---\n\nReady body.\n",
		"pending.md": "---\ntitle: Pending Attachment\n---\n\nPending body.\n",
	})

	idx := NewIndexer(brainDir, store)
	if err := idx.IndexFile("ready.md"); err != nil {
		t.Fatalf("IndexFile ready.md failed: %v", err)
	}
	if err := idx.IndexFile("pending.md"); err != nil {
		t.Fatalf("IndexFile pending.md failed: %v", err)
	}

	readyNote, err := store.GetNoteByPath(ctx, "ready.md")
	if err != nil || readyNote == nil {
		t.Fatalf("expected ready note, got %#v err=%v", readyNote, err)
	}
	pendingNote, err := store.GetNoteByPath(ctx, "pending.md")
	if err != nil || pendingNote == nil {
		t.Fatalf("expected pending note, got %#v err=%v", pendingNote, err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE notes SET indexed_at = '2025-01-01 00:00:00'`); err != nil {
		t.Fatalf("set note indexed_at failed: %v", err)
	}
	if err := store.UpsertNoteEmbeddings(ctx, []storage.EmbeddingRecord{
		{NoteID: readyNote.ID, ChunkIndex: 0, Vector: []float32{1, 0, 0}},
		{NoteID: pendingNote.ID, ChunkIndex: 0, Vector: []float32{0, 1, 0}},
	}); err != nil {
		t.Fatalf("UpsertNoteEmbeddings failed: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE note_embeddings_meta SET embedding_indexed_at = '2025-01-02 00:00:00'`); err != nil {
		t.Fatalf("set embedding_indexed_at failed: %v", err)
	}

	readyAttachment, err := store.CreateAttachment(ctx, storage.AttachmentInput{Digest: strings.Repeat("b", 64), Size: 10, MediaType: "text/plain"})
	if err != nil {
		t.Fatalf("CreateAttachment ready failed: %v", err)
	}
	if err := store.LinkAttachmentToEntry(ctx, "ready.md", readyAttachment.ID, "inline"); err != nil {
		t.Fatalf("LinkAttachmentToEntry ready failed: %v", err)
	}
	if _, err := store.UpsertAttachmentDerived(ctx, storage.AttachmentDerivedInput{
		AttachmentID: readyAttachment.ID,
		Kind:         "text",
		Status:       "ready",
		ContentType:  "text/plain",
		Text:         "ready derived text",
	}); err != nil {
		t.Fatalf("UpsertAttachmentDerived ready failed: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE attachment_derived SET updated_at = '2025-01-03 00:00:00' WHERE attachment_id = ?`, readyAttachment.ID); err != nil {
		t.Fatalf("set ready derived updated_at failed: %v", err)
	}

	pendingAttachment, err := store.CreateAttachment(ctx, storage.AttachmentInput{Digest: strings.Repeat("c", 64), Size: 10, MediaType: "text/plain"})
	if err != nil {
		t.Fatalf("CreateAttachment pending failed: %v", err)
	}
	if err := store.LinkAttachmentToEntry(ctx, "pending.md", pendingAttachment.ID, "inline"); err != nil {
		t.Fatalf("LinkAttachmentToEntry pending failed: %v", err)
	}
	if _, err := store.UpsertAttachmentDerived(ctx, storage.AttachmentDerivedInput{
		AttachmentID: pendingAttachment.ID,
		Kind:         "text",
		Status:       "pending",
		ContentType:  "text/plain",
		Text:         "pending derived text",
	}); err != nil {
		t.Fatalf("UpsertAttachmentDerived pending failed: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE attachment_derived SET updated_at = '2025-01-04 00:00:00' WHERE attachment_id = ?`, pendingAttachment.ID); err != nil {
		t.Fatalf("set pending derived updated_at failed: %v", err)
	}

	candidates, err := idx.ListEmbeddingBackfillCandidates(ctx, EmbeddingIndexOptions{})
	if err != nil {
		t.Fatalf("ListEmbeddingBackfillCandidates failed: %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1: %#v", len(candidates), candidates)
	}
	if candidates[0].Path != "ready.md" {
		t.Fatalf("candidate path = %q, want ready.md", candidates[0].Path)
	}

	health, err := idx.GetEmbeddingHealth()
	if err != nil {
		t.Fatalf("GetEmbeddingHealth failed: %v", err)
	}
	if health.StaleEmbeddings != 1 {
		t.Fatalf("StaleEmbeddings = %d, want 1", health.StaleEmbeddings)
	}
}

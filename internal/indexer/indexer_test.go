package indexer

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/glebarez/go-sqlite"
	"github.com/huynle/brain-api/internal/embeddings"
	"github.com/huynle/brain-api/internal/storage"
)

type fakeEmbedder struct {
	vectors [][]float32
}

func (f fakeEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	return f.vectors[:len(texts)], nil
}

type recordingEmbedder struct {
	texts [][]string
	err   error
}

func (r *recordingEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	r.texts = append(r.texts, append([]string(nil), texts...))
	if r.err != nil {
		return nil, r.err
	}
	vectors := make([][]float32, len(texts))
	for i := range texts {
		vectors[i] = []float32{float32(len(r.texts)), float32(i + 1)}
	}
	return vectors, nil
}

func (r *recordingEmbedder) reset() {
	r.texts = nil
}

func (r *recordingEmbedder) textCount() int {
	count := 0
	for _, batch := range r.texts {
		count += len(batch)
	}
	return count
}

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

func TestRebuildAll_StoresEmbeddingsWhenConfigured(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
		"note2.md": noteContent("Note Two"),
	})
	embedder := &recordingEmbedder{}
	idx := NewIndexer(brainDir, store, WithEmbeddings(embedder, "test-model", 16))

	result, err := idx.RebuildAll()
	if err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}

	if result.Added != 2 {
		t.Fatalf("Added = %d, want 2", result.Added)
	}
	if embedder.textCount() != 2 {
		t.Fatalf("embedded text count = %d, want 2", embedder.textCount())
	}
	for _, path := range []string{"note1.md", "note2.md"} {
		got, err := store.GetEntryEmbedding(context.Background(), path, 0, "test-model")
		if err != nil {
			t.Fatalf("GetEntryEmbedding(%q) failed: %v", path, err)
		}
		if got == nil {
			t.Fatalf("expected embedding row for %q", path)
		}
	}
}

func TestNewIndexerWithEmbeddingsStoresOptions(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, nil)
	embedder := fakeEmbedder{vectors: [][]float32{{1, 2, 3}}}

	idx := NewIndexer(brainDir, store, WithEmbeddings(embedder, "test-model", 32))

	if idx.embedder == nil {
		t.Fatal("embedder was not configured")
	}
	if idx.embeddingModel != "test-model" {
		t.Fatalf("embeddingModel = %q, want test-model", idx.embeddingModel)
	}
	if idx.embeddingBatchSize != 32 {
		t.Fatalf("embeddingBatchSize = %d, want 32", idx.embeddingBatchSize)
	}
}

func TestIndexFileStoresEmbeddingWhenConfigured(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})
	idx := NewIndexer(brainDir, store, WithEmbeddings(fakeEmbedder{vectors: [][]float32{{1, 2, 3}}}, "test-model", 16))

	if err := idx.IndexFile("note1.md"); err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}

	got, err := store.GetEntryEmbedding(context.Background(), "note1.md", 0, "test-model")
	if err != nil {
		t.Fatalf("GetEntryEmbedding failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected embedding row, got nil")
	}
	if got.Dimensions != 3 {
		t.Fatalf("Dimensions = %d, want 3", got.Dimensions)
	}
	decoded, err := embeddings.DecodeFloat32Vector(got.Embedding)
	if err != nil {
		t.Fatalf("DecodeFloat32Vector failed: %v", err)
	}
	if len(decoded) != 3 || decoded[0] != 1 || decoded[1] != 2 || decoded[2] != 3 {
		t.Fatalf("decoded embedding = %+v, want [1 2 3]", decoded)
	}
}

func TestIndexFileEmbedsSemanticTextWithoutRawFrontmatter(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": "---\ntitle: Semantic Title\nsecret: do-not-embed\ntags:\n  - internal\n---\n\nLead paragraph.\n\nBody details here.\n",
	})
	embedder := &recordingEmbedder{}
	idx := NewIndexer(brainDir, store, WithEmbeddings(embedder, "test-model", 16))

	if err := idx.IndexFile("note1.md"); err != nil {
		t.Fatalf("IndexFile failed: %v", err)
	}

	if embedder.textCount() != 1 {
		t.Fatalf("embedded text count = %d, want 1", embedder.textCount())
	}
	text := embedder.texts[0][0]
	for _, want := range []string{"Semantic Title", "Lead paragraph.", "Body details here."} {
		if !strings.Contains(text, want) {
			t.Fatalf("embedded text %q missing %q", text, want)
		}
	}
	for _, unwanted := range []string{"secret", "do-not-embed", "tags:", "internal", "---"} {
		if strings.Contains(text, unwanted) {
			t.Fatalf("embedded text %q includes raw frontmatter value %q", text, unwanted)
		}
	}
}

func TestIndexFileReturnsEmbeddingFailureWhenConfigured(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
	})
	wantErr := errors.New("provider unavailable")
	idx := NewIndexer(brainDir, store, WithEmbeddings(&recordingEmbedder{err: wantErr}, "test-model", 16))

	err := idx.IndexFile("note1.md")
	if !errors.Is(err, wantErr) {
		t.Fatalf("IndexFile error = %v, want %v", err, wantErr)
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

func TestIndexChanged_EmbedsOnlyNewAndModifiedFilesWhenConfigured(t *testing.T) {
	store := newTestStorage(t)
	brainDir := createBrainDir(t, map[string]string{
		"note1.md": noteContent("Note One"),
		"note2.md": noteContent("Note Two"),
	})
	embedder := &recordingEmbedder{}
	idx := NewIndexer(brainDir, store, WithEmbeddings(embedder, "test-model", 16))

	if _, err := idx.RebuildAll(); err != nil {
		t.Fatalf("RebuildAll failed: %v", err)
	}
	embedder.reset()

	if err := os.WriteFile(filepath.Join(brainDir, "note1.md"), []byte(noteContent("Note One Updated")), 0o644); err != nil {
		t.Fatalf("write modified note: %v", err)
	}
	if err := os.WriteFile(filepath.Join(brainDir, "note3.md"), []byte(noteContent("Note Three")), 0o644); err != nil {
		t.Fatalf("write new note: %v", err)
	}

	result, err := idx.IndexChanged()
	if err != nil {
		t.Fatalf("IndexChanged failed: %v", err)
	}

	if result.Added != 1 {
		t.Fatalf("Added = %d, want 1", result.Added)
	}
	if result.Updated != 1 {
		t.Fatalf("Updated = %d, want 1", result.Updated)
	}
	if result.Skipped != 1 {
		t.Fatalf("Skipped = %d, want 1", result.Skipped)
	}
	if embedder.textCount() != 2 {
		t.Fatalf("embedded text count = %d, want 2", embedder.textCount())
	}

	embedded := strings.Join([]string{embedder.texts[0][0], embedder.texts[1][0]}, "\n")
	for _, want := range []string{"Note One Updated", "Note Three"} {
		if !strings.Contains(embedded, want) {
			t.Fatalf("embedded text %q missing %q", embedded, want)
		}
	}
	if strings.Contains(embedded, "Note Two") {
		t.Fatalf("embedded text %q includes unchanged note", embedded)
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

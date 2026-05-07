package storage

import (
	"bytes"
	"context"
	"testing"
)

func TestEntryEmbeddingsSchemaCreated(t *testing.T) {
	s := newTestStorage(t)

	var name string
	if err := s.DB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='entry_embeddings'").Scan(&name); err != nil {
		t.Fatalf("entry_embeddings table not found: %v", err)
	}
	if name != "entry_embeddings" {
		t.Errorf("table name = %q, want entry_embeddings", name)
	}

	indexes := []string{
		"idx_entry_embeddings_path",
		"idx_entry_embeddings_model",
		"idx_entry_embeddings_content_hash",
	}
	for _, index := range indexes {
		t.Run(index, func(t *testing.T) {
			var got string
			if err := s.DB().QueryRow("SELECT name FROM sqlite_master WHERE type='index' AND name=?", index).Scan(&got); err != nil {
				t.Fatalf("index %q not found: %v", index, err)
			}
		})
	}
}

func TestEntryEmbeddingsMigrationCreatesTable(t *testing.T) {
	db := openMemoryDB(t)
	defer db.Close()

	if _, err := db.Exec(createNotesTable); err != nil {
		t.Fatalf("create notes table: %v", err)
	}
	if _, err := db.Exec(createSchemaVersionTable); err != nil {
		t.Fatalf("create schema_version table: %v", err)
	}
	if err := SetSchemaVersion(db, 6); err != nil {
		t.Fatalf("set schema version: %v", err)
	}

	if err := migrateSchema(db); err != nil {
		t.Fatalf("migrateSchema failed: %v", err)
	}

	var name string
	if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='entry_embeddings'").Scan(&name); err != nil {
		t.Fatalf("entry_embeddings table not found after migration: %v", err)
	}
}

func TestUpsertEntryEmbeddingInsertsAndUpdatesChunk(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	insertNoteForEmbeddingTest(t, ctx, s, "projects/test/a.md", "hash-a")

	inserted, err := s.UpsertEntryEmbedding(ctx, &EntryEmbeddingRow{
		Path:        "projects/test/a.md",
		ChunkIndex:  0,
		ContentHash: "hash-a",
		Model:       "test-model",
		Dimensions:  3,
		Embedding:   []byte{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("UpsertEntryEmbedding insert failed: %v", err)
	}
	if inserted.ID == 0 || inserted.CreatedAt == "" || inserted.UpdatedAt == "" {
		t.Fatalf("inserted embedding missing generated fields: %+v", inserted)
	}

	updated, err := s.UpsertEntryEmbedding(ctx, &EntryEmbeddingRow{
		Path:        "projects/test/a.md",
		ChunkIndex:  0,
		ContentHash: "hash-b",
		Model:       "test-model",
		Dimensions:  4,
		Embedding:   []byte{4, 5, 6, 7},
	})
	if err != nil {
		t.Fatalf("UpsertEntryEmbedding update failed: %v", err)
	}
	if updated.ID != inserted.ID {
		t.Errorf("updated ID = %d, want original ID %d", updated.ID, inserted.ID)
	}
	if updated.ContentHash != "hash-b" || updated.Dimensions != 4 || !bytes.Equal(updated.Embedding, []byte{4, 5, 6, 7}) {
		t.Errorf("updated embedding = %+v, want new values", updated)
	}
}

func TestDeleteStaleEntryEmbeddingChunksKeepsCurrentChunks(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	insertNoteForEmbeddingTest(t, ctx, s, "projects/test/a.md", "hash-a")

	err := s.UpsertEntryEmbeddings(ctx, []*EntryEmbeddingRow{
		{Path: "projects/test/a.md", ChunkIndex: 0, ContentHash: "hash-a", Model: "test-model", Dimensions: 3, Embedding: []byte{1}},
		{Path: "projects/test/a.md", ChunkIndex: 1, ContentHash: "hash-a", Model: "test-model", Dimensions: 3, Embedding: []byte{2}},
		{Path: "projects/test/a.md", ChunkIndex: 2, ContentHash: "hash-a", Model: "test-model", Dimensions: 3, Embedding: []byte{3}},
	})
	if err != nil {
		t.Fatalf("UpsertEntryEmbeddings failed: %v", err)
	}

	deleted, err := s.DeleteStaleEntryEmbeddingChunks(ctx, "projects/test/a.md", "test-model", []int{0, 2})
	if err != nil {
		t.Fatalf("DeleteStaleEntryEmbeddingChunks failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}

	got, err := s.ListEntryEmbeddingsForSearch(ctx, "test-model", 3)
	if err != nil {
		t.Fatalf("ListEntryEmbeddingsForSearch failed: %v", err)
	}
	if len(got) != 2 || got[0].ChunkIndex != 0 || got[1].ChunkIndex != 2 {
		t.Fatalf("remaining chunks = %+v, want chunks 0 and 2", got)
	}
}

func TestFindMissingOrStaleEntryEmbeddings(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	insertNoteForEmbeddingTest(t, ctx, s, "projects/test/a.md", "hash-a")
	insertNoteForEmbeddingTest(t, ctx, s, "projects/test/b.md", "hash-b")
	insertNoteForEmbeddingTest(t, ctx, s, "projects/test/c.md", "hash-c")

	if _, err := s.UpsertEntryEmbedding(ctx, &EntryEmbeddingRow{Path: "projects/test/a.md", ChunkIndex: 0, ContentHash: "hash-a", Model: "test-model", Dimensions: 3, Embedding: []byte{1}}); err != nil {
		t.Fatalf("upsert current embedding: %v", err)
	}
	if _, err := s.UpsertEntryEmbedding(ctx, &EntryEmbeddingRow{Path: "projects/test/b.md", ChunkIndex: 0, ContentHash: "old-hash", Model: "test-model", Dimensions: 3, Embedding: []byte{2}}); err != nil {
		t.Fatalf("upsert stale embedding: %v", err)
	}

	got, err := s.FindMissingOrStaleEntryEmbeddings(ctx, "test-model", 0)
	if err != nil {
		t.Fatalf("FindMissingOrStaleEntryEmbeddings failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("candidates count = %d, want 2: %+v", len(got), got)
	}
	if got[0].Path != "projects/test/b.md" || got[0].ContentHash != "hash-b" {
		t.Errorf("first candidate = %+v, want stale note b", got[0])
	}
	if got[1].Path != "projects/test/c.md" || got[1].ContentHash != "hash-c" {
		t.Errorf("second candidate = %+v, want missing note c", got[1])
	}
}

func TestEntryEmbeddingsCascadeDeleteWithNote(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	insertNoteForEmbeddingTest(t, ctx, s, "projects/test/a.md", "hash-a")

	if _, err := s.UpsertEntryEmbedding(ctx, &EntryEmbeddingRow{Path: "projects/test/a.md", ChunkIndex: 0, ContentHash: "hash-a", Model: "test-model", Dimensions: 3, Embedding: []byte{1}}); err != nil {
		t.Fatalf("upsert embedding: %v", err)
	}
	if _, err := s.DeleteNote(ctx, "projects/test/a.md"); err != nil {
		t.Fatalf("DeleteNote failed: %v", err)
	}

	var count int
	if err := s.DB().QueryRow("SELECT count(*) FROM entry_embeddings WHERE path = ?", "projects/test/a.md").Scan(&count); err != nil {
		t.Fatalf("count embeddings: %v", err)
	}
	if count != 0 {
		t.Errorf("embedding count after note delete = %d, want 0", count)
	}
}

func TestListEntryEmbeddingsForSearchFiltersByModelAndDimensions(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	insertNoteForEmbeddingTest(t, ctx, s, "projects/test/a.md", "hash-a")
	insertNoteForEmbeddingTest(t, ctx, s, "projects/test/b.md", "hash-b")

	if err := s.UpsertEntryEmbeddings(ctx, []*EntryEmbeddingRow{
		{Path: "projects/test/b.md", ChunkIndex: 0, ContentHash: "hash-b", Model: "test-model", Dimensions: 3, Embedding: []byte{2}},
		{Path: "projects/test/a.md", ChunkIndex: 1, ContentHash: "hash-a", Model: "test-model", Dimensions: 3, Embedding: []byte{1, 1}},
		{Path: "projects/test/a.md", ChunkIndex: 0, ContentHash: "hash-a", Model: "other-model", Dimensions: 3, Embedding: []byte{9}},
		{Path: "projects/test/a.md", ChunkIndex: 0, ContentHash: "hash-a", Model: "test-model", Dimensions: 2, Embedding: []byte{8}},
	}); err != nil {
		t.Fatalf("UpsertEntryEmbeddings failed: %v", err)
	}

	got, err := s.ListEntryEmbeddingsForSearch(ctx, "test-model", 3)
	if err != nil {
		t.Fatalf("ListEntryEmbeddingsForSearch failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("embedding count = %d, want 2: %+v", len(got), got)
	}
	if got[0].Path != "projects/test/a.md" || got[0].ChunkIndex != 1 || got[1].Path != "projects/test/b.md" {
		t.Errorf("embeddings order/filter = %+v, want matching model/dimensions ordered by path/chunk", got)
	}
}

func insertNoteForEmbeddingTest(t *testing.T, ctx context.Context, s *StorageLayer, path string, checksum string) {
	t.Helper()
	note := sampleNote(path, path, path)
	note.Checksum = &checksum
	if _, err := s.InsertNote(ctx, note); err != nil {
		t.Fatalf("InsertNote(%q) failed: %v", path, err)
	}
}

func TestUpsertEntryEmbeddingRejectsMissingNote(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	_, err := s.UpsertEntryEmbedding(ctx, &EntryEmbeddingRow{
		Path:        "projects/test/missing.md",
		ChunkIndex:  0,
		ContentHash: "hash-a",
		Model:       "test-model",
		Dimensions:  3,
		Embedding:   []byte{1, 2, 3},
	})
	if err == nil {
		t.Fatal("expected foreign key error for missing note, got nil")
	}
}

func TestDeleteEntryEmbeddingsCanScopeByModel(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()
	insertNoteForEmbeddingTest(t, ctx, s, "projects/test/a.md", "hash-a")

	if err := s.UpsertEntryEmbeddings(ctx, []*EntryEmbeddingRow{
		{Path: "projects/test/a.md", ChunkIndex: 0, ContentHash: "hash-a", Model: "test-model", Dimensions: 3, Embedding: []byte{1}},
		{Path: "projects/test/a.md", ChunkIndex: 0, ContentHash: "hash-a", Model: "other-model", Dimensions: 3, Embedding: []byte{2}},
	}); err != nil {
		t.Fatalf("UpsertEntryEmbeddings failed: %v", err)
	}

	deleted, err := s.DeleteEntryEmbeddings(ctx, "projects/test/a.md", "test-model")
	if err != nil {
		t.Fatalf("DeleteEntryEmbeddings failed: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}

	missing, err := s.GetEntryEmbedding(ctx, "projects/test/a.md", 0, "test-model")
	if err != nil {
		t.Fatalf("GetEntryEmbedding deleted model failed: %v", err)
	}
	if missing != nil {
		t.Fatalf("deleted embedding still present: %+v", missing)
	}
	remaining, err := s.GetEntryEmbedding(ctx, "projects/test/a.md", 0, "other-model")
	if err != nil {
		t.Fatalf("GetEntryEmbedding remaining model failed: %v", err)
	}
	if remaining == nil {
		t.Fatal("other model embedding was deleted")
	}
}

func TestGetEntryEmbeddingReturnsNilWhenMissing(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	got, err := s.GetEntryEmbedding(ctx, "missing.md", 0, "test-model")
	if err != nil {
		t.Fatalf("GetEntryEmbedding failed: %v", err)
	}
	if got != nil {
		t.Fatalf("GetEntryEmbedding = %+v, want nil", got)
	}
}

package storage

import (
	"context"
	"math"
	"testing"
)

// ---------------------------------------------------------------------------
// Helper: create sample embedding records
// ---------------------------------------------------------------------------

func sampleEmbeddingRecord(noteID int64, chunkIndex int, dims int) EmbeddingRecord {
	vec := make([]float32, dims)
	for i := range vec {
		vec[i] = float32(i) * 0.1
	}

	projectID := "test-project"
	typ := "plan"
	status := "active"
	featureID := "feat-1"
	priority := "high"

	return EmbeddingRecord{
		NoteID:     noteID,
		ChunkIndex: chunkIndex,
		Vector:     vec,
		ProjectID:  &projectID,
		Type:       &typ,
		Status:     &status,
		FeatureID:  &featureID,
		Priority:   &priority,
	}
}

// ---------------------------------------------------------------------------
// UpsertNoteEmbeddings Tests
// ---------------------------------------------------------------------------

func TestUpsertNoteEmbeddings_SingleRecord(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Insert a note first
	note := sampleNote("projects/test/note.md", "abc123", "Test Note")
	inserted, err := store.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("failed to insert note: %v", err)
	}

	// Create embedding record
	rec := sampleEmbeddingRecord(inserted.ID, 0, 384)

	// Upsert embedding
	err = store.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{rec})
	if err != nil {
		t.Fatalf("UpsertNoteEmbeddings failed: %v", err)
	}

	// Verify embedding was stored
	retrieved, err := store.GetNoteEmbedding(ctx, inserted.ID, 0)
	if err != nil {
		t.Fatalf("GetNoteEmbedding failed: %v", err)
	}
	if retrieved == nil {
		t.Fatal("expected embedding, got nil")
	}
	if len(retrieved) != 384 {
		t.Errorf("expected 384 dimensions, got %d", len(retrieved))
	}

	// Verify vector values
	for i, expected := range rec.Vector {
		if math.Abs(float64(retrieved[i]-expected)) > 1e-6 {
			t.Errorf("vector[%d]: expected %f, got %f", i, expected, retrieved[i])
		}
	}

	// Verify metadata was stored
	var projectID, typ, status string
	err = store.db.QueryRowContext(ctx,
		"SELECT project_id, type, status FROM note_embeddings_meta WHERE note_id = ? AND chunk_index = ?",
		inserted.ID, 0,
	).Scan(&projectID, &typ, &status)
	if err != nil {
		t.Fatalf("failed to query metadata: %v", err)
	}
	if projectID != "test-project" {
		t.Errorf("expected project_id 'test-project', got %q", projectID)
	}
	if typ != "plan" {
		t.Errorf("expected type 'plan', got %q", typ)
	}
	if status != "active" {
		t.Errorf("expected status 'active', got %q", status)
	}
}

func TestUpsertNoteEmbeddings_BatchUpsert(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Insert a note
	note := sampleNote("projects/test/batch.md", "xyz789", "Batch Note")
	inserted, err := store.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("failed to insert note: %v", err)
	}

	// Create multiple embedding records (simulating chunks)
	records := []EmbeddingRecord{
		sampleEmbeddingRecord(inserted.ID, 0, 384),
		sampleEmbeddingRecord(inserted.ID, 1, 384),
		sampleEmbeddingRecord(inserted.ID, 2, 384),
	}

	// Batch upsert
	err = store.UpsertNoteEmbeddings(ctx, records)
	if err != nil {
		t.Fatalf("UpsertNoteEmbeddings batch failed: %v", err)
	}

	// Verify all chunks were stored
	for i := 0; i < 3; i++ {
		retrieved, err := store.GetNoteEmbedding(ctx, inserted.ID, i)
		if err != nil {
			t.Fatalf("GetNoteEmbedding chunk %d failed: %v", i, err)
		}
		if retrieved == nil {
			t.Fatalf("expected embedding for chunk %d, got nil", i)
		}
		if len(retrieved) != 384 {
			t.Errorf("chunk %d: expected 384 dimensions, got %d", i, len(retrieved))
		}
	}
}

func TestUpsertNoteEmbeddings_Idempotency(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Insert a note
	note := sampleNote("projects/test/idempotent.md", "idem01", "Idempotent Note")
	inserted, err := store.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("failed to insert note: %v", err)
	}

	// First upsert
	rec1 := sampleEmbeddingRecord(inserted.ID, 0, 384)
	err = store.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{rec1})
	if err != nil {
		t.Fatalf("first upsert failed: %v", err)
	}

	// Second upsert with different vector
	rec2 := sampleEmbeddingRecord(inserted.ID, 0, 384)
	for i := range rec2.Vector {
		rec2.Vector[i] = float32(i) * 0.2 // Different values
	}
	newStatus := "completed"
	rec2.Status = &newStatus

	err = store.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{rec2})
	if err != nil {
		t.Fatalf("second upsert failed: %v", err)
	}

	// Verify vector was updated
	retrieved, err := store.GetNoteEmbedding(ctx, inserted.ID, 0)
	if err != nil {
		t.Fatalf("GetNoteEmbedding failed: %v", err)
	}
	for i, expected := range rec2.Vector {
		if math.Abs(float64(retrieved[i]-expected)) > 1e-6 {
			t.Errorf("vector not updated: vector[%d]: expected %f, got %f", i, expected, retrieved[i])
		}
	}

	// Verify metadata was updated
	var status string
	err = store.db.QueryRowContext(ctx,
		"SELECT status FROM note_embeddings_meta WHERE note_id = ? AND chunk_index = ?",
		inserted.ID, 0,
	).Scan(&status)
	if err != nil {
		t.Fatalf("failed to query metadata: %v", err)
	}
	if status != "completed" {
		t.Errorf("expected status 'completed', got %q", status)
	}

	// Verify only one row exists (not duplicated)
	var count int
	err = store.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM note_embeddings WHERE note_id = ? AND chunk_index = ?",
		inserted.ID, 0,
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count rows: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 row, got %d", count)
	}
}

func TestUpsertNoteEmbeddings_ForeignKeyConstraint(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Try to upsert embedding for non-existent note
	rec := sampleEmbeddingRecord(99999, 0, 384)
	err := store.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{rec})
	if err == nil {
		t.Fatal("expected foreign key constraint error, got nil")
	}
}

func TestUpsertNoteEmbeddings_EmptyVector(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Insert a note
	note := sampleNote("projects/test/empty.md", "empty1", "Empty Vector Note")
	inserted, err := store.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("failed to insert note: %v", err)
	}

	// Create record with empty vector
	rec := sampleEmbeddingRecord(inserted.ID, 0, 0)
	rec.Vector = []float32{}

	err = store.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{rec})
	if err == nil {
		t.Fatal("expected error for empty vector, got nil")
	}
}

func TestUpsertNoteEmbeddings_EmptyBatch(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Upsert empty batch should be no-op
	err := store.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{})
	if err != nil {
		t.Fatalf("expected no error for empty batch, got: %v", err)
	}
}

func TestUpsertNoteEmbeddings_TransactionRollback(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Insert a note
	note := sampleNote("projects/test/rollback.md", "roll01", "Rollback Note")
	inserted, err := store.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("failed to insert note: %v", err)
	}

	// Create batch with one valid and one invalid (non-existent note_id)
	records := []EmbeddingRecord{
		sampleEmbeddingRecord(inserted.ID, 0, 384),
		sampleEmbeddingRecord(99999, 1, 384), // Invalid note_id
	}

	err = store.UpsertNoteEmbeddings(ctx, records)
	if err == nil {
		t.Fatal("expected error due to foreign key violation, got nil")
	}

	// Verify first record was NOT inserted (transaction rolled back)
	retrieved, err := store.GetNoteEmbedding(ctx, inserted.ID, 0)
	if err != nil {
		t.Fatalf("GetNoteEmbedding failed: %v", err)
	}
	if retrieved != nil {
		t.Error("expected no embedding (transaction rolled back), but found one")
	}
}

// ---------------------------------------------------------------------------
// GetNoteEmbedding Tests
// ---------------------------------------------------------------------------

func TestGetNoteEmbedding_NotFound(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Query non-existent embedding
	retrieved, err := store.GetNoteEmbedding(ctx, 99999, 0)
	if err != nil {
		t.Fatalf("expected nil error for not found, got: %v", err)
	}
	if retrieved != nil {
		t.Error("expected nil for non-existent embedding, got vector")
	}
}

// ---------------------------------------------------------------------------
// DeleteNoteEmbeddings Tests
// ---------------------------------------------------------------------------

func TestDeleteNoteEmbeddings_CascadeDelete(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Insert a note
	note := sampleNote("projects/test/cascade.md", "casc01", "Cascade Note")
	inserted, err := store.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("failed to insert note: %v", err)
	}

	// Insert embeddings
	records := []EmbeddingRecord{
		sampleEmbeddingRecord(inserted.ID, 0, 384),
		sampleEmbeddingRecord(inserted.ID, 1, 384),
	}
	err = store.UpsertNoteEmbeddings(ctx, records)
	if err != nil {
		t.Fatalf("UpsertNoteEmbeddings failed: %v", err)
	}

	// Delete embeddings
	err = store.DeleteNoteEmbeddings(ctx, inserted.ID)
	if err != nil {
		t.Fatalf("DeleteNoteEmbeddings failed: %v", err)
	}

	// Verify embeddings were deleted
	retrieved, err := store.GetNoteEmbedding(ctx, inserted.ID, 0)
	if err != nil {
		t.Fatalf("GetNoteEmbedding failed: %v", err)
	}
	if retrieved != nil {
		t.Error("expected embedding to be deleted, but found one")
	}

	// Verify metadata was also deleted (cascade)
	var count int
	err = store.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM note_embeddings_meta WHERE note_id = ?",
		inserted.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count metadata rows: %v", err)
	}
	if count != 0 {
		t.Errorf("expected metadata to be cascade-deleted, found %d rows", count)
	}
}

func TestDeleteNoteEmbeddings_NoteDelete(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Insert a note
	note := sampleNote("projects/test/notedel.md", "notd01", "Note Delete")
	inserted, err := store.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("failed to insert note: %v", err)
	}

	// Insert embeddings
	rec := sampleEmbeddingRecord(inserted.ID, 0, 384)
	err = store.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{rec})
	if err != nil {
		t.Fatalf("UpsertNoteEmbeddings failed: %v", err)
	}

	// Delete the note (should cascade to embeddings)
	deleted, err := store.DeleteNote(ctx, note.Path)
	if err != nil {
		t.Fatalf("DeleteNote failed: %v", err)
	}
	if !deleted {
		t.Fatal("note was not deleted")
	}

	// Verify embeddings were cascade-deleted
	retrieved, err := store.GetNoteEmbedding(ctx, inserted.ID, 0)
	if err != nil {
		t.Fatalf("GetNoteEmbedding failed: %v", err)
	}
	if retrieved != nil {
		t.Error("expected embeddings to be cascade-deleted with note, but found one")
	}

	// Verify metadata was also cascade-deleted
	var count int
	err = store.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM note_embeddings_meta WHERE note_id = ?",
		inserted.ID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("failed to count metadata rows: %v", err)
	}
	if count != 0 {
		t.Errorf("expected metadata to be cascade-deleted, found %d rows", count)
	}
}

// ---------------------------------------------------------------------------
// packFloat32s / unpack Tests
// ---------------------------------------------------------------------------

func TestPackFloat32s_RoundTrip(t *testing.T) {
	original := []float32{0.1, 0.2, 0.3, -0.4, 1.5, -2.7}
	blob := packFloat32s(original)

	// Verify blob size
	expectedSize := len(original) * 4
	if len(blob) != expectedSize {
		t.Fatalf("expected blob size %d, got %d", expectedSize, len(blob))
	}

	// Unpack manually to verify round-trip
	unpacked := make([]float32, len(original))
	for i := range unpacked {
		bits := uint32(blob[i*4]) | uint32(blob[i*4+1])<<8 | uint32(blob[i*4+2])<<16 | uint32(blob[i*4+3])<<24
		unpacked[i] = math.Float32frombits(bits)
	}

	// Verify values match
	for i, expected := range original {
		if math.Abs(float64(unpacked[i]-expected)) > 1e-6 {
			t.Errorf("round-trip failed at index %d: expected %f, got %f", i, expected, unpacked[i])
		}
	}
}

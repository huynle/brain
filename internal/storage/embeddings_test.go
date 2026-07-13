package storage

import (
	"context"
	"fmt"
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

func TestEmbeddingStatus_StaleWhenReadyAttachmentDerivedTextIsNewerThanEmbedding(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	note := sampleNote("projects/test/attachment-stale.md", "attstale", "Attachment Stale")
	inserted, err := store.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("failed to insert note: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE notes SET indexed_at = '2025-01-01 00:00:00' WHERE id = ?`, inserted.ID); err != nil {
		t.Fatalf("set note indexed_at failed: %v", err)
	}

	if err := store.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{sampleEmbeddingRecord(inserted.ID, 0, 384)}); err != nil {
		t.Fatalf("UpsertNoteEmbeddings failed: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE note_embeddings_meta SET embedding_indexed_at = '2025-01-02 00:00:00' WHERE note_id = ?`, inserted.ID); err != nil {
		t.Fatalf("set embedding_indexed_at failed: %v", err)
	}

	attachment, err := store.CreateAttachment(ctx, AttachmentInput{
		Digest:    "sha256:attachment-status-stale",
		Size:      42,
		MediaType: "text/plain",
	})
	if err != nil {
		t.Fatalf("CreateAttachment failed: %v", err)
	}
	if err := store.LinkAttachmentToEntry(ctx, inserted.Path, attachment.ID, "inline"); err != nil {
		t.Fatalf("LinkAttachmentToEntry failed: %v", err)
	}
	if _, err := store.UpsertAttachmentDerived(ctx, AttachmentDerivedInput{
		AttachmentID: attachment.ID,
		Kind:         "text",
		Status:       "ready",
		ContentType:  "text/plain",
		Text:         "new derived text",
	}); err != nil {
		t.Fatalf("UpsertAttachmentDerived failed: %v", err)
	}
	if _, err := store.DB().ExecContext(ctx, `UPDATE attachment_derived SET updated_at = '2025-01-03 00:00:00' WHERE attachment_id = ?`, attachment.ID); err != nil {
		t.Fatalf("set attachment derived updated_at failed: %v", err)
	}

	refetched, err := store.GetNoteByPath(ctx, inserted.Path)
	if err != nil {
		t.Fatalf("GetNoteByPath failed: %v", err)
	}
	status, err := store.EmbeddingStatus(ctx, refetched)
	if err != nil {
		t.Fatalf("EmbeddingStatus failed: %v", err)
	}
	if status != "stale" {
		t.Fatalf("EmbeddingStatus = %q, want stale", status)
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
// SyncNoteEmbeddingMetadata Tests
// ---------------------------------------------------------------------------

func TestSyncNoteEmbeddingMetadata_UpdatesColumnsWithoutTouchingVectors(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	note := sampleNote("projects/test/sync.md", "sync01", "Sync Note")
	inserted, err := store.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("failed to insert note: %v", err)
	}

	records := []EmbeddingRecord{
		sampleEmbeddingRecord(inserted.ID, 0, 8),
		sampleEmbeddingRecord(inserted.ID, 1, 8),
	}
	if err := store.UpsertNoteEmbeddings(ctx, records); err != nil {
		t.Fatalf("UpsertNoteEmbeddings failed: %v", err)
	}

	// Backdate embedding_indexed_at so the sync's bump is observable.
	if _, err := store.db.ExecContext(ctx,
		`UPDATE note_embeddings_meta SET embedding_indexed_at = '2020-01-01 00:00:00' WHERE note_id = ?`,
		inserted.ID,
	); err != nil {
		t.Fatalf("failed to backdate embedding_indexed_at: %v", err)
	}

	// Simulate a metadata-only entry update: same note, new filter values.
	projectID := "other-project"
	typ := "task"
	status := "completed"
	featureID := "feat-2"
	priority := "low"
	inserted.ProjectID = &projectID
	inserted.Type = &typ
	inserted.Status = &status
	inserted.FeatureID = &featureID
	inserted.Priority = &priority

	if err := store.SyncNoteEmbeddingMetadata(ctx, inserted); err != nil {
		t.Fatalf("SyncNoteEmbeddingMetadata failed: %v", err)
	}

	// Every chunk's metadata reflects the new values and a fresh timestamp.
	for chunk := 0; chunk <= 1; chunk++ {
		var gotProject, gotType, gotStatus, gotFeature, gotPriority, gotIndexedAt string
		err := store.db.QueryRowContext(ctx,
			`SELECT project_id, type, status, feature_id, priority, embedding_indexed_at
			 FROM note_embeddings_meta WHERE note_id = ? AND chunk_index = ?`,
			inserted.ID, chunk,
		).Scan(&gotProject, &gotType, &gotStatus, &gotFeature, &gotPriority, &gotIndexedAt)
		if err != nil {
			t.Fatalf("failed to query metadata for chunk %d: %v", chunk, err)
		}
		if gotProject != projectID || gotType != typ || gotStatus != status ||
			gotFeature != featureID || gotPriority != priority {
			t.Errorf("chunk %d metadata = (%q, %q, %q, %q, %q), want (%q, %q, %q, %q, %q)",
				chunk, gotProject, gotType, gotStatus, gotFeature, gotPriority,
				projectID, typ, status, featureID, priority)
		}
		if gotIndexedAt <= "2020-01-01 00:00:00" {
			t.Errorf("chunk %d embedding_indexed_at = %q, expected it to be bumped", chunk, gotIndexedAt)
		}
	}

	// Vectors are untouched.
	for chunk, rec := range records {
		retrieved, err := store.GetNoteEmbedding(ctx, inserted.ID, chunk)
		if err != nil {
			t.Fatalf("GetNoteEmbedding failed for chunk %d: %v", chunk, err)
		}
		if len(retrieved) != len(rec.Vector) {
			t.Fatalf("chunk %d vector length = %d, want %d", chunk, len(retrieved), len(rec.Vector))
		}
		for i := range rec.Vector {
			if math.Abs(float64(retrieved[i]-rec.Vector[i])) > 1e-6 {
				t.Errorf("chunk %d vector[%d] = %f, want %f", chunk, i, retrieved[i], rec.Vector[i])
			}
		}
	}
}

func TestSyncNoteEmbeddingMetadata_NoEmbeddingsIsNoop(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	note := sampleNote("projects/test/sync-none.md", "sync02", "No Embeddings")
	inserted, err := store.InsertNote(ctx, note)
	if err != nil {
		t.Fatalf("failed to insert note: %v", err)
	}

	if err := store.SyncNoteEmbeddingMetadata(ctx, inserted); err != nil {
		t.Fatalf("SyncNoteEmbeddingMetadata on note without embeddings failed: %v", err)
	}
	if err := store.SyncNoteEmbeddingMetadata(ctx, nil); err != nil {
		t.Fatalf("SyncNoteEmbeddingMetadata(nil) failed: %v", err)
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

// ---------------------------------------------------------------------------
// SearchByEmbedding Tests
// ---------------------------------------------------------------------------

func TestSearchByEmbedding_BasicRetrieval(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Insert test notes
	note1 := sampleNote("projects/test/note1.md", "abc001", "Note 1")
	note2 := sampleNote("projects/test/note2.md", "abc002", "Note 2")
	note3 := sampleNote("projects/test/note3.md", "abc003", "Note 3")

	inserted1, _ := store.InsertNote(ctx, note1)
	inserted2, _ := store.InsertNote(ctx, note2)
	inserted3, _ := store.InsertNote(ctx, note3)

	// Create embeddings with different similarity to query
	// Query vector: [1, 0, 0, ...]
	// Note1 vector: [0.9, 0.1, 0, ...] - high similarity
	// Note2 vector: [0.5, 0.5, 0, ...] - medium similarity
	// Note3 vector: [0, 1, 0, ...] - low similarity

	vec1 := make([]float32, 384)
	vec1[0] = 0.9
	vec1[1] = 0.1

	vec2 := make([]float32, 384)
	vec2[0] = 0.5
	vec2[1] = 0.5

	vec3 := make([]float32, 384)
	vec3[0] = 0.0
	vec3[1] = 1.0

	rec1 := sampleEmbeddingRecord(inserted1.ID, 0, 384)
	rec1.Vector = vec1

	rec2 := sampleEmbeddingRecord(inserted2.ID, 0, 384)
	rec2.Vector = vec2

	rec3 := sampleEmbeddingRecord(inserted3.ID, 0, 384)
	rec3.Vector = vec3

	_ = store.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{rec1, rec2, rec3})

	// Query vector
	queryVec := make([]float32, 384)
	queryVec[0] = 1.0

	// Search
	results, err := store.SearchByEmbedding(ctx, queryVec, &EmbeddingSearchOptions{Limit: 3})
	if err != nil {
		t.Fatalf("SearchByEmbedding failed: %v", err)
	}

	// Verify results are returned in descending order of similarity
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Note1 should be first (highest similarity)
	if results[0].ID != inserted1.ID {
		t.Errorf("expected first result to be note1, got note ID %d", results[0].ID)
	}
}

func TestSearchByEmbedding_ProjectFilter(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Insert notes with different projects
	note1 := sampleNote("projects/proj-a/note1.md", "abc001", "Note 1")
	note2 := sampleNote("projects/proj-b/note2.md", "abc002", "Note 2")

	inserted1, _ := store.InsertNote(ctx, note1)
	inserted2, _ := store.InsertNote(ctx, note2)

	// Create embeddings with metadata
	rec1 := sampleEmbeddingRecord(inserted1.ID, 0, 384)
	projectA := "proj-a"
	rec1.ProjectID = &projectA

	rec2 := sampleEmbeddingRecord(inserted2.ID, 0, 384)
	projectB := "proj-b"
	rec2.ProjectID = &projectB

	_ = store.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{rec1, rec2})

	// Query vector
	queryVec := make([]float32, 384)
	queryVec[0] = 1.0

	// Search with project filter
	results, err := store.SearchByEmbedding(ctx, queryVec, &EmbeddingSearchOptions{
		ProjectID: "proj-a",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("SearchByEmbedding failed: %v", err)
	}

	// Should only return note1
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != inserted1.ID {
		t.Errorf("expected note1, got note ID %d", results[0].ID)
	}
}

func TestSearchByEmbedding_TypeFilter(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Insert notes
	note1 := sampleNote("projects/test/note1.md", "abc001", "Note 1")
	note2 := sampleNote("projects/test/note2.md", "abc002", "Note 2")

	inserted1, _ := store.InsertNote(ctx, note1)
	inserted2, _ := store.InsertNote(ctx, note2)

	// Create embeddings with different types
	rec1 := sampleEmbeddingRecord(inserted1.ID, 0, 384)
	typePlan := "plan"
	rec1.Type = &typePlan

	rec2 := sampleEmbeddingRecord(inserted2.ID, 0, 384)
	typeTask := "task"
	rec2.Type = &typeTask

	_ = store.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{rec1, rec2})

	// Query vector
	queryVec := make([]float32, 384)
	queryVec[0] = 1.0

	// Search with type filter
	results, err := store.SearchByEmbedding(ctx, queryVec, &EmbeddingSearchOptions{
		Type:  "plan",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("SearchByEmbedding failed: %v", err)
	}

	// Should only return note1
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != inserted1.ID {
		t.Errorf("expected note1, got note ID %d", results[0].ID)
	}
}

func TestSearchByEmbedding_StatusFilter(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Insert notes
	note1 := sampleNote("projects/test/note1.md", "abc001", "Note 1")
	note2 := sampleNote("projects/test/note2.md", "abc002", "Note 2")

	inserted1, _ := store.InsertNote(ctx, note1)
	inserted2, _ := store.InsertNote(ctx, note2)

	// Create embeddings with different statuses
	rec1 := sampleEmbeddingRecord(inserted1.ID, 0, 384)
	statusActive := "active"
	rec1.Status = &statusActive

	rec2 := sampleEmbeddingRecord(inserted2.ID, 0, 384)
	statusCompleted := "completed"
	rec2.Status = &statusCompleted

	_ = store.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{rec1, rec2})

	// Query vector
	queryVec := make([]float32, 384)
	queryVec[0] = 1.0

	// Search with status filter
	results, err := store.SearchByEmbedding(ctx, queryVec, &EmbeddingSearchOptions{
		Status: "active",
		Limit:  10,
	})
	if err != nil {
		t.Fatalf("SearchByEmbedding failed: %v", err)
	}

	// Should only return note1
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != inserted1.ID {
		t.Errorf("expected note1, got note ID %d", results[0].ID)
	}
}

func TestSearchByEmbedding_MultipleFilters(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Insert notes
	note1 := sampleNote("projects/test/note1.md", "abc001", "Note 1")
	note2 := sampleNote("projects/test/note2.md", "abc002", "Note 2")
	note3 := sampleNote("projects/test/note3.md", "abc003", "Note 3")

	inserted1, _ := store.InsertNote(ctx, note1)
	inserted2, _ := store.InsertNote(ctx, note2)
	inserted3, _ := store.InsertNote(ctx, note3)

	// Create embeddings
	projectA := "proj-a"
	typePlan := "plan"
	statusActive := "active"
	featureFeat1 := "feat-1"
	priorityHigh := "high"

	rec1 := sampleEmbeddingRecord(inserted1.ID, 0, 384)
	rec1.ProjectID = &projectA
	rec1.Type = &typePlan
	rec1.Status = &statusActive
	rec1.FeatureID = &featureFeat1
	rec1.Priority = &priorityHigh

	projectB := "proj-b"
	rec2 := sampleEmbeddingRecord(inserted2.ID, 0, 384)
	rec2.ProjectID = &projectB
	rec2.Type = &typePlan
	rec2.Status = &statusActive

	rec3 := sampleEmbeddingRecord(inserted3.ID, 0, 384)
	rec3.ProjectID = &projectA
	typeTask := "task"
	rec3.Type = &typeTask
	rec3.Status = &statusActive

	_ = store.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{rec1, rec2, rec3})

	// Query vector
	queryVec := make([]float32, 384)
	queryVec[0] = 1.0

	// Search with multiple filters
	results, err := store.SearchByEmbedding(ctx, queryVec, &EmbeddingSearchOptions{
		ProjectID: "proj-a",
		Type:      "plan",
		Status:    "active",
		FeatureID: "feat-1",
		Priority:  "high",
		Limit:     10,
	})
	if err != nil {
		t.Fatalf("SearchByEmbedding failed: %v", err)
	}

	// Should only return note1
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].ID != inserted1.ID {
		t.Errorf("expected note1, got note ID %d", results[0].ID)
	}
}

func TestSearchByEmbedding_Deduplication(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Insert a note
	note1 := sampleNote("projects/test/note1.md", "abc001", "Note 1")
	inserted1, _ := store.InsertNote(ctx, note1)

	// Create multiple chunks for the same note with different similarity scores
	vec1 := make([]float32, 384)
	vec1[0] = 0.9 // High similarity

	vec2 := make([]float32, 384)
	vec2[0] = 0.5 // Medium similarity

	rec1 := sampleEmbeddingRecord(inserted1.ID, 0, 384)
	rec1.Vector = vec1

	rec2 := sampleEmbeddingRecord(inserted1.ID, 1, 384)
	rec2.Vector = vec2

	_ = store.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{rec1, rec2})

	// Query vector
	queryVec := make([]float32, 384)
	queryVec[0] = 1.0

	// Search
	results, err := store.SearchByEmbedding(ctx, queryVec, &EmbeddingSearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("SearchByEmbedding failed: %v", err)
	}

	// Should only return note1 once (deduplicated)
	if len(results) != 1 {
		t.Fatalf("expected 1 result (deduplicated), got %d", len(results))
	}
	if results[0].ID != inserted1.ID {
		t.Errorf("expected note1, got note ID %d", results[0].ID)
	}
}

func TestSearchByEmbedding_EmptyQuery(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Empty query vector
	results, err := store.SearchByEmbedding(ctx, []float32{}, &EmbeddingSearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("SearchByEmbedding failed: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results for empty query, got %d", len(results))
	}
}

func TestSearchByEmbedding_NoMatches(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// No embeddings in database
	queryVec := make([]float32, 384)
	queryVec[0] = 1.0

	results, err := store.SearchByEmbedding(ctx, queryVec, &EmbeddingSearchOptions{Limit: 10})
	if err != nil {
		t.Fatalf("SearchByEmbedding failed: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results when no embeddings exist, got %d", len(results))
	}
}

func TestSearchByEmbedding_LimitRespected(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Insert 10 notes with unique paths
	for i := 0; i < 10; i++ {
		path := fmt.Sprintf("projects/test/note%d.md", i)
		shortID := fmt.Sprintf("abc%03d", i)
		note := sampleNote(path, shortID, fmt.Sprintf("Note %d", i))
		inserted, err := store.InsertNote(ctx, note)
		if err != nil {
			t.Fatalf("failed to insert note %d: %v", i, err)
		}
		rec := sampleEmbeddingRecord(inserted.ID, 0, 384)
		if err := store.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{rec}); err != nil {
			t.Fatalf("failed to upsert embeddings for note %d: %v", i, err)
		}
	}

	// Query vector
	queryVec := make([]float32, 384)
	queryVec[0] = 1.0

	// Search with limit 5
	results, err := store.SearchByEmbedding(ctx, queryVec, &EmbeddingSearchOptions{Limit: 5})
	if err != nil {
		t.Fatalf("SearchByEmbedding failed: %v", err)
	}

	if len(results) != 5 {
		t.Errorf("expected 5 results, got %d", len(results))
	}
}

func TestSearchByEmbedding_DefaultLimit(t *testing.T) {
	ctx := context.Background()
	store := newTestStorage(t)

	// Insert 1 note
	note := sampleNote("projects/test/note.md", "abc001", "Note")
	inserted, _ := store.InsertNote(ctx, note)
	rec := sampleEmbeddingRecord(inserted.ID, 0, 384)
	_ = store.UpsertNoteEmbeddings(ctx, []EmbeddingRecord{rec})

	// Query vector
	queryVec := make([]float32, 384)
	queryVec[0] = 1.0

	// Search with nil options (should use default limit)
	results, err := store.SearchByEmbedding(ctx, queryVec, nil)
	if err != nil {
		t.Fatalf("SearchByEmbedding failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// Cosine Similarity Tests
// ---------------------------------------------------------------------------

func TestCosineSimilarity_Identical(t *testing.T) {
	vec := []float32{1.0, 2.0, 3.0}
	similarity := cosineSimilarity(vec, vec)

	// Identical vectors should have similarity of 1.0
	if math.Abs(similarity-1.0) > 1e-6 {
		t.Errorf("expected similarity 1.0 for identical vectors, got %f", similarity)
	}
}

func TestCosineSimilarity_Orthogonal(t *testing.T) {
	vec1 := []float32{1.0, 0.0, 0.0}
	vec2 := []float32{0.0, 1.0, 0.0}
	similarity := cosineSimilarity(vec1, vec2)

	// Orthogonal vectors should have similarity of 0.0
	if math.Abs(similarity) > 1e-6 {
		t.Errorf("expected similarity 0.0 for orthogonal vectors, got %f", similarity)
	}
}

func TestCosineSimilarity_Opposite(t *testing.T) {
	vec1 := []float32{1.0, 2.0, 3.0}
	vec2 := []float32{-1.0, -2.0, -3.0}
	similarity := cosineSimilarity(vec1, vec2)

	// Opposite vectors should have similarity of -1.0
	if math.Abs(similarity+1.0) > 1e-6 {
		t.Errorf("expected similarity -1.0 for opposite vectors, got %f", similarity)
	}
}

func TestCosineSimilarity_DifferentLength(t *testing.T) {
	vec1 := []float32{1.0, 2.0, 3.0}
	vec2 := []float32{1.0, 2.0}
	similarity := cosineSimilarity(vec1, vec2)

	// Different length vectors should return 0.0
	if similarity != 0.0 {
		t.Errorf("expected similarity 0.0 for different length vectors, got %f", similarity)
	}
}

func TestCosineSimilarity_ZeroVector(t *testing.T) {
	vec1 := []float32{1.0, 2.0, 3.0}
	vec2 := []float32{0.0, 0.0, 0.0}
	similarity := cosineSimilarity(vec1, vec2)

	// Zero vector should return 0.0
	if similarity != 0.0 {
		t.Errorf("expected similarity 0.0 for zero vector, got %f", similarity)
	}
}

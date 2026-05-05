package storage

import (
	"context"
	"database/sql"
	"encoding/binary"
	"fmt"
	"math"
)

// EmbeddingRecord represents a single embedding chunk for a note.
type EmbeddingRecord struct {
	NoteID     int64
	ChunkIndex int
	Vector     []float32 // Will be packed as BLOB

	// Metadata fields
	ProjectID *string
	Type      *string
	Status    *string
	FeatureID *string
	Priority  *string
}

// packFloat32s converts a slice of float32 values into a binary BLOB (little-endian).
func packFloat32s(vec []float32) []byte {
	buf := make([]byte, len(vec)*4)
	for i, v := range vec {
		bits := math.Float32bits(v)
		binary.LittleEndian.PutUint32(buf[i*4:(i+1)*4], bits)
	}
	return buf
}

// UpsertNoteEmbeddings atomically upserts embeddings and metadata for multiple note chunks.
// It wraps all operations in a transaction to ensure consistency.
//
// For each record:
//   - Inserts or replaces the embedding vector in note_embeddings
//   - Inserts or replaces metadata in note_embeddings_meta with embedding_indexed_at = now()
//
// Returns an error if any operation fails (transaction is rolled back automatically).
func (s *StorageLayer) UpsertNoteEmbeddings(ctx context.Context, records []EmbeddingRecord) error {
	if len(records) == 0 {
		return nil
	}

	// Begin transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() // No-op if committed

	// Prepare statements for efficiency
	embeddingStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO note_embeddings (note_id, chunk_index, embedding)
		VALUES (?, ?, ?)
		ON CONFLICT(note_id, chunk_index) DO UPDATE SET
			embedding = excluded.embedding
	`)
	if err != nil {
		return fmt.Errorf("prepare embedding statement: %w", err)
	}
	defer embeddingStmt.Close()

	metaStmt, err := tx.PrepareContext(ctx, `
		INSERT INTO note_embeddings_meta (note_id, chunk_index, project_id, type, status, feature_id, priority, embedding_indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(note_id, chunk_index) DO UPDATE SET
			project_id = excluded.project_id,
			type = excluded.type,
			status = excluded.status,
			feature_id = excluded.feature_id,
			priority = excluded.priority,
			embedding_indexed_at = datetime('now')
	`)
	if err != nil {
		return fmt.Errorf("prepare meta statement: %w", err)
	}
	defer metaStmt.Close()

	// Execute all upserts
	for _, rec := range records {
		// Validate vector is not empty
		if len(rec.Vector) == 0 {
			return fmt.Errorf("embedding vector for note_id=%d chunk_index=%d is empty", rec.NoteID, rec.ChunkIndex)
		}

		// Pack vector as binary blob
		blob := packFloat32s(rec.Vector)

		// Upsert embedding
		if _, err := embeddingStmt.ExecContext(ctx, rec.NoteID, rec.ChunkIndex, blob); err != nil {
			return fmt.Errorf("upsert embedding for note_id=%d chunk_index=%d: %w", rec.NoteID, rec.ChunkIndex, err)
		}

		// Upsert metadata
		if _, err := metaStmt.ExecContext(ctx,
			rec.NoteID, rec.ChunkIndex,
			rec.ProjectID, rec.Type, rec.Status, rec.FeatureID, rec.Priority,
		); err != nil {
			return fmt.Errorf("upsert metadata for note_id=%d chunk_index=%d: %w", rec.NoteID, rec.ChunkIndex, err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// GetNoteEmbedding retrieves a specific embedding by note_id and chunk_index.
// Returns nil, nil if not found.
func (s *StorageLayer) GetNoteEmbedding(ctx context.Context, noteID int64, chunkIndex int) ([]float32, error) {
	var blob []byte
	err := s.db.QueryRowContext(ctx,
		"SELECT embedding FROM note_embeddings WHERE note_id = ? AND chunk_index = ?",
		noteID, chunkIndex,
	).Scan(&blob)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("query embedding: %w", err)
	}

	// Unpack blob to float32 slice
	if len(blob)%4 != 0 {
		return nil, fmt.Errorf("invalid embedding blob size: %d (not divisible by 4)", len(blob))
	}

	vec := make([]float32, len(blob)/4)
	for i := range vec {
		bits := binary.LittleEndian.Uint32(blob[i*4 : (i+1)*4])
		vec[i] = math.Float32frombits(bits)
	}

	return vec, nil
}

// DeleteNoteEmbeddings deletes all embeddings and metadata for a given note_id.
// Both tables must be explicitly deleted since they reference notes, not each other.
func (s *StorageLayer) DeleteNoteEmbeddings(ctx context.Context, noteID int64) error {
	// Begin transaction to ensure both deletes succeed or fail together
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Delete embeddings
	_, err = tx.ExecContext(ctx, "DELETE FROM note_embeddings WHERE note_id = ?", noteID)
	if err != nil {
		return fmt.Errorf("delete note embeddings: %w", err)
	}

	// Delete metadata
	_, err = tx.ExecContext(ctx, "DELETE FROM note_embeddings_meta WHERE note_id = ?", noteID)
	if err != nil {
		return fmt.Errorf("delete note embeddings metadata: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

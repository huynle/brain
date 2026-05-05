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

// unpackFloat32s converts a binary BLOB back into a slice of float32 values (little-endian).
func unpackFloat32s(blob []byte) ([]float32, error) {
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

// cosineSimilarity computes the cosine similarity between two vectors.
// Returns a value in [-1, 1] where 1 means identical direction.
func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) {
		return 0.0
	}

	var dotProduct, normA, normB float64
	for i := range a {
		dotProduct += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}

	if normA == 0 || normB == 0 {
		return 0.0
	}

	return dotProduct / (math.Sqrt(normA) * math.Sqrt(normB))
}

// embeddingMatch represents a candidate match with its similarity score.
type embeddingMatch struct {
	noteID     int64
	chunkIndex int
	score      float64
}

// SearchByEmbedding finds similar notes using cosine similarity over stored embeddings.
// It pre-filters candidates using note_embeddings_meta, loads candidate embeddings,
// computes cosine similarity, and returns the top-K matches deduplicated by note_id.
func (s *StorageLayer) SearchByEmbedding(ctx context.Context, queryVec []float32, opts *EmbeddingSearchOptions) ([]*NoteRow, error) {
	if len(queryVec) == 0 {
		return []*NoteRow{}, nil
	}

	// Apply defaults
	limit := 20
	if opts != nil && opts.Limit > 0 {
		limit = opts.Limit
	}

	// Build query to get candidate note_ids from note_embeddings_meta with filters
	sql := "SELECT DISTINCT m.note_id FROM note_embeddings_meta m"
	var params []interface{}
	var whereClauses []string

	if opts != nil {
		if opts.ProjectID != "" {
			whereClauses = append(whereClauses, "m.project_id = ?")
			params = append(params, opts.ProjectID)
		}
		if opts.Type != "" {
			whereClauses = append(whereClauses, "m.type = ?")
			params = append(params, opts.Type)
		}
		if opts.Status != "" {
			whereClauses = append(whereClauses, "m.status = ?")
			params = append(params, opts.Status)
		}
		if opts.FeatureID != "" {
			whereClauses = append(whereClauses, "m.feature_id = ?")
			params = append(params, opts.FeatureID)
		}
		if opts.Priority != "" {
			whereClauses = append(whereClauses, "m.priority = ?")
			params = append(params, opts.Priority)
		}
		if len(opts.Tags) > 0 {
			// Join with tags table to filter by tags
			sql = "SELECT DISTINCT m.note_id FROM note_embeddings_meta m INNER JOIN tags t ON m.note_id = t.note_id"
			placeholders := make([]string, len(opts.Tags))
			for i, tag := range opts.Tags {
				placeholders[i] = "?"
				params = append(params, tag)
			}
			whereClauses = append(whereClauses, fmt.Sprintf("t.tag IN (%s)", joinStrings(placeholders, ",")))
		}
	}

	if len(whereClauses) > 0 {
		sql += " WHERE " + joinStrings(whereClauses, " AND ")
	}

	// Get candidate note IDs
	rows, err := s.db.QueryContext(ctx, sql, params...)
	if err != nil {
		return nil, fmt.Errorf("query candidate notes: %w", err)
	}

	var candidateNoteIDs []int64
	for rows.Next() {
		var noteID int64
		if err := rows.Scan(&noteID); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scan candidate note_id: %w", err)
		}
		candidateNoteIDs = append(candidateNoteIDs, noteID)
	}
	rows.Close()

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate candidate notes: %w", err)
	}

	if len(candidateNoteIDs) == 0 {
		return []*NoteRow{}, nil
	}

	// Load embeddings for all candidate notes and compute similarity
	var matches []embeddingMatch

	for _, noteID := range candidateNoteIDs {
		// Get all chunks for this note
		chunkRows, err := s.db.QueryContext(ctx,
			"SELECT chunk_index, embedding FROM note_embeddings WHERE note_id = ?",
			noteID,
		)
		if err != nil {
			return nil, fmt.Errorf("query embeddings for note_id=%d: %w", noteID, err)
		}

		for chunkRows.Next() {
			var chunkIndex int
			var blob []byte
			if err := chunkRows.Scan(&chunkIndex, &blob); err != nil {
				chunkRows.Close()
				return nil, fmt.Errorf("scan embedding for note_id=%d: %w", noteID, err)
			}

			// Unpack embedding vector
			vec, err := unpackFloat32s(blob)
			if err != nil {
				chunkRows.Close()
				return nil, fmt.Errorf("unpack embedding for note_id=%d chunk_index=%d: %w", noteID, chunkIndex, err)
			}

			// Compute cosine similarity
			score := cosineSimilarity(queryVec, vec)
			matches = append(matches, embeddingMatch{
				noteID:     noteID,
				chunkIndex: chunkIndex,
				score:      score,
			})
		}
		chunkRows.Close()

		if err := chunkRows.Err(); err != nil {
			return nil, fmt.Errorf("iterate embeddings for note_id=%d: %w", noteID, err)
		}
	}

	// Deduplicate by note_id, keeping the best score per note
	bestScores := make(map[int64]float64)
	for _, match := range matches {
		if existing, ok := bestScores[match.noteID]; !ok || match.score > existing {
			bestScores[match.noteID] = match.score
		}
	}

	// Convert to sorted list
	type noteScore struct {
		noteID int64
		score  float64
	}
	var sortedNotes []noteScore
	for noteID, score := range bestScores {
		sortedNotes = append(sortedNotes, noteScore{noteID: noteID, score: score})
	}

	// Sort by score descending
	for i := 0; i < len(sortedNotes); i++ {
		for j := i + 1; j < len(sortedNotes); j++ {
			if sortedNotes[j].score > sortedNotes[i].score {
				sortedNotes[i], sortedNotes[j] = sortedNotes[j], sortedNotes[i]
			}
		}
	}

	// Limit results
	if len(sortedNotes) > limit {
		sortedNotes = sortedNotes[:limit]
	}

	// Fetch full note records
	if len(sortedNotes) == 0 {
		return []*NoteRow{}, nil
	}

	// Build IN clause for note IDs
	placeholders := make([]string, len(sortedNotes))
	noteIDParams := make([]interface{}, len(sortedNotes))
	for i, ns := range sortedNotes {
		placeholders[i] = "?"
		noteIDParams[i] = ns.noteID
	}

	noteSQL := fmt.Sprintf("SELECT %s FROM notes WHERE id IN (%s)", noteColumns, joinStrings(placeholders, ","))
	noteRows, err := s.db.QueryContext(ctx, noteSQL, noteIDParams...)
	if err != nil {
		return nil, fmt.Errorf("query notes: %w", err)
	}
	defer noteRows.Close()

	notes, err := scanNoteRows(noteRows)
	if err != nil {
		return nil, fmt.Errorf("scan notes: %w", err)
	}

	// Preserve sort order by score
	noteMap := make(map[int64]*NoteRow)
	for _, note := range notes {
		noteMap[note.ID] = note
	}

	result := make([]*NoteRow, 0, len(sortedNotes))
	for _, ns := range sortedNotes {
		if note, ok := noteMap[ns.noteID]; ok {
			result = append(result, note)
		}
	}

	return result, nil
}

// joinStrings joins a slice of strings with a separator.
func joinStrings(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}

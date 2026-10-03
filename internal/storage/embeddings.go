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
func (s *TenantStore) UpsertNoteEmbeddings(ctx context.Context, records []EmbeddingRecord) error {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return nil
	}

	// Begin transaction
	tx, err := beginResilientTx(ctx, s.db, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	// Rolling back an already-committed tx returns sql.ErrTxDone; the
	// commit result above is what callers act on.
	defer func() { _ = tx.Rollback() }()

	// Prepare statements for efficiency
	embeddingSQL := `
		INSERT INTO note_embeddings (note_id, chunk_index, embedding)
		VALUES (?, ?, ?)
		ON CONFLICT(note_id, chunk_index) DO UPDATE SET
			embedding = excluded.embedding
	`
	if scope.owner != "" {
		embeddingSQL = `INSERT INTO note_embeddings (tenant_id, note_id, chunk_index, embedding)
		VALUES (?, ?, ?, ?) ON CONFLICT(tenant_id, note_id, chunk_index) DO UPDATE SET embedding = excluded.embedding`
	}
	embeddingStmt, err := tx.PrepareContext(ctx, embeddingSQL)
	if err != nil {
		return fmt.Errorf("prepare embedding statement: %w", err)
	}
	defer embeddingStmt.Close()

	metaSQL := `
		INSERT INTO note_embeddings_meta (note_id, chunk_index, project_id, type, status, feature_id, priority, embedding_indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(note_id, chunk_index) DO UPDATE SET
			project_id = excluded.project_id,
			type = excluded.type,
			status = excluded.status,
			feature_id = excluded.feature_id,
			priority = excluded.priority,
			embedding_indexed_at = datetime('now')
	`
	if scope.owner != "" {
		metaSQL = `INSERT INTO note_embeddings_meta (tenant_id, note_id, chunk_index, project_id, type, status, feature_id, priority, embedding_indexed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(tenant_id, note_id, chunk_index) DO UPDATE SET
		project_id=excluded.project_id, type=excluded.type, status=excluded.status,
		feature_id=excluded.feature_id, priority=excluded.priority, embedding_indexed_at=datetime('now')`
	}
	metaStmt, err := tx.PrepareContext(ctx, metaSQL)
	if err != nil {
		return fmt.Errorf("prepare meta statement: %w", err)
	}
	defer metaStmt.Close()

	// Execute all upserts
	for _, rec := range records {
		if err := requireOwnedNote(ctx, tx.Tx, scope, rec.NoteID); err != nil {
			return err
		}
		// Validate vector is not empty
		if len(rec.Vector) == 0 {
			return fmt.Errorf("embedding vector for note_id=%d chunk_index=%d is empty", rec.NoteID, rec.ChunkIndex)
		}

		// Pack vector as binary blob
		blob := packFloat32s(rec.Vector)

		// Upsert embedding
		embeddingArgs := []interface{}{rec.NoteID, rec.ChunkIndex, blob}
		metaArgs := []interface{}{rec.NoteID, rec.ChunkIndex, rec.ProjectID, rec.Type, rec.Status, rec.FeatureID, rec.Priority}
		if scope.owner != "" {
			embeddingArgs = append([]interface{}{scope.owner}, embeddingArgs...)
			metaArgs = append([]interface{}{scope.owner}, metaArgs...)
		}
		if _, err := embeddingStmt.ExecContext(ctx, embeddingArgs...); err != nil {
			return fmt.Errorf("upsert embedding for note_id=%d chunk_index=%d: %w", rec.NoteID, rec.ChunkIndex, err)
		}

		// Upsert metadata
		if _, err := metaStmt.ExecContext(ctx, metaArgs...); err != nil {
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
func (s *TenantStore) GetNoteEmbedding(ctx context.Context, noteID int64, chunkIndex int) ([]float32, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	where, args := scope.where("note_id = ? AND chunk_index = ?", noteID, chunkIndex)
	var blob []byte
	err = s.db.QueryRowContext(ctx,
		"SELECT embedding FROM note_embeddings WHERE "+where,
		args...,
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

// EmbeddingStatus reports whether a note has current embeddings.
func (s *TenantStore) EmbeddingStatus(ctx context.Context, note *NoteRow) (string, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return "", err
	}
	if note == nil {
		return "unknown", nil
	}
	noteWhere, noteArgs := scope.where("id = ?", note.ID)
	var indexedAt string
	if err := s.db.QueryRowContext(ctx, "SELECT indexed_at FROM notes WHERE "+noteWhere, noteArgs...).Scan(&indexedAt); err != nil {
		return "", fmt.Errorf("note endpoint not owned: %w", err)
	}
	where, args := scope.where("note_id = ?", note.ID)
	var latest string
	err = s.db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(embedding_indexed_at), '')
		FROM note_embeddings_meta
		WHERE `+where, args...).Scan(&latest)
	if err != nil {
		return "", fmt.Errorf("query embedding status: %w", err)
	}
	if latest == "" {
		return "missing", nil
	}
	if indexedAt != "" && indexedAt > latest {
		return "stale", nil
	}
	var latestReadyAttachmentDerived string
	attachmentQuery := `
		SELECT COALESCE(MAX(ad.updated_at), '')
		FROM entry_attachments ea
		JOIN attachment_derived ad ON ad.attachment_id = ea.attachment_id
		WHERE ea.note_id = ?
		  AND ad.kind = 'text'
		  AND ad.status = 'ready'
		  AND TRIM(ad.text) <> ''
	`
	attachmentArgs := []interface{}{note.ID}
	if scope.owner != "" {
		attachmentQuery += " AND ea.tenant_id = ? AND ad.tenant_id = ea.tenant_id"
		attachmentArgs = append(attachmentArgs, scope.owner)
	}
	err = s.db.QueryRowContext(ctx, attachmentQuery, attachmentArgs...).Scan(&latestReadyAttachmentDerived)
	if err != nil {
		return "", fmt.Errorf("query attachment derived embedding status: %w", err)
	}
	if latestReadyAttachmentDerived != "" && latestReadyAttachmentDerived > latest {
		return "stale", nil
	}
	return "current", nil
}

// SyncNoteEmbeddingMetadata refreshes the filterable metadata columns
// (project_id, type, status, feature_id, priority) on a note's existing
// embedding chunks and bumps embedding_indexed_at, without touching the
// vectors. Used after metadata-only entry updates: the embedded text is
// unchanged, so regenerating vectors would be a wasted round-trip to the
// embedding API, but semantic search pre-filters on these columns and must
// see the new values. No-op for notes that have no embeddings.
func (s *TenantStore) SyncNoteEmbeddingMetadata(ctx context.Context, note *NoteRow) error {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return err
	}
	if note == nil {
		return nil
	}
	tx, err := beginResilientTx(ctx, s.db, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := requireOwnedNote(ctx, tx.Tx, scope, note.ID); err != nil {
		return err
	}
	where, args := scope.where("note_id = ?", note.ID)
	args = append([]interface{}{note.ProjectID, note.Type, note.Status, note.FeatureID, note.Priority}, args...)
	_, err = tx.ExecContext(ctx, `
		UPDATE note_embeddings_meta
		SET project_id = ?, type = ?, status = ?, feature_id = ?, priority = ?,
			embedding_indexed_at = datetime('now')
		WHERE `+where, args...)
	if err != nil {
		return fmt.Errorf("sync note embedding metadata: %w", err)
	}
	return tx.Commit()
}

// DeleteNoteEmbeddings deletes all embeddings and metadata for a given note_id.
// Metadata is deleted first for the v29 exact-chunk FK; v28 requires both deletes.
func (s *TenantStore) DeleteNoteEmbeddings(ctx context.Context, noteID int64) error {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return err
	}
	where, args := scope.where("note_id = ?", noteID)
	// Begin transaction to ensure both deletes succeed or fail together
	tx, err := beginResilientTx(ctx, s.db, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	// Rolling back an already-committed tx returns sql.ErrTxDone; the
	// commit result above is what callers act on.
	defer func() { _ = tx.Rollback() }()

	// Delete metadata first: v29 references the exact vector chunk.
	_, err = tx.ExecContext(ctx, "DELETE FROM note_embeddings_meta WHERE "+where, args...)
	if err != nil {
		return fmt.Errorf("delete note embeddings metadata: %w", err)
	}

	// Delete vectors
	_, err = tx.ExecContext(ctx, "DELETE FROM note_embeddings WHERE "+where, args...)
	if err != nil {
		return fmt.Errorf("delete note embeddings: %w", err)
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
func (s *TenantStore) SearchByEmbedding(ctx context.Context, queryVec []float32, opts *EmbeddingSearchOptions) ([]*NoteRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	if len(queryVec) == 0 {
		return []*NoteRow{}, nil
	}

	// Apply defaults
	limit := 20
	if opts != nil && opts.Limit > 0 {
		limit = opts.Limit
	}

	// Build query to get candidate note_ids from note_embeddings_meta with filters
	sql := "SELECT DISTINCT m.note_id FROM note_embeddings_meta m JOIN notes n ON n.id = m.note_id"
	var params []interface{}
	var whereClauses []string
	if scope.owner != "" {
		whereClauses = append(whereClauses, "m.tenant_id = ? AND n.tenant_id = m.tenant_id")
		params = append(params, scope.owner)
	}

	if opts != nil {
		if opts.ProjectID != "" {
			whereClauses = append(whereClauses, "m.project_id = ?")
			params = append(params, opts.ProjectID)
		} else if clause, scopeParams := projectScopeClause(
			"m.project_id",
			// Global paths come from the same-owner notes join.
			"n.path",
			opts.ProjectIDs, opts.IncludeGlobalPath,
		); clause != "" {
			whereClauses = append(whereClauses, clause)
			params = append(params, scopeParams...)
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
			sql += " INNER JOIN tags t ON m.note_id = t.note_id"
			if scope.owner != "" {
				sql += " AND t.tenant_id = m.tenant_id"
			}
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
		where, args := scope.where("note_id = ?", noteID)
		// Get all chunks for this note
		chunkRows, err := s.db.QueryContext(ctx,
			"SELECT chunk_index, embedding FROM note_embeddings WHERE "+where,
			args...,
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
	if scope.owner != "" {
		noteSQL += " AND tenant_id = ?"
		noteIDParams = append(noteIDParams, scope.owner)
	}
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

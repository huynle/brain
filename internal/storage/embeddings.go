package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/huynle/brain-api/internal/embeddings"
)

const embeddingColumns = `id, path, chunk_index, content_hash, model, dimensions, embedding, created_at, updated_at`

// UpsertEntryEmbedding inserts or replaces one embedding chunk for a note path/model.
func (s *StorageLayer) UpsertEntryEmbedding(ctx context.Context, embedding *EntryEmbeddingRow) (*EntryEmbeddingRow, error) {
	query := `
		INSERT INTO entry_embeddings (path, chunk_index, content_hash, model, dimensions, embedding, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(path, chunk_index, model) DO UPDATE SET
			content_hash = excluded.content_hash,
			dimensions = excluded.dimensions,
			embedding = excluded.embedding,
			updated_at = datetime('now')
	`
	_, err := s.db.ExecContext(ctx, query,
		embedding.Path,
		embedding.ChunkIndex,
		embedding.ContentHash,
		embedding.Model,
		embedding.Dimensions,
		embedding.Embedding,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert entry embedding: %w", err)
	}

	return s.GetEntryEmbedding(ctx, embedding.Path, embedding.ChunkIndex, embedding.Model)
}

// UpsertEntryEmbeddings inserts or replaces multiple embedding chunks atomically.
func (s *StorageLayer) UpsertEntryEmbeddings(ctx context.Context, embeddings []*EntryEmbeddingRow) error {
	if len(embeddings) == 0 {
		return nil
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin embedding upsert: %w", err)
	}
	defer tx.Rollback()

	query := `
		INSERT INTO entry_embeddings (path, chunk_index, content_hash, model, dimensions, embedding, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(path, chunk_index, model) DO UPDATE SET
			content_hash = excluded.content_hash,
			dimensions = excluded.dimensions,
			embedding = excluded.embedding,
			updated_at = datetime('now')
	`
	stmt, err := tx.PrepareContext(ctx, query)
	if err != nil {
		return fmt.Errorf("prepare embedding upsert: %w", err)
	}
	defer stmt.Close()

	for _, embedding := range embeddings {
		_, err := stmt.ExecContext(ctx,
			embedding.Path,
			embedding.ChunkIndex,
			embedding.ContentHash,
			embedding.Model,
			embedding.Dimensions,
			embedding.Embedding,
		)
		if err != nil {
			return fmt.Errorf("upsert entry embedding chunk %d for %q: %w", embedding.ChunkIndex, embedding.Path, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit embedding upsert: %w", err)
	}
	return nil
}

// GetEntryEmbedding loads one embedding chunk by path, chunk index, and model.
func (s *StorageLayer) GetEntryEmbedding(ctx context.Context, path string, chunkIndex int, model string) (*EntryEmbeddingRow, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT "+embeddingColumns+" FROM entry_embeddings WHERE path = ? AND chunk_index = ? AND model = ?",
		path,
		chunkIndex,
		model,
	)
	embedding, err := scanEntryEmbedding(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get entry embedding: %w", err)
	}
	return embedding, nil
}

// DeleteEntryEmbeddings removes all embeddings for a path, optionally scoped to a model.
func (s *StorageLayer) DeleteEntryEmbeddings(ctx context.Context, path string, model string) (int64, error) {
	query := "DELETE FROM entry_embeddings WHERE path = ?"
	args := []interface{}{path}
	if model != "" {
		query += " AND model = ?"
		args = append(args, model)
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("delete entry embeddings: %w", err)
	}
	count, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return count, nil
}

// DeleteStaleEntryEmbeddingChunks removes old chunks for a path/model that are no longer present.
func (s *StorageLayer) DeleteStaleEntryEmbeddingChunks(ctx context.Context, path string, model string, keepChunkIndexes []int) (int64, error) {
	query := "DELETE FROM entry_embeddings WHERE path = ? AND model = ?"
	args := []interface{}{path, model}

	if len(keepChunkIndexes) > 0 {
		placeholders := make([]string, len(keepChunkIndexes))
		for i, chunkIndex := range keepChunkIndexes {
			placeholders[i] = "?"
			args = append(args, chunkIndex)
		}
		query += " AND chunk_index NOT IN (" + strings.Join(placeholders, ", ") + ")"
	}

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("delete stale entry embedding chunks: %w", err)
	}
	count, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return count, nil
}

// SemanticEmbeddingContentHash returns a stable hash of text sent to the embedding provider.
func SemanticEmbeddingContentHash(title, lead, body string) string {
	text := strings.TrimSpace(strings.Join([]string{title, lead, body}, "\n\n"))
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// FindMissingOrStaleEntryEmbeddings returns notes without a current semantic embedding for a model.
func (s *StorageLayer) FindMissingOrStaleEntryEmbeddings(ctx context.Context, model string, args ...int) ([]*EmbeddingCandidate, error) {
	dimensions := 0
	limit := 0
	if len(args) == 1 {
		limit = args[0]
	} else if len(args) >= 2 {
		dimensions = args[0]
		limit = args[1]
	}

	query := `
		SELECT n.path, n.title, COALESCE(n.lead, ''), COALESCE(n.body, '')
		FROM notes n
		ORDER BY n.path
	`
	queryArgs := []interface{}{}
	if limit > 0 {
		query += " LIMIT ?"
		queryArgs = append(queryArgs, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, queryArgs...)
	if err != nil {
		return nil, fmt.Errorf("find missing or stale entry embeddings: %w", err)
	}
	defer rows.Close()

	type noteEmbeddingCandidate struct {
		path        string
		contentHash string
	}
	var notes []noteEmbeddingCandidate

	for rows.Next() {
		var path, title, lead, body string
		if err := rows.Scan(&path, &title, &lead, &body); err != nil {
			return nil, fmt.Errorf("scan embedding candidate: %w", err)
		}
		notes = append(notes, noteEmbeddingCandidate{
			path:        path,
			contentHash: SemanticEmbeddingContentHash(title, lead, body),
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate embedding candidates: %w", err)
	}

	var candidates []*EmbeddingCandidate
	for _, note := range notes {
		current, err := s.hasCurrentEntryEmbedding(ctx, note.path, model, note.contentHash, dimensions)
		if err != nil {
			return nil, err
		}
		if !current {
			candidates = append(candidates, &EmbeddingCandidate{Path: note.path, ContentHash: note.contentHash})
		}
	}
	return candidates, nil
}

func (s *StorageLayer) hasCurrentEntryEmbedding(ctx context.Context, path, model, contentHash string, dimensions int) (bool, error) {
	query := "SELECT 1 FROM entry_embeddings WHERE path = ? AND chunk_index = 0 AND model = ? AND content_hash = ?"
	args := []interface{}{path, model, contentHash}
	if dimensions > 0 {
		query += " AND dimensions = ?"
		args = append(args, dimensions)
	}
	var exists int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check current entry embedding: %w", err)
	}
	return true, nil
}

// ListEntryEmbeddingsForSearch loads vectors for semantic search, ordered by path and chunk.
func (s *StorageLayer) ListEntryEmbeddingsForSearch(ctx context.Context, model string, dimensions int) ([]*EntryEmbeddingRow, error) {
	query := "SELECT " + embeddingColumns + " FROM entry_embeddings WHERE model = ?"
	args := []interface{}{model}
	if dimensions > 0 {
		query += " AND dimensions = ?"
		args = append(args, dimensions)
	}
	query += " ORDER BY path, chunk_index"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list entry embeddings for search: %w", err)
	}
	defer rows.Close()
	return scanEntryEmbeddings(rows)
}

func (s *StorageLayer) searchSemantic(ctx context.Context, limit int, opts *SearchOptions) ([]*NoteRow, error) {
	if opts == nil || opts.SemanticModel == "" {
		return nil, errors.New("semantic search requires an embedding model")
	}
	if len(opts.SemanticVector) == 0 {
		return nil, errors.New("semantic search requires a query embedding")
	}

	sql := "SELECT " + noteColumnsAliased + ", e.embedding FROM entry_embeddings e JOIN notes n ON n.path = e.path WHERE e.model = ? AND e.dimensions = ?"
	params := []interface{}{opts.SemanticModel, len(opts.SemanticVector)}
	sql, params = appendFilters(sql, params, "n", opts)
	sql += " ORDER BY n.path, e.chunk_index"

	rows, err := s.db.QueryContext(ctx, sql, params...)
	if err != nil {
		return nil, fmt.Errorf("semantic search query embeddings: %w", err)
	}
	defer rows.Close()

	type semanticHit struct {
		note  *NoteRow
		score float32
	}
	bestByPath := map[string]semanticHit{}
	for rows.Next() {
		note, encoded, err := scanNoteRowWithEmbedding(rows)
		if err != nil {
			return nil, err
		}
		vector, err := embeddings.DecodeFloat32Vector(encoded)
		if err != nil {
			return nil, fmt.Errorf("semantic search decode embedding for %q: %w", note.Path, err)
		}
		score, err := embeddings.CosineSimilarity(opts.SemanticVector, vector)
		if err != nil {
			return nil, fmt.Errorf("semantic search compare embedding for %q: %w", note.Path, err)
		}
		current, ok := bestByPath[note.Path]
		if !ok || score > current.score {
			bestByPath[note.Path] = semanticHit{note: note, score: score}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("semantic search iterate embeddings: %w", err)
	}
	if len(bestByPath) == 0 {
		return []*NoteRow{}, nil
	}

	hits := make([]semanticHit, 0, len(bestByPath))
	for _, hit := range bestByPath {
		hits = append(hits, hit)
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].score == hits[j].score {
			return hits[i].note.Path < hits[j].note.Path
		}
		return hits[i].score > hits[j].score
	})
	if limit > 0 && len(hits) > limit {
		hits = hits[:limit]
	}

	notes := make([]*NoteRow, 0, len(hits))
	for _, hit := range hits {
		notes = append(notes, hit.note)
	}
	return notes, nil
}

func scanNoteRowWithEmbedding(rows *sql.Rows) (*NoteRow, []byte, error) {
	var note NoteRow
	var embedding []byte
	err := rows.Scan(
		&note.ID,
		&note.Path,
		&note.ShortID,
		&note.Title,
		&note.Lead,
		&note.Body,
		&note.RawContent,
		&note.WordCount,
		&note.Checksum,
		&note.Metadata,
		&note.Type,
		&note.Status,
		&note.Priority,
		&note.ProjectID,
		&note.FeatureID,
		&note.Created,
		&note.Modified,
		&note.IndexedAt,
		&embedding,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("scan semantic search row: %w", err)
	}
	return &note, embedding, nil
}

func scanEntryEmbedding(row *sql.Row) (*EntryEmbeddingRow, error) {
	var embedding EntryEmbeddingRow
	err := row.Scan(
		&embedding.ID,
		&embedding.Path,
		&embedding.ChunkIndex,
		&embedding.ContentHash,
		&embedding.Model,
		&embedding.Dimensions,
		&embedding.Embedding,
		&embedding.CreatedAt,
		&embedding.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &embedding, nil
}

func scanEntryEmbeddings(rows *sql.Rows) ([]*EntryEmbeddingRow, error) {
	var embeddings []*EntryEmbeddingRow
	for rows.Next() {
		var embedding EntryEmbeddingRow
		err := rows.Scan(
			&embedding.ID,
			&embedding.Path,
			&embedding.ChunkIndex,
			&embedding.ContentHash,
			&embedding.Model,
			&embedding.Dimensions,
			&embedding.Embedding,
			&embedding.CreatedAt,
			&embedding.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan entry embedding: %w", err)
		}
		embeddings = append(embeddings, &embedding)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate entry embeddings: %w", err)
	}
	return embeddings, nil
}

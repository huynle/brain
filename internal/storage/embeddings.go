package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
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

// FindMissingOrStaleEntryEmbeddings returns notes whose checksum lacks a matching embedding for a model.
func (s *StorageLayer) FindMissingOrStaleEntryEmbeddings(ctx context.Context, model string, limit int) ([]*EmbeddingCandidate, error) {
	query := `
		SELECT n.path, COALESCE(n.checksum, '')
		FROM notes n
		WHERE NOT EXISTS (
			SELECT 1 FROM entry_embeddings e
			WHERE e.path = n.path
			  AND e.model = ?
			  AND e.content_hash = COALESCE(n.checksum, '')
		)
		ORDER BY n.path
	`
	args := []interface{}{model}
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("find missing or stale entry embeddings: %w", err)
	}
	defer rows.Close()

	var candidates []*EmbeddingCandidate
	for rows.Next() {
		var candidate EmbeddingCandidate
		if err := rows.Scan(&candidate.Path, &candidate.ContentHash); err != nil {
			return nil, fmt.Errorf("scan embedding candidate: %w", err)
		}
		candidates = append(candidates, &candidate)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate embedding candidates: %w", err)
	}
	return candidates, nil
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

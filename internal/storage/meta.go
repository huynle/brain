package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// defaultStaleLimit is the default number of stale entries returned.
const defaultStaleLimit = 50

// RecordAccess records an access to the given path, incrementing the access count.
// Uses UPSERT: inserts with access_count=1 or increments existing.
func (s *StorageLayer) RecordAccess(ctx context.Context, path string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO entry_meta (path, access_count, last_accessed)
		VALUES (?, 1, datetime('now'))
		ON CONFLICT(path) DO UPDATE SET
			access_count = access_count + 1,
			last_accessed = datetime('now')
	`, path)
	if err != nil {
		return fmt.Errorf("record access: %w", err)
	}
	return nil
}

// GetAccessStats retrieves the entry_meta record for the given path.
// Returns nil, nil if not found.
func (s *StorageLayer) GetAccessStats(ctx context.Context, path string) (*EntryMetaRow, error) {
	var m EntryMetaRow
	err := s.db.QueryRowContext(ctx,
		"SELECT path, project_id, access_count, last_accessed, last_verified, created_at FROM entry_meta WHERE path = ?",
		path,
	).Scan(&m.Path, &m.ProjectID, &m.AccessCount, &m.LastAccessed, &m.LastVerified, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get access stats: %w", err)
	}
	return &m, nil
}

// SetVerified marks the given path as verified at the current time.
// Uses UPSERT: inserts or updates last_verified to datetime('now').
func (s *StorageLayer) SetVerified(ctx context.Context, path string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO entry_meta (path, last_verified)
		VALUES (?, datetime('now'))
		ON CONFLICT(path) DO UPDATE SET
			last_verified = datetime('now')
	`, path)
	if err != nil {
		return fmt.Errorf("set verified: %w", err)
	}
	return nil
}

// GetStaleEntries finds notes that have never been verified or were verified more than N days ago.
// Supports optional type filter and limit via StaleOptions. Default limit is 50.
func (s *StorageLayer) GetStaleEntries(ctx context.Context, days int, opts *StaleOptions) ([]*NoteRow, error) {
	query := `SELECT ` + noteColumnsAliased + ` FROM notes n
		LEFT JOIN entry_meta em ON n.path = em.path
		WHERE (em.last_verified IS NULL
		   OR em.last_verified < datetime('now', ?))`
	params := []interface{}{fmt.Sprintf("-%d days", days)}

	if opts != nil && opts.Type != "" {
		query += ` AND n.type = ?`
		params = append(params, opts.Type)
	}

	limit := defaultStaleLimit
	if opts != nil && opts.Limit > 0 {
		limit = opts.Limit
	}
	query += ` LIMIT ?`
	params = append(params, limit)

	rows, err := s.db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, fmt.Errorf("get stale entries: %w", err)
	}
	defer rows.Close()

	notes, err := scanNoteRows(rows)
	if err != nil {
		return nil, fmt.Errorf("get stale entries: %w", err)
	}
	if notes == nil {
		return []*NoteRow{}, nil
	}
	return notes, nil
}

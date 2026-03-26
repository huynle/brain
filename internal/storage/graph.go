package storage

import (
	"context"
	"fmt"
)

// defaultOrphanLimit is the default number of orphans returned.
const defaultOrphanLimit = 50

// GetBacklinks finds notes that link TO the given path.
// Matches via target_id (resolved link) OR target_path (unresolved link).
// Returns DISTINCT notes. Returns a non-nil empty slice if none found.
func (s *StorageLayer) GetBacklinks(ctx context.Context, path string) ([]*NoteRow, error) {
	query := `SELECT DISTINCT ` + noteColumnsAliased + ` FROM notes n
		JOIN links l ON l.source_id = n.id
		WHERE l.target_id = (SELECT id FROM notes WHERE path = ?)
		   OR l.target_path = ?`

	rows, err := s.db.QueryContext(ctx, query, path, path)
	if err != nil {
		return nil, fmt.Errorf("get backlinks: %w", err)
	}
	defer rows.Close()

	notes, err := scanNoteRows(rows)
	if err != nil {
		return nil, fmt.Errorf("get backlinks: %w", err)
	}
	if notes == nil {
		return []*NoteRow{}, nil
	}
	return notes, nil
}

// GetOutlinks finds notes linked BY the given path.
// Only returns resolved links (target_id IS NOT NULL).
// Returns DISTINCT notes. Returns a non-nil empty slice if none found.
func (s *StorageLayer) GetOutlinks(ctx context.Context, path string) ([]*NoteRow, error) {
	query := `SELECT DISTINCT ` + noteColumnsAliased + ` FROM notes n
		JOIN links l ON l.target_id = n.id
		WHERE l.source_id = (SELECT id FROM notes WHERE path = ?)`

	rows, err := s.db.QueryContext(ctx, query, path)
	if err != nil {
		return nil, fmt.Errorf("get outlinks: %w", err)
	}
	defer rows.Close()

	notes, err := scanNoteRows(rows)
	if err != nil {
		return nil, fmt.Errorf("get outlinks: %w", err)
	}
	if notes == nil {
		return []*NoteRow{}, nil
	}
	return notes, nil
}

// GetRelated finds notes sharing link targets (co-citation) with the given path.
// Two notes are related if they both link to the same target_path.
// Excludes the source note itself. Returns a non-nil empty slice if none found.
func (s *StorageLayer) GetRelated(ctx context.Context, path string, limit int) ([]*NoteRow, error) {
	query := `SELECT DISTINCT ` + noteColumnsAliased + ` FROM notes n
		WHERE n.id IN (
			SELECT l2.source_id FROM links l1
			JOIN links l2 ON l1.target_path = l2.target_path
			WHERE l1.source_id = (SELECT id FROM notes WHERE path = ?)
			  AND l2.source_id != l1.source_id
		) AND n.path != ?
		LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, path, path, limit)
	if err != nil {
		return nil, fmt.Errorf("get related: %w", err)
	}
	defer rows.Close()

	notes, err := scanNoteRows(rows)
	if err != nil {
		return nil, fmt.Errorf("get related: %w", err)
	}
	if notes == nil {
		return []*NoteRow{}, nil
	}
	return notes, nil
}

// GetOrphans finds notes with no incoming links (not referenced by any link's target_id).
// Supports optional type filter and limit. Returns a non-nil empty slice if none found.
func (s *StorageLayer) GetOrphans(ctx context.Context, opts *OrphanOptions) ([]*NoteRow, error) {
	query := `SELECT ` + noteColumns + ` FROM notes WHERE id NOT IN (
		SELECT DISTINCT target_id FROM links WHERE target_id IS NOT NULL
	)`
	params := make([]interface{}, 0)

	if opts != nil && opts.Type != "" {
		query += ` AND type = ?`
		params = append(params, opts.Type)
	}

	limit := defaultOrphanLimit
	if opts != nil && opts.Limit > 0 {
		limit = opts.Limit
	}
	query += ` LIMIT ?`
	params = append(params, limit)

	rows, err := s.db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, fmt.Errorf("get orphans: %w", err)
	}
	defer rows.Close()

	notes, err := scanNoteRows(rows)
	if err != nil {
		return nil, fmt.Errorf("get orphans: %w", err)
	}
	if notes == nil {
		return []*NoteRow{}, nil
	}
	return notes, nil
}

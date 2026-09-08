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
func (s *TenantStore) GetBacklinks(ctx context.Context, path string) ([]*NoteRow, error) {
	prefix, args, err := s.graphScope(ctx)
	if err != nil {
		return nil, err
	}
	query := prefix + `SELECT DISTINCT ` + noteColumnsAliased + ` FROM graph_notes n
		JOIN graph_links l ON l.source_id = n.id
		WHERE l.target_id = (SELECT id FROM graph_notes WHERE path = ?)
		   OR l.target_path = ?`

	rows, err := s.db.QueryContext(ctx, query, append(args, path, path)...)
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
func (s *TenantStore) GetOutlinks(ctx context.Context, path string) ([]*NoteRow, error) {
	prefix, args, err := s.graphScope(ctx)
	if err != nil {
		return nil, err
	}
	query := prefix + `SELECT DISTINCT ` + noteColumnsAliased + ` FROM graph_notes n
		JOIN graph_links l ON l.target_id = n.id
		WHERE l.source_id = (SELECT id FROM graph_notes WHERE path = ?)`

	rows, err := s.db.QueryContext(ctx, query, append(args, path)...)
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
// Two notes are related if they link to the same resolved target, or to the
// same raw target_path when unresolved (path hrefs and bare short-ID hrefs to
// the same note co-cite via target_id).
// Excludes the source note itself. Returns a non-nil empty slice if none found.
func (s *TenantStore) GetRelated(ctx context.Context, path string, limit int) ([]*NoteRow, error) {
	prefix, args, err := s.graphScope(ctx)
	if err != nil {
		return nil, err
	}
	query := prefix + `SELECT DISTINCT ` + noteColumnsAliased + ` FROM graph_notes n
		WHERE n.id IN (
			SELECT l2.source_id FROM graph_links l1
			JOIN graph_links l2 ON (
				(l1.target_id IS NOT NULL AND l1.target_id = l2.target_id)
				OR l1.target_path = l2.target_path
			)
			WHERE l1.source_id = (SELECT id FROM graph_notes WHERE path = ?)
			  AND l2.source_id != l1.source_id
		) AND n.path != ?
		LIMIT ?`

	rows, err := s.db.QueryContext(ctx, query, append(args, path, path, limit)...)
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

// orphanPredicate is the legacy stats condition for "no incoming links": a
// note is linked-to if some link resolves to its id OR names its path unresolved.
// GetOrphans uses the same two arms over tenant-filtered graph inputs instead.
//
// Those two queries used to disagree — orphans consulted only target_id — so an
// entry with an unresolved but path-matching inbound link was reported as an
// orphan and simultaneously returned a backlink. Both halves are expressed as
// NOT IN against an indexed column so the check stays cheap on a large brain.
// links.target_path is NOT NULL, so neither subquery can poison the NOT IN.
//
// It applies to `notes` unaliased. Keep stats compatible until its receiver moves.
const orphanPredicate = `id NOT IN (SELECT target_id FROM links WHERE target_id IS NOT NULL)
	AND path NOT IN (SELECT target_path FROM links)`

// GetOrphans finds notes with no incoming links — neither a resolved link
// pointing at the note's id nor an unresolved one naming its path.
// Supports optional type filter and limit. Returns a non-nil empty slice if none found.
func (s *TenantStore) GetOrphans(ctx context.Context, opts *OrphanOptions) ([]*NoteRow, error) {
	prefix, params, err := s.graphScope(ctx)
	if err != nil {
		return nil, err
	}
	query := prefix + `SELECT ` + noteColumns + ` FROM graph_notes WHERE
		id NOT IN (SELECT target_id FROM graph_links WHERE target_id IS NOT NULL)
		AND path NOT IN (SELECT target_path FROM graph_links)`

	if opts != nil && opts.Type != "" {
		query += ` AND type = ?`
		params = append(params, opts.Type)
	}
	if opts != nil && opts.Path != "" {
		query += ` AND path LIKE ? ESCAPE '\'`
		params = append(params, escapeLikePrefix(opts.Path)+"%")
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

// Filter the entire graph before evaluating nested lookups or OR arms. Both
// child ownership and endpoint ownership are required even for damaged rows.
// The legacy orphanPredicate above remains solely for the unmigrated stats plane.
func (s *TenantStore) graphScope(ctx context.Context) (string, []interface{}, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return "", nil, err
	}
	prefix, args := graphContentScope(scope)
	return prefix, args, nil
}

func graphContentScope(scope contentScope) (string, []interface{}) {
	noteWhere, args := scope.where("1=1")
	linkWhere, linkArgs := scope.where("1=1")
	return `WITH graph_notes AS (SELECT * FROM notes WHERE ` + noteWhere + `),
		graph_links AS (SELECT * FROM links WHERE ` + linkWhere + `
		AND source_id IN (SELECT id FROM graph_notes)
		AND (target_id IS NULL OR target_id IN (SELECT id FROM graph_notes))) `,
		append(args, linkArgs...)
}

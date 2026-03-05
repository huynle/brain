package storage

import (
	"context"
	"fmt"
	"strings"
)

// defaultSearchLimit is the default number of results returned by search.
const defaultSearchLimit = 20

// noteColumnsAliased is noteColumns with each column prefixed by "n." for use in JOINs.
const noteColumnsAliased = `n.id, n.path, n.short_id, n.title, n.lead, n.body, n.raw_content, n.word_count, n.checksum, n.metadata, n.type, n.status, n.priority, n.project_id, n.feature_id, n.created, n.modified, n.indexed_at`

// SearchNotes searches notes using the specified strategy.
// Returns empty slice for blank queries. Catches errors gracefully (especially FTS5 syntax errors).
func (s *StorageLayer) SearchNotes(ctx context.Context, query string, opts *SearchOptions) ([]*NoteRow, error) {
	if strings.TrimSpace(query) == "" {
		return []*NoteRow{}, nil
	}

	// Apply defaults.
	strategy := "fts"
	limit := defaultSearchLimit
	if opts != nil {
		if opts.Strategy != "" {
			strategy = opts.Strategy
		}
		if opts.Limit > 0 {
			limit = opts.Limit
		}
	}

	switch strategy {
	case "exact":
		return s.searchExact(ctx, query, limit, opts)
	case "like":
		return s.searchLike(ctx, query, limit, opts)
	case "fts":
		return s.searchFTS(ctx, query, limit, opts)
	default:
		// Unknown strategy falls back to FTS.
		return s.searchFTS(ctx, query, limit, opts)
	}
}

// searchFTS performs FTS5 full-text search with BM25 ranking.
func (s *StorageLayer) searchFTS(ctx context.Context, query string, limit int, opts *SearchOptions) ([]*NoteRow, error) {
	sql := "SELECT " + noteColumnsAliased + " FROM notes n JOIN notes_fts fts ON n.id = fts.rowid WHERE notes_fts MATCH ?"
	params := []interface{}{query}

	sql, params = appendFilters(sql, params, "n", opts)

	sql += " ORDER BY bm25(notes_fts, 10.0, 1.0, 5.0) LIMIT ?"
	params = append(params, limit)

	rows, err := s.db.QueryContext(ctx, sql, params...)
	if err != nil {
		// FTS5 syntax errors are caught gracefully — return empty results.
		return []*NoteRow{}, nil
	}
	defer rows.Close()

	notes, err := scanNoteRows(rows)
	if err != nil {
		// FTS5 errors can also surface during row iteration — catch gracefully.
		return []*NoteRow{}, nil
	}
	if notes == nil {
		return []*NoteRow{}, nil
	}
	return notes, nil
}

// searchExact performs exact title match OR body LIKE substring search.
func (s *StorageLayer) searchExact(ctx context.Context, query string, limit int, opts *SearchOptions) ([]*NoteRow, error) {
	sql := "SELECT " + noteColumns + " FROM notes WHERE (title = ? OR body LIKE ?)"
	params := []interface{}{query, "%" + query + "%"}

	sql, params = appendFilters(sql, params, "", opts)

	sql += " LIMIT ?"
	params = append(params, limit)

	rows, err := s.db.QueryContext(ctx, sql, params...)
	if err != nil {
		return nil, fmt.Errorf("search exact: %w", err)
	}
	defer rows.Close()

	notes, err := scanNoteRows(rows)
	if err != nil {
		return nil, fmt.Errorf("search exact: %w", err)
	}
	if notes == nil {
		return []*NoteRow{}, nil
	}
	return notes, nil
}

// searchLike performs LIKE substring search across title, body, and path.
func (s *StorageLayer) searchLike(ctx context.Context, query string, limit int, opts *SearchOptions) ([]*NoteRow, error) {
	likeQuery := "%" + query + "%"
	sql := "SELECT " + noteColumns + " FROM notes WHERE (title LIKE ? OR body LIKE ? OR path LIKE ?)"
	params := []interface{}{likeQuery, likeQuery, likeQuery}

	sql, params = appendFilters(sql, params, "", opts)

	sql += " LIMIT ?"
	params = append(params, limit)

	rows, err := s.db.QueryContext(ctx, sql, params...)
	if err != nil {
		return nil, fmt.Errorf("search like: %w", err)
	}
	defer rows.Close()

	notes, err := scanNoteRows(rows)
	if err != nil {
		return nil, fmt.Errorf("search like: %w", err)
	}
	if notes == nil {
		return []*NoteRow{}, nil
	}
	return notes, nil
}

// appendFilters adds optional WHERE clauses for PathPrefix, Type, and Status.
// tableAlias is the table alias prefix (e.g. "n" for "n.path"); empty string means no alias.
func appendFilters(sql string, params []interface{}, tableAlias string, opts *SearchOptions) (string, []interface{}) {
	if opts == nil {
		return sql, params
	}

	col := func(name string) string {
		if tableAlias != "" {
			return tableAlias + "." + name
		}
		return name
	}

	if opts.PathPrefix != "" {
		sql += " AND " + col("path") + " LIKE ?"
		params = append(params, opts.PathPrefix+"%")
	}
	if opts.Type != "" {
		sql += " AND " + col("type") + " = ?"
		params = append(params, opts.Type)
	}
	if opts.Status != "" {
		sql += " AND " + col("status") + " = ?"
		params = append(params, opts.Status)
	}

	return sql, params
}

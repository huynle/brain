package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// searchTenant owns one snapshot for catalog, all fallbacks, filtering, full-row
// hydration and attachment merging. No code called inside it may use the pool.
// This path is reached only on privately migrated tenant29/31, not runtime30.
func (s *TenantStore) searchTenant(ctx context.Context, query, strategy string, limit int, opts *SearchOptions) ([]*NoteRow, error) {
	tx, err := beginResilientTx(ctx, s.db, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTenantSearchUnavailable, err)
	}
	defer func() { _ = tx.Rollback() }()
	var version int
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version),0) FROM main.schema_version").Scan(&version); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTenantSearchUnavailable, err)
	}
	if version != 29 && version != successorSchemaVersion {
		return nil, ErrTenantSearchUnavailable
	}
	name, err := tenantSearchTable(ctx, tx.Tx, s.tenantID)
	if err != nil {
		return nil, err
	}
	// Validate even when there are no terms, no rows or no selected filters.
	if strings.TrimSpace(query) == "" {
		return []*NoteRow{}, nil
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	search := tenantSearchSnapshot{tx: tx.Tx, owner: s.tenantID.String(), table: name}
	var notes []*NoteRow
	switch strategy {
	case "match":
		notes, _, err = search.match(ctx, query, limit, opts)
		return notes, err
	case "words":
		return search.words(ctx, query, limit, opts)
	case "attachment":
		return search.attachments(ctx, query, limit, opts)
	case "exact":
		notes, err = search.rows(ctx, " FROM notes n WHERE (n.title = ? OR n.body LIKE ?)", []interface{}{query, "%" + query + "%"}, "n.id", limit, opts)
	case "like":
		like := "%" + query + "%"
		notes, err = search.rows(ctx, " FROM notes n WHERE (n.title LIKE ? OR n.body LIKE ? OR n.path LIKE ?)", []interface{}{like, like, like}, "n.id", limit, opts)
	default:
		if looksLikeFTSExpression(query) {
			notes, _, err = search.match(ctx, query, limit, opts)
			if err != nil && tenantFTSSyntaxError(err) {
				notes, err = search.words(ctx, query, limit, opts)
			}
		} else {
			notes, err = search.words(ctx, query, limit, opts)
		}
	}
	if err != nil {
		return nil, err
	}
	markMatchSource(notes, "entry")
	attachments, err := search.attachments(ctx, query, limit, opts)
	if err != nil {
		return nil, err
	}
	return mergeSearchRows(notes, attachments, limit), nil
}

type tenantSearchSnapshot struct {
	tx           *sql.Tx
	owner, table string
}

func (s tenantSearchSnapshot) filters(from string, args []interface{}, opts *SearchOptions) (string, []interface{}) {
	from += " AND n.tenant_id = ?"
	args = append(args, s.owner)
	return appendSearchFilters(from, args, "n", opts, s.owner)
}

func (s tenantSearchSnapshot) rows(ctx context.Context, from string, args []interface{}, order string, limit int, opts *SearchOptions) ([]*NoteRow, error) {
	from, args = s.filters(from, args, opts)
	return tenantSearchRows(ctx, s.tx, "SELECT DISTINCT "+noteColumnsAliased+from+" ORDER BY "+order+" LIMIT ?", append(args, limit)...)
}

func tenantSearchRows(ctx context.Context, tx *sql.Tx, query string, args ...interface{}) ([]*NoteRow, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTenantSearchUnavailable, err)
	}
	defer rows.Close()
	notes, err := scanNoteRows(rows)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrTenantSearchUnavailable, err)
	}
	if notes == nil {
		notes = []*NoteRow{}
	}
	return notes, nil
}

func (s tenantSearchSnapshot) match(ctx context.Context, expr string, limit int, opts *SearchOptions) ([]*NoteRow, int, error) {
	from, args := s.filters(" FROM "+s.table+" JOIN notes n ON n.id="+s.table+".rowid WHERE "+s.table+" MATCH ?", []interface{}{expr}, opts)
	var count int
	if err := s.tx.QueryRowContext(ctx, "SELECT count(*)"+from, args...).Scan(&count); err != nil {
		// Only a recognized query parse failure may degrade to literal words.
		// Catalog/ownership/table errors always retain the unavailable sentinel.
		if tenantFTSSyntaxError(err) {
			return nil, 0, err
		}
		return nil, 0, fmt.Errorf("%w: %w", ErrTenantSearchUnavailable, err)
	}
	notes, err := tenantSearchRows(ctx, s.tx, "SELECT "+noteColumnsAliased+from+" ORDER BY bm25("+s.table+",10.0,1.0,5.0),n.id LIMIT ?", append(args, limit)...)
	return notes, count, err
}

func (s tenantSearchSnapshot) words(ctx context.Context, query string, limit int, opts *SearchOptions) ([]*NoteRow, error) {
	expr := buildFTSMatchExpr(query, "AND")
	if expr == "" {
		return []*NoteRow{}, nil
	}
	notes, _, err := s.match(ctx, expr, limit, opts)
	if err != nil || len(notes) > 0 || len(ftsWordTokens(query)) < 2 {
		return notes, err
	}
	notes, _, err = s.match(ctx, buildFTSMatchExpr(query, "OR"), limit, opts)
	return notes, err
}

func (s tenantSearchSnapshot) attachments(ctx context.Context, query string, limit int, opts *SearchOptions) ([]*NoteRow, error) {
	from := ` FROM notes n
		JOIN entry_attachments ea ON ea.note_id=n.id AND ea.tenant_id=n.tenant_id
		JOIN attachments a ON a.id=ea.attachment_id AND a.tenant_id=ea.tenant_id
		JOIN attachment_derived ad ON ad.attachment_id=a.id AND ad.tenant_id=a.tenant_id
		WHERE ad.kind='text' AND ad.status='ready' AND LOWER(ad.text) LIKE ?`
	notes, err := s.rows(ctx, from, []interface{}{"%" + strings.ToLower(strings.TrimSpace(query)) + "%"}, "n.modified DESC,n.id DESC", limit, opts)
	markMatchSource(notes, "attachment")
	return notes, err
}

func tenantFTSSyntaxError(err error) bool {
	if err == nil || errors.Is(err, ErrTenantSearchUnavailable) {
		return false
	}
	text := err.Error()
	return strings.Contains(text, "fts5: syntax error") || strings.Contains(text, "unterminated string") || strings.Contains(text, "no such column:") || strings.Contains(text, "unknown special query:")
}

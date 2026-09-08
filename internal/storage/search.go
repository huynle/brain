package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// defaultSearchLimit is the default number of results returned by search.
const defaultSearchLimit = 20

// noteColumnsAliased is noteColumns with each column prefixed by "n." for use in JOINs.
const noteColumnsAliased = `n.id, n.path, n.short_id, n.title, n.lead, n.body, n.raw_content, n.word_count, n.checksum, n.metadata, n.type, n.status, n.priority, n.project_id, n.feature_id, n.created, n.modified, n.indexed_at`

// SearchNotes searches notes using the specified strategy.
// Returns empty slice for blank queries after validating the execution schema.
// Query syntax may degrade gracefully; unavailable tenant search never does.
func (s *TenantStore) SearchNotes(ctx context.Context, query string, opts *SearchOptions) ([]*NoteRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	if scope.owner != "" {
		strategy, limit := "fts", defaultSearchLimit
		if opts != nil {
			// Unknown public strategies must not select internal helper modes.
			switch opts.Strategy {
			case "exact", "like", "fts":
				strategy = opts.Strategy
			}
			if opts.Limit > 0 {
				limit = opts.Limit
			}
		}
		return s.searchTenant(ctx, query, strategy, limit, opts)
	}
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

// ftsOperatorTokens are the bare words FTS5 reads as operators rather than
// as search terms.
var ftsOperatorTokens = map[string]struct{}{
	"AND": {}, "OR": {}, "NOT": {}, "NEAR": {},
}

// looksLikeFTSExpression reports whether a query appears to be deliberate
// FTS5 syntax rather than natural language.
//
// Power users write `foo OR bar`, `"exact phrase"`, and `prefix*`, and
// that must keep working. Everything else is treated as a bag of words
// and quoted, because a raw natural-language query is a syntax hazard:
// FTS5 reads `:` as a column filter, so the entirely reasonable query
// `once_per: cooldown` becomes `no such column: once_per`.
func looksLikeFTSExpression(query string) bool {
	if strings.ContainsAny(query, `"*()^`) {
		return true
	}
	for _, token := range strings.Fields(query) {
		if _, ok := ftsOperatorTokens[token]; ok {
			return true
		}
		if strings.HasPrefix(token, "NEAR(") {
			return true
		}
	}
	return false
}

// ftsWordTokens splits a query into search terms on non-word characters,
// keeping underscores so identifiers like once_per survive intact.
//
// Splitting on punctuation rather than whitespace matters: quoting an
// unsplit token turns it into an FTS5 *phrase*, so `Go/performance` would
// require the two words to be adjacent. Someone typing that wants both
// words, not adjacency — and anyone who genuinely wants a phrase writes
// quotes, which routes to the passthrough path instead.
func ftsWordTokens(query string) []string {
	return strings.FieldsFunc(query, func(r rune) bool {
		if r == '_' {
			return false
		}
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// buildFTSMatchExpr turns a natural-language query into a safe FTS5 MATCH
// expression by quoting each token and joining them with op ("AND"/"OR").
//
// Quoting makes every token a literal, so punctuation an agent naturally
// types — colons, slashes, dots, parens — can no longer be parsed as FTS5
// syntax. Returns "" when the query has no usable tokens.
func buildFTSMatchExpr(query, op string) string {
	tokens := ftsWordTokens(query)
	quoted := make([]string, 0, len(tokens))
	for _, token := range tokens {
		// Escape embedded quotes per FTS5 string rules ("" is a literal ").
		quoted = append(quoted, `"`+strings.ReplaceAll(token, `"`, `""`)+`"`)
	}
	if len(quoted) == 0 {
		return ""
	}
	return strings.Join(quoted, " "+op+" ")
}

// searchFTS performs FTS5 full-text search with BM25 ranking.
//
// Two behaviours here exist because the naive version failed silently in
// exactly the cases agents hit most:
//
//  1. The query is tokenized and quoted unless it looks like deliberate
//     FTS5 syntax (see looksLikeFTSExpression).
//  2. FTS5 joins bare terms with implicit AND, so a long descriptive
//     query shrinks monotonically toward zero — every added word can only
//     remove results. When the AND pass finds nothing and more than one
//     token was supplied, retry with OR. BM25 still ranks documents
//     matching more terms above those matching one, so precision is kept
//     where it exists and recall is the fallback rather than silence.
//
// Legacy syntax errors still degrade gracefully — TestSearchQuality_
// FTSSyntaxErrorReturnsEmpty pins that contract. But
// "gracefully empty" was doing too much work: a malformed *expression* and
// a perfectly ordinary phrase both ended in silence. Now a deliberate
// expression that fails to parse degrades to a literal word search rather
// than to nothing, and an ordinary phrase can no longer fail to parse at
// all.
func (s *TenantStore) searchFTS(ctx context.Context, query string, limit int, opts *SearchOptions) ([]*NoteRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	if scope.owner != "" {
		return s.searchTenant(ctx, query, "fts", limit, opts)
	}
	var notes []*NoteRow

	if looksLikeFTSExpression(query) {
		raw, err := s.ftsMatch(ctx, query, limit, opts)
		if err == nil {
			notes = raw
		} else if errors.Is(err, ErrTenantSearchUnavailable) {
			return nil, err
		} else {
			// The user meant it as an expression and it did not parse.
			// Fall back to reading it as words — `"unclosed quote` almost
			// certainly means the words, not a syntax error.
			notes, err = s.ftsMatchWordsResult(ctx, query, limit, opts)
			if err != nil {
				return nil, err
			}
		}
	} else {
		notes, err = s.ftsMatchWordsResult(ctx, query, limit, opts)
		if err != nil {
			return nil, err
		}
	}

	markMatchSource(notes, "entry")

	// The attachment path is a LIKE substring match, so it takes the raw
	// query — FTS5 syntax rules do not apply to it.
	attachmentMatches, err := s.searchAttachmentDerivedText(ctx, query, limit, opts)
	if err != nil {
		return nil, err
	}
	return mergeSearchRows(notes, attachmentMatches, limit), nil
}

// ftsMatchWords searches for the query as a bag of quoted words: all terms
// first, then any term if that found nothing.
//
// FTS5 joins bare terms with implicit AND, so a long descriptive query
// shrinks monotonically toward zero — every word an agent adds can only
// remove results, and agents write descriptive queries. The OR retry makes
// recall the fallback instead of silence. BM25 still ranks documents
// matching more terms above those matching one, so precision is preserved
// wherever it actually exists.
//
// Never returns an error: quoting makes a syntax error very nearly
// unreachable, and if one happens anyway the caller's contract is an empty
// result rather than a failure.
func (s *TenantStore) ftsMatchWords(ctx context.Context, query string, limit int, opts *SearchOptions) []*NoteRow {
	// Legacy signature retained. Production v29 fallbacks use the error-returning
	// transaction path, never this compatibility helper's error-to-empty contract.
	notes, _ := s.ftsMatchWordsResult(ctx, query, limit, opts)
	return notes
}

// Error-preserving path for every production fallback, including a schema
// transition observed between the legacy routing check and a MATCH call.
func (s *TenantStore) ftsMatchWordsResult(ctx context.Context, query string, limit int, opts *SearchOptions) ([]*NoteRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	if scope.owner != "" {
		return s.searchTenant(ctx, query, "words", limit, opts)
	}
	andExpr := buildFTSMatchExpr(query, "AND")
	if andExpr == "" {
		return []*NoteRow{}, nil
	}

	notes, err := s.ftsMatch(ctx, andExpr, limit, opts)
	if errors.Is(err, ErrTenantSearchUnavailable) {
		return nil, err
	}
	if err != nil {
		return []*NoteRow{}, nil
	}
	if len(notes) > 0 || len(ftsWordTokens(query)) < 2 {
		return notes, nil
	}

	orNotes, err := s.ftsMatch(ctx, buildFTSMatchExpr(query, "OR"), limit, opts)
	if errors.Is(err, ErrTenantSearchUnavailable) {
		return nil, err
	}
	if err != nil {
		return notes, nil
	}
	return orNotes, nil
}

// ftsMatch runs one FTS5 MATCH query and returns the ranked rows.
func (s *TenantStore) ftsMatch(ctx context.Context, matchExpr string, limit int, opts *SearchOptions) ([]*NoteRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	if scope.owner != "" {
		return s.searchTenant(ctx, matchExpr, "match", limit, opts)
	}
	sql := "SELECT " + noteColumnsAliased + " FROM notes n JOIN notes_fts fts ON n.id = fts.rowid WHERE notes_fts MATCH ?"
	params := []interface{}{matchExpr}

	sql, params = appendFilters(sql, params, "n", opts)

	sql += " ORDER BY bm25(notes_fts, 10.0, 1.0, 5.0) LIMIT ?"
	params = append(params, limit)

	rows, err := s.db.QueryContext(ctx, sql, params...)
	if err != nil {
		return nil, fmt.Errorf("fts search %q: %w", matchExpr, err)
	}
	defer rows.Close()

	notes, err := scanNoteRows(rows)
	if err != nil {
		return nil, fmt.Errorf("fts search %q: %w", matchExpr, err)
	}
	if notes == nil {
		notes = []*NoteRow{}
	}
	return notes, nil
}

// searchExact performs exact title match OR body LIKE substring search.
func (s *TenantStore) searchExact(ctx context.Context, query string, limit int, opts *SearchOptions) ([]*NoteRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	if scope.owner != "" {
		return s.searchTenant(ctx, query, "exact", limit, opts)
	}
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
		notes = []*NoteRow{}
	}
	markMatchSource(notes, "entry")

	attachmentMatches, err := s.searchAttachmentDerivedText(ctx, query, limit, opts)
	if err != nil {
		return nil, err
	}
	return mergeSearchRows(notes, attachmentMatches, limit), nil
}

// searchLike performs LIKE substring search across title, body, and path.
func (s *TenantStore) searchLike(ctx context.Context, query string, limit int, opts *SearchOptions) ([]*NoteRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	if scope.owner != "" {
		return s.searchTenant(ctx, query, "like", limit, opts)
	}
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
		notes = []*NoteRow{}
	}
	markMatchSource(notes, "entry")

	attachmentMatches, err := s.searchAttachmentDerivedText(ctx, query, limit, opts)
	if err != nil {
		return nil, err
	}
	return mergeSearchRows(notes, attachmentMatches, limit), nil
}

func (s *TenantStore) searchAttachmentDerivedText(ctx context.Context, query string, limit int, opts *SearchOptions) ([]*NoteRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	if scope.owner != "" {
		return s.searchTenant(ctx, query, "attachment", limit, opts)
	}
	likeQuery := "%" + strings.ToLower(strings.TrimSpace(query)) + "%"
	if likeQuery == "%%" {
		return []*NoteRow{}, nil
	}

	sql := "SELECT DISTINCT " + noteColumnsAliased + `
		FROM notes n
		JOIN entry_attachments ea ON ea.note_id = n.id
		JOIN attachment_derived ad ON ad.attachment_id = ea.attachment_id
		WHERE ad.kind = 'text'
			AND ad.status = 'ready'
			AND LOWER(ad.text) LIKE ?`
	params := []interface{}{likeQuery}

	sql, params = appendFilters(sql, params, "n", opts)

	sql += " ORDER BY n.modified DESC, n.id DESC LIMIT ?"
	params = append(params, limit)

	rows, err := s.db.QueryContext(ctx, sql, params...)
	if err != nil {
		return nil, fmt.Errorf("search attachment derived text: %w", err)
	}
	defer rows.Close()

	notes, err := scanNoteRows(rows)
	if err != nil {
		return nil, fmt.Errorf("search attachment derived text: %w", err)
	}
	if notes == nil {
		notes = []*NoteRow{}
	}
	markMatchSource(notes, "attachment")
	return notes, nil
}

func markMatchSource(rows []*NoteRow, source string) {
	for _, row := range rows {
		row.MatchSource = source
	}
}

func mergeSearchRows(primary, secondary []*NoteRow, limit int) []*NoteRow {
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	merged := make([]*NoteRow, 0, min(limit, len(primary)+len(secondary)))
	seen := make(map[string]bool, len(primary)+len(secondary))
	for _, group := range [][]*NoteRow{primary, secondary} {
		for _, row := range group {
			if len(merged) >= limit {
				return merged
			}
			if seen[row.Path] {
				continue
			}
			seen[row.Path] = true
			merged = append(merged, row)
		}
	}
	return merged
}

// appendFilters adds optional WHERE clauses for PathPrefix, Type, and Status.
// tableAlias is the table alias prefix (e.g. "n" for "n.path"); empty string means no alias.
func appendFilters(sql string, params []interface{}, tableAlias string, opts *SearchOptions) (string, []interface{}) {
	return appendSearchFilters(sql, params, tableAlias, opts, "")
}

// owner is nonempty only on the validated v29 path. Keep the v28 wrapper for
// unmoved callers; never infer an owner from optional project/tag filters.
func appendSearchFilters(sql string, params []interface{}, tableAlias string, opts *SearchOptions, owner string) (string, []interface{}) {
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
	if opts.ProjectID != "" {
		sql += " AND " + col("project_id") + " = ?"
		params = append(params, opts.ProjectID)
	} else if clause, scopeParams := projectScopeClause(
		col("project_id"), col("path"), opts.ProjectIDs, opts.IncludeGlobalPath,
	); clause != "" {
		sql += " AND " + clause
		params = append(params, scopeParams...)
	}
	if opts.FeatureID != "" {
		sql += " AND " + col("feature_id") + " = ?"
		params = append(params, opts.FeatureID)
	}
	if opts.Priority != "" {
		sql += " AND " + col("priority") + " = ?"
		params = append(params, opts.Priority)
	}
	if len(opts.Tags) > 0 {
		tagWhere := ""
		if owner != "" {
			tagWhere = "tenant_id = ? AND "
			params = append(params, owner)
		}
		placeholders := make([]string, len(opts.Tags))
		for i := range opts.Tags {
			placeholders[i] = "?"
			params = append(params, opts.Tags[i])
		}
		idCol := col("id")
		sql += fmt.Sprintf(" AND %s IN (SELECT note_id FROM tags WHERE %stag IN (%s) GROUP BY note_id HAVING COUNT(DISTINCT tag) = ?)",
			idCol, tagWhere, strings.Join(placeholders, ","))
		params = append(params, len(opts.Tags))
	}

	return sql, params
}

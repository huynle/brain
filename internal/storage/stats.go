package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// GetStats returns aggregate storage statistics.
// Supports optional path prefix filters via StatsOptions.
func (s *StorageLayer) GetStats(ctx context.Context, opts *StatsOptions) (*Stats, error) {
	// One predicate reused by all five queries below. It is rendered against
	// a bare `path` and against the aliased `n.path` of the stale join, so
	// build it twice from the same prefix set rather than string-patching.
	prefixes := statsPrefixes(opts)
	pathPred, pathParam := pathPrefixClause("path", prefixes)
	stalePred, _ := pathPrefixClause("n.path", prefixes)

	pathFilter := ""
	if pathPred != "" {
		pathFilter = " WHERE " + pathPred
	}

	// 1. Total notes count.
	var totalNotes int
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM notes"+pathFilter, pathParam...,
	).Scan(&totalNotes)
	if err != nil {
		return nil, fmt.Errorf("get stats total: %w", err)
	}

	// 2. Count by type (GROUP BY type).
	byType := make(map[string]int)
	typeRows, err := s.db.QueryContext(ctx,
		"SELECT type, COUNT(*) FROM notes"+pathFilter+" GROUP BY type", pathParam...,
	)
	if err != nil {
		return nil, fmt.Errorf("get stats by type: %w", err)
	}
	defer typeRows.Close()

	for typeRows.Next() {
		var typ sql.NullString
		var count int
		if err := typeRows.Scan(&typ, &count); err != nil {
			return nil, fmt.Errorf("scan type row: %w", err)
		}
		key := "untyped"
		if typ.Valid {
			key = typ.String
		}
		byType[key] = count
	}
	if err := typeRows.Err(); err != nil {
		return nil, fmt.Errorf("iterate type rows: %w", err)
	}

	// 3. Orphan count (notes with no incoming links).
	// Shares orphanPredicate with GetOrphans — the two used to carry separate
	// copies of this condition and drifted apart.
	orphanQuery := `SELECT COUNT(*) FROM notes WHERE ` + orphanPredicate
	if pathPred != "" {
		orphanQuery += " AND " + pathPred
	}
	var orphanCount int
	err = s.db.QueryRowContext(ctx, orphanQuery, pathParam...).Scan(&orphanCount)
	if err != nil {
		return nil, fmt.Errorf("get stats orphans: %w", err)
	}

	// 4. Tracked count (entries in entry_meta).
	trackedQuery := "SELECT COUNT(*) FROM entry_meta"
	if pathPred != "" {
		trackedQuery += " WHERE " + pathPred
	}
	var trackedCount int
	err = s.db.QueryRowContext(ctx, trackedQuery, pathParam...).Scan(&trackedCount)
	if err != nil {
		return nil, fmt.Errorf("get stats tracked: %w", err)
	}

	// 5. Stale count (never verified or verified > 30 days ago).
	staleQuery := `SELECT COUNT(*) FROM notes n
		LEFT JOIN entry_meta em ON n.path = em.path
		WHERE (em.last_verified IS NULL OR em.last_verified < datetime('now', '-30 days'))`
	if stalePred != "" {
		staleQuery += " AND " + stalePred
	}
	var staleCount int
	err = s.db.QueryRowContext(ctx, staleQuery, pathParam...).Scan(&staleCount)
	if err != nil {
		return nil, fmt.Errorf("get stats stale: %w", err)
	}

	return &Stats{
		TotalNotes:   totalNotes,
		ByType:       byType,
		OrphanCount:  orphanCount,
		TrackedCount: trackedCount,
		StaleCount:   staleCount,
	}, nil
}

// statsPrefixes resolves StatsOptions to the set of path prefixes to count
// under. Path (one prefix) wins over Paths (several) so the long-standing
// single-project callers keep their exact behavior.
func statsPrefixes(opts *StatsOptions) []string {
	if opts == nil {
		return nil
	}
	if opts.Path != "" {
		return []string{opts.Path}
	}
	out := make([]string, 0, len(opts.Paths))
	for _, p := range opts.Paths {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// pathPrefixClause renders `col LIKE ?` OR-ed across every prefix, plus the
// matching params. Returns ("", nil) for an empty prefix set — no filter.
func pathPrefixClause(col string, prefixes []string) (string, []interface{}) {
	if len(prefixes) == 0 {
		return "", nil
	}
	clauses := make([]string, len(prefixes))
	params := make([]interface{}, len(prefixes))
	for i, p := range prefixes {
		clauses[i] = col + " LIKE ?"
		params[i] = p + "%"
	}
	if len(clauses) == 1 {
		return clauses[0], params
	}
	return "(" + strings.Join(clauses, " OR ") + ")", params
}

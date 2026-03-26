package storage

import (
	"context"
	"database/sql"
	"fmt"
)

// GetStats returns aggregate storage statistics.
// Supports optional path prefix filter via StatsOptions.
func (s *StorageLayer) GetStats(ctx context.Context, opts *StatsOptions) (*Stats, error) {
	pathFilter := ""
	var pathParam []interface{}
	if opts != nil && opts.Path != "" {
		pathFilter = " WHERE path LIKE ?"
		pathParam = []interface{}{opts.Path + "%"}
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
	orphanQuery := `SELECT COUNT(*) FROM notes WHERE id NOT IN (
		SELECT DISTINCT target_id FROM links WHERE target_id IS NOT NULL
	)`
	if opts != nil && opts.Path != "" {
		orphanQuery += " AND path LIKE ?"
	}
	var orphanCount int
	err = s.db.QueryRowContext(ctx, orphanQuery, pathParam...).Scan(&orphanCount)
	if err != nil {
		return nil, fmt.Errorf("get stats orphans: %w", err)
	}

	// 4. Tracked count (entries in entry_meta).
	trackedQuery := "SELECT COUNT(*) FROM entry_meta"
	if opts != nil && opts.Path != "" {
		trackedQuery += " WHERE path LIKE ?"
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
	if opts != nil && opts.Path != "" {
		staleQuery += " AND n.path LIKE ?"
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

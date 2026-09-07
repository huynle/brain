package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/huynle/brain-api/internal/tenant"
)

// ErrTenantSearchUnavailable must survive receiver error handling. In particular,
// do not feed it into the legacy global search's error-to-empty/fallback paths.
var ErrTenantSearchUnavailable = errors.New("tenant search unavailable")

type tenantFTSHit struct {
	ID                   int64
	Title, Path, Snippet string
	Score                float64
}

func queryTenantFTS(ctx context.Context, tx *sql.Tx, owner tenant.ID, match string, limit int) ([]tenantFTSHit, int, error) {
	if ctx == nil || tx == nil || !owner.Valid() {
		return nil, 0, ErrTenantSearchUnavailable
	}
	// *sql.Tx is deliberate: mapping, catalog, count and hits share a snapshot.
	// An already-reserved single-connection transaction must never call the pool.
	mapping, err := checkTenantSearchCatalog(tx)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrTenantSearchUnavailable, err)
	}
	name, err := tenantFTSName(mapping[owner.String()])
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrTenantSearchUnavailable, err)
	}
	if limit <= 0 {
		limit = defaultSearchLimit
	}
	from := " FROM " + name + " JOIN notes n ON n.id=" + name + ".rowid AND n.tenant_id=? WHERE " + name + " MATCH ?"
	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*)"+from, owner.String(), match).Scan(&count); err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrTenantSearchUnavailable, err)
	}
	rows, err := tx.QueryContext(ctx, "SELECT n.id,coalesce(n.title,''),n.path,coalesce(snippet("+name+",1,'<b>','</b>','...',32),''),bm25("+name+",10.0,1.0,5.0)"+from+" ORDER BY bm25("+name+",10.0,1.0,5.0),n.id LIMIT ?", owner.String(), match, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrTenantSearchUnavailable, err)
	}
	defer rows.Close()
	hits := []tenantFTSHit{}
	for rows.Next() {
		var hit tenantFTSHit
		if err := rows.Scan(&hit.ID, &hit.Title, &hit.Path, &hit.Snippet, &hit.Score); err != nil {
			return nil, 0, fmt.Errorf("%w: %w", ErrTenantSearchUnavailable, err)
		}
		hits = append(hits, hit)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("%w: %w", ErrTenantSearchUnavailable, err)
	}
	return hits, count, nil
}

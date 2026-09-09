package storage

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/huynle/brain-api/internal/tenantfs"
)

const createTenantRootsTable = `CREATE TABLE IF NOT EXISTS tenant_roots (
 tenant_id TEXT PRIMARY KEY NOT NULL CHECK(length(tenant_id)>0),
 brain_root TEXT NOT NULL CHECK(length(brain_root)>0),
 blob_root TEXT NOT NULL CHECK(length(blob_root)>0),
 brain_absolute TEXT NOT NULL,
 blob_absolute TEXT NOT NULL,
 brain_canonical TEXT NOT NULL,
 blob_canonical TEXT NOT NULL,
 layout TEXT NOT NULL CHECK ((tenant_id='local' AND layout='legacy') OR (tenant_id!='local' AND layout='tenant-sha256'))
);`

type rootQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

func listTenantRoots(ctx context.Context, q rootQuerier) ([]tenantfs.Mapping, error) {
	rows, err := q.QueryContext(ctx, `SELECT tenant_id,brain_root,blob_root,brain_absolute,blob_absolute,brain_canonical,blob_canonical,layout FROM tenant_roots ORDER BY tenant_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []tenantfs.Mapping{}
	for rows.Next() {
		var m tenantfs.Mapping
		if err := rows.Scan(&m.ID, &m.BrainRoot, &m.BlobRoot, &m.BrainAbsolute, &m.BlobAbsolute, &m.BrainCanonical, &m.BlobCanonical, &m.Layout); err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// ListTenantRoots returns a complete authoritative snapshot, never content-index
// derived data. Reindexing must not delete this operator-owned configuration.
func (s *StorageLayer) ListTenantRoots(ctx context.Context) ([]tenantfs.Mapping, error) {
	return listTenantRoots(ctx, s.db)
}

// RegisterTenantRoots serializes check-and-insert across shared SQLite connections
// and processes. No replacement/delete API: changing ownership needs a separate
// fenced lifecycle migration. The validator is mandatory trusted policy.
func (s *StorageLayer) RegisterTenantRoots(ctx context.Context, m tenantfs.Mapping, validate func([]tenantfs.Mapping) error) error {
	if !m.ID.Valid() || validate == nil {
		return fmt.Errorf("invalid tenant root registration")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Acquire SQLite's writer reservation BEFORE reading, avoiding stale snapshots
	// and cross-process overlapping registrations. No rows need to exist yet.
	if _, err = tx.ExecContext(ctx, "UPDATE tenant_roots SET tenant_id=tenant_id WHERE 0"); err != nil {
		return err
	}
	existing, err := listTenantRoots(ctx, tx)
	if err != nil {
		return err
	}
	if err = validate(existing); err != nil {
		return err
	}
	for _, old := range existing {
		if old.ID == m.ID {
			if old != m {
				return tenantfs.ErrConflict
			}
			return tx.Commit()
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO tenant_roots (tenant_id,brain_root,blob_root,brain_absolute,blob_absolute,brain_canonical,blob_canonical,layout) VALUES (?,?,?,?,?,?,?,?)`, m.ID, m.BrainRoot, m.BlobRoot, m.BrainAbsolute, m.BlobAbsolute, m.BrainCanonical, m.BlobCanonical, m.Layout)
	if err != nil {
		return err
	}
	return tx.Commit()
}

package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/huynle/brain-api/internal/tenant"
)

// Ordinary data-plane readiness is NOT a global migration rehearsal. Shared
// schema/ownership definitions remain exact, but row, FTS and CAS readiness is
// restricted to the bound owner. The dormant migration keeps its full validator.
func validateSuccessorReceiver(ctx context.Context, tx *sql.Tx, owner tenant.ID) error {
	if ctx == nil || tx == nil || !owner.Valid() {
		return fmt.Errorf("invalid receiver readiness scope")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := readSuccessorProvenanceRecord(ctx, tx); err != nil {
		return err
	}
	if err := checkMigrationControlSchema(tx); err != nil {
		return err
	}
	if err := checkSuccessorIndexes(tx); err != nil {
		return err
	}
	mapping, err := checkTenantSearchCatalogComplete(tx, true)
	if err != nil {
		return err
	}
	id, ok := mapping[owner.String()]
	if !ok {
		return fmt.Errorf("tenant missing FTS mapping")
	}
	tables := []string{"tenant_runner_keys", "tenant_client_keys"}
	for _, spec := range relationalTables {
		tables = append(tables, spec.name)
	}
	tables = append(tables, successorWorkloadTables()...)
	for _, table := range tables {
		if err := checkReceiverForeignKeys(ctx, tx, table, owner); err != nil {
			return err
		}
	}
	var epoch string
	if err := tx.QueryRowContext(ctx, "SELECT epoch FROM entry_sync_identity WHERE tenant_id=? AND id=1", owner.String()).Scan(&epoch); err != nil {
		return fmt.Errorf("tenant sync identity: %w", err)
	}
	var count, high, ownHigh int64
	if err := tx.QueryRowContext(ctx, "SELECT count(*),coalesce(max(seq),0) FROM sqlite_sequence WHERE name='entry_sync_changes'").Scan(&count, &high); err != nil {
		return err
	}
	if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(seq),0) FROM entry_sync_changes WHERE tenant_id=?", owner.String()).Scan(&ownHigh); err != nil {
		return err
	}
	if count > 1 || high < ownHigh || ownHigh > 0 && count != 1 {
		return fmt.Errorf("invalid tenant sync high-water state")
	}
	// FTS integrity-check has INSERT syntax. Never execute it on a foreign
	// tenant's FTS table during ordinary readiness, even on a read request.
	if err := checkTenantFTSContents(tx, map[string]string{owner.String(): id}); err != nil {
		return err
	}
	return validateTenantFiles(ctx, tx, true, owner)
}

// Tables and foreign-key definitions were checked against the exact catalog
// before reaching here. Anti-joins inspect only this owner's child rows, unlike
// global PRAGMA foreign_key_check/integrity_check used by offline migration.
func checkReceiverForeignKeys(ctx context.Context, tx *sql.Tx, table string, owner tenant.ID) error {
	rows, err := tx.QueryContext(ctx, `SELECT id,"table","from","to" FROM pragma_foreign_key_list(?) ORDER BY id,seq`, table)
	if err != nil {
		return err
	}
	type relation struct {
		parent         string
		equal, present []string
	}
	keys := map[int]*relation{}
	for rows.Next() {
		var id int
		var parent, from, to string
		if err := rows.Scan(&id, &parent, &from, &to); err != nil {
			_ = rows.Close()
			return err
		}
		r := keys[id]
		if r == nil {
			r = &relation{parent: parent}
			keys[id] = r
		}
		r.equal = append(r.equal, fmt.Sprintf("p.%q=c.%q", to, from))
		r.present = append(r.present, fmt.Sprintf("c.%q IS NOT NULL", from))
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, r := range keys {
		query := fmt.Sprintf("SELECT count(*) FROM %q c WHERE c.tenant_id=? AND %s AND NOT EXISTS(SELECT 1 FROM %q p WHERE %s)", table, strings.Join(r.present, " AND "), r.parent, strings.Join(r.equal, " AND "))
		var invalid int
		if err := tx.QueryRowContext(ctx, query, owner.String()).Scan(&invalid); err != nil {
			return err
		}
		if invalid != 0 {
			return fmt.Errorf("tenant foreign key violation in %s", table)
		}
	}
	return nil
}

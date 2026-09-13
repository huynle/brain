package storage

import (
	"context"
	"fmt"

	"github.com/huynle/brain-api/internal/tenant"
)

// executionScope is deliberately separate from contentScope/listQuery. These
// ledgers were already tenant-keyed on main29/30, neither of which is private29.
// It grants no authorization and creates no schema. Main profiles are local-only;
// the exact unpublished partial staging catalog exercises tenant SQL privately.
// No tenant/31 profile is admitted until phase 4 supplies its complete validator.
// Like the other receiver preflights, this requires schema writers to be fenced;
// it is not a concurrent migration lock covering the subsequent operation.
func (s *TenantStore) executionScope(ctx context.Context, group string) (string, error) {
	if s == nil || s.db == nil || !s.tenantID.Valid() || ctx == nil {
		return "", fmt.Errorf("execution ledger requires a valid bound store and context")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	id, ok := tenant.From(ctx)
	if !ok || id != s.tenantID {
		return "", fmt.Errorf("execution ledger tenant scope mismatch")
	}
	if group != "bulk" && group != "budget" && group != "supervisor" {
		return "", fmt.Errorf("unknown execution ledger group")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	var count, version, temporary int
	if err = tx.QueryRowContext(ctx, "SELECT count(*),coalesce(max(version),0) FROM main.schema_version").Scan(&count, &version); err != nil {
		return "", err
	}
	if err = tx.QueryRowContext(ctx, "SELECT count(*) FROM sqlite_temp_schema").Scan(&temporary); err != nil {
		return "", err
	}
	if temporary != 0 {
		return "", fmt.Errorf("execution ledger refuses temporary schema objects")
	}
	if count != 0 {
		if id != tenant.Local || version < 29 || version > 30 {
			return "", fmt.Errorf("execution ledger schema unavailable")
		}
		profile, err := classifySchemaSource(ctx, tx)
		if err != nil {
			return "", err
		}
		if profile == "main29" && group == "bulk" {
			return id.String(), nil
		}
		if profile == "main30-pre-sync" || profile == "main30-initial-sync" || profile == "main30-devices" {
			return id.String(), nil
		}
		return "", fmt.Errorf("execution ledger missing from source profile %s", profile)
	}
	// Exact controlled partial staging profile: zero version rows, ONLY these
	// seven tables, their registry parent, version table and reviewed index. This
	// does not validate/admit a full successor or relax the private29 inventories.
	want := successorLedgerDefinitions()
	want["schema_version"] = successorControlDefinitions()["schema_version"]
	want["tenants"] = `CREATE TABLE tenants(id TEXT PRIMARY KEY NOT NULL CHECK(length(id)>0),name TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('active','suspended','deleting','deleted')),created_at TEXT NOT NULL,root_override TEXT)`
	for _, name := range []string{"tenants", "bulk_jobs", "bulk_job_items", "execution_budgets", "budget_reservations", "supervisor_checkpoints", "supervisor_checkpoint_versions", "supervisor_operations"} {
		want["sqlite_autoindex_"+name+"_1"] = ""
	}
	want["sqlite_autoindex_bulk_jobs_2"] = ""
	want["sqlite_autoindex_bulk_job_items_2"] = ""
	rows, err := tx.QueryContext(ctx, "SELECT type,name,tbl_name,coalesce(sql,'') FROM main.sqlite_schema")
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var kind, name, table, ddl string
		if err = rows.Scan(&kind, &name, &table, &ddl); err != nil {
			_ = rows.Close()
			return "", err
		}
		expected, exists := want[name]
		validKind := kind == "table" && table == name
		if name == "bulk_items_pending" {
			validKind = kind == "index" && table == "bulk_job_items"
		}
		if expected == "" {
			validKind = kind == "index" && name == "sqlite_autoindex_"+table+"_1" || kind == "index" && (table == "bulk_jobs" || table == "bulk_job_items") && name == "sqlite_autoindex_"+table+"_2"
		}
		if !exists || !validKind || ddl != expected {
			_ = rows.Close()
			return "", fmt.Errorf("unreviewed execution staging object %s", name)
		}
		delete(want, name)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return "", err
	}
	if len(want) != 0 {
		return "", fmt.Errorf("incomplete execution ledger staging schema")
	}
	return id.String(), nil
}

package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Dormant/offline only. No public initializer or service calls this owner.
// A read-only exact admission precedes even connection PRAGMA changes. Admission
// is repeated under the writer reservation; callers must fence filesystem writers.
func migrateSuccessorSchema(ctx context.Context, db *sql.DB, checkpoint func(string) error) (err error) {
	if ctx == nil || db == nil {
		return fmt.Errorf("migration requires database and context")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, conn.Close()) }()
	inspect, err := conn.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	var version int
	if err = inspect.QueryRowContext(ctx, "SELECT coalesce(max(version),0) FROM main.schema_version").Scan(&version); err != nil {
		_ = inspect.Rollback()
		return err
	}
	if version == 31 {
		err = validateSuccessorSchema(ctx, inspect, true)
		return errors.Join(err, inspect.Rollback()) // no PRAGMA write, repair or replay
	}
	plan, err := selectSuccessorSchema(ctx, inspect)
	if err == nil {
		err = validateSuccessorSourceRows(ctx, inspect, plan.source)
	}
	err = errors.Join(err, inspect.Rollback())
	if err != nil {
		return err
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, e := conn.ExecContext(cleanup, "PRAGMA foreign_keys=ON")
		var fk int
		if e == nil {
			e = conn.QueryRowContext(cleanup, "PRAGMA foreign_keys").Scan(&fk)
			if e == nil && fk != 1 {
				e = fmt.Errorf("foreign keys not restored")
			}
		}
		if e != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		err = errors.Join(err, e)
	}()
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return err
	}
	var fk int
	if err = conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		return err
	}
	if fk != 0 {
		return fmt.Errorf("foreign keys must be off before transaction")
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "UPDATE schema_version SET version=version WHERE 0"); err != nil {
		return err
	}
	locked, err := selectSuccessorSchema(ctx, tx)
	if err != nil {
		return err
	}
	if locked != plan {
		return fmt.Errorf("source changed during admission")
	}
	if err = validateSuccessorSourceRows(ctx, tx, plan.source); err != nil {
		return err
	}
	step := func(s string) error {
		if checkpoint != nil {
			return checkpoint(s)
		}
		return nil
	}
	if err = step("reserved"); err != nil {
		return err
	}
	snapshots, err := captureSuccessorSource(tx, plan.source)
	if err != nil {
		return err
	}
	if err = step("snapshot"); err != nil {
		return err
	}
	private := plan.source.profile == "private29"
	hasSync := plan.source.profile == "main30-initial-sync" || plan.source.profile == "main30-devices"
	if hasSync {
		for _, name := range []string{"entry_sync_insert", "entry_sync_update", "entry_sync_delete"} {
			if _, err = tx.Exec("DROP TRIGGER " + name); err != nil {
				return err
			}
		}
	}
	if !private {
		if err = rebuildUnownedRelationalSchema(tx); err != nil {
			return err
		}
	}
	if err = stageSuccessorWorkloads(tx); err != nil {
		return err
	}
	// Restore ALL original allocated and absent high-water records exactly before
	// initializing a genuinely sync-less source. Copying history never seeds notes.
	if _, err = tx.Exec("DELETE FROM sqlite_sequence; INSERT INTO sqlite_sequence SELECT * FROM p31_sequence"); err != nil {
		return err
	}
	if err = step("relational"); err != nil {
		return err
	}
	if !private {
		if err = rebuildTenantFTS(tx); err != nil {
			return err
		}
	}
	if err = step("fts"); err != nil {
		return err
	}
	if !hasSync {
		if _, err = tx.Exec(`INSERT INTO entry_sync_identity(tenant_id,id,epoch) SELECT id,1,lower(hex(randomblob(16))) FROM tenants;
 INSERT INTO entry_sync_changes(tenant_id,path) SELECT tenant_id,path FROM notes ORDER BY id`); err != nil {
			return err
		}
	}
	for _, ddl := range successorSyncTriggers() {
		if _, err = tx.Exec(ddl); err != nil {
			return err
		}
	}
	controls := successorControlDefinitions()
	if _, err = tx.Exec(controls["schema_provenance"]); err != nil {
		return err
	}
	for _, ddl := range controls {
		if strings.HasPrefix(ddl, "CREATE TRIGGER ") {
			if _, err = tx.Exec(ddl); err != nil {
				return err
			}
		}
	}
	if err = step("sync"); err != nil {
		return err
	}
	for _, s := range snapshots {
		if err = s.check(tx); err != nil {
			return err
		}
	}
	// Source sequence preservation is exact for every original sequence. Newly
	// initialized sync-less sources may add only their new changes sequence.
	extra := ""
	if !hasSync {
		extra = " WHERE name!='entry_sync_changes'"
	}
	for _, q := range []string{"SELECT * FROM p31_sequence EXCEPT SELECT * FROM sqlite_sequence", "SELECT * FROM sqlite_sequence" + extra + " EXCEPT SELECT * FROM p31_sequence"} {
		var n int
		if err = tx.QueryRow("SELECT count(*) FROM (" + q + ")").Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("sequence preservation failed")
		}
	}
	if _, err = tx.Exec("DROP TABLE p31_sequence"); err != nil {
		return err
	}
	if err = validateSuccessorSchema(ctx, tx, false); err != nil {
		return err
	}
	if err = step("validated"); err != nil {
		return err
	}
	// Provenance and numeric routing discriminator are the LAST database writes.
	source := plan.source
	if _, err = tx.ExecContext(ctx, `INSERT INTO schema_provenance VALUES(1,?,?,?,'tenant',31)`, source.profile, source.format.family, source.format.version); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO schema_version(version) VALUES(31)"); err != nil {
		return err
	}
	if err = step("published"); err != nil {
		return err
	}
	return tx.Commit()
}

func successorWorkloadTables() []string {
	var result []string
	for _, spec := range successorTableTransforms() {
		if spec.kind != unownedRelational {
			result = append(result, spec.name)
		}
	}
	return result
}

type successorCopyKind uint8

const (
	unownedRelational successorCopyKind = iota // 26 main -> local; private -> preserve
	keyedLedger                                // 7 original owner columns preserved, never prepended/reassigned
	unownedSync                                // 4 main -> local, absent sources initialize explicitly
)

type successorTableTransform struct {
	name string
	kind successorCopyKind
}

func successorTableTransforms() []successorTableTransform {
	var result []successorTableTransform
	for _, spec := range relationalTables {
		result = append(result, successorTableTransform{spec.name, unownedRelational})
	}
	for _, name := range strings.Fields("bulk_jobs bulk_job_items execution_budgets budget_reservations supervisor_checkpoints supervisor_checkpoint_versions supervisor_operations") {
		result = append(result, successorTableTransform{name, keyedLedger})
	}
	for _, name := range strings.Fields("entry_sync_devices entry_sync_identity entry_sync_changes entry_sync_operations") {
		result = append(result, successorTableTransform{name, unownedSync})
	}
	return result
}

func validateSuccessorSourceRows(ctx context.Context, tx *sql.Tx, source schemaSource) error {
	private := source.profile == "private29"
	if err := validateTenantMigrationFiles(ctx, tx, private); err != nil {
		return err
	}
	if private {
		return checkFinalTenantSearchSchema(tx)
	}
	// A main source is genuinely single-mode. Never silently remap an already
	// tenant-keyed ledger owner to local or invent a corresponding tenant.
	for _, spec := range successorTableTransforms() {
		if spec.kind != keyedLedger {
			continue
		}
		name := spec.name
		var exists int
		if err := tx.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='table' AND name=?", name).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			continue
		}
		var n int
		if err := tx.QueryRow("SELECT count(*) FROM " + name + " WHERE tenant_id IS NOT 'local'").Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("nonlocal single-mode ledger owner in %s", name)
		}
	}
	return nil
}

type successorSnapshot struct{ name, projection, table string }

func (s successorSnapshot) check(tx *sql.Tx) error {
	a := "SELECT " + s.projection + ",count(*) FROM " + s.name + " GROUP BY " + s.projection
	b := "SELECT " + s.projection + ",count(*) FROM " + s.table + " GROUP BY " + s.projection
	for _, q := range []string{a + " EXCEPT " + b, b + " EXCEPT " + a} {
		var n int
		if err := tx.QueryRow("SELECT count(*) FROM (" + q + ")").Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("changed exact source rows in %s", s.table)
		}
	}
	_, err := tx.Exec("DROP TABLE " + s.name)
	return err
}

func captureSuccessorSource(tx *sql.Tx, source schemaSource) ([]successorSnapshot, error) {
	var out []successorSnapshot
	private := source.profile == "private29"
	add := func(table, where, target string, local bool) error {
		cols, err := relationalColumns(tx, table)
		if err != nil {
			return err
		}
		projection := strings.Join(cols, ",")
		selects := projection
		if local {
			projection = "tenant_id," + projection
			selects = "'local' AS tenant_id," + selects
		}
		name := fmt.Sprintf("p31_snapshot_%d", len(out))
		if _, err = tx.Exec("CREATE TEMP TABLE " + name + " AS SELECT " + selects + " FROM " + table + where); err != nil {
			return err
		}
		out = append(out, successorSnapshot{name, projection, target})
		return nil
	}
	for _, spec := range relationalTables {
		where := ""
		if !private && spec.name == "entry_meta" {
			where = " WHERE path IS NOT 'brain:system/install_claimed'"
		}
		if err := add(spec.name, where, spec.name, !private); err != nil {
			return nil, err
		}
	}
	for _, table := range strings.Fields("schema_version api_tokens oauth_clients oauth_auth_codes oauth_access_tokens oauth_refresh_tokens tenant_roots") {
		if err := add(table, "", table, false); err != nil {
			return nil, err
		}
	}
	if private {
		for _, table := range strings.Fields("tenants tenant_runner_keys tenant_client_keys operator_install_claim tenant_fts tenant_fts_insert_guard") {
			if err := add(table, "", table, false); err != nil {
				return nil, err
			}
		}
	} else if err := add("entry_meta", " WHERE path='brain:system/install_claimed'", "operator_install_claim", false); err != nil {
		return nil, err
	}
	for _, spec := range successorTableTransforms() {
		if spec.kind == unownedRelational {
			continue
		}
		table := spec.name
		var n int
		if err := tx.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='table' AND name=?", table).Scan(&n); err != nil {
			return nil, err
		}
		if n == 1 {
			if err := add(table, "", table, spec.kind == unownedSync); err != nil {
				return nil, err
			}
		}
	}
	_, err := tx.Exec("CREATE TEMP TABLE p31_sequence AS SELECT * FROM sqlite_sequence")
	return out, err
}

// Three transformations, never a generic owner prepend: the original 26 are
// rebuilt only for unowned main; seven keyed ledgers copy every column; the four
// unowned main sync tables alone receive local. Private29's owners stay intact.
func stageSuccessorWorkloads(tx *sql.Tx) error {
	definitions := successorLedgerDefinitions()
	for k, v := range successorSyncDefinitions() {
		definitions[k] = v
	}
	for _, spec := range successorTableTransforms() {
		if spec.kind == unownedRelational {
			continue
		}
		name := spec.name
		var exists int
		if err := tx.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='table' AND name=?", name).Scan(&exists); err != nil {
			return err
		}
		ddl := definitions[name]
		if exists == 0 {
			if _, err := tx.Exec(ddl); err != nil {
				return err
			}
			continue
		}
		cols, err := relationalColumns(tx, name)
		if err != nil {
			return err
		}
		list := strings.Join(cols, ",")
		target, projection := list, list
		if spec.kind == unownedSync {
			target = "tenant_id," + list
			projection = "'local'," + list
		}
		if _, err = tx.Exec(strings.Replace(ddl, "CREATE TABLE "+name+" ", "CREATE TABLE p31_new_"+name+" ", 1)); err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO p31_new_" + name + " (" + target + ") SELECT " + projection + " FROM " + name); err != nil {
			return err
		}
		if _, err = tx.Exec("DROP TABLE " + name); err != nil {
			return err
		}
		if _, err = tx.Exec("ALTER TABLE p31_new_" + name + " RENAME TO " + name); err != nil {
			return err
		}
	}
	for _, ddl := range definitions {
		if strings.HasPrefix(ddl, "CREATE INDEX ") {
			if _, err := tx.Exec(ddl); err != nil {
				return err
			}
		}
	}
	return nil
}

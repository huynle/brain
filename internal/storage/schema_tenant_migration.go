package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
)

// Deliberately independent of CurrentSchemaVersion. No production caller may use
// this owner until ALL receiver/bootstrap/claim routing is cut over together.
const pendingTenantSchemaVersion = 29

// migrateTenantSchema is dormant, fenced/offline composition, NOT startup wiring.
// checkpoint is a private rehearsal seam; it must never call the pool. Filesystem
// writers must also be fenced: root policy is check-then-use, not a sandbox.
func migrateTenantSchema(ctx context.Context, db *sql.DB, checkpoint func(string) error) (err error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer func() {
		// Cancellation must not leave a pooled connection with enforcement disabled.
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, restore := conn.ExecContext(cleanup, "PRAGMA foreign_keys=ON")
		var fk int
		if restore == nil {
			restore = conn.QueryRowContext(cleanup, "PRAGMA foreign_keys").Scan(&fk)
			if restore == nil && fk != 1 {
				restore = fmt.Errorf("foreign keys not restored")
			}
		}
		if restore != nil {
			_ = conn.Raw(func(any) error { return driver.ErrBadConn })
		}
		err = errors.Join(err, restore, conn.Close())
	}()
	if _, err = conn.ExecContext(ctx, "PRAGMA foreign_keys=OFF"); err != nil {
		return err
	}
	var fk int
	if err = conn.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&fk); err != nil {
		return err
	}
	if fk != 0 {
		return fmt.Errorf("foreign keys must be disabled before BEGIN")
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Reserve the SQLite writer BEFORE any snapshot/version read.
	if _, err = tx.ExecContext(ctx, "UPDATE schema_version SET version=version WHERE 0"); err != nil {
		return err
	}
	step := func(name string) error {
		if checkpoint != nil {
			return checkpoint(name)
		}
		return nil
	}
	if err = step("reserved"); err != nil {
		return err
	}
	if err = checkMigrationControlSchema(tx); err != nil {
		return err
	}
	var version int
	if err = tx.QueryRowContext(ctx, "SELECT max(version) FROM schema_version").Scan(&version); err != nil {
		return err
	}
	if version == pendingTenantSchemaVersion {
		if err = checkFinalTenantSearchSchema(tx); err != nil {
			return err
		}
		if err = validateTenantMigrationFiles(ctx, tx, true); err != nil {
			return err
		}
		return tx.Rollback() // validate, never rebuild/repair a published schema
	}
	if version != 28 {
		return fmt.Errorf("unsupported tenant migration source version %d", version)
	}
	owned, err := auditRelationalInventory(tx)
	if err != nil {
		return err
	}
	if owned {
		return fmt.Errorf("partially committed tenant schema at v28")
	}
	if err = validateTenantMigrationFiles(ctx, tx, false); err != nil {
		return err
	}
	snapshots, err := captureTenantMigration(tx)
	if err != nil {
		return err
	}
	if err = step("snapshot"); err != nil {
		return err
	}
	if err = stageTenantRelationalSchema(tx); err != nil {
		return err
	}
	for _, s := range snapshots {
		if s.final != "sqlite_sequence" {
			continue
		}
		// INSERT ... SELECT into an empty AUTOINCREMENT copy creates a zero
		// sequence even when the original never allocated one. Remove only
		// those reviewed, empty-table artifacts; never reset a high-water mark.
		for _, spec := range relationalTables {
			if _, err = tx.Exec("DELETE FROM sqlite_sequence WHERE name=? AND seq=0 AND name NOT IN (SELECT name FROM "+s.name+") AND NOT EXISTS (SELECT 1 FROM "+spec.name+")", spec.name); err != nil {
				return err
			}
		}
	}
	if err = step("relational"); err != nil {
		return err
	}
	if err = stageTenantFTS(tx); err != nil {
		return err
	}
	if err = step("fts"); err != nil {
		return err
	}
	if err = validateTenantMigrationFiles(ctx, tx, true); err != nil {
		return err
	}
	if err = step("files"); err != nil {
		return err
	}
	if err = checkFinalTenantSearchSchema(tx); err != nil {
		return err
	}
	if err = checkMigrationControlSchema(tx); err != nil {
		return err
	}
	for _, s := range snapshots {
		if err = s.check(tx); err != nil {
			return err
		}
	}
	if err = step("validated"); err != nil {
		return err
	}
	// Final-only publication. The version is the future routing discriminator;
	// current runtime rejects it rather than treating future SQL as v28.
	if _, err = tx.ExecContext(ctx, "INSERT INTO schema_version(version) VALUES(?)", pendingTenantSchemaVersion); err != nil {
		return err
	}
	if err = step("published"); err != nil {
		return err
	}
	return tx.Commit()
}

// Relational staging intentionally owns only workload DDL. The outer owner also
// audits untouched authoritative tables, rather than trusting a v28 stamp to
// imply their shape. Unknown historical variants require explicit review.
func checkMigrationControlSchema(tx *sql.Tx) error {
	definitions := map[string]string{
		"schema_version":       createSchemaVersionTable,
		"api_tokens":           createAPITokensTable,
		"oauth_clients":        createOAuthClientsTable,
		"oauth_auth_codes":     createOAuthAuthCodesTable,
		"oauth_access_tokens":  createOAuthAccessTokensTable,
		"oauth_refresh_tokens": createOAuthRefreshTokensTable,
		"tenant_roots":         createTenantRootsTable,
	}
	for name, ddl := range definitions {
		var actual string
		if err := tx.QueryRow("SELECT sql FROM sqlite_schema WHERE type='table' AND name=?", name).Scan(&actual); err != nil {
			return err
		}
		// InitSchema runs the historical v4 OAuth rebuild even for a fresh DB.
		// Accept its exact reviewed no-FK form as well as the bootstrap DDL;
		// do not normalize arbitrary whitespace/constraints out of source SQL.
		v4 := ""
		if name == "oauth_access_tokens" || name == "oauth_refresh_tokens" {
			v4 = "CREATE TABLE " + name + ` (
				token TEXT PRIMARY KEY,
				client_id TEXT NOT NULL,
				scope TEXT,
				user_id TEXT,
				expires_at INTEGER NOT NULL,
				created_at INTEGER NOT NULL
			)`
		}
		if normalizeRelationalDDL(actual) != normalizeRelationalDDL(ddl) && (v4 == "" || normalizeRelationalDDL(actual) != v4) {
			return fmt.Errorf("unreviewed control definition %s", name)
		}
	}
	want := map[string]bool{}
	for _, ddl := range createIndexes {
		for table := range definitions {
			if strings.Contains(ddl, " ON "+table+"(") {
				want[normalizeRelationalDDL(ddl)] = true
			}
		}
	}
	rows, err := tx.Query("SELECT tbl_name,sql FROM sqlite_schema WHERE type='index' AND sql IS NOT NULL")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var table, ddl string
		if err := rows.Scan(&table, &ddl); err != nil {
			return err
		}
		if _, ok := definitions[table]; !ok {
			continue
		}
		key := normalizeRelationalDDL(ddl)
		if !want[key] {
			return fmt.Errorf("unreviewed control index on %s", table)
		}
		delete(want, key)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(want) != 0 {
		return fmt.Errorf("missing control indexes")
	}
	return nil
}

// Temporary exact multiset snapshots avoid holding the whole database in Go
// memory. Explicit source columns preserve NULLs, bytes and duplicate rows.
type tenantMigrationSnapshot struct{ name, columns, final string }

func captureTenantMigration(tx *sql.Tx) ([]tenantMigrationSnapshot, error) {
	var result []tenantMigrationSnapshot
	add := func(table, where, final string) error {
		cols, err := relationalColumns(tx, table)
		if err != nil {
			return err
		}
		columns := strings.Join(cols, ",")
		name := fmt.Sprintf("p4_snapshot_%d", len(result))
		if _, err = tx.Exec("CREATE TEMP TABLE " + name + " AS SELECT " + columns + " FROM " + table + where); err != nil {
			return err
		}
		result = append(result, tenantMigrationSnapshot{name, columns, final})
		return nil
	}
	for _, s := range relationalTables {
		where := ""
		if s.name == "entry_meta" {
			where = " WHERE path!='brain:system/install_claimed'"
		}
		if err := add(s.name, where, s.name); err != nil {
			return nil, err
		}
	}
	for _, table := range strings.Fields("schema_version sqlite_sequence api_tokens oauth_clients oauth_auth_codes oauth_access_tokens oauth_refresh_tokens tenant_roots") {
		if err := add(table, "", table); err != nil {
			return nil, err
		}
	}
	if err := add("entry_meta", " WHERE path='brain:system/install_claimed'", "operator_install_claim"); err != nil {
		return nil, err
	}
	return result, nil
}

func (s tenantMigrationSnapshot) check(tx *sql.Tx) error {
	for _, spec := range relationalTables {
		if s.final != spec.name {
			continue
		}
		var n int
		if err := tx.QueryRow("SELECT count(*) FROM " + s.final + " WHERE tenant_id IS NOT 'local'").Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("migration changed legacy owner in %s", s.final)
		}
	}
	a := "SELECT " + s.columns + ",count(*) FROM " + s.name + " GROUP BY " + s.columns
	b := "SELECT " + s.columns + ",count(*) FROM " + s.final + " GROUP BY " + s.columns
	for _, q := range []string{a + " EXCEPT " + b, b + " EXCEPT " + a} {
		var n int
		if err := tx.QueryRow("SELECT count(*) FROM (" + q + ")").Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("migration changed exact rows in %s", s.final)
		}
	}
	_, err := tx.Exec("DROP TABLE " + s.name)
	return err
}

// This adapter cannot write or reenter the pool. Resolver re-reads the same tx
// snapshot for drift/exclusion checks; no Provision or Mkdir operation is used.
type migrationRootRepository struct{ tx *sql.Tx }

func (r migrationRootRepository) ListTenantRoots(ctx context.Context) ([]tenantfs.Mapping, error) {
	return listTenantRoots(ctx, r.tx)
}
func (r migrationRootRepository) RegisterTenantRoots(context.Context, tenantfs.Mapping, func([]tenantfs.Mapping) error) error {
	return fmt.Errorf("migration roots are read-only")
}

func validateTenantMigrationFiles(ctx context.Context, tx *sql.Tx, owned bool) error {
	return validateTenantFiles(ctx, tx, owned, tenant.ID{})
}

// A zero selection is reserved for the full offline migration/reopen wrapper.
// Receivers pass their validated binding and retain all mappings for exclusion
// and root-identity policy, but stat/hash only that tenant's roots and blobs.
func validateTenantFiles(ctx context.Context, tx *sql.Tx, owned bool, selected tenant.ID) error {
	maps, err := listTenantRoots(ctx, tx)
	if err != nil {
		return err
	}
	if len(maps) == 0 {
		return fmt.Errorf("missing durable local roots")
	}
	resolver, err := tenantfs.New(migrationRootRepository{tx}, maps[0].BrainAbsolute)
	if err != nil {
		return err
	}
	required := tenant.Local
	if selected.Valid() {
		required = selected
	}
	if _, err = resolver.Lookup(ctx, required); err != nil {
		return err
	}
	for _, m := range maps {
		if selected.Valid() && m.ID != selected {
			continue
		}
		if _, err = resolver.Lookup(ctx, m.ID); err != nil {
			return err
		}
		for _, root := range []string{m.BrainAbsolute, m.BlobAbsolute} {
			info, e := os.Stat(root)
			if e != nil {
				return e
			}
			if !info.IsDir() {
				return fmt.Errorf("root is not a directory")
			}
		}
		if owned {
			var n int
			if err = tx.QueryRow("SELECT count(*) FROM tenants WHERE id=?", m.ID).Scan(&n); err != nil {
				return err
			}
			if n != 1 {
				return fmt.Errorf("root without registered tenant")
			}
		}
	}
	if owned {
		var n int
		query, args := "SELECT count(*) FROM tenants WHERE id NOT IN (SELECT tenant_id FROM tenant_roots)", []any{}
		if selected.Valid() {
			query += " AND id=?"
			args = append(args, selected.String())
		}
		if err = tx.QueryRow(query, args...).Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("tenant missing durable roots")
		}
	}
	owner := "'local'"
	if owned {
		owner = "tenant_id"
	}
	query, args := "SELECT "+owner+",digest,size FROM attachments", []any{}
	if selected.Valid() {
		query += " WHERE tenant_id=?"
		args = append(args, selected.String())
	}
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	type blob struct {
		id     tenant.ID
		digest string
		size   int64
	}
	var blobs []blob
	for rows.Next() {
		var b blob
		if err = rows.Scan(&b.id, &b.digest, &b.size); err != nil {
			_ = rows.Close()
			return err
		}
		blobs = append(blobs, b)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, b := range blobs {
		if err = ctx.Err(); err != nil {
			return err
		}
		path, e := resolver.BlobPath(ctx, b.id, b.digest)
		if e != nil {
			return e
		}
		// Reject FIFOs/devices/etc BEFORE open: a blocking FIFO open cannot be
		// interrupted by ctx cancellation. Keep the descriptor check too, and
		// compare identity so a regular-file replacement is not silently read.
		// This is check-then-open, NOT an atomic filesystem security boundary.
		// The migration's external writer fence must cover CAS paths and their
		// ancestors for the entire validation. A concurrent swap to a FIFO can
		// still block open before f.Stat; cancellation is not a substitute fence.
		before, e := os.Stat(path)
		if e != nil {
			return e
		}
		if !before.Mode().IsRegular() {
			return fmt.Errorf("CAS object not regular")
		}
		f, e := os.Open(path)
		if e != nil {
			return e
		}
		info, e := f.Stat()
		if e != nil {
			_ = f.Close()
			return e
		}
		if !info.Mode().IsRegular() {
			_ = f.Close()
			return fmt.Errorf("CAS object not regular")
		}
		if !os.SameFile(before, info) {
			_ = f.Close()
			return fmt.Errorf("CAS object changed during validation")
		}
		h := sha256.New()
		n, e := io.Copy(h, f)
		closeErr := f.Close()
		if e != nil {
			return e
		}
		if closeErr != nil {
			return closeErr
		}
		if n != b.size || fmt.Sprintf("%x", h.Sum(nil)) != b.digest {
			return fmt.Errorf("CAS length/hash mismatch")
		}
	}
	return nil
}

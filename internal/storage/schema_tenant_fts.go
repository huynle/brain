package storage

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// Dormant P4.3 component. The caller owns the existing transaction and writer
// fence. Never commit independently or publish a version here. No pool calls.
func stageTenantFTS(tx *sql.Tx) (err error) {
	if tx == nil {
		return fmt.Errorf("tenant FTS requires an existing transaction")
	}
	var version int
	if err = tx.QueryRow("SELECT max(version) FROM schema_version").Scan(&version); err != nil {
		return err
	}
	if version != 28 {
		return fmt.Errorf("tenant FTS staging requires v28")
	}
	var exists int
	if err = tx.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='tenant_fts'").Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return checkFinalTenantSearchSchema(tx)
	}
	owned, err := auditRelationalInventory(tx)
	if err != nil {
		return err
	}
	if !owned {
		return fmt.Errorf("tenant FTS requires staged relational ownership")
	}
	if err = checkRelationalOwnership(tx); err != nil {
		return err
	}
	if err = checkFTSDefinitions(tx, legacyFTSDefinitions()); err != nil {
		return err
	}
	if _, err = tx.Exec("SAVEPOINT p4_fts"); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if _, e := tx.Exec("ROLLBACK TO p4_fts"); e != nil {
				err = fmt.Errorf("%w; FTS rollback: %v (outer rollback required)", err, e)
			}
		}
		if _, e := tx.Exec("RELEASE p4_fts"); e != nil {
			err = fmt.Errorf("FTS release: %v; original: %w", e, err)
		}
	}()
	if _, err = tx.Exec(tenantFTSMappingDDL); err != nil {
		return err
	}
	if _, err = tx.Exec(tenantFTSInsertGuardDDL); err != nil {
		return err
	}
	rows, err := tx.Query("SELECT id FROM tenants ORDER BY id")
	if err != nil {
		return err
	}
	var tenants []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			break
		}
		tenants = append(tenants, id)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, owner := range tenants {
		var bytes [16]byte
		if _, err = rand.Read(bytes[:]); err != nil {
			return err
		}
		id := hex.EncodeToString(bytes[:])
		name, e := tenantFTSName(id)
		if e != nil {
			return e
		}
		if _, err = tx.Exec(tenantFTSTableDDL(name)); err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO "+name+"(rowid,title,body,path) SELECT id,title,body,path FROM notes WHERE tenant_id=?", owner); err != nil {
			return err
		}
		if _, err = tx.Exec("INSERT INTO tenant_fts(tenant_id,internal_id) VALUES(?,?)", owner, id); err != nil {
			return err
		}
	}
	if _, err = tx.Exec("DROP TRIGGER notes_ai; DROP TRIGGER notes_ad; DROP TRIGGER notes_au; DROP TABLE notes_fts"); err != nil {
		return err
	}
	mapping, err := readTenantFTSMapping(tx)
	if err != nil {
		return err
	}
	for _, ddl := range tenantFTSTriggers(mapping) {
		if _, err = tx.Exec(ddl); err != nil {
			return err
		}
	}
	return checkFinalTenantSearchSchema(tx)
}

const tenantFTSMappingDDL = `CREATE TABLE tenant_fts(tenant_id TEXT PRIMARY KEY NOT NULL REFERENCES tenants(id),internal_id TEXT UNIQUE NOT NULL CHECK(length(internal_id)=32 AND internal_id NOT GLOB '*[^0-9a-f]*'))`

// Internal per-row scratch, not an ownership registry. BEFORE INSERT always
// replaces its contents; a skipped INSERT/UPSERT may leave one harmless candidate.
const tenantFTSInsertGuardDDL = `CREATE TABLE tenant_fts_insert_guard(slot INTEGER PRIMARY KEY CHECK(slot=1),conflicting_id INTEGER NOT NULL)`

var tenantFTSID = regexp.MustCompile(`^[0-9a-f]{32}$`)

func tenantFTSName(id string) (string, error) {
	if !tenantFTSID.MatchString(id) {
		return "", fmt.Errorf("invalid internal FTS identifier")
	}
	return `"fts_t_` + id + `"`, nil
}

func tenantFTSTableDDL(name string) string {
	return "CREATE VIRTUAL TABLE " + name + " USING fts5(title, body, path, tokenize='porter unicode61')"
}

// Private mapping only. No caller supplies a table name or an internal ID.
// Lifecycle provisioning/deletion must replace these guards under its own fence;
// ordinary content writers cannot rebind, remove, or add mappings.
func tenantFTSTriggers(mapping map[string]string) map[string]string {
	result := map[string]string{}
	for _, action := range []string{"INSERT", "UPDATE", "DELETE"} {
		name := "p4_fts_map_" + strings.ToLower(action)
		result[name] = "CREATE TRIGGER " + name + " BEFORE " + action + " ON tenant_fts BEGIN SELECT RAISE(ABORT,'FTS mapping is server owned'); END"
		row := "NEW"
		if action == "DELETE" {
			row = "OLD"
		}
		name = "p4_fts_require_" + strings.ToLower(action)
		result[name] = "CREATE TRIGGER " + name + " BEFORE " + action + " ON notes BEGIN SELECT CASE WHEN NOT EXISTS(SELECT 1 FROM tenant_fts WHERE tenant_id=" + row + ".tenant_id) THEN RAISE(ABORT,'tenant search unavailable') END; END"
		if action == "UPDATE" {
			name = "p4_fts_rowid_" + strings.ToLower(action)
			result[name] = "CREATE TRIGGER " + name + " BEFORE " + action + " ON notes WHEN EXISTS(SELECT 1 FROM notes WHERE id=NEW.id AND tenant_id IS NOT NEW.tenant_id) BEGIN SELECT RAISE(ABORT,'foreign note rowid replacement'); END"
		}
	}
	// SQLite defines implicit NEW.rowid as undefined BEFORE INSERT (currently -1).
	// Never reject on that value alone or exempt -1: it is also a valid legacy ID.
	// Capture the possible foreign collision before REPLACE deletes its victim,
	// then compare against the real allocated ID AFTER INSERT. An automatically
	// allocated ID cannot equal an existing row's ID; an explicit replacement can.
	// ABORT restores the whole statement, including implicit deletes, FK cascades
	// and FTS changes, regardless of recursive_triggers or AFTER trigger order.
	// SQLite serializes writers and executes row triggers per row. The audited
	// catalog has no nested INSERTs into notes: none can overwrite this scratch
	// between capture and check. DO NOTHING/UPSERT can skip AFTER INSERT, so reset
	// on EVERY BEFORE INSERT, not merely after success. Direct scratch/DDL writes
	// are server-owned, like the mapping; this is not a raw-SQL attacker boundary.
	result["p4_fts_rowid_insert"] = `CREATE TRIGGER p4_fts_rowid_insert BEFORE INSERT ON notes BEGIN
 DELETE FROM tenant_fts_insert_guard;
 INSERT INTO tenant_fts_insert_guard(slot,conflicting_id) SELECT 1,id FROM notes WHERE id=NEW.id AND tenant_id IS NOT NEW.tenant_id;
 END`
	result["p4_fts_rowid_insert_check"] = `CREATE TRIGGER p4_fts_rowid_insert_check AFTER INSERT ON notes BEGIN
 SELECT CASE WHEN EXISTS(SELECT 1 FROM tenant_fts_insert_guard WHERE conflicting_id=NEW.id) THEN RAISE(ABORT,'foreign note rowid replacement') END;
 DELETE FROM tenant_fts_insert_guard;
 END`
	result["p4_fts_owner"] = "CREATE TRIGGER p4_fts_owner BEFORE UPDATE OF tenant_id ON notes WHEN OLD.tenant_id IS NOT NEW.tenant_id BEGIN SELECT RAISE(ABORT,'note owner is immutable'); END"
	for _, id := range mapping {
		// IDs were validated by readTenantFTSMapping; no owner string is SQL text.
		name, _ := tenantFTSName(id)
		for _, action := range []string{"INSERT", "UPDATE", "DELETE"} {
			trigger := "p4_fts_" + id + "_" + strings.ToLower(action)
			row := "NEW"
			if action == "DELETE" {
				row = "OLD"
			}
			body := ""
			if action != "INSERT" {
				body += "DELETE FROM " + name + " WHERE rowid=OLD.id; "
			}
			if action != "DELETE" {
				// REPLACE's implicit delete does not fire delete triggers unless
				// recursive_triggers is enabled. Clean the conflicting logical key
				// and physical identity AFTER success, so UPSERT DO NOTHING cannot
				// erase an existing posting. This scans tenant-local FTS content;
				// include its cost in the required large-corpus write benchmarks.
				body += "DELETE FROM " + name + " WHERE rowid=NEW.id OR path=NEW.path; "
				body += "INSERT INTO " + name + "(rowid,title,body,path) VALUES(NEW.id,NEW.title,NEW.body,NEW.path); "
			}
			// SQLite trigger definitions cannot bind parameters. Only the
			// validated server-generated hex ID is a literal here; tenant IDs
			// and all content values remain row references or bound parameters.
			result[trigger] = "CREATE TRIGGER \"" + trigger + "\" AFTER " + action + " ON notes WHEN " + row + ".tenant_id=(SELECT tenant_id FROM tenant_fts WHERE internal_id='" + id + "') BEGIN " + body + "END"
		}
	}
	return result
}

func readTenantFTSMapping(tx *sql.Tx) (map[string]string, error) {
	rows, err := tx.Query("SELECT t.id,m.internal_id FROM tenants t LEFT JOIN tenant_fts m ON m.tenant_id=t.id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := map[string]string{}
	seen := map[string]bool{}
	for rows.Next() {
		var owner, id string
		if err := rows.Scan(&owner, &id); err != nil {
			return nil, err
		}
		if owner == "" || seen[id] {
			return nil, fmt.Errorf("invalid FTS mapping")
		}
		if _, err := tenantFTSName(id); err != nil {
			return nil, err
		}
		result[owner] = id
		seen[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	var orphan int
	if err := tx.QueryRow("SELECT count(*) FROM tenant_fts m WHERE NOT EXISTS(SELECT 1 FROM tenants t WHERE t.id=m.tenant_id)").Scan(&orphan); err != nil {
		return nil, err
	}
	if orphan != 0 {
		return nil, fmt.Errorf("orphan FTS mapping")
	}
	return result, nil
}

// Pin SQLite's reviewed FTS5 shadow schema too, not just the virtual table name.
func ftsShadowDefinitions(name string, contentful bool) map[string]string {
	r := map[string]string{
		name + "_data":    `CREATE TABLE '` + name + `_data'(id INTEGER PRIMARY KEY, block BLOB)`,
		name + "_idx":     `CREATE TABLE '` + name + `_idx'(segid, term, pgno, PRIMARY KEY(segid, term)) WITHOUT ROWID`,
		name + "_docsize": `CREATE TABLE '` + name + `_docsize'(id INTEGER PRIMARY KEY, sz BLOB)`,
		name + "_config":  `CREATE TABLE '` + name + `_config'(k PRIMARY KEY, v) WITHOUT ROWID`,
	}
	if contentful {
		r[name+"_content"] = `CREATE TABLE '` + name + `_content'(id INTEGER PRIMARY KEY, c0, c1, c2)`
	}
	return r
}

func legacyFTSDefinitions() map[string]string {
	r := ftsShadowDefinitions("notes_fts", false)
	r["notes_fts"] = strings.Replace(strings.TrimSuffix(strings.TrimSpace(createFTS5Table), ";"), "IF NOT EXISTS ", "", 1)
	return r
}

func checkFTSDefinitions(tx *sql.Tx, expected map[string]string) error {
	for name, want := range expected {
		var actual string
		if err := tx.QueryRow("SELECT sql FROM sqlite_schema WHERE name=? AND type='table'", name).Scan(&actual); err != nil {
			return fmt.Errorf("missing FTS table %s: %w", name, err)
		}
		if actual != want {
			return fmt.Errorf("changed FTS definition %s", name)
		}
		// FTS shadows are engine-owned, not extension points for custom indexes.
		var extra int
		if err := tx.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='index' AND tbl_name=? AND sql IS NOT NULL", name).Scan(&extra); err != nil {
			return err
		}
		if extra != 0 {
			return fmt.Errorf("unreviewed FTS index on %s", name)
		}
	}
	return nil
}

// Phase 2 seam: validate the finalized relational + search catalog without
// relaxing auditRelationalInventory's legacy-source acceptance in any way.
func checkFinalTenantSearchSchema(tx *sql.Tx) error {
	mapping, err := checkTenantSearchCatalog(tx)
	if err != nil {
		return err
	}
	if err := checkRelationalOwnership(tx); err != nil {
		return err
	}
	return checkTenantFTSContents(tx, mapping)
}

// Read-only catalog check for transaction-local queries. Content/index integrity
// scans belong to staging/maintenance, not each request. DDL and mappings must
// remain server owned; this is not protection against an attacker with raw SQL.
func checkTenantSearchCatalog(tx *sql.Tx) (map[string]string, error) {
	mapping, err := readTenantFTSMapping(tx)
	if err != nil {
		return nil, err
	}
	tables := map[string]string{"tenant_fts": tenantFTSMappingDDL, "tenant_fts_insert_guard": tenantFTSInsertGuardDDL}
	for _, id := range mapping {
		quoted, _ := tenantFTSName(id)
		name := "fts_t_" + id
		tables[name] = tenantFTSTableDDL(quoted)
		for key, ddl := range ftsShadowDefinitions(name, true) {
			tables[key] = ddl
		}
	}
	if err := checkFTSDefinitions(tx, tables); err != nil {
		return nil, err
	}
	names := map[string]bool{}
	for name := range tables {
		names[name] = true
	}
	if _, err := auditRelationalInventoryWithSearch(tx, names, tenantFTSTriggers(mapping)); err != nil {
		return nil, err
	}
	if err := checkRelationalDefinitions(tx); err != nil {
		return nil, err
	}
	return mapping, nil
}

func checkTenantFTSContents(tx *sql.Tx, mapping map[string]string) error {
	for owner, id := range mapping {
		name, _ := tenantFTSName(id)
		var mismatch int
		stmt := "SELECT count(*) FROM (SELECT id,title,body,path FROM notes WHERE tenant_id=? EXCEPT SELECT rowid,title,body,path FROM " + name + ")"
		if err := tx.QueryRow(stmt, owner).Scan(&mismatch); err != nil {
			return err
		}
		if mismatch != 0 {
			return fmt.Errorf("FTS content missing or stale")
		}
		stmt = "SELECT count(*) FROM (SELECT rowid,title,body,path FROM " + name + " EXCEPT SELECT id,title,body,path FROM notes WHERE tenant_id=?)"
		if err := tx.QueryRow(stmt, owner).Scan(&mismatch); err != nil {
			return err
		}
		if mismatch != 0 {
			return fmt.Errorf("FTS contains foreign or stale content")
		}
		if _, err := tx.Exec("INSERT INTO " + name + "(" + name + ") VALUES('integrity-check')"); err != nil {
			return err
		}
	}
	return nil
}

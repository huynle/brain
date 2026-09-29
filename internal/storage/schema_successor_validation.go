package storage

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Exact new-object manifest including implicit indexes. No arbitrary additions
// are admitted to the historical private29 validator or the pinned main sources.
func checkSuccessorAdditions(tx *sql.Tx) error {
	want := successorLedgerDefinitions()
	for k, v := range successorSyncDefinitions() {
		want[k] = v
	}
	for k, v := range successorSyncTriggers() {
		want[k] = v
	}
	for k, v := range successorControlDefinitions() {
		if k != "schema_version" {
			want[k] = v
		}
	}
	tables := map[string]bool{"schema_provenance": true}
	for _, name := range successorWorkloadTables() {
		tables[name] = true
		want["sqlite_autoindex_"+name+"_1"] = ""
	}
	want["sqlite_autoindex_bulk_jobs_2"] = ""
	want["sqlite_autoindex_bulk_job_items_2"] = ""
	rows, err := tx.Query("SELECT type,name,tbl_name,coalesce(sql,'') FROM sqlite_schema")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, name, table, ddl string
		if err := rows.Scan(&kind, &name, &table, &ddl); err != nil {
			return err
		}
		expected, known := want[name]
		if !tables[table] && !known {
			continue
		}
		if !known {
			return fmt.Errorf("unreviewed successor object %s", name)
		}
		valid := false
		switch {
		case strings.HasPrefix(expected, "CREATE TABLE "):
			valid = kind == "table" && name == table
		case strings.HasPrefix(expected, "CREATE TRIGGER "):
			valid = kind == "trigger" && ((strings.HasPrefix(name, "entry_sync_") && table == "notes") || (strings.HasPrefix(name, "schema_provenance_") && table == "schema_provenance"))
		case strings.HasPrefix(expected, "CREATE INDEX "):
			valid = kind == "index" && ((name == "bulk_items_pending" && table == "bulk_job_items") || (name == "entry_sync_changes_owner_sequence" && table == "entry_sync_changes"))
		case expected == "":
			valid = kind == "index" && strings.HasPrefix(name, "sqlite_autoindex_"+table+"_")
		}
		if !valid || normalizeRelationalDDL(ddl) != expected {
			return fmt.Errorf("changed successor definition %s", name)
		}
		delete(want, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(want) != 0 {
		return fmt.Errorf("missing successor definitions: %v", want)
	}
	return nil
}

// Complete target validation. published=false is private to the final precommit
// gate. Repeat/reopen never provisions roots, seeds sync, repairs FTS or receipts.
func validateSuccessorSchema(ctx context.Context, tx *sql.Tx, published bool) error {
	if published {
		if _, err := readSuccessorProvenanceRecord(ctx, tx); err != nil {
			return err
		}
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
	if err = checkRelationalOwnership(tx); err != nil {
		return err
	}
	for _, table := range successorWorkloadTables() {
		var n int
		if err = tx.QueryRow("SELECT count(*) FROM " + table + " WHERE tenant_id IS NULL OR tenant_id='' OR tenant_id NOT IN (SELECT id FROM tenants)").Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("unowned successor rows in %s", table)
		}
	}
	var missing int
	if err = tx.QueryRow("SELECT count(*) FROM tenants t WHERE NOT EXISTS(SELECT 1 FROM entry_sync_identity s WHERE s.tenant_id=t.id AND s.id=1)").Scan(&missing); err != nil {
		return err
	}
	if missing != 0 {
		return fmt.Errorf("missing tenant sync identity")
	}
	var seqCount, high, actual int64
	if err = tx.QueryRow("SELECT count(*),coalesce(max(seq),0) FROM sqlite_sequence WHERE name='entry_sync_changes'").Scan(&seqCount, &high); err != nil {
		return err
	}
	if err = tx.QueryRow("SELECT coalesce(max(seq),0) FROM entry_sync_changes").Scan(&actual); err != nil {
		return err
	}
	if seqCount > 1 || high < actual || (actual > 0 && seqCount != 1) {
		return fmt.Errorf("invalid sync sequence high-water state")
	}
	// FTS5 integrity-check validates inverted segments as well as row content.
	// Its INSERT syntax performs validation, never rebuild/repair/replay.
	if err = checkTenantFTSContents(tx, mapping); err != nil {
		return err
	}
	return validateTenantMigrationFiles(ctx, tx, true)
}

func checkSuccessorIndexes(tx *sql.Tx) error {
	want := map[string]bool{}
	for _, ddl := range requiredRelationalIndexes() {
		want[normalizeRelationalDDL(ddl)] = true
	}
	for _, spec := range relationalTables {
		want["CREATE INDEX p4_owner_"+spec.name+" ON "+spec.name+"(tenant_id)"] = true
	}
	controls := strings.Fields("schema_version api_tokens oauth_clients oauth_auth_codes oauth_access_tokens oauth_refresh_tokens tenant_roots")
	for _, ddl := range createIndexes {
		for _, table := range controls {
			if strings.Contains(ddl, " ON "+table+"(") {
				want[normalizeRelationalDDL(ddl)] = true
			}
		}
	}
	for _, definitions := range []map[string]string{successorLedgerDefinitions(), successorSyncDefinitions()} {
		for _, ddl := range definitions {
			if strings.HasPrefix(ddl, "CREATE INDEX ") {
				want[ddl] = true
			}
		}
	}
	rows, err := tx.Query("SELECT name,sql FROM sqlite_schema WHERE type='index' AND sql IS NOT NULL")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name, ddl string
		if err = rows.Scan(&name, &ddl); err != nil {
			return err
		}
		key := normalizeRelationalDDL(ddl)
		if !want[key] {
			return fmt.Errorf("unreviewed successor index %s", name)
		}
		delete(want, key)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if len(want) != 0 {
		return fmt.Errorf("missing successor indexes")
	}
	return nil
}

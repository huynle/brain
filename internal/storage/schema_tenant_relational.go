package storage

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
)

// This is ONLY the relational component of the pending v29 migration. There is
// deliberately no production caller, version write, feature flag or completion
// stamp. P4.1/P4.2/P4.3 must be committed by ONE future outer migration after FTS,
// CAS and routing validation. Never commit this component on its own.
//
// The outer owner must quiesce writers, reserve a single connection, disable
// foreign_keys BEFORE beginning its transaction, and restore/verify enforcement
// after rollback/commit. SQLite ignores changes to foreign_keys inside a tx.
// This component accepts only that existing transaction; it never owns the pool,
// begins a transaction or commits one. Its savepoint makes errors locally atomic.
func stageTenantRelationalSchema(tx *sql.Tx) (err error) {
	if tx == nil {
		return fmt.Errorf("relational migration requires an existing transaction")
	}
	var fk, version int
	if err = tx.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		return err
	}
	if fk != 0 {
		return fmt.Errorf("relational rebuild requires foreign_keys OFF before the outer transaction")
	}
	if err = tx.QueryRow("SELECT max(version) FROM schema_version").Scan(&version); err != nil {
		return err
	}
	if version != 28 {
		return fmt.Errorf("relational component requires v28, got %d", version)
	}
	if _, err = tx.Exec("SAVEPOINT p4_relational"); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			if _, e := tx.Exec("ROLLBACK TO p4_relational"); e != nil {
				err = fmt.Errorf("%w; rollback savepoint: %v (outer rollback required)", err, e)
			}
		}
		if _, e := tx.Exec("RELEASE p4_relational"); e != nil {
			err = fmt.Errorf("relational savepoint release: %v; original: %w", e, err)
		}
	}()
	owned, err := auditRelationalInventory(tx)
	if err != nil {
		return err
	}
	if owned {
		return checkRelationalOwnership(tx)
	}
	return rebuildUnownedRelationalSchema(tx)
}

// Caller must have validated the complete source catalog and disabled FKs.
// Shared by the strict private29 owner and the exact-profile successor owner;
// unlike the entry point this primitive neither admits nor relabels a version.
func rebuildUnownedRelationalSchema(tx *sql.Tx) (err error) {

	// Explicit legacy-local backfill is authorized by the migration contract.
	// root_override is registry metadata, never a replacement for tenant_roots.
	for _, ddl := range relationalRegistryDDL {
		if _, err = tx.Exec(ddl); err != nil {
			return err
		}
	}
	if _, err = tx.Exec(`INSERT INTO tenants(id,name,status,created_at,root_override)
 VALUES('local','local','active',datetime('now'),NULL)`); err != nil {
		return err
	}
	// These durable keys express reference ownership only, NEVER authentication or
	// active enrollment. They outlive registry deregistration and historical rows.
	// No task/project/feature parent is invented for derived or dangling IDs/paths.
	for _, spec := range relationalTables {
		if spec.runner != "" {
			stmt := `INSERT INTO tenant_runner_keys(tenant_id,runner_id) SELECT DISTINCT 'local',` + spec.runner + ` FROM ` + spec.name + ` WHERE ` + spec.runner + ` IS NOT NULL AND ` + spec.runner + `!='' ON CONFLICT DO NOTHING`
			if _, err = tx.Exec(stmt); err != nil {
				return err
			}
		}
		if spec.client != "" {
			if _, err = tx.Exec(`INSERT INTO tenant_client_keys(tenant_id,client_id) SELECT DISTINCT 'local',` + spec.client + ` FROM ` + spec.name + ` WHERE true ON CONFLICT DO NOTHING`); err != nil {
				return err
			}
		}
	}
	if _, err = tx.Exec(`INSERT INTO operator_install_claim SELECT * FROM entry_meta WHERE path='brain:system/install_claimed'`); err != nil {
		return err
	}
	// Carry every column of the permanent marker, not just a reconstructed boolean.
	// The private migration is the only writer here; future control access MUST use
	// the existing operator capability. No TenantStore method exposes this table.
	for _, ddl := range relationalClaimGuards {
		if _, err = tx.Exec(ddl); err != nil {
			return err
		}
	}

	indexes, err := relationalIndexes(tx, false)
	if err != nil {
		return err
	}
	sequences := map[string]int64{}
	for _, spec := range relationalTables {
		var seq int64
		e := tx.QueryRow("SELECT seq FROM sqlite_sequence WHERE name=?", spec.name).Scan(&seq)
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		if e == nil {
			sequences[spec.name] = seq
		}
		ddl := relationalDDL(spec)
		if _, err = tx.Exec(strings.Replace(ddl, "CREATE TABLE "+spec.name+" (", "CREATE TABLE p4_new_"+spec.name+" (", 1)); err != nil {
			return fmt.Errorf("create %s: %w", spec.name, err)
		}
		columns, e := relationalColumns(tx, spec.name)
		if e != nil {
			return e
		}
		where := ""
		if spec.name == "entry_meta" {
			// IS NOT includes legacy NULL primary keys. They must fail the new
			// NOT NULL constraint and roll back, never disappear from the copy.
			where = " WHERE path IS NOT 'brain:system/install_claimed'"
		}
		list := strings.Join(columns, ",")
		if _, err = tx.Exec("INSERT INTO p4_new_" + spec.name + " (tenant_id," + list + ") SELECT 'local'," + list + " FROM " + spec.name + where); err != nil {
			return fmt.Errorf("backfill %s: %w", spec.name, err)
		}
		var before, after int64
		if err = tx.QueryRow("SELECT count(*) FROM " + spec.name).Scan(&before); err != nil {
			return err
		}
		if err = tx.QueryRow("SELECT count(*) FROM p4_new_" + spec.name).Scan(&after); err != nil {
			return err
		}
		if spec.name == "entry_meta" {
			// Account for the entire source independently of the split predicate.
			var operatorRows int64
			if err = tx.QueryRow("SELECT count(*) FROM operator_install_claim").Scan(&operatorRows); err != nil {
				return err
			}
			after += operatorRows
		}
		if before != after {
			return fmt.Errorf("%s row count changed: %d -> %d", spec.name, before, after)
		}
	}
	// Copies exist for every table before any original is dropped. With FK
	// enforcement disabled there are no cascading deletes during the replacement.
	for _, spec := range relationalTables {
		if _, err = tx.Exec("DROP TABLE " + spec.name); err != nil {
			return err
		}
	}
	for _, spec := range relationalTables {
		if _, err = tx.Exec("ALTER TABLE p4_new_" + spec.name + " RENAME TO " + spec.name); err != nil {
			return err
		}
	}
	for table, seq := range sequences {
		if _, err = tx.Exec("UPDATE sqlite_sequence SET seq=max(seq,?) WHERE name=?", seq, table); err != nil {
			return err
		}
	}
	for _, ddl := range indexes {
		if _, err = tx.Exec(ddl); err != nil {
			return err
		}
	}
	for _, ddl := range requiredRelationalIndexes() {
		if _, err = tx.Exec(ddl); err != nil {
			return err
		}
	}
	for _, spec := range relationalTables {
		if _, err = tx.Exec("CREATE INDEX p4_owner_" + spec.name + " ON " + spec.name + "(tenant_id)"); err != nil {
			return err
		}
	}
	for _, ddl := range relationalReferenceGuards {
		if _, err = tx.Exec(ddl); err != nil {
			return err
		}
	}
	// Leave the old external-content FTS and its maintenance semantics intact for
	// downstream replacement IN THIS SAME TX. No tenant FTS/ranking claim is made.
	for _, ddl := range []string{createTriggerAfterInsert, createTriggerAfterDelete, createTriggerAfterUpdate} {
		if _, err = tx.Exec(ddl); err != nil {
			return err
		}
	}
	return checkRelationalOwnership(tx)
}

type relationalTable struct{ name, ddl, key, runner, client string }

// Explicit 26-table tenant manifest. The seven ordinary non-tenant tables are
// classified in auditRelationalInventory. FTS plus all four shadows are retained
// but belong to downstream P4.3, NOT to this relational completion boundary.
var relationalTables = []relationalTable{
	{"notes", createNotesTable, "", "", ""},
	{"links", createLinksTable, "", "", ""},
	{"tags", createTagsTable, "", "", ""},
	{"entry_meta", createEntryMetaTable, "path", "", ""},
	{"generated_tasks", createGeneratedTasksTable, "key", "", ""},
	{"event_log", createEventLogTable, "", "", ""},
	{"task_claims", createTaskClaimsTable, "", "runner_id", ""},
	{"task_dispatch_leases", createTaskDispatchLeasesTable, "", "assigned_runner_id", ""},
	{"task_placement_reasons", createTaskPlacementReasonsTable, "", "runner_id", ""},
	{"feature_assignments", createFeatureAssignmentsTable, "", "runner_id", ""},
	{"runners", createRunnersTable, "runner_id", "runner_id", ""},
	{"opencode_instances", createOpencodeInstancesTable, "instance_id", "runner_id", ""},
	{"project_pause_state", createProjectPauseStateTable, "project_id", "", ""},
	{"feature_pause_state", createFeaturePauseStateTable, "", "", ""},
	{"runner_pause_state", createRunnerPauseStateTable, "runner_id", "runner_id", ""},
	{"feature_cascade_roots", createFeatureCascadeRootsTable, "", "", ""},
	{"project_placement", createProjectPlacementTable, "project_id", "", ""},
	{"brain_clients", createBrainClientsTable, "client_id", "", "client_id"},
	{"brain_client_workspaces", createBrainClientWorkspacesTable, "", "", "client_id"},
	{"webhooks", createWebhooksTable, "id", "", ""},
	{"webhook_deliveries", createWebhookDeliveriesTable, "id", "", ""},
	{"note_embeddings", createNoteEmbeddingsTable, "", "", ""},
	{"note_embeddings_meta", createNoteEmbeddingsMetaTable, "", "", ""},
	{"attachments", createAttachmentsTable, "", "", ""},
	{"entry_attachments", createEntryAttachmentsTable, "", "", ""},
	{"attachment_derived", createAttachmentDerivedTable, "", "", ""},
}

var relationalRegistryDDL = []string{
	`CREATE TABLE tenants(id TEXT PRIMARY KEY NOT NULL CHECK(length(id)>0),name TEXT NOT NULL,status TEXT NOT NULL CHECK(status IN ('active','suspended','deleting','deleted')),created_at TEXT NOT NULL,root_override TEXT)`,
	`CREATE TABLE tenant_runner_keys(tenant_id TEXT NOT NULL CHECK(length(tenant_id)>0) REFERENCES tenants(id),runner_id TEXT NOT NULL CHECK(length(runner_id)>0),PRIMARY KEY(tenant_id,runner_id))`,
	`CREATE TABLE tenant_client_keys(tenant_id TEXT NOT NULL CHECK(length(tenant_id)>0) REFERENCES tenants(id),client_id TEXT NOT NULL CHECK(length(client_id)>0),PRIMARY KEY(tenant_id,client_id))`,
	`CREATE TABLE operator_install_claim(path TEXT PRIMARY KEY NOT NULL CHECK(path='brain:system/install_claimed'),project_id TEXT,access_count INTEGER DEFAULT 0,last_accessed TEXT,last_verified TEXT,created_at TEXT DEFAULT (datetime('now')))`,
}
var relationalClaimGuards = []string{
	`CREATE TRIGGER p4_claim_no_delete BEFORE DELETE ON operator_install_claim BEGIN SELECT RAISE(ABORT,'permanent installation claim'); END`,
	`CREATE TRIGGER p4_claim_no_update BEFORE UPDATE ON operator_install_claim BEGIN SELECT RAISE(ABORT,'permanent installation claim'); END`,
}
var relationalReferenceGuards = []string{
	`CREATE TRIGGER p4_link_target_delete BEFORE DELETE ON notes BEGIN UPDATE links SET target_id=NULL WHERE tenant_id=OLD.tenant_id AND target_id=OLD.id; END`,
}

// Transform ONLY reviewed, compile-time DDL, never arbitrary catalog SQL. Physical
// INTEGER PRIMARY KEY AUTOINCREMENT identities stay global and retain sequences;
// every logical primary/unique key and every parent reference includes tenant.
func relationalDDL(s relationalTable) string {
	ddl := strings.TrimSpace(strings.Replace(s.ddl, "IF NOT EXISTS ", "", 1))
	ddl = strings.Replace(ddl, " (", " (\n tenant_id TEXT NOT NULL CHECK(length(tenant_id)>0) REFERENCES tenants(id),", 1)
	ddl = strings.ReplaceAll(ddl, "TEXT PRIMARY KEY", "TEXT NOT NULL")
	ddl = strings.ReplaceAll(ddl, "PRIMARY KEY (", "PRIMARY KEY (tenant_id, ")
	ddl = strings.ReplaceAll(ddl, "UNIQUE(", "UNIQUE(tenant_id, ")
	ddl = strings.ReplaceAll(ddl, "TEXT UNIQUE NOT NULL", "TEXT NOT NULL")
	extra := ""
	if s.key != "" {
		extra += ", PRIMARY KEY(tenant_id," + s.key + ")"
	}
	if strings.Contains(ddl, "id INTEGER PRIMARY KEY AUTOINCREMENT") {
		extra += ", UNIQUE(tenant_id,id)"
	}
	if s.name == "notes" {
		extra += ", UNIQUE(tenant_id,path)"
	}
	if s.name == "attachments" {
		extra += ", UNIQUE(tenant_id,digest)"
	}
	if s.name == "note_embeddings_meta" {
		// Metadata describes one vector chunk, not merely its owning note.
		// Writers insert vectors first; legacy orphan metadata must fail the
		// post-copy foreign_key_check rather than survive as a false index hit.
		extra += ", FOREIGN KEY(tenant_id,note_id,chunk_index) REFERENCES note_embeddings(tenant_id,note_id,chunk_index) ON DELETE CASCADE"
	}
	if s.name == "entry_meta" {
		extra += ", CHECK(path!='brain:system/install_claimed')"
	}
	// Inline references become table-level composite references. SET NULL is
	// deliberately handled by p4_link_target_delete (only target_id is nullable).
	inline := regexp.MustCompile(`(\w+) INTEGER( NOT NULL)? REFERENCES (notes|attachments)\(id\) ON DELETE (CASCADE|SET NULL|RESTRICT)`)
	ddl = inline.ReplaceAllStringFunc(ddl, func(part string) string {
		m := inline.FindStringSubmatch(part)
		action := m[4]
		if action == "SET NULL" {
			action = "NO ACTION"
		}
		extra += ", FOREIGN KEY(tenant_id," + m[1] + ") REFERENCES " + m[3] + "(tenant_id,id) ON DELETE " + action
		return m[1] + " INTEGER" + m[2]
	})
	for _, r := range []struct{ column, parent, key string }{{"note_id", "notes", "id"}, {"webhook_id", "webhooks", "id"}} {
		ddl = strings.ReplaceAll(ddl, "FOREIGN KEY ("+r.column+") REFERENCES "+r.parent+"("+r.key+")", "FOREIGN KEY (tenant_id,"+r.column+") REFERENCES "+r.parent+"(tenant_id,"+r.key+")")
	}
	if s.runner != "" && s.name != "task_placement_reasons" {
		extra += ", FOREIGN KEY(tenant_id," + s.runner + ") REFERENCES tenant_runner_keys(tenant_id,runner_id)"
	}
	// Placement's empty runner means 'no candidate', not a missing enrollment.
	// NULLIF in a generated column allows that sentinel but fences nonempty IDs.
	if s.name == "task_placement_reasons" {
		ddl = strings.Replace(ddl, " (", " (\n runner_reference TEXT GENERATED ALWAYS AS (NULLIF(runner_id,'')) VIRTUAL,", 1)
		extra += ", FOREIGN KEY(tenant_id,runner_reference) REFERENCES tenant_runner_keys(tenant_id,runner_id)"
	}
	if s.client != "" {
		extra += ", FOREIGN KEY(tenant_id," + s.client + ") REFERENCES tenant_client_keys(tenant_id,client_id)"
	}
	pos := strings.LastIndex(ddl, ")")
	return ddl[:pos] + extra + ddl[pos:]
}

func relationalColumns(tx *sql.Tx, table string) ([]string, error) {
	// table_info omits generated/hidden columns, which would silently disappear
	// when a source table is replaced. Inventory them even though they cannot be
	// named in the INSERT column list.
	rows, err := tx.Query("PRAGMA table_xinfo(" + table + ")")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var cid, nn, pk, hidden int
		var name, typ string
		var def any
		if err := rows.Scan(&cid, &name, &typ, &nn, &def, &pk, &hidden); err != nil {
			return nil, err
		}
		if hidden != 0 {
			// The sole reviewed generated column is introduced by this component.
			// Source/final DDL validation below also checks its exact expression;
			// this exception cannot admit it in an unreviewed v28 source schema.
			if table == "task_placement_reasons" && name == "runner_reference" && hidden == 2 {
				continue
			}
			return nil, fmt.Errorf("unreviewed hidden/generated column %s.%s (hidden=%d)", table, name, hidden)
		}
		names = append(names, name)
	}
	return names, rows.Err()
}

func auditRelationalInventory(tx *sql.Tx) (bool, error) {
	return auditRelationalInventoryWithSearch(tx, nil, nil)
}

// Non-nil search manifests are private to the finalized FTS validator, which
// checks every definition before substituting the exact search inventory.
func auditRelationalInventoryWithSearch(tx *sql.Tx, searchTables map[string]bool, searchTriggers map[string]string) (bool, error) {
	return auditRelationalInventoryComplete(tx, searchTables, searchTriggers, false)
}

func auditRelationalInventoryComplete(tx *sql.Tx, searchTables map[string]bool, searchTriggers map[string]string, successor bool) (bool, error) {
	expected := map[string]bool{}
	if successor {
		// Not an allowance: every added object is checked byte-for-byte first.
		if err := checkSuccessorAdditions(tx); err != nil {
			return false, err
		}
		for _, name := range successorWorkloadTables() {
			expected[name] = true
		}
		expected["schema_provenance"] = true
	}
	for _, s := range relationalTables {
		expected[s.name] = true
	}
	for _, name := range strings.Fields(`schema_version api_tokens oauth_clients oauth_auth_codes oauth_access_tokens oauth_refresh_tokens tenant_roots notes_fts notes_fts_data notes_fts_idx notes_fts_docsize notes_fts_config sqlite_sequence`) {
		expected[name] = true
	}
	if searchTables != nil {
		for name := range legacyFTSDefinitions() {
			delete(expected, name)
		}
		for name := range searchTables {
			expected[name] = true
		}
	}
	rows, err := tx.Query("SELECT name FROM sqlite_schema WHERE type='table'")
	if err != nil {
		return false, err
	}
	actual := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			_ = rows.Close()
			return false, err
		}
		actual[name] = true
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return false, err
	}
	owned := actual["tenants"]
	// Approved inventory classifies optional ANALYZE statistics as operator-owned
	// engine metadata. They are not tenant rows. SQLite invalidates per-table
	// samples on DROP; the eventual outer migration may ANALYZE its final schema.
	for _, name := range []string{"sqlite_stat1", "sqlite_stat4"} {
		if actual[name] && !successor {
			expected[name] = true
		}
	}
	if owned {
		for _, name := range []string{"tenants", "tenant_runner_keys", "tenant_client_keys", "operator_install_claim"} {
			expected[name] = true
		}
	}
	for name := range actual {
		if !expected[name] {
			return false, fmt.Errorf("unclassified relational table %s", name)
		}
	}
	for name := range expected {
		if !actual[name] {
			return false, fmt.Errorf("missing relational table %s", name)
		}
	}
	for _, s := range relationalTables {
		columns, err := relationalColumns(tx, s.name)
		if err != nil {
			return false, err
		}
		hasOwner := false
		for _, c := range columns {
			if c == "tenant_id" {
				hasOwner = true
			}
		}
		if hasOwner != owned {
			return false, fmt.Errorf("partial relational schema: %s", s.name)
		}
		if !owned {
			// Names alone cannot detect dropped CHECK/UNIQUE constraints,
			// collations, defaults or table options. Accept only reviewed v28
			// definitions, never treat the version number as proof of their shape.
			// Noncanonical historical upgrade DDL requires explicit review rather
			// than a best-effort SQL rewrite that can silently remove semantics.
			var actualDDL string
			if err := tx.QueryRow("SELECT sql FROM sqlite_schema WHERE type='table' AND name=?", s.name).Scan(&actualDDL); err != nil {
				return false, err
			}
			if normalizeRelationalDDL(actualDDL) != normalizeRelationalDDL(s.ddl) {
				return false, fmt.Errorf("unreviewed v28 source definition %s", s.name)
			}
		}
	}
	// Rebuilds drop triggers. Refuse unreviewed extensions instead of losing them.
	triggers := map[string]string{"notes_ai": createTriggerAfterInsert, "notes_ad": createTriggerAfterDelete, "notes_au": createTriggerAfterUpdate}
	if searchTriggers != nil {
		triggers = make(map[string]string, len(searchTriggers))
		for name, ddl := range searchTriggers {
			triggers[name] = ddl
		}
	}
	if owned {
		for _, ddl := range append(append([]string{}, relationalClaimGuards...), relationalReferenceGuards...) {
			triggers[strings.Fields(ddl)[2]] = ddl
		}
	}
	if successor {
		for name, ddl := range successorSyncTriggers() {
			triggers[name] = ddl
		}
		for name, ddl := range successorControlDefinitions() {
			if strings.HasPrefix(ddl, "CREATE TRIGGER ") {
				triggers[name] = ddl
			}
		}
	}
	rows, err = tx.Query("SELECT name,sql FROM sqlite_schema WHERE type IN ('trigger','view')")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name, ddl string
		if err := rows.Scan(&name, &ddl); err != nil {
			return false, err
		}
		want, ok := triggers[name]
		if !ok || normalizeRelationalDDL(want) != normalizeRelationalDDL(ddl) {
			return false, fmt.Errorf("unreviewed relational trigger/view %s", name)
		}
		delete(triggers, name)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(triggers) != 0 {
		return false, fmt.Errorf("missing relational triggers")
	}
	return owned, nil
}

func requiredRelationalIndexes() []string {
	// Include indexes historically created only by upgrades, not createIndexes.
	legacy := append(append([]string{}, createIndexes...),
		"CREATE INDEX IF NOT EXISTS idx_brain_clients_host ON brain_clients(host_id)",
		"CREATE INDEX IF NOT EXISTS idx_brain_client_workspaces_project ON brain_client_workspaces(project_id)",
		"CREATE INDEX IF NOT EXISTS idx_brain_client_workspaces_host_path ON brain_client_workspaces(host_id, path)",
		"CREATE INDEX IF NOT EXISTS idx_brain_client_workspaces_git_remote ON brain_client_workspaces(git_remote)",
		"CREATE INDEX IF NOT EXISTS idx_feature_cascade_roots_project ON feature_cascade_roots(project_id)",
		"CREATE INDEX IF NOT EXISTS idx_feature_pause_state_paused ON feature_pause_state(project_id, paused)")
	var result []string
	for _, ddl := range legacy {
		for _, s := range relationalTables {
			if strings.Contains(ddl, " ON "+s.name+"(") {
				pos := strings.Index(ddl, "(")
				result = append(result, ddl[:pos+1]+"tenant_id,"+ddl[pos+1:])
				break
			}
		}
	}
	return result
}

// Reviewed lookup indexes are preserved with tenant as their leading key,
// including partial event dedup uniqueness and upgrade-created client indexes.
// Reject extra catalog indexes, even simple ones: accepting them without a
// durable definition manifest would make their later loss/change undetectable.
// The compile-time index list is the same contract for staging and repeat checks;
// operator/control-table indexes are untouched and outside this rebuild.
func relationalIndexes(tx *sql.Tx, owned bool) ([]string, error) {
	rows, err := tx.Query("SELECT tbl_name,sql FROM sqlite_schema WHERE type='index' AND sql IS NOT NULL ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tenantTables := map[string]bool{}
	reviewed := map[string]bool{}
	for _, ddl := range requiredRelationalIndexes() {
		reviewed[normalizeRelationalDDL(ddl)] = true
	}
	for _, s := range relationalTables {
		tenantTables[s.name] = true
		if owned {
			reviewed["CREATE INDEX p4_owner_"+s.name+" ON "+s.name+"(tenant_id)"] = true
		}
	}
	pattern := regexp.MustCompile(`(?i)^(CREATE (?:UNIQUE )?INDEX (?:IF NOT EXISTS )?\w+ ON \w+\s*\()([\w, ]+)(\).*)$`)
	var result []string
	for rows.Next() {
		var table, ddl string
		if err := rows.Scan(&table, &ddl); err != nil {
			return nil, err
		}
		if !tenantTables[table] {
			continue
		}
		candidate := ddl
		if !owned {
			m := pattern.FindStringSubmatch(ddl)
			if m == nil {
				return nil, fmt.Errorf("unreviewed index on %s: %s", table, ddl)
			}
			candidate = m[1] + "tenant_id," + m[2] + m[3]
		}
		if !reviewed[normalizeRelationalDDL(candidate)] {
			return nil, fmt.Errorf("unreviewed index on %s: %s", table, ddl)
		}
		result = append(result, candidate)
	}
	return result, rows.Err()
}

func checkRelationalOwnership(tx *sql.Tx) error {
	if _, err := relationalIndexes(tx, true); err != nil {
		return err
	}
	tables := []string{"tenant_runner_keys", "tenant_client_keys"}
	for _, s := range relationalTables {
		tables = append(tables, s.name)
	}
	for _, table := range tables {
		var n int64
		// Mandatory post-copy assertion: a failed backfill can NEVER count as success.
		if err := tx.QueryRow("SELECT count(*) FROM " + table + " WHERE tenant_id IS NULL OR tenant_id = ''").Scan(&n); err != nil {
			return err
		}
		if n != 0 {
			return fmt.Errorf("%s has %d unowned rows", table, n)
		}
	}
	if err := checkRelationalDefinitions(tx); err != nil {
		return err
	}
	rows, err := tx.Query("PRAGMA foreign_key_check")
	if err != nil {
		return err
	}
	if rows.Next() {
		_ = rows.Close()
		return fmt.Errorf("relational foreign_key_check failed")
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.Query("PRAGMA integrity_check")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var result string
		if err := rows.Scan(&result); err != nil {
			return err
		}
		if result != "ok" {
			return fmt.Errorf("relational integrity_check: %s", result)
		}
	}
	return rows.Err()
}

// Shared read-only definition checks; source auditing still uses its unchanged
// v28 manifest. Final query/catalog validation must not execute integrity writes.
func checkRelationalDefinitions(tx *sql.Tx) error {
	if _, err := relationalIndexes(tx, true); err != nil {
		return err
	}
	definitions := append([]string{}, relationalRegistryDDL...)
	definitions = append(definitions, relationalClaimGuards...)
	definitions = append(definitions, relationalReferenceGuards...)
	definitions = append(definitions, requiredRelationalIndexes()...)
	for _, s := range relationalTables {
		definitions = append(definitions, relationalDDL(s), "CREATE INDEX p4_owner_"+s.name+" ON "+s.name+"(tenant_id)")
	}
	for _, ddl := range definitions {
		words := strings.Fields(strings.ReplaceAll(ddl, "IF NOT EXISTS ", ""))
		name := words[2]
		if words[1] == "UNIQUE" {
			name = words[3]
		}
		if i := strings.Index(name, "("); i >= 0 {
			name = name[:i]
		}
		var actual string
		if err := tx.QueryRow("SELECT sql FROM sqlite_schema WHERE name=?", name).Scan(&actual); err != nil {
			return fmt.Errorf("missing relational definition %s: %w", name, err)
		}
		if normalizeRelationalDDL(actual) != normalizeRelationalDDL(ddl) {
			return fmt.Errorf("changed relational definition %s", name)
		}
	}
	return nil
}

func normalizeRelationalDDL(ddl string) string {
	// This is deliberately NOT general SQL normalization. Preserve the entire
	// body byte-for-byte, including quoted text, escapes and literal whitespace.
	// Only remove catalog header syntax and the statement's outer terminator.
	ddl = strings.TrimSuffix(strings.TrimSpace(ddl), ";")
	for _, prefix := range []string{"CREATE TABLE ", "CREATE INDEX ", "CREATE UNIQUE INDEX ", "CREATE TRIGGER "} {
		if strings.HasPrefix(ddl, prefix+"IF NOT EXISTS ") {
			ddl = prefix + strings.TrimPrefix(ddl, prefix+"IF NOT EXISTS ")
			break
		}
	}
	// SQLite quotes the table identifier on RENAME. Unquote only that header
	// identifier, restricted to our simple internal names; never quoted body text.
	header := regexp.MustCompile(`^CREATE TABLE "([a-z_][a-z0-9_]*)"(\s*\()`)
	return header.ReplaceAllString(ddl, "CREATE TABLE ${1}${2}")
}

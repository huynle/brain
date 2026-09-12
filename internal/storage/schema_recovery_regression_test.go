package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Synthetic additive catalog transcribed from cd22b4bd, not a database copied
// from main or production. Main's v29 means bulk jobs, NOT private P4 ownership.
const main29FixtureSQL = `
CREATE TABLE bulk_jobs (
 tenant_id TEXT NOT NULL, id TEXT NOT NULL, request_id TEXT NOT NULL,
 request_hash TEXT NOT NULL, request_json TEXT NOT NULL, operation TEXT NOT NULL,
 state TEXT NOT NULL, submitted_by TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
 PRIMARY KEY(tenant_id,id), UNIQUE(tenant_id,request_id));
CREATE TABLE bulk_job_items (
 tenant_id TEXT NOT NULL, job_id TEXT NOT NULL, sequence INTEGER NOT NULL,
 path TEXT NOT NULL, entry_id TEXT NOT NULL, title TEXT NOT NULL, fingerprint TEXT NOT NULL,
 state TEXT NOT NULL, attempts INTEGER NOT NULL DEFAULT 0, error TEXT NOT NULL DEFAULT '', destination TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(tenant_id,job_id,sequence), UNIQUE(tenant_id,job_id,path),
 FOREIGN KEY(tenant_id,job_id) REFERENCES bulk_jobs(tenant_id,id) ON DELETE CASCADE);
CREATE INDEX bulk_items_pending ON bulk_job_items(tenant_id,job_id,state,sequence);
INSERT INTO bulk_jobs VALUES('local','job','request','hash','{}','update','running','operator','then','now');
INSERT INTO bulk_job_items VALUES('local','job',1,'projects/p/task/same.md','same','source','fingerprint','uncertain',2,'interrupted','');
`

const main30FixtureSQL = `
CREATE TABLE execution_budgets (tenant_id TEXT NOT NULL,project TEXT NOT NULL,id TEXT NOT NULL,timezone TEXT NOT NULL,unit TEXT NOT NULL,limit_units INTEGER NOT NULL,revision INTEGER NOT NULL,PRIMARY KEY(tenant_id,project,id));
CREATE TABLE budget_reservations (tenant_id TEXT NOT NULL,project TEXT NOT NULL,budget_id TEXT NOT NULL,id TEXT NOT NULL,parent_id TEXT NOT NULL,window TEXT NOT NULL,units INTEGER NOT NULL,state TEXT NOT NULL,PRIMARY KEY(tenant_id,project,budget_id,id),FOREIGN KEY(tenant_id,project,budget_id) REFERENCES execution_budgets(tenant_id,project,id));
CREATE TABLE supervisor_checkpoint_versions (tenant_id TEXT NOT NULL,project TEXT NOT NULL,id TEXT NOT NULL,revision INTEGER NOT NULL,payload TEXT NOT NULL,PRIMARY KEY(tenant_id,project,id,revision));
CREATE TABLE supervisor_checkpoints (tenant_id TEXT NOT NULL, project TEXT NOT NULL, id TEXT NOT NULL, revision INTEGER NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(tenant_id,project,id));
CREATE TABLE supervisor_operations (
 tenant_id TEXT NOT NULL, actor TEXT NOT NULL, id TEXT NOT NULL, operation TEXT NOT NULL,
 digest TEXT NOT NULL, state TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(tenant_id,actor,id));
INSERT INTO execution_budgets VALUES('local','p','budget','UTC','tasks',10,3);
INSERT INTO budget_reservations VALUES('local','p','budget','reservation','','2026-09-12',2,'committed');
INSERT INTO supervisor_checkpoint_versions VALUES('local','p','checkpoint',2,'{"answer":"keep"}');
INSERT INTO supervisor_checkpoints VALUES('local','p','checkpoint',2,'{"answer":"keep"}');
INSERT INTO supervisor_operations VALUES('local','operator','operation','trigger','digest','outcome_unknown','then','now','keep');
`

// Main installs these outside the numbered migration blocks. Preserve populated
// sync state AND its note triggers; a version stamp alone cannot identify it.
const mainSyncFixtureSQL = `
CREATE TABLE entry_sync_devices (id TEXT PRIMARY KEY, data TEXT NOT NULL);
CREATE TABLE entry_sync_identity (id INTEGER PRIMARY KEY CHECK(id=1), epoch TEXT NOT NULL);
CREATE TABLE entry_sync_changes (seq INTEGER PRIMARY KEY AUTOINCREMENT, path TEXT NOT NULL UNIQUE);
CREATE TABLE entry_sync_operations (id TEXT PRIMARY KEY, hash TEXT NOT NULL, status INTEGER NOT NULL DEFAULT 0, body TEXT NOT NULL DEFAULT '');
CREATE TRIGGER entry_sync_insert AFTER INSERT ON notes BEGIN
 INSERT OR REPLACE INTO entry_sync_changes(path) VALUES (new.path);
END;
CREATE TRIGGER entry_sync_update AFTER UPDATE ON notes BEGIN
 INSERT OR REPLACE INTO entry_sync_changes(path) VALUES (old.path);
 INSERT OR REPLACE INTO entry_sync_changes(path) VALUES (new.path);
END;
CREATE TRIGGER entry_sync_delete AFTER DELETE ON notes BEGIN
 INSERT OR REPLACE INTO entry_sync_changes(path) VALUES (old.path);
END;
INSERT INTO entry_sync_devices VALUES('device','{"pending":true}');
INSERT INTO entry_sync_identity VALUES(1,'synthetic-epoch');
INSERT INTO entry_sync_changes(path) SELECT path FROM notes;
INSERT INTO entry_sync_operations VALUES('operation','hash',409,'preserve conflict');
`

// Includes every table (including FTS shadows and additive tables), catalog SQL,
// catalog root pages, and sequence high-water marks. No production inventory
// is consulted, so an unreviewed table cannot disappear from this comparison.
func recoverySnapshot(t *testing.T, db *sql.DB) map[string][]string {
	t.Helper()
	result := map[string][]string{"sqlite_schema": relationalSnapshot(t, db, "sqlite_schema")}
	for _, table := range compatibilityRows(t, db, "SELECT name FROM sqlite_schema WHERE type='table' ORDER BY name") {
		result[table] = relationalSnapshot(t, db, `"`+strings.ReplaceAll(table, `"`, `""`)+`"`)
	}
	return result
}

func recoveryIntegrity(t *testing.T, db *sql.DB) {
	t.Helper()
	migrationFK(t, db)
	if got := compatibilityRows(t, db, "PRAGMA integrity_check"); !reflect.DeepEqual(got, []string{"ok"}) {
		t.Fatalf("integrity_check: %v", got)
	}
	if got := relationalSnapshot(t, db, "pragma_foreign_key_check"); len(got) != 0 {
		t.Fatalf("foreign_key_check: %v", got)
	}
}

func recoveryPath(t *testing.T, db *sql.DB) string {
	t.Helper()
	var seq int
	var name, path string
	if err := db.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSchemaCompatibilityMainAdditionsBeforeMutation(t *testing.T) {
	for _, version := range []int{29, 30} {
		for _, constructor := range []string{"New", "NewWithDB"} {
			t.Run(fmt.Sprintf("v%d/%s", version, constructor), func(t *testing.T) {
				db := relationalFixture(t)
				relationalExec(t, db, main29FixtureSQL+mainSyncFixtureSQL)
				if version == 30 {
					relationalExec(t, db, main30FixtureSQL)
				}
				relationalExec(t, db, "INSERT INTO schema_version VALUES(29,'main bulk jobs')")
				if version == 30 {
					relationalExec(t, db, "INSERT INTO schema_version VALUES(30,'main budgets/supervisor')")
				}
				before := recoverySnapshot(t, db)
				journal := compatibilityRows(t, db, "PRAGMA journal_mode")
				var s *StorageLayer
				var err error
				if constructor == "New" {
					path := recoveryPath(t, db)
					if err := db.Close(); err != nil {
						t.Fatal(err)
					}
					s, err = New(path)
					db = compatibilityDB(t, path)
					db.SetMaxOpenConns(1)
					relationalExec(t, db, "PRAGMA foreign_keys=ON")
				} else {
					s, err = NewWithDB(db)
				}
				if s != nil {
					s.Close()
					t.Fatal("returned storage for incompatible main schema")
				}
				if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("%d is newer than supported version 28", version)) {
					t.Fatalf("expected version preflight refusal, got %v", err)
				}
				if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
					t.Fatal("constructor changed catalog/rows")
				}
				if !reflect.DeepEqual(journal, compatibilityRows(t, db, "PRAGMA journal_mode")) {
					t.Fatal("constructor changed journal mode")
				}
				recoveryIntegrity(t, db)
			})
		}
	}
}

func TestTenantMigrationRejectsMainSyncCatalog(t *testing.T) {
	db, _ := completeMigrationFixture(t)
	relationalExec(t, db, mainSyncFixtureSQL)
	before := recoverySnapshot(t, db)
	// Keep v28: this must be inventory rejection, not the future-version guard.
	err := migrateTenantSchema(context.Background(), db, nil)
	if err == nil || !strings.Contains(err.Error(), "entry_sync_") {
		t.Fatalf("expected sync inventory refusal: %v", err)
	}
	if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
		t.Fatal("private migration changed sync catalog/rows")
	}
	recoveryIntegrity(t, db)
}

func TestTenantMigrationPostcommitChild(t *testing.T) {
	if os.Getenv("BRAIN_P4_POSTCOMMIT_CHILD") != "1" {
		t.Skip("subprocess only")
	}
	// Parent supplies only its generated fixture path over stdin; no operator DB
	// argument or migration CLI is introduced.
	var path string
	if err := json.NewDecoder(os.Stdin).Decode(&path); err != nil {
		t.Fatal(err)
	}
	db := compatibilityDB(t, path)
	db.SetMaxOpenConns(1)
	relationalExec(t, db, "PRAGMA wal_autocheckpoint=0")
	if err := migrateTenantSchema(context.Background(), db, nil); err != nil {
		t.Fatal(err)
	}
	if v, err := GetSchemaVersion(db); err != nil || v != 29 {
		t.Fatalf("commit version %d: %v", v, err)
	}
	recoveryIntegrity(t, db)
	if err := json.NewEncoder(os.Stdout).Encode(recoverySnapshot(t, db)); err != nil {
		t.Fatal(err)
	}
	// AFTER Commit returned, unlike checkpoint("published"). Deliberately skip
	// database Close and test cleanups, leaving recovery to another process.
	os.Exit(0)
}

func TestTenantMigrationPostcommitExitReopen(t *testing.T) {
	if CurrentSchemaVersion != 28 || pendingTenantSchemaVersion != 29 {
		t.Fatal("runtime/private version contract changed")
	}
	db, blob := completeMigrationFixture(t)
	relationalExec(t, db, `UPDATE notes SET body='recovery body', raw_content='--- raw ---', checksum='keep-checksum', metadata='{"status":"active"}' WHERE id=10;
UPDATE notes SET lead=NULL, body='different target body' WHERE id=11;`)
	path := recoveryPath(t, db)
	var brain string
	if err := db.QueryRow("SELECT brain_absolute FROM tenant_roots WHERE tenant_id='local'").Scan(&brain); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	identities := map[string]os.FileInfo{}
	for _, p := range []string{brain, filepath.Dir(filepath.Dir(filepath.Dir(blob))), blob, filepath.Join(brain, "untouched.md")} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		identities[p] = info
		if !info.IsDir() {
			files[p], err = os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	// Independent legacy-column projection: ownership is the only added column;
	// install claim moves to its own table. Capture before the child exists.
	legacy := map[string][]string{}
	for _, table := range relationalTenantTables {
		cols := compatibilityRows(t, db, "SELECT name FROM pragma_table_info('"+table+"') ORDER BY cid")
		where := ""
		if table == "entry_meta" {
			where = " WHERE path!='brain:system/install_claimed'"
		}
		query := "(SELECT " + strings.Join(cols, ",") + " FROM " + table + where + ")"
		legacy[query] = relationalSnapshot(t, db, query)
	}
	controls := map[string][]string{}
	for _, table := range append([]string{"sqlite_sequence"}, relationalControlTables...) {
		controls[table] = relationalSnapshot(t, db, table)
	}
	// Root pages may move during a rebuild; SQL definitions of control objects
	// must not. Capture this separately from the postcommit full catalog oracle.
	controlCatalogQuery := "SELECT type || ':' || name || ':' || COALESCE(sql,'') FROM sqlite_schema WHERE tbl_name IN ('" + strings.Join(relationalControlTables, "','") + "') ORDER BY type,name"
	controlCatalog := compatibilityRows(t, db, controlCatalogQuery)
	claim := relationalSnapshot(t, db, "(SELECT * FROM entry_meta WHERE path='brain:system/install_claimed')")
	relationalExec(t, db, "PRAGMA journal_mode=WAL")
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTenantMigrationPostcommitChild$")
	cmd.Env = append(os.Environ(), "BRAIN_P4_POSTCOMMIT_CHILD=1")
	input, err := json.Marshal(path)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdin = strings.NewReader(string(input))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("postcommit child: %v\n%s", err, output)
	}
	var committed map[string][]string
	if err := json.Unmarshal(output, &committed); err != nil {
		t.Fatalf("missing committed snapshot: %v\n%s", err, output)
	}
	if info, err := os.Stat(path + "-wal"); err != nil || info.Size() == 0 {
		t.Fatalf("expected committed WAL: %v", err)
	}
	// Supported runtime must STILL refuse this artifact. Reopen below is test-only.
	if s, err := New(path); s != nil || err == nil || !strings.Contains(err.Error(), "29 is newer than supported version 28") {
		if s != nil {
			s.Close()
		}
		t.Fatalf("public constructor accepted private29: %v", err)
	}
	db = compatibilityDB(t, path)
	db.SetMaxOpenConns(1)
	relationalExec(t, db, "PRAGMA foreign_keys=ON")
	recoveryIntegrity(t, db)
	if !reflect.DeepEqual(committed, recoverySnapshot(t, db)) {
		t.Fatal("exit/reopen or constructor refusal changed committed catalog/rows")
	}
	for query, want := range legacy {
		if !reflect.DeepEqual(want, relationalSnapshot(t, db, query)) {
			t.Fatalf("legacy content changed: %s", query)
		}
	}
	for _, table := range relationalTenantTables {
		if got := compatibilityRows(t, db, "SELECT DISTINCT tenant_id FROM "+table); !reflect.DeepEqual(got, []string{"local"}) {
			t.Fatalf("owner %s: %v", table, got)
		}
	}
	for table, want := range controls {
		query := table
		if table == "schema_version" {
			query = "(SELECT * FROM schema_version WHERE version!=29)"
		}
		if !reflect.DeepEqual(want, relationalSnapshot(t, db, query)) {
			t.Fatalf("control changed: %s", table)
		}
	}
	if !reflect.DeepEqual(claim, relationalSnapshot(t, db, "operator_install_claim")) {
		t.Fatal("install claim changed")
	}
	if !reflect.DeepEqual(controlCatalog, compatibilityRows(t, db, controlCatalogQuery)) {
		t.Fatal("control catalog definitions changed")
	}
	for i := 0; i < 2; i++ {
		if err := migrateTenantSchema(ctx, db, nil); err != nil {
			t.Fatalf("repeat validation: %v", err)
		}
		recoveryIntegrity(t, db)
		if !reflect.DeepEqual(committed, recoverySnapshot(t, db)) {
			t.Fatal("validation changed committed catalog/rows")
		}
	}
	for p, before := range identities {
		after, err := os.Stat(p)
		if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() {
			t.Fatalf("root/file identity changed: %s: %v", p, err)
		}
	}
	for p, before := range files {
		after, err := os.ReadFile(p)
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("file bytes changed: %s: %v", p, err)
		}
	}
}

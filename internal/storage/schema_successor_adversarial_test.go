package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
)

func TestSuccessorSearchRouting(t *testing.T) {
	db := successorPrivateFixture(t)
	s := &TenantStore{db: db, tenantID: tenant.MustParse("a")}
	public, err := s.SearchNotes(context.Background(), "alpha", nil)
	if err != nil || len(public) != 1 {
		t.Fatalf("successor public search: %v %v", public, err)
	}
	tx, e := db.Begin()
	collisionMust(t, e)
	defer tx.Rollback()
	hits, n, e := queryTenantFTS(context.Background(), tx, tenant.MustParse("a"), "alpha", 10)
	if e != nil || n != 1 || len(hits) != 1 || hits[0].Title != "A" {
		t.Fatalf("successor FTS query: %v %d %v", hits, n, e)
	}
}

func TestSuccessorRejectsTargetMutations(t *testing.T) {
	for name, mutation := range map[string]string{
		"registry-extra-index": "CREATE INDEX alien ON tenants(name)",
		"control-extra-index":  "CREATE INDEX alien ON api_tokens(name)",
		"sync-extra-index":     "CREATE INDEX alien ON entry_sync_devices(data)",
		"sync-index-order":     "DROP INDEX entry_sync_changes_owner_sequence; CREATE INDEX entry_sync_changes_owner_sequence ON entry_sync_changes(seq,tenant_id)",
		"sync-trigger":         "DROP TRIGGER entry_sync_update; CREATE TRIGGER entry_sync_update AFTER UPDATE ON notes BEGIN SELECT 1; END",
		"unknown-table":        "CREATE TABLE unknown(x)",
		"unknown-view":         "CREATE VIEW unknown AS SELECT path FROM notes",
		"missing-epoch":        "DELETE FROM entry_sync_identity WHERE tenant_id='a'",
		"missing-highwater":    "DELETE FROM sqlite_sequence WHERE name='entry_sync_changes'",
		"lowered-highwater":    "UPDATE sqlite_sequence SET seq=0 WHERE name='entry_sync_changes'",
		"missing-control":      "DROP TRIGGER schema_provenance_no_delete",
		"missing-fts-guard":    "DROP TRIGGER p4_fts_rowid_insert_check",
		"foreign-owner":        "PRAGMA foreign_keys=OFF; INSERT INTO entry_sync_operations VALUES('missing','id','hash',0,'')",
	} {
		t.Run(name, func(t *testing.T) {
			db := successorPrivateFixture(t)
			relationalExec(t, db, mutation)
			before := recoverySnapshot(t, db)
			if e := migrateSuccessorSchema(context.Background(), db, nil); e == nil {
				t.Error("invalid successor accepted")
			}
			if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
				t.Fatal("invalid successor repaired")
			}
		})
	}
}

func TestSuccessorRollbackAndPublication(t *testing.T) {
	for _, source := range provenanceSources[:5] {
		for _, stage := range []string{"reserved", "snapshot", "relational", "fts", "sync", "validated", "published"} {
			t.Run(source.profile+"/"+stage, func(t *testing.T) {
				db := successorSourceFixture(t, source.revision)
				before := recoverySnapshot(t, db)
				reached := false
				e := migrateSuccessorSchema(context.Background(), db, func(at string) error {
					if at == stage {
						reached = true
						return fmt.Errorf("injected %s", at)
					}
					return nil
				})
				if e == nil || !reached {
					t.Fatalf("missing rollback checkpoint %s: %v", stage, e)
				}
				if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
					t.Fatal("partial migration persisted")
				}
				recoveryIntegrity(t, db)
				migrationFK(t, db)
			})
		}
	}
}

func TestSuccessorUnknownSourceNoPragmaMutation(t *testing.T) {
	db := successorSourceFixture(t, provenanceSources[5].revision)
	relationalExec(t, db, "CREATE TABLE alien(x); PRAGMA foreign_keys=OFF; PRAGMA query_only=ON")
	before := recoverySnapshot(t, db)
	if e := migrateSuccessorSchema(context.Background(), db, nil); e == nil {
		t.Fatal("unknown admitted")
	}
	var fk int
	collisionMust(t, db.QueryRow("PRAGMA foreign_keys").Scan(&fk))
	if fk != 0 {
		t.Fatal("refusal wrote FK pragma")
	}
	if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
		t.Fatal("refusal mutated source")
	}
}

func TestSuccessorNonlocalSourceLedgerRefusal(t *testing.T) {
	for _, table := range []string{"bulk_jobs", "execution_budgets", "supervisor_operations"} {
		t.Run(table, func(t *testing.T) {
			db := successorSourceFixture(t, provenanceSources[5].revision)
			statements := map[string]string{
				"bulk_jobs":             `INSERT INTO bulk_jobs VALUES('foreign','job','req','hash','{}','update','queued','actor','created','updated')`,
				"execution_budgets":     `INSERT INTO execution_budgets VALUES('foreign','p','budget','UTC','tasks',1,1)`,
				"supervisor_operations": `INSERT INTO supervisor_operations VALUES('foreign','actor','op','prompt','digest','outcome_unknown','created','updated','')`,
			}
			relationalExec(t, db, statements[table])
			before := recoverySnapshot(t, db)
			if e := migrateSuccessorSchema(context.Background(), db, nil); e == nil {
				t.Fatal("foreign main ledger reassigned")
			}
			if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
				t.Fatal("foreign rows changed")
			}
		})
	}
}

func TestSuccessorPostcommitExitReopen(t *testing.T) {
	if path := os.Getenv("BRAIN_P31_POSTCOMMIT_COPY"); path != "" {
		db, e := sql.Open("sqlite", path)
		if e != nil {
			panic(e)
		}
		db.SetMaxOpenConns(1)
		if e = migrateSuccessorSchema(context.Background(), db, nil); e != nil {
			panic(e)
		}
		os.Exit(0) // intentionally no Close or test cleanup after successful Commit
	}
	db := successorSourceFixture(t, provenanceSources[5].revision)
	relationalExec(t, db, `PRAGMA journal_mode=WAL;
 INSERT INTO entry_sync_operations VALUES('reserved','hash',0,'');
 INSERT INTO entry_sync_devices VALUES('device',' { "pending": [1] } ');
 UPDATE entry_sync_identity SET epoch='0123456789abcdef0123456789abcdef';
 INSERT INTO notes(path,short_id,title) VALUES('deleted','deleted0','gone'); DELETE FROM notes WHERE path='deleted';
 UPDATE sqlite_sequence SET seq=900 WHERE name='entry_sync_changes'`)
	before := map[string][]string{}
	for _, table := range []string{"entry_sync_operations", "entry_sync_devices", "entry_sync_identity", "entry_sync_changes", "sqlite_sequence"} {
		before[table] = relationalSnapshot(t, db, table)
	}
	path := recoveryPath(t, db)
	collisionMust(t, db.Close())
	cmd := exec.Command(os.Args[0], "-test.run=^TestSuccessorPostcommitExitReopen$")
	cmd.Env = append(os.Environ(), "BRAIN_P31_POSTCOMMIT_COPY="+path)
	if output, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("postcommit child: %v %s", e, output)
	}
	reopened := compatibilityDB(t, path)
	relationalExec(t, reopened, "PRAGMA foreign_keys=ON")
	projections := map[string]string{"entry_sync_operations": "id,hash,status,body", "entry_sync_devices": "id,data", "entry_sync_identity": "id,epoch", "entry_sync_changes": "seq,path", "sqlite_sequence": "*"}
	for table, want := range before {
		got := relationalSnapshot(t, reopened, "(SELECT "+projections[table]+" FROM "+table+")")
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("crash changed %s", table)
		}
	}
	snapshot := recoverySnapshot(t, reopened)
	// FTS5's integrity-check is syntactically an INSERT command, not a repair.
	// Repeats roll back their validation transaction and must preserve all bytes.
	for i := 0; i < 2; i++ {
		collisionMust(t, migrateSuccessorSchema(context.Background(), reopened, nil))
	}
	if !reflect.DeepEqual(snapshot, recoverySnapshot(t, reopened)) {
		t.Fatal("reopen replay/repair")
	}
	recoveryIntegrity(t, reopened)
}

func TestSuccessorPartialLedgerRefused(t *testing.T) {
	db := compatibilityDB(t, filepath.Join(t.TempDir(), "partial.db"))
	for _, ddl := range ledgerFixtureDDL {
		relationalExec(t, db, ddl)
	}
	relationalExec(t, db, `INSERT INTO tenants VALUES('a','A','active','now',NULL)`)
	s := ledgerAPI(t, db, tenant.MustParse("a"))
	assertLedgerDenied(t, s, tenant.Into(context.Background(), tenant.MustParse("a")))
}

func TestSuccessorRejectsInvertedFTSCorruption(t *testing.T) {
	db := successorPrivateFixture(t)
	var id string
	collisionMust(t, db.QueryRow("SELECT internal_id FROM tenant_fts WHERE tenant_id='a'").Scan(&id))
	// Leave the content table intact while removing an inverted-index segment.
	// A row-content comparison and ordinary SQLite integrity_check both miss it.
	relationalExec(t, db, "DELETE FROM fts_t_"+id+"_data WHERE id>10")
	before := recoverySnapshot(t, db)
	if e := migrateSuccessorSchema(context.Background(), db, nil); e == nil {
		t.Fatal("corrupt inverted FTS accepted")
	}
	if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
		t.Fatal("corrupt FTS repaired")
	}
}

func TestSuccessorPrivateAllOwnedRows(t *testing.T) {
	f := newCollisionFixture(t)
	db := f.owner.db
	// The original collision fixture intentionally omits foreign filesystem
	// readiness. Supply actual independent foreign CAS bytes, not a root waiver.
	resolver, e := tenantfs.New(registryHandle(t, f.owner), t.TempDir())
	collisionMust(t, e)
	id := tenant.MustParse("acme")
	_, e = resolver.Provision(context.Background(), id, tenantfs.Overrides{BrainRoot: t.TempDir(), BlobRoot: t.TempDir()})
	collisionMust(t, e)
	data := []byte("real local CAS bytes")
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	path, e := resolver.BlobPath(context.Background(), id, digest)
	collisionMust(t, e)
	collisionMust(t, os.MkdirAll(filepath.Dir(path), 0700))
	collisionMust(t, os.WriteFile(path, data, 0600))
	a, b := f.snapshot(t, "local"), f.snapshot(t, "acme")
	claim := relationalSnapshot(t, db, "operator_install_claim")
	roots := relationalSnapshot(t, db, "tenant_roots")
	collisionMust(t, migrateSuccessorSchema(context.Background(), db, nil))
	f.unchanged(t, "local", a)
	f.unchanged(t, "acme", b)
	if !reflect.DeepEqual(claim, relationalSnapshot(t, db, "operator_install_claim")) || !reflect.DeepEqual(roots, relationalSnapshot(t, db, "tenant_roots")) {
		t.Fatal("private claim/roots changed")
	}
	snapshot := recoverySnapshot(t, db)
	collisionMust(t, migrateSuccessorSchema(context.Background(), db, nil))
	if !reflect.DeepEqual(snapshot, recoverySnapshot(t, db)) {
		t.Fatal("private repeat mutated")
	}
}

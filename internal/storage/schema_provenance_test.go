package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
)

// Immutable Git objects, never main/HEAD or the target's mutable DDL. These
// fixtures execute archived InitSchema, including its migrations and initEntrySync.
// Missing history is a failure, not permission to fall back to today's schema.
var provenanceSources = []struct{ profile, revision string }{
	{"main28", "c0b28634355a91b24ab9e9415978f12866d3b5cd"},
	{"main29", "44f963bbb0c5102390a06f6548088eda6d0f9832"},
	{"main30-pre-sync", "3d2baaf450b1b93f981f6e03eb3ab07766237c21"},
	{"main30-initial-sync", "d21ba2d948738bed6456c66f4453fc96a593d1b4"},
	{"main30-devices", "7132bf0ce97cb961b3d233c915c1a99ef63708c2"},
	{"main30-devices", "cd22b4bdc3b5229621169fe5b214d7ffd12a6015"},
}

// Keep the complete archived schema.go logic. Ancillary files contribute only
// their constants and initEntrySync, not receivers/services or their dependencies.
func archivedSchemaProgram(t *testing.T, revision string) string {
	t.Helper()
	show := func(file string) string {
		b, err := exec.Command("git", "show", revision+":internal/storage/"+file).CombinedOutput()
		if err != nil {
			t.Fatalf("archived source %s/%s: %v: %s", revision, file, err, b)
		}
		return string(b)
	}
	schema := show("schema.go")
	files := []string{"tenant_roots.go"}
	if strings.Contains(schema, "createBulkJobsTable") {
		files = append(files, "bulk_jobs.go")
	}
	if strings.Contains(schema, "createExecutionBudgets") {
		files = append(files, "execution_budgets.go", "supervisor_checkpoints.go", "supervisor_operations.go")
	}
	if strings.Contains(schema, "initEntrySync") {
		files = append(files, "entry_sync.go")
	}
	var declarations strings.Builder
	for _, file := range files {
		source := show(file)
		set := token.NewFileSet()
		parsed, err := parser.ParseFile(set, file, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range parsed.Decls {
			keep := false
			switch d := decl.(type) {
			case *ast.GenDecl:
				keep = d.Tok == token.CONST
			case *ast.FuncDecl:
				keep = d.Recv == nil && d.Name.Name == "initEntrySync"
			}
			if keep {
				declarations.WriteString(source[set.Position(decl.Pos()).Offset:set.Position(decl.End()).Offset])
				declarations.WriteByte('\n')
			}
		}
	}
	schema = strings.Replace(schema, "package storage", "package main", 1)
	schema = strings.Replace(schema, "\"fmt\"", "\"fmt\"\n\"os\"\n_ \"github.com/glebarez/go-sqlite\"", 1)
	return schema + "\n" + declarations.String() + `
func main() {
 db, err := sql.Open("sqlite", os.Args[1]); if err != nil { panic(err) }
 db.SetMaxOpenConns(1)
 if _, err = db.Exec("PRAGMA foreign_keys=ON"); err != nil { panic(err) }
 if err = InitSchema(db); err != nil { panic(err) }
 if err = db.Close(); err != nil { panic(err) }
}
`
}

var archivedFixtureCache struct {
	sync.Mutex
	files map[string][]byte
}

func archivedSchemaFixture(t *testing.T, revision string) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "source.db")
	func() {
		archivedFixtureCache.Lock()
		defer archivedFixtureCache.Unlock()
		if data, ok := archivedFixtureCache.files[revision]; ok {
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			return
		}
		runArchivedSchema(t, revision, path)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if archivedFixtureCache.files == nil {
			archivedFixtureCache.files = map[string][]byte{}
		}
		archivedFixtureCache.files[revision] = data
	}()
	db := compatibilityDB(t, path)
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatal(err)
	}
	return db
}

func runArchivedSchema(t *testing.T, revision, path string) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "main.go")
	if err := os.WriteFile(file, []byte(archivedSchemaProgram(t, revision)), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", file, path)
	cmd.Env = append(os.Environ(), "CI=1", "GOPROXY=off", "GOTOOLCHAIN=local")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("archived InitSchema %s: %v\n%s", revision, err, out)
	}
}

func TestSchemaProvenanceActualMain(t *testing.T) {
	for _, source := range provenanceSources {
		t.Run(source.profile+"/"+source.revision[:8], func(t *testing.T) {
			db := archivedSchemaFixture(t, source.revision)
			// Independent exact ordered catalog pin; no target-DDL normalization.
			rows, err := db.Query("SELECT type,name,tbl_name,coalesce(sql,'') FROM sqlite_schema ORDER BY type,name")
			if err != nil {
				t.Fatal(err)
			}
			var catalog [][4]string
			for rows.Next() {
				var row [4]string
				if err := rows.Scan(&row[0], &row[1], &row[2], &row[3]); err != nil {
					t.Fatal(err)
				}
				catalog = append(catalog, row)
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			_ = rows.Close()
			encoded, err := json.Marshal(catalog)
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("archived catalog %s: %d objects sha256=%x", source.revision, len(catalog), sha256.Sum256(encoded))
			assertSourceClassification(t, db, source.profile)
			recoveryIntegrity(t, db)
		})
	}
}

func assertSourceClassification(t *testing.T, db *sql.DB, want string) {
	t.Helper()
	before := recoverySnapshot(t, db)
	// A mistaken repair/write in the classifier must fail even if later rolled back.
	if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	got, classifyErr := classifySchemaSource(context.Background(), tx)
	var alive int
	if err := tx.QueryRow("SELECT 1").Scan(&alive); err != nil {
		t.Fatalf("classifier took transaction ownership: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA query_only=OFF"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
		t.Fatal("classification mutated catalog, rows or sequences")
	}
	if want == "" {
		if classifyErr == nil || got != "" {
			t.Fatalf("unknown source accepted as %q: %v", got, classifyErr)
		}
	} else if classifyErr != nil || got != want {
		t.Fatalf("want %s, got %q: %v", want, got, classifyErr)
	}
}

func TestSchemaProvenancePrivate29(t *testing.T) {
	db, _ := completeMigrationFixture(t)
	if err := migrateTenantSchema(context.Background(), db, nil); err != nil {
		t.Fatal(err)
	}
	assertSourceClassification(t, db, "private29")
	// Pin the private fixture independently too. Only random internal FTS names
	// are replaced; SQL bodies, constraints, indexes and triggers stay exact.
	rows := compatibilityRows(t, db, "SELECT type||char(9)||name||char(9)||tbl_name||char(9)||coalesce(sql,'') FROM sqlite_schema ORDER BY type,name")
	var internalID string
	if err := db.QueryRow("SELECT internal_id FROM tenant_fts WHERE tenant_id='local'").Scan(&internalID); err != nil {
		t.Fatal(err)
	}
	// The same internal ID also occurs in trigger names and mapping literals.
	canonical := strings.ReplaceAll(strings.Join(rows, "\n"), internalID, "PINNED")
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(canonical))); got != "fb03efc669e87027ba2e3607d640ce3fe763d08336a7434d0da35acdaca8848e" {
		t.Fatalf("private29 catalog drift from 014d3d1d: %s", got)
	}
	for _, mutation := range []string{
		"CREATE TABLE bulk_jobs(tenant_id TEXT NOT NULL,id TEXT NOT NULL,PRIMARY KEY(tenant_id,id))",
		"CREATE TABLE entry_sync_devices(id TEXT PRIMARY KEY,data TEXT NOT NULL)",
		"CREATE INDEX alien ON notes(title)",
		"DROP TRIGGER p4_claim_no_delete",
	} {
		t.Run(mutation, func(t *testing.T) {
			before := recoverySnapshot(t, db)
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tx.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			if got, err := classifySchemaSource(context.Background(), tx); err == nil || got != "" {
				t.Errorf("private hybrid accepted: %q %v", got, err)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
				t.Fatal("private refusal/rollback changed source")
			}
		})
	}
}

func TestSchemaProvenanceRefusesUnknown(t *testing.T) {
	for name, mutation := range map[string]string{
		"missing-table":    "DROP TABLE entry_sync_operations",
		"unknown-table":    "CREATE TABLE alien(id INTEGER)",
		"unknown-view":     "CREATE VIEW alien AS SELECT path FROM notes",
		"unknown-index":    "CREATE INDEX alien ON notes(title)",
		"missing-index":    "DROP INDEX bulk_items_pending",
		"changed-index":    "DROP INDEX bulk_items_pending; CREATE INDEX bulk_items_pending ON bulk_job_items(job_id,tenant_id,state,sequence)",
		"missing-trigger":  "DROP TRIGGER entry_sync_delete",
		"changed-trigger":  "DROP TRIGGER entry_sync_delete; CREATE TRIGGER entry_sync_delete AFTER DELETE ON notes BEGIN SELECT 1; END",
		"extra-column":     "ALTER TABLE notes ADD COLUMN alien TEXT",
		"generated-column": "ALTER TABLE notes ADD COLUMN alien TEXT GENERATED ALWAYS AS (path) VIRTUAL",
		"private-hybrid":   "CREATE TABLE tenants(id TEXT PRIMARY KEY)",
		"numeric-relabel":  "DELETE FROM schema_version; INSERT INTO schema_version(version) VALUES(29)",
		"no-version":       "DELETE FROM schema_version",
		"negative-history": "INSERT INTO schema_version(version) VALUES(-1)",
		"future-history":   "INSERT INTO schema_version(version) VALUES(31)",
	} {
		t.Run(name, func(t *testing.T) {
			db := archivedSchemaFixture(t, provenanceSources[4].revision)
			if _, err := db.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			assertSourceClassification(t, db, "")
		})
	}
}

// Real historical upgrades, not a P4 InitSchema plus synthetic additive DDL.
// Seed receipt/history state BEFORE each subsequent archived startup. This also
// proves the final catalog is the same for fresh and sequentially upgraded main.
func TestSchemaProvenanceHistoricalUpgrades(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.db")
	for i, source := range provenanceSources {
		runArchivedSchema(t, source.revision, path)
		db := compatibilityDB(t, path)
		if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
			t.Fatal(err)
		}
		seed := ""
		switch i {
		case 0:
			seed = `INSERT INTO notes(id,path,short_id,title,body) VALUES(41,'projects/p/note/live.md','live0001','original title','body');
INSERT INTO entry_meta(path,project_id,access_count,last_verified) VALUES('brain:system/install_claimed','p',7,'2026-09-01');`
		case 1:
			seed = `INSERT INTO bulk_jobs VALUES('local','job','request','hash','{"label":"preserve"}','update','needs_attention','actor','created','updated');
INSERT INTO bulk_job_items VALUES('local','job',7,'projects/p/note/absent.md','absent00','old title','fingerprint','uncertain',3,'unknown outcome','destination');`
		case 2:
			seed = `INSERT INTO execution_budgets VALUES('local','p','budget','UTC','tasks',99,7);
INSERT INTO budget_reservations VALUES('local','p','budget','parent','','2026-09-10',2,'committed');
INSERT INTO budget_reservations VALUES('local','p','budget','child','parent','2026-09-11',3,'reserved');
INSERT INTO supervisor_checkpoints VALUES('local','p','checkpoint',2,'{"answer":"not verified","revision":2}');
INSERT INTO supervisor_checkpoint_versions VALUES('local','p','checkpoint',1,'{"question":"verify artifact","revision":1}');
INSERT INTO supervisor_checkpoint_versions VALUES('local','p','checkpoint',2,'{"answer":"not verified","revision":2}');
INSERT INTO supervisor_operations VALUES('local','actor','operation','prompt','digest','outcome_unknown','created','updated','do not replay');`
		case 3:
			seed = `UPDATE entry_sync_identity SET epoch='0123456789abcdef0123456789abcdef';
INSERT INTO notes(id,path,short_id,title) VALUES(77,'projects/p/note/deleted.md','deleted0','gone');
DELETE FROM notes WHERE id=77;
UPDATE notes SET title='updated title' WHERE id=41;
INSERT INTO entry_sync_operations VALUES('receipt','request-hash',409,'{"error":"revision conflict"}');
UPDATE sqlite_sequence SET seq=900 WHERE name='entry_sync_changes';`
		case 4:
			seed = `INSERT INTO entry_sync_devices VALUES('device','{"pending":[{"id":"draft","state":"uncertain"}]}');`
		}
		if seed != "" {
			if _, err := db.Exec(seed); err != nil {
				t.Fatalf("seed %s: %v", source.profile, err)
			}
		}
		assertSourceClassification(t, db, source.profile)
		recoveryIntegrity(t, db)
		if i >= 3 {
			for query, want := range map[string]string{
				"SELECT epoch FROM entry_sync_identity": "0123456789abcdef0123456789abcdef",
				"SELECT count(*) FROM entry_sync_changes WHERE path='projects/p/note/deleted.md' AND NOT EXISTS(SELECT 1 FROM notes WHERE path=entry_sync_changes.path)": "1",
				// Archived startup consumes one sequence per live note even on
				// INSERT OR IGNORE conflict. Classification itself consumes none.
				"SELECT seq FROM sqlite_sequence WHERE name='entry_sync_changes'":        fmt.Sprint(900 + i - 3),
				"SELECT status||':'||body FROM entry_sync_operations WHERE id='receipt'": `409:{"error":"revision conflict"}`,
				"SELECT state||':'||attempts||':'||fingerprint FROM bulk_job_items":      "uncertain:3:fingerprint",
				"SELECT state||':'||detail FROM supervisor_operations":                   "outcome_unknown:do not replay",
				"SELECT count(*) FROM supervisor_checkpoint_versions":                    "2",
			} {
				var got string
				if err := db.QueryRow(query).Scan(&got); err != nil {
					t.Fatal(err)
				}
				if got != want {
					t.Fatalf("%s: got %q want %q", query, got, want)
				}
			}
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSchemaProvenanceDoesNotAdmitOldPrivateMigration(t *testing.T) {
	if CurrentSchemaVersion != 30 || pendingTenantSchemaVersion != 29 {
		t.Fatal("runtime/private schema contract changed")
	}
	for _, source := range provenanceSources[1:] {
		t.Run(source.revision[:8], func(t *testing.T) {
			db := archivedSchemaFixture(t, source.revision)
			before := recoverySnapshot(t, db)
			if err := migrateTenantSchema(context.Background(), db, nil); err == nil {
				t.Fatal("old private migrator admitted main source")
			}
			if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
				t.Fatal("refusal mutated source")
			}
		})
	}
}

func TestSchemaProvenanceInvalidContext(t *testing.T) {
	if _, err := classifySchemaSource(context.Background(), nil); err == nil {
		t.Fatal("nil transaction accepted")
	}
	db := archivedSchemaFixture(t, provenanceSources[0].revision)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := classifySchemaSource(nil, tx); err == nil { //nolint:staticcheck // Deliberately exercise fail-closed nil-context validation.
		t.Fatal("nil context accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := classifySchemaSource(ctx, tx); err == nil {
		t.Fatal("cancelled context accepted")
	}
	if _, err := tx.Exec("CREATE TEMP TABLE notes(id)"); err != nil {
		t.Fatal(err)
	}
	if got, err := classifySchemaSource(context.Background(), tx); err == nil || got != "" {
		t.Fatal("temporary shadow accepted")
	}
}

func TestSchemaProvenanceVersionCollision(t *testing.T) {
	for _, source := range provenanceSources[:5] {
		t.Run(source.profile, func(t *testing.T) {
			db := archivedSchemaFixture(t, source.revision)
			var actual int
			if err := db.QueryRow("SELECT max(version) FROM schema_version").Scan(&actual); err != nil {
				t.Fatal(err)
			}
			for _, version := range []int{28, 29, 30} {
				if version == actual {
					continue
				}
				t.Run(fmt.Sprint(version), func(t *testing.T) {
					tx, err := db.Begin()
					if err != nil {
						t.Fatal(err)
					}
					if _, err := tx.Exec("DELETE FROM schema_version; INSERT INTO schema_version(version) VALUES(?)", version); err != nil {
						t.Fatal(err)
					}
					if got, err := classifySchemaSource(context.Background(), tx); err == nil || got != "" {
						t.Errorf("relabel accepted as %q: %v", got, err)
					}
					if err := tx.Rollback(); err != nil {
						t.Fatal(err)
					}
				})
			}
		})
	}
}

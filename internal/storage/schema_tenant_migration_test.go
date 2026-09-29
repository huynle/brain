package storage

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
)

func completeMigrationFixture(t *testing.T) (*sql.DB, string) {
	t.Helper()
	db := relationalFixture(t)
	relationalExec(t, db, "DELETE FROM tenant_roots")
	brain, blob := t.TempDir(), t.TempDir()
	resolver, err := tenantfs.New(registryHandle(t, &StorageLayer{db: db}), brain)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.ProvisionLocal(context.Background(), brain, blob); err != nil {
		t.Fatal(err)
	}
	bytes := []byte("real local CAS bytes")
	digest := fmt.Sprintf("%x", sha256.Sum256(bytes))
	path := filepath.Join(blob, digest[:2], digest[2:4], digest)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE attachments SET digest=?,size=?", digest, len(bytes)); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(brain, "untouched.md"), []byte("local markdown"), 0600); err != nil {
		t.Fatal(err)
	}
	return db, path
}

func TestTenantMigrationComposition(t *testing.T) {
	db, blob := completeMigrationFixture(t)
	before := map[string][]string{}
	for _, table := range append([]string{"sqlite_sequence"}, relationalControlTables...) {
		before[table] = relationalSnapshot(t, db, table)
	}
	claim := relationalSnapshot(t, db, "(SELECT * FROM entry_meta WHERE path='brain:system/install_claimed')")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := migrateTenantSchema(ctx, db, nil); err != nil {
		t.Fatalf("complete migration: %v", err)
	}
	if v, err := GetSchemaVersion(db); err != nil || v != 29 {
		t.Fatalf("future version = %d: %v", v, err)
	}
	if CurrentSchemaVersion != 30 {
		t.Fatal("runtime activated")
	}
	for table, want := range before {
		if table == "schema_version" {
			continue
		}
		if got := relationalSnapshot(t, db, table); !reflect.DeepEqual(got, want) {
			t.Fatalf("changed %s", table)
		}
	}
	if got := relationalSnapshot(t, db, "operator_install_claim"); !reflect.DeepEqual(got, claim) {
		t.Fatal("claim changed")
	}
	contents, err := os.ReadFile(blob)
	if err != nil || string(contents) != "real local CAS bytes" {
		t.Fatal("CAS changed", err)
	}
	migrationFK(t, db)
	snapshot := relationalSnapshot(t, db, "sqlite_schema")
	if err := migrateTenantSchema(ctx, db, nil); err != nil {
		t.Fatalf("repeat validation: %v", err)
	}
	if !reflect.DeepEqual(snapshot, relationalSnapshot(t, db, "sqlite_schema")) {
		t.Fatal("repeat changed catalog")
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := checkFinalTenantSearchSchema(tx); err != nil {
		t.Fatal(err)
	}
	relationalExec(t, tx, "UPDATE notes SET title='fresh title' WHERE id=10")
	hits, count, err := queryTenantFTS(ctx, tx, tenant.Local, "fresh", 10)
	if err != nil || count != 1 || len(hits) != 1 || hits[0].ID != 10 || hits[0].Title != "fresh title" {
		t.Fatalf("stale search after composed migration: %v %d %v", hits, count, err)
	}
	if err := checkFinalTenantSearchSchema(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec("DELETE FROM operator_install_claim"); err == nil {
		t.Fatal("claim not permanent")
	}
}

func migrationFK(t *testing.T, db *sql.DB) {
	t.Helper()
	var fk int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("FK=%d: %v", fk, err)
	}
}

func migrationBaseline(t *testing.T, db *sql.DB) map[string][]string {
	t.Helper()
	tables := append(append([]string{"sqlite_schema", "sqlite_sequence", "notes_fts_data", "notes_fts_idx", "notes_fts_docsize", "notes_fts_config"}, relationalTenantTables...), relationalControlTables...)
	result := map[string][]string{}
	for _, table := range tables {
		result[table] = relationalSnapshot(t, db, table)
	}
	return result
}

func assertMigrationBaseline(t *testing.T, db *sql.DB, before map[string][]string) {
	t.Helper()
	for table, want := range before {
		if !reflect.DeepEqual(want, relationalSnapshot(t, db, table)) {
			t.Fatalf("changed baseline %s", table)
		}
	}
	migrationFK(t, db)
}

func TestTenantMigrationRollbackStages(t *testing.T) {
	for _, stage := range []string{"reserved", "snapshot", "relational", "fts", "files", "validated", "published"} {
		t.Run(stage, func(t *testing.T) {
			db, _ := completeMigrationFixture(t)
			before := migrationBaseline(t, db)
			reached := false
			err := migrateTenantSchema(context.Background(), db, func(s string) error {
				if s == stage {
					reached = true
					return fmt.Errorf("injected %s", s)
				}
				return nil
			})
			if err == nil || !reached {
				t.Fatalf("missing failure at %s: %v", stage, err)
			}
			assertMigrationBaseline(t, db, before)
			// Failure cleanup also drops temporary snapshots on this reused connection.
			var count int
			if err := db.QueryRow("SELECT count(*) FROM sqlite_temp_schema").Scan(&count); err != nil || count != 0 {
				t.Fatalf("temporary artifacts: %d %v", count, err)
			}
			if err := migrateTenantSchema(context.Background(), db, nil); err != nil {
				t.Fatal("retry", err)
			}
		})
	}
}

func TestTenantMigrationRejectsInvalidState(t *testing.T) {
	for _, damage := range []string{"control-ddl", "control-index", "missing-roots", "root-drift", "digest", "missing-blob", "corrupt-blob", "unknown-table", "partial-v28", "future-version"} {
		t.Run(damage, func(t *testing.T) {
			db, blob := completeMigrationFixture(t)
			switch damage {
			case "control-ddl":
				relationalExec(t, db, "ALTER TABLE api_tokens ADD COLUMN unreviewed TEXT")
			case "control-index":
				relationalExec(t, db, "CREATE INDEX unreviewed_control ON api_tokens(token)")
			case "missing-roots":
				relationalExec(t, db, "DELETE FROM tenant_roots")
			case "root-drift":
				relationalExec(t, db, "UPDATE tenant_roots SET blob_canonical='/wrong'")
			case "digest":
				relationalExec(t, db, "UPDATE attachments SET digest='fake'")
			case "missing-blob":
				if err := os.Remove(blob); err != nil {
					t.Fatal(err)
				}
			case "corrupt-blob":
				if err := os.WriteFile(blob, []byte("wrong"), 0600); err != nil {
					t.Fatal(err)
				}
			case "unknown-table":
				relationalExec(t, db, "CREATE TABLE unreviewed(x)")
			case "partial-v28":
				tx := relationalBegin(t, db)
				if err := stageTenantRelationalSchema(tx); err != nil {
					t.Fatal(err)
				}
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
			case "future-version":
				relationalExec(t, db, "INSERT INTO schema_version(version) VALUES(30)")
			}
			before := relationalSnapshot(t, db, "sqlite_schema")
			if err := migrateTenantSchema(context.Background(), db, nil); err == nil {
				t.Fatal("accepted invalid migration source")
			}
			if !reflect.DeepEqual(before, relationalSnapshot(t, db, "sqlite_schema")) {
				t.Fatal("mutated invalid source")
			}
			migrationFK(t, db)
		})
	}
}

func TestTenantMigrationExactTransition(t *testing.T) {
	for _, damage := range []string{"UPDATE api_tokens SET token='changed'", "UPDATE attachments SET media_type='changed'", "UPDATE notes SET body='changed'", "UPDATE sqlite_sequence SET seq=901 WHERE name='notes'", "UPDATE tenant_roots SET brain_root='changed'"} {
		t.Run(damage, func(t *testing.T) {
			db, _ := completeMigrationFixture(t)
			before := migrationBaseline(t, db)
			tx := relationalBegin(t, db)
			snapshots, err := captureTenantMigration(tx)
			if err != nil {
				t.Fatal(err)
			}
			if err := stageTenantRelationalSchema(tx); err != nil {
				t.Fatal(err)
			}
			if err := stageTenantFTS(tx); err != nil {
				t.Fatal(err)
			}
			relationalExec(t, tx, damage)
			rejected := false
			for _, s := range snapshots {
				if s.check(tx) != nil {
					rejected = true
					break
				}
			}
			if !rejected {
				t.Fatal("accepted changed transition")
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			relationalExec(t, db, "PRAGMA foreign_keys=ON")
			assertMigrationBaseline(t, db, before)
		})
	}
}

func TestTenantMigrationRepeatRejectsDamage(t *testing.T) {
	for _, damage := range []string{"DROP INDEX p4_owner_notes", "DROP TRIGGER p4_claim_no_delete", "DELETE FROM tenant_roots", "ALTER TABLE oauth_clients ADD COLUMN unreviewed TEXT", "DELETE FROM schema_version WHERE version=29"} {
		t.Run(damage, func(t *testing.T) {
			db, _ := completeMigrationFixture(t)
			if err := migrateTenantSchema(context.Background(), db, nil); err != nil {
				t.Fatal(err)
			}
			relationalExec(t, db, damage)
			if err := migrateTenantSchema(context.Background(), db, nil); err == nil {
				t.Fatal("accepted damaged published schema")
			}
			migrationFK(t, db)
		})
	}
}

func TestTenantMigrationCrashChild(t *testing.T) {
	path := os.Getenv("BRAIN_P4_COMPLETE_CHILD")
	if path == "" {
		t.Skip("subprocess only")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	relationalExec(t, db, "PRAGMA cache_size=8; PRAGMA wal_autocheckpoint=0")
	err = migrateTenantSchema(context.Background(), db, func(stage string) error {
		if stage == os.Getenv("BRAIN_P4_COMPLETE_STAGE") {
			fmt.Println("P4_COMPLETE_UNCOMMITTED")
			time.Sleep(25 * time.Second)
			return fmt.Errorf("parent did not interrupt child")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Fatal("child reached commit")
}

func TestTenantMigrationInterruptedRestart(t *testing.T) {
	for _, stage := range []string{"relational", "fts", "files", "published"} {
		t.Run(stage, func(t *testing.T) {
			db, _ := completeMigrationFixture(t)
			var seq int
			var name, path, mode string
			if err := db.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
				t.Fatal(err)
			}
			before := migrationBaseline(t, db)
			if err := db.Close(); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestTenantMigrationCrashChild$")
			cmd.Env = append(os.Environ(), "BRAIN_P4_COMPLETE_CHILD="+path, "BRAIN_P4_COMPLETE_STAGE="+stage)
			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() { _ = cmd.Process.Kill() }()
			scanner := bufio.NewScanner(out)
			if !scanner.Scan() || scanner.Text() != "P4_COMPLETE_UNCOMMITTED" {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				t.Fatal("child did not reach checkpoint")
			}
			t.Log(rehearsalSizes(t, path))
			if info, err := os.Stat(path + "-wal"); err != nil || info.Size() == 0 {
				t.Fatal("missing spilled WAL", err)
			}
			if err := cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err := cmd.Wait(); err == nil {
				t.Fatal("unexpected clean exit")
			}
			db = compatibilityDB(t, path)
			db.SetMaxOpenConns(1)
			relationalExec(t, db, "PRAGMA foreign_keys=ON")
			assertMigrationBaseline(t, db, before)
			if err := migrateTenantSchema(context.Background(), db, nil); err != nil {
				t.Fatal("recovery retry", err)
			}
			migrationFK(t, db)
		})
	}
}

// Synthetic only: includes exact-row snapshots, relational/FTS rebuild, CAS
// hashing, root checks, commit and FK restoration; excludes fixture creation.
func BenchmarkTenantMigrationComplete78952(b *testing.B) {
	if os.Getenv("BRAIN_P4_SYNTHETIC_SCALE") != "1" {
		b.Skip("opt-in synthetic complete migration")
	}
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		base := b.TempDir()
		db, err := sql.Open("sqlite", filepath.Join(base, "complete.db"))
		if err != nil {
			b.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		b.Cleanup(func() { _ = db.Close() })
		if err := InitSchema(db); err != nil {
			b.Fatal(err)
		}
		resolver, err := tenantfs.New(registryHandle(b, &StorageLayer{db: db}), base)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := resolver.ProvisionLocal(context.Background(), base, base); err != nil {
			b.Fatal(err)
		}
		bytes := []byte("synthetic CAS bytes")
		digest := fmt.Sprintf("%x", sha256.Sum256(bytes))
		path := filepath.Join(base, digest[:2], digest[2:4], digest)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			b.Fatal(err)
		}
		if err := os.WriteFile(path, bytes, 0600); err != nil {
			b.Fatal(err)
		}
		if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0;
WITH RECURSIVE n(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM n WHERE x<78952)
INSERT INTO notes(id,path,short_id,title,body,metadata,project_id,type)
SELECT x,printf('projects/p%d/note/%08d.md',x%100,x),printf('%08d',x),printf('Synthetic note %d',x),
printf('Migration recovery %d %s',x,replace(hex(zeroblob(256)),'00','text ')), '{"status":"active"}',printf('p%d',x%100),'note' FROM n;
INSERT INTO tags(note_id,tag) SELECT id,'synthetic' FROM notes;
INSERT INTO links(source_id,target_path,target_id,href) SELECT id,path,id,path FROM notes WHERE id%10=0;
INSERT INTO note_embeddings(note_id,chunk_index,embedding) SELECT id,0,zeroblob(1536) FROM notes WHERE id%10=0;
INSERT INTO note_embeddings_meta(note_id,chunk_index) SELECT id,0 FROM notes WHERE id%10=0;
INSERT INTO entry_meta(path) VALUES('brain:system/install_claimed');`); err != nil {
			b.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO attachments(id,digest,size) VALUES(1,?,?)", digest, len(bytes)); err != nil {
			b.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO entry_attachments(note_id,attachment_id) VALUES(1,1); INSERT INTO attachment_derived(attachment_id,text) VALUES(1,'synthetic extracted text')"); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
		start := time.Now()
		if err := migrateTenantSchema(context.Background(), db, nil); err != nil {
			b.Fatal(err)
		}
		elapsed := time.Since(start)
		b.StopTimer()
		b.Logf("SYNTHETIC complete commit + validation + FK restoration: %s; %s", elapsed, rehearsalSizes(b, filepath.Join(base, "complete.db")))
		if err := db.Close(); err != nil {
			b.Fatal(err)
		}
	}
}

func TestTenantMigrationCancellationRestoresFK(t *testing.T) {
	db, _ := completeMigrationFixture(t)
	before := migrationBaseline(t, db)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	err := migrateTenantSchema(ctx, db, func(stage string) error {
		if stage == "fts" {
			cancel()
			return ctx.Err()
		}
		return nil
	})
	if err == nil {
		t.Fatal("cancelled migration succeeded")
	}
	assertMigrationBaseline(t, db, before)
}

func TestTenantMigrationExactOwner(t *testing.T) {
	db, _ := completeMigrationFixture(t)
	tx := relationalBegin(t, db)
	snapshots, err := captureTenantMigration(tx)
	if err != nil {
		t.Fatal(err)
	}
	if err := stageTenantRelationalSchema(tx); err != nil {
		t.Fatal(err)
	}
	// Same legacy values, different owner: value-only comparison is insufficient.
	relationalExec(t, tx, "INSERT INTO tenants(id,name,status,created_at) VALUES('B','B','active','now'); UPDATE entry_meta SET tenant_id='B'")
	rejected := false
	for _, s := range snapshots {
		if s.check(tx) != nil {
			rejected = true
			break
		}
	}
	if !rejected {
		t.Fatal("accepted legacy values reassigned to foreign owner")
	}
}

func TestTenantMigrationPreservesAbsentSequence(t *testing.T) {
	db, _ := completeMigrationFixture(t)
	relationalExec(t, db, "DELETE FROM event_log; DELETE FROM sqlite_sequence WHERE name='event_log'")
	before := relationalSnapshot(t, db, "sqlite_sequence")
	if err := migrateTenantSchema(context.Background(), db, nil); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, relationalSnapshot(t, db, "sqlite_sequence")) {
		t.Fatal("created absent sequence")
	}
}

func TestTenantMigrationWriterReservationAndVisibility(t *testing.T) {
	db, _ := completeMigrationFixture(t)
	var seq int
	var name, path, mode string
	if err := db.QueryRow("PRAGMA database_list").Scan(&seq, &name, &path); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("PRAGMA journal_mode=WAL").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	observer, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer observer.Close()
	observer.SetMaxOpenConns(1)
	// This test asserts lock ownership, not the configured busy-wait duration.
	relationalExec(t, observer, "PRAGMA busy_timeout=0")
	before := migrationBaseline(t, db)
	if err := migrateTenantSchema(context.Background(), db, func(stage string) error {
		// Reservation is already held at the first checkpoint, before source reads.
		if _, err := observer.Exec("UPDATE schema_version SET version=version WHERE 0"); err == nil {
			return fmt.Errorf("writer reservation missing at %s", stage)
		}
		for table, want := range before {
			if !reflect.DeepEqual(want, relationalSnapshot(t, observer, table)) {
				return fmt.Errorf("partial publication of %s at %s", table, stage)
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if v, err := GetSchemaVersion(observer); err != nil || v != 29 {
		t.Fatalf("commit invisible: %d %v", v, err)
	}
	migrationFK(t, db)
}

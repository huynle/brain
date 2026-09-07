package storage

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func compatibilityDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	u := url.URL{Scheme: "file", Path: path}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func compatibilityRows(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			t.Fatal(err)
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return result
}

func TestSchemaCompatibilityRefusesBeforeMutation(t *testing.T) {
	for _, entry := range []string{"init", "migration", "with-db", "new", "new-special-path"} {
		for _, fixture := range []struct{ name, ddl, read, want string }{
			{"future", "CREATE TABLE schema_version(version INTEGER PRIMARY KEY, applied_at TEXT); INSERT INTO schema_version VALUES (28,'old'),(29,'future');", "SELECT version || ':' || applied_at FROM schema_version ORDER BY version", "newer"},
			{"missing-column", "CREATE TABLE schema_version(broken TEXT); INSERT INTO schema_version VALUES ('sentinel');", "SELECT broken FROM schema_version", "schema version"},
			{"noninteger", "CREATE TABLE schema_version(version TEXT); INSERT INTO schema_version VALUES ('broken');", "SELECT version FROM schema_version", "schema version"},
		} {
			t.Run(entry+"/"+fixture.name, func(t *testing.T) {
				name := "brain.db"
				if entry == "new-special-path" {
					name = "brain ?mode=memory&x=1#%.db"
				}
				path := filepath.Join(t.TempDir(), name)
				db := compatibilityDB(t, path)
				_, err := db.Exec(fixture.ddl + `
CREATE TABLE note_embeddings(path TEXT, payload TEXT);
INSERT INTO note_embeddings VALUES ('legacy', 'keep');
CREATE TABLE opencode_instances(instance_id TEXT);
INSERT INTO opencode_instances VALUES ('keep');
CREATE TABLE entry_meta(path TEXT PRIMARY KEY, access_count INTEGER);
INSERT INTO entry_meta VALUES ('brain:system/install_claimed', 1);
CREATE TABLE tenant_roots(tenant_id TEXT, anchor TEXT);
INSERT INTO tenant_roots VALUES ('local', '/keep');`)
				if err != nil {
					t.Fatal(err)
				}
				queries := []string{
					"SELECT type || ':' || name || ':' || COALESCE(sql,'') FROM sqlite_master ORDER BY type,name",
					fixture.read, "SELECT path || payload FROM note_embeddings",
					"SELECT instance_id FROM opencode_instances", "SELECT path || access_count FROM entry_meta",
					"SELECT tenant_id || anchor FROM tenant_roots", "PRAGMA journal_mode",
				}
				before := make([][]string, len(queries))
				for i, q := range queries {
					before[i] = compatibilityRows(t, db, q)
				}
				switch entry {
				case "init":
					err = InitSchema(db)
				case "migration":
					err = migrateSchema(db)
				case "with-db":
					var s *StorageLayer
					s, err = NewWithDB(db)
					if s != nil {
						t.Error("returned storage on incompatible schema")
					}
				default:
					if err := db.Close(); err != nil {
						t.Fatal(err)
					}
					var s *StorageLayer
					s, err = New(path)
					if s != nil {
						s.Close()
						t.Error("returned storage on incompatible schema")
					}
					db = compatibilityDB(t, path)
				}
				if err == nil || !strings.Contains(err.Error(), fixture.want) {
					t.Errorf("expected %q refusal, got %v", fixture.want, err)
				}
				for i, q := range queries {
					if after := compatibilityRows(t, db, q); !reflect.DeepEqual(before[i], after) {
						t.Errorf("mutated %s: before=%v after=%v", q, before[i], after)
					}
				}
			})
		}
	}
}

func TestSchemaCompatibilitySupportedAndFresh(t *testing.T) {
	for _, version := range []int{-1, 0, 27, 28} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "brain.db")
			if version >= 0 {
				db := compatibilityDB(t, path)
				if version > 0 {
					if err := InitSchema(db); err != nil {
						t.Fatal(err)
					}
					if _, err := db.Exec("DELETE FROM schema_version; INSERT INTO schema_version(version) VALUES (?)", version); err != nil {
						t.Fatal(err)
					}
				} else {
					if _, err := db.Exec(createSchemaVersionTable); err != nil {
						t.Fatal(err)
					}
				}
				db.Close()
			}
			for i := 0; i < 2; i++ {
				s, err := New(path)
				if err != nil {
					t.Fatal(err)
				}
				got, err := GetSchemaVersion(s.db)
				if err != nil || got != 28 {
					t.Fatalf("version=%d err=%v; must remain v28", got, err)
				}
				s.Close()
			}
		})
	}
	// Direct migration must still permit an absent version table without DDL.
	db := compatibilityDB(t, filepath.Join(t.TempDir(), "fresh.db"))
	if err := migrateSchema(db); err != nil {
		t.Fatal(err)
	}
	if rows := compatibilityRows(t, db, "SELECT name FROM sqlite_master"); len(rows) != 0 {
		t.Fatal(rows)
	}
}

func TestSchemaCompatibilityCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.db")
	before := []byte("not a sqlite database")
	if err := os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	if s, err := New(path); err == nil {
		s.Close()
		t.Fatal("accepted corrupt database")
	}
	after, err := os.ReadFile(path)
	if err != nil || string(before) != string(after) {
		t.Fatalf("corrupt file changed: %q %v", after, err)
	}
}

func TestSchemaCompatibilityFutureInWAL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "brain.db")
	db := compatibilityDB(t, path)
	if err := InitSchema(db); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; INSERT INTO schema_version(version, applied_at) VALUES (29, 'future')"); err != nil {
		t.Fatal(err)
	}
	// Keep the writer open so the future stamp remains in the WAL. An
	// immutable preflight would incorrectly inspect only the v28 main file.
	if info, err := os.Stat(path + "-wal"); err != nil || info.Size() == 0 {
		t.Fatalf("expected populated WAL: %v", err)
	}
	queries := []string{
		"SELECT type || ':' || name || ':' || COALESCE(sql,'') FROM sqlite_master ORDER BY type,name",
		"SELECT version || ':' || applied_at FROM schema_version ORDER BY version",
		"PRAGMA journal_mode",
	}
	for _, open := range []func() error{
		func() error { return InitSchema(db) },
		func() error { return migrateSchema(db) },
		func() error { _, err := NewWithDB(db); return err },
		func() error {
			s, err := New(path)
			if s != nil {
				s.Close()
			}
			return err
		},
	} {
		before := make([][]string, len(queries))
		for i, q := range queries {
			before[i] = compatibilityRows(t, db, q)
		}
		if err := open(); err == nil || !strings.Contains(err.Error(), "29 is newer than supported version 28") {
			t.Fatalf("expected future refusal, got %v", err)
		}
		for i, q := range queries {
			if after := compatibilityRows(t, db, q); !reflect.DeepEqual(before[i], after) {
				t.Fatalf("mutated %s", q)
			}
		}
	}
}

func TestSchemaCompatibilityConstructorRegressions(t *testing.T) {
	for _, path := range []string{":memory:", filepath.Join(t.TempDir(), "brain ?mode=memory&x=1#%.db")} {
		s, err := New(path)
		if err != nil {
			t.Fatal(err)
		}
		if version, err := GetSchemaVersion(s.db); err != nil || version != 28 {
			t.Fatalf("version=%d err=%v", version, err)
		}
		s.Close()
		if path != ":memory:" {
			db := compatibilityDB(t, path)
			if version, err := GetSchemaVersion(db); err != nil || version != 28 {
				t.Fatalf("literal path version=%d err=%v", version, err)
			}
		}
	}
	// Catalog read failures must be returned, including on direct migration.
	db := compatibilityDB(t, filepath.Join(t.TempDir(), "closed.db"))
	db.Close()
	for _, run := range []func(*sql.DB) error{InitSchema, migrateSchema} {
		if err := run(db); err == nil || !strings.Contains(err.Error(), "database is closed") {
			t.Fatalf("closed handle accepted: %v", err)
		}
	}
}

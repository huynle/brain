package storage

import (
	"reflect"
	"strings"
	"testing"
)

func TestTenantRelationalRejectsNullMetadataKey(t *testing.T) {
	db := relationalFixture(t)
	relationalExec(t, db, "INSERT INTO entry_meta(path,access_count) VALUES(NULL,123)")
	before := relationalSnapshot(t, db, "entry_meta")
	schema := relationalSnapshot(t, db, "sqlite_schema")
	tx := relationalBegin(t, db)
	err := stageTenantRelationalSchema(tx)
	if err == nil {
		t.Fatal("accepted NULL metadata key: migration silently loses the source row")
	}
	if !strings.Contains(err.Error(), "entry_meta") {
		t.Fatalf("expected metadata failure, got %v", err)
	}
	if got := relationalSnapshot(t, tx, "entry_meta"); !reflect.DeepEqual(got, before) {
		t.Fatalf("failure changed metadata: %v -> %v", before, got)
	}
	if got := relationalSnapshot(t, tx, "sqlite_schema"); !reflect.DeepEqual(got, schema) {
		t.Fatal("failure left partial schema")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := relationalSnapshot(t, db, "entry_meta"); !reflect.DeepEqual(got, before) {
		t.Fatal("outer rollback lost original metadata")
	}
}

func TestTenantRelationalMetadataSplitTotal(t *testing.T) {
	for _, claimed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unclaimed", true: "claimed"}[claimed], func(t *testing.T) {
			db := relationalFixture(t)
			if !claimed {
				relationalExec(t, db, "DELETE FROM entry_meta WHERE path='brain:system/install_claimed'")
			}
			var source int
			if err := db.QueryRow("SELECT count(*) FROM entry_meta").Scan(&source); err != nil {
				t.Fatal(err)
			}
			tx := relationalBegin(t, db)
			if err := stageTenantRelationalSchema(tx); err != nil {
				t.Fatal(err)
			}
			var copied int
			if err := tx.QueryRow("SELECT (SELECT count(*) FROM entry_meta)+(SELECT count(*) FROM operator_install_claim)").Scan(&copied); err != nil {
				t.Fatal(err)
			}
			if copied != source {
				t.Fatalf("split lost metadata: source=%d destinations=%d", source, copied)
			}
		})
	}
}

func TestTenantRelationalRejectsUnreviewedSourceSchema(t *testing.T) {
	for _, tc := range []struct{ name, ddl string }{
		{"generated", "ALTER TABLE notes ADD COLUMN audit_value TEXT GENERATED ALWAYS AS (path || ':audit') VIRTUAL"},
		{"ordinary", "ALTER TABLE notes ADD COLUMN audit_value TEXT DEFAULT 'audit'"},
		// Default changes have the same column names and are silently replaced by
		// the compile-time DDL unless the source definition itself is validated.
		{"default", strings.Replace(createNotesTable, "title TEXT NOT NULL DEFAULT ''", "title TEXT NOT NULL DEFAULT 'audit'", 1)},
		{"quoted-default", strings.Replace(createNotesTable, "title TEXT NOT NULL DEFAULT ''", `title TEXT NOT NULL DEFAULT '""'`, 1)},
		{"check", strings.Replace(createNotesTable, "title TEXT NOT NULL DEFAULT ''", "title TEXT NOT NULL DEFAULT '' CHECK(title!='forbidden')", 1)},
		{"unique", strings.Replace(createNotesTable, "short_id TEXT NOT NULL", "short_id TEXT NOT NULL UNIQUE", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := relationalFixture(t)
			if tc.name == "generated" || tc.name == "ordinary" {
				relationalExec(t, db, tc.ddl)
			} else {
				// Rebuild this test's source table with an unreviewed constraint or
				// default; preserve rows, named indexes and legacy FTS triggers.
				relationalExec(t, db, "PRAGMA foreign_keys=OFF")
				relationalExec(t, db, "CREATE TEMP TABLE saved_notes AS SELECT * FROM notes; DROP TABLE notes")
				relationalExec(t, db, tc.ddl)
				relationalExec(t, db, "INSERT INTO notes SELECT * FROM saved_notes; DROP TABLE saved_notes")
				for _, ddl := range createIndexes {
					relationalExec(t, db, ddl)
				}
				for _, ddl := range []string{createTriggerAfterInsert, createTriggerAfterDelete, createTriggerAfterUpdate} {
					relationalExec(t, db, ddl)
				}
			}
			before := relationalSnapshot(t, db, "notes")
			schema := relationalSnapshot(t, db, "sqlite_schema")
			tx := relationalBegin(t, db)
			err := stageTenantRelationalSchema(tx)
			if err == nil {
				t.Fatal("accepted unreviewed source schema: rebuild silently removes its definition")
			}
			if !strings.Contains(err.Error(), "notes") {
				t.Fatalf("expected notes schema failure, got %v", err)
			}
			if got := relationalSnapshot(t, tx, "notes"); !reflect.DeepEqual(got, before) {
				t.Fatalf("source rows changed: %v", got)
			}
			if got := relationalSnapshot(t, tx, "sqlite_schema"); !reflect.DeepEqual(got, schema) {
				t.Fatal("failure left partial schema")
			}
		})
	}
}

func TestNormalizeRelationalDDLPreservesLiterals(t *testing.T) {
	for _, literal := range []string{
		`'""'`, `'a  b'`, "'a\tb'", "'a\nb'", `' IF NOT EXISTS '`, `'it''s  "quoted"'`,
	} {
		t.Run(literal, func(t *testing.T) {
			ddl := "CREATE TABLE notes (title TEXT DEFAULT " + literal + ")"
			if got := normalizeRelationalDDL(ddl); got != ddl {
				t.Fatalf("rewrote literal content: %q -> %q", ddl, got)
			}
		})
	}
	// Only the catalog's syntactic header/terminator differences are equivalent.
	want := "CREATE TABLE notes (title TEXT DEFAULT 'a  b')"
	for _, ddl := range []string{
		"\nCREATE TABLE IF NOT EXISTS notes (title TEXT DEFAULT 'a  b');\n",
		`CREATE TABLE "notes" (title TEXT DEFAULT 'a  b')`,
	} {
		if got := normalizeRelationalDDL(ddl); got != want {
			t.Fatalf("catalog syntax mismatch: %q != %q", got, want)
		}
	}
}

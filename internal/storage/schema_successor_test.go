package storage

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"
)

// Independent literal contract. These fixtures contain ONLY control metadata,
// never a complete successor database: no successful successor admission is tested.
var successorControlContract = map[string]string{
	"schema_version": `CREATE TABLE schema_version (
  version INTEGER PRIMARY KEY,
  applied_at TEXT DEFAULT (datetime('now'))
)`,
	"schema_provenance": `CREATE TABLE schema_provenance (
  singleton INTEGER NOT NULL PRIMARY KEY CHECK (singleton = 1),
  source_profile TEXT NOT NULL,
  source_family TEXT NOT NULL,
  source_version INTEGER NOT NULL,
  target_family TEXT NOT NULL CHECK (target_family = 'tenant'),
  target_version INTEGER NOT NULL CHECK (target_version = 31),
  CHECK (
    (source_profile = 'main28' AND source_family = 'main' AND source_version = 28) OR
    (source_profile = 'main29' AND source_family = 'main' AND source_version = 29) OR
    (source_profile IN ('main30-pre-sync','main30-initial-sync','main30-devices') AND source_family = 'main' AND source_version = 30) OR
    (source_profile = 'private29' AND source_family = 'private-tenant' AND source_version = 29)
  )
) STRICT`,
	"schema_provenance_no_update":  `CREATE TRIGGER schema_provenance_no_update BEFORE UPDATE ON schema_provenance BEGIN SELECT RAISE(ABORT, 'schema provenance is immutable'); END`,
	"schema_provenance_no_delete":  `CREATE TRIGGER schema_provenance_no_delete BEFORE DELETE ON schema_provenance BEGIN SELECT RAISE(ABORT, 'schema provenance is immutable'); END`,
	"schema_provenance_no_replace": `CREATE TRIGGER schema_provenance_no_replace BEFORE INSERT ON schema_provenance WHEN EXISTS (SELECT 1 FROM schema_provenance) BEGIN SELECT RAISE(ABORT, 'schema provenance already exists'); END`,
}

func successorRecordFixture(t *testing.T, profile, family string, version int) *sql.DB {
	t.Helper()
	db := compatibilityDB(t, filepath.Join(t.TempDir(), "controls-only.db"))
	db.SetMaxOpenConns(1)
	for _, name := range []string{"schema_version", "schema_provenance", "schema_provenance_no_update", "schema_provenance_no_delete", "schema_provenance_no_replace"} {
		if _, err := db.Exec(successorControlContract[name]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec("INSERT INTO schema_version(version) VALUES (?),(31)", version); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO schema_provenance VALUES (1,?,?,?,'tenant',31)", profile, family, version); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestSuccessorControlDefinitions(t *testing.T) {
	if !reflect.DeepEqual(successorControlDefinitions(), successorControlContract) {
		t.Fatal("successor control DDL differs from independent exact contract")
	}
}

func TestSuccessorSourceSelection(t *testing.T) {
	for i, fixture := range provenanceSources {
		t.Run(fixture.profile+"/"+fixture.revision[:8], func(t *testing.T) {
			db := archivedSchemaFixture(t, fixture.revision)
			version := 30
			if i == 0 {
				version = 28
			}
			if i == 1 {
				version = 29
			}
			assertSuccessorSelection(t, db, schemaSource{fixture.profile, schemaFormat{"main", version}})
		})
	}
	db, _ := completeMigrationFixture(t)
	if err := migrateTenantSchema(context.Background(), db, nil); err != nil {
		t.Fatal(err)
	}
	assertSuccessorSelection(t, db, schemaSource{"private29", schemaFormat{"private-tenant", 29}})
}

func assertSuccessorSelection(t *testing.T, db *sql.DB, source schemaSource) {
	t.Helper()
	before := recoverySnapshot(t, db)
	if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	plan, selectionErr := selectSuccessorSchema(context.Background(), tx)
	var alive int
	if err := tx.QueryRow("SELECT 1").Scan(&alive); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA query_only=OFF"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
		t.Fatal("selection mutated source")
	}
	if selectionErr != nil || plan != (successorSchemaPlan{source, schemaFormat{"tenant", 31}}) {
		t.Fatalf("wrong source/target separation: %+v, %v", plan, selectionErr)
	}
}

func TestSuccessorProvenanceRecord(t *testing.T) {
	for _, source := range []schemaSource{
		{"main28", schemaFormat{"main", 28}}, {"main29", schemaFormat{"main", 29}},
		{"main30-pre-sync", schemaFormat{"main", 30}}, {"main30-initial-sync", schemaFormat{"main", 30}},
		{"main30-devices", schemaFormat{"main", 30}}, {"private29", schemaFormat{"private-tenant", 29}},
	} {
		t.Run(source.profile, func(t *testing.T) {
			db := successorRecordFixture(t, source.profile, source.format.family, source.format.version)
			before := recoverySnapshot(t, db)
			if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
				t.Fatal(err)
			}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 2; i++ {
				got, err := readSuccessorProvenanceRecord(context.Background(), tx)
				if err != nil || got != source {
					t.Errorf("record=%+v error=%v", got, err)
				}
				// Metadata does not certify the missing eleven tables or permit routing.
				if plan, err := selectSuccessorSchema(context.Background(), tx); err == nil || plan != (successorSchemaPlan{}) {
					t.Error("control-only fixture admitted as a complete successor")
				}
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("PRAGMA query_only=OFF"); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
				t.Fatal("record inspection mutated database")
			}
		})
	}
}

func TestSuccessorProvenanceRefusesDamagedRecord(t *testing.T) {
	for _, mutation := range []string{
		"DROP TRIGGER schema_provenance_no_update",
		"CREATE TABLE schema_provenance_unknown(x)",
		"CREATE VIEW SCHEMA_PROVENANCE_UNKNOWN AS SELECT 1",
		"CREATE INDEX alien ON schema_provenance(source_profile)",
		"CREATE TRIGGER alien AFTER INSERT ON schema_version BEGIN SELECT 1; END",
		"ALTER TABLE schema_provenance ADD COLUMN alien TEXT",
		"DELETE FROM schema_version WHERE version=31",
		"DELETE FROM schema_version WHERE version=29",
		"INSERT INTO schema_version(version) VALUES (0)",
		"INSERT INTO schema_version(version) VALUES (30)",
		"INSERT INTO schema_version(version) VALUES (32)",
		"PRAGMA ignore_check_constraints=ON; DROP TRIGGER schema_provenance_no_update; UPDATE schema_provenance SET source_family='main'; " + successorControlContract["schema_provenance_no_update"],
		"DROP TRIGGER schema_provenance_no_delete; DELETE FROM schema_provenance; " + successorControlContract["schema_provenance_no_delete"],
		"CREATE TEMP TABLE schema_provenance(fake TEXT)",
	} {
		t.Run(mutation, func(t *testing.T) {
			db := successorRecordFixture(t, "private29", "private-tenant", 29)
			if _, err := db.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			before := recoverySnapshot(t, db)
			if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
				t.Fatal(err)
			}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			if got, err := readSuccessorProvenanceRecord(context.Background(), tx); err == nil || got != (schemaSource{}) {
				t.Errorf("damaged record accepted: %+v %v", got, err)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("PRAGMA query_only=OFF"); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
				t.Fatal("damaged record inspection mutated database")
			}
		})
	}
}

func TestSuccessorProvenanceRejectsInconsistentFields(t *testing.T) {
	for _, assignment := range []string{
		"singleton=2", "source_profile='main29'", "source_profile='unreviewed'",
		"source_family='main'", "source_version=28", "target_family='private-tenant'", "target_version=29",
	} {
		t.Run(assignment, func(t *testing.T) {
			db := successorRecordFixture(t, "private29", "private-tenant", 29)
			// Corrupt rows while restoring the exact DDL. Validation must check
			// stored values independently, not assume CHECK has always been on.
			if _, err := db.Exec("PRAGMA ignore_check_constraints=ON; DROP TRIGGER schema_provenance_no_update; UPDATE schema_provenance SET " + assignment + "; " + successorControlContract["schema_provenance_no_update"]); err != nil {
				t.Fatal(err)
			}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			defer tx.Rollback()
			if source, err := readSuccessorProvenanceRecord(context.Background(), tx); err == nil || source != (schemaSource{}) {
				t.Errorf("inconsistent fields accepted: %+v %v", source, err)
			}
		})
	}
}

func TestSuccessorSelectionRejectsUnknownCatalog(t *testing.T) {
	for _, mutation := range []string{
		"CREATE TABLE alien(x)", "CREATE INDEX alien ON notes(title)",
		"CREATE TRIGGER alien AFTER INSERT ON notes BEGIN SELECT 1; END",
		"CREATE VIEW alien AS SELECT 1", "ALTER TABLE notes ADD COLUMN alien TEXT",
		"CREATE TABLE schema_provenance(x)", "INSERT INTO schema_version(version) VALUES (29)",
		"CREATE TEMP TABLE notes(x)",
	} {
		t.Run(mutation, func(t *testing.T) {
			db := compatibilityDB(t, filepath.Join(t.TempDir(), "source.db"))
			db.SetMaxOpenConns(1)
			if err := InitSchema(db); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(mutation); err != nil {
				t.Fatal(err)
			}
			before := recoverySnapshot(t, db)
			if _, err := db.Exec("PRAGMA query_only=ON"); err != nil {
				t.Fatal(err)
			}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			if plan, err := selectSuccessorSchema(context.Background(), tx); err == nil || plan != (successorSchemaPlan{}) {
				t.Errorf("unreviewed catalog selected: %+v %v", plan, err)
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec("PRAGMA query_only=OFF"); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
				t.Fatal("selection mutated unknown source")
			}
		})
	}
	for _, profile := range []string{"", "29", "tenant31", "main30", "MAIN29"} {
		if _, err := describeSchemaSource(profile); err == nil {
			t.Errorf("unknown profile accepted: %q", profile)
		}
	}
}

func TestSuccessorProvenanceImmutable(t *testing.T) {
	db := successorRecordFixture(t, "main29", "main", 29)
	before := recoverySnapshot(t, db)
	for _, mutation := range []string{
		"UPDATE schema_provenance SET source_profile='private29',source_family='private-tenant'",
		"DELETE FROM schema_provenance",
		"INSERT OR REPLACE INTO schema_provenance VALUES (1,'private29','private-tenant',29,'tenant',31)",
		"INSERT INTO schema_provenance VALUES (1,'main29','main',29,'tenant',31) ON CONFLICT(singleton) DO UPDATE SET source_profile=excluded.source_profile",
	} {
		if _, err := db.Exec(mutation); err == nil {
			t.Errorf("permitted %s", mutation)
		}
		if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
			t.Fatal("immutable provenance changed")
		}
	}
}

func TestSuccessorInvalidInspection(t *testing.T) {
	db := successorRecordFixture(t, "main28", "main", 28)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, input := range []struct {
		ctx context.Context
		tx  *sql.Tx
	}{{nil, tx}, {context.Background(), nil}, {cancelled, tx}} {
		if _, err := readSuccessorProvenanceRecord(input.ctx, input.tx); err == nil {
			t.Error("invalid record inspection accepted")
		}
		if _, err := selectSuccessorSchema(input.ctx, input.tx); err == nil {
			t.Error("invalid selection accepted")
		}
	}
}

func TestProvenanceArtifactsRefusedBeforePublicMutation(t *testing.T) {
	for _, entry := range []string{"init", "migration", "with-db", "new"} {
		for _, fixture := range []string{"runtime28", "unversioned", "renamed-trigger", "case-alias"} {
			t.Run(entry+"/"+fixture, func(t *testing.T) {
				path := filepath.Join(t.TempDir(), "runtime.db")
				db := compatibilityDB(t, path)
				db.SetMaxOpenConns(1)
				if fixture != "unversioned" {
					if err := InitSchema(db); err != nil {
						t.Fatal(err)
					}
				}
				ddl := "CREATE TABLE schema_provenance(unreviewed TEXT)"
				if fixture == "renamed-trigger" {
					ddl = "CREATE TRIGGER schema_provenance_unknown AFTER INSERT ON notes BEGIN SELECT 1; END"
				}
				if fixture == "case-alias" {
					ddl = "CREATE VIEW SCHEMA_PROVENANCE AS SELECT 1"
				}
				if _, err := db.Exec(ddl); err != nil {
					t.Fatal(err)
				}
				before := recoverySnapshot(t, db)
				journal := compatibilityRows(t, db, "PRAGMA journal_mode")
				var err error
				switch entry {
				case "init":
					err = InitSchema(db)
				case "migration":
					err = migrateSchema(db)
				case "with-db":
					var owner *StorageLayer
					owner, err = NewWithDB(db)
					if owner != nil {
						t.Error("returned owner for provenance artifact")
					}
				case "new":
					var owner *StorageLayer
					owner, err = New(path)
					if owner != nil {
						owner.Close()
						t.Error("returned owner for provenance artifact")
					}
				}
				if err == nil {
					t.Error("public startup accepted reserved provenance artifact")
				}
				if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
					t.Error("refusal mutated catalog/rows/sequences")
				}
				if !reflect.DeepEqual(journal, compatibilityRows(t, db, "PRAGMA journal_mode")) {
					t.Error("refusal mutated journal mode")
				}
			})
		}
	}
}

func TestSuccessorPublicControlsRemainRefused(t *testing.T) {
	db := successorRecordFixture(t, "main29", "main", 29)
	before := recoverySnapshot(t, db)
	for _, open := range []func() error{
		func() error { return InitSchema(db) },
		func() error { return migrateSchema(db) },
		func() error { _, err := NewWithDB(db); return err },
	} {
		if err := open(); err == nil {
			t.Error("control-only successor publicly admitted")
		}
		if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
			t.Error("public refusal mutated control-only fixture")
		}
	}
}

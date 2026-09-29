package storage

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
	"testing"
)

func successorSourceFixture(t *testing.T, revision string) *sql.DB {
	t.Helper()
	db := archivedSchemaFixture(t, revision)
	db.SetMaxOpenConns(1)
	// Load actual data into the archived schema, NEVER substitute current DDL.
	// This independently populates all 26 historical workloads and credentials,
	// permanent claim, real roots/CAS and deleted physical-ID high-water state.
	seed, _ := completeMigrationFixture(t)
	_, err := db.Exec("ATTACH DATABASE ? AS fixture", recoveryPath(t, seed))
	collisionMust(t, err)
	for _, table := range append(append([]string{}, relationalTenantTables...), "api_tokens", "oauth_clients", "oauth_auth_codes", "oauth_access_tokens", "oauth_refresh_tokens", "tenant_roots") {
		relationalExec(t, db, "INSERT INTO "+table+" SELECT * FROM fixture."+table)
	}
	relationalExec(t, db, `UPDATE sqlite_sequence SET seq=(SELECT seq FROM fixture.sqlite_sequence f WHERE f.name=sqlite_sequence.name) WHERE name IN (SELECT name FROM fixture.sqlite_sequence)`)
	relationalExec(t, db, "DETACH DATABASE fixture")
	relationalExec(t, db, `INSERT INTO notes(id,path,short_id,title,body) VALUES(41,'projects/p/note/live.md','live0001','title','body');
 UPDATE entry_meta SET access_count=7,last_verified='original' WHERE path='brain:system/install_claimed';`)
	return db
}

func TestSuccessorMigrationActualProfiles(t *testing.T) {
	for _, source := range provenanceSources {
		t.Run(source.profile+source.revision[:8], func(t *testing.T) {
			db := successorSourceFixture(t, source.revision)
			tables := []string{}
			if source.profile != "main28" {
				relationalExec(t, db, `INSERT INTO bulk_jobs VALUES('local','job','req','hash',' { "raw": true } ','update','needs_attention','actor','created','updated');
 INSERT INTO bulk_job_items VALUES('local','job',7,'gone','entry','title','fingerprint','uncertain',3,'unknown','dest');`)
				tables = append(tables, "bulk_jobs", "bulk_job_items")
			}
			if strings.HasPrefix(source.profile, "main30") {
				relationalExec(t, db, `INSERT INTO execution_budgets VALUES('local','p','budget','UTC','tasks',99,7);
 INSERT INTO budget_reservations VALUES('local','p','budget','parent','','old-window',3,'reserved');
 INSERT INTO supervisor_checkpoints VALUES('local','p','cp',2,' { "revision": 2 } ');
 INSERT INTO supervisor_checkpoint_versions VALUES('local','p','cp',1,' { "revision": 1 } ');
 INSERT INTO supervisor_checkpoint_versions VALUES('local','p','cp',2,' { "revision": 2 } ');
 INSERT INTO supervisor_operations VALUES('local','actor','op','prompt','digest','outcome_unknown','created','updated','never replay');`)
				tables = append(tables, "execution_budgets", "budget_reservations", "supervisor_checkpoints", "supervisor_checkpoint_versions", "supervisor_operations")
			}
			sync := source.profile == "main30-initial-sync" || source.profile == "main30-devices"
			if sync {
				relationalExec(t, db, `UPDATE entry_sync_identity SET epoch='0123456789abcdef0123456789abcdef';
 INSERT INTO notes(id,path,short_id,title) VALUES(77,'gone','deleted0','deleted'); DELETE FROM notes WHERE id=77;
 INSERT INTO entry_sync_operations VALUES('reserved','hash',0,'');
 INSERT INTO entry_sync_operations VALUES('receipt','hash',409,' { "conflict": true } ');
 UPDATE sqlite_sequence SET seq=900 WHERE name='entry_sync_changes';`)
				tables = append(tables, "entry_sync_identity", "entry_sync_changes", "entry_sync_operations")
				if source.profile == "main30-devices" {
					relationalExec(t, db, `INSERT INTO entry_sync_devices VALUES('device',' { "device_id": "device", "pending": [] } ');`)
					tables = append(tables, "entry_sync_devices")
				}
			}
			before := map[string][]string{}
			columns := map[string]string{}
			baseBefore := map[string][]string{}
			baseColumns := map[string]string{}
			tx, err := db.Begin()
			if err != nil {
				t.Fatal(err)
			}
			for _, table := range tables {
				cols, e := relationalColumns(tx, table)
				if e != nil {
					t.Fatal(e)
				}
				columns[table] = strings.Join(cols, ",")
			}
			for _, table := range relationalTenantTables {
				cols, e := relationalColumns(tx, table)
				collisionMust(t, e)
				baseColumns[table] = strings.Join(cols, ",")
				where := ""
				if table == "entry_meta" {
					where = " WHERE path IS NOT 'brain:system/install_claimed'"
				}
				baseBefore[table] = relationalSnapshot(t, tx, "(SELECT "+baseColumns[table]+" FROM "+table+where+")")
				if len(baseBefore[table]) == 0 {
					t.Fatalf("unpopulated archived workload %s", table)
				}
			}
			_ = tx.Rollback()
			for _, table := range tables {
				before[table] = relationalSnapshot(t, db, table)
			}
			if err = migrateSuccessorSchema(context.Background(), db, nil); err != nil {
				t.Fatalf("complete successor migration: %v", err)
			}
			for table, want := range baseBefore {
				got := relationalSnapshot(t, db, "(SELECT "+baseColumns[table]+" FROM "+table+" WHERE tenant_id='local')")
				if !reflect.DeepEqual(want, got) {
					t.Fatalf("archived original rows changed: %s", table)
				}
			}
			for _, table := range tables {
				if got := relationalSnapshot(t, db, "(SELECT "+columns[table]+" FROM "+table+")"); !reflect.DeepEqual(got, before[table]) {
					t.Fatalf("changed original %s: %v != %v", table, got, before[table])
				}
			}
			if v, e := GetSchemaVersion(db); e != nil || v != 31 {
				t.Fatalf("version %d: %v", v, e)
			}
			if sync {
				var high int
				if e := db.QueryRow("SELECT seq FROM sqlite_sequence WHERE name='entry_sync_changes'").Scan(&high); e != nil || high != 900 {
					t.Fatalf("lost highwater %d: %v", high, e)
				}
			}
			recoveryIntegrity(t, db)
			migrationFK(t, db)
			snapshot := recoverySnapshot(t, db)
			if err = migrateSuccessorSchema(context.Background(), db, nil); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(snapshot, recoverySnapshot(t, db)) {
				t.Fatal("repeat repaired/replayed successor")
			}
			if err = InitSchema(db); err == nil {
				t.Fatal("public activation")
			}
		})
	}
}

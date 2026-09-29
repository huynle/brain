package storage

import (
	"reflect"
	"strings"
	"testing"
)

func TestTenantRelationalProjectKeys(t *testing.T) {
	db := relationalFixture(t)
	tx := relationalBegin(t, db)
	if err := stageTenantRelationalSchema(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	} // Isolated constraint fixture only, not a supported activation path.
	relationalExec(t, db, `PRAGMA foreign_keys=ON;
 INSERT INTO tenants(id,name,status,created_at) VALUES('B','B','active','now');
 INSERT INTO tenant_runner_keys VALUES('B','runner')`)
	// Explicit acceptance inventory: P4.1 already rebuilt all seven, including
	// cascade roots and feature pause. Do not derive expected keys from its DDL.
	for _, tc := range []struct{ table, key string }{
		{"task_claims", "tenant_id,project_id,task_id"},
		{"task_dispatch_leases", "tenant_id,project_id,task_id"},
		{"feature_assignments", "tenant_id,project_id,feature_id"},
		{"feature_pause_state", "tenant_id,project_id,feature_id"},
		{"feature_cascade_roots", "tenant_id,project_id,root_feature_id"},
		{"project_pause_state", "tenant_id,project_id"},
		{"project_placement", "tenant_id,project_id"},
	} {
		t.Run(tc.table, func(t *testing.T) {
			var key, indexKey, columns, values string
			if err := db.QueryRow(`SELECT group_concat(name,',') FROM
 (SELECT name FROM pragma_table_info(?) WHERE pk>0 ORDER BY pk)`, tc.table).Scan(&key); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT group_concat(name,',') FROM
 (SELECT ii.name FROM pragma_index_list(?) il, pragma_index_info(il.name) ii
 WHERE il.origin='pk' AND il."unique"=1 ORDER BY ii.seqno)`, tc.table).Scan(&indexKey); err != nil {
				t.Fatal(err)
			}
			if key != tc.key || indexKey != tc.key {
				t.Fatalf("PK columns=%q backing unique index=%q want=%q", key, indexKey, tc.key)
			}
			// Reuse the existing populated fixture, changing only project/tenant.
			relationalExec(t, db, "UPDATE "+tc.table+" SET project_id='brain'")
			if err := db.QueryRow(`SELECT group_concat(name,','),group_concat(
 CASE WHEN name='tenant_id' THEN '''B''' ELSE name END,',')
 FROM (SELECT name FROM pragma_table_info(?) ORDER BY cid)`, tc.table).Scan(&columns, &values); err != nil {
				t.Fatal(err)
			}
			relationalExec(t, db, "INSERT INTO "+tc.table+"("+columns+") SELECT "+values+" FROM "+tc.table+" WHERE tenant_id='local'")
			for _, tenant := range []string{"local", "B"} {
				var n int
				if err := db.QueryRow("SELECT count(*) FROM "+tc.table+" WHERE tenant_id=? AND project_id='brain'", tenant).Scan(&n); err != nil || n != 1 {
					t.Fatalf("%s coexistence count=%d err=%v", tenant, n, err)
				}
				if _, err := db.Exec("INSERT INTO "+tc.table+" SELECT * FROM "+tc.table+" WHERE tenant_id=?", tenant); err == nil || !strings.Contains(err.Error(), "UNIQUE constraint failed") {
					t.Fatalf("%s duplicate must fail uniqueness: %v", tenant, err)
				}
			}
		})
	}
}

func TestTenantRelationalForeignKeyInventory(t *testing.T) {
	db := relationalFixture(t)
	tx := relationalBegin(t, db)
	if err := stageTenantRelationalSchema(tx); err != nil {
		t.Fatal(err)
	}
	// Explicit parent/from/to/delete contracts. All updates are NO ACTION. This
	// includes durable reference keys, NOT invented task/project/feature parents.
	extra := map[string][]string{
		"links":                   {"notes:tenant_id,source_id:tenant_id,id:CASCADE", "notes:tenant_id,target_id:tenant_id,id:NO ACTION"},
		"tags":                    {"notes:tenant_id,note_id:tenant_id,id:CASCADE"},
		"note_embeddings":         {"notes:tenant_id,note_id:tenant_id,id:CASCADE"},
		"note_embeddings_meta":    {"notes:tenant_id,note_id:tenant_id,id:CASCADE", "note_embeddings:tenant_id,note_id,chunk_index:tenant_id,note_id,chunk_index:CASCADE"},
		"entry_attachments":       {"notes:tenant_id,note_id:tenant_id,id:CASCADE", "attachments:tenant_id,attachment_id:tenant_id,id:RESTRICT"},
		"attachment_derived":      {"attachments:tenant_id,attachment_id:tenant_id,id:CASCADE"},
		"webhook_deliveries":      {"webhooks:tenant_id,webhook_id:tenant_id,id:CASCADE"},
		"task_claims":             {"tenant_runner_keys:tenant_id,runner_id:tenant_id,runner_id:NO ACTION"},
		"task_dispatch_leases":    {"tenant_runner_keys:tenant_id,assigned_runner_id:tenant_id,runner_id:NO ACTION"},
		"task_placement_reasons":  {"tenant_runner_keys:tenant_id,runner_reference:tenant_id,runner_id:NO ACTION"},
		"feature_assignments":     {"tenant_runner_keys:tenant_id,runner_id:tenant_id,runner_id:NO ACTION"},
		"runners":                 {"tenant_runner_keys:tenant_id,runner_id:tenant_id,runner_id:NO ACTION"},
		"runner_pause_state":      {"tenant_runner_keys:tenant_id,runner_id:tenant_id,runner_id:NO ACTION"},
		"opencode_instances":      {"tenant_runner_keys:tenant_id,runner_id:tenant_id,runner_id:NO ACTION"},
		"brain_clients":           {"tenant_client_keys:tenant_id,client_id:tenant_id,client_id:NO ACTION"},
		"brain_client_workspaces": {"tenant_client_keys:tenant_id,client_id:tenant_id,client_id:NO ACTION"},
	}
	for _, table := range append(append([]string{}, relationalTenantTables...), "tenant_runner_keys", "tenant_client_keys") {
		t.Run(table, func(t *testing.T) {
			want := map[string]bool{"tenants:tenant_id:id:NO ACTION:NO ACTION:NONE": true}
			for _, contract := range extra[table] {
				want[contract+":NO ACTION:NONE"] = true
			}
			rows, err := tx.Query(`SELECT "table" || ':' || group_concat("from",',') || ':' ||
 group_concat("to",',') || ':' || on_delete || ':' || on_update || ':' || match
 FROM (SELECT * FROM pragma_foreign_key_list(?) ORDER BY id,seq) GROUP BY id`, table)
			if err != nil {
				t.Fatal(err)
			}
			defer rows.Close()
			got := map[string]bool{}
			for rows.Next() {
				var contract string
				if err := rows.Scan(&contract); err != nil {
					t.Fatal(err)
				}
				if got[contract] {
					t.Fatalf("duplicate FK %s", contract)
				}
				got[contract] = true
			}
			if err := rows.Err(); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("FK inventory: got %v want %v", got, want)
			}
		})
	}
}

func TestTenantRelationalDownstreamFailureRollsBackOuter(t *testing.T) {
	db := relationalFixture(t)
	// Snapshot all ordinary workload/control rows plus the complete catalog,
	// sequences, and legacy FTS shadows, not just the relational savepoint.
	before := map[string][]string{}
	tables := append(append([]string{}, relationalTenantTables...), relationalControlTables...)
	tables = append(tables, "sqlite_schema", "sqlite_sequence", "notes_fts_data", "notes_fts_idx", "notes_fts_docsize", "notes_fts_config")
	for _, table := range tables {
		before[table] = relationalSnapshot(t, db, table)
	}
	tx := relationalBegin(t, db)
	if err := stageTenantRelationalSchema(tx); err != nil {
		t.Fatal(err)
	}
	// Stand-in for downstream work AFTER the component released its savepoint.
	// It is deliberately not an implementation of FTS/CAS/runtime cutover.
	relationalExec(t, tx, `CREATE TABLE downstream_probe(value INTEGER CHECK(value=1));
 INSERT INTO downstream_probe VALUES(1);
 UPDATE notes SET title='downstream mutation' WHERE id=10`)
	if _, err := tx.Exec("INSERT INTO downstream_probe VALUES(2)"); err == nil || !strings.Contains(err.Error(), "CHECK constraint failed") {
		t.Fatalf("expected simulated downstream failure, got %v", err)
	}
	var version int
	if err := tx.QueryRow("SELECT max(version) FROM schema_version").Scan(&version); err != nil || version != 28 {
		t.Fatalf("premature publication: version=%d err=%v", version, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	relationalExec(t, db, "PRAGMA foreign_keys=ON")
	for table, want := range before {
		if got := relationalSnapshot(t, db, table); !reflect.DeepEqual(got, want) {
			t.Errorf("outer rollback changed %s:\nwant %v\ngot %v", table, want, got)
		}
	}
	var fk int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Fatalf("FK enforcement=%d err=%v", fk, err)
	}
	if got := relationalSnapshot(t, db, "pragma_foreign_key_check"); len(got) != 0 {
		t.Fatalf("rollback FK violations: %v", got)
	}
	if got := relationalSnapshot(t, db, "pragma_integrity_check"); !reflect.DeepEqual(got, []string{"[ok]"}) {
		t.Fatalf("rollback integrity: %v", got)
	}
}

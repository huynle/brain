package storage

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// Independent inventory of v28 application tables; FTS shadows are separate.
var relationalTenantTables = strings.Fields(`notes links tags entry_meta generated_tasks event_log
 task_claims task_dispatch_leases task_placement_reasons feature_assignments runners opencode_instances
 project_pause_state feature_pause_state runner_pause_state feature_cascade_roots project_placement
 brain_clients brain_client_workspaces webhooks webhook_deliveries note_embeddings note_embeddings_meta
 attachments entry_attachments attachment_derived`)
var relationalControlTables = strings.Fields(`schema_version api_tokens oauth_clients oauth_auth_codes
 oauth_access_tokens oauth_refresh_tokens tenant_roots`)

func TestTenantRelationalAnalyzedFixture(t *testing.T) {
	db := relationalFixture(t)
	relationalExec(t, db, "ANALYZE")
	tx := relationalBegin(t, db)
	if err := stageTenantRelationalSchema(tx); err != nil {
		t.Fatalf("approved optional SQLite engine statistics rejected: %v", err)
	}
}

func relationalExec(t *testing.T, q interface {
	Exec(string, ...any) (sql.Result, error)
}, stmt string) {
	t.Helper()
	if _, err := q.Exec(stmt); err != nil {
		t.Fatalf("%s: %v", stmt, err)
	}
}

func relationalFixture(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "v28.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	relationalExec(t, db, "PRAGMA foreign_keys=ON")
	if err := InitSchema(db); err != nil {
		t.Fatal(err)
	}
	// Deleted high IDs exercise sqlite_sequence preservation, not just MAX(id).
	relationalExec(t, db, `INSERT INTO notes(id,path,short_id) VALUES(900,'deleted','deleted'); DELETE FROM notes WHERE id=900;
 INSERT INTO notes(id,path,short_id,title) VALUES(10,'projects/p/task/same.md','same','source'),(11,'target','target','target');
 INSERT INTO links(id,source_id,target_path,target_id,href) VALUES(20,10,'target',11,'target');
 INSERT INTO tags(id,note_id,tag) VALUES(30,10,'tag');
 INSERT INTO entry_meta(path,project_id,access_count) VALUES('orphan','deleted-project',7),('brain:system/install_claimed',NULL,0);
 INSERT INTO generated_tasks(key,task_path,feature_id) VALUES('dedup','deleted-task','deleted-feature');
 INSERT INTO event_log(id,event_type,payload,dedup_key) VALUES(40,'task.completed','{"project_id":"p"}','dedup');
 INSERT INTO runners(runner_id,hostname,registered_at,last_heartbeat) VALUES('runner','host',1,2);
 INSERT INTO task_claims VALUES('p','same','runner',1,2);
 INSERT INTO task_dispatch_leases(project_id,task_id,lease_id,assigned_runner_id,state,pushed_at,expires_at) VALUES('p','same','lease','runner','acked',1,2);
 INSERT INTO task_placement_reasons(id,project_id,task_id,runner_id,decision,created_at) VALUES(50,'deleted-project','deleted-task','gone-runner','no_candidate',1);
 INSERT INTO feature_assignments VALUES('p','feature','runner','manual','assigned',1,2);
 INSERT INTO opencode_instances(instance_id,runner_id,task_id,session_ids) VALUES('instance','runner','same','["session"]');
 INSERT INTO project_pause_state VALUES('deleted-project',1,1,2);
 INSERT INTO feature_pause_state VALUES('deleted-project','deleted-feature',1,2);
 INSERT INTO runner_pause_state VALUES('gone-runner',1,2);
 INSERT INTO feature_cascade_roots VALUES('deleted-project','deleted-feature',1,1);
 INSERT INTO project_placement(project_id) VALUES('p');
 INSERT INTO brain_clients(client_id,host_id,registered_at,last_seen) VALUES('client','host',1,2);
 INSERT INTO brain_client_workspaces(id,client_id,host_id,project_id,path,first_seen,last_seen) VALUES(60,'client','host','p','/workspace',1,2);
 INSERT INTO webhooks(id,name,url,events,created_at,updated_at) VALUES('hook','hook','https://example.invalid','["task.*"]','then','now');
 INSERT INTO webhook_deliveries(id,webhook_id,event_type,success,created_at) VALUES('delivery','hook','task.completed',1,'now');
 INSERT INTO note_embeddings VALUES(10,0,x'0102');
 INSERT INTO note_embeddings_meta(note_id,chunk_index,project_id) VALUES(10,0,'p');
 INSERT INTO attachments(id,digest,size) VALUES(70,'digest',2);
 INSERT INTO entry_attachments(id,note_id,attachment_id) VALUES(80,10,70);
 INSERT INTO attachment_derived(id,attachment_id,text) VALUES(90,70,'text');
 INSERT INTO api_tokens(name,token) VALUES('operator','secret');
 INSERT INTO oauth_clients(client_id,client_secret,redirect_uris,grant_types,response_types,created_at) VALUES('oauth','secret','[]','[]','[]',1);
 INSERT INTO oauth_auth_codes(code,client_id,redirect_uri,code_challenge,expires_at,created_at) VALUES('code','oauth','uri','challenge',2,1);
 INSERT INTO oauth_access_tokens(token,client_id,expires_at,created_at) VALUES('access','password-login',2,1);
 INSERT INTO oauth_refresh_tokens(token,client_id,expires_at,created_at) VALUES('refresh','password-login',2,1);
 INSERT INTO tenant_roots VALUES('local','relative-brain','relative-cas','/old/brain','/old/cas','/canonical/brain','/canonical/cas','legacy');`)
	return db
}

func relationalSnapshot(t *testing.T, q interface {
	Query(string, ...any) (*sql.Rows, error)
}, table string) []string {
	t.Helper()
	rows, err := q.Query("SELECT * FROM " + table + " ORDER BY 1")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		t.Fatal(err)
	}
	result := []string{}
	for rows.Next() {
		values := make([]any, len(cols))
		args := make([]any, len(cols))
		for i := range args {
			args[i] = &values[i]
		}
		if err := rows.Scan(args...); err != nil {
			t.Fatal(err)
		}
		result = append(result, fmt.Sprint(values))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	// ORDER BY 1 is not a total order (tenant_id is commonly column one).
	// Canonicalize complete rows, retaining duplicates, independently of indexes.
	sort.Strings(result)
	return result
}

func relationalBegin(t *testing.T, db *sql.DB) *sql.Tx {
	t.Helper()
	// SQLite table rebuild requires disabling FKs BEFORE the enclosing transaction.
	relationalExec(t, db, "PRAGMA foreign_keys=OFF")
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	return tx
}

func TestTenantRelationalBackfill(t *testing.T) {
	db := relationalFixture(t)
	controls := map[string][]string{}
	for _, table := range relationalControlTables {
		controls[table] = relationalSnapshot(t, db, table)
	}
	tx := relationalBegin(t, db)
	data := map[string][]string{}
	columns := map[string]string{}
	for _, table := range relationalTenantTables {
		cols, err := relationalColumns(tx, table)
		if err != nil {
			t.Fatal(err)
		}
		columns[table] = strings.Join(cols, ",")
		where := ""
		if table == "entry_meta" {
			where = " WHERE path!='brain:system/install_claimed'"
		}
		data[table] = relationalSnapshot(t, tx, "(SELECT "+columns[table]+" FROM "+table+where+")")
	}
	fts := relationalSnapshot(t, tx, "(SELECT rowid,title,body,path FROM notes_fts)")
	claim := relationalSnapshot(t, tx, "(SELECT * FROM entry_meta WHERE path='brain:system/install_claimed')")
	if err := stageTenantRelationalSchema(tx); err != nil {
		t.Fatal(err)
	}
	for _, table := range relationalTenantTables {
		got := relationalSnapshot(t, tx, "(SELECT "+columns[table]+" FROM "+table+" WHERE tenant_id='local')")
		if !reflect.DeepEqual(got, data[table]) {
			t.Fatalf("%s changed legacy values: %v -> %v", table, data[table], got)
		}
		var unowned, total int
		if err := tx.QueryRow("SELECT count(*) FROM " + table + " WHERE tenant_id IS NULL OR tenant_id = ''").Scan(&unowned); err != nil {
			t.Fatalf("%s missing ownership: %v", table, err)
		}
		if unowned != 0 {
			t.Fatalf("%s: %d unowned", table, unowned)
		}
		if err := tx.QueryRow("SELECT count(*) FROM " + table + " WHERE tenant_id='local'").Scan(&total); err != nil {
			t.Fatal(err)
		}
		want := 1
		if table == "notes" {
			want = 2
		}
		if total != want {
			t.Fatalf("%s count=%d want=%d", table, total, want)
		}
	}
	if got := relationalSnapshot(t, tx, "operator_install_claim"); !reflect.DeepEqual(got, claim) {
		t.Fatalf("claim changed: %v", got)
	}
	if got := relationalSnapshot(t, tx, "(SELECT rowid,title,body,path FROM notes_fts)"); !reflect.DeepEqual(got, fts) {
		t.Fatalf("legacy FTS changed: %v", got)
	}
	var seq int
	if err := tx.QueryRow("SELECT seq FROM sqlite_sequence WHERE name='notes'").Scan(&seq); err != nil || seq != 900 {
		t.Fatalf("lost sequence highwater: %d %v", seq, err)
	}
	for table, want := range controls {
		if got := relationalSnapshot(t, tx, table); !reflect.DeepEqual(got, want) {
			t.Fatalf("control table %s changed: %v", table, got)
		}
	}
	var claimed int
	if err := tx.QueryRow("SELECT count(*) FROM operator_install_claim").Scan(&claimed); err != nil || claimed != 1 {
		t.Fatalf("claim=%d err=%v", claimed, err)
	}
	if err := stageTenantRelationalSchema(tx); err != nil {
		t.Fatalf("repeat component: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	// No commit, no version mutation, no partial relational artifacts after rollback.
	var count int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='tenants'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("rollback count=%d err=%v", count, err)
	}
	for table, want := range controls {
		if got := relationalSnapshot(t, db, table); !reflect.DeepEqual(got, want) {
			t.Fatalf("rollback changed %s", table)
		}
	}
	if CurrentSchemaVersion != 28 {
		t.Fatalf("premature schema advance: %d", CurrentSchemaVersion)
	}
}

func TestTenantRelationalRepeatRejectsDamage(t *testing.T) {
	for _, damage := range []string{
		"DROP TRIGGER p4_claim_no_delete",
		"DROP TRIGGER p4_link_target_delete",
		"DROP INDEX p4_owner_notes",
		"DROP INDEX idx_event_log_dedup_key",
		"PRAGMA ignore_check_constraints=ON; UPDATE notes SET tenant_id=''",
	} {
		t.Run(damage, func(t *testing.T) {
			db := relationalFixture(t)
			tx := relationalBegin(t, db)
			if err := stageTenantRelationalSchema(tx); err != nil {
				t.Fatal(err)
			}
			relationalExec(t, tx, damage)
			if err := stageTenantRelationalSchema(tx); err == nil {
				t.Fatal("accepted damaged relational schema")
			}
		})
	}
}

func TestTenantRelationalEveryOwnershipAndIndex(t *testing.T) {
	db := relationalFixture(t)
	var ordinary int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'notes_fts%' AND name!='sqlite_sequence'").Scan(&ordinary); err != nil || ordinary != 33 {
		t.Fatalf("ordinary inventory=%d err=%v", ordinary, err)
	}
	// Read the actual v28 catalog, not the migration's index DDL. Preserve the
	// entire named-index contract: identity, uniqueness, ordered columns, predicate.
	const indexShape = `(SELECT il.name,il."unique",il.partial,
 %s(SELECT group_concat(name,',') FROM (SELECT name FROM pragma_index_info(il.name) ORDER BY seqno)),
 CASE WHEN il.partial THEN substr(s.sql,instr(upper(s.sql),'WHERE')) ELSE '' END
 FROM pragma_index_list('%s') il JOIN sqlite_schema s ON s.name=il.name
 WHERE il.origin='c' AND il.name NOT LIKE 'p4_owner_%%')`
	indexes := map[string][]string{}
	// SQLite renumbers autoindexes when composite keys are added. Compare their
	// ordered unique columns instead of treating unstable names as identities.
	const uniqueShape = `(SELECT %s(SELECT group_concat(name,',') FROM
 (SELECT name FROM pragma_index_info(il.name) ORDER BY seqno)) AS columns
 FROM pragma_index_list('%s') il WHERE il.origin='u' AND il."unique"=1)`
	uniqueKeys := map[string][]string{}
	for _, table := range relationalTenantTables {
		indexes[table] = relationalSnapshot(t, db, fmt.Sprintf(indexShape, "'tenant_id,' || ", table))
		uniqueKeys[table] = relationalSnapshot(t, db, fmt.Sprintf(uniqueShape, "'tenant_id,' || ", table))
	}
	tx := relationalBegin(t, db)
	if err := stageTenantRelationalSchema(tx); err != nil {
		t.Fatal(err)
	}
	for _, table := range relationalTenantTables {
		var nn int
		var def sql.NullString
		if err := tx.QueryRow("SELECT \"notnull\",dflt_value FROM pragma_table_info(?) WHERE name='tenant_id'", table).Scan(&nn, &def); err != nil || nn != 1 || def.Valid {
			t.Fatalf("%s owner definition: notnull=%d default=%v err=%v", table, nn, def, err)
		}
		for _, value := range []string{"NULL", "''"} {
			if _, err := tx.Exec("UPDATE " + table + " SET tenant_id=" + value); err == nil {
				t.Errorf("%s accepted owner %s", table, value)
			}
		}
	}
	for table, want := range indexes {
		if got := relationalSnapshot(t, tx, fmt.Sprintf(indexShape, "", table)); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s index semantics changed:\nwant %v\ngot  %v", table, want, got)
		}
		gotKeys := relationalSnapshot(t, tx, fmt.Sprintf(uniqueShape, "", table))
		for _, key := range uniqueKeys[table] {
			found := false
			for _, got := range gotKeys {
				found = found || got == key
			}
			if !found {
				t.Errorf("%s lost tenant-qualified unique key %s (got %v)", table, key, gotKeys)
			}
		}
	}
}

func TestTenantRelationalConstraints(t *testing.T) {
	db := relationalFixture(t)
	tx := relationalBegin(t, db)
	if err := stageTenantRelationalSchema(tx); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	} // Test owns commit; production component must not.
	relationalExec(t, db, "PRAGMA foreign_keys=ON")
	relationalExec(t, db, `INSERT INTO tenants(id,name,status,created_at) VALUES('B','B','active','now');
 INSERT INTO tenant_runner_keys(tenant_id,runner_id) SELECT 'B',runner_id FROM tenant_runner_keys WHERE tenant_id='local';
 INSERT INTO tenant_runner_keys(tenant_id,runner_id) VALUES('B','only-B');
 INSERT INTO tenant_client_keys(tenant_id,client_id) SELECT 'B',client_id FROM tenant_client_keys WHERE tenant_id='local';
 INSERT INTO tenant_client_keys(tenant_id,client_id) VALUES('B','only-B');`)
	// Copy all legacy values (including logical IDs) into B. Integer IDs are physical
	// SQLite row identities, retained globally; their logical IDs/keys may coincide.
	for _, table := range relationalTenantTables {
		rows, err := db.Query("PRAGMA table_info(" + table + ")")
		if err != nil {
			t.Fatal(err)
		}
		var columns, values []string
		for rows.Next() {
			var cid, nn, pk int
			var name, typ string
			var def any
			if err := rows.Scan(&cid, &name, &typ, &nn, &def, &pk); err != nil {
				t.Fatal(err)
			}
			columns = append(columns, name)
			value := name
			if name == "tenant_id" {
				value = "'B'"
			} else if typ == "INTEGER" && (name == "id" || name == "note_id" || name == "source_id" || name == "target_id" || name == "attachment_id") {
				value = name + "+1000"
			}
			values = append(values, value)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		_ = rows.Close()
		relationalExec(t, db, "INSERT INTO "+table+"("+strings.Join(columns, ",")+") SELECT "+strings.Join(values, ",")+" FROM "+table+" WHERE tenant_id='local'")
	}
	bad := []string{
		`INSERT INTO notes(path,short_id) VALUES('no-owner','x')`,
		`INSERT INTO notes(tenant_id,path,short_id) VALUES('','empty-owner','x')`,
		`INSERT INTO notes(tenant_id,path,short_id) VALUES('missing','no-registry','x')`,
		`INSERT INTO tags(tenant_id,note_id,tag) VALUES('B',10,'foreign')`,
		`UPDATE links SET target_id=11 WHERE tenant_id='B'`,
		`UPDATE links SET source_id=10 WHERE tenant_id='B'`,
		`UPDATE note_embeddings SET note_id=10 WHERE tenant_id='B'`,
		`UPDATE note_embeddings_meta SET note_id=10 WHERE tenant_id='B'`,
		`UPDATE entry_attachments SET attachment_id=70 WHERE tenant_id='B'`,
		`UPDATE entry_attachments SET note_id=10 WHERE tenant_id='B'`,
		`UPDATE attachment_derived SET attachment_id=70 WHERE tenant_id='B'`,
		`UPDATE task_claims SET runner_id='only-B' WHERE tenant_id='local'`,
		`UPDATE task_dispatch_leases SET assigned_runner_id='only-B' WHERE tenant_id='local'`,
		`UPDATE feature_assignments SET runner_id='only-B' WHERE tenant_id='local'`,
		`UPDATE opencode_instances SET runner_id='only-B' WHERE tenant_id='local'`,
		`UPDATE brain_client_workspaces SET client_id='only-B' WHERE tenant_id='local'`,
		`INSERT INTO entry_meta(tenant_id,path) VALUES('B','brain:system/install_claimed')`,
		`DELETE FROM operator_install_claim`,
		`UPDATE operator_install_claim SET path='changed'`,
		`DELETE FROM tenants WHERE id='local'`,
	}
	for _, stmt := range bad {
		if _, err := db.Exec(stmt); err == nil {
			t.Errorf("accepted forbidden write: %s", stmt)
		}
	}
	relationalExec(t, db, `INSERT INTO webhooks(tenant_id,id,name,url,events,created_at,updated_at) VALUES('B','only-B','x','x','[]','now','now')`)
	if _, err := db.Exec(`UPDATE webhook_deliveries SET webhook_id='only-B' WHERE tenant_id='local'`); err == nil {
		t.Fatal("cross-tenant delivery accepted")
	}
	// SET NULL must clear only the target ID, not the non-null tenant component.
	relationalExec(t, db, `DELETE FROM notes WHERE tenant_id='local' AND id=11`)
	var owner string
	var target sql.NullInt64
	if err := db.QueryRow("SELECT tenant_id,target_id FROM links WHERE id=20").Scan(&owner, &target); err != nil || owner != "local" || target.Valid {
		t.Fatalf("target delete owner=%s target=%v err=%v", owner, target, err)
	}
	// Deregistration must not erase durable pause/history; keys are NOT enrollment.
	relationalExec(t, db, `DELETE FROM runners WHERE tenant_id='local'; DELETE FROM brain_clients WHERE tenant_id='local'`)
	for _, table := range []string{"task_claims", "task_placement_reasons", "runner_pause_state", "brain_client_workspaces", "entry_meta", "generated_tasks"} {
		var n int
		if err := db.QueryRow("SELECT count(*) FROM " + table + " WHERE tenant_id='local'").Scan(&n); err != nil || n != 1 {
			t.Fatalf("lost durable %s: %d %v", table, n, err)
		}
	}
	var seq int
	if err := db.QueryRow("SELECT seq FROM sqlite_sequence WHERE name='notes'").Scan(&seq); err != nil || seq < 900 {
		t.Fatalf("sequence=%d err=%v", seq, err)
	}
	rows, err := db.Query("PRAGMA foreign_key_check")
	if err != nil {
		t.Fatal(err)
	}
	if rows.Next() {
		t.Fatal("foreign key violation")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	var integrity string
	if err := db.QueryRow("PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		t.Fatalf("integrity=%s err=%v", integrity, err)
	}
}

func TestTenantRelationalFailure(t *testing.T) {
	for _, kind := range []string{"foreign-key", "inventory", "trigger", "already-enabled"} {
		t.Run(kind, func(t *testing.T) {
			db := relationalFixture(t)
			if kind == "already-enabled" {
				tx, err := db.Begin()
				if err != nil {
					t.Fatal(err)
				}
				defer tx.Rollback()
				if err := stageTenantRelationalSchema(tx); err == nil {
					t.Fatal("accepted unsafe FK rebuild mode")
				}
				return
			}
			relationalExec(t, db, "PRAGMA foreign_keys=OFF")
			if kind == "foreign-key" {
				relationalExec(t, db, "UPDATE tags SET note_id=123456")
			}
			if kind == "inventory" {
				relationalExec(t, db, "CREATE TABLE surprise(secret TEXT)")
			}
			if kind == "trigger" {
				relationalExec(t, db, "CREATE TRIGGER surprise AFTER INSERT ON notes BEGIN SELECT 1; END")
			}
			before := relationalSnapshot(t, db, "sqlite_schema")
			tx := relationalBegin(t, db)
			if err := stageTenantRelationalSchema(tx); err == nil {
				t.Fatal("accepted invalid baseline")
			}
			if got := relationalSnapshot(t, tx, "sqlite_schema"); !reflect.DeepEqual(got, before) {
				t.Fatal("failed component left partial schema")
			}
			if err := tx.Rollback(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

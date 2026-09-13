package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
)

func TestTenantFTSBackfill(t *testing.T) {
	db := relationalFixture(t)
	// Current content, not a stale legacy posting, is the source of truth.
	relationalExec(t, db, `UPDATE notes SET title='current running',body='running body' WHERE id=10`)
	tx := relationalBegin(t, db)
	if err := stageTenantRelationalSchema(tx); err != nil {
		t.Fatal(err)
	}
	relationalExec(t, tx, `INSERT INTO tenants(id,name,status,created_at) VALUES('b','b','active','now');
 INSERT INTO notes(tenant_id,id,path,short_id,title,body) VALUES('b',1010,'projects/p/task/same.md','same','foreign running','running running')`)
	if err := stageTenantFTS(tx); err != nil {
		t.Fatalf("stage tenant FTS: %v", err)
	}
	var id string
	if err := tx.QueryRow(`SELECT internal_id FROM tenant_fts WHERE tenant_id='local'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	var rowid int
	var title string
	if err := tx.QueryRow(`SELECT rowid,title FROM "fts_t_`+id+`" WHERE "fts_t_`+id+`" MATCH 'run'`).Scan(&rowid, &title); err != nil {
		t.Fatal(err)
	}
	if rowid != 10 || title != "current running" {
		t.Fatalf("rowid/title = %d/%q", rowid, title)
	}
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM sqlite_schema WHERE name LIKE 'notes_fts%'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("legacy FTS retained: %d %v", n, err)
	}
	if err := stageTenantFTS(tx); err != nil {
		t.Fatalf("repeat validation: %v", err)
	}
	if err := tx.QueryRow(`SELECT max(version) FROM schema_version`).Scan(&n); err != nil || n != 28 || CurrentSchemaVersion != 30 {
		t.Fatalf("version advanced: %d %v", n, err)
	}
}

func TestTenantFTSNegativeRowIDAllocation(t *testing.T) {
	for _, recursive := range []string{"OFF", "ON"} {
		t.Run(recursive, func(t *testing.T) {
			tx := relationalBegin(t, relationalFixture(t))
			if err := stageTenantRelationalSchema(tx); err != nil {
				t.Fatal(err)
			}
			relationalExec(t, tx, `INSERT INTO tenants(id,name,status,created_at) VALUES('b','b','active','now');
 INSERT INTO notes(tenant_id,id,path,short_id,title) VALUES
 ('b',-1,'negative','negative','foreignnegative'),('b',-2,'negative-two','two','foreignnegative'),
 ('local',-3,'local-negative','three','localnegative')`)
			if err := stageTenantFTS(tx); err != nil {
				t.Fatal(err)
			}
			relationalExec(t, tx, "PRAGMA recursive_triggers="+recursive)
			// This valid finalized catalog includes legacy negative identities.
			if err := checkFinalTenantSearchSchema(tx); err != nil {
				t.Fatal(err)
			}
			foreign := relationalSnapshot(t, tx, "(SELECT * FROM notes WHERE tenant_id='b')")
			for _, stmt := range []string{
				`INSERT INTO notes(tenant_id,path,short_id,title) VALUES('local','auto','auto','allocated')`,
				`INSERT INTO notes(tenant_id,id,path,short_id) VALUES('local',NULL,'null-auto','null')`,
				`INSERT INTO notes(tenant_id,path,short_id) VALUES('local','multi-a','a'),('local','multi-b','b')`,
				`INSERT OR REPLACE INTO notes(tenant_id,path,short_id) VALUES('local','auto','auto')`,
				`INSERT INTO notes(tenant_id,path,short_id,title) VALUES('local','auto','auto','upserted') ON CONFLICT(tenant_id,path) DO UPDATE SET title=excluded.title`,
				`INSERT INTO notes(tenant_id,path,short_id) VALUES('local','auto','auto') ON CONFLICT DO NOTHING`,
				`INSERT OR REPLACE INTO notes(tenant_id,id,path,short_id) VALUES('local',-3,'local-negative','three')`,
			} {
				if _, err := tx.Exec(stmt); err != nil {
					t.Errorf("valid insert blocked: %s: %v", stmt, err)
				}
			}
			// A skipped insert must not poison the next row's ownership decision.
			relationalExec(t, tx, `INSERT INTO notes(tenant_id,id,path,short_id) VALUES('local',-1,'ignored','i') ON CONFLICT DO NOTHING`)
			relationalExec(t, tx, `INSERT OR REPLACE INTO notes SELECT * FROM notes WHERE id=-1`)
			for _, id := range []int{-1, -2} {
				for _, stmt := range []string{
					`INSERT OR REPLACE INTO notes(tenant_id,id,path,short_id) VALUES('local',%d,'stolen','s')`,
					`UPDATE OR REPLACE notes SET id=%d WHERE id=-3`,
					`INSERT OR REPLACE INTO notes(tenant_id,id,path,short_id) VALUES('local',NULL,'rollback-probe','r'),('local',%d,'stolen','s')`,
				} {
					before := relationalSnapshot(t, tx, "notes")
					if _, err := tx.Exec(fmt.Sprintf(stmt, id)); err == nil {
						t.Errorf("foreign replacement accepted: %s", fmt.Sprintf(stmt, id))
					}
					if !reflect.DeepEqual(before, relationalSnapshot(t, tx, "notes")) {
						t.Fatal("rejected statement did not roll back all rows")
					}
					if err := checkFinalTenantSearchSchema(tx); err != nil {
						t.Fatalf("rejected replacement changed search state: %v", err)
					}
				}
			}
			if !reflect.DeepEqual(foreign, relationalSnapshot(t, tx, "(SELECT * FROM notes WHERE tenant_id='b')")) {
				t.Fatal("foreign negative IDs changed")
			}
			if err := checkFinalTenantSearchSchema(tx); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func tenantFTSFixture(t *testing.T) *sql.Tx {
	t.Helper()
	tx := relationalBegin(t, relationalFixture(t))
	if err := stageTenantRelationalSchema(tx); err != nil {
		t.Fatal(err)
	}
	relationalExec(t, tx, `INSERT INTO tenants(id,name,status,created_at) VALUES('b','b','active','now');
 UPDATE notes SET title='current running',body='running body' WHERE id=10;
 INSERT INTO notes(tenant_id,id,path,short_id,title,body) VALUES
 ('local',12,'other','other','ordinary','running fast'),
 ('b',1010,'projects/p/task/same.md','same','foreign running','running running')`)
	if err := stageTenantFTS(tx); err != nil {
		t.Fatal(err)
	}
	return tx
}

func TestTenantFTSQueryIsolation(t *testing.T) {
	tx := tenantFTSFixture(t)
	ctx := context.Background()
	baseline, count, err := queryTenantFTS(ctx, tx, tenant.Local, "run", 100)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 || len(baseline) != 2 || baseline[0].ID != 10 || baseline[0].Title != "current running" || baseline[0].Snippet != "<b>running</b> body" || baseline[0].Score >= 0 {
		t.Fatalf("unexpected baseline: %#v count=%d", baseline, count)
	}
	for _, stmt := range []string{
		`WITH RECURSIVE c(x) AS (VALUES(1) UNION ALL SELECT x+1 FROM c WHERE x<100) INSERT INTO notes(tenant_id,path,short_id,title,body) SELECT 'b','b-'||x,'x','running',replace(hex(zeroblob(4000)),'0','running ') FROM c`,
		`UPDATE notes SET body='other other other',title='absent',path=path||'-changed' WHERE tenant_id='b'`,
		`DELETE FROM notes WHERE tenant_id='b'`,
	} {
		relationalExec(t, tx, stmt)
		got, n, err := queryTenantFTS(ctx, tx, tenant.Local, "run", 100)
		if err != nil || n != count || !reflect.DeepEqual(got, baseline) {
			t.Fatalf("B changed A scores/order/snippets/count: %#v/%d %v", got, n, err)
		}
	}
	got, n, err := queryTenantFTS(ctx, tx, tenant.Local, "run", 1)
	if err != nil || n != 2 || len(got) != 1 || got[0] != baseline[0] {
		t.Fatalf("limit/count: %v %d %v", got, n, err)
	}
	for _, owner := range []tenant.ID{{}, tenant.MustParse("missing")} {
		if _, _, err := queryTenantFTS(ctx, tx, owner, "run", 10); !errors.Is(err, ErrTenantSearchUnavailable) {
			t.Fatalf("missing owner did not fail closed: %v", err)
		}
	}
}

func TestTenantFTSMaintenance(t *testing.T) {
	tx := tenantFTSFixture(t)
	relationalExec(t, tx, `INSERT INTO notes(tenant_id,id,path,short_id,title,body) VALUES('local',99,'inserted','inserted','needle','needle')`)
	check := func(id int64, title string, count int) {
		t.Helper()
		got, n, err := queryTenantFTS(context.Background(), tx, tenant.Local, "needle", 10)
		if err != nil || n != count || len(got) != count {
			t.Fatalf("query: %v %d %v", got, n, err)
		}
		if count > 0 && (got[0].ID != id || got[0].Title != title) {
			t.Fatalf("stale result: %v", got)
		}
		if err := checkFinalTenantSearchSchema(tx); err != nil {
			t.Fatal(err)
		}
	}
	check(99, "needle", 1)
	relationalExec(t, tx, `UPDATE notes SET rowid=199,path='moved',title='needle updated',body='updated needle' WHERE id=99`)
	check(199, "needle updated", 1)
	if _, err := tx.Exec(`UPDATE notes SET tenant_id='b' WHERE id=199`); err == nil {
		t.Fatal("owner change accepted")
	}
	relationalExec(t, tx, `DELETE FROM notes WHERE id=199`)
	check(0, "", 0)
	for _, stmt := range []string{`UPDATE tenant_fts SET internal_id=internal_id`, `DELETE FROM tenant_fts`, `INSERT OR REPLACE INTO tenant_fts SELECT * FROM tenant_fts`} {
		if _, err := tx.Exec(stmt); err == nil {
			t.Fatalf("mapping write accepted: %s", stmt)
		}
	}
	relationalExec(t, tx, `INSERT INTO tenants(id,name,status,created_at) VALUES('unprovisioned','u','active','now')`)
	if _, err := tx.Exec(`INSERT INTO notes(tenant_id,path,short_id) VALUES('unprovisioned','x','x')`); err == nil {
		t.Fatal("unmapped write accepted")
	}
}

func TestTenantFTSRejectsCatalogDamage(t *testing.T) {
	for _, damage := range []string{
		`DROP TABLE tenant_fts_insert_guard`,
		`ALTER TABLE tenant_fts_insert_guard ADD COLUMN unreviewed TEXT`,
		`DROP TRIGGER p4_fts_rowid_insert_check`,
		`CREATE INDEX rogue_fts ON %s_content(c0)`,
		`DROP TABLE %s`,
		`DROP TABLE %s; CREATE TABLE %s(title,body,path)`,
		`DROP TABLE %s; CREATE VIRTUAL TABLE %s USING fts5(title,body,path,content=notes,content_rowid=id,tokenize='porter unicode61')`,
		`DROP TRIGGER p4_fts_owner`,
		`CREATE VIEW rogue AS SELECT * FROM notes`,
		`CREATE TABLE rogue(secret TEXT)`,
		`DROP TRIGGER p4_fts_map_update; PRAGMA ignore_check_constraints=ON; UPDATE tenant_fts SET internal_id='ABCDEF0123456789ABCDEF0123456789' WHERE tenant_id='local'`,
		`DROP TRIGGER p4_fts_map_delete; DELETE FROM tenant_fts WHERE tenant_id='local'`,
		`DROP INDEX p4_owner_notes`,
	} {
		t.Run(damage, func(t *testing.T) {
			tx := tenantFTSFixture(t)
			var id string
			if err := tx.QueryRow(`SELECT internal_id FROM tenant_fts WHERE tenant_id='local'`).Scan(&id); err != nil {
				t.Fatal(err)
			}
			stmt := strings.ReplaceAll(damage, "%s", "fts_t_"+id)
			relationalExec(t, tx, stmt)
			before := relationalSnapshot(t, tx, "sqlite_schema")
			if err := stageTenantFTS(tx); err == nil {
				t.Error("accepted damaged final catalog")
			}
			if !reflect.DeepEqual(before, relationalSnapshot(t, tx, "sqlite_schema")) {
				t.Fatal("failed repeat mutated catalog")
			}
			if _, _, err := queryTenantFTS(context.Background(), tx, tenant.Local, "run", 10); !errors.Is(err, ErrTenantSearchUnavailable) {
				t.Fatalf("query did not fail unavailable: %v", err)
			}
		})
	}
}

func TestTenantFTSRejectsSourceDamage(t *testing.T) {
	for _, damage := range []string{
		`CREATE INDEX rogue_fts ON notes_fts_docsize(sz)`,
		`DROP TABLE notes_fts; CREATE VIRTUAL TABLE notes_fts USING fts5(title,body,path)`,
		`CREATE TABLE fts_t_0123456789abcdef0123456789abcdef(secret TEXT)`,
		`DROP TRIGGER notes_au; CREATE TRIGGER notes_au AFTER UPDATE ON notes BEGIN SELECT 1; END`,
	} {
		t.Run(damage, func(t *testing.T) {
			tx := relationalBegin(t, relationalFixture(t))
			if err := stageTenantRelationalSchema(tx); err != nil {
				t.Fatal(err)
			}
			relationalExec(t, tx, damage)
			before := relationalSnapshot(t, tx, "sqlite_schema")
			if err := stageTenantFTS(tx); err == nil {
				t.Fatal("accepted damaged source catalog")
			}
			if !reflect.DeepEqual(before, relationalSnapshot(t, tx, "sqlite_schema")) {
				t.Fatal("source failure mutated catalog")
			}
		})
	}
}

func TestTenantFTSIdentifierValidation(t *testing.T) {
	for _, id := range []string{"", strings.Repeat("a", 31), strings.Repeat("a", 33), strings.Repeat("A", 32), strings.Repeat("g", 32), `x"; DROP TABLE notes;--`, strings.Repeat("a", 32) + "\n"} {
		if name, err := tenantFTSName(id); err == nil || name != "" {
			t.Errorf("accepted %q: %s", id, name)
		}
	}
	id := "0123456789abcdef0123456789abcdef"
	if name, err := tenantFTSName(id); err != nil || name != fmt.Sprintf(`"fts_t_%s"`, id) {
		t.Fatalf("valid ID: %s %v", name, err)
	}
}

func TestTenantFTSConflictMaintenance(t *testing.T) {
	tx := tenantFTSFixture(t)
	// SQLite does NOT fire REPLACE's implicit deletes with recursive_triggers OFF.
	relationalExec(t, tx, `PRAGMA recursive_triggers=OFF;
 INSERT INTO notes(tenant_id,id,path,short_id,title) VALUES('local',99,'conflict','c','obsolete');
 INSERT OR REPLACE INTO notes(tenant_id,id,path,short_id,title) VALUES('local',199,'conflict','c','replacement')`)
	if err := checkFinalTenantSearchSchema(tx); err != nil {
		t.Fatalf("REPLACE left stale postings: %v", err)
	}
	for _, stmt := range []string{
		`INSERT OR REPLACE INTO notes(tenant_id,id,path,short_id,title) VALUES('local',1010,'steal','s','foreign rowid')`,
		`UPDATE OR REPLACE notes SET id=1010 WHERE id=199`,
	} {
		if _, err := tx.Exec(stmt); err == nil {
			t.Errorf("cross-owner rowid replacement accepted: %s", stmt)
		}
	}
	// UPSERT's update and do-nothing paths must remain usable.
	relationalExec(t, tx, `INSERT INTO notes(tenant_id,path,short_id,title) VALUES('local','conflict','c','upserted') ON CONFLICT(tenant_id,path) DO UPDATE SET title=excluded.title;
 INSERT INTO notes(tenant_id,path,short_id,title) VALUES('local','conflict','c','ignored') ON CONFLICT DO NOTHING;
 INSERT INTO notes(tenant_id,id,path,short_id,title) VALUES('local',299,'second-conflict','s','deleted by update');
 UPDATE OR REPLACE notes SET path='second-conflict' WHERE id=199`)
	if err := checkFinalTenantSearchSchema(tx); err != nil {
		t.Fatal(err)
	}
	got, n, err := queryTenantFTS(context.Background(), tx, tenant.Local, "upserted", 10)
	if err != nil || n != 1 || len(got) != 1 || got[0].ID != 199 {
		t.Fatalf("upsert: %v %d %v", got, n, err)
	}
}

func TestTenantFTSMissingIndexAndMappingDenyWrites(t *testing.T) {
	for _, kind := range []string{"index", "mapping"} {
		t.Run(kind, func(t *testing.T) {
			tx := tenantFTSFixture(t)
			var id string
			if err := tx.QueryRow(`SELECT internal_id FROM tenant_fts WHERE tenant_id='local'`).Scan(&id); err != nil {
				t.Fatal(err)
			}
			if kind == "index" {
				relationalExec(t, tx, `DROP TABLE "fts_t_`+id+`"`)
			} else {
				var guard string
				if err := tx.QueryRow(`SELECT sql FROM sqlite_schema WHERE name='p4_fts_map_delete'`).Scan(&guard); err != nil {
					t.Fatal(err)
				}
				relationalExec(t, tx, `DROP TRIGGER p4_fts_map_delete; DELETE FROM tenant_fts WHERE tenant_id='local'`)
				relationalExec(t, tx, guard)
			}
			for _, stmt := range []string{
				`INSERT INTO notes(tenant_id,path,short_id) VALUES('local','missing','m')`,
				`UPDATE notes SET title='lost' WHERE id=10`,
				`DELETE FROM notes WHERE id=10`,
			} {
				if _, err := tx.Exec(stmt); err == nil {
					t.Errorf("accepted %s", stmt)
				}
			}
			if _, _, err := queryTenantFTS(context.Background(), tx, tenant.Local, "run", 10); !errors.Is(err, ErrTenantSearchUnavailable) {
				t.Fatalf("not unavailable: %v", err)
			}
		})
	}
}

func TestTenantFTSLegacyWeightsAndRollback(t *testing.T) {
	db := relationalFixture(t)
	relationalExec(t, db, `UPDATE notes SET title='running',body='plain' WHERE id=10;
 INSERT INTO notes(id,path,short_id,title,body) VALUES(12,'running','path','plain','plain'),(13,'body','body','plain','running')`)
	// Compare exact legacy weighted ranking before adding any foreign corpus.
	rows, err := db.Query(`SELECT rowid,bm25(notes_fts,10.0,1.0,5.0) FROM notes_fts WHERE notes_fts MATCH 'run' ORDER BY bm25(notes_fts,10.0,1.0,5.0),rowid`)
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	var scores []float64
	for rows.Next() {
		var id int64
		var score float64
		if err := rows.Scan(&id, &score); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		scores = append(scores, score)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	if !reflect.DeepEqual(ids, []int64{10, 12, 13}) {
		t.Fatalf("weighted fixture did not distinguish fields: %v", ids)
	}
	before := relationalSnapshot(t, db, "sqlite_schema")
	tx := relationalBegin(t, db)
	if err := stageTenantRelationalSchema(tx); err != nil {
		t.Fatal(err)
	}
	if err := stageTenantFTS(tx); err != nil {
		t.Fatal(err)
	}
	if _, err := auditRelationalInventory(tx); err == nil {
		t.Fatal("legacy relational source audit accepted final FTS catalog")
	}
	got, n, err := queryTenantFTS(context.Background(), tx, tenant.Local, "run", 100)
	if err != nil || n != len(ids) || len(got) != len(ids) {
		t.Fatalf("result: %v %d %v", got, n, err)
	}
	for i, hit := range got {
		if hit.ID != ids[i] || hit.Score != scores[i] {
			t.Fatalf("weight/tokenizer changed at %d: %v want %d/%v", i, hit, ids[i], scores[i])
		}
	}
	var seq int
	if err := tx.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name='notes'`).Scan(&seq); err != nil || seq != 900 {
		t.Fatalf("sequence: %d %v", seq, err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, relationalSnapshot(t, db, "sqlite_schema")) {
		t.Fatal("outer rollback changed source catalog")
	}
}

func TestTenantFTSStaleLegacyAndContentValidation(t *testing.T) {
	db := relationalFixture(t)
	// Mimic stale legacy postings while keeping the reviewed trigger catalog.
	relationalExec(t, db, `DROP TRIGGER notes_au; UPDATE notes SET title='freshword',body='freshword' WHERE id=10`)
	relationalExec(t, db, createTriggerAfterUpdate)
	tx := relationalBegin(t, db)
	if err := stageTenantRelationalSchema(tx); err != nil {
		t.Fatal(err)
	}
	if err := stageTenantFTS(tx); err != nil {
		t.Fatal(err)
	}
	got, n, err := queryTenantFTS(context.Background(), tx, tenant.Local, "freshword", 10)
	if err != nil || n != 1 || len(got) != 1 || got[0].Title != "freshword" {
		t.Fatalf("stale backfill: %v %d %v", got, n, err)
	}
	var id string
	if err := tx.QueryRow(`SELECT internal_id FROM tenant_fts WHERE tenant_id='local'`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	relationalExec(t, tx, `INSERT INTO "fts_t_`+id+`"(rowid,title,body,path) VALUES(98765,'foreign','foreign','foreign')`)
	if err := checkFinalTenantSearchSchema(tx); err == nil {
		t.Fatal("accepted foreign content row")
	}
}

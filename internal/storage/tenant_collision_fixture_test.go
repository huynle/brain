package storage

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// collisionFixture extends, rather than replaces, the migration fixture. Both
// views were bound on 28; all legacy rows survive. SQL seeding and the complete
// SQL oracle are independent of the workload methods under test. Attachment
// digests/sizes represent the identical "real local CAS bytes" in that fixture;
// this is a storage test, not a claim of CAS authorization or provisioning.
type collisionFixture struct {
	owner *StorageLayer
	a, b  *TenantStore
}

// Explicit dependency order and integer remapping: logical registry IDs, paths,
// titles, projects, tags, hashes and vectors collide; physical IDs must not.
var collisionTables = strings.Fields(`tenant_runner_keys tenant_client_keys notes attachments
runners brain_clients links tags entry_meta generated_tasks event_log task_claims
task_dispatch_leases task_placement_reasons feature_assignments opencode_instances
project_pause_state feature_pause_state runner_pause_state feature_cascade_roots
project_placement brain_client_workspaces webhooks webhook_deliveries
note_embeddings note_embeddings_meta entry_attachments attachment_derived`)

var collisionIntegerColumns = map[string]string{
	"notes": "id", "attachments": "id", "links": "id source_id target_id",
	"tags": "id note_id", "event_log": "id", "task_placement_reasons": "id",
	"brain_client_workspaces": "id", "note_embeddings": "note_id",
	"note_embeddings_meta": "note_id", "entry_attachments": "id note_id attachment_id",
	"attachment_derived": "id attachment_id",
}

func newCollisionFixture(t *testing.T) *collisionFixture {
	t.Helper()
	o, a, b := migratedNoteStores(t)
	// Turn the legacy corpus into valid runtime inputs without dropping its rows.
	relationalExec(t, o.db, `UPDATE notes SET project_id='p',type='task',status='pending',body='collision corpus';
UPDATE note_embeddings SET embedding=x'0000803f00000000';
UPDATE opencode_instances SET project_id='p';
UPDATE webhooks SET created_at='2026-01-01T00:00:00Z',updated_at='2026-01-01T00:00:00Z';
UPDATE webhook_deliveries SET created_at='2026-01-01T00:00:00Z'`)
	for _, table := range collisionTables {
		rows, err := o.db.Query("SELECT name FROM pragma_table_info(?) ORDER BY cid", table)
		if err != nil {
			t.Fatal(err)
		}
		var columns, values []string
		for rows.Next() {
			var col string
			if err := rows.Scan(&col); err != nil {
				t.Fatal(err)
			}
			columns = append(columns, `"`+col+`"`)
			value := `"` + col + `"`
			if col == "tenant_id" {
				value = "'acme'"
			}
			for _, integer := range strings.Fields(collisionIntegerColumns[table]) {
				if col == integer {
					value += "+10000"
				}
			}
			values = append(values, value)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		relationalExec(t, o.db, "INSERT INTO "+table+"("+strings.Join(columns, ",")+") SELECT "+strings.Join(values, ",")+" FROM "+table+" WHERE tenant_id='local'")
	}
	f := &collisionFixture{o, a, b}
	for _, id := range []string{"local", "acme"} {
		for _, table := range collisionTables {
			if len(relationalSnapshot(t, o.db, "(SELECT * FROM "+table+" WHERE tenant_id='"+id+"')")) == 0 {
				t.Fatalf("empty fixture %s/%s", id, table)
			}
		}
	}
	if rows := relationalSnapshot(t, o.db, "pragma_foreign_key_check"); len(rows) != 0 {
		t.Fatalf("fixture foreign keys: %v", rows)
	}
	return f
}

// All columns and rows, not selected fields read back through potentially faulty
// workload predicates. Include durable keys, mapping and every mapped FTS shadow.
func (f *collisionFixture) snapshot(t *testing.T, id string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, table := range append(append([]string{}, collisionTables...), "tenant_fts") {
		out[table] = relationalSnapshot(t, f.owner.db, "(SELECT * FROM "+table+" WHERE tenant_id='"+id+"')")
	}
	var internal string
	if err := f.owner.db.QueryRow("SELECT internal_id FROM tenant_fts WHERE tenant_id=?", id).Scan(&internal); err != nil {
		t.Fatal(err)
	}
	name := "fts_t_" + internal // from trusted test DB, not the production name resolver
	for _, suffix := range []string{"", "_data", "_idx", "_content", "_docsize", "_config"} {
		projection := name + suffix
		if suffix == "" {
			projection = "(SELECT rowid,* FROM " + name + ")"
		}
		out["fts"+suffix] = relationalSnapshot(t, f.owner.db, projection)
	}
	return out
}

func (f *collisionFixture) unchanged(t *testing.T, id string, before map[string][]string) {
	t.Helper()
	after := f.snapshot(t, id)
	for table, want := range before {
		if !reflect.DeepEqual(want, after[table]) {
			t.Errorf("%s/%s changed:\nwant %v\ngot  %v", id, table, want, after[table])
		}
	}
}

func collisionMust(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestTenantCollisionFixtureInventory(t *testing.T) {
	f := newCollisionFixture(t)
	want := append(append([]string{}, relationalTenantTables...), "tenant_runner_keys", "tenant_client_keys")
	if len(want) != 28 || len(collisionTables) != 28 {
		t.Fatal("workload table inventory changed")
	}
	for _, table := range want {
		if !strings.Contains(" "+strings.Join(collisionTables, " ")+" ", " "+table+" ") {
			t.Errorf("unseeded table %s", table)
		}
	}
	// Independently discover ownership columns in the staged catalog, so a new
	// scoped table cannot silently evade the fixture by omitting both inventories.
	rows, err := f.owner.db.Query(`SELECT s.name FROM sqlite_schema s WHERE s.type='table'
AND EXISTS(SELECT 1 FROM pragma_table_info(s.name) p WHERE p.name='tenant_id')`)
	collisionMust(t, err)
	for rows.Next() {
		var table string
		collisionMust(t, rows.Scan(&table))
		switch table {
		case "tenant_roots", "tenant_fts", "tenant_fts_insert_guard": // control, mapping, trusted trigger scratch
			continue
		}
		if !strings.Contains(" "+strings.Join(collisionTables, " ")+" ", " "+table+" ") {
			t.Errorf("unclassified tenant table %s", table)
		}
	}
	collisionMust(t, rows.Err())
	collisionMust(t, rows.Close())
	// Exact normalized row equality proves every seeded value collides, not just
	// row counts. Physical IDs are inverted only for this fixture assertion.
	for _, table := range collisionTables {
		rows, err := f.owner.db.Query("SELECT name FROM pragma_table_info(?) ORDER BY cid", table)
		collisionMust(t, err)
		var cols []string
		for rows.Next() {
			var col string
			collisionMust(t, rows.Scan(&col))
			if col == "tenant_id" {
				continue
			}
			expr := `"` + col + `"`
			for _, integer := range strings.Fields(collisionIntegerColumns[table]) {
				if integer == col {
					expr += "-10000"
				}
			}
			cols = append(cols, expr)
		}
		collisionMust(t, rows.Err())
		collisionMust(t, rows.Close())
		foreign := relationalSnapshot(t, f.owner.db, "(SELECT "+strings.Join(cols, ",")+" FROM "+table+" WHERE tenant_id='acme')")
		local := relationalSnapshot(t, f.owner.db, "(SELECT "+strings.ReplaceAll(strings.Join(cols, ","), "-10000", "")+" FROM "+table+" WHERE tenant_id='local')")
		if !reflect.DeepEqual(local, foreign) {
			t.Errorf("noncolliding %s: %v / %v", table, local, foreign)
		}
	}
	for _, s := range []*TenantStore{f.a, f.b} {
		n, err := s.GetNoteByPath(context.Background(), "projects/p/task/same.md")
		collisionMust(t, err)
		wantID := int64(10)
		if s == f.b {
			wantID += 10000
		}
		if n == nil || n.ID != wantID {
			t.Fatalf("physical note identity: %+v", n)
		}
	}
	t.Logf("%d populated colliding relational/key tables plus tenant FTS", len(collisionTables))
}

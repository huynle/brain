package storage

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"sort"
	"sync"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/tenantfs"
	"github.com/huynle/brain-api/internal/types"
)

type syncAPI interface {
	ReadEntryChanges(context.Context, string, int64, int) (*EntrySyncPage, error)
	ReadSelectedEntries(context.Context, []string) (*EntrySyncPage, error)
	ReserveSyncOperation(context.Context, string, string) (*SyncReceipt, error)
	CompleteSyncOperation(context.Context, string, int, string) error
	SyncDevices(context.Context) ([]types.SyncDevice, error)
	SaveSyncDevice(context.Context, *types.SyncDevice, types.SyncDevice) error
	SyncNote(context.Context, string) (*NoteRow, error)
}

// Full old private29 (retained history) -> successor fixture. No staging catalog
// admission: each owner has real roots and its own fully maintained FTS mapping.
func successorPrivateFixture(t *testing.T) *sql.DB {
	t.Helper()
	db, _ := completeMigrationFixture(t)
	collisionMust(t, migrateTenantSchema(context.Background(), db, nil))
	resolver, err := tenantfs.New(registryHandle(t, &StorageLayer{db: db}), t.TempDir())
	collisionMust(t, err)
	for _, id := range []string{"a", "b"} {
		_, err = resolver.Provision(context.Background(), tenant.MustParse(id), tenantfs.Overrides{BrainRoot: t.TempDir(), BlobRoot: t.TempDir()})
		collisionMust(t, err)
	}
	tx, err := db.Begin()
	collisionMust(t, err)
	relationalExec(t, tx, `INSERT INTO tenants VALUES('a','A','active','now',NULL),('b','B','active','now',NULL); DROP TRIGGER p4_fts_map_insert`)
	for i, id := range []string{"a", "b"} {
		internal := fmt.Sprintf("%032x", i+1)
		name, _ := tenantFTSName(internal)
		relationalExec(t, tx, tenantFTSTableDDL(name))
		_, err = tx.Exec("INSERT INTO tenant_fts VALUES(?,?)", id, internal)
		collisionMust(t, err)
	}
	mapping, err := readTenantFTSMapping(tx)
	collisionMust(t, err)
	for name, ddl := range tenantFTSTriggers(mapping) {
		relationalExec(t, tx, "DROP TRIGGER IF EXISTS "+name)
		relationalExec(t, tx, ddl)
	}
	relationalExec(t, tx, `INSERT INTO notes(tenant_id,id,path,short_id,title,body) VALUES('a',100,'same','same0001','A','alpha'),('b',200,'same','same0001','B','beta')`)
	collisionMust(t, tx.Commit())
	before := map[string][]string{}
	for _, s := range relationalTables {
		before[s.name] = relationalSnapshot(t, db, s.name)
	}
	collisionMust(t, migrateSuccessorSchema(context.Background(), db, nil))
	for table, want := range before {
		if !reflect.DeepEqual(want, relationalSnapshot(t, db, table)) {
			t.Fatalf("private owners or bytes changed: %s", table)
		}
	}
	return db
}

func syncReceiver(t *testing.T, db *sql.DB, id string) syncAPI {
	t.Helper()
	owner := tenant.Local
	if id != "local" {
		owner = tenant.MustParse(id)
	}
	s := &TenantStore{db: db, tenantID: owner}
	api, ok := any(s).(syncAPI)
	if !ok {
		t.Fatal("missing tenant sync receivers")
	}
	return api
}

func syncSnapshot(t *testing.T, db *sql.DB, id string) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, table := range append(append([]string{}, successorWorkloadTables()...), "notes", "tenant_fts") {
		out[table] = relationalSnapshot(t, db, "(SELECT * FROM "+table+" WHERE tenant_id='"+id+"')")
	}
	var internal string
	collisionMust(t, db.QueryRow("SELECT internal_id FROM tenant_fts WHERE tenant_id=?", id).Scan(&internal))
	for _, suffix := range []string{"_data", "_idx", "_content", "_docsize", "_config"} {
		out[suffix] = relationalSnapshot(t, db, "fts_t_"+internal+suffix)
	}
	return out
}

func TestSuccessorReceiverRouting(t *testing.T) {
	db := successorPrivateFixture(t)
	s := &TenantStore{db: db, tenantID: tenant.MustParse("a")}
	ctx := tenant.Into(context.Background(), s.tenantID)
	n, err := s.GetNoteByPath(ctx, "same")
	if err != nil || n == nil || n.Title != "A" {
		t.Fatalf("content routing: %+v %v", n, err)
	}
	if _, err = s.executionScope(ctx, "bulk"); err != nil {
		t.Fatalf("ledger routing: %v", err)
	}
	tx, err := db.Begin()
	collisionMust(t, err)
	defer tx.Rollback()
	table, err := installClaimTable(ctx, tx)
	if err != nil || table != "operator_install_claim" {
		t.Fatalf("claim routing %s: %v", table, err)
	}
}

func TestTenantSyncIsolation(t *testing.T) {
	db := successorPrivateFixture(t)
	a, b := syncReceiver(t, db, "a"), syncReceiver(t, db, "b")
	ca, cb := tenant.Into(context.Background(), tenant.MustParse("a")), tenant.Into(context.Background(), tenant.MustParse("b"))
	for _, p := range []struct {
		s     syncAPI
		ctx   context.Context
		label string
	}{{a, ca, "A"}, {b, cb, "B"}} {
		n, e := p.s.SyncNote(p.ctx, "same")
		if e != nil || n == nil || n.Title != p.label {
			t.Fatal(n, e)
		}
		page, e := p.s.ReadSelectedEntries(p.ctx, nil)
		if e != nil || len(page.Rows) != 0 || page.Epoch == "" {
			t.Fatal(page, e)
		}
		page, e = p.s.ReadEntryChanges(p.ctx, "", 0, 1)
		if e != nil || len(page.Rows) != 1 || page.Rows[0].Note.Title != p.label {
			t.Fatal(page, e)
		}
		if _, e = p.s.ReadEntryChanges(p.ctx, "wrong", page.Cursor, 1); e == nil {
			t.Fatal("foreign epoch accepted")
		}
		if _, e = p.s.ReadEntryChanges(p.ctx, page.Epoch, 100000, 1); e == nil {
			t.Fatal("future cursor accepted")
		}
		r, e := p.s.ReserveSyncOperation(p.ctx, "same", "hash")
		if e != nil || r != nil {
			t.Fatal(r, e)
		}
		r, e = p.s.ReserveSyncOperation(p.ctx, "same", "hash")
		if e != nil || r == nil || r.Status != 0 {
			t.Fatal("reservation replay", r, e)
		}
		collisionMust(t, p.s.SaveSyncDevice(p.ctx, nil, types.SyncDevice{ID: "same", Owner: p.label, Pending: []types.SyncPending{{ID: "draft", Raw: p.label}}}))
	}
	foreign := syncSnapshot(t, db, "b")
	collisionMust(t, a.CompleteSyncOperation(ca, "same", 409, " { \"conflict\": true } "))
	r, e := a.ReserveSyncOperation(ca, "same", "hash")
	if e != nil || r.Status != 409 || r.Body != " { \"conflict\": true } " {
		t.Fatal(r, e)
	}
	if _, e = a.ReserveSyncOperation(ca, "same", "different"); e == nil {
		t.Fatal("hash reuse accepted")
	}
	devices, e := a.SyncDevices(ca)
	if e != nil || len(devices) != 1 || devices[0].Owner != "A" {
		t.Fatal(devices, e)
	}
	old := devices[0]
	next := old
	next.Connection = "online"
	collisionMust(t, a.SaveSyncDevice(ca, &old, next))
	if e = a.SaveSyncDevice(ca, &old, next); e == nil {
		t.Fatal("stale device CAS accepted")
	}
	if !reflect.DeepEqual(foreign, syncSnapshot(t, db, "b")) {
		t.Fatal("foreign sync changed")
	}
}

func TestTenantSyncTriggerReplacement(t *testing.T) {
	for _, recursive := range []int{0, 1} {
		t.Run(fmt.Sprint(recursive), func(t *testing.T) {
			db := successorPrivateFixture(t)
			a := syncReceiver(t, db, "a")
			ctx := tenant.Into(context.Background(), tenant.MustParse("a"))
			relationalExec(t, db, fmt.Sprintf("PRAGMA recursive_triggers=%d", recursive))
			foreign := syncSnapshot(t, db, "b")
			page, e := a.ReadEntryChanges(ctx, "", 0, 100)
			collisionMust(t, e)
			// Explicit rowid REPLACE of a different path must advance the old tombstone,
			// even when SQLite suppresses implicit delete triggers.
			relationalExec(t, db, `INSERT OR REPLACE INTO notes(tenant_id,id,path,short_id,title) VALUES('a',100,'replacement','same0001','replaced')`)
			delta, e := a.ReadEntryChanges(ctx, page.Epoch, page.Cursor, 100)
			collisionMust(t, e)
			if len(delta.Rows) != 2 {
				t.Fatalf("lost replacement tombstone: %+v", delta.Rows)
			}
			seen := map[string]*NoteRow{}
			for _, r := range delta.Rows {
				seen[r.Path] = r.Note
			}
			if _, ok := seen["same"]; !ok || seen["same"] != nil || seen["replacement"] == nil {
				t.Fatal(seen)
			}
			relationalExec(t, db, `UPDATE notes SET path='renamed',title='renamed' WHERE tenant_id='a' AND id=100; DELETE FROM notes WHERE tenant_id='a' AND id=100`)
			delta, e = a.ReadEntryChanges(ctx, delta.Epoch, delta.Cursor, 100)
			collisionMust(t, e)
			if len(delta.Rows) != 2 {
				t.Fatalf("rename/delete history missing: %+v", delta.Rows)
			}
			for _, r := range delta.Rows {
				if r.Note != nil || (r.Path != "renamed" && r.Path != "replacement") {
					t.Fatalf("bad rename/delete tombstone: %+v", r)
				}
			}
			if _, e = db.Exec(`INSERT OR REPLACE INTO notes(tenant_id,id,path,short_id,title) VALUES('a',200,'foreign','bad00001','bad')`); e == nil {
				t.Fatal("foreign replacement accepted")
			}
			if !reflect.DeepEqual(foreign, syncSnapshot(t, db, "b")) {
				t.Fatal("foreign trigger or FTS changed")
			}
			collisionMust(t, migrateSuccessorSchema(context.Background(), db, nil))
		})
	}
}

func TestTenantSyncGuards(t *testing.T) {
	db := successorPrivateFixture(t)
	a := syncReceiver(t, db, "a")
	ca := tenant.Into(context.Background(), tenant.MustParse("a"))
	cb := tenant.Into(context.Background(), tenant.MustParse("b"))
	assertDenied := func(s syncAPI, ctx context.Context) {
		t.Helper()
		v := reflect.ValueOf(s)
		typ := reflect.TypeOf((*syncAPI)(nil)).Elem()
		for i := 0; i < typ.NumMethod(); i++ {
			m := typ.Method(i)
			args := []reflect.Value{reflect.ValueOf(&ctx).Elem()}
			for j := 1; j < m.Type.NumIn(); j++ {
				args = append(args, reflect.Zero(m.Type.In(j)))
			}
			out := v.MethodByName(m.Name).Call(args)
			if out[len(out)-1].IsNil() {
				t.Errorf("%s accepted invalid scope", m.Name)
			}
		}
	}
	before := recoverySnapshot(t, db)
	for _, ctx := range []context.Context{nil, context.Background(), cb} {
		assertDenied(a, ctx)
	}
	cancelled, cancel := context.WithCancel(ca)
	cancel()
	assertDenied(a, cancelled)
	var nilStore *TenantStore
	assertDenied(nilStore, ca)
	assertDenied(&TenantStore{}, ca)
	for _, bounds := range [][2]int64{{-1, 1}, {0, 0}, {0, -1}, {0, 10001}} {
		if _, e := a.ReadEntryChanges(ca, "", bounds[0], int(bounds[1])); e == nil {
			t.Fatal("invalid page bounds accepted")
		}
	}
	if !reflect.DeepEqual(before, recoverySnapshot(t, db)) {
		t.Fatal("guard changed rows")
	}
	for _, mutation := range []string{"DROP TRIGGER entry_sync_insert", "CREATE TEMP TABLE notes(path)", "DROP INDEX entry_sync_changes_owner_sequence"} {
		tx, e := db.Begin()
		collisionMust(t, e)
		relationalExec(t, tx, mutation)
		// Hold the tamper in the same snapshot as the scope check; no schema repair.
		if _, e = (&TenantStore{db: db, tenantID: tenant.MustParse("a")}).syncScope(ca, tx, false); e == nil {
			t.Fatal("damaged sync admitted")
		}
		collisionMust(t, tx.Rollback())
	}
}

func TestTenantSyncMainProfiles(t *testing.T) {
	for _, source := range provenanceSources {
		t.Run(source.profile+source.revision[:8], func(t *testing.T) {
			db := archivedSchemaFixture(t, source.revision)
			local := syncReceiver(t, db, "local")
			ctx := tenant.Into(context.Background(), tenant.Local)
			page, e := local.ReadSelectedEntries(ctx, nil)
			hasSync := source.profile == "main30-initial-sync" || source.profile == "main30-devices"
			if hasSync {
				if e != nil || page.Epoch == "" {
					t.Fatal(page, e)
				}
			} else if e == nil {
				t.Fatal("absent sync admitted")
			}
			_, e = local.SyncDevices(ctx)
			if source.profile == "main30-devices" {
				collisionMust(t, e)
			} else if e == nil {
				t.Fatal("absent devices admitted")
			}
			foreign := syncReceiver(t, db, "a")
			if _, e = foreign.ReadSelectedEntries(tenant.Into(ctx, tenant.MustParse("a")), nil); e == nil {
				t.Fatal("nonlocal main sync admitted")
			}
		})
	}
}

func TestTenantSyncConcurrentCAS(t *testing.T) {
	db := successorPrivateFixture(t)
	s := syncReceiver(t, db, "a")
	ctx := tenant.Into(context.Background(), tenant.MustParse("a"))
	before := types.SyncDevice{ID: "same", Owner: "original"}
	collisionMust(t, s.SaveSyncDevice(ctx, nil, before))
	foreign := syncSnapshot(t, db, "b")
	start := make(chan struct{})
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			after := before
			after.Owner = fmt.Sprint(i)
			results <- s.SaveSyncDevice(ctx, &before, after)
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	wins := 0
	for e := range results {
		if e == nil {
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("CAS winners %d", wins)
	}
	if !reflect.DeepEqual(foreign, syncSnapshot(t, db, "b")) {
		t.Fatal("CAS changed foreign rows")
	}
}

func TestSuccessorElevenTableInventory(t *testing.T) {
	db := successorPrivateFixture(t)
	want := []string{"bulk_jobs", "bulk_job_items", "execution_budgets", "budget_reservations", "supervisor_checkpoints", "supervisor_checkpoint_versions", "supervisor_operations", "entry_sync_devices", "entry_sync_identity", "entry_sync_changes", "entry_sync_operations"}
	actual := successorWorkloadTables()
	sort.Strings(actual)
	sorted := append([]string{}, want...)
	sort.Strings(sorted)
	if !reflect.DeepEqual(actual, sorted) {
		t.Fatal("eleven-table manifest drift", actual)
	}
	for _, id := range []string{"a", "b"} {
		ctx := tenant.Into(context.Background(), tenant.MustParse(id))
		seedLedgers(t, ledgerAPI(t, db, tenant.MustParse(id)), ctx, id)
		s := syncReceiver(t, db, id)
		collisionMust(t, s.SaveSyncDevice(ctx, nil, types.SyncDevice{ID: "same", Owner: id}))
		_, e := s.ReserveSyncOperation(ctx, "same", "hash")
		collisionMust(t, e)
		for _, table := range want {
			var n int
			collisionMust(t, db.QueryRow("SELECT count(*) FROM "+table+" WHERE tenant_id=?", id).Scan(&n))
			if n == 0 {
				t.Fatalf("unpopulated %s/%s", id, table)
			}
		}
	}
	for _, table := range want {
		var fks int
		collisionMust(t, db.QueryRow(`SELECT count(*) FROM pragma_foreign_key_list(?) WHERE "table"='tenants' AND "from"='tenant_id' AND "to"='id'`, table).Scan(&fks))
		if fks != 1 {
			t.Fatalf("missing ownership FK %s", table)
		}
	}
	// Discover EVERY tenant-column table, not merely the eleven selected names.
	discovered := compatibilityRows(t, db, `SELECT m.name FROM sqlite_schema m WHERE m.type='table' AND EXISTS(SELECT 1 FROM pragma_table_info(m.name) WHERE name='tenant_id') ORDER BY m.name`)
	full := append(append([]string{}, relationalTenantTables...), want...)
	full = append(full, "tenant_runner_keys", "tenant_client_keys", "tenant_roots", "tenant_fts")
	sort.Strings(full)
	if !reflect.DeepEqual(discovered, full) {
		t.Fatal("unclassified owned table", discovered)
	}
	recoveryIntegrity(t, db)
}

package storage

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
)

func TestPhase6WebhookIsolation(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	c := context.Background()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	// Seed through SQL so reads and failed writes are independently exercised.
	for _, id := range []string{"local", "acme"} {
		relationalExec(t, owner.db, fmt.Sprintf(`INSERT INTO webhooks(tenant_id,id,name,url,events,filter,secret,enabled,created_at,updated_at) VALUES('%s','same','%s','https://example.invalid','[]','{}','%s',1,'now','now');
INSERT INTO webhook_deliveries(tenant_id,id,webhook_id,event_type,success,created_at) VALUES('%s','same','same','%s',1,'now')`, id, id, id, id, id))
	}
	relationalExec(t, owner.db, `INSERT INTO webhooks(tenant_id,id,name,url,events,filter,secret,created_at,updated_at) VALUES('acme','foreign','foreign','x','[]','{}','foreign-secret','now','now')`)
	before := relationalSnapshot(t, owner.db, `(SELECT * FROM webhooks WHERE tenant_id='acme')`)
	for _, s := range []*TenantStore{a, b} {
		wh, e := s.GetWebhook(c, "same")
		must(e)
		if wh.Secret != s.TenantID().String() {
			t.Errorf("foreign secret: %+v", wh)
		}
		ds, e := s.ListDeliveries(c, "same", 0)
		must(e)
		if len(ds) != 1 || ds[0].EventType != s.TenantID().String() {
			t.Errorf("foreign deliveries: %+v", ds)
		}
	}
	if _, e := a.GetWebhook(c, "foreign"); e == nil {
		t.Error("foreign subscription visible")
	}
	if e := a.UpdateWebhook(c, &Webhook{ID: "foreign", Secret: "stolen"}); e == nil {
		t.Error("foreign update accepted")
	}
	if e := a.DeleteWebhook(c, "foreign"); e == nil {
		t.Error("foreign delete accepted")
	}
	if e := a.CreateDelivery(c, &WebhookDelivery{ID: "cross-parent", WebhookID: "foreign"}); e == nil {
		t.Error("foreign parent accepted")
	}
	if got := relationalSnapshot(t, owner.db, `(SELECT * FROM webhooks WHERE tenant_id='acme')`); !reflect.DeepEqual(got, before) {
		t.Error("failed writes changed B")
	}
	for _, s := range []*TenantStore{a, b} {
		must(s.CreateWebhook(c, &Webhook{ID: "created", Name: s.TenantID().String(), URL: "x", Enabled: true}))
		must(s.CreateDelivery(c, &WebhookDelivery{ID: "created", WebhookID: "created", EventType: s.TenantID().String()}))
	}
	if e := a.CreateWebhook(c, &Webhook{ID: "created", URL: "duplicate"}); e == nil {
		t.Error("duplicate subscription accepted")
	}
	if e := a.CreateDelivery(c, &WebhookDelivery{ID: "created", WebhookID: "same"}); e == nil {
		t.Error("duplicate delivery accepted")
	}
	must(a.UpdateWebhook(c, &Webhook{ID: "created", Secret: "local-only", URL: "changed"}))
	for _, enabled := range []bool{false, true} {
		ws, e := a.ListWebhooks(c, enabled)
		must(e)
		for _, w := range ws {
			if w.ID == "foreign" || w.Secret == "acme" {
				t.Errorf("foreign listing: %+v", w)
			}
		}
	}
	must(a.DeleteWebhook(c, "same"))
	ds, e := a.ListDeliveries(c, "same", 10)
	must(e)
	if len(ds) != 0 {
		t.Error("own cascade missing")
	}
	ds, e = b.ListDeliveries(c, "same", 10)
	must(e)
	if len(ds) != 1 {
		t.Error("foreign cascade")
	}
	w, e := b.GetWebhook(c, "created")
	must(e)
	if w.URL != "x" || w.Secret != "" {
		t.Errorf("B changed: %+v", w)
	}
	ds, e = b.ListDeliveries(c, "created", 10)
	must(e)
	if len(ds) != 1 || ds[0].WebhookID != "created" {
		t.Error("B delivery changed")
	}
}

func TestPhase6ProjectPurgeIsolation(t *testing.T) {
	owner, a, _ := migratedNoteStores(t)
	c := context.Background()
	for _, id := range []string{"local", "acme"} {
		// Both OR branches, identical entry IDs/paths and a literal wildcard prefix.
		seed := fmt.Sprintf(`INSERT INTO notes(tenant_id,id,path,short_id,project_id) VALUES('%[1]s',1001,'projects/p_%%/task/same.md','same',''),('%[1]s',1002,'elsewhere','other','p_%%'),('%[1]s',1003,'projects/pXmore/task/keep.md','keep','keep');
INSERT INTO tenant_runner_keys(tenant_id,runner_id) VALUES('%[1]s','purge-runner');
INSERT INTO runners(tenant_id,runner_id,hostname,registered_at,last_heartbeat) VALUES('%[1]s','purge-runner','host',1,2);
INSERT INTO task_claims(tenant_id,project_id,task_id,runner_id,claimed_at,expires_at) VALUES('%[1]s','p_%%','same','purge-runner',1,2);
INSERT INTO task_dispatch_leases(tenant_id,project_id,task_id,lease_id,assigned_runner_id,state,pushed_at,expires_at) VALUES('%[1]s','p_%%','same','purge-lease','purge-runner','acked',1,2);
INSERT INTO task_placement_reasons(tenant_id,project_id,task_id,decision,created_at) VALUES('%[1]s','p_%%','same','no_candidate',1);
INSERT INTO feature_assignments(tenant_id,project_id,feature_id,runner_id,source,status,assigned_at,updated_at) VALUES('%[1]s','p_%%','same','purge-runner','manual','assigned',1,2);
INSERT INTO feature_cascade_roots(tenant_id,project_id,root_feature_id,requested_at,paused_at_request) VALUES('%[1]s','p_%%','same',1,1);
INSERT INTO project_pause_state(tenant_id,project_id,tasks_paused,automations_paused,updated_at) VALUES('%[1]s','p_%%',1,1,2);
INSERT INTO project_placement(tenant_id,project_id) VALUES('%[1]s','p_%%');
INSERT INTO opencode_instances(tenant_id,instance_id,runner_id,project_id) VALUES('%[1]s','purge-instance','purge-runner','p_%%');
INSERT INTO event_log(tenant_id,event_type,payload) VALUES('%[1]s','task.completed','{"project_id":"p_%%"}');
INSERT INTO entry_meta(tenant_id,path,project_id) VALUES('%[1]s','projects/p_%%/task/same.md','p_%%');
INSERT INTO attachments(tenant_id,id,digest,size) VALUES('%[1]s',1001,'shared-digest',2);
INSERT INTO entry_attachments(tenant_id,note_id,attachment_id) VALUES('%[1]s',1001,1001),('%[1]s',1003,1001);`, id)
		// Physical autoincrement IDs remain global; logical entry IDs collide.
		if id == "acme" {
			seed = strings.NewReplacer("1001", "2001", "1002", "2002", "1003", "2003").Replace(seed)
		}
		relationalExec(t, owner.db, seed)
	}
	tables := append(append([]string{}, projectScopedTables...), "opencode_instances", "notes", "event_log", "entry_meta", "attachments", "entry_attachments")
	before := map[string][]string{}
	localBefore := map[string][]string{}
	for _, table := range tables {
		before[table] = relationalSnapshot(t, owner.db, "(SELECT * FROM "+table+" WHERE tenant_id='acme')")
		localBefore[table] = relationalSnapshot(t, owner.db, "(SELECT * FROM "+table+" WHERE tenant_id='local')")
	}
	paths, e := a.ListProjectNotePaths(c, "p_%")
	if e != nil || !reflect.DeepEqual(paths, []string{"elsewhere", "projects/p_%/task/same.md"}) {
		t.Errorf("paths: %v %v", paths, e)
	}
	// Fail at the separate instances delete after the loop has deleted rows.
	// The entire transaction must roll back, not leave a partially purged A.
	relationalExec(t, owner.db, `CREATE TRIGGER phase6_refuse_purge BEFORE DELETE ON opencode_instances WHEN OLD.tenant_id='local' BEGIN SELECT RAISE(ABORT,'test refusal'); END`)
	if _, e := a.PurgeProjectState(c, "p_%"); e == nil {
		t.Fatal("purge ignored transaction failure")
	}
	for _, table := range tables {
		for id, want := range map[string][]string{"local": localBefore[table], "acme": before[table]} {
			if got := relationalSnapshot(t, owner.db, "(SELECT * FROM "+table+" WHERE tenant_id='"+id+"')"); !reflect.DeepEqual(got, want) {
				t.Errorf("failed purge changed %s %s", id, table)
			}
		}
	}
	relationalExec(t, owner.db, `DROP TRIGGER phase6_refuse_purge`)
	removed, e := a.PurgeProjectState(c, "p_%")
	if e != nil {
		t.Fatal(e)
	}
	for _, table := range append(append([]string{}, projectScopedTables...), "opencode_instances") {
		if removed[table] != 1 {
			t.Errorf("%s removed %d", table, removed[table])
		}
	}
	n, e := a.DeleteProjectNotes(c, "p_%")
	if e != nil || n != 2 {
		t.Errorf("delete: %d %v", n, e)
	}
	for _, table := range tables {
		if got := relationalSnapshot(t, owner.db, "(SELECT * FROM "+table+" WHERE tenant_id='acme')"); !reflect.DeepEqual(got, before[table]) {
			t.Errorf("B %s changed", table)
		}
	}
	for _, table := range []string{"event_log", "entry_meta", "attachments"} {
		if got := relationalSnapshot(t, owner.db, "(SELECT * FROM "+table+" WHERE tenant_id='local')"); !reflect.DeepEqual(got, localBefore[table]) {
			t.Errorf("retention changed %s", table)
		}
	}
	var nlinks int
	if e := owner.db.QueryRow(`SELECT count(*) FROM entry_attachments WHERE tenant_id='local' AND attachment_id=1001 AND note_id=1003`).Scan(&nlinks); e != nil || nlinks != 1 {
		t.Errorf("shared attachment reference: %d %v", nlinks, e)
	}
}

func TestPhase6ScopeGuards(t *testing.T) {
	ops := []func(*TenantStore, context.Context) error{
		func(s *TenantStore, c context.Context) error { return s.CreateWebhook(c, &Webhook{}) },
		func(s *TenantStore, c context.Context) error { _, e := s.GetWebhook(c, "x"); return e },
		func(s *TenantStore, c context.Context) error { _, e := s.ListWebhooks(c); return e },
		func(s *TenantStore, c context.Context) error { return s.UpdateWebhook(c, &Webhook{}) },
		func(s *TenantStore, c context.Context) error { return s.DeleteWebhook(c, "x") },
		func(s *TenantStore, c context.Context) error { return s.CreateDelivery(c, &WebhookDelivery{}) },
		func(s *TenantStore, c context.Context) error { _, e := s.ListDeliveries(c, "x", 0); return e },
		func(s *TenantStore, c context.Context) error { _, e := s.ListProjectNotePaths(c, "p"); return e },
		func(s *TenantStore, c context.Context) error { _, e := s.PurgeProjectState(c, "p"); return e },
		func(s *TenantStore, c context.Context) error { _, e := s.DeleteProjectNotes(c, "p"); return e },
	}
	owner := newTestStorage(t)
	local, _ := owner.ForTenant(tenant.Local)
	foreign, _ := owner.ForTenant(tenant.MustParse("acme"))
	closedOwner := newTestStorage(t)
	closed, _ := closedOwner.ForTenant(tenant.Local)
	if e := closedOwner.Close(); e != nil {
		t.Fatal(e)
	}
	unknownOwner := newTestStorage(t)
	unknown, _ := unknownOwner.ForTenant(tenant.Local)
	relationalExec(t, unknownOwner.db, "UPDATE schema_version SET version=30")
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for i, op := range ops {
		for j, tc := range []struct {
			s *TenantStore
			c context.Context
		}{{nil, context.Background()}, {&TenantStore{}, context.Background()}, {&TenantStore{db: owner.db}, context.Background()}, {foreign, context.Background()}, {local, nil}, {local, cancelled}, {closed, context.Background()}, {unknown, context.Background()}} {
			t.Run(fmt.Sprintf("%d/%d", i, j), func(t *testing.T) {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("guard panic: %v", r)
					}
				}()
				if e := op(tc.s, tc.c); e == nil {
					t.Error("invalid scope accepted")
				}
			})
		}
	}
}

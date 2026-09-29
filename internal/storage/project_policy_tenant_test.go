package storage

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
)

func TestTenantProjectPolicyIsolation(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	// Remove the migration fixture's historical policy rows only.
	for _, table := range []string{"project_pause_state", "feature_pause_state", "feature_cascade_roots"} {
		relationalExec(t, owner.db, "DELETE FROM "+table+" WHERE tenant_id='local' AND project_id='deleted-project'")
	}
	c := context.Background()
	must := func(e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
	}
	// Colliding durable IDs need no live task or feature row.
	for _, s := range []*TenantStore{a, b} {
		must(s.SetProjectTaskPaused(c, "shared", true))
		must(s.SetProjectAutomationsPaused(c, "shared", true))
		must(s.SetFeaturePaused(c, "shared", "same", true))
		must(s.UpsertFeatureCascadeRoot(c, "shared", "same", true))
		must(s.UpsertProjectPlacement(c, &ProjectPlacementRow{ProjectID: "shared", Affinity: "strict", AllowedMachines: []string{s.TenantID().String()}}))
	}
	must(a.SetProjectTaskPaused(c, "shared", false))
	must(a.SetProjectAutomationsPaused(c, "shared", false))
	must(a.SetFeaturePaused(c, "shared", "same", false))
	must(a.UpsertFeatureCascadeRoot(c, "shared", "same", false))
	must(a.UpsertProjectPlacement(c, &ProjectPlacementRow{ProjectID: "shared", Affinity: "none"}))
	for _, read := range []func(context.Context, string) (bool, error){b.IsProjectTaskPaused, b.IsProjectAutomationsPaused} {
		v, e := read(c, "shared")
		must(e)
		if !v {
			t.Fatal("foreign project pause changed")
		}
	}
	if v, e := b.IsFeaturePaused(c, "shared", "same"); e != nil || !v {
		t.Fatalf("foreign feature pause: %v %v", v, e)
	}
	if rs, e := a.ListProjectPauseStates(c); e != nil || len(rs) != 0 {
		t.Fatalf("foreign pause listed: %+v %v", rs, e)
	}
	if rs, e := a.ListPausedFeatures(c); e != nil || len(rs) != 0 {
		t.Fatalf("foreign feature listed: %+v %v", rs, e)
	}
	if rs, e := b.ListFeatureCascadeRoots(c, "shared"); e != nil || len(rs) != 1 || !rs[0].PausedAtRequest {
		t.Fatalf("foreign cascade updated: %+v %v", rs, e)
	}
	if ok, e := a.DeleteFeatureCascadeRoot(c, "shared", "same"); e != nil || !ok {
		t.Fatalf("delete: %v %v", ok, e)
	}
	if ok, e := a.DeleteFeatureCascadeRoot(c, "shared", "same"); e != nil || ok {
		t.Fatalf("foreign delete: %v %v", ok, e)
	}
	if rs, e := a.ListFeatureCascadeRoots(c, ""); e != nil || len(rs) != 0 {
		t.Fatalf("foreign boot cascade: %+v %v", rs, e)
	}
	if rs, e := b.ListFeatureCascadeRoots(c, ""); e != nil || len(rs) != 1 {
		t.Fatalf("foreign cascade removed: %+v %v", rs, e)
	}
	if r, e := b.GetProjectPlacement(c, "shared"); e != nil || r.Affinity != "strict" || !reflect.DeepEqual(r.AllowedMachines, []string{"acme"}) {
		t.Fatalf("foreign placement: %+v %v", r, e)
	}
	must(b.UpsertProjectPlacement(c, &ProjectPlacementRow{ProjectID: "foreign-only", Affinity: "strict"}))
	if r, e := a.GetProjectPlacement(c, "foreign-only"); e != nil || !reflect.DeepEqual(r, DefaultProjectPlacement("foreign-only")) {
		t.Fatalf("foreign policy instead of defaults: %+v %v", r, e)
	}
	// Both UNION branches must be scoped, including a pause-only project.
	for _, row := range []struct{ owner, path, project string }{{"local", "local-note", "local-note"}, {"acme", "foreign-note", "foreign-note"}} {
		_, e := owner.db.Exec(`INSERT INTO notes(tenant_id,path,short_id,title,type,project_id) VALUES(?,?,?,?,'task',?)`, row.owner, row.path, row.path, row.path, row.project)
		must(e)
	}
	must(a.SetProjectTaskPaused(c, "local-durable", true))
	must(b.SetProjectAutomationsPaused(c, "foreign-durable", true))
	if ids, e := a.listKnownProjectIDs(c); e != nil || !reflect.DeepEqual(ids, []string{"local-durable", "local-note", "shared"}) {
		t.Fatalf("known projects: %v %v", ids, e)
	}
	for _, paused := range []bool{true, false} {
		must(a.SetAllProjectTasksPaused(c, paused))
		must(a.SetAllProjectAutomationsPaused(c, paused))
		for _, project := range []string{"local-durable", "local-note", "shared"} {
			for _, read := range []func(context.Context, string) (bool, error){a.IsProjectTaskPaused, a.IsProjectAutomationsPaused} {
				v, e := read(c, project)
				must(e)
				if v != paused {
					t.Fatalf("all-project %s: %v", project, v)
				}
			}
		}
		if v, e := b.IsProjectTaskPaused(c, "foreign-note"); e != nil || v {
			t.Fatalf("foreign note pause: %v %v", v, e)
		}
		if v, e := b.IsProjectAutomationsPaused(c, "foreign-durable"); e != nil || !v {
			t.Fatalf("foreign durable pause: %v %v", v, e)
		}
	}
	var n int
	must(owner.db.QueryRow(`SELECT count(*) FROM project_pause_state WHERE tenant_id='local' AND project_id LIKE 'foreign-%'`).Scan(&n))
	if n != 0 {
		t.Fatalf("all-project created foreign projects: %d", n)
	}
	// A grouped OR must not leak the foreign automation-only pause above.
	if rs, e := a.ListProjectPauseStates(c); e != nil || len(rs) != 0 {
		t.Fatalf("OR leaked: %+v %v", rs, e)
	}
	must(a.SetProjectTaskPaused(c, "local-note", true))
	must(a.SetFeaturePaused(c, "local-note", "removed", true))
	_, e := owner.db.Exec(`DELETE FROM notes WHERE tenant_id='local' AND project_id='local-note'`)
	must(e)
	if v, e := a.IsProjectTaskPaused(c, "local-note"); e != nil || !v {
		t.Fatalf("lost durable project hold: %v %v", v, e)
	}
	if v, e := a.IsFeaturePaused(c, "local-note", "removed"); e != nil || !v {
		t.Fatalf("lost durable feature hold: %v %v", v, e)
	}
	if fs, e := a.ListPausedFeatures(c); e != nil || !reflect.DeepEqual(fs, []PausedFeature{{"local-note", "removed"}}) {
		t.Fatalf("durable feature list: %+v %v", fs, e)
	}
	if v, e := a.IsFeaturePaused(c, "shared", ""); e != nil || v {
		t.Fatalf("empty feature sentinel: %v %v", v, e)
	}
	if e := a.SetFeaturePaused(c, "shared", "", true); e == nil {
		t.Fatal("empty feature accepted")
	}
}

func TestTenantProjectPolicyScopeGuards(t *testing.T) {
	ops := []func(*TenantStore, context.Context) error{
		func(s *TenantStore, c context.Context) error { return s.SetProjectTaskPaused(c, "p", true) },
		func(s *TenantStore, c context.Context) error { return s.SetProjectAutomationsPaused(c, "p", true) },
		func(s *TenantStore, c context.Context) error {
			return s.setProjectPauseColumn(c, "p", "tasks_paused", true)
		},
		func(s *TenantStore, c context.Context) error { return s.SetAllProjectTasksPaused(c, true) },
		func(s *TenantStore, c context.Context) error { return s.SetAllProjectAutomationsPaused(c, true) },
		func(s *TenantStore, c context.Context) error { _, e := s.IsProjectTaskPaused(c, "p"); return e },
		func(s *TenantStore, c context.Context) error { _, e := s.IsProjectAutomationsPaused(c, "p"); return e },
		func(s *TenantStore, c context.Context) error {
			_, e := s.isProjectPauseColumn(c, "p", "tasks_paused")
			return e
		},
		func(s *TenantStore, c context.Context) error { _, e := s.ListProjectPauseStates(c); return e },
		func(s *TenantStore, c context.Context) error { _, e := s.listKnownProjectIDs(c); return e },
		func(s *TenantStore, c context.Context) error { return s.SetFeaturePaused(c, "p", "f", true) },
		func(s *TenantStore, c context.Context) error { _, e := s.IsFeaturePaused(c, "", ""); return e },
		func(s *TenantStore, c context.Context) error { _, e := s.ListPausedFeatures(c); return e },
		func(s *TenantStore, c context.Context) error { return s.UpsertFeatureCascadeRoot(c, "p", "f", true) },
		func(s *TenantStore, c context.Context) error {
			_, e := s.DeleteFeatureCascadeRoot(c, "p", "f")
			return e
		},
		func(s *TenantStore, c context.Context) error { _, e := s.ListFeatureCascadeRoots(c, ""); return e },
		func(s *TenantStore, c context.Context) error { _, e := s.GetProjectPlacement(c, "p"); return e },
		func(s *TenantStore, c context.Context) error {
			return s.UpsertProjectPlacement(c, &ProjectPlacementRow{ProjectID: "p"})
		},
	}
	for i, op := range ops {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			owner := newTestStorage(t)
			local, _ := owner.ForTenant(tenant.Local)
			foreign, _ := owner.ForTenant(tenant.MustParse("acme"))
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			for _, tc := range []struct {
				name string
				s    *TenantStore
				c    context.Context
			}{
				{"foreign28", foreign, context.Background()}, {"nil", nil, context.Background()}, {"zero", &TenantStore{}, context.Background()}, {"invalidID", &TenantStore{db: owner.db}, context.Background()}, {"cancelled", local, cancelled}, {"nilContext", local, nil},
			} {
				t.Run(tc.name, func(t *testing.T) {
					defer func() {
						if r := recover(); r != nil {
							t.Errorf("guard panicked: %v", r)
						}
					}()
					if e := op(tc.s, tc.c); e == nil {
						t.Fatal("invalid scope accepted")
					}
				})
			}
		})
	}
}

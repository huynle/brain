package storage

import (
	"context"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/tenant"
)

func TestTenantRegistryCollisions(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	ctx := context.Background()
	// The migration fixture carries one legacy local runner and instance.
	if _, err := a.DeleteInstancesByRunner(ctx, "runner"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DeleteRunner(ctx, "runner"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*TenantStore{a, b} {
		id := s.TenantID().String()
		if err := s.UpsertRunner(ctx, &RunnerRow{RunnerID: "same", Hostname: id, Status: "online", LastHeartbeat: 1}); err != nil {
			t.Fatalf("register %s: %v", id, err)
		}
		if err := s.UpsertBrainClient(ctx, &BrainClientRow{ClientID: "same", Hostname: id}); err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertBrainClientWorkspace(ctx, &BrainClientWorkspaceRow{ClientID: "same", Path: "/same", ProjectID: "same", HostID: id}); err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertInstance(ctx, &InstanceRow{InstanceID: "same", RunnerID: "same", Title: id}); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []*TenantStore{a, b} {
		id := s.TenantID().String()
		r, err := s.GetRunner(ctx, "same")
		if err != nil || r == nil || r.Hostname != id {
			t.Fatalf("runner: %+v %v", r, err)
		}
		c, err := s.GetBrainClient(ctx, "same")
		if err != nil || c == nil || c.Hostname != id {
			t.Fatalf("client: %+v %v", c, err)
		}
		w, err := s.ListBrainClientWorkspaces(ctx, "same")
		if err != nil || len(w) != 1 || w[0].HostID != id {
			t.Fatalf("workspaces: %+v %v", w, err)
		}
		i, err := s.GetInstance(ctx, "same")
		if err != nil || i == nil || i.Title != id {
			t.Fatalf("instance: %+v %v", i, err)
		}
		rs, err := s.ListRunners(ctx)
		if err != nil || len(rs) != 1 {
			t.Fatalf("runners: %+v %v", rs, err)
		}
		is, err := s.ListAllInstances(ctx)
		if err != nil || len(is) != 1 {
			t.Fatalf("instances: %+v %v", is, err)
		}
	}
	if err := a.UpdateHeartbeat(ctx, "same", 3, map[string]interface{}{"cpu": 2}); err != nil {
		t.Fatal(err)
	}
	if err := a.UpdateRunnerCapabilities(ctx, "same", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if err := a.UpdateAffinity(ctx, "same", []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if err := a.UpdateRunnerMaxParallel(ctx, "same", 7); err != nil {
		t.Fatal(err)
	}
	push, drain := true, true
	if err := a.UpdateRunnerDispatchMetadata(ctx, "same", &push, map[string]string{"a": "a"}, []string{"/a"}, []string{"a"}, map[string]interface{}{"cpu": 1}, map[string]interface{}{"slots": 2}, &drain); err != nil {
		t.Fatal(err)
	}
	if err := a.SetRunnerStatus(ctx, "same", "stale"); err != nil {
		t.Fatal(err)
	}
	updated, err := a.GetRunner(ctx, "same")
	if err != nil || updated == nil || updated.LastHeartbeat <= 1 || updated.Labels["_running_tasks"] != "3" || updated.Labels["_stat_cpu"] != "2" || len(updated.Capabilities) != 1 || updated.Capabilities[0] != "a" || updated.FeatureIDs != "a" || updated.MaxParallel != 7 || !updated.DispatchPush || !updated.Draining || len(updated.WorkspaceRoots) != 1 || updated.WorkspaceRoots[0] != "/a" || len(updated.Projects) != 1 || updated.Projects[0] != "a" || updated.Resources["cpu"] != float64(1) || updated.Capacity["slots"] != float64(2) {
		t.Fatalf("own runner update: %+v %v", updated, err)
	}
	if rs, err := a.ListRunnersByStatus(ctx, "stale"); err != nil || len(rs) != 1 {
		t.Fatalf("status: %+v %v", rs, err)
	}
	r, err := b.GetRunner(ctx, "same")
	if err != nil || r == nil || r.Status != "online" || r.LastHeartbeat != 1 || len(r.Capabilities) != 0 || r.FeatureIDs != "" || r.MaxParallel != 0 || r.DispatchPush || r.Draining || len(r.Labels) != 0 {
		t.Fatalf("foreign mutation: %+v %v", r, err)
	}
	if ok, err := a.DeleteInstance(ctx, "same", "same"); err != nil || !ok {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if i, err := b.GetInstance(ctx, "same"); err != nil || i == nil {
		t.Fatalf("foreign instance deleted: %+v %v", i, err)
	}
	if err := a.UpsertBrainClient(ctx, &BrainClientRow{ClientID: "same", Hostname: "updated", RegisteredAt: 1, LastSeen: 42}); err != nil {
		t.Fatal(err)
	}
	if c, err := a.GetBrainClient(ctx, "same"); err != nil || c == nil || c.Hostname != "updated" || c.RegisteredAt == 1 || c.LastSeen != 42 {
		t.Fatalf("client upsert semantics: %+v %v", c, err)
	}
	if c, err := b.GetBrainClient(ctx, "same"); err != nil || c == nil || c.Hostname != "acme" {
		t.Fatalf("foreign client upsert: %+v %v", c, err)
	}
	if err := a.UpsertBrainClientWorkspace(ctx, &BrainClientWorkspaceRow{ClientID: "same", Path: "/same", ProjectID: "same", HostID: "updated", FirstSeen: 1, LastSeen: 42}); err != nil {
		t.Fatal(err)
	}
	if ws, err := a.ListBrainClientWorkspaces(ctx, "same"); err != nil || len(ws) != 1 || ws[0].HostID != "updated" || ws[0].FirstSeen == 1 || ws[0].LastSeen != 42 {
		t.Fatalf("workspace upsert semantics: %+v %v", ws, err)
	}
	if ws, err := b.ListBrainClientWorkspaces(ctx, "same"); err != nil || len(ws) != 1 || ws[0].HostID != "acme" {
		t.Fatalf("foreign workspace upsert: %+v %v", ws, err)
	}
}

func TestTenantRegistrySweepPauseDurability(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	ctx := context.Background()
	if _, err := a.DeleteInstancesByRunner(ctx, "runner"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.DeleteRunner(ctx, "runner"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*TenantStore{a, b} {
		if err := s.UpsertRunner(ctx, &RunnerRow{RunnerID: "same", Status: "online", LastHeartbeat: 1}); err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertInstance(ctx, &InstanceRow{InstanceID: "same", RunnerID: "same"}); err != nil {
			t.Fatal(err)
		}
	}
	if ok, err := a.SetRunnerPaused(ctx, "same", true); err != nil || !ok {
		t.Fatalf("pause %v %v", ok, err)
	}
	if n, err := a.ExpireStaleRunners(ctx, time.Minute); err != nil || n != 1 {
		t.Fatalf("expire %d %v", n, err)
	}
	if n, err := a.DeleteInstancesByRunner(ctx, "same"); err != nil || n != 1 {
		t.Fatalf("sweep %d %v", n, err)
	}
	if ok, err := a.DeleteRunner(ctx, "same"); err != nil || !ok {
		t.Fatalf("deregister %v %v", ok, err)
	}
	if ok, err := a.SetRunnerPaused(ctx, "same", false); err != nil || ok {
		t.Fatalf("unregistered pause %v %v", ok, err)
	}
	if err := a.UpsertInstance(ctx, &InstanceRow{InstanceID: "historical", RunnerID: "same"}); err != nil {
		t.Fatalf("durable runner reference lost: %v", err)
	}
	// Rebinding has no enrollment meaning; durable keys and pause survive deletion.
	a, _ = owner.ForTenant(a.TenantID())
	if err := a.UpsertRunner(ctx, &RunnerRow{RunnerID: "same", Status: "online"}); err != nil {
		t.Fatal(err)
	}
	r, err := a.GetRunner(ctx, "same")
	if err != nil || r == nil || !r.Paused {
		t.Fatalf("lost pause %+v %v", r, err)
	}
	r, err = b.GetRunner(ctx, "same")
	if err != nil || r == nil || r.Paused || r.Status != "online" {
		t.Fatalf("foreign sweep/pause %+v %v", r, err)
	}
	if is, err := b.ListInstancesByRunner(ctx, "same"); err != nil || len(is) != 1 {
		t.Fatalf("foreign sweep %+v %v", is, err)
	}
}

func TestTenantRegistryForeignReferencesAndReplacementRollback(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	ctx := context.Background()
	if err := b.UpsertRunner(ctx, &RunnerRow{RunnerID: "foreign"}); err != nil {
		t.Fatal(err)
	}
	if err := b.UpsertBrainClient(ctx, &BrainClientRow{ClientID: "foreign"}); err != nil {
		t.Fatal(err)
	}
	if err := a.UpsertInstance(ctx, &InstanceRow{InstanceID: "bad", RunnerID: "foreign"}); err == nil {
		t.Fatal("foreign runner accepted")
	}
	if err := a.UpsertBrainClientWorkspace(ctx, &BrainClientWorkspaceRow{ClientID: "foreign", Path: "/bad"}); err == nil {
		t.Fatal("foreign client accepted")
	}
	if err := a.ReplaceInstancesForRunner(ctx, "foreign", []InstanceRow{{InstanceID: "bad"}}); err == nil {
		t.Fatal("foreign replacement accepted")
	}
	if err := a.ReplaceInstancesForRunner(ctx, "foreign", nil); err == nil {
		t.Fatal("empty foreign replacement accepted")
	}
	for _, table := range []string{"tenant_runner_keys", "tenant_client_keys"} {
		var count int
		if err := owner.db.QueryRow("SELECT count(*) FROM " + table + " WHERE tenant_id='local' AND " + map[string]string{"tenant_runner_keys": "runner_id", "tenant_client_keys": "client_id"}[table] + "='foreign'").Scan(&count); err != nil || count != 0 {
			t.Fatalf("dependent write manufactured %s: %d %v", table, count, err)
		}
	}
	for _, s := range []*TenantStore{a, b} {
		if err := s.UpsertRunner(ctx, &RunnerRow{RunnerID: "same"}); err != nil {
			t.Fatal(err)
		}
		if err := s.UpsertInstance(ctx, &InstanceRow{InstanceID: "old", RunnerID: "same", Title: s.TenantID().String()}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := owner.db.Exec(`CREATE TRIGGER fail_instance BEFORE INSERT ON opencode_instances WHEN NEW.instance_id='fail' BEGIN SELECT RAISE(ABORT,'injected failure'); END`); err != nil {
		t.Fatal(err)
	}
	if err := a.ReplaceInstancesForRunner(ctx, "same", []InstanceRow{{InstanceID: "new"}, {InstanceID: "fail"}}); err == nil {
		t.Fatal("injected insert failure ignored")
	}
	for _, s := range []*TenantStore{a, b} {
		is, err := s.ListInstancesByRunner(ctx, "same")
		if err != nil || len(is) != 1 || is[0].InstanceID != "old" || is[0].Title != s.TenantID().String() {
			t.Fatalf("rollback %+v %v", is, err)
		}
	}
	if err := a.ReplaceInstancesForRunner(ctx, "same", []InstanceRow{{InstanceID: "old", Title: "replacement"}, {InstanceID: "new"}}); err != nil {
		t.Fatal(err)
	}
	if is, err := a.ListInstancesByRunner(ctx, "same"); err != nil || len(is) != 2 {
		t.Fatalf("replacement %+v %v", is, err)
	}
	if i, err := b.GetInstance(ctx, "old"); err != nil || i == nil || i.Title != "acme" {
		t.Fatalf("foreign replacement %+v %v", i, err)
	}
	if err := a.ReplaceInstancesForRunner(ctx, "same", nil); err != nil {
		t.Fatal(err)
	}
	if is, err := a.ListInstancesByRunner(ctx, "same"); err != nil || len(is) != 0 {
		t.Fatalf("empty replacement: %+v %v", is, err)
	}
	if is, err := b.ListInstancesByRunner(ctx, "same"); err != nil || len(is) != 1 {
		t.Fatalf("foreign empty replacement: %+v %v", is, err)
	}
}

func TestTenantRegistryKeyAtomicity(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	ctx := context.Background()
	for _, table := range []string{"runners", "brain_clients"} {
		if _, err := owner.db.Exec("CREATE TRIGGER fail_" + table + " BEFORE INSERT ON " + table + " BEGIN SELECT RAISE(ABORT,'injected registry failure'); END"); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.UpsertRunner(ctx, &RunnerRow{RunnerID: "new-key"}); err == nil {
		t.Fatal("runner failure ignored")
	}
	if err := a.UpsertBrainClient(ctx, &BrainClientRow{ClientID: "new-key"}); err == nil {
		t.Fatal("client failure ignored")
	}
	for _, table := range []string{"tenant_runner_keys", "tenant_client_keys"} {
		key := "runner_id"
		if table == "tenant_client_keys" {
			key = "client_id"
		}
		var n int
		if err := owner.db.QueryRow("SELECT count(*) FROM " + table + " WHERE " + key + "='new-key'").Scan(&n); err != nil || n != 0 {
			t.Fatalf("orphan key in %s: %d %v", table, n, err)
		}
	}
	if _, err := owner.db.Exec("DROP TRIGGER fail_brain_clients"); err != nil {
		t.Fatal(err)
	}
	if err := b.UpsertBrainClient(ctx, &BrainClientRow{ClientID: "durable"}); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.db.Exec("DELETE FROM brain_clients WHERE tenant_id='acme' AND client_id='durable'"); err != nil {
		t.Fatal(err)
	}
	if err := b.UpsertBrainClientWorkspace(ctx, &BrainClientWorkspaceRow{ClientID: "durable", Path: "/history"}); err != nil {
		t.Fatalf("durable client reference lost: %v", err)
	}
	if err := a.UpsertBrainClientWorkspace(ctx, &BrainClientWorkspaceRow{ClientID: "durable", Path: "/history"}); err == nil {
		t.Fatal("foreign durable client reference accepted")
	}
}

func TestTenantRegistryScopeGuards(t *testing.T) {
	owner := newTestStorage(t)
	local, err := owner.ForTenant(tenant.Local)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := owner.ForTenant(tenant.MustParse("acme"))
	if err != nil {
		t.Fatal(err)
	}
	ops := []struct {
		name string
		run  func(*TenantStore, context.Context) error
	}{
		{"UpsertRunner", func(s *TenantStore, c context.Context) error { return s.UpsertRunner(c, &RunnerRow{}) }},
		{"GetRunner", func(s *TenantStore, c context.Context) error { _, e := s.GetRunner(c, ""); return e }},
		{"ListRunners", func(s *TenantStore, c context.Context) error { _, e := s.ListRunners(c); return e }},
		{"ListRunnersByStatus", func(s *TenantStore, c context.Context) error { _, e := s.ListRunnersByStatus(c, ""); return e }},
		{"DeleteRunner", func(s *TenantStore, c context.Context) error { _, e := s.DeleteRunner(c, ""); return e }},
		{"UpdateHeartbeat", func(s *TenantStore, c context.Context) error { return s.UpdateHeartbeat(c, "", 0, nil) }},
		{"UpdateRunnerDispatchMetadata", func(s *TenantStore, c context.Context) error {
			return s.UpdateRunnerDispatchMetadata(c, "", nil, nil, nil, nil, nil, nil, nil)
		}},
		{"UpdateRunnerCapabilities", func(s *TenantStore, c context.Context) error { return s.UpdateRunnerCapabilities(c, "", nil) }},
		{"UpdateAffinity", func(s *TenantStore, c context.Context) error { return s.UpdateAffinity(c, "", nil) }},
		{"SetRunnerStatus", func(s *TenantStore, c context.Context) error { return s.SetRunnerStatus(c, "", "") }},
		{"UpdateRunnerMaxParallel", func(s *TenantStore, c context.Context) error { return s.UpdateRunnerMaxParallel(c, "", 0) }},
		{"ExpireStaleRunners", func(s *TenantStore, c context.Context) error { _, e := s.ExpireStaleRunners(c, 0); return e }},
		{"SetRunnerPaused", func(s *TenantStore, c context.Context) error { _, e := s.SetRunnerPaused(c, "", false); return e }},
		{"UpsertInstance", func(s *TenantStore, c context.Context) error { return s.UpsertInstance(c, &InstanceRow{}) }},
		{"DeleteInstance", func(s *TenantStore, c context.Context) error { _, e := s.DeleteInstance(c, "", ""); return e }},
		{"DeleteInstancesByRunner", func(s *TenantStore, c context.Context) error { _, e := s.DeleteInstancesByRunner(c, ""); return e }},
		{"GetInstance", func(s *TenantStore, c context.Context) error { _, e := s.GetInstance(c, ""); return e }},
		{"ListInstancesByRunner", func(s *TenantStore, c context.Context) error { _, e := s.ListInstancesByRunner(c, ""); return e }},
		{"ListAllInstances", func(s *TenantStore, c context.Context) error { _, e := s.ListAllInstances(c); return e }},
		{"ReplaceInstancesForRunner", func(s *TenantStore, c context.Context) error { return s.ReplaceInstancesForRunner(c, "", nil) }},
		{"UpsertBrainClient", func(s *TenantStore, c context.Context) error { return s.UpsertBrainClient(c, &BrainClientRow{}) }},
		{"GetBrainClient", func(s *TenantStore, c context.Context) error { _, e := s.GetBrainClient(c, ""); return e }},
		{"UpsertBrainClientWorkspace", func(s *TenantStore, c context.Context) error {
			return s.UpsertBrainClientWorkspace(c, &BrainClientWorkspaceRow{})
		}},
		{"ListBrainClientWorkspaces", func(s *TenantStore, c context.Context) error { _, e := s.ListBrainClientWorkspaces(c, ""); return e }},
	}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			for _, s := range []*TenantStore{nil, {}, foreign} {
				if err := op.run(s, context.Background()); err == nil {
					t.Fatal("invalid or v28 nonlocal operation accepted")
				}
			}
			if err := op.run(local, nil); err == nil {
				t.Fatal("nil context accepted")
			}
			cancelled, cancel := context.WithCancel(context.Background())
			cancel()
			if err := op.run(local, cancelled); err == nil {
				t.Fatal("cancelled context accepted")
			}
		})
	}
}

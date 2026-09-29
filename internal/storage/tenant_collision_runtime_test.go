package storage

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

const collisionPath = "projects/p/task/same.md"

func TestTenantCollisionRuntimeReads(t *testing.T) {
	f := newCollisionFixture(t)
	ctx := context.Background()
	a, b := f.snapshot(t, "local"), f.snapshot(t, "acme")
	for _, s := range []*TenantStore{f.a, f.b} {
		id := int64(10)
		if s == f.b {
			id += 10000
		}
		states, err := s.ListIndexedNoteStates(ctx)
		collisionMust(t, err)
		if len(states) != 2 {
			t.Fatalf("index states: %+v", states)
		}
		for _, state := range states {
			if state.Path != collisionPath && state.Path != "target" {
				t.Fatalf("foreign indexed path: %s", state.Path)
			}
		}
		n, err := s.GetNoteByTitle(ctx, "source")
		collisionMust(t, err)
		if n == nil || n.ID != id {
			t.Fatalf("title result: %+v", n)
		}
		notes, err := s.SearchNotes(ctx, "collision", &SearchOptions{})
		collisionMust(t, err)
		if len(notes) != 2 {
			t.Fatalf("FTS own corpus: %+v", notes)
		}
		for _, note := range notes {
			if note.ID != id && note.ID != id+1 {
				t.Fatalf("foreign FTS ID: %d", note.ID)
			}
		}
		tags, err := s.GetTags(ctx, collisionPath)
		collisionMust(t, err)
		if !reflect.DeepEqual(tags, []string{"tag"}) {
			t.Fatalf("tags: %v", tags)
		}
		links, err := s.GetLinks(ctx, collisionPath)
		collisionMust(t, err)
		if len(links) != 1 || links[0].SourceID != id {
			t.Fatalf("links: %+v", links)
		}
		attachments, err := s.ListAttachmentsForEntry(ctx, collisionPath)
		collisionMust(t, err)
		if len(attachments) != 1 || attachments[0].ID != id+60 {
			t.Fatalf("attachments: %+v", attachments)
		}
		vector, err := s.GetNoteEmbedding(ctx, id, 0)
		collisionMust(t, err)
		if !reflect.DeepEqual(vector, []float32{1, 0}) {
			t.Fatalf("vector: %v", vector)
		}
		r, err := s.GetRunner(ctx, "runner")
		collisionMust(t, err)
		if r == nil || r.Hostname != "host" {
			t.Fatalf("runner: %+v", r)
		}
		client, err := s.GetBrainClient(ctx, "client")
		collisionMust(t, err)
		if client == nil || client.HostID != "host" {
			t.Fatalf("client: %+v", client)
		}
		ws, err := s.ListBrainClientWorkspaces(ctx, "p")
		collisionMust(t, err)
		if len(ws) != 1 || ws[0].Path != "/workspace" {
			t.Fatalf("workspaces: %+v", ws)
		}
		claim, err := s.GetClaim(ctx, "p", "same")
		collisionMust(t, err)
		if claim == nil || claim.RunnerID != "runner" {
			t.Fatalf("claim: %+v", claim)
		}
		lease, err := s.GetDispatchLeaseRow(ctx, "p", "same")
		collisionMust(t, err)
		if lease == nil || lease.LeaseID != "lease" {
			t.Fatalf("lease: %+v", lease)
		}
		paused, err := s.IsProjectTaskPaused(ctx, "deleted-project")
		collisionMust(t, err)
		if !paused {
			t.Fatal("own pause missing")
		}
		policy, err := s.GetProjectPlacement(ctx, "p")
		collisionMust(t, err)
		if policy == nil || policy.ProjectID != "p" {
			t.Fatalf("policy: %+v", policy)
		}
		hook, err := s.GetWebhook(ctx, "hook")
		collisionMust(t, err)
		if hook == nil || hook.Name != "hook" {
			t.Fatalf("hook: %+v", hook)
		}
		ds, err := s.ListDeliveries(ctx, "hook", 10)
		collisionMust(t, err)
		if len(ds) != 1 || ds[0].ID != "delivery" {
			t.Fatalf("deliveries: %+v", ds)
		}
		meta, err := s.GetAccessStats(ctx, "orphan")
		collisionMust(t, err)
		if meta == nil || meta.AccessCount != 7 {
			t.Fatalf("meta: %+v", meta)
		}
		f.unchanged(t, "local", a)
		f.unchanged(t, "acme", b)
	}
}

func TestTenantCollisionRuntimeWrites(t *testing.T) {
	ctx := context.Background()
	cases := []struct {
		name string
		run  func(*testing.T, *TenantStore)
	}{
		{"tags-replace", func(t *testing.T, s *TenantStore) {
			collisionMust(t, s.SetTags(ctx, collisionPath, []string{"replacement"}))
			v, e := s.GetTags(ctx, collisionPath)
			collisionMust(t, e)
			if !reflect.DeepEqual(v, []string{"replacement"}) {
				t.Fatalf("tags: %v", v)
			}
		}},
		{"links-replace", func(t *testing.T, s *TenantStore) {
			collisionMust(t, s.SetLinks(ctx, collisionPath, nil))
			v, e := s.GetLinks(ctx, collisionPath)
			collisionMust(t, e)
			if len(v) != 0 {
				t.Fatal("links not cleared")
			}
		}},
		{"instances-replace", func(t *testing.T, s *TenantStore) {
			collisionMust(t, s.ReplaceInstancesForRunner(ctx, "runner", []InstanceRow{{InstanceID: "instance", Title: "replacement"}, {InstanceID: "new"}}))
			v, e := s.ListInstancesByRunner(ctx, "runner")
			collisionMust(t, e)
			if len(v) != 2 {
				t.Fatalf("instances: %+v", v)
			}
		}},
		{"runner", func(t *testing.T, s *TenantStore) {
			collisionMust(t, s.UpdateRunnerMaxParallel(ctx, "runner", 17))
			v, e := s.GetRunner(ctx, "runner")
			collisionMust(t, e)
			if v.MaxParallel != 17 {
				t.Fatal("runner update missing")
			}
		}},
		{"client", func(t *testing.T, s *TenantStore) {
			collisionMust(t, s.UpsertBrainClient(ctx, &BrainClientRow{ClientID: "client", Hostname: "replacement"}))
			v, e := s.GetBrainClient(ctx, "client")
			collisionMust(t, e)
			if v.Hostname != "replacement" {
				t.Fatal("client update missing")
			}
		}},
		{"workspace", func(t *testing.T, s *TenantStore) {
			collisionMust(t, s.UpsertBrainClientWorkspace(ctx, &BrainClientWorkspaceRow{ClientID: "client", HostID: "host", ProjectID: "p", Path: "/workspace", LastSeen: 42}))
			v, e := s.ListBrainClientWorkspaces(ctx, "p")
			collisionMust(t, e)
			if len(v) != 1 || v[0].LastSeen != 42 {
				t.Fatalf("workspace: %+v", v)
			}
		}},
		{"claim", func(t *testing.T, s *TenantStore) {
			ok, e := s.ReleaseClaim(ctx, "p", "same", "runner")
			collisionMust(t, e)
			if !ok {
				t.Fatal("claim not released")
			}
			v, e := s.GetClaim(ctx, "p", "same")
			collisionMust(t, e)
			if v != nil {
				t.Fatal("claim remains")
			}
		}},
		{"dispatch", func(t *testing.T, s *TenantStore) {
			ok, e := s.ClearDispatchLease(ctx, "p", "same")
			collisionMust(t, e)
			if !ok {
				t.Fatal("lease not cleared")
			}
			v, e := s.GetDispatchLeaseRow(ctx, "p", "same")
			collisionMust(t, e)
			if v != nil {
				t.Fatal("lease remains")
			}
		}},
		{"project-pause", func(t *testing.T, s *TenantStore) {
			collisionMust(t, s.SetAllProjectTasksPaused(ctx, false))
			v, e := s.IsProjectTaskPaused(ctx, "deleted-project")
			collisionMust(t, e)
			if v {
				t.Fatal("still paused")
			}
		}},
		{"feature-pause", func(t *testing.T, s *TenantStore) {
			collisionMust(t, s.SetFeaturePaused(ctx, "deleted-project", "deleted-feature", false))
			v, e := s.IsFeaturePaused(ctx, "deleted-project", "deleted-feature")
			collisionMust(t, e)
			if v {
				t.Fatal("still paused")
			}
		}},
		{"placement", func(t *testing.T, s *TenantStore) {
			v, e := s.GetProjectPlacement(ctx, "p")
			collisionMust(t, e)
			v.Affinity = "strict"
			collisionMust(t, s.UpsertProjectPlacement(ctx, v))
			v, e = s.GetProjectPlacement(ctx, "p")
			collisionMust(t, e)
			if v.Affinity != "strict" {
				t.Fatal("placement update missing")
			}
		}},
		{"metadata", func(t *testing.T, s *TenantStore) {
			collisionMust(t, s.RecordAccess(ctx, "orphan"))
			collisionMust(t, s.SetVerified(ctx, "orphan"))
			v, e := s.GetAccessStats(ctx, "orphan")
			collisionMust(t, e)
			if v.AccessCount != 8 {
				t.Fatalf("access count: %+v", v)
			}
		}},
		{"webhook-cascade", func(t *testing.T, s *TenantStore) {
			collisionMust(t, s.DeleteWebhook(ctx, "hook"))
			v, e := s.ListDeliveries(ctx, "hook", 10)
			collisionMust(t, e)
			if len(v) != 0 {
				t.Fatal("deliveries remain")
			}
		}},
		{"delete-all-notes", func(t *testing.T, s *TenantStore) {
			n, e := s.DeleteAllNotes(ctx)
			collisionMust(t, e)
			if n != 2 {
				t.Fatalf("deleted %d", n)
			}
			v, e := s.SearchNotes(ctx, "collision", &SearchOptions{})
			collisionMust(t, e)
			if len(v) != 0 {
				t.Fatal("FTS remains")
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newCollisionFixture(t)
			foreign, own := f.snapshot(t, "acme"), f.snapshot(t, "local")
			tc.run(t, f.a)
			f.unchanged(t, "acme", foreign)
			if reflect.DeepEqual(own, f.snapshot(t, "local")) {
				t.Fatal("vacuous successful write")
			}
			// No current receiver owns legacy generation state. Even DeleteAllNotes
			// retains it; do not advertise project purge as tenant erasure.
			if got := f.snapshot(t, "local")["generated_tasks"]; !reflect.DeepEqual(got, own["generated_tasks"]) {
				t.Fatal("legacy generation retention changed")
			}
		})
	}
}

func TestTenantCollisionPurgeRollback(t *testing.T) {
	f := newCollisionFixture(t)
	ctx := context.Background()
	a, b := f.snapshot(t, "local"), f.snapshot(t, "acme")
	relationalExec(t, f.owner.db, `CREATE TRIGGER collision_refuse_purge BEFORE DELETE ON opencode_instances WHEN OLD.tenant_id='local' BEGIN SELECT RAISE(ABORT,'collision rollback sentinel'); END`)
	_, err := f.a.PurgeProjectState(ctx, "p")
	if err == nil || !strings.Contains(err.Error(), "collision rollback sentinel") {
		t.Fatalf("wrong failure: %v", err)
	}
	f.unchanged(t, "local", a)
	f.unchanged(t, "acme", b)
	relationalExec(t, f.owner.db, "DROP TRIGGER collision_refuse_purge")
	removed, err := f.a.PurgeProjectState(ctx, "p")
	collisionMust(t, err)
	for _, table := range []string{"task_claims", "task_dispatch_leases", "feature_assignments", "project_placement", "opencode_instances"} {
		if removed[table] != 1 {
			t.Errorf("%s removed=%d", table, removed[table])
		}
	}
	n, err := f.a.DeleteProjectNotes(ctx, "p")
	collisionMust(t, err)
	if n != 2 {
		t.Fatalf("notes removed=%d", n)
	}
	f.unchanged(t, "acme", b)
	for _, table := range []string{"generated_tasks", "event_log", "attachments", "entry_meta"} {
		if !reflect.DeepEqual(a[table], f.snapshot(t, "local")[table]) {
			t.Errorf("retention changed %s", table)
		}
	}
}

func TestTenantCollisionConcurrentWrites(t *testing.T) {
	f := newCollisionFixture(t)
	ctx := context.Background()
	b := f.snapshot(t, "acme")
	// Shared pool, simultaneous API calls; no sleeps and no t.Fatal in workers.
	start := make(chan struct{})
	errs := make(chan error, 16)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); <-start; errs <- f.a.RecordAccess(ctx, "orphan") }()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		collisionMust(t, err)
	}
	v, err := f.a.GetAccessStats(ctx, "orphan")
	collisionMust(t, err)
	if v.AccessCount != 23 {
		t.Fatalf("lost increments: %+v", v)
	}
	f.unchanged(t, "acme", b)
	// Existing claim contention suite additionally tests same-key ownership races.
	collisionMust(t, f.a.RenewClaim(ctx, "p", "same", "runner", time.Now().Add(time.Hour)))
	f.unchanged(t, "acme", b)
}

func TestTenantCollisionForeignRelationships(t *testing.T) {
	f := newCollisionFixture(t)
	ctx := context.Background()
	// Distinct foreign-only logical registry keys supplement the colliding keys.
	collisionMust(t, f.b.UpsertRunner(ctx, &RunnerRow{RunnerID: "foreign-only"}))
	collisionMust(t, f.b.UpsertBrainClient(ctx, &BrainClientRow{ClientID: "foreign-only"}))
	collisionMust(t, f.b.CreateWebhook(ctx, &Webhook{ID: "foreign-only", URL: "https://example.invalid"}))
	a, b := f.snapshot(t, "local"), f.snapshot(t, "acme")
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"attachment", func() error { return f.a.LinkAttachmentToEntry(ctx, collisionPath, 10070, "source") }},
		{"derived", func() error {
			_, e := f.a.UpsertAttachmentDerived(ctx, AttachmentDerivedInput{AttachmentID: 10070, Kind: "text", Status: "ready"})
			return e
		}},
		{"instance", func() error {
			return f.a.UpsertInstance(ctx, &InstanceRow{InstanceID: "bad", RunnerID: "foreign-only"})
		}},
		{"workspace", func() error {
			return f.a.UpsertBrainClientWorkspace(ctx, &BrainClientWorkspaceRow{ClientID: "foreign-only", Path: "/workspace"})
		}},
		{"delivery", func() error { return f.a.CreateDelivery(ctx, &WebhookDelivery{ID: "bad", WebhookID: "foreign-only"}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.run(); err == nil {
				t.Fatal("foreign parent accepted")
			}
			f.unchanged(t, "local", a)
			f.unchanged(t, "acme", b)
		})
	}
}

func TestTenantCollisionReplacementRollback(t *testing.T) {
	f := newCollisionFixture(t)
	ctx := context.Background()
	a, b := f.snapshot(t, "local"), f.snapshot(t, "acme")
	relationalExec(t, f.owner.db, `CREATE TRIGGER collision_refuse_instance BEFORE INSERT ON opencode_instances WHEN NEW.instance_id='fail' BEGIN SELECT RAISE(ABORT,'replacement rollback sentinel'); END`)
	err := f.a.ReplaceInstancesForRunner(ctx, "runner", []InstanceRow{{InstanceID: "new"}, {InstanceID: "fail"}})
	if err == nil || !strings.Contains(err.Error(), "replacement rollback sentinel") {
		t.Fatalf("wrong failure: %v", err)
	}
	f.unchanged(t, "local", a)
	f.unchanged(t, "acme", b)
}

func TestTenantCollisionSnapshotCanonicalOrder(t *testing.T) {
	f := newCollisionFixture(t)
	before := f.snapshot(t, "acme")
	relationalExec(t, f.owner.db, "PRAGMA reverse_unordered_selects=ON")
	f.unchanged(t, "acme", before)
}

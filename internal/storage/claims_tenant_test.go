package storage

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/tenant"
)

func TestTenantClaimsLifecycle(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	ctx := context.Background()
	// The full migration fixture includes a historical local claim.
	if ok, err := a.ReleaseClaim(ctx, "p", "same", "runner"); err != nil || !ok {
		t.Fatalf("fixture cleanup: %v %v", ok, err)
	}
	for _, s := range []*TenantStore{a, b} {
		for _, r := range []string{"same", "other"} {
			if err := s.UpsertRunner(ctx, &RunnerRow{RunnerID: r}); err != nil {
				t.Fatal(err)
			}
		}
		for _, project := range []string{"shared", "second"} {
			ok, old, err := s.ClaimTask(ctx, project, "task", "same", time.Hour)
			if err != nil || !ok || old != nil {
				t.Fatalf("claim %s: %v %+v %v", s.TenantID(), ok, old, err)
			}
		}
	}
	before, err := b.GetClaim(ctx, "shared", "task")
	if err != nil || before == nil {
		t.Fatalf("foreign baseline: %+v %v", before, err)
	}
	if ok, old, err := a.ClaimTask(ctx, "shared", "task", "other", time.Hour); err != nil || ok || old == nil || old.RunnerID != "same" {
		t.Fatalf("loser: %v %+v %v", ok, old, err)
	}
	if ok, old, err := a.ClaimTask(ctx, "shared", "task", "same", 2*time.Hour); err != nil || !ok || old != nil {
		t.Fatalf("reclaim: %v %+v %v", ok, old, err)
	}
	expiry := time.Now().Add(3 * time.Hour)
	if err := a.RenewClaim(ctx, "shared", "task", "same", expiry); err != nil {
		t.Fatal(err)
	}
	if c, err := a.GetClaim(ctx, "shared", "task"); err != nil || c == nil || c.ExpiresAt != expiry.UnixMilli() {
		t.Fatalf("renew: %+v %v", c, err)
	}
	if err := a.RenewClaim(ctx, "shared", "task", "other", expiry); err == nil {
		t.Fatal("wrong runner renewed")
	}
	if ok, err := a.ReleaseClaim(ctx, "shared", "task", "other"); err != nil || ok {
		t.Fatalf("wrong release: %v %v", ok, err)
	}
	if c, err := b.GetClaim(ctx, "shared", "task"); err != nil || !reflect.DeepEqual(c, before) {
		t.Fatalf("foreign changed: %+v %v", c, err)
	}
	if cs, err := a.GetClaimsByRunner(ctx, "same"); err != nil || len(cs) != 2 {
		t.Fatalf("list: %+v %v", cs, err)
	}
	if ok, err := a.ReleaseClaim(ctx, "shared", "task", "same"); err != nil || !ok {
		t.Fatalf("release: %v %v", ok, err)
	}
	if c, err := a.GetClaim(ctx, "shared", "task"); err != nil || c != nil {
		t.Fatalf("released: %+v %v", c, err)
	}
	if n, err := a.ReleaseAllByRunner(ctx, "same"); err != nil || n != 1 {
		t.Fatalf("release all: %d %v", n, err)
	}
	if cs, err := b.GetClaimsByRunner(ctx, "same"); err != nil || len(cs) != 2 {
		t.Fatalf("foreign release all: %+v %v", cs, err)
	}
	for _, s := range []*TenantStore{a, b} {
		if ok, _, err := s.ClaimTask(ctx, "shared", "expired", "same", -time.Hour); err != nil || !ok {
			t.Fatalf("expired seed: %v %v", ok, err)
		}
		if ok, _, err := s.ClaimTask(ctx, "shared", "takeover", "same", -time.Hour); err != nil || !ok {
			t.Fatalf("takeover seed: %v %v", ok, err)
		}
	}
	if ok, _, err := a.ClaimTask(ctx, "shared", "takeover", "other", time.Hour); err != nil || !ok {
		t.Fatalf("takeover: %v %v", ok, err)
	}
	if n, err := a.ExpireStaleClaims(ctx); err != nil || n != 1 {
		t.Fatalf("expire: %d %v", n, err)
	}
	if c, err := a.GetClaim(ctx, "shared", "takeover"); err != nil || c == nil || c.RunnerID != "other" {
		t.Fatalf("live takeover: %+v %v", c, err)
	}
	if c, err := b.GetClaim(ctx, "shared", "expired"); err != nil || c == nil {
		t.Fatalf("foreign expiry: %+v %v", c, err)
	}
	if n, err := b.ExpireStaleClaims(ctx); err != nil || n != 2 {
		t.Fatalf("foreign own expiry: %d %v", n, err)
	}
}

func TestTenantFeatureAssignmentsLifecycle(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	ctx := context.Background()
	for _, s := range []*TenantStore{a, b} {
		for _, r := range []string{"same", "other"} {
			if err := s.UpsertRunner(ctx, &RunnerRow{RunnerID: r}); err != nil {
				t.Fatal(err)
			}
		}
		for _, project := range []string{"shared", "second"} {
			ok, old, err := s.AssignFeatureIfEmpty(ctx, project, "feature", "same", s.TenantID().String(), "active")
			if err != nil || !ok || old != nil {
				t.Fatalf("assign %s: %v %+v %v", s.TenantID(), ok, old, err)
			}
		}
		if snap, err := s.LoadRunnerEligibility(ctx, "shared"); err != nil || len(snap.Runners) < 2 || snap.Placement == nil {
			t.Fatalf("eligibility snapshot %s: %+v %v", s.TenantID(), snap, err)
		}
		if ok, old, err := s.AssignTaskIfEmpty(ctx, "shared", "standalone", "same", "manual", "active"); err != nil || !ok || old != nil {
			t.Fatalf("task assign %s: %v %+v %v", s.TenantID(), ok, old, err)
		}
	}
	taskBefore, err := b.GetTaskAssignment(ctx, "shared", "standalone")
	if err != nil || taskBefore == nil || taskBefore.RunnerID != "same" {
		t.Fatalf("task baseline: %+v %v", taskBefore, err)
	}
	if effective, err := a.ResolveRunnerAssignment(ctx, "shared", "", "standalone"); err != nil || effective == nil || effective.RunnerID != "same" || effective.Scope != "task" {
		t.Fatalf("task effective: %+v %v", effective, err)
	}
	if updated, err := a.ForceAssignTask(ctx, "shared", "standalone", "other", "manual", "active"); err != nil || updated == nil || updated.RunnerID != "other" {
		t.Fatalf("task force: %+v %v", updated, err)
	}
	if ok, err := a.ClearTaskAssignment(ctx, "shared", "standalone"); err != nil || !ok {
		t.Fatalf("task clear: %v %v", ok, err)
	}
	if foreign, err := b.GetTaskAssignment(ctx, "shared", "standalone"); err != nil || !reflect.DeepEqual(foreign, taskBefore) {
		t.Fatalf("foreign task changed: %+v %v", foreign, err)
	}
	before, err := b.GetFeatureAssignment(ctx, "shared", "feature")
	if err != nil || before == nil {
		t.Fatalf("baseline: %+v %v", before, err)
	}
	initial, err := a.GetFeatureAssignment(ctx, "shared", "feature")
	if err != nil || initial == nil {
		t.Fatalf("initial: %+v %v", initial, err)
	}
	if ok, old, err := a.AssignFeatureIfEmpty(ctx, "shared", "feature", "other", "changed", "inactive"); err != nil || ok || !reflect.DeepEqual(old, initial) {
		t.Fatalf("assignment loser: %v %+v %v", ok, old, err)
	}
	updated, err := a.ForceAssignFeature(ctx, "shared", "feature", "other", "manual", "inactive")
	if err != nil || updated == nil || updated.RunnerID != "other" || updated.Source != "manual" || updated.Status != "inactive" || updated.AssignedAt <= initial.AssignedAt || updated.UpdatedAt != updated.AssignedAt {
		t.Fatalf("force: %+v %v", updated, err)
	}
	if fs, err := a.ListFeatureAssignmentsByProject(ctx, "shared"); err != nil || len(fs) != 1 || !reflect.DeepEqual(fs[0], *updated) {
		t.Fatalf("project list: %+v %v", fs, err)
	}
	if fs, err := b.ListFeatureAssignmentsByRunner(ctx, "same"); err != nil || len(fs) != 2 || fs[0].ProjectID != "second" || fs[1].ProjectID != "shared" {
		t.Fatalf("runner list: %+v %v", fs, err)
	}
	if ok, err := a.ClearFeatureAssignment(ctx, "shared", "feature"); err != nil || !ok {
		t.Fatalf("clear: %v %v", ok, err)
	}
	if ok, err := a.ClearFeatureAssignment(ctx, "shared", "feature"); err != nil || ok {
		t.Fatalf("clear missing: %v %v", ok, err)
	}
	if n, err := a.ClearFeatureAssignmentsByRunner(ctx, "same"); err != nil || n != 1 {
		t.Fatalf("cleanup: %d %v", n, err)
	}
	if f, err := b.GetFeatureAssignment(ctx, "shared", "feature"); err != nil || !reflect.DeepEqual(f, before) {
		t.Fatalf("foreign changed: %+v %v", f, err)
	}
	if fs, err := a.ListFeatureAssignmentsByProject(ctx, "second"); err != nil || len(fs) != 0 {
		t.Fatalf("cleanup remaining: %+v %v", fs, err)
	}
}

func TestTenantClaimAssignmentContention(t *testing.T) {
	_, a, b := migratedNoteStores(t)
	ctx := context.Background()
	for _, s := range []*TenantStore{a, b} {
		for i := 0; i < 8; i++ {
			if err := s.UpsertRunner(ctx, &RunnerRow{RunnerID: fmt.Sprint(i)}); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, feature := range []bool{false, true} {
		t.Run(fmt.Sprint("feature=", feature), func(t *testing.T) {
			type result struct {
				s      *TenantStore
				runner string
				won    bool
				old    string
				err    error
			}
			results := make(chan result, 16)
			start := make(chan struct{})
			for _, s := range []*TenantStore{a, b} {
				for i := 0; i < 8; i++ {
					go func(s *TenantStore, runner string) {
						<-start
						r := result{s: s, runner: runner}
						if feature {
							var old *FeatureAssignmentRow
							r.won, old, r.err = s.AssignFeatureIfEmpty(ctx, "race", "same", runner, "auto", "active")
							if old != nil {
								r.old = old.RunnerID
							}
						} else {
							var old *TaskClaimRow
							r.won, old, r.err = s.ClaimTask(ctx, "race", "same", runner, time.Hour)
							if old != nil {
								r.old = old.RunnerID
							}
						}
						results <- r
					}(s, fmt.Sprint(i))
				}
			}
			close(start)
			counts := map[*TenantStore]int{}
			winners := map[*TenantStore]string{}
			var all []result
			for i := 0; i < 16; i++ {
				r := <-results
				all = append(all, r)
				if r.err != nil {
					t.Errorf("contention: %v", r.err)
				}
				if r.won {
					counts[r.s]++
					winners[r.s] = r.runner
				}
			}
			for _, s := range []*TenantStore{a, b} {
				if counts[s] != 1 {
					t.Errorf("tenant %s winners=%d", s.TenantID(), counts[s])
				}
			}
			for _, r := range all {
				if !r.won && r.err == nil && r.old != winners[r.s] {
					t.Errorf("loser lookup=%q want %q", r.old, winners[r.s])
				}
			}
		})
	}
}

func TestTenantClaimAssignmentForeignReferences(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	ctx := context.Background()
	if err := b.UpsertRunner(ctx, &RunnerRow{RunnerID: "foreign"}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*TenantStore{a, b} {
		if err := s.UpsertRunner(ctx, &RunnerRow{RunnerID: "same"}); err != nil {
			t.Fatal(err)
		}
	}
	if ok, _, err := a.ClaimTask(ctx, "shared", "occupied", "same", -time.Hour); err != nil || !ok {
		t.Fatalf("seed: %v %v", ok, err)
	}
	if _, err := a.ForceAssignFeature(ctx, "shared", "occupied", "same", "auto", "active"); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"missing", "occupied"} {
		if ok, _, err := a.ClaimTask(ctx, "shared", key, "foreign", time.Hour); err == nil || ok {
			t.Fatalf("foreign claim accepted %s: %v %v", key, ok, err)
		}
		if _, err := a.ForceAssignFeature(ctx, "shared", key, "foreign", "auto", "active"); err == nil {
			t.Fatalf("foreign force accepted %s", key)
		}
	}
	if ok, _, err := a.AssignFeatureIfEmpty(ctx, "shared", "missing", "foreign", "auto", "active"); err == nil || ok {
		t.Fatalf("foreign assign accepted: %v %v", ok, err)
	}
	if c, err := a.GetClaim(ctx, "shared", "occupied"); err != nil || c == nil || c.RunnerID != "same" {
		t.Fatalf("foreign takeover changed row: %+v %v", c, err)
	}
	if f, err := a.GetFeatureAssignment(ctx, "shared", "occupied"); err != nil || f == nil || f.RunnerID != "same" {
		t.Fatalf("foreign force changed row: %+v %v", f, err)
	}
	var n int
	if err := owner.db.QueryRow("SELECT count(*) FROM tenant_runner_keys WHERE tenant_id='local' AND runner_id='foreign'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("manufactured key: %d %v", n, err)
	}
	if ok, err := b.DeleteRunner(ctx, "foreign"); err != nil || !ok {
		t.Fatalf("deregister: %v %v", ok, err)
	}
	if ok, _, err := b.ClaimTask(ctx, "shared", "historical", "foreign", time.Hour); err != nil || !ok {
		t.Fatalf("durable claim: %v %v", ok, err)
	}
	if ok, _, err := b.AssignFeatureIfEmpty(ctx, "shared", "historical", "foreign", "auto", "active"); err != nil || !ok {
		t.Fatalf("durable assignment: %v %v", ok, err)
	}
}

func TestTenantClaimAssignmentScopeGuards(t *testing.T) {
	ops := []struct {
		name string
		run  func(*TenantStore, context.Context) error
	}{
		{"ClaimTask", func(s *TenantStore, c context.Context) error {
			_, _, e := s.ClaimTask(c, "p", "t", "r", time.Hour)
			return e
		}},
		{"ReleaseClaim", func(s *TenantStore, c context.Context) error { _, e := s.ReleaseClaim(c, "p", "t", "r"); return e }},
		{"GetClaim", func(s *TenantStore, c context.Context) error { _, e := s.GetClaim(c, "p", "t"); return e }},
		{"GetClaimsByRunner", func(s *TenantStore, c context.Context) error { _, e := s.GetClaimsByRunner(c, "r"); return e }},
		{"ExpireStaleClaims", func(s *TenantStore, c context.Context) error { _, e := s.ExpireStaleClaims(c); return e }},
		{"ReleaseAllByRunner", func(s *TenantStore, c context.Context) error { _, e := s.ReleaseAllByRunner(c, "r"); return e }},
		{"RenewClaim", func(s *TenantStore, c context.Context) error { return s.RenewClaim(c, "p", "t", "r", time.Now()) }},
		{"AssignFeatureIfEmpty", func(s *TenantStore, c context.Context) error {
			_, _, e := s.AssignFeatureIfEmpty(c, "p", "f", "r", "auto", "active")
			return e
		}},
		{"ForceAssignFeature", func(s *TenantStore, c context.Context) error {
			_, e := s.ForceAssignFeature(c, "p", "f", "r", "auto", "active")
			return e
		}},
		{"GetFeatureAssignment", func(s *TenantStore, c context.Context) error { _, e := s.GetFeatureAssignment(c, "p", "f"); return e }},
		{"ClearFeatureAssignment", func(s *TenantStore, c context.Context) error { _, e := s.ClearFeatureAssignment(c, "p", "f"); return e }},
		{"ClearFeatureAssignmentsByRunner", func(s *TenantStore, c context.Context) error {
			_, e := s.ClearFeatureAssignmentsByRunner(c, "r")
			return e
		}},
		{"ListFeatureAssignmentsByRunner", func(s *TenantStore, c context.Context) error {
			_, e := s.ListFeatureAssignmentsByRunner(c, "r")
			return e
		}},
		{"ListFeatureAssignmentsByProject", func(s *TenantStore, c context.Context) error {
			_, e := s.ListFeatureAssignmentsByProject(c, "p")
			return e
		}},
	}
	for _, op := range ops {
		t.Run(op.name, func(t *testing.T) {
			// Isolate methods so a regressed guard cannot corrupt another case's
			// fixture. Run nil context last: database/sql may retain a connection
			// if an unguarded call panics after acquiring it.
			owner := newTestStorage(t)
			local, err := owner.ForTenant(tenant.Local)
			if err != nil {
				t.Fatal(err)
			}
			foreign, err := owner.ForTenant(tenant.MustParse("acme"))
			if err != nil {
				t.Fatal(err)
			}
			if ok, _, err := local.ClaimTask(context.Background(), "p", "t", "r", time.Hour); err != nil || !ok {
				t.Fatalf("local guard seed: %v %v", ok, err)
			}
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
					if err := op.run(tc.s, tc.c); err == nil {
						t.Fatal("invalid scope accepted")
					}
				})
			}
		})
	}
}

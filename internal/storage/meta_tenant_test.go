package storage

import (
	"context"
	"github.com/huynle/brain-api/internal/tenant"
	"testing"
)

func TestTenantPhaseOneIsolation(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	ctx := context.Background()
	const path = "projects/phaseone/task/same.md"
	for _, id := range []string{"local", "acme"} {
		if _, err := owner.db.Exec(`INSERT INTO notes(tenant_id,path,title,short_id,type,status,project_id,metadata) VALUES(?,?,?,'phase001','task','in_progress','phaseone','{"trigger":{"event":"other.event"}}')`, id, path, id); err != nil {
			t.Fatal(err)
		}
	}
	t.Run("access", func(t *testing.T) {
		for i, s := range []*TenantStore{a, b} {
			for j := 0; j <= i; j++ {
				if err := s.RecordAccess(ctx, path); err != nil {
					t.Fatalf("tenant access write: %v", err)
				}
			}
		}
		for i, s := range []*TenantStore{a, b} {
			m, err := s.GetAccessStats(ctx, path)
			if err != nil || m == nil || m.AccessCount != i+1 {
				t.Fatalf("isolated access: %+v %v", m, err)
			}
		}
	})
	t.Run("verification", func(t *testing.T) {
		if err := a.SetVerified(ctx, path); err != nil {
			t.Fatalf("tenant verification write: %v", err)
		}
		for i, s := range []*TenantStore{a, b} {
			rows, err := s.GetStaleEntries(ctx, 30, &StaleOptions{Path: "projects/phaseone/", Type: "task"})
			if err != nil || len(rows) != i {
				t.Fatalf("stale tenant %d: %d %v", i, len(rows), err)
			}
		}
	})
	t.Run("triggers", func(t *testing.T) {
		for _, s := range []*TenantStore{a, b} {
			rows, err := s.ListTriggeredTasks(ctx)
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for _, n := range rows {
				if n.Path == path {
					count++
					if n.Title != s.TenantID().String() {
						t.Error("foreign trigger")
					}
				}
			}
			if count != 1 {
				t.Errorf("same-path triggers=%d", count)
			}
			n, err := s.CountInProgressByTrigger(ctx, "ignored.event", "phaseone")
			if err != nil || n != 1 {
				t.Errorf("count=%d %v", n, err)
			}
		}
	})
	t.Run("activation", func(t *testing.T) {
		if err := a.ActivateTask(ctx, path, map[string]interface{}{"status": "pending"}); err != nil {
			t.Fatalf("bound activation: %v", err)
		}
		n, err := b.GetNoteByPath(ctx, path)
		if err != nil || n.Status == nil || *n.Status != "in_progress" {
			t.Fatalf("foreign activation: %+v %v", n, err)
		}
	})
}

func TestTenantPhaseOneSchema28(t *testing.T) {
	owner := newTestStorage(t)
	local, err := owner.ForTenant(tenant.Local)
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := owner.ForTenant(tenant.MustParse("acme"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := local.RecordAccess(ctx, "missing"); err != nil {
		t.Fatal(err)
	}
	if err := local.SetVerified(ctx, "missing"); err != nil {
		t.Fatal(err)
	}
	m, err := local.GetAccessStats(ctx, "missing")
	if err != nil || m == nil || m.AccessCount != 1 || m.LastVerified == nil {
		t.Fatalf("legacy telemetry %+v %v", m, err)
	}
	for _, s := range []*TenantStore{foreign, nil, {}} {
		if err := s.RecordAccess(ctx, "missing"); err == nil {
			t.Error("access admitted")
		}
		if err := s.SetVerified(ctx, "missing"); err == nil {
			t.Error("verify admitted")
		}
		if _, err := s.GetAccessStats(ctx, "missing"); err == nil {
			t.Error("access read admitted")
		}
		if _, err := s.GetStaleEntries(ctx, 30, nil); err == nil {
			t.Error("stale admitted")
		}
		if _, err := s.GetStats(ctx, nil); err == nil {
			t.Error("stats admitted")
		}
		if _, err := s.ListTriggeredTasks(ctx); err == nil {
			t.Error("trigger list admitted")
		}
		if _, err := s.CountInProgressByTrigger(ctx, "", ""); err == nil {
			t.Error("trigger count admitted")
		}
		if err := s.ActivateTask(ctx, "missing", nil); err == nil {
			t.Error("activation admitted")
		}
	}
}

func TestTenantPhaseOneStats(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	ctx := context.Background()
	for _, id := range []string{"local", "acme"} {
		for _, p := range []string{"projects/phaseone/note/a.md", "global/note/phaseone.md"} {
			if _, err := owner.db.Exec(`INSERT INTO notes(tenant_id,path,title,short_id,type) VALUES(?,?,?,?,'note')`, id, p, id, p); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Foreign unresolved inbound links must not hide local orphans.
	if _, err := owner.db.Exec(`INSERT INTO links(tenant_id,source_id,target_path,href) SELECT 'acme',id,'global/note/phaseone.md','global/note/phaseone.md' FROM notes WHERE tenant_id='acme' AND path='projects/phaseone/note/a.md'`); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.db.Exec(`INSERT INTO entry_meta(tenant_id,path,access_count,last_verified) VALUES('acme','global/note/phaseone.md',3,datetime('now'))`); err != nil {
		t.Fatal(err)
	}
	for i, s := range []*TenantStore{a, b} {
		stats, err := s.GetStats(ctx, &StatsOptions{Paths: []string{"projects/phaseone/", "global/note/phaseone"}})
		if err != nil {
			t.Fatal(err)
		}
		if stats.TotalNotes != 2 || stats.ByType["note"] != 2 || stats.OrphanCount != 2-i || stats.TrackedCount != i || stats.StaleCount != 2-i {
			t.Fatalf("tenant %d stats: %+v", i, stats)
		}
	}
}

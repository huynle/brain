package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/huynle/brain-api/internal/tenant"
)

func TestTenantEventsIsolation(t *testing.T) {
	owner, a, b := migratedNoteStores(t)
	ctx := context.Background()
	// The migration fixture contains legacy events; isolate this test's history.
	if _, err := owner.db.Exec("DELETE FROM event_log"); err != nil {
		t.Fatal(err)
	}
	ids := make(map[*TenantStore][]int64)
	for i := 0; i < 3; i++ {
		for _, s := range []*TenantStore{a, b} {
			id, err := s.InsertEvent(ctx, "reminder.fired", fmt.Sprintf(`{"n":%d}`, i), fmt.Sprintf("reminder:same-id:same-time:%d", i), "reminder")
			if err != nil {
				t.Fatalf("tenant %s insert: %v", s.tenantID, err)
			}
			ids[s] = append(ids[s], id)
			if _, err := s.InsertEvent(ctx, "reminder.fired", "{}", fmt.Sprintf("reminder:same-id:same-time:%d", i), "reminder"); err == nil {
				t.Fatal("duplicate accepted")
			}
		}
	}
	// Force timestamp ties to exercise the id tie-breakers.
	if _, err := owner.db.Exec("UPDATE event_log SET created_at='2026-09-08 00:00:00'"); err != nil {
		t.Fatal(err)
	}
	for _, s := range []*TenantStore{a, b} {
		rows, err := s.GetEventsByType(ctx, "reminder.fired", 2)
		if err != nil || len(rows) != 2 {
			t.Fatalf("history: %v %v", rows, err)
		}
		if rows[0].ID != ids[s][2] || rows[1].ID != ids[s][1] {
			t.Fatalf("foreign or unordered history: %v", rows)
		}
		rows, err = s.GetUnprocessed(ctx)
		if err != nil || len(rows) != 3 {
			t.Fatalf("FIFO: %v %v", rows, err)
		}
		for i, row := range rows {
			if row.ID != ids[s][i] {
				t.Fatal("foreign or unordered FIFO")
			}
		}
	}
	if err := a.MarkProcessed(ctx, ids[b][0]); err == nil {
		t.Fatal("foreign mark accepted")
	}
	rows, err := b.GetUnprocessed(ctx)
	if err != nil || len(rows) != 3 || rows[0].ProcessedAt != nil {
		t.Fatalf("foreign mark mutated B: %v %v", rows, err)
	}
	if err := b.MarkProcessed(ctx, ids[b][0]); err != nil {
		t.Fatal(err)
	}
	rows, err = b.GetUnprocessed(ctx)
	if err != nil || len(rows) != 2 || rows[0].ID != ids[b][1] {
		t.Fatalf("owned mark: %v %v", rows, err)
	}
	for _, s := range []*TenantStore{a, b} {
		for i := 0; i < 2; i++ {
			if _, err := s.InsertEvent(ctx, "no-dedup", "{}", "", "api"); err != nil {
				t.Fatal(err)
			}
		}
		rows, err := s.GetEventsByType(ctx, "no-dedup", 0)
		if err != nil || len(rows) != 2 {
			t.Fatalf("empty dedup: %v %v", rows, err)
		}
		for _, row := range rows {
			if row.DedupKey != nil {
				t.Fatal("empty dedup not NULL")
			}
		}
	}
}

func TestTenantEventsGuards(t *testing.T) {
	operations := map[string]func(*TenantStore, context.Context) error{
		"insert": func(s *TenantStore, ctx context.Context) error {
			_, err := s.InsertEvent(ctx, "", "", "", "")
			return err
		},
		"mark":    func(s *TenantStore, ctx context.Context) error { return s.MarkProcessed(ctx, 0) },
		"history": func(s *TenantStore, ctx context.Context) error { _, err := s.GetEventsByType(ctx, "", 0); return err },
		"FIFO":    func(s *TenantStore, ctx context.Context) error { _, err := s.GetUnprocessed(ctx); return err },
	}
	for name, op := range operations {
		t.Run(name, func(t *testing.T) {
			owner := newTestStorage(t)
			local, _ := owner.ForTenant(tenant.Local)
			foreign, _ := owner.ForTenant(tenant.MustParse("acme"))
			if err := op(foreign, context.Background()); err == nil {
				t.Fatal("v28 foreign accepted")
			}
			for _, invalid := range []*TenantStore{nil, {}, {StorageLayer: owner}} {
				if err := op(invalid, context.Background()); err == nil {
					t.Fatal("invalid handle accepted")
				}
			}
			if err := op(local, nil); err == nil {
				t.Fatal("nil context accepted")
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			if err := op(local, ctx); !errors.Is(err, context.Canceled) {
				t.Fatalf("canceled: %v", err)
			}
			if _, err := owner.db.Exec("UPDATE schema_version SET version=30"); err != nil {
				t.Fatal(err)
			}
			if err := op(local, context.Background()); err == nil {
				t.Fatal("unsupported schema accepted")
			}
		})
	}
}

func TestTenantEventsV29NoFallback(t *testing.T) {
	owner, a, _ := migratedNoteStores(t)
	if _, err := owner.db.Exec("DROP TABLE event_log; CREATE TABLE event_log(id INTEGER PRIMARY KEY,event_type TEXT,payload TEXT,dedup_key TEXT,source TEXT,created_at TEXT,processed_at TEXT)"); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := a.InsertEvent(ctx, "test", "{}", "", "api"); err == nil {
		t.Fatal("v29 insert fell back to unowned schema")
	}
	if err := a.MarkProcessed(ctx, 1); err == nil {
		t.Fatal("v29 mark fell back")
	}
	if _, err := a.GetEventsByType(ctx, "test", 1); err == nil {
		t.Fatal("v29 history fell back")
	}
	if _, err := a.GetUnprocessed(ctx); err == nil {
		t.Fatal("v29 FIFO fell back")
	}
}

package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/tenant"
	"github.com/huynle/brain-api/internal/types"
)

func attnStore(t *testing.T) (*TenantStore, context.Context) {
	t.Helper()
	owner, err := New(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = owner.Close() })
	store, _ := owner.ForTenant(tenant.Local)
	ctx := tenant.Into(context.Background(), tenant.Local)
	return store, ctx
}

func TestAttentionCreateAndGet(t *testing.T) {
	store, ctx := attnStore(t)
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	item := &types.Attention{
		ID: "a1", Recipient: "alice", Kind: "task_blocked",
		Severity: types.AttentionSeverityWarning, Title: "Task blocked",
		Project: "canis", TaskID: "t1", State: types.AttentionStateUnread,
	}
	created, err := store.CreateAttention(ctx, item, now)
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected created=true")
	}
	got, err := store.GetAttention(ctx, "alice", "a1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Title != "Task blocked" || got.State != types.AttentionStateUnread {
		t.Fatalf("unexpected get: %+v", got)
	}
	if got.Revision != 1 {
		t.Fatalf("revision = %d, want 1", got.Revision)
	}
}

func TestAttentionDedupIsIdempotentPerRecipient(t *testing.T) {
	store, ctx := attnStore(t)
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	mk := func(id, recipient string) *types.Attention {
		return &types.Attention{ID: id, Recipient: recipient, Kind: "k", Title: "t",
			Severity: types.AttentionSeverityInfo, State: types.AttentionStateUnread, DedupKey: "dk"}
	}
	if created, err := store.CreateAttention(ctx, mk("a1", "alice"), now); err != nil || !created {
		t.Fatal(created, err)
	}
	// Same recipient + same dedup key => not created again.
	created, err := store.CreateAttention(ctx, mk("a2", "alice"), now)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected dedup to suppress the second create for alice")
	}
	// A different recipient with the same dedup key IS its own item.
	if created, err := store.CreateAttention(ctx, mk("a3", "bob"), now); err != nil || !created {
		t.Fatalf("bob should get his own item: created=%v err=%v", created, err)
	}
}

func TestAttentionListScopedToRecipientAndState(t *testing.T) {
	store, ctx := attnStore(t)
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	seed := []*types.Attention{
		{ID: "a1", Recipient: "alice", Kind: "k", Title: "unread", Severity: "info", State: types.AttentionStateUnread},
		{ID: "a2", Recipient: "alice", Kind: "k", Title: "resolved", Severity: "info", State: types.AttentionStateResolved},
		{ID: "b1", Recipient: "bob", Kind: "k", Title: "bobs", Severity: "info", State: types.AttentionStateUnread},
	}
	for _, s := range seed {
		if _, err := store.CreateAttention(ctx, s, now); err != nil {
			t.Fatal(err)
		}
	}
	got, err := store.ListAttention(ctx, types.AttentionListFilter{Recipient: "alice", State: types.AttentionStateUnread})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "a1" {
		t.Fatalf("expected only alice's unread, got %+v", got)
	}
}

func TestAttentionTransitionUsesRevision(t *testing.T) {
	store, ctx := attnStore(t)
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	if _, err := store.CreateAttention(ctx, &types.Attention{
		ID: "a1", Recipient: "alice", Kind: "k", Title: "t", Severity: "info", State: types.AttentionStateUnread,
	}, now); err != nil {
		t.Fatal(err)
	}
	// Stale revision must be refused.
	if ok, err := store.TransitionAttention(ctx, "alice", "a1", types.AttentionStateRead, "", 99, now); err != nil || ok {
		t.Fatalf("stale revision should not apply: ok=%v err=%v", ok, err)
	}
	// Correct revision applies and bumps revision.
	ok, err := store.TransitionAttention(ctx, "alice", "a1", types.AttentionStateRead, "", 1, now)
	if err != nil || !ok {
		t.Fatalf("expected transition to apply: ok=%v err=%v", ok, err)
	}
	got, _ := store.GetAttention(ctx, "alice", "a1")
	if got.State != types.AttentionStateRead || got.Revision != 2 || got.ReadAt == "" {
		t.Fatalf("unexpected post-transition: %+v", got)
	}
}

func TestAttentionCounts(t *testing.T) {
	store, ctx := attnStore(t)
	now := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	seed := []*types.Attention{
		{ID: "a1", Recipient: "alice", Kind: "k", Title: "1", Severity: "info", State: types.AttentionStateUnread},
		{ID: "a2", Recipient: "alice", Kind: "k", Title: "2", Severity: "critical", State: types.AttentionStateUnread},
		{ID: "a3", Recipient: "alice", Kind: "k", Title: "3", Severity: "info", State: types.AttentionStateResolved},
	}
	for _, s := range seed {
		if _, err := store.CreateAttention(ctx, s, now); err != nil {
			t.Fatal(err)
		}
	}
	c, err := store.AttentionCounts(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if c.Unread != 2 || c.Critical != 1 || c.Total != 3 {
		t.Fatalf("unexpected counts: %+v", c)
	}
}

func TestAttentionMigrationFrom30(t *testing.T) {
	path := filepath.Join(t.TempDir(), "brain.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.DB().Exec("DROP TABLE attention_items; DELETE FROM schema_version; INSERT INTO schema_version(version) VALUES(30)"); err != nil {
		t.Fatal(err)
	}
	if err = migrateSchema(s.DB()); err != nil {
		t.Fatal(err)
	}
	var n int
	if err = s.DB().QueryRow("SELECT count(*) FROM attention_items").Scan(&n); err != nil {
		t.Fatalf("attention_items missing after v31 migration: %v", err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = New(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if version, err := GetSchemaVersion(s.DB()); err != nil || version != CurrentSchemaVersion {
		t.Fatalf("version %d: %v", version, err)
	}
}

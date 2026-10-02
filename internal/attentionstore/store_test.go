package attentionstore

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

func newStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "attention.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, context.Background()
}

func TestCreateAndGet(t *testing.T) {
	s, ctx := newStore(t)
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	item := &types.Attention{
		ID: "a1", Recipient: "alice", Kind: "task_blocked",
		Severity: types.AttentionSeverityWarning, Title: "Task blocked",
		Project: "canis", TaskID: "t1", State: types.AttentionStateUnread,
	}
	created, err := s.CreateAttention(ctx, item, now)
	if err != nil || !created {
		t.Fatalf("create: %v created=%v", err, created)
	}
	got, err := s.GetAttention(ctx, "alice", "a1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Title != "Task blocked" || got.State != types.AttentionStateUnread || got.Revision != 1 {
		t.Fatalf("unexpected get: %+v", got)
	}
}

func TestDedupIsIdempotentPerRecipient(t *testing.T) {
	s, ctx := newStore(t)
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	mk := func(id, recipient string) *types.Attention {
		return &types.Attention{ID: id, Recipient: recipient, Kind: "k", Title: "t",
			Severity: types.AttentionSeverityInfo, State: types.AttentionStateUnread, DedupKey: "dk"}
	}
	if created, err := s.CreateAttention(ctx, mk("a1", "alice"), now); err != nil || !created {
		t.Fatal(created, err)
	}
	created, err := s.CreateAttention(ctx, mk("a2", "alice"), now)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("expected dedup to suppress the second create for alice")
	}
	if created, err := s.CreateAttention(ctx, mk("a3", "bob"), now); err != nil || !created {
		t.Fatalf("bob should get his own item: created=%v err=%v", created, err)
	}
}

func TestListScopedToRecipientAndState(t *testing.T) {
	s, ctx := newStore(t)
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	seed := []*types.Attention{
		{ID: "a1", Recipient: "alice", Kind: "k", Title: "unread", Severity: "info", State: types.AttentionStateUnread},
		{ID: "a2", Recipient: "alice", Kind: "k", Title: "resolved", Severity: "info", State: types.AttentionStateResolved},
		{ID: "b1", Recipient: "bob", Kind: "k", Title: "bobs", Severity: "info", State: types.AttentionStateUnread},
	}
	for _, it := range seed {
		if _, err := s.CreateAttention(ctx, it, now); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.ListAttention(ctx, types.AttentionListFilter{Recipient: "alice", State: types.AttentionStateUnread})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "a1" {
		t.Fatalf("expected only alice's unread, got %+v", got)
	}
}

func TestTransitionUsesRevision(t *testing.T) {
	s, ctx := newStore(t)
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	if _, err := s.CreateAttention(ctx, &types.Attention{
		ID: "a1", Recipient: "alice", Kind: "k", Title: "t", Severity: "info", State: types.AttentionStateUnread,
	}, now); err != nil {
		t.Fatal(err)
	}
	if ok, err := s.TransitionAttention(ctx, "alice", "a1", types.AttentionStateRead, "", 99, now); err != nil || ok {
		t.Fatalf("stale revision should not apply: ok=%v err=%v", ok, err)
	}
	ok, err := s.TransitionAttention(ctx, "alice", "a1", types.AttentionStateRead, "", 1, now)
	if err != nil || !ok {
		t.Fatalf("expected transition to apply: ok=%v err=%v", ok, err)
	}
	got, _ := s.GetAttention(ctx, "alice", "a1")
	if got.State != types.AttentionStateRead || got.Revision != 2 || got.ReadAt == "" {
		t.Fatalf("unexpected post-transition: %+v", got)
	}
}

func TestCounts(t *testing.T) {
	s, ctx := newStore(t)
	now := time.Date(2026, 9, 30, 10, 0, 0, 0, time.UTC)
	seed := []*types.Attention{
		{ID: "a1", Recipient: "alice", Kind: "k", Title: "1", Severity: "info", State: types.AttentionStateUnread},
		{ID: "a2", Recipient: "alice", Kind: "k", Title: "2", Severity: "critical", State: types.AttentionStateUnread},
		{ID: "a3", Recipient: "alice", Kind: "k", Title: "3", Severity: "info", State: types.AttentionStateResolved},
	}
	for _, it := range seed {
		if _, err := s.CreateAttention(ctx, it, now); err != nil {
			t.Fatal(err)
		}
	}
	c, err := s.AttentionCounts(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if c.Unread != 2 || c.Critical != 1 || c.Total != 3 {
		t.Fatalf("unexpected counts: %+v", c)
	}
}

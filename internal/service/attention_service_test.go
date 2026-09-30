package service

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/huynle/brain-api/internal/attentionstore"
	"github.com/huynle/brain-api/internal/types"
)

type captureIngester struct{ events []types.Event }

func (c *captureIngester) Ingest(_ context.Context, ev []types.Event) error {
	c.events = append(c.events, ev...)
	return nil
}

func newAttentionService(t *testing.T) (*AttentionService, context.Context, *captureIngester) {
	t.Helper()
	store, err := attentionstore.Open(filepath.Join(t.TempDir(), "attention.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	cap := &captureIngester{}
	svc := NewAttentionService(store, WithAttentionEventIngester(cap))
	return svc, ctx, cap
}

func TestAttentionService_CreateDefaultsRecipientAndEmits(t *testing.T) {
	svc, ctx, cap := newAttentionService(t)
	got, err := svc.CreateAttention(ctx, "alice", types.CreateAttentionRequest{
		Kind: "task_blocked", Title: "Blocked", Project: "canis", TaskID: "t1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Recipient != "alice" {
		t.Fatalf("recipient = %q, want alice (caller default)", got.Recipient)
	}
	if got.Severity != types.AttentionSeverityInfo || got.State != types.AttentionStateUnread {
		t.Fatalf("unexpected defaults: %+v", got)
	}
	if got.ID == "" {
		t.Fatal("expected a generated id")
	}
	if len(cap.events) != 1 || cap.events[0].Type != types.EventAttentionCreated {
		t.Fatalf("expected one attention.created event, got %+v", cap.events)
	}
}

func TestAttentionService_ExplicitRecipientOverridesCaller(t *testing.T) {
	svc, ctx, _ := newAttentionService(t)
	got, err := svc.CreateAttention(ctx, "alice", types.CreateAttentionRequest{
		Recipient: "bob", Kind: "k", Title: "for bob",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Recipient != "bob" {
		t.Fatalf("recipient = %q, want bob (explicit)", got.Recipient)
	}
}

func TestAttentionService_DedupReturnsExisting(t *testing.T) {
	svc, ctx, _ := newAttentionService(t)
	first, err := svc.CreateAttention(ctx, "alice", types.CreateAttentionRequest{
		Kind: "k", Title: "one", DedupKey: "dk",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateAttention(ctx, "alice", types.CreateAttentionRequest{
		Kind: "k", Title: "two", DedupKey: "dk",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID {
		t.Fatalf("dedup should return the existing item: %s vs %s", second.ID, first.ID)
	}
	if second.Title != "one" {
		t.Fatalf("existing item should be unchanged, got title %q", second.Title)
	}
}

func TestAttentionService_TransitionByIDReadsRevisionAndEmits(t *testing.T) {
	svc, ctx, cap := newAttentionService(t)
	item, err := svc.CreateAttention(ctx, "alice", types.CreateAttentionRequest{Kind: "k", Title: "t"})
	if err != nil {
		t.Fatal(err)
	}
	cap.events = nil
	updated, err := svc.SetAttentionState(ctx, "alice", item.ID, types.AttentionStateResolved, "")
	if err != nil {
		t.Fatal(err)
	}
	if updated.State != types.AttentionStateResolved || updated.ResolvedAt == "" {
		t.Fatalf("unexpected updated item: %+v", updated)
	}
	if len(cap.events) != 1 || cap.events[0].Type != types.EventAttentionUpdated {
		t.Fatalf("expected one attention.updated event, got %+v", cap.events)
	}
}

func TestAttentionService_TransitionUnknownItemErrors(t *testing.T) {
	svc, ctx, _ := newAttentionService(t)
	if _, err := svc.SetAttentionState(ctx, "alice", "nope", types.AttentionStateRead, ""); err == nil {
		t.Fatal("expected an error transitioning a missing item")
	}
}

func TestAttentionService_CountsAndList(t *testing.T) {
	svc, ctx, _ := newAttentionService(t)
	for _, title := range []string{"a", "b"} {
		if _, err := svc.CreateAttention(ctx, "alice", types.CreateAttentionRequest{Kind: "k", Title: title}); err != nil {
			t.Fatal(err)
		}
	}
	list, err := svc.ListAttention(ctx, types.AttentionListFilter{Recipient: "alice"})
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("want 2 items, got %d", len(list))
	}
	counts, err := svc.AttentionCounts(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if counts.Unread != 2 {
		t.Fatalf("want 2 unread, got %d", counts.Unread)
	}
}

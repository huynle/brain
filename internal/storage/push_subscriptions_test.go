package storage

import (
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestPushSubscriptionUpsertListDelete(t *testing.T) {
	store, ctx := attnStore(t)
	sub := types.PushSubscription{
		ID: "s1", Recipient: "alice", Endpoint: "https://push.example/abc",
		P256dh: "p256", Auth: "auth",
	}
	if err := store.UpsertPushSubscription(ctx, sub); err != nil {
		t.Fatal(err)
	}
	// Idempotent upsert by endpoint: same endpoint updates, does not duplicate.
	sub.ID = "s2"
	sub.Auth = "auth2"
	if err := store.UpsertPushSubscription(ctx, sub); err != nil {
		t.Fatal(err)
	}
	subs, err := store.ListPushSubscriptions(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if len(subs) != 1 {
		t.Fatalf("want 1 subscription (upsert by endpoint), got %d", len(subs))
	}
	if subs[0].Auth != "auth2" {
		t.Fatalf("upsert did not update auth, got %q", subs[0].Auth)
	}
	// Another recipient is isolated.
	if err := store.UpsertPushSubscription(ctx, types.PushSubscription{
		ID: "b1", Recipient: "bob", Endpoint: "https://push.example/bob", P256dh: "p", Auth: "a",
	}); err != nil {
		t.Fatal(err)
	}
	if subs, _ := store.ListPushSubscriptions(ctx, "alice"); len(subs) != 1 {
		t.Fatalf("alice should still have 1, got %d", len(subs))
	}
	// Delete by endpoint.
	if err := store.DeletePushSubscription(ctx, "alice", "https://push.example/abc"); err != nil {
		t.Fatal(err)
	}
	if subs, _ := store.ListPushSubscriptions(ctx, "alice"); len(subs) != 0 {
		t.Fatalf("want 0 after delete, got %d", len(subs))
	}
}

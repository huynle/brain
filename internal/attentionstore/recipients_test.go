package attentionstore

import (
	"reflect"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

func TestListRecipientsReturnsDistinctSortedRecipients(t *testing.T) {
	s, ctx := newStore(t)
	now := time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC)
	for i, recipient := range []string{"bob", "alice", "bob", "carol"} {
		item := &types.Attention{
			ID: "item" + string(rune('a'+i)), Recipient: recipient,
			Kind: "k", Title: "t",
		}
		if created, err := s.CreateAttention(ctx, item, now); err != nil || !created {
			t.Fatalf("create %d: created=%v err=%v", i, created, err)
		}
	}

	got, err := s.ListRecipients(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alice", "bob", "carol"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListRecipients() = %v, want %v", got, want)
	}
}

func TestListRecipientsEmptyStore(t *testing.T) {
	s, ctx := newStore(t)
	got, err := s.ListRecipients(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("ListRecipients() on empty store = %v, want none", got)
	}
}

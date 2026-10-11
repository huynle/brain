package service

import (
	"context"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/attentionstore"
	phonepush "github.com/huynle/brain-api/internal/phonepush"
	"github.com/huynle/brain-api/internal/types"
)

type recordingEnqueuer struct {
	mu     sync.Mutex
	owners []string
}

func (r *recordingEnqueuer) Enqueue(owner, kind, event string, at time.Time, m phonepush.PushMessage) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.owners = append(r.owners, owner)
	return nil
}

func dispatchAttentionTo(t *testing.T, recipient string) []string {
	t.Helper()
	store, err := attentionstore.Open(filepath.Join(t.TempDir(), "attention.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	item := &types.Attention{ID: "att1", Recipient: recipient, Kind: "scheduling_report", Severity: "warning", Title: "T", State: "unread"}
	if _, err := store.CreateAttention(context.Background(), item, time.Now()); err != nil {
		t.Fatal(err)
	}
	push := &recordingEnqueuer{}
	d := NewAttentionDispatcher(nil, store, push)
	d.deliver(context.Background(), types.Event{
		Type:     types.EventAttentionCreated,
		Metadata: map[string]string{"recipient": recipient, "attention_id": "att1"},
	})
	sort.Strings(push.owners)
	return push.owners
}

// Devices are registered under the caller's full principal
// ("local:<auth type>:<name>", see api.jobOwner), while attention items are
// addressed to the bare name (api.attentionRecipient). Delivery must reach the
// devices of that name under every auth type; an exact-string match reached
// none, so no attention push was ever delivered.
func TestAttentionDispatcherPushesToTheRecipientsDeviceOwners(t *testing.T) {
	got := dispatchAttentionTo(t, "brain_password_session")
	want := []string{
		"local:api_token:brain_password_session",
		"local:jwt:brain_password_session",
		"local:local:brain_password_session",
		"local:oauth:brain_password_session",
	}
	if len(got) != len(want) {
		t.Fatalf("enqueued owners = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("enqueued owners = %v, want %v", got, want)
		}
	}
}

// A recipient already written as a full principal is used as is.
func TestAttentionDispatcherKeepsAFullPrincipalRecipient(t *testing.T) {
	got := dispatchAttentionTo(t, "local:oauth:brain_password_session")
	if len(got) != 1 || got[0] != "local:oauth:brain_password_session" {
		t.Fatalf("enqueued owners = %v, want the principal itself", got)
	}
}

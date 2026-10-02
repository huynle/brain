package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/huynle/brain-api/internal/attentionstore"
	phonepush "github.com/huynle/brain-api/internal/phonepush"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// attentionPushEnqueuer is the slice of phonepush the dispatcher needs. Attention
// reuses the single browser Web Push transport (phonepush) rather than running a
// second, competing push stack: a browser service worker can hold only one push
// subscription, so there is exactly one VAPID keypair, one SW handler, and one
// delivery loop for the whole app.
type attentionPushEnqueuer interface {
	Enqueue(owner, kind, event string, at time.Time, m phonepush.PushMessage) error
}

// AttentionDispatcher subscribes to attention.created events and fans each new
// item out to browser Web Push via phonepush. It is best-effort and never
// blocks or reverses inbox writes: the durable item already exists before any
// event is published.
type AttentionDispatcher struct {
	hub   *realtime.EventHub
	store *attentionstore.Store
	push  attentionPushEnqueuer
}

// NewAttentionDispatcher wires the hub, attention store, and the phonepush
// enqueuer. A nil enqueuer disables browser delivery (the in-app inbox still
// works); Start becomes a no-op.
func NewAttentionDispatcher(hub *realtime.EventHub, store *attentionstore.Store, push attentionPushEnqueuer) *AttentionDispatcher {
	return &AttentionDispatcher{hub: hub, store: store, push: push}
}

// Start consumes attention.created events until ctx is cancelled.
func (d *AttentionDispatcher) Start(ctx context.Context) {
	if d.push == nil {
		return
	}
	ch, unsub := d.hub.Subscribe(realtime.EventFilter{})
	defer unsub()
	slog.Info("attention dispatcher started")
	for {
		select {
		case <-ctx.Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			if !shouldDeliverAttention(evt) {
				continue
			}
			d.deliver(ctx, evt)
		}
	}
}

func (d *AttentionDispatcher) deliver(ctx context.Context, evt types.Event) {
	recipient := evt.Metadata["recipient"]
	id := evt.Metadata["attention_id"]
	if recipient == "" || id == "" {
		return
	}
	item, err := d.store.GetAttention(ctx, recipient, id)
	if err != nil || item == nil {
		return
	}
	// Enqueue into the shared phonepush transport. kind "attention" is not
	// gated by the reminder/job device preferences, so an attention item
	// reaches every one of the owner's registered devices — appropriate for a
	// first-class "needs your attention" signal. The event key dedupes per
	// item so a retry or duplicate event never double-notifies.
	msg := phonepush.PushMessage{
		Title: item.Title,
		Body:  attentionPushBody(item),
		URL:   "/?attention=" + item.ID,
		Tag:   "attention:" + item.ID,
	}
	if err := d.push.Enqueue(recipient, "attention", "attention:"+item.ID, time.Now(), msg); err != nil {
		slog.Warn("attention push enqueue failed", "attention_id", id, "error", err)
	}
}

// attentionPushBody renders a compact lock-screen body: the item body if
// present, else its project/kind context.
func attentionPushBody(item *types.Attention) string {
	if item.Body != "" {
		return item.Body
	}
	ctx := item.Project
	if item.Kind != "" {
		if ctx != "" {
			ctx += " · "
		}
		ctx += item.Kind
	}
	if ctx == "" {
		return "You have a new Brain notification."
	}
	return ctx
}

// shouldDeliverAttention reports whether an event is a newly-created attention
// item that warrants out-of-band delivery. Updates (read/resolve/etc.) do not
// re-notify; only creation does.
func shouldDeliverAttention(evt types.Event) bool {
	return evt.Type == types.EventAttentionCreated
}

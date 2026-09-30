package service

import (
	"context"
	"log/slog"

	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

// AttentionDispatcher subscribes to attention.created events and fans each new
// item out to the configured delivery providers (Web Push, webhooks, and later
// Telegram/Discord). It is best-effort and never blocks or reverses inbox
// writes: the durable item already exists before any event is published.
//
// The tenant-bound store and its context are supplied by the composition root
// (the audited apiserver seam) — the dispatcher never resolves tenancy itself,
// mirroring how the reminder sweeper receives an already-scoped store.
type AttentionDispatcher struct {
	hub       *realtime.EventHub
	store     *storage.TenantStore
	baseCtx   context.Context
	providers []AttentionDeliveryProvider
}

// NewAttentionDispatcher wires the hub, tenant-bound store, tenant-scoped base
// context, and providers. Providers with no configuration (e.g. Web Push
// without VAPID keys) may be included; they no-op.
func NewAttentionDispatcher(hub *realtime.EventHub, store *storage.TenantStore, baseCtx context.Context, providers ...AttentionDeliveryProvider) *AttentionDispatcher {
	return &AttentionDispatcher{hub: hub, store: store, baseCtx: baseCtx, providers: providers}
}

// Start consumes attention.created events until ctx is cancelled.
func (d *AttentionDispatcher) Start(ctx context.Context) {
	if len(d.providers) == 0 {
		return
	}
	ch, unsub := d.hub.Subscribe(realtime.EventFilter{})
	defer unsub()
	slog.Info("attention dispatcher started", "providers", d.providerNames())
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
			d.deliver(evt)
		}
	}
}

func (d *AttentionDispatcher) deliver(evt types.Event) {
	recipient := evt.Metadata["recipient"]
	id := evt.Metadata["attention_id"]
	if recipient == "" || id == "" {
		return
	}
	item, err := d.store.GetAttention(d.baseCtx, recipient, id)
	if err != nil || item == nil {
		return
	}
	for _, p := range d.providers {
		if err := p.Deliver(d.baseCtx, item); err != nil {
			slog.Warn("attention delivery failed", "provider", p.Name(), "attention_id", id, "error", err)
		}
	}
}

func (d *AttentionDispatcher) providerNames() []string {
	names := make([]string, 0, len(d.providers))
	for _, p := range d.providers {
		names = append(names, p.Name())
	}
	return names
}

// shouldDeliverAttention reports whether an event is a newly-created attention
// item that warrants out-of-band delivery. Updates (read/resolve/etc.) do not
// re-notify; only creation does.
func shouldDeliverAttention(evt types.Event) bool {
	return evt.Type == types.EventAttentionCreated
}

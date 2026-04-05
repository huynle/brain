package realtime

import (
	"context"
	"log/slog"

	"github.com/huynle/brain-api/internal/types"
)

// WebhookDeliverer is the subset of WebhookService that the dispatcher
// needs. Keeping the interface minimal avoids importing the full service
// or api package and makes testing straightforward.
type WebhookDeliverer interface {
	Deliver(ctx context.Context, event types.Event) error
}

// WebhookDispatcher subscribes to the EventHub and forwards every event
// to registered webhooks via WebhookDeliverer.Deliver. It runs as a
// background goroutine and shuts down cleanly when its context is cancelled.
type WebhookDispatcher struct {
	hub       *EventHub
	deliverer WebhookDeliverer
}

// NewWebhookDispatcher creates a new dispatcher wiring an EventHub to a
// WebhookDeliverer. Call Start to begin consuming events.
func NewWebhookDispatcher(hub *EventHub, deliverer WebhookDeliverer) *WebhookDispatcher {
	return &WebhookDispatcher{
		hub:       hub,
		deliverer: deliverer,
	}
}

// Start subscribes to all events on the EventHub and delivers each one
// to matching webhooks. It blocks until ctx is cancelled, then
// unsubscribes and returns. Delivery is asynchronous — each event is
// handed off to WebhookDeliverer.Deliver, which spawns its own
// goroutines for HTTP delivery.
func (d *WebhookDispatcher) Start(ctx context.Context) {
	// Subscribe with an empty filter to receive all events.
	ch, unsub := d.hub.Subscribe(EventFilter{})
	defer unsub()

	slog.Info("webhook dispatcher started")

	for {
		select {
		case <-ctx.Done():
			slog.Info("webhook dispatcher stopped")
			return
		case evt, ok := <-ch:
			if !ok {
				// Channel closed (hub shut down).
				slog.Info("webhook dispatcher: event channel closed")
				return
			}
			if err := d.deliverer.Deliver(ctx, evt); err != nil {
				slog.Warn("webhook dispatcher: delivery error",
					"event_id", evt.ID,
					"event_type", evt.Type,
					"error", err,
				)
			}
		}
	}
}

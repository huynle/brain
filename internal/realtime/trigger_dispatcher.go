package realtime

import (
	"context"
	"log/slog"

	"github.com/huynle/brain-api/internal/types"
)

// TriggerHandler processes an event against task triggers.
// Implementations should evaluate the event against configured triggers
// and activate any matching tasks. This is the minimal interface the
// dispatcher needs — keeping it simple avoids importing the service package.
type TriggerHandler interface {
	HandleEvent(ctx context.Context, evt types.Event) error
}

// TriggerDispatcher subscribes to the EventHub and evaluates every event
// against configured task triggers via TriggerHandler. It runs as a
// background goroutine and shuts down cleanly when its context is cancelled.
type TriggerDispatcher struct {
	hub     *EventHub
	handler TriggerHandler
}

// NewTriggerDispatcher creates a new dispatcher wiring an EventHub to a
// TriggerHandler. Call Start to begin consuming events.
func NewTriggerDispatcher(hub *EventHub, handler TriggerHandler) *TriggerDispatcher {
	return &TriggerDispatcher{
		hub:     hub,
		handler: handler,
	}
}

// Start subscribes to all events on the EventHub and passes each one
// to the TriggerHandler for evaluation and activation. It blocks until
// ctx is cancelled, then unsubscribes and returns.
func (d *TriggerDispatcher) Start(ctx context.Context) {
	// Subscribe with an empty filter to receive all events.
	ch, unsub := d.hub.Subscribe(EventFilter{})
	defer unsub()

	slog.Info("trigger dispatcher started")

	for {
		select {
		case <-ctx.Done():
			slog.Info("trigger dispatcher stopped")
			return
		case evt, ok := <-ch:
			if !ok {
				// Channel closed (hub shut down).
				slog.Info("trigger dispatcher: event channel closed")
				return
			}
			if err := d.handler.HandleEvent(ctx, evt); err != nil {
				slog.Warn("trigger dispatcher: handler error",
					"event_id", evt.ID,
					"event_type", evt.Type,
					"error", err,
				)
			}
		}
	}
}

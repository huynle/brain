// Package events — DedupBus: event deduplication filter with persistence.
//
// DedupBus wraps a Bus to provide:
//   - Persisting events to the event_log table before dispatching
//   - Dropping duplicate DedupKeys within a configurable time window
//   - Replaying unprocessed events on startup for at-least-once delivery
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"
)

// EventStore abstracts the storage operations needed by DedupBus.
// This avoids importing the full storage package into events.
type EventStore interface {
	// InsertEvent persists an event and returns its ID.
	// Empty dedupKey is stored as NULL (no uniqueness constraint).
	InsertEvent(ctx context.Context, eventType, payload, dedupKey, source string) (int64, error)
	// MarkProcessed sets the processed_at timestamp for an event.
	MarkProcessed(ctx context.Context, id int64) error
	// GetUnprocessed returns all events where processed_at IS NULL, ordered by created_at ASC.
	GetUnprocessed(ctx context.Context) ([]UnprocessedEvent, error)
}

// UnprocessedEvent is a minimal view of a persisted event for replay.
type UnprocessedEvent struct {
	ID        int64
	EventType string
	Payload   string // JSON
	DedupKey  string // empty if NULL in DB
	Source    string
	CreatedAt string
}

// DedupBusConfig holds configuration for creating a DedupBus.
type DedupBusConfig struct {
	Inner  Bus           // The underlying bus to delegate to
	Store  EventStore    // Persistence layer for event_log
	Window time.Duration // Dedup window for duplicate DedupKeys (default 60s)
}

// DedupBus wraps a Bus to add event persistence and deduplication.
//
// On Publish:
//  1. Persist event to event_log (before dispatching)
//  2. If DedupKey is set and was seen within the dedup window, drop the event
//  3. Forward to the inner bus for handler dispatch
//
// The event's database ID is stored in Payload["_event_log_id"] so downstream
// handlers (like AutomationMatcher) can mark it as processed.
type DedupBus struct {
	inner  Bus
	store  EventStore
	window time.Duration

	mu       sync.Mutex
	seen     map[string]time.Time // DedupKey -> last seen timestamp
	closed   bool
	cleanCtx context.Context
	cleanFn  context.CancelFunc
}

// NewDedupBus creates a new DedupBus wrapping the given bus.
func NewDedupBus(cfg DedupBusConfig) *DedupBus {
	window := cfg.Window
	if window == 0 {
		window = 60 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())
	d := &DedupBus{
		inner:    cfg.Inner,
		store:    cfg.Store,
		window:   window,
		seen:     make(map[string]time.Time),
		cleanCtx: ctx,
		cleanFn:  cancel,
	}

	// Start background cleanup of expired dedup keys
	go d.cleanLoop()

	return d
}

// Publish persists the event, applies dedup filtering, and forwards to the inner bus.
func (d *DedupBus) Publish(event Event) {
	// Auto-set timestamp if not provided
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Marshal payload for persistence
	payloadJSON, err := json.Marshal(event.Payload)
	if err != nil {
		slog.Warn("dedup_bus: failed to marshal payload", "error", err)
		payloadJSON = []byte("{}")
	}

	// Persist to event_log BEFORE dispatching
	eventID, err := d.store.InsertEvent(
		context.Background(),
		string(event.Type),
		string(payloadJSON),
		event.DedupKey,
		event.Source,
	)
	if err != nil {
		// If the insert fails due to dedup_key uniqueness constraint,
		// the event is a duplicate at the DB level — drop it.
		slog.Debug("dedup_bus: insert failed (likely dedup)", "dedup_key", event.DedupKey, "error", err)
		return
	}

	// Time-window dedup for events with a DedupKey
	if event.DedupKey != "" {
		d.mu.Lock()
		if lastSeen, ok := d.seen[event.DedupKey]; ok {
			if time.Since(lastSeen) < d.window {
				d.mu.Unlock()
				slog.Debug("dedup_bus: dropping duplicate within window",
					"dedup_key", event.DedupKey,
					"window", d.window,
				)
				return
			}
		}
		d.seen[event.DedupKey] = event.Timestamp
		d.mu.Unlock()
	}

	// Inject the event_log ID into the payload so downstream handlers can mark processed
	if event.Payload == nil {
		event.Payload = make(map[string]any)
	}
	event.Payload["_event_log_id"] = eventID

	// Forward to inner bus
	d.inner.Publish(event)
}

// Subscribe delegates to the inner bus.
func (d *DedupBus) Subscribe(eventType EventType, handler Handler) Subscription {
	return d.inner.Subscribe(eventType, handler)
}

// SubscribePattern delegates to the inner bus.
func (d *DedupBus) SubscribePattern(pattern string, handler Handler) Subscription {
	return d.inner.SubscribePattern(pattern, handler)
}

// Close stops the cleanup goroutine and closes the inner bus.
func (d *DedupBus) Close() {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()

	d.cleanFn()
	d.inner.Close()
}

// ReplayUnprocessed replays all persisted but unprocessed events through the inner bus.
// Call this on server startup after all subscribers are registered.
func (d *DedupBus) ReplayUnprocessed(ctx context.Context) error {
	events, err := d.store.GetUnprocessed(ctx)
	if err != nil {
		return err
	}

	if len(events) == 0 {
		return nil
	}

	slog.Info("dedup_bus: replaying unprocessed events", "count", len(events))

	for _, row := range events {
		var payload map[string]any
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			slog.Warn("dedup_bus: failed to unmarshal replay payload", "id", row.ID, "error", err)
			payload = make(map[string]any)
		}

		// Inject event_log ID for processed tracking
		payload["_event_log_id"] = row.ID

		event := Event{
			Type:      EventType(row.EventType),
			Payload:   payload,
			DedupKey:  row.DedupKey,
			Source:    row.Source,
			Timestamp: time.Now(), // Use current time for replay
		}

		// Publish directly to inner bus (skip persistence, already persisted)
		d.inner.Publish(event)
	}

	return nil
}

// cleanLoop periodically removes expired entries from the in-memory dedup map.
func (d *DedupBus) cleanLoop() {
	ticker := time.NewTicker(d.window)
	defer ticker.Stop()

	for {
		select {
		case <-d.cleanCtx.Done():
			return
		case <-ticker.C:
			d.cleanExpired()
		}
	}
}

// cleanExpired removes dedup keys older than the window.
func (d *DedupBus) cleanExpired() {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	for key, ts := range d.seen {
		if now.Sub(ts) > d.window {
			delete(d.seen, key)
		}
	}
}

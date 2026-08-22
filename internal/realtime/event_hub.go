package realtime

import (
	"sync"

	"github.com/huynle/brain-api/internal/types"
)

// DefaultEventBufferCapacity is the default ring buffer size.
const DefaultEventBufferCapacity = 1000

// EventFilter controls which events a subscriber receives.
// All fields are optional; an empty filter matches all events.
// When multiple fields are set, all must match (AND logic).
type EventFilter struct {
	// TypePatterns are event type patterns (exact, "task.*", or "*").
	TypePatterns []string
	// ProjectID filters to events for this project only.
	ProjectID string
	// FeatureID filters to events for this feature only.
	FeatureID string
}

// eventSubscriber holds a subscriber's channel and filter.
//
// send/close are serialized by mu so that Publish, which fans out after
// releasing the hub lock, can never send on a channel that unsubscribe has
// already closed (which would panic and permanently drop the event).
type eventSubscriber struct {
	ch     chan types.Event
	filter EventFilter

	mu     sync.Mutex
	closed bool
}

// send delivers evt unless the subscriber is already closed. Non-blocking:
// drops the event if the subscriber's buffer is full.
func (s *eventSubscriber) send(evt types.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.ch <- evt:
	default:
		// Drop event for slow subscriber.
	}
}

// close closes the channel exactly once. Consumers detect shutdown via the
// channel's closed state, so the close must actually happen.
func (s *eventSubscriber) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	close(s.ch)
}

// EventHub stores events in a ring buffer and fans out to
// filter-based subscribers. Thread-safe for concurrent use.
type EventHub struct {
	mu sync.RWMutex

	// Ring buffer
	buffer []types.Event
	cap    int
	head   int // next write position
	count  int // number of events stored (up to cap)

	// Subscribers
	subscribers map[*eventSubscriber]struct{}
}

// NewEventHub creates an EventHub with the default buffer capacity (1000).
func NewEventHub() *EventHub {
	return NewEventHubWithCapacity(DefaultEventBufferCapacity)
}

// NewEventHubWithCapacity creates an EventHub with a specified buffer capacity.
func NewEventHubWithCapacity(capacity int) *EventHub {
	if capacity <= 0 {
		capacity = DefaultEventBufferCapacity
	}
	return &EventHub{
		buffer:      make([]types.Event, capacity),
		cap:         capacity,
		subscribers: make(map[*eventSubscriber]struct{}),
	}
}

// Publish stores the event in the ring buffer and fans out to
// all matching subscribers. Non-blocking: drops events for slow subscribers.
func (h *EventHub) Publish(evt types.Event) {
	h.mu.Lock()

	// Store in ring buffer.
	h.buffer[h.head] = evt
	h.head = (h.head + 1) % h.cap
	if h.count < h.cap {
		h.count++
	}

	// Snapshot subscribers under lock to iterate safely.
	subs := make([]*eventSubscriber, 0, len(h.subscribers))
	for sub := range h.subscribers {
		subs = append(subs, sub)
	}
	h.mu.Unlock()

	// Fan out to matching subscribers (outside write lock).
	for _, sub := range subs {
		if matchesFilter(sub.filter, evt) {
			sub.send(evt)
		}
	}
}

// Subscribe registers a subscriber with the given filter.
// Returns a read-only event channel and an unsubscribe function.
// The unsubscribe function is safe to call multiple times.
func (h *EventHub) Subscribe(filter EventFilter) (<-chan types.Event, func()) {
	sub := &eventSubscriber{
		ch:     make(chan types.Event, 64),
		filter: filter,
	}

	h.mu.Lock()
	h.subscribers[sub] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	unsub := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subscribers, sub)
			h.mu.Unlock()
			sub.close()
		})
	}

	return sub.ch, unsub
}

// Replay returns events from the ring buffer after the given lastEventID.
// If lastEventID is empty, returns all buffered events.
// If lastEventID is not found (evicted or unknown), returns all buffered events.
func (h *EventHub) Replay(lastEventID string) []types.Event {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.count == 0 {
		return nil
	}

	// Collect all events from the buffer in order.
	events := h.orderedEvents()

	if lastEventID == "" {
		return events
	}

	// Find the event with lastEventID and return everything after it.
	for i, evt := range events {
		if evt.ID == lastEventID {
			return events[i+1:]
		}
	}

	// ID not found (evicted or unknown) — return all.
	return events
}

// orderedEvents returns the buffered events in chronological order.
// Must be called with h.mu held (at least RLock).
func (h *EventHub) orderedEvents() []types.Event {
	events := make([]types.Event, 0, h.count)

	// Start is the oldest event in the ring.
	start := (h.head - h.count + h.cap) % h.cap
	for i := 0; i < h.count; i++ {
		idx := (start + i) % h.cap
		events = append(events, h.buffer[idx])
	}
	return events
}

// matchesFilter checks if an event matches the given filter.
// An empty filter matches everything.
func matchesFilter(f EventFilter, evt types.Event) bool {
	// Check type patterns (if any are specified, at least one must match).
	if len(f.TypePatterns) > 0 {
		matched := false
		for _, pattern := range f.TypePatterns {
			if types.MatchEventPattern(pattern, evt.Type) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}

	// Check project ID.
	if f.ProjectID != "" && evt.ProjectID != f.ProjectID {
		return false
	}

	// Check feature ID.
	if f.FeatureID != "" && evt.FeatureID != f.FeatureID {
		return false
	}

	return true
}

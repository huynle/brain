package events

import (
	"strings"
	"sync"
	"time"
)

// Compile-time check: *MemoryBus implements Bus.
var _ Bus = (*MemoryBus)(nil)

// subscription implements Subscription for both exact and pattern subscribers.
type subscription struct {
	unsubscribe func()
	once        sync.Once
}

func (s *subscription) Unsubscribe() {
	s.once.Do(s.unsubscribe)
}

// noopSubscription is returned when subscribing to a closed bus.
type noopSubscription struct{}

func (noopSubscription) Unsubscribe() {}

// exactSub holds a handler subscribed to a specific event type.
type exactSub struct {
	handler Handler
}

// patternSub holds a handler subscribed via a wildcard pattern.
type patternSub struct {
	pattern string
	matchFn func(EventType) bool
	handler Handler
}

// MemoryBus is an in-memory implementation of Bus.
//
// It uses sync.RWMutex for concurrent access and delivers events
// with non-blocking goroutine dispatch (matching the existing Hub behavior).
type MemoryBus struct {
	mu       sync.RWMutex
	closed   bool
	nextID   uint64
	exact    map[EventType]map[uint64]*exactSub
	patterns map[uint64]*patternSub
}

// NewMemoryBus creates a new in-memory event bus.
func NewMemoryBus() *MemoryBus {
	return &MemoryBus{
		exact:    make(map[EventType]map[uint64]*exactSub),
		patterns: make(map[uint64]*patternSub),
	}
}

// Publish sends an event to all matching subscribers.
// Non-blocking: each handler is invoked in its own goroutine.
// If Timestamp is zero, it is set to time.Now().
// After Close, Publish is a no-op.
func (b *MemoryBus) Publish(event Event) {
	b.mu.RLock()
	if b.closed {
		b.mu.RUnlock()
		return
	}

	// Auto-set timestamp if not provided
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Collect matching exact subscribers
	var handlers []Handler
	if subs, ok := b.exact[event.Type]; ok {
		for _, s := range subs {
			handlers = append(handlers, s.handler)
		}
	}

	// Collect matching pattern subscribers
	for _, ps := range b.patterns {
		if ps.matchFn(event.Type) {
			handlers = append(handlers, ps.handler)
		}
	}
	b.mu.RUnlock()

	// Dispatch to all handlers non-blocking
	for _, h := range handlers {
		h := h // capture for goroutine
		go h(event)
	}
}

// Subscribe registers a handler for a specific event type.
// Returns a Subscription that can be used to unsubscribe.
// After Close, returns a no-op Subscription.
func (b *MemoryBus) Subscribe(eventType EventType, handler Handler) Subscription {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return noopSubscription{}
	}

	id := b.nextID
	b.nextID++

	if b.exact[eventType] == nil {
		b.exact[eventType] = make(map[uint64]*exactSub)
	}
	b.exact[eventType][id] = &exactSub{handler: handler}
	b.mu.Unlock()

	return &subscription{
		unsubscribe: func() {
			b.mu.Lock()
			delete(b.exact[eventType], id)
			if len(b.exact[eventType]) == 0 {
				delete(b.exact, eventType)
			}
			b.mu.Unlock()
		},
	}
}

// SubscribePattern registers a handler for events matching a wildcard pattern.
// Patterns use "prefix.*" to match any event type starting with "prefix.".
// A bare "*" matches all events.
// After Close, returns a no-op Subscription.
func (b *MemoryBus) SubscribePattern(pattern string, handler Handler) Subscription {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return noopSubscription{}
	}

	id := b.nextID
	b.nextID++

	ps := &patternSub{
		pattern: pattern,
		handler: handler,
		matchFn: compilePattern(pattern),
	}
	b.patterns[id] = ps
	b.mu.Unlock()

	return &subscription{
		unsubscribe: func() {
			b.mu.Lock()
			delete(b.patterns, id)
			b.mu.Unlock()
		},
	}
}

// Close shuts down the bus. After Close, Publish is a no-op and
// Subscribe/SubscribePattern return no-op Subscriptions.
func (b *MemoryBus) Close() {
	b.mu.Lock()
	b.closed = true
	b.exact = nil
	b.patterns = nil
	b.mu.Unlock()
}

// compilePattern returns a match function for the given pattern.
// Supported patterns:
//   - "*" matches everything
//   - "prefix.*" matches any EventType starting with "prefix."
//   - Exact string with no wildcard matches only that exact EventType
func compilePattern(pattern string) func(EventType) bool {
	if pattern == "*" {
		return func(EventType) bool { return true }
	}

	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return func(et EventType) bool {
			s := string(et)
			if !strings.HasPrefix(s, prefix) {
				return false
			}
			// Ensure no additional dots in the remainder (single level wildcard)
			remainder := s[len(prefix):]
			return !strings.Contains(remainder, ".")
		}
	}

	// No wildcard — exact match
	exact := EventType(pattern)
	return func(et EventType) bool {
		return et == exact
	}
}

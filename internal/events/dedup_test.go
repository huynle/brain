package events

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock EventStore
// =============================================================================

type mockEventStore struct {
	mu        sync.Mutex
	events    []storedEvent
	nextID    int64
	insertErr error
	dedupKeys map[string]bool // tracks unique dedup keys
	processed map[int64]bool  // id -> processed
}

type storedEvent struct {
	ID        int64
	EventType string
	Payload   string
	DedupKey  string
	Source    string
}

func newMockEventStore() *mockEventStore {
	return &mockEventStore{
		dedupKeys: make(map[string]bool),
		processed: make(map[int64]bool),
	}
}

func (m *mockEventStore) InsertEvent(ctx context.Context, eventType, payload, dedupKey, source string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.insertErr != nil {
		return 0, m.insertErr
	}

	// Simulate unique dedup_key constraint
	if dedupKey != "" {
		if m.dedupKeys[dedupKey] {
			return 0, fmt.Errorf("UNIQUE constraint failed: dedup_key %q", dedupKey)
		}
		m.dedupKeys[dedupKey] = true
	}

	m.nextID++
	m.events = append(m.events, storedEvent{
		ID:        m.nextID,
		EventType: eventType,
		Payload:   payload,
		DedupKey:  dedupKey,
		Source:    source,
	})
	return m.nextID, nil
}

func (m *mockEventStore) MarkProcessed(ctx context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.processed[id] = true
	return nil
}

func (m *mockEventStore) GetUnprocessed(ctx context.Context) ([]UnprocessedEvent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var result []UnprocessedEvent
	for _, e := range m.events {
		if !m.processed[e.ID] {
			result = append(result, UnprocessedEvent{
				ID:        e.ID,
				EventType: e.EventType,
				Payload:   e.Payload,
				DedupKey:  e.DedupKey,
				Source:    e.Source,
			})
		}
	}
	return result, nil
}

func (m *mockEventStore) getEvents() []storedEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]storedEvent, len(m.events))
	copy(result, m.events)
	return result
}

func (m *mockEventStore) isProcessed(id int64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.processed[id]
}

// =============================================================================
// Tests: Event Persistence
// =============================================================================

func TestDedupBus_PersistsEventBeforeDispatching(t *testing.T) {
	inner := NewMemoryBus()
	defer inner.Close()
	store := newMockEventStore()

	bus := NewDedupBus(DedupBusConfig{
		Inner:  inner,
		Store:  store,
		Window: 60 * time.Second,
	})
	defer bus.Close()

	var received atomic.Int32
	inner.Subscribe(TaskCompleted, func(e Event) {
		received.Add(1)
	})

	bus.Publish(Event{
		Type:      TaskCompleted,
		Source:    "service",
		ProjectID: "test",
		Payload:   map[string]any{"id": "task1"},
	})

	// Wait for async dispatch
	require.Eventually(t, func() bool {
		return received.Load() > 0
	}, 3*time.Second, 50*time.Millisecond, "event should be dispatched")

	// Verify persisted
	events := store.getEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, "task.completed", events[0].EventType)
	assert.Equal(t, "service", events[0].Source)
}

func TestDedupBus_InjectsEventLogID(t *testing.T) {
	inner := NewMemoryBus()
	defer inner.Close()
	store := newMockEventStore()

	bus := NewDedupBus(DedupBusConfig{
		Inner:  inner,
		Store:  store,
		Window: 60 * time.Second,
	})
	defer bus.Close()

	var capturedID any
	var mu sync.Mutex
	inner.Subscribe(TaskCompleted, func(e Event) {
		mu.Lock()
		capturedID = e.Payload["_event_log_id"]
		mu.Unlock()
	})

	bus.Publish(Event{
		Type:    TaskCompleted,
		Source:  "service",
		Payload: map[string]any{"id": "task1"},
	})

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return capturedID != nil
	}, 3*time.Second, 50*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, int64(1), capturedID)
}

// =============================================================================
// Tests: Time-Window Dedup
// =============================================================================

func TestDedupBus_DropsDuplicateDedupKeyWithinWindow(t *testing.T) {
	inner := NewMemoryBus()
	defer inner.Close()
	store := newMockEventStore()

	bus := NewDedupBus(DedupBusConfig{
		Inner:  inner,
		Store:  store,
		Window: 5 * time.Second, // long window for test
	})
	defer bus.Close()

	var received atomic.Int32
	inner.Subscribe(TaskCompleted, func(e Event) {
		received.Add(1)
	})

	// First publish — should go through
	bus.Publish(Event{
		Type:     TaskCompleted,
		Source:   "service",
		DedupKey: "dedup-1",
		Payload:  map[string]any{"id": "task1"},
	})

	require.Eventually(t, func() bool {
		return received.Load() > 0
	}, 3*time.Second, 50*time.Millisecond)

	// Second publish with same DedupKey — should be dropped by DB uniqueness constraint
	bus.Publish(Event{
		Type:     TaskCompleted,
		Source:   "service",
		DedupKey: "dedup-1",
		Payload:  map[string]any{"id": "task1"},
	})

	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, int32(1), received.Load(), "duplicate dedup_key should be dropped")
}

func TestDedupBus_AllowsDifferentDedupKeys(t *testing.T) {
	inner := NewMemoryBus()
	defer inner.Close()
	store := newMockEventStore()

	bus := NewDedupBus(DedupBusConfig{
		Inner:  inner,
		Store:  store,
		Window: 60 * time.Second,
	})
	defer bus.Close()

	var received atomic.Int32
	inner.Subscribe(TaskCompleted, func(e Event) {
		received.Add(1)
	})

	bus.Publish(Event{
		Type:     TaskCompleted,
		Source:   "service",
		DedupKey: "dedup-1",
		Payload:  map[string]any{"id": "task1"},
	})

	bus.Publish(Event{
		Type:     TaskCompleted,
		Source:   "service",
		DedupKey: "dedup-2",
		Payload:  map[string]any{"id": "task2"},
	})

	require.Eventually(t, func() bool {
		return received.Load() >= 2
	}, 3*time.Second, 50*time.Millisecond, "different dedup keys should both dispatch")
}

func TestDedupBus_EmptyDedupKeyBypassesDedup(t *testing.T) {
	inner := NewMemoryBus()
	defer inner.Close()
	store := newMockEventStore()

	bus := NewDedupBus(DedupBusConfig{
		Inner:  inner,
		Store:  store,
		Window: 60 * time.Second,
	})
	defer bus.Close()

	var received atomic.Int32
	inner.Subscribe(TaskCompleted, func(e Event) {
		received.Add(1)
	})

	// Two events with empty DedupKey — both should go through
	bus.Publish(Event{
		Type:    TaskCompleted,
		Source:  "service",
		Payload: map[string]any{"id": "task1"},
	})

	bus.Publish(Event{
		Type:    TaskCompleted,
		Source:  "service",
		Payload: map[string]any{"id": "task2"},
	})

	require.Eventually(t, func() bool {
		return received.Load() >= 2
	}, 3*time.Second, 50*time.Millisecond, "events without dedup key should not be deduplicated")
}

// =============================================================================
// Tests: Replay Unprocessed
// =============================================================================

func TestDedupBus_ReplayUnprocessed(t *testing.T) {
	inner := NewMemoryBus()
	defer inner.Close()
	store := newMockEventStore()

	// Pre-populate store with unprocessed events (simulating server restart)
	store.InsertEvent(context.Background(), "task.completed", `{"id":"task1"}`, "replay-1", "service")
	store.InsertEvent(context.Background(), "task.failed", `{"id":"task2"}`, "replay-2", "service")

	bus := NewDedupBus(DedupBusConfig{
		Inner:  inner,
		Store:  store,
		Window: 60 * time.Second,
	})
	defer bus.Close()

	var received atomic.Int32
	var mu sync.Mutex
	var receivedTypes []string
	inner.SubscribePattern("*", func(e Event) {
		received.Add(1)
		mu.Lock()
		receivedTypes = append(receivedTypes, string(e.Type))
		mu.Unlock()
	})

	// Replay unprocessed events
	err := bus.ReplayUnprocessed(context.Background())
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return received.Load() >= 2
	}, 3*time.Second, 50*time.Millisecond, "should replay 2 unprocessed events")

	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, receivedTypes, "task.completed")
	assert.Contains(t, receivedTypes, "task.failed")
}

func TestDedupBus_ReplayInjectsEventLogID(t *testing.T) {
	inner := NewMemoryBus()
	defer inner.Close()
	store := newMockEventStore()

	// Pre-populate
	store.InsertEvent(context.Background(), "task.completed", `{"id":"task1"}`, "replay-1", "service")

	bus := NewDedupBus(DedupBusConfig{
		Inner:  inner,
		Store:  store,
		Window: 60 * time.Second,
	})
	defer bus.Close()

	var capturedID any
	var mu sync.Mutex
	inner.Subscribe(TaskCompleted, func(e Event) {
		mu.Lock()
		capturedID = e.Payload["_event_log_id"]
		mu.Unlock()
	})

	err := bus.ReplayUnprocessed(context.Background())
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return capturedID != nil
	}, 3*time.Second, 50*time.Millisecond)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, int64(1), capturedID)
}

func TestDedupBus_ReplayNoEvents(t *testing.T) {
	inner := NewMemoryBus()
	defer inner.Close()
	store := newMockEventStore()

	bus := NewDedupBus(DedupBusConfig{
		Inner:  inner,
		Store:  store,
		Window: 60 * time.Second,
	})
	defer bus.Close()

	err := bus.ReplayUnprocessed(context.Background())
	require.NoError(t, err, "replay with no events should succeed")
}

// =============================================================================
// Tests: DedupBus delegates Subscribe/SubscribePattern
// =============================================================================

func TestDedupBus_SubscribeDelegatesInner(t *testing.T) {
	inner := NewMemoryBus()
	defer inner.Close()
	store := newMockEventStore()

	bus := NewDedupBus(DedupBusConfig{
		Inner:  inner,
		Store:  store,
		Window: 60 * time.Second,
	})
	defer bus.Close()

	var received atomic.Int32
	sub := bus.Subscribe(TaskCompleted, func(e Event) {
		received.Add(1)
	})

	bus.Publish(Event{
		Type:    TaskCompleted,
		Source:  "service",
		Payload: map[string]any{"id": "task1"},
	})

	require.Eventually(t, func() bool {
		return received.Load() > 0
	}, 3*time.Second, 50*time.Millisecond)

	sub.Unsubscribe()

	bus.Publish(Event{
		Type:    TaskCompleted,
		Source:  "service",
		Payload: map[string]any{"id": "task2"},
	})

	time.Sleep(200 * time.Millisecond)
	assert.Equal(t, int32(1), received.Load(), "unsubscribed handler should not receive events")
}

// =============================================================================
// Tests: Default window
// =============================================================================

func TestDedupBus_DefaultWindowIs60Seconds(t *testing.T) {
	inner := NewMemoryBus()
	defer inner.Close()
	store := newMockEventStore()

	bus := NewDedupBus(DedupBusConfig{
		Inner: inner,
		Store: store,
		// Window not set — should default to 60s
	})
	defer bus.Close()

	assert.Equal(t, 60*time.Second, bus.window)
}

// =============================================================================
// Tests: Close
// =============================================================================

func TestDedupBus_CloseStopsCleanup(t *testing.T) {
	inner := NewMemoryBus()
	store := newMockEventStore()

	bus := NewDedupBus(DedupBusConfig{
		Inner:  inner,
		Store:  store,
		Window: 50 * time.Millisecond, // fast for test
	})

	// Should not panic or hang
	bus.Close()

	// Publish after close should be a no-op (inner bus closed)
	bus.Publish(Event{
		Type:    TaskCompleted,
		Source:  "service",
		Payload: map[string]any{"id": "task1"},
	})
}

// =============================================================================
// Tests: Automation source flag on events
// =============================================================================

func TestDedupBus_AutomationSourceEventsArePersisted(t *testing.T) {
	inner := NewMemoryBus()
	defer inner.Close()
	store := newMockEventStore()

	bus := NewDedupBus(DedupBusConfig{
		Inner:  inner,
		Store:  store,
		Window: 60 * time.Second,
	})
	defer bus.Close()

	// Events with source="automation" should still be persisted
	bus.Publish(Event{
		Type:    EntryCreated,
		Source:  "automation",
		Payload: map[string]any{"id": "auto-task1"},
	})

	require.Eventually(t, func() bool {
		return len(store.getEvents()) > 0
	}, 3*time.Second, 50*time.Millisecond)

	events := store.getEvents()
	assert.Len(t, events, 1)
	assert.Equal(t, "automation", events[0].Source)
}

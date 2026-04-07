package events

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewMemoryBus(t *testing.T) {
	bus := NewMemoryBus()
	if bus == nil {
		t.Fatal("NewMemoryBus() returned nil")
	}
	defer bus.Close()
}

func TestPublishAndSubscribe(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	var received Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EntryCreated, func(e Event) {
		received = e
		wg.Done()
	})

	evt := Event{
		Type:      EntryCreated,
		Payload:   map[string]any{"path": "test.md"},
		Source:    "test",
		Timestamp: time.Now(),
		ProjectID: "proj-1",
	}
	bus.Publish(evt)

	waitOrTimeout(t, &wg, time.Second)

	if received.Type != EntryCreated {
		t.Errorf("received.Type = %q, want %q", received.Type, EntryCreated)
	}
	if received.ProjectID != "proj-1" {
		t.Errorf("received.ProjectID = %q, want %q", received.ProjectID, "proj-1")
	}
	if received.Payload["path"] != "test.md" {
		t.Errorf("received.Payload[path] = %v, want %q", received.Payload["path"], "test.md")
	}
}

func TestSubscribeOnlyReceivesMatchingType(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	var count atomic.Int32

	bus.Subscribe(EntryCreated, func(e Event) {
		count.Add(1)
	})

	// Publish a different event type
	bus.Publish(Event{Type: EntryDeleted})
	// Publish matching event type
	bus.Publish(Event{Type: EntryCreated})

	// Give time for delivery
	time.Sleep(50 * time.Millisecond)

	if got := count.Load(); got != 1 {
		t.Errorf("handler called %d times, want 1", got)
	}
}

func TestMultipleSubscribers(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	var count atomic.Int32
	var wg sync.WaitGroup
	wg.Add(3)

	for i := 0; i < 3; i++ {
		bus.Subscribe(TaskCompleted, func(e Event) {
			count.Add(1)
			wg.Done()
		})
	}

	bus.Publish(Event{Type: TaskCompleted})

	waitOrTimeout(t, &wg, time.Second)

	if got := count.Load(); got != 3 {
		t.Errorf("handlers called %d times, want 3", got)
	}
}

func TestUnsubscribe(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	var count atomic.Int32

	sub := bus.Subscribe(EntryUpdated, func(e Event) {
		count.Add(1)
	})

	bus.Publish(Event{Type: EntryUpdated})
	time.Sleep(50 * time.Millisecond)

	if got := count.Load(); got != 1 {
		t.Fatalf("before unsub: handler called %d times, want 1", got)
	}

	sub.Unsubscribe()

	bus.Publish(Event{Type: EntryUpdated})
	time.Sleep(50 * time.Millisecond)

	if got := count.Load(); got != 1 {
		t.Errorf("after unsub: handler called %d times, want 1", got)
	}
}

func TestDoubleUnsubscribe(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	sub := bus.Subscribe(EntryCreated, func(e Event) {})

	// Should not panic
	sub.Unsubscribe()
	sub.Unsubscribe()
}

func TestSubscribePatternWildcard(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	var received []EventType
	var mu sync.Mutex
	var wg sync.WaitGroup
	wg.Add(2)

	bus.SubscribePattern("task.*", func(e Event) {
		mu.Lock()
		received = append(received, e.Type)
		mu.Unlock()
		wg.Done()
	})

	bus.Publish(Event{Type: TaskCompleted})
	bus.Publish(Event{Type: TaskFailed})
	bus.Publish(Event{Type: EntryCreated}) // Should NOT match

	waitOrTimeout(t, &wg, time.Second)

	mu.Lock()
	defer mu.Unlock()

	if len(received) != 2 {
		t.Fatalf("received %d events, want 2", len(received))
	}
	hasCompleted, hasFailed := false, false
	for _, et := range received {
		if et == TaskCompleted {
			hasCompleted = true
		}
		if et == TaskFailed {
			hasFailed = true
		}
	}
	if !hasCompleted || !hasFailed {
		t.Errorf("received = %v, want TaskCompleted and TaskFailed", received)
	}
}

func TestSubscribePatternMatchAll(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	var count atomic.Int32
	var wg sync.WaitGroup
	wg.Add(3)

	bus.SubscribePattern("*", func(e Event) {
		count.Add(1)
		wg.Done()
	})

	bus.Publish(Event{Type: EntryCreated})
	bus.Publish(Event{Type: TaskCompleted})
	bus.Publish(Event{Type: ScheduleFired})

	waitOrTimeout(t, &wg, time.Second)

	if got := count.Load(); got != 3 {
		t.Errorf("handler called %d times, want 3", got)
	}
}

func TestSubscribePatternDoesNotMatchPartial(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	var count atomic.Int32

	// "task.*" should NOT match "task_extra.completed"
	bus.SubscribePattern("task.*", func(e Event) {
		count.Add(1)
	})

	bus.Publish(Event{Type: EventType("task_extra.completed")})
	time.Sleep(50 * time.Millisecond)

	if got := count.Load(); got != 0 {
		t.Errorf("handler called %d times, want 0", got)
	}
}

func TestNonBlockingDelivery(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	// Subscribe with a handler that blocks for a long time
	bus.Subscribe(EntryCreated, func(e Event) {
		time.Sleep(5 * time.Second)
	})

	// Publish should return quickly even with a slow subscriber
	done := make(chan struct{})
	go func() {
		bus.Publish(Event{Type: EntryCreated})
		close(done)
	}()

	select {
	case <-done:
		// Good - publish returned quickly
	case <-time.After(500 * time.Millisecond):
		t.Fatal("Publish blocked on slow subscriber")
	}
}

func TestPublishAfterClose(t *testing.T) {
	bus := NewMemoryBus()

	var count atomic.Int32
	bus.Subscribe(EntryCreated, func(e Event) {
		count.Add(1)
	})

	bus.Close()

	// Should not panic, should be a no-op
	bus.Publish(Event{Type: EntryCreated})
	time.Sleep(50 * time.Millisecond)

	if got := count.Load(); got != 0 {
		t.Errorf("handler called %d times after close, want 0", got)
	}
}

func TestSubscribeAfterClose(t *testing.T) {
	bus := NewMemoryBus()
	bus.Close()

	// Should not panic, returns a no-op subscription
	sub := bus.Subscribe(EntryCreated, func(e Event) {})
	if sub == nil {
		t.Fatal("Subscribe after Close returned nil")
	}

	// Unsubscribe should not panic
	sub.Unsubscribe()
}

func TestSubscribePatternAfterClose(t *testing.T) {
	bus := NewMemoryBus()
	bus.Close()

	sub := bus.SubscribePattern("*", func(e Event) {})
	if sub == nil {
		t.Fatal("SubscribePattern after Close returned nil")
	}
	sub.Unsubscribe()
}

func TestPublishToNoSubscribers(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	// Should not panic
	bus.Publish(Event{Type: EntryCreated})
	bus.Publish(Event{Type: TaskCompleted, Payload: map[string]any{"id": "123"}})
}

func TestConcurrentPublishSubscribe(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	var total atomic.Int32
	const numGoroutines = 10
	const numEvents = 100

	var wg sync.WaitGroup

	// Start subscribers concurrently
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.Subscribe(EntryCreated, func(e Event) {
				total.Add(1)
			})
		}()
	}

	wg.Wait() // Ensure all subscribers registered

	// Publish events concurrently
	var pubWg sync.WaitGroup
	for i := 0; i < numGoroutines; i++ {
		pubWg.Add(1)
		go func() {
			defer pubWg.Done()
			for j := 0; j < numEvents; j++ {
				bus.Publish(Event{Type: EntryCreated})
			}
		}()
	}
	pubWg.Wait()

	// Allow time for all events to be delivered
	time.Sleep(200 * time.Millisecond)

	// Each of numGoroutines subscribers should receive each of numGoroutines*numEvents events
	expected := int32(numGoroutines * numGoroutines * numEvents)
	got := total.Load()
	if got != expected {
		t.Errorf("total received = %d, want %d", got, expected)
	}
}

func TestEventTimestampAutoSet(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	var received Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EntryCreated, func(e Event) {
		received = e
		wg.Done()
	})

	before := time.Now()
	bus.Publish(Event{Type: EntryCreated})
	after := time.Now()

	waitOrTimeout(t, &wg, time.Second)

	if received.Timestamp.IsZero() {
		t.Error("Timestamp should be auto-set when zero")
	}
	if received.Timestamp.Before(before) || received.Timestamp.After(after) {
		t.Errorf("Timestamp %v not between %v and %v", received.Timestamp, before, after)
	}
}

func TestEventTimestampPreserved(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	var received Event
	var wg sync.WaitGroup
	wg.Add(1)

	bus.Subscribe(EntryCreated, func(e Event) {
		received = e
		wg.Done()
	})

	fixed := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	bus.Publish(Event{Type: EntryCreated, Timestamp: fixed})

	waitOrTimeout(t, &wg, time.Second)

	if !received.Timestamp.Equal(fixed) {
		t.Errorf("Timestamp = %v, want %v (should preserve existing)", received.Timestamp, fixed)
	}
}

func TestMixedSubscribeAndPattern(t *testing.T) {
	bus := NewMemoryBus()
	defer bus.Close()

	var exactCount, patternCount atomic.Int32
	var wg sync.WaitGroup
	wg.Add(2) // One from exact, one from pattern

	bus.Subscribe(TaskCompleted, func(e Event) {
		exactCount.Add(1)
		wg.Done()
	})

	bus.SubscribePattern("task.*", func(e Event) {
		patternCount.Add(1)
		wg.Done()
	})

	bus.Publish(Event{Type: TaskCompleted})

	waitOrTimeout(t, &wg, time.Second)

	if got := exactCount.Load(); got != 1 {
		t.Errorf("exact handler called %d times, want 1", got)
	}
	if got := patternCount.Load(); got != 1 {
		t.Errorf("pattern handler called %d times, want 1", got)
	}
}

// waitOrTimeout waits for a WaitGroup with a timeout, failing the test if exceeded.
func waitOrTimeout(t *testing.T, wg *sync.WaitGroup, timeout time.Duration) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(timeout):
		t.Fatal("timed out waiting for WaitGroup")
	}
}

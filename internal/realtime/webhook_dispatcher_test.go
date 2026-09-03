package realtime

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Test Helpers
// =============================================================================

// mockWebhookDeliverer implements WebhookDeliverer for testing.
type mockWebhookDeliverer struct {
	mu     sync.Mutex
	events []types.Event
	err    error
}

func (m *mockWebhookDeliverer) Deliver(_ context.Context, evt types.Event) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events = append(m.events, evt)
	return m.err
}

func (m *mockWebhookDeliverer) receivedEvents() []types.Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]types.Event, len(m.events))
	copy(cp, m.events)
	return cp
}

// waitForEvents polls until the deliverer has received at least n events
// or the deadline expires.
func (m *mockWebhookDeliverer) waitForEvents(t *testing.T, n int, timeout time.Duration) []types.Event {
	t.Helper()
	deadline := time.After(timeout)
	for {
		received := m.receivedEvents()
		if len(received) >= n {
			return received
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d events, got %d", n, len(received))
			return nil
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}
}

// =============================================================================
// Tests
// =============================================================================

func TestWebhookDispatcher_ReceivesAndDeliversEvents(t *testing.T) {
	hub := NewEventHub()
	deliverer := &mockWebhookDeliverer{}
	dispatcher := NewWebhookDispatcher(hub, deliverer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		dispatcher.Start(ctx)
		close(done)
	}()

	// Give the dispatcher time to subscribe.
	waitForSubscribers(t, hub, 1)

	// Publish an event.
	evt := types.NewEvent("task.completed", "runner")
	evt.ProjectID = "myproj"
	evt.TaskID = "t1234567"
	hub.Publish(evt)

	// Wait for deliverer to receive it.
	received := deliverer.waitForEvents(t, 1, 2*time.Second)
	if received[0].ID != evt.ID {
		t.Errorf("expected event ID %q, got %q", evt.ID, received[0].ID)
	}
	if received[0].Type != "task.completed" {
		t.Errorf("expected event type 'task.completed', got %q", received[0].Type)
	}

	cancel()
	<-done
}

func TestWebhookDispatcher_DeliversMultipleEvents(t *testing.T) {
	hub := NewEventHub()
	deliverer := &mockWebhookDeliverer{}
	dispatcher := NewWebhookDispatcher(hub, deliverer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		dispatcher.Start(ctx)
		close(done)
	}()

	waitForSubscribers(t, hub, 1)

	// Publish multiple events.
	for i := 0; i < 5; i++ {
		evt := types.NewEvent("task.completed", "runner")
		evt.ProjectID = fmt.Sprintf("proj-%d", i)
		hub.Publish(evt)
	}

	// Wait for all events.
	received := deliverer.waitForEvents(t, 5, 2*time.Second)
	if len(received) != 5 {
		t.Fatalf("expected 5 events, got %d", len(received))
	}

	cancel()
	<-done
}

func TestWebhookDispatcher_ContinuesOnDeliveryError(t *testing.T) {
	hub := NewEventHub()
	deliverer := &mockWebhookDeliverer{err: fmt.Errorf("webhook delivery failed")}
	dispatcher := NewWebhookDispatcher(hub, deliverer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		dispatcher.Start(ctx)
		close(done)
	}()

	waitForSubscribers(t, hub, 1)

	// Publish two events — dispatcher should continue processing after error.
	evt1 := types.NewEvent("task.completed", "runner")
	hub.Publish(evt1)

	waitForSubscribers(t, hub, 1)

	evt2 := types.NewEvent("task.started", "runner")
	hub.Publish(evt2)

	// Wait for both events to be delivered (despite errors).
	received := deliverer.waitForEvents(t, 2, 2*time.Second)
	if received[0].ID != evt1.ID {
		t.Errorf("first event: expected ID %q, got %q", evt1.ID, received[0].ID)
	}
	if received[1].ID != evt2.ID {
		t.Errorf("second event: expected ID %q, got %q", evt2.ID, received[1].ID)
	}

	cancel()
	<-done
}

func TestWebhookDispatcher_StopsOnContextCancel(t *testing.T) {
	hub := NewEventHub()
	deliverer := &mockWebhookDeliverer{}
	dispatcher := NewWebhookDispatcher(hub, deliverer)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		dispatcher.Start(ctx)
		close(done)
	}()

	waitForSubscribers(t, hub, 1)

	// Cancel and verify it exits.
	cancel()

	select {
	case <-done:
		// Good — dispatcher exited.
	case <-time.After(2 * time.Second):
		t.Fatal("dispatcher did not stop after context cancellation")
	}
}

func TestWebhookDispatcher_NoEventsAfterCancel(t *testing.T) {
	hub := NewEventHub()
	deliverer := &mockWebhookDeliverer{}
	dispatcher := NewWebhookDispatcher(hub, deliverer)

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		dispatcher.Start(ctx)
		close(done)
	}()

	waitForSubscribers(t, hub, 1)

	// Cancel the dispatcher.
	cancel()
	<-done

	// Publish after shutdown — should not be delivered.
	evt := types.NewEvent("task.completed", "runner")
	hub.Publish(evt)

	time.Sleep(50 * time.Millisecond)

	received := deliverer.receivedEvents()
	if len(received) != 0 {
		t.Errorf("expected 0 events after cancel, got %d", len(received))
	}
}

func TestWebhookDispatcher_ReceivesAllEventTypes(t *testing.T) {
	hub := NewEventHub()
	deliverer := &mockWebhookDeliverer{}
	dispatcher := NewWebhookDispatcher(hub, deliverer)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		dispatcher.Start(ctx)
		close(done)
	}()

	waitForSubscribers(t, hub, 1)

	// Publish different event types — dispatcher uses an empty filter,
	// so all types should be received.
	eventTypes := []string{
		"task.started",
		"task.completed",
		"runner.connected",
		"feature.completed",
		"entry.created",
	}
	for _, et := range eventTypes {
		evt := types.NewEvent(et, "api")
		hub.Publish(evt)
	}

	received := deliverer.waitForEvents(t, len(eventTypes), 2*time.Second)
	for i, et := range eventTypes {
		if received[i].Type != et {
			t.Errorf("event %d: expected type %q, got %q", i, et, received[i].Type)
		}
	}

	cancel()
	<-done
}

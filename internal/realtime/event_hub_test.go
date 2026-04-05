package realtime

import (
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// --- Helpers ---

func newTestEvent(eventType, projectID string) types.Event {
	e := types.NewEvent(eventType, types.EventSourceAPI)
	e.ProjectID = projectID
	return e
}

func newTestEventWithFeature(eventType, projectID, featureID string) types.Event {
	e := newTestEvent(eventType, projectID)
	e.FeatureID = featureID
	return e
}

// --- Construction ---

func TestNewEventHub(t *testing.T) {
	hub := NewEventHub()
	if hub == nil {
		t.Fatal("NewEventHub() returned nil")
	}
}

func TestNewEventHubWithCapacity(t *testing.T) {
	hub := NewEventHubWithCapacity(50)
	if hub == nil {
		t.Fatal("NewEventHubWithCapacity() returned nil")
	}
}

// --- Ring Buffer: Stores Events ---

func TestPublishStoresEventInBuffer(t *testing.T) {
	hub := NewEventHub()
	evt := newTestEvent(types.EventTaskStarted, "proj-1")

	hub.Publish(evt)

	events := hub.Replay("")
	if len(events) != 1 {
		t.Fatalf("expected 1 event in buffer, got %d", len(events))
	}
	if events[0].ID != evt.ID {
		t.Errorf("event ID = %q, want %q", events[0].ID, evt.ID)
	}
}

func TestRingBufferCapacity(t *testing.T) {
	hub := NewEventHubWithCapacity(5)

	// Publish 7 events; ring should retain only the last 5.
	for i := 0; i < 7; i++ {
		hub.Publish(newTestEvent(types.EventTaskStarted, "proj-1"))
	}

	events := hub.Replay("")
	if len(events) != 5 {
		t.Fatalf("expected 5 events (capacity), got %d", len(events))
	}
}

func TestRingBufferOrder(t *testing.T) {
	hub := NewEventHubWithCapacity(3)

	ids := make([]string, 5)
	for i := 0; i < 5; i++ {
		evt := newTestEvent(types.EventTaskStarted, "proj-1")
		ids[i] = evt.ID
		hub.Publish(evt)
	}

	events := hub.Replay("")
	// Should have events with ids[2], ids[3], ids[4] in order.
	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}
	for i, evt := range events {
		if evt.ID != ids[i+2] {
			t.Errorf("event[%d].ID = %q, want %q", i, evt.ID, ids[i+2])
		}
	}
}

// --- Subscribe and Receive ---

func TestSubscribeReceivesPublishedEvents(t *testing.T) {
	hub := NewEventHub()

	ch, unsub := hub.Subscribe(EventFilter{})
	defer unsub()

	evt := newTestEvent(types.EventTaskStarted, "proj-1")
	hub.Publish(evt)

	select {
	case received := <-ch:
		if received.ID != evt.ID {
			t.Errorf("received ID = %q, want %q", received.ID, evt.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

// --- Filter: By Event Type Pattern ---

func TestFilterByExactEventType(t *testing.T) {
	hub := NewEventHub()

	ch, unsub := hub.Subscribe(EventFilter{
		TypePatterns: []string{types.EventTaskStarted},
	})
	defer unsub()

	hub.Publish(newTestEvent(types.EventTaskCompleted, "proj-1")) // should NOT match
	hub.Publish(newTestEvent(types.EventTaskStarted, "proj-1"))   // should match

	select {
	case received := <-ch:
		if received.Type != types.EventTaskStarted {
			t.Errorf("type = %q, want %q", received.Type, types.EventTaskStarted)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}

	// Ensure the completed event was NOT delivered.
	select {
	case msg := <-ch:
		t.Fatalf("should not receive second event, got: %+v", msg)
	case <-time.After(50 * time.Millisecond):
		// Good
	}
}

func TestFilterByWildcardType(t *testing.T) {
	hub := NewEventHub()

	ch, unsub := hub.Subscribe(EventFilter{
		TypePatterns: []string{"task.*"},
	})
	defer unsub()

	hub.Publish(newTestEvent(types.EventRunnerStarted, "proj-1")) // should NOT match
	hub.Publish(newTestEvent(types.EventTaskStarted, "proj-1"))   // should match

	select {
	case received := <-ch:
		if received.Type != types.EventTaskStarted {
			t.Errorf("type = %q, want %q", received.Type, types.EventTaskStarted)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestFilterByGlobalWildcard(t *testing.T) {
	hub := NewEventHub()

	ch, unsub := hub.Subscribe(EventFilter{
		TypePatterns: []string{"*"},
	})
	defer unsub()

	hub.Publish(newTestEvent(types.EventRunnerStarted, "proj-1"))

	select {
	case received := <-ch:
		if received.Type != types.EventRunnerStarted {
			t.Errorf("type = %q, want %q", received.Type, types.EventRunnerStarted)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

// --- Filter: By ProjectID ---

func TestFilterByProjectID(t *testing.T) {
	hub := NewEventHub()

	ch, unsub := hub.Subscribe(EventFilter{
		ProjectID: "proj-1",
	})
	defer unsub()

	hub.Publish(newTestEvent(types.EventTaskStarted, "proj-2")) // should NOT match
	hub.Publish(newTestEvent(types.EventTaskStarted, "proj-1")) // should match

	select {
	case received := <-ch:
		if received.ProjectID != "proj-1" {
			t.Errorf("project_id = %q, want %q", received.ProjectID, "proj-1")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

// --- Filter: By FeatureID ---

func TestFilterByFeatureID(t *testing.T) {
	hub := NewEventHub()

	ch, unsub := hub.Subscribe(EventFilter{
		FeatureID: "feat-1",
	})
	defer unsub()

	hub.Publish(newTestEventWithFeature(types.EventFeatureStarted, "proj-1", "feat-2")) // no match
	hub.Publish(newTestEventWithFeature(types.EventFeatureStarted, "proj-1", "feat-1")) // match

	select {
	case received := <-ch:
		if received.FeatureID != "feat-1" {
			t.Errorf("feature_id = %q, want %q", received.FeatureID, "feat-1")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

// --- Filter: Combined ---

func TestFilterCombined(t *testing.T) {
	hub := NewEventHub()

	ch, unsub := hub.Subscribe(EventFilter{
		TypePatterns: []string{"task.*"},
		ProjectID:    "proj-1",
		FeatureID:    "feat-1",
	})
	defer unsub()

	// Wrong type
	hub.Publish(newTestEventWithFeature(types.EventRunnerStarted, "proj-1", "feat-1"))
	// Wrong project
	hub.Publish(newTestEventWithFeature(types.EventTaskStarted, "proj-2", "feat-1"))
	// Wrong feature
	hub.Publish(newTestEventWithFeature(types.EventTaskStarted, "proj-1", "feat-2"))
	// All match
	hub.Publish(newTestEventWithFeature(types.EventTaskCompleted, "proj-1", "feat-1"))

	select {
	case received := <-ch:
		if received.Type != types.EventTaskCompleted {
			t.Errorf("type = %q, want %q", received.Type, types.EventTaskCompleted)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

// --- Replay: From Last-Event-ID ---

func TestReplayFromLastEventID(t *testing.T) {
	hub := NewEventHub()

	var ids []string
	for i := 0; i < 5; i++ {
		evt := newTestEvent(types.EventTaskStarted, "proj-1")
		ids = append(ids, evt.ID)
		hub.Publish(evt)
	}

	// Replay from the 3rd event ID — should get events 4 and 5 (index 3 and 4).
	events := hub.Replay(ids[2])
	if len(events) != 2 {
		t.Fatalf("expected 2 events after replay, got %d", len(events))
	}
	if events[0].ID != ids[3] {
		t.Errorf("events[0].ID = %q, want %q", events[0].ID, ids[3])
	}
	if events[1].ID != ids[4] {
		t.Errorf("events[1].ID = %q, want %q", events[1].ID, ids[4])
	}
}

func TestReplayUnknownIDReturnsAll(t *testing.T) {
	hub := NewEventHub()

	for i := 0; i < 3; i++ {
		hub.Publish(newTestEvent(types.EventTaskStarted, "proj-1"))
	}

	events := hub.Replay("evt_nonexistent")
	if len(events) != 3 {
		t.Fatalf("expected 3 events (all), got %d", len(events))
	}
}

func TestReplayEmptyBuffer(t *testing.T) {
	hub := NewEventHub()
	events := hub.Replay("")
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}
}

// --- Non-blocking Publish ---

func TestNonBlockingPublish(t *testing.T) {
	hub := NewEventHub()

	// Subscribe with default buffer.
	ch, unsub := hub.Subscribe(EventFilter{})
	defer unsub()

	// Fill the subscriber's channel buffer + extra events.
	// Should not block or panic.
	for i := 0; i < 200; i++ {
		hub.Publish(newTestEvent(types.EventTaskStarted, "proj-1"))
	}

	// Drain what we can — we don't care about exact count,
	// just that publishing didn't block.
	drained := 0
	for {
		select {
		case <-ch:
			drained++
		default:
			goto done
		}
	}
done:
	if drained == 0 {
		t.Fatal("expected at least some events to be received")
	}
	// We expect some drops since we published 200 events into a limited buffer.
	t.Logf("drained %d of 200 published events", drained)
}

// --- Unsubscribe ---

func TestUnsubscribeStopsDelivery(t *testing.T) {
	hub := NewEventHub()

	ch, unsub := hub.Subscribe(EventFilter{})
	unsub()

	// Publish after unsubscribe — should not block or panic.
	hub.Publish(newTestEvent(types.EventTaskStarted, "proj-1"))

	// Channel should be closed.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed after unsubscribe")
		}
	case <-time.After(50 * time.Millisecond):
		// Also acceptable
	}
}

func TestDoubleUnsubscribeSafe(t *testing.T) {
	hub := NewEventHub()

	_, unsub := hub.Subscribe(EventFilter{})

	// Should not panic.
	unsub()
	unsub()
}

// --- Multiple Subscribers ---

func TestMultipleSubscribersReceiveEvents(t *testing.T) {
	hub := NewEventHub()

	ch1, unsub1 := hub.Subscribe(EventFilter{})
	defer unsub1()
	ch2, unsub2 := hub.Subscribe(EventFilter{})
	defer unsub2()

	evt := newTestEvent(types.EventTaskStarted, "proj-1")
	hub.Publish(evt)

	for i, ch := range []<-chan types.Event{ch1, ch2} {
		select {
		case received := <-ch:
			if received.ID != evt.ID {
				t.Errorf("subscriber %d: ID = %q, want %q", i, received.ID, evt.ID)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out", i)
		}
	}
}

// --- No Subscribers ---

func TestPublishWithNoSubscribers(t *testing.T) {
	hub := NewEventHub()

	// Should not panic.
	hub.Publish(newTestEvent(types.EventTaskStarted, "proj-1"))
}

// --- Replay wraps around ring buffer correctly ---

func TestReplayAfterWrapAround(t *testing.T) {
	hub := NewEventHubWithCapacity(3)

	var ids []string
	for i := 0; i < 5; i++ {
		evt := newTestEvent(types.EventTaskStarted, "proj-1")
		ids = append(ids, evt.ID)
		hub.Publish(evt)
	}

	// Buffer should hold ids[2], ids[3], ids[4].
	// Replay from ids[3] should return ids[4] only.
	events := hub.Replay(ids[3])
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].ID != ids[4] {
		t.Errorf("event ID = %q, want %q", events[0].ID, ids[4])
	}
}

func TestReplayIDEvictedReturnsAll(t *testing.T) {
	hub := NewEventHubWithCapacity(3)

	var ids []string
	for i := 0; i < 5; i++ {
		evt := newTestEvent(types.EventTaskStarted, "proj-1")
		ids = append(ids, evt.ID)
		hub.Publish(evt)
	}

	// ids[0] has been evicted, so replay should return all buffer contents.
	events := hub.Replay(ids[0])
	if len(events) != 3 {
		t.Fatalf("expected 3 events (all in buffer), got %d", len(events))
	}
}

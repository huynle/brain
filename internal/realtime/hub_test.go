package realtime

import (
	"testing"
	"time"
)

func TestNewHub(t *testing.T) {
	hub := NewHub()
	if hub == nil {
		t.Fatal("NewHub() returned nil")
	}
}

func TestSubscribeAndPublish(t *testing.T) {
	hub := NewHub()
	ch, unsub := hub.Subscribe("project-a")
	defer unsub()

	// Publish a message
	hub.PublishProjectDirty("project-a")

	select {
	case msg := <-ch:
		if msg.Event != "project_dirty" {
			t.Errorf("event = %q, want %q", msg.Event, "project_dirty")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
	}
}

func TestPublishToCorrectProject(t *testing.T) {
	hub := NewHub()
	chA, unsubA := hub.Subscribe("project-a")
	defer unsubA()
	chB, unsubB := hub.Subscribe("project-b")
	defer unsubB()

	// Publish to project-a only
	hub.PublishProjectDirty("project-a")

	// project-a should receive
	select {
	case msg := <-chA:
		if msg.Event != "project_dirty" {
			t.Errorf("event = %q, want %q", msg.Event, "project_dirty")
		}
	case <-time.After(time.Second):
		t.Fatal("project-a: timed out waiting for message")
	}

	// project-b should NOT receive
	select {
	case msg := <-chB:
		t.Fatalf("project-b should not receive message, got: %+v", msg)
	case <-time.After(50 * time.Millisecond):
		// Expected — no message
	}
}

func TestMultipleSubscribers(t *testing.T) {
	hub := NewHub()
	ch1, unsub1 := hub.Subscribe("project-a")
	defer unsub1()
	ch2, unsub2 := hub.Subscribe("project-a")
	defer unsub2()

	hub.PublishProjectDirty("project-a")

	// Both should receive
	for i, ch := range []<-chan SSEMessage{ch1, ch2} {
		select {
		case msg := <-ch:
			if msg.Event != "project_dirty" {
				t.Errorf("subscriber %d: event = %q, want %q", i, msg.Event, "project_dirty")
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d: timed out waiting for message", i)
		}
	}
}

func TestUnsubscribeCleanup(t *testing.T) {
	hub := NewHub()
	ch, unsub := hub.Subscribe("project-a")

	// Unsubscribe
	unsub()

	// Publish after unsubscribe — should not block or panic
	hub.PublishProjectDirty("project-a")

	// Channel should be closed
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed after unsubscribe")
		}
	case <-time.After(50 * time.Millisecond):
		// Also acceptable — channel drained
	}
}

func TestPublishTaskSnapshot(t *testing.T) {
	hub := NewHub()
	ch, unsub := hub.Subscribe("project-a")
	defer unsub()

	snapshot := map[string]any{"tasks": []string{"task1"}, "count": 1}
	hub.PublishTaskSnapshot("project-a", snapshot)

	select {
	case msg := <-ch:
		if msg.Event != "tasks_snapshot" {
			t.Errorf("event = %q, want %q", msg.Event, "tasks_snapshot")
		}
		if msg.Data == nil {
			t.Error("data should not be nil")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for snapshot message")
	}
}

func TestPublishError(t *testing.T) {
	hub := NewHub()
	ch, unsub := hub.Subscribe("project-a")
	defer unsub()

	hub.PublishError("project-a", "something went wrong")

	select {
	case msg := <-ch:
		if msg.Event != "error" {
			t.Errorf("event = %q, want %q", msg.Event, "error")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for error message")
	}
}

func TestDoubleUnsubscribe(t *testing.T) {
	hub := NewHub()
	_, unsub := hub.Subscribe("project-a")

	// Should not panic
	unsub()
	unsub()
}

func TestPublishToNoSubscribers(t *testing.T) {
	hub := NewHub()

	// Should not panic
	hub.PublishProjectDirty("nonexistent")
	hub.PublishTaskSnapshot("nonexistent", nil)
	hub.PublishError("nonexistent", "test")
}

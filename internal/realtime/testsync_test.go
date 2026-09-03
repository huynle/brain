package realtime

import (
	"testing"
	"time"
)

// waitForSubscribers blocks until the hub has at least n subscribers.
//
// Every dispatcher in this package subscribes inside the goroutine its Start
// runs in, and EventHub.Publish only reaches subscribers that are already
// registered — there is no replay for a late subscriber. A test that starts a
// dispatcher and immediately publishes is therefore racing goroutine
// scheduling, and losing that race silently drops the events rather than
// delaying them.
//
// These tests used to sleep 20ms and hope. That bet held on an idle machine
// and lost under `go test ./...` parallel load, which is why the suite failed
// on a different test almost every run. Waiting for the subscriber to exist
// makes the handoff deterministic at any machine speed, and returns as soon as
// it is true rather than always burning the full sleep.
func waitForSubscribers(t *testing.T, hub *EventHub, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if hub.SubscriberCount() >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d subscriber(s); hub has %d", n, hub.SubscriberCount())
}

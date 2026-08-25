package realtime

import (
	"sync"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// churnDuration is how long each race test hammers the hub. Long enough to hit
// the window reliably, short enough to keep the suite fast.
const churnDuration = 300 * time.Millisecond

// TestHubPublishRacesWithSubscriberChurn drives Hub.publish concurrently with
// Subscribe/unsubscribe.
//
// Before the fix this failed two ways:
//   - publish ranged the live subscriber map outside the lock, so the runtime
//     threw "fatal error: concurrent map iteration and map write". That is an
//     unrecoverable throw, not a panic, so it kills the test binary outright.
//   - unsubscribe closed the channel after dropping the lock, so a publish
//     holding a stale reference panicked with "send on closed channel".
func TestHubPublishRacesWithSubscriberChurn(t *testing.T) {
	h := NewHub()
	const project = "race-project"

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					h.PublishProjectDirty(project)
					h.PublishTaskSnapshot(project, "snapshot")
				}
			}
		}()
	}

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					ch, unsub := h.Subscribe(project)
					// Drain so a full buffer doesn't mask the send path; the
					// loop exits when unsub closes the channel.
					drained := make(chan struct{})
					go func() {
						defer close(drained)
						for range ch {
						}
					}()
					unsub()
					<-drained
				}
			}
		}()
	}

	time.Sleep(churnDuration)
	close(stop)
	wg.Wait()
}

// TestEventHubPublishRacesWithUnsubscribe drives EventHub.Publish concurrently
// with Subscribe/unsubscribe.
//
// EventHub already snapshotted its subscribers under the lock, so it never had
// the map-iteration throw. It did have the send-on-closed-channel panic: the
// fan-out happens after the lock is released, so unsubscribe could close the
// channel between the snapshot and the send.
func TestEventHubPublishRacesWithUnsubscribe(t *testing.T) {
	h := NewEventHub()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					h.Publish(types.Event{
						Type:      types.EventTaskCompleted,
						ProjectID: "race-project",
					})
				}
			}
		}()
	}

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					ch, unsub := h.Subscribe(EventFilter{})
					drained := make(chan struct{})
					go func() {
						defer close(drained)
						for range ch {
						}
					}()
					unsub()
					<-drained
				}
			}
		}()
	}

	time.Sleep(churnDuration)
	close(stop)
	wg.Wait()
}

// TestHubSubscriberStillReceivesAfterChurn guards the fix against the trivial
// regression of making send a no-op: a live subscriber must still get messages
// while other subscribers come and go.
func TestHubSubscriberStillReceivesAfterChurn(t *testing.T) {
	h := NewHub()
	const project = "delivery-project"

	ch, unsub := h.Subscribe(project)
	defer unsub()

	other, otherUnsub := h.Subscribe(project)
	go func() {
		for range other {
		}
	}()
	otherUnsub()

	h.PublishProjectDirty(project)

	select {
	case msg := <-ch:
		if msg.Event != "project_dirty" {
			t.Fatalf("expected project_dirty, got %q", msg.Event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("live subscriber received nothing after another subscriber unsubscribed")
	}
}

// TestHubUnsubscribeIsIdempotent locks in the documented contract that the
// unsubscribe function is safe to call multiple times.
func TestHubUnsubscribeIsIdempotent(t *testing.T) {
	h := NewHub()
	ch, unsub := h.Subscribe("p")

	unsub()
	unsub()
	unsub()

	if _, open := <-ch; open {
		t.Fatal("expected channel to be closed after unsubscribe")
	}
}

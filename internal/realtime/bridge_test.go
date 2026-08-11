package realtime

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/huynle/brain-api/internal/events"
	"github.com/huynle/brain-api/internal/types"
)

// countingTaskProvider records how many snapshot queries the bridge runs.
type countingTaskProvider struct {
	calls    atomic.Int32
	projects sync.Map // projectID -> *atomic.Int32
}

func (p *countingTaskProvider) GetTasks(_ context.Context, projectId string) (*types.TaskListResponse, error) {
	p.calls.Add(1)
	v, _ := p.projects.LoadOrStore(projectId, &atomic.Int32{})
	v.(*atomic.Int32).Add(1)
	return &types.TaskListResponse{Tasks: []types.ResolvedTask{}, Count: 0}, nil
}

func (p *countingTaskProvider) countFor(projectId string) int {
	v, ok := p.projects.Load(projectId)
	if !ok {
		return 0
	}
	return int(v.(*atomic.Int32).Load())
}

func entryEvent(projectID string) events.Event {
	return events.Event{Type: "entry.updated", ProjectID: projectID}
}

// A burst of entry events on one project must collapse into a single
// snapshot query. This is the regression that made bulk updates quadratic:
// 100 entries meant 100 full task-list reads fanned out to every client.
func TestBridge_CoalescesBurstIntoOneSnapshot(t *testing.T) {
	bus := events.NewMemoryBus()
	hub := NewHub()
	provider := &countingTaskProvider{}

	sub := BridgeBusToHub(bus, hub, provider)
	defer sub.Unsubscribe()

	for i := 0; i < 100; i++ {
		bus.Publish(entryEvent("proj-a"))
	}

	waitFor(t, 2*time.Second, func() bool { return provider.calls.Load() >= 1 })
	// Let any stragglers land before asserting the upper bound.
	time.Sleep(snapshotCoalesceWindow * 2)

	if got := provider.countFor("proj-a"); got != 1 {
		t.Errorf("snapshot queries for proj-a = %d, want exactly 1 for a single burst", got)
	}
}

// Coalescing is per project — a burst on one project must not swallow
// another project's snapshot.
func TestBridge_CoalescesPerProjectNotGlobally(t *testing.T) {
	bus := events.NewMemoryBus()
	hub := NewHub()
	provider := &countingTaskProvider{}

	sub := BridgeBusToHub(bus, hub, provider)
	defer sub.Unsubscribe()

	for i := 0; i < 20; i++ {
		bus.Publish(entryEvent("proj-a"))
		bus.Publish(entryEvent("proj-b"))
	}

	waitFor(t, 2*time.Second, func() bool {
		return provider.countFor("proj-a") >= 1 && provider.countFor("proj-b") >= 1
	})
	time.Sleep(snapshotCoalesceWindow * 2)

	if got := provider.countFor("proj-a"); got != 1 {
		t.Errorf("proj-a snapshots = %d, want 1", got)
	}
	if got := provider.countFor("proj-b"); got != 1 {
		t.Errorf("proj-b snapshots = %d, want 1", got)
	}
}

// Events arriving after a window has elapsed must produce a fresh snapshot,
// not be swallowed. Otherwise the UI would go stale after the first burst.
func TestBridge_EventAfterWindowPublishesAgain(t *testing.T) {
	bus := events.NewMemoryBus()
	hub := NewHub()
	provider := &countingTaskProvider{}

	sub := BridgeBusToHub(bus, hub, provider)
	defer sub.Unsubscribe()

	bus.Publish(entryEvent("proj-a"))
	waitFor(t, 2*time.Second, func() bool { return provider.countFor("proj-a") == 1 })

	time.Sleep(snapshotCoalesceWindow * 2)

	bus.Publish(entryEvent("proj-a"))
	waitFor(t, 2*time.Second, func() bool { return provider.countFor("proj-a") == 2 })
}

// project_dirty is the cheap hint with no query behind it, so it must fire
// per event rather than being coalesced along with the snapshot.
func TestBridge_ProjectDirtyIsNotCoalesced(t *testing.T) {
	bus := events.NewMemoryBus()
	hub := NewHub()
	provider := &countingTaskProvider{}

	ch, unsub := hub.Subscribe("proj-a")
	defer unsub()

	sub := BridgeBusToHub(bus, hub, provider)
	defer sub.Unsubscribe()

	const n = 5
	for i := 0; i < n; i++ {
		bus.Publish(entryEvent("proj-a"))
	}

	dirty := 0
	deadline := time.After(2 * time.Second)
	for dirty < n {
		select {
		case msg := <-ch:
			if msg.Event == "project_dirty" {
				dirty++
			}
		case <-deadline:
			t.Fatalf("got %d project_dirty messages, want %d", dirty, n)
		}
	}
}

// An event with no resolvable project must be dropped, not turned into a
// snapshot query for the empty-string project.
func TestBridge_IgnoresEventWithoutProject(t *testing.T) {
	bus := events.NewMemoryBus()
	hub := NewHub()
	provider := &countingTaskProvider{}

	sub := BridgeBusToHub(bus, hub, provider)
	defer sub.Unsubscribe()

	bus.Publish(events.Event{Type: "entry.updated"})
	time.Sleep(snapshotCoalesceWindow * 3)

	if got := provider.calls.Load(); got != 0 {
		t.Errorf("snapshot queries = %d, want 0 for an event with no project", got)
	}
}

// The project can also arrive in the payload rather than the typed field.
func TestBridge_ResolvesProjectFromPayload(t *testing.T) {
	bus := events.NewMemoryBus()
	hub := NewHub()
	provider := &countingTaskProvider{}

	sub := BridgeBusToHub(bus, hub, provider)
	defer sub.Unsubscribe()

	bus.Publish(events.Event{
		Type:    "entry.updated",
		Payload: map[string]interface{}{"project": "proj-payload"},
	})

	waitFor(t, 2*time.Second, func() bool { return provider.countFor("proj-payload") == 1 })
}

// A nil TaskProvider must not panic — project_dirty should still flow.
func TestBridge_NilTaskProviderDoesNotPanic(t *testing.T) {
	bus := events.NewMemoryBus()
	hub := NewHub()

	ch, unsub := hub.Subscribe("proj-a")
	defer unsub()

	sub := BridgeBusToHub(bus, hub, nil)
	defer sub.Unsubscribe()

	bus.Publish(entryEvent("proj-a"))

	select {
	case msg := <-ch:
		if msg.Event != "project_dirty" {
			t.Errorf("first message = %q, want project_dirty", msg.Event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no project_dirty message with a nil task provider")
	}
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}

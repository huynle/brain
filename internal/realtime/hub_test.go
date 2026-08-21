package realtime

import (
	"sync"
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

func TestRunnerTopic(t *testing.T) {
	topic := RunnerTopic("runner-1")
	if topic != "runner:runner-1" {
		t.Errorf("RunnerTopic = %q, want %q", topic, "runner:runner-1")
	}
}

func TestPublishRunnerCommand(t *testing.T) {
	hub := NewHub()
	ch, unsub := hub.Subscribe(RunnerTopic("runner-1"))
	defer unsub()

	hub.PublishRunnerCommand("runner-1", "shutdown", map[string]string{"reason": "maintenance"})

	select {
	case msg := <-ch:
		if msg.Event != "command" {
			t.Errorf("event = %q, want %q", msg.Event, "command")
		}
		data, ok := msg.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("data type = %T, want map[string]interface{}", msg.Data)
		}
		if data["command"] != "shutdown" {
			t.Errorf("command = %v, want %q", data["command"], "shutdown")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runner command")
	}
}

func TestPublishRunnerCommandIsolation(t *testing.T) {
	hub := NewHub()
	ch1, unsub1 := hub.Subscribe(RunnerTopic("runner-1"))
	defer unsub1()
	ch2, unsub2 := hub.Subscribe(RunnerTopic("runner-2"))
	defer unsub2()

	// Publish to runner-1 only
	hub.PublishRunnerCommand("runner-1", "config", nil)

	// runner-1 should receive
	select {
	case msg := <-ch1:
		if msg.Event != "command" {
			t.Errorf("event = %q, want %q", msg.Event, "command")
		}
	case <-time.After(time.Second):
		t.Fatal("runner-1: timed out waiting for command")
	}

	// runner-2 should NOT receive
	select {
	case msg := <-ch2:
		t.Fatalf("runner-2 should not receive message, got: %+v", msg)
	case <-time.After(50 * time.Millisecond):
		// Expected — no message
	}
}

func TestPublishRunnerTasksChanged(t *testing.T) {
	hub := NewHub()
	ch, unsub := hub.Subscribe(RunnerTopic("runner-1"))
	defer unsub()

	hub.PublishRunnerTasksChanged("runner-1", map[string]string{"project": "test"})

	select {
	case msg := <-ch:
		if msg.Event != "tasks_changed" {
			t.Errorf("event = %q, want %q", msg.Event, "tasks_changed")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for tasks_changed message")
	}
}

func TestPublishRunnerRegistered(t *testing.T) {
	hub := NewHub()
	ch, unsub := hub.Subscribe(RunnerLifecycleTopic)
	defer unsub()

	hub.PublishRunnerRegistered(map[string]string{"runnerId": "runner-1", "hostname": "host-a"})

	select {
	case msg := <-ch:
		if msg.Event != "runner_registered" {
			t.Errorf("event = %q, want %q", msg.Event, "runner_registered")
		}
		if msg.Data == nil {
			t.Error("data should not be nil")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runner_registered event")
	}
}

func TestPublishRunnerOffline(t *testing.T) {
	hub := NewHub()
	ch, unsub := hub.Subscribe(RunnerLifecycleTopic)
	defer unsub()

	hub.PublishRunnerOffline(map[string]string{"runnerId": "runner-1", "reason": "heartbeat timeout"})

	select {
	case msg := <-ch:
		if msg.Event != "runner_offline" {
			t.Errorf("event = %q, want %q", msg.Event, "runner_offline")
		}
		if msg.Data == nil {
			t.Error("data should not be nil")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for runner_offline event")
	}
}

func TestPublishTaskClaimed(t *testing.T) {
	hub := NewHub()
	ch, unsub := hub.Subscribe("project-a")
	defer unsub()

	hub.PublishTaskClaimed("project-a", map[string]string{"taskId": "task-1", "runnerId": "runner-1"})

	select {
	case msg := <-ch:
		if msg.Event != "task_claimed" {
			t.Errorf("event = %q, want %q", msg.Event, "task_claimed")
		}
		if msg.Data == nil {
			t.Error("data should not be nil")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for task_claimed event")
	}
}

func TestPublishTaskReleased(t *testing.T) {
	hub := NewHub()
	ch, unsub := hub.Subscribe("project-a")
	defer unsub()

	hub.PublishTaskReleased("project-a", map[string]string{"taskId": "task-1", "runnerId": "runner-1"})

	select {
	case msg := <-ch:
		if msg.Event != "task_released" {
			t.Errorf("event = %q, want %q", msg.Event, "task_released")
		}
		if msg.Data == nil {
			t.Error("data should not be nil")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for task_released event")
	}
}

func TestRunnerLifecycleTopicIsolation(t *testing.T) {
	hub := NewHub()
	projectCh, projectUnsub := hub.Subscribe("my-project")
	defer projectUnsub()
	lifecycleCh, lifecycleUnsub := hub.Subscribe(RunnerLifecycleTopic)
	defer lifecycleUnsub()

	// Publish runner lifecycle event
	hub.PublishRunnerRegistered(map[string]string{"runnerId": "runner-1"})

	// Lifecycle subscriber should receive
	select {
	case msg := <-lifecycleCh:
		if msg.Event != "runner_registered" {
			t.Errorf("event = %q, want %q", msg.Event, "runner_registered")
		}
	case <-time.After(time.Second):
		t.Fatal("lifecycle: timed out waiting for runner_registered event")
	}

	// Project subscriber should NOT receive lifecycle events
	select {
	case msg := <-projectCh:
		t.Fatalf("project should not receive lifecycle message, got: %+v", msg)
	case <-time.After(50 * time.Millisecond):
		// Expected
	}

	// Publish project-scoped task_claimed event
	hub.PublishTaskClaimed("my-project", map[string]string{"taskId": "t1"})

	// Project subscriber should receive
	select {
	case msg := <-projectCh:
		if msg.Event != "task_claimed" {
			t.Errorf("event = %q, want %q", msg.Event, "task_claimed")
		}
	case <-time.After(time.Second):
		t.Fatal("project: timed out waiting for task_claimed event")
	}

	// Lifecycle subscriber should NOT receive project events
	select {
	case msg := <-lifecycleCh:
		t.Fatalf("lifecycle should not receive project message, got: %+v", msg)
	case <-time.After(50 * time.Millisecond):
		// Expected
	}
}

func TestRunnerTopicDoesNotInterfereWithProjectTopics(t *testing.T) {
	hub := NewHub()
	projectCh, projectUnsub := hub.Subscribe("my-project")
	defer projectUnsub()
	runnerCh, runnerUnsub := hub.Subscribe(RunnerTopic("runner-1"))
	defer runnerUnsub()

	// Publish to project
	hub.PublishProjectDirty("my-project")

	// Project should receive
	select {
	case <-projectCh:
	case <-time.After(time.Second):
		t.Fatal("project: timed out waiting for message")
	}

	// Runner should NOT receive project events
	select {
	case msg := <-runnerCh:
		t.Fatalf("runner should not receive project message, got: %+v", msg)
	case <-time.After(50 * time.Millisecond):
		// Expected
	}

	// Publish runner command
	hub.PublishRunnerCommand("runner-1", "dispatch", nil)

	// Runner should receive
	select {
	case <-runnerCh:
	case <-time.After(time.Second):
		t.Fatal("runner: timed out waiting for command")
	}

	// Project should NOT receive runner events
	select {
	case msg := <-projectCh:
		t.Fatalf("project should not receive runner message, got: %+v", msg)
	case <-time.After(50 * time.Millisecond):
		// Expected
	}
}

// TestPublishRacesUnsubscribe hammers publish against churning subscribers on
// the same topic. Before publish held its read lock across the fan-out, this
// reproduced two failures under -race: a "concurrent map iteration and map
// write" fatal error, and a send on a channel unsubscribe had already closed.
// The runner-shell exec stream subscribes and unsubscribes per request while
// output flows, so this is the exact production shape.
func TestPublishRacesUnsubscribe(t *testing.T) {
	hub := NewHub()
	const topic = "exec:runner-1:exec-1"

	done := make(chan struct{})
	var publishers, subscribers sync.WaitGroup

	for i := 0; i < 4; i++ {
		publishers.Add(1)
		go func() {
			defer publishers.Done()
			for {
				select {
				case <-done:
					return
				default:
					hub.Publish(topic, "exec_data", "chunk")
				}
			}
		}()
	}

	for i := 0; i < 4; i++ {
		subscribers.Add(1)
		go func() {
			defer subscribers.Done()
			for j := 0; j < 200; j++ {
				ch, unsub := hub.Subscribe(topic)
				// Drain a little so the buffer does not simply fill up,
				// exercising the send path rather than the drop path.
				select {
				case <-ch:
				default:
				}
				unsub()
			}
		}()
	}

	subscribers.Wait()
	close(done)
	publishers.Wait()

	if got := len(hub.subscribers); got != 0 {
		t.Fatalf("expected all subscriptions cleaned up, got %d topics", got)
	}
}

// A subscription's buffer is the only thing between a burst and lost data:
// publish never blocks. Bulk streams (runner-shell output) therefore ask for
// their own depth, and the shared default must not silently cap them.
func TestSubscribeWithCapacity_AbsorbsBurstTheDefaultWouldDrop(t *testing.T) {
	hub := NewHub()
	const topic = "exec:runner-1:exec-1"
	const burst = DefaultSubscriberBuffer * 8

	deep, unsubDeep := hub.SubscribeWithCapacity(topic, burst)
	defer unsubDeep()
	shallow, unsubShallow := hub.Subscribe(topic)
	defer unsubShallow()

	// Nobody reads while this runs — exactly the case of a browser slower
	// than the command producing output.
	for i := 0; i < burst; i++ {
		hub.Publish(topic, "exec_data", i)
	}

	if got := len(deep); got != burst {
		t.Errorf("deep subscriber queued %d of %d messages", got, burst)
	}
	if got := len(shallow); got != DefaultSubscriberBuffer {
		t.Errorf("default subscriber queued %d, want its %d cap", got, DefaultSubscriberBuffer)
	}
}

// A capacity below the default is raised to it: no caller gets a subscription
// shallower than the shared one.
func TestSubscribeWithCapacity_FloorsAtDefault(t *testing.T) {
	hub := NewHub()
	ch, unsub := hub.SubscribeWithCapacity("project-a", 1)
	defer unsub()

	if got := cap(ch); got != DefaultSubscriberBuffer {
		t.Errorf("capacity = %d, want the %d floor", got, DefaultSubscriberBuffer)
	}
}

// Dropping is unavoidable when a subscriber falls far enough behind, but a
// caller that must not lose data silently has to be able to see it happen.
func TestPublishTracked_ReportsDeliveredAndDropped(t *testing.T) {
	hub := NewHub()
	const topic = "exec:runner-1:exec-1"

	ch, unsub := hub.Subscribe(topic)
	defer unsub()

	// Fill the subscriber's buffer exactly.
	for i := 0; i < DefaultSubscriberBuffer; i++ {
		delivered, dropped := hub.PublishTracked(topic, "exec_data", i)
		if delivered != 1 || dropped != 0 {
			t.Fatalf("message %d: delivered=%d dropped=%d, want 1/0", i, delivered, dropped)
		}
	}

	delivered, dropped := hub.PublishTracked(topic, "exec_data", "overflow")
	if delivered != 0 || dropped != 1 {
		t.Errorf("overflow: delivered=%d dropped=%d, want 0/1", delivered, dropped)
	}

	// Draining one slot makes room again.
	<-ch
	if delivered, dropped = hub.PublishTracked(topic, "exec_data", "after drain"); delivered != 1 || dropped != 0 {
		t.Errorf("after drain: delivered=%d dropped=%d, want 1/0", delivered, dropped)
	}

	// No subscribers is not a drop — there is nobody to lose the message.
	if delivered, dropped = hub.PublishTracked("exec:runner-1:nobody", "exec_data", "x"); delivered != 0 || dropped != 0 {
		t.Errorf("unsubscribed topic: delivered=%d dropped=%d, want 0/0", delivered, dropped)
	}
}

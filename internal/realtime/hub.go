// Package realtime provides a pub/sub hub for SSE event distribution.
package realtime

import "sync"

// SSEMessage represents a message sent through the hub.
type SSEMessage struct {
	Event string
	Data  interface{}
}

// Hub manages SSE subscriptions keyed by projectId.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[string]map[chan SSEMessage]struct{}
}

// NewHub creates a new Hub.
func NewHub() *Hub {
	return &Hub{
		subscribers: make(map[string]map[chan SSEMessage]struct{}),
	}
}

// DefaultSubscriberBuffer is the queue depth of a subscription made with
// Subscribe. It suits event streams, where a burst is a handful of small
// messages; a stream that can burst thousands of frames must ask for its own
// depth via SubscribeWithCapacity.
const DefaultSubscriberBuffer = 64

// Subscribe registers a subscriber for the given projectId.
// Returns a read-only channel and an unsubscribe function.
// The unsubscribe function is safe to call multiple times.
func (h *Hub) Subscribe(projectId string) (<-chan SSEMessage, func()) {
	return h.SubscribeWithCapacity(projectId, DefaultSubscriberBuffer)
}

// SubscribeWithCapacity is Subscribe with an explicit queue depth. Publishing
// never blocks, so the buffer is the whole defence against a producer that
// outruns this subscriber: anything that does not fit is dropped. Callers
// streaming bulk output (the runner shell) size it to their real burst.
func (h *Hub) SubscribeWithCapacity(projectId string, capacity int) (<-chan SSEMessage, func()) {
	if capacity < DefaultSubscriberBuffer {
		capacity = DefaultSubscriberBuffer
	}
	ch := make(chan SSEMessage, capacity)

	h.mu.Lock()
	if h.subscribers[projectId] == nil {
		h.subscribers[projectId] = make(map[chan SSEMessage]struct{})
	}
	h.subscribers[projectId][ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	unsub := func() {
		once.Do(func() {
			// Close under the same lock that publish holds while sending,
			// so "removed from the map" and "closed" are one atomic step
			// and no publisher can be mid-send on this channel.
			h.mu.Lock()
			delete(h.subscribers[projectId], ch)
			if len(h.subscribers[projectId]) == 0 {
				delete(h.subscribers, projectId)
			}
			close(ch)
			h.mu.Unlock()
		})
	}

	return ch, unsub
}

// publish sends a message to all subscribers of the given project.
// Non-blocking: drops messages if a subscriber's buffer is full.
//
// The read lock is held across the whole fan-out, not just the map lookup.
// Releasing it early would leave this goroutine ranging over a map that
// unsubscribe can mutate concurrently (an unrecoverable "concurrent map
// iteration and map write" fatal error) and sending on a channel that
// unsubscribe may already have closed. Because every send is non-blocking,
// holding the lock here is bounded and cannot stall on a slow subscriber.
// Returns how many subscribers took the message and how many dropped it, so
// a caller that must not lose data silently (exec output) can account for
// the loss and tell the user.
func (h *Hub) publish(projectId string, msg SSEMessage) (delivered, dropped int) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for ch := range h.subscribers[projectId] {
		select {
		case ch <- msg:
			delivered++
		default:
			// Drop message if subscriber is slow
			dropped++
		}
	}
	return delivered, dropped
}

// PublishProjectDirty sends a project_dirty event to all subscribers of the project.
func (h *Hub) PublishProjectDirty(projectId string) {
	h.publish(projectId, SSEMessage{
		Event: "project_dirty",
	})
}

// PublishTaskSnapshot sends a tasks_snapshot event to all subscribers of the project.
func (h *Hub) PublishTaskSnapshot(projectId string, snapshot interface{}) {
	h.publish(projectId, SSEMessage{
		Event: "tasks_snapshot",
		Data:  snapshot,
	})
}

// PublishError sends an error event to all subscribers of the project.
func (h *Hub) PublishError(projectId string, message string) {
	h.publish(projectId, SSEMessage{
		Event: "error",
		Data:  message,
	})
}

// InstanceTopic returns the topic key for an OpenCode instance's event
// stream, namespaced by runner so instance IDs cannot collide across runners.
func InstanceTopic(runnerID, instanceID string) string {
	return "instance:" + runnerID + ":" + instanceID
}

// ExecTopic returns the topic key for one runner-shell command's output
// stream, namespaced by runner so exec IDs cannot collide across runners.
func ExecTopic(runnerID, execID string) string {
	return "exec:" + runnerID + ":" + execID
}

// Publish sends an arbitrary event to all subscribers of a topic. Used by
// the bridge hub to fan out instance events to browser SSE streams.
func (h *Hub) Publish(topic string, event string, data interface{}) {
	h.publish(topic, SSEMessage{Event: event, Data: data})
}

// PublishTracked is Publish with delivery accounting: it reports how many
// subscribers received the event and how many were too far behind to take
// it. Use it where a dropped message must be surfaced rather than swallowed.
func (h *Hub) PublishTracked(topic string, event string, data interface{}) (delivered, dropped int) {
	return h.publish(topic, SSEMessage{Event: event, Data: data})
}

// RunnerTopic returns the topic key for a runner's SSE stream.
// Uses "runner:" prefix to namespace runner subscriptions separately from project subscriptions.
func RunnerTopic(runnerID string) string {
	return "runner:" + runnerID
}

// PublishRunnerCommand sends a command event to a specific runner's SSE stream.
func (h *Hub) PublishRunnerCommand(runnerID string, command string, payload interface{}) {
	h.publish(RunnerTopic(runnerID), SSEMessage{
		Event: "command",
		Data: map[string]interface{}{
			"command": command,
			"payload": payload,
		},
	})
}

// PublishRunnerTasksChanged sends a tasks_changed event to a specific runner's SSE stream.
func (h *Hub) PublishRunnerTasksChanged(runnerID string, data interface{}) {
	h.publish(RunnerTopic(runnerID), SSEMessage{
		Event: "tasks_changed",
		Data:  data,
	})
}

// PublishRunnerLog sends a runner_log event to all subscribers of the project.
// Used to re-broadcast log lines from runners to TUI clients via SSE.
func (h *Hub) PublishRunnerLog(projectId string, data interface{}) {
	h.publish(projectId, SSEMessage{
		Event: "runner_log",
		Data:  data,
	})
}

// RunnerLifecycleTopic is the global topic for runner lifecycle events.
// TUI clients subscribe to this to receive runner_registered, runner_offline, etc.
const RunnerLifecycleTopic = "runners"

// PublishRunnerRegistered sends a runner_registered event to all lifecycle subscribers.
func (h *Hub) PublishRunnerRegistered(data interface{}) {
	h.publish(RunnerLifecycleTopic, SSEMessage{
		Event: "runner_registered",
		Data:  data,
	})
}

// PublishRunnerOffline sends a runner_offline event to all lifecycle subscribers.
func (h *Hub) PublishRunnerOffline(data interface{}) {
	h.publish(RunnerLifecycleTopic, SSEMessage{
		Event: "runner_offline",
		Data:  data,
	})
}

// PublishTaskClaimed sends a task_claimed event to all subscribers of the project.
func (h *Hub) PublishTaskClaimed(projectId string, data interface{}) {
	h.publish(projectId, SSEMessage{
		Event: "task_claimed",
		Data:  data,
	})
}

// PublishTaskReleased sends a task_released event to all subscribers of the project.
func (h *Hub) PublishTaskReleased(projectId string, data interface{}) {
	h.publish(projectId, SSEMessage{
		Event: "task_released",
		Data:  data,
	})
}

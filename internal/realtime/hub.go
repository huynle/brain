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

// Subscribe registers a subscriber for the given projectId.
// Returns a read-only channel and an unsubscribe function.
// The unsubscribe function is safe to call multiple times.
func (h *Hub) Subscribe(projectId string) (<-chan SSEMessage, func()) {
	ch := make(chan SSEMessage, 64)

	h.mu.Lock()
	if h.subscribers[projectId] == nil {
		h.subscribers[projectId] = make(map[chan SSEMessage]struct{})
	}
	h.subscribers[projectId][ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	unsub := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subscribers[projectId], ch)
			if len(h.subscribers[projectId]) == 0 {
				delete(h.subscribers, projectId)
			}
			h.mu.Unlock()
			close(ch)
		})
	}

	return ch, unsub
}

// publish sends a message to all subscribers of the given project.
// Non-blocking: drops messages if a subscriber's buffer is full.
func (h *Hub) publish(projectId string, msg SSEMessage) {
	h.mu.RLock()
	subs := h.subscribers[projectId]
	h.mu.RUnlock()

	for ch := range subs {
		select {
		case ch <- msg:
		default:
			// Drop message if subscriber is slow
		}
	}
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

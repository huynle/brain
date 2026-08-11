package realtime

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/huynle/brain-api/internal/events"
	"github.com/huynle/brain-api/internal/types"
)

// TaskProvider is a minimal interface for fetching task snapshots.
// Implemented by service.TaskServiceImpl (via api.TaskService).
type TaskProvider interface {
	GetTasks(ctx context.Context, projectId string) (*types.TaskListResponse, error)
}

// snapshotCoalesceWindow is how long the bridge waits for further entry
// events on a project before building its task snapshot.
//
// Bulk operations emit one entry.* event per entry. Without coalescing, a
// 100-task feature update ran 100 full GetTasks queries and fanned 100
// identical snapshots out to every subscriber — quadratic work for a result
// the last snapshot alone would have conveyed. The window trades a few tens
// of milliseconds of latency for collapsing that to one.
const snapshotCoalesceWindow = 120 * time.Millisecond

// BridgeBusToHub subscribes the SSE Hub to EventBus entry events, translating
// them into project_dirty + tasks_snapshot SSE messages for backward
// compatibility.
//
// This replaces the direct notifyProjectChanged() calls that previously lived
// in the API handler layer.
//
// project_dirty is published immediately on every event — it is a cheap hint
// with no query behind it, and clients use it to show activity. The expensive
// tasks_snapshot is coalesced per project (see snapshotCoalesceWindow).
func BridgeBusToHub(bus events.Bus, hub *Hub, tasks TaskProvider) events.Subscription {
	c := newSnapshotCoalescer(hub, tasks)
	return bus.SubscribePattern("entry.*", func(e events.Event) {
		projectID := e.ProjectID
		if projectID == "" {
			// Try to extract from payload
			if p, ok := e.Payload["project"].(string); ok {
				projectID = p
			}
		}
		if projectID == "" {
			return
		}

		hub.PublishProjectDirty(projectID)

		if tasks != nil {
			c.schedule(projectID)
		}
	})
}

// snapshotCoalescer batches per-project snapshot publishes.
//
// One timer per project with a pending snapshot. Re-scheduling inside the
// window is a no-op rather than a timer reset, so a continuous stream of
// events still produces a snapshot every window rather than starving until
// the stream stops.
type snapshotCoalescer struct {
	hub   *Hub
	tasks TaskProvider

	mu      sync.Mutex
	pending map[string]bool
}

func newSnapshotCoalescer(hub *Hub, tasks TaskProvider) *snapshotCoalescer {
	return &snapshotCoalescer{
		hub:     hub,
		tasks:   tasks,
		pending: make(map[string]bool),
	}
}

func (c *snapshotCoalescer) schedule(projectID string) {
	c.mu.Lock()
	if c.pending[projectID] {
		c.mu.Unlock()
		return
	}
	c.pending[projectID] = true
	c.mu.Unlock()

	time.AfterFunc(snapshotCoalesceWindow, func() {
		c.mu.Lock()
		delete(c.pending, projectID)
		c.mu.Unlock()
		c.publish(projectID)
	})
}

// publish fetches and broadcasts one task snapshot for a project.
func (c *snapshotCoalescer) publish(projectID string) {
	resp, err := c.tasks.GetTasks(context.Background(), projectID)
	if err != nil {
		slog.Debug("bridge: failed to fetch tasks for snapshot",
			"project", projectID, "error", err)
		return
	}
	c.hub.PublishTaskSnapshot(projectID, types.SSETasksSnapshotData{
		SSEEventData: types.SSEEventData{
			Type:      types.SSEEventTasksSnapshot,
			Transport: "sse",
			Timestamp: types.TimeNowUTC().Format("2006-01-02T15:04:05Z"),
			ProjectID: projectID,
		},
		Tasks:  resp.Tasks,
		Count:  resp.Count,
		Stats:  resp.Stats,
		Cycles: resp.Cycles,
	})
}

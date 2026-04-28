package realtime

import (
	"context"
	"log/slog"

	"github.com/huynle/brain-api/internal/events"
	"github.com/huynle/brain-api/internal/types"
)

// TaskProvider is a minimal interface for fetching task snapshots.
// Implemented by service.TaskServiceImpl (via api.TaskService).
type TaskProvider interface {
	GetTasks(ctx context.Context, projectId string) (*types.TaskListResponse, error)
}

// BridgeBusToHub subscribes the SSE Hub to EventBus entry events, translating
// them into project_dirty + tasks_snapshot SSE messages for backward compatibility.
//
// This replaces the direct notifyProjectChanged() calls that previously lived
// in the API handler layer.
func BridgeBusToHub(bus events.Bus, hub *Hub, tasks TaskProvider) events.Subscription {
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
			resp, err := tasks.GetTasks(context.Background(), projectID)
			if err != nil {
				slog.Debug("bridge: failed to fetch tasks for snapshot",
					"project", projectID, "error", err)
				return
			}
			hub.PublishTaskSnapshot(projectID, types.SSETasksSnapshotData{
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
	})
}

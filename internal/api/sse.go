package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// DefaultHeartbeatInterval is the default interval for SSE heartbeat events.
var DefaultHeartbeatInterval = 30 * time.Second

// HandleSSEStream handles GET /tasks/{projectId}/stream — SSE event stream.
func (h *Handler) HandleSSEStream(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")

	// Check that the ResponseWriter supports flushing
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", "streaming not supported")
		return
	}

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	// Subscribe to hub for this project
	ch, unsub := h.hub.Subscribe(projectId)
	defer unsub()

	now := types.TimeNowUTC().Format(time.RFC3339)

	// Send connected event
	writeSSEEvent(w, "connected", types.SSEConnectedData{
		SSEEventData: types.SSEEventData{
			Type:      types.SSEEventConnected,
			Transport: "sse",
			Timestamp: now,
			ProjectID: projectId,
		},
	})
	flusher.Flush()

	// Send initial task snapshot
	if h.tasks != nil {
		resp, err := h.tasks.GetTasks(r.Context(), projectId)
		if err == nil {
			writeSSEEvent(w, "tasks_snapshot", types.SSETasksSnapshotData{
				SSEEventData: types.SSEEventData{
					Type:      types.SSEEventTasksSnapshot,
					Transport: "sse",
					Timestamp: types.TimeNowUTC().Format(time.RFC3339),
					ProjectID: projectId,
				},
				Tasks:  resp.Tasks,
				Count:  resp.Count,
				Stats:  resp.Stats,
				Cycles: resp.Cycles,
			})
			flusher.Flush()
		}
	}

	// Start heartbeat ticker
	heartbeat := time.NewTicker(DefaultHeartbeatInterval)
	defer heartbeat.Stop()

	// Event loop
	for {
		select {
		case <-r.Context().Done():
			// Client disconnected
			return

		case msg, ok := <-ch:
			if !ok {
				// Channel closed
				return
			}
			writeSSEEvent(w, msg.Event, msg.Data)
			flusher.Flush()

		case <-heartbeat.C:
			writeSSEEvent(w, "heartbeat", types.SSEEventData{
				Type:      types.SSEEventHeartbeat,
				Transport: "sse",
				Timestamp: types.TimeNowUTC().Format(time.RFC3339),
				ProjectID: projectId,
			})
			flusher.Flush()
		}
	}
}

// writeSSEEvent writes a single SSE event to the writer.
func writeSSEEvent(w http.ResponseWriter, event string, data interface{}) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)
}

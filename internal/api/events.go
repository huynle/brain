package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// HandleIngestEvents handles POST /events — ingest a batch of events from runners.
// Accepts a JSON array of Event structs. Validates, stores in ring buffer,
// and publishes to EventHub.
func (h *Handler) HandleIngestEvents(w http.ResponseWriter, r *http.Request) {
	if h.events == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "event service not configured")
		return
	}

	var events []types.Event
	if err := json.NewDecoder(r.Body).Decode(&events); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON: "+err.Error())
		return
	}

	if err := h.events.Ingest(r.Context(), events); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}

	WriteJSON(w, http.StatusAccepted, map[string]any{
		"accepted": len(events),
	})
}

// HandleEventStream handles GET /events/stream — SSE event stream with filters.
// Supports query param filters: ?type=task.*&project_id=brain-api&feature_id=auth
// Supports Last-Event-ID header for reconnection replay.
func (h *Handler) HandleEventStream(w http.ResponseWriter, r *http.Request) {
	if h.events == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "event service not configured")
		return
	}

	// Check that the ResponseWriter supports flushing.
	flusher, ok := w.(http.Flusher)
	if !ok {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", "streaming not supported")
		return
	}

	// Parse filter query params.
	filters := make(map[string]string)
	for _, key := range []string{"type", "project_id", "feature_id", "source"} {
		if v := r.URL.Query().Get(key); v != "" {
			filters[key] = v
		}
	}

	// Set SSE headers.
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// Handle Last-Event-ID reconnection replay.
	if lastEventID := r.Header.Get("Last-Event-ID"); lastEventID != "" {
		replayEvents, _ := h.events.Recent(r.Context(), 1000, nil)
		replaying := false
		for _, evt := range replayEvents {
			if evt.ID == lastEventID {
				replaying = true
				continue
			}
			if replaying && matchesEventFilters(evt, filters) {
				writeEventSSE(w, evt)
			}
		}
		flusher.Flush()
	}

	// Subscribe to real-time events.
	ch, unsub := h.events.Subscribe(r.Context(), filters)
	defer unsub()

	// Start heartbeat ticker.
	heartbeat := time.NewTicker(DefaultHeartbeatInterval)
	defer heartbeat.Stop()

	// Event loop.
	for {
		select {
		case <-r.Context().Done():
			return

		case evt, ok := <-ch:
			if !ok {
				return
			}
			writeEventSSE(w, evt)
			flusher.Flush()

		case <-heartbeat.C:
			// Write SSE comment as heartbeat to keep connection alive.
			fmt.Fprintf(w, ": heartbeat %s\n\n", types.TimeNowUTC().Format(time.RFC3339))
			flusher.Flush()
		}
	}
}

// HandleRecentEvents handles GET /events/recent — returns last N events as JSON.
// Supports query params: ?limit=100&type=task.*&project_id=brain-api&feature_id=auth
func (h *Handler) HandleRecentEvents(w http.ResponseWriter, r *http.Request) {
	if h.events == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "event service not configured")
		return
	}

	// Parse limit (default 100, max 1000).
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			WriteError(w, http.StatusBadRequest, "Bad Request", "limit must be a positive integer")
			return
		}
		if n > 1000 {
			n = 1000
		}
		limit = n
	}

	// Parse filter query params.
	filters := make(map[string]string)
	for _, key := range []string{"type", "project_id", "feature_id", "source"} {
		if v := r.URL.Query().Get(key); v != "" {
			filters[key] = v
		}
	}

	events, err := h.events.Recent(r.Context(), limit, filters)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	// Report the window that was actually searched. Without it a zero count is
	// silent about whether the buffer holds 1000 events and none matched, or
	// holds three because the process restarted a minute ago.
	WriteJSON(w, http.StatusOK, map[string]any{
		"events":   events,
		"count":    len(events),
		"coverage": h.events.Coverage(),
	})
}

// writeEventSSE writes a single event as an SSE event with id field.
func writeEventSSE(w http.ResponseWriter, evt types.Event) {
	jsonData, err := json.Marshal(evt)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "id: %s\nevent: %s\ndata: %s\n\n", evt.ID, evt.Type, jsonData)
}

// matchesEventFilters checks whether an event matches the given filter map.
// Used for Last-Event-ID replay filtering.
func matchesEventFilters(evt types.Event, filters map[string]string) bool {
	for key, val := range filters {
		switch key {
		case "project_id":
			if evt.ProjectID != val {
				return false
			}
		case "type":
			if !types.MatchEventPattern(val, evt.Type) {
				return false
			}
		case "source":
			if evt.Source != val {
				return false
			}
		case "feature_id":
			if evt.FeatureID != val {
				return false
			}
		}
	}
	return true
}

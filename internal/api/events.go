package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/events"
)

// emitEventRequest is the JSON body for POST /api/v1/events/emit.
type emitEventRequest struct {
	Type     string         `json:"type"`
	Payload  map[string]any `json:"payload,omitempty"`
	DedupKey string         `json:"dedup_key,omitempty"`
}

// HandleEmitEvent handles POST /api/v1/events/emit.
// It allows runners and external systems to publish events into the event bus.
func (h *Handler) HandleEmitEvent(w http.ResponseWriter, r *http.Request) {
	if h.eventBus == nil {
		WriteError(w, http.StatusServiceUnavailable, "Service Unavailable", "Event bus not configured")
		return
	}

	var req emitEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Invalid JSON body")
		return
	}

	// Validate: event type must be non-empty
	eventType := strings.TrimSpace(req.Type)
	if eventType == "" {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Event type is required")
		return
	}

	// Build and publish the event
	event := events.Event{
		Type:      events.EventType(eventType),
		Payload:   req.Payload,
		DedupKey:  req.DedupKey,
		Source:    "external",
		Timestamp: time.Now(),
	}

	h.eventBus.Publish(event)

	WriteJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})
}

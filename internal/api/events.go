package api

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/events"
)

// DefaultWebhookMaxBodySize is the default maximum request body size for webhooks (1MB).
const DefaultWebhookMaxBodySize int64 = 1 << 20

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

// webhookTriggerResponse is the JSON body for a successful webhook trigger.
type webhookTriggerResponse struct {
	Status      string              `json:"status"`
	Matched     int                 `json:"matched"`
	Automations []webhookMatchEntry `json:"automations"`
}

// webhookMatchEntry is a matched automation in the webhook trigger response.
type webhookMatchEntry struct {
	ID        string `json:"id"`
	Path      string `json:"path"`
	ProjectID string `json:"project_id,omitempty"`
}

// HandleWebhookTrigger handles POST /api/v1/events/webhook/{path...}.
// External systems (GitHub, CI, etc.) POST arbitrary JSON to trigger automations
// whose trigger.type=webhook and trigger.webhook matches the URL path.
func (h *Handler) HandleWebhookTrigger(w http.ResponseWriter, r *http.Request) {
	if h.eventBus == nil {
		WriteError(w, http.StatusServiceUnavailable, "Service Unavailable", "Event bus not configured")
		return
	}

	// Extract the webhook path from the URL.
	// chi.URLParam with "*" captures everything after /webhook/
	webhookPath := chi.URLParam(r, "*")
	if webhookPath == "" {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Webhook path is required")
		return
	}

	// Enforce payload size limit
	limited := io.LimitReader(r.Body, DefaultWebhookMaxBodySize+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Failed to read request body")
		return
	}
	if int64(len(body)) > DefaultWebhookMaxBodySize {
		WriteError(w, http.StatusRequestEntityTooLarge, "Payload Too Large",
			"Request body exceeds maximum size of 1MB")
		return
	}

	// Parse JSON body (empty body is allowed — treated as empty payload)
	var payload map[string]any
	if len(body) > 0 {
		if err := json.Unmarshal(body, &payload); err != nil {
			WriteError(w, http.StatusBadRequest, "Bad Request", "Invalid JSON body")
			return
		}
	}
	if payload == nil {
		payload = make(map[string]any)
	}

	// Inject webhook metadata into payload
	payload["webhook_path"] = webhookPath

	// Find matching automations
	var matched []webhookMatchEntry
	if h.automations != nil {
		automations, err := h.automations.ListActiveAutomations(r.Context())
		if err == nil {
			for _, auto := range automations {
				if auto.Trigger.Type == "webhook" && matchWebhookPath(auto.Trigger.Webhook, webhookPath) {
					matched = append(matched, webhookMatchEntry{
						ID:        auto.ID,
						Path:      auto.Path,
						ProjectID: auto.ProjectID,
					})
				}
			}
		}
	}

	// Publish webhook.received event with full request body
	event := events.Event{
		Type:      events.WebhookReceived,
		Payload:   payload,
		Source:    "webhook",
		Timestamp: time.Now(),
	}
	h.eventBus.Publish(event)

	// Return 200 with matched automations, or 204 if no match
	if len(matched) == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	WriteJSON(w, http.StatusOK, webhookTriggerResponse{
		Status:      "triggered",
		Matched:     len(matched),
		Automations: matched,
	})
}

// matchWebhookPath checks if a trigger's webhook path matches the incoming path.
// Both paths are normalized by trimming leading/trailing slashes for comparison.
func matchWebhookPath(triggerPath, incomingPath string) bool {
	return normalizePath(triggerPath) == normalizePath(incomingPath)
}

// normalizePath strips leading and trailing slashes for consistent comparison.
func normalizePath(p string) string {
	return strings.Trim(p, "/")
}

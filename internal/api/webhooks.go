package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// HandleCreateWebhook handles POST /webhooks.
func (h *Handler) HandleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	if h.webhooks == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "Webhook service not configured")
		return
	}

	var req types.CreateWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Invalid JSON body")
		return
	}

	// Validate required fields
	var details []types.ValidationDetail
	if req.Name == "" {
		details = append(details, types.ValidationDetail{Field: "name", Message: "required"})
	}
	if req.URL == "" {
		details = append(details, types.ValidationDetail{Field: "url", Message: "required"})
	}
	if len(req.Events) == 0 {
		details = append(details, types.ValidationDetail{Field: "events", Message: "must be non-empty"})
	}
	if len(details) > 0 {
		WriteValidationError(w, details)
		return
	}

	resp, err := h.webhooks.Create(r.Context(), req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "already exists") {
			WriteError(w, http.StatusConflict, "Conflict", errMsg)
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", errMsg)
		return
	}

	WriteJSON(w, http.StatusCreated, resp)
}

// HandleListWebhooks handles GET /webhooks.
func (h *Handler) HandleListWebhooks(w http.ResponseWriter, r *http.Request) {
	if h.webhooks == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "Webhook service not configured")
		return
	}

	enabledOnly := r.URL.Query().Get("enabled") == "true"

	webhooks, err := h.webhooks.List(r.Context(), enabledOnly)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	// Ensure we return an empty array, not null
	if webhooks == nil {
		webhooks = []types.WebhookResponse{}
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"webhooks": webhooks,
	})
}

// HandleGetWebhook handles GET /webhooks/{id}.
func (h *Handler) HandleGetWebhook(w http.ResponseWriter, r *http.Request) {
	if h.webhooks == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "Webhook service not configured")
		return
	}

	id := chi.URLParam(r, "id")

	resp, err := h.webhooks.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Webhook not found: %s", id))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, resp)
}

// HandleUpdateWebhook handles PATCH /webhooks/{id}.
func (h *Handler) HandleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	if h.webhooks == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "Webhook service not configured")
		return
	}

	id := chi.URLParam(r, "id")

	var req types.UpdateWebhookRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Invalid JSON body")
		return
	}

	// Validate: if URL provided, must not be empty
	var details []types.ValidationDetail
	if req.URL != nil && *req.URL == "" {
		details = append(details, types.ValidationDetail{Field: "url", Message: "must not be empty"})
	}
	if req.Events != nil && len(req.Events) == 0 {
		details = append(details, types.ValidationDetail{Field: "events", Message: "must be non-empty"})
	}
	if len(details) > 0 {
		WriteValidationError(w, details)
		return
	}

	resp, err := h.webhooks.Update(r.Context(), id, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Webhook not found: %s", id))
			return
		}
		errMsg := err.Error()
		if strings.Contains(errMsg, "already exists") {
			WriteError(w, http.StatusConflict, "Conflict", errMsg)
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", errMsg)
		return
	}

	WriteJSON(w, http.StatusOK, resp)
}

// HandleDeleteWebhook handles DELETE /webhooks/{id}.
func (h *Handler) HandleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	if h.webhooks == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "Webhook service not configured")
		return
	}

	id := chi.URLParam(r, "id")

	err := h.webhooks.Delete(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Webhook not found: %s", id))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"success": true,
	})
}

// HandleTestWebhook handles POST /webhooks/{id}/test.
// It fires a synthetic test event to the specified webhook and returns the
// delivery result synchronously, allowing users to verify their webhook URL
// and payload format without creating a real event.
func (h *Handler) HandleTestWebhook(w http.ResponseWriter, r *http.Request) {
	if h.webhooks == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "Webhook service not configured")
		return
	}

	webhookID := chi.URLParam(r, "id")

	// Build a synthetic test event
	testEvent := types.Event{
		ID:        "evt_test_" + webhookID,
		Type:      "webhook.test",
		Source:    "api",
		Timestamp: time.Now().UTC(),
		Metadata:  map[string]string{"webhook_id": webhookID},
	}

	delivery, err := h.webhooks.TestDeliver(r.Context(), webhookID, testEvent)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Webhook not found: %s", webhookID))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, delivery)
}

// HandleListWebhookDeliveries handles GET /webhooks/{id}/deliveries.
func (h *Handler) HandleListWebhookDeliveries(w http.ResponseWriter, r *http.Request) {
	if h.webhooks == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "Webhook service not configured")
		return
	}

	id := chi.URLParam(r, "id")

	// Parse optional limit query param (default: 50)
	limit := 50
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		parsed, err := strconv.Atoi(limitStr)
		if err != nil || parsed < 1 {
			WriteError(w, http.StatusBadRequest, "Bad Request", "limit must be a positive integer")
			return
		}
		limit = parsed
	}

	deliveries, err := h.webhooks.ListDeliveries(r.Context(), id, limit)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Webhook not found: %s", id))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	// Ensure we return an empty array, not null
	if deliveries == nil {
		deliveries = []types.WebhookDeliveryResponse{}
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"deliveries": deliveries,
	})
}

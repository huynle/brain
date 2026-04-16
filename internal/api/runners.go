package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// HandleRegisterRunner handles POST /runners/register.
func (h *Handler) HandleRegisterRunner(w http.ResponseWriter, r *http.Request) {
	var req types.RegisterRunnerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}

	// Validate required fields
	var details []types.ValidationDetail
	if req.RunnerID == "" {
		details = append(details, types.ValidationDetail{
			Field: "runner_id", Message: "runner_id is required",
		})
	}
	if req.Hostname == "" {
		details = append(details, types.ValidationDetail{
			Field: "hostname", Message: "hostname is required",
		})
	}
	if len(details) > 0 {
		WriteValidationError(w, details)
		return
	}

	info, err := h.runners.Register(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	// Publish runners_update SSE event so TUI picks up the new runner
	h.publishRunnersUpdate(r.Context())

	WriteJSON(w, http.StatusOK, info)
}

// HandleHeartbeatRunner handles POST /runners/heartbeat.
func (h *Handler) HandleHeartbeatRunner(w http.ResponseWriter, r *http.Request) {
	var req types.HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}

	if req.RunnerID == "" {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "runner_id", Message: "runner_id is required"},
		})
		return
	}

	err := h.runners.Heartbeat(r.Context(), req)
	if err != nil {
		// Check if runner not found
		if errors.Is(err, ErrNotFound) || containsStr(err.Error(), "not found") {
			WriteError(w, http.StatusNotFound, "Not Found", "runner not found: "+req.RunnerID)
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	// Publish runners_update SSE event so TUI tracks heartbeat state
	h.publishRunnersUpdate(r.Context())

	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// HandleListRunners handles GET /runners.
func (h *Handler) HandleListRunners(w http.ResponseWriter, r *http.Request) {
	resp, err := h.runners.List(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, resp)
}

// containsStr is a simple substring check for error messages.
func containsStr(s, substr string) bool {
	return len(substr) <= len(s) && searchStr(s, substr)
}

func searchStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// publishRunnersUpdate fetches the current runner list and broadcasts it
// via SSE to all connected TUI clients.
func (h *Handler) publishRunnersUpdate(ctx context.Context) {
	if h.hub == nil || h.runners == nil {
		return
	}
	resp, err := h.runners.List(ctx)
	if err != nil {
		return
	}
	h.hub.PublishRunnersUpdate(types.SSERunnersUpdateData{
		SSEEventData: types.SSEEventData{
			Type:      types.SSEEventRunnersUpdate,
			Transport: "sse",
			Timestamp: time.Now().UTC().Format(time.RFC3339),
		},
		Runners: resp.Runners,
		Total:   resp.Total,
	})
}

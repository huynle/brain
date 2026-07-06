package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// HandleRunAutomation handles POST /api/v1/automations/run.
//
// Body: {"path": "<automation path or id>"}. The run goes through the same
// server-side task-generation code as cron/event triggers, so a manual run
// can never diverge from scheduled behavior (the PWA and TUI used to build
// the task client-side, each with their own drift).
func (h *Handler) HandleRunAutomation(w http.ResponseWriter, r *http.Request) {
	if h.automationRun == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "automation run service is not configured")
		return
	}
	var req struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		WriteError(w, http.StatusBadRequest, "Bad Request", "body must be {\"path\": \"<automation path or id>\"}")
		return
	}
	taskID, err := h.automationRun.RunAutomationNow(r.Context(), req.Path)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Automation not found: %s", req.Path))
			return
		}
		WriteError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	if taskID == "" {
		// Generation was skipped (e.g. max_concurrent); the run audit records why.
		WriteJSON(w, http.StatusConflict, map[string]any{
			"skipped": true,
			"message": "task generation skipped (see automation run audit)",
		})
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"task_id": taskID})
}

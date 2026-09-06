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
	// `project` is optional and scopes the run. It is the only way a caller
	// can say which project a GLOBAL automation is being run for; an entry
	// that owns a project ignores it.
	var req struct {
		Path    string `json:"path"`
		Project string `json:"project"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Path == "" {
		WriteError(w, http.StatusBadRequest, "Bad Request", "body must be {\"path\": \"<automation path or id>\", \"project\": \"<optional project>\"}")
		return
	}
	taskIDs, err := h.automationRun.RunAutomationNow(r.Context(), req.Path, req.Project)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Automation not found: %s", req.Path))
			return
		}
		WriteError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}
	if len(taskIDs) == 0 {
		// Generation was skipped (e.g. max_concurrent); the run audit records why.
		WriteJSON(w, http.StatusConflict, map[string]any{
			"skipped": true,
			"message": "task generation skipped (see automation run audit)",
		})
		return
	}
	// `task_id` stays in the response for every existing reader; a fan-out
	// over several projects reports all of them in `task_ids`.
	WriteJSON(w, http.StatusOK, map[string]any{
		"task_id":  taskIDs[0],
		"task_ids": taskIDs,
	})
}

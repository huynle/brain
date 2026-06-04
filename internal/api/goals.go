package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// HandleCreateGoal handles POST /goals — create a goal automation.
func (h *Handler) HandleCreateGoal(w http.ResponseWriter, r *http.Request) {
	if h.goalService == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "goal service not configured")
		return
	}

	var req types.CreateGoalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON: "+err.Error())
		return
	}

	summary, err := h.goalService.CreateGoal(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", err.Error())
		return
	}

	WriteJSON(w, http.StatusCreated, summary)
}

// HandleUpdateGoal handles PATCH /goals/{goalId} — update a goal automation.
func (h *Handler) HandleUpdateGoal(w http.ResponseWriter, r *http.Request) {
	if h.goalService == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "goal service not configured")
		return
	}

	goalID := chi.URLParam(r, "goalId")
	if goalID == "" {
		WriteError(w, http.StatusBadRequest, "Bad Request", "goal id is required")
		return
	}

	var req types.UpdateGoalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON: "+err.Error())
		return
	}

	summary, err := h.goalService.UpdateGoal(r.Context(), goalID, req)
	if err != nil {
		writeGoalError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, summary)
}

// HandleListGoals handles GET /goals — list goal automations.
// Supports query params: ?project=brain-api&feature_id=auth-system
func (h *Handler) HandleListGoals(w http.ResponseWriter, r *http.Request) {
	if h.goalService == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "goal service not configured")
		return
	}

	project := r.URL.Query().Get("project")
	featureID := r.URL.Query().Get("feature_id")

	goals, err := h.goalService.ListGoals(r.Context(), project, featureID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"goals": goals,
		"count": len(goals),
	})
}

// HandleRunGoal handles POST /goals/{goalId}/run — trigger a manual reconcile.
func (h *Handler) HandleRunGoal(w http.ResponseWriter, r *http.Request) {
	if h.goalService == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "goal service not configured")
		return
	}

	goalID := chi.URLParam(r, "goalId")
	if goalID == "" {
		WriteError(w, http.StatusBadRequest, "Bad Request", "goal id is required")
		return
	}

	audit, err := h.goalService.RunGoal(r.Context(), goalID)
	if err != nil {
		writeGoalError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, audit)
}

// HandleGoalProgress handles GET /goals/{goalId}/progress — linked-task progress.
func (h *Handler) HandleGoalProgress(w http.ResponseWriter, r *http.Request) {
	if h.goalService == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "goal service not configured")
		return
	}

	goalID := chi.URLParam(r, "goalId")
	if goalID == "" {
		WriteError(w, http.StatusBadRequest, "Bad Request", "goal id is required")
		return
	}

	progress, err := h.goalService.GoalProgress(r.Context(), goalID)
	if err != nil {
		writeGoalError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, progress)
}

// HandleGoalAudit handles GET /goals/{goalId}/audit — reconcile audit history.
// Supports query param: ?limit=50 (default 50, max 1000).
func (h *Handler) HandleGoalAudit(w http.ResponseWriter, r *http.Request) {
	if h.goalService == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "goal service not configured")
		return
	}

	goalID := chi.URLParam(r, "goalId")
	if goalID == "" {
		WriteError(w, http.StatusBadRequest, "Bad Request", "goal id is required")
		return
	}

	limit := 50
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

	history, err := h.goalService.GoalAuditHistory(r.Context(), goalID, limit)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"audit": history,
		"count": len(history),
	})
}

// writeGoalError maps goal service errors to HTTP responses, returning 404 for
// not-found goals and 400 for other errors.
func writeGoalError(w http.ResponseWriter, err error) {
	if errors.Is(err, types.ErrGoalNotFound) {
		WriteError(w, http.StatusNotFound, "Not Found", err.Error())
		return
	}
	WriteError(w, http.StatusBadRequest, "Bad Request", err.Error())
}

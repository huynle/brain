package api

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/huynle/brain-api/internal/types"

	"github.com/go-chi/chi/v5"
)

// HandleSchedulerStatus handles GET /scheduler/status.
func (h *Handler) HandleSchedulerStatus(w http.ResponseWriter, r *http.Request) {
	if h.scheduler == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "scheduler service is not configured")
		return
	}
	WriteJSON(w, http.StatusOK, h.scheduler.Status())
}

// HandleGetDispatchLease handles GET /tasks/{projectId}/{taskId}/dispatch-lease.
func (h *Handler) HandleGetDispatchLease(w http.ResponseWriter, r *http.Request) {
	if h.schedulerViews == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "scheduler visibility service is not configured")
		return
	}
	projectID := chi.URLParam(r, "projectId")
	taskID := chi.URLParam(r, "taskId")
	lease, err := h.schedulerViews.GetDispatchLease(r.Context(), projectID, taskID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	if lease == nil {
		WriteError(w, http.StatusNotFound, "Not Found", "dispatch lease not found")
		return
	}
	WriteJSON(w, http.StatusOK, lease)
}

// HandleListPlacementReasons handles GET /tasks/{projectId}/{taskId}/placement-reasons.
func (h *Handler) HandleListPlacementReasons(w http.ResponseWriter, r *http.Request) {
	if h.schedulerViews == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "scheduler visibility service is not configured")
		return
	}
	projectID := chi.URLParam(r, "projectId")
	taskID := chi.URLParam(r, "taskId")
	reasons, err := h.schedulerViews.ListPlacementReasons(r.Context(), projectID, taskID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	if reasons == nil {
		reasons = []types.PlacementReason{}
	}
	// Only when there is nothing to report: distinguish "no decisions recorded"
	// from "no such task". The query is a bare WHERE project_id/task_id with no
	// join, so a typo'd or wrong-project id renders "Total decisions: 0" — the
	// same output a real task the scheduler has never rejected produces. The
	// lookup is confined to the empty path so the common case does not pay for it.
	if len(reasons) == 0 && h.tasks != nil {
		if _, err := h.tasks.GetTask(r.Context(), projectID, taskID); errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found",
				fmt.Sprintf("Task not found: %s in project %s", taskID, projectID))
			return
		}
	}
	WriteJSON(w, http.StatusOK, types.PlacementReasonListResponse{Reasons: reasons, Total: len(reasons)})
}

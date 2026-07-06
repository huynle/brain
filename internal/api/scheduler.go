package api

import (
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
	WriteJSON(w, http.StatusOK, types.PlacementReasonListResponse{Reasons: reasons, Total: len(reasons)})
}

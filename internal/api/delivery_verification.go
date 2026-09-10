package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

type deliveryVerificationService interface {
	UpdateDeliveryVerification(context.Context, string, string, json.RawMessage, string) (*types.DeliveryVerification, error)
}

func (h *Handler) HandleDeliveryVerification(w http.ResponseWriter, r *http.Request) {
	project, task := chi.URLParam(r, "projectId"), chi.URLParam(r, "taskId")
	svc, ok := h.tasks.(deliveryVerificationService)
	if !ok {
		WriteError(w, 501, "Not Implemented", "delivery verifier unavailable")
		return
	}
	var raw json.RawMessage
	if !decodeBulkJob(w, r, &raw) {
		return
	}
	actor := "local-owner"
	if auth, ok := AuthResultFromContext(r.Context()); ok {
		actor = auth.Type + ":" + auth.Name
	}
	state, err := svc.UpdateDeliveryVerification(r.Context(), project, task, raw, actor)
	if err != nil {
		bulkJobReply(w, 500, nil, err)
		return
	}
	if h.events != nil {
		_ = h.events.Ingest(r.Context(), []types.Event{{Type: types.EventTaskDeliveryChanged, Source: types.EventSourceAPI, ProjectID: project, TaskID: task}})
	}
	WriteJSON(w, 200, map[string]any{"delivery": state, "unmet": state.Unmet()})
}
func (h *Handler) HandleDeliveryGate(w http.ResponseWriter, r *http.Request) {
	task, err := h.tasks.GetTask(r.Context(), chi.URLParam(r, "projectId"), chi.URLParam(r, "taskId"))
	if err != nil {
		bulkJobReply(w, 500, nil, err)
		return
	}
	if task == nil {
		WriteError(w, 404, "Not Found", "task not found")
		return
	}
	WriteJSON(w, 200, map[string]any{"task_id": task.ID, "implementation_status": task.Status, "delivery": task.DeliveryVerification, "unmet": task.DeliveryVerification.Unmet()})
}

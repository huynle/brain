package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

func (h *Handler) HandleGetProjectPlacement(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	placement, err := h.placement.Get(r.Context(), projectID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, placement)
}

func (h *Handler) HandlePutProjectPlacement(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "projectId")
	var req types.ProjectPlacement
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}
	placement, err := h.placement.Put(r.Context(), projectID, req)
	if err != nil {
		if strings.Contains(err.Error(), "invalid affinity") {
			WriteValidationError(w, []types.ValidationDetail{{Field: "affinity", Message: err.Error()}})
			return
		}
		if strings.Contains(err.Error(), "invalid workspace_policy") {
			WriteValidationError(w, []types.ValidationDetail{{Field: "workspace_policy", Message: err.Error()}})
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, placement)
}

package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// HandlePauseProjectAutomations handles POST /tasks/runner/automations/pause/{projectId}.
func (h *Handler) HandlePauseProjectAutomations(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	if err := h.runner.PauseProjectAutomations(r.Context(), projectId); err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// HandleResumeProjectAutomations handles POST /tasks/runner/automations/resume/{projectId}.
func (h *Handler) HandleResumeProjectAutomations(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	if err := h.runner.ResumeProjectAutomations(r.Context(), projectId); err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

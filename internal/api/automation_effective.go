package api

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
)

// HandleAutomationEffective handles GET /api/v1/automations/{id}/effective.
//
// Query: project=<project>, required. {id} is a global automation, a project-
// owned automation, or a binding, by ID or path. The response is the config
// that project runs the automation under, with per-field inherited/overridden
// states, the governing binding, whether the scheduler targets the project, and
// broken state. A broken view is still 200: it is data about the automation.
//
// 400: missing or invalid project, or a project that does not own the entry.
// 404: unknown id, or an entry that is not an automation.
// 501: the automation run service is not configured.
func (h *Handler) HandleAutomationEffective(w http.ResponseWriter, r *http.Request) {
	if h.automationRun == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "automation run service is not configured")
		return
	}
	project := r.URL.Query().Get("project")
	if project == "" {
		WriteError(w, http.StatusBadRequest, "Bad Request", "project query parameter is required")
		return
	}
	// chi hands the raw segment over, so a path ID arrives percent-encoded
	// (global%2Ffoo.md). Decode it once here.
	id, err := url.PathUnescape(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "automation id is not a valid path segment")
		return
	}
	view, err := h.automationRun.EffectiveAutomation(r.Context(), id, project)
	if err != nil {
		switch {
		case errors.Is(err, ErrNotFound):
			WriteError(w, http.StatusNotFound, "Not Found", "Automation not found: "+id)
		case errors.Is(err, ErrInvalidInput):
			WriteError(w, http.StatusBadRequest, "Bad Request", err.Error())
		default:
			WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		}
		return
	}
	WriteJSON(w, http.StatusOK, view)
}

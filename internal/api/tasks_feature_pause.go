package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// The FEATURE-scoped pause dial.
//
// The fourth dial, and the one the other three could not express: holding a
// single feature's work while the rest of the project keeps running. It is
// the shape a manually started feature needs — "Run feature now" force-
// dispatches past the project dial by design, so pausing the project was
// never an answer for work that had already been started by hand.
//
// Like the project dial it holds NEW dispatch only: a task already handed
// to a runner runs to completion, and an explicit "Run now" still overrides.
//
// NOTE the "Dispatch" suffix. `HandleResumeFeature` (tasks.go) already
// exists and is an unrelated operation — it fans the ABANDONMENT resume
// across a feature's tasks, flipping stuck in_progress rows back to
// pending. These two would be a genuinely dangerous pair to confuse:
// one turns a dial, the other rewrites task status.

// HandlePauseFeatureDispatch handles POST /tasks/runner/features/pause/{projectId}/{featureId}.
func (h *Handler) HandlePauseFeatureDispatch(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	featureId := chi.URLParam(r, "featureId")
	if featureId == "" {
		// An empty feature id would write a row that no task can match —
		// or worse, be read as "every task without a feature".
		WriteError(w, http.StatusBadRequest, "Bad Request", "featureId is required")
		return
	}
	if err := h.runner.PauseFeature(r.Context(), projectId, featureId); err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// HandleResumeFeatureDispatch handles POST /tasks/runner/features/resume/{projectId}/{featureId}.
func (h *Handler) HandleResumeFeatureDispatch(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	featureId := chi.URLParam(r, "featureId")
	if featureId == "" {
		WriteError(w, http.StatusBadRequest, "Bad Request", "featureId is required")
		return
	}
	if err := h.runner.ResumeFeature(r.Context(), projectId, featureId); err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

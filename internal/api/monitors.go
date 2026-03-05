package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// HandleListMonitorTemplates handles GET /monitors/templates.
func (h *Handler) HandleListMonitorTemplates(w http.ResponseWriter, r *http.Request) {
	templates := h.monitor.ListTemplates()

	// Ensure we return an empty array, not null
	if templates == nil {
		templates = []types.MonitorTemplate{}
	}

	WriteJSON(w, http.StatusOK, types.MonitorTemplatesResponse{
		Templates: templates,
	})
}

// HandleListMonitors handles GET /monitors.
func (h *Handler) HandleListMonitors(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	var filter *types.MonitorListFilter
	project := q.Get("project")
	featureID := q.Get("feature_id")
	templateID := q.Get("template_id")

	if project != "" || featureID != "" || templateID != "" {
		filter = &types.MonitorListFilter{
			Project:    project,
			FeatureID:  featureID,
			TemplateID: templateID,
		}
	}

	monitors, err := h.monitor.List(r.Context(), filter)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	// Ensure we return an empty array, not null
	if monitors == nil {
		monitors = []types.MonitorInfo{}
	}

	WriteJSON(w, http.StatusOK, types.MonitorListResponse{
		Monitors: monitors,
	})
}

// HandleCreateMonitor handles POST /monitors.
func (h *Handler) HandleCreateMonitor(w http.ResponseWriter, r *http.Request) {
	var req types.CreateMonitorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Invalid JSON body")
		return
	}

	// Validate required fields
	var details []types.ValidationDetail
	if req.TemplateID == "" {
		details = append(details, types.ValidationDetail{Field: "template_id", Message: "required"})
	}
	if req.ScopeType == "" {
		details = append(details, types.ValidationDetail{Field: "scope_type", Message: "required"})
	}
	if len(details) > 0 {
		WriteValidationError(w, details)
		return
	}

	scope := types.MonitorScope{
		Type:      req.ScopeType,
		Project:   req.Project,
		FeatureID: req.FeatureID,
	}

	var opts *types.CreateMonitorOptions
	if req.Schedule != "" {
		opts = &types.CreateMonitorOptions{
			Schedule: req.Schedule,
		}
	}

	result, err := h.monitor.Create(r.Context(), req.TemplateID, scope, opts)
	if err != nil {
		// Classify error for appropriate HTTP status
		errMsg := err.Error()
		if strings.Contains(errMsg, "already exists") {
			WriteError(w, http.StatusConflict, "Conflict", errMsg)
			return
		}
		if strings.Contains(errMsg, "unknown monitor template") {
			WriteError(w, http.StatusBadRequest, "Bad Request", errMsg)
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", errMsg)
		return
	}

	WriteJSON(w, http.StatusCreated, result)
}

// HandleToggleMonitor handles PATCH /monitors/{taskId}/toggle.
func (h *Handler) HandleToggleMonitor(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	var req types.ToggleMonitorRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Invalid JSON body")
		return
	}

	path, err := h.monitor.Toggle(r.Context(), taskID, req.Enabled)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Monitor not found: %s", taskID))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, types.MonitorToggleResponse{
		Success: true,
		Path:    path,
	})
}

// HandleDeleteMonitor handles DELETE /monitors/{taskId}.
func (h *Handler) HandleDeleteMonitor(w http.ResponseWriter, r *http.Request) {
	taskID := chi.URLParam(r, "taskId")

	path, err := h.monitor.Delete(r.Context(), taskID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Monitor not found: %s", taskID))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, types.MonitorDeleteResponse{
		Success: true,
		Path:    path,
	})
}

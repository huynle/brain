package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// HandleListProjects handles GET /tasks — list all projects.
func (h *Handler) HandleListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.tasks.ListProjects(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, types.ProjectListResponse{Projects: projects})
}

// HandleGetTasks handles GET /tasks/{projectId} — list tasks with dependency resolution.
func (h *Handler) HandleGetTasks(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	resp, err := h.tasks.GetTasks(r.Context(), projectId)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

// HandleGetReady handles GET /tasks/{projectId}/ready.
func (h *Handler) HandleGetReady(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	tasks, err := h.tasks.GetReady(r.Context(), projectId)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

// HandleGetWaiting handles GET /tasks/{projectId}/waiting.
func (h *Handler) HandleGetWaiting(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	tasks, err := h.tasks.GetWaiting(r.Context(), projectId)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

// HandleGetBlocked handles GET /tasks/{projectId}/blocked.
func (h *Handler) HandleGetBlocked(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	tasks, err := h.tasks.GetBlocked(r.Context(), projectId)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

// HandleGetNext handles GET /tasks/{projectId}/next.
func (h *Handler) HandleGetNext(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	task, err := h.tasks.GetNext(r.Context(), projectId)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", "no ready tasks available")
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, task)
}

// HandleClaimTask handles POST /tasks/{projectId}/{taskId}/claim.
func (h *Handler) HandleClaimTask(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	taskId := chi.URLParam(r, "taskId")

	var req types.ClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}

	if req.RunnerID == "" {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "runnerId", Message: "runnerId is required"},
		})
		return
	}

	resp, err := h.tasks.ClaimTask(r.Context(), projectId, taskId, req.RunnerID)
	if err != nil {
		if errors.Is(err, ErrConflict) {
			WriteJSON(w, http.StatusConflict, resp)
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

// HandleReleaseTask handles POST /tasks/{projectId}/{taskId}/release.
func (h *Handler) HandleReleaseTask(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	taskId := chi.URLParam(r, "taskId")

	var req types.ClaimRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}

	if req.RunnerID == "" {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "runnerId", Message: "runnerId is required"},
		})
		return
	}

	err := h.tasks.ReleaseTask(r.Context(), projectId, taskId, req.RunnerID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", "task not claimed or not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// HandleGetClaimStatus handles GET /tasks/{projectId}/{taskId}/claim-status.
func (h *Handler) HandleGetClaimStatus(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	taskId := chi.URLParam(r, "taskId")

	resp, err := h.tasks.GetClaimStatus(r.Context(), projectId, taskId)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", "task not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

// HandleMultiTaskStatus handles POST /tasks/{projectId}/status.
func (h *Handler) HandleMultiTaskStatus(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")

	var req types.MultiTaskStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}

	if len(req.TaskIDs) == 0 {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "taskIds", Message: "taskIds is required and must not be empty"},
		})
		return
	}

	resp, err := h.tasks.GetMultiTaskStatus(r.Context(), projectId, req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

// HandleGetFeatures handles GET /tasks/{projectId}/features.
func (h *Handler) HandleGetFeatures(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	resp, err := h.tasks.GetFeatures(r.Context(), projectId)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

// HandleGetReadyFeatures handles GET /tasks/{projectId}/features/ready.
func (h *Handler) HandleGetReadyFeatures(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	resp, err := h.tasks.GetReadyFeatures(r.Context(), projectId)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

// HandleGetFeature handles GET /tasks/{projectId}/features/{featureId}.
func (h *Handler) HandleGetFeature(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	featureId := chi.URLParam(r, "featureId")

	resp, err := h.tasks.GetFeature(r.Context(), projectId, featureId)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", "feature not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

// HandleCheckoutFeature handles POST /tasks/{projectId}/features/{featureId}/checkout.
func (h *Handler) HandleCheckoutFeature(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	featureId := chi.URLParam(r, "featureId")

	err := h.tasks.CheckoutFeature(r.Context(), projectId, featureId)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", "feature not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// HandleTriggerTask handles POST /tasks/{projectId}/{taskId}/trigger.
func (h *Handler) HandleTriggerTask(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	taskId := chi.URLParam(r, "taskId")

	resp, err := h.tasks.TriggerTask(r.Context(), projectId, taskId)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", "task not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

// HandlePauseProject handles POST /tasks/runner/pause/{projectId}.
func (h *Handler) HandlePauseProject(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	if err := h.runner.Pause(r.Context(), projectId); err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// HandleResumeProject handles POST /tasks/runner/resume/{projectId}.
func (h *Handler) HandleResumeProject(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	if err := h.runner.Resume(r.Context(), projectId); err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// HandlePauseAll handles POST /tasks/runner/pause.
func (h *Handler) HandlePauseAll(w http.ResponseWriter, r *http.Request) {
	if err := h.runner.PauseAll(r.Context()); err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// HandleResumeAll handles POST /tasks/runner/resume.
func (h *Handler) HandleResumeAll(w http.ResponseWriter, r *http.Request) {
	if err := h.runner.ResumeAll(r.Context()); err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// HandleRunnerStatus handles GET /tasks/runner/status.
func (h *Handler) HandleRunnerStatus(w http.ResponseWriter, r *http.Request) {
	resp, err := h.runner.GetStatus(r.Context())
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

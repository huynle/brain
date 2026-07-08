package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// parseTaskFilterOptions extracts optional TaskFilterOptions from query parameters.
// Returns nil if no filter parameters are present (backward compatible).
//
// Supported query params:
//   - feature_id: filter by feature ID (repeatable)
//   - executors: comma-separated list of executor types (e.g., "opencode,pi")
//   - runner_id or runnerId: runner requesting task selection
//   - generated_by_prefix: filter by generated_by prefix (e.g., "automation:")
func parseTaskFilterOptions(r *http.Request) *TaskFilterOptions {
	featureIDs := r.URL.Query()["feature_id"]
	executors := parseExecutors(r)
	runnerID := r.URL.Query().Get("runner_id")
	if runnerID == "" {
		runnerID = r.URL.Query().Get("runnerId")
	}
	generatedByPrefix := r.URL.Query().Get("generated_by_prefix")

	if len(featureIDs) == 0 && len(executors) == 0 && runnerID == "" && generatedByPrefix == "" {
		return nil
	}
	return &TaskFilterOptions{
		FeatureIDs:        featureIDs,
		Executors:         executors,
		RunnerID:          runnerID,
		GeneratedByPrefix: generatedByPrefix,
	}
}

// parseExecutors extracts executor types from the "executors" query parameter.
// Supports comma-separated values: ?executors=opencode,pi
func parseExecutors(r *http.Request) []string {
	raw := r.URL.Query().Get("executors")
	if raw == "" {
		return nil
	}
	var executors []string
	for _, e := range strings.Split(raw, ",") {
		e = strings.TrimSpace(e)
		if e != "" {
			executors = append(executors, e)
		}
	}
	return executors
}

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
	opts := parseTaskFilterOptions(r)
	tasks, err := h.tasks.GetReady(r.Context(), projectId, opts)
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
	opts := parseTaskFilterOptions(r)
	task, err := h.tasks.GetNext(r.Context(), projectId, opts)
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

// HandleGetTask handles GET /tasks/{projectId}/{taskId} — fetch a single task by ID.
func (h *Handler) HandleGetTask(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	taskId := chi.URLParam(r, "taskId")

	task, err := h.tasks.GetTask(r.Context(), projectId, taskId)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", "task not found")
			return
		}
		// Check for "not found" string from service layer
		if task == nil {
			WriteError(w, http.StatusNotFound, "Not Found", err.Error())
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, task)
}

// TaskMetadataResponse contains only execution config fields for a task,
// excluding content/title (which are returned by HandleGetTask).
type TaskMetadataResponse struct {
	Path                string                       `json:"path"`
	Agent               string                       `json:"agent"`
	Model               string                       `json:"model"`
	ExecutionMode       string                       `json:"execution_mode"`
	GitBranch           string                       `json:"git_branch"`
	GitRemote           string                       `json:"git_remote"`
	MergePolicy         string                       `json:"merge_policy"`
	MergeStrategy       string                       `json:"merge_strategy"`
	MergeTargetBranch   string                       `json:"merge_target_branch"`
	RemoteBranchPolicy  string                       `json:"remote_branch_policy"`
	CompleteOnIdle      *bool                        `json:"complete_on_idle"`
	OpenPRBeforeMerge   *bool                        `json:"open_pr_before_merge"`
	TargetWorkdir       string                       `json:"target_workdir"`
	ResolvedWorkdir     string                       `json:"resolved_workdir"`
	DirectPrompt        string                       `json:"direct_prompt"`
	Executor            string                       `json:"executor"`
	FeatureID           string                       `json:"feature_id"`
	FeaturePriority     string                       `json:"feature_priority"`
	FeatureDependsOn    []string                     `json:"feature_depends_on"`
	DependsOn           []string                     `json:"depends_on"`
	ResolvedDeps        []string                     `json:"resolved_deps"`
	UnresolvedDeps      []string                     `json:"unresolved_deps"`
	BlockedBy           []string                     `json:"blocked_by"`
	BlockedByReason     string                       `json:"blocked_by_reason"`
	WaitingOn           []string                     `json:"waiting_on"`
	InCycle             bool                         `json:"in_cycle"`
	Status              string                       `json:"status"`
	Priority            string                       `json:"priority"`
	Classification      string                       `json:"classification"`
	Created             string                       `json:"created"`
	Tags                []string                     `json:"tags"`
	Sessions            map[string]types.SessionInfo `json:"sessions"`
	Env                 map[string]string            `json:"env"`
	Extensions          []string                     `json:"extensions"`
	DispatchLease       *types.DispatchLease         `json:"dispatch_lease,omitempty"`
	PlacementReasons    []types.PlacementReason      `json:"placement_reasons,omitempty"`
	LastPlacementReason *types.PlacementReason       `json:"last_placement_reason,omitempty"`
}

// HandleGetTaskMetadata handles GET /tasks/{projectId}/{taskId}/metadata.
// Returns only execution metadata fields, not content or title.
func (h *Handler) HandleGetTaskMetadata(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	taskId := chi.URLParam(r, "taskId")

	task, err := h.tasks.GetTask(r.Context(), projectId, taskId)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", "task not found")
			return
		}
		if task == nil {
			WriteError(w, http.StatusNotFound, "Not Found", err.Error())
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	resp := TaskMetadataResponse{
		Path:                task.Path,
		Agent:               task.Agent,
		Model:               task.Model,
		ExecutionMode:       task.ExecutionMode,
		GitBranch:           task.GitBranch,
		GitRemote:           task.GitRemote,
		MergePolicy:         task.MergePolicy,
		MergeStrategy:       task.MergeStrategy,
		MergeTargetBranch:   task.MergeTargetBranch,
		RemoteBranchPolicy:  task.RemoteBranchPolicy,
		CompleteOnIdle:      task.CompleteOnIdle,
		OpenPRBeforeMerge:   task.OpenPRBeforeMerge,
		TargetWorkdir:       task.TargetWorkdir,
		ResolvedWorkdir:     task.ResolvedWorkdir,
		DirectPrompt:        task.DirectPrompt,
		Executor:            task.Executor,
		FeatureID:           task.FeatureID,
		FeaturePriority:     task.FeaturePriority,
		FeatureDependsOn:    task.FeatureDependsOn,
		DependsOn:           task.DependsOn,
		ResolvedDeps:        task.ResolvedDeps,
		UnresolvedDeps:      task.UnresolvedDeps,
		BlockedBy:           task.BlockedBy,
		BlockedByReason:     task.BlockedByReason,
		WaitingOn:           task.WaitingOn,
		InCycle:             task.InCycle,
		Status:              task.Status,
		Priority:            task.Priority,
		Classification:      task.Classification,
		Created:             task.Created,
		Sessions:            task.Sessions,
		Env:                 task.Env,
		Extensions:          task.Extensions,
		DispatchLease:       task.DispatchLease,
		PlacementReasons:    task.PlacementReasons,
		LastPlacementReason: task.LastPlacementReason,
	}

	WriteJSON(w, http.StatusOK, resp)
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
	slog.Info("claim request", "project", projectId, "task_id", taskId, "runner_id", req.RunnerID, "success", resp.Success)

	// Emit task.claimed event
	evt := types.NewEvent(types.EventTaskClaimed, types.EventSourceAPI)
	evt.ProjectID = projectId
	evt.TaskID = taskId
	evt.TaskPath = fmt.Sprintf("projects/%s/task/%s.md", projectId, taskId)
	evt.Metadata = map[string]string{
		"runner_id": req.RunnerID,
	}
	h.emitEvent(r.Context(), evt)

	// Publish task_claimed SSE event to project subscribers
	if h.hub != nil && resp.Success {
		h.hub.PublishTaskClaimed(projectId, types.SSETaskClaimedData{
			SSEEventData: types.SSEEventData{
				Type:      types.SSEEventTaskClaimed,
				Transport: "sse",
				Timestamp: types.TimeNowUTC().Format(time.RFC3339),
				ProjectID: projectId,
			},
			TaskID:   taskId,
			RunnerID: req.RunnerID,
		})
	}

	WriteJSON(w, http.StatusOK, resp)
}

// HandleReleaseTask handles POST /tasks/{projectId}/{taskId}/release.
func (h *Handler) HandleReleaseTask(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	taskId := chi.URLParam(r, "taskId")

	var req types.ReleaseRequest
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
	slog.Info("release request", "project", projectId, "task_id", taskId, "runner_id", req.RunnerID)

	// Emit task.released event. Mirror the runner-source shape (see
	// internal/runner/event_conversion.go): reason is exposed BOTH as a
	// top-level field AND in metadata["reason"] so consumers using either
	// access pattern see the same diagnostic. Empty reason is left empty
	// in both places — we never fabricate a synthetic reason string.
	evt := types.NewEvent(types.EventTaskReleased, types.EventSourceAPI)
	evt.ProjectID = projectId
	evt.TaskID = taskId
	evt.TaskPath = fmt.Sprintf("projects/%s/task/%s.md", projectId, taskId)
	meta := map[string]string{
		"runner_id": req.RunnerID,
	}
	if req.Reason != "" {
		evt.Reason = req.Reason
		meta["reason"] = req.Reason
	}
	evt.Metadata = meta
	h.emitEvent(r.Context(), evt)

	// Publish task_released SSE event to project subscribers
	if h.hub != nil {
		h.hub.PublishTaskReleased(projectId, types.SSETaskReleasedData{
			SSEEventData: types.SSEEventData{
				Type:      types.SSEEventTaskReleased,
				Transport: "sse",
				Timestamp: types.TimeNowUTC().Format(time.RFC3339),
				ProjectID: projectId,
			},
			TaskID:   taskId,
			RunnerID: req.RunnerID,
		})
	}

	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// HandleDispatchTask handles POST /tasks/{projectId}/{taskId}/dispatch.
// Dispatches a task directly to a specific runner by creating a pre-claim and sending an SSE command.
func (h *Handler) HandleDispatchTask(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	taskId := chi.URLParam(r, "taskId")

	var req types.DispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}

	if req.TargetRunnerID == "" {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "targetRunnerId", Message: "targetRunnerId is required"},
		})
		return
	}

	// Verify runner exists
	if h.runnerRegistry != nil {
		_, err := h.runnerRegistry.GetRunner(r.Context(), req.TargetRunnerID)
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				WriteError(w, http.StatusNotFound, "Not Found", "runner not found")
				return
			}
			WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
			return
		}
	}

	// Create dispatch lease and pre-claim for target runner (60 second expiry for dispatch)
	dispatchResp, err := h.tasks.DispatchTask(r.Context(), projectId, taskId, req.TargetRunnerID)
	if err != nil {
		if errors.Is(err, ErrConflict) {
			WriteJSON(w, http.StatusConflict, map[string]any{
				"success": false,
				"error":   "task is already claimed by another runner",
			})
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	if !dispatchResp.Success {
		WriteJSON(w, http.StatusConflict, map[string]any{
			"success": false,
			"error":   "failed to create pre-claim for target runner",
		})
		return
	}

	// Send dispatch command via SSE to target runner
	if h.hub != nil {
		h.hub.PublishRunnerCommand(req.TargetRunnerID, "dispatch", map[string]string{
			"taskId":    taskId,
			"projectId": projectId,
			"leaseId":   dispatchResp.LeaseID,
			"lease":     dispatchResp.LeaseID,
			"expiresAt": dispatchResp.ExpiresAt,
		})
	}

	slog.Info("dispatch request", "project", projectId, "task_id", taskId, "target_runner", req.TargetRunnerID)

	WriteJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"runnerId":  req.TargetRunnerID,
		"leaseId":   dispatchResp.LeaseID,
		"expiresAt": dispatchResp.ExpiresAt,
	})
}

func validateDispatchAckRequest(req types.DispatchAckRequest) []types.ValidationDetail {
	var details []types.ValidationDetail
	if strings.TrimSpace(req.LeaseID) == "" {
		details = append(details, types.ValidationDetail{Field: "leaseId", Message: "leaseId is required"})
	}
	if strings.TrimSpace(req.ProjectID) == "" {
		details = append(details, types.ValidationDetail{Field: "projectId", Message: "projectId is required"})
	}
	if strings.TrimSpace(req.TaskID) == "" {
		details = append(details, types.ValidationDetail{Field: "taskId", Message: "taskId is required"})
	}
	return details
}

// HandleAckDispatch handles POST /tasks/runners/{runnerId}/dispatch/ack.
func (h *Handler) HandleAckDispatch(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")
	var req types.DispatchAckRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}
	if details := validateDispatchAckRequest(req); len(details) > 0 {
		WriteValidationError(w, details)
		return
	}
	resp, err := h.tasks.AckDispatch(r.Context(), req.ProjectID, req.TaskID, runnerID, req.LeaseID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteJSON(w, http.StatusNotFound, resp)
			return
		}
		if errors.Is(err, ErrConflict) {
			WriteJSON(w, http.StatusConflict, resp)
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

// HandleRejectDispatch handles POST /tasks/runners/{runnerId}/dispatch/reject.
func (h *Handler) HandleRejectDispatch(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")
	var req types.DispatchRejectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}
	details := validateDispatchAckRequest(types.DispatchAckRequest{LeaseID: req.LeaseID, ProjectID: req.ProjectID, TaskID: req.TaskID})
	if strings.TrimSpace(req.Reason.Code) == "" {
		details = append(details, types.ValidationDetail{Field: "reason.code", Message: "reason.code is required"})
	}
	if len(details) > 0 {
		WriteValidationError(w, details)
		return
	}
	resp, err := h.tasks.RejectDispatch(r.Context(), req.ProjectID, req.TaskID, runnerID, req.LeaseID, req.Reason)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteJSON(w, http.StatusNotFound, resp)
			return
		}
		if errors.Is(err, ErrConflict) {
			WriteJSON(w, http.StatusConflict, resp)
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

// HandleReleaseDispatch handles POST /tasks/runners/{runnerId}/dispatch/release.
func (h *Handler) HandleReleaseDispatch(w http.ResponseWriter, r *http.Request) {
	runnerID := chi.URLParam(r, "runnerId")
	var req struct {
		ProjectID string `json:"projectId"`
		TaskID    string `json:"taskId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}
	var details []types.ValidationDetail
	if strings.TrimSpace(req.ProjectID) == "" {
		details = append(details, types.ValidationDetail{Field: "projectId", Message: "projectId is required"})
	}
	if strings.TrimSpace(req.TaskID) == "" {
		details = append(details, types.ValidationDetail{Field: "taskId", Message: "taskId is required"})
	}
	if len(details) > 0 {
		WriteValidationError(w, details)
		return
	}
	resp, err := h.tasks.ReleaseDispatch(r.Context(), req.ProjectID, req.TaskID, runnerID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteJSON(w, http.StatusNotFound, resp)
			return
		}
		if errors.Is(err, ErrConflict) {
			WriteJSON(w, http.StatusConflict, resp)
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

// HandleRenewClaim handles POST /tasks/{projectId}/{taskId}/renew.
func (h *Handler) HandleRenewClaim(w http.ResponseWriter, r *http.Request) {
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

	resp, err := h.tasks.RenewClaim(r.Context(), projectId, taskId, req.RunnerID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteJSON(w, http.StatusNotFound, resp)
			return
		}
		if errors.Is(err, ErrConflict) {
			WriteJSON(w, http.StatusConflict, resp)
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	slog.Info("renew request", "project", projectId, "task_id", taskId, "runner_id", req.RunnerID)
	WriteJSON(w, http.StatusOK, resp)
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

	var opts types.FeatureCheckoutOptions
	if r.Body != nil {
		if err := json.NewDecoder(r.Body).Decode(&opts); err != nil {
			// Allow empty body - use defaults
			opts = types.FeatureCheckoutOptions{}
		}
	}

	result, err := h.tasks.CheckoutFeature(r.Context(), projectId, featureId, &opts)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", "feature not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, result)
}

// HandleAssignFeatureToRunner handles PUT /tasks/{projectId}/features/{featureId}/assignment.
func (h *Handler) HandleAssignFeatureToRunner(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	featureId := chi.URLParam(r, "featureId")

	var req types.FeatureAssignmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.RunnerID) == "" {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "runner_id", Message: "runner_id is required"},
		})
		return
	}

	resp, err := h.tasks.AssignFeatureToRunner(r.Context(), projectId, featureId, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", "runner not found")
			return
		}
		if errors.Is(err, ErrConflict) {
			WriteError(w, http.StatusConflict, "Conflict", "feature assignment conflict")
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, resp)
}

// HandleClearFeatureAssignment handles POST /tasks/{projectId}/features/{featureId}/assignment/clear.
func (h *Handler) HandleClearFeatureAssignment(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	featureId := chi.URLParam(r, "featureId")

	var req types.ClearFeatureAssignmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Intent) != "clear" {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "intent", Message: "intent must be clear"},
		})
		return
	}

	resp, err := h.tasks.ClearFeatureAssignment(r.Context(), projectId, featureId, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", "feature assignment not found")
			return
		}
		if errors.Is(err, ErrConflict) {
			WriteError(w, http.StatusConflict, "Conflict", "feature assignment conflict")
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, resp)
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

	// Emit task.triggered event
	evt := types.NewEvent(types.EventTaskTriggered, types.EventSourceAPI)
	evt.ProjectID = projectId
	evt.TaskID = taskId
	evt.TaskPath = fmt.Sprintf("projects/%s/task/%s.md", projectId, taskId)
	h.emitEvent(r.Context(), evt)

	WriteJSON(w, http.StatusOK, resp)
}

// HandleRunTask handles POST /tasks/{projectId}/{taskId}/run.
//
// This is the user-explicit "run this task now" path used by the PWA "x"
// shortcut and any other UI that needs to push a task to a runner without
// waiting for the scheduler tick or runner poll loop. It picks an eligible
// runner via the scheduler's selectCandidate logic and publishes a dispatch
// command via the realtime hub.
//
// Behaviour:
//   - 200 with Dispatched=true and {runnerId, leaseId, expiresAt} on success.
//   - 200 with Dispatched=false and a Reason token (no_online_runner,
//     no_eligible_runner, task_not_ready, all_runners_at_capacity, ...) when
//     dispatch can't proceed. The PWA can branch on Reason for UX (e.g. show
//     "no runner online" toast).
//   - 501 Not Implemented when the run service is not wired, so clients can
//     fall back to /trigger transparently.
//   - 500 only on unexpected infrastructure failures (storage, hub).
func (h *Handler) HandleRunTask(w http.ResponseWriter, r *http.Request) {
	if h.runTask == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "run-task service is not configured")
		return
	}

	projectId := chi.URLParam(r, "projectId")
	taskId := chi.URLParam(r, "taskId")

	var req types.RunTaskRequest
	// Empty body is fine — Force defaults to false. Only fail on malformed JSON.
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
			return
		}
	}

	resp, err := h.runTask.RunTaskNow(r.Context(), projectId, taskId, req.Force)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	// Emit task.triggered event when dispatch actually happened so existing
	// audit/observability paths (dispatch UI, automation reactions) see it.
	if resp != nil && resp.Dispatched {
		evt := types.NewEvent(types.EventTaskTriggered, types.EventSourceAPI)
		evt.ProjectID = projectId
		evt.TaskID = taskId
		evt.TaskPath = fmt.Sprintf("projects/%s/task/%s.md", projectId, taskId)
		h.emitEvent(r.Context(), evt)
	}

	WriteJSON(w, http.StatusOK, resp)
}

// HandleRunFeature handles POST /tasks/{projectId}/features/{featureId}/run.
//
// User-explicit "run this whole feature now" path. The scheduler dispatches
// every ready task in the feature up to current runner capacity and queues
// the rest for a feature-scoped cascade that auto-dispatches as in-flight
// tasks complete — even when the project is paused.
//
// Behaviour:
//   - 200 with Dispatched=true and a per-task Results array on success.
//   - 200 with Dispatched=false and Reason ("feature_not_found",
//     "no_ready_tasks", "feature_in_progress", "scheduler_not_configured")
//     when nothing dispatched.
//   - 400 on malformed JSON body.
//   - 501 Not Implemented when the run-feature service is not wired so PWA
//     can fall back gracefully.
//   - 500 only on unexpected infrastructure failures.
func (h *Handler) HandleRunFeature(w http.ResponseWriter, r *http.Request) {
	if h.runFeature == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "run-feature service is not configured")
		return
	}

	projectId := chi.URLParam(r, "projectId")
	featureId := chi.URLParam(r, "featureId")

	var req types.RunFeatureRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON body")
			return
		}
	}

	resp, err := h.runFeature.RunFeatureNow(r.Context(), projectId, featureId, req.Force)
	if err != nil {
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
	publishPauseCommand(r.Context(), h.hub, h.runnerRegistry, projectId, "tasks", true)
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// HandleResumeProject handles POST /tasks/runner/resume/{projectId}.
func (h *Handler) HandleResumeProject(w http.ResponseWriter, r *http.Request) {
	projectId := chi.URLParam(r, "projectId")
	if err := h.runner.Resume(r.Context(), projectId); err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	publishPauseCommand(r.Context(), h.hub, h.runnerRegistry, projectId, "tasks", false)
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// HandlePauseAll handles POST /tasks/runner/pause.
func (h *Handler) HandlePauseAll(w http.ResponseWriter, r *http.Request) {
	if err := h.runner.PauseAll(r.Context()); err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	publishPauseCommand(r.Context(), h.hub, h.runnerRegistry, "", "tasks", true)
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// HandleResumeAll handles POST /tasks/runner/resume.
func (h *Handler) HandleResumeAll(w http.ResponseWriter, r *http.Request) {
	if err := h.runner.ResumeAll(r.Context()); err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	publishPauseCommand(r.Context(), h.hub, h.runnerRegistry, "", "tasks", false)
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// HandlePauseAutomations handles POST /tasks/runner/automations/pause.
func (h *Handler) HandlePauseAutomations(w http.ResponseWriter, r *http.Request) {
	if err := h.runner.PauseAutomations(r.Context()); err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	publishPauseCommand(r.Context(), h.hub, h.runnerRegistry, "", "automations", true)
	WriteJSON(w, http.StatusOK, map[string]any{"success": true})
}

// HandleResumeAutomations handles POST /tasks/runner/automations/resume.
func (h *Handler) HandleResumeAutomations(w http.ResponseWriter, r *http.Request) {
	if err := h.runner.ResumeAutomations(r.Context()); err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	publishPauseCommand(r.Context(), h.hub, h.runnerRegistry, "", "automations", false)
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

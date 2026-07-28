package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/cron"
	"github.com/huynle/brain-api/pkg/frontmatter"
)

// AllowedMetadataUpdateFields is the strict allowlist for
// PATCH /entries/*/metadata. Any field not in this set is rejected with a
// 400 validation error, because PATCH .../metadata does NOT rewrite the .md
// file on disk — it only merges into the SQLite metadata JSON column. Fields
// that would need to live in the frontmatter but were silently accepted here
// would be reverted by the next PATCH /entries/{path} call (which re-indexes
// from disk). Callers that need to update full-frontmatter fields
// (git_remote, workdir, merge_policy, execution_mode, git_branch,
// merge_target_branch, target_workdir, user_original_request, etc.) must use
// PATCH /entries/{path} instead.
//
// This set is the union of three categories:
//
//  1. File-syncable durable fields — mirrored to service.durableMetadataFields
//     and written back to the frontmatter by syncDurableFieldsToFile.
//     Keep this list in sync with service.durableMetadataFields.
//
//  2. Runtime-only fields the runner and scheduler legitimately write direct
//     to SQLite. These are preserved across re-index by service.runtimeKeys
//     (see internal/service/brain.go). They MUST NOT appear in on-disk
//     frontmatter (they would churn the file needlessly).
//
//  3. Audit-only DB mirrors written by internal services (e.g. goal
//     reconciliation, script executor output) that intentionally stay out of
//     the frontmatter.
var AllowedMetadataUpdateFields = map[string]bool{
	// (1) File-syncable durable fields. Mirror of service.durableMetadataFields.
	"status":             true,
	"priority":           true,
	"tags":               true,
	"depends_on":         true,
	"title":              true,
	"feature_id":         true,
	"feature_priority":   true,
	"feature_depends_on": true,
	"note":               true,
	"append":             true,
	"starts_at":          true,
	"expires_at":         true,
	"run_once_at":        true,
	"timezone":           true,
	"automation_run_id":  true,
	"completed_at":       true,

	// (2) Runtime-only fields — see service.runtimeKeys in brain.go.
	"sessions":         true, // runner: per-task session discovery
	"next_run":         true, // scheduler: bookkeeping
	"schedule":         true, // scheduler: cron expression cache
	"schedule_enabled": true, // scheduler: on/off flag
	"complete_on_idle": true, // task defaults cache
	"direct_prompt":    true, // task defaults cache
	"runs":             true, // scheduler: run history array
	"max_runs":         true, // scheduler: run cap

	// (3) Audit-only DB mirrors.
	"last_reconcile": true, // goal_service: reconcile audit trail
	"exit_code":      true, // runner script executor: process exit code
	"script_output":  true, // runner script executor: captured output tail

	// (4) Task-runtime lifecycle fields for the resume-abandoned-tasks flow.
	// These are read/written by the runner (resume_requested → IsResume prompt)
	// and by the orphan reaper / lifecycle-sweep reconciler (abandoned_*
	// stamps let the API surface abandonment without grepping body text).
	// Kept out of durableMetadataFields so they never touch on-disk frontmatter.
	"resume_requested":    true, // set by /resume endpoint, cleared by runner on spawn
	"resume_requested_at": true, // RFC3339 timestamp for audit
	"abandoned_at":        true, // RFC3339 timestamp set by reaper / reconciler
	"abandoned_reason":    true, // enum: runner_orphan | runner_offline | claim_expired | no_claim
}

// HandleCreateEntry handles POST /entries.
func (h *Handler) HandleCreateEntry(w http.ResponseWriter, r *http.Request) {
	var req types.CreateEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Invalid JSON body")
		return
	}

	// Validate required fields
	var details []types.ValidationDetail
	if req.Type == "" {
		details = append(details, types.ValidationDetail{Field: "type", Message: "required"})
	} else if !types.IsValidEntryType(req.Type) {
		details = append(details, types.ValidationDetail{
			Field:   "type",
			Message: fmt.Sprintf("invalid type %q, must be one of: %s", req.Type, strings.Join(types.EntryTypes, ", ")),
		})
	}
	if req.Title == "" {
		details = append(details, types.ValidationDetail{Field: "title", Message: "required"})
	}
	if req.Content == "" {
		details = append(details, types.ValidationDetail{Field: "content", Message: "required"})
	}

	// Validate optional enum fields
	if req.Status != "" && !types.IsValidEntryStatus(req.Status) {
		details = append(details, types.ValidationDetail{
			Field:   "status",
			Message: fmt.Sprintf("invalid status %q", req.Status),
		})
	}
	if req.Priority != "" && !types.IsValidPriority(req.Priority) {
		details = append(details, types.ValidationDetail{
			Field:   "priority",
			Message: fmt.Sprintf("invalid priority %q", req.Priority),
		})
	}
	if req.MergePolicy != "" && !isValidEnum(req.MergePolicy, types.MergePolicies) {
		details = append(details, types.ValidationDetail{
			Field:   "merge_policy",
			Message: fmt.Sprintf("invalid merge_policy %q", req.MergePolicy),
		})
	}
	if !types.IsValidCheckoutMode(req.CheckoutMode) {
		details = append(details, types.ValidationDetail{
			Field:   "checkout_mode",
			Message: fmt.Sprintf("invalid checkout_mode %q", req.CheckoutMode),
		})
	}
	if req.MergeStrategy != "" && !isValidEnum(req.MergeStrategy, types.MergeStrategies) {
		details = append(details, types.ValidationDetail{
			Field:   "merge_strategy",
			Message: fmt.Sprintf("invalid merge_strategy %q", req.MergeStrategy),
		})
	}
	if req.RemoteBranchPolicy != "" && !isValidEnum(req.RemoteBranchPolicy, types.RemoteBranchPolicies) {
		details = append(details, types.ValidationDetail{
			Field:   "remote_branch_policy",
			Message: fmt.Sprintf("invalid remote_branch_policy %q", req.RemoteBranchPolicy),
		})
	}
	if req.ExecutionMode != "" && !isValidEnum(req.ExecutionMode, types.ExecutionModes) {
		details = append(details, types.ValidationDetail{
			Field:   "execution_mode",
			Message: fmt.Sprintf("invalid execution_mode %q", req.ExecutionMode),
		})
	}
	if req.Executor != "" && !isValidEnum(req.Executor, types.Executors) {
		details = append(details, types.ValidationDetail{
			Field:   "executor",
			Message: fmt.Sprintf("invalid executor %q; valid values: opencode, pi", req.Executor),
		})
	}
	if req.FeaturePriority != "" && !types.IsValidPriority(req.FeaturePriority) {
		details = append(details, types.ValidationDetail{
			Field:   "feature_priority",
			Message: fmt.Sprintf("invalid feature_priority %q", req.FeaturePriority),
		})
	}

	// Validate schedule/time fields
	details = append(details, validateTimezone(req.Timezone, "timezone")...)
	details = append(details, validateRFC3339(req.RunOnceAt, "run_once_at")...)
	details = append(details, validateRFC3339(req.StartsAt, "starts_at")...)
	details = append(details, validateRFC3339(req.ExpiresAt, "expires_at")...)
	details = append(details, validateExpiresAfterStarts(req.StartsAt, req.ExpiresAt)...)
	details = append(details, validateCronSchedule(req.Schedule, "schedule")...)

	if len(details) > 0 {
		WriteValidationError(w, details)
		return
	}

	resp, err := h.brain.Save(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	// Notify SSE clients about the change
	h.notifyProjectChanged(r, resp.Path, req.Type)

	// Emit entry.created event
	evt := types.NewEvent(types.EventEntryCreated, types.EventSourceAPI)
	evt.ProjectID = extractProjectID(resp.Path)
	evt.TaskPath = resp.Path
	evt.Metadata = map[string]string{
		"entry_type": req.Type,
		"title":      req.Title,
	}
	if req.FeatureID != "" {
		evt.FeatureID = req.FeatureID
	}
	h.emitEvent(r.Context(), evt)

	WriteJSON(w, http.StatusCreated, resp)
}

// HandleGetEntry handles GET /entries/{id} or GET /entries/path/to/entry.md.
func (h *Handler) HandleGetEntry(w http.ResponseWriter, r *http.Request) {
	// Chi wildcard /* captures everything after /entries/ in the "*" parameter
	id := chi.URLParam(r, "*")
	// Fallback to "id" parameter for backward compatibility (if route uses /{id})
	if id == "" {
		id = chi.URLParam(r, "id")
	}

	entry, err := h.brain.Recall(r.Context(), id, parseIncludeQuery(r.URL.Query().Get("include"))...)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Entry not found: %s", id))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	// Content negotiation based on Accept header
	accept := r.Header.Get("Accept")

	if strings.Contains(accept, "text/markdown") || strings.Contains(accept, "text/plain") {
		// Return raw markdown body only with metadata in X-Brain-* headers
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.Header().Set("X-Brain-Entry-Id", entry.ID)
		w.Header().Set("X-Brain-Entry-Path", entry.Path)
		w.Header().Set("X-Brain-Entry-Title", entry.Title)
		w.Header().Set("X-Brain-Entry-Status", entry.Status)
		w.Header().Set("X-Brain-Entry-Type", entry.Type)
		if len(entry.Tags) > 0 {
			w.Header().Set("X-Brain-Entry-Tags", strings.Join(entry.Tags, ","))
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(entry.Content))
		return
	} else if strings.Contains(accept, "text/x-brain-full") {
		// Return full file content (frontmatter + body)
		fullContent, err := h.brain.RecallFull(r.Context(), id)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
			return
		}
		w.Header().Set("Content-Type", "text/x-brain-full; charset=utf-8")
		w.Header().Set("X-Brain-Entry-Id", entry.ID)
		w.Header().Set("X-Brain-Entry-Path", entry.Path)
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fullContent))
		return
	}

	// Default: JSON (existing behavior, unchanged)
	WriteJSON(w, http.StatusOK, entry)
}

// HandleListEntries handles GET /entries.
func (h *Handler) HandleListEntries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// Validate enum query params before parsing
	var details []types.ValidationDetail
	if typ := q.Get("type"); typ != "" && !types.IsValidEntryType(typ) {
		details = append(details, types.ValidationDetail{
			Field:   "type",
			Message: fmt.Sprintf("invalid type %q", typ),
		})
	}
	if status := q.Get("status"); status != "" && !types.IsValidEntryStatus(status) {
		details = append(details, types.ValidationDetail{
			Field:   "status",
			Message: fmt.Sprintf("invalid status %q", status),
		})
	}
	if sortBy := q.Get("sortBy"); sortBy != "" && !isValidSortBy(sortBy) {
		details = append(details, types.ValidationDetail{
			Field:   "sortBy",
			Message: fmt.Sprintf("invalid sortBy %q, must be one of: created, modified, priority, completed, title", sortBy),
		})
	}
	if sortOrder := q.Get("sortOrder"); sortOrder != "" && sortOrder != "asc" && sortOrder != "desc" {
		details = append(details, types.ValidationDetail{
			Field:   "sortOrder",
			Message: fmt.Sprintf("invalid sortOrder %q, must be one of: asc, desc", sortOrder),
		})
	}
	if priority := q.Get("priority"); priority != "" && !isValidPriority(priority) {
		details = append(details, types.ValidationDetail{
			Field:   "priority",
			Message: fmt.Sprintf("invalid priority %q, must be one of: high, medium, low", priority),
		})
	}

	if len(details) > 0 {
		WriteValidationError(w, details)
		return
	}

	req := types.ListEntriesRequest{
		Type:      q.Get("type"),
		Status:    q.Get("status"),
		FeatureID: q.Get("feature_id"),
		Filename:  q.Get("filename"),
		Tags:      q.Get("tags"),
		Include:   parseIncludeQuery(q.Get("include")),
		SortBy:    q.Get("sortBy"),
		Project:   q.Get("project"),
		SortOrder: q.Get("sortOrder"),
		Priority:  q.Get("priority"),
	}

	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			req.Limit = n
		}
	}
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			req.Offset = n
		}
	}
	if v := q.Get("global"); v != "" {
		b := v == "true"
		req.Global = &b
	}

	resp, err := h.brain.List(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, resp)
}

// HandleUpdateEntry handles PATCH /entries/{id} or PATCH /entries/path/to/entry.md.
func (h *Handler) HandleUpdateEntry(w http.ResponseWriter, r *http.Request) {
	// Chi wildcard /* captures everything after /entries/ in the "*" parameter
	id := chi.URLParam(r, "*")
	// Fallback to "id" parameter for backward compatibility (if route uses /{id})
	if id == "" {
		id = chi.URLParam(r, "id")
	}

	var req types.UpdateEntryRequest
	contentType := r.Header.Get("Content-Type")

	if strings.Contains(contentType, "text/markdown") || strings.Contains(contentType, "text/plain") {
		// Raw markdown/text body = full content replacement
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "Bad Request", "Failed to read request body")
			return
		}
		content := string(bodyBytes)
		req = types.UpdateEntryRequest{Content: &content}
	} else if strings.Contains(contentType, "text/x-brain-full") {
		// Full file with YAML frontmatter + body
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "Bad Request", "Failed to read request body")
			return
		}
		doc, err := frontmatter.Parse(string(bodyBytes))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "Bad Request", "Failed to parse frontmatter: "+err.Error())
			return
		}
		req = mapFrontmatterToUpdateRequest(doc.Frontmatter, doc.Body)
	} else {
		// Default: JSON (existing behavior, unchanged)
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "Bad Request", "Invalid JSON body")
			return
		}
	}

	// Validate optional enum fields
	var details []types.ValidationDetail
	if req.Status != nil && !types.IsValidEntryStatus(*req.Status) {
		details = append(details, types.ValidationDetail{
			Field:   "status",
			Message: fmt.Sprintf("invalid status %q", *req.Status),
		})
	}
	if req.Priority != nil && !types.IsValidPriority(*req.Priority) {
		details = append(details, types.ValidationDetail{
			Field:   "priority",
			Message: fmt.Sprintf("invalid priority %q", *req.Priority),
		})
	}
	if req.MergePolicy != nil && !isValidEnum(*req.MergePolicy, types.MergePolicies) {
		details = append(details, types.ValidationDetail{
			Field:   "merge_policy",
			Message: fmt.Sprintf("invalid merge_policy %q", *req.MergePolicy),
		})
	}
	if req.CheckoutMode != nil && !types.IsValidCheckoutMode(*req.CheckoutMode) {
		details = append(details, types.ValidationDetail{
			Field:   "checkout_mode",
			Message: fmt.Sprintf("invalid checkout_mode %q", *req.CheckoutMode),
		})
	}
	if req.MergeStrategy != nil && !isValidEnum(*req.MergeStrategy, types.MergeStrategies) {
		details = append(details, types.ValidationDetail{
			Field:   "merge_strategy",
			Message: fmt.Sprintf("invalid merge_strategy %q", *req.MergeStrategy),
		})
	}
	if req.RemoteBranchPolicy != nil && !isValidEnum(*req.RemoteBranchPolicy, types.RemoteBranchPolicies) {
		details = append(details, types.ValidationDetail{
			Field:   "remote_branch_policy",
			Message: fmt.Sprintf("invalid remote_branch_policy %q", *req.RemoteBranchPolicy),
		})
	}
	if req.ExecutionMode != nil && !isValidEnum(*req.ExecutionMode, types.ExecutionModes) {
		details = append(details, types.ValidationDetail{
			Field:   "execution_mode",
			Message: fmt.Sprintf("invalid execution_mode %q", *req.ExecutionMode),
		})
	}
	if req.Executor != nil && *req.Executor != "" && !isValidEnum(*req.Executor, types.Executors) {
		details = append(details, types.ValidationDetail{
			Field:   "executor",
			Message: fmt.Sprintf("invalid executor %q; valid values: opencode, pi", *req.Executor),
		})
	}
	if req.FeaturePriority != nil && !types.IsValidPriority(*req.FeaturePriority) {
		details = append(details, types.ValidationDetail{
			Field:   "feature_priority",
			Message: fmt.Sprintf("invalid feature_priority %q", *req.FeaturePriority),
		})
	}

	// Validate schedule/time fields
	if req.Timezone != nil {
		details = append(details, validateTimezone(*req.Timezone, "timezone")...)
	}
	if req.RunOnceAt != nil {
		details = append(details, validateRFC3339(*req.RunOnceAt, "run_once_at")...)
	}
	if req.StartsAt != nil {
		details = append(details, validateRFC3339(*req.StartsAt, "starts_at")...)
	}
	if req.ExpiresAt != nil {
		details = append(details, validateRFC3339(*req.ExpiresAt, "expires_at")...)
	}
	if req.StartsAt != nil && req.ExpiresAt != nil {
		details = append(details, validateExpiresAfterStarts(*req.StartsAt, *req.ExpiresAt)...)
	}
	if req.Schedule != nil {
		details = append(details, validateCronSchedule(*req.Schedule, "schedule")...)
	}

	if len(details) > 0 {
		WriteValidationError(w, details)
		return
	}

	// Capture old status before update for status_changed events.
	var oldStatus string
	if req.Status != nil && h.events != nil {
		if oldEntry, err := h.brain.Recall(r.Context(), id); err == nil {
			oldStatus = oldEntry.Status
		}
	}

	entry, err := h.brain.Update(r.Context(), id, req)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Entry not found: %s", id))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	// Notify SSE clients about the change
	h.notifyProjectChanged(r, entry.Path, entry.Type)

	// Check feature completion when a task status changes to completed/validated
	h.checkFeatureCompletionForEntry(r.Context(), entry, req.Status)

	// Emit entry.updated event
	projectID := extractProjectID(entry.Path)
	evt := types.NewEvent(types.EventEntryUpdated, types.EventSourceAPI)
	evt.ProjectID = projectID
	evt.TaskPath = entry.Path
	evt.TaskID = entry.ID
	evt.FeatureID = entry.FeatureID
	evt.Metadata = map[string]string{
		"entry_type": entry.Type,
		"title":      entry.Title,
	}
	if req.Status != nil {
		evt.Metadata["new_status"] = *req.Status
	}
	h.emitEvent(r.Context(), evt)

	// Emit task.status_changed when a task's status actually changed
	if entry.Type == "task" && req.Status != nil && oldStatus != "" && oldStatus != *req.Status {
		statusEvt := types.NewEvent(types.EventTaskStatusChanged, types.EventSourceAPI)
		statusEvt.ProjectID = projectID
		statusEvt.TaskID = entry.ID
		statusEvt.TaskPath = entry.Path
		statusEvt.TaskTitle = entry.Title
		statusEvt.FeatureID = entry.FeatureID
		statusEvt.FromStatus = oldStatus
		statusEvt.ToStatus = *req.Status
		h.emitEvent(r.Context(), statusEvt)
	}

	WriteJSON(w, http.StatusOK, entry)
}

// HandleUpdateOrMetadata dispatches PATCH /entries/* to either HandleUpdateEntry
// or HandleUpdateMetadata based on whether the path ends with "/metadata".
func (h *Handler) HandleUpdateOrMetadata(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "*")
	if strings.HasSuffix(id, "/metadata") {
		h.HandleUpdateMetadata(w, r)
		return
	}
	h.HandleUpdateEntry(w, r)
}

// HandleUpdateMetadata handles PATCH /entries/*/metadata.
// It merges the provided JSON fields into the entry's metadata column directly
// in SQLite, bypassing the file-read/write logic. Used for runtime state like
// session tracking, claims, etc.
func (h *Handler) HandleUpdateMetadata(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "*")
	if id == "" {
		id = chi.URLParam(r, "id")
	}
	// Strip "/metadata" suffix from the path to get the entry path
	id = strings.TrimSuffix(id, "/metadata")

	var fields map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&fields); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Invalid JSON body")
		return
	}

	// Strict allowlist: PATCH /entries/*/metadata only accepts fields that this
	// endpoint can fully persist. Fields outside the allowlist would land in
	// the SQLite metadata JSON column but NEVER be written to the .md file on
	// disk, so a later PATCH /entries/{path} (which re-indexes from disk)
	// would silently revert them. That is a data-loss footgun, so we fail
	// loud instead. Callers that want to update full-frontmatter fields such
	// as git_remote, workdir, merge_policy, execution_mode, git_branch,
	// merge_target_branch, target_workdir, user_original_request, etc. must
	// use PATCH /entries/{path} (which reads → patches → writes → re-indexes).
	var disallowed []string
	for k := range fields {
		if !AllowedMetadataUpdateFields[k] {
			disallowed = append(disallowed, k)
		}
	}
	if len(disallowed) > 0 {
		sort.Strings(disallowed) // deterministic order for tests and users
		details := make([]types.ValidationDetail, 0, len(disallowed))
		for _, f := range disallowed {
			details = append(details, types.ValidationDetail{
				Field:   f,
				Message: fmt.Sprintf("field %q cannot be updated via PATCH /entries/*/metadata; use PATCH /entries/{path} for full-frontmatter fields", f),
			})
		}
		WriteValidationError(w, details)
		return
	}

	// Capture old status before update so we can emit task.status_changed.
	// The runner marks task completion through this endpoint, so this is the
	// emission path that drives goal/automation reconcile loops.
	var oldStatus string
	if _, ok := fields["status"]; ok && h.events != nil {
		if oldEntry, err := h.brain.Recall(r.Context(), id); err == nil {
			oldStatus = oldEntry.Status
		}
	}

	entry, err := h.brain.UpdateMetadata(r.Context(), id, fields)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Entry not found: %s", id))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	// Notify SSE clients about the change
	h.notifyProjectChanged(r, entry.Path, entry.Type)

	// Emit entry.updated event for metadata change
	evt := types.NewEvent(types.EventEntryUpdated, types.EventSourceAPI)
	projectID := extractProjectID(entry.Path)
	evt.ProjectID = projectID
	evt.TaskPath = entry.Path
	evt.TaskID = entry.ID
	evt.FeatureID = entry.FeatureID
	evt.Metadata = map[string]string{
		"entry_type": entry.Type,
		"action":     "metadata_update",
	}
	h.emitEvent(r.Context(), evt)

	// Emit task.status_changed when a task's status actually changed. This
	// mirrors HandleUpdateEntry so that runner-driven status updates (which go
	// through this metadata endpoint) trigger goal/automation reconcile loops.
	if entry.Type == "task" && entry.Status != "" && oldStatus != "" && oldStatus != entry.Status {
		statusEvt := types.NewEvent(types.EventTaskStatusChanged, types.EventSourceAPI)
		statusEvt.ProjectID = projectID
		statusEvt.TaskID = entry.ID
		statusEvt.TaskPath = entry.Path
		statusEvt.TaskTitle = entry.Title
		statusEvt.FeatureID = entry.FeatureID
		statusEvt.FromStatus = oldStatus
		statusEvt.ToStatus = entry.Status
		h.emitEvent(r.Context(), statusEvt)
	}

	if h.events != nil && entry.Type == "task" && entry.FeatureID != "" {
		if _, ok := fields["status"]; ok {
			h.events.CheckFeatureCompletion(r.Context(), projectID, entry.FeatureID, entry.ID)
		}
	}

	WriteJSON(w, http.StatusOK, entry)
}

// HandleDeleteEntry handles DELETE /entries/{id} or DELETE /entries/path/to/entry.md.
func (h *Handler) HandleDeleteEntry(w http.ResponseWriter, r *http.Request) {
	// Chi wildcard /* captures everything after /entries/ in the "*" parameter
	id := chi.URLParam(r, "*")
	// Fallback to "id" parameter for backward compatibility (if route uses /{id})
	if id == "" {
		id = chi.URLParam(r, "id")
	}

	confirm := r.URL.Query().Get("confirm")
	if confirm != "true" {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Missing confirm=true query parameter")
		return
	}

	// Recall entry before deletion for event metadata.
	var deletedEntry *types.BrainEntry
	if h.events != nil {
		deletedEntry, _ = h.brain.Recall(r.Context(), id)
	}

	err := h.brain.Delete(r.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Entry not found: %s", id))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	// Notify SSE clients about the deletion
	h.notifyProjectChanged(r, id, "task") // id may be a path like projects/test1/task/xxx.md

	// Emit entry.deleted event
	evt := types.NewEvent(types.EventEntryDeleted, types.EventSourceAPI)
	evt.ProjectID = extractProjectID(id)
	evt.TaskPath = id
	if deletedEntry != nil {
		evt.TaskID = deletedEntry.ID
		evt.FeatureID = deletedEntry.FeatureID
		evt.Metadata = map[string]string{
			"entry_type": deletedEntry.Type,
			"title":      deletedEntry.Title,
		}
	}
	h.emitEvent(r.Context(), evt)

	w.WriteHeader(http.StatusNoContent)
}

// HandleBulkUpdate handles POST /entries/bulk-update.
func (h *Handler) HandleBulkUpdate(w http.ResponseWriter, r *http.Request) {
	// Read the raw body so we can (a) do strict field validation up front and
	// (b) still decode into the typed request afterwards. Strict decoding
	// prevents silent field drops (e.g. filter typos like "generted_by") which
	// have caused unintended mass mutations on unrelated entries.
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Failed to read request body")
		return
	}

	// Strict decode first: reject unknown fields at any nesting level of the
	// request. If unknown fields exist, respond 400 with the list of names so
	// the caller can fix their payload instead of matching too broadly.
	if unknown := findUnknownBulkUpdateFields(raw); len(unknown) > 0 {
		WriteError(w, http.StatusBadRequest, "Bad Request",
			fmt.Sprintf("unknown fields: %s", strings.Join(unknown, ", ")))
		return
	}

	var req types.BulkUpdateRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Invalid JSON body")
		return
	}

	// Structural validation: must have (filter+updates) XOR entries, not both, not neither
	var details []types.ValidationDetail
	hasFilter := req.Filter != nil
	hasEntries := len(req.Entries) > 0

	if !hasFilter && !hasEntries {
		details = append(details, types.ValidationDetail{
			Field:   "filter/entries",
			Message: "must provide either 'filter'+'updates' or 'entries', not neither",
		})
	}
	if hasFilter && hasEntries {
		details = append(details, types.ValidationDetail{
			Field:   "filter/entries",
			Message: "must provide either 'filter'+'updates' or 'entries', not both",
		})
	}
	if hasFilter && req.Updates == nil {
		details = append(details, types.ValidationDetail{
			Field:   "updates",
			Message: "required when using 'filter' mode",
		})
	}

	if len(details) > 0 {
		WriteJSON(w, http.StatusUnprocessableEntity, types.ErrorResponse{
			Error:   "Validation Error",
			Message: "Invalid request",
			Details: details,
		})
		return
	}

	// Validate enum fields in updates
	if hasFilter && req.Updates != nil {
		details = append(details, validateUpdateEnums("updates", req.Updates)...)
	}
	if hasEntries {
		for i, entry := range req.Entries {
			prefix := fmt.Sprintf("entries[%d].updates", i)
			details = append(details, validateUpdateEnums(prefix, &entry.Updates)...)
		}
	}

	if len(details) > 0 {
		WriteJSON(w, http.StatusUnprocessableEntity, types.ErrorResponse{
			Error:   "Validation Error",
			Message: "Invalid request",
			Details: details,
		})
		return
	}

	resp, err := h.brain.BulkUpdate(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	// Send deduplicated SSE notifications (skip for dry runs)
	if !req.DryRun {
		seen := make(map[string]bool)
		for _, result := range resp.Results {
			if result.Status != "ok" {
				continue
			}
			projectID := extractProjectID(result.Path)
			if projectID != "" && !seen[projectID] {
				seen[projectID] = true
				h.notifyProjectChanged(r, result.Path, "task")
			}
		}

		// Emit entry.updated events for each successful update
		for _, result := range resp.Results {
			if result.Status != "ok" {
				continue
			}
			evt := types.NewEvent(types.EventEntryUpdated, types.EventSourceAPI)
			evt.ProjectID = extractProjectID(result.Path)
			evt.TaskPath = result.Path
			evt.Metadata = map[string]string{
				"bulk": "true",
			}
			h.emitEvent(r.Context(), evt)
		}

		// Check feature completion for bulk-updated tasks
		h.checkFeatureCompletionForBulkUpdate(r.Context(), req, resp)
	}

	WriteJSON(w, http.StatusOK, resp)
}

// bulkUpdateRequestFields is the set of top-level JSON keys accepted on a
// BulkUpdateRequest. Anything else is rejected by HandleBulkUpdate.
var bulkUpdateRequestFields = map[string]struct{}{
	"filter":  {},
	"updates": {},
	"entries": {},
	"dry_run": {},
	"limit":   {},
}

// bulkUpdateFilterFields is the set of top-level JSON keys accepted on a
// BulkUpdateFilter. This must be kept in sync with types.BulkUpdateFilter.
var bulkUpdateFilterFields = map[string]struct{}{
	"feature_id":     {},
	"project":        {},
	"type":           {},
	"status":         {},
	"tags":           {},
	"priority":       {},
	"generated_by":   {},
	"generated_key":  {},
	"agent":          {},
	"executor":       {},
	"execution_mode": {},
}

// findUnknownBulkUpdateFields returns a sorted-by-appearance list of unknown
// JSON keys in the bulk-update request body, checking both top-level fields
// and nested filter fields. It's tolerant of malformed JSON at deeper levels:
// if a section is not a JSON object, it's simply skipped (the typed decode
// step will produce the appropriate error).
func findUnknownBulkUpdateFields(body []byte) []string {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		// Not a JSON object at all — let the typed decoder produce the error.
		return nil
	}

	var unknown []string
	for k := range top {
		if _, ok := bulkUpdateRequestFields[k]; !ok {
			unknown = append(unknown, k)
		}
	}

	if rawFilter, ok := top["filter"]; ok && len(rawFilter) > 0 {
		var filter map[string]json.RawMessage
		if err := json.Unmarshal(rawFilter, &filter); err == nil {
			for k := range filter {
				if _, ok := bulkUpdateFilterFields[k]; !ok {
					unknown = append(unknown, "filter."+k)
				}
			}
		}
	}

	if len(unknown) == 0 {
		return nil
	}
	// Sort for stable, testable output.
	sort.Strings(unknown)
	return unknown
}

// validateUpdateEnums validates enum fields in an UpdateEntryRequest and returns
// validation details with the given field prefix.
func validateUpdateEnums(prefix string, req *types.UpdateEntryRequest) []types.ValidationDetail {
	var details []types.ValidationDetail
	if req.Status != nil && !types.IsValidEntryStatus(*req.Status) {
		details = append(details, types.ValidationDetail{
			Field:   prefix + ".status",
			Message: fmt.Sprintf("invalid status %q", *req.Status),
		})
	}
	if req.Priority != nil && !types.IsValidPriority(*req.Priority) {
		details = append(details, types.ValidationDetail{
			Field:   prefix + ".priority",
			Message: fmt.Sprintf("invalid priority %q", *req.Priority),
		})
	}
	if req.MergePolicy != nil && !isValidEnum(*req.MergePolicy, types.MergePolicies) {
		details = append(details, types.ValidationDetail{
			Field:   prefix + ".merge_policy",
			Message: fmt.Sprintf("invalid merge_policy %q", *req.MergePolicy),
		})
	}
	if req.CheckoutMode != nil && !types.IsValidCheckoutMode(*req.CheckoutMode) {
		details = append(details, types.ValidationDetail{
			Field:   prefix + ".checkout_mode",
			Message: fmt.Sprintf("invalid checkout_mode %q", *req.CheckoutMode),
		})
	}
	if req.MergeStrategy != nil && !isValidEnum(*req.MergeStrategy, types.MergeStrategies) {
		details = append(details, types.ValidationDetail{
			Field:   prefix + ".merge_strategy",
			Message: fmt.Sprintf("invalid merge_strategy %q", *req.MergeStrategy),
		})
	}
	if req.RemoteBranchPolicy != nil && !isValidEnum(*req.RemoteBranchPolicy, types.RemoteBranchPolicies) {
		details = append(details, types.ValidationDetail{
			Field:   prefix + ".remote_branch_policy",
			Message: fmt.Sprintf("invalid remote_branch_policy %q", *req.RemoteBranchPolicy),
		})
	}
	if req.ExecutionMode != nil && !isValidEnum(*req.ExecutionMode, types.ExecutionModes) {
		details = append(details, types.ValidationDetail{
			Field:   prefix + ".execution_mode",
			Message: fmt.Sprintf("invalid execution_mode %q", *req.ExecutionMode),
		})
	}
	if req.FeaturePriority != nil && !types.IsValidPriority(*req.FeaturePriority) {
		details = append(details, types.ValidationDetail{
			Field:   prefix + ".feature_priority",
			Message: fmt.Sprintf("invalid feature_priority %q", *req.FeaturePriority),
		})
	}
	return details
}

// HandlePostWildcard dispatches POST /entries/* to either HandleMoveEntry
// or HandleVerifyEntry based on whether the path ends with "/move" or "/verify".
func (h *Handler) HandlePostWildcard(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "*")
	if strings.HasSuffix(id, "/move") {
		h.HandleMoveEntry(w, r)
		return
	}
	if strings.HasSuffix(id, "/verify") {
		h.HandleVerifyEntry(w, r)
		return
	}
	WriteError(w, http.StatusNotFound, "Not Found", "Unknown POST endpoint")
}

// HandleMoveEntry handles POST /entries/{id}/move or POST /entries/*/move.
func (h *Handler) HandleMoveEntry(w http.ResponseWriter, r *http.Request) {
	// Chi wildcard /* captures everything after /entries/ in the "*" parameter
	id := chi.URLParam(r, "*")
	if id == "" {
		id = chi.URLParam(r, "id")
	}
	// Strip "/move" suffix from the path to get the entry identifier
	id = strings.TrimSuffix(id, "/move")

	var req types.MoveEntryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Invalid JSON body")
		return
	}

	if req.Project == "" {
		WriteValidationError(w, []types.ValidationDetail{
			{Field: "project", Message: "required"},
		})
		return
	}

	result, err := h.brain.Move(r.Context(), id, req.Project)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", fmt.Sprintf("Entry not found: %s", id))
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	// Notify both source and target projects
	h.notifyProjectChanged(r, result.From, "task") // source project
	h.notifyProjectChanged(r, result.To, "task")   // target project

	// Emit entry.updated event for move
	evt := types.NewEvent(types.EventEntryUpdated, types.EventSourceAPI)
	evt.ProjectID = extractProjectID(result.To)
	evt.TaskPath = result.To
	evt.Metadata = map[string]string{
		"action":       "move",
		"from_path":    result.From,
		"to_path":      result.To,
		"from_project": extractProjectID(result.From),
		"to_project":   extractProjectID(result.To),
	}
	h.emitEvent(r.Context(), evt)

	WriteJSON(w, http.StatusOK, result)
}

// =============================================================================
// SSE Notification Helpers
// =============================================================================

// notifyProjectChanged publishes SSE events after an entry mutation.
// It publishes both a project_dirty and a tasks_snapshot event for the
// project extracted from the entry path.
func (h *Handler) notifyProjectChanged(r *http.Request, entryPath string, entryType string) {
	if h.hub == nil {
		return
	}

	// Only publish for task-type entries (or always dirty for any mutation)
	projectID := extractProjectID(entryPath)
	if projectID == "" {
		return
	}

	h.hub.PublishProjectDirty(projectID)

	// Also send a fresh tasks_snapshot so SSE clients get updated data
	if h.tasks != nil {
		resp, err := h.tasks.GetTasks(r.Context(), projectID)
		if err == nil {
			h.hub.PublishTaskSnapshot(projectID, types.SSETasksSnapshotData{
				SSEEventData: types.SSEEventData{
					Type:      types.SSEEventTasksSnapshot,
					Transport: "sse",
					Timestamp: types.TimeNowUTC().Format("2006-01-02T15:04:05Z"),
					ProjectID: projectID,
				},
				Tasks:  resp.Tasks,
				Count:  resp.Count,
				Stats:  resp.Stats,
				Cycles: resp.Cycles,
			})
		}
	}
}

// =============================================================================
// Event Emission Helper
// =============================================================================

// emitEvent publishes an event through the EventService if configured.
// It is a fire-and-forget helper — errors are logged but never block the
// HTTP response path.
func (h *Handler) emitEvent(ctx context.Context, evt types.Event) {
	if h.events == nil {
		return
	}
	if err := h.events.Ingest(ctx, []types.Event{evt}); err != nil {
		slog.Warn("failed to emit event",
			"event_type", evt.Type,
			"error", err,
		)
	}
}

// =============================================================================
// Feature Completion Detection
// =============================================================================

// checkFeatureCompletionForEntry checks if a task's feature is now complete
// after a single entry update. Only triggers when:
// 1. The entry is a task type
// 2. The status was explicitly changed to "completed" or "validated"
// 3. The entry has a feature_id
// 4. An event service is configured
func (h *Handler) checkFeatureCompletionForEntry(ctx context.Context, entry *types.BrainEntry, newStatus *string) {
	if h.events == nil || entry == nil {
		return
	}
	if entry.Type != "task" {
		return
	}
	if newStatus == nil {
		return
	}
	if *newStatus != "completed" && *newStatus != "validated" {
		return
	}
	if entry.FeatureID == "" {
		return
	}

	projectID := extractProjectID(entry.Path)
	if projectID == "" {
		return
	}

	h.events.CheckFeatureCompletion(ctx, projectID, entry.FeatureID, entry.ID)
}

// checkFeatureCompletionForBulkUpdate checks feature completion for tasks
// updated in a bulk update operation. It recalls each successfully updated
// entry to check if it's a task with a feature_id.
func (h *Handler) checkFeatureCompletionForBulkUpdate(ctx context.Context, req types.BulkUpdateRequest, resp *types.BulkUpdateResponse) {
	if h.events == nil || resp == nil {
		return
	}

	// Determine the status being set
	var newStatus *string
	if req.Updates != nil && req.Updates.Status != nil {
		newStatus = req.Updates.Status
	}

	// For explicit mode, check each entry's status
	if newStatus == nil && len(req.Entries) > 0 {
		for _, result := range resp.Results {
			if result.Status != "ok" {
				continue
			}
			for _, reqEntry := range req.Entries {
				if reqEntry.Path == result.Path && reqEntry.Updates.Status != nil {
					s := *reqEntry.Updates.Status
					if s == "completed" || s == "validated" {
						entry, err := h.brain.Recall(ctx, result.Path)
						if err == nil && entry.Type == "task" && entry.FeatureID != "" {
							projectID := extractProjectID(entry.Path)
							if projectID != "" {
								h.events.CheckFeatureCompletion(ctx, projectID, entry.FeatureID, entry.ID)
							}
						}
					}
					break
				}
			}
		}
		return
	}

	// For filter mode with status change to completed/validated
	if newStatus == nil {
		return
	}
	if *newStatus != "completed" && *newStatus != "validated" {
		return
	}

	// Check each successfully updated entry
	seenFeatures := make(map[string]bool)
	for _, result := range resp.Results {
		if result.Status != "ok" {
			continue
		}
		entry, err := h.brain.Recall(ctx, result.Path)
		if err != nil {
			continue
		}
		if entry.Type != "task" || entry.FeatureID == "" {
			continue
		}
		projectID := extractProjectID(entry.Path)
		if projectID == "" {
			continue
		}
		featureKey := projectID + ":" + entry.FeatureID
		if seenFeatures[featureKey] {
			continue // Already checked this feature
		}
		seenFeatures[featureKey] = true
		h.events.CheckFeatureCompletion(ctx, projectID, entry.FeatureID, entry.ID)
	}
}

// extractProjectID extracts the project ID from an entry path.
// Path format: "projects/{projectId}/task/{shortId}.md"
func extractProjectID(path string) string {
	if !strings.HasPrefix(path, "projects/") {
		return ""
	}
	parts := strings.SplitN(path, "/", 4)
	if len(parts) < 2 {
		return ""
	}
	return parts[1]
}

// =============================================================================
// Helpers
// =============================================================================

// isValidEnum checks if a value is in the given list.
func isValidEnum(val string, valid []string) bool {
	for _, v := range valid {
		if val == v {
			return true
		}
	}
	return false
}

// isValidSortBy checks if a sortBy value is valid.
func isValidSortBy(s string) bool {
	switch s {
	case "created", "modified", "priority", "completed", "title":
		return true
	}
	return false
}

// isValidPriority checks if a priority value is valid.
func isValidPriority(p string) bool {
	return p == "high" || p == "medium" || p == "low"
}

// parseIncludeQuery parses comma-separated include query values while ignoring
// empty segments. It preserves caller order so services can apply precedence.
func parseIncludeQuery(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	include := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		include = append(include, part)
	}
	return include
}

// strPtr returns a pointer to the given string, or nil if empty.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// validateTimezone validates that a non-empty timezone string is a valid IANA timezone name.
func validateTimezone(tz string, field string) []types.ValidationDetail {
	if tz == "" {
		return nil
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return []types.ValidationDetail{{
			Field:   field,
			Message: fmt.Sprintf("invalid IANA timezone %q", tz),
		}}
	}
	return nil
}

// validateRFC3339 validates that a non-empty string is a valid RFC3339 timestamp.
func validateRFC3339(ts string, field string) []types.ValidationDetail {
	if ts == "" {
		return nil
	}
	if _, err := time.Parse(time.RFC3339, ts); err != nil {
		return []types.ValidationDetail{{
			Field:   field,
			Message: fmt.Sprintf("invalid RFC3339 timestamp %q", ts),
		}}
	}
	return nil
}

// validateExpiresAfterStarts validates that expires_at > starts_at when both are set and valid.
func validateExpiresAfterStarts(startsAt, expiresAt string) []types.ValidationDetail {
	if startsAt == "" || expiresAt == "" {
		return nil
	}
	st, errS := time.Parse(time.RFC3339, startsAt)
	et, errE := time.Parse(time.RFC3339, expiresAt)
	if errS != nil || errE != nil {
		// Individual field errors already reported by validateRFC3339
		return nil
	}
	if !et.After(st) {
		return []types.ValidationDetail{{
			Field:   "expires_at",
			Message: "expires_at must be after starts_at",
		}}
	}
	return nil
}

// validateCronSchedule validates that a non-empty string is a valid cron expression.
func validateCronSchedule(expr string, field string) []types.ValidationDetail {
	if expr == "" {
		return nil
	}
	if _, err := cron.Parse(expr); err != nil {
		return []types.ValidationDetail{{
			Field:   field,
			Message: fmt.Sprintf("invalid cron expression %q: %v", expr, err),
		}}
	}
	return nil
}

// mapFrontmatterToUpdateRequest converts parsed frontmatter fields and body
// into an UpdateEntryRequest. Only non-empty/non-nil fields are set so that
// the update is a partial merge (matching JSON PATCH semantics).
func mapFrontmatterToUpdateRequest(fm frontmatter.Frontmatter, body string) types.UpdateEntryRequest {
	req := types.UpdateEntryRequest{
		Title:               strPtr(fm.Title),
		Status:              strPtr(fm.Status),
		Priority:            strPtr(fm.Priority),
		FeatureID:           strPtr(fm.FeatureID),
		FeaturePriority:     strPtr(fm.FeaturePriority),
		GitBranch:           strPtr(fm.GitBranch),
		GitRemote:           strPtr(fm.GitRemote),
		Workdir:             strPtr(fm.Workdir),
		TargetWorkdir:       strPtr(fm.TargetWorkdir),
		MergeTargetBranch:   strPtr(fm.MergeTargetBranch),
		MergePolicy:         strPtr(fm.MergePolicy),
		MergeStrategy:       strPtr(fm.MergeStrategy),
		RemoteBranchPolicy:  strPtr(fm.RemoteBranchPolicy),
		ExecutionMode:       strPtr(fm.ExecutionMode),
		CheckoutMode:        strPtr(fm.CheckoutMode),
		DirectPrompt:        strPtr(fm.DirectPrompt),
		UserOriginalRequest: strPtr(fm.UserOriginalRequest),
		Agent:               strPtr(fm.Agent),
		Model:               strPtr(fm.Model),
		Executor:            strPtr(fm.Executor),
		Schedule:            strPtr(fm.Schedule),
	}

	// Body → Content
	if body != "" {
		req.Content = &body
	}

	// Tags (slice — set if non-empty)
	if len(fm.Tags) > 0 {
		req.Tags = fm.Tags
	}

	// DependsOn (pointer to slice)
	if len(fm.DependsOn) > 0 {
		deps := fm.DependsOn
		req.DependsOn = &deps
	}

	if len(fm.Attachments) > 0 {
		attachments := attachmentRefsFromFM(fm.Attachments)
		req.Attachments = &attachments
	}

	// FeatureDependsOn (pointer to slice)
	if len(fm.FeatureDependsOn) > 0 {
		fdeps := fm.FeatureDependsOn
		req.FeatureDependsOn = &fdeps
	}

	// Extensions (pointer to slice)
	if len(fm.Extensions) > 0 {
		exts := fm.Extensions
		req.Extensions = &exts
	}

	// Boolean pointer fields — only set when present in frontmatter
	if fm.ScheduleEnabled != nil {
		req.ScheduleEnabled = fm.ScheduleEnabled
	}
	if fm.CompleteOnIdle != nil {
		req.CompleteOnIdle = fm.CompleteOnIdle
	}
	if fm.OpenPRBeforeMerge != nil {
		req.OpenPRBeforeMerge = fm.OpenPRBeforeMerge
	}

	// Integer pointer fields
	if fm.MaxRuns != nil {
		req.MaxRuns = fm.MaxRuns
	}

	// Automation-specific nested structs (trigger/action/retry/goal). These
	// are required for editing automation entries via the PWA's raw-file
	// editor — without them the save would silently drop schedule/event
	// changes and leave the entry unchanged. brain.Update at
	// service/brain.go:856–867 knows how to apply these; we just need to
	// carry them through the FM→UpdateRequest hop.
	if fm.Trigger != nil {
		req.Trigger = fmTriggerConfigToType(fm.Trigger)
	}
	if fm.Action != nil {
		req.Action = fmAutomationActionToType(fm.Action)
	}
	if fm.Retry != nil {
		req.Retry = fmAutomationRetryToType(fm.Retry)
	}
	if fm.Goal != nil {
		req.Goal = fmGoalConfigToType(fm.Goal)
	}

	return req
}

// fmTriggerConfigToType converts a frontmatter TriggerConfig to a domain
// TriggerConfig. The two structs are field-identical; this exists purely
// as an API-package-local converter so we don't create an internal/api →
// internal/service import.
func fmTriggerConfigToType(t *frontmatter.TriggerConfig) *types.TriggerConfig {
	if t == nil {
		return nil
	}
	return &types.TriggerConfig{
		Type:                   t.Type,
		Event:                  t.Event,
		Events:                 t.Events,
		Schedule:               t.Schedule,
		Timezone:               t.Timezone,
		Filter:                 t.Filter,
		OncePer:                t.OncePer,
		Webhook:                t.Webhook,
		IgnoreAutomationEvents: t.IgnoreAutomationEvents,
		Cooldown:               t.Cooldown,
		MaxConcurrent:          t.MaxConcurrent,
	}
}

// fmAutomationActionToType converts a frontmatter AutomationAction to a
// domain AutomationAction.
func fmAutomationActionToType(a *frontmatter.AutomationAction) *types.AutomationAction {
	if a == nil {
		return nil
	}
	return &types.AutomationAction{
		Type:               a.Type,
		DirectPrompt:       a.DirectPrompt,
		Command:            a.Command,
		Agent:              a.Agent,
		Model:              a.Model,
		Executor:           a.Executor,
		TargetWorkdir:      a.TargetWorkdir,
		ExecutionMode:      a.ExecutionMode,
		SessionMode:        a.SessionMode,
		CompleteOnIdle:     a.CompleteOnIdle,
		Timeout:            a.Timeout,
		RequiresCapability: a.RequiresCapability,
	}
}

// fmAutomationRetryToType converts a frontmatter AutomationRetry to a
// domain AutomationRetry.
func fmAutomationRetryToType(r *frontmatter.AutomationRetry) *types.AutomationRetry {
	if r == nil {
		return nil
	}
	return &types.AutomationRetry{
		MaxAttempts: r.MaxAttempts,
		Backoff:     r.Backoff,
		Delay:       r.Delay,
	}
}

// fmGoalConfigToType converts a frontmatter GoalConfig to a domain GoalConfig.
func fmGoalConfigToType(g *frontmatter.GoalConfig) *types.GoalConfig {
	if g == nil {
		return nil
	}
	return &types.GoalConfig{
		ID:               g.ID,
		Criteria:         g.Criteria,
		Validation:       g.Validation,
		Workdir:          g.Workdir,
		TriggerSource:    g.TriggerSource,
		CompleteStatuses: g.CompleteStatuses,
		BlockedStatuses:  g.BlockedStatuses,
	}
}

func attachmentRefsFromFM(refs []frontmatter.AttachmentReference) []types.AttachmentReference {
	if len(refs) == 0 {
		return nil
	}
	result := make([]types.AttachmentReference, 0, len(refs))
	for _, ref := range refs {
		result = append(result, types.AttachmentReference{
			ID:          ref.ID,
			Filename:    ref.Filename,
			ContentType: ref.ContentType,
			Size:        ref.Size,
			SHA256:      ref.SHA256,
			Role:        ref.Role,
			Caption:     ref.Caption,
			Derived:     attachmentDerivedFromFM(ref.Derived),
		})
	}
	return result
}

func attachmentDerivedFromFM(items []frontmatter.AttachmentDerived) []types.AttachmentDerived {
	if len(items) == 0 {
		return nil
	}
	result := make([]types.AttachmentDerived, 0, len(items))
	for _, item := range items {
		result = append(result, types.AttachmentDerived{
			ID:          item.ID,
			Kind:        item.Kind,
			ContentType: item.ContentType,
			Size:        item.Size,
			StorageKey:  item.StorageKey,
			Created:     item.Created,
		})
	}
	return result
}

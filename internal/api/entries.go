package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/pkg/frontmatter"
)

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
	if req.FeaturePriority != "" && !types.IsValidPriority(req.FeaturePriority) {
		details = append(details, types.ValidationDetail{
			Field:   "feature_priority",
			Message: fmt.Sprintf("invalid feature_priority %q", req.FeaturePriority),
		})
	}

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

	entry, err := h.brain.Recall(r.Context(), id)
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
			Message: fmt.Sprintf("invalid sortBy %q, must be one of: created, modified, priority", sortBy),
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
	if req.FeaturePriority != nil && !types.IsValidPriority(*req.FeaturePriority) {
		details = append(details, types.ValidationDetail{
			Field:   "feature_priority",
			Message: fmt.Sprintf("invalid feature_priority %q", *req.FeaturePriority),
		})
	}

	if len(details) > 0 {
		WriteValidationError(w, details)
		return
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

	w.WriteHeader(http.StatusNoContent)
}

// HandleMoveEntry handles POST /entries/{id}/move.
func (h *Handler) HandleMoveEntry(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

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
	case "created", "modified", "priority":
		return true
	}
	return false
}

// isValidPriority checks if a priority value is valid.
func isValidPriority(p string) bool {
	return p == "high" || p == "medium" || p == "low"
}

// strPtr returns a pointer to the given string, or nil if empty.
func strPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// mapFrontmatterToUpdateRequest converts parsed frontmatter fields and body
// into an UpdateEntryRequest. Only non-empty/non-nil fields are set so that
// the update is a partial merge (matching JSON PATCH semantics).
func mapFrontmatterToUpdateRequest(fm frontmatter.Frontmatter, body string) types.UpdateEntryRequest {
	req := types.UpdateEntryRequest{
		Title:              strPtr(fm.Title),
		Status:             strPtr(fm.Status),
		Priority:           strPtr(fm.Priority),
		FeatureID:          strPtr(fm.FeatureID),
		FeaturePriority:    strPtr(fm.FeaturePriority),
		GitBranch:          strPtr(fm.GitBranch),
		TargetWorkdir:      strPtr(fm.TargetWorkdir),
		MergeTargetBranch:  strPtr(fm.MergeTargetBranch),
		MergePolicy:        strPtr(fm.MergePolicy),
		MergeStrategy:      strPtr(fm.MergeStrategy),
		RemoteBranchPolicy: strPtr(fm.RemoteBranchPolicy),
		ExecutionMode:      strPtr(fm.ExecutionMode),
		DirectPrompt:       strPtr(fm.DirectPrompt),
		Agent:              strPtr(fm.Agent),
		Model:              strPtr(fm.Model),
		Executor:           strPtr(fm.Executor),
		Schedule:           strPtr(fm.Schedule),
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

	return req
}

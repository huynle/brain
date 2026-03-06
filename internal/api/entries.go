package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "Invalid JSON body")
		return
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

	WriteJSON(w, http.StatusOK, result)
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

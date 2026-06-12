package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// HandleListAutomationRuns handles GET /automation-runs.
func (h *Handler) HandleListAutomationRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 100
	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}

	resp, err := h.brain.List(r.Context(), types.ListEntriesRequest{
		Type:    "automation_run",
		Project: q.Get("project"),
		Status:  q.Get("status"),
		Limit:   limit,
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	automationID := q.Get("automation_id")
	if automationID != "" {
		filtered := make([]types.BrainEntry, 0, len(resp.Entries))
		for _, entry := range resp.Entries {
			if automationRunContentField(entry.Content, "automation_id") == automationID {
				filtered = append(filtered, entry)
			}
		}
		resp.Entries = filtered
		resp.Total = len(filtered)
	}

	WriteJSON(w, http.StatusOK, resp)
}

// HandleGetAutomationRun handles GET /automation-runs/{id}.
func (h *Handler) HandleGetAutomationRun(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runId")
	entry, err := h.brain.Recall(r.Context(), runID)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			WriteError(w, http.StatusNotFound, "Not Found", "automation run not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	if entry.Type != "automation_run" {
		WriteError(w, http.StatusNotFound, "Not Found", "automation run not found")
		return
	}
	WriteJSON(w, http.StatusOK, entry)
}

func automationRunContentField(content, field string) string {
	prefix := field + ":"
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(line, prefix))
		}
	}
	return ""
}

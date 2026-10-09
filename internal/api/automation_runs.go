package api

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

const (
	// automationRunFilterOverfetch is how many candidate rows to scan per
	// requested row when filtering by automation_id, which is not a column.
	automationRunFilterOverfetch = 20
	// automationRunFilterMaxScan caps that scan. automation_run is by far
	// the largest table; an unbounded walk is worse than an incomplete page.
	automationRunFilterMaxScan = 5000
)

// HandleListAutomationRuns handles GET /automation-runs.
//
// Without automation_id it is a plain newest-first listing. With automation_id
// it reads the automation:<id> tag index first, which every audit written
// since tagging carries. Only when that page is short does it fall back to a
// bounded body scan, for legacy audits that predate the tag, and the two sets
// are merged, de-duplicated, and sorted newest first.
func (h *Handler) HandleListAutomationRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 100
	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}

	automationID := q.Get("automation_id")
	project := q.Get("project")
	status := q.Get("status")

	if automationID == "" {
		resp, err := h.brain.List(r.Context(), types.ListEntriesRequest{
			Type:    "automation_run",
			Project: project,
			Status:  status,
			Limit:   limit,
		})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, resp)
		return
	}

	// Tag-indexed query. A full page is final: any run older than the
	// newest `limit` tagged runs is not on this page either.
	tagged, err := h.brain.List(r.Context(), types.ListEntriesRequest{
		Type:      "automation_run",
		Project:   project,
		Status:    status,
		Tags:      "automation:" + automationID,
		SortBy:    "created",
		SortOrder: "desc",
		Limit:     limit,
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	entries := tagged.Entries
	truncated := false

	// Legacy fallback. automation_id is not a column for audits written
	// before the tag, so a short tagged page is completed by scanning
	// bodies. Over-fetch, then trim. Bounded deliberately: an unbounded scan
	// of that table is its own problem.
	if len(entries) < limit {
		fetchLimit := limit * automationRunFilterOverfetch
		if fetchLimit > automationRunFilterMaxScan {
			fetchLimit = automationRunFilterMaxScan
		}
		scan, err := h.brain.List(r.Context(), types.ListEntriesRequest{
			Type:    "automation_run",
			Project: project,
			Status:  status,
			Limit:   fetchLimit,
		})
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
			return
		}

		seen := make(map[string]bool, len(entries))
		for _, e := range entries {
			seen[e.ID] = true
		}
		merged := append([]types.BrainEntry{}, entries...)
		for _, entry := range scan.Entries {
			if seen[entry.ID] {
				continue
			}
			if automationRunContentField(entry.Content, "automation_id") == automationID {
				merged = append(merged, entry)
			}
		}
		sort.SliceStable(merged, func(i, j int) bool {
			return merged[i].Created > merged[j].Created
		})
		if len(merged) > limit {
			merged = merged[:limit]
		}
		entries = merged

		// Say so when the scan window was exhausted without filling the
		// page: "no more runs" and "older than the window" are different
		// answers and the caller must be able to tell them apart.
		truncated = len(entries) < limit && len(scan.Entries) >= fetchLimit
	}

	tagged.Entries = entries
	tagged.Total = len(entries)
	tagged.Truncated = truncated
	WriteJSON(w, http.StatusOK, tagged)
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

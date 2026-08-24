package api

import (
	"errors"
	"net/http"
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
func (h *Handler) HandleListAutomationRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 100
	if raw := q.Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			limit = n
		}
	}

	automationID := q.Get("automation_id")

	// automation_id is not a column — it lives in the run's markdown body —
	// so it can only be matched after fetching. Fetching exactly `limit`
	// rows and filtering afterwards therefore asked for the N most recent
	// runs across ALL automations and then kept the few that matched. On a
	// store where automation_run is ~95% of all entries, one automation's
	// runs are almost never in that page, so a filtered query returned
	// nothing while its runs existed in their thousands — indistinguishable
	// from "this automation has never run".
	//
	// Over-fetch when filtering, then trim. Bounded deliberately: an
	// unbounded scan of that table is its own problem.
	fetchLimit := limit
	if automationID != "" {
		fetchLimit = limit * automationRunFilterOverfetch
		if fetchLimit > automationRunFilterMaxScan {
			fetchLimit = automationRunFilterMaxScan
		}
	}

	resp, err := h.brain.List(r.Context(), types.ListEntriesRequest{
		Type:    "automation_run",
		Project: q.Get("project"),
		Status:  q.Get("status"),
		Limit:   fetchLimit,
	})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}

	if automationID != "" {
		scanned := len(resp.Entries)
		filtered := make([]types.BrainEntry, 0, limit)
		for _, entry := range resp.Entries {
			if automationRunContentField(entry.Content, "automation_id") == automationID {
				filtered = append(filtered, entry)
				if len(filtered) == limit {
					break
				}
			}
		}
		resp.Entries = filtered
		resp.Total = len(filtered)
		// Say so when the scan window was exhausted without filling the
		// page: "no more runs" and "older than the window" are different
		// answers and the caller must be able to tell them apart.
		resp.Truncated = len(filtered) < limit && scanned >= fetchLimit
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

package api

import (
	"net/http"
	"strings"
	"time"
)

const maxTimelineRange = 366 * 24 * time.Hour

// HandleTimeline returns actual events and read-only schedule projections.
func (h *Handler) HandleTimeline(w http.ResponseWriter, r *http.Request) {
	now := time.Now().UTC()
	from, err := parseTimelineTime(r.URL.Query().Get("from"), now.Add(-30*24*time.Hour))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Validation Error", "from must be RFC3339")
		return
	}
	to, err := parseTimelineTime(r.URL.Query().Get("to"), now.Add(30*24*time.Hour))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Validation Error", "to must be RFC3339")
		return
	}
	if !to.After(from) {
		WriteError(w, http.StatusBadRequest, "Validation Error", "to must be after from")
		return
	}
	if to.Sub(from) > maxTimelineRange {
		WriteError(w, http.StatusBadRequest, "Validation Error", "timeline range cannot exceed 366 days")
		return
	}
	response, err := h.timeline.Timeline(r.Context(), from, to, strings.TrimSpace(r.URL.Query().Get("project")))
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", err.Error())
		return
	}
	WriteJSON(w, http.StatusOK, response)
}

func parseTimelineTime(value string, fallback time.Time) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return fallback, nil
	}
	return time.Parse(time.RFC3339, value)
}

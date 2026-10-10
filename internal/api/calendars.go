package api

import (
	"net/http"
	"time"

	"github.com/huynle/brain-api/internal/calendar"
)

// calendarStatusView is one source as GET /calendars reports it. A builtin
// source carries only name and kind. An ics source adds its fetch fields, and
// the time fields appear only once the source has been fetched. A feed URL or
// file path never appears here.
type calendarStatusView struct {
	Name        string     `json:"name"`
	Kind        string     `json:"kind"`
	LastFetch   *time.Time `json:"last_fetch,omitempty"`
	LastSuccess *time.Time `json:"last_success,omitempty"`
	EventCount  *int       `json:"event_count,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
	Stale       *bool      `json:"stale,omitempty"`
}

type calendarsResponse struct {
	Calendars []calendarStatusView `json:"calendars"`
}

// HandleCalendars handles GET /api/v1/calendars: the status of every
// configured calendar source, sorted by name.
//
// 501: the calendar service is not configured.
func (h *Handler) HandleCalendars(w http.ResponseWriter, r *http.Request) {
	if h.calendars == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "calendar service is not configured")
		return
	}
	statuses := h.calendars.Statuses()
	out := make([]calendarStatusView, 0, len(statuses))
	for _, s := range statuses {
		out = append(out, viewOfCalendar(s))
	}
	WriteJSON(w, http.StatusOK, calendarsResponse{Calendars: out})
}

// viewOfCalendar maps a registry status onto the response shape.
func viewOfCalendar(s calendar.Status) calendarStatusView {
	v := calendarStatusView{Name: s.Name, Kind: s.Kind}
	if s.Kind != calendar.KindICS {
		return v
	}
	count := s.EventCount
	stale := s.Stale
	v.EventCount = &count
	v.Stale = &stale
	if !s.LastFetch.IsZero() {
		fetched := s.LastFetch
		v.LastFetch = &fetched
	}
	if !s.LastSuccess.IsZero() {
		succeeded := s.LastSuccess
		v.LastSuccess = &succeeded
	}
	v.LastError = s.LastError
	return v
}

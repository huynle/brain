package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/calendar"
)

// CalendarManager is the write side of runtime calendar sources.
// *calendar.Manager implements it. A nil manager means tenant mode, where the
// write routes answer 501.
type CalendarManager interface {
	Add(ctx context.Context, req calendar.AddRequest, actor string) (calendar.Status, error)
	Update(ctx context.Context, name string, req calendar.UpdateRequest, actor string) (calendar.Status, error)
	Remove(ctx context.Context, name string, force bool, actor string) (calendar.RemoveResult, error)
}

// calendarWriteBodyMaxBytes bounds one calendar write body. An address is at
// most 2048 bytes, so 8 KiB leaves room for the other fields and nothing more.
const calendarWriteBodyMaxBytes = 8 << 10

// createCalendarRequest is the body of POST /api/v1/calendars. Poll is a Go
// duration string; empty means the default.
type createCalendarRequest struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Poll string `json:"poll,omitempty"`
}

// updateCalendarRequest is the body of PUT /api/v1/calendars/{name}. At least
// one field must be present; the manager enforces that.
type updateCalendarRequest struct {
	URL  *string `json:"url,omitempty"`
	Poll *string `json:"poll,omitempty"`
}

// calendarReferenceView is one automation field that names a source. The
// field names match the design's HTTP contract.
type calendarReferenceView struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Title string `json:"title"`
	Field string `json:"field"`
}

// calendarRemovalResponse is the 200 body of DELETE /api/v1/calendars/{name}.
type calendarRemovalResponse struct {
	Name       string                  `json:"name"`
	Removed    bool                    `json:"removed"`
	References []calendarReferenceView `json:"references"`
}

// calendarErrorResponse is an error body. Only the 409 for a referenced
// source carries references.
type calendarErrorResponse struct {
	Error      string                  `json:"error"`
	Message    string                  `json:"message"`
	References []calendarReferenceView `json:"references,omitempty"`
}

// WithCalendarManager attaches the runtime calendar manager behind the admin
// write routes. Tenant mode never calls it, so the write routes answer 501.
func WithCalendarManager(m CalendarManager) HandlerOption {
	return func(h *Handler) {
		h.calendarManager = m
	}
}

// HandleCreateCalendar handles POST /api/v1/calendars. The address is accepted
// only here and in PUT; it is never returned, logged or echoed in an error.
//
// 201 created, 400 bad body/name/poll, 409 exists/limit/config source,
// 422 rejected address, 501 no manager (tenant mode).
func (h *Handler) HandleCreateCalendar(w http.ResponseWriter, r *http.Request) {
	if h.calendarManager == nil {
		writeCalendarNotImplemented(w)
		return
	}
	var req createCalendarRequest
	if !decodeCalendarBody(w, r, &req) {
		return
	}
	poll, err := parseCalendarPoll(req.Poll)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "calendar poll must be a Go duration, e.g. 5m")
		return
	}
	status, err := h.calendarManager.Add(r.Context(), calendar.AddRequest{Name: req.Name, URL: req.URL, Poll: poll}, callerName(r))
	if err != nil {
		writeCalendarWriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, viewOfCalendar(status))
}

// HandleUpdateCalendar handles PUT /api/v1/calendars/{name}: replace the
// address, change the poll, or both.
//
// 200 updated, 400 bad body/poll/empty update, 404 unknown name,
// 409 config source, 422 rejected address, 501 no manager.
func (h *Handler) HandleUpdateCalendar(w http.ResponseWriter, r *http.Request) {
	if h.calendarManager == nil {
		writeCalendarNotImplemented(w)
		return
	}
	var body updateCalendarRequest
	if !decodeCalendarBody(w, r, &body) {
		return
	}
	req := calendar.UpdateRequest{URL: body.URL}
	if body.Poll != nil {
		if *body.Poll == "" {
			WriteError(w, http.StatusBadRequest, "Bad Request", "calendar poll must be a Go duration, e.g. 10m")
			return
		}
		poll, err := parseCalendarPoll(*body.Poll)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "Bad Request", "calendar poll must be a Go duration, e.g. 10m")
			return
		}
		req.Poll = &poll
	}
	status, err := h.calendarManager.Update(r.Context(), chi.URLParam(r, "name"), req, callerName(r))
	if err != nil {
		writeCalendarWriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, viewOfCalendar(status))
}

// HandleDeleteCalendar handles DELETE /api/v1/calendars/{name}[?force=true].
// A source that automations reference is refused with 409 and the references
// unless force is set; the response lists the references that were removed.
//
// 200 removed, 400 bad force, 404 unknown name, 409 config source or
// referenced without force, 501 no manager.
func (h *Handler) HandleDeleteCalendar(w http.ResponseWriter, r *http.Request) {
	if h.calendarManager == nil {
		writeCalendarNotImplemented(w)
		return
	}
	force := false
	if raw := r.URL.Query().Get("force"); raw != "" {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			WriteError(w, http.StatusBadRequest, "Bad Request", "force must be true or false")
			return
		}
		force = parsed
	}
	name := chi.URLParam(r, "name")
	result, err := h.calendarManager.Remove(r.Context(), name, force, callerName(r))
	if err != nil {
		writeCalendarWriteError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, calendarRemovalResponse{
		Name:       name,
		Removed:    true,
		References: calendarReferenceViews(result.References),
	})
}

// writeCalendarNotImplemented answers the write routes in tenant mode.
func writeCalendarNotImplemented(w http.ResponseWriter) {
	WriteError(w, http.StatusNotImplemented, "Not Implemented", "calendar sources cannot be changed in this deployment")
}

// writeCalendarWriteError maps a manager error to its status. Manager errors
// are redacted by contract, so their text is safe to return. Anything else is
// a server fault and gets a fixed message.
func writeCalendarWriteError(w http.ResponseWriter, err error) {
	var referenced *calendar.ReferencedError
	switch {
	case errors.As(err, &referenced):
		WriteJSON(w, http.StatusConflict, calendarErrorResponse{
			Error:      "Conflict",
			Message:    "calendar is referenced by automations; remove with force=true to proceed",
			References: calendarReferenceViews(referenced.References),
		})
	case errors.Is(err, calendar.ErrNotFound):
		WriteError(w, http.StatusNotFound, "Not Found", err.Error())
	case errors.Is(err, calendar.ErrConfigSource),
		errors.Is(err, calendar.ErrExists),
		errors.Is(err, calendar.ErrLimit):
		WriteError(w, http.StatusConflict, "Conflict", err.Error())
	case errors.Is(err, calendar.ErrRejectedURL):
		WriteError(w, http.StatusUnprocessableEntity, "Unprocessable Entity", err.Error())
	case errors.Is(err, calendar.ErrInvalidName),
		errors.Is(err, calendar.ErrInvalidPoll),
		errors.Is(err, calendar.ErrNothingToUpdate):
		WriteError(w, http.StatusBadRequest, "Bad Request", err.Error())
	default:
		slog.Error("calendar write failed", "error", err.Error())
		WriteError(w, http.StatusInternalServerError, "Internal Server Error", "calendar write failed")
	}
}

// decodeCalendarBody reads one small JSON object with unknown fields rejected
// and no trailing content. Its errors never echo the body, because the body
// may hold an address.
func decodeCalendarBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	fail := func() bool {
		WriteError(w, http.StatusBadRequest, "Bad Request", "request body must be a JSON object with the documented fields")
		return false
	}
	if r.Body == nil {
		return fail()
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, calendarWriteBodyMaxBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fail()
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fail()
	}
	return true
}

// parseCalendarPoll parses an optional poll. Empty means zero, which the
// manager reads as the default. Range checks are the manager's.
func parseCalendarPoll(raw string) (time.Duration, error) {
	if raw == "" {
		return 0, nil
	}
	return time.ParseDuration(raw)
}

// callerName is the audit name of the authenticated caller.
func callerName(r *http.Request) string {
	if auth, ok := AuthResultFromContext(r.Context()); ok && auth.Name != "" {
		return auth.Name
	}
	return "unknown"
}

// calendarReferenceViews maps references to their JSON shape. It never
// returns nil, so an empty list encodes as [].
func calendarReferenceViews(refs []calendar.Reference) []calendarReferenceView {
	out := make([]calendarReferenceView, 0, len(refs))
	for _, ref := range refs {
		out = append(out, calendarReferenceView{ID: ref.ID, Path: ref.Path, Title: ref.Title, Field: ref.Field})
	}
	return out
}

// formatPoll renders a poll interval as the compact Go duration the contract
// documents ("5m", "1h"), falling back to the full form for other values.
// Every result parses back to the same duration.
func formatPoll(d time.Duration) string {
	switch {
	case d > 0 && d%time.Hour == 0:
		return strconv.FormatInt(int64(d/time.Hour), 10) + "h"
	case d > 0 && d%time.Minute == 0:
		return strconv.FormatInt(int64(d/time.Minute), 10) + "m"
	default:
		return d.String()
	}
}

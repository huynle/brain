package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// writeReminderError maps service errors to status codes.
func writeReminderError(w http.ResponseWriter, err error) {
	if errors.Is(err, types.ErrReminderNotFound) {
		WriteError(w, http.StatusNotFound, "Not Found", err.Error())
		return
	}
	WriteError(w, http.StatusBadRequest, "Bad Request", err.Error())
}

// reminderServiceReady writes the 501 guard and reports whether to continue.
//
// Every handler opens with it: the router only mounts these routes when the
// service is non-nil, but a field wired without its option leaves the handler
// reachable and nil, and "501 not configured" is a far better answer than a
// panic.
func (h *Handler) reminderServiceReady(w http.ResponseWriter) bool {
	if h.reminders == nil {
		WriteError(w, http.StatusNotImplemented, "Not Implemented", "reminder service not configured")
		return false
	}
	return true
}

// HandleCreateReminder handles POST /reminders.
func (h *Handler) HandleCreateReminder(w http.ResponseWriter, r *http.Request) {
	if !h.reminderServiceReady(w) {
		return
	}
	var req types.CreateReminderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON: "+err.Error())
		return
	}
	summary, err := h.reminders.CreateReminder(r.Context(), req)
	if err != nil {
		writeReminderError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, summary)
}

// HandleListReminders handles GET /reminders?project=&state=.
func (h *Handler) HandleListReminders(w http.ResponseWriter, r *http.Request) {
	if !h.reminderServiceReady(w) {
		return
	}
	q := r.URL.Query()
	reminders, err := h.reminders.ListReminders(r.Context(), q.Get("project"), q.Get("state"))
	if err != nil {
		writeReminderError(w, err)
		return
	}
	if reminders == nil {
		reminders = []types.ReminderSummary{}
	}
	WriteJSON(w, http.StatusOK, types.ReminderListResponse{
		Reminders: reminders,
		Count:     len(reminders),
	})
}

// HandleGetReminder handles GET /reminders/{reminderId}.
func (h *Handler) HandleGetReminder(w http.ResponseWriter, r *http.Request) {
	if !h.reminderServiceReady(w) {
		return
	}
	summary, err := h.reminders.GetReminder(r.Context(), chi.URLParam(r, "reminderId"))
	if err != nil {
		writeReminderError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, summary)
}

// HandleUpdateReminder handles PATCH /reminders/{reminderId}.
func (h *Handler) HandleUpdateReminder(w http.ResponseWriter, r *http.Request) {
	if !h.reminderServiceReady(w) {
		return
	}
	var req types.UpdateReminderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON: "+err.Error())
		return
	}
	summary, err := h.reminders.UpdateReminder(r.Context(), chi.URLParam(r, "reminderId"), req)
	if err != nil {
		writeReminderError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, summary)
}

// HandleDeleteReminder handles DELETE /reminders/{reminderId}.
func (h *Handler) HandleDeleteReminder(w http.ResponseWriter, r *http.Request) {
	if !h.reminderServiceReady(w) {
		return
	}
	if err := h.reminders.DeleteReminder(r.Context(), chi.URLParam(r, "reminderId")); err != nil {
		writeReminderError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

// HandleAckReminder handles POST /reminders/{reminderId}/ack.
func (h *Handler) HandleAckReminder(w http.ResponseWriter, r *http.Request) {
	if !h.reminderServiceReady(w) {
		return
	}
	summary, err := h.reminders.AckReminder(r.Context(), chi.URLParam(r, "reminderId"))
	if err != nil {
		writeReminderError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, summary)
}

// HandleSnoozeReminder handles POST /reminders/{reminderId}/snooze.
func (h *Handler) HandleSnoozeReminder(w http.ResponseWriter, r *http.Request) {
	if !h.reminderServiceReady(w) {
		return
	}
	var body struct {
		RemindAt string `json:"remind_at"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		WriteError(w, http.StatusBadRequest, "Bad Request", "invalid JSON: "+err.Error())
		return
	}
	summary, err := h.reminders.SnoozeReminder(r.Context(), chi.URLParam(r, "reminderId"), body.RemindAt)
	if err != nil {
		writeReminderError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, summary)
}

// HandleFireReminder handles POST /reminders/{reminderId}/fire — fire now,
// ignoring the schedule.
func (h *Handler) HandleFireReminder(w http.ResponseWriter, r *http.Request) {
	if !h.reminderServiceReady(w) {
		return
	}
	summary, err := h.reminders.FireReminderNow(r.Context(), chi.URLParam(r, "reminderId"))
	if err != nil {
		writeReminderError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, summary)
}

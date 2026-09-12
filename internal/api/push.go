package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/storage"
)

func WithPushService(s *storage.PhonePush) HandlerOption { return func(h *Handler) { h.push = s } }

func (h *Handler) HandlePush(w http.ResponseWriter, r *http.Request) {
	if h.push == nil {
		WriteError(w, 503, "Unavailable", "phone notifications unavailable")
		return
	}
	owner, e := jobOwner(r.Context())
	if e != nil {
		WriteError(w, 403, "Forbidden", "a signed-in local account is required")
		return
	}
	if r.Method == http.MethodGet {
		WriteJSON(w, 200, map[string]string{"public_key": h.push.PublicKey})
		return
	}
	var d storage.PushDevice
	if e = json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&d); e != nil {
		WriteError(w, 400, "Bad Request", "invalid notification subscription")
		return
	}
	switch chi.URLParam(r, "action") {
	case "subscribe":
		e = h.push.Save(owner, d)
	case "unsubscribe":
		e = h.push.Remove(owner, d.Endpoint)
	case "status":
		var device *storage.PushDevice
		device, e = h.push.Device(owner, d.Endpoint)
		if e == nil {
			WriteJSON(w, 200, map[string]any{"device": device})
			return
		}
	case "test":
		e = h.push.Test(r.Context(), owner, d.Endpoint)
	default:
		WriteError(w, 404, "Not Found", "unknown notification action")
		return
	}
	if e != nil {
		WriteError(w, 400, "Notification error", e.Error())
		return
	}
	WriteJSON(w, 200, map[string]bool{"ok": true})
}

// StartPush reads durable reminder and job state, so a restart or a browser
// disconnect cannot lose a completion between the source commit and enqueue.
// It never loads note bodies. Deduplication and retries live in the push store.
func (h *Handler) StartPush(parent context.Context) func() {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTicker(10 * time.Second)
		defer timer.Stop()
		for {
			h.collectPush(ctx)
			if e := h.push.Deliver(ctx); e != nil && ctx.Err() == nil {
				slog.Warn("phone notification delivery failed", "error", e)
			}
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
			}
		}
	}()
	return func() { cancel(); <-done }
}
func (h *Handler) collectPush(ctx context.Context) {
	active, err := h.push.HasDevices()
	if err != nil {
		slog.Warn("phone device scan failed", "error", err)
		return
	}
	if !active {
		return
	}
	if h.reminders != nil {
		reminders, e := h.reminders.ListReminders(ctx, "", "")
		if e != nil {
			slog.Warn("phone reminder scan failed", "error", e)
		} else {
			for _, r := range reminders {
				at, e := time.Parse(time.RFC3339, r.FiredAt)
				if e != nil || time.Since(at) > 24*time.Hour {
					continue
				}
				// Keep private note/task contents off the lock screen by default.
				e = h.push.Enqueue("", "reminder", "reminder:"+r.ReminderID+":"+r.FiredAt, at, storage.PushMessage{Title: "Brain reminder", Body: r.Title, URL: "/?notification=reminders", Tag: "reminder:" + r.ReminderID})
				if e != nil {
					slog.Warn("phone reminder enqueue failed", "error", e)
				}
			}
		}
	}
	if h.assistant != nil && h.assistant.jobs != nil {
		records, e := h.assistant.jobs.store.NotificationJobs()
		if e != nil {
			slog.Warn("phone job scan failed", "error", e)
			return
		}
		for _, r := range records {
			switch r.State {
			case "completed", "failed", "paused", "cancelled":
			default:
				continue
			}
			at, e := time.Parse(time.RFC3339Nano, r.Updated)
			if e != nil || time.Since(at) > 24*time.Hour {
				continue
			}
			e = h.push.Enqueue(r.Owner, "job", fmt.Sprintf("job:%s:%d", r.ID, r.Revision), at, storage.PushMessage{Title: "Brain Assistant", Body: "A background job is " + r.State + ". Open Brain for details.", URL: "/?notification=assistant", Tag: "job:" + r.ID})
			if e != nil {
				slog.Warn("phone job enqueue failed", "error", e)
			}
		}
	}
}

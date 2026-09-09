package api

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/huynle/brain-api/internal/supervision"
	"github.com/huynle/brain-api/internal/types"
)

type eventCursor struct {
	Scope string `json:"scope"`
	ID    string `json:"id"`
}

func supervisorScope(r *http.Request, resource string) string {
	auth, _ := AuthResultFromContext(r.Context())
	b, _ := json.Marshal(struct {
		Resource string
		Auth     *AuthResult
	}{resource, auth})
	// AuthResult.Tenant is deliberately omitted from JSON, so bind it explicitly.
	if auth != nil {
		b = append(b, []byte(fmt.Sprint(auth.Tenant))...)
	}
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func encodeEventCursor(scope, id string) string {
	b, _ := json.Marshal(eventCursor{scope, id})
	return base64.RawURLEncoding.EncodeToString(b)
}

type eventWaitPage struct {
	Events        []types.Event `json:"events"`
	NextCursor    string        `json:"next_cursor"`
	CursorExpired bool          `json:"cursor_expired"`
	TimedOut      bool          `json:"timed_out"`
	Shutdown      bool          `json:"shutdown"`
	Truncated     bool          `json:"truncated"`
}

// HandleEventWait subscribes before reading the ring. Notifications only wake
// the reader; ring order is authoritative even when fan-out arrives out of order.
func (h *Handler) HandleEventWait(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	filters := map[string]string{}
	for _, key := range []string{"project_id", "feature_id", "task_id", "type", "source"} {
		if v := q.Get(key); v != "" {
			if len(v) > 256 {
				WriteError(w, 400, "Bad Request", "filter too long")
				return
			}
			filters[key] = v
		}
	}
	if filters["project_id"] == "" {
		WriteError(w, 400, "Bad Request", "project_id is required")
		return
	}
	timeout, limit := 25000, 100
	for key, dest := range map[string]*int{"timeout_ms": &timeout, "limit": &limit} {
		if value := q.Get(key); value != "" {
			n, err := strconv.Atoi(value)
			if err != nil {
				WriteError(w, 400, "Bad Request", "invalid "+key)
				return
			}
			*dest = n
		}
	}
	if timeout < 0 || timeout > 25000 || limit < 1 || limit > 100 {
		WriteError(w, 400, "Bad Request", "timeout_ms must be 0..25000; limit 1..100")
		return
	}
	b, _ := json.Marshal(filters)
	scope := supervisorScope(r, string(b))
	last := ""
	if after := q.Get("after"); after != "" {
		var c eventCursor
		b, err := base64.RawURLEncoding.DecodeString(after)
		if err != nil || len(b) > 2048 || json.Unmarshal(b, &c) != nil || c.Scope != scope {
			WriteError(w, 400, "Bad Request", "invalid cursor for these filters or caller")
			return
		}
		last = c.ID
	}
	ch, unsub := h.events.Subscribe(r.Context(), filters)
	defer unsub()
	timer := time.NewTimer(time.Duration(timeout) * time.Millisecond)
	defer timer.Stop()
	page := eventWaitPage{Events: []types.Event{}}
	for {
		events, err := h.events.Recent(r.Context(), max(1000, h.events.Coverage().Capacity), nil)
		if err != nil {
			WriteError(w, 500, "Internal Server Error", err.Error())
			return
		}
		start := 0
		if last != "" {
			found := false
			for i, e := range events {
				if e.ID == last {
					start = i + 1
					found = true
					break
				}
			}
			if !found {
				page.CursorExpired = true
			}
		}
		used := 1024
		for _, e := range events[start:] {
			originalID := e.ID
			if matchesEventFilters(e, filters) {
				// Avoid high-volume poll/sample noise unless explicitly requested.
				if filters["type"] == "" && (e.Type == types.EventTaskResourceSample || e.Type == types.EventRunnerPollComplete) {
					last = e.ID
					continue
				}
				e = supervisorEvent(e)
				data, _ := json.Marshal(e)
				if len(page.Events) == limit || used+len(data) > 65536 {
					page.Truncated = true
					break
				}
				page.Events = append(page.Events, e)
				used += len(data)
			}
			last = originalID
		}
		page.NextCursor = encodeEventCursor(scope, last)
		if len(page.Events) > 0 || page.CursorExpired || page.Truncated {
			WriteJSON(w, 200, page)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-timer.C:
			page.TimedOut = true
			WriteJSON(w, 200, page)
			return
		case _, ok := <-ch:
			if !ok {
				page.Shutdown = true
				WriteJSON(w, 200, page)
				return
			}
		}
	}
}
func supervisorEvent(e types.Event) types.Event {
	// Only bounded identifiers and supervisor-relevant metadata are exposed.
	clip := func(s string) string {
		s = supervision.Redact(s)
		if len(s) > 256 {
			s = s[:256]
		}
		return s
	}
	e.ID = clip(e.ID)
	e.Type = clip(e.Type)
	e.Source = clip(e.Source)
	e.RunnerID = clip(e.RunnerID)
	e.ProjectID = clip(e.ProjectID)
	e.TaskID = clip(e.TaskID)
	e.FeatureID = clip(e.FeatureID)
	e.FromStatus = clip(e.FromStatus)
	e.ToStatus = clip(e.ToStatus)
	e.Reason = clip(e.Reason)
	e.TaskTitle = clip(e.TaskTitle)
	e.TaskPath = clip(e.TaskPath)
	meta := map[string]string{}
	for _, key := range []string{"session_id", "instance_id", "reason", "permission_id"} {
		if v := e.Metadata[key]; v != "" {
			meta[key] = clip(v)
		}
	}
	if v := e.Metadata["resource"]; len(v) > 0 && len(v) < 4096 {
		meta["resource"] = supervision.Redact(v)
	}
	e.Metadata = meta
	return e
}

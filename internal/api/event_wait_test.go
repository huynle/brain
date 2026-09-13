package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestEventWaitRegistrationReplayAndReconnect(t *testing.T) {
	events := []types.Event{{ID: "old", Type: types.EventTaskCompleted, ProjectID: "p", TaskID: "t"}}
	subscribed, unsubscribed := false, false
	es := &mockEventService{
		subscribeFunc: func(context.Context, map[string]string) (<-chan types.Event, func()) {
			subscribed = true
			// Event arrives during registration, before the first ring read.
			events = append(events, types.Event{ID: "new", Type: types.EventTaskFailed, ProjectID: "p", TaskID: "t"})
			return make(chan types.Event), func() { unsubscribed = true }
		},
		recentFunc: func(context.Context, int, map[string]string) ([]types.Event, error) {
			if !subscribed {
				t.Fatal("read before subscription")
			}
			return events, nil
		},
	}
	h := &Handler{events: es}
	req := httptest.NewRequest("GET", "/events/wait?project_id=p&task_id=t&timeout_ms=0&limit=1", nil)
	w := httptest.NewRecorder()
	h.HandleEventWait(w, req)
	var p eventWaitPage
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err, w.Body)
	}
	if !unsubscribed || len(p.Events) != 1 || p.Events[0].ID != "old" || !p.Truncated {
		t.Fatal(p)
	}
	es.subscribeFunc = func(context.Context, map[string]string) (<-chan types.Event, func()) {
		return make(chan types.Event), func() {}
	}
	w = httptest.NewRecorder()
	h.HandleEventWait(w, httptest.NewRequest("GET", "/events/wait?project_id=p&task_id=t&timeout_ms=0&after="+url.QueryEscape(p.NextCursor), nil))
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if len(p.Events) != 1 || p.Events[0].ID != "new" {
		t.Fatal(p)
	}
	w = httptest.NewRecorder()
	h.HandleEventWait(w, httptest.NewRequest("GET", "/events/wait?project_id=other&task_id=t&timeout_ms=0&after="+url.QueryEscape(p.NextCursor), nil))
	if w.Code != 400 {
		t.Fatal("cross-filter cursor accepted", w.Code)
	}
}
func TestEventWaitTimeoutExpiryAndCancellation(t *testing.T) {
	es := &mockEventService{}
	h := &Handler{events: es}
	req := httptest.NewRequest("GET", "/events/wait?project_id=p&timeout_ms=0", nil)
	w := httptest.NewRecorder()
	h.HandleEventWait(w, req)
	var p eventWaitPage
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if !p.TimedOut || p.NextCursor == "" {
		t.Fatal(p)
	}
	scope := supervisorScope(req, `{"project_id":"p"}`)
	w = httptest.NewRecorder()
	h.HandleEventWait(w, httptest.NewRequest("GET", "/events/wait?project_id=p&after="+encodeEventCursor(scope, "expired"), nil))
	_ = json.Unmarshal(w.Body.Bytes(), &p)
	if !p.CursorExpired {
		t.Fatal(p)
	}
	released := false
	es.subscribeFunc = func(context.Context, map[string]string) (<-chan types.Event, func()) {
		return make(chan types.Event), func() { released = true }
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.HandleEventWait(httptest.NewRecorder(), req.WithContext(ctx))
	if !released {
		t.Fatal("leaked subscription")
	}
}

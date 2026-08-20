package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// PUT /runners/{runnerId}/pause used to be fire-and-forget: it published one
// SSE command and recorded nothing. Any runner that missed the event (SSE
// reconnect, headless start after the pause, restart) came back unpaused, and
// the scheduler — which has no view of SSE traffic — kept pushing dispatch
// leases at it. The pause has to be written to the runner registry so it
// survives, and the SSE command has to be scoped so the runner routes it to
// its own dial instead of the per-project ones.

var errPausePersistFailure = errors.New("persist failed")

type setPausedCall struct {
	runnerID string
	paused   bool
}

type pauseCallRecorder struct {
	mu    sync.Mutex
	calls []setPausedCall
	err   error
}

func (p *pauseCallRecorder) record(runnerID string, paused bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.err != nil {
		return p.err
	}
	p.calls = append(p.calls, setPausedCall{runnerID: runnerID, paused: paused})
	return nil
}

func (p *pauseCallRecorder) getCalls() []setPausedCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]setPausedCall(nil), p.calls...)
}

// newPauseRouter wires the pause/resume routes against a registry that
// records SetPaused, plus an optional real hub so the published SSE command
// can be observed on the runner's topic.
func newPauseRouter(rec *pauseCallRecorder, hub *realtime.Hub) *chi.Mux {
	mock := &mockRunnerRegistryService{
		getRunnerFunc: func(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
			return &types.RunnerInfo{RunnerID: runnerID}, nil
		},
		setPausedFunc: func(ctx context.Context, runnerID string, paused bool) error {
			return rec.record(runnerID, paused)
		},
	}
	opts := []HandlerOption{WithRunnerRegistryService(mock)}
	if hub != nil {
		opts = append(opts, WithHub(hub))
	}
	h := NewHandler(&mockBrainService{}, opts...)
	r := chi.NewRouter()
	r.Put("/runners/{runnerId}/pause", h.HandlePauseRunner)
	r.Put("/runners/{runnerId}/resume", h.HandleResumeRunner)
	return r
}

func nextRunnerCommand(t *testing.T, ch <-chan realtime.SSEMessage) map[string]interface{} {
	t.Helper()
	select {
	case msg := <-ch:
		data, ok := msg.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("SSE data = %#v, want map[string]interface{}", msg.Data)
		}
		return data
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a runner command on the SSE topic")
		return nil
	}
}

func TestHandlePauseRunner_PersistsPauseState(t *testing.T) {
	rec := &pauseCallRecorder{}
	router := newPauseRouter(rec, nil)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/runners/runner-1/pause", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\nBody: %s", w.Code, w.Body.String())
	}
	calls := rec.getCalls()
	if len(calls) != 1 || calls[0].runnerID != "runner-1" || !calls[0].paused {
		t.Fatalf("SetPaused calls = %+v, want one {runner-1 true}", calls)
	}
}

func TestHandleResumeRunner_PersistsResumeState(t *testing.T) {
	rec := &pauseCallRecorder{}
	router := newPauseRouter(rec, nil)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/runners/runner-1/resume", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\nBody: %s", w.Code, w.Body.String())
	}
	calls := rec.getCalls()
	if len(calls) != 1 || calls[0].runnerID != "runner-1" || calls[0].paused {
		t.Fatalf("SetPaused calls = %+v, want one {runner-1 false}", calls)
	}
}

// The persisted write is the source of truth. If it fails the caller must
// learn about it rather than getting {"success":true} for a pause that was
// never recorded — the exact false confidence that made this bug hard to spot
// from the PWA.
func TestHandlePauseRunner_ReportsPersistFailure(t *testing.T) {
	rec := &pauseCallRecorder{err: errPausePersistFailure}
	router := newPauseRouter(rec, nil)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/runners/runner-1/pause", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 when the pause could not be persisted\nBody: %s", w.Code, w.Body.String())
	}
}

// The SSE command must carry scope="runner" so the runner routes it to its
// own dial. Without a scope the runner treats it as a *global project* pause,
// which its per-project reconcile then wipes on the next poll tick.
func TestHandlePauseRunner_PublishesRunnerScopedCommand(t *testing.T) {
	rec := &pauseCallRecorder{}
	hub := realtime.NewHub()
	ch, unsub := hub.Subscribe(realtime.RunnerTopic("runner-1"))
	defer unsub()
	router := newPauseRouter(rec, hub)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/runners/runner-1/pause", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\nBody: %s", w.Code, w.Body.String())
	}

	data := nextRunnerCommand(t, ch)
	if data["command"] != "pause" {
		t.Fatalf("command = %v, want pause", data["command"])
	}
	payload, ok := data["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload = %#v, want a map carrying a scope", data["payload"])
	}
	if payload["scope"] != "runner" {
		t.Errorf("payload scope = %v, want \"runner\"", payload["scope"])
	}
}

func TestHandleResumeRunner_PublishesRunnerScopedCommand(t *testing.T) {
	rec := &pauseCallRecorder{}
	hub := realtime.NewHub()
	ch, unsub := hub.Subscribe(realtime.RunnerTopic("runner-1"))
	defer unsub()
	router := newPauseRouter(rec, hub)

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPut, "/runners/runner-1/resume", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200\nBody: %s", w.Code, w.Body.String())
	}

	data := nextRunnerCommand(t, ch)
	if data["command"] != "resume" {
		t.Fatalf("command = %v, want resume", data["command"])
	}
	payload, ok := data["payload"].(map[string]any)
	if !ok || payload["scope"] != "runner" {
		t.Errorf("payload = %#v, want scope=runner", data["payload"])
	}
}

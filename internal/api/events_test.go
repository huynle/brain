package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Mock EventService
// =============================================================================

type mockEventService struct {
	mu       sync.Mutex
	ingested []types.Event
	checks   []featureCompletionCheck

	ingestFunc    func(ctx context.Context, events []types.Event) error
	recentFunc    func(ctx context.Context, limit int, filters map[string]string) ([]types.Event, error)
	subscribeFunc func(ctx context.Context, filters map[string]string) (<-chan types.Event, func())
}

type featureCompletionCheck struct {
	ProjectID string
	FeatureID string
	TaskID    string
}

func (m *mockEventService) Ingest(ctx context.Context, events []types.Event) error {
	if m.ingestFunc != nil {
		return m.ingestFunc(ctx, events)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Auto-assign IDs for testing.
	for i := range events {
		if events[i].ID == "" {
			events[i].ID = fmt.Sprintf("evt_test_%d", len(m.ingested)+i)
		}
	}
	m.ingested = append(m.ingested, events...)
	return nil
}

func (m *mockEventService) Recent(ctx context.Context, limit int, filters map[string]string) ([]types.Event, error) {
	if m.recentFunc != nil {
		return m.recentFunc(ctx, limit, filters)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]types.Event, len(m.ingested))
	copy(result, m.ingested)
	if limit > 0 && len(result) > limit {
		result = result[len(result)-limit:]
	}
	return result, nil
}

func (m *mockEventService) Subscribe(ctx context.Context, filters map[string]string) (<-chan types.Event, func()) {
	if m.subscribeFunc != nil {
		return m.subscribeFunc(ctx, filters)
	}
	ch := make(chan types.Event, 64)
	return ch, func() { close(ch) }
}

func (m *mockEventService) CheckFeatureCompletion(ctx context.Context, projectID, featureID, taskID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checks = append(m.checks, featureCompletionCheck{ProjectID: projectID, FeatureID: featureID, TaskID: taskID})
}

// =============================================================================
// Helper: create event test router
// =============================================================================

func newEventTestRouter() (*chi.Mux, *mockEventService) {
	es := &mockEventService{}
	h := NewHandler(
		&mockBrainService{},
		WithEventService(es),
	)
	r := chi.NewRouter()
	r.Post("/events", h.HandleIngestEvents)
	r.Get("/events/stream", h.HandleEventStream)
	r.Get("/events/recent", h.HandleRecentEvents)
	return r, es
}

// =============================================================================
// POST /events
// =============================================================================

func TestHandleIngestEvents_Success(t *testing.T) {
	router, _ := newEventTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	events := []types.Event{
		{Type: types.EventTaskStarted, Source: types.EventSourceRunner, ProjectID: "proj-1"},
		{Type: types.EventTaskCompleted, Source: types.EventSourceRunner, ProjectID: "proj-1"},
	}
	body, _ := json.Marshal(events)

	resp, err := http.Post(srv.URL+"/events", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /events failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusAccepted)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if accepted, ok := result["accepted"].(float64); !ok || int(accepted) != 2 {
		t.Errorf("accepted = %v, want 2", result["accepted"])
	}
}

func TestHandleIngestEvents_InvalidJSON(t *testing.T) {
	router, _ := newEventTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/events", "application/json", strings.NewReader("not json"))
	if err != nil {
		t.Fatalf("POST /events failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandleIngestEvents_ValidationError(t *testing.T) {
	router, _ := newEventTestRouter()

	// Override ingest to simulate validation error.
	r2, es := newEventTestRouter()
	_ = r2
	es.ingestFunc = func(ctx context.Context, events []types.Event) error {
		return fmt.Errorf("invalid event type \"bad.type\" at index 0")
	}
	h := NewHandler(&mockBrainService{}, WithEventService(es))
	mux := chi.NewRouter()
	mux.Post("/events", h.HandleIngestEvents)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	events := []types.Event{{Type: "bad.type", Source: types.EventSourceRunner}}
	body, _ := json.Marshal(events)

	resp, err := http.Post(srv.URL+"/events", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /events failed: %v", err)
	}
	defer resp.Body.Close()

	_ = router
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandleIngestEvents_NilService(t *testing.T) {
	h := NewHandler(&mockBrainService{})
	r := chi.NewRouter()
	r.Post("/events", h.HandleIngestEvents)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/events", "application/json", strings.NewReader("[]"))
	if err != nil {
		t.Fatalf("POST /events failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotImplemented)
	}
}

// =============================================================================
// GET /events/recent
// =============================================================================

func TestHandleRecentEvents_Empty(t *testing.T) {
	router, _ := newEventTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events/recent")
	if err != nil {
		t.Fatalf("GET /events/recent failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	count, ok := result["count"].(float64)
	if !ok || int(count) != 0 {
		t.Errorf("count = %v, want 0", result["count"])
	}
}

func TestHandleRecentEvents_WithEvents(t *testing.T) {
	es := &mockEventService{
		recentFunc: func(ctx context.Context, limit int, filters map[string]string) ([]types.Event, error) {
			return []types.Event{
				{ID: "evt_1", Type: types.EventTaskStarted, Source: types.EventSourceRunner},
				{ID: "evt_2", Type: types.EventTaskCompleted, Source: types.EventSourceRunner},
				{ID: "evt_3", Type: types.EventTaskFailed, Source: types.EventSourceRunner},
			}, nil
		},
	}
	h := NewHandler(&mockBrainService{}, WithEventService(es))
	r := chi.NewRouter()
	r.Get("/events/recent", h.HandleRecentEvents)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events/recent")
	if err != nil {
		t.Fatalf("GET /events/recent failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	count, ok := result["count"].(float64)
	if !ok || int(count) != 3 {
		t.Errorf("count = %v, want 3", result["count"])
	}
}

func TestHandleRecentEvents_PassesFilters(t *testing.T) {
	var capturedLimit int
	var capturedFilters map[string]string

	es := &mockEventService{
		recentFunc: func(ctx context.Context, limit int, filters map[string]string) ([]types.Event, error) {
			capturedLimit = limit
			capturedFilters = filters
			return nil, nil
		},
	}
	h := NewHandler(&mockBrainService{}, WithEventService(es))
	r := chi.NewRouter()
	r.Get("/events/recent", h.HandleRecentEvents)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events/recent?limit=50&type=task.*&project_id=proj-1&feature_id=auth")
	if err != nil {
		t.Fatalf("GET /events/recent failed: %v", err)
	}
	defer resp.Body.Close()

	if capturedLimit != 50 {
		t.Errorf("limit = %d, want 50", capturedLimit)
	}
	if capturedFilters["type"] != "task.*" {
		t.Errorf("type filter = %q, want %q", capturedFilters["type"], "task.*")
	}
	if capturedFilters["project_id"] != "proj-1" {
		t.Errorf("project_id filter = %q, want %q", capturedFilters["project_id"], "proj-1")
	}
	if capturedFilters["feature_id"] != "auth" {
		t.Errorf("feature_id filter = %q, want %q", capturedFilters["feature_id"], "auth")
	}
}

func TestHandleRecentEvents_LimitClamped(t *testing.T) {
	var capturedLimit int

	es := &mockEventService{
		recentFunc: func(ctx context.Context, limit int, filters map[string]string) ([]types.Event, error) {
			capturedLimit = limit
			return nil, nil
		},
	}
	h := NewHandler(&mockBrainService{}, WithEventService(es))
	r := chi.NewRouter()
	r.Get("/events/recent", h.HandleRecentEvents)
	srv := httptest.NewServer(r)
	defer srv.Close()

	// Limit above 1000 should be clamped.
	resp, err := http.Get(srv.URL + "/events/recent?limit=5000")
	if err != nil {
		t.Fatalf("GET /events/recent failed: %v", err)
	}
	defer resp.Body.Close()

	if capturedLimit != 1000 {
		t.Errorf("limit = %d, want 1000 (clamped)", capturedLimit)
	}
}

func TestHandleRecentEvents_InvalidLimit(t *testing.T) {
	router, _ := newEventTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events/recent?limit=abc")
	if err != nil {
		t.Fatalf("GET /events/recent failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandleRecentEvents_NegativeLimit(t *testing.T) {
	router, _ := newEventTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events/recent?limit=-1")
	if err != nil {
		t.Fatalf("GET /events/recent failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestHandleRecentEvents_NilService(t *testing.T) {
	h := NewHandler(&mockBrainService{})
	r := chi.NewRouter()
	r.Get("/events/recent", h.HandleRecentEvents)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events/recent")
	if err != nil {
		t.Fatalf("GET /events/recent failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotImplemented)
	}
}

func TestHandleRecentEvents_ServiceError(t *testing.T) {
	es := &mockEventService{
		recentFunc: func(ctx context.Context, limit int, filters map[string]string) ([]types.Event, error) {
			return nil, fmt.Errorf("database error")
		},
	}
	h := NewHandler(&mockBrainService{}, WithEventService(es))
	r := chi.NewRouter()
	r.Get("/events/recent", h.HandleRecentEvents)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events/recent")
	if err != nil {
		t.Fatalf("GET /events/recent failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}

// =============================================================================
// GET /events/stream (SSE)
// =============================================================================

func TestHandleEventStream_SSEHeaders(t *testing.T) {
	router, _ := newEventTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/events/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events/stream failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream prefix", ct)
	}

	if resp.Header.Get("Cache-Control") != "no-cache, no-transform" {
		t.Errorf("Cache-Control = %q, want %q", resp.Header.Get("Cache-Control"), "no-cache, no-transform")
	}

	if resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Errorf("X-Accel-Buffering = %q, want %q", resp.Header.Get("X-Accel-Buffering"), "no")
	}
}

func TestHandleEventStream_ReceivesEvents(t *testing.T) {
	eventCh := make(chan types.Event, 64)
	es := &mockEventService{
		subscribeFunc: func(ctx context.Context, filters map[string]string) (<-chan types.Event, func()) {
			return eventCh, func() {}
		},
	}
	h := NewHandler(&mockBrainService{}, WithEventService(es))
	r := chi.NewRouter()
	r.Get("/events/stream", h.HandleEventStream)
	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/events/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events/stream failed: %v", err)
	}
	defer resp.Body.Close()

	// Send an event through the channel.
	go func() {
		time.Sleep(100 * time.Millisecond)
		eventCh <- types.Event{
			ID:     "evt_test_1",
			Type:   types.EventTaskStarted,
			Source: types.EventSourceRunner,
		}
	}()

	events := parseEventSSE(t, resp, 1, 2*time.Second)
	if len(events) < 1 {
		t.Fatal("expected at least 1 SSE event")
	}

	if events[0].Event != types.EventTaskStarted {
		t.Errorf("event type = %q, want %q", events[0].Event, types.EventTaskStarted)
	}

	if events[0].ID != "evt_test_1" {
		t.Errorf("event id = %q, want %q", events[0].ID, "evt_test_1")
	}
}

func TestHandleEventStream_PassesFilters(t *testing.T) {
	// Subscribe runs on the server's goroutine and the assertions on this
	// one, so the capture needs a lock of its own (-race flags the bare
	// variable).
	var captureMu sync.Mutex
	var capturedFilters map[string]string
	filter := func(key string) string {
		captureMu.Lock()
		defer captureMu.Unlock()
		return capturedFilters[key]
	}

	es := &mockEventService{
		subscribeFunc: func(ctx context.Context, filters map[string]string) (<-chan types.Event, func()) {
			captureMu.Lock()
			capturedFilters = filters
			captureMu.Unlock()
			ch := make(chan types.Event, 1)
			return ch, func() { close(ch) }
		},
	}
	h := NewHandler(&mockBrainService{}, WithEventService(es))
	r := chi.NewRouter()
	r.Get("/events/stream", h.HandleEventStream)
	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/events/stream?type=task.*&project_id=proj-1&feature_id=auth", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events/stream failed: %v", err)
	}
	defer resp.Body.Close()

	// Wait for connection to be established.
	time.Sleep(100 * time.Millisecond)

	if got := filter("type"); got != "task.*" {
		t.Errorf("type filter = %q, want %q", got, "task.*")
	}
	if got := filter("project_id"); got != "proj-1" {
		t.Errorf("project_id filter = %q, want %q", got, "proj-1")
	}
	if got := filter("feature_id"); got != "auth" {
		t.Errorf("feature_id filter = %q, want %q", got, "auth")
	}
}

func TestHandleEventStream_NilService(t *testing.T) {
	h := NewHandler(&mockBrainService{})
	r := chi.NewRouter()
	r.Get("/events/stream", h.HandleEventStream)
	srv := httptest.NewServer(r)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/events/stream")
	if err != nil {
		t.Fatalf("GET /events/stream failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotImplemented)
	}
}

func TestHandleEventStream_Heartbeat(t *testing.T) {
	// Use a very short heartbeat interval for testing.
	origInterval := DefaultHeartbeatInterval
	DefaultHeartbeatInterval = 100 * time.Millisecond
	defer func() { DefaultHeartbeatInterval = origInterval }()

	router, _ := newEventTestRouter()
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/events/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events/stream failed: %v", err)
	}
	defer resp.Body.Close()

	// Read lines looking for heartbeat comment.
	scanner := bufio.NewScanner(resp.Body)
	found := false
	deadline := time.After(1 * time.Second)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, ": heartbeat") {
				found = true
				return
			}
		}
	}()

	select {
	case <-done:
	case <-deadline:
	}

	if !found {
		t.Error("expected heartbeat comment in SSE stream")
	}
}

func TestHandleEventStream_LastEventIDReplay(t *testing.T) {
	es := &mockEventService{
		recentFunc: func(ctx context.Context, limit int, filters map[string]string) ([]types.Event, error) {
			return []types.Event{
				{ID: "evt_1", Type: types.EventTaskStarted, Source: types.EventSourceRunner, ProjectID: "proj-1"},
				{ID: "evt_2", Type: types.EventTaskCompleted, Source: types.EventSourceRunner, ProjectID: "proj-1"},
				{ID: "evt_3", Type: types.EventTaskFailed, Source: types.EventSourceRunner, ProjectID: "proj-1"},
			}, nil
		},
		subscribeFunc: func(ctx context.Context, filters map[string]string) (<-chan types.Event, func()) {
			ch := make(chan types.Event, 1)
			return ch, func() { close(ch) }
		},
	}
	h := NewHandler(&mockBrainService{}, WithEventService(es))
	r := chi.NewRouter()
	r.Get("/events/stream", h.HandleEventStream)
	srv := httptest.NewServer(r)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/events/stream", nil)
	req.Header.Set("Last-Event-ID", "evt_1")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /events/stream failed: %v", err)
	}
	defer resp.Body.Close()

	// Should replay evt_2 and evt_3 (events after evt_1).
	events := parseEventSSE(t, resp, 2, 2*time.Second)
	if len(events) < 2 {
		t.Fatalf("expected 2 replayed events, got %d", len(events))
	}

	if events[0].ID != "evt_2" {
		t.Errorf("first replayed event ID = %q, want %q", events[0].ID, "evt_2")
	}
	if events[1].ID != "evt_3" {
		t.Errorf("second replayed event ID = %q, want %q", events[1].ID, "evt_3")
	}
}

// =============================================================================
// Router integration: verify routes registered
// =============================================================================

func TestEventsRoutes_Registered(t *testing.T) {
	es := &mockEventService{}
	h := NewHandler(
		&mockBrainService{},
		WithEventService(es),
	)

	cfg := testConfig()
	router := NewRouter(cfg, WithHandler(h))
	srv := httptest.NewServer(router)
	defer srv.Close()

	// POST /api/v1/events should not be 404.
	events := []types.Event{
		{Type: types.EventTaskStarted, Source: types.EventSourceRunner},
	}
	body, _ := json.Marshal(events)
	resp, err := http.Post(srv.URL+"/api/v1/events", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /api/v1/events failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		t.Error("POST /api/v1/events should be registered (got 404)")
	}

	// GET /api/v1/events/recent should be accessible.
	resp2, err := http.Get(srv.URL + "/api/v1/events/recent")
	if err != nil {
		t.Fatalf("GET /api/v1/events/recent failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode == http.StatusNotFound {
		t.Error("GET /api/v1/events/recent should be registered (got 404)")
	}
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("GET /api/v1/events/recent status = %d, want %d", resp2.StatusCode, http.StatusOK)
	}
}

func TestEventsRoutes_NotImplementedWithoutHandler(t *testing.T) {
	cfg := testConfig()
	router := NewRouter(cfg)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/events/recent")
	if err != nil {
		t.Fatalf("GET /api/v1/events/recent failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusNotImplemented)
	}
}

// =============================================================================
// Helpers
// =============================================================================

// eventSSE represents a parsed SSE event from the event stream.
type eventSSE struct {
	ID    string
	Event string
	Data  string
}

// parseEventSSE reads SSE events from a response body until count is reached or timeout.
func parseEventSSE(t *testing.T, resp *http.Response, count int, timeout time.Duration) []eventSSE {
	t.Helper()
	var events []eventSSE
	scanner := bufio.NewScanner(resp.Body)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		var current eventSSE
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "id: ") {
				current.ID = strings.TrimPrefix(line, "id: ")
			} else if strings.HasPrefix(line, "event: ") {
				current.Event = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				current.Data = strings.TrimPrefix(line, "data: ")
			} else if line == "" && current.Event != "" {
				events = append(events, current)
				current = eventSSE{}
				if len(events) >= count {
					return
				}
			}
		}
	}()

	select {
	case <-done:
	case <-ctx.Done():
	}

	return events
}

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Mock RunnerRegistryService
// =============================================================================

type mockRunnerRegistryService struct {
	registerFunc       func(ctx context.Context, req types.RunnerRegistration) (*types.RunnerInfo, error)
	heartbeatFunc      func(ctx context.Context, runnerID string, req types.RunnerHeartbeatRequest) error
	deregisterFunc     func(ctx context.Context, runnerID string) error
	listRunnersFunc    func(ctx context.Context) (*types.RunnerListResponse, error)
	getRunnerFunc      func(ctx context.Context, runnerID string) (*types.RunnerInfo, error)
	updateConfigFunc   func(ctx context.Context, runnerID string, maxParallel int) error
	updateAffinityFunc func(ctx context.Context, runnerID string, featureIDs []string) error
}

func (m *mockRunnerRegistryService) Register(ctx context.Context, req types.RunnerRegistration) (*types.RunnerInfo, error) {
	if m.registerFunc != nil {
		return m.registerFunc(ctx, req)
	}
	return &types.RunnerInfo{
		RunnerID:    req.RunnerID,
		Hostname:    req.Hostname,
		MaxParallel: req.MaxParallel,
		Status:      types.RunnerStatusOnline,
	}, nil
}

func (m *mockRunnerRegistryService) Heartbeat(ctx context.Context, runnerID string, req types.RunnerHeartbeatRequest) error {
	if m.heartbeatFunc != nil {
		return m.heartbeatFunc(ctx, runnerID, req)
	}
	return nil
}

func (m *mockRunnerRegistryService) Deregister(ctx context.Context, runnerID string) error {
	if m.deregisterFunc != nil {
		return m.deregisterFunc(ctx, runnerID)
	}
	return nil
}

func (m *mockRunnerRegistryService) ListRunners(ctx context.Context) (*types.RunnerListResponse, error) {
	if m.listRunnersFunc != nil {
		return m.listRunnersFunc(ctx)
	}
	return &types.RunnerListResponse{Runners: []types.RunnerInfo{}, Total: 0}, nil
}

func (m *mockRunnerRegistryService) GetRunner(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
	if m.getRunnerFunc != nil {
		return m.getRunnerFunc(ctx, runnerID)
	}
	return nil, ErrNotFound
}

func (m *mockRunnerRegistryService) UpdateConfig(ctx context.Context, runnerID string, maxParallel int) error {
	if m.updateConfigFunc != nil {
		return m.updateConfigFunc(ctx, runnerID, maxParallel)
	}
	return nil
}

func (m *mockRunnerRegistryService) UpdateAffinity(ctx context.Context, runnerID string, featureIDs []string) error {
	if m.updateAffinityFunc != nil {
		return m.updateAffinityFunc(ctx, runnerID, featureIDs)
	}
	return nil
}

// =============================================================================
// Test Helpers
// =============================================================================

func newRunnerTestRouter(mock *mockRunnerRegistryService) *chi.Mux {
	h := NewHandler(&mockBrainService{}, WithRunnerRegistryService(mock))
	r := chi.NewRouter()
	r.Route("/runners", func(r chi.Router) {
		r.Post("/register", h.HandleRegisterRunner)
		r.Get("/", h.HandleListRunners)
		r.Get("/{runnerId}", h.HandleGetRunner)
		r.Post("/{runnerId}/heartbeat", h.HandleHeartbeat)
		r.Post("/{runnerId}/deregister", h.HandleDeregisterRunner)
		r.Put("/{runnerId}/pause", h.HandlePauseRunner)
		r.Put("/{runnerId}/resume", h.HandleResumeRunner)
		r.Patch("/{runnerId}/config", h.HandleUpdateRunnerConfig)
		r.Post("/{runnerId}/features/{featureId}/toggle", h.HandleToggleRunnerFeature)
	})
	return r
}

// =============================================================================
// Tests: POST /runners/register
// =============================================================================

func TestHandleRegisterRunner(t *testing.T) {
	tests := []struct {
		name           string
		body           string
		mockRegister   func(ctx context.Context, req types.RunnerRegistration) (*types.RunnerInfo, error)
		wantStatus     int
		wantRegistered bool
		wantErrField   string // if non-empty, expect a validation error with this field
	}{
		{
			name:           "success",
			body:           `{"runner_id":"runner-1","hostname":"host-1","max_parallel":4,"executors":["opencode"]}`,
			wantStatus:     http.StatusOK,
			wantRegistered: true,
		},
		{
			name:         "missing runner_id",
			body:         `{"hostname":"host-1"}`,
			wantStatus:   http.StatusBadRequest,
			wantErrField: "runner_id",
		},
		{
			name:         "missing hostname",
			body:         `{"runner_id":"runner-1"}`,
			wantStatus:   http.StatusBadRequest,
			wantErrField: "hostname",
		},
		{
			name:       "invalid JSON",
			body:       `{invalid`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "service error",
			body: `{"runner_id":"runner-1","hostname":"host-1"}`,
			mockRegister: func(ctx context.Context, req types.RunnerRegistration) (*types.RunnerInfo, error) {
				return nil, ErrConflict
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockRunnerRegistryService{registerFunc: tt.mockRegister}
			router := newRunnerTestRouter(mock)

			req := httptest.NewRequest(http.MethodPost, "/runners/register", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantRegistered {
				var resp map[string]any
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if resp["registered"] != true {
					t.Errorf("registered = %v, want true", resp["registered"])
				}
				hbInterval, ok := resp["heartbeat_interval"].(float64)
				if !ok || hbInterval != 30 {
					t.Errorf("heartbeat_interval = %v, want 30", resp["heartbeat_interval"])
				}
				leaseInterval, ok := resp["lease_renewal_interval"].(float64)
				if !ok || leaseInterval != 300 {
					t.Errorf("lease_renewal_interval = %v, want 300", resp["lease_renewal_interval"])
				}
			}

			if tt.wantErrField != "" {
				var resp types.ErrorResponse
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("failed to decode error response: %v", err)
				}
				if len(resp.Details) == 0 {
					t.Fatal("expected validation details, got none")
				}
				if resp.Details[0].Field != tt.wantErrField {
					t.Errorf("error field = %q, want %q", resp.Details[0].Field, tt.wantErrField)
				}
			}
		})
	}
}

// =============================================================================
// Tests: POST /runners/{runnerId}/heartbeat
// =============================================================================

func TestHandleHeartbeat(t *testing.T) {
	tests := []struct {
		name          string
		runnerID      string
		body          string
		mockHeartbeat func(ctx context.Context, runnerID string, req types.RunnerHeartbeatRequest) error
		wantStatus    int
		wantAck       bool
	}{
		{
			name:       "success",
			runnerID:   "runner-1",
			body:       `{"running_tasks":3}`,
			wantStatus: http.StatusOK,
			wantAck:    true,
		},
		{
			name:       "empty body is valid (defaults)",
			runnerID:   "runner-1",
			body:       `{}`,
			wantStatus: http.StatusOK,
			wantAck:    true,
		},
		{
			name:       "invalid JSON",
			runnerID:   "runner-1",
			body:       `{bad`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:     "runner not found",
			runnerID: "nonexistent",
			body:     `{"running_tasks":0}`,
			mockHeartbeat: func(ctx context.Context, runnerID string, req types.RunnerHeartbeatRequest) error {
				return ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockRunnerRegistryService{heartbeatFunc: tt.mockHeartbeat}
			router := newRunnerTestRouter(mock)

			req := httptest.NewRequest(http.MethodPost, "/runners/"+tt.runnerID+"/heartbeat", strings.NewReader(tt.body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantAck {
				var resp map[string]any
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if resp["ack"] != true {
					t.Errorf("ack = %v, want true", resp["ack"])
				}
			}
		})
	}
}

// =============================================================================
// Tests: POST /runners/{runnerId}/deregister
// =============================================================================

func TestHandleDeregisterRunner(t *testing.T) {
	tests := []struct {
		name           string
		runnerID       string
		mockDeregister func(ctx context.Context, runnerID string) error
		wantStatus     int
		wantSuccess    bool
	}{
		{
			name:        "success",
			runnerID:    "runner-1",
			wantStatus:  http.StatusOK,
			wantSuccess: true,
		},
		{
			name:     "runner not found",
			runnerID: "nonexistent",
			mockDeregister: func(ctx context.Context, runnerID string) error {
				return ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockRunnerRegistryService{deregisterFunc: tt.mockDeregister}
			router := newRunnerTestRouter(mock)

			req := httptest.NewRequest(http.MethodPost, "/runners/"+tt.runnerID+"/deregister", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantSuccess {
				var resp map[string]any
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if resp["success"] != true {
					t.Errorf("success = %v, want true", resp["success"])
				}
			}
		})
	}
}

// =============================================================================
// Tests: GET /runners
// =============================================================================

func TestHandleListRunners(t *testing.T) {
	tests := []struct {
		name       string
		mockList   func(ctx context.Context) (*types.RunnerListResponse, error)
		wantStatus int
		wantTotal  int
	}{
		{
			name:       "empty list",
			wantStatus: http.StatusOK,
			wantTotal:  0,
		},
		{
			name: "with runners",
			mockList: func(ctx context.Context) (*types.RunnerListResponse, error) {
				return &types.RunnerListResponse{
					Runners: []types.RunnerInfo{
						{RunnerID: "runner-1", Hostname: "host-1", Status: types.RunnerStatusOnline},
						{RunnerID: "runner-2", Hostname: "host-2", Status: types.RunnerStatusStale},
					},
					Total: 2,
				}, nil
			},
			wantStatus: http.StatusOK,
			wantTotal:  2,
		},
		{
			name: "service error",
			mockList: func(ctx context.Context) (*types.RunnerListResponse, error) {
				return nil, ErrConflict
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockRunnerRegistryService{listRunnersFunc: tt.mockList}
			router := newRunnerTestRouter(mock)

			req := httptest.NewRequest(http.MethodGet, "/runners", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("status = %d, want %d; body = %s", w.Code, tt.wantStatus, w.Body.String())
			}

			if tt.wantStatus == http.StatusOK {
				var resp types.RunnerListResponse
				if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
					t.Fatalf("failed to decode response: %v", err)
				}
				if resp.Total != tt.wantTotal {
					t.Errorf("total = %d, want %d", resp.Total, tt.wantTotal)
				}
				if len(resp.Runners) != tt.wantTotal {
					t.Errorf("len(runners) = %d, want %d", len(resp.Runners), tt.wantTotal)
				}
			}
		})
	}
}

// =============================================================================
// Tests: GET /runners/{runnerId}
// =============================================================================

func TestHandleGetRunner_ReturnsRunner(t *testing.T) {
	mock := &mockRunnerRegistryService{
		getRunnerFunc: func(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
			if runnerID == "runner-abc" {
				return &types.RunnerInfo{
					RunnerID:      "runner-abc",
					Hostname:      "host-1",
					Status:        types.RunnerStatusOnline,
					MaxParallel:   4,
					LastHeartbeat: time.Now().UTC().Format(time.RFC3339),
				}, nil
			}
			return nil, ErrNotFound
		},
	}
	router := newRunnerTestRouter(mock)

	req := httptest.NewRequest(http.MethodGet, "/runners/runner-abc", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusOK, w.Body.String())
	}

	var runner types.RunnerInfo
	if err := json.NewDecoder(w.Body).Decode(&runner); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if runner.RunnerID != "runner-abc" {
		t.Errorf("RunnerID = %q, want %q", runner.RunnerID, "runner-abc")
	}
	if runner.Hostname != "host-1" {
		t.Errorf("Hostname = %q, want %q", runner.Hostname, "host-1")
	}
	if runner.Status != types.RunnerStatusOnline {
		t.Errorf("Status = %q, want %q", runner.Status, types.RunnerStatusOnline)
	}
	if runner.MaxParallel != 4 {
		t.Errorf("MaxParallel = %d, want %d", runner.MaxParallel, 4)
	}
}

func TestHandleGetRunner_NotFound(t *testing.T) {
	mock := &mockRunnerRegistryService{
		getRunnerFunc: func(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
			return nil, ErrNotFound
		},
	}
	router := newRunnerTestRouter(mock)

	req := httptest.NewRequest(http.MethodGet, "/runners/nonexistent", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body = %s", w.Code, http.StatusNotFound, w.Body.String())
	}

	body := w.Body.String()
	if !strings.Contains(body, "not found") {
		t.Errorf("body should contain 'not found', got: %s", body)
	}
}

// =============================================================================
// Runner Stream Tests
// =============================================================================

func newRunnerStreamTestRouter(registryMock *mockRunnerRegistryService, hub *realtime.Hub) *chi.Mux {
	h := NewHandler(
		&mockBrainService{},
		WithRunnerRegistryService(registryMock),
		WithHub(hub),
	)
	r := chi.NewRouter()
	r.Route("/runners", func(r chi.Router) {
		r.Get("/{runnerId}/stream", h.HandleRunnerStream)
	})
	return r
}

func TestRunnerStreamConnectedEvent(t *testing.T) {
	hub := realtime.NewHub()
	registryMock := &mockRunnerRegistryService{
		getRunnerFunc: func(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
			return &types.RunnerInfo{RunnerID: runnerID, Status: types.RunnerStatusOnline}, nil
		},
	}
	router := newRunnerStreamTestRouter(registryMock, hub)
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/runners/runner-1/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Check SSE headers
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if resp.Header.Get("Cache-Control") != "no-cache, no-transform" {
		t.Errorf("Cache-Control = %q, want %q", resp.Header.Get("Cache-Control"), "no-cache, no-transform")
	}
	if resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Errorf("X-Accel-Buffering = %q, want %q", resp.Header.Get("X-Accel-Buffering"), "no")
	}

	// Read first event: connected
	events := parseSSEEvents(t, resp, 1, 2*time.Second)
	if len(events) < 1 {
		t.Fatal("expected at least 1 SSE event")
	}

	if events[0].Event != "connected" {
		t.Errorf("event[0] = %q, want %q", events[0].Event, "connected")
	}

	// Parse connected data — should have runnerId, not projectId
	var connData types.RunnerSSEConnectedData
	if err := json.Unmarshal([]byte(events[0].Data), &connData); err != nil {
		t.Fatalf("failed to parse connected data: %v", err)
	}
	if connData.RunnerID != "runner-1" {
		t.Errorf("runnerId = %q, want %q", connData.RunnerID, "runner-1")
	}
	if connData.Transport != "sse" {
		t.Errorf("transport = %q, want %q", connData.Transport, "sse")
	}
}

func TestRunnerStreamNotFound(t *testing.T) {
	hub := realtime.NewHub()
	registryMock := &mockRunnerRegistryService{
		getRunnerFunc: func(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
			return nil, ErrNotFound
		},
	}
	router := newRunnerStreamTestRouter(registryMock, hub)

	req := httptest.NewRequest(http.MethodGet, "/runners/nonexistent/stream", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d; body = %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestRunnerStreamHeartbeat(t *testing.T) {
	// Use a very short heartbeat interval for testing
	origInterval := DefaultHeartbeatInterval
	DefaultHeartbeatInterval = 100 * time.Millisecond
	defer func() { DefaultHeartbeatInterval = origInterval }()

	hub := realtime.NewHub()
	registryMock := &mockRunnerRegistryService{
		getRunnerFunc: func(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
			return &types.RunnerInfo{RunnerID: runnerID, Status: types.RunnerStatusOnline}, nil
		},
	}
	router := newRunnerStreamTestRouter(registryMock, hub)
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/runners/runner-1/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read connected + heartbeat = 2 events
	events := parseSSEEvents(t, resp, 2, 2*time.Second)
	if len(events) < 2 {
		t.Fatalf("expected at least 2 SSE events, got %d", len(events))
	}

	// Second event should be heartbeat
	if events[1].Event != "heartbeat" {
		t.Errorf("event[1] = %q, want %q", events[1].Event, "heartbeat")
	}

	// Heartbeat should have runnerId
	var hbData types.RunnerSSEEventData
	if err := json.Unmarshal([]byte(events[1].Data), &hbData); err != nil {
		t.Fatalf("failed to parse heartbeat data: %v", err)
	}
	if hbData.RunnerID != "runner-1" {
		t.Errorf("runnerId = %q, want %q", hbData.RunnerID, "runner-1")
	}
}

func TestRunnerStreamReceivesCommand(t *testing.T) {
	hub := realtime.NewHub()
	registryMock := &mockRunnerRegistryService{
		getRunnerFunc: func(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
			return &types.RunnerInfo{RunnerID: runnerID, Status: types.RunnerStatusOnline}, nil
		},
	}
	router := newRunnerStreamTestRouter(registryMock, hub)
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/runners/runner-1/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read connected event first
	events := parseSSEEvents(t, resp, 1, 2*time.Second)
	if len(events) < 1 {
		t.Fatal("expected at least 1 initial event")
	}

	// Publish a command through the hub
	hub.PublishRunnerCommand("runner-1", "shutdown", map[string]string{"reason": "maintenance"})

	// Read the command event
	events = parseSSEEvents(t, resp, 1, 2*time.Second)
	if len(events) < 1 {
		t.Fatal("expected to receive command event")
	}

	if events[0].Event != "command" {
		t.Errorf("event = %q, want %q", events[0].Event, "command")
	}

	// Parse command data
	var cmdData map[string]interface{}
	if err := json.Unmarshal([]byte(events[0].Data), &cmdData); err != nil {
		t.Fatalf("failed to parse command data: %v", err)
	}
	if cmdData["command"] != "shutdown" {
		t.Errorf("command = %v, want %q", cmdData["command"], "shutdown")
	}
}

func TestRunnerStreamReceivesTasksChanged(t *testing.T) {
	hub := realtime.NewHub()
	registryMock := &mockRunnerRegistryService{
		getRunnerFunc: func(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
			return &types.RunnerInfo{RunnerID: runnerID, Status: types.RunnerStatusOnline}, nil
		},
	}
	router := newRunnerStreamTestRouter(registryMock, hub)
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/runners/runner-1/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Read connected event first
	_ = parseSSEEvents(t, resp, 1, 2*time.Second)

	// Publish tasks_changed through the hub
	hub.PublishRunnerTasksChanged("runner-1", map[string]string{"project": "test-project"})

	// Read the tasks_changed event
	events := parseSSEEvents(t, resp, 1, 2*time.Second)
	if len(events) < 1 {
		t.Fatal("expected to receive tasks_changed event")
	}

	if events[0].Event != "tasks_changed" {
		t.Errorf("event = %q, want %q", events[0].Event, "tasks_changed")
	}
}

func TestRunnerStreamClientDisconnect(t *testing.T) {
	hub := realtime.NewHub()
	registryMock := &mockRunnerRegistryService{
		getRunnerFunc: func(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
			return &types.RunnerInfo{RunnerID: runnerID, Status: types.RunnerStatusOnline}, nil
		},
	}
	router := newRunnerStreamTestRouter(registryMock, hub)
	srv := httptest.NewServer(router)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/runners/runner-1/stream", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	// Read initial events
	_ = parseSSEEvents(t, resp, 1, 2*time.Second)

	// Cancel context to simulate client disconnect
	cancel()
	resp.Body.Close()

	// Publishing after disconnect should not panic
	hub.PublishRunnerCommand("runner-1", "shutdown", nil)

	// Give time for cleanup
	time.Sleep(50 * time.Millisecond)

	// If we get here without panic, the test passes
}

// =============================================================================
// Tests: PATCH /runners/{runnerId}/config
// =============================================================================

func TestHandleUpdateRunnerConfig(t *testing.T) {
	tests := []struct {
		name             string
		runnerID         string
		body             string
		mockUpdateConfig func(ctx context.Context, runnerID string, maxParallel int) error
		mockGetRunner    func(ctx context.Context, runnerID string) (*types.RunnerInfo, error)
		wantStatus       int
		wantSuccess      bool
	}{
		{
			name:     "success",
			runnerID: "runner-1",
			body:     `{"maxParallel":8}`,
			mockUpdateConfig: func(ctx context.Context, runnerID string, maxParallel int) error {
				return nil
			},
			mockGetRunner: func(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
				return &types.RunnerInfo{RunnerID: runnerID, MaxParallel: 8}, nil
			},
			wantStatus:  http.StatusOK,
			wantSuccess: true,
		},
		{
			name:       "invalid json",
			runnerID:   "runner-1",
			body:       `{invalid}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid maxParallel",
			runnerID:   "runner-1",
			body:       `{"maxParallel":0}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockRunnerRegistryService{
				updateConfigFunc: tt.mockUpdateConfig,
				getRunnerFunc:    tt.mockGetRunner,
			}
			router := newRunnerTestRouter(mock)

			req := httptest.NewRequest(http.MethodPatch, "/runners/"+tt.runnerID+"/config", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d\nBody: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}

			if tt.wantSuccess {
				var resp map[string]interface{}
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if success, ok := resp["success"].(bool); !ok || !success {
					t.Errorf("expected success=true, got %v", resp["success"])
				}
			}
		})
	}
}

// =============================================================================
// Tests: POST /runners/{runnerId}/features/{featureId}/toggle
// =============================================================================

func TestHandleToggleRunnerFeature(t *testing.T) {
	tests := []struct {
		name          string
		runnerID      string
		featureID     string
		body          string
		mockGetRunner func(ctx context.Context, runnerID string) (*types.RunnerInfo, error)
		wantStatus    int
		wantSuccess   bool
	}{
		{
			name:      "enable feature",
			runnerID:  "runner-1",
			featureID: "feature-1",
			body:      `{"enabled":true}`,
			mockGetRunner: func(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
				return &types.RunnerInfo{RunnerID: runnerID}, nil
			},
			wantStatus:  http.StatusOK,
			wantSuccess: true,
		},
		{
			name:      "disable feature",
			runnerID:  "runner-1",
			featureID: "feature-1",
			body:      `{"enabled":false}`,
			mockGetRunner: func(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
				return &types.RunnerInfo{RunnerID: runnerID}, nil
			},
			wantStatus:  http.StatusOK,
			wantSuccess: true,
		},
		{
			name:       "invalid json",
			runnerID:   "runner-1",
			featureID:  "feature-1",
			body:       `{invalid}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:      "runner not found",
			runnerID:  "runner-1",
			featureID: "feature-1",
			body:      `{"enabled":true}`,
			mockGetRunner: func(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
				return nil, ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockRunnerRegistryService{
				getRunnerFunc: tt.mockGetRunner,
			}
			router := newRunnerTestRouter(mock)

			req := httptest.NewRequest(http.MethodPost, "/runners/"+tt.runnerID+"/features/"+tt.featureID+"/toggle", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d\nBody: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}

			if tt.wantSuccess {
				var resp map[string]interface{}
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if success, ok := resp["success"].(bool); !ok || !success {
					t.Errorf("expected success=true, got %v", resp["success"])
				}
			}
		})
	}
}

// =============================================================================
// Tests: PUT /runners/{runnerId}/pause
// =============================================================================

func TestHandlePauseRunner_SendsSSECommand(t *testing.T) {
	tests := []struct {
		name          string
		runnerID      string
		mockGetRunner func(ctx context.Context, runnerID string) (*types.RunnerInfo, error)
		wantStatus    int
		wantAction    string
	}{
		{
			name:     "pause existing runner",
			runnerID: "runner-1",
			mockGetRunner: func(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
				return &types.RunnerInfo{RunnerID: runnerID}, nil
			},
			wantStatus: http.StatusOK,
			wantAction: "pause",
		},
		{
			name:     "runner not found",
			runnerID: "nonexistent",
			mockGetRunner: func(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
				return nil, ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockRunnerRegistryService{
				getRunnerFunc: tt.mockGetRunner,
			}
			router := newRunnerTestRouter(mock)

			req := httptest.NewRequest(http.MethodPut, "/runners/"+tt.runnerID+"/pause", nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d\nBody: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}

			if tt.wantAction != "" {
				var resp map[string]interface{}
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if resp["runnerId"] != tt.runnerID {
					t.Errorf("runnerId = %v, want %v", resp["runnerId"], tt.runnerID)
				}
				if resp["action"] != tt.wantAction {
					t.Errorf("action = %v, want %v", resp["action"], tt.wantAction)
				}
				if success, ok := resp["success"].(bool); !ok || !success {
					t.Errorf("expected success=true, got %v", resp["success"])
				}
			}
		})
	}
}

// =============================================================================
// Tests: PUT /runners/{runnerId}/resume
// =============================================================================

func TestHandleResumeRunner_SendsSSECommand(t *testing.T) {
	tests := []struct {
		name          string
		runnerID      string
		mockGetRunner func(ctx context.Context, runnerID string) (*types.RunnerInfo, error)
		wantStatus    int
		wantAction    string
	}{
		{
			name:     "resume existing runner",
			runnerID: "runner-1",
			mockGetRunner: func(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
				return &types.RunnerInfo{RunnerID: runnerID}, nil
			},
			wantStatus: http.StatusOK,
			wantAction: "resume",
		},
		{
			name:     "runner not found",
			runnerID: "nonexistent",
			mockGetRunner: func(ctx context.Context, runnerID string) (*types.RunnerInfo, error) {
				return nil, ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockRunnerRegistryService{
				getRunnerFunc: tt.mockGetRunner,
			}
			router := newRunnerTestRouter(mock)

			req := httptest.NewRequest(http.MethodPut, "/runners/"+tt.runnerID+"/resume", nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("got status %d, want %d\nBody: %s", rec.Code, tt.wantStatus, rec.Body.String())
			}

			if tt.wantAction != "" {
				var resp map[string]interface{}
				if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}
				if resp["runnerId"] != tt.runnerID {
					t.Errorf("runnerId = %v, want %v", resp["runnerId"], tt.runnerID)
				}
				if resp["action"] != tt.wantAction {
					t.Errorf("action = %v, want %v", resp["action"], tt.wantAction)
				}
				if success, ok := resp["success"].(bool); !ok || !success {
					t.Errorf("expected success=true, got %v", resp["success"])
				}
			}
		})
	}
}

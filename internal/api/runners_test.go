package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Mock RunnerRegistryService
// =============================================================================

type mockRunnerRegistryService struct {
	registerFunc    func(ctx context.Context, req types.RunnerRegistration) (*types.RunnerInfo, error)
	heartbeatFunc   func(ctx context.Context, runnerID string, req types.RunnerHeartbeatRequest) error
	deregisterFunc  func(ctx context.Context, runnerID string) error
	listRunnersFunc func(ctx context.Context) (*types.RunnerListResponse, error)
	getRunnerFunc   func(ctx context.Context, runnerID string) (*types.RunnerInfo, error)
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

// =============================================================================
// Test Helpers
// =============================================================================

func newRunnerTestRouter(mock *mockRunnerRegistryService) *chi.Mux {
	h := NewHandler(&mockBrainService{}, WithRunnerRegistryService(mock))
	r := chi.NewRouter()
	r.Route("/runners", func(r chi.Router) {
		r.Post("/register", h.HandleRegisterRunner)
		r.Get("/", h.HandleListRunners)
		r.Post("/{runnerId}/heartbeat", h.HandleHeartbeat)
		r.Post("/{runnerId}/deregister", h.HandleDeregisterRunner)
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

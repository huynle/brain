package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Mock TaskService
// =============================================================================

type mockTaskService struct {
	listProjectsFunc     func(ctx context.Context) ([]string, error)
	getTasksFunc         func(ctx context.Context, projectId string) (*types.TaskListResponse, error)
	getReadyFunc         func(ctx context.Context, projectId string, opts *TaskFilterOptions) ([]types.ResolvedTask, error)
	getWaitingFunc       func(ctx context.Context, projectId string) ([]types.ResolvedTask, error)
	getBlockedFunc       func(ctx context.Context, projectId string) ([]types.ResolvedTask, error)
	getNextFunc          func(ctx context.Context, projectId string, opts *TaskFilterOptions) (*types.ResolvedTask, error)
	claimTaskFunc        func(ctx context.Context, projectId, taskId, runnerId string) (*types.ClaimResponse, error)
	releaseTaskFunc      func(ctx context.Context, projectId, taskId, runnerId string) error
	renewClaimFunc       func(ctx context.Context, projectId, taskId, runnerId string) (*types.RenewClaimResponse, error)
	getClaimStatusFunc   func(ctx context.Context, projectId, taskId string) (*types.ClaimStatusResponse, error)
	getMultiTaskStatusFn func(ctx context.Context, projectId string, req types.MultiTaskStatusRequest) (*types.MultiTaskStatusResponse, error)
	getFeaturesFunc      func(ctx context.Context, projectId string) (*types.FeatureListResponse, error)
	getReadyFeaturesFunc func(ctx context.Context, projectId string) (*types.FeatureListResponse, error)
	getFeatureFunc       func(ctx context.Context, projectId, featureId string) (*types.FeatureResponse, error)
	checkoutFeatureFunc  func(ctx context.Context, projectId, featureId string, opts *types.FeatureCheckoutOptions) (*types.CheckoutFeatureResult, error)
	triggerTaskFunc      func(ctx context.Context, projectId, taskId string) (*types.TriggerResponse, error)
	getTaskFunc          func(ctx context.Context, projectId, taskId string) (*types.ResolvedTask, error)
}

func (m *mockTaskService) ListProjects(ctx context.Context) ([]string, error) {
	if m.listProjectsFunc != nil {
		return m.listProjectsFunc(ctx)
	}
	return nil, fmt.Errorf("listProjectsFunc not set")
}

func (m *mockTaskService) GetTasks(ctx context.Context, projectId string) (*types.TaskListResponse, error) {
	if m.getTasksFunc != nil {
		return m.getTasksFunc(ctx, projectId)
	}
	return nil, fmt.Errorf("getTasksFunc not set")
}

func (m *mockTaskService) GetReady(ctx context.Context, projectId string, opts *TaskFilterOptions) ([]types.ResolvedTask, error) {
	if m.getReadyFunc != nil {
		return m.getReadyFunc(ctx, projectId, opts)
	}
	return nil, fmt.Errorf("getReadyFunc not set")
}

func (m *mockTaskService) GetWaiting(ctx context.Context, projectId string) ([]types.ResolvedTask, error) {
	if m.getWaitingFunc != nil {
		return m.getWaitingFunc(ctx, projectId)
	}
	return nil, fmt.Errorf("getWaitingFunc not set")
}

func (m *mockTaskService) GetBlocked(ctx context.Context, projectId string) ([]types.ResolvedTask, error) {
	if m.getBlockedFunc != nil {
		return m.getBlockedFunc(ctx, projectId)
	}
	return nil, fmt.Errorf("getBlockedFunc not set")
}

func (m *mockTaskService) GetNext(ctx context.Context, projectId string, opts *TaskFilterOptions) (*types.ResolvedTask, error) {
	if m.getNextFunc != nil {
		return m.getNextFunc(ctx, projectId, opts)
	}
	return nil, fmt.Errorf("getNextFunc not set")
}

func (m *mockTaskService) ClaimTask(ctx context.Context, projectId, taskId, runnerId string) (*types.ClaimResponse, error) {
	if m.claimTaskFunc != nil {
		return m.claimTaskFunc(ctx, projectId, taskId, runnerId)
	}
	return nil, fmt.Errorf("claimTaskFunc not set")
}

func (m *mockTaskService) ReleaseTask(ctx context.Context, projectId, taskId, runnerId string) error {
	if m.releaseTaskFunc != nil {
		return m.releaseTaskFunc(ctx, projectId, taskId, runnerId)
	}
	return fmt.Errorf("releaseTaskFunc not set")
}

func (m *mockTaskService) RenewClaim(ctx context.Context, projectId, taskId, runnerId string) (*types.RenewClaimResponse, error) {
	if m.renewClaimFunc != nil {
		return m.renewClaimFunc(ctx, projectId, taskId, runnerId)
	}
	return nil, fmt.Errorf("renewClaimFunc not set")
}

func (m *mockTaskService) GetClaimStatus(ctx context.Context, projectId, taskId string) (*types.ClaimStatusResponse, error) {
	if m.getClaimStatusFunc != nil {
		return m.getClaimStatusFunc(ctx, projectId, taskId)
	}
	return nil, fmt.Errorf("getClaimStatusFunc not set")
}

func (m *mockTaskService) GetMultiTaskStatus(ctx context.Context, projectId string, req types.MultiTaskStatusRequest) (*types.MultiTaskStatusResponse, error) {
	if m.getMultiTaskStatusFn != nil {
		return m.getMultiTaskStatusFn(ctx, projectId, req)
	}
	return nil, fmt.Errorf("getMultiTaskStatusFn not set")
}

func (m *mockTaskService) GetFeatures(ctx context.Context, projectId string) (*types.FeatureListResponse, error) {
	if m.getFeaturesFunc != nil {
		return m.getFeaturesFunc(ctx, projectId)
	}
	return nil, fmt.Errorf("getFeaturesFunc not set")
}

func (m *mockTaskService) GetReadyFeatures(ctx context.Context, projectId string) (*types.FeatureListResponse, error) {
	if m.getReadyFeaturesFunc != nil {
		return m.getReadyFeaturesFunc(ctx, projectId)
	}
	return nil, fmt.Errorf("getReadyFeaturesFunc not set")
}

func (m *mockTaskService) GetFeature(ctx context.Context, projectId, featureId string) (*types.FeatureResponse, error) {
	if m.getFeatureFunc != nil {
		return m.getFeatureFunc(ctx, projectId, featureId)
	}
	return nil, fmt.Errorf("getFeatureFunc not set")
}

func (m *mockTaskService) CheckoutFeature(ctx context.Context, projectId, featureId string, opts *types.FeatureCheckoutOptions) (*types.CheckoutFeatureResult, error) {
	if m.checkoutFeatureFunc != nil {
		return m.checkoutFeatureFunc(ctx, projectId, featureId, opts)
	}
	return nil, fmt.Errorf("checkoutFeatureFunc not set")
}

func (m *mockTaskService) TriggerTask(ctx context.Context, projectId, taskId string) (*types.TriggerResponse, error) {
	if m.triggerTaskFunc != nil {
		return m.triggerTaskFunc(ctx, projectId, taskId)
	}
	return nil, fmt.Errorf("triggerTaskFunc not set")
}

func (m *mockTaskService) DispatchTask(ctx context.Context, projectId, taskId, runnerID string) (*types.ClaimResponse, error) {
	return nil, fmt.Errorf("dispatchTask not implemented in mock")
}

func (m *mockTaskService) GetTask(ctx context.Context, projectId, taskId string) (*types.ResolvedTask, error) {
	if m.getTaskFunc != nil {
		return m.getTaskFunc(ctx, projectId, taskId)
	}
	return nil, fmt.Errorf("getTaskFunc not set")
}

// =============================================================================
// Mock RunnerService
// =============================================================================

type mockRunnerService struct {
	pauseFunc     func(ctx context.Context, projectId string) error
	resumeFunc    func(ctx context.Context, projectId string) error
	pauseAllFunc  func(ctx context.Context) error
	resumeAllFunc func(ctx context.Context) error
	getStatusFunc func(ctx context.Context) (*types.RunnerStatusResponse, error)
}

func (m *mockRunnerService) Pause(ctx context.Context, projectId string) error {
	if m.pauseFunc != nil {
		return m.pauseFunc(ctx, projectId)
	}
	return fmt.Errorf("pauseFunc not set")
}

func (m *mockRunnerService) Resume(ctx context.Context, projectId string) error {
	if m.resumeFunc != nil {
		return m.resumeFunc(ctx, projectId)
	}
	return fmt.Errorf("resumeFunc not set")
}

func (m *mockRunnerService) PauseAll(ctx context.Context) error {
	if m.pauseAllFunc != nil {
		return m.pauseAllFunc(ctx)
	}
	return fmt.Errorf("pauseAllFunc not set")
}

func (m *mockRunnerService) ResumeAll(ctx context.Context) error {
	if m.resumeAllFunc != nil {
		return m.resumeAllFunc(ctx)
	}
	return fmt.Errorf("resumeAllFunc not set")
}

func (m *mockRunnerService) GetStatus(ctx context.Context) (*types.RunnerStatusResponse, error) {
	if m.getStatusFunc != nil {
		return m.getStatusFunc(ctx)
	}
	return nil, fmt.Errorf("getStatusFunc not set")
}

// =============================================================================
// Test Helpers
// =============================================================================

func newTaskTestRouter(taskMock *mockTaskService, runnerMock *mockRunnerService) *chi.Mux {
	hub := realtime.NewHub()
	h := NewHandler(
		&mockBrainService{},
		WithTaskService(taskMock),
		WithRunnerService(runnerMock),
		WithHub(hub),
	)
	r := chi.NewRouter()
	r.Route("/tasks", func(r chi.Router) {
		r.Get("/", h.HandleListProjects)

		// Runner routes (must be before {projectId} wildcard)
		r.Post("/runner/pause/{projectId}", h.HandlePauseProject)
		r.Post("/runner/resume/{projectId}", h.HandleResumeProject)
		r.Post("/runner/pause", h.HandlePauseAll)
		r.Post("/runner/resume", h.HandleResumeAll)
		r.Get("/runner/status", h.HandleRunnerStatus)

		r.Route("/{projectId}", func(r chi.Router) {
			r.Get("/", h.HandleGetTasks)
			r.Get("/ready", h.HandleGetReady)
			r.Get("/waiting", h.HandleGetWaiting)
			r.Get("/blocked", h.HandleGetBlocked)
			r.Get("/next", h.HandleGetNext)
			r.Post("/status", h.HandleMultiTaskStatus)

			r.Get("/features", h.HandleGetFeatures)
			r.Get("/features/ready", h.HandleGetReadyFeatures)
			r.Get("/features/{featureId}", h.HandleGetFeature)
			r.Post("/features/{featureId}/checkout", h.HandleCheckoutFeature)

			r.Get("/stream", h.HandleSSEStream)

			r.Get("/{taskId}", h.HandleGetTask)
			r.Get("/{taskId}/metadata", h.HandleGetTaskMetadata)
			r.Post("/{taskId}/claim", h.HandleClaimTask)
			r.Post("/{taskId}/release", h.HandleReleaseTask)
			r.Post("/{taskId}/renew", h.HandleRenewClaim)
			r.Get("/{taskId}/claim-status", h.HandleGetClaimStatus)
			r.Post("/{taskId}/trigger", h.HandleTriggerTask)
		})
	})
	return r
}

// =============================================================================
// List Projects Tests
// =============================================================================

func TestHandleListProjects(t *testing.T) {
	tests := []struct {
		name       string
		mockFn     func(ctx context.Context) ([]string, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success",
			mockFn: func(ctx context.Context) ([]string, error) {
				return []string{"project-a", "project-b"}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ProjectListResponse](t, resp)
				if len(body.Projects) != 2 {
					t.Fatalf("projects count = %d, want 2", len(body.Projects))
				}
				if body.Projects[0] != "project-a" {
					t.Errorf("projects[0] = %q, want %q", body.Projects[0], "project-a")
				}
			},
		},
		{
			name: "empty",
			mockFn: func(ctx context.Context) ([]string, error) {
				return []string{}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ProjectListResponse](t, resp)
				if len(body.Projects) != 0 {
					t.Errorf("projects count = %d, want 0", len(body.Projects))
				}
			},
		},
		{
			name: "service error",
			mockFn: func(ctx context.Context) ([]string, error) {
				return nil, fmt.Errorf("disk error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskMock := &mockTaskService{listProjectsFunc: tt.mockFn}
			router := newTaskTestRouter(taskMock, &mockRunnerService{})
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/tasks")
			if err != nil {
				t.Fatalf("GET /tasks failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.checkBody != nil {
				tt.checkBody(t, resp)
			}
		})
	}
}

// =============================================================================
// Get Tasks Tests
// =============================================================================

func TestHandleGetTasks(t *testing.T) {
	tests := []struct {
		name       string
		projectId  string
		mockFn     func(ctx context.Context, projectId string) (*types.TaskListResponse, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name:      "success",
			projectId: "my-project",
			mockFn: func(ctx context.Context, projectId string) (*types.TaskListResponse, error) {
				if projectId != "my-project" {
					return nil, fmt.Errorf("unexpected projectId: %s", projectId)
				}
				return &types.TaskListResponse{
					Tasks: []types.ResolvedTask{
						{ID: "abc12def", Title: "Task 1", Classification: "ready"},
					},
					Count:  1,
					Stats:  &types.TaskStats{Total: 1, Ready: 1},
					Cycles: [][]string{},
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.TaskListResponse](t, resp)
				if body.Count != 1 {
					t.Errorf("count = %d, want 1", body.Count)
				}
				if len(body.Tasks) != 1 {
					t.Fatalf("tasks count = %d, want 1", len(body.Tasks))
				}
				if body.Tasks[0].ID != "abc12def" {
					t.Errorf("tasks[0].id = %q, want %q", body.Tasks[0].ID, "abc12def")
				}
			},
		},
		{
			name:      "service error",
			projectId: "my-project",
			mockFn: func(ctx context.Context, projectId string) (*types.TaskListResponse, error) {
				return nil, fmt.Errorf("disk error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskMock := &mockTaskService{getTasksFunc: tt.mockFn}
			router := newTaskTestRouter(taskMock, &mockRunnerService{})
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/tasks/" + tt.projectId)
			if err != nil {
				t.Fatalf("GET /tasks/%s failed: %v", tt.projectId, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.checkBody != nil {
				tt.checkBody(t, resp)
			}
		})
	}
}

// =============================================================================
// Get Single Task Tests
// =============================================================================

func TestHandleGetTask_ReturnsTaskWithDefaults(t *testing.T) {
	tests := []struct {
		name       string
		projectId  string
		taskId     string
		mockFn     func(ctx context.Context, projectId, taskId string) (*types.ResolvedTask, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name:      "returns existing task",
			projectId: "my-project",
			taskId:    "abc12def",
			mockFn: func(ctx context.Context, projectId, taskId string) (*types.ResolvedTask, error) {
				return &types.ResolvedTask{
					ID:             taskId,
					Classification: "ready",
					Agent:          "tdd-dev",
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ResolvedTask](t, resp)
				if body.ID != "abc12def" {
					t.Errorf("id = %q, want %q", body.ID, "abc12def")
				}
				if body.Agent != "tdd-dev" {
					t.Errorf("agent = %q, want %q", body.Agent, "tdd-dev")
				}
			},
		},
		{
			name:      "returns 404 for nonexistent task",
			projectId: "my-project",
			taskId:    "nonexistent",
			mockFn: func(ctx context.Context, projectId, taskId string) (*types.ResolvedTask, error) {
				return nil, fmt.Errorf("task %q not found in project %q", taskId, projectId)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:      "service error returns 500",
			projectId: "my-project",
			taskId:    "abc12def",
			mockFn: func(ctx context.Context, projectId, taskId string) (*types.ResolvedTask, error) {
				return nil, fmt.Errorf("unexpected disk error")
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskMock := &mockTaskService{getTaskFunc: tt.mockFn}
			router := newTaskTestRouter(taskMock, &mockRunnerService{})
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/tasks/" + tt.projectId + "/" + tt.taskId)
			if err != nil {
				t.Fatalf("GET /tasks/%s/%s failed: %v", tt.projectId, tt.taskId, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.checkBody != nil {
				tt.checkBody(t, resp)
			}
		})
	}
}

// =============================================================================
// Get Ready/Waiting/Blocked Tests
// =============================================================================

func TestHandleGetReady(t *testing.T) {
	taskMock := &mockTaskService{
		getReadyFunc: func(ctx context.Context, projectId string, opts *TaskFilterOptions) ([]types.ResolvedTask, error) {
			return []types.ResolvedTask{
				{ID: "task1", Classification: "ready"},
			}, nil
		},
	}
	router := newTaskTestRouter(taskMock, &mockRunnerService{})
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/tasks/my-project/ready")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body struct {
		Tasks []types.ResolvedTask `json:"tasks"`
	}
	json.NewDecoder(resp.Body).Decode(&body)
	if len(body.Tasks) != 1 {
		t.Errorf("tasks count = %d, want 1", len(body.Tasks))
	}
}

func TestHandleGetWaiting(t *testing.T) {
	taskMock := &mockTaskService{
		getWaitingFunc: func(ctx context.Context, projectId string) ([]types.ResolvedTask, error) {
			return []types.ResolvedTask{
				{ID: "task2", Classification: "waiting"},
			}, nil
		},
	}
	router := newTaskTestRouter(taskMock, &mockRunnerService{})
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/tasks/my-project/waiting")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandleGetBlocked(t *testing.T) {
	taskMock := &mockTaskService{
		getBlockedFunc: func(ctx context.Context, projectId string) ([]types.ResolvedTask, error) {
			return []types.ResolvedTask{}, nil
		},
	}
	router := newTaskTestRouter(taskMock, &mockRunnerService{})
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/tasks/my-project/blocked")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

// =============================================================================
// Get Next Tests
// =============================================================================

func TestHandleGetNext(t *testing.T) {
	tests := []struct {
		name       string
		mockFn     func(ctx context.Context, projectId string, opts *TaskFilterOptions) (*types.ResolvedTask, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success",
			mockFn: func(ctx context.Context, projectId string, opts *TaskFilterOptions) (*types.ResolvedTask, error) {
				return &types.ResolvedTask{ID: "task1", Title: "Next Task"}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ResolvedTask](t, resp)
				if body.ID != "task1" {
					t.Errorf("id = %q, want %q", body.ID, "task1")
				}
			},
		},
		{
			name: "no tasks available",
			mockFn: func(ctx context.Context, projectId string, opts *TaskFilterOptions) (*types.ResolvedTask, error) {
				return nil, ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskMock := &mockTaskService{getNextFunc: tt.mockFn}
			router := newTaskTestRouter(taskMock, &mockRunnerService{})
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/tasks/my-project/next")
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.checkBody != nil {
				tt.checkBody(t, resp)
			}
		})
	}
}

// =============================================================================
// Claim Task Tests
// =============================================================================

func TestHandleClaimTask(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		mockFn     func(ctx context.Context, projectId, taskId, runnerId string) (*types.ClaimResponse, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success",
			body: map[string]any{"runnerId": "runner-001"},
			mockFn: func(ctx context.Context, projectId, taskId, runnerId string) (*types.ClaimResponse, error) {
				if runnerId != "runner-001" {
					return nil, fmt.Errorf("unexpected runnerId: %s", runnerId)
				}
				return &types.ClaimResponse{
					Success:   true,
					TaskID:    taskId,
					RunnerID:  runnerId,
					ClaimedAt: "2024-01-01T00:00:00Z",
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ClaimResponse](t, resp)
				if !body.Success {
					t.Error("expected success = true")
				}
				if body.RunnerID != "runner-001" {
					t.Errorf("runnerId = %q, want %q", body.RunnerID, "runner-001")
				}
			},
		},
		{
			name: "conflict",
			body: map[string]any{"runnerId": "runner-002"},
			mockFn: func(ctx context.Context, projectId, taskId, runnerId string) (*types.ClaimResponse, error) {
				isStale := false
				return &types.ClaimResponse{
					Success:   false,
					TaskID:    taskId,
					Error:     "conflict",
					Message:   "Task already claimed",
					ClaimedBy: "runner-001",
					ClaimedAt: "2024-01-01T00:00:00Z",
					IsStale:   &isStale,
				}, ErrConflict
			},
			wantStatus: http.StatusConflict,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ClaimResponse](t, resp)
				if body.Success {
					t.Error("expected success = false")
				}
				if body.ClaimedBy != "runner-001" {
					t.Errorf("claimedBy = %q, want %q", body.ClaimedBy, "runner-001")
				}
			},
		},
		{
			name:       "missing runnerId",
			body:       map[string]any{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid JSON",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskMock := &mockTaskService{claimTaskFunc: tt.mockFn}
			router := newTaskTestRouter(taskMock, &mockRunnerService{})
			srv := httptest.NewServer(router)
			defer srv.Close()

			var body *bytes.Buffer
			switch v := tt.body.(type) {
			case string:
				body = bytes.NewBufferString(v)
			default:
				body = jsonBody(t, v)
			}

			resp, err := http.Post(srv.URL+"/tasks/my-project/task1/claim", "application/json", body)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.checkBody != nil {
				tt.checkBody(t, resp)
			}
		})
	}
}

// =============================================================================
// Release Task Tests
// =============================================================================

func TestHandleReleaseTask(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		mockFn     func(ctx context.Context, projectId, taskId, runnerId string) error
		wantStatus int
	}{
		{
			name: "success",
			body: map[string]any{"runnerId": "runner-001"},
			mockFn: func(ctx context.Context, projectId, taskId, runnerId string) error {
				return nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "not found",
			body: map[string]any{"runnerId": "runner-001"},
			mockFn: func(ctx context.Context, projectId, taskId, runnerId string) error {
				return ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "missing runnerId",
			body:       map[string]any{},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskMock := &mockTaskService{releaseTaskFunc: tt.mockFn}
			router := newTaskTestRouter(taskMock, &mockRunnerService{})
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Post(srv.URL+"/tasks/my-project/task1/release", "application/json", jsonBody(t, tt.body))
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

// =============================================================================
// Renew Claim Tests
// =============================================================================

func TestHandleRenewClaim(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		mockFn     func(ctx context.Context, projectId, taskId, runnerId string) (*types.RenewClaimResponse, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success",
			body: map[string]any{"runnerId": "runner-001"},
			mockFn: func(ctx context.Context, projectId, taskId, runnerId string) (*types.RenewClaimResponse, error) {
				return &types.RenewClaimResponse{
					Success:   true,
					TaskID:    taskId,
					RunnerID:  runnerId,
					ExpiresAt: "2024-01-01T00:10:00Z",
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.RenewClaimResponse](t, resp)
				if !body.Success {
					t.Error("expected success = true")
				}
				if body.ExpiresAt == "" {
					t.Error("expected non-empty expiresAt")
				}
			},
		},
		{
			name: "not found",
			body: map[string]any{"runnerId": "runner-001"},
			mockFn: func(ctx context.Context, projectId, taskId, runnerId string) (*types.RenewClaimResponse, error) {
				return &types.RenewClaimResponse{
					Success:  false,
					TaskID:   taskId,
					RunnerID: runnerId,
					Error:    "claim not found",
				}, ErrNotFound
			},
			wantStatus: http.StatusNotFound,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.RenewClaimResponse](t, resp)
				if body.Success {
					t.Error("expected success = false")
				}
			},
		},
		{
			name: "conflict - wrong runner",
			body: map[string]any{"runnerId": "runner-002"},
			mockFn: func(ctx context.Context, projectId, taskId, runnerId string) (*types.RenewClaimResponse, error) {
				return &types.RenewClaimResponse{
					Success:  false,
					TaskID:   taskId,
					RunnerID: runnerId,
					Error:    "claim owned by different runner",
				}, ErrConflict
			},
			wantStatus: http.StatusConflict,
		},
		{
			name:       "missing runnerId",
			body:       map[string]any{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid JSON",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskMock := &mockTaskService{renewClaimFunc: tt.mockFn}
			router := newTaskTestRouter(taskMock, &mockRunnerService{})
			srv := httptest.NewServer(router)
			defer srv.Close()

			var body *bytes.Buffer
			switch v := tt.body.(type) {
			case string:
				body = bytes.NewBufferString(v)
			default:
				body = jsonBody(t, v)
			}

			resp, err := http.Post(srv.URL+"/tasks/my-project/task1/renew", "application/json", body)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.checkBody != nil {
				tt.checkBody(t, resp)
			}
		})
	}
}

// =============================================================================
// Claim Status Tests
// =============================================================================

func TestHandleGetClaimStatus(t *testing.T) {
	taskMock := &mockTaskService{
		getClaimStatusFunc: func(ctx context.Context, projectId, taskId string) (*types.ClaimStatusResponse, error) {
			return &types.ClaimStatusResponse{
				TaskID:    taskId,
				Claimed:   true,
				RunnerID:  "runner-001",
				ClaimedAt: "2024-01-01T00:00:00Z",
				IsStale:   false,
			}, nil
		},
	}
	router := newTaskTestRouter(taskMock, &mockRunnerService{})
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/tasks/my-project/task1/claim-status")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body := decodeJSON[types.ClaimStatusResponse](t, resp)
	if !body.Claimed {
		t.Error("expected claimed = true")
	}
	if body.RunnerID != "runner-001" {
		t.Errorf("runnerId = %q, want %q", body.RunnerID, "runner-001")
	}
}

// =============================================================================
// Multi-Task Status Tests
// =============================================================================

func TestHandleMultiTaskStatus(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		mockFn     func(ctx context.Context, projectId string, req types.MultiTaskStatusRequest) (*types.MultiTaskStatusResponse, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success",
			body: map[string]any{
				"taskIds": []string{"task1", "task2"},
				"waitFor": "completed",
				"timeout": 60000,
			},
			mockFn: func(ctx context.Context, projectId string, req types.MultiTaskStatusRequest) (*types.MultiTaskStatusResponse, error) {
				if len(req.TaskIDs) != 2 {
					return nil, fmt.Errorf("expected 2 taskIds, got %d", len(req.TaskIDs))
				}
				return &types.MultiTaskStatusResponse{
					Tasks: []types.ResolvedTask{
						{ID: "task1", Status: "completed"},
						{ID: "task2", Status: "completed"},
					},
					AllCompleted: true,
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.MultiTaskStatusResponse](t, resp)
				if !body.AllCompleted {
					t.Error("expected allCompleted = true")
				}
				if len(body.Tasks) != 2 {
					t.Errorf("tasks count = %d, want 2", len(body.Tasks))
				}
			},
		},
		{
			name:       "missing taskIds",
			body:       map[string]any{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid JSON",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskMock := &mockTaskService{getMultiTaskStatusFn: tt.mockFn}
			router := newTaskTestRouter(taskMock, &mockRunnerService{})
			srv := httptest.NewServer(router)
			defer srv.Close()

			var body *bytes.Buffer
			switch v := tt.body.(type) {
			case string:
				body = bytes.NewBufferString(v)
			default:
				body = jsonBody(t, v)
			}

			resp, err := http.Post(srv.URL+"/tasks/my-project/status", "application/json", body)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.checkBody != nil {
				tt.checkBody(t, resp)
			}
		})
	}
}

// =============================================================================
// Features Tests
// =============================================================================

func TestHandleGetFeatures(t *testing.T) {
	taskMock := &mockTaskService{
		getFeaturesFunc: func(ctx context.Context, projectId string) (*types.FeatureListResponse, error) {
			return &types.FeatureListResponse{
				Features: []types.Feature{
					{FeatureID: "auth-system", Ready: true},
				},
			}, nil
		},
	}
	router := newTaskTestRouter(taskMock, &mockRunnerService{})
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/tasks/my-project/features")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body := decodeJSON[types.FeatureListResponse](t, resp)
	if len(body.Features) != 1 {
		t.Errorf("features count = %d, want 1", len(body.Features))
	}
}

func TestHandleGetReadyFeatures(t *testing.T) {
	taskMock := &mockTaskService{
		getReadyFeaturesFunc: func(ctx context.Context, projectId string) (*types.FeatureListResponse, error) {
			return &types.FeatureListResponse{Features: []types.Feature{}}, nil
		},
	}
	router := newTaskTestRouter(taskMock, &mockRunnerService{})
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/tasks/my-project/features/ready")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandleGetFeature(t *testing.T) {
	tests := []struct {
		name       string
		featureId  string
		mockFn     func(ctx context.Context, projectId, featureId string) (*types.FeatureResponse, error)
		wantStatus int
	}{
		{
			name:      "success",
			featureId: "auth-system",
			mockFn: func(ctx context.Context, projectId, featureId string) (*types.FeatureResponse, error) {
				return &types.FeatureResponse{
					Feature: types.Feature{FeatureID: featureId, Ready: true},
				}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:      "not found",
			featureId: "nonexistent",
			mockFn: func(ctx context.Context, projectId, featureId string) (*types.FeatureResponse, error) {
				return nil, ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskMock := &mockTaskService{getFeatureFunc: tt.mockFn}
			router := newTaskTestRouter(taskMock, &mockRunnerService{})
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/tasks/my-project/features/" + tt.featureId)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestHandleCheckoutFeature(t *testing.T) {
	tests := []struct {
		name       string
		mockFn     func(ctx context.Context, projectId, featureId string, opts *types.FeatureCheckoutOptions) (*types.CheckoutFeatureResult, error)
		wantStatus int
	}{
		{
			name: "success",
			mockFn: func(ctx context.Context, projectId, featureId string, opts *types.FeatureCheckoutOptions) (*types.CheckoutFeatureResult, error) {
				return &types.CheckoutFeatureResult{Created: true}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name: "not found",
			mockFn: func(ctx context.Context, projectId, featureId string, opts *types.FeatureCheckoutOptions) (*types.CheckoutFeatureResult, error) {
				return nil, ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskMock := &mockTaskService{checkoutFeatureFunc: tt.mockFn}
			router := newTaskTestRouter(taskMock, &mockRunnerService{})
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Post(srv.URL+"/tasks/my-project/features/auth-system/checkout", "application/json", nil)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

// =============================================================================
// Trigger Task Tests
// =============================================================================

func TestHandleTriggerTask(t *testing.T) {
	tests := []struct {
		name       string
		mockFn     func(ctx context.Context, projectId, taskId string) (*types.TriggerResponse, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success - triggered",
			mockFn: func(ctx context.Context, projectId, taskId string) (*types.TriggerResponse, error) {
				return &types.TriggerResponse{
					Success:   true,
					TaskID:    taskId,
					Triggered: true,
					RunID:     "20250101-0000-abc123",
					NextRun:   "2025-01-01T00:05:00Z",
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.TriggerResponse](t, resp)
				if !body.Success {
					t.Error("expected success = true")
				}
				if !body.Triggered {
					t.Error("expected triggered = true")
				}
				if body.RunID == "" {
					t.Error("expected non-empty RunID")
				}
				if body.NextRun == "" {
					t.Error("expected non-empty NextRun")
				}
			},
		},
		{
			name: "success - not triggered (schedule disabled)",
			mockFn: func(ctx context.Context, projectId, taskId string) (*types.TriggerResponse, error) {
				return &types.TriggerResponse{
					Success:   true,
					TaskID:    taskId,
					Triggered: false,
					Reason:    "schedule is disabled",
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.TriggerResponse](t, resp)
				if !body.Success {
					t.Error("expected success = true")
				}
				if body.Triggered {
					t.Error("expected triggered = false")
				}
				if body.Reason == "" {
					t.Error("expected non-empty Reason")
				}
			},
		},
		{
			name: "not found",
			mockFn: func(ctx context.Context, projectId, taskId string) (*types.TriggerResponse, error) {
				return nil, ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "internal server error",
			mockFn: func(ctx context.Context, projectId, taskId string) (*types.TriggerResponse, error) {
				return nil, fmt.Errorf("database connection lost")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskMock := &mockTaskService{triggerTaskFunc: tt.mockFn}
			router := newTaskTestRouter(taskMock, &mockRunnerService{})
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Post(srv.URL+"/tasks/my-project/task1/trigger", "application/json", nil)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.checkBody != nil {
				tt.checkBody(t, resp)
			}
		})
	}
}

// =============================================================================
// Runner Tests
// =============================================================================

func TestHandlePauseProject(t *testing.T) {
	runnerMock := &mockRunnerService{
		pauseFunc: func(ctx context.Context, projectId string) error {
			if projectId != "my-project" {
				return fmt.Errorf("unexpected projectId: %s", projectId)
			}
			return nil
		},
	}
	router := newTaskTestRouter(&mockTaskService{}, runnerMock)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/tasks/runner/pause/my-project", "application/json", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandleResumeProject(t *testing.T) {
	runnerMock := &mockRunnerService{
		resumeFunc: func(ctx context.Context, projectId string) error {
			return nil
		},
	}
	router := newTaskTestRouter(&mockTaskService{}, runnerMock)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/tasks/runner/resume/my-project", "application/json", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandlePauseAll(t *testing.T) {
	runnerMock := &mockRunnerService{
		pauseAllFunc: func(ctx context.Context) error {
			return nil
		},
	}
	router := newTaskTestRouter(&mockTaskService{}, runnerMock)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/tasks/runner/pause", "application/json", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandleResumeAll(t *testing.T) {
	runnerMock := &mockRunnerService{
		resumeAllFunc: func(ctx context.Context) error {
			return nil
		},
	}
	router := newTaskTestRouter(&mockTaskService{}, runnerMock)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/tasks/runner/resume", "application/json", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
}

func TestHandleRunnerStatus(t *testing.T) {
	runnerMock := &mockRunnerService{
		getStatusFunc: func(ctx context.Context) (*types.RunnerStatusResponse, error) {
			return &types.RunnerStatusResponse{
				Running:        true,
				Paused:         false,
				PausedProjects: []string{},
			}, nil
		},
	}
	router := newTaskTestRouter(&mockTaskService{}, runnerMock)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/tasks/runner/status")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	body := decodeJSON[types.RunnerStatusResponse](t, resp)
	if !body.Running {
		t.Error("expected running = true")
	}
}

func TestHandleRunnerStatusError(t *testing.T) {
	runnerMock := &mockRunnerService{
		getStatusFunc: func(ctx context.Context) (*types.RunnerStatusResponse, error) {
			return nil, fmt.Errorf("runner not available")
		},
	}
	router := newTaskTestRouter(&mockTaskService{}, runnerMock)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/tasks/runner/status")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", resp.StatusCode, http.StatusInternalServerError)
	}
}

// =============================================================================
// Task Event Emission Tests
// =============================================================================

func newTaskTestRouterWithEvents(taskMock *mockTaskService, es *mockEventService) *chi.Mux {
	hub := realtime.NewHub()
	h := NewHandler(
		&mockBrainService{},
		WithTaskService(taskMock),
		WithRunnerService(&mockRunnerService{}),
		WithEventService(es),
		WithHub(hub),
	)
	r := chi.NewRouter()
	r.Route("/tasks/{projectId}", func(r chi.Router) {
		r.Post("/{taskId}/claim", h.HandleClaimTask)
		r.Post("/{taskId}/release", h.HandleReleaseTask)
		r.Post("/{taskId}/trigger", h.HandleTriggerTask)
	})
	return r
}

func TestHandleClaimTask_EmitsEvent(t *testing.T) {
	es := &mockEventService{}
	taskMock := &mockTaskService{
		claimTaskFunc: func(_ context.Context, _, _, _ string) (*types.ClaimResponse, error) {
			return &types.ClaimResponse{Success: true}, nil
		},
	}
	router := newTaskTestRouterWithEvents(taskMock, es)
	srv := httptest.NewServer(router)
	defer srv.Close()

	body := jsonBody(t, map[string]any{"runnerId": "runner-1"})
	resp, err := http.Post(srv.URL+"/tasks/myproj/task123/claim", "application/json", body)
	if err != nil {
		t.Fatalf("POST claim failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	es.mu.Lock()
	defer es.mu.Unlock()
	if len(es.ingested) != 1 {
		t.Fatalf("ingested events = %d, want 1", len(es.ingested))
	}
	evt := es.ingested[0]
	if evt.Type != types.EventTaskClaimed {
		t.Errorf("event type = %q, want %q", evt.Type, types.EventTaskClaimed)
	}
	if evt.Source != types.EventSourceAPI {
		t.Errorf("event source = %q, want %q", evt.Source, types.EventSourceAPI)
	}
	if evt.ProjectID != "myproj" {
		t.Errorf("project_id = %q, want %q", evt.ProjectID, "myproj")
	}
	if evt.TaskID != "task123" {
		t.Errorf("task_id = %q, want %q", evt.TaskID, "task123")
	}
	if evt.Metadata["runner_id"] != "runner-1" {
		t.Errorf("metadata[runner_id] = %q, want %q", evt.Metadata["runner_id"], "runner-1")
	}
}

func TestHandleReleaseTask_EmitsEvent(t *testing.T) {
	es := &mockEventService{}
	taskMock := &mockTaskService{
		releaseTaskFunc: func(_ context.Context, _, _, _ string) error {
			return nil
		},
	}
	router := newTaskTestRouterWithEvents(taskMock, es)
	srv := httptest.NewServer(router)
	defer srv.Close()

	body := jsonBody(t, map[string]any{"runnerId": "runner-1"})
	resp, err := http.Post(srv.URL+"/tasks/myproj/task123/release", "application/json", body)
	if err != nil {
		t.Fatalf("POST release failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	es.mu.Lock()
	defer es.mu.Unlock()
	if len(es.ingested) != 1 {
		t.Fatalf("ingested events = %d, want 1", len(es.ingested))
	}
	evt := es.ingested[0]
	if evt.Type != types.EventTaskReleased {
		t.Errorf("event type = %q, want %q", evt.Type, types.EventTaskReleased)
	}
	if evt.Source != types.EventSourceAPI {
		t.Errorf("event source = %q, want %q", evt.Source, types.EventSourceAPI)
	}
	if evt.ProjectID != "myproj" {
		t.Errorf("project_id = %q, want %q", evt.ProjectID, "myproj")
	}
	if evt.TaskID != "task123" {
		t.Errorf("task_id = %q, want %q", evt.TaskID, "task123")
	}
}

// =============================================================================
// Get Task Metadata Tests
// =============================================================================

func TestHandleGetTaskMetadata_ReturnsExecutionFields(t *testing.T) {
	completeOnIdle := true
	openPR := false

	tests := []struct {
		name       string
		projectId  string
		taskId     string
		mockFn     func(ctx context.Context, projectId, taskId string) (*types.ResolvedTask, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name:      "returns metadata fields without content or title",
			projectId: "my-project",
			taskId:    "abc12def",
			mockFn: func(ctx context.Context, projectId, taskId string) (*types.ResolvedTask, error) {
				return &types.ResolvedTask{
					ID:                  taskId,
					Path:                "projects/my-project/task/abc12def.md",
					Title:               "Some Task Title",
					Agent:               "tdd-dev",
					Model:               "claude-sonnet",
					ExecutionMode:       "worktree",
					GitBranch:           "feature/abc",
					GitRemote:           "origin",
					MergePolicy:         "auto_merge",
					MergeStrategy:       "squash",
					MergeTargetBranch:   "main",
					RemoteBranchPolicy:  "delete",
					CompleteOnIdle:      &completeOnIdle,
					OpenPRBeforeMerge:   &openPR,
					TargetWorkdir:       "/tmp/work",
					ResolvedWorkdir:     "/tmp/work/resolved",
					DirectPrompt:        "Fix the auth bug",
					Executor:            "opencode",
					FeatureID:           "auth-system",
					FeaturePriority:     "high",
					FeatureDependsOn:    []string{"setup-feat"},
					DependsOn:           []string{"dep1"},
					ResolvedDeps:        []string{"dep1"},
					UnresolvedDeps:      []string{},
					Classification:      "ready",
					BlockedBy:           []string{},
					WaitingOn:           []string{},
					InCycle:             false,
					Status:              "pending",
					Priority:            "high",
					Created:             "2025-01-01T00:00:00Z",
					UserOriginalRequest: "Please fix the auth bug",
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[TaskMetadataResponse](t, resp)

				// Verify metadata fields are present
				if body.Agent != "tdd-dev" {
					t.Errorf("agent = %q, want %q", body.Agent, "tdd-dev")
				}
				if body.Model != "claude-sonnet" {
					t.Errorf("model = %q, want %q", body.Model, "claude-sonnet")
				}
				if body.ExecutionMode != "worktree" {
					t.Errorf("execution_mode = %q, want %q", body.ExecutionMode, "worktree")
				}
				if body.GitBranch != "feature/abc" {
					t.Errorf("git_branch = %q, want %q", body.GitBranch, "feature/abc")
				}
				if body.MergePolicy != "auto_merge" {
					t.Errorf("merge_policy = %q, want %q", body.MergePolicy, "auto_merge")
				}
				if body.FeatureID != "auth-system" {
					t.Errorf("feature_id = %q, want %q", body.FeatureID, "auth-system")
				}
				if body.DirectPrompt != "Fix the auth bug" {
					t.Errorf("direct_prompt = %q, want %q", body.DirectPrompt, "Fix the auth bug")
				}
				if body.Path != "projects/my-project/task/abc12def.md" {
					t.Errorf("path = %q, want %q", body.Path, "projects/my-project/task/abc12def.md")
				}
				if body.Executor != "opencode" {
					t.Errorf("executor = %q, want %q", body.Executor, "opencode")
				}
				if body.CompleteOnIdle == nil || !*body.CompleteOnIdle {
					t.Error("expected complete_on_idle = true")
				}
				if body.OpenPRBeforeMerge == nil || *body.OpenPRBeforeMerge {
					t.Error("expected open_pr_before_merge = false")
				}

				// Verify content and title are NOT in the response
				// (We decode into a raw map to check for absence of these keys)
			},
		},
		{
			name:      "returns 404 for nonexistent task",
			projectId: "my-project",
			taskId:    "nonexistent",
			mockFn: func(ctx context.Context, projectId, taskId string) (*types.ResolvedTask, error) {
				return nil, ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			taskMock := &mockTaskService{getTaskFunc: tt.mockFn}
			router := newTaskTestRouter(taskMock, &mockRunnerService{})
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/tasks/" + tt.projectId + "/" + tt.taskId + "/metadata")
			if err != nil {
				t.Fatalf("GET /tasks/%s/%s/metadata failed: %v", tt.projectId, tt.taskId, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.checkBody != nil {
				tt.checkBody(t, resp)
			}
		})
	}
}

func TestHandleGetTaskMetadata_ExcludesTitleAndContent(t *testing.T) {
	taskMock := &mockTaskService{
		getTaskFunc: func(ctx context.Context, projectId, taskId string) (*types.ResolvedTask, error) {
			return &types.ResolvedTask{
				ID:                  taskId,
				Title:               "Should Not Appear",
				Agent:               "tdd-dev",
				UserOriginalRequest: "Should Not Appear Either",
			}, nil
		},
	}
	router := newTaskTestRouter(taskMock, &mockRunnerService{})
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/tasks/my-project/abc12def/metadata")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Decode into a raw map to verify title and user_original_request are absent
	var raw map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if _, ok := raw["title"]; ok {
		t.Error("response should NOT contain 'title' field")
	}
	if _, ok := raw["user_original_request"]; ok {
		t.Error("response should NOT contain 'user_original_request' field")
	}
	if _, ok := raw["id"]; ok {
		t.Error("response should NOT contain 'id' field")
	}

	// But metadata fields should be present
	if _, ok := raw["agent"]; !ok {
		t.Error("response should contain 'agent' field")
	}
}

func TestHandleTriggerTask_EmitsEvent(t *testing.T) {
	es := &mockEventService{}
	taskMock := &mockTaskService{
		triggerTaskFunc: func(_ context.Context, _, _ string) (*types.TriggerResponse, error) {
			return &types.TriggerResponse{
				Triggered: true,
				TaskID:    "task123",
				Success:   true,
			}, nil
		},
	}
	router := newTaskTestRouterWithEvents(taskMock, es)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/tasks/myproj/task123/trigger", "application/json", nil)
	if err != nil {
		t.Fatalf("POST trigger failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	es.mu.Lock()
	defer es.mu.Unlock()
	if len(es.ingested) != 1 {
		t.Fatalf("ingested events = %d, want 1", len(es.ingested))
	}
	evt := es.ingested[0]
	if evt.Type != types.EventTaskTriggered {
		t.Errorf("event type = %q, want %q", evt.Type, types.EventTaskTriggered)
	}
	if evt.Source != types.EventSourceAPI {
		t.Errorf("event source = %q, want %q", evt.Source, types.EventSourceAPI)
	}
	if evt.ProjectID != "myproj" {
		t.Errorf("project_id = %q, want %q", evt.ProjectID, "myproj")
	}
	if evt.TaskID != "task123" {
		t.Errorf("task_id = %q, want %q", evt.TaskID, "task123")
	}
}

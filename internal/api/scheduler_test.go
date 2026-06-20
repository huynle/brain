package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

type mockSchedulerService struct {
	status types.SchedulerStatus
}

func (m mockSchedulerService) Status() types.SchedulerStatus {
	return m.status
}

type mockSchedulerVisibilityService struct {
	lease      *types.DispatchLease
	reasons    []types.PlacementReason
	leaseErr   error
	reasonsErr error
	gotProject string
	gotTask    string
}

func (m *mockSchedulerVisibilityService) GetDispatchLease(ctx context.Context, projectID, taskID string) (*types.DispatchLease, error) {
	m.gotProject = projectID
	m.gotTask = taskID
	return m.lease, m.leaseErr
}

func (m *mockSchedulerVisibilityService) ListPlacementReasons(ctx context.Context, projectID, taskID string) ([]types.PlacementReason, error) {
	m.gotProject = projectID
	m.gotTask = taskID
	return m.reasons, m.reasonsErr
}

func newSchedulerTestRouter(s SchedulerService, views SchedulerVisibilityService) http.Handler {
	h := NewHandler(&mockBrainService{}, WithSchedulerService(s), WithSchedulerVisibilityService(views))
	r := chi.NewRouter()
	r.Get("/scheduler/status", h.HandleSchedulerStatus)
	r.Get("/tasks/{projectId}/{taskId}/dispatch-lease", h.HandleGetDispatchLease)
	r.Get("/tasks/{projectId}/{taskId}/placement-reasons", h.HandleListPlacementReasons)
	return r
}

func TestHandleSchedulerStatus(t *testing.T) {
	router := newSchedulerTestRouter(mockSchedulerService{status: types.SchedulerStatus{
		Started:           true,
		Running:           true,
		Interval:          "5s",
		TotalTicks:        7,
		LastExpiredLeases: 2,
		LastProjectResults: map[string]types.SchedulerResult{
			"brain-api": {ProjectID: "brain-api", Considered: 3, Dispatched: 1, Skipped: 2},
		},
	}}, nil)

	req := httptest.NewRequest(http.MethodGet, "/scheduler/status", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var got types.SchedulerStatus
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !got.Started || !got.Running || got.Interval != "5s" || got.TotalTicks != 7 {
		t.Fatalf("status response = %+v", got)
	}
	if got.LastProjectResults["brain-api"].Dispatched != 1 {
		t.Fatalf("project results = %+v", got.LastProjectResults)
	}
}

func TestHandleGetDispatchLease(t *testing.T) {
	views := &mockSchedulerVisibilityService{lease: &types.DispatchLease{
		ProjectID:        "brain-api",
		TaskID:           "task-1",
		AssignedRunnerID: "runner-1",
		State:            "pushed",
		PushedAt:         100,
		ExpiresAt:        200,
	}}
	router := newSchedulerTestRouter(nil, views)

	req := httptest.NewRequest(http.MethodGet, "/tasks/brain-api/task-1/dispatch-lease", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	if views.gotProject != "brain-api" || views.gotTask != "task-1" {
		t.Fatalf("lookup = %s/%s, want brain-api/task-1", views.gotProject, views.gotTask)
	}
	var got types.DispatchLease
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.AssignedRunnerID != "runner-1" || got.State != "pushed" {
		t.Fatalf("lease response = %+v", got)
	}
}

func TestHandleGetDispatchLeaseNotFound(t *testing.T) {
	router := newSchedulerTestRouter(nil, &mockSchedulerVisibilityService{})

	req := httptest.NewRequest(http.MethodGet, "/tasks/brain-api/missing/dispatch-lease", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestHandleListPlacementReasons(t *testing.T) {
	views := &mockSchedulerVisibilityService{reasons: []types.PlacementReason{
		{ID: 1, ProjectID: "brain-api", TaskID: "task-1", RunnerID: "runner-1", Decision: "no_candidate", Reason: "runner at capacity", CreatedAt: 123},
	}}
	router := newSchedulerTestRouter(nil, views)

	req := httptest.NewRequest(http.MethodGet, "/tasks/brain-api/task-1/placement-reasons", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var got types.PlacementReasonListResponse
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Total != 1 || got.Reasons[0].Reason != "runner at capacity" {
		t.Fatalf("reasons response = %+v", got)
	}
}

func TestSchedulerVisibilityRoutes(t *testing.T) {
	h := NewHandler(
		&mockBrainService{},
		WithSchedulerService(mockSchedulerService{status: types.SchedulerStatus{Started: true}}),
		WithSchedulerVisibilityService(&mockSchedulerVisibilityService{lease: &types.DispatchLease{ProjectID: "brain-api", TaskID: "task-1", State: "pushed"}}),
	)
	router := NewRouter(testConfig(), WithHandler(h))

	for _, path := range []string{
		"/api/v1/scheduler/status",
		"/api/v1/tasks/brain-api/task-1/dispatch-lease",
		"/api/v1/tasks/brain-api/task-1/placement-reasons",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d; body=%s", path, w.Code, http.StatusOK, w.Body.String())
		}
	}
}

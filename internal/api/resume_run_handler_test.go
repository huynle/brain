package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// mockRunProjectService is the fake used to exercise HandleRunProject
// without spinning up the real scheduler. Nil runProject on the handler
// yields 501 — a case we also cover below.
type mockRunProjectService struct {
	fn func(ctx context.Context, projectID string, force bool) (*types.RunProjectResponse, error)
}

func (m *mockRunProjectService) RunProjectNow(ctx context.Context, projectID string, force bool) (*types.RunProjectResponse, error) {
	if m.fn != nil {
		return m.fn(ctx, projectID, force)
	}
	return nil, fmt.Errorf("runProjectFunc not set")
}

// newResumeRunTestRouter builds the minimum chi router surface to exercise
// HandleResumeTask, HandleResumeFeature, and HandleRunProject. Kept
// intentionally small so tests don't accidentally depend on unrelated
// route registration order changes.
func newResumeRunTestRouter(t *testing.T, taskMock *mockTaskService, runProject RunProjectService) *chi.Mux {
	t.Helper()
	hub := realtime.NewHub()
	opts := []HandlerOption{
		WithTaskService(taskMock),
		WithHub(hub),
	}
	if runProject != nil {
		opts = append(opts, WithRunProjectService(runProject))
	}
	h := NewHandler(&mockBrainService{}, opts...)

	r := chi.NewRouter()
	r.Route("/tasks/{projectId}", func(r chi.Router) {
		r.Post("/{taskId}/resume", h.HandleResumeTask)
		r.Post("/features/{featureId}/resume", h.HandleResumeFeature)
		r.Post("/run", h.HandleRunProject)
	})
	return r
}

func doPost(t *testing.T, url string, body interface{}) *http.Response {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatalf("encode: %v", err)
		}
	}
	resp, err := http.Post(url, "application/json", &buf)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	return resp
}

// -----------------------------------------------------------------------
// HandleResumeTask
// -----------------------------------------------------------------------

func TestHandleResumeTask_200_HappyPath(t *testing.T) {
	taskMock := &mockTaskService{
		resumeTaskFunc: func(_ context.Context, project, task string, opts *types.ResumeTaskOptions) (*types.ResumeTaskResult, error) {
			if project != "my-proj" || task != "abc12def" {
				t.Errorf("unexpected identifiers: %s/%s", project, task)
			}
			if opts == nil || !opts.Force {
				t.Errorf("expected Force=true in decoded body, got %+v", opts)
			}
			return &types.ResumeTaskResult{
				TaskID: task, Resumed: true,
				PriorStatus: "in_progress", AbandonReason: "runner_offline",
			}, nil
		},
	}
	router := newResumeRunTestRouter(t, taskMock, nil)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/tasks/my-proj/abc12def/resume", map[string]bool{"force": true})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeJSON[types.ResumeTaskResult](t, resp)
	if !body.Resumed {
		t.Errorf("expected Resumed=true")
	}
	if body.AbandonReason != "runner_offline" {
		t.Errorf("AbandonReason = %q, want runner_offline", body.AbandonReason)
	}
}

func TestHandleResumeTask_404_NotFound(t *testing.T) {
	taskMock := &mockTaskService{
		resumeTaskFunc: func(_ context.Context, _, _ string, _ *types.ResumeTaskOptions) (*types.ResumeTaskResult, error) {
			// Service-layer error containing "not found" — the handler's
			// string-contains fallback must map this to 404.
			return nil, fmt.Errorf("resume: task not found: my-proj/missing")
		},
	}
	router := newResumeRunTestRouter(t, taskMock, nil)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/tasks/my-proj/missing/resume", nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (string-contains fallback)", resp.StatusCode)
	}
}

func TestHandleResumeTask_400_BadPathParam(t *testing.T) {
	taskMock := &mockTaskService{
		resumeTaskFunc: func(_ context.Context, _, _ string, _ *types.ResumeTaskOptions) (*types.ResumeTaskResult, error) {
			t.Fatal("service should not be reached — path param must fail validation first")
			return nil, nil
		},
	}
	router := newResumeRunTestRouter(t, taskMock, nil)
	srv := httptest.NewServer(router)
	defer srv.Close()

	// %2F encodes to /, which would let the router pass but the value
	// contains a slash — validatePathParam should reject it.
	resp, err := http.Post(srv.URL+"/tasks/proj/bad%20id%2Fslash/resume", "application/json", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for path traversal input", resp.StatusCode)
	}
}

func TestHandleResumeTask_400_UnknownJSONField(t *testing.T) {
	// DisallowUnknownFields — a body with an unknown key must 400 instead
	// of silently succeeding.
	taskMock := &mockTaskService{
		resumeTaskFunc: func(_ context.Context, _, _ string, _ *types.ResumeTaskOptions) (*types.ResumeTaskResult, error) {
			t.Fatal("service should not be reached — bad body must 400 first")
			return nil, nil
		},
	}
	router := newResumeRunTestRouter(t, taskMock, nil)
	srv := httptest.NewServer(router)
	defer srv.Close()

	body := bytes.NewBufferString(`{"forceType":"true","typo":"yes"}`)
	resp, err := http.Post(srv.URL+"/tasks/my-proj/abc/resume", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for unknown JSON field", resp.StatusCode)
	}
}

// -----------------------------------------------------------------------
// HandleResumeFeature
// -----------------------------------------------------------------------

func TestHandleResumeFeature_200_HappyPath(t *testing.T) {
	taskMock := &mockTaskService{
		resumeFeatureFunc: func(_ context.Context, project, feature string, opts *types.ResumeTaskOptions) (*types.ResumeFeatureResult, error) {
			return &types.ResumeFeatureResult{
				FeatureID:    feature,
				TotalResumed: 2, TotalSkipped: 1,
				Results: []types.ResumeTaskResult{
					{TaskID: "a", Resumed: true, PriorStatus: "in_progress", AbandonReason: "runner_offline"},
					{TaskID: "b", Resumed: true, PriorStatus: "blocked", AbandonReason: "orphan_reaped"},
					{TaskID: "c", Resumed: false, PriorStatus: "completed", Reason: "terminal_status_excluded_from_batch (completed)"},
				},
			}, nil
		},
	}
	router := newResumeRunTestRouter(t, taskMock, nil)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/tasks/my-proj/features/some-feat/resume", map[string]bool{"force": true})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeJSON[types.ResumeFeatureResult](t, resp)
	if body.TotalResumed != 2 || body.TotalSkipped != 1 {
		t.Errorf("counts = %d/%d, want 2/1", body.TotalResumed, body.TotalSkipped)
	}
	if len(body.Results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(body.Results))
	}
	// Guard: last result must clearly report the terminal skip reason so
	// the PWA can render it distinctly. This is the wire-level regression
	// test for the critical batch-force bug.
	if !strings.Contains(body.Results[2].Reason, "terminal_status_excluded_from_batch") {
		t.Errorf("terminal-skip reason not surfaced in response: %q", body.Results[2].Reason)
	}
}

func TestHandleResumeFeature_404_NotFound(t *testing.T) {
	taskMock := &mockTaskService{
		resumeFeatureFunc: func(_ context.Context, _, _ string, _ *types.ResumeTaskOptions) (*types.ResumeFeatureResult, error) {
			return nil, fmt.Errorf("resume feature: not found — feature has no tasks")
		},
	}
	router := newResumeRunTestRouter(t, taskMock, nil)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/tasks/my-proj/features/ghost/resume", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// -----------------------------------------------------------------------
// HandleRunProject
// -----------------------------------------------------------------------

func TestHandleRunProject_501_WhenServiceNotWired(t *testing.T) {
	taskMock := &mockTaskService{}
	router := newResumeRunTestRouter(t, taskMock, nil) // no runProject
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/tasks/my-proj/run", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501 when runProject unwired", resp.StatusCode)
	}
}

func TestHandleRunProject_200_HappyPath(t *testing.T) {
	taskMock := &mockTaskService{}
	rp := &mockRunProjectService{
		fn: func(_ context.Context, project string, force bool) (*types.RunProjectResponse, error) {
			if project != "my-proj" {
				t.Errorf("project = %q, want my-proj", project)
			}
			return &types.RunProjectResponse{
				ProjectID:            project,
				FeaturesConsidered:   3,
				FeaturesDispatched:   2,
				FeaturesSkipped:      1,
				TotalTasksDispatched: 5,
			}, nil
		},
	}
	router := newResumeRunTestRouter(t, taskMock, rp)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/tasks/my-proj/run", map[string]bool{"force": false})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeJSON[types.RunProjectResponse](t, resp)
	if body.TotalTasksDispatched != 5 {
		t.Errorf("TotalTasksDispatched = %d, want 5", body.TotalTasksDispatched)
	}
	if body.FeaturesDispatched != 2 || body.FeaturesSkipped != 1 {
		t.Errorf("features counts wrong: %d dispatched / %d skipped", body.FeaturesDispatched, body.FeaturesSkipped)
	}
}

func TestHandleRunProject_400_BadPathParam(t *testing.T) {
	rp := &mockRunProjectService{
		fn: func(_ context.Context, _ string, _ bool) (*types.RunProjectResponse, error) {
			t.Fatal("service should not be reached")
			return nil, nil
		},
	}
	router := newResumeRunTestRouter(t, &mockTaskService{}, rp)
	srv := httptest.NewServer(router)
	defer srv.Close()

	// Semi-colon is not in the allowed [a-zA-Z0-9._-] set.
	resp, err := http.Post(srv.URL+"/tasks/bad;proj/run", "application/json", nil)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for invalid projectId", resp.StatusCode)
	}
}

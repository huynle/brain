package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// newResumeWithContextTestRouter wires the minimum chi surface for the two
// resume-with-context handlers, mirroring newResumeRunTestRouter.
func newResumeWithContextTestRouter(t *testing.T, taskMock *mockTaskService) *chi.Mux {
	t.Helper()
	hub := realtime.NewHub()
	h := NewHandler(&mockBrainService{}, WithTaskService(taskMock), WithHub(hub))

	r := chi.NewRouter()
	r.Route("/tasks/{projectId}", func(r chi.Router) {
		r.Post("/{taskId}/resume-with-context", h.HandleResumeWithContext)
		r.Post("/features/{featureId}/resume-with-context", h.HandleResumeFeatureWithContext)
	})
	return r
}

// -----------------------------------------------------------------------
// HandleResumeWithContext
// -----------------------------------------------------------------------

func TestHandleResumeWithContext_200_HappyPath(t *testing.T) {
	taskMock := &mockTaskService{
		resumeTaskWithContextFunc: func(_ context.Context, project, task string, opts *types.ResumeWithContextOptions) (*types.ResumeWithContextResult, error) {
			if project != "my-proj" || task != "abc12def" {
				t.Errorf("unexpected identifiers: %s/%s", project, task)
			}
			if opts == nil || opts.InjectedContext != "hello ctx" {
				t.Errorf("expected injected_context decoded, got %+v", opts)
			}
			return &types.ResumeWithContextResult{
				ResumeTaskResult: types.ResumeTaskResult{TaskID: task, Resumed: true, PriorStatus: "in_progress", AbandonReason: "runner_offline"},
				ResumeMode:       types.ResumeModeSameSession,
				TargetSessionID:  "ses-abc",
			}, nil
		},
	}
	router := newResumeWithContextTestRouter(t, taskMock)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/tasks/my-proj/abc12def/resume-with-context",
		map[string]interface{}{"injected_context": "hello ctx", "prefer_same_session": true})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeJSON[types.ResumeWithContextResult](t, resp)
	if !body.Resumed {
		t.Error("expected Resumed=true")
	}
	if body.ResumeMode != types.ResumeModeSameSession {
		t.Errorf("ResumeMode = %q, want same_session", body.ResumeMode)
	}
	if body.TargetSessionID != "ses-abc" {
		t.Errorf("TargetSessionID = %q, want ses-abc", body.TargetSessionID)
	}
}

func TestHandleResumeWithContext_200_LiveInjected(t *testing.T) {
	taskMock := &mockTaskService{
		resumeTaskWithContextFunc: func(_ context.Context, _, task string, _ *types.ResumeWithContextOptions) (*types.ResumeWithContextResult, error) {
			return &types.ResumeWithContextResult{
				ResumeTaskResult: types.ResumeTaskResult{TaskID: task, Resumed: true, PriorStatus: "in_progress"},
				ResumeMode:       types.ResumeModeLiveInjected,
				TargetSessionID:  "ses-live",
				InjectedLive:     true,
			}, nil
		},
	}
	router := newResumeWithContextTestRouter(t, taskMock)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/tasks/my-proj/abc12def/resume-with-context",
		map[string]interface{}{"injected_context": "live"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeJSON[types.ResumeWithContextResult](t, resp)
	if !body.InjectedLive || body.ResumeMode != types.ResumeModeLiveInjected {
		t.Errorf("expected live_injected, got mode=%q injectedLive=%v", body.ResumeMode, body.InjectedLive)
	}
}

func TestHandleResumeWithContext_400_MissingInjectedContext(t *testing.T) {
	taskMock := &mockTaskService{
		resumeTaskWithContextFunc: func(_ context.Context, _, _ string, _ *types.ResumeWithContextOptions) (*types.ResumeWithContextResult, error) {
			t.Fatal("service must NOT be reached when injected_context is missing")
			return nil, nil
		},
	}
	router := newResumeWithContextTestRouter(t, taskMock)
	srv := httptest.NewServer(router)
	defer srv.Close()

	// Empty body → InjectedContext="" → 400 before the service is called.
	resp := doPost(t, srv.URL+"/tasks/my-proj/abc12def/resume-with-context", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("empty body: status = %d, want 400", resp.StatusCode)
	}

	// Body present but injected_context empty → also 400.
	resp2 := doPost(t, srv.URL+"/tasks/my-proj/abc12def/resume-with-context",
		map[string]interface{}{"prefer_same_session": true})
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusBadRequest {
		t.Errorf("empty injected_context: status = %d, want 400", resp2.StatusCode)
	}
}

func TestHandleResumeWithContext_404_NotFound(t *testing.T) {
	taskMock := &mockTaskService{
		resumeTaskWithContextFunc: func(_ context.Context, _, _ string, _ *types.ResumeWithContextOptions) (*types.ResumeWithContextResult, error) {
			return nil, fmt.Errorf("resume-with-context: task not found: my-proj/missing")
		},
	}
	router := newResumeWithContextTestRouter(t, taskMock)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/tasks/my-proj/missing/resume-with-context",
		map[string]interface{}{"injected_context": "ctx"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

func TestHandleResumeWithContext_400_BadPathParam(t *testing.T) {
	taskMock := &mockTaskService{
		resumeTaskWithContextFunc: func(_ context.Context, _, _ string, _ *types.ResumeWithContextOptions) (*types.ResumeWithContextResult, error) {
			t.Fatal("service should not be reached — path param must fail validation first")
			return nil, nil
		},
	}
	router := newResumeWithContextTestRouter(t, taskMock)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/tasks/proj/bad%20id%2Fslash/resume-with-context", "application/json",
		bytes.NewBufferString(`{"injected_context":"x"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for path traversal input", resp.StatusCode)
	}
}

func TestHandleResumeWithContext_400_UnknownJSONField(t *testing.T) {
	taskMock := &mockTaskService{
		resumeTaskWithContextFunc: func(_ context.Context, _, _ string, _ *types.ResumeWithContextOptions) (*types.ResumeWithContextResult, error) {
			t.Fatal("service should not be reached — bad body must 400 first")
			return nil, nil
		},
	}
	router := newResumeWithContextTestRouter(t, taskMock)
	srv := httptest.NewServer(router)
	defer srv.Close()

	body := bytes.NewBufferString(`{"injected_context":"x","typo":"yes"}`)
	resp, err := http.Post(srv.URL+"/tasks/my-proj/abc/resume-with-context", "application/json", body)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for unknown JSON field", resp.StatusCode)
	}
}

// -----------------------------------------------------------------------
// HandleResumeFeatureWithContext
// -----------------------------------------------------------------------

func TestHandleResumeFeatureWithContext_200_HappyPath(t *testing.T) {
	taskMock := &mockTaskService{
		resumeFeatureWithContextFunc: func(_ context.Context, _, feature string, opts *types.ResumeWithContextOptions) (*types.ResumeWithContextFeatureResult, error) {
			if opts == nil || opts.InjectedContext != "batch ctx" {
				t.Errorf("expected injected_context decoded, got %+v", opts)
			}
			return &types.ResumeWithContextFeatureResult{
				FeatureID:    feature,
				TotalResumed: 2, TotalSkipped: 1,
				Results: []types.ResumeWithContextResult{
					{ResumeTaskResult: types.ResumeTaskResult{TaskID: "a", Resumed: true, PriorStatus: "in_progress"}, ResumeMode: types.ResumeModeRehydrate},
					{ResumeTaskResult: types.ResumeTaskResult{TaskID: "b", Resumed: true, PriorStatus: "in_progress"}, ResumeMode: types.ResumeModeLiveInjected, InjectedLive: true},
					{ResumeTaskResult: types.ResumeTaskResult{TaskID: "c", Resumed: false, PriorStatus: "completed", Reason: "terminal_status_excluded_from_batch (completed)"}},
				},
			}, nil
		},
	}
	router := newResumeWithContextTestRouter(t, taskMock)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/tasks/my-proj/features/some-feat/resume-with-context",
		map[string]interface{}{"injected_context": "batch ctx"})
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := decodeJSON[types.ResumeWithContextFeatureResult](t, resp)
	if body.TotalResumed != 2 || body.TotalSkipped != 1 {
		t.Errorf("counts = %d/%d, want 2/1", body.TotalResumed, body.TotalSkipped)
	}
	if len(body.Results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(body.Results))
	}
}

func TestHandleResumeFeatureWithContext_400_MissingInjectedContext(t *testing.T) {
	taskMock := &mockTaskService{
		resumeFeatureWithContextFunc: func(_ context.Context, _, _ string, _ *types.ResumeWithContextOptions) (*types.ResumeWithContextFeatureResult, error) {
			t.Fatal("service must NOT be reached when injected_context is missing")
			return nil, nil
		},
	}
	router := newResumeWithContextTestRouter(t, taskMock)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/tasks/my-proj/features/f/resume-with-context", nil)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for missing injected_context", resp.StatusCode)
	}
}

func TestHandleResumeFeatureWithContext_404_NotFound(t *testing.T) {
	taskMock := &mockTaskService{
		resumeFeatureWithContextFunc: func(_ context.Context, _, _ string, _ *types.ResumeWithContextOptions) (*types.ResumeWithContextFeatureResult, error) {
			return nil, fmt.Errorf("resume-with-context feature: not found — feature has no tasks")
		},
	}
	router := newResumeWithContextTestRouter(t, taskMock)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp := doPost(t, srv.URL+"/tasks/my-proj/features/ghost/resume-with-context",
		map[string]interface{}{"injected_context": "ctx"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

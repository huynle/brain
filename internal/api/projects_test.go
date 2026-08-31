package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// newDeleteProjectRouter wires the project-delete route plus an optional task
// service, which is what supplies the live-claim guard.
func newDeleteProjectRouter(brain *mockBrainService, tasks TaskService) *chi.Mux {
	opts := []HandlerOption{WithHub(realtime.NewHub())}
	if tasks != nil {
		opts = append(opts, WithTaskService(tasks))
	}
	h := NewHandler(brain, opts...)
	r := chi.NewRouter()
	r.Route("/tasks/{projectId}", func(r chi.Router) {
		r.Delete("/", h.HandleDeleteProject)
	})
	return r
}

func deleteProjectReq(t *testing.T, srv *httptest.Server, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodDelete, srv.URL+path, nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE %s: %v", path, err)
	}
	return resp
}

// okDeleteProject is a mock that records the project it was asked to delete.
func okDeleteProject(seen *string) func(context.Context, string) (*types.DeleteProjectResponse, error) {
	return func(_ context.Context, projectID string) (*types.DeleteProjectResponse, error) {
		*seen = projectID
		return &types.DeleteProjectResponse{
			Project:          projectID,
			Deleted:          7,
			DirectoryRemoved: true,
		}, nil
	}
}

func TestHandleDeleteProject_ConfirmMustNameTheProject(t *testing.T) {
	// Every other delete on this API takes confirm=true — a formality a
	// client sets once and forgets. Naming the project is what makes a
	// mis-routed request fail instead of wiping the wrong thing.
	tests := []struct {
		name       string
		query      string
		wantStatus int
	}{
		{"no confirmation at all", "", http.StatusBadRequest},
		{"the generic confirm=true", "?confirm=true", http.StatusBadRequest},
		{"a different project's name", "?confirm=warehouse", http.StatusBadRequest},
		{"case mismatch", "?confirm=SHOP", http.StatusBadRequest},
		{"the project's own name", "?confirm=shop", http.StatusOK},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var seen string
			brain := &mockBrainService{deleteProjectFunc: okDeleteProject(&seen)}
			srv := httptest.NewServer(newDeleteProjectRouter(brain, nil))
			defer srv.Close()

			resp := deleteProjectReq(t, srv, "/tasks/shop"+tt.query)
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
			if tt.wantStatus != http.StatusOK && seen != "" {
				t.Errorf("service was called with %q despite a rejected confirmation", seen)
			}
		})
	}
}

func TestHandleDeleteProject_PassesTheProjectAndReturnsTheCounts(t *testing.T) {
	var seen string
	brain := &mockBrainService{deleteProjectFunc: okDeleteProject(&seen)}
	srv := httptest.NewServer(newDeleteProjectRouter(brain, nil))
	defer srv.Close()

	resp := deleteProjectReq(t, srv, "/tasks/shop?confirm=shop")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if seen != "shop" {
		t.Errorf("service called with %q, want %q", seen, "shop")
	}

	body := decodeJSON[types.DeleteProjectResponse](t, resp)
	if body.Deleted != 7 {
		t.Errorf("deleted = %d, want 7", body.Deleted)
	}
	if !body.DirectoryRemoved {
		t.Error("directory_removed = false, want true")
	}
}

func TestHandleDeleteProject_NotFound(t *testing.T) {
	brain := &mockBrainService{
		deleteProjectFunc: func(context.Context, string) (*types.DeleteProjectResponse, error) {
			return nil, ErrNotFound
		},
	}
	srv := httptest.NewServer(newDeleteProjectRouter(brain, nil))
	defer srv.Close()

	resp := deleteProjectReq(t, srv, "/tasks/ghost?confirm=ghost")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
}

// liveClaimTasks builds a task service reporting one in_progress task held by
// an online runner.
func liveClaimTasks(live bool) *mockTaskService {
	return &mockTaskService{
		getTasksFunc: func(context.Context, string) (*types.TaskListResponse, error) {
			return &types.TaskListResponse{Tasks: []types.ResolvedTask{
				{ID: "done1", Status: "completed"},
				{ID: "busy1", Status: "in_progress"},
			}}, nil
		},
		getLiveClaimFunc: func(_ context.Context, _, taskID string) (*types.LiveClaim, error) {
			if taskID == "busy1" && live {
				return &types.LiveClaim{Live: true, RunnerID: "runner-a"}, nil
			}
			return &types.LiveClaim{}, nil
		},
	}
}

func TestHandleDeleteProject_RefusesWhileARunnerIsExecuting(t *testing.T) {
	var seen string
	brain := &mockBrainService{deleteProjectFunc: okDeleteProject(&seen)}
	srv := httptest.NewServer(newDeleteProjectRouter(brain, liveClaimTasks(true)))
	defer srv.Close()

	resp := deleteProjectReq(t, srv, "/tasks/shop?confirm=shop")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
	if seen != "" {
		t.Error("the wipe ran despite the live-claim refusal")
	}

	// The refusal has to name the runner — otherwise the user has nothing
	// to act on but "try again later".
	body := decodeJSON[types.ErrorResponse](t, resp)
	if !strings.Contains(body.Message, "runner-a") {
		t.Errorf("message %q does not name the blocking runner", body.Message)
	}
}

func TestHandleDeleteProject_ForceBypassesTheLiveClaimGuard(t *testing.T) {
	var seen string
	brain := &mockBrainService{deleteProjectFunc: okDeleteProject(&seen)}
	srv := httptest.NewServer(newDeleteProjectRouter(brain, liveClaimTasks(true)))
	defer srv.Close()

	resp := deleteProjectReq(t, srv, "/tasks/shop?confirm=shop&force=true")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if seen != "shop" {
		t.Error("force did not reach the service")
	}
}

func TestHandleDeleteProject_AStaleClaimDoesNotBlock(t *testing.T) {
	// A claim held by a crashed runner is exactly what users are trying to
	// clean up; only a LIVE claim refuses.
	var seen string
	brain := &mockBrainService{deleteProjectFunc: okDeleteProject(&seen)}
	srv := httptest.NewServer(newDeleteProjectRouter(brain, liveClaimTasks(false)))
	defer srv.Close()

	resp := deleteProjectReq(t, srv, "/tasks/shop?confirm=shop")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if seen != "shop" {
		t.Error("a stale claim blocked the wipe")
	}
}

func TestHandleDeleteProject_GuardFailsOpenWhenTheRegistryIsUnreachable(t *testing.T) {
	// A guard that blocks deletion whenever it cannot reach the registry is
	// worse than one that occasionally lets a racing delete through — the
	// same rule taskHasLiveClaim follows.
	var seen string
	brain := &mockBrainService{deleteProjectFunc: okDeleteProject(&seen)}
	tasks := &mockTaskService{
		getTasksFunc: func(context.Context, string) (*types.TaskListResponse, error) {
			return nil, fmt.Errorf("registry unreachable")
		},
	}
	srv := httptest.NewServer(newDeleteProjectRouter(brain, tasks))
	defer srv.Close()

	resp := deleteProjectReq(t, srv, "/tasks/shop?confirm=shop")
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if seen != "shop" {
		t.Error("a registry blip blocked the wipe")
	}
}

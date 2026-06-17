package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

type mockProjectPlacementService struct {
	getFunc func(ctx context.Context, projectID string) (*types.ProjectPlacement, error)
	putFunc func(ctx context.Context, projectID string, placement types.ProjectPlacement) (*types.ProjectPlacement, error)
}

func (m *mockProjectPlacementService) Get(ctx context.Context, projectID string) (*types.ProjectPlacement, error) {
	return m.getFunc(ctx, projectID)
}

func (m *mockProjectPlacementService) Put(ctx context.Context, projectID string, placement types.ProjectPlacement) (*types.ProjectPlacement, error) {
	return m.putFunc(ctx, projectID, placement)
}

func newPlacementTestRouter(mock *mockProjectPlacementService) http.Handler {
	h := NewHandler(&mockBrainService{}, WithProjectPlacementService(mock))
	r := chi.NewRouter()
	r.Get("/projects/{projectId}/placement", h.HandleGetProjectPlacement)
	r.Put("/projects/{projectId}/placement", h.HandlePutProjectPlacement)
	return r
}

func TestHandleGetProjectPlacement(t *testing.T) {
	router := newPlacementTestRouter(&mockProjectPlacementService{
		getFunc: func(ctx context.Context, projectID string) (*types.ProjectPlacement, error) {
			if projectID != "brain" {
				t.Fatalf("projectID = %q, want brain", projectID)
			}
			return &types.ProjectPlacement{ProjectID: projectID, Affinity: types.PlacementAffinitySoft}, nil
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/projects/brain/placement", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var got types.ProjectPlacement
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ProjectID != "brain" || got.Affinity != types.PlacementAffinitySoft {
		t.Fatalf("placement = %+v, want brain soft", got)
	}
}

func TestHandlePutProjectPlacement(t *testing.T) {
	router := newPlacementTestRouter(&mockProjectPlacementService{
		putFunc: func(ctx context.Context, projectID string, placement types.ProjectPlacement) (*types.ProjectPlacement, error) {
			if projectID != "brain" {
				t.Fatalf("projectID = %q, want brain", projectID)
			}
			if placement.Affinity != types.PlacementAffinityStrict || placement.WorkspacePolicy != types.WorkspacePolicyWorktree {
				t.Fatalf("placement request = %+v", placement)
			}
			placement.ProjectID = projectID
			return &placement, nil
		},
	})

	body := []byte(`{"affinity":"strict","workspace_policy":"worktree","preferred_machines":["runner-a"]}`)
	req := httptest.NewRequest(http.MethodPut, "/projects/brain/placement", bytes.NewReader(body))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
	}
	var got types.ProjectPlacement
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.ProjectID != "brain" || got.Affinity != types.PlacementAffinityStrict {
		t.Fatalf("placement = %+v, want strict persisted response", got)
	}
}

func TestProjectPlacementRoutes(t *testing.T) {
	h := NewHandler(&mockBrainService{}, WithProjectPlacementService(&mockProjectPlacementService{
		getFunc: func(ctx context.Context, projectID string) (*types.ProjectPlacement, error) {
			return &types.ProjectPlacement{ProjectID: projectID, Affinity: types.PlacementAffinitySoft}, nil
		},
		putFunc: func(ctx context.Context, projectID string, placement types.ProjectPlacement) (*types.ProjectPlacement, error) {
			placement.ProjectID = projectID
			return &placement, nil
		},
	}))
	router := NewRouter(testConfig(), WithHandler(h))

	t.Run("GET exact placement path", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/projects/brain/placement", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
	})

	t.Run("PUT exact placement path", func(t *testing.T) {
		body := []byte(`{"affinity":"none"}`)
		req := httptest.NewRequest(http.MethodPut, "/api/v1/projects/brain/placement", bytes.NewReader(body))
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body=%s", w.Code, http.StatusOK, w.Body.String())
		}
	})
}

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

type mockClientContextService struct {
	resolveFunc func(ctx context.Context, req types.ResolveClientContextRequest) (*types.ResolveClientContextResponse, error)
}

func (m *mockClientContextService) Resolve(ctx context.Context, req types.ResolveClientContextRequest) (*types.ResolveClientContextResponse, error) {
	if m.resolveFunc != nil {
		return m.resolveFunc(ctx, req)
	}
	return &types.ResolveClientContextResponse{ProjectID: "brain", Confidence: "high", Source: "folder_name"}, nil
}

func newContextTestRouter(mock *mockClientContextService) *chi.Mux {
	h := NewHandler(&mockBrainService{}, WithClientContextService(mock))
	r := chi.NewRouter()
	r.Route("/context", func(r chi.Router) {
		r.Post("/resolve", h.HandleResolveClientContext)
	})
	return r
}

func TestHandleResolveClientContext(t *testing.T) {
	var captured types.ResolveClientContextRequest
	router := newContextTestRouter(&mockClientContextService{
		resolveFunc: func(ctx context.Context, req types.ResolveClientContextRequest) (*types.ResolveClientContextResponse, error) {
			captured = req
			return &types.ResolveClientContextResponse{
				ProjectID:  "brain",
				Confidence: "high",
				Source:     "folder_name",
				Dream: &types.DreamContext{
					ID:      "dream1",
					Title:   "Project Dream",
					Path:    "projects/brain/dream/dream1.md",
					Content: "consolidated context",
				},
			}, nil
		},
	})

	body := `{"client":{"client_id":"client-1","kind":"opencode","host_id":"host-1","hostname":"mac"},"workspace":{"path":"/Users/me/code/brain","folder_name":"brain","git_branch":"dev"}}`
	req := httptest.NewRequest(http.MethodPost, "/context/resolve", strings.NewReader(body))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if captured.Client.ClientID != "client-1" || captured.Client.HostID != "host-1" {
		t.Fatalf("captured client = %+v", captured.Client)
	}
	if captured.Workspace.FolderName != "brain" || captured.Workspace.GitBranch != "dev" {
		t.Fatalf("captured workspace = %+v", captured.Workspace)
	}

	var resp types.ResolveClientContextResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.ProjectID != "brain" || resp.Dream == nil || resp.Dream.Content != "consolidated context" {
		t.Fatalf("response = %+v", resp)
	}
}

func TestHandleResolveClientContextRequiresClientIdentity(t *testing.T) {
	router := newContextTestRouter(&mockClientContextService{})
	req := httptest.NewRequest(http.MethodPost, "/context/resolve", strings.NewReader(`{"client":{"host_id":"host-1"},"workspace":{}}`))
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}
}

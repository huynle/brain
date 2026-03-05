package api

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// newSearchRouter creates a chi router with search handlers wired to the given mock.
func newSearchRouter(mock *mockBrainService) *chi.Mux {
	h := NewHandler(mock)
	r := chi.NewRouter()
	r.Post("/search", h.HandleSearch)
	r.Post("/inject", h.HandleInject)
	return r
}

// =============================================================================
// Search Tests
// =============================================================================

func TestHandleSearch(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		mockSearch func(ctx context.Context, req types.SearchRequest) (*types.SearchResponse, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success",
			body: map[string]any{
				"query": "test search",
				"type":  "task",
				"limit": 5,
			},
			mockSearch: func(ctx context.Context, req types.SearchRequest) (*types.SearchResponse, error) {
				if req.Query != "test search" {
					return nil, fmt.Errorf("query = %q, want %q", req.Query, "test search")
				}
				return &types.SearchResponse{
					Results: []types.SearchResult{
						{ID: "abc12def", Path: "projects/default/task/test.md", Title: "Test Task", Type: "task", Status: "active", Snippet: "test content..."},
					},
					Total: 1,
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.SearchResponse](t, resp)
				if body.Total != 1 {
					t.Errorf("total = %d, want %d", body.Total, 1)
				}
				if len(body.Results) != 1 {
					t.Fatalf("results count = %d, want %d", len(body.Results), 1)
				}
				if body.Results[0].ID != "abc12def" {
					t.Errorf("result id = %q, want %q", body.Results[0].ID, "abc12def")
				}
			},
		},
		{
			name:       "missing query",
			body:       map[string]any{},
			wantStatus: http.StatusBadRequest,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Validation Error" {
					t.Errorf("error = %q, want %q", body.Error, "Validation Error")
				}
			},
		},
		{
			name:       "empty query string",
			body:       map[string]any{"query": ""},
			wantStatus: http.StatusBadRequest,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Validation Error" {
					t.Errorf("error = %q, want %q", body.Error, "Validation Error")
				}
			},
		},
		{
			name:       "invalid JSON body",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Bad Request" {
					t.Errorf("error = %q, want %q", body.Error, "Bad Request")
				}
			},
		},
		{
			name: "service error",
			body: map[string]any{"query": "test"},
			mockSearch: func(ctx context.Context, req types.SearchRequest) (*types.SearchResponse, error) {
				return nil, fmt.Errorf("search index unavailable")
			},
			wantStatus: http.StatusInternalServerError,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Internal Server Error" {
					t.Errorf("error = %q, want %q", body.Error, "Internal Server Error")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{searchFunc: tt.mockSearch}
			router := newSearchRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			var body *bytes.Buffer
			switch v := tt.body.(type) {
			case string:
				body = bytes.NewBufferString(v)
			default:
				body = jsonBody(t, v)
			}

			resp, err := http.Post(srv.URL+"/search", "application/json", body)
			if err != nil {
				t.Fatalf("POST /search failed: %v", err)
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
// Inject Tests
// =============================================================================

func TestHandleInject(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		mockInject func(ctx context.Context, req types.InjectRequest) (*types.InjectResponse, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success",
			body: map[string]any{
				"query":      "auth module",
				"type":       "plan",
				"maxEntries": 3,
			},
			mockInject: func(ctx context.Context, req types.InjectRequest) (*types.InjectResponse, error) {
				if req.Query != "auth module" {
					return nil, fmt.Errorf("query = %q, want %q", req.Query, "auth module")
				}
				return &types.InjectResponse{
					Context: "## Auth Module\n\nSome context...",
					Entries: []types.InjectEntry{
						{ID: "abc12def", Path: "projects/default/plan/auth.md", Title: "Auth Plan", Type: "plan"},
					},
					Total: 1,
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.InjectResponse](t, resp)
				if body.Total != 1 {
					t.Errorf("total = %d, want %d", body.Total, 1)
				}
				if body.Context == "" {
					t.Error("context should not be empty")
				}
				if len(body.Entries) != 1 {
					t.Fatalf("entries count = %d, want %d", len(body.Entries), 1)
				}
			},
		},
		{
			name:       "missing query",
			body:       map[string]any{},
			wantStatus: http.StatusBadRequest,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Validation Error" {
					t.Errorf("error = %q, want %q", body.Error, "Validation Error")
				}
			},
		},
		{
			name:       "invalid JSON body",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "service error",
			body: map[string]any{"query": "test"},
			mockInject: func(ctx context.Context, req types.InjectRequest) (*types.InjectResponse, error) {
				return nil, fmt.Errorf("inject failed")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{injectFunc: tt.mockInject}
			router := newSearchRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			var body *bytes.Buffer
			switch v := tt.body.(type) {
			case string:
				body = bytes.NewBufferString(v)
			default:
				body = jsonBody(t, v)
			}

			resp, err := http.Post(srv.URL+"/inject", "application/json", body)
			if err != nil {
				t.Fatalf("POST /inject failed: %v", err)
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

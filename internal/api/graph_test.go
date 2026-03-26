package api

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/huynle/brain-api/internal/types"
)

// newGraphRouter creates a chi router with graph handlers wired to the given mock.
func newGraphRouter(mock *mockBrainService) *chi.Mux {
	h := NewHandler(mock)
	r := chi.NewRouter()
	r.Get("/entries/{id}/backlinks", h.HandleGetBacklinks)
	r.Get("/entries/{id}/outlinks", h.HandleGetOutlinks)
	r.Get("/entries/{id}/related", h.HandleGetRelated)
	return r
}

// =============================================================================
// Backlinks Tests
// =============================================================================

func TestHandleGetBacklinks(t *testing.T) {
	tests := []struct {
		name          string
		id            string
		mockBacklinks func(ctx context.Context, path string) ([]types.BrainEntry, error)
		wantStatus    int
		checkBody     func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success",
			id:   "abc12def",
			mockBacklinks: func(ctx context.Context, path string) ([]types.BrainEntry, error) {
				if path != "abc12def" {
					return nil, fmt.Errorf("path = %q, want %q", path, "abc12def")
				}
				return []types.BrainEntry{
					{ID: "xyz98765", Path: "projects/default/plan/other.md", Title: "Other Entry", Type: "plan"},
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[[]types.BrainEntry](t, resp)
				if len(body) != 1 {
					t.Fatalf("entries count = %d, want %d", len(body), 1)
				}
				if body[0].ID != "xyz98765" {
					t.Errorf("entry id = %q, want %q", body[0].ID, "xyz98765")
				}
			},
		},
		{
			name: "not found",
			id:   "notexist",
			mockBacklinks: func(ctx context.Context, path string) ([]types.BrainEntry, error) {
				return nil, ErrNotFound
			},
			wantStatus: http.StatusNotFound,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Not Found" {
					t.Errorf("error = %q, want %q", body.Error, "Not Found")
				}
			},
		},
		{
			name: "service error",
			id:   "abc12def",
			mockBacklinks: func(ctx context.Context, path string) ([]types.BrainEntry, error) {
				return nil, fmt.Errorf("database error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{getBacklinksFunc: tt.mockBacklinks}
			router := newGraphRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/entries/" + tt.id + "/backlinks")
			if err != nil {
				t.Fatalf("GET /entries/%s/backlinks failed: %v", tt.id, err)
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
// Outlinks Tests
// =============================================================================

func TestHandleGetOutlinks(t *testing.T) {
	tests := []struct {
		name         string
		id           string
		mockOutlinks func(ctx context.Context, path string) ([]types.BrainEntry, error)
		wantStatus   int
		checkBody    func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success",
			id:   "abc12def",
			mockOutlinks: func(ctx context.Context, path string) ([]types.BrainEntry, error) {
				return []types.BrainEntry{
					{ID: "link1234", Path: "projects/default/task/linked.md", Title: "Linked Task", Type: "task"},
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[[]types.BrainEntry](t, resp)
				if len(body) != 1 {
					t.Fatalf("entries count = %d, want %d", len(body), 1)
				}
			},
		},
		{
			name: "not found",
			id:   "notexist",
			mockOutlinks: func(ctx context.Context, path string) ([]types.BrainEntry, error) {
				return nil, ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "service error",
			id:   "abc12def",
			mockOutlinks: func(ctx context.Context, path string) ([]types.BrainEntry, error) {
				return nil, fmt.Errorf("database error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{getOutlinksFunc: tt.mockOutlinks}
			router := newGraphRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/entries/" + tt.id + "/outlinks")
			if err != nil {
				t.Fatalf("GET /entries/%s/outlinks failed: %v", tt.id, err)
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
// Related Tests
// =============================================================================

func TestHandleGetRelated(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		query       string
		mockRelated func(ctx context.Context, path string, limit int) ([]types.BrainEntry, error)
		wantStatus  int
		checkBody   func(t *testing.T, resp *http.Response)
	}{
		{
			name:  "success with default limit",
			id:    "abc12def",
			query: "",
			mockRelated: func(ctx context.Context, path string, limit int) ([]types.BrainEntry, error) {
				if limit != 10 {
					return nil, fmt.Errorf("limit = %d, want %d", limit, 10)
				}
				return []types.BrainEntry{
					{ID: "rel12345", Path: "projects/default/plan/related.md", Title: "Related Entry", Type: "plan"},
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[[]types.BrainEntry](t, resp)
				if len(body) != 1 {
					t.Fatalf("entries count = %d, want %d", len(body), 1)
				}
			},
		},
		{
			name:  "success with custom limit",
			id:    "abc12def",
			query: "?limit=5",
			mockRelated: func(ctx context.Context, path string, limit int) ([]types.BrainEntry, error) {
				if limit != 5 {
					return nil, fmt.Errorf("limit = %d, want %d", limit, 5)
				}
				return []types.BrainEntry{}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:  "not found",
			id:    "notexist",
			query: "",
			mockRelated: func(ctx context.Context, path string, limit int) ([]types.BrainEntry, error) {
				return nil, ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:  "service error",
			id:    "abc12def",
			query: "",
			mockRelated: func(ctx context.Context, path string, limit int) ([]types.BrainEntry, error) {
				return nil, fmt.Errorf("database error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{getRelatedFunc: tt.mockRelated}
			router := newGraphRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/entries/" + tt.id + "/related" + tt.query)
			if err != nil {
				t.Fatalf("GET /entries/%s/related%s failed: %v", tt.id, tt.query, err)
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

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

// newHealthExtRouter creates a chi router with health-extended handlers wired to the given mock.
func newHealthExtRouter(mock *mockBrainService) *chi.Mux {
	h := NewHandler(mock)
	r := chi.NewRouter()
	r.Get("/stats", h.HandleGetStats)
	r.Get("/orphans", h.HandleGetOrphans)
	r.Get("/stale", h.HandleGetStale)
	r.Post("/entries/{id}/verify", h.HandleVerifyEntry)
	r.Post("/link", h.HandleGenerateLink)
	return r
}

// =============================================================================
// Stats Tests
// =============================================================================

func TestHandleGetStats(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		mockStats  func(ctx context.Context, global bool) (*types.StatsResponse, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name:  "success without global",
			query: "",
			mockStats: func(ctx context.Context, global bool) (*types.StatsResponse, error) {
				if global {
					return nil, fmt.Errorf("global = true, want false")
				}
				return &types.StatsResponse{
					TotalEntries: 100,
					ByType:       map[string]int{"task": 50, "plan": 30},
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.StatsResponse](t, resp)
				if body.TotalEntries != 100 {
					t.Errorf("totalEntries = %d, want %d", body.TotalEntries, 100)
				}
				if body.ByType["task"] != 50 {
					t.Errorf("byType[task] = %d, want %d", body.ByType["task"], 50)
				}
			},
		},
		{
			name:  "success with global=true",
			query: "?global=true",
			mockStats: func(ctx context.Context, global bool) (*types.StatsResponse, error) {
				if !global {
					return nil, fmt.Errorf("global = false, want true")
				}
				return &types.StatsResponse{
					TotalEntries: 200,
					ByType:       map[string]int{},
				}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:  "service error",
			query: "",
			mockStats: func(ctx context.Context, global bool) (*types.StatsResponse, error) {
				return nil, fmt.Errorf("stats unavailable")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{getStatsFunc: tt.mockStats}
			router := newHealthExtRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/stats" + tt.query)
			if err != nil {
				t.Fatalf("GET /stats%s failed: %v", tt.query, err)
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
// Orphans Tests
// =============================================================================

func TestHandleGetOrphans(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		mockOrphans func(ctx context.Context, entryType string, limit int) ([]types.BrainEntry, error)
		wantStatus  int
		checkBody   func(t *testing.T, resp *http.Response)
	}{
		{
			name:  "success with defaults",
			query: "",
			mockOrphans: func(ctx context.Context, entryType string, limit int) ([]types.BrainEntry, error) {
				if entryType != "" {
					return nil, fmt.Errorf("entryType = %q, want empty", entryType)
				}
				if limit != 0 {
					return nil, fmt.Errorf("limit = %d, want 0", limit)
				}
				return []types.BrainEntry{
					{ID: "orph1234", Path: "projects/default/plan/orphan.md", Title: "Orphan Entry", Type: "plan"},
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
			name:  "with type and limit",
			query: "?type=task&limit=5",
			mockOrphans: func(ctx context.Context, entryType string, limit int) ([]types.BrainEntry, error) {
				if entryType != "task" {
					return nil, fmt.Errorf("entryType = %q, want %q", entryType, "task")
				}
				if limit != 5 {
					return nil, fmt.Errorf("limit = %d, want %d", limit, 5)
				}
				return []types.BrainEntry{}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:  "service error",
			query: "",
			mockOrphans: func(ctx context.Context, entryType string, limit int) ([]types.BrainEntry, error) {
				return nil, fmt.Errorf("database error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{getOrphansFunc: tt.mockOrphans}
			router := newHealthExtRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/orphans" + tt.query)
			if err != nil {
				t.Fatalf("GET /orphans%s failed: %v", tt.query, err)
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
// Stale Tests
// =============================================================================

func TestHandleGetStale(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		mockStale  func(ctx context.Context, days int, entryType string, limit int) ([]types.BrainEntry, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name:  "success with defaults",
			query: "",
			mockStale: func(ctx context.Context, days int, entryType string, limit int) ([]types.BrainEntry, error) {
				if days != 30 {
					return nil, fmt.Errorf("days = %d, want %d", days, 30)
				}
				if entryType != "" {
					return nil, fmt.Errorf("entryType = %q, want empty", entryType)
				}
				return []types.BrainEntry{
					{ID: "stale123", Path: "projects/default/plan/stale.md", Title: "Stale Entry", Type: "plan"},
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
			name:  "with custom params",
			query: "?days=7&type=task&limit=10",
			mockStale: func(ctx context.Context, days int, entryType string, limit int) ([]types.BrainEntry, error) {
				if days != 7 {
					return nil, fmt.Errorf("days = %d, want %d", days, 7)
				}
				if entryType != "task" {
					return nil, fmt.Errorf("entryType = %q, want %q", entryType, "task")
				}
				if limit != 10 {
					return nil, fmt.Errorf("limit = %d, want %d", limit, 10)
				}
				return []types.BrainEntry{}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:  "service error",
			query: "",
			mockStale: func(ctx context.Context, days int, entryType string, limit int) ([]types.BrainEntry, error) {
				return nil, fmt.Errorf("database error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{getStaleFunc: tt.mockStale}
			router := newHealthExtRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/stale" + tt.query)
			if err != nil {
				t.Fatalf("GET /stale%s failed: %v", tt.query, err)
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
// Verify Tests
// =============================================================================

func TestHandleVerifyEntry(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		mockVerify func(ctx context.Context, path string) (*types.VerifyResponse, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success",
			id:   "abc12def",
			mockVerify: func(ctx context.Context, path string) (*types.VerifyResponse, error) {
				if path != "abc12def" {
					return nil, fmt.Errorf("path = %q, want %q", path, "abc12def")
				}
				return &types.VerifyResponse{
					Success:    true,
					Path:       "projects/default/plan/test.md",
					VerifiedAt: "2025-01-15T10:30:00Z",
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.VerifyResponse](t, resp)
				if !body.Success {
					t.Error("expected success = true")
				}
				if body.Path != "projects/default/plan/test.md" {
					t.Errorf("path = %q, want %q", body.Path, "projects/default/plan/test.md")
				}
				if body.VerifiedAt == "" {
					t.Error("verified_at should not be empty")
				}
			},
		},
		{
			name: "not found",
			id:   "notexist",
			mockVerify: func(ctx context.Context, path string) (*types.VerifyResponse, error) {
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
			mockVerify: func(ctx context.Context, path string) (*types.VerifyResponse, error) {
				return nil, fmt.Errorf("database error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{verifyFunc: tt.mockVerify}
			router := newHealthExtRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Post(srv.URL+"/entries/"+tt.id+"/verify", "application/json", nil)
			if err != nil {
				t.Fatalf("POST /entries/%s/verify failed: %v", tt.id, err)
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
// Generate Link Tests
// =============================================================================

func TestHandleGenerateLink(t *testing.T) {
	tests := []struct {
		name        string
		body        any
		mockGenLink func(ctx context.Context, req types.LinkRequest) (*types.LinkResponse, error)
		wantStatus  int
		checkBody   func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success",
			body: map[string]any{
				"path":      "projects/brain-api/plan/abc.md",
				"title":     "My Plan",
				"withTitle": true,
			},
			mockGenLink: func(ctx context.Context, req types.LinkRequest) (*types.LinkResponse, error) {
				if req.Path != "projects/brain-api/plan/abc.md" {
					return nil, fmt.Errorf("path = %q, want %q", req.Path, "projects/brain-api/plan/abc.md")
				}
				return &types.LinkResponse{
					Link: "[My Plan](abc12def)",
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.LinkResponse](t, resp)
				if body.Link != "[My Plan](abc12def)" {
					t.Errorf("link = %q, want %q", body.Link, "[My Plan](abc12def)")
				}
			},
		},
		{
			name:       "missing path",
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
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Bad Request" {
					t.Errorf("error = %q, want %q", body.Error, "Bad Request")
				}
			},
		},
		{
			name: "service error",
			body: map[string]any{"path": "projects/test/plan/abc.md"},
			mockGenLink: func(ctx context.Context, req types.LinkRequest) (*types.LinkResponse, error) {
				return nil, fmt.Errorf("link generation failed")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{generateLinkFunc: tt.mockGenLink}
			router := newHealthExtRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			var body *bytes.Buffer
			switch v := tt.body.(type) {
			case string:
				body = bytes.NewBufferString(v)
			default:
				body = jsonBody(t, v)
			}

			resp, err := http.Post(srv.URL+"/link", "application/json", body)
			if err != nil {
				t.Fatalf("POST /link failed: %v", err)
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

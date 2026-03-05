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
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Mock BrainService
// =============================================================================

type mockBrainService struct {
	saveFunc         func(ctx context.Context, req types.CreateEntryRequest) (*types.CreateEntryResponse, error)
	recallFunc       func(ctx context.Context, pathOrID string) (*types.BrainEntry, error)
	updateFunc       func(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error)
	deleteFunc       func(ctx context.Context, pathOrID string) error
	listFunc         func(ctx context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error)
	moveFunc         func(ctx context.Context, pathOrID string, targetProject string) (*types.MoveResult, error)
	searchFunc       func(ctx context.Context, req types.SearchRequest) (*types.SearchResponse, error)
	injectFunc       func(ctx context.Context, req types.InjectRequest) (*types.InjectResponse, error)
	getBacklinksFunc func(ctx context.Context, path string) ([]types.BrainEntry, error)
	getOutlinksFunc  func(ctx context.Context, path string) ([]types.BrainEntry, error)
	getRelatedFunc   func(ctx context.Context, path string, limit int) ([]types.BrainEntry, error)
	getSectionsFunc  func(ctx context.Context, path string) (*types.SectionsResponse, error)
	getSectionFunc   func(ctx context.Context, path string, title string, includeSubsections bool) (*types.SectionContentResponse, error)
	getStatsFunc     func(ctx context.Context, global bool) (*types.StatsResponse, error)
	getOrphansFunc   func(ctx context.Context, entryType string, limit int) ([]types.BrainEntry, error)
	getStaleFunc     func(ctx context.Context, days int, entryType string, limit int) ([]types.BrainEntry, error)
	verifyFunc       func(ctx context.Context, path string) (*types.VerifyResponse, error)
	generateLinkFunc func(ctx context.Context, req types.LinkRequest) (*types.LinkResponse, error)
}

func (m *mockBrainService) Save(ctx context.Context, req types.CreateEntryRequest) (*types.CreateEntryResponse, error) {
	if m.saveFunc != nil {
		return m.saveFunc(ctx, req)
	}
	return nil, fmt.Errorf("saveFunc not set")
}

func (m *mockBrainService) Recall(ctx context.Context, pathOrID string) (*types.BrainEntry, error) {
	if m.recallFunc != nil {
		return m.recallFunc(ctx, pathOrID)
	}
	return nil, fmt.Errorf("recallFunc not set")
}

func (m *mockBrainService) Update(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, pathOrID, req)
	}
	return nil, fmt.Errorf("updateFunc not set")
}

func (m *mockBrainService) Delete(ctx context.Context, pathOrID string) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, pathOrID)
	}
	return fmt.Errorf("deleteFunc not set")
}

func (m *mockBrainService) List(ctx context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, req)
	}
	return nil, fmt.Errorf("listFunc not set")
}

func (m *mockBrainService) Move(ctx context.Context, pathOrID string, targetProject string) (*types.MoveResult, error) {
	if m.moveFunc != nil {
		return m.moveFunc(ctx, pathOrID, targetProject)
	}
	return nil, fmt.Errorf("moveFunc not set")
}

func (m *mockBrainService) Search(ctx context.Context, req types.SearchRequest) (*types.SearchResponse, error) {
	if m.searchFunc != nil {
		return m.searchFunc(ctx, req)
	}
	return nil, fmt.Errorf("searchFunc not set")
}

func (m *mockBrainService) Inject(ctx context.Context, req types.InjectRequest) (*types.InjectResponse, error) {
	if m.injectFunc != nil {
		return m.injectFunc(ctx, req)
	}
	return nil, fmt.Errorf("injectFunc not set")
}

func (m *mockBrainService) GetBacklinks(ctx context.Context, path string) ([]types.BrainEntry, error) {
	if m.getBacklinksFunc != nil {
		return m.getBacklinksFunc(ctx, path)
	}
	return nil, fmt.Errorf("getBacklinksFunc not set")
}

func (m *mockBrainService) GetOutlinks(ctx context.Context, path string) ([]types.BrainEntry, error) {
	if m.getOutlinksFunc != nil {
		return m.getOutlinksFunc(ctx, path)
	}
	return nil, fmt.Errorf("getOutlinksFunc not set")
}

func (m *mockBrainService) GetRelated(ctx context.Context, path string, limit int) ([]types.BrainEntry, error) {
	if m.getRelatedFunc != nil {
		return m.getRelatedFunc(ctx, path, limit)
	}
	return nil, fmt.Errorf("getRelatedFunc not set")
}

func (m *mockBrainService) GetSections(ctx context.Context, path string) (*types.SectionsResponse, error) {
	if m.getSectionsFunc != nil {
		return m.getSectionsFunc(ctx, path)
	}
	return nil, fmt.Errorf("getSectionsFunc not set")
}

func (m *mockBrainService) GetSection(ctx context.Context, path string, title string, includeSubsections bool) (*types.SectionContentResponse, error) {
	if m.getSectionFunc != nil {
		return m.getSectionFunc(ctx, path, title, includeSubsections)
	}
	return nil, fmt.Errorf("getSectionFunc not set")
}

func (m *mockBrainService) GetStats(ctx context.Context, global bool) (*types.StatsResponse, error) {
	if m.getStatsFunc != nil {
		return m.getStatsFunc(ctx, global)
	}
	return nil, fmt.Errorf("getStatsFunc not set")
}

func (m *mockBrainService) GetOrphans(ctx context.Context, entryType string, limit int) ([]types.BrainEntry, error) {
	if m.getOrphansFunc != nil {
		return m.getOrphansFunc(ctx, entryType, limit)
	}
	return nil, fmt.Errorf("getOrphansFunc not set")
}

func (m *mockBrainService) GetStale(ctx context.Context, days int, entryType string, limit int) ([]types.BrainEntry, error) {
	if m.getStaleFunc != nil {
		return m.getStaleFunc(ctx, days, entryType, limit)
	}
	return nil, fmt.Errorf("getStaleFunc not set")
}

func (m *mockBrainService) Verify(ctx context.Context, path string) (*types.VerifyResponse, error) {
	if m.verifyFunc != nil {
		return m.verifyFunc(ctx, path)
	}
	return nil, fmt.Errorf("verifyFunc not set")
}

func (m *mockBrainService) GenerateLink(ctx context.Context, req types.LinkRequest) (*types.LinkResponse, error) {
	if m.generateLinkFunc != nil {
		return m.generateLinkFunc(ctx, req)
	}
	return nil, fmt.Errorf("generateLinkFunc not set")
}

// =============================================================================
// Test Helpers
// =============================================================================

// newTestRouter creates a chi router with entry handlers wired to the given mock.
func newTestRouter(mock *mockBrainService) *chi.Mux {
	h := NewHandler(mock)
	r := chi.NewRouter()
	r.Route("/entries", func(r chi.Router) {
		r.Post("/", h.HandleCreateEntry)
		r.Get("/", h.HandleListEntries)
		r.Post("/{id}/move", h.HandleMoveEntry)
		// Wildcard routes must be last to allow specific routes to match first
		r.Get("/*", h.HandleGetEntry)
		r.Patch("/*", h.HandleUpdateEntry)
		r.Delete("/*", h.HandleDeleteEntry)
	})
	return r
}

func jsonBody(t *testing.T, v any) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(v); err != nil {
		t.Fatalf("failed to encode JSON body: %v", err)
	}
	return buf
}

func decodeJSON[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	return v
}

// =============================================================================
// Create Entry Tests
// =============================================================================

func TestHandleCreateEntry(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		mockSave   func(ctx context.Context, req types.CreateEntryRequest) (*types.CreateEntryResponse, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success",
			body: map[string]any{
				"type":    "plan",
				"title":   "My Plan",
				"content": "Plan content here",
				"tags":    []string{"go", "api"},
			},
			mockSave: func(ctx context.Context, req types.CreateEntryRequest) (*types.CreateEntryResponse, error) {
				return &types.CreateEntryResponse{
					ID:     "abc12def",
					Path:   "projects/default/plan/my-plan.md",
					Title:  req.Title,
					Type:   req.Type,
					Status: "active",
					Link:   "[My Plan](abc12def)",
				}, nil
			},
			wantStatus: http.StatusCreated,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.CreateEntryResponse](t, resp)
				if body.ID != "abc12def" {
					t.Errorf("id = %q, want %q", body.ID, "abc12def")
				}
				if body.Type != "plan" {
					t.Errorf("type = %q, want %q", body.Type, "plan")
				}
				if body.Title != "My Plan" {
					t.Errorf("title = %q, want %q", body.Title, "My Plan")
				}
				if body.Status != "active" {
					t.Errorf("status = %q, want %q", body.Status, "active")
				}
			},
		},
		{
			name:       "missing required fields",
			body:       map[string]any{},
			wantStatus: http.StatusBadRequest,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Validation Error" {
					t.Errorf("error = %q, want %q", body.Error, "Validation Error")
				}
				if len(body.Details) < 3 {
					t.Errorf("expected at least 3 validation details (type, title, content), got %d", len(body.Details))
				}
				// Check that all three required fields are mentioned
				fields := make(map[string]bool)
				for _, d := range body.Details {
					fields[d.Field] = true
				}
				for _, f := range []string{"type", "title", "content"} {
					if !fields[f] {
						t.Errorf("expected validation detail for field %q", f)
					}
				}
			},
		},
		{
			name: "invalid type",
			body: map[string]any{
				"type":    "invalid_type",
				"title":   "Test",
				"content": "Content",
			},
			wantStatus: http.StatusBadRequest,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Validation Error" {
					t.Errorf("error = %q, want %q", body.Error, "Validation Error")
				}
				if len(body.Details) == 0 {
					t.Fatal("expected validation details")
				}
				if body.Details[0].Field != "type" {
					t.Errorf("field = %q, want %q", body.Details[0].Field, "type")
				}
			},
		},
		{
			name: "invalid status",
			body: map[string]any{
				"type":    "plan",
				"title":   "Test",
				"content": "Content",
				"status":  "bogus",
			},
			wantStatus: http.StatusBadRequest,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Validation Error" {
					t.Errorf("error = %q, want %q", body.Error, "Validation Error")
				}
				found := false
				for _, d := range body.Details {
					if d.Field == "status" {
						found = true
					}
				}
				if !found {
					t.Error("expected validation detail for field 'status'")
				}
			},
		},
		{
			name: "invalid priority",
			body: map[string]any{
				"type":     "task",
				"title":    "Test",
				"content":  "Content",
				"priority": "critical",
			},
			wantStatus: http.StatusBadRequest,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				found := false
				for _, d := range body.Details {
					if d.Field == "priority" {
						found = true
					}
				}
				if !found {
					t.Error("expected validation detail for field 'priority'")
				}
			},
		},
		{
			name: "invalid merge_policy",
			body: map[string]any{
				"type":         "task",
				"title":        "Test",
				"content":      "Content",
				"merge_policy": "yolo",
			},
			wantStatus: http.StatusBadRequest,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				found := false
				for _, d := range body.Details {
					if d.Field == "merge_policy" {
						found = true
					}
				}
				if !found {
					t.Error("expected validation detail for field 'merge_policy'")
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
			body: map[string]any{
				"type":    "plan",
				"title":   "Test",
				"content": "Content",
			},
			mockSave: func(ctx context.Context, req types.CreateEntryRequest) (*types.CreateEntryResponse, error) {
				return nil, fmt.Errorf("disk full")
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
			mock := &mockBrainService{saveFunc: tt.mockSave}
			router := newTestRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			var body *bytes.Buffer
			switch v := tt.body.(type) {
			case string:
				body = bytes.NewBufferString(v)
			default:
				body = jsonBody(t, v)
			}

			resp, err := http.Post(srv.URL+"/entries", "application/json", body)
			if err != nil {
				t.Fatalf("POST /entries failed: %v", err)
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
// Get Entry Tests
// =============================================================================

func TestHandleGetEntry(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		mockRecall func(ctx context.Context, pathOrID string) (*types.BrainEntry, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success by ID",
			id:   "abc12def",
			mockRecall: func(ctx context.Context, pathOrID string) (*types.BrainEntry, error) {
				if pathOrID != "abc12def" {
					return nil, fmt.Errorf("unexpected pathOrID: %s", pathOrID)
				}
				return &types.BrainEntry{
					ID:      "abc12def",
					Path:    "projects/default/plan/test.md",
					Title:   "Test Entry",
					Type:    "plan",
					Status:  "active",
					Content: "Some content",
					Tags:    []string{"go"},
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.BrainEntry](t, resp)
				if body.ID != "abc12def" {
					t.Errorf("id = %q, want %q", body.ID, "abc12def")
				}
				if body.Title != "Test Entry" {
					t.Errorf("title = %q, want %q", body.Title, "Test Entry")
				}
			},
		},
		{
			name: "not found",
			id:   "notexist",
			mockRecall: func(ctx context.Context, pathOrID string) (*types.BrainEntry, error) {
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
			mockRecall: func(ctx context.Context, pathOrID string) (*types.BrainEntry, error) {
				return nil, fmt.Errorf("database error")
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "success by full path",
			id:   "projects/govpu/task/1bg4bj9y.md",
			mockRecall: func(ctx context.Context, pathOrID string) (*types.BrainEntry, error) {
				if pathOrID != "projects/govpu/task/1bg4bj9y.md" {
					return nil, fmt.Errorf("unexpected pathOrID: %s", pathOrID)
				}
				return &types.BrainEntry{
					ID:      "1bg4bj9y",
					Path:    "projects/govpu/task/1bg4bj9y.md",
					Title:   "Test Task",
					Type:    "task",
					Status:  "active",
					Content: "Task content",
					Tags:    []string{"test"},
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.BrainEntry](t, resp)
				if body.ID != "1bg4bj9y" {
					t.Errorf("id = %q, want %q", body.ID, "1bg4bj9y")
				}
				if body.Path != "projects/govpu/task/1bg4bj9y.md" {
					t.Errorf("path = %q, want %q", body.Path, "projects/govpu/task/1bg4bj9y.md")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{recallFunc: tt.mockRecall}
			router := newTestRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/entries/" + tt.id)
			if err != nil {
				t.Fatalf("GET /entries/%s failed: %v", tt.id, err)
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
// List Entries Tests
// =============================================================================

func TestHandleListEntries(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		mockList   func(ctx context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
		checkReq   func(t *testing.T, req types.ListEntriesRequest) // verify parsed query params
	}{
		{
			name:  "success with defaults",
			query: "",
			mockList: func(ctx context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
				return &types.ListEntriesResponse{
					Entries: []types.BrainEntry{
						{ID: "abc12def", Title: "Entry 1", Type: "plan", Status: "active"},
					},
					Total:  1,
					Limit:  20,
					Offset: 0,
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ListEntriesResponse](t, resp)
				if body.Total != 1 {
					t.Errorf("total = %d, want %d", body.Total, 1)
				}
				if len(body.Entries) != 1 {
					t.Errorf("entries count = %d, want %d", len(body.Entries), 1)
				}
			},
		},
		{
			name:  "with all query params",
			query: "?type=task&status=pending&feature_id=auth&filename=abc&tags=go,api&limit=10&offset=5&global=true&sortBy=modified",
			mockList: func(ctx context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
				// Verify all query params were parsed correctly
				if req.Type != "task" {
					return nil, fmt.Errorf("type = %q, want %q", req.Type, "task")
				}
				if req.Status != "pending" {
					return nil, fmt.Errorf("status = %q, want %q", req.Status, "pending")
				}
				if req.FeatureID != "auth" {
					return nil, fmt.Errorf("feature_id = %q, want %q", req.FeatureID, "auth")
				}
				if req.Filename != "abc" {
					return nil, fmt.Errorf("filename = %q, want %q", req.Filename, "abc")
				}
				if req.Tags != "go,api" {
					return nil, fmt.Errorf("tags = %q, want %q", req.Tags, "go,api")
				}
				if req.Limit != 10 {
					return nil, fmt.Errorf("limit = %d, want %d", req.Limit, 10)
				}
				if req.Offset != 5 {
					return nil, fmt.Errorf("offset = %d, want %d", req.Offset, 5)
				}
				if req.Global == nil || !*req.Global {
					return nil, fmt.Errorf("global = %v, want true", req.Global)
				}
				if req.SortBy != "modified" {
					return nil, fmt.Errorf("sortBy = %q, want %q", req.SortBy, "modified")
				}
				return &types.ListEntriesResponse{
					Entries: []types.BrainEntry{},
					Total:   0,
					Limit:   10,
					Offset:  5,
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ListEntriesResponse](t, resp)
				if body.Limit != 10 {
					t.Errorf("limit = %d, want %d", body.Limit, 10)
				}
				if body.Offset != 5 {
					t.Errorf("offset = %d, want %d", body.Offset, 5)
				}
			},
		},
		{
			name:  "invalid type filter",
			query: "?type=bogus",
			mockList: func(ctx context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
				return nil, fmt.Errorf("should not be called")
			},
			wantStatus: http.StatusBadRequest,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Validation Error" {
					t.Errorf("error = %q, want %q", body.Error, "Validation Error")
				}
			},
		},
		{
			name:  "invalid status filter",
			query: "?status=bogus",
			mockList: func(ctx context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
				return nil, fmt.Errorf("should not be called")
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:  "service error",
			query: "",
			mockList: func(ctx context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
				return nil, fmt.Errorf("database error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{listFunc: tt.mockList}
			router := newTestRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/entries" + tt.query)
			if err != nil {
				t.Fatalf("GET /entries%s failed: %v", tt.query, err)
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
// Update Entry Tests
// =============================================================================

func TestHandleUpdateEntry(t *testing.T) {
	completedStatus := "completed"
	newTitle := "Updated Title"

	tests := []struct {
		name       string
		id         string
		body       any
		mockUpdate func(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success - update status",
			id:   "abc12def",
			body: map[string]any{
				"status": "completed",
			},
			mockUpdate: func(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
				if pathOrID != "abc12def" {
					return nil, fmt.Errorf("unexpected pathOrID: %s", pathOrID)
				}
				if req.Status == nil || *req.Status != completedStatus {
					return nil, fmt.Errorf("status = %v, want %q", req.Status, completedStatus)
				}
				return &types.BrainEntry{
					ID:     "abc12def",
					Path:   "projects/default/plan/test.md",
					Title:  "Test Entry",
					Type:   "plan",
					Status: "completed",
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.BrainEntry](t, resp)
				if body.Status != "completed" {
					t.Errorf("status = %q, want %q", body.Status, "completed")
				}
			},
		},
		{
			name: "success - update title and append",
			id:   "abc12def",
			body: map[string]any{
				"title":  "Updated Title",
				"append": "## Progress\nDone step 1",
			},
			mockUpdate: func(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
				if req.Title == nil || *req.Title != newTitle {
					return nil, fmt.Errorf("title = %v, want %q", req.Title, newTitle)
				}
				if req.Append == nil || *req.Append != "## Progress\nDone step 1" {
					return nil, fmt.Errorf("append not set correctly")
				}
				return &types.BrainEntry{
					ID:    "abc12def",
					Title: "Updated Title",
					Type:  "plan",
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.BrainEntry](t, resp)
				if body.Title != "Updated Title" {
					t.Errorf("title = %q, want %q", body.Title, "Updated Title")
				}
			},
		},
		{
			name: "invalid status enum",
			id:   "abc12def",
			body: map[string]any{
				"status": "bogus",
			},
			wantStatus: http.StatusBadRequest,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Validation Error" {
					t.Errorf("error = %q, want %q", body.Error, "Validation Error")
				}
			},
		},
		{
			name: "invalid priority enum",
			id:   "abc12def",
			body: map[string]any{
				"priority": "critical",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "invalid merge_strategy enum",
			id:   "abc12def",
			body: map[string]any{
				"merge_strategy": "yolo",
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "not found",
			id:   "notexist",
			body: map[string]any{
				"status": "completed",
			},
			mockUpdate: func(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
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
			name:       "invalid JSON body",
			id:         "abc12def",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "service error",
			id:   "abc12def",
			body: map[string]any{
				"status": "completed",
			},
			mockUpdate: func(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
				return nil, fmt.Errorf("disk full")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{updateFunc: tt.mockUpdate}
			router := newTestRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			var body *bytes.Buffer
			switch v := tt.body.(type) {
			case string:
				body = bytes.NewBufferString(v)
			default:
				body = jsonBody(t, v)
			}

			req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/entries/"+tt.id, body)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("PATCH /entries/%s failed: %v", tt.id, err)
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
// Delete Entry Tests
// =============================================================================

func TestHandleDeleteEntry(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		query      string
		mockDelete func(ctx context.Context, pathOrID string) error
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name:  "success",
			id:    "abc12def",
			query: "?confirm=true",
			mockDelete: func(ctx context.Context, pathOrID string) error {
				if pathOrID != "abc12def" {
					return fmt.Errorf("unexpected pathOrID: %s", pathOrID)
				}
				return nil
			},
			wantStatus: http.StatusNoContent,
		},
		{
			name:       "missing confirm param",
			id:         "abc12def",
			query:      "",
			wantStatus: http.StatusBadRequest,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Bad Request" {
					t.Errorf("error = %q, want %q", body.Error, "Bad Request")
				}
			},
		},
		{
			name:       "confirm=false",
			id:         "abc12def",
			query:      "?confirm=false",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:  "not found",
			id:    "notexist",
			query: "?confirm=true",
			mockDelete: func(ctx context.Context, pathOrID string) error {
				return ErrNotFound
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
			name:  "service error",
			id:    "abc12def",
			query: "?confirm=true",
			mockDelete: func(ctx context.Context, pathOrID string) error {
				return fmt.Errorf("disk error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{deleteFunc: tt.mockDelete}
			router := newTestRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/entries/"+tt.id+tt.query, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("DELETE /entries/%s%s failed: %v", tt.id, tt.query, err)
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
// Move Entry Tests
// =============================================================================

func TestHandleMoveEntry(t *testing.T) {
	tests := []struct {
		name       string
		id         string
		body       any
		mockMove   func(ctx context.Context, pathOrID string, targetProject string) (*types.MoveResult, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success",
			id:   "abc12def",
			body: map[string]any{
				"project": "new-project",
			},
			mockMove: func(ctx context.Context, pathOrID string, targetProject string) (*types.MoveResult, error) {
				if pathOrID != "abc12def" {
					return nil, fmt.Errorf("unexpected pathOrID: %s", pathOrID)
				}
				if targetProject != "new-project" {
					return nil, fmt.Errorf("unexpected project: %s", targetProject)
				}
				return &types.MoveResult{
					Success: true,
					From:    "projects/old/plan/test.md",
					To:      "projects/new-project/plan/test.md",
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.MoveResult](t, resp)
				if !body.Success {
					t.Error("expected success = true")
				}
				if body.From != "projects/old/plan/test.md" {
					t.Errorf("from = %q, want %q", body.From, "projects/old/plan/test.md")
				}
				if body.To != "projects/new-project/plan/test.md" {
					t.Errorf("to = %q, want %q", body.To, "projects/new-project/plan/test.md")
				}
			},
		},
		{
			name:       "missing project field",
			id:         "abc12def",
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
			id:         "abc12def",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "not found",
			id:   "notexist",
			body: map[string]any{
				"project": "new-project",
			},
			mockMove: func(ctx context.Context, pathOrID string, targetProject string) (*types.MoveResult, error) {
				return nil, ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "service error",
			id:   "abc12def",
			body: map[string]any{
				"project": "new-project",
			},
			mockMove: func(ctx context.Context, pathOrID string, targetProject string) (*types.MoveResult, error) {
				return nil, fmt.Errorf("disk error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{moveFunc: tt.mockMove}
			router := newTestRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			var body *bytes.Buffer
			switch v := tt.body.(type) {
			case string:
				body = bytes.NewBufferString(v)
			default:
				body = jsonBody(t, v)
			}

			resp, err := http.Post(srv.URL+"/entries/"+tt.id+"/move", "application/json", body)
			if err != nil {
				t.Fatalf("POST /entries/%s/move failed: %v", tt.id, err)
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

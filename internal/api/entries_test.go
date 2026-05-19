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
	"github.com/huynle/brain-api/internal/realtime"
	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Mock BrainService
// =============================================================================

type mockBrainService struct {
	saveFunc           func(ctx context.Context, req types.CreateEntryRequest) (*types.CreateEntryResponse, error)
	recallFunc         func(ctx context.Context, pathOrID string) (*types.BrainEntry, error)
	recallFullFunc     func(ctx context.Context, pathOrID string) (string, error)
	updateFunc         func(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error)
	deleteFunc         func(ctx context.Context, pathOrID string) error
	listFunc           func(ctx context.Context, req types.ListEntriesRequest) (*types.ListEntriesResponse, error)
	moveFunc           func(ctx context.Context, pathOrID string, targetProject string) (*types.MoveResult, error)
	searchFunc         func(ctx context.Context, req types.SearchRequest) (*types.SearchResponse, error)
	injectFunc         func(ctx context.Context, req types.InjectRequest) (*types.InjectResponse, error)
	getBacklinksFunc   func(ctx context.Context, path string) ([]types.BrainEntry, error)
	getOutlinksFunc    func(ctx context.Context, path string) ([]types.BrainEntry, error)
	getRelatedFunc     func(ctx context.Context, path string, limit int) ([]types.BrainEntry, error)
	getSectionsFunc    func(ctx context.Context, path string) (*types.SectionsResponse, error)
	getSectionFunc     func(ctx context.Context, path string, title string, includeSubsections bool) (*types.SectionContentResponse, error)
	getStatsFunc       func(ctx context.Context, global bool) (*types.StatsResponse, error)
	getOrphansFunc     func(ctx context.Context, entryType string, limit int) ([]types.BrainEntry, error)
	getStaleFunc       func(ctx context.Context, days int, entryType string, limit int) ([]types.BrainEntry, error)
	verifyFunc         func(ctx context.Context, path string) (*types.VerifyResponse, error)
	generateLinkFunc   func(ctx context.Context, req types.LinkRequest) (*types.LinkResponse, error)
	bulkUpdateFunc     func(ctx context.Context, req types.BulkUpdateRequest) (*types.BulkUpdateResponse, error)
	updateMetadataFunc func(ctx context.Context, pathOrID string, fields map[string]interface{}) (*types.BrainEntry, error)
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

func (m *mockBrainService) RecallFull(ctx context.Context, pathOrID string) (string, error) {
	if m.recallFullFunc != nil {
		return m.recallFullFunc(ctx, pathOrID)
	}
	return "", fmt.Errorf("recallFullFunc not set")
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

func (m *mockBrainService) BulkUpdate(ctx context.Context, req types.BulkUpdateRequest) (*types.BulkUpdateResponse, error) {
	if m.bulkUpdateFunc != nil {
		return m.bulkUpdateFunc(ctx, req)
	}
	return nil, fmt.Errorf("bulkUpdateFunc not set")
}

func (m *mockBrainService) UpdateMetadata(ctx context.Context, pathOrID string, fields map[string]interface{}) (*types.BrainEntry, error) {
	if m.updateMetadataFunc != nil {
		return m.updateMetadataFunc(ctx, pathOrID, fields)
	}
	return nil, fmt.Errorf("updateMetadataFunc not set")
}

// =============================================================================
// Test Helpers
// =============================================================================

// newTestRouter creates a chi router with entry handlers wired to the given mock.
// Mirrors the real router's wildcard approach for POST (move/verify) routes.
func newTestRouter(mock *mockBrainService) *chi.Mux {
	h := NewHandler(mock)
	r := chi.NewRouter()
	r.Route("/entries", func(r chi.Router) {
		r.Post("/", h.HandleCreateEntry)
		r.Get("/", h.HandleListEntries)
		r.Post("/bulk-update", h.HandleBulkUpdate)
		// Wildcard routes must be last to allow specific routes to match first
		r.Get("/*", h.HandleGetEntry)
		r.Post("/*", h.HandlePostWildcard)
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
// Get Entry Content Negotiation Tests
// =============================================================================

func TestHandleGetEntry_ContentNegotiation(t *testing.T) {
	testEntry := &types.BrainEntry{
		ID:      "abc12def",
		Path:    "projects/default/plan/test.md",
		Title:   "Test Entry",
		Type:    "plan",
		Status:  "active",
		Content: "## Hello\n\nSome markdown content.",
		Tags:    []string{"go", "api"},
	}

	testFullContent := `---
title: Test Entry
type: plan
status: active
tags:
  - go
  - api
---
## Hello

Some markdown content.`

	tests := []struct {
		name           string
		id             string
		acceptHeader   string
		mockRecall     func(ctx context.Context, pathOrID string) (*types.BrainEntry, error)
		mockRecallFull func(ctx context.Context, pathOrID string) (string, error)
		wantStatus     int
		checkResp      func(t *testing.T, resp *http.Response)
	}{
		{
			name:         "Accept text/markdown returns raw markdown body",
			id:           "abc12def",
			acceptHeader: "text/markdown",
			mockRecall: func(ctx context.Context, pathOrID string) (*types.BrainEntry, error) {
				return testEntry, nil
			},
			wantStatus: http.StatusOK,
			checkResp: func(t *testing.T, resp *http.Response) {
				// Check Content-Type
				ct := resp.Header.Get("Content-Type")
				if ct != "text/markdown; charset=utf-8" {
					t.Errorf("Content-Type = %q, want %q", ct, "text/markdown; charset=utf-8")
				}
				// Check X-Brain-* headers
				if got := resp.Header.Get("X-Brain-Entry-Id"); got != "abc12def" {
					t.Errorf("X-Brain-Entry-Id = %q, want %q", got, "abc12def")
				}
				if got := resp.Header.Get("X-Brain-Entry-Path"); got != "projects/default/plan/test.md" {
					t.Errorf("X-Brain-Entry-Path = %q, want %q", got, "projects/default/plan/test.md")
				}
				if got := resp.Header.Get("X-Brain-Entry-Title"); got != "Test Entry" {
					t.Errorf("X-Brain-Entry-Title = %q, want %q", got, "Test Entry")
				}
				if got := resp.Header.Get("X-Brain-Entry-Status"); got != "active" {
					t.Errorf("X-Brain-Entry-Status = %q, want %q", got, "active")
				}
				if got := resp.Header.Get("X-Brain-Entry-Type"); got != "plan" {
					t.Errorf("X-Brain-Entry-Type = %q, want %q", got, "plan")
				}
				if got := resp.Header.Get("X-Brain-Entry-Tags"); got != "go,api" {
					t.Errorf("X-Brain-Entry-Tags = %q, want %q", got, "go,api")
				}
				// Check body is raw markdown
				var buf bytes.Buffer
				buf.ReadFrom(resp.Body)
				if buf.String() != "## Hello\n\nSome markdown content." {
					t.Errorf("body = %q, want raw markdown content", buf.String())
				}
			},
		},
		{
			name:         "Accept text/plain also returns raw markdown body",
			id:           "abc12def",
			acceptHeader: "text/plain",
			mockRecall: func(ctx context.Context, pathOrID string) (*types.BrainEntry, error) {
				return testEntry, nil
			},
			wantStatus: http.StatusOK,
			checkResp: func(t *testing.T, resp *http.Response) {
				ct := resp.Header.Get("Content-Type")
				if ct != "text/markdown; charset=utf-8" {
					t.Errorf("Content-Type = %q, want %q", ct, "text/markdown; charset=utf-8")
				}
				if got := resp.Header.Get("X-Brain-Entry-Id"); got != "abc12def" {
					t.Errorf("X-Brain-Entry-Id = %q, want %q", got, "abc12def")
				}
			},
		},
		{
			name:         "Accept text/markdown with no tags omits X-Brain-Entry-Tags",
			id:           "abc12def",
			acceptHeader: "text/markdown",
			mockRecall: func(ctx context.Context, pathOrID string) (*types.BrainEntry, error) {
				return &types.BrainEntry{
					ID:      "abc12def",
					Path:    "projects/default/plan/test.md",
					Title:   "No Tags",
					Type:    "plan",
					Status:  "active",
					Content: "content",
					Tags:    nil,
				}, nil
			},
			wantStatus: http.StatusOK,
			checkResp: func(t *testing.T, resp *http.Response) {
				if got := resp.Header.Get("X-Brain-Entry-Tags"); got != "" {
					t.Errorf("X-Brain-Entry-Tags = %q, want empty (no tags)", got)
				}
			},
		},
		{
			name:         "Accept text/x-brain-full returns full frontmatter + body",
			id:           "abc12def",
			acceptHeader: "text/x-brain-full",
			mockRecall: func(ctx context.Context, pathOrID string) (*types.BrainEntry, error) {
				return testEntry, nil
			},
			mockRecallFull: func(ctx context.Context, pathOrID string) (string, error) {
				return testFullContent, nil
			},
			wantStatus: http.StatusOK,
			checkResp: func(t *testing.T, resp *http.Response) {
				ct := resp.Header.Get("Content-Type")
				if ct != "text/x-brain-full; charset=utf-8" {
					t.Errorf("Content-Type = %q, want %q", ct, "text/x-brain-full; charset=utf-8")
				}
				if got := resp.Header.Get("X-Brain-Entry-Id"); got != "abc12def" {
					t.Errorf("X-Brain-Entry-Id = %q, want %q", got, "abc12def")
				}
				if got := resp.Header.Get("X-Brain-Entry-Path"); got != "projects/default/plan/test.md" {
					t.Errorf("X-Brain-Entry-Path = %q, want %q", got, "projects/default/plan/test.md")
				}
				var buf bytes.Buffer
				buf.ReadFrom(resp.Body)
				if buf.String() != testFullContent {
					t.Errorf("body = %q, want full frontmatter+body content", buf.String())
				}
			},
		},
		{
			name:         "Accept text/x-brain-full with RecallFull error returns 500",
			id:           "abc12def",
			acceptHeader: "text/x-brain-full",
			mockRecall: func(ctx context.Context, pathOrID string) (*types.BrainEntry, error) {
				return testEntry, nil
			},
			mockRecallFull: func(ctx context.Context, pathOrID string) (string, error) {
				return "", fmt.Errorf("file not found on disk")
			},
			wantStatus: http.StatusInternalServerError,
			checkResp: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Internal Server Error" {
					t.Errorf("error = %q, want %q", body.Error, "Internal Server Error")
				}
			},
		},
		{
			name:         "Default (no Accept header) returns JSON as before",
			id:           "abc12def",
			acceptHeader: "",
			mockRecall: func(ctx context.Context, pathOrID string) (*types.BrainEntry, error) {
				return testEntry, nil
			},
			wantStatus: http.StatusOK,
			checkResp: func(t *testing.T, resp *http.Response) {
				ct := resp.Header.Get("Content-Type")
				if ct != "application/json" {
					t.Errorf("Content-Type = %q, want %q", ct, "application/json")
				}
				body := decodeJSON[types.BrainEntry](t, resp)
				if body.ID != "abc12def" {
					t.Errorf("id = %q, want %q", body.ID, "abc12def")
				}
			},
		},
		{
			name:         "Accept application/json returns JSON",
			id:           "abc12def",
			acceptHeader: "application/json",
			mockRecall: func(ctx context.Context, pathOrID string) (*types.BrainEntry, error) {
				return testEntry, nil
			},
			wantStatus: http.StatusOK,
			checkResp: func(t *testing.T, resp *http.Response) {
				ct := resp.Header.Get("Content-Type")
				if ct != "application/json" {
					t.Errorf("Content-Type = %q, want %q", ct, "application/json")
				}
				body := decodeJSON[types.BrainEntry](t, resp)
				if body.ID != "abc12def" {
					t.Errorf("id = %q, want %q", body.ID, "abc12def")
				}
			},
		},
		{
			name:         "Not found still returns JSON error regardless of Accept header",
			id:           "notexist",
			acceptHeader: "text/markdown",
			mockRecall: func(ctx context.Context, pathOrID string) (*types.BrainEntry, error) {
				return nil, ErrNotFound
			},
			wantStatus: http.StatusNotFound,
			checkResp: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Not Found" {
					t.Errorf("error = %q, want %q", body.Error, "Not Found")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{
				recallFunc:     tt.mockRecall,
				recallFullFunc: tt.mockRecallFull,
			}
			router := newTestRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			req, _ := http.NewRequest(http.MethodGet, srv.URL+"/entries/"+tt.id, nil)
			if tt.acceptHeader != "" {
				req.Header.Set("Accept", tt.acceptHeader)
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("GET /entries/%s failed: %v", tt.id, err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			if tt.checkResp != nil {
				tt.checkResp(t, resp)
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
// Update Entry Content Negotiation Tests
// =============================================================================

func TestHandleUpdateEntry_ContentNegotiation(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		contentType string
		body        string
		mockUpdate  func(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error)
		wantStatus  int
		checkBody   func(t *testing.T, resp *http.Response)
	}{
		{
			name:        "text/markdown replaces content",
			id:          "abc12def",
			contentType: "text/markdown",
			body:        "# Updated Content\n\nThis is new markdown.",
			mockUpdate: func(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
				if pathOrID != "abc12def" {
					t.Errorf("pathOrID = %q, want %q", pathOrID, "abc12def")
				}
				if req.Content == nil {
					t.Fatal("expected Content to be set")
				}
				if *req.Content != "# Updated Content\n\nThis is new markdown." {
					t.Errorf("content = %q, want %q", *req.Content, "# Updated Content\n\nThis is new markdown.")
				}
				// Other fields should be nil
				if req.Title != nil {
					t.Errorf("expected Title to be nil, got %q", *req.Title)
				}
				if req.Status != nil {
					t.Errorf("expected Status to be nil, got %q", *req.Status)
				}
				return &types.BrainEntry{
					ID:      "abc12def",
					Path:    "projects/default/plan/test.md",
					Title:   "Test Entry",
					Type:    "plan",
					Status:  "active",
					Content: "# Updated Content\n\nThis is new markdown.",
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.BrainEntry](t, resp)
				if body.Content != "# Updated Content\n\nThis is new markdown." {
					t.Errorf("content = %q, want updated markdown", body.Content)
				}
			},
		},
		{
			name:        "text/plain replaces content",
			id:          "abc12def",
			contentType: "text/plain",
			body:        "Plain text content replacement",
			mockUpdate: func(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
				if req.Content == nil || *req.Content != "Plain text content replacement" {
					t.Errorf("content = %v, want %q", req.Content, "Plain text content replacement")
				}
				return &types.BrainEntry{
					ID:      "abc12def",
					Path:    "projects/default/plan/test.md",
					Title:   "Test Entry",
					Type:    "plan",
					Status:  "active",
					Content: "Plain text content replacement",
				}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:        "text/x-brain-full parses frontmatter and body",
			id:          "abc12def",
			contentType: "text/x-brain-full",
			body: `---
title: New Title
type: task
status: completed
tags:
  - api
  - go
priority: high
feature_id: auth-feature
git_branch: feature/auth
agent: tdd-dev
model: claude-sonnet
direct_prompt: Fix the auth bug
---
## Updated Body

This is the new content from a full brain file.`,
			mockUpdate: func(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
				if req.Title == nil || *req.Title != "New Title" {
					t.Errorf("Title = %v, want %q", req.Title, "New Title")
				}
				if req.Status == nil || *req.Status != "completed" {
					t.Errorf("Status = %v, want %q", req.Status, "completed")
				}
				if len(req.Tags) != 2 || req.Tags[0] != "api" || req.Tags[1] != "go" {
					t.Errorf("Tags = %v, want [api go]", req.Tags)
				}
				if req.Priority == nil || *req.Priority != "high" {
					t.Errorf("Priority = %v, want %q", req.Priority, "high")
				}
				if req.FeatureID == nil || *req.FeatureID != "auth-feature" {
					t.Errorf("FeatureID = %v, want %q", req.FeatureID, "auth-feature")
				}
				if req.GitBranch == nil || *req.GitBranch != "feature/auth" {
					t.Errorf("GitBranch = %v, want %q", req.GitBranch, "feature/auth")
				}
				if req.Agent == nil || *req.Agent != "tdd-dev" {
					t.Errorf("Agent = %v, want %q", req.Agent, "tdd-dev")
				}
				if req.Model == nil || *req.Model != "claude-sonnet" {
					t.Errorf("Model = %v, want %q", req.Model, "claude-sonnet")
				}
				if req.DirectPrompt == nil || *req.DirectPrompt != "Fix the auth bug" {
					t.Errorf("DirectPrompt = %v, want %q", req.DirectPrompt, "Fix the auth bug")
				}
				if req.Content == nil || *req.Content != "## Updated Body\n\nThis is the new content from a full brain file." {
					t.Errorf("Content = %v, want body text", req.Content)
				}
				return &types.BrainEntry{
					ID:      "abc12def",
					Path:    "projects/default/task/test.md",
					Title:   "New Title",
					Type:    "task",
					Status:  "completed",
					Content: "## Updated Body\n\nThis is the new content from a full brain file.",
					Tags:    []string{"api", "go"},
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.BrainEntry](t, resp)
				if body.Title != "New Title" {
					t.Errorf("title = %q, want %q", body.Title, "New Title")
				}
				if body.Status != "completed" {
					t.Errorf("status = %q, want %q", body.Status, "completed")
				}
			},
		},
		{
			name:        "text/x-brain-full with invalid frontmatter returns 400",
			id:          "abc12def",
			contentType: "text/x-brain-full",
			body:        "---\ninvalid: [yaml: broken\n---\nBody text",
			wantStatus:  http.StatusBadRequest,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Bad Request" {
					t.Errorf("error = %q, want %q", body.Error, "Bad Request")
				}
			},
		},
		{
			name:        "text/x-brain-full with only body (no frontmatter) sets content only",
			id:          "abc12def",
			contentType: "text/x-brain-full",
			body:        "Just a plain body with no frontmatter delimiters.",
			mockUpdate: func(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
				if req.Content == nil || *req.Content != "Just a plain body with no frontmatter delimiters." {
					t.Errorf("Content = %v, want body text", req.Content)
				}
				return &types.BrainEntry{
					ID:      "abc12def",
					Path:    "projects/default/plan/test.md",
					Title:   "Test Entry",
					Type:    "plan",
					Status:  "active",
					Content: "Just a plain body with no frontmatter delimiters.",
				}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:        "text/x-brain-full maps depends_on",
			id:          "abc12def",
			contentType: "text/x-brain-full",
			body: `---
title: Task with deps
type: task
status: pending
depends_on:
  - task-1
  - task-2
---
Content here`,
			mockUpdate: func(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
				if req.DependsOn == nil || len(*req.DependsOn) != 2 || (*req.DependsOn)[0] != "task-1" {
					t.Errorf("DependsOn = %v, want [task-1 task-2]", req.DependsOn)
				}
				return &types.BrainEntry{
					ID:    "abc12def",
					Path:  "projects/default/task/test.md",
					Title: "Task with deps",
					Type:  "task",
				}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:        "text/x-brain-full maps boolean fields",
			id:          "abc12def",
			contentType: "text/x-brain-full",
			body: `---
title: Bool test
type: task
status: active
schedule_enabled: true
complete_on_idle: false
open_pr_before_merge: true
---
Body`,
			mockUpdate: func(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
				if req.ScheduleEnabled == nil || *req.ScheduleEnabled != true {
					t.Errorf("ScheduleEnabled = %v, want true", req.ScheduleEnabled)
				}
				if req.CompleteOnIdle == nil || *req.CompleteOnIdle != false {
					t.Errorf("CompleteOnIdle = %v, want false", req.CompleteOnIdle)
				}
				if req.OpenPRBeforeMerge == nil || *req.OpenPRBeforeMerge != true {
					t.Errorf("OpenPRBeforeMerge = %v, want true", req.OpenPRBeforeMerge)
				}
				return &types.BrainEntry{
					ID:    "abc12def",
					Path:  "projects/default/task/test.md",
					Title: "Bool test",
					Type:  "task",
				}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:        "default JSON still works unchanged",
			id:          "abc12def",
			contentType: "application/json",
			body:        `{"status":"completed"}`,
			mockUpdate: func(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
				if req.Status == nil || *req.Status != "completed" {
					t.Errorf("Status = %v, want %q", req.Status, "completed")
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
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{updateFunc: tt.mockUpdate}
			router := newTestRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/entries/"+tt.id, bytes.NewBufferString(tt.body))
			req.Header.Set("Content-Type", tt.contentType)
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
					OldPath: "projects/old/plan/test.md",
					NewPath: "projects/new-project/plan/test.md",
					Project: "new-project",
					ID:      "abc12def",
					Title:   "Test Plan",
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
				// Verify client-compatible fields
				if body.OldPath != "projects/old/plan/test.md" {
					t.Errorf("oldPath = %q, want %q", body.OldPath, "projects/old/plan/test.md")
				}
				if body.NewPath != "projects/new-project/plan/test.md" {
					t.Errorf("newPath = %q, want %q", body.NewPath, "projects/new-project/plan/test.md")
				}
				if body.Project != "new-project" {
					t.Errorf("project = %q, want %q", body.Project, "new-project")
				}
				if body.ID != "abc12def" {
					t.Errorf("id = %q, want %q", body.ID, "abc12def")
				}
				if body.Title != "Test Plan" {
					t.Errorf("title = %q, want %q", body.Title, "Test Plan")
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
		{
			name: "success with full path containing slashes",
			id:   "projects/brain-api/task/q94q9lie.md",
			body: map[string]any{
				"project": "other-project",
			},
			mockMove: func(ctx context.Context, pathOrID string, targetProject string) (*types.MoveResult, error) {
				if pathOrID != "projects/brain-api/task/q94q9lie.md" {
					return nil, fmt.Errorf("unexpected pathOrID: %s", pathOrID)
				}
				if targetProject != "other-project" {
					return nil, fmt.Errorf("unexpected project: %s", targetProject)
				}
				return &types.MoveResult{
					Success: true,
					From:    "projects/brain-api/task/q94q9lie.md",
					To:      "projects/other-project/task/q94q9lie.md",
					OldPath: "projects/brain-api/task/q94q9lie.md",
					NewPath: "projects/other-project/task/q94q9lie.md",
					Project: "other-project",
					ID:      "q94q9lie",
					Title:   "Test Task",
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.MoveResult](t, resp)
				if !body.Success {
					t.Error("expected success = true")
				}
				if body.From != "projects/brain-api/task/q94q9lie.md" {
					t.Errorf("from = %q, want %q", body.From, "projects/brain-api/task/q94q9lie.md")
				}
				if body.To != "projects/other-project/task/q94q9lie.md" {
					t.Errorf("to = %q, want %q", body.To, "projects/other-project/task/q94q9lie.md")
				}
				if body.ID != "q94q9lie" {
					t.Errorf("id = %q, want %q", body.ID, "q94q9lie")
				}
			},
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

// =============================================================================
// Schedule Field Validation Tests (Create)
// =============================================================================

func TestHandleCreateEntry_ScheduleValidation(t *testing.T) {
	validSave := func(ctx context.Context, req types.CreateEntryRequest) (*types.CreateEntryResponse, error) {
		return &types.CreateEntryResponse{
			ID:     "abc12def",
			Path:   "projects/default/task/test.md",
			Title:  req.Title,
			Type:   req.Type,
			Status: "active",
		}, nil
	}

	tests := []struct {
		name       string
		body       map[string]any
		mockSave   func(ctx context.Context, req types.CreateEntryRequest) (*types.CreateEntryResponse, error)
		wantStatus int
		wantField  string // expected validation field name
	}{
		{
			name: "valid timezone accepted",
			body: map[string]any{
				"type": "task", "title": "Test", "content": "c",
				"timezone": "America/New_York",
			},
			mockSave:   validSave,
			wantStatus: http.StatusCreated,
		},
		{
			name: "invalid timezone rejected",
			body: map[string]any{
				"type": "task", "title": "Test", "content": "c",
				"timezone": "Mars/Olympus",
			},
			wantStatus: http.StatusBadRequest,
			wantField:  "timezone",
		},
		{
			name: "valid RFC3339 run_once_at accepted",
			body: map[string]any{
				"type": "task", "title": "Test", "content": "c",
				"run_once_at": "2025-06-15T10:00:00Z",
			},
			mockSave:   validSave,
			wantStatus: http.StatusCreated,
		},
		{
			name: "invalid run_once_at rejected",
			body: map[string]any{
				"type": "task", "title": "Test", "content": "c",
				"run_once_at": "not-a-timestamp",
			},
			wantStatus: http.StatusBadRequest,
			wantField:  "run_once_at",
		},
		{
			name: "invalid starts_at rejected",
			body: map[string]any{
				"type": "task", "title": "Test", "content": "c",
				"starts_at": "2025/06/15",
			},
			wantStatus: http.StatusBadRequest,
			wantField:  "starts_at",
		},
		{
			name: "invalid expires_at rejected",
			body: map[string]any{
				"type": "task", "title": "Test", "content": "c",
				"expires_at": "tomorrow",
			},
			wantStatus: http.StatusBadRequest,
			wantField:  "expires_at",
		},
		{
			name: "expires_at before starts_at rejected",
			body: map[string]any{
				"type": "task", "title": "Test", "content": "c",
				"starts_at":  "2025-06-15T10:00:00Z",
				"expires_at": "2025-06-14T10:00:00Z",
			},
			wantStatus: http.StatusBadRequest,
			wantField:  "expires_at",
		},
		{
			name: "expires_at equal to starts_at rejected",
			body: map[string]any{
				"type": "task", "title": "Test", "content": "c",
				"starts_at":  "2025-06-15T10:00:00Z",
				"expires_at": "2025-06-15T10:00:00Z",
			},
			wantStatus: http.StatusBadRequest,
			wantField:  "expires_at",
		},
		{
			name: "expires_at after starts_at accepted",
			body: map[string]any{
				"type": "task", "title": "Test", "content": "c",
				"starts_at":  "2025-06-15T10:00:00Z",
				"expires_at": "2025-06-16T10:00:00Z",
			},
			mockSave:   validSave,
			wantStatus: http.StatusCreated,
		},
		{
			name: "valid cron schedule accepted",
			body: map[string]any{
				"type": "task", "title": "Test", "content": "c",
				"schedule": "*/15 * * * *",
			},
			mockSave:   validSave,
			wantStatus: http.StatusCreated,
		},
		{
			name: "invalid cron schedule rejected",
			body: map[string]any{
				"type": "task", "title": "Test", "content": "c",
				"schedule": "not a cron",
			},
			wantStatus: http.StatusBadRequest,
			wantField:  "schedule",
		},
		{
			name: "starts_at alone without expires_at accepted",
			body: map[string]any{
				"type": "task", "title": "Test", "content": "c",
				"starts_at": "2025-06-15T10:00:00Z",
			},
			mockSave:   validSave,
			wantStatus: http.StatusCreated,
		},
		{
			name: "expires_at alone without starts_at accepted",
			body: map[string]any{
				"type": "task", "title": "Test", "content": "c",
				"expires_at": "2025-06-16T10:00:00Z",
			},
			mockSave:   validSave,
			wantStatus: http.StatusCreated,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{saveFunc: tt.mockSave}
			router := newTestRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Post(srv.URL+"/entries", "application/json", jsonBody(t, tt.body))
			if err != nil {
				t.Fatalf("POST /entries failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			if tt.wantField != "" && resp.StatusCode == http.StatusBadRequest {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Validation Error" {
					t.Errorf("error = %q, want %q", body.Error, "Validation Error")
				}
				found := false
				for _, d := range body.Details {
					if d.Field == tt.wantField {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected validation detail for field %q, got %v", tt.wantField, body.Details)
				}
			}
		})
	}
}

// =============================================================================
// Schedule Field Validation Tests (Update)
// =============================================================================

func TestHandleUpdateEntry_ScheduleValidation(t *testing.T) {
	validUpdate := func(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
		return &types.BrainEntry{
			ID:     "abc12def",
			Path:   "projects/default/task/test.md",
			Title:  "Test",
			Type:   "task",
			Status: "active",
		}, nil
	}

	tests := []struct {
		name       string
		body       map[string]any
		mockUpdate func(ctx context.Context, pathOrID string, req types.UpdateEntryRequest) (*types.BrainEntry, error)
		wantStatus int
		wantField  string
	}{
		{
			name: "valid timezone accepted",
			body: map[string]any{
				"timezone": "Europe/London",
			},
			mockUpdate: validUpdate,
			wantStatus: http.StatusOK,
		},
		{
			name: "invalid timezone rejected",
			body: map[string]any{
				"timezone": "Invalid/Zone",
			},
			wantStatus: http.StatusBadRequest,
			wantField:  "timezone",
		},
		{
			name: "valid run_once_at accepted",
			body: map[string]any{
				"run_once_at": "2025-12-25T00:00:00Z",
			},
			mockUpdate: validUpdate,
			wantStatus: http.StatusOK,
		},
		{
			name: "invalid run_once_at rejected",
			body: map[string]any{
				"run_once_at": "25-12-2025",
			},
			wantStatus: http.StatusBadRequest,
			wantField:  "run_once_at",
		},
		{
			name: "invalid starts_at rejected",
			body: map[string]any{
				"starts_at": "June 15, 2025",
			},
			wantStatus: http.StatusBadRequest,
			wantField:  "starts_at",
		},
		{
			name: "invalid expires_at rejected",
			body: map[string]any{
				"expires_at": "1234567890",
			},
			wantStatus: http.StatusBadRequest,
			wantField:  "expires_at",
		},
		{
			name: "expires_at must be after starts_at",
			body: map[string]any{
				"starts_at":  "2025-06-15T10:00:00Z",
				"expires_at": "2025-06-14T10:00:00Z",
			},
			wantStatus: http.StatusBadRequest,
			wantField:  "expires_at",
		},
		{
			name: "valid schedule accepted",
			body: map[string]any{
				"schedule": "0 9 * * 1-5",
			},
			mockUpdate: validUpdate,
			wantStatus: http.StatusOK,
		},
		{
			name: "invalid schedule rejected",
			body: map[string]any{
				"schedule": "every 5 minutes",
			},
			wantStatus: http.StatusBadRequest,
			wantField:  "schedule",
		},
		{
			name: "UTC timezone accepted",
			body: map[string]any{
				"timezone": "UTC",
			},
			mockUpdate: validUpdate,
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{updateFunc: tt.mockUpdate}
			router := newTestRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/entries/abc12def", jsonBody(t, tt.body))
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("PATCH /entries/abc12def failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.wantStatus {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}

			if tt.wantField != "" && resp.StatusCode == http.StatusBadRequest {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Validation Error" {
					t.Errorf("error = %q, want %q", body.Error, "Validation Error")
				}
				found := false
				for _, d := range body.Details {
					if d.Field == tt.wantField {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected validation detail for field %q, got %v", tt.wantField, body.Details)
				}
			}
		})
	}
}

func TestHandleBulkUpdate(t *testing.T) {
	completedStatus := "completed"

	tests := []struct {
		name           string
		body           any
		mockBulkUpdate func(ctx context.Context, req types.BulkUpdateRequest) (*types.BulkUpdateResponse, error)
		wantStatus     int
		checkBody      func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success - explicit entries mode",
			body: types.BulkUpdateRequest{
				Entries: []types.BulkUpdateEntry{
					{Path: "projects/myproj/task/abc.md", Updates: types.UpdateEntryRequest{Status: &completedStatus}},
				},
			},
			mockBulkUpdate: func(ctx context.Context, req types.BulkUpdateRequest) (*types.BulkUpdateResponse, error) {
				if len(req.Entries) != 1 {
					t.Errorf("expected 1 entry, got %d", len(req.Entries))
				}
				return &types.BulkUpdateResponse{
					Updated: 1,
					Total:   1,
					Results: []types.BulkUpdateResult{
						{Path: "projects/myproj/task/abc.md", ID: "abc12345", Title: "Test", Status: "ok"},
					},
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.BulkUpdateResponse](t, resp)
				if body.Updated != 1 {
					t.Errorf("updated = %d, want %d", body.Updated, 1)
				}
				if body.Total != 1 {
					t.Errorf("total = %d, want %d", body.Total, 1)
				}
				if len(body.Results) != 1 {
					t.Fatalf("results count = %d, want %d", len(body.Results), 1)
				}
				if body.Results[0].Status != "ok" {
					t.Errorf("result status = %q, want %q", body.Results[0].Status, "ok")
				}
			},
		},
		{
			name: "success - filter mode",
			body: types.BulkUpdateRequest{
				Filter:  &types.BulkUpdateFilter{Project: &completedStatus},
				Updates: &types.UpdateEntryRequest{Status: &completedStatus},
			},
			mockBulkUpdate: func(ctx context.Context, req types.BulkUpdateRequest) (*types.BulkUpdateResponse, error) {
				if req.Filter == nil {
					t.Error("expected filter to be set")
				}
				if req.Updates == nil {
					t.Error("expected updates to be set")
				}
				return &types.BulkUpdateResponse{
					Updated: 3,
					Total:   3,
					Results: []types.BulkUpdateResult{
						{Path: "projects/proj/task/a.md", ID: "a1234567", Status: "ok"},
						{Path: "projects/proj/task/b.md", ID: "b1234567", Status: "ok"},
						{Path: "projects/proj/task/c.md", ID: "c1234567", Status: "ok"},
					},
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.BulkUpdateResponse](t, resp)
				if body.Updated != 3 {
					t.Errorf("updated = %d, want %d", body.Updated, 3)
				}
			},
		},
		{
			name:       "validation error - neither filter nor entries",
			body:       types.BulkUpdateRequest{},
			wantStatus: http.StatusUnprocessableEntity,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Validation Error" {
					t.Errorf("error = %q, want %q", body.Error, "Validation Error")
				}
			},
		},
		{
			name: "validation error - both filter and entries",
			body: types.BulkUpdateRequest{
				Filter:  &types.BulkUpdateFilter{},
				Entries: []types.BulkUpdateEntry{{Path: "x"}},
			},
			wantStatus: http.StatusUnprocessableEntity,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Validation Error" {
					t.Errorf("error = %q, want %q", body.Error, "Validation Error")
				}
			},
		},
		{
			name: "validation error - filter without updates",
			body: types.BulkUpdateRequest{
				Filter: &types.BulkUpdateFilter{},
			},
			wantStatus: http.StatusUnprocessableEntity,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Validation Error" {
					t.Errorf("error = %q, want %q", body.Error, "Validation Error")
				}
			},
		},
		{
			name: "validation error - invalid status in updates",
			body: map[string]any{
				"entries": []map[string]any{
					{
						"path": "projects/x/task/y.md",
						"updates": map[string]any{
							"status": "bogus",
						},
					},
				},
			},
			wantStatus: http.StatusUnprocessableEntity,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				if body.Error != "Validation Error" {
					t.Errorf("error = %q, want %q", body.Error, "Validation Error")
				}
				found := false
				for _, d := range body.Details {
					if d.Field == "entries[0].updates.status" {
						found = true
					}
				}
				if !found {
					t.Errorf("expected validation detail for field 'entries[0].updates.status', got %+v", body.Details)
				}
			},
		},
		{
			name: "validation error - invalid priority in filter-mode updates",
			body: map[string]any{
				"filter": map[string]any{
					"project": "myproj",
				},
				"updates": map[string]any{
					"priority": "critical",
				},
			},
			wantStatus: http.StatusUnprocessableEntity,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				found := false
				for _, d := range body.Details {
					if d.Field == "updates.priority" {
						found = true
					}
				}
				if !found {
					t.Errorf("expected validation detail for field 'updates.priority', got %+v", body.Details)
				}
			},
		},
		{
			name: "validation error - invalid merge_policy in entry updates",
			body: map[string]any{
				"entries": []map[string]any{
					{
						"path": "projects/x/task/y.md",
						"updates": map[string]any{
							"merge_policy": "yolo",
						},
					},
				},
			},
			wantStatus: http.StatusUnprocessableEntity,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.ErrorResponse](t, resp)
				found := false
				for _, d := range body.Details {
					if d.Field == "entries[0].updates.merge_policy" {
						found = true
					}
				}
				if !found {
					t.Errorf("expected validation detail for field 'entries[0].updates.merge_policy', got %+v", body.Details)
				}
			},
		},
		{
			name: "dry run - returns 200 without SSE (via mock verifying service called)",
			body: types.BulkUpdateRequest{
				Entries: []types.BulkUpdateEntry{
					{Path: "projects/myproj/task/abc.md", Updates: types.UpdateEntryRequest{Status: &completedStatus}},
				},
				DryRun: true,
			},
			mockBulkUpdate: func(ctx context.Context, req types.BulkUpdateRequest) (*types.BulkUpdateResponse, error) {
				if !req.DryRun {
					t.Error("expected DryRun to be true")
				}
				return &types.BulkUpdateResponse{
					Updated: 1,
					Total:   1,
					DryRun:  true,
					Results: []types.BulkUpdateResult{
						{Path: "projects/myproj/task/abc.md", ID: "abc12345", Status: "ok"},
					},
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.BulkUpdateResponse](t, resp)
				if !body.DryRun {
					t.Error("expected dry_run = true in response")
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
			body: types.BulkUpdateRequest{
				Entries: []types.BulkUpdateEntry{
					{Path: "projects/myproj/task/abc.md", Updates: types.UpdateEntryRequest{Status: &completedStatus}},
				},
			},
			mockBulkUpdate: func(ctx context.Context, req types.BulkUpdateRequest) (*types.BulkUpdateResponse, error) {
				return nil, fmt.Errorf("database error")
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
			mock := &mockBrainService{bulkUpdateFunc: tt.mockBulkUpdate}
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

			resp, err := http.Post(srv.URL+"/entries/bulk-update", "application/json", body)
			if err != nil {
				t.Fatalf("POST /entries/bulk-update failed: %v", err)
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
// Event Emission Tests
// =============================================================================

// newTestRouterWithEvents creates a chi router with entry handlers wired to
// both a mock BrainService and a mock EventService for testing event emission.
func newTestRouterWithEvents(mock *mockBrainService, es *mockEventService) *chi.Mux {
	hub := realtime.NewHub()
	h := NewHandler(mock,
		WithEventService(es),
		WithHub(hub),
	)
	r := chi.NewRouter()
	r.Route("/entries", func(r chi.Router) {
		r.Post("/", h.HandleCreateEntry)
		r.Get("/", h.HandleListEntries)
		r.Post("/{id}/move", h.HandleMoveEntry)
		r.Post("/bulk-update", h.HandleBulkUpdate)
		r.Get("/*", h.HandleGetEntry)
		r.Patch("/*", h.HandleUpdateOrMetadata)
		r.Delete("/*", h.HandleDeleteEntry)
	})
	return r
}

func TestHandleCreateEntry_EmitsEvent(t *testing.T) {
	es := &mockEventService{}
	mock := &mockBrainService{
		saveFunc: func(_ context.Context, req types.CreateEntryRequest) (*types.CreateEntryResponse, error) {
			return &types.CreateEntryResponse{
				ID:    "abc12def",
				Path:  "projects/myproj/plan/test.md",
				Title: req.Title,
				Type:  req.Type,
			}, nil
		},
	}
	router := newTestRouterWithEvents(mock, es)
	srv := httptest.NewServer(router)
	defer srv.Close()

	body := jsonBody(t, map[string]any{
		"type":    "plan",
		"title":   "My Plan",
		"content": "Plan content",
	})
	resp, err := http.Post(srv.URL+"/entries", "application/json", body)
	if err != nil {
		t.Fatalf("POST /entries failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	es.mu.Lock()
	defer es.mu.Unlock()
	if len(es.ingested) != 1 {
		t.Fatalf("ingested events = %d, want 1", len(es.ingested))
	}
	evt := es.ingested[0]
	if evt.Type != types.EventEntryCreated {
		t.Errorf("event type = %q, want %q", evt.Type, types.EventEntryCreated)
	}
	if evt.Source != types.EventSourceAPI {
		t.Errorf("event source = %q, want %q", evt.Source, types.EventSourceAPI)
	}
	if evt.ProjectID != "myproj" {
		t.Errorf("project_id = %q, want %q", evt.ProjectID, "myproj")
	}
	if evt.Metadata["entry_type"] != "plan" {
		t.Errorf("metadata[entry_type] = %q, want %q", evt.Metadata["entry_type"], "plan")
	}
}

func TestHandleUpdateEntry_EmitsEvents(t *testing.T) {
	es := &mockEventService{}
	mock := &mockBrainService{
		recallFunc: func(_ context.Context, _ string) (*types.BrainEntry, error) {
			return &types.BrainEntry{
				ID:        "abc12def",
				Path:      "projects/myproj/task/abc12def.md",
				Type:      "task",
				Status:    "pending",
				Title:     "Test Task",
				FeatureID: "feat-1",
			}, nil
		},
		updateFunc: func(_ context.Context, _ string, req types.UpdateEntryRequest) (*types.BrainEntry, error) {
			status := "pending"
			if req.Status != nil {
				status = *req.Status
			}
			return &types.BrainEntry{
				ID:        "abc12def",
				Path:      "projects/myproj/task/abc12def.md",
				Type:      "task",
				Status:    status,
				Title:     "Test Task",
				FeatureID: "feat-1",
			}, nil
		},
	}
	router := newTestRouterWithEvents(mock, es)
	srv := httptest.NewServer(router)
	defer srv.Close()

	body := jsonBody(t, map[string]any{
		"status": "completed",
	})
	req, _ := http.NewRequest("PATCH", srv.URL+"/entries/abc12def", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /entries/abc12def failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	es.mu.Lock()
	defer es.mu.Unlock()

	// Should emit both entry.updated and task.status_changed
	if len(es.ingested) != 2 {
		t.Fatalf("ingested events = %d, want 2", len(es.ingested))
	}

	// First event: entry.updated
	if es.ingested[0].Type != types.EventEntryUpdated {
		t.Errorf("event[0].type = %q, want %q", es.ingested[0].Type, types.EventEntryUpdated)
	}
	if es.ingested[0].Source != types.EventSourceAPI {
		t.Errorf("event[0].source = %q, want %q", es.ingested[0].Source, types.EventSourceAPI)
	}
	if es.ingested[0].TaskID != "abc12def" {
		t.Errorf("event[0].task_id = %q, want %q", es.ingested[0].TaskID, "abc12def")
	}

	// Second event: task.status_changed
	if es.ingested[1].Type != types.EventTaskStatusChanged {
		t.Errorf("event[1].type = %q, want %q", es.ingested[1].Type, types.EventTaskStatusChanged)
	}
	if es.ingested[1].FromStatus != "pending" {
		t.Errorf("event[1].from_status = %q, want %q", es.ingested[1].FromStatus, "pending")
	}
	if es.ingested[1].ToStatus != "completed" {
		t.Errorf("event[1].to_status = %q, want %q", es.ingested[1].ToStatus, "completed")
	}
	if es.ingested[1].FeatureID != "feat-1" {
		t.Errorf("event[1].feature_id = %q, want %q", es.ingested[1].FeatureID, "feat-1")
	}
}

func TestHandleDeleteEntry_EmitsEvent(t *testing.T) {
	es := &mockEventService{}
	mock := &mockBrainService{
		recallFunc: func(_ context.Context, _ string) (*types.BrainEntry, error) {
			return &types.BrainEntry{
				ID:    "abc12def",
				Path:  "projects/myproj/task/abc12def.md",
				Type:  "task",
				Title: "Test Task",
			}, nil
		},
		deleteFunc: func(_ context.Context, _ string) error {
			return nil
		},
	}
	router := newTestRouterWithEvents(mock, es)
	srv := httptest.NewServer(router)
	defer srv.Close()

	req, _ := http.NewRequest("DELETE", srv.URL+"/entries/projects/myproj/task/abc12def.md?confirm=true", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	es.mu.Lock()
	defer es.mu.Unlock()
	if len(es.ingested) != 1 {
		t.Fatalf("ingested events = %d, want 1", len(es.ingested))
	}
	evt := es.ingested[0]
	if evt.Type != types.EventEntryDeleted {
		t.Errorf("event type = %q, want %q", evt.Type, types.EventEntryDeleted)
	}
	if evt.Source != types.EventSourceAPI {
		t.Errorf("event source = %q, want %q", evt.Source, types.EventSourceAPI)
	}
	if evt.TaskID != "abc12def" {
		t.Errorf("task_id = %q, want %q", evt.TaskID, "abc12def")
	}
}

func TestHandleUpdateMetadata_ChecksFeatureCompletionOnTaskStatusChange(t *testing.T) {
	es := &mockEventService{}
	mock := &mockBrainService{
		updateMetadataFunc: func(_ context.Context, pathOrID string, fields map[string]interface{}) (*types.BrainEntry, error) {
			if pathOrID != "projects/myproj/task/abc12def.md" {
				t.Fatalf("pathOrID = %q, want task path", pathOrID)
			}
			if fields["status"] != "completed" {
				t.Fatalf("status field = %v, want completed", fields["status"])
			}
			return &types.BrainEntry{
				ID:        "abc12def",
				Path:      "projects/myproj/task/abc12def.md",
				Type:      "task",
				Status:    "completed",
				Title:     "Test Task",
				FeatureID: "feat-1",
			}, nil
		},
	}
	router := newTestRouterWithEvents(mock, es)
	srv := httptest.NewServer(router)
	defer srv.Close()

	body := jsonBody(t, map[string]any{
		"status": "completed",
	})
	req, _ := http.NewRequest("PATCH", srv.URL+"/entries/projects/myproj/task/abc12def.md/metadata", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /entries/.../metadata failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	es.mu.Lock()
	defer es.mu.Unlock()
	if len(es.checks) != 1 {
		t.Fatalf("feature completion checks = %d, want 1", len(es.checks))
	}
	check := es.checks[0]
	if check.ProjectID != "myproj" || check.FeatureID != "feat-1" || check.TaskID != "abc12def" {
		t.Fatalf("feature completion check = %+v, want myproj/feat-1/abc12def", check)
	}
}

func TestHandleUpdateEntry_NoStatusChange_EmitsSingleEvent(t *testing.T) {
	es := &mockEventService{}
	mock := &mockBrainService{
		updateFunc: func(_ context.Context, _ string, _ types.UpdateEntryRequest) (*types.BrainEntry, error) {
			return &types.BrainEntry{
				ID:    "abc12def",
				Path:  "projects/myproj/plan/abc12def.md",
				Type:  "plan",
				Title: "Updated Plan",
			}, nil
		},
	}
	router := newTestRouterWithEvents(mock, es)
	srv := httptest.NewServer(router)
	defer srv.Close()

	body := jsonBody(t, map[string]any{
		"title": "Updated Plan",
	})
	req, _ := http.NewRequest("PATCH", srv.URL+"/entries/abc12def", body)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH /entries/abc12def failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	es.mu.Lock()
	defer es.mu.Unlock()

	// Should emit only entry.updated (no status change, no task.status_changed)
	if len(es.ingested) != 1 {
		t.Fatalf("ingested events = %d, want 1", len(es.ingested))
	}
	if es.ingested[0].Type != types.EventEntryUpdated {
		t.Errorf("event type = %q, want %q", es.ingested[0].Type, types.EventEntryUpdated)
	}
}

func TestNoEventsForReadOnly(t *testing.T) {
	es := &mockEventService{}
	mock := &mockBrainService{
		recallFunc: func(_ context.Context, _ string) (*types.BrainEntry, error) {
			return &types.BrainEntry{
				ID:    "abc12def",
				Path:  "projects/myproj/plan/abc12def.md",
				Type:  "plan",
				Title: "Test Plan",
			}, nil
		},
		listFunc: func(_ context.Context, _ types.ListEntriesRequest) (*types.ListEntriesResponse, error) {
			return &types.ListEntriesResponse{Entries: []types.BrainEntry{}}, nil
		},
	}
	router := newTestRouterWithEvents(mock, es)
	srv := httptest.NewServer(router)
	defer srv.Close()

	// GET entry
	resp, err := http.Get(srv.URL + "/entries/abc12def")
	if err != nil {
		t.Fatalf("GET /entries/abc12def failed: %v", err)
	}
	resp.Body.Close()

	// GET list
	resp, err = http.Get(srv.URL + "/entries")
	if err != nil {
		t.Fatalf("GET /entries failed: %v", err)
	}
	resp.Body.Close()

	es.mu.Lock()
	defer es.mu.Unlock()
	if len(es.ingested) != 0 {
		t.Errorf("ingested events = %d, want 0 for read-only operations", len(es.ingested))
	}
}

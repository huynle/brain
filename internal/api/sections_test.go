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

// newSectionsRouter creates a chi router with section handlers wired to the given mock.
func newSectionsRouter(mock *mockBrainService) *chi.Mux {
	h := NewHandler(mock)
	r := chi.NewRouter()
	r.Get("/entries/{id}/sections", h.HandleGetSections)
	r.Get("/entries/{id}/sections/{title}", h.HandleGetSection)
	return r
}

// =============================================================================
// List Sections Tests
// =============================================================================

func TestHandleGetSections(t *testing.T) {
	tests := []struct {
		name         string
		id           string
		mockSections func(ctx context.Context, path string) (*types.SectionsResponse, error)
		wantStatus   int
		checkBody    func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success",
			id:   "abc12def",
			mockSections: func(ctx context.Context, path string) (*types.SectionsResponse, error) {
				if path != "abc12def" {
					return nil, fmt.Errorf("path = %q, want %q", path, "abc12def")
				}
				return &types.SectionsResponse{
					Sections: []types.SectionHeader{
						{Title: "Overview", Level: 2},
						{Title: "Implementation", Level: 2},
						{Title: "Details", Level: 3},
					},
					Path: "projects/default/plan/test.md",
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.SectionsResponse](t, resp)
				if len(body.Sections) != 3 {
					t.Fatalf("sections count = %d, want %d", len(body.Sections), 3)
				}
				if body.Sections[0].Title != "Overview" {
					t.Errorf("first section title = %q, want %q", body.Sections[0].Title, "Overview")
				}
				if body.Sections[0].Level != 2 {
					t.Errorf("first section level = %d, want %d", body.Sections[0].Level, 2)
				}
				if body.Path != "projects/default/plan/test.md" {
					t.Errorf("path = %q, want %q", body.Path, "projects/default/plan/test.md")
				}
			},
		},
		{
			name: "not found",
			id:   "notexist",
			mockSections: func(ctx context.Context, path string) (*types.SectionsResponse, error) {
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
			mockSections: func(ctx context.Context, path string) (*types.SectionsResponse, error) {
				return nil, fmt.Errorf("parse error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{getSectionsFunc: tt.mockSections}
			router := newSectionsRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/entries/" + tt.id + "/sections")
			if err != nil {
				t.Fatalf("GET /entries/%s/sections failed: %v", tt.id, err)
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
// Get Section Content Tests
// =============================================================================

func TestHandleGetSection(t *testing.T) {
	tests := []struct {
		name        string
		id          string
		title       string
		query       string
		mockSection func(ctx context.Context, path string, title string, includeSubsections bool) (*types.SectionContentResponse, error)
		wantStatus  int
		checkBody   func(t *testing.T, resp *http.Response)
	}{
		{
			name:  "success without subsections",
			id:    "abc12def",
			title: "Overview",
			query: "",
			mockSection: func(ctx context.Context, path string, title string, includeSubsections bool) (*types.SectionContentResponse, error) {
				if path != "abc12def" {
					return nil, fmt.Errorf("path = %q, want %q", path, "abc12def")
				}
				if title != "Overview" {
					return nil, fmt.Errorf("title = %q, want %q", title, "Overview")
				}
				if includeSubsections {
					return nil, fmt.Errorf("includeSubsections = true, want false")
				}
				return &types.SectionContentResponse{
					Title:              "Overview",
					Content:            "## Overview\n\nThis is the overview section.",
					Path:               "projects/default/plan/test.md",
					IncludeSubsections: false,
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.SectionContentResponse](t, resp)
				if body.Title != "Overview" {
					t.Errorf("title = %q, want %q", body.Title, "Overview")
				}
				if body.Content == "" {
					t.Error("content should not be empty")
				}
				if body.IncludeSubsections {
					t.Error("includeSubsections should be false")
				}
			},
		},
		{
			name:  "success with subsections",
			id:    "abc12def",
			title: "Overview",
			query: "?includeSubsections=true",
			mockSection: func(ctx context.Context, path string, title string, includeSubsections bool) (*types.SectionContentResponse, error) {
				if !includeSubsections {
					return nil, fmt.Errorf("includeSubsections = false, want true")
				}
				return &types.SectionContentResponse{
					Title:              "Overview",
					Content:            "## Overview\n\nContent\n\n### Details\n\nMore content",
					Path:               "projects/default/plan/test.md",
					IncludeSubsections: true,
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				body := decodeJSON[types.SectionContentResponse](t, resp)
				if !body.IncludeSubsections {
					t.Error("includeSubsections should be true")
				}
			},
		},
		{
			name:  "not found",
			id:    "notexist",
			title: "Overview",
			query: "",
			mockSection: func(ctx context.Context, path string, title string, includeSubsections bool) (*types.SectionContentResponse, error) {
				return nil, ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:  "service error",
			id:    "abc12def",
			title: "Overview",
			query: "",
			mockSection: func(ctx context.Context, path string, title string, includeSubsections bool) (*types.SectionContentResponse, error) {
				return nil, fmt.Errorf("parse error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockBrainService{getSectionFunc: tt.mockSection}
			router := newSectionsRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/entries/" + tt.id + "/sections/" + tt.title + tt.query)
			if err != nil {
				t.Fatalf("GET /entries/%s/sections/%s%s failed: %v", tt.id, tt.title, tt.query, err)
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

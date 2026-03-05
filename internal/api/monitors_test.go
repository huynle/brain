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
// Mock MonitorService
// =============================================================================

type mockMonitorService struct {
	listTemplatesFunc func() []types.MonitorTemplate
	listFunc          func(ctx context.Context, filter *types.MonitorListFilter) ([]types.MonitorInfo, error)
	createFunc        func(ctx context.Context, templateID string, scope types.MonitorScope, opts *types.CreateMonitorOptions) (*types.CreateMonitorResult, error)
	toggleFunc        func(ctx context.Context, taskID string, enabled bool) (string, error)
	deleteFunc        func(ctx context.Context, taskID string) (string, error)
}

func (m *mockMonitorService) ListTemplates() []types.MonitorTemplate {
	if m.listTemplatesFunc != nil {
		return m.listTemplatesFunc()
	}
	return nil
}

func (m *mockMonitorService) List(ctx context.Context, filter *types.MonitorListFilter) ([]types.MonitorInfo, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, filter)
	}
	return nil, fmt.Errorf("listFunc not set")
}

func (m *mockMonitorService) Create(ctx context.Context, templateID string, scope types.MonitorScope, opts *types.CreateMonitorOptions) (*types.CreateMonitorResult, error) {
	if m.createFunc != nil {
		return m.createFunc(ctx, templateID, scope, opts)
	}
	return nil, fmt.Errorf("createFunc not set")
}

func (m *mockMonitorService) Toggle(ctx context.Context, taskID string, enabled bool) (string, error) {
	if m.toggleFunc != nil {
		return m.toggleFunc(ctx, taskID, enabled)
	}
	return "", fmt.Errorf("toggleFunc not set")
}

func (m *mockMonitorService) Delete(ctx context.Context, taskID string) (string, error) {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, taskID)
	}
	return "", fmt.Errorf("deleteFunc not set")
}

// =============================================================================
// Test Helpers
// =============================================================================

func newMonitorTestRouter(mock *mockMonitorService) *chi.Mux {
	h := NewHandler(&mockBrainService{}, WithMonitorService(mock))
	r := chi.NewRouter()
	r.Route("/monitors", func(r chi.Router) {
		r.Get("/templates", h.HandleListMonitorTemplates)
		r.Get("/", h.HandleListMonitors)
		r.Post("/", h.HandleCreateMonitor)
		r.Patch("/{taskId}/toggle", h.HandleToggleMonitor)
		r.Delete("/{taskId}", h.HandleDeleteMonitor)
	})
	return r
}

// =============================================================================
// List Templates Tests
// =============================================================================

func TestHandleListMonitorTemplates(t *testing.T) {
	tests := []struct {
		name              string
		mockListTemplates func() []types.MonitorTemplate
		wantStatus        int
		checkBody         func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success",
			mockListTemplates: func() []types.MonitorTemplate {
				return []types.MonitorTemplate{
					{
						ID:              "blocked-inspector",
						Label:           "Blocked Task Inspector",
						Description:     "Checks for blocked tasks",
						DefaultSchedule: "*/15 * * * *",
						Tags:            []string{"scheduled"},
					},
				}
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				var body types.MonitorTemplatesResponse
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode: %v", err)
				}
				if len(body.Templates) != 1 {
					t.Fatalf("templates count = %d, want 1", len(body.Templates))
				}
				if body.Templates[0].ID != "blocked-inspector" {
					t.Errorf("id = %q, want %q", body.Templates[0].ID, "blocked-inspector")
				}
			},
		},
		{
			name: "empty templates returns empty array",
			mockListTemplates: func() []types.MonitorTemplate {
				return nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				var body types.MonitorTemplatesResponse
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode: %v", err)
				}
				if body.Templates == nil {
					t.Error("expected non-nil templates array (empty, not null)")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockMonitorService{listTemplatesFunc: tt.mockListTemplates}
			router := newMonitorTestRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/monitors/templates")
			if err != nil {
				t.Fatalf("GET /monitors/templates failed: %v", err)
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
// List Monitors Tests
// =============================================================================

func TestHandleListMonitors(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		mockList   func(ctx context.Context, filter *types.MonitorListFilter) ([]types.MonitorInfo, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name:  "success with no filters",
			query: "",
			mockList: func(ctx context.Context, filter *types.MonitorListFilter) ([]types.MonitorInfo, error) {
				return []types.MonitorInfo{
					{
						ID:         "abc12def",
						Path:       "projects/test/task/monitor.md",
						TemplateID: "blocked-inspector",
						Scope:      types.MonitorScope{Type: "project", Project: "test"},
						Enabled:    true,
						Schedule:   "*/15 * * * *",
						Title:      "Monitor: Blocked Task Inspector (project test)",
					},
				}, nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				var body types.MonitorListResponse
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode: %v", err)
				}
				if len(body.Monitors) != 1 {
					t.Fatalf("monitors count = %d, want 1", len(body.Monitors))
				}
				if body.Monitors[0].ID != "abc12def" {
					t.Errorf("id = %q, want %q", body.Monitors[0].ID, "abc12def")
				}
			},
		},
		{
			name:  "with project filter",
			query: "?project=myproject",
			mockList: func(ctx context.Context, filter *types.MonitorListFilter) ([]types.MonitorInfo, error) {
				if filter == nil || filter.Project != "myproject" {
					return nil, fmt.Errorf("expected project filter 'myproject', got %+v", filter)
				}
				return []types.MonitorInfo{}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:  "with feature_id filter",
			query: "?feature_id=auth",
			mockList: func(ctx context.Context, filter *types.MonitorListFilter) ([]types.MonitorInfo, error) {
				if filter == nil || filter.FeatureID != "auth" {
					return nil, fmt.Errorf("expected feature_id filter 'auth', got %+v", filter)
				}
				return []types.MonitorInfo{}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:  "with template_id filter",
			query: "?template_id=blocked-inspector",
			mockList: func(ctx context.Context, filter *types.MonitorListFilter) ([]types.MonitorInfo, error) {
				if filter == nil || filter.TemplateID != "blocked-inspector" {
					return nil, fmt.Errorf("expected template_id filter 'blocked-inspector', got %+v", filter)
				}
				return []types.MonitorInfo{}, nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:  "service error",
			query: "",
			mockList: func(ctx context.Context, filter *types.MonitorListFilter) ([]types.MonitorInfo, error) {
				return nil, fmt.Errorf("database error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockMonitorService{listFunc: tt.mockList}
			router := newMonitorTestRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/monitors" + tt.query)
			if err != nil {
				t.Fatalf("GET /monitors%s failed: %v", tt.query, err)
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
// Create Monitor Tests
// =============================================================================

func TestHandleCreateMonitor(t *testing.T) {
	tests := []struct {
		name       string
		body       any
		mockCreate func(ctx context.Context, templateID string, scope types.MonitorScope, opts *types.CreateMonitorOptions) (*types.CreateMonitorResult, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name: "success - project scope",
			body: map[string]any{
				"template_id": "blocked-inspector",
				"project":     "myproject",
				"scope_type":  "project",
			},
			mockCreate: func(ctx context.Context, templateID string, scope types.MonitorScope, opts *types.CreateMonitorOptions) (*types.CreateMonitorResult, error) {
				if templateID != "blocked-inspector" {
					return nil, fmt.Errorf("templateID = %q, want %q", templateID, "blocked-inspector")
				}
				if scope.Type != "project" {
					return nil, fmt.Errorf("scope.Type = %q, want %q", scope.Type, "project")
				}
				if scope.Project != "myproject" {
					return nil, fmt.Errorf("scope.Project = %q, want %q", scope.Project, "myproject")
				}
				return &types.CreateMonitorResult{
					ID:    "abc12def",
					Path:  "projects/myproject/task/monitor.md",
					Title: "Monitor: Blocked Task Inspector (project myproject)",
				}, nil
			},
			wantStatus: http.StatusCreated,
			checkBody: func(t *testing.T, resp *http.Response) {
				var body types.CreateMonitorResult
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode: %v", err)
				}
				if body.ID != "abc12def" {
					t.Errorf("id = %q, want %q", body.ID, "abc12def")
				}
			},
		},
		{
			name: "success - feature scope",
			body: map[string]any{
				"template_id": "feature-review",
				"project":     "myproject",
				"feature_id":  "auth",
				"scope_type":  "feature",
			},
			mockCreate: func(ctx context.Context, templateID string, scope types.MonitorScope, opts *types.CreateMonitorOptions) (*types.CreateMonitorResult, error) {
				if scope.Type != "feature" {
					return nil, fmt.Errorf("scope.Type = %q, want %q", scope.Type, "feature")
				}
				if scope.FeatureID != "auth" {
					return nil, fmt.Errorf("scope.FeatureID = %q, want %q", scope.FeatureID, "auth")
				}
				return &types.CreateMonitorResult{
					ID:    "xyz98765",
					Path:  "projects/myproject/task/monitor.md",
					Title: "Monitor: Feature Code Review (feature auth)",
				}, nil
			},
			wantStatus: http.StatusCreated,
		},
		{
			name: "success - with schedule",
			body: map[string]any{
				"template_id": "blocked-inspector",
				"project":     "myproject",
				"scope_type":  "project",
				"schedule":    "*/30 * * * *",
			},
			mockCreate: func(ctx context.Context, templateID string, scope types.MonitorScope, opts *types.CreateMonitorOptions) (*types.CreateMonitorResult, error) {
				if opts == nil || opts.Schedule != "*/30 * * * *" {
					return nil, fmt.Errorf("expected schedule '*/30 * * * *', got %+v", opts)
				}
				return &types.CreateMonitorResult{
					ID:    "abc12def",
					Path:  "projects/myproject/task/monitor.md",
					Title: "Monitor: Blocked Task Inspector (project myproject)",
				}, nil
			},
			wantStatus: http.StatusCreated,
		},
		{
			name:       "missing template_id",
			body:       map[string]any{"project": "myproject", "scope_type": "project"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "missing scope_type",
			body:       map[string]any{"template_id": "blocked-inspector", "project": "myproject"},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid JSON body",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "service error - already exists",
			body: map[string]any{
				"template_id": "blocked-inspector",
				"project":     "myproject",
				"scope_type":  "project",
			},
			mockCreate: func(ctx context.Context, templateID string, scope types.MonitorScope, opts *types.CreateMonitorOptions) (*types.CreateMonitorResult, error) {
				return nil, fmt.Errorf("monitor already exists for template %q with this scope (id: abc12def)", templateID)
			},
			wantStatus: http.StatusConflict,
		},
		{
			name: "service error - unknown template",
			body: map[string]any{
				"template_id": "nonexistent",
				"project":     "myproject",
				"scope_type":  "project",
			},
			mockCreate: func(ctx context.Context, templateID string, scope types.MonitorScope, opts *types.CreateMonitorOptions) (*types.CreateMonitorResult, error) {
				return nil, fmt.Errorf("unknown monitor template: nonexistent")
			},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "service error - generic",
			body: map[string]any{
				"template_id": "blocked-inspector",
				"project":     "myproject",
				"scope_type":  "project",
			},
			mockCreate: func(ctx context.Context, templateID string, scope types.MonitorScope, opts *types.CreateMonitorOptions) (*types.CreateMonitorResult, error) {
				return nil, fmt.Errorf("database error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockMonitorService{createFunc: tt.mockCreate}
			router := newMonitorTestRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			var body *bytes.Buffer
			switch v := tt.body.(type) {
			case string:
				body = bytes.NewBufferString(v)
			default:
				body = jsonBody(t, v)
			}

			resp, err := http.Post(srv.URL+"/monitors", "application/json", body)
			if err != nil {
				t.Fatalf("POST /monitors failed: %v", err)
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
// Toggle Monitor Tests
// =============================================================================

func TestHandleToggleMonitor(t *testing.T) {
	tests := []struct {
		name       string
		taskID     string
		body       any
		mockToggle func(ctx context.Context, taskID string, enabled bool) (string, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name:   "success - enable",
			taskID: "abc12def",
			body:   map[string]any{"enabled": true},
			mockToggle: func(ctx context.Context, taskID string, enabled bool) (string, error) {
				if taskID != "abc12def" {
					return "", fmt.Errorf("taskID = %q, want %q", taskID, "abc12def")
				}
				if !enabled {
					return "", fmt.Errorf("enabled = false, want true")
				}
				return "projects/test/task/monitor.md", nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				var body types.MonitorToggleResponse
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode: %v", err)
				}
				if !body.Success {
					t.Error("expected success = true")
				}
				if body.Path != "projects/test/task/monitor.md" {
					t.Errorf("path = %q, want %q", body.Path, "projects/test/task/monitor.md")
				}
			},
		},
		{
			name:   "success - disable",
			taskID: "abc12def",
			body:   map[string]any{"enabled": false},
			mockToggle: func(ctx context.Context, taskID string, enabled bool) (string, error) {
				if enabled {
					return "", fmt.Errorf("enabled = true, want false")
				}
				return "projects/test/task/monitor.md", nil
			},
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid JSON body",
			taskID:     "abc12def",
			body:       "not json",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "not found",
			taskID: "notexist",
			body:   map[string]any{"enabled": true},
			mockToggle: func(ctx context.Context, taskID string, enabled bool) (string, error) {
				return "", ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:   "service error",
			taskID: "abc12def",
			body:   map[string]any{"enabled": true},
			mockToggle: func(ctx context.Context, taskID string, enabled bool) (string, error) {
				return "", fmt.Errorf("database error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockMonitorService{toggleFunc: tt.mockToggle}
			router := newMonitorTestRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			var body *bytes.Buffer
			switch v := tt.body.(type) {
			case string:
				body = bytes.NewBufferString(v)
			default:
				body = jsonBody(t, v)
			}

			req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/monitors/"+tt.taskID+"/toggle", body)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("PATCH /monitors/%s/toggle failed: %v", tt.taskID, err)
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
// Delete Monitor Tests
// =============================================================================

func TestHandleDeleteMonitor(t *testing.T) {
	tests := []struct {
		name       string
		taskID     string
		mockDelete func(ctx context.Context, taskID string) (string, error)
		wantStatus int
		checkBody  func(t *testing.T, resp *http.Response)
	}{
		{
			name:   "success",
			taskID: "abc12def",
			mockDelete: func(ctx context.Context, taskID string) (string, error) {
				if taskID != "abc12def" {
					return "", fmt.Errorf("taskID = %q, want %q", taskID, "abc12def")
				}
				return "projects/test/task/monitor.md", nil
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, resp *http.Response) {
				var body types.MonitorDeleteResponse
				if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
					t.Fatalf("failed to decode: %v", err)
				}
				if !body.Success {
					t.Error("expected success = true")
				}
				if body.Path != "projects/test/task/monitor.md" {
					t.Errorf("path = %q, want %q", body.Path, "projects/test/task/monitor.md")
				}
			},
		},
		{
			name:   "not found",
			taskID: "notexist",
			mockDelete: func(ctx context.Context, taskID string) (string, error) {
				return "", ErrNotFound
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name:   "service error",
			taskID: "abc12def",
			mockDelete: func(ctx context.Context, taskID string) (string, error) {
				return "", fmt.Errorf("database error")
			},
			wantStatus: http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockMonitorService{deleteFunc: tt.mockDelete}
			router := newMonitorTestRouter(mock)
			srv := httptest.NewServer(router)
			defer srv.Close()

			req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/monitors/"+tt.taskID, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("DELETE /monitors/%s failed: %v", tt.taskID, err)
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

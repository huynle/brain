package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// =============================================================================
// Tool Registration Tests
// =============================================================================

func TestRegisterBrainTools_Count(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterBrainTools(s, client)

	// Count registered tools
	count := len(s.tools)
	if count != 22 {
		t.Errorf("expected 22 brain tools registered, got %d", count)
	}
}

func TestRegisterBrainTools_Names(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterBrainTools(s, client)

	expectedTools := []string{
		"brain_save",
		"brain_recall",
		"brain_search",
		"brain_list",
		"brain_inject",
		"brain_update",
		"brain_bulk_update",
		"brain_delete",
		"brain_move",
		"brain_stats",
		"brain_check_connection",
		"brain_link",
		"brain_section",
		"brain_plan_sections",
		"brain_verify",
		"brain_stale",
		"brain_orphans",
		"brain_backlinks",
		"brain_outlinks",
		"brain_related",
		"brain_automation_list",
		"brain_automation_test",
	}

	for _, name := range expectedTools {
		if _, ok := s.tools[name]; !ok {
			t.Errorf("tool %q not registered", name)
		}
	}
}

func TestRegisterBrainTools_Schemas(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterBrainTools(s, client)

	// Verify brain_save has required fields
	saveTool := s.tools["brain_save"].tool
	if len(saveTool.InputSchema.Required) != 3 {
		t.Errorf("brain_save required fields = %d, want 3", len(saveTool.InputSchema.Required))
	}
	for _, req := range []string{"type", "title", "content"} {
		found := false
		for _, r := range saveTool.InputSchema.Required {
			if r == req {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("brain_save missing required field %q", req)
		}
	}

	// Verify brain_save has type enum
	typeProp, ok := saveTool.InputSchema.Properties["type"]
	if !ok {
		t.Fatal("brain_save missing 'type' property")
	}
	if len(typeProp.Enum) != 14 {
		t.Errorf("brain_save type enum has %d values, want 14", len(typeProp.Enum))
	}

	// Verify brain_search has required query
	searchTool := s.tools["brain_search"].tool
	if len(searchTool.InputSchema.Required) != 1 || searchTool.InputSchema.Required[0] != "query" {
		t.Errorf("brain_search required = %v, want [query]", searchTool.InputSchema.Required)
	}

	// Verify brain_delete has required path and confirm
	deleteTool := s.tools["brain_delete"].tool
	if len(deleteTool.InputSchema.Required) != 2 {
		t.Errorf("brain_delete required fields = %d, want 2", len(deleteTool.InputSchema.Required))
	}

	// Verify brain_update has required path
	updateTool := s.tools["brain_update"].tool
	if len(updateTool.InputSchema.Required) != 1 || updateTool.InputSchema.Required[0] != "path" {
		t.Errorf("brain_update required = %v, want [path]", updateTool.InputSchema.Required)
	}

	// Verify brain_move has required path and project
	moveTool := s.tools["brain_move"].tool
	if len(moveTool.InputSchema.Required) != 2 {
		t.Errorf("brain_move required fields = %d, want 2", len(moveTool.InputSchema.Required))
	}

	// Verify brain_list has no required fields
	listTool := s.tools["brain_list"].tool
	if len(listTool.InputSchema.Required) != 0 {
		t.Errorf("brain_list required = %v, want []", listTool.InputSchema.Required)
	}

	// Verify brain_check_connection has no properties
	checkTool := s.tools["brain_check_connection"].tool
	if len(checkTool.InputSchema.Properties) != 0 {
		t.Errorf("brain_check_connection properties = %d, want 0", len(checkTool.InputSchema.Properties))
	}
}

// =============================================================================
// Handler Tests (with mock HTTP server)
// =============================================================================

func TestBrainSave_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/entries" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)

		if body["title"] != "Test Entry" {
			t.Errorf("title = %v, want %q", body["title"], "Test Entry")
		}
		if body["type"] != "summary" {
			t.Errorf("type = %v, want %q", body["type"], "summary")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id":     "abc12345",
			"path":   "projects/test/summary/abc12345.md",
			"title":  "Test Entry",
			"type":   "summary",
			"status": "active",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_save"].handler
	result, err := handler(context.Background(), map[string]any{
		"type":    "summary",
		"title":   "Test Entry",
		"content": "Some content",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "abc12345") {
		t.Errorf("result should contain ID, got: %s", result)
	}
	if !strings.Contains(result, "Test Entry") {
		t.Errorf("result should contain title, got: %s", result)
	}
}

func TestBrainRecall_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.HasPrefix(r.URL.Path, "/api/v1/entries/") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "abc12345",
			"path":    "projects/test/summary/abc12345.md",
			"title":   "Test Entry",
			"type":    "summary",
			"status":  "active",
			"content": "# Test\n\nSome content here",
			"tags":    []string{"go", "test"},
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_recall"].handler
	result, err := handler(context.Background(), map[string]any{
		"path": "projects/test/summary/abc12345.md",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Test Entry") {
		t.Errorf("result should contain title, got: %s", result)
	}
	if !strings.Contains(result, "Some content here") {
		t.Errorf("result should contain content, got: %s", result)
	}
}

func TestBrainSearch_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/search" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"id": "abc", "path": "p/summary/abc.md", "title": "Found Entry", "type": "summary", "status": "active", "snippet": "matching text"},
			},
			"total": 1,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_search"].handler
	result, err := handler(context.Background(), map[string]any{
		"query": "test",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Found Entry") {
		t.Errorf("result should contain entry title, got: %s", result)
	}
	if !strings.Contains(result, "1 entries") {
		t.Errorf("result should contain count, got: %s", result)
	}
}

func TestBrainCheckConnection_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_check_connection"].handler
	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "CONNECTED") {
		t.Errorf("result should contain CONNECTED, got: %s", result)
	}
}

func TestBrainCheckConnection_Unavailable(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:1") // Will fail to connect
	RegisterBrainTools(s, client)

	handler := s.tools["brain_check_connection"].handler
	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "UNAVAILABLE") {
		t.Errorf("result should contain UNAVAILABLE, got: %s", result)
	}
}

func TestBrainList_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/entries" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		// Verify query params
		if r.URL.Query().Get("type") != "task" {
			t.Errorf("query type = %q, want %q", r.URL.Query().Get("type"), "task")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"entries": []map[string]any{
				{"id": "abc", "path": "p/task/abc.md", "title": "Task 1", "type": "task", "status": "pending"},
			},
			"total": 1,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_list"].handler
	result, err := handler(context.Background(), map[string]any{
		"type": "task",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Task 1") {
		t.Errorf("result should contain entry title, got: %s", result)
	}
}

func TestBrainUpdate_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("method = %q, want PATCH", r.Method)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["status"] != "completed" {
			t.Errorf("body.status = %v, want %q", body["status"], "completed")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"path":   "projects/test/task/abc.md",
			"title":  "Test Task",
			"status": "completed",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_update"].handler
	result, err := handler(context.Background(), map[string]any{
		"path":   "projects/test/task/abc.md",
		"status": "completed",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Updated") {
		t.Errorf("result should contain 'Updated', got: %s", result)
	}
	if !strings.Contains(result, "completed") {
		t.Errorf("result should contain status, got: %s", result)
	}
	if strings.Contains(result, "Status: ->") {
		t.Errorf("result should not use ambiguous empty-arrow status audit, got: %s", result)
	}
	if !strings.Contains(result, "Status: completed") {
		t.Errorf("result should show new status clearly, got: %s", result)
	}
}

func TestBrainUpdate_HandlerOmitsEmptyDefaultFields(t *testing.T) {
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("method = %q, want PATCH", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"path":   "projects/test/task/abc.md",
			"title":  "Original Task",
			"status": "pending",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_update"].handler
	_, err := handler(context.Background(), map[string]any{
		"path":                 "projects/test/task/abc.md",
		"status":               "pending",
		"title":                "",
		"append":               "",
		"note":                 "",
		"depends_on":           []any{},
		"tags":                 []any{},
		"priority":             "",
		"target_workdir":       "",
		"git_branch":           "",
		"merge_target_branch":  "",
		"merge_policy":         "",
		"merge_strategy":       "",
		"remote_branch_policy": "",
		"execution_mode":       "",
		"schedule":             "",
		"run_once_at":          "",
		"timezone":             "",
		"starts_at":            "",
		"expires_at":           "",
		"feature_id":           "",
		"feature_priority":     "",
		"feature_depends_on":   []any{},
		"feature_schedule":     "",
		"feature_starts_at":    "",
		"feature_expires_at":   "",
		"feature_run_once_at":  "",
		"feature_timezone":     "",
		"direct_prompt":        "",
		"agent":                "",
		"model":                "",
		"executor":             "",
		"extensions":           []any{},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if capturedBody["status"] != "pending" {
		t.Fatalf("body.status = %v, want pending", capturedBody["status"])
	}
	for _, key := range []string{"title", "depends_on", "tags", "feature_id", "feature_depends_on", "direct_prompt", "extensions"} {
		if _, ok := capturedBody[key]; ok {
			t.Errorf("body should omit empty default field %q, got %#v", key, capturedBody[key])
		}
	}
}

func TestBrainUpdate_HandlerSupportsExplicitDirectPromptClear(t *testing.T) {
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("method = %q, want PATCH", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"path":   "projects/test/task/abc.md",
			"title":  "Original Task",
			"status": "pending",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_update"].handler
	result, err := handler(context.Background(), map[string]any{
		"path":                "projects/test/task/abc.md",
		"clear_direct_prompt": true,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	value, ok := capturedBody["direct_prompt"]
	if !ok {
		t.Fatalf("body should include direct_prompt for explicit clear, got %#v", capturedBody)
	}
	if value != "" {
		t.Fatalf("body.direct_prompt = %#v, want empty string", value)
	}
	if !strings.Contains(result, "Direct Prompt: cleared") {
		t.Fatalf("result should audit direct prompt clear, got: %s", result)
	}
}

func TestBrainBulkUpdate_HandlerOmitsEmptyDefaultUpdateFields(t *testing.T) {
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/entries/bulk-update" {
			t.Errorf("path = %q, want /api/v1/entries/bulk-update", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"updated": 1,
			"failed":  0,
			"total":   1,
			"dry_run": false,
			"results": []map[string]any{{
				"path":   "projects/test/task/abc.md",
				"id":     "abc",
				"title":  "Original Task",
				"status": "ok",
			}},
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_bulk_update"].handler
	_, err := handler(context.Background(), map[string]any{
		"filter": map[string]any{"feature_id": "feature-a", "type": "task"},
		"updates": map[string]any{
			"status":             "pending",
			"title":              "",
			"depends_on":         []any{},
			"tags":               []any{},
			"feature_id":         "",
			"feature_depends_on": []any{},
			"direct_prompt":      "",
			"agent":              "",
			"model":              "",
		},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	updates, ok := capturedBody["updates"].(map[string]any)
	if !ok {
		t.Fatalf("captured updates = %#v, want object", capturedBody["updates"])
	}
	if updates["status"] != "pending" {
		t.Fatalf("updates.status = %v, want pending", updates["status"])
	}
	for _, key := range []string{"title", "depends_on", "tags", "feature_id", "feature_depends_on", "direct_prompt", "agent", "model"} {
		if _, ok := updates[key]; ok {
			t.Errorf("updates should omit empty/default field %q, got %#v", key, updates[key])
		}
	}
}

func TestBrainBulkUpdate_HandlerReportsPartialFailureRecovery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"updated": 1,
			"failed":  1,
			"total":   2,
			"dry_run": false,
			"results": []map[string]any{
				{"path": "projects/test/task/ok.md", "id": "ok", "title": "Updated Task", "status": "ok"},
				{"path": "projects/test/task/missing.md", "title": "Missing Task", "status": "error", "error": "entry not found"},
			},
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_bulk_update"].handler
	result, err := handler(context.Background(), map[string]any{
		"entries": []any{
			map[string]any{"path": "projects/test/task/ok.md", "updates": map[string]any{"status": "pending"}},
			map[string]any{"path": "projects/test/task/missing.md", "updates": map[string]any{"status": "pending"}},
		},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Partial failure") {
		t.Fatalf("result should clearly report partial failure, got: %s", result)
	}
	if !strings.Contains(result, "successful updates were not rolled back") {
		t.Fatalf("result should explain non-atomic recovery behavior, got: %s", result)
	}
	if !strings.Contains(result, "Re-run with `dry_run: true`") {
		t.Fatalf("result should include safe recovery instructions, got: %s", result)
	}
}

func TestBrainDelete_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" {
			t.Errorf("method = %q, want DELETE", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "deleted"})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_delete"].handler
	result, err := handler(context.Background(), map[string]any{
		"path":    "projects/test/task/abc.md",
		"confirm": true,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Deleted") {
		t.Errorf("result should contain 'Deleted', got: %s", result)
	}
}

func TestBrainDelete_NoConfirm(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:1")
	RegisterBrainTools(s, client)

	handler := s.tools["brain_delete"].handler
	result, err := handler(context.Background(), map[string]any{
		"path":    "projects/test/task/abc.md",
		"confirm": false,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "confirm: true") {
		t.Errorf("result should ask for confirmation, got: %s", result)
	}
}

func TestBrainStats_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"totalEntries":   42,
			"globalEntries":  10,
			"projectEntries": 32,
			"byType":         map[string]int{"task": 20, "summary": 12, "plan": 10},
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_stats"].handler
	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "42") {
		t.Errorf("result should contain total count, got: %s", result)
	}
	if !strings.Contains(result, "Brain Statistics") {
		t.Errorf("result should contain header, got: %s", result)
	}
}

func TestBrainBacklinks_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"entries": []map[string]any{
				{"id": "xyz", "path": "p/summary/xyz.md", "title": "Linking Entry", "type": "summary"},
			},
			"total": 1,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_backlinks"].handler
	result, err := handler(context.Background(), map[string]any{
		"path": "projects/test/plan/abc.md",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Linking Entry") {
		t.Errorf("result should contain entry title, got: %s", result)
	}
	if !strings.Contains(result, "Backlinks") {
		t.Errorf("result should contain 'Backlinks', got: %s", result)
	}
}

func TestBrainVerify_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"message": "verified", "path": "test/path"})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_verify"].handler
	result, err := handler(context.Background(), map[string]any{
		"path": "test/path",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Verified") {
		t.Errorf("result should contain 'Verified', got: %s", result)
	}
}

func TestBrainSave_TaskEnrichment(t *testing.T) {
	// Override cached context for testing
	cachedContext = &ExecutionContext{
		ProjectID: "test-project",
		Workdir:   "projects/test",
		GitRemote: "git@github.com:test/repo.git",
		GitBranch: "main",
	}
	defer func() { cachedContext = nil }()

	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id": "abc", "path": "p/task/abc.md", "title": "Task", "type": "task", "status": "draft",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_save"].handler
	_, err := handler(context.Background(), map[string]any{
		"type":    "task",
		"title":   "Test Task",
		"content": "Do something",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// Task should be enriched with execution context
	if capturedBody["project"] != "test-project" {
		t.Errorf("project = %v, want %q", capturedBody["project"], "test-project")
	}
	if capturedBody["workdir"] != "projects/test" {
		t.Errorf("workdir = %v, want %q", capturedBody["workdir"], "projects/test")
	}
	if capturedBody["git_remote"] != "git@github.com:test/repo.git" {
		t.Errorf("git_remote = %v, want %q", capturedBody["git_remote"], "git@github.com:test/repo.git")
	}
}

func TestBrainSave_NonTaskNoEnrichment(t *testing.T) {
	cachedContext = &ExecutionContext{
		ProjectID: "test-project",
		Workdir:   "projects/test",
		GitRemote: "git@github.com:test/repo.git",
		GitBranch: "main",
	}
	defer func() { cachedContext = nil }()

	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id": "abc", "path": "p/summary/abc.md", "title": "Summary", "type": "summary", "status": "active",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_save"].handler
	_, err := handler(context.Background(), map[string]any{
		"type":    "summary",
		"title":   "Test Summary",
		"content": "Some content",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// Non-task entries should NOT have execution context fields
	if _, ok := capturedBody["workdir"]; ok {
		t.Errorf("non-task should not have workdir, got %v", capturedBody["workdir"])
	}
	if _, ok := capturedBody["git_remote"]; ok {
		t.Errorf("non-task should not have git_remote, got %v", capturedBody["git_remote"])
	}
}

func TestBrainRecall_TitleFallback(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if r.Method == "POST" && r.URL.Path == "/api/v1/search" {
			// Search for title
			json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"path": "projects/test/plan/abc.md", "title": "My Plan"},
				},
				"total": 1,
			})
			return
		}

		if r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v1/entries/") {
			json.NewEncoder(w).Encode(map[string]any{
				"id":      "abc",
				"path":    "projects/test/plan/abc.md",
				"title":   "My Plan",
				"type":    "plan",
				"status":  "active",
				"content": "Plan content",
				"tags":    []string{},
			})
			return
		}

		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_recall"].handler
	result, err := handler(context.Background(), map[string]any{
		"title": "My Plan",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "My Plan") {
		t.Errorf("result should contain title, got: %s", result)
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls (search + get), got %d", callCount)
	}
}

func TestBrainRecall_NoPathOrTitle(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:1")
	RegisterBrainTools(s, client)

	handler := s.tools["brain_recall"].handler
	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "provide a path or title") {
		t.Errorf("result should ask for path/title, got: %s", result)
	}
}

// Verify all tool descriptions are non-empty
func TestRegisterBrainTools_Descriptions(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterBrainTools(s, client)

	for name, rt := range s.tools {
		if rt.tool.Description == "" {
			t.Errorf("tool %q has empty description", name)
		}
		if rt.tool.InputSchema.Type != "object" {
			t.Errorf("tool %q inputSchema.type = %q, want %q", name, rt.tool.InputSchema.Type, "object")
		}
	}
}

// Verify brain_save schema has all expected properties
func TestBrainSave_SchemaProperties(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterBrainTools(s, client)

	saveTool := s.tools["brain_save"].tool
	expectedProps := []string{
		"type", "title", "content", "tags", "status", "priority",
		"global", "project", "depends_on", "user_original_request",
		"target_workdir", "feature_id", "feature_priority", "feature_depends_on",
		"direct_prompt", "agent", "model", "schedule", "schedule_enabled",
		"git_branch", "merge_target_branch", "merge_policy", "merge_strategy",
		"remote_branch_policy", "open_pr_before_merge", "execution_mode",
		"complete_on_idle", "relatedEntries",
	}

	for _, prop := range expectedProps {
		if _, ok := saveTool.InputSchema.Properties[prop]; !ok {
			t.Errorf("brain_save missing property %q", prop)
		}
	}
}

// Verify brain_update schema has all expected properties
func TestBrainUpdate_SchemaProperties(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterBrainTools(s, client)

	updateTool := s.tools["brain_update"].tool
	expectedProps := []string{
		"path", "status", "title", "append", "note", "depends_on", "tags",
		"priority", "target_workdir", "git_branch", "merge_target_branch",
		"merge_policy", "merge_strategy", "remote_branch_policy",
		"open_pr_before_merge", "execution_mode", "complete_on_idle",
		"schedule", "schedule_enabled", "feature_id", "feature_priority",
		"feature_depends_on", "direct_prompt", "agent", "model",
	}

	for _, prop := range expectedProps {
		if _, ok := updateTool.InputSchema.Properties[prop]; !ok {
			t.Errorf("brain_update missing property %q", prop)
		}
	}
}

// Verify brain_move handler
func TestBrainMove_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"oldPath": "projects/old/task/abc.md",
			"newPath": "projects/new/task/abc.md",
			"project": "new-project",
			"id":      "abc",
			"title":   "Test Task",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_move"].handler
	result, err := handler(context.Background(), map[string]any{
		"path":    "projects/old/task/abc.md",
		"project": "new-project",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Moved") {
		t.Errorf("result should contain 'Moved', got: %s", result)
	}
}

func TestBrainMove_MissingArgs(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:1")
	RegisterBrainTools(s, client)

	handler := s.tools["brain_move"].handler
	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "provide both") {
		t.Errorf("result should ask for both args, got: %s", result)
	}
}

// Test brain_link handler
func TestBrainLink_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"link":  "[Test Entry](projects/test/summary/abc.md)",
			"id":    "abc",
			"path":  "projects/test/summary/abc.md",
			"title": "Test Entry",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_link"].handler
	result, err := handler(context.Background(), map[string]any{
		"path": "projects/test/summary/abc.md",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Link:") {
		t.Errorf("result should contain 'Link:', got: %s", result)
	}
}

// Test brain_inject handler
func TestBrainInject_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"context": "## Relevant Context\n\nSome context here",
			"entries": []map[string]any{
				{"id": "abc", "path": "p/summary/abc.md", "title": "Entry", "type": "summary"},
			},
			"total": 1,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_inject"].handler
	result, err := handler(context.Background(), map[string]any{
		"query": "test context",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Relevant Context") {
		t.Errorf("result should contain context, got: %s", result)
	}
}

// Verify all handlers are non-nil
func TestRegisterBrainTools_AllHandlersSet(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterBrainTools(s, client)

	for name, rt := range s.tools {
		if rt.handler == nil {
			t.Errorf("tool %q has nil handler", name)
		}
	}
}

// Test error formatting from handler
func TestBrainSave_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string{"error": "validation", "message": "title is required"})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_save"].handler
	_, err := handler(context.Background(), map[string]any{
		"type":    "summary",
		"title":   "",
		"content": "test",
	})
	// Errors from API should be returned as errors
	if err == nil {
		t.Fatal("expected error from API")
	}
	if !strings.Contains(err.Error(), "title is required") {
		t.Errorf("error should contain API message, got: %v", err)
	}
}

// =============================================================================
// brain_automation_list Tests
// =============================================================================

func TestBrainAutomationList_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterBrainTools(s, client)

	tool := s.tools["brain_automation_list"].tool

	// No required fields (all optional)
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("brain_automation_list required = %v, want []", tool.InputSchema.Required)
	}

	// Has project and status properties
	if _, ok := tool.InputSchema.Properties["project"]; !ok {
		t.Error("brain_automation_list missing 'project' property")
	}
	if _, ok := tool.InputSchema.Properties["status"]; !ok {
		t.Error("brain_automation_list missing 'status' property")
	}

	// Status has enum
	statusProp := tool.InputSchema.Properties["status"]
	if len(statusProp.Enum) != 3 {
		t.Errorf("status enum has %d values, want 3", len(statusProp.Enum))
	}
}

func TestBrainAutomationList_Handler_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || !strings.Contains(r.URL.Path, "/api/v1/entries") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("type") != "automation" {
			t.Errorf("expected type=automation, got %q", r.URL.Query().Get("type"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"entries": []any{},
			"total":   0,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_automation_list"].handler
	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "No automations found") {
		t.Errorf("result should indicate no automations, got: %s", result)
	}
}

func TestBrainAutomationList_Handler_WithEntries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"entries": []map[string]any{
				{
					"id":         "auto1234",
					"title":      "On Task Complete",
					"type":       "automation",
					"status":     "active",
					"project_id": "my-project",
					"trigger":    map[string]any{"type": "event", "event": "task.completed"},
					"action":     map[string]any{"type": "prompt", "direct_prompt": "Review task"},
				},
				{
					"id":         "auto5678",
					"title":      "Nightly Build",
					"type":       "automation",
					"status":     "active",
					"project_id": "my-project",
					"trigger":    map[string]any{"type": "cron", "schedule": "0 0 * * *"},
					"action":     map[string]any{"type": "script", "command": "make build"},
				},
			},
			"total": 2,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_automation_list"].handler
	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Automations (2)") {
		t.Errorf("result should show count, got: %s", result)
	}
	if !strings.Contains(result, "On Task Complete") {
		t.Errorf("result should contain automation title, got: %s", result)
	}
	if !strings.Contains(result, "auto1234") {
		t.Errorf("result should contain automation ID, got: %s", result)
	}
}

func TestBrainAutomationList_Handler_WithProjectFilter(t *testing.T) {
	var capturedQuery string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedQuery = r.URL.Query().Get("project")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"entries": []any{},
			"total":   0,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_automation_list"].handler
	_, err := handler(context.Background(), map[string]any{
		"project": "my-project",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if capturedQuery != "my-project" {
		t.Errorf("project query = %q, want %q", capturedQuery, "my-project")
	}
}

// =============================================================================
// brain_automation_test Tests
// =============================================================================

func TestBrainAutomationTest_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterBrainTools(s, client)

	tool := s.tools["brain_automation_test"].tool

	// Required: event
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "event" {
		t.Errorf("brain_automation_test required = %v, want [event]", tool.InputSchema.Required)
	}

	// Has event and project properties
	if _, ok := tool.InputSchema.Properties["event"]; !ok {
		t.Error("brain_automation_test missing 'event' property")
	}
	if _, ok := tool.InputSchema.Properties["project"]; !ok {
		t.Error("brain_automation_test missing 'project' property")
	}
}

func TestBrainAutomationTest_Handler_NoMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"entries": []map[string]any{
				{
					"id":      "auto1234",
					"title":   "On Feature Complete",
					"type":    "automation",
					"status":  "active",
					"trigger": map[string]any{"type": "event", "event": "feature.all_completed"},
					"action":  map[string]any{"type": "prompt"},
				},
			},
			"total": 1,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_automation_test"].handler
	result, err := handler(context.Background(), map[string]any{
		"event": "task.completed",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "No automations matched") {
		t.Errorf("result should indicate no match, got: %s", result)
	}
	if !strings.Contains(result, "task.completed") {
		t.Errorf("result should contain event name, got: %s", result)
	}
}

func TestBrainAutomationTest_Handler_WithMatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"entries": []map[string]any{
				{
					"id":      "auto1234",
					"title":   "On Task Complete",
					"type":    "automation",
					"status":  "active",
					"trigger": map[string]any{"type": "event", "event": "task.completed"},
					"action":  map[string]any{"type": "prompt", "direct_prompt": "Review the task"},
				},
				{
					"id":      "auto5678",
					"title":   "On Task Wildcard",
					"type":    "automation",
					"status":  "active",
					"trigger": map[string]any{"type": "event", "event": "task.*"},
					"action":  map[string]any{"type": "script", "command": "echo done"},
				},
				{
					"id":      "cron1234",
					"title":   "Nightly",
					"type":    "automation",
					"status":  "active",
					"trigger": map[string]any{"type": "cron", "schedule": "0 0 * * *"},
					"action":  map[string]any{"type": "script"},
				},
			},
			"total": 3,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_automation_test"].handler
	result, err := handler(context.Background(), map[string]any{
		"event": "task.completed",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "MATCH") {
		t.Errorf("result should contain MATCH, got: %s", result)
	}
	if !strings.Contains(result, "On Task Complete") {
		t.Errorf("result should contain matched automation title, got: %s", result)
	}
	if !strings.Contains(result, "On Task Wildcard") {
		t.Errorf("result should match wildcard automation, got: %s", result)
	}
	if !strings.Contains(result, "2 automation(s) would match") {
		t.Errorf("result should show 2 matches (not cron), got: %s", result)
	}
}

// =============================================================================
// matchesAutomationEvent Tests
// =============================================================================

func TestMatchesAutomationEvent(t *testing.T) {
	tests := []struct {
		pattern   string
		eventName string
		want      bool
	}{
		{"task.completed", "task.completed", true},
		{"task.completed", "task.failed", false},
		{"task.*", "task.completed", true},
		{"task.*", "task.failed", true},
		{"task.*", "feature.all_completed", false},
		{"*", "anything", true},
		{"feature.all_completed", "task.completed", false},
	}

	for _, tt := range tests {
		got := matchesAutomationEvent(tt.pattern, tt.eventName)
		if got != tt.want {
			t.Errorf("matchesAutomationEvent(%q, %q) = %v, want %v",
				tt.pattern, tt.eventName, got, tt.want)
		}
	}
}

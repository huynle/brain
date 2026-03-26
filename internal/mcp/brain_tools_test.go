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
	if count != 19 {
		t.Errorf("expected 19 brain tools registered, got %d", count)
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
	if len(typeProp.Enum) != 12 {
		t.Errorf("brain_save type enum has %d values, want 12", len(typeProp.Enum))
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

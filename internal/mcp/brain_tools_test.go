package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
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
	if count != 32 {
		t.Errorf("expected 32 brain tools registered, got %d", count)
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
		"brain_attachment_upload",
		"brain_attachment_attach",
		"brain_attachment_detach",
		"brain_attachment_list",
		"brain_attachment_get",
		"brain_attachment_delete",
		"brain_attachment_backfill",
		"brain_attachment_extract",
		"brain_attachment_text",
		"brain_attachment_download",
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
	if len(typeProp.Enum) != len(types.EntryTypes) {
		t.Errorf("brain_save type enum has %d values, want %d", len(typeProp.Enum), len(types.EntryTypes))
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

func TestBrainUpdate_HandlerOmitsEmptyOptionalStringDefaults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" {
			t.Errorf("method = %q, want PATCH", r.Method)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if body["status"] != "draft" {
			t.Errorf("body.status = %v, want %q", body["status"], "draft")
		}
		if _, ok := body["title"]; ok {
			t.Fatalf("body should not include empty title default: %#v", body)
		}
		if _, ok := body["feature_id"]; ok {
			t.Fatalf("body should not include empty feature_id default: %#v", body)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"path":   "projects/test/task/abc.md",
			"title":  "Test Task",
			"status": "draft",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_update"].handler
	_, err := handler(context.Background(), map[string]any{
		"path":       "projects/test/task/abc.md",
		"status":     "draft",
		"title":      "",
		"feature_id": "",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
}

func TestBrainUpdate_HandlerOmitsOpenCodeOptionalDefaults(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}

		if body["status"] != "pending" {
			t.Errorf("body.status = %v, want %q", body["status"], "pending")
		}
		for _, key := range []string{"priority", "feature_priority", "merge_policy", "merge_strategy", "remote_branch_policy", "execution_mode", "executor", "open_pr_before_merge", "complete_on_idle", "schedule_enabled", "max_runs"} {
			if _, ok := body[key]; ok {
				t.Fatalf("body should not include OpenCode optional default %q: %#v", key, body)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"path":   "projects/test/task/abc.md",
			"title":  "Test Task",
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
		"priority":             "medium",
		"feature_id":           "",
		"feature_priority":     "high",
		"target_workdir":       "",
		"git_branch":           "",
		"merge_target_branch":  "",
		"merge_policy":         "prompt_only",
		"merge_strategy":       "squash",
		"open_pr_before_merge": false,
		"execution_mode":       "worktree",
		"complete_on_idle":     false,
		"remote_branch_policy": "keep",
		"schedule":             "",
		"schedule_enabled":     false,
		"max_runs":             float64(0),
		"run_once_at":          "",
		"timezone":             "",
		"starts_at":            "",
		"expires_at":           "",
		"feature_schedule":     "",
		"feature_starts_at":    "",
		"feature_expires_at":   "",
		"feature_run_once_at":  "",
		"feature_timezone":     "",
		"direct_prompt":        "",
		"agent":                "",
		"model":                "",
		"executor":             "opencode",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
}

func TestBrainBulkUpdate_HandlerOmitsOpenCodeOptionalDefaultsInEntryUpdates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/entries/bulk-update" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}

		entries, ok := body["entries"].([]any)
		if !ok || len(entries) != 1 {
			t.Fatalf("entries = %#v, want one entry", body["entries"])
		}
		entry, ok := entries[0].(map[string]any)
		if !ok {
			t.Fatalf("entry = %#v, want object", entries[0])
		}
		updates, ok := entry["updates"].(map[string]any)
		if !ok {
			t.Fatalf("updates = %#v, want object", entry["updates"])
		}
		if updates["status"] != "pending" {
			t.Errorf("updates.status = %v, want %q", updates["status"], "pending")
		}
		for _, key := range []string{"priority", "feature_priority", "merge_policy", "merge_strategy", "remote_branch_policy", "execution_mode", "executor", "open_pr_before_merge", "complete_on_idle", "schedule_enabled", "max_runs"} {
			if _, ok := updates[key]; ok {
				t.Fatalf("updates should not include OpenCode optional default %q: %#v", key, updates)
			}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"updated": 1,
			"failed":  0,
			"total":   1,
			"dry_run": false,
			"results": []map[string]any{{"path": "projects/test/task/abc.md", "id": "abc12345", "title": "Test Task", "status": "ok"}},
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_bulk_update"].handler
	_, err := handler(context.Background(), map[string]any{
		"entries": []any{
			map[string]any{
				"path": "projects/test/task/abc.md",
				"updates": map[string]any{
					"status":               "pending",
					"priority":             "medium",
					"feature_priority":     "high",
					"merge_policy":         "prompt_only",
					"merge_strategy":       "squash",
					"open_pr_before_merge": false,
					"execution_mode":       "worktree",
					"complete_on_idle":     false,
					"remote_branch_policy": "keep",
					"schedule_enabled":     false,
					"max_runs":             float64(0),
					"executor":             "opencode",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
}

func TestBrainBulkUpdate_HandlerTreatsEmptyFilterAsAbsentForExplicitEntries(t *testing.T) {
	serverCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serverCalled = true
		if r.Method != "POST" || r.URL.Path != "/api/v1/entries/bulk-update" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}

		if _, ok := body["filter"]; ok {
			t.Fatalf("body should not include empty filter: %#v", body)
		}
		if _, ok := body["updates"]; ok {
			t.Fatalf("body should not include top-level updates in explicit mode: %#v", body)
		}
		entries, ok := body["entries"].([]any)
		if !ok || len(entries) != 1 {
			t.Fatalf("entries = %#v, want one entry", body["entries"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"updated": 1,
			"failed":  0,
			"total":   1,
			"dry_run": false,
			"results": []map[string]any{{"path": "projects/test/task/abc.md", "id": "abc12345", "title": "Test Task", "status": "ok"}},
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_bulk_update"].handler
	_, err := handler(context.Background(), map[string]any{
		"filter": map[string]any{},
		"entries": []any{
			map[string]any{
				"path": "projects/test/task/abc.md",
				"updates": map[string]any{
					"status": "pending",
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !serverCalled {
		t.Fatal("handler did not forward explicit entries request")
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

func TestBrainRecall_IncludeQueryAndAttachmentFormatting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/entries/projects/test/report/abc.md" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if got := r.URL.Query().Get("include"); got != "attachments,attachment_text" {
			t.Errorf("include query = %q, want attachments,attachment_text", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"id":      "abc",
			"path":    "projects/test/report/abc.md",
			"title":   "Report With PDF",
			"type":    "report",
			"status":  "active",
			"content": "Report body",
			"tags":    []string{"pdf"},
			"attachments": []map[string]any{
				{
					"id":           "att_123",
					"filename":     "source.pdf",
					"content_type": "application/pdf",
					"size":         42,
					"role":         "source",
					"caption":      "Original source PDF",
					"download_url": "/api/v1/attachments/att_123/content",
					"text_url":     "/api/v1/attachments/att_123/text",
					"derived": []map[string]any{
						{"id": "drv_1", "kind": "text", "content_type": "text/plain", "size": 100},
					},
				},
			},
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	result, err := s.tools["brain_recall"].handler(context.Background(), map[string]any{
		"path":    "projects/test/report/abc.md",
		"include": []any{"attachments", "attachment_text"},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	for _, want := range []string{"Attachments", "att_123", "source.pdf", "Original source PDF", "Derived", "drv_1", "text/plain"} {
		if !strings.Contains(result, want) {
			t.Errorf("result should contain %q, got: %s", want, result)
		}
	}
}

func TestAttachmentToolSchemas(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterBrainTools(s, client)

	tests := []struct {
		tool     string
		required []string
	}{
		{"brain_attachment_upload", []string{"project_id", "file_path"}},
		{"brain_attachment_attach", []string{"project_id", "entry_id", "attachment_id"}},
		{"brain_attachment_detach", []string{"project_id", "entry_id", "attachment_id"}},
		{"brain_attachment_list", []string{"project_id"}},
		{"brain_attachment_get", []string{"project_id", "attachment_id"}},
		{"brain_attachment_delete", []string{"project_id", "attachment_id"}},
		{"brain_attachment_backfill", []string{"project_id"}},
		{"brain_attachment_extract", []string{"project_id", "attachment_id"}},
		{"brain_attachment_text", []string{"project_id", "attachment_id"}},
		{"brain_attachment_download", []string{"project_id", "attachment_id", "output_path"}},
	}

	listTool := s.tools["brain_attachment_list"].tool
	if _, ok := listTool.InputSchema.Properties["entry_id"]; !ok {
		t.Fatalf("brain_attachment_list schema missing optional entry_id property")
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			tool, ok := s.tools[tt.tool]
			if !ok {
				t.Fatalf("tool %q not registered", tt.tool)
			}
			if strings.TrimSpace(tool.tool.Description) == "" {
				t.Fatalf("tool %q has empty description", tt.tool)
			}
			if len(tool.tool.InputSchema.Required) != len(tt.required) {
				t.Fatalf("required = %v, want %v", tool.tool.InputSchema.Required, tt.required)
			}
			for _, req := range tt.required {
				if _, ok := tool.tool.InputSchema.Properties[req]; !ok {
					t.Errorf("schema missing required property %q", req)
				}
			}
		})
	}
}

func TestBrainAttachmentUpload_HandlerUsesMultipartHelper(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "source.txt")
	if err := os.WriteFile(filePath, []byte("hello attachment"), 0o600); err != nil {
		t.Fatalf("write temp file: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/attachments" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
			t.Fatalf("content type = %q, want multipart/form-data", r.Header.Get("Content-Type"))
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if got := r.FormValue("project_id"); got != "test-project" {
			t.Errorf("project_id = %q, want test-project", got)
		}
		if !strings.Contains(r.FormValue("metadata"), "fixture") {
			t.Errorf("metadata = %q, want fixture marker", r.FormValue("metadata"))
		}
		f, _, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("missing file part: %v", err)
		}
		defer f.Close()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"attachment": map[string]any{"id": "att_123", "filename": "source.txt", "content_type": "text/plain", "size": 16}})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	result, err := s.tools["brain_attachment_upload"].handler(context.Background(), map[string]any{
		"project_id": "test-project",
		"file_path":  filePath,
		"metadata":   map[string]any{"kind": "fixture"},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(result, "att_123") || !strings.Contains(result, "brain_attachment_attach") {
		t.Errorf("result should contain uploaded ID and attach hint, got: %s", result)
	}
}

func TestBrainAttachmentAttachDetachListGetExtractTextDownload_RequestShapes(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "downloaded.pdf")
	requests := make([]string, 0, 5)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/entries/entry-123/attachments":
			w.Header().Set("Content-Type", "application/json")
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode attach body: %v", err)
			}
			att := body["attachment"].(map[string]any)
			if att["id"] != "att_123" || att["role"] != "source" || att["caption"] != "PDF source" {
				t.Fatalf("attach body = %#v", body)
			}
			json.NewEncoder(w).Encode(map[string]any{"path": "projects/test/report/abc.md", "entry_id": "entry-123", "attachments": []map[string]any{{"id": "att_123", "filename": "source.pdf", "role": "source"}}})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/entries/entry-123/attachments/att_123":
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Query().Get("role") != "source" || r.URL.Query().Get("project_id") != "test-project" {
				t.Fatalf("detach query = %s", r.URL.RawQuery)
			}
			json.NewEncoder(w).Encode(map[string]any{"path": "projects/test/report/abc.md", "entry_id": "entry-123", "attachments": []any{}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/attachments":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"attachments": []map[string]any{{"id": "att_123", "filename": "source.pdf", "content_type": "application/pdf", "size": 42}}, "total": 1})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/entries/entry-123/attachments":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"path": "projects/test/report/abc.md", "entry_id": "entry-123", "attachments": []map[string]any{{"id": "att_entry", "filename": "entry.pdf", "role": "inline", "caption": "Entry attachment"}}})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/attachments/att_123":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"id": "att_123", "filename": "source.pdf", "content_type": "application/pdf", "size": 42, "derived": []map[string]any{{"id": "drv_1", "kind": "text"}}})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/attachments/att_123":
			if got := r.URL.Query().Get("project_id"); got != "test-project" {
				t.Fatalf("delete project_id = %q, want test-project", got)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{"deleted": true})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attachments/backfill/extraction":
			if got := r.URL.Query().Get("project_id"); got != "test-project" {
				t.Fatalf("backfill project_id = %q, want test-project", got)
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode backfill body: %v", err)
			}
			if body["dry_run"] != true || body["force"] != true || body["batch_size"].(float64) != 5 || body["rate_limit_delay_ms"].(float64) != 25 {
				t.Fatalf("backfill body = %#v", body)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(types.AttachmentExtractionBackfillResponse{
				Total: 3, Candidates: 2, Processed: 1, Skipped: 1, Failed: 1, DryRun: true,
				Attachments: []types.AttachmentExtractionBackfillItem{{AttachmentID: "att_ready", Filename: "ready.pdf", Status: types.AttachmentExtractionStatusReady}, {AttachmentID: "att_failed", Filename: "failed.pdf", Status: types.AttachmentExtractionStatusFailed, Error: "extract failed"}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/attachments/att_123/extract":
			if got := r.URL.Query().Get("project_id"); got != "test-project" {
				t.Fatalf("extract project_id = %q, want test-project", got)
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(types.AttachmentExtractionResult{
				Attachment: types.Attachment{ID: "att_123", Filename: "source.pdf", ContentType: "application/pdf", Size: 42},
				DerivedText: types.AttachmentDerivedText{
					ID:          "drv_1",
					Kind:        "text",
					Status:      types.AttachmentExtractionStatusReady,
					ContentType: "text/plain",
					Text:        "extracted text",
					Metadata: map[string]string{
						"provider":        "openrouter",
						"model":           "google/gemini-2.5-flash",
						"extracted_chars": "14",
						"elapsed_ms":      "1234",
					},
				},
				LinkedEntries: []types.AttachmentLinkedEntry{{Path: "projects/test/report/abc.md", Role: "source"}},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/attachments/att_123/text":
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte("extracted text"))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/attachments/att_123/content":
			w.Header().Set("Content-Type", "application/pdf")
			_, _ = w.Write([]byte("raw attachment bytes"))
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
		}
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	calls := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"brain_attachment_attach", map[string]any{"project_id": "test-project", "entry_id": "entry-123", "attachment_id": "att_123", "role": "source", "caption": "PDF source"}, "Attached"},
		{"brain_attachment_detach", map[string]any{"project_id": "test-project", "entry_id": "entry-123", "attachment_id": "att_123", "role": "source"}, "Detached"},
		{"brain_attachment_list", map[string]any{"project_id": "test-project"}, "source.pdf"},
		{"brain_attachment_list", map[string]any{"project_id": "test-project", "entry_id": "entry-123"}, "Entry: entry-123"},
		{"brain_attachment_get", map[string]any{"project_id": "test-project", "attachment_id": "att_123"}, "drv_1"},
		{"brain_attachment_delete", map[string]any{"project_id": "test-project", "attachment_id": "att_123"}, "Deleted attachment att_123"},
		{"brain_attachment_backfill", map[string]any{"project_id": "test-project", "dry_run": true, "force": true, "batch_size": float64(5), "rate_limit_delay_ms": float64(25)}, "Failed: 1"},
		{"brain_attachment_extract", map[string]any{"project_id": "test-project", "attachment_id": "att_123"}, "Status: ready"},
		{"brain_attachment_text", map[string]any{"project_id": "test-project", "attachment_id": "att_123"}, "extracted text"},
		{"brain_attachment_download", map[string]any{"project_id": "test-project", "attachment_id": "att_123", "output_path": outputPath}, "Downloaded"},
	}
	for _, call := range calls {
		t.Run(call.tool, func(t *testing.T) {
			result, err := s.tools[call.tool].handler(context.Background(), call.args)
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if !strings.Contains(result, call.want) {
				t.Fatalf("result = %q, want substring %q", result, call.want)
			}
			if call.tool == "brain_attachment_extract" {
				for _, want := range []string{"Provider: openrouter", "Model: google/gemini-2.5-flash", "extracted_chars: 14", "elapsed_ms: 1234", "Linked entries", "projects/test/report/abc.md", "Text: 14 chars"} {
					if !strings.Contains(result, want) {
						t.Fatalf("extract result missing %q:\n%s", want, result)
					}
				}
			}
			if call.tool == "brain_attachment_backfill" {
				for _, want := range []string{"Project: test-project", "Total: 3", "Candidates: 2", "Processed: 1", "Skipped: 1", "Dry run: true", "att_ready", "Status: ready", "att_failed", "Error: extract failed"} {
					if !strings.Contains(result, want) {
						t.Fatalf("backfill result missing %q:\n%s", want, result)
					}
				}
			}
		})
	}

	wantRequests := []string{
		"POST /api/v1/entries/entry-123/attachments?project_id=test-project",
		"DELETE /api/v1/entries/entry-123/attachments/att_123?project_id=test-project&role=source",
		"GET /api/v1/attachments?project_id=test-project",
		"GET /api/v1/entries/entry-123/attachments?project_id=test-project",
		"GET /api/v1/attachments/att_123?project_id=test-project",
		"DELETE /api/v1/attachments/att_123?project_id=test-project",
		"POST /api/v1/attachments/backfill/extraction?project_id=test-project",
		"POST /api/v1/attachments/att_123/extract?project_id=test-project",
		"GET /api/v1/attachments/att_123/text?project_id=test-project",
		"GET /api/v1/attachments/att_123/content?project_id=test-project",
	}
	if len(requests) != len(wantRequests) {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
	for i := range wantRequests {
		if requests[i] != wantRequests[i] {
			t.Errorf("request[%d] = %q, want %q", i, requests[i], wantRequests[i])
		}
	}
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != "raw attachment bytes" {
		t.Fatalf("downloaded data = %q", string(data))
	}
}

func TestBrainAttachmentTools_ValidateRequiredIDsBeforeRequest(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:1")
	RegisterBrainTools(s, client)

	tests := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"brain_attachment_upload", map[string]any{"project_id": "test-project"}, "project_id and file_path"},
		{"brain_attachment_attach", map[string]any{"project_id": "test-project", "entry_id": "entry-123"}, "project_id, entry_id, and attachment_id"},
		{"brain_attachment_detach", map[string]any{"project_id": "test-project", "attachment_id": "att_123"}, "project_id, entry_id, and attachment_id"},
		{"brain_attachment_list", map[string]any{}, "project_id"},
		{"brain_attachment_get", map[string]any{"project_id": "test-project"}, "project_id and attachment_id"},
		{"brain_attachment_delete", map[string]any{"project_id": "test-project"}, "project_id and attachment_id"},
		{"brain_attachment_backfill", map[string]any{}, "project_id"},
		{"brain_attachment_extract", map[string]any{"attachment_id": "att_123"}, "project_id and attachment_id"},
		{"brain_attachment_text", map[string]any{"attachment_id": "att_123"}, "project_id and attachment_id"},
		{"brain_attachment_download", map[string]any{"project_id": "test-project", "attachment_id": "att_123"}, "project_id, attachment_id, and output_path"},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			result, err := s.tools[tt.tool].handler(context.Background(), tt.args)
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if !strings.Contains(result, tt.want) {
				t.Fatalf("result = %q, want substring %q", result, tt.want)
			}
		})
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
		"complete_on_idle", "executor", "extensions", "trigger", "action", "retry", "relatedEntries",
	}

	for _, prop := range expectedProps {
		if _, ok := saveTool.InputSchema.Properties[prop]; !ok {
			t.Errorf("brain_save missing property %q", prop)
		}
	}

	triggerDesc := saveTool.InputSchema.Properties["trigger"].Description
	for _, want := range []string{"session", "cooldown", "max_concurrent"} {
		if !strings.Contains(triggerDesc, want) {
			t.Errorf("brain_save trigger description should mention %q, got %q", want, triggerDesc)
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
		"executor", "extensions", "trigger", "action", "retry",
	}

	for _, prop := range expectedProps {
		if _, ok := updateTool.InputSchema.Properties[prop]; !ok {
			t.Errorf("brain_update missing property %q", prop)
		}
	}

	triggerDesc := updateTool.InputSchema.Properties["trigger"].Description
	for _, want := range []string{"session", "cooldown", "max_concurrent"} {
		if !strings.Contains(triggerDesc, want) {
			t.Errorf("brain_update trigger description should mention %q, got %q", want, triggerDesc)
		}
	}
}

func TestBrainSave_ForwardsFeatureCompletionTrigger(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/entries" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		trigger, ok := body["trigger"].(map[string]any)
		if !ok {
			t.Fatalf("trigger was not forwarded as an object: %#v", body["trigger"])
		}
		if trigger["event"] != "feature.completed" {
			t.Errorf("trigger.event = %v, want feature.completed", trigger["event"])
		}
		filter, ok := trigger["filter"].(map[string]any)
		if !ok {
			t.Fatalf("trigger.filter was not forwarded as an object: %#v", trigger["filter"])
		}
		if filter["feature_id"] != "chain-main" {
			t.Errorf("trigger.filter.feature_id = %v, want chain-main", filter["feature_id"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id":     "post1234",
			"path":   "projects/test/task/post1234.md",
			"title":  "Post task",
			"type":   "task",
			"status": "active",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	_, err := s.tools["brain_save"].handler(context.Background(), map[string]any{
		"type":    "task",
		"title":   "Post task",
		"content": "Triggered after feature completion",
		"trigger": map[string]any{
			"event": "feature.completed",
			"filter": map[string]any{
				"feature_id": "chain-main",
				"project_id": "test",
			},
		},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
}

func TestBrainSave_ForwardsAutomationTriggerActionAndRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/entries" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		if body["type"] != "automation" {
			t.Errorf("type = %v, want automation", body["type"])
		}
		trigger, ok := body["trigger"].(map[string]any)
		if !ok {
			t.Fatalf("trigger was not forwarded as an object: %#v", body["trigger"])
		}
		if trigger["type"] != "session" {
			t.Errorf("trigger.type = %v, want session", trigger["type"])
		}
		if trigger["cooldown"] != "5m" {
			t.Errorf("trigger.cooldown = %v, want 5m", trigger["cooldown"])
		}
		if trigger["max_concurrent"] != float64(1) {
			t.Errorf("trigger.max_concurrent = %v, want 1", trigger["max_concurrent"])
		}

		action, ok := body["action"].(map[string]any)
		if !ok {
			t.Fatalf("action was not forwarded as an object: %#v", body["action"])
		}
		if action["type"] != "prompt" {
			t.Errorf("action.type = %v, want prompt", action["type"])
		}
		if action["direct_prompt"] != "Summarize the new session" {
			t.Errorf("action.direct_prompt = %v, want session prompt", action["direct_prompt"])
		}

		retry, ok := body["retry"].(map[string]any)
		if !ok {
			t.Fatalf("retry was not forwarded as an object: %#v", body["retry"])
		}
		if retry["max_attempts"] != float64(2) {
			t.Errorf("retry.max_attempts = %v, want 2", retry["max_attempts"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id":     "auto1234",
			"path":   "projects/test/automation/auto1234.md",
			"title":  "Session automation",
			"type":   "automation",
			"status": "active",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterBrainTools(s, client)

	_, err := s.tools["brain_save"].handler(context.Background(), map[string]any{
		"type":    "automation",
		"title":   "Session automation",
		"content": "Runs when a session is discovered",
		"trigger": map[string]any{
			"type":           "session",
			"once_per":       "session",
			"cooldown":       "5m",
			"max_concurrent": 1,
		},
		"action": map[string]any{
			"type":          "prompt",
			"direct_prompt": "Summarize the new session",
		},
		"retry": map[string]any{
			"max_attempts": 2,
		},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
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
				{
					"id":         "auto9999",
					"title":      "Session Summary",
					"type":       "automation",
					"status":     "active",
					"project_id": "my-project",
					"trigger":    map[string]any{"type": "session"},
					"action":     map[string]any{"type": "prompt", "direct_prompt": "Summarize session"},
				},
			},
			"total": 3,
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

	if !strings.Contains(result, "Automations (3)") {
		t.Errorf("result should show count, got: %s", result)
	}
	if !strings.Contains(result, "On Task Complete") {
		t.Errorf("result should contain automation title, got: %s", result)
	}
	if !strings.Contains(result, "auto1234") {
		t.Errorf("result should contain automation ID, got: %s", result)
	}
	if !strings.Contains(result, "Session Summary") {
		t.Errorf("result should contain session automation title, got: %s", result)
	}
	if !strings.Contains(result, "Trigger: session") {
		t.Errorf("result should show session trigger, got: %s", result)
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

// =============================================================================
// Project-scoping regression tests
//
// Background: users asking an LLM for entries in a specific project were
// getting cross-project results because MCP tool schemas didn't advertise
// a `project` parameter, even when the underlying API supported it. These
// tests lock in that (a) each affected tool's schema exposes `project`,
// and (b) the handler forwards it to the API.
// =============================================================================

// projectSchemaFixtures maps each tool that should accept a `project` filter
// to a short human-readable note. Kept in one place so future additions
// (P1/P3 fixes) can extend coverage in a single edit.
var projectSchemaFixtures = []struct {
	tool string
	note string
}{
	{"brain_list", "P0 — reported bug: list must be scopable by project"},
	{"brain_search", "P0 — sibling of brain_list; API already accepts SearchRequest.Project"},
	{"brain_recall", "P3 — title fallback must not silently return wrong-project matches"},
	{"brain_plan_sections", "P3 — same title-fallback risk as brain_recall"},
	{"brain_bulk_update", "P2 — top-level project shortcut merges into filter.project"},
	{"brain_inject", "P1 — service now honors Project in InjectRequest via SearchOptions.ProjectID"},
	{"brain_stats", "P1 — service+API+storage all extended to accept project"},
	{"brain_stale", "P1 — service+API+storage all extended to accept project"},
	{"brain_orphans", "P1 — service+API+storage all extended to accept project"},
}

func TestBrainTools_ProjectParameter_InSchema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterBrainTools(s, client)

	for _, fx := range projectSchemaFixtures {
		fx := fx
		t.Run(fx.tool, func(t *testing.T) {
			rt, ok := s.tools[fx.tool]
			if !ok {
				t.Fatalf("%s not registered (%s)", fx.tool, fx.note)
			}
			prop, ok := rt.tool.InputSchema.Properties["project"]
			if !ok {
				t.Fatalf("%s schema missing 'project' property (%s)", fx.tool, fx.note)
			}
			if prop.Type != "string" {
				t.Errorf("%s.project.type = %q, want %q", fx.tool, prop.Type, "string")
			}
			if prop.Description == "" {
				t.Errorf("%s.project has empty description; LLMs need guidance", fx.tool)
			}
		})
	}
}

// TestBrainList_Handler_ForwardsProject verifies that when `project` is
// passed to the brain_list tool, it reaches the API as a query parameter.
// This is the exact bug that had the LLM fall back to raw curl.
func TestBrainList_Handler_ForwardsProject(t *testing.T) {
	var gotProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/entries" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		gotProject = r.URL.Query().Get("project")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"entries": []map[string]any{
				{"id": "abc", "path": "projects/orion-ai/summary/abc.md", "title": "Orion Summary", "type": "summary", "status": "active"},
			},
			"total": 1,
		})
	}))
	defer srv.Close()

	s := NewServer()
	client := NewAPIClient(srv.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_list"].handler
	if _, err := handler(context.Background(), map[string]any{
		"project": "orion-ai",
		"limit":   float64(1),
		"sortBy":  "modified",
	}); err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if gotProject != "orion-ai" {
		t.Errorf("brain_list did not forward project param: got %q, want %q", gotProject, "orion-ai")
	}
}

// TestBrainList_Handler_OmitsProjectWhenAbsent guards against a regression
// where the handler starts sending an empty `project=` (which the API
// treats as "match empty string" in some paths).
func TestBrainList_Handler_OmitsProjectWhenAbsent(t *testing.T) {
	var sawProjectKey bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, sawProjectKey = r.URL.Query()["project"]
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": []any{}, "total": 0})
	}))
	defer srv.Close()

	s := NewServer()
	client := NewAPIClient(srv.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_list"].handler
	if _, err := handler(context.Background(), map[string]any{"limit": float64(1)}); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if sawProjectKey {
		t.Error("brain_list sent a 'project' query param when the arg was absent; should be omitted entirely")
	}
}

// TestBrainSearch_Handler_ForwardsProject verifies that brain_search
// (which sends its args map verbatim as the POST body) delivers `project`
// to /api/v1/search. This is the analog of TestBrainList_Handler_ForwardsProject.
func TestBrainSearch_Handler_ForwardsProject(t *testing.T) {
	var bodyProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/search" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		bodyProject, _ = body["project"].(string)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}, "total": 0})
	}))
	defer srv.Close()

	s := NewServer()
	client := NewAPIClient(srv.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_search"].handler
	if _, err := handler(context.Background(), map[string]any{
		"query":   "hello",
		"project": "orion-ai",
	}); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if bodyProject != "orion-ai" {
		t.Errorf("brain_search did not forward project in POST body: got %q, want %q", bodyProject, "orion-ai")
	}
}

// TestBrainRecall_TitleFallback_ForwardsProject verifies that when
// brain_recall is called with `title` + `project`, the intermediate
// /search request includes the project filter, so a same-titled note
// in a different project cannot silently be returned.
func TestBrainRecall_TitleFallback_ForwardsProject(t *testing.T) {
	var searchBodyProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/search":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode search body: %v", err)
			}
			searchBodyProject, _ = body["project"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"path": "projects/orion-ai/summary/abc.md", "title": "Overview"},
				},
			})
		case "/api/v1/entries/projects/orion-ai/summary/abc.md":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"path":    "projects/orion-ai/summary/abc.md",
				"title":   "Overview",
				"type":    "summary",
				"status":  "active",
				"content": "body",
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	s := NewServer()
	client := NewAPIClient(srv.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_recall"].handler
	if _, err := handler(context.Background(), map[string]any{
		"title":   "Overview",
		"project": "orion-ai",
	}); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if searchBodyProject != "orion-ai" {
		t.Errorf("brain_recall title-fallback did not forward project: got %q, want %q", searchBodyProject, "orion-ai")
	}
}

// TestBrainPlanSections_TitleFallback_ForwardsProject is the analog of
// TestBrainRecall_TitleFallback_ForwardsProject for brain_plan_sections.
func TestBrainPlanSections_TitleFallback_ForwardsProject(t *testing.T) {
	var searchBodyProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/search":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decode search body: %v", err)
			}
			searchBodyProject, _ = body["project"].(string)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"path": "projects/orion-ai/plan/auth.md", "title": "Auth Plan"},
				},
			})
		case "/api/v1/entries/projects/orion-ai/plan/auth.md/sections":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"sections": []map[string]any{{"title": "Overview", "level": 2, "line": 1}},
				"total":    1,
			})
		case "/api/v1/entries/projects/orion-ai/plan/auth.md":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"title": "Auth Plan",
				"type":  "plan",
			})
		default:
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	s := NewServer()
	client := NewAPIClient(srv.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_plan_sections"].handler
	if _, err := handler(context.Background(), map[string]any{
		"title":   "Auth Plan",
		"project": "orion-ai",
	}); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if searchBodyProject != "orion-ai" {
		t.Errorf("brain_plan_sections title-fallback did not forward project: got %q, want %q", searchBodyProject, "orion-ai")
	}
}

// TestBrainBulkUpdate_TopLevelProject_MergedIntoFilter verifies the P2
// discoverability improvement: a top-level `project` field is copied into
// filter.project (which the API already recognizes), so LLMs don't need
// to know the nested filter shape.
func TestBrainBulkUpdate_TopLevelProject_MergedIntoFilter(t *testing.T) {
	var filterProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/entries/bulk-update" {
			t.Errorf("unexpected: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if f, ok := body["filter"].(map[string]any); ok {
			filterProject, _ = f["project"].(string)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"updated": 0, "failed": 0, "total": 0, "dry_run": true})
	}))
	defer srv.Close()

	s := NewServer()
	client := NewAPIClient(srv.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_bulk_update"].handler
	if _, err := handler(context.Background(), map[string]any{
		"project": "orion-ai",
		"filter":  map[string]any{"status": "draft"},
		"updates": map[string]any{"status": "pending"},
		"dry_run": true,
	}); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if filterProject != "orion-ai" {
		t.Errorf("top-level project did not merge into filter.project: got %q, want %q", filterProject, "orion-ai")
	}
}

// TestBrainBulkUpdate_ExplicitFilterProject_WinsOverTopLevel verifies the
// non-clobbering rule: if the caller sets filter.project explicitly, that
// wins over any top-level shortcut.
func TestBrainBulkUpdate_ExplicitFilterProject_WinsOverTopLevel(t *testing.T) {
	var filterProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if f, ok := body["filter"].(map[string]any); ok {
			filterProject, _ = f["project"].(string)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"updated": 0, "total": 0, "dry_run": true})
	}))
	defer srv.Close()

	s := NewServer()
	client := NewAPIClient(srv.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_bulk_update"].handler
	if _, err := handler(context.Background(), map[string]any{
		"project": "top-level",
		"filter":  map[string]any{"project": "nested-wins", "status": "draft"},
		"updates": map[string]any{"status": "pending"},
		"dry_run": true,
	}); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if filterProject != "nested-wins" {
		t.Errorf("explicit filter.project should win: got %q, want %q", filterProject, "nested-wins")
	}
}

// TestBrainInject_Handler_ForwardsProject verifies P1.a: brain_inject
// passes `project` in its POST body, which the extended types.InjectRequest
// now supports and the service applies as a SearchOptions.ProjectID filter.
func TestBrainInject_Handler_ForwardsProject(t *testing.T) {
	var bodyProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/inject" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		bodyProject, _ = body["project"].(string)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"context": "", "entries": []any{}})
	}))
	defer srv.Close()

	s := NewServer()
	client := NewAPIClient(srv.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_inject"].handler
	if _, err := handler(context.Background(), map[string]any{
		"query":   "auth flow",
		"project": "orion-ai",
	}); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if bodyProject != "orion-ai" {
		t.Errorf("brain_inject did not forward project: got %q, want %q", bodyProject, "orion-ai")
	}
}

// TestBrainStats_Handler_ForwardsProject verifies P1.b: brain_stats
// includes `project` as a query parameter, which the API handler now reads
// and the service uses as a path-prefix filter.
func TestBrainStats_Handler_ForwardsProject(t *testing.T) {
	var gotProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/stats" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotProject = r.URL.Query().Get("project")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"totalEntries": 5, "globalEntries": 0, "projectEntries": 5,
			"byType": map[string]int{"summary": 3, "plan": 2},
		})
	}))
	defer srv.Close()

	s := NewServer()
	client := NewAPIClient(srv.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_stats"].handler
	if _, err := handler(context.Background(), map[string]any{
		"project": "orion-ai",
	}); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if gotProject != "orion-ai" {
		t.Errorf("brain_stats did not forward project: got %q, want %q", gotProject, "orion-ai")
	}
}

// TestBrainStale_Handler_ForwardsProject verifies P1.c.
func TestBrainStale_Handler_ForwardsProject(t *testing.T) {
	var gotProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/stale" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotProject = r.URL.Query().Get("project")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": []any{}, "total": 0})
	}))
	defer srv.Close()

	s := NewServer()
	client := NewAPIClient(srv.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_stale"].handler
	if _, err := handler(context.Background(), map[string]any{
		"project": "orion-ai",
		"days":    float64(14),
	}); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if gotProject != "orion-ai" {
		t.Errorf("brain_stale did not forward project: got %q, want %q", gotProject, "orion-ai")
	}
}

// TestBrainOrphans_Handler_ForwardsProject verifies P1.d.
func TestBrainOrphans_Handler_ForwardsProject(t *testing.T) {
	var gotProject string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/orphans" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		gotProject = r.URL.Query().Get("project")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": []any{}, "total": 0})
	}))
	defer srv.Close()

	s := NewServer()
	client := NewAPIClient(srv.URL)
	RegisterBrainTools(s, client)

	handler := s.tools["brain_orphans"].handler
	if _, err := handler(context.Background(), map[string]any{
		"project": "orion-ai",
	}); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if gotProject != "orion-ai" {
		t.Errorf("brain_orphans did not forward project: got %q, want %q", gotProject, "orion-ai")
	}
}

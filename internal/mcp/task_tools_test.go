package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

// =============================================================================
// Tool Registration Tests
// =============================================================================

func TestRegisterTaskTools_Count(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	count := len(s.tools)
	if count != 14 {
		t.Errorf("expected 14 task tools registered, got %d", count)
	}
}

func TestRegisterTaskTools_Names(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	expectedTools := []string{
		"tasks",
		"task_next",
		"task_get",
		"task_metadata",
		"tasks_status",
		"task_trigger",
		"feature_review_enable",
		"feature_review_disable",
		"blocked_inspector_enable",
		"blocked_inspector_disable",
		"dream_enable",
		"dream_disable",
	}

	for _, name := range expectedTools {
		if _, ok := s.tools[name]; !ok {
			t.Errorf("tool %q not registered", name)
		}
	}
}

func TestRegisterTaskTools_AllHandlersSet(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	for name, rt := range s.tools {
		if rt.handler == nil {
			t.Errorf("tool %q has nil handler", name)
		}
	}
}

func TestRegisterTaskTools_Descriptions(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	for name, rt := range s.tools {
		if rt.tool.Description == "" {
			t.Errorf("tool %q has empty description", name)
		}
		if rt.tool.InputSchema.Type != "object" {
			t.Errorf("tool %q inputSchema.type = %q, want %q", name, rt.tool.InputSchema.Type, "object")
		}
	}
}

// =============================================================================
// Schema Tests
// =============================================================================

func TestBrainTasks_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	tool := s.tools["tasks"].tool

	// No required fields
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("brain_tasks required = %v, want []", tool.InputSchema.Required)
	}

	// Check properties exist
	expectedProps := []string{"status", "classification", "feature_id", "limit", "project"}
	for _, prop := range expectedProps {
		if _, ok := tool.InputSchema.Properties[prop]; !ok {
			t.Errorf("brain_tasks missing property %q", prop)
		}
	}

	// Check classification enum by CONTENT, not count. A bare length
	// assertion fails on any deliberate addition while saying nothing about
	// whether the right values are present.
	//
	// not_pending is in the list because the renderer groups it: without it
	// in the enum, the one bucket holding ~99% of a mature project's tasks
	// was unreachable by filter.
	classProp := tool.InputSchema.Properties["classification"]
	wantClassifications := map[string]bool{
		"ready": false, "waiting": false, "blocked": false, "not_pending": false,
	}
	for _, v := range classProp.Enum {
		if _, ok := wantClassifications[v]; !ok {
			t.Errorf("classification enum has unexpected value %q", v)
			continue
		}
		wantClassifications[v] = true
	}
	for v, seen := range wantClassifications {
		if !seen {
			t.Errorf("classification enum missing %q", v)
		}
	}
}

func TestBrainTaskGet_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	tool := s.tools["task_get"].tool

	// Required: task_id
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "task_id" {
		t.Errorf("task_get required = %v, want [task_id]", tool.InputSchema.Required)
	}

	// Property is snake_case and describes title matching
	prop, ok := tool.InputSchema.Properties["task_id"]
	if !ok {
		t.Fatal("task_get missing property 'task_id'")
	}
	if !strings.Contains(prop.Description, "or exact title") {
		t.Errorf("task_id description should mention 'or exact title', got: %s", prop.Description)
	}
}

func TestBrainTaskMetadata_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	tool := s.tools["task_metadata"].tool

	// Required: task_id
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "task_id" {
		t.Errorf("task_metadata required = %v, want [task_id]", tool.InputSchema.Required)
	}

	if _, ok := tool.InputSchema.Properties["task_id"]; !ok {
		t.Error("task_metadata missing property 'task_id'")
	}
}

func TestBrainTasksStatus_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	tool := s.tools["tasks_status"].tool

	// Required: task_ids
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "task_ids" {
		t.Errorf("tasks_status required = %v, want [task_ids]", tool.InputSchema.Required)
	}

	// Check task_ids is array type with string items
	taskIdsProp := tool.InputSchema.Properties["task_ids"]
	if taskIdsProp.Type != "array" {
		t.Errorf("task_ids type = %q, want %q", taskIdsProp.Type, "array")
	}
	if taskIdsProp.Items == nil || taskIdsProp.Items.Type != "string" {
		t.Error("task_ids items should be string type")
	}

	// Check wait_for enum and description
	waitForProp := tool.InputSchema.Properties["wait_for"]
	if len(waitForProp.Enum) != 2 {
		t.Errorf("wait_for enum has %d values, want 2", len(waitForProp.Enum))
	}
	if !strings.Contains(waitForProp.Description, "'completed' waits until ALL listed tasks are done") {
		t.Errorf("wait_for description mismatch, got: %s", waitForProp.Description)
	}
	if !strings.Contains(waitForProp.Description, "'any' returns on the first status change among them") {
		t.Errorf("wait_for description mismatch, got: %s", waitForProp.Description)
	}
}

func TestBrainTaskTrigger_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	tool := s.tools["task_trigger"].tool

	// Required: task_id
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "task_id" {
		t.Errorf("task_trigger required = %v, want [task_id]", tool.InputSchema.Required)
	}

	if _, ok := tool.InputSchema.Properties["task_id"]; !ok {
		t.Error("task_trigger missing property 'task_id'")
	}
}

func TestBrainFeatureReviewEnable_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	tool := s.tools["feature_review_enable"].tool

	// Required: project, feature_id
	if len(tool.InputSchema.Required) != 2 {
		t.Errorf("brain_feature_review_enable required fields = %d, want 2", len(tool.InputSchema.Required))
	}
}

func TestBrainBlockedInspectorEnable_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	tool := s.tools["blocked_inspector_enable"].tool

	// Required: project, feature_id
	if len(tool.InputSchema.Required) != 2 {
		t.Errorf("brain_blocked_inspector_enable required fields = %d, want 2", len(tool.InputSchema.Required))
	}

	// Has optional schedule
	if _, ok := tool.InputSchema.Properties["schedule"]; !ok {
		t.Error("brain_blocked_inspector_enable missing 'schedule' property")
	}
}

// =============================================================================
// Handler Tests
// =============================================================================

func TestBrainTasks_Handler(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/tasks/test-project" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"tasks": []map[string]any{
				{
					"id": "abc12345", "title": "Ready Task", "status": "pending",
					"priority": "high", "classification": "ready",
					"dependsOn": []map[string]any{},
				},
				{
					"id": "def67890", "title": "Waiting Task", "status": "pending",
					"priority": "medium", "classification": "waiting",
					"dependsOn": []map[string]any{
						{"id": "abc12345", "title": "Ready Task", "status": "pending"},
					},
				},
				{
					"id": "ghi11111", "title": "Blocked Task", "status": "pending",
					"priority": "low", "classification": "blocked",
					"blocked_by_reason": "circular dependency",
				},
			},
			"count": 3,
			"stats": map[string]int{
				"ready": 1, "waiting": 1, "blocked": 1, "completed": 0, "total": 3,
			},
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["tasks"].handler
	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// Check project header
	if !strings.Contains(result, "test-project") {
		t.Errorf("result should contain project name, got: %s", result)
	}

	// Check stats
	if !strings.Contains(result, "1 ready") {
		t.Errorf("result should contain stats, got: %s", result)
	}

	// Check ready section
	if !strings.Contains(result, "Ready (can start now)") {
		t.Errorf("result should contain ready section, got: %s", result)
	}
	if !strings.Contains(result, "[HIGH] Ready Task") {
		t.Errorf("result should contain ready task with priority, got: %s", result)
	}

	// Check waiting section
	if !strings.Contains(result, "Waiting (deps incomplete)") {
		t.Errorf("result should contain waiting section, got: %s", result)
	}

	// Check blocked section
	if !strings.Contains(result, "### Blocked") {
		t.Errorf("result should contain blocked section, got: %s", result)
	}
	if !strings.Contains(result, "circular dependency") {
		t.Errorf("result should contain blocked reason, got: %s", result)
	}
}

// TestBrainTasks_StatsSplitBlocked verifies that the stats line surfaces both
// dep_blocked (classification-blocked) and status_blocked (Status == "blocked")
// as separate counters. See task ghtzzp1x / plan 24urhmtl (Finding 4).
//
// The fixture mixes four combinations:
//   - dep-blocked-only:    classification="blocked", status="pending"
//   - status-blocked-only: classification="waiting",  status="blocked"
//   - both:                classification="blocked", status="blocked"
//   - neither:             classification="ready",    status="pending"
//
// Expected counts:
//   - dep_blocked    = 2 (both classification="blocked" entries)
//   - status_blocked = 2 (both status="blocked" entries)
func TestBrainTasks_StatsSplitBlocked(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"tasks": []map[string]any{
				{
					"id": "aaaa1111", "title": "Dep blocked only", "status": "pending",
					"priority": "medium", "classification": "blocked",
					"blocked_by_reason": "waiting on X",
				},
				{
					"id": "bbbb2222", "title": "Status blocked only", "status": "blocked",
					"priority": "medium", "classification": "waiting",
					"dependsOn": []map[string]any{},
				},
				{
					"id": "cccc3333", "title": "Both blocked", "status": "blocked",
					"priority": "medium", "classification": "blocked",
					"blocked_by_reason": "waiting on Y",
				},
				{
					"id": "dddd4444", "title": "Neither blocked", "status": "pending",
					"priority": "medium", "classification": "ready",
					"dependsOn": []map[string]any{},
				},
			},
			"count": 4,
			// Server-side stats reports classification-blocked count (2).
			// The MCP tool computes status_blocked locally from the tasks array.
			"stats": map[string]int{
				"ready": 1, "waiting": 1, "blocked": 2, "completed": 0, "total": 4,
			},
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["tasks"].handler
	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// Assert stats line surfaces dep_blocked = 2.
	if !strings.Contains(result, "2 dep_blocked") {
		t.Errorf("stats line should report '2 dep_blocked', got: %s", result)
	}

	// Assert stats line surfaces status_blocked = 2.
	if !strings.Contains(result, "2 status_blocked") {
		t.Errorf("stats line should report '2 status_blocked', got: %s", result)
	}

	// Legacy: we do NOT keep an ambiguous "blocked" counter alongside the
	// split counters. This prevents the "N blocked" wording that conflated
	// the two meanings. If a legacy field ever reappears it must alias one
	// of the two counters explicitly (see code comment in task_tools.go).
	if strings.Contains(result, " blocked |") || strings.Contains(result, "| 2 blocked") {
		t.Errorf("legacy ambiguous 'blocked' counter must not appear in stats line, got: %s", result)
	}
}

func TestBrainTasks_FilterByClassification(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"tasks": []map[string]any{
				{"id": "abc", "title": "Ready Task", "status": "pending", "priority": "high", "classification": "ready"},
				{"id": "def", "title": "Waiting Task", "status": "pending", "priority": "medium", "classification": "waiting"},
			},
			"count": 2,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["tasks"].handler
	result, err := handler(context.Background(), map[string]any{
		"classification": "ready",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// Should only show ready tasks
	if !strings.Contains(result, "Ready Task") {
		t.Errorf("result should contain ready task, got: %s", result)
	}
	if strings.Contains(result, "Waiting Task") {
		t.Errorf("result should NOT contain waiting task when filtered, got: %s", result)
	}
}

func TestBrainTasks_EmptyResult(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"tasks": []map[string]any{},
			"count": 0,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["tasks"].handler
	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "No tasks found") {
		t.Errorf("result should indicate no tasks, got: %s", result)
	}
}

func TestBrainTaskNext_Handler(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/api/v1/tasks/test-project/next" {
			// A BARE ResolvedTask, which is what api/tasks.go:174 writes.
			// This fixture used to wrap it in {"task": ...} — an envelope
			// the server has never sent. The mock and the hand-rolled
			// decode struct agreed with each other and were both wrong
			// about the server, so the test passed while the tool reported
			// "No ready tasks available" for a queue full of ready work.
			json.NewEncoder(w).Encode(types.ResolvedTask{
				ID: "abc12345", Path: "projects/test/task/abc12345.md",
				Title: "Next Task", Status: "pending", Priority: "high",
				Classification: "ready", ResolvedDeps: []string{"dep1"},
			})
			return
		}

		if strings.HasPrefix(r.URL.Path, "/api/v1/entries/") {
			json.NewEncoder(w).Encode(map[string]any{
				"id": "abc12345", "path": "projects/test/task/abc12345.md",
				"title": "Next Task", "type": "task", "status": "pending",
				"content":               "## Implementation\n\nDo the thing.",
				"tags":                  []string{"feature"},
				"user_original_request": "Build the auth module",
			})
			return
		}

		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["task_next"].handler
	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Next Task") {
		t.Errorf("result should contain task title, got: %s", result)
	}
	if !strings.Contains(result, "HIGH") {
		t.Errorf("result should contain priority, got: %s", result)
	}
	if !strings.Contains(result, "Build the auth module") {
		t.Errorf("result should contain user original request, got: %s", result)
	}
	if !strings.Contains(result, "1 dependencies (all satisfied)") {
		t.Errorf("result should contain dependency count, got: %s", result)
	}
	if !strings.Contains(result, "Do the thing") {
		t.Errorf("result should contain content, got: %s", result)
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls (next + entry), got %d", callCount)
	}
}

func TestBrainTaskNext_NoReadyTasks(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/api/v1/tasks/test-project/next" {
			json.NewEncoder(w).Encode(map[string]any{
				"task":    nil,
				"message": "No ready tasks",
			})
			return
		}

		if r.URL.Path == "/api/v1/tasks/test-project" {
			json.NewEncoder(w).Encode(map[string]any{
				"tasks": []map[string]any{
					// One status-blocked task in the fixture so we can
					// assert the split counter surfaces.
					{
						"id": "sss11111", "title": "Explicitly blocked",
						"status": "blocked", "priority": "medium",
						"classification": "waiting",
					},
				},
				"count": 1,
				"stats": map[string]int{
					"ready": 0, "waiting": 3, "blocked": 1,
					"status_blocked": 1, "completed": 5,
				},
			})
			return
		}
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["task_next"].handler
	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "No ready tasks") {
		t.Errorf("result should indicate no ready tasks, got: %s", result)
	}
	if !strings.Contains(result, "3 tasks waiting") {
		t.Errorf("result should contain waiting count, got: %s", result)
	}
	// Split counters (task ghtzzp1x): dep_blocked and status_blocked
	// are reported independently. Ambiguous "N tasks blocked" wording
	// must not appear.
	if !strings.Contains(result, "1 tasks dep_blocked") {
		t.Errorf("result should contain dep_blocked count, got: %s", result)
	}
	if !strings.Contains(result, "1 tasks status_blocked") {
		t.Errorf("result should contain status_blocked count, got: %s", result)
	}
	if strings.Contains(result, "tasks blocked\n") || strings.Contains(result, "- 1 tasks blocked") {
		t.Errorf("legacy ambiguous 'tasks blocked' wording must not appear, got: %s", result)
	}
}

func TestBrainTaskGet_Handler(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/api/v1/tasks/test-project" {
			json.NewEncoder(w).Encode(map[string]any{
				"tasks": []map[string]any{
					{
						"id": "abc12345", "title": "Target Task", "path": "projects/test/task/abc12345.md",
						"status": "pending", "priority": "high", "classification": "ready",
						"resolved_deps": []string{},
						"depends_on":    []string{"dep1"},
					},
					{
						"id": "dep1", "title": "Dep Task", "path": "projects/test/task/dep1.md",
						"status": "completed", "priority": "high", "classification": "ready",
						"resolved_deps": []string{},
						"depends_on":    []string{},
					},
					{
						"id": "xyz99999", "title": "Dependent Task", "path": "projects/test/task/xyz99999.md",
						"status": "pending", "priority": "medium", "classification": "waiting",
						"resolved_deps": []string{"abc12345"},
						"depends_on":    []string{"abc12345"},
					},
				},
				"count": 3,
			})
			return
		}

		if strings.HasPrefix(r.URL.Path, "/api/v1/entries/") {
			json.NewEncoder(w).Encode(map[string]any{
				"id": "abc12345", "path": "projects/test/task/abc12345.md",
				"title": "Target Task", "type": "task", "status": "pending",
				"content":               "Task content here",
				"tags":                  []string{},
				"user_original_request": "Build feature X",
			})
			return
		}
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["task_get"].handler
	result, err := handler(context.Background(), map[string]any{
		"taskId": "abc12345",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// Check task details
	if !strings.Contains(result, "Target Task") {
		t.Errorf("result should contain task title, got: %s", result)
	}
	if !strings.Contains(result, "abc12345") {
		t.Errorf("result should contain task ID, got: %s", result)
	}

	// Check dependencies section
	if !strings.Contains(result, "Dependencies (what this task needs)") {
		t.Errorf("result should contain dependencies section, got: %s", result)
	}
	if !strings.Contains(result, "Dep Task") {
		t.Errorf("result should contain dependency, got: %s", result)
	}

	// Check dependents section
	if !strings.Contains(result, "Dependents (what needs this task)") {
		t.Errorf("result should contain dependents section, got: %s", result)
	}
	if !strings.Contains(result, "Dependent Task") {
		t.Errorf("result should contain dependent, got: %s", result)
	}

	// Check user original request
	if !strings.Contains(result, "Build feature X") {
		t.Errorf("result should contain user original request, got: %s", result)
	}

	// Check content
	if !strings.Contains(result, "Task content here") {
		t.Errorf("result should contain content, got: %s", result)
	}
}

func TestBrainTaskGet_DeepDependencyChain(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-depends"}
	defer func() { cachedContext = nil }()

	// Simulate the test-depends project with deep dependency chains:
	// Level 0: db-schema, auth-module, api-framework (no deps)
	// Level 1: registration, login (depend on all 3 foundations)
	// Level 2: auth-middleware, profile (depend on registration + login)
	// Level 3: protected-routes (depends on middleware + profile)
	// Level 4: e2e-tests (depends on middleware + profile + registration + login)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		if r.URL.Path == "/api/v1/tasks/test-depends" {
			json.NewEncoder(w).Encode(map[string]any{
				"tasks": []map[string]any{
					// Level 0 - foundations
					{"id": "nt9htpts", "title": "Setup database schema", "path": "projects/test-depends/task/nt9htpts.md",
						"status": "pending", "priority": "high", "classification": "ready",
						"depends_on": []string{}, "resolved_deps": []string{}},
					{"id": "ef9u5d82", "title": "Setup authentication module", "path": "projects/test-depends/task/ef9u5d82.md",
						"status": "pending", "priority": "high", "classification": "ready",
						"depends_on": []string{}, "resolved_deps": []string{}},
					{"id": "rnjjcchj", "title": "Setup API framework", "path": "projects/test-depends/task/rnjjcchj.md",
						"status": "pending", "priority": "high", "classification": "ready",
						"depends_on": []string{}, "resolved_deps": []string{}},
					// Level 1 - depend on all 3 foundations
					{"id": "yvrsnpf2", "title": "Build user registration endpoint", "path": "projects/test-depends/task/yvrsnpf2.md",
						"status": "pending", "priority": "high", "classification": "waiting",
						"depends_on": []string{"nt9htpts", "ef9u5d82", "rnjjcchj"}, "resolved_deps": []string{"nt9htpts", "ef9u5d82", "rnjjcchj"}},
					{"id": "q4bqjmoh", "title": "Build login endpoint", "path": "projects/test-depends/task/q4bqjmoh.md",
						"status": "pending", "priority": "high", "classification": "waiting",
						"depends_on": []string{"nt9htpts", "ef9u5d82", "rnjjcchj"}, "resolved_deps": []string{"nt9htpts", "ef9u5d82", "rnjjcchj"}},
					// Level 2 - depend on level 1
					{"id": "pfa6dv3h", "title": "Build auth middleware", "path": "projects/test-depends/task/pfa6dv3h.md",
						"status": "pending", "priority": "medium", "classification": "waiting",
						"depends_on": []string{"yvrsnpf2", "q4bqjmoh"}, "resolved_deps": []string{"yvrsnpf2", "q4bqjmoh"}},
					{"id": "nhrol3mx", "title": "Build user profile endpoint", "path": "projects/test-depends/task/nhrol3mx.md",
						"status": "pending", "priority": "medium", "classification": "waiting",
						"depends_on": []string{"yvrsnpf2", "q4bqjmoh"}, "resolved_deps": []string{"yvrsnpf2", "q4bqjmoh"}},
					// Level 3 - depends on level 2
					{"id": "z5gahc9i", "title": "Build protected API routes", "path": "projects/test-depends/task/z5gahc9i.md",
						"status": "pending", "priority": "medium", "classification": "waiting",
						"depends_on": []string{"pfa6dv3h", "nhrol3mx"}, "resolved_deps": []string{"pfa6dv3h", "nhrol3mx"}},
					// Level 4 - depends on levels 1+2
					{"id": "0if2p4bc", "title": "End-to-end integration tests", "path": "projects/test-depends/task/0if2p4bc.md",
						"status": "pending", "priority": "low", "classification": "waiting",
						"depends_on": []string{"pfa6dv3h", "nhrol3mx", "yvrsnpf2", "q4bqjmoh"}, "resolved_deps": []string{"pfa6dv3h", "nhrol3mx", "yvrsnpf2", "q4bqjmoh"}},
				},
				"count": 9,
			})
			return
		}

		if strings.HasPrefix(r.URL.Path, "/api/v1/entries/") {
			// Return content for whichever task is requested
			taskPath := strings.TrimPrefix(r.URL.Path, "/api/v1/entries/")
			json.NewEncoder(w).Encode(map[string]any{
				"id": "0if2p4bc", "path": taskPath,
				"title": "End-to-end integration tests", "type": "task", "status": "pending",
				"content": "Write comprehensive integration tests.", "tags": []string{},
			})
			return
		}
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	// Test the deepest task (level 4) - has 4 dependencies
	handler := s.tools["task_get"].handler
	result, err := handler(context.Background(), map[string]any{
		"taskId": "0if2p4bc",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	t.Logf("=== brain_task_get output for deepest task ===\n%s", result)

	// Must show all 4 dependencies (the bug was: always showed "No dependencies")
	if strings.Contains(result, "*No dependencies*") {
		t.Fatal("BUG: still showing 'No dependencies' - JSON deserialization mismatch not fixed")
	}
	if !strings.Contains(result, "Build auth middleware") {
		t.Error("missing dependency: Build auth middleware")
	}
	if !strings.Contains(result, "Build user profile endpoint") {
		t.Error("missing dependency: Build user profile endpoint")
	}
	if !strings.Contains(result, "Build user registration endpoint") {
		t.Error("missing dependency: Build user registration endpoint")
	}
	if !strings.Contains(result, "Build login endpoint") {
		t.Error("missing dependency: Build login endpoint")
	}

	// Verify no tasks depend on the deepest task
	if !strings.Contains(result, "*No tasks depend on this one*") {
		t.Error("deepest task should have no dependents")
	}

	// Now test a mid-level task (level 1 registration) - has 3 deps, 3 dependents
	result2, err := handler(context.Background(), map[string]any{
		"taskId": "yvrsnpf2",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	t.Logf("=== brain_task_get output for mid-level task ===\n%s", result2)

	if strings.Contains(result2, "*No dependencies*") {
		t.Fatal("BUG: mid-level task still showing 'No dependencies'")
	}
	// Should show 3 foundation deps
	if !strings.Contains(result2, "Setup database schema") {
		t.Error("missing dependency: Setup database schema")
	}
	if !strings.Contains(result2, "Setup authentication module") {
		t.Error("missing dependency: Setup authentication module")
	}
	if !strings.Contains(result2, "Setup API framework") {
		t.Error("missing dependency: Setup API framework")
	}
	// Should show dependents
	if !strings.Contains(result2, "Build auth middleware") {
		t.Error("missing dependent: Build auth middleware")
	}

	// Test a foundation task (level 0) - no deps, has dependents
	result3, err := handler(context.Background(), map[string]any{
		"taskId": "nt9htpts",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	t.Logf("=== brain_task_get output for foundation task ===\n%s", result3)

	// Foundation task correctly has no dependencies
	if !strings.Contains(result3, "*No dependencies*") {
		t.Error("foundation task should have no dependencies")
	}
	// But should have dependents
	if strings.Contains(result3, "*No tasks depend on this one*") {
		t.Error("foundation task should have dependents")
	}
}

func TestBrainTaskGet_NotFound(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"tasks": []map[string]any{
				{"id": "abc12345", "title": "Some Task", "path": "p/task/abc.md", "status": "pending", "priority": "high", "classification": "ready"},
			},
			"count": 1,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["task_get"].handler
	result, err := handler(context.Background(), map[string]any{
		"taskId": "nonexistent",
	})
	if err == nil {
		t.Fatalf("expected not-found error, got result: %s", result)
	}

	if !strings.Contains(err.Error(), `task not found: "nonexistent"`) {
		t.Errorf("error should indicate not found, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Use the tasks tool to list all tasks") {
		t.Errorf("error should point at the tasks tool, got: %v", err)
	}
}

func TestBrainTaskGet_PartialMatch(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"tasks": []map[string]any{
				{"id": "abc12345", "title": "Build Auth Module", "path": "p/task/abc.md", "status": "pending", "priority": "high", "classification": "ready"},
			},
			"count": 1,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["task_get"].handler
	result, err := handler(context.Background(), map[string]any{
		"taskId": "auth",
	})
	if err == nil {
		t.Fatalf("expected not-found error with suggestions, got result: %s", result)
	}

	if !strings.Contains(err.Error(), `task not found: "auth"`) {
		t.Errorf("error should indicate not found, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Did you mean") {
		t.Errorf("error should suggest partial matches, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Build Auth Module") {
		t.Errorf("error should contain suggestion, got: %v", err)
	}
}

func TestBrainTaskGet_MissingTaskId(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:1")
	RegisterTaskTools(s, client)

	handler := s.tools["task_get"].handler
	result, err := handler(context.Background(), map[string]any{})
	if err == nil {
		t.Fatalf("expected validation error, got result: %s", result)
	}

	if !strings.Contains(err.Error(), "provide a 'task_id' (ID or title)") {
		t.Errorf("error should ask for task_id, got: %v", err)
	}
}

func TestBrainTaskMetadata_Handler(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"tasks": []map[string]any{
				{
					"id": "abc12345", "title": "Test Task", "path": "projects/test/task/abc12345.md",
					"status": "pending", "priority": "high", "classification": "ready",
					"depends_on": []string{}, "resolved_deps": []string{},
					"unresolved_deps": []string{}, "blocked_by": []string{},
					"waiting_on": []string{}, "in_cycle": false,
					"tags": []string{"feature"}, "created": "2024-01-01T00:00:00Z",
					"agent": "tdd-dev", "model": "anthropic/claude-sonnet-4-20250514",
					"git_branch": "feature-branch", "git_remote": "origin",
					"execution_mode": "worktree", "complete_on_idle": true,
					"merge_target_branch": "main", "merge_policy": "auto_merge",
					"merge_strategy": "squash", "remote_branch_policy": "delete",
					"open_pr_before_merge": true,
					"feature_id":           "auth-system", "feature_priority": "high",
				},
			},
			"count": 1,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["task_metadata"].handler
	result, err := handler(context.Background(), map[string]any{
		"taskId": "abc12345",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// Result should be valid JSON
	var metadata map[string]any
	if err := json.Unmarshal([]byte(result), &metadata); err != nil {
		t.Fatalf("result should be valid JSON: %v\nGot: %s", err, result)
	}

	// Check top-level fields
	if metadata["id"] != "abc12345" {
		t.Errorf("id = %v, want abc12345", metadata["id"])
	}

	// Check execution config
	exec, ok := metadata["execution"].(map[string]any)
	if !ok {
		t.Fatal("missing execution config")
	}
	if exec["agent"] != "tdd-dev" {
		t.Errorf("agent = %v, want tdd-dev", exec["agent"])
	}
	if exec["execution_mode"] != "worktree" {
		t.Errorf("execution.execution_mode = %v, want worktree", exec["execution_mode"])
	}
	if exec["complete_on_idle"] != true {
		t.Errorf("execution.complete_on_idle = %v, want true", exec["complete_on_idle"])
	}

	// Check merge intent
	merge, ok := metadata["merge"].(map[string]any)
	if !ok {
		t.Fatal("missing merge config")
	}
	if merge["merge_target_branch"] != "main" {
		t.Errorf("merge.merge_target_branch = %v, want main", merge["merge_target_branch"])
	}
	if merge["merge_policy"] != "auto_merge" {
		t.Errorf("merge.merge_policy = %v, want auto_merge", merge["merge_policy"])
	}
	if merge["merge_strategy"] != "squash" {
		t.Errorf("merge.merge_strategy = %v, want squash", merge["merge_strategy"])
	}
	if merge["remote_branch_policy"] != "delete" {
		t.Errorf("merge.remote_branch_policy = %v, want delete", merge["remote_branch_policy"])
	}
	if merge["open_pr_before_merge"] != true {
		t.Errorf("merge.open_pr_before_merge = %v, want true", merge["open_pr_before_merge"])
	}

	// Check feature grouping
	feature, ok := metadata["feature"].(map[string]any)
	if !ok {
		t.Fatal("missing feature config")
	}
	if feature["id"] != "auth-system" {
		t.Errorf("feature.id = %v, want auth-system", feature["id"])
	}
}

func TestBrainTaskMetadata_NotFound(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"tasks": []map[string]any{},
			"count": 0,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["task_metadata"].handler
	result, err := handler(context.Background(), map[string]any{
		"taskId": "nonexistent",
	})
	if err == nil {
		t.Fatalf("expected not-found error, got result: %s", result)
	}

	if !strings.Contains(err.Error(), `task not found: "nonexistent"`) {
		t.Errorf("error should indicate not found, got: %v", err)
	}
	if !strings.Contains(err.Error(), "Use the tasks tool to list all tasks") {
		t.Errorf("error should point at the tasks tool, got: %v", err)
	}
}

func TestBrainTasksStatus_Handler(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/tasks/test-project/status" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)

		// Verify taskIds were sent
		taskIds, ok := body["taskIds"].([]any)
		if !ok || len(taskIds) != 2 {
			t.Errorf("expected 2 taskIds, got %v", body["taskIds"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"tasks": []map[string]any{
				{"id": "abc12345", "title": "Task A", "status": "completed", "priority": "high", "classification": "ready"},
				{"id": "def67890", "title": "Task B", "status": "pending", "priority": "medium", "classification": "waiting"},
			},
			"notFound": []string{},
			"changed":  false,
			"timedOut": false,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["tasks_status"].handler
	result, err := handler(context.Background(), map[string]any{
		"taskIds": []any{"abc12345", "def67890"},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Task Status Check") {
		t.Errorf("result should contain header, got: %s", result)
	}
	if !strings.Contains(result, "Task A") {
		t.Errorf("result should contain task A, got: %s", result)
	}
	if !strings.Contains(result, "1/2 tasks completed") {
		t.Errorf("result should contain summary, got: %s", result)
	}
}

func TestBrainTasksStatus_EmptyTaskIds(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:1")
	RegisterTaskTools(s, client)

	handler := s.tools["tasks_status"].handler
	result, err := handler(context.Background(), map[string]any{
		"taskIds": []any{},
	})
	if err == nil {
		t.Fatalf("expected validation error, got result: %s", result)
	}

	if !strings.Contains(err.Error(), "provide at least one task ID in 'task_ids'") {
		t.Errorf("error should ask for task IDs, got: %v", err)
	}
}

func TestBrainTasksStatus_WithNotFound(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Mirror what the server actually sends. This fixture used to
		// return notFound/changed/timedOut, none of which exist on
		// types.MultiTaskStatusResponse — a mock asserting a contract the
		// server has never had, which is what let the phantom fields look
		// real. A requested id simply does not come back in tasks.
		json.NewEncoder(w).Encode(types.MultiTaskStatusResponse{
			Tasks:        []types.ResolvedTask{},
			AllCompleted: false,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["tasks_status"].handler
	result, err := handler(context.Background(), map[string]any{
		"taskIds": []any{"missing123"},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Not Found") {
		t.Errorf("result should contain not found section, got: %s", result)
	}
	if !strings.Contains(result, "missing123") {
		t.Errorf("result should contain missing ID, got: %s", result)
	}
}

func TestBrainTaskTrigger_Handler(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			t.Errorf("method = %q, want POST", r.Method)
		}
		if r.URL.Path != "/api/v1/tasks/test-project/abc12345/trigger" {
			t.Errorf("path = %q, want /api/v1/tasks/test-project/abc12345/trigger", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"taskId":  "abc12345",
			"message": "triggered",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["task_trigger"].handler
	result, err := handler(context.Background(), map[string]any{
		"taskId": "abc12345",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	// Result should be JSON
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result should be valid JSON: %v", err)
	}
	if parsed["operation"] != "task_trigger" {
		t.Errorf("operation = %v, want task_trigger", parsed["operation"])
	}
}

func TestBrainTaskTrigger_Error(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not_found", "message": "task not found"})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["task_trigger"].handler
	result, err := handler(context.Background(), map[string]any{
		"taskId": "nonexistent",
	})
	// Trigger returns error as JSON, not as Go error
	if err != nil {
		t.Fatalf("handler should not return Go error: %v", err)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		t.Fatalf("result should be valid JSON: %v", err)
	}
	if parsed["error"] == nil {
		t.Error("result should contain error field")
	}
}

func TestBrainFeatureReviewEnable_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/monitors" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		// Decode into the real request type. Asserting raw JSON keys is what
		// let this test pass while the tool sent "templateId" and a nested
		// "scope" — neither of which exists on types.CreateMonitorRequest,
		// so every call was an HTTP 400.
		var body types.CreateMonitorRequest
		json.NewDecoder(r.Body).Decode(&body)
		if body.TemplateID != "feature-review" {
			t.Errorf("template_id = %q, want feature-review", body.TemplateID)
		}
		if body.ScopeType != "feature" {
			t.Errorf("scope_type = %q, want feature (empty means HTTP 400)", body.ScopeType)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id":    "rev123",
			"path":  "projects/test/task/rev123.md",
			"title": "Feature Review: auth-system",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["feature_review_enable"].handler
	result, err := handler(context.Background(), map[string]any{
		"project":    "test-project",
		"feature_id": "auth-system",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Feature Code Review enabled") {
		t.Errorf("result should confirm enablement, got: %s", result)
	}
	if !strings.Contains(result, "rev123") {
		t.Errorf("result should contain task ID, got: %s", result)
	}
}

func TestBrainFeatureReviewDisable_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/api/v1/monitors/by-scope" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		// Verify DELETE body is sent (templateId + scope)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["templateId"] != "feature-review" {
			t.Errorf("templateId = %v, want feature-review", body["templateId"])
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": "deleted",
			"taskId":  "rev123",
			"path":    "projects/test/task/rev123.md",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["feature_review_disable"].handler
	result, err := handler(context.Background(), map[string]any{
		"project":    "test-project",
		"feature_id": "auth-system",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Feature Code Review disabled") {
		t.Errorf("result should confirm disablement, got: %s", result)
	}
	if !strings.Contains(result, "rev123") {
		t.Errorf("result should contain task ID, got: %s", result)
	}
}

func TestBrainBlockedInspectorEnable_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/monitors" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		// Decode into the real request type. Asserting raw JSON keys is what
		// let this test pass while the tool sent "templateId" and a nested
		// "scope" — neither of which exists on types.CreateMonitorRequest,
		// so every call was an HTTP 400.
		var body types.CreateMonitorRequest
		json.NewDecoder(r.Body).Decode(&body)
		if body.TemplateID != "blocked-inspector" {
			t.Errorf("template_id = %q, want blocked-inspector", body.TemplateID)
		}
		if body.ScopeType != "feature" {
			t.Errorf("scope_type = %q, want feature (empty means HTTP 400)", body.ScopeType)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id":    "insp123",
			"path":  "projects/test/task/insp123.md",
			"title": "Blocked Inspector: auth-system",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["blocked_inspector_enable"].handler
	result, err := handler(context.Background(), map[string]any{
		"project":    "test-project",
		"feature_id": "auth-system",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Blocked Task Inspector enabled") {
		t.Errorf("result should confirm enablement, got: %s", result)
	}
	if !strings.Contains(result, "insp123") {
		t.Errorf("result should contain task ID, got: %s", result)
	}
}

func TestBrainBlockedInspectorEnable_WithSchedule(t *testing.T) {
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id": "insp123", "path": "p/task/insp123.md", "title": "Inspector",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["blocked_inspector_enable"].handler
	_, err := handler(context.Background(), map[string]any{
		"project":    "test-project",
		"feature_id": "auth-system",
		"schedule":   "*/15 * * * *",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if capturedBody["schedule"] != "*/15 * * * *" {
		t.Errorf("schedule = %v, want */15 * * * *", capturedBody["schedule"])
	}
}

func TestBrainBlockedInspectorDisable_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/api/v1/monitors/by-scope" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		// Verify DELETE body is sent (templateId + scope)
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["templateId"] != "blocked-inspector" {
			t.Errorf("templateId = %v, want blocked-inspector", body["templateId"])
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": "deleted",
			"taskId":  "insp123",
			"path":    "projects/test/task/insp123.md",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["blocked_inspector_disable"].handler
	result, err := handler(context.Background(), map[string]any{
		"project":    "test-project",
		"feature_id": "auth-system",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Blocked Task Inspector disabled") {
		t.Errorf("result should confirm disablement, got: %s", result)
	}
}

func TestBrainDreamEnable_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	tool := s.tools["dream_enable"].tool

	// Required: project only
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "project" {
		t.Errorf("brain_dream_enable required = %v, want [project]", tool.InputSchema.Required)
	}

	// Has optional schedule
	if _, ok := tool.InputSchema.Properties["schedule"]; !ok {
		t.Error("brain_dream_enable missing 'schedule' property")
	}

	// Has project property
	if _, ok := tool.InputSchema.Properties["project"]; !ok {
		t.Error("brain_dream_enable missing 'project' property")
	}
}

func TestBrainDreamDisable_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	tool := s.tools["dream_disable"].tool

	// Required: project only
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "project" {
		t.Errorf("brain_dream_disable required = %v, want [project]", tool.InputSchema.Required)
	}

	// Has project property
	if _, ok := tool.InputSchema.Properties["project"]; !ok {
		t.Error("brain_dream_disable missing 'project' property")
	}
}

func TestBrainDreamEnable_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/monitors" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		// Decode into the real request type. Asserting raw JSON keys is what
		// let this test pass while the tool sent "templateId" and a nested
		// "scope" — neither of which exists on types.CreateMonitorRequest,
		// so every call was an HTTP 400.
		var body types.CreateMonitorRequest
		json.NewDecoder(r.Body).Decode(&body)
		if body.TemplateID != "dream" {
			t.Errorf("template_id = %q, want dream", body.TemplateID)
		}
		if body.ScopeType != "project" {
			t.Errorf("scope_type = %q, want project (empty means HTTP 400)", body.ScopeType)
		}

		// Verify the monitor is project-scoped, not feature-scoped.
		// scope_type is already asserted above; this pins the project and
		// that no feature leaks in.
		if body.Project != "my-project" {
			t.Errorf("project = %q, want my-project", body.Project)
		}
		if body.FeatureID != "" {
			t.Errorf("feature_id = %q, want empty for a project-scoped monitor", body.FeatureID)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id":    "dream123",
			"path":  "projects/my-project/task/dream123.md",
			"title": "Dream: my-project",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["dream_enable"].handler
	result, err := handler(context.Background(), map[string]any{
		"project": "my-project",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Dream Mode enabled") {
		t.Errorf("result should confirm enablement, got: %s", result)
	}
	if !strings.Contains(result, "dream123") {
		t.Errorf("result should contain task ID, got: %s", result)
	}
}

func TestBrainDreamEnable_WithSchedule(t *testing.T) {
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"id": "dream123", "path": "p/task/dream123.md", "title": "Dream",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["dream_enable"].handler
	_, err := handler(context.Background(), map[string]any{
		"project":  "my-project",
		"schedule": "0 3 * * *",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if capturedBody["schedule"] != "0 3 * * *" {
		t.Errorf("schedule = %v, want 0 3 * * *", capturedBody["schedule"])
	}
}

func TestBrainDreamEnable_Conflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]string{"error": "conflict", "message": "409 conflict: monitor already exists"})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["dream_enable"].handler
	result, err := handler(context.Background(), map[string]any{
		"project": "my-project",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "already enabled") {
		t.Errorf("result should indicate already enabled, got: %s", result)
	}
}

func TestBrainDreamDisable_Handler(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/api/v1/monitors/by-scope" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["templateId"] != "dream" {
			t.Errorf("templateId = %v, want dream", body["templateId"])
		}

		// Verify scope is project-scoped
		scope, ok := body["scope"].(map[string]any)
		if !ok {
			t.Fatal("missing scope in request body")
		}
		if scope["type"] != "project" {
			t.Errorf("scope.type = %v, want project", scope["type"])
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{
			"message": "deleted",
			"taskId":  "dream123",
			"path":    "projects/my-project/task/dream123.md",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["dream_disable"].handler
	result, err := handler(context.Background(), map[string]any{
		"project": "my-project",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Dream Mode disabled") {
		t.Errorf("result should confirm disablement, got: %s", result)
	}
	if !strings.Contains(result, "dream123") {
		t.Errorf("result should contain task ID, got: %s", result)
	}
}

func TestBrainDreamDisable_NotEnabled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]string{"error": "not_found", "message": "not found"})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["dream_disable"].handler
	result, err := handler(context.Background(), map[string]any{
		"project": "my-project",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "not currently enabled") {
		t.Errorf("result should indicate not enabled, got: %s", result)
	}
}

// =============================================================================
// Helper function tests
// =============================================================================

func TestPriorityLabel(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"high", "[HIGH]"},
		{"medium", "[MED]"},
		{"low", "[LOW]"},
		{"", "[LOW]"},
	}
	for _, tt := range tests {
		got := priorityLabel(tt.input)
		if got != tt.want {
			t.Errorf("priorityLabel(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestPriorityLabelUpper(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"high", "HIGH"},
		{"medium", "MEDIUM"},
		{"low", "LOW"},
		{"", "LOW"},
	}
	for _, tt := range tests {
		got := priorityLabelUpper(tt.input)
		if got != tt.want {
			t.Errorf("priorityLabelUpper(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStatusEmoji(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"completed", "✓"},
		{"in_progress", "⋯"},
		{"pending", "○"},
		{"", "○"},
	}
	for _, tt := range tests {
		got := statusEmoji(tt.input)
		if got != tt.want {
			t.Errorf("statusEmoji(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestStatusEmojiExtended(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"completed", "✓"},
		{"validated", "✓"},
		{"in_progress", "⋯"},
		{"blocked", "✗"},
		{"pending", "○"},
	}
	for _, tt := range tests {
		got := statusEmojiExtended(tt.input)
		if got != tt.want {
			t.Errorf("statusEmojiExtended(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEmptyIfNil(t *testing.T) {
	if got := emptyIfNil(nil); got == nil {
		t.Error("emptyIfNil(nil) should return empty slice, not nil")
	}
	if got := emptyIfNil([]string{"a"}); len(got) != 1 {
		t.Errorf("emptyIfNil([a]) = %v, want [a]", got)
	}
}

func TestNilIfEmpty(t *testing.T) {
	if got := nilIfEmpty(""); got != nil {
		t.Errorf("nilIfEmpty(\"\") = %v, want nil", got)
	}
	if got := nilIfEmpty("hello"); got != "hello" {
		t.Errorf("nilIfEmpty(\"hello\") = %v, want \"hello\"", got)
	}
}

// =============================================================================
// Integration with brain tools (no overlap)
// =============================================================================

func TestTaskToolsDoNotOverlapBrainTools(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterBrainTools(s, client)

	brainToolCount := len(s.tools)
	if brainToolCount != 32 {
		t.Errorf("expected 32 brain tools, got %d", brainToolCount)
	}

	RegisterTaskTools(s, client)

	totalCount := len(s.tools)
	taskToolCount := totalCount - brainToolCount

	if taskToolCount != 14 {
		t.Errorf("expected 14 new task tools (no overlap), got %d new tools", taskToolCount)
	}
}

// TestBrainTasks_DecodesServerPayload feeds the handler a payload shaped by
// types.TaskListResponse and asserts the rendered output reflects it.
//
// Nothing exercised this handler against a realistic payload, so four field
// mismatches sat undetected: DependsOn was tagged `dependsOn` and typed as a
// struct array (wire: `depends_on`, []string), Completed did not exist on
// TaskStats at all, Cycles was a struct array (wire: [][]string), and
// status_blocked/not_pending were dropped. Every one failed silently, and
// the first printed "Dependencies: none" on every task ever listed.
func TestBrainTasks_DecodesServerPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.TaskListResponse{
			Tasks: []types.ResolvedTask{
				{ID: "aaa11111", Title: "Ready with deps", Status: "pending", Classification: "ready", DependsOn: []string{"ddd44444"}},
				{ID: "bbb22222", Title: "Waiting one", Status: "pending", Classification: "waiting", DependsOn: []string{"aaa11111", "ddd44444"}},
				{ID: "ccc33333", Title: "Finished one", Status: "completed", Classification: "not_pending"},
			},
			Count: 3,
			Stats: &types.TaskStats{Total: 3, Ready: 1, Waiting: 1, NotPending: 1},
		})
	}))
	defer server.Close()

	s := NewServer()
	RegisterTaskTools(s, NewAPIClient(server.URL))
	out, err := s.tools["tasks"].handler(context.Background(), map[string]any{"project": "p"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if strings.Contains(out, "Dependencies: none") {
		t.Errorf("a task with depends_on rendered as having none:\n%s", out)
	}
	if !strings.Contains(out, "ddd44444") {
		t.Errorf("dependency id missing from output:\n%s", out)
	}
	if !strings.Contains(out, "not_pending") {
		t.Errorf("not_pending counter missing from stats:\n%s", out)
	}
	if strings.Contains(out, "completed") && strings.Contains(out, "| 0 not_pending") {
		t.Errorf("not_pending counted zero despite a not_pending task:\n%s", out)
	}
	if !strings.Contains(out, "Finished one") {
		t.Errorf("not_pending task was never rendered:\n%s", out)
	}
}

// TestBrainTasks_CyclesDoNotBreakTheTool pins that a dependency cycle
// renders. Cycles was typed as a struct array against a [][]string wire
// field, and the API client turns an unmarshal error into a hard failure —
// so the tool broke completely exactly when a cycle existed.
func TestBrainTasks_CyclesDoNotBreakTheTool(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.TaskListResponse{
			Tasks:  []types.ResolvedTask{{ID: "aaa11111", Title: "In a cycle", Status: "pending", Classification: "blocked"}},
			Count:  1,
			Cycles: [][]string{{"aaa11111", "bbb22222", "aaa11111"}},
		})
	}))
	defer server.Close()

	s := NewServer()
	RegisterTaskTools(s, NewAPIClient(server.URL))
	out, err := s.tools["tasks"].handler(context.Background(), map[string]any{"project": "p"})
	if err != nil {
		t.Fatalf("a dependency cycle broke the tool entirely: %v", err)
	}
	if !strings.Contains(out, "Circular Dependencies Detected") {
		t.Errorf("cycle not reported:\n%s", out)
	}
	if !strings.Contains(out, "aaa11111 -> bbb22222 -> aaa11111") {
		t.Errorf("cycle path not rendered:\n%s", out)
	}
}

// TestBrainTasks_LimitKeepsActionableTasks pins that the limit is spent on
// actionable buckets first.
//
// The limit used to be applied to the raw list, which in a mature project is
// ~99% finished work — so lowering it discarded exactly the rows the caller
// wanted. On a real project, limit:10 rendered a stats line and zero tasks.
func TestBrainTasks_LimitKeepsActionableTasks(t *testing.T) {
	var tasks []types.ResolvedTask
	for i := 0; i < 30; i++ {
		tasks = append(tasks, types.ResolvedTask{
			ID: fmt.Sprintf("done%04d", i), Title: "Finished", Status: "completed", Classification: "not_pending",
		})
	}
	tasks = append(tasks, types.ResolvedTask{ID: "act00001", Title: "Actionable one", Status: "pending", Classification: "ready"})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.TaskListResponse{Tasks: tasks, Count: len(tasks)})
	}))
	defer server.Close()

	s := NewServer()
	RegisterTaskTools(s, NewAPIClient(server.URL))
	out, err := s.tools["tasks"].handler(context.Background(), map[string]any{"project": "p", "limit": float64(3)})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(out, "Actionable one") {
		t.Errorf("the single ready task was truncated away by a low limit:\n%s", out)
	}
	if !strings.Contains(out, "Showing 3 of 31") {
		t.Errorf("truncation was not disclosed:\n%s", out)
	}
}

// TestBrainTasks_StatsMatchTheRenderedBody pins that the header describes the
// same set as the body. The server's Stats block is project-wide, so
// printing it above a filtered body produced "4 ready | 2 waiting"
// immediately followed by "No tasks found matching criteria."
func TestBrainTasks_StatsMatchTheRenderedBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(types.TaskListResponse{
			Tasks: []types.ResolvedTask{
				{ID: "aaa11111", Title: "Other feature", Status: "pending", Classification: "ready", FeatureID: "other"},
			},
			Count: 1,
			Stats: &types.TaskStats{Total: 400, Ready: 4, Waiting: 2},
		})
	}))
	defer server.Close()

	s := NewServer()
	RegisterTaskTools(s, NewAPIClient(server.URL))
	out, err := s.tools["tasks"].handler(context.Background(), map[string]any{
		"project": "p", "feature_id": "nonexistent",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(out, "No tasks found matching criteria") {
		t.Fatalf("expected the empty state:\n%s", out)
	}
	if strings.Contains(out, "4 ready") {
		t.Errorf("project-wide stats printed above an empty filtered body:\n%s", out)
	}
	if !strings.Contains(out, "0 ready") {
		t.Errorf("stats should describe the filtered set:\n%s", out)
	}
}

// TestBrainTasksStatus_UsesTheRealResponseType pins tasks_status against
// types.MultiTaskStatusResponse.
//
// The hand-rolled struct declared notFound, changed and timedOut — none of
// which the server sends — and omitted allCompleted, which it does. Being
// structurally always false, the phantoms meant the status line could only
// ever take its third branch: a wait_for call that genuinely waited and saw
// its condition met still reported "Immediate check (no wait)".
func TestBrainTasksStatus_UsesTheRealResponseType(t *testing.T) {
	respond := func(allCompleted bool, ids ...string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			var tasks []types.ResolvedTask
			for _, id := range ids {
				tasks = append(tasks, types.ResolvedTask{ID: id, Title: "T " + id, Status: "completed"})
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(types.MultiTaskStatusResponse{Tasks: tasks, AllCompleted: allCompleted})
		}))
	}

	t.Run("allCompleted is reported", func(t *testing.T) {
		srv := respond(true, "aaa11111")
		defer srv.Close()
		s := NewServer()
		RegisterTaskTools(s, NewAPIClient(srv.URL))
		out, err := s.tools["tasks_status"].handler(context.Background(), map[string]any{
			"project": "p", "task_ids": []any{"aaa11111"},
		})
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if !strings.Contains(out, "All requested tasks are completed") {
			t.Errorf("allCompleted was dropped:\n%s", out)
		}
		if strings.Contains(out, "Immediate check (no wait)") {
			t.Errorf("status line still reports the dead phantom branch:\n%s", out)
		}
	})

	t.Run("an unresolved id is reported as not found", func(t *testing.T) {
		srv := respond(false, "aaa11111") // bbb22222 requested but not returned
		defer srv.Close()
		s := NewServer()
		RegisterTaskTools(s, NewAPIClient(srv.URL))
		out, err := s.tools["tasks_status"].handler(context.Background(), map[string]any{
			"project": "p", "task_ids": []any{"aaa11111", "bbb22222"},
		})
		if err != nil {
			t.Fatalf("handler error: %v", err)
		}
		if !strings.Contains(out, "Not Found") || !strings.Contains(out, "bbb22222") {
			t.Errorf("an id the server could not resolve must be reported:\n%s", out)
		}
		if strings.Contains(out, "aaa11111 - task not found") {
			t.Errorf("a returned task must not be listed as missing:\n%s", out)
		}
	})
}

// TestMonitorEnableTools_SendTheRealCreateContract pins all four *_enable
// tools against types.CreateMonitorRequest by DECODING what they send.
//
// POST /monitors takes a flat snake_case body (template_id, project,
// feature_id, scope_type). All four tools sent camelCase "templateId" plus
// a nested "scope" object — the shape of DeleteMonitorByScopeRequest, which
// really is {templateId, scope:{...}} and is what the *_disable tools use.
// The enable tools were written from the disable contract.
//
// Nothing matched: the underscore in template_id defeats Go's
// case-insensitive key fallback, and "scope" is not a field, so TemplateID
// and ScopeType arrived empty, both required checks in HandleCreateMonitor
// fired, and every call returned HTTP 400.
//
// Decoding into the real request type is the point — a test asserting raw
// JSON keys would have happily passed on the wrong contract.
func TestMonitorEnableTools_SendTheRealCreateContract(t *testing.T) {
	cases := []struct {
		tool          string
		args          map[string]any
		wantTemplate  string
		wantScopeType string
		wantFeature   string
	}{
		{"monitor_enable", map[string]any{"template_id": "code-review", "project": "p", "feature_id": "f"}, "code-review", "feature", "f"},
		{"feature_review_enable", map[string]any{"project": "p", "feature_id": "f"}, "feature-review", "feature", "f"},
		{"blocked_inspector_enable", map[string]any{"project": "p", "feature_id": "f"}, "blocked-inspector", "feature", "f"},
		{"dream_enable", map[string]any{"project": "p"}, "dream", "project", ""},
	}

	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			var got types.CreateMonitorRequest
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
					t.Fatalf("decode request into CreateMonitorRequest: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "mon12345", "title": "Monitor"})
			}))
			defer srv.Close()

			s := NewServer()
			RegisterTaskTools(s, NewAPIClient(srv.URL))
			if _, err := s.tools[tc.tool].handler(context.Background(), tc.args); err != nil {
				t.Fatalf("handler error: %v", err)
			}

			// These two are what HandleCreateMonitor requires; empty means
			// a guaranteed 400.
			if got.TemplateID != tc.wantTemplate {
				t.Errorf("template_id = %q, want %q (empty means HTTP 400 on every call)", got.TemplateID, tc.wantTemplate)
			}
			if got.ScopeType != tc.wantScopeType {
				t.Errorf("scope_type = %q, want %q (empty means HTTP 400 on every call)", got.ScopeType, tc.wantScopeType)
			}
			if got.Project != "p" {
				t.Errorf("project = %q, want %q", got.Project, "p")
			}
			if got.FeatureID != tc.wantFeature {
				t.Errorf("feature_id = %q, want %q", got.FeatureID, tc.wantFeature)
			}
		})
	}
}

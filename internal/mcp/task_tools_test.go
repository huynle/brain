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

func TestRegisterTaskTools_Count(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	count := len(s.tools)
	if count != 10 {
		t.Errorf("expected 10 task tools registered, got %d", count)
	}
}

func TestRegisterTaskTools_Names(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	expectedTools := []string{
		"brain_tasks",
		"brain_task_next",
		"brain_task_get",
		"brain_task_metadata",
		"brain_tasks_status",
		"brain_task_trigger",
		"brain_feature_review_enable",
		"brain_feature_review_disable",
		"brain_blocked_inspector_enable",
		"brain_blocked_inspector_disable",
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

	tool := s.tools["brain_tasks"].tool

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

	// Check classification enum
	classProp := tool.InputSchema.Properties["classification"]
	if len(classProp.Enum) != 3 {
		t.Errorf("classification enum has %d values, want 3", len(classProp.Enum))
	}
}

func TestBrainTaskGet_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	tool := s.tools["brain_task_get"].tool

	// Required: taskId
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "taskId" {
		t.Errorf("brain_task_get required = %v, want [taskId]", tool.InputSchema.Required)
	}
}

func TestBrainTaskMetadata_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	tool := s.tools["brain_task_metadata"].tool

	// Required: taskId
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "taskId" {
		t.Errorf("brain_task_metadata required = %v, want [taskId]", tool.InputSchema.Required)
	}
}

func TestBrainTasksStatus_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	tool := s.tools["brain_tasks_status"].tool

	// Required: taskIds
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "taskIds" {
		t.Errorf("brain_tasks_status required = %v, want [taskIds]", tool.InputSchema.Required)
	}

	// Check taskIds is array type with string items
	taskIdsProp := tool.InputSchema.Properties["taskIds"]
	if taskIdsProp.Type != "array" {
		t.Errorf("taskIds type = %q, want %q", taskIdsProp.Type, "array")
	}
	if taskIdsProp.Items == nil || taskIdsProp.Items.Type != "string" {
		t.Error("taskIds items should be string type")
	}

	// Check waitFor enum
	waitForProp := tool.InputSchema.Properties["waitFor"]
	if len(waitForProp.Enum) != 2 {
		t.Errorf("waitFor enum has %d values, want 2", len(waitForProp.Enum))
	}
}

func TestBrainTaskTrigger_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	tool := s.tools["brain_task_trigger"].tool

	// Required: taskId
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "taskId" {
		t.Errorf("brain_task_trigger required = %v, want [taskId]", tool.InputSchema.Required)
	}
}

func TestBrainFeatureReviewEnable_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	tool := s.tools["brain_feature_review_enable"].tool

	// Required: project, feature_id
	if len(tool.InputSchema.Required) != 2 {
		t.Errorf("brain_feature_review_enable required fields = %d, want 2", len(tool.InputSchema.Required))
	}
}

func TestBrainBlockedInspectorEnable_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	tool := s.tools["brain_blocked_inspector_enable"].tool

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

	handler := s.tools["brain_tasks"].handler
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

	handler := s.tools["brain_tasks"].handler
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

	handler := s.tools["brain_tasks"].handler
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
			json.NewEncoder(w).Encode(map[string]any{
				"task": map[string]any{
					"id": "abc12345", "path": "projects/test/task/abc12345.md",
					"title": "Next Task", "status": "pending", "priority": "high",
					"classification": "ready", "resolved_deps": []string{"dep1"},
				},
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

	handler := s.tools["brain_task_next"].handler
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
				"tasks": []map[string]any{},
				"count": 0,
				"stats": map[string]int{
					"ready": 0, "waiting": 3, "blocked": 1, "completed": 5,
				},
			})
			return
		}
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["brain_task_next"].handler
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
	if !strings.Contains(result, "1 tasks blocked") {
		t.Errorf("result should contain blocked count, got: %s", result)
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
						"dependsOn": []map[string]any{
							{"id": "dep1", "title": "Dep Task", "status": "completed"},
						},
					},
					{
						"id": "xyz99999", "title": "Dependent Task", "path": "projects/test/task/xyz99999.md",
						"status": "pending", "priority": "medium", "classification": "waiting",
						"resolved_deps": []string{"abc12345"},
					},
				},
				"count": 2,
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

	handler := s.tools["brain_task_get"].handler
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

	handler := s.tools["brain_task_get"].handler
	result, err := handler(context.Background(), map[string]any{
		"taskId": "nonexistent",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Task not found") {
		t.Errorf("result should indicate not found, got: %s", result)
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

	handler := s.tools["brain_task_get"].handler
	result, err := handler(context.Background(), map[string]any{
		"taskId": "auth",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Did you mean") {
		t.Errorf("result should suggest partial matches, got: %s", result)
	}
	if !strings.Contains(result, "Build Auth Module") {
		t.Errorf("result should contain suggestion, got: %s", result)
	}
}

func TestBrainTaskGet_MissingTaskId(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:1")
	RegisterTaskTools(s, client)

	handler := s.tools["brain_task_get"].handler
	result, err := handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "provide a task ID") {
		t.Errorf("result should ask for task ID, got: %s", result)
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
					"feature_id": "auth-system", "feature_priority": "high",
				},
			},
			"count": 1,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["brain_task_metadata"].handler
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

	handler := s.tools["brain_task_metadata"].handler
	result, err := handler(context.Background(), map[string]any{
		"taskId": "nonexistent",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "Task not found") {
		t.Errorf("result should indicate not found, got: %s", result)
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

	handler := s.tools["brain_tasks_status"].handler
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

	handler := s.tools["brain_tasks_status"].handler
	result, err := handler(context.Background(), map[string]any{
		"taskIds": []any{},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if !strings.Contains(result, "provide at least one task ID") {
		t.Errorf("result should ask for task IDs, got: %s", result)
	}
}

func TestBrainTasksStatus_WithNotFound(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"tasks":    []map[string]any{},
			"notFound": []string{"missing123"},
			"changed":  false,
			"timedOut": false,
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)

	handler := s.tools["brain_tasks_status"].handler
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

	handler := s.tools["brain_task_trigger"].handler
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

	handler := s.tools["brain_task_trigger"].handler
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

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["templateId"] != "feature-review" {
			t.Errorf("templateId = %v, want feature-review", body["templateId"])
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

	handler := s.tools["brain_feature_review_enable"].handler
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

	handler := s.tools["brain_feature_review_disable"].handler
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

		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if body["templateId"] != "blocked-inspector" {
			t.Errorf("templateId = %v, want blocked-inspector", body["templateId"])
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

	handler := s.tools["brain_blocked_inspector_enable"].handler
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

	handler := s.tools["brain_blocked_inspector_enable"].handler
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

	handler := s.tools["brain_blocked_inspector_disable"].handler
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

	RegisterTaskTools(s, client)

	totalCount := len(s.tools)
	taskToolCount := totalCount - brainToolCount

	if taskToolCount != 10 {
		t.Errorf("expected 10 new task tools (no overlap), got %d new tools", taskToolCount)
	}
}

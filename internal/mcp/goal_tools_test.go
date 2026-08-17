package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegisterGoalTools_CountNamesHandlersDescriptions(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterGoalTools(s, client)

	expected := []string{
		"goal_create",
		"goal_list",
		"goal_update",
		"goal_pause",
		"goal_resume",
		"goal_archive",
		"goal_run",
		"goal_progress",
		"goal_audit",
		"goal_delete",
	}
	if len(s.tools) != len(expected) {
		t.Fatalf("expected %d goal tools registered, got %d", len(expected), len(s.tools))
	}
	for _, name := range expected {
		rt, ok := s.tools[name]
		if !ok {
			t.Fatalf("tool %q not registered", name)
		}
		if rt.handler == nil {
			t.Errorf("tool %q has nil handler", name)
		}
		if strings.TrimSpace(rt.tool.Description) == "" {
			t.Errorf("tool %q has empty description", name)
		}
		if rt.tool.InputSchema.Type != "object" {
			t.Errorf("tool %q inputSchema.type = %q, want object", name, rt.tool.InputSchema.Type)
		}
	}
}

func TestGoalToolSchemas(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterGoalTools(s, client)

	tests := []struct {
		tool     string
		required []string
		props    []string
	}{
		{"goal_create", []string{"project", "title"}, []string{"project", "feature_id", "title", "content", "goal_id", "criteria", "validation", "workdir", "trigger_source", "task_id", "complete_statuses", "blocked_statuses", "steering_enabled", "steering_cooldown_minutes", "action_type", "direct_prompt", "command", "agent", "model", "executor", "target_workdir", "execution_mode", "session_mode", "complete_on_idle", "timeout", "requires_capability", "config", "action"}},
		{"goal_list", nil, []string{"project", "feature_id", "status"}},
		{"goal_update", []string{"goal_id"}, []string{"goal_id", "title", "content", "status", "criteria", "validation", "workdir", "trigger_source", "task_id", "complete_statuses", "blocked_statuses", "steering_enabled", "steering_cooldown_minutes", "action_type", "direct_prompt", "command", "agent", "model", "executor", "target_workdir", "execution_mode", "session_mode", "complete_on_idle", "timeout", "requires_capability", "action"}},
		{"goal_pause", []string{"goal_id"}, []string{"goal_id"}},
		{"goal_resume", []string{"goal_id"}, []string{"goal_id"}},
		{"goal_archive", []string{"goal_id"}, []string{"goal_id"}},
		{"goal_run", []string{"goal_id"}, []string{"goal_id"}},
		{"goal_progress", []string{"goal_id"}, []string{"goal_id"}},
		{"goal_audit", []string{"goal_id"}, []string{"goal_id", "limit"}},
		{"goal_delete", []string{"goal_id"}, []string{"goal_id"}},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			tool := s.tools[tt.tool].tool
			if len(tool.InputSchema.Required) != len(tt.required) {
				t.Fatalf("required = %v, want %v", tool.InputSchema.Required, tt.required)
			}
			for _, req := range tt.required {
				found := false
				for _, got := range tool.InputSchema.Required {
					if got == req {
						found = true
					}
				}
				if !found {
					t.Errorf("required missing %q in %v", req, tool.InputSchema.Required)
				}
			}
			for _, prop := range tt.props {
				if _, ok := tool.InputSchema.Properties[prop]; !ok {
					t.Errorf("schema missing property %q", prop)
				}
			}
		})
	}
}

func TestBrainGoalCreate_RequestAndFormatting(t *testing.T) {
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/v1/goals" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"entry_id": "entry-123", "goal_id": "goal-123", "title": "Ship goals", "project": "brain", "feature_id": "goals", "status": "active",
			"config": map[string]any{"id": "goal-123", "trigger_source": "both", "criteria": "all tests pass"},
			"action": map[string]any{"type": "prompt", "agent": "dev"},
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterGoalTools(s, client)

	result, err := s.tools["goal_create"].handler(context.Background(), map[string]any{
		"project": "brain", "feature_id": "goals", "title": "Ship goals", "content": "Keep going",
		"goal_id": "goal-123", "criteria": "all tests pass", "validation": "go test ./...", "workdir": "/repo", "trigger_source": "both",
		"complete_statuses": []any{"completed", "validated"}, "blocked_statuses": []any{"blocked"},
		"action_type": "prompt", "direct_prompt": "Implement next step", "agent": "dev", "model": "gpt", "executor": "opencode", "target_workdir": "/repo", "execution_mode": "worktree", "session_mode": "fresh", "complete_on_idle": true, "timeout": "30m", "requires_capability": "go",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if capturedBody["project"] != "brain" || capturedBody["title"] != "Ship goals" || capturedBody["feature_id"] != "goals" {
		t.Fatalf("unexpected top-level body: %#v", capturedBody)
	}
	config := capturedBody["config"].(map[string]any)
	if config["id"] != "goal-123" || config["criteria"] != "all tests pass" || config["validation"] != "go test ./..." || config["workdir"] != "/repo" || config["trigger_source"] != "both" {
		t.Fatalf("unexpected config body: %#v", config)
	}
	action := capturedBody["action"].(map[string]any)
	if action["type"] != "prompt" || action["direct_prompt"] != "Implement next step" || action["complete_on_idle"] != true || action["requires_capability"] != "go" {
		t.Fatalf("unexpected action body: %#v", action)
	}
	for _, want := range []string{"Goal created", "goal-123", "entry-123", "brain", "goals", "active", "both"} {
		if !strings.Contains(result, want) {
			t.Errorf("result missing %q:\n%s", want, result)
		}
	}
}

func TestBrainGoalList_RequestAndEmptyFormatting(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/goals" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.URL.Query().Get("project") != "brain" || r.URL.Query().Get("feature_id") != "goals" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"goals": []map[string]any{}, "count": 0})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterGoalTools(s, client)
	result, err := s.tools["goal_list"].handler(context.Background(), map[string]any{"project": "brain", "feature_id": "goals"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if !strings.Contains(result, "No goals found") || !strings.Contains(result, "goal_create") {
		t.Fatalf("unexpected result: %s", result)
	}
}

func TestBrainGoalList_FormatsGoals(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"goals": []map[string]any{{"entry_id": "entry-1", "goal_id": "goal-1", "title": "Finish feature", "project": "brain", "feature_id": "goals", "status": "active", "config": map[string]any{"trigger_source": "feature"}}}, "count": 1})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterGoalTools(s, client)
	result, err := s.tools["goal_list"].handler(context.Background(), map[string]any{})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	for _, want := range []string{"Goals", "goal-1", "Finish feature", "brain", "goals", "active", "feature"} {
		if !strings.Contains(result, want) {
			t.Errorf("result missing %q:\n%s", want, result)
		}
	}
}

func TestBrainGoalUpdate_RequestAndFormatting(t *testing.T) {
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/goals/goal-123" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"entry_id": "entry-123", "goal_id": "goal-123", "title": "Updated", "project": "brain", "feature_id": "goals", "status": "blocked", "config": map[string]any{"trigger_source": "task"}})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterGoalTools(s, client)
	result, err := s.tools["goal_update"].handler(context.Background(), map[string]any{"goal_id": "goal-123", "title": "Updated", "status": "blocked", "criteria": "done", "trigger_source": "task", "complete_statuses": []any{"completed"}, "action_type": "script", "command": "just test"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if capturedBody["title"] != "Updated" || capturedBody["status"] != "blocked" || capturedBody["criteria"] != "done" || capturedBody["trigger_source"] != "task" {
		t.Fatalf("unexpected update body: %#v", capturedBody)
	}
	action := capturedBody["action"].(map[string]any)
	if action["type"] != "script" || action["command"] != "just test" {
		t.Fatalf("unexpected action: %#v", action)
	}
	for _, want := range []string{"Goal updated", "goal-123", "entry-123", "blocked", "task"} {
		if !strings.Contains(result, want) {
			t.Errorf("result missing %q:\n%s", want, result)
		}
	}
}

func TestBrainGoalLifecycleAliases_RequestAndFormatting(t *testing.T) {
	tests := []struct {
		tool   string
		status string
		prefix string
	}{
		{"goal_pause", "blocked", "Goal paused"},
		{"goal_resume", "active", "Goal resumed"},
		{"goal_archive", "archived", "Goal archived"},
	}

	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			var capturedBody map[string]any
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPatch || r.URL.Path != "/api/v1/goals/goal-123" {
					t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
					t.Fatalf("decode body: %v", err)
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{"entry_id": "entry-123", "goal_id": "goal-123", "title": "Lifecycle", "project": "brain", "feature_id": "goals", "status": tt.status, "config": map[string]any{"trigger_source": "both"}})
			}))
			defer server.Close()

			s := NewServer()
			client := NewAPIClient(server.URL)
			RegisterGoalTools(s, client)
			result, err := s.tools[tt.tool].handler(context.Background(), map[string]any{"goal_id": "goal-123"})
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if len(capturedBody) != 1 || capturedBody["status"] != tt.status {
				t.Fatalf("unexpected body: %#v", capturedBody)
			}
			for _, want := range []string{tt.prefix, "goal-123", "entry-123", tt.status, "both"} {
				if !strings.Contains(result, want) {
					t.Errorf("result missing %q:\n%s", want, result)
				}
			}
		})
	}
}

func TestBrainGoalRunProgressAudit_RequestAndFormatting(t *testing.T) {
	requests := []string{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/goals/goal-123/run":
			json.NewEncoder(w).Encode(map[string]any{"timestamp": "2026-06-17T00:00:00Z", "goal_id": "goal-123", "project": "brain", "feature_id": "goals", "triggering_event": "manual", "decision": "need_work", "reason": "No active work", "generated_task_id": "task-1", "linked_tasks": []map[string]any{{"id": "task-0", "title": "Seed", "status": "completed"}}})
		case "/api/v1/goals/goal-123/progress":
			json.NewEncoder(w).Encode(map[string]any{"goal_id": "goal-123", "entry_id": "entry-123", "project": "brain", "feature_id": "goals", "feature_status": "active", "total": 3, "pending": 1, "in_progress": 1, "completed": 1, "blocked": 0, "tasks": []map[string]any{{"id": "task-1", "title": "Do work", "status": "in_progress"}}})
		case "/api/v1/goals/goal-123/audit":
			if r.URL.Query().Get("limit") != "7" {
				t.Fatalf("limit query = %q", r.URL.Query().Get("limit"))
			}
			json.NewEncoder(w).Encode(map[string]any{"audit": []map[string]any{{"timestamp": "2026-06-17T00:00:00Z", "goal_id": "goal-123", "project": "brain", "feature_id": "goals", "triggering_event": "manual", "decision": "complete", "reason": "All linked tasks done", "linked_tasks": []map[string]any{{"id": "task-1", "title": "Do work", "status": "completed"}}}}, "count": 1})
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterGoalTools(s, client)
	run, err := s.tools["goal_run"].handler(context.Background(), map[string]any{"goal_id": "goal-123"})
	if err != nil {
		t.Fatalf("run handler error: %v", err)
	}
	for _, want := range []string{"Reconcile", "goal-123", "need_work", "No active work", "task-1", "Linked tasks: 1"} {
		if !strings.Contains(run, want) {
			t.Errorf("run missing %q:\n%s", want, run)
		}
	}
	progress, err := s.tools["goal_progress"].handler(context.Background(), map[string]any{"goal_id": "goal-123"})
	if err != nil {
		t.Fatalf("progress handler error: %v", err)
	}
	for _, want := range []string{"Progress", "goal-123", "Total: 3", "Pending: 1", "In Progress: 1", "Completed: 1", "Blocked: 0", "Feature: active", "Do work"} {
		if !strings.Contains(progress, want) {
			t.Errorf("progress missing %q:\n%s", want, progress)
		}
	}
	audit, err := s.tools["goal_audit"].handler(context.Background(), map[string]any{"goal_id": "goal-123", "limit": float64(7)})
	if err != nil {
		t.Fatalf("audit handler error: %v", err)
	}
	for _, want := range []string{"Reconcile Audit", "goal-123", "complete", "All linked tasks done", "Linked tasks: 1"} {
		if !strings.Contains(audit, want) {
			t.Errorf("audit missing %q:\n%s", want, audit)
		}
	}
	wantRequests := []string{"POST /api/v1/goals/goal-123/run", "GET /api/v1/goals/goal-123/progress", "GET /api/v1/goals/goal-123/audit?limit=7"}
	if len(requests) != len(wantRequests) {
		t.Fatalf("requests = %#v, want %#v", requests, wantRequests)
	}
	for i := range wantRequests {
		if requests[i] != wantRequests[i] {
			t.Errorf("request[%d] = %q, want %q", i, requests[i], wantRequests[i])
		}
	}
}

func TestGoalTools_ValidationErrors(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:1")
	RegisterGoalTools(s, client)
	tests := []struct {
		tool string
		args map[string]any
		want string
	}{
		{"goal_create", map[string]any{"title": "Missing project"}, "project"},
		{"goal_create", map[string]any{"project": "brain"}, "title"},
		{"goal_update", map[string]any{}, "goal_id"},
		{"goal_pause", map[string]any{}, "goal_id"},
		{"goal_resume", map[string]any{}, "goal_id"},
		{"goal_archive", map[string]any{}, "goal_id"},
		{"goal_run", map[string]any{}, "goal_id"},
		{"goal_progress", map[string]any{}, "goal_id"},
		{"goal_audit", map[string]any{}, "goal_id"},
		{"goal_delete", map[string]any{}, "goal_id"},
	}
	for _, tt := range tests {
		t.Run(tt.tool, func(t *testing.T) {
			result, err := s.tools[tt.tool].handler(context.Background(), tt.args)
			if err == nil {
				t.Fatalf("expected validation error, got result %q", result)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
}

func TestBrainGoalCreate_TaskScopeAndSteering(t *testing.T) {
	var capturedBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"entry_id": "entry-1", "goal_id": "g-task", "title": "Task goal", "project": "brain", "status": "active"})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterGoalTools(s, client)

	_, err := s.tools["goal_create"].handler(context.Background(), map[string]any{
		"project": "brain", "title": "Task goal", "goal_id": "g-task",
		"task_id":                   "task-42",
		"steering_enabled":          false,
		"steering_cooldown_minutes": float64(45),
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	config := capturedBody["config"].(map[string]any)
	if config["task_id"] != "task-42" {
		t.Errorf("config.task_id = %v, want task-42", config["task_id"])
	}
	steering, ok := config["steering"].(map[string]any)
	if !ok {
		t.Fatalf("config.steering missing: %#v", config)
	}
	if steering["enabled"] != false {
		t.Errorf("steering.enabled = %v, want false", steering["enabled"])
	}
	if steering["cooldown_minutes"] != float64(45) {
		t.Errorf("steering.cooldown_minutes = %v, want 45", steering["cooldown_minutes"])
	}
}

func TestBrainGoalList_StatusQuery(t *testing.T) {
	var capturedStatus string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedStatus = r.URL.Query().Get("status")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"goals": []map[string]any{}, "count": 0})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterGoalTools(s, client)
	if _, err := s.tools["goal_list"].handler(context.Background(), map[string]any{"status": "archived"}); err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if capturedStatus != "archived" {
		t.Errorf("status query = %q, want archived", capturedStatus)
	}
}

func TestBrainGoalDelete_Request(t *testing.T) {
	var captured string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured = r.Method + " " + r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"success": true, "goal_id": "goal-123"})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterGoalTools(s, client)
	result, err := s.tools["goal_delete"].handler(context.Background(), map[string]any{"goal_id": "goal-123"})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	if captured != "DELETE /api/v1/goals/goal-123" {
		t.Errorf("request = %q, want DELETE /api/v1/goals/goal-123", captured)
	}
	if !strings.Contains(result, "Goal deleted") || !strings.Contains(result, "goal-123") {
		t.Errorf("unexpected result: %s", result)
	}
}

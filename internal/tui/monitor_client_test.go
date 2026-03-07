package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// =============================================================================
// Constructor
// =============================================================================

func TestNewMonitorClient(t *testing.T) {
	client := NewMonitorClient("http://localhost:3333")
	if client == nil {
		t.Fatal("Expected non-nil MonitorClient")
	}
}

// =============================================================================
// CreateScheduledTask
// =============================================================================

func TestMonitorClient_CreateScheduledTask(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/entries" {
			t.Errorf("Expected path /api/v1/entries, got %s", r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"entry":{"path":"projects/myproj/task/abc12345.md"}}`))
	}))
	defer server.Close()

	client := NewMonitorClient(server.URL)
	err := client.CreateScheduledTask(context.Background(), "blocked-inspector", "my-feature", "myproj", "*/30 * * * *", "Check for blocked tasks")
	if err != nil {
		t.Fatalf("CreateScheduledTask failed: %v", err)
	}

	// Verify request body fields
	if receivedBody["type"] != "task" {
		t.Errorf("Expected type 'task', got %v", receivedBody["type"])
	}
	if receivedBody["title"] != "Blocked Inspector: my-feature" {
		t.Errorf("Expected title 'Blocked Inspector: my-feature', got %v", receivedBody["title"])
	}
	if receivedBody["content"] != "Check for blocked tasks" {
		t.Errorf("Expected content 'Check for blocked tasks', got %v", receivedBody["content"])
	}
	if receivedBody["project"] != "myproj" {
		t.Errorf("Expected project 'myproj', got %v", receivedBody["project"])
	}
	if receivedBody["schedule"] != "*/30 * * * *" {
		t.Errorf("Expected schedule '*/30 * * * *', got %v", receivedBody["schedule"])
	}
	if receivedBody["schedule_enabled"] != true {
		t.Errorf("Expected schedule_enabled true, got %v", receivedBody["schedule_enabled"])
	}
	if receivedBody["complete_on_idle"] != true {
		t.Errorf("Expected complete_on_idle true, got %v", receivedBody["complete_on_idle"])
	}
	if receivedBody["execution_mode"] != "current_branch" {
		t.Errorf("Expected execution_mode 'current_branch', got %v", receivedBody["execution_mode"])
	}
	if receivedBody["feature_id"] != "my-feature" {
		t.Errorf("Expected feature_id 'my-feature', got %v", receivedBody["feature_id"])
	}

	// Verify tags
	tags, ok := receivedBody["tags"].([]interface{})
	if !ok || len(tags) != 1 {
		t.Fatalf("Expected tags array with 1 element, got %v", receivedBody["tags"])
	}
	expectedTag := "monitor:blocked-inspector:feature:my-feature"
	if tags[0] != expectedTag {
		t.Errorf("Expected tag '%s', got %v", expectedTag, tags[0])
	}
}

func TestMonitorClient_CreateScheduledTask_409Conflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"already exists"}`))
	}))
	defer server.Close()

	client := NewMonitorClient(server.URL)
	err := client.CreateScheduledTask(context.Background(), "blocked-inspector", "my-feature", "myproj", "*/30 * * * *", "prompt")

	// 409 should NOT return an error (silently handled)
	if err != nil {
		t.Errorf("Expected nil error for 409 Conflict, got %v", err)
	}
}

func TestMonitorClient_CreateScheduledTask_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal error"}`))
	}))
	defer server.Close()

	client := NewMonitorClient(server.URL)
	err := client.CreateScheduledTask(context.Background(), "blocked-inspector", "my-feature", "myproj", "*/30 * * * *", "prompt")

	if err == nil {
		t.Error("Expected error for 500 response, got nil")
	}
}

// =============================================================================
// FindScheduledTask
// =============================================================================

func TestMonitorClient_FindScheduledTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/entries" {
			t.Errorf("Expected path /api/v1/entries, got %s", r.URL.Path)
		}

		// Verify query params
		q := r.URL.Query()
		if q.Get("type") != "task" {
			t.Errorf("Expected type=task, got %s", q.Get("type"))
		}
		expectedTag := "monitor:blocked-inspector:feature:my-feature"
		if q.Get("tags") != expectedTag {
			t.Errorf("Expected tags=%s, got %s", expectedTag, q.Get("tags"))
		}
		if q.Get("limit") != "1" {
			t.Errorf("Expected limit=1, got %s", q.Get("limit"))
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"entries":[{"path":"projects/myproj/task/abc12345.md"}]}`))
	}))
	defer server.Close()

	client := NewMonitorClient(server.URL)
	result, err := client.FindScheduledTask(context.Background(), "blocked-inspector", "my-feature")
	if err != nil {
		t.Fatalf("FindScheduledTask failed: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.Path != "projects/myproj/task/abc12345.md" {
		t.Errorf("Expected path 'projects/myproj/task/abc12345.md', got '%s'", result.Path)
	}
}

func TestMonitorClient_FindScheduledTask_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"entries":[]}`))
	}))
	defer server.Close()

	client := NewMonitorClient(server.URL)
	result, err := client.FindScheduledTask(context.Background(), "blocked-inspector", "my-feature")
	if err != nil {
		t.Fatalf("FindScheduledTask failed: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil result for empty entries, got %v", result)
	}
}

// =============================================================================
// DeleteScheduledTask
// =============================================================================

func TestMonitorClient_DeleteScheduledTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("Expected DELETE, got %s", r.Method)
		}
		// Path should be percent-encoded
		if r.URL.Query().Get("confirm") != "true" {
			t.Errorf("Expected confirm=true query param")
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"deleted":true}`))
	}))
	defer server.Close()

	client := NewMonitorClient(server.URL)
	err := client.DeleteScheduledTask(context.Background(), "projects/myproj/task/abc12345.md")
	if err != nil {
		t.Fatalf("DeleteScheduledTask failed: %v", err)
	}
}

func TestMonitorClient_DeleteScheduledTask_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	}))
	defer server.Close()

	client := NewMonitorClient(server.URL)
	err := client.DeleteScheduledTask(context.Background(), "projects/myproj/task/abc12345.md")
	if err == nil {
		t.Error("Expected error for 404 response, got nil")
	}
}

// =============================================================================
// CreateMonitorTask
// =============================================================================

func TestMonitorClient_CreateMonitorTask(t *testing.T) {
	var receivedBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("Expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/monitors" {
			t.Errorf("Expected path /api/v1/monitors, got %s", r.URL.Path)
		}

		if err := json.NewDecoder(r.Body).Decode(&receivedBody); err != nil {
			t.Fatalf("Failed to decode request body: %v", err)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"monitor":{"taskId":"abc12345"}}`))
	}))
	defer server.Close()

	client := NewMonitorClient(server.URL)
	err := client.CreateMonitorTask(context.Background(), "feature-review", "my-feature", "myproj")
	if err != nil {
		t.Fatalf("CreateMonitorTask failed: %v", err)
	}

	// Verify request body
	if receivedBody["template_id"] != "feature-review" {
		t.Errorf("Expected template_id 'feature-review', got %v", receivedBody["template_id"])
	}
	if receivedBody["scope_type"] != "feature" {
		t.Errorf("Expected scope_type 'feature', got %v", receivedBody["scope_type"])
	}
	if receivedBody["feature_id"] != "my-feature" {
		t.Errorf("Expected feature_id 'my-feature', got %v", receivedBody["feature_id"])
	}
	if receivedBody["project"] != "myproj" {
		t.Errorf("Expected project 'myproj', got %v", receivedBody["project"])
	}
}

func TestMonitorClient_CreateMonitorTask_409Conflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"error":"already exists"}`))
	}))
	defer server.Close()

	client := NewMonitorClient(server.URL)
	err := client.CreateMonitorTask(context.Background(), "feature-review", "my-feature", "myproj")

	// 409 should NOT return an error
	if err != nil {
		t.Errorf("Expected nil error for 409 Conflict, got %v", err)
	}
}

func TestMonitorClient_CreateMonitorTask_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal error"}`))
	}))
	defer server.Close()

	client := NewMonitorClient(server.URL)
	err := client.CreateMonitorTask(context.Background(), "feature-review", "my-feature", "myproj")

	if err == nil {
		t.Error("Expected error for 500 response, got nil")
	}
}

// =============================================================================
// FindMonitorTask
// =============================================================================

func TestMonitorClient_FindMonitorTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("Expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/monitors" {
			t.Errorf("Expected path /api/v1/monitors, got %s", r.URL.Path)
		}

		// Verify query params
		q := r.URL.Query()
		if q.Get("template_id") != "feature-review" {
			t.Errorf("Expected template_id=feature-review, got %s", q.Get("template_id"))
		}
		if q.Get("feature_id") != "my-feature" {
			t.Errorf("Expected feature_id=my-feature, got %s", q.Get("feature_id"))
		}
		if q.Get("project") != "myproj" {
			t.Errorf("Expected project=myproj, got %s", q.Get("project"))
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"monitors":[{"taskId":"abc12345","path":"projects/myproj/task/abc12345.md"}]}`))
	}))
	defer server.Close()

	client := NewMonitorClient(server.URL)
	result, err := client.FindMonitorTask(context.Background(), "feature-review", "my-feature", "myproj")
	if err != nil {
		t.Fatalf("FindMonitorTask failed: %v", err)
	}
	if result == nil {
		t.Fatal("Expected non-nil result")
	}
	if result.TaskID != "abc12345" {
		t.Errorf("Expected TaskID 'abc12345', got '%s'", result.TaskID)
	}
	if result.Path != "projects/myproj/task/abc12345.md" {
		t.Errorf("Expected Path 'projects/myproj/task/abc12345.md', got '%s'", result.Path)
	}
}

func TestMonitorClient_FindMonitorTask_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"monitors":[]}`))
	}))
	defer server.Close()

	client := NewMonitorClient(server.URL)
	result, err := client.FindMonitorTask(context.Background(), "feature-review", "my-feature", "myproj")
	if err != nil {
		t.Fatalf("FindMonitorTask failed: %v", err)
	}
	if result != nil {
		t.Errorf("Expected nil result for empty monitors, got %v", result)
	}
}

// =============================================================================
// DeleteMonitorTask
// =============================================================================

func TestMonitorClient_DeleteMonitorTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("Expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/monitors/abc12345" {
			t.Errorf("Expected path /api/v1/monitors/abc12345, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"deleted":true}`))
	}))
	defer server.Close()

	client := NewMonitorClient(server.URL)
	err := client.DeleteMonitorTask(context.Background(), "abc12345")
	if err != nil {
		t.Fatalf("DeleteMonitorTask failed: %v", err)
	}
}

func TestMonitorClient_DeleteMonitorTask_NotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"not found"}`))
	}))
	defer server.Close()

	client := NewMonitorClient(server.URL)
	err := client.DeleteMonitorTask(context.Background(), "abc12345")
	if err == nil {
		t.Error("Expected error for 404 response, got nil")
	}
}

// =============================================================================
// Tag Format
// =============================================================================

func TestMonitorTagFormat(t *testing.T) {
	tag := monitorTag("blocked-inspector", "my-feature")
	expected := "monitor:blocked-inspector:feature:my-feature"
	if tag != expected {
		t.Errorf("Expected tag '%s', got '%s'", expected, tag)
	}
}

func TestMonitorTagFormat_DifferentIDs(t *testing.T) {
	tag := monitorTag("feature-review", "auth-v2")
	expected := "monitor:feature-review:feature:auth-v2"
	if tag != expected {
		t.Errorf("Expected tag '%s', got '%s'", expected, tag)
	}
}

// =============================================================================
// Scheduled Task Title Format
// =============================================================================

func TestScheduledTaskTitle(t *testing.T) {
	title := scheduledTaskTitle("blocked-inspector", "my-feature")
	expected := "Blocked Inspector: my-feature"
	if title != expected {
		t.Errorf("Expected title '%s', got '%s'", expected, title)
	}
}

func TestScheduledTaskTitle_FeatureReview(t *testing.T) {
	title := scheduledTaskTitle("feature-review", "auth-v2")
	expected := "Feature Review: auth-v2"
	if title != expected {
		t.Errorf("Expected title '%s', got '%s'", expected, title)
	}
}

package mcp

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// resume_task_with_context mirrors POST
// /tasks/{project}/{taskId}/resume-with-context. These tests assert the tool
// is registered, its schema enforces the required params, and its handler
// posts the right path + body and formats the structured response.

func TestRegisterResumeTaskWithContext_Registered(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterTaskTools(s, client)

	rt, ok := s.tools["resume_task_with_context"]
	if !ok {
		t.Fatal("tool resume_task_with_context not registered")
	}
	if rt.handler == nil {
		t.Fatal("resume_task_with_context has nil handler")
	}
	if rt.tool.InputSchema.Type != "object" {
		t.Errorf("inputSchema.type = %q, want object", rt.tool.InputSchema.Type)
	}
	req := rt.tool.InputSchema.Required
	if !containsString(req, "task_id") {
		t.Errorf("required missing task_id: %v", req)
	}
	if !containsString(req, "injected_context") {
		t.Errorf("required missing injected_context: %v", req)
	}
	for _, prop := range []string{"project", "task_id", "injected_context", "prefer_same_session", "executor_override", "force"} {
		if _, ok := rt.tool.InputSchema.Properties[prop]; !ok {
			t.Errorf("schema missing property %q", prop)
		}
	}
	desc := strings.ToLower(rt.tool.Description)
	if !strings.Contains(desc, "resume-with-context") {
		t.Errorf("description should mention resume-with-context endpoint: %q", rt.tool.Description)
	}
}

func TestResumeTaskWithContext_Validation(t *testing.T) {
	hit := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		t.Fatalf("unexpected network call for invalid args: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)
	handler := s.tools["resume_task_with_context"].handler

	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"missing task_id", map[string]any{"project": "proj-a", "injected_context": "ctx"}, "task_id is required"},
		{"missing injected_context", map[string]any{"project": "proj-a", "task_id": "abc12def"}, "injected_context is required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := handler(context.Background(), tt.args)
			if err == nil {
				t.Fatalf("expected validation error, got result %q", result)
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %q, want substring %q", err.Error(), tt.want)
			}
		})
	}
	if hit {
		t.Fatal("invalid validation cases made a network call")
	}
}

func TestResumeTaskWithContext_PostsPathBodyAndFormats(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotMethod, gotPath, gotBody = r.Method, r.URL.Path, string(b)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task_id":           "abc12def",
			"resumed":           true,
			"resume_mode":       "same_session",
			"target_session_id": "ses_stored_1",
			"injected_live":     false,
			"prior_status":      "blocked",
			"abandon_reason":    "runner_offline",
			"reason":            "",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)
	handler := s.tools["resume_task_with_context"].handler

	got, err := handler(context.Background(), map[string]any{
		"project":             "proj-a",
		"task_id":             "abc12def",
		"injected_context":    "the migration already ran",
		"prefer_same_session": true,
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/api/v1/tasks/proj-a/abc12def/resume-with-context" {
		t.Errorf("path = %q, want /api/v1/tasks/proj-a/abc12def/resume-with-context", gotPath)
	}
	// Body must carry injected_context + prefer_same_session; executor_override
	// omitted when empty.
	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("body not JSON: %v (%s)", err, gotBody)
	}
	if body["injected_context"] != "the migration already ran" {
		t.Errorf("body.injected_context = %v", body["injected_context"])
	}
	if body["prefer_same_session"] != true {
		t.Errorf("body.prefer_same_session = %v, want true", body["prefer_same_session"])
	}
	if _, present := body["executor_override"]; present {
		t.Errorf("body should omit empty executor_override, got %v", body["executor_override"])
	}
	// Formatting: summary must surface mode, injected_live, session, reason.
	for _, want := range []string{"abc12def", "proj-a", "same_session", "ses_stored_1"} {
		if !strings.Contains(got, want) {
			t.Errorf("result %q missing substring %q", got, want)
		}
	}
}

func TestResumeTaskWithContext_DefaultsPreferSameSessionTrue_AndIncludesOverride(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task_id": "abc12def", "resumed": true, "resume_mode": "rehydrate",
		})
	}))
	defer server.Close()

	s := NewServer()
	client := NewAPIClient(server.URL)
	RegisterTaskTools(s, client)
	handler := s.tools["resume_task_with_context"].handler

	// prefer_same_session omitted -> defaults to true. executor_override set.
	_, err := handler(context.Background(), map[string]any{
		"project":           "proj-a",
		"task_id":           "abc12def",
		"injected_context":  "ctx",
		"executor_override": "pi",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	if body["prefer_same_session"] != true {
		t.Errorf("prefer_same_session default = %v, want true", body["prefer_same_session"])
	}
	if body["executor_override"] != "pi" {
		t.Errorf("executor_override = %v, want pi", body["executor_override"])
	}
}

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

func TestRegisterWebhookTools_Count(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterWebhookTools(s, client)

	count := len(s.tools)
	if count != 4 {
		t.Errorf("expected 4 webhook tools registered, got %d", count)
	}
}

func TestRegisterWebhookTools_Names(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterWebhookTools(s, client)

	expectedTools := []string{
		"brain_webhook_create",
		"brain_webhook_list",
		"brain_webhook_delete",
		"brain_webhook_toggle",
	}

	for _, name := range expectedTools {
		if _, ok := s.tools[name]; !ok {
			t.Errorf("tool %q not registered", name)
		}
	}
}

func TestRegisterWebhookTools_AllHandlersSet(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterWebhookTools(s, client)

	for name, rt := range s.tools {
		if rt.handler == nil {
			t.Errorf("tool %q has nil handler", name)
		}
	}
}

func TestRegisterWebhookTools_Descriptions(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterWebhookTools(s, client)

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

func TestBrainWebhookCreate_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterWebhookTools(s, client)

	tool := s.tools["brain_webhook_create"].tool

	// Required fields
	requiredSet := map[string]bool{}
	for _, r := range tool.InputSchema.Required {
		requiredSet[r] = true
	}
	if !requiredSet["url"] {
		t.Error("expected 'url' to be required")
	}
	if !requiredSet["events"] {
		t.Error("expected 'events' to be required")
	}

	// Check properties exist
	expectedProps := []string{"name", "url", "events", "filter", "secret", "enabled"}
	for _, prop := range expectedProps {
		if _, ok := tool.InputSchema.Properties[prop]; !ok {
			t.Errorf("expected property %q in schema", prop)
		}
	}
}

func TestBrainWebhookList_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterWebhookTools(s, client)

	tool := s.tools["brain_webhook_list"].tool

	// No required fields
	if len(tool.InputSchema.Required) != 0 {
		t.Errorf("brain_webhook_list required = %v, want []", tool.InputSchema.Required)
	}

	if _, ok := tool.InputSchema.Properties["enabled_only"]; !ok {
		t.Error("expected property 'enabled_only' in schema")
	}
}

func TestBrainWebhookDelete_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterWebhookTools(s, client)

	tool := s.tools["brain_webhook_delete"].tool

	requiredSet := map[string]bool{}
	for _, r := range tool.InputSchema.Required {
		requiredSet[r] = true
	}
	if !requiredSet["id"] {
		t.Error("expected 'id' to be required")
	}
}

func TestBrainWebhookToggle_Schema(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterWebhookTools(s, client)

	tool := s.tools["brain_webhook_toggle"].tool

	requiredSet := map[string]bool{}
	for _, r := range tool.InputSchema.Required {
		requiredSet[r] = true
	}
	if !requiredSet["id"] {
		t.Error("expected 'id' to be required")
	}
	if !requiredSet["enabled"] {
		t.Error("expected 'enabled' to be required")
	}
}

// =============================================================================
// Handler Tests - brain_webhook_create
// =============================================================================

func TestBrainWebhookCreate_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/webhooks" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		if req["url"] != "https://example.com/hook" {
			t.Errorf("expected url 'https://example.com/hook', got %v", req["url"])
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id":         "wh_abc123",
			"name":       "test-hook",
			"url":        "https://example.com/hook",
			"events":     []string{"task.completed"},
			"enabled":    true,
			"created_at": "2024-01-01T00:00:00Z",
			"updated_at": "2024-01-01T00:00:00Z",
		})
	}))
	defer ts.Close()

	s := NewServer()
	client := NewAPIClient(ts.URL)
	RegisterWebhookTools(s, client)

	result, err := s.tools["brain_webhook_create"].handler(context.Background(), map[string]any{
		"name":   "test-hook",
		"url":    "https://example.com/hook",
		"events": []any{"task.completed"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "wh_abc123") {
		t.Errorf("expected result to contain webhook ID, got: %s", result)
	}
	if !strings.Contains(result, "created successfully") {
		t.Errorf("expected success message, got: %s", result)
	}
}

func TestBrainWebhookCreate_MissingURL(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterWebhookTools(s, client)

	result, err := s.tools["brain_webhook_create"].handler(context.Background(), map[string]any{
		"events": []any{"task.completed"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "url is required") {
		t.Errorf("expected 'url is required' error, got: %s", result)
	}
}

func TestBrainWebhookCreate_MissingEvents(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterWebhookTools(s, client)

	result, err := s.tools["brain_webhook_create"].handler(context.Background(), map[string]any{
		"url": "https://example.com/hook",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "events must be") {
		t.Errorf("expected events validation error, got: %s", result)
	}
}

func TestBrainWebhookCreate_InvalidURL(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterWebhookTools(s, client)

	result, err := s.tools["brain_webhook_create"].handler(context.Background(), map[string]any{
		"url":    "not-a-url",
		"events": []any{"task.completed"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "invalid URL") {
		t.Errorf("expected 'invalid URL' error, got: %s", result)
	}
}

func TestBrainWebhookCreate_WithFilter(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		filter, ok := req["filter"].(map[string]any)
		if !ok {
			t.Error("expected filter in request body")
		}
		if filter["project"] != "my-project" {
			t.Errorf("expected filter project 'my-project', got %v", filter["project"])
		}

		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"id":         "wh_abc123",
			"name":       "filtered-hook",
			"url":        "https://example.com/hook",
			"events":     []string{"task.completed"},
			"filter":     map[string]string{"project": "my-project"},
			"enabled":    true,
			"created_at": "2024-01-01T00:00:00Z",
			"updated_at": "2024-01-01T00:00:00Z",
		})
	}))
	defer ts.Close()

	s := NewServer()
	client := NewAPIClient(ts.URL)
	RegisterWebhookTools(s, client)

	result, err := s.tools["brain_webhook_create"].handler(context.Background(), map[string]any{
		"name":   "filtered-hook",
		"url":    "https://example.com/hook",
		"events": []any{"task.completed"},
		"filter": map[string]any{"project": "my-project"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "project=my-project") {
		t.Errorf("expected filter in result, got: %s", result)
	}
}

func TestBrainWebhookCreate_Conflict(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		json.NewEncoder(w).Encode(map[string]any{
			"error":   "Conflict",
			"message": "webhook already exists for this URL",
		})
	}))
	defer ts.Close()

	s := NewServer()
	client := NewAPIClient(ts.URL)
	RegisterWebhookTools(s, client)

	result, err := s.tools["brain_webhook_create"].handler(context.Background(), map[string]any{
		"url":    "https://example.com/hook",
		"events": []any{"task.completed"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "already exists") {
		t.Errorf("expected 'already exists' message, got: %s", result)
	}
}

// =============================================================================
// Handler Tests - brain_webhook_list
// =============================================================================

func TestBrainWebhookList_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" || r.URL.Path != "/api/v1/webhooks" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"webhooks": []map[string]any{
				{
					"id":         "wh_001",
					"name":       "deploy-hook",
					"url":        "https://example.com/deploy",
					"events":     []string{"task.completed"},
					"enabled":    true,
					"created_at": "2024-01-01T00:00:00Z",
					"updated_at": "2024-01-01T00:00:00Z",
				},
				{
					"id":         "wh_002",
					"name":       "notify-hook",
					"url":        "https://example.com/notify",
					"events":     []string{"task.*"},
					"enabled":    false,
					"created_at": "2024-01-01T00:00:00Z",
					"updated_at": "2024-01-01T00:00:00Z",
				},
			},
		})
	}))
	defer ts.Close()

	s := NewServer()
	client := NewAPIClient(ts.URL)
	RegisterWebhookTools(s, client)

	result, err := s.tools["brain_webhook_list"].handler(context.Background(), map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "wh_001") {
		t.Errorf("expected webhook ID in result, got: %s", result)
	}
	if !strings.Contains(result, "deploy-hook") {
		t.Errorf("expected webhook name in result, got: %s", result)
	}
	if !strings.Contains(result, "disabled") {
		t.Errorf("expected 'disabled' status for second webhook, got: %s", result)
	}
	if !strings.Contains(result, "Webhooks (2)") {
		t.Errorf("expected webhook count header, got: %s", result)
	}
}

func TestBrainWebhookList_Empty(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"webhooks": []map[string]any{},
		})
	}))
	defer ts.Close()

	s := NewServer()
	client := NewAPIClient(ts.URL)
	RegisterWebhookTools(s, client)

	result, err := s.tools["brain_webhook_list"].handler(context.Background(), map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "No webhooks registered") {
		t.Errorf("expected empty message, got: %s", result)
	}
}

func TestBrainWebhookList_EnabledOnly(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("enabled") != "true" {
			t.Error("expected 'enabled=true' query param")
		}

		json.NewEncoder(w).Encode(map[string]any{
			"webhooks": []map[string]any{},
		})
	}))
	defer ts.Close()

	s := NewServer()
	client := NewAPIClient(ts.URL)
	RegisterWebhookTools(s, client)

	_, err := s.tools["brain_webhook_list"].handler(context.Background(), map[string]any{
		"enabled_only": true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// =============================================================================
// Handler Tests - brain_webhook_delete
// =============================================================================

func TestBrainWebhookDelete_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "DELETE" || r.URL.Path != "/api/v1/webhooks/wh_abc123" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		json.NewEncoder(w).Encode(map[string]any{
			"success": true,
		})
	}))
	defer ts.Close()

	s := NewServer()
	client := NewAPIClient(ts.URL)
	RegisterWebhookTools(s, client)

	result, err := s.tools["brain_webhook_delete"].handler(context.Background(), map[string]any{
		"id": "wh_abc123",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "deleted successfully") {
		t.Errorf("expected success message, got: %s", result)
	}
}

func TestBrainWebhookDelete_MissingID(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterWebhookTools(s, client)

	result, err := s.tools["brain_webhook_delete"].handler(context.Background(), map[string]any{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "id is required") {
		t.Errorf("expected 'id is required' error, got: %s", result)
	}
}

func TestBrainWebhookDelete_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{
			"error":   "Not Found",
			"message": "Webhook not found: wh_missing",
		})
	}))
	defer ts.Close()

	s := NewServer()
	client := NewAPIClient(ts.URL)
	RegisterWebhookTools(s, client)

	result, err := s.tools["brain_webhook_delete"].handler(context.Background(), map[string]any{
		"id": "wh_missing",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "not found") {
		t.Errorf("expected 'not found' message, got: %s", result)
	}
}

// =============================================================================
// Handler Tests - brain_webhook_toggle
// =============================================================================

func TestBrainWebhookToggle_Disable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PATCH" || r.URL.Path != "/api/v1/webhooks/wh_abc123" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}

		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		if req["enabled"] != false {
			t.Errorf("expected enabled=false, got %v", req["enabled"])
		}

		json.NewEncoder(w).Encode(map[string]any{
			"id":         "wh_abc123",
			"name":       "test-hook",
			"url":        "https://example.com/hook",
			"events":     []string{"task.completed"},
			"enabled":    false,
			"created_at": "2024-01-01T00:00:00Z",
			"updated_at": "2024-01-01T00:00:00Z",
		})
	}))
	defer ts.Close()

	s := NewServer()
	client := NewAPIClient(ts.URL)
	RegisterWebhookTools(s, client)

	result, err := s.tools["brain_webhook_toggle"].handler(context.Background(), map[string]any{
		"id":      "wh_abc123",
		"enabled": false,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "disabled") {
		t.Errorf("expected 'disabled' in result, got: %s", result)
	}
}

func TestBrainWebhookToggle_Enable(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)

		if req["enabled"] != true {
			t.Errorf("expected enabled=true, got %v", req["enabled"])
		}

		json.NewEncoder(w).Encode(map[string]any{
			"id":         "wh_abc123",
			"name":       "test-hook",
			"url":        "https://example.com/hook",
			"events":     []string{"task.completed"},
			"enabled":    true,
			"created_at": "2024-01-01T00:00:00Z",
			"updated_at": "2024-01-01T00:00:00Z",
		})
	}))
	defer ts.Close()

	s := NewServer()
	client := NewAPIClient(ts.URL)
	RegisterWebhookTools(s, client)

	result, err := s.tools["brain_webhook_toggle"].handler(context.Background(), map[string]any{
		"id":      "wh_abc123",
		"enabled": true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "enabled") {
		t.Errorf("expected 'enabled' in result, got: %s", result)
	}
}

func TestBrainWebhookToggle_MissingID(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterWebhookTools(s, client)

	result, err := s.tools["brain_webhook_toggle"].handler(context.Background(), map[string]any{
		"enabled": true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "id is required") {
		t.Errorf("expected 'id is required' error, got: %s", result)
	}
}

func TestBrainWebhookToggle_NotFound(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{
			"error":   "Not Found",
			"message": "Webhook not found: wh_missing",
		})
	}))
	defer ts.Close()

	s := NewServer()
	client := NewAPIClient(ts.URL)
	RegisterWebhookTools(s, client)

	result, err := s.tools["brain_webhook_toggle"].handler(context.Background(), map[string]any{
		"id":      "wh_missing",
		"enabled": true,
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "not found") {
		t.Errorf("expected 'not found' message, got: %s", result)
	}
}

// =============================================================================
// No Collision Test
// =============================================================================

func TestWebhookToolsNoCollision(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")

	RegisterBrainTools(s, client)
	RegisterTaskTools(s, client)
	RegisterWebhookTools(s, client)

	// Verify webhook tools are all present and didn't collide
	webhookTools := []string{
		"brain_webhook_create",
		"brain_webhook_list",
		"brain_webhook_delete",
		"brain_webhook_toggle",
	}

	for _, name := range webhookTools {
		if _, ok := s.tools[name]; !ok {
			t.Errorf("webhook tool %q missing after registering all tool groups", name)
		}
	}
}

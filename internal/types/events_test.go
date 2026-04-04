package types

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// =============================================================================
// MatchEventPattern Tests (Route T - has conditionals, wildcards, edge cases)
// =============================================================================

func TestMatchEventPattern(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		eventType string
		want      bool
	}{
		// Exact matches
		{"exact match task.started", "task.started", "task.started", true},
		{"exact match runner.started", "runner.started", "runner.started", true},
		{"exact no match", "task.started", "task.completed", false},

		// Wildcard matches
		{"wildcard task.*", "task.*", "task.started", true},
		{"wildcard task.* completed", "task.*", "task.completed", true},
		{"wildcard task.* no match runner", "task.*", "runner.started", false},
		{"wildcard runner.*", "runner.*", "runner.stopped", true},
		{"wildcard project.*", "project.*", "project.started", true},
		{"wildcard feature.*", "feature.*", "feature.completed", true},
		{"wildcard entry.*", "entry.*", "entry.created", true},

		// Global wildcard
		{"global wildcard *", "*", "task.started", true},
		{"global wildcard * runner", "*", "runner.stopped", true},

		// Edge cases
		{"empty pattern", "", "task.started", false},
		{"empty event type", "task.*", "", false},
		{"both empty", "", "", false},
		{"pattern with no wildcard, partial match", "task", "task.started", false},
		{"wildcard at wrong position", "*.started", "task.started", false}, // only suffix wildcard "namespace.*" supported
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchEventPattern(tt.pattern, tt.eventType)
			if got != tt.want {
				t.Errorf("MatchEventPattern(%q, %q) = %v, want %v", tt.pattern, tt.eventType, got, tt.want)
			}
		})
	}
}

// =============================================================================
// NewEvent Tests (Route T - generates IDs, has logic)
// =============================================================================

func TestNewEvent(t *testing.T) {
	t.Run("generates unique ID", func(t *testing.T) {
		e1 := NewEvent(EventTaskStarted, EventSourceRunner)
		e2 := NewEvent(EventTaskStarted, EventSourceRunner)

		if e1.ID == "" {
			t.Error("expected non-empty ID")
		}
		if e1.ID == e2.ID {
			t.Errorf("expected unique IDs, got %q and %q", e1.ID, e2.ID)
		}
	})

	t.Run("sets event type and source", func(t *testing.T) {
		e := NewEvent(EventTaskCompleted, EventSourceAPI)

		if e.Type != EventTaskCompleted {
			t.Errorf("expected type %q, got %q", EventTaskCompleted, e.Type)
		}
		if e.Source != EventSourceAPI {
			t.Errorf("expected source %q, got %q", EventSourceAPI, e.Source)
		}
	})

	t.Run("sets timestamp close to now", func(t *testing.T) {
		before := time.Now().UTC()
		e := NewEvent(EventRunnerStarted, EventSourceRunner)
		after := time.Now().UTC()

		if e.Timestamp.Before(before) || e.Timestamp.After(after) {
			t.Errorf("timestamp %v not between %v and %v", e.Timestamp, before, after)
		}
	})

	t.Run("ID has expected format", func(t *testing.T) {
		e := NewEvent(EventTaskStarted, EventSourceRunner)

		// ID should be prefixed with "evt_"
		if !strings.HasPrefix(e.ID, "evt_") {
			t.Errorf("expected ID to start with 'evt_', got %q", e.ID)
		}
	})
}

// =============================================================================
// Event Type Constants Tests
// =============================================================================

func TestEventTypeConstants(t *testing.T) {
	// Verify all expected event types exist as constants
	expectedTypes := []string{
		"runner.started", "runner.stopped",
		"project.started", "project.paused", "project.resumed",
		"task.claimed", "task.claim_rejected", "task.started", "task.completed",
		"task.failed", "task.blocked", "task.cancelled", "task.released",
		"task.status_changed", "task.idle_detected",
		"feature.started", "feature.completed", "feature.blocked", "feature.progress",
		"entry.created", "entry.updated", "entry.deleted",
	}

	for _, et := range expectedTypes {
		if !IsValidEventType(et) {
			t.Errorf("expected %q to be a valid event type", et)
		}
	}

	// Invalid types should fail
	invalidTypes := []string{"invalid", "", "TASK.STARTED", "task", "unknown.event"}
	for _, et := range invalidTypes {
		if IsValidEventType(et) {
			t.Errorf("expected %q to NOT be a valid event type", et)
		}
	}
}

// =============================================================================
// Event Source Constants Tests
// =============================================================================

func TestEventSourceConstants(t *testing.T) {
	if EventSourceRunner != "runner" {
		t.Errorf("expected EventSourceRunner = 'runner', got %q", EventSourceRunner)
	}
	if EventSourceAPI != "api" {
		t.Errorf("expected EventSourceAPI = 'api', got %q", EventSourceAPI)
	}
}

// =============================================================================
// JSON Serialization Tests
// =============================================================================

func TestEvent_JSONSerialization(t *testing.T) {
	e := Event{
		ID:         "evt_test123",
		Type:       EventTaskStarted,
		Source:     EventSourceRunner,
		Timestamp:  time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		RunnerID:   "runner-1",
		ProjectID:  "my-project",
		TaskID:     "abc123",
		TaskPath:   "projects/my-project/task/abc123.md",
		TaskTitle:  "My Task",
		FeatureID:  "feature-1",
		FromStatus: "pending",
		ToStatus:   "in_progress",
		Metadata:   map[string]string{"key": "value"},
	}

	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded Event
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if decoded.ID != e.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, e.ID)
	}
	if decoded.Type != e.Type {
		t.Errorf("Type: got %q, want %q", decoded.Type, e.Type)
	}
	if decoded.Source != e.Source {
		t.Errorf("Source: got %q, want %q", decoded.Source, e.Source)
	}
	if decoded.RunnerID != e.RunnerID {
		t.Errorf("RunnerID: got %q, want %q", decoded.RunnerID, e.RunnerID)
	}
	if decoded.Metadata["key"] != "value" {
		t.Errorf("Metadata[key]: got %q, want %q", decoded.Metadata["key"], "value")
	}
}

func TestEvent_JSON_OmitsEmptyFields(t *testing.T) {
	e := Event{
		ID:        "evt_test",
		Type:      EventRunnerStarted,
		Source:    EventSourceRunner,
		Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	data, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	jsonStr := string(data)
	// Optional fields should not appear
	for _, field := range []string{"runner_id", "project_id", "task_id", "task_path", "task_title", "feature_id", "from_status", "to_status", "metadata"} {
		if strings.Contains(jsonStr, field) {
			t.Errorf("expected field %q to be omitted from JSON, got: %s", field, jsonStr)
		}
	}
}

// =============================================================================
// YAML Serialization Tests
// =============================================================================

func TestEvent_YAMLSerialization(t *testing.T) {
	e := Event{
		ID:        "evt_yaml_test",
		Type:      EventTaskCompleted,
		Source:    EventSourceAPI,
		Timestamp: time.Date(2025, 6, 15, 12, 0, 0, 0, time.UTC),
		ProjectID: "test-project",
		TaskID:    "task123",
	}

	data, err := yaml.Marshal(e)
	if err != nil {
		t.Fatalf("yaml.Marshal failed: %v", err)
	}

	var decoded Event
	if err := yaml.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v", err)
	}

	if decoded.ID != e.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, e.ID)
	}
	if decoded.Type != e.Type {
		t.Errorf("Type: got %q, want %q", decoded.Type, e.Type)
	}
	if decoded.ProjectID != e.ProjectID {
		t.Errorf("ProjectID: got %q, want %q", decoded.ProjectID, e.ProjectID)
	}
}

// =============================================================================
// TriggerConfig Tests
// =============================================================================

func TestTriggerConfig_JSONSerialization(t *testing.T) {
	tc := TriggerConfig{
		Event:         "task.completed",
		Filter:        map[string]string{"project_id": "my-project"},
		Cooldown:      "5m",
		MaxConcurrent: 3,
	}

	data, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded TriggerConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if decoded.Event != tc.Event {
		t.Errorf("Event: got %q, want %q", decoded.Event, tc.Event)
	}
	if decoded.MaxConcurrent != tc.MaxConcurrent {
		t.Errorf("MaxConcurrent: got %d, want %d", decoded.MaxConcurrent, tc.MaxConcurrent)
	}
	if decoded.Filter["project_id"] != "my-project" {
		t.Errorf("Filter[project_id]: got %q, want %q", decoded.Filter["project_id"], "my-project")
	}
}

// =============================================================================
// WebhookConfig Tests
// =============================================================================

func TestWebhookConfig_JSONSerialization(t *testing.T) {
	wc := WebhookConfig{
		ID:      "wh_123",
		Name:    "My Webhook",
		URL:     "https://example.com/webhook",
		Events:  []string{"task.completed", "task.failed"},
		Filter:  map[string]string{"project_id": "prod"},
		Secret:  "secret123",
		Enabled: true,
	}

	data, err := json.Marshal(wc)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded WebhookConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if decoded.ID != wc.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, wc.ID)
	}
	if decoded.URL != wc.URL {
		t.Errorf("URL: got %q, want %q", decoded.URL, wc.URL)
	}
	if len(decoded.Events) != 2 {
		t.Errorf("Events: got %d items, want 2", len(decoded.Events))
	}
	if !decoded.Enabled {
		t.Error("expected Enabled = true")
	}
}

// =============================================================================
// WebhookDelivery Tests
// =============================================================================

func TestWebhookDelivery_JSONSerialization(t *testing.T) {
	wd := WebhookDelivery{
		ID:           "del_123",
		WebhookID:    "wh_456",
		EventID:      "evt_789",
		EventType:    EventTaskCompleted,
		Timestamp:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		URL:          "https://example.com/webhook",
		StatusCode:   200,
		Success:      true,
		ResponseBody: `{"ok": true}`,
		Duration:     150,
	}

	data, err := json.Marshal(wd)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}

	var decoded WebhookDelivery
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}

	if decoded.ID != wd.ID {
		t.Errorf("ID: got %q, want %q", decoded.ID, wd.ID)
	}
	if decoded.StatusCode != 200 {
		t.Errorf("StatusCode: got %d, want 200", decoded.StatusCode)
	}
	if decoded.Duration != 150 {
		t.Errorf("Duration: got %d, want 150", decoded.Duration)
	}
}

package sse

import (
	"encoding/json"
	"testing"
)

// =============================================================================
// ParseEvent Tests
// =============================================================================

func TestParseEvent_ConnectedEvent(t *testing.T) {
	data := map[string]interface{}{
		"type":      "connected",
		"transport": "sse",
		"timestamp": "2025-01-01T00:00:00Z",
		"projectId": "test-project",
	}
	jsonData, _ := json.Marshal(data)

	lines := []string{
		"event: connected",
		"data: " + string(jsonData),
	}

	event, err := ParseEvent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("expected non-nil event for connected")
	}
	if event.Type != "connected" {
		t.Errorf("expected type 'connected', got %q", event.Type)
	}
	if event.Data == nil {
		t.Fatal("expected non-nil data")
	}
}

func TestParseEvent_TasksSnapshotEvent(t *testing.T) {
	data := map[string]interface{}{
		"type":      "tasks_snapshot",
		"transport": "sse",
		"timestamp": "2025-01-01T00:00:00Z",
		"projectId": "test-project",
		"tasks": []map[string]interface{}{
			{"id": "abc12345", "title": "Test Task"},
		},
		"count": 1,
		"stats": map[string]interface{}{"total": 1, "ready": 1},
	}
	jsonData, _ := json.Marshal(data)

	lines := []string{
		"event: tasks_snapshot",
		"data: " + string(jsonData),
	}

	event, err := ParseEvent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("expected non-nil event for tasks_snapshot")
	}
	if event.Type != "tasks_snapshot" {
		t.Errorf("expected type 'tasks_snapshot', got %q", event.Type)
	}
	if event.Data == nil {
		t.Fatal("expected non-nil data")
	}
	// Verify data is valid JSON
	var parsed map[string]interface{}
	if err := json.Unmarshal(event.Data, &parsed); err != nil {
		t.Fatalf("event.Data is not valid JSON: %v", err)
	}
}

func TestParseEvent_HeartbeatReturnsNil(t *testing.T) {
	data := map[string]interface{}{
		"type":      "heartbeat",
		"transport": "sse",
		"timestamp": "2025-01-01T00:00:00Z",
		"projectId": "test-project",
	}
	jsonData, _ := json.Marshal(data)

	lines := []string{
		"event: heartbeat",
		"data: " + string(jsonData),
	}

	event, err := ParseEvent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Errorf("expected nil for heartbeat, got event with type %q", event.Type)
	}
}

func TestParseEvent_ProjectDirtyReturnsNil(t *testing.T) {
	data := map[string]interface{}{
		"type":      "project_dirty",
		"transport": "sse",
		"projectId": "test-project",
	}
	jsonData, _ := json.Marshal(data)

	lines := []string{
		"event: project_dirty",
		"data: " + string(jsonData),
	}

	event, err := ParseEvent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Errorf("expected nil for project_dirty, got event with type %q", event.Type)
	}
}

func TestParseEvent_ErrorEvent(t *testing.T) {
	data := map[string]interface{}{
		"type":      "error",
		"transport": "sse",
		"projectId": "test-project",
		"message":   "something went wrong",
	}
	jsonData, _ := json.Marshal(data)

	lines := []string{
		"event: error",
		"data: " + string(jsonData),
	}

	event, err := ParseEvent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("expected non-nil event for error")
	}
	if event.Type != "error" {
		t.Errorf("expected type 'error', got %q", event.Type)
	}
	if event.Data == nil {
		t.Fatal("expected non-nil data")
	}
}

func TestParseEvent_UnknownEventReturnsNil(t *testing.T) {
	lines := []string{
		"event: unknown_event_type",
		"data: {}",
	}

	event, err := ParseEvent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Errorf("expected nil for unknown event, got event with type %q", event.Type)
	}
}

func TestParseEvent_MissingEventLine(t *testing.T) {
	lines := []string{
		"data: {}",
	}

	event, err := ParseEvent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Errorf("expected nil for missing event line, got %v", event)
	}
}

func TestParseEvent_MissingDataLine(t *testing.T) {
	lines := []string{
		"event: connected",
	}

	event, err := ParseEvent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Errorf("expected nil for missing data line, got %v", event)
	}
}

func TestParseEvent_EmptyLines(t *testing.T) {
	event, err := ParseEvent(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event != nil {
		t.Errorf("expected nil for empty lines, got %v", event)
	}
}

func TestParseEvent_InvalidJSON(t *testing.T) {
	lines := []string{
		"event: tasks_snapshot",
		"data: {invalid json",
	}

	// For known event types with invalid JSON, we expect an error
	_, err := ParseEvent(lines)
	if err == nil {
		t.Error("expected error for invalid JSON on known event type, got nil")
	}
}

func TestParseEvent_CommandEvent(t *testing.T) {
	data := map[string]interface{}{
		"type":      "dispatch",
		"taskId":    "task-123",
		"projectId": "proj-a",
	}
	jsonData, _ := json.Marshal(data)

	lines := []string{
		"event: command",
		"data: " + string(jsonData),
	}

	event, err := ParseEvent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("expected non-nil event for command")
	}
	if event.Type != "command" {
		t.Errorf("expected type 'command', got %q", event.Type)
	}
	if event.Data == nil {
		t.Fatal("expected non-nil data")
	}
}

func TestParseEvent_TasksChangedEvent(t *testing.T) {
	data := map[string]interface{}{
		"type":      "tasks_changed",
		"projectId": "proj-a",
	}
	jsonData, _ := json.Marshal(data)

	lines := []string{
		"event: tasks_changed",
		"data: " + string(jsonData),
	}

	event, err := ParseEvent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("expected non-nil event for tasks_changed")
	}
	if event.Type != "tasks_changed" {
		t.Errorf("expected type 'tasks_changed', got %q", event.Type)
	}
}

func TestParseEvent_DataPreservedAsRawJSON(t *testing.T) {
	// Verify that the raw JSON data is preserved exactly
	data := `{"type":"tasks_snapshot","tasks":[{"id":"t1"}],"count":1}`
	lines := []string{
		"event: tasks_snapshot",
		"data: " + data,
	}

	event, err := ParseEvent(lines)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if event == nil {
		t.Fatal("expected non-nil event")
	}

	// The raw JSON should be the data string
	if string(event.Data) != data {
		t.Errorf("expected data %q, got %q", data, string(event.Data))
	}
}

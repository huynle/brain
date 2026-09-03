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
		"runner.poll_complete", "runner.state_saved",
		"runner.all_paused", "runner.all_resumed", "runner.session_discovered",
		"project.started", "project.paused", "project.resumed",
		"task.claimed", "task.claim_rejected", "task.started", "task.completed",
		"task.failed", "task.blocked", "task.cancelled", "task.released",
		"task.status_changed", "task.idle_detected",
		"feature.started", "feature.completed", "feature.blocked", "feature.progress",
		"entry.created", "entry.updated", "entry.deleted",
		// Remote-control audit events must be valid so EventService.Ingest
		// accepts them (otherwise control.* audit silently no-ops).
		"control.prompt_sent", "control.permission_responded",
		"control.instance_spawned", "control.instance_killed",
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
// TriggerConfig Multi-Event + OR-able Filter Tests (Route T)
// =============================================================================

func TestTriggerConfig_EventPatterns(t *testing.T) {
	tests := []struct {
		name string
		tc   TriggerConfig
		want []string
	}{
		{
			name: "single event field only (back-compat)",
			tc:   TriggerConfig{Event: "task.completed"},
			want: []string{"task.completed"},
		},
		{
			name: "events slice only",
			tc:   TriggerConfig{Events: []string{"task.completed", "feature.completed"}},
			want: []string{"task.completed", "feature.completed"},
		},
		{
			name: "event + events combined (OR union)",
			tc:   TriggerConfig{Event: "task.completed", Events: []string{"feature.completed"}},
			want: []string{"task.completed", "feature.completed"},
		},
		{
			name: "dedupes overlapping event and events",
			tc:   TriggerConfig{Event: "task.completed", Events: []string{"task.completed", "feature.completed"}},
			want: []string{"task.completed", "feature.completed"},
		},
		{
			name: "skips empty entries and trims whitespace",
			tc:   TriggerConfig{Event: "", Events: []string{"  task.completed  ", ""}},
			want: []string{"task.completed"},
		},
		{
			name: "no patterns",
			tc:   TriggerConfig{},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.tc.EventPatterns()
			if len(got) != len(tt.want) {
				t.Fatalf("EventPatterns() = %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("EventPatterns()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestTriggerConfig_EventPatterns_NilReceiver(t *testing.T) {
	var tc *TriggerConfig
	if got := tc.EventPatterns(); got != nil {
		t.Errorf("nil receiver EventPatterns() = %v, want nil", got)
	}
}

func TestTriggerConfig_MatchesEvent(t *testing.T) {
	tests := []struct {
		name      string
		tc        TriggerConfig
		eventType string
		want      bool
	}{
		// Single-event back-compat
		{"single exact match", TriggerConfig{Event: "task.completed"}, "task.completed", true},
		{"single exact no match", TriggerConfig{Event: "task.completed"}, "task.blocked", false},
		{"single wildcard match", TriggerConfig{Event: "task.*"}, "task.blocked", true},

		// Multi-event OR semantics
		{"multi-event first matches", TriggerConfig{Events: []string{"task.completed", "feature.completed"}}, "task.completed", true},
		{"multi-event second matches", TriggerConfig{Events: []string{"task.completed", "feature.completed"}}, "feature.completed", true},
		{"multi-event none match", TriggerConfig{Events: []string{"task.completed", "feature.completed"}}, "entry.created", false},

		// Combined Event + Events
		{"combined matches event field", TriggerConfig{Event: "task.completed", Events: []string{"feature.completed"}}, "task.completed", true},
		{"combined matches events slice", TriggerConfig{Event: "task.completed", Events: []string{"feature.completed"}}, "feature.completed", true},

		// Wildcard in events slice
		{"wildcard in events matches", TriggerConfig{Events: []string{"feature.*"}}, "feature.blocked", true},
		{"global wildcard in events", TriggerConfig{Events: []string{"*"}}, "anything.goes", true},

		// Empty trigger matches nothing
		{"empty trigger no match", TriggerConfig{}, "task.completed", false},
		{"empty event type", TriggerConfig{Event: "task.*"}, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.tc.MatchesEvent(tt.eventType)
			if got != tt.want {
				t.Errorf("MatchesEvent(%q) = %v, want %v", tt.eventType, got, tt.want)
			}
		})
	}
}

func TestTriggerConfig_MatchesEvent_NilReceiver(t *testing.T) {
	var tc *TriggerConfig
	if tc.MatchesEvent("task.completed") {
		t.Error("nil receiver MatchesEvent() = true, want false")
	}
}

func TestMatchFilterValue(t *testing.T) {
	tests := []struct {
		name       string
		actual     string
		filterExpr string
		want       bool
	}{
		// Exact match (default, back-compat)
		{"exact match", "completed", "completed", true},
		{"exact no match", "blocked", "completed", false},
		{"exact empty both", "", "", true},

		// Wildcard
		{"wildcard non-empty", "anything", "*", true},
		{"wildcard empty actual", "", "*", false},

		// in: OR-able set
		{"in: first member", "completed", "in:completed,blocked", true},
		{"in: second member", "blocked", "in:completed,blocked", true},
		{"in: no member matches", "cancelled", "in:completed,blocked", false},
		{"in: single member match", "completed", "in:completed", true},
		{"in: whitespace trimmed", "blocked", "in: completed , blocked ", true},
		{"in: empty members ignored", "completed", "in:completed,,", true},
		{"in: empty actual no match", "", "in:completed,blocked", false},
		{"in: empty actual matches empty member excluded", "", "in:,", false},

		// has: set-membership over a comma-joined ACTUAL value.
		{"has: hit first element", "supernote,page,draft", "has:supernote", true},
		{"has: hit middle element", "supernote,page,draft", "has:page", true},
		{"has: hit last element", "supernote,page,draft", "has:draft", true},
		{"has: miss", "supernote,page,draft", "has:archived", false},
		{"has: single element actual hit", "supernote", "has:supernote", true},
		{"has: single element actual miss", "supernote", "has:page", false},
		// Element-exact, NOT substring: this is the whole point of has: over
		// a contains: form. "note" must not match inside "supernote".
		{"has: substring near-miss is not a match", "supernote,page", "has:note", false},
		{"has: reverse substring near-miss is not a match", "note,page", "has:supernote", false},
		{"has: whitespace around elements trimmed", "supernote , page , draft", "has:page", true},
		{"has: whitespace around operand trimmed", "supernote,page", "has: page ", true},
		{"has: empty actual is false", "", "has:page", false},
		// Fail CLOSED on an empty operand. The nearby normalizeWebhookPath
		// bug fails OPEN on empty input and matches everything; do not
		// repeat that shape.
		{"has: empty operand fails closed", "supernote,page", "has:", false},
		{"has: whitespace-only operand fails closed", "supernote,page", "has:   ", false},
		{"has: empty operand with empty actual fails closed", "", "has:", false},
		{"has: does not match an empty element in actual", "a,,b", "has:", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchFilterValue(tt.actual, tt.filterExpr)
			if got != tt.want {
				t.Errorf("MatchFilterValue(%q, %q) = %v, want %v", tt.actual, tt.filterExpr, got, tt.want)
			}
		})
	}
}

func TestTriggerConfig_MatchesFilters(t *testing.T) {
	fields := map[string]string{
		"project_id": "brain-api",
		"feature_id": "goals",
		"to_status":  "blocked",
	}
	getField := func(key string) string { return fields[key] }

	tests := []struct {
		name   string
		filter map[string]string
		want   bool
	}{
		{"no filters matches", nil, true},
		{"empty filters matches", map[string]string{}, true},
		{"single exact match", map[string]string{"project_id": "brain-api"}, true},
		{"single exact no match", map[string]string{"project_id": "other"}, false},
		{"in: OR-able match", map[string]string{"to_status": "in:completed,blocked"}, true},
		{"in: OR-able no match", map[string]string{"to_status": "in:completed,cancelled"}, false},
		{"wildcard match present field", map[string]string{"feature_id": "*"}, true},
		{"wildcard no match absent field", map[string]string{"missing": "*"}, false},
		{"all filters must match (AND across keys)", map[string]string{"project_id": "brain-api", "to_status": "in:completed,blocked"}, true},
		{"one filter fails fails all", map[string]string{"project_id": "brain-api", "to_status": "completed"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := TriggerConfig{Filter: tt.filter}
			got := tc.MatchesFilters(getField)
			if got != tt.want {
				t.Errorf("MatchesFilters() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTriggerConfig_MatchesFilters_NilReceiverAndGetter(t *testing.T) {
	var tc *TriggerConfig
	if !tc.MatchesFilters(nil) {
		t.Error("nil receiver MatchesFilters(nil) = false, want true (no filters)")
	}

	// No filters with nil getter still matches.
	empty := TriggerConfig{}
	if !empty.MatchesFilters(nil) {
		t.Error("empty filters MatchesFilters(nil) = false, want true")
	}
}

func TestTriggerConfig_MultiEvent_Serialization_RoundTrip(t *testing.T) {
	tc := TriggerConfig{
		Type:   "event",
		Event:  "task.status_changed",
		Events: []string{"feature.completed", "feature.blocked"},
		Filter: map[string]string{
			"project_id": "brain-api",
			"to_status":  "in:completed,blocked",
		},
		OncePer:       "feature_id",
		MaxConcurrent: 1,
	}

	t.Run("JSON round-trip", func(t *testing.T) {
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
		if len(decoded.Events) != 2 || decoded.Events[0] != "feature.completed" || decoded.Events[1] != "feature.blocked" {
			t.Errorf("Events: got %v, want %v", decoded.Events, tc.Events)
		}
		if decoded.Filter["to_status"] != "in:completed,blocked" {
			t.Errorf("Filter[to_status]: got %q, want %q", decoded.Filter["to_status"], "in:completed,blocked")
		}
		if decoded.OncePer != tc.OncePer {
			t.Errorf("OncePer: got %q, want %q", decoded.OncePer, tc.OncePer)
		}
	})

	t.Run("YAML round-trip", func(t *testing.T) {
		data, err := yaml.Marshal(tc)
		if err != nil {
			t.Fatalf("yaml.Marshal failed: %v", err)
		}
		var decoded TriggerConfig
		if err := yaml.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("yaml.Unmarshal failed: %v", err)
		}
		if len(decoded.Events) != 2 {
			t.Errorf("Events: got %d items, want 2", len(decoded.Events))
		}
		if decoded.Filter["to_status"] != "in:completed,blocked" {
			t.Errorf("Filter[to_status]: got %q, want %q", decoded.Filter["to_status"], "in:completed,blocked")
		}
	})
}

func TestTriggerConfig_SingleEvent_BackCompat_Serialization(t *testing.T) {
	// A legacy single-event config (no Events slice) must still round-trip
	// and must not emit an "events" key when empty.
	tc := TriggerConfig{Event: "task.completed", Filter: map[string]string{"project_id": "p"}}

	data, err := json.Marshal(tc)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	jsonStr := string(data)
	if strings.Contains(jsonStr, "events") {
		t.Errorf("expected no 'events' key in JSON for single-event config, got: %s", jsonStr)
	}

	var decoded TriggerConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if decoded.Event != "task.completed" {
		t.Errorf("Event: got %q, want %q", decoded.Event, "task.completed")
	}
	if decoded.Events != nil {
		t.Errorf("Events: got %v, want nil", decoded.Events)
	}

	// A config that previously serialized an empty "event" can still be parsed.
	legacy := `{"event":"feature.completed","filter":{"feature_id":"x"}}`
	var fromLegacy TriggerConfig
	if err := json.Unmarshal([]byte(legacy), &fromLegacy); err != nil {
		t.Fatalf("unmarshal legacy failed: %v", err)
	}
	if !fromLegacy.MatchesEvent("feature.completed") {
		t.Error("legacy single-event config should MatchesEvent(feature.completed)")
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

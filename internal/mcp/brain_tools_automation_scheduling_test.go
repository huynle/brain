package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// captureEntryRequest starts a fake Brain API that records the decoded JSON
// body of the single entries request the tool under test sends.
func captureEntryRequest(t *testing.T, method string) (*httptest.Server, *map[string]any) {
	t.Helper()
	var body map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != method || !strings.HasPrefix(r.URL.Path, "/api/v1/entries") {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"id": "auto1234", "path": "projects/test/automation/auto1234.md",
			"title": "Automation", "type": "automation", "status": "active",
		})
	}))
	t.Cleanup(server.Close)
	return server, &body
}

// TestBrainSave_ForwardsAutomationLifecycleExtendsAndSchedulingKeys pins the
// automation-scheduling write path: an automation save must carry the entry
// lifecycle fields (starts_at, expires_at, max_runs, timezone) and extends,
// which used to be forwarded only for tasks, plus the new trigger/action keys.
func TestBrainSave_ForwardsAutomationLifecycleExtendsAndSchedulingKeys(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	server, body := captureEntryRequest(t, http.MethodPost)
	s := NewServer()
	RegisterBrainTools(s, NewAPIClient(server.URL))

	_, err := s.tools["save"].handler(context.Background(), map[string]any{
		"type":       "automation",
		"title":      "Weekly review binding",
		"content":    "Per-project binding",
		"extends":    "parent01",
		"starts_at":  "2026-10-01T00:00:00Z",
		"expires_at": "2026-12-31T00:00:00Z",
		"max_runs":   5,
		"timezone":   "America/Denver",
		"trigger": map[string]any{
			"type":          "cron",
			"every":         "4d",
			"at":            "09:30",
			"stagger":       "2h",
			"catch_up":      "none",
			"calendar":      "workdays",
			"skip_if_event": map[string]any{"calendar": "work", "title": "re:(?i)^vacation"},
			"only_if_event": map[string]any{"calendar": "work", "all_day": "true"},
			"match":         map[string]any{"title": "re:standup"},
			"offset":        "-15m",
		},
		"action": map[string]any{
			"type":          "prompt",
			"prompt_append": "Focus on the API.",
		},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}

	got := *body
	for key, want := range map[string]any{
		"extends":    "parent01",
		"starts_at":  "2026-10-01T00:00:00Z",
		"expires_at": "2026-12-31T00:00:00Z",
		"max_runs":   float64(5),
		"timezone":   "America/Denver",
	} {
		if got[key] != want {
			t.Errorf("body[%q] = %#v, want %#v", key, got[key], want)
		}
	}

	trigger, ok := got["trigger"].(map[string]any)
	if !ok {
		t.Fatalf("trigger not forwarded as object: %#v", got["trigger"])
	}
	for key, want := range map[string]string{
		"every": "4d", "at": "09:30", "stagger": "2h", "catch_up": "none",
		"calendar": "workdays", "offset": "-15m",
	} {
		if trigger[key] != want {
			t.Errorf("trigger[%q] = %#v, want %q", key, trigger[key], want)
		}
	}
	for _, key := range []string{"skip_if_event", "only_if_event", "match"} {
		if _, ok := trigger[key].(map[string]any); !ok {
			t.Errorf("trigger[%q] not forwarded as object: %#v", key, trigger[key])
		}
	}
	action, ok := got["action"].(map[string]any)
	if !ok || action["prompt_append"] != "Focus on the API." {
		t.Errorf("action.prompt_append not forwarded: %#v", got["action"])
	}
}

// TestBrainSave_ExtendsIgnoredForNonAutomation keeps extends scoped to
// automation entries, like action/retry.
func TestBrainSave_ExtendsIgnoredForNonAutomation(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	for _, entryType := range []string{"summary", "task"} {
		t.Run(entryType, func(t *testing.T) {
			server, body := captureEntryRequest(t, http.MethodPost)
			s := NewServer()
			RegisterBrainTools(s, NewAPIClient(server.URL))
			_, err := s.tools["save"].handler(context.Background(), map[string]any{
				"type": entryType, "title": "Not a binding", "content": "x", "extends": "parent01",
			})
			if err != nil {
				t.Fatalf("handler error: %v", err)
			}
			if v, ok := (*body)["extends"]; ok && v != nil {
				t.Fatalf("extends forwarded for %s: %#v", entryType, v)
			}
		})
	}
}

// TestBrainSave_LifecycleIgnoredForPlainEntries keeps the lifecycle fields off
// non-task, non-automation entries.
func TestBrainSave_LifecycleIgnoredForPlainEntries(t *testing.T) {
	cachedContext = &ExecutionContext{ProjectID: "test-project"}
	defer func() { cachedContext = nil }()

	server, body := captureEntryRequest(t, http.MethodPost)
	s := NewServer()
	RegisterBrainTools(s, NewAPIClient(server.URL))
	_, err := s.tools["save"].handler(context.Background(), map[string]any{
		"type": "summary", "title": "Note", "content": "x",
		"starts_at": "2026-10-01T00:00:00Z", "max_runs": 3, "timezone": "UTC",
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	for _, key := range []string{"starts_at", "expires_at", "max_runs", "timezone"} {
		if v, ok := (*body)[key]; ok && v != nil {
			t.Errorf("%s forwarded for summary: %#v", key, v)
		}
	}
}

func TestBrainUpdate_ForwardsExtends(t *testing.T) {
	server, body := captureEntryRequest(t, http.MethodPatch)
	s := NewServer()
	RegisterBrainTools(s, NewAPIClient(server.URL))

	_, err := s.tools["update"].handler(context.Background(), map[string]any{
		"path":    "projects/test/automation/auto1234.md",
		"extends": "parent02",
		"trigger": map[string]any{"type": "calendar", "calendar": "work", "at": "start", "offset": "-15m"},
		"action":  map[string]any{"prompt_append": "extra"},
	})
	if err != nil {
		t.Fatalf("handler error: %v", err)
	}
	got := *body
	if got["extends"] != "parent02" {
		t.Errorf("extends = %#v, want parent02", got["extends"])
	}
	trigger, ok := got["trigger"].(map[string]any)
	if !ok || trigger["type"] != "calendar" || trigger["offset"] != "-15m" {
		t.Errorf("trigger not forwarded intact: %#v", got["trigger"])
	}
	action, ok := got["action"].(map[string]any)
	if !ok || action["prompt_append"] != "extra" {
		t.Errorf("action not forwarded intact: %#v", got["action"])
	}
}

// TestBrainSaveUpdate_SchemaDocumentsSchedulingKeys pins the schema surface
// an agent reads to discover the new keys.
func TestBrainSaveUpdate_SchemaDocumentsSchedulingKeys(t *testing.T) {
	s := NewServer()
	RegisterBrainTools(s, NewAPIClient("http://localhost:3333"))

	for _, name := range []string{"save", "update"} {
		props := s.tools[name].tool.InputSchema.Properties
		if _, ok := props["extends"]; !ok {
			t.Errorf("%s schema missing extends", name)
		}
		triggerDesc := props["trigger"].Description
		for _, want := range []string{
			"calendar", "every", "at", "stagger", "catch_up", "skip_if_event",
			"only_if_event", "match", "offset", "re:",
		} {
			if !strings.Contains(triggerDesc, want) {
				t.Errorf("%s trigger description missing %q", name, want)
			}
		}
		if !strings.Contains(props["action"].Description, "prompt_append") {
			t.Errorf("%s action description missing prompt_append", name)
		}
	}
}

package types

import (
	"encoding/json"
	"reflect"
	"testing"
)

// The automation scheduling keys are a wire contract (api/openapi.yaml, the
// SDKs, the PWA and pkg/frontmatter all use these exact names), and
// parseMetadataIntoEntry decodes notes.metadata into TriggerConfig by JSON,
// so a renamed tag silently drops the value on every read.
func TestAutomationSchedulingFields_JSONKeys(t *testing.T) {
	trigger := TriggerConfig{
		Type:        TriggerTypeCalendar,
		Every:       "4d",
		At:          "03:00",
		Stagger:     "2h",
		CatchUp:     "none",
		Calendar:    "work",
		SkipIfEvent: &CalendarEventFilter{Calendar: "work", Title: "re:^OOO", Description: "*", Location: "in:a,b", AllDay: "true"},
		OnlyIfEvent: &CalendarEventFilter{Calendar: "personal"},
		Match:       map[string]string{"title": "re:(?P<person>.+)"},
		Offset:      "-15m",
	}
	assertJSON(t, trigger, map[string]any{
		"type":     "calendar",
		"every":    "4d",
		"at":       "03:00",
		"stagger":  "2h",
		"catch_up": "none",
		"calendar": "work",
		"skip_if_event": map[string]any{
			"calendar": "work", "title": "re:^OOO", "description": "*", "location": "in:a,b", "all_day": "true",
		},
		"only_if_event": map[string]any{"calendar": "personal"},
		"match":         map[string]any{"title": "re:(?P<person>.+)"},
		"offset":        "-15m",
	})

	assertJSON(t, AutomationAction{Type: "prompt", PromptAppend: "more"},
		map[string]any{"type": "prompt", "prompt_append": "more"})

	refs := map[string]any{"extends": "p1", "scheduled_for": "2026-10-09T03:00:00Z", "binding": "b1"}
	assertJSON(t, BrainEntry{Extends: "p1", ScheduledFor: "2026-10-09T03:00:00Z", Binding: "b1"}, refs)
	assertJSON(t, CreateEntryRequest{Extends: "p1", ScheduledFor: "2026-10-09T03:00:00Z", Binding: "b1"}, refs)

	var update UpdateEntryRequest
	if err := json.Unmarshal([]byte(`{"extends":"p1","scheduled_for":"2026-10-09T03:00:00Z","binding":"b1"}`), &update); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if update.Extends == nil || *update.Extends != "p1" ||
		update.ScheduledFor == nil || *update.ScheduledFor != "2026-10-09T03:00:00Z" ||
		update.Binding == nil || *update.Binding != "b1" {
		t.Errorf("update = %+v, want all three references set", update)
	}
}

// TestAutomationSchedulingFields_OmittedWhenEmpty: the fields are optional,
// so existing payloads must not grow new keys.
func TestAutomationSchedulingFields_OmittedWhenEmpty(t *testing.T) {
	assertJSON(t, TriggerConfig{Type: TriggerTypeCron, Schedule: "0 3 * * *"},
		map[string]any{"type": "cron", "schedule": "0 3 * * *"})
	assertJSON(t, AutomationAction{Type: "prompt"}, map[string]any{"type": "prompt"})
	assertJSON(t, UpdateEntryRequest{}, map[string]any{})
}

func TestTriggerTypeConstants(t *testing.T) {
	for got, want := range map[string]string{
		TriggerTypeEvent:    "event",
		TriggerTypeCron:     "cron",
		TriggerTypeWebhook:  "webhook",
		TriggerTypeSession:  "session",
		TriggerTypeCalendar: "calendar",
	} {
		if got != want {
			t.Errorf("trigger type constant = %q, want %q", got, want)
		}
	}
}

// assertJSON compares v's JSON object with want. Keys BrainEntry and
// CreateEntryRequest always emit (no omitempty) are ignored unless asked for.
func assertJSON(t *testing.T, v any, want map[string]any) {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal %T: %v", v, err)
	}
	for k := range got {
		if _, asked := want[k]; !asked && isAlwaysEmittedKey(k) {
			delete(got, k)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%T JSON =\n  %v\nwant\n  %v", v, got, want)
	}
}

func isAlwaysEmittedKey(k string) bool {
	switch k {
	case "id", "path", "title", "type", "status", "content", "tags":
		return true
	}
	return false
}

package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/huynle/brain-api/internal/types"
)

func TestRegisterReminderTools_CountNamesHandlersDescriptions(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterReminderTools(s, client)

	expected := []string{
		"reminder_create",
		"reminder_list",
		"reminder_get",
		"reminder_update",
		"reminder_ack",
		"reminder_snooze",
		"reminder_delete",
	}
	if len(s.tools) != len(expected) {
		t.Fatalf("expected %d reminder tools registered, got %d", len(expected), len(s.tools))
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

func TestReminderToolSchemas(t *testing.T) {
	s := NewServer()
	client := NewAPIClient("http://localhost:3333")
	RegisterReminderTools(s, client)

	cases := []struct {
		tool     string
		required []string
		props    []string
	}{
		{"reminder_create", []string{"title"},
			[]string{"project", "global", "title", "content", "remind_at", "timezone", "action", "prompt", "agent", "model", "executor", "execution_mode", "target_workdir", "tags", "feature_id", "repeat", "repeat_until"}},
		{"reminder_list", nil, []string{"project", "state"}},
		{"reminder_get", []string{"reminder_id"}, []string{"reminder_id"}},
		{"reminder_update", []string{"reminder_id"},
			[]string{"reminder_id", "title", "content", "status", "remind_at", "timezone", "action", "prompt", "repeat", "repeat_until"}},
		{"reminder_ack", []string{"reminder_id"}, []string{"reminder_id"}},
		{"reminder_snooze", []string{"reminder_id", "remind_at"}, []string{"reminder_id", "remind_at"}},
		{"reminder_delete", []string{"reminder_id"}, []string{"reminder_id"}},
	}
	for _, c := range cases {
		rt, ok := s.tools[c.tool]
		if !ok {
			t.Fatalf("tool %q not registered", c.tool)
		}
		if len(rt.tool.InputSchema.Required) != len(c.required) {
			t.Errorf("%s required = %v, want %v", c.tool, rt.tool.InputSchema.Required, c.required)
		}
		for _, r := range c.required {
			found := false
			for _, got := range rt.tool.InputSchema.Required {
				if got == r {
					found = true
				}
			}
			if !found {
				t.Errorf("%s missing required arg %q", c.tool, r)
			}
		}
		for _, p := range c.props {
			if _, ok := rt.tool.InputSchema.Properties[p]; !ok {
				t.Errorf("%s missing property %q", c.tool, p)
			}
		}
	}
}

// The MCP Property struct has no `format`, so the RFC3339 requirement can only
// reach the model through the description. A bare "type: string" gives it no
// way to know what shape to send.
func TestReminderCreate_SpellsOutTheDateFormat(t *testing.T) {
	s := NewServer()
	RegisterReminderTools(s, NewAPIClient("http://localhost:3333"))
	desc := s.tools["reminder_create"].tool.InputSchema.Properties["remind_at"].Description
	for _, want := range []string{"RFC3339", "offset", "undated"} {
		if !strings.Contains(desc, want) {
			t.Errorf("remind_at description does not mention %q: %s", want, desc)
		}
	}
}

// action must be a closed enum, or the model can invent one and the API
// rejects it after the fact.
func TestReminderCreate_ActionIsAClosedEnum(t *testing.T) {
	s := NewServer()
	RegisterReminderTools(s, NewAPIClient("http://localhost:3333"))
	got := s.tools["reminder_create"].tool.InputSchema.Properties["action"].Enum
	if len(got) != len(types.ReminderActions) {
		t.Fatalf("action enum = %v, want %v", got, types.ReminderActions)
	}
}

// A reminder with neither a project nor global:true would be filed under
// whatever project the API host's own directory resembles — the ambient
// context trap. It must be refused instead.
func TestReminderCreate_RefusesWithoutProjectOrGlobal(t *testing.T) {
	s := NewServer()
	RegisterReminderTools(s, NewAPIClient("http://localhost:3333"))
	_, err := s.tools["reminder_create"].handler(context.Background(), map[string]any{
		"title": "no home",
	})
	if err == nil {
		t.Fatal("expected a refusal when neither project nor global is given")
	}
	if !strings.Contains(err.Error(), "project") {
		t.Errorf("error should name the missing argument, got: %v", err)
	}
}

// reminder_update must be PRESENCE-based: an emptiness check cannot express
// "clear the date", so a dated reminder could never be made undated.
func TestReminderUpdate_EmptyRemindAtClearsTheDate(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(types.ReminderSummary{
			ReminderID: "r1", Title: "t", State: types.ReminderStateUndated,
			Action: types.ReminderActionNotify,
		})
	}))
	defer srv.Close()

	s := NewServer()
	RegisterReminderTools(s, NewAPIClient(srv.URL))
	if _, err := s.tools["reminder_update"].handler(context.Background(), map[string]any{
		"reminder_id": "r1",
		"remind_at":   "",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	v, present := gotBody["remind_at"]
	if !present {
		t.Fatal(`remind_at was dropped from the request body: "clear the date" is inexpressible`)
	}
	if v != "" {
		t.Errorf("remind_at = %v, want the empty string that clears it", v)
	}
}

// And an omitted field must NOT be sent, or every update would clobber every
// field the caller did not mention.
func TestReminderUpdate_OmittedFieldsAreNotSent(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(types.ReminderSummary{ReminderID: "r1"})
	}))
	defer srv.Close()

	s := NewServer()
	RegisterReminderTools(s, NewAPIClient(srv.URL))
	if _, err := s.tools["reminder_update"].handler(context.Background(), map[string]any{
		"reminder_id": "r1",
		"title":       "new title",
	}); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, present := gotBody["remind_at"]; present {
		t.Error("remind_at was sent though the caller never mentioned it")
	}
	if gotBody["title"] != "new title" {
		t.Errorf("title = %v, want the supplied value", gotBody["title"])
	}
}

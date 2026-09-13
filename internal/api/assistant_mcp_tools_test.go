package api

import (
	"testing"

	mcppkg "github.com/huynle/brain-api/internal/mcp"
)

// TestClassifyMCPTier checks the name-based tier classifier that preserves the
// assistant's read/write/destructive confirmation UX for the adapted MCP tools.
func TestClassifyMCPTier(t *testing.T) {
	cases := []struct {
		name string
		want ToolTier
	}{
		// Reads
		{"search", TierRead},
		{"recall", TierRead},
		{"list", TierRead},
		{"tasks", TierRead},
		{"task_get", TierRead},
		{"features", TierRead},
		{"runners", TierRead},
		{"runner_status", TierRead},    // exact read override (contains no write frag)
		{"scheduler_status", TierRead}, // exact read override
		{"goal_progress", TierRead},    // exact read override
		{"goal_audit", TierRead},       // exact read override
		{"tasks_status", TierRead},     // exact read override
		// Writes
		{"save", TierWrite},
		{"update", TierWrite},
		{"create_task", TierWrite},
		{"verify", TierWrite},
		{"link", TierWrite},
		{"goal_create", TierWrite},
		{"webhook_create", TierWrite},
		{"reminder_snooze", TierWrite},
		{"trigger_task", TierWrite},
		// Destructive
		{"delete", TierDestructive},
		{"move", TierDestructive},
		{"bulk_update", TierDestructive},
		{"runner_pause_project", TierDestructive},
		{"runner_resume_project", TierDestructive},
		{"control_spawn_instance", TierDestructive},
		{"control_kill_instance", TierDestructive},
		{"control_abort_session", TierDestructive},
		{"goal_delete", TierDestructive},
		{"webhook_delete", TierDestructive},
		{"feature_checkout", TierDestructive},
	}
	for _, c := range cases {
		if got := classifyMCPTier(c.name); got != c.want {
			t.Errorf("classifyMCPTier(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestMCPSchemaToMap verifies MCP typed InputSchema converts to a JSON-Schema
// map with a non-nil "properties" object (required by the OpenRouter payload).
func TestMCPSchemaToMap(t *testing.T) {
	in := mcppkg.InputSchema{
		Type: "object",
		Properties: map[string]mcppkg.Property{
			"project": {Type: "string", Description: "project id"},
			"limit":   {Type: "number"},
		},
		Required: []string{"project"},
	}
	out := mcpSchemaToMap(in)
	if out["type"] != "object" {
		t.Errorf("type = %v, want object", out["type"])
	}
	props, ok := out["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties missing or wrong type: %T", out["properties"])
	}
	if _, ok := props["project"]; !ok {
		t.Errorf("properties missing 'project'")
	}

	// Empty schema still yields an object with a properties map.
	empty := mcpSchemaToMap(mcppkg.InputSchema{})
	if empty["type"] != "object" {
		t.Errorf("empty type = %v, want object", empty["type"])
	}
	if _, ok := empty["properties"].(map[string]any); !ok {
		t.Errorf("empty schema must still carry a properties object")
	}
}

// TestMCPResultValue confirms JSON output is structured back into a value while
// plain text is passed through.
func TestMCPResultValue(t *testing.T) {
	if v := mcpResultValue(`{"count":2}`); func() bool {
		m, ok := v.(map[string]any)
		return ok && m["count"] == float64(2)
	}() == false {
		t.Errorf("JSON object result not decoded: %#v", v)
	}
	if v := mcpResultValue("plain text"); v != "plain text" {
		t.Errorf("plain text result = %#v, want passthrough", v)
	}
	if v := mcpResultValue("   "); func() bool {
		m, ok := v.(map[string]any)
		return ok && m["ok"] == true
	}() == false {
		t.Errorf("empty result should yield ok:true, got %#v", v)
	}
}

// TestMCPToolset_DisabledWithoutBaseURL ensures the adapter is inert (nil) when
// no loopback base URL is configured, so the legacy tools remain the fallback.
func TestMCPToolset_DisabledWithoutBaseURL(t *testing.T) {
	s := &AssistantService{} // mcpBaseURL == ""
	if defs := s.mcpToolset(""); defs != nil {
		t.Errorf("mcpToolset should be nil without mcpBaseURL, got %d defs", len(defs))
	}
	// toolDefinitions falls back to the legacy in-process set.
	if defs := s.toolDefinitions(""); len(defs) == 0 {
		t.Errorf("toolDefinitions fallback returned no tools")
	}
}

// TestMCPToolset_BuildsFullSet builds the adapted MCP registry and asserts it
// covers the full surface (well beyond the 41 legacy tools) with valid,
// non-duplicate, non-nil definitions and expected tier assignments for a few
// representative tools.
func TestMCPToolset_BuildsFullSet(t *testing.T) {
	s := &AssistantService{mcpBaseURL: "http://localhost:3333"}
	defs := s.mcpToolset("")
	if len(defs) < 80 {
		t.Fatalf("expected the full MCP tool set (>=80), got %d", len(defs))
	}
	seen := map[string]ToolDefinition{}
	for _, d := range defs {
		if d.Name == "" {
			t.Errorf("tool with empty name")
		}
		if _, dup := seen[d.Name]; dup {
			t.Errorf("duplicate adapted tool %q", d.Name)
		}
		if d.Handler == nil {
			t.Errorf("tool %q has nil handler", d.Name)
		}
		if d.Schema == nil || d.Schema["type"] == nil {
			t.Errorf("tool %q has invalid schema %v", d.Name, d.Schema)
		}
		seen[d.Name] = d
	}
	// Representative tier checks against real MCP tool names.
	if d, ok := seen["search"]; !ok || d.Tier != TierRead {
		t.Errorf("search tool missing or not read: ok=%v tier=%v", ok, d.Tier)
	}
	if d, ok := seen["save"]; !ok || d.Tier != TierWrite {
		t.Errorf("save tool missing or not write: ok=%v tier=%v", ok, d.Tier)
	}
	if d, ok := seen["delete"]; !ok || d.Tier != TierDestructive {
		t.Errorf("delete tool missing or not destructive: ok=%v tier=%v", ok, d.Tier)
	}
}

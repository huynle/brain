package api

// assistant_mcp_tools.go adapts the full Brain MCP tool set into the assistant's
// ToolDefinition registry so the in-UI assistant has parity with the ~120 tools
// the MCP server exposes to external clients (Claude Code, OpenCode, editors).
//
// Design:
//   - The assistant builds a throwaway *mcp.Server, registers every MCP tool
//     group against a loopback *mcp.APIClient, and enumerates the result via
//     the exported Server.RegisteredTools().
//   - Each MCP tool becomes a ToolDefinition: its typed InputSchema is converted
//     to the raw map[string]any JSON Schema the OpenRouter payload wants, its
//     tier is derived from a name-based classifier (MCP has no tier concept),
//     and its handler is wrapped to bridge the two calling conventions.
//   - The loopback client is authenticated per-request with the caller's own
//     Bearer token, so MCP tool calls run with the caller's scope/tenant — no
//     privilege escalation, exactly like the MCP HTTP transport does.
//
// This REPLACES the 41 hand-rolled in-process tools (readTools/writeTools/
// destructiveTools). Those calls went straight to the service layer; these go
// over a localhost HTTP hop to the same server. The trade is a small latency
// cost for a single, complete, always-in-sync tool surface.

import (
	"context"
	"encoding/json"
	"strings"

	mcppkg "github.com/huynle/brain-api/internal/mcp"
)

// assistantTokenKey is the context key under which the caller's raw bearer
// token is stashed by the HTTP handlers so the agent loop can forward it to
// the loopback MCP client. Its own type prevents collisions.
type assistantTokenKey struct{}

// withAssistantToken returns ctx carrying the caller's bearer token.
func withAssistantToken(ctx context.Context, token string) context.Context {
	if token == "" {
		return ctx
	}
	return context.WithValue(ctx, assistantTokenKey{}, token)
}

// assistantTokenFromContext extracts the caller's bearer token, or "" if none.
func assistantTokenFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(assistantTokenKey{}).(string); ok {
		return v
	}
	return ""
}


// mcpToolset builds the assistant tool registry from the MCP tool set,
// authenticated with the given bearer token (may be empty when auth is
// disabled). Returns nil when MCP adaptation is not configured, letting the
// caller fall back to the legacy in-process tools.
func (s *AssistantService) mcpToolset(token string) []ToolDefinition {
	if s == nil || s.mcpBaseURL == "" {
		return nil
	}
	client := mcppkg.NewAPIClient(s.mcpBaseURL)
	if token != "" {
		client = client.WithAuthToken(token)
	}
	server := mcppkg.NewServer()
	mcppkg.RegisterBrainTools(server, client)
	mcppkg.RegisterTaskTools(server, client)
	mcppkg.RegisterFeatureTools(server, client)
	mcppkg.RegisterRunnerTools(server, client)
	mcppkg.RegisterSupervisorTools(server, client)
	mcppkg.RegisterObservabilityTools(server, client)
	mcppkg.RegisterControlTools(server, client)
	mcppkg.RegisterProjectTools(server, client)
	mcppkg.RegisterPlanningTools(server, client)
	mcppkg.RegisterWebhookTools(server, client)
	mcppkg.RegisterGoalTools(server, client)
	mcppkg.RegisterReminderTools(server, client)

	regs := server.RegisteredTools()
	defs := make([]ToolDefinition, 0, len(regs))
	for _, rt := range regs {
		defs = append(defs, adaptMCPTool(rt))
	}
	return defs
}

// adaptMCPTool converts one MCP RegisteredTool into an assistant ToolDefinition.
func adaptMCPTool(rt mcppkg.RegisteredTool) ToolDefinition {
	handler := rt.Handler
	name := rt.Tool.Name
	return ToolDefinition{
		Name:        name,
		Description: rt.Tool.Description,
		Tier:        classifyMCPTier(name),
		Schema:      mcpSchemaToMap(rt.Tool.InputSchema),
		Handler: func(ctx context.Context, s *AssistantService, defaultProject string, args json.RawMessage) (any, error) {
			m := decodeArgs(args)
			// The assistant threads a default project per request; MCP tools
			// take an explicit "project" arg. Inject it when the model omitted
			// one so tools resolve against the active project.
			if defaultProject != "" {
				if _, ok := m["project"]; !ok {
					m["project"] = defaultProject
				}
			}
			// The destructive gate already consumed _explicit in executeToolCall;
			// strip it so it never leaks into the MCP tool's arg map.
			delete(m, "_explicit")
			text, err := handler(ctx, m)
			if err != nil {
				return nil, err
			}
			return mcpResultValue(text), nil
		},
	}
}

// mcpResultValue turns an MCP tool's text result into a value the assistant
// loop can feed back to the model. MCP handlers return pre-rendered text that
// is usually JSON; when it parses, hand back the structured value so the
// loop's summarizer/encoder can work with it, otherwise pass the raw string.
func mcpResultValue(text string) any {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return map[string]any{"ok": true}
	}
	if trimmed[0] == '{' || trimmed[0] == '[' {
		var v any
		if err := json.Unmarshal([]byte(trimmed), &v); err == nil {
			return v
		}
	}
	return trimmed
}

// mcpSchemaToMap converts the MCP typed InputSchema into the raw JSON Schema
// map the OpenRouter tools payload expects. It round-trips through JSON so the
// omitempty tags on Property are honored (e.g. absent enum/items are dropped).
func mcpSchemaToMap(in mcppkg.InputSchema) map[string]any {
	if in.Type == "" {
		in.Type = "object"
	}
	b, err := json.Marshal(in)
	if err != nil {
		return map[string]any{"type": "object"}
	}
	out := map[string]any{}
	if err := json.Unmarshal(b, &out); err != nil {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	// OpenRouter/OpenAI function schemas require a "properties" object even
	// when the tool takes no args. A tool with no properties marshals the nil
	// map to JSON null, so normalize both "absent" and "null" to an empty map.
	if m, ok := out["properties"].(map[string]any); !ok || m == nil {
		out["properties"] = map[string]any{}
	}
	return out
}

// destructiveMCPExact names MCP tools whose destructive nature the substring
// heuristic below would miss or misjudge. Membership here forces
// TierDestructive (UI confirmation) regardless of the name pattern.
var destructiveMCPExact = map[string]struct{}{
	"delete":                 {},
	"move":                   {},
	"bulk_update":            {},
	"control_spawn_instance": {},
	"control_kill_instance":  {},
	"control_send_prompt":    {},
	"control_abort_session":  {},
	"control_permission":     {},
	"webhook_delete":         {},
	"reminder_delete":        {},
	"goal_delete":            {},
	"runner_pause_project":   {},
	"runner_resume_project":  {},
	"feature_checkout":       {},
	"resume_task_with_context": {},
}

// readMCPExact names read-only tools the write/destructive substring heuristic
// would otherwise misclassify (e.g. names containing "update"/"status" that
// only report state).
var readMCPExact = map[string]struct{}{
	"runner_status":    {},
	"scheduler_status": {},
	"tasks_status":     {},
	"goal_progress":    {},
	"goal_audit":       {},
}

// classifyMCPTier assigns a ToolTier to an MCP tool from its name. MCP has no
// tier concept, so this preserves the assistant's read/write/destructive
// confirmation UX. Destructive tools (delete/move/bulk/pause/kill/abort/spawn)
// require the model to set _explicit=true and surface as confirmable proposed
// actions; write tools (create/update/save/trigger/run) auto-execute; the rest
// are treated as reads.
//
// Matching is on underscore-delimited segments (not raw substrings) so tokens
// like "run" match the verb "run" or "run_goal" but never the noun "runners".
func classifyMCPTier(name string) ToolTier {
	if _, ok := destructiveMCPExact[name]; ok {
		return TierDestructive
	}
	if _, ok := readMCPExact[name]; ok {
		return TierRead
	}
	segs := strings.Split(name, "_")
	segSet := make(map[string]struct{}, len(segs))
	for _, s := range segs {
		segSet[s] = struct{}{}
	}
	for _, frag := range destructiveVerbs {
		if _, ok := segSet[frag]; ok {
			return TierDestructive
		}
	}
	for _, frag := range writeVerbs {
		if _, ok := segSet[frag]; ok {
			return TierWrite
		}
	}
	return TierRead
}

// destructiveVerbs are name segments that mark a tool as destructive.
var destructiveVerbs = []string{
	"delete", "remove", "kill", "abort", "spawn", "pause", "resume",
	"bulk", "move", "cancel", "revoke", "unpublish",
}

// writeVerbs are name segments that mark a tool as a non-destructive mutation.
// Checked only after destructiveVerbs. Segments only — "run" matches the verb
// "run" / "run_goal" but not the noun "runners".
var writeVerbs = []string{
	"save", "create", "update", "add", "set", "enable", "disable",
	"assign", "clear", "link", "verify", "trigger", "run", "checkout",
	"snooze", "ack", "attach", "detach", "grant", "put", "publish",
	"reserve", "commit", "record", "steer", "draft", "reorder",
}

// assistantAvailableToolNames returns the tool names the assistant exposes for
// the given token, for the Status endpoint's capability list.
func (s *AssistantService) assistantAvailableToolNames() []string {
	defs := s.toolDefinitions("")
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Name)
	}
	return names
}

// toolDefinitions returns the active tool registry for a request. When MCP
// adaptation is configured it returns the full MCP-backed set; otherwise it
// falls back to the legacy in-process tools so behavior is unchanged in
// deployments/tests that don't wire a loopback base URL.
func (s *AssistantService) toolDefinitions(token string) []ToolDefinition {
	if defs := s.mcpToolset(token); len(defs) > 0 {
		return defs
	}
	return ListToolDefinitions()
}

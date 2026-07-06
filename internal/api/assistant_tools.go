package api

// This file implements the tool registry that powers the agentic assistant
// loop. The old single-shot planner returned a JSON blob with a `reply` and
// `actions` array; the new loop uses OpenAI-style function/tool calls so the
// model can iteratively read Brain state, make decisions, and issue writes.
//
// Design:
//   - Every tool has a stable name, JSON schema, tier (read/write/destructive),
//     and a handler that consumes decoded args + the service context.
//   - Tools are declared via a package-level slice so tests can iterate them.
//   - The registry does no execution itself; it is data. `runAgentLoop` in
//     assistant_loop.go wires tools into the OpenRouter payload and dispatches
//     calls back to the handlers here.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// ToolTier controls whether the assistant may auto-execute a tool call, must
// gate on the model marking the call `explicit=true`, or is always safe as a
// pure read.
type ToolTier int

const (
	// TierRead: pure read, always auto-executes and result is fed back to the model.
	TierRead ToolTier = iota
	// TierWrite: non-destructive mutation. Auto-executes. Result appears in
	// executed_actions and is echoed back to the model.
	TierWrite
	// TierDestructive: destructive/bulk mutation. Only executes when the model
	// sets the tool call arg `_explicit=true`; otherwise surfaces as a proposed
	// action for the UI to confirm.
	TierDestructive
)

// ToolDefinition describes a single tool the assistant can call. Handlers
// receive raw JSON args so the tool schema stays the single source of truth;
// each handler decodes into its own typed request struct.
type ToolDefinition struct {
	Name        string
	Description string
	Tier        ToolTier
	// Schema is a JSON Schema object (as a map so it serializes as-is to
	// OpenRouter's `tools[].function.parameters`).
	Schema  map[string]any
	Handler func(ctx context.Context, s *AssistantService, defaultProject string, args json.RawMessage) (any, error)
}

// ToolCallEvent is emitted on the stream when the model asks to invoke a tool.
type ToolCallEvent struct {
	ID   string          `json:"id"`
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
	Tier string          `json:"tier"`
}

// ToolResultEvent is emitted on the stream once a tool has run (or been
// deferred as a proposed action).
type ToolResultEvent struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`             // "completed" | "failed" | "proposed"
	Result   any    `json:"result,omitempty"`   // sanitized/truncated for stream
	Error    string `json:"error,omitempty"`
	Proposed bool   `json:"proposed,omitempty"` // true when a destructive call was gated
}

// tierName renders a ToolTier as the string exposed to the client stream.
func (t ToolTier) String() string {
	switch t {
	case TierRead:
		return "read"
	case TierWrite:
		return "write"
	case TierDestructive:
		return "destructive"
	default:
		return "unknown"
	}
}

// stringArg pulls a required or optional string from a decoded arg map. The
// zero value is returned when the key is absent or not a string.
func stringArg(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// intArg pulls an int-ish value out. JSON numbers arrive as float64 so we
// coerce.
func intArg(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return 0
}

// boolPtrArg returns a *bool when the key is present, else nil so callers can
// tell "absent" from "false".
func boolPtrArg(m map[string]any, key string) *bool {
	if v, ok := m[key].(bool); ok {
		return &v
	}
	return nil
}

// stringSliceArg returns a []string from either a JSON array or a
// comma-separated string.
func stringSliceArg(m map[string]any, key string) []string {
	switch v := m[key].(type) {
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		parts := strings.Split(v, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			if s := strings.TrimSpace(p); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

// decodeArgs unmarshals raw JSON into a generic map. Empty or malformed args
// yield an empty map rather than an error so the tool handler can decide
// whether missing args are fatal.
func decodeArgs(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	m := map[string]any{}
	_ = json.Unmarshal(raw, &m)
	return m
}

// resolveProject picks project from args, falling back to the request's
// default project. Returns empty string if neither is set (some tools accept
// this, e.g. cross-project search when Global is true).
func resolveProject(args map[string]any, defaultProject string) string {
	if p := stringArg(args, "project"); p != "" {
		return p
	}
	return defaultProject
}

// requiredString extracts a required string arg and errors out with a
// consistent message.
func requiredString(args map[string]any, key string) (string, error) {
	v := stringArg(args, key)
	if v == "" {
		return "", fmt.Errorf("missing required arg %q", key)
	}
	return v, nil
}

// truncateEntries caps a list of entries for streaming so the model doesn't
// see thousands of rows. Returns the trimmed slice and a bool indicating
// truncation.
func truncateEntries(entries []types.BrainEntry, cap int) ([]types.BrainEntry, bool) {
	if cap <= 0 || len(entries) <= cap {
		return entries, false
	}
	return entries[:cap], true
}

func truncateResolvedTasks(tasks []types.ResolvedTask, cap int) ([]types.ResolvedTask, bool) {
	if cap <= 0 || len(tasks) <= cap {
		return tasks, false
	}
	return tasks[:cap], true
}

// summarizeEntry returns a shallow view of a BrainEntry suitable for feeding
// to the model without dragging along the entire content body every time.
func summarizeEntry(e types.BrainEntry) map[string]any {
	m := map[string]any{
		"id":     e.ID,
		"path":   e.Path,
		"title":  e.Title,
		"type":   e.Type,
		"status": e.Status,
	}
	if e.ProjectID != "" {
		m["project"] = e.ProjectID
	}
	if e.FeatureID != "" {
		m["feature_id"] = e.FeatureID
	}
	if e.Priority != "" {
		m["priority"] = e.Priority
	}
	if len(e.Tags) > 0 {
		m["tags"] = e.Tags
	}
	if e.Schedule != "" {
		m["schedule"] = e.Schedule
	}
	if e.NextRun != "" {
		m["next_run"] = e.NextRun
	}
	if e.Modified != "" {
		m["modified"] = e.Modified
	}
	return m
}

// summarizeTask returns a compact view of a ResolvedTask.
func summarizeTask(t types.ResolvedTask) map[string]any {
	m := map[string]any{
		"id":             t.ID,
		"path":           t.Path,
		"title":          t.Title,
		"status":         t.Status,
		"priority":       t.Priority,
		"classification": t.Classification,
	}
	if t.ProjectID != "" {
		m["project"] = t.ProjectID
	}
	if t.FeatureID != "" {
		m["feature_id"] = t.FeatureID
	}
	if len(t.DependsOn) > 0 {
		m["depends_on"] = t.DependsOn
	}
	if len(t.BlockedBy) > 0 {
		m["blocked_by"] = t.BlockedBy
	}
	if t.BlockedByReason != "" {
		m["blocked_by_reason"] = t.BlockedByReason
	}
	if len(t.WaitingOn) > 0 {
		m["waiting_on"] = t.WaitingOn
	}
	if t.InCycle {
		m["in_cycle"] = true
	}
	if t.Agent != "" {
		m["agent"] = t.Agent
	}
	if t.Executor != "" {
		m["executor"] = t.Executor
	}
	if t.ResolvedWorkdir != "" {
		m["resolved_workdir"] = t.ResolvedWorkdir
	}
	if t.Schedule != "" {
		m["schedule"] = t.Schedule
	}
	if t.NextRun != "" {
		m["next_run"] = t.NextRun
	}
	return m
}

// ListToolDefinitions returns all tool definitions the assistant exposes. The
// slice is built lazily on each call so tests can assert on it without global
// state.
func ListToolDefinitions() []ToolDefinition {
	defs := []ToolDefinition{}
	defs = append(defs, readTools()...)
	defs = append(defs, writeTools()...)
	defs = append(defs, destructiveTools()...)
	return defs
}

// toolIndex maps name -> definition for fast dispatch inside the agent loop.
func toolIndex(defs []ToolDefinition) map[string]ToolDefinition {
	out := make(map[string]ToolDefinition, len(defs))
	for _, d := range defs {
		out[d.Name] = d
	}
	return out
}

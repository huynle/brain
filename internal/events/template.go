// Package events — Template variable resolution for automation prompts and commands.
//
// Uses Go text/template to resolve variables in automation action prompts
// and commands. The TemplateContext is populated from the event payload,
// making all event data available to template authors.
package events

import (
	"bytes"
	"fmt"
	"log/slog"
	"text/template"
	"time"
)

// TemplateContext holds all variables available to automation templates.
// Fields are populated from the event payload when an automation fires.
type TemplateContext struct {
	Project            string         // Project ID from the event
	FeatureID          string         // Feature ID from payload
	TaskID             string         // Task short ID from payload ("id")
	TaskPath           string         // Full task path from payload ("path")
	Workdir            string         // Working directory from payload
	MergeTargetBranch  string         // Target branch for merge operations
	PreviousTaskOutput string         // Output from the previous task in a chain
	Payload            map[string]any // Full event payload for custom field access
	Event              string         // Event type string (e.g. "task.completed")
	ScheduleTime       string         // Scheduled fire time (for cron automations)
	RunNumber          int            // Run counter (for cron automations)
	RunnerID           string         // ID of the runner processing the event
	Date               string         // Current date in YYYY-MM-DD format
	DayOfWeek          string         // Current day name (e.g. "Monday")
}

// NewTemplateContext creates a TemplateContext populated from an event.
// String fields are extracted from the event payload; missing keys default
// to empty strings. The Payload map is copied so templates can access
// arbitrary event data via {{index .Payload "key"}}.
func NewTemplateContext(event Event) TemplateContext {
	payload := event.Payload
	if payload == nil {
		payload = map[string]any{}
	}

	now := time.Now()

	ctx := TemplateContext{
		Project:            event.ProjectID,
		FeatureID:          payloadString(payload, "feature_id"),
		TaskID:             payloadString(payload, "id"),
		TaskPath:           payloadString(payload, "path"),
		Workdir:            payloadString(payload, "workdir"),
		MergeTargetBranch:  payloadString(payload, "merge_target_branch"),
		PreviousTaskOutput: payloadString(payload, "previous_output"),
		RunnerID:           payloadString(payload, "runner_id"),
		Event:              string(event.Type),
		ScheduleTime:       payloadString(payload, "schedule_time"),
		RunNumber:          payloadInt(payload, "run_number"),
		Date:               now.Format("2006-01-02"),
		DayOfWeek:          now.Weekday().String(),
		Payload:            payload,
	}

	return ctx
}

// ResolveTemplate resolves a Go text/template string against the given context.
// Returns the rendered string or an error if the template is invalid or
// references an undefined field. Empty templates return an empty string.
func ResolveTemplate(tmpl string, ctx TemplateContext) (string, error) {
	if tmpl == "" {
		return "", nil
	}

	t, err := template.New("action").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("template_resolver: invalid template syntax: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, ctx); err != nil {
		slog.Warn("template_resolver: template execution failed", "error", err)
		return "", fmt.Errorf("template_resolver: execution failed: %w", err)
	}

	return buf.String(), nil
}

// ResolveActionTemplates resolves both the direct_prompt and command templates
// against the given context. If either template has invalid syntax or fails
// to execute, an error is returned and neither result should be used.
func ResolveActionTemplates(prompt, command string, ctx TemplateContext) (string, string, error) {
	resolvedPrompt, err := ResolveTemplate(prompt, ctx)
	if err != nil {
		return "", "", fmt.Errorf("template_resolver: prompt: %w", err)
	}

	resolvedCmd, err := ResolveTemplate(command, ctx)
	if err != nil {
		return "", "", fmt.Errorf("template_resolver: command: %w", err)
	}

	return resolvedPrompt, resolvedCmd, nil
}

// payloadString extracts a string value from the payload map.
// Returns empty string if the key is missing or not a string.
func payloadString(payload map[string]any, key string) string {
	val, ok := payload[key]
	if !ok {
		return ""
	}
	s, ok := val.(string)
	if ok {
		return s
	}
	return fmt.Sprintf("%v", val)
}

// payloadInt extracts an int value from the payload map.
// Returns 0 if the key is missing or not an int-like type.
func payloadInt(payload map[string]any, key string) int {
	val, ok := payload[key]
	if !ok {
		return 0
	}
	switch v := val.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

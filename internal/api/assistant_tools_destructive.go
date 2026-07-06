package api

// Tier-3 (destructive/bulk) tool handlers. Only execute when the tool call
// arg _explicit=true; otherwise the loop surfaces a proposed-action stub
// for the PWA to render as a confirmation card.
//
// The _explicit gate is enforced in AssistantService.executeToolCall — the
// handlers here can assume they were only invoked because the user
// confirmed.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/huynle/brain-api/internal/types"
)

// destructiveTools returns all TierDestructive tool definitions.
func destructiveTools() []ToolDefinition {
	return []ToolDefinition{
		deleteEntryTool(),
		bulkUpdateTool(),
		moveEntryTool(),
		pauseProjectTool(),
		resumeProjectTool(),
		pauseAutomationsTool(),
		resumeAutomationsTool(),
		assignFeatureTool(),
		clearFeatureAssignmentTool(),
	}
}

// ─── delete_entry ─────────────────────────────────────────────────────

func deleteEntryTool() ToolDefinition {
	return ToolDefinition{
		Name:        "delete_entry",
		Description: "Permanently delete a brain entry (task/automation/note/etc.). Requires _explicit=true after user confirmation.",
		Tier:        TierDestructive,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path_or_id": map[string]any{"type": "string"},
				"_explicit":  map[string]any{"type": "boolean", "description": "Must be true to actually delete; otherwise the tool call is deferred as a proposed action."},
			},
			"required": []string{"path_or_id"},
		},
		Handler: func(ctx context.Context, s *AssistantService, _ string, raw json.RawMessage) (any, error) {
			if s.brain == nil {
				return nil, fmt.Errorf("brain service unavailable")
			}
			args := decodeArgs(raw)
			id, err := requiredString(args, "path_or_id")
			if err != nil {
				return nil, err
			}
			if err := s.brain.Delete(ctx, id); err != nil {
				return nil, err
			}
			return map[string]any{"deleted": id}, nil
		},
	}
}

// ─── bulk_update ──────────────────────────────────────────────────────

func bulkUpdateTool() ToolDefinition {
	return ToolDefinition{
		Name:        "bulk_update",
		Description: "Apply updates to many brain entries in one shot. Use dry_run=true first to preview. Requires _explicit=true.",
		Tier:        TierDestructive,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"filter": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"project":    map[string]any{"type": "string"},
						"type":       map[string]any{"type": "string"},
						"status":     map[string]any{"type": "string"},
						"feature_id": map[string]any{"type": "string"},
						"tags":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"priority":   map[string]any{"type": "string"},
					},
				},
				"updates": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"status":   map[string]any{"type": "string"},
						"priority": map[string]any{"type": "string"},
						"tags":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
						"append":   map[string]any{"type": "string"},
						"note":     map[string]any{"type": "string"},
					},
				},
				"dry_run":   map[string]any{"type": "boolean"},
				"_explicit": map[string]any{"type": "boolean"},
			},
			"required": []string{"filter", "updates"},
		},
		Handler: handleBulkUpdate,
	}
}

func handleBulkUpdate(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
	if s.brain == nil {
		return nil, fmt.Errorf("brain service unavailable")
	}
	var payload struct {
		Filter  map[string]any `json:"filter"`
		Updates map[string]any `json:"updates"`
		DryRun  bool           `json:"dry_run"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("decode bulk_update args: %w", err)
	}
	req := types.BulkUpdateRequest{DryRun: payload.DryRun}
	if payload.Filter != nil {
		f := &types.BulkUpdateFilter{
			Tags: stringSliceArg(payload.Filter, "tags"),
		}
		project := firstNonEmptyString(stringArg(payload.Filter, "project"), defaultProject)
		if project != "" {
			f.Project = &project
		}
		if v := stringArg(payload.Filter, "type"); v != "" {
			f.Type = &v
		}
		if v := stringArg(payload.Filter, "status"); v != "" {
			f.Status = &v
		}
		if v := stringArg(payload.Filter, "feature_id"); v != "" {
			f.FeatureID = &v
		}
		if v := stringArg(payload.Filter, "priority"); v != "" {
			f.Priority = &v
		}
		req.Filter = f
	}
	if payload.Updates != nil {
		u := &types.UpdateEntryRequest{}
		if v, ok := payload.Updates["status"].(string); ok && v != "" {
			u.Status = &v
		}
		if v, ok := payload.Updates["priority"].(string); ok && v != "" {
			u.Priority = &v
		}
		if v := stringSliceArg(payload.Updates, "tags"); v != nil {
			u.Tags = v
		}
		if v, ok := payload.Updates["append"].(string); ok && v != "" {
			u.Append = &v
		}
		if v, ok := payload.Updates["note"].(string); ok && v != "" {
			u.Note = &v
		}
		req.Updates = u
	}
	return s.brain.BulkUpdate(ctx, req)
}

// ─── move_entry ───────────────────────────────────────────────────────

func moveEntryTool() ToolDefinition {
	return ToolDefinition{
		Name:        "move_entry",
		Description: "Move a brain entry to a different project. Requires _explicit=true.",
		Tier:        TierDestructive,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path_or_id":     map[string]any{"type": "string"},
				"target_project": map[string]any{"type": "string"},
				"_explicit":      map[string]any{"type": "boolean"},
			},
			"required": []string{"path_or_id", "target_project"},
		},
		Handler: func(ctx context.Context, s *AssistantService, _ string, raw json.RawMessage) (any, error) {
			if s.brain == nil {
				return nil, fmt.Errorf("brain service unavailable")
			}
			args := decodeArgs(raw)
			id, err := requiredString(args, "path_or_id")
			if err != nil {
				return nil, err
			}
			target, err := requiredString(args, "target_project")
			if err != nil {
				return nil, err
			}
			return s.brain.Move(ctx, id, target)
		},
	}
}

// ─── runner pause/resume ──────────────────────────────────────────────

func pauseProjectTool() ToolDefinition {
	return ToolDefinition{
		Name:        "pause_project",
		Description: "Pause the runner for one project (no new tasks will be claimed). Requires _explicit=true.",
		Tier:        TierDestructive,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project":   map[string]any{"type": "string"},
				"_explicit": map[string]any{"type": "boolean"},
			},
			"required": []string{"project"},
		},
		Handler: func(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
			if s.runner == nil {
				return nil, fmt.Errorf("runner service unavailable")
			}
			project := resolveProject(decodeArgs(raw), defaultProject)
			if project == "" {
				return nil, fmt.Errorf("project is required")
			}
			if err := s.runner.Pause(ctx, project); err != nil {
				return nil, err
			}
			return map[string]any{"paused": project}, nil
		},
	}
}

func resumeProjectTool() ToolDefinition {
	return ToolDefinition{
		Name:        "resume_project",
		Description: "Resume the runner for one project. Requires _explicit=true.",
		Tier:        TierDestructive,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project":   map[string]any{"type": "string"},
				"_explicit": map[string]any{"type": "boolean"},
			},
			"required": []string{"project"},
		},
		Handler: func(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
			if s.runner == nil {
				return nil, fmt.Errorf("runner service unavailable")
			}
			project := resolveProject(decodeArgs(raw), defaultProject)
			if project == "" {
				return nil, fmt.Errorf("project is required")
			}
			if err := s.runner.Resume(ctx, project); err != nil {
				return nil, err
			}
			return map[string]any{"resumed": project}, nil
		},
	}
}

func pauseAutomationsTool() ToolDefinition {
	return ToolDefinition{
		Name:        "pause_automations",
		Description: "Pause automation-generated task execution globally or for one project. Requires _explicit=true.",
		Tier:        TierDestructive,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project":   map[string]any{"type": "string", "description": "If set, only pause automations for this project"},
				"_explicit": map[string]any{"type": "boolean"},
			},
		},
		Handler: func(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
			if s.runner == nil {
				return nil, fmt.Errorf("runner service unavailable")
			}
			project := stringArg(decodeArgs(raw), "project")
			if project == "" {
				project = defaultProject
			}
			if project != "" {
				if err := s.runner.PauseProjectAutomations(ctx, project); err != nil {
					return nil, err
				}
				return map[string]any{"automations_paused": project}, nil
			}
			if err := s.runner.PauseAutomations(ctx); err != nil {
				return nil, err
			}
			return map[string]any{"automations_paused": "all"}, nil
		},
	}
}

func resumeAutomationsTool() ToolDefinition {
	return ToolDefinition{
		Name:        "resume_automations",
		Description: "Resume automation-generated task execution globally or for one project. Requires _explicit=true.",
		Tier:        TierDestructive,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project":   map[string]any{"type": "string"},
				"_explicit": map[string]any{"type": "boolean"},
			},
		},
		Handler: func(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
			if s.runner == nil {
				return nil, fmt.Errorf("runner service unavailable")
			}
			project := stringArg(decodeArgs(raw), "project")
			if project == "" {
				project = defaultProject
			}
			if project != "" {
				if err := s.runner.ResumeProjectAutomations(ctx, project); err != nil {
					return nil, err
				}
				return map[string]any{"automations_resumed": project}, nil
			}
			if err := s.runner.ResumeAutomations(ctx); err != nil {
				return nil, err
			}
			return map[string]any{"automations_resumed": "all"}, nil
		},
	}
}

// ─── feature assign / clear ───────────────────────────────────────────

func assignFeatureTool() ToolDefinition {
	return ToolDefinition{
		Name:        "assign_feature",
		Description: "Manually assign or reassign a feature group to a specific runner. Requires _explicit=true.",
		Tier:        TierDestructive,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project":    map[string]any{"type": "string"},
				"feature_id": map[string]any{"type": "string"},
				"runner_id":  map[string]any{"type": "string"},
				"intent":     map[string]any{"type": "string", "description": "Human-readable reason for the assignment"},
				"force":      map[string]any{"type": "boolean"},
				"_explicit":  map[string]any{"type": "boolean"},
			},
			"required": []string{"feature_id", "runner_id"},
		},
		Handler: func(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
			if s.tasks == nil {
				return nil, fmt.Errorf("task service unavailable")
			}
			args := decodeArgs(raw)
			project := resolveProject(args, defaultProject)
			if project == "" {
				return nil, fmt.Errorf("project is required")
			}
			featureID, err := requiredString(args, "feature_id")
			if err != nil {
				return nil, err
			}
			runnerID, err := requiredString(args, "runner_id")
			if err != nil {
				return nil, err
			}
			req := types.FeatureAssignmentRequest{
				RunnerID: runnerID,
				Intent:   stringArg(args, "intent"),
			}
			if b := boolPtrArg(args, "force"); b != nil {
				req.Force = *b
			}
			return s.tasks.AssignFeatureToRunner(ctx, project, featureID, req)
		},
	}
}

func clearFeatureAssignmentTool() ToolDefinition {
	return ToolDefinition{
		Name:        "clear_feature_assignment",
		Description: "Clear the runner assignment for a feature group. Requires _explicit=true.",
		Tier:        TierDestructive,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project":    map[string]any{"type": "string"},
				"feature_id": map[string]any{"type": "string"},
				"intent":     map[string]any{"type": "string"},
				"_explicit":  map[string]any{"type": "boolean"},
			},
			"required": []string{"feature_id"},
		},
		Handler: func(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
			if s.tasks == nil {
				return nil, fmt.Errorf("task service unavailable")
			}
			args := decodeArgs(raw)
			project := resolveProject(args, defaultProject)
			if project == "" {
				return nil, fmt.Errorf("project is required")
			}
			featureID, err := requiredString(args, "feature_id")
			if err != nil {
				return nil, err
			}
			req := types.ClearFeatureAssignmentRequest{Intent: stringArg(args, "intent")}
			return s.tasks.ClearFeatureAssignment(ctx, project, featureID, req)
		},
	}
}

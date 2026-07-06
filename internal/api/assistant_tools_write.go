package api

// Tier-2 (non-destructive write) tool handlers. See assistant_tools.go for
// tier semantics. All tools here auto-execute — they mutate state but do not
// delete or perform bulk changes.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/huynle/brain-api/internal/types"
)

// writeTools returns all TierWrite tool definitions.
func writeTools() []ToolDefinition {
	return []ToolDefinition{
		createEntryTool(),
		createTaskTool(),
		createAutomationTool(),
		createGoalTool(),
		updateEntryTool(),
		updateTaskTool(),
		updateAutomationTool(),
		verifyEntryTool(),
		linkEntryTool(),
		runGoalTool(),
		triggerTaskTool(),
		checkoutFeatureTool(),
	}
}

// ─── create_task / create_entry / create_automation ───────────────────

// createEntrySchema is shared across the three create-* tools since they use
// the same payload shape and differ only in the type field, which the tool
// sets implicitly.
func createEntrySchema(kind string) map[string]any {
	desc := "Content body (markdown supported)"
	titleHint := "Short descriptive title"
	if kind == "automation" {
		desc = "Content describing what the automation does"
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"project":               map[string]any{"type": "string"},
			"title":                 map[string]any{"type": "string", "description": titleHint},
			"content":               map[string]any{"type": "string", "description": desc},
			"status":                map[string]any{"type": "string"},
			"priority":              map[string]any{"type": "string", "enum": []string{"high", "medium", "low"}},
			"tags":                  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"feature_id":            map[string]any{"type": "string"},
			"user_original_request": map[string]any{"type": "string"},
			"direct_prompt":         map[string]any{"type": "string"},
			"agent":                 map[string]any{"type": "string"},
			"model":                 map[string]any{"type": "string"},
			"schedule":              map[string]any{"type": "string", "description": "Cron expression for scheduled runs"},
			"trigger":               map[string]any{"type": "object"},
			"action":                map[string]any{"type": "object"},
			"attachments":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required": []string{"title"},
	}
}

func createEntryTool() ToolDefinition {
	return ToolDefinition{
		Name:        "create_entry",
		Description: "Create a brain entry (note/report/plan/summary/etc.).",
		Tier:        TierWrite,
		Schema:      createEntrySchema("entry"),
		Handler:     makeCreateHandler("entry"),
	}
}

func createTaskTool() ToolDefinition {
	return ToolDefinition{
		Name:        "create_task",
		Description: "Create a task in the task queue. Include user_original_request when the model is faithfully capturing the user's ask.",
		Tier:        TierWrite,
		Schema:      createEntrySchema("task"),
		Handler:     makeCreateHandler("task"),
	}
}

func createAutomationTool() ToolDefinition {
	return ToolDefinition{
		Name:        "create_automation",
		Description: "Create a cron/event/webhook automation. Include trigger and action objects when the user describes timing/event behavior.",
		Tier:        TierWrite,
		Schema:      createEntrySchema("automation"),
		Handler:     makeCreateHandler("automation"),
	}
}

func makeCreateHandler(entryType string) func(context.Context, *AssistantService, string, json.RawMessage) (any, error) {
	return func(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
		if s.brain == nil {
			return nil, fmt.Errorf("brain service unavailable")
		}
		payload := decodeArgs(raw)
		if payload["title"] == nil {
			return nil, fmt.Errorf("title is required")
		}
		// Reuse the legacy shape by hand-rolling the request from the payload
		// so we keep behavior consistent with the pre-agent planner.
		action := AssistantAction{Type: "create_" + entryType, Explicit: true, Payload: payload}
		req := createEntryRequestFromAction(defaultProject, action)
		req.Type = entryType
		return s.brain.Save(ctx, req)
	}
}

func createGoalTool() ToolDefinition {
	return ToolDefinition{
		Name:        "create_goal",
		Description: "Create a goal automation with criteria and validation that will spawn follow-up work until satisfied.",
		Tier:        TierWrite,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project":           map[string]any{"type": "string"},
				"feature_id":        map[string]any{"type": "string"},
				"title":             map[string]any{"type": "string"},
				"criteria":          map[string]any{"type": "string"},
				"validation":        map[string]any{"type": "string"},
				"workdir":           map[string]any{"type": "string"},
				"trigger_source":    map[string]any{"type": "string"},
				"agent":             map[string]any{"type": "string"},
				"model":             map[string]any{"type": "string"},
				"complete_statuses": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"blocked_statuses":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"content":           map[string]any{"type": "string"},
				"direct_prompt":     map[string]any{"type": "string"},
			},
			"required": []string{"title"},
		},
		Handler: func(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
			if s.goals == nil {
				return nil, fmt.Errorf("goal service unavailable")
			}
			payload := decodeArgs(raw)
			if payload["title"] == nil {
				return nil, fmt.Errorf("title is required")
			}
			action := AssistantAction{Type: "create_goal", Explicit: true, Payload: payload}
			req := createGoalRequestFromAction(defaultProject, action)
			return s.goals.CreateGoal(ctx, req)
		},
	}
}

// ─── update_entry / update_task / update_automation ───────────────────

// updateEntrySchema builds the shared schema for the update-* tools. All
// fields are optional pointer-ish — the model only sets what it wants to
// change.
func updateEntrySchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path_or_id":       map[string]any{"type": "string"},
			"status":           map[string]any{"type": "string"},
			"title":            map[string]any{"type": "string"},
			"content":          map[string]any{"type": "string"},
			"append":           map[string]any{"type": "string", "description": "Text to append to the entry body"},
			"note":             map[string]any{"type": "string", "description": "Short one-line note to record"},
			"tags":             map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"priority":         map[string]any{"type": "string"},
			"depends_on":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"schedule":         map[string]any{"type": "string"},
			"schedule_enabled": map[string]any{"type": "boolean"},
			"agent":            map[string]any{"type": "string"},
			"model":            map[string]any{"type": "string"},
			"executor":         map[string]any{"type": "string"},
			"direct_prompt":    map[string]any{"type": "string"},
			"trigger":          map[string]any{"type": "object"},
			"action":           map[string]any{"type": "object"},
		},
		"required": []string{"path_or_id"},
	}
}

func updateEntryTool() ToolDefinition {
	return ToolDefinition{
		Name:        "update_entry",
		Description: "Update an existing brain entry's status, tags, content, note, priority, dependencies, schedule, or trigger. Only specified fields are changed.",
		Tier:        TierWrite,
		Schema:      updateEntrySchema(),
		Handler:     handleUpdateEntry,
	}
}

func updateTaskTool() ToolDefinition {
	return ToolDefinition{
		Name:        "update_task",
		Description: "Update a task entry. Same fields as update_entry — thin wrapper for clarity.",
		Tier:        TierWrite,
		Schema:      updateEntrySchema(),
		Handler:     handleUpdateEntry,
	}
}

func updateAutomationTool() ToolDefinition {
	return ToolDefinition{
		Name:        "update_automation",
		Description: "Update an automation entry (change schedule, trigger, action, or disable via schedule_enabled=false).",
		Tier:        TierWrite,
		Schema:      updateEntrySchema(),
		Handler:     handleUpdateEntry,
	}
}

func handleUpdateEntry(ctx context.Context, s *AssistantService, _ string, raw json.RawMessage) (any, error) {
	if s.brain == nil {
		return nil, fmt.Errorf("brain service unavailable")
	}
	args := decodeArgs(raw)
	id, err := requiredString(args, "path_or_id")
	if err != nil {
		return nil, err
	}
	req := types.UpdateEntryRequest{}
	if v, ok := args["status"].(string); ok && v != "" {
		req.Status = &v
	}
	if v, ok := args["title"].(string); ok && v != "" {
		req.Title = &v
	}
	if v, ok := args["content"].(string); ok {
		req.Content = &v
	}
	if v, ok := args["append"].(string); ok && v != "" {
		req.Append = &v
	}
	if v, ok := args["note"].(string); ok && v != "" {
		req.Note = &v
	}
	if v := stringSliceArg(args, "tags"); v != nil {
		req.Tags = v
	}
	if v, ok := args["priority"].(string); ok && v != "" {
		req.Priority = &v
	}
	if v := stringSliceArg(args, "depends_on"); v != nil {
		req.DependsOn = &v
	}
	if v, ok := args["schedule"].(string); ok && v != "" {
		req.Schedule = &v
	}
	if v, ok := args["schedule_enabled"].(bool); ok {
		req.ScheduleEnabled = &v
	}
	if v, ok := args["agent"].(string); ok && v != "" {
		req.Agent = &v
	}
	if v, ok := args["model"].(string); ok && v != "" {
		req.Model = &v
	}
	if v, ok := args["executor"].(string); ok && v != "" {
		req.Executor = &v
	}
	if v, ok := args["direct_prompt"].(string); ok && v != "" {
		req.DirectPrompt = &v
	}
	if trig := triggerFromPayload(args); trig != nil {
		req.Trigger = trig
	}
	if act := actionFromPayload(args); act != nil {
		req.Action = act
	}
	return s.brain.Update(ctx, id, req)
}

// ─── verify_entry ─────────────────────────────────────────────────────

func verifyEntryTool() ToolDefinition {
	return ToolDefinition{
		Name:        "verify_entry",
		Description: "Mark a brain entry as verified (up-to-date). Records the current time as last_verified.",
		Tier:        TierWrite,
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
			"required":   []string{"path"},
		},
		Handler: func(ctx context.Context, s *AssistantService, _ string, raw json.RawMessage) (any, error) {
			if s.brain == nil {
				return nil, fmt.Errorf("brain service unavailable")
			}
			args := decodeArgs(raw)
			path, err := requiredString(args, "path")
			if err != nil {
				return nil, err
			}
			return s.brain.Verify(ctx, path)
		},
	}
}

// ─── link_entry ───────────────────────────────────────────────────────

func linkEntryTool() ToolDefinition {
	return ToolDefinition{
		Name:        "link_entry",
		Description: "Generate a markdown link to a brain entry.",
		Tier:        TierWrite,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":       map[string]any{"type": "string"},
				"title":      map[string]any{"type": "string"},
				"with_title": map[string]any{"type": "boolean"},
			},
			"required": []string{"path"},
		},
		Handler: func(ctx context.Context, s *AssistantService, _ string, raw json.RawMessage) (any, error) {
			if s.brain == nil {
				return nil, fmt.Errorf("brain service unavailable")
			}
			args := decodeArgs(raw)
			path, err := requiredString(args, "path")
			if err != nil {
				return nil, err
			}
			req := types.LinkRequest{
				Path:      path,
				Title:     stringArg(args, "title"),
				WithTitle: boolPtrArg(args, "with_title"),
			}
			return s.brain.GenerateLink(ctx, req)
		},
	}
}

// ─── run_goal ─────────────────────────────────────────────────────────

func runGoalTool() ToolDefinition {
	return ToolDefinition{
		Name:        "run_goal",
		Description: "Manually trigger a reconcile pass for a goal automation.",
		Tier:        TierWrite,
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"goal_id": map[string]any{"type": "string"}},
			"required":   []string{"goal_id"},
		},
		Handler: func(ctx context.Context, s *AssistantService, _ string, raw json.RawMessage) (any, error) {
			if s.goals == nil {
				return nil, fmt.Errorf("goal service unavailable")
			}
			args := decodeArgs(raw)
			goalID, err := requiredString(args, "goal_id")
			if err != nil {
				return nil, err
			}
			return s.goals.RunGoal(ctx, goalID)
		},
	}
}

// ─── trigger_task ─────────────────────────────────────────────────────

func triggerTaskTool() ToolDefinition {
	return ToolDefinition{
		Name:        "trigger_task",
		Description: "Manually trigger a scheduled task now (runs the automation immediately without waiting for the next scheduled run).",
		Tier:        TierWrite,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project": map[string]any{"type": "string"},
				"task_id": map[string]any{"type": "string"},
			},
			"required": []string{"task_id"},
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
			taskID, err := requiredString(args, "task_id")
			if err != nil {
				return nil, err
			}
			return s.tasks.TriggerTask(ctx, project, taskID)
		},
	}
}

// ─── checkout_feature ─────────────────────────────────────────────────

func checkoutFeatureTool() ToolDefinition {
	return ToolDefinition{
		Name:        "checkout_feature",
		Description: "Create a feature checkout task for review and merge orchestration.",
		Tier:        TierWrite,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project":              map[string]any{"type": "string"},
				"feature_id":           map[string]any{"type": "string"},
				"execution_branch":     map[string]any{"type": "string"},
				"merge_target_branch":  map[string]any{"type": "string"},
				"merge_policy":         map[string]any{"type": "string", "enum": []string{"prompt_only", "auto_pr", "auto_merge"}},
				"merge_strategy":       map[string]any{"type": "string", "enum": []string{"squash", "merge", "rebase"}},
				"remote_branch_policy": map[string]any{"type": "string", "enum": []string{"keep", "delete"}},
				"open_pr_before_merge": map[string]any{"type": "boolean"},
				"execution_mode":       map[string]any{"type": "string", "enum": []string{"worktree", "current_branch"}},
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
			opts := &types.FeatureCheckoutOptions{
				ExecutionBranch:    stringArg(args, "execution_branch"),
				MergeTargetBranch:  stringArg(args, "merge_target_branch"),
				MergePolicy:        stringArg(args, "merge_policy"),
				MergeStrategy:      stringArg(args, "merge_strategy"),
				RemoteBranchPolicy: stringArg(args, "remote_branch_policy"),
				ExecutionMode:      stringArg(args, "execution_mode"),
			}
			if b := boolPtrArg(args, "open_pr_before_merge"); b != nil {
				opts.OpenPRBeforeMerge = *b
			}
			return s.tasks.CheckoutFeature(ctx, project, featureID, opts)
		},
	}
}

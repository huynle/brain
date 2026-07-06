package api

// Read-only tool handlers for the agentic assistant. See assistant_tools.go
// for the ToolDefinition contract and tier semantics. Every tool here is
// TierRead and safe to auto-execute.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/huynle/brain-api/internal/types"
)

// readEntriesCap is the max number of entries any list/search read returns
// back to the model in one call. Well above the practical query size but
// bounded to protect the context window.
const readEntriesCap = 50

// readTools returns all TierRead tool definitions.
func readTools() []ToolDefinition {
	return []ToolDefinition{
		listEntriesTool(),
		getEntryTool(),
		searchBrainTool(),
		listTasksTool(),
		getTaskTool(),
		getTaskMetadataTool(),
		listFeaturesTool(),
		getFeatureTool(),
		listAutomationsTool(),
		listGoalsTool(),
		goalProgressTool(),
		listRunnersTool(),
		runnerStatusTool(),
		getStatsTool(),
		getBacklinksTool(),
		getOutlinksTool(),
		getRelatedTool(),
		getSectionsTool(),
		getSectionTool(),
		recentEventsTool(),
	}
}

// ─── list_entries ─────────────────────────────────────────────────────

func listEntriesTool() ToolDefinition {
	return ToolDefinition{
		Name:        "list_entries",
		Description: "List brain entries with optional filters. Use type=automation for automations, type=task for tasks, etc. Defaults to the active project unless global=true.",
		Tier:        TierRead,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project":    map[string]any{"type": "string", "description": "Project ID; defaults to the active project"},
				"type":       map[string]any{"type": "string", "description": "Filter by entry type (task, automation, plan, summary, etc.)"},
				"status":     map[string]any{"type": "string"},
				"feature_id": map[string]any{"type": "string"},
				"tags":       map[string]any{"type": "string", "description": "Comma-separated tag list"},
				"priority":   map[string]any{"type": "string"},
				"limit":      map[string]any{"type": "integer", "description": "Max 50"},
				"offset":     map[string]any{"type": "integer"},
				"global":     map[string]any{"type": "boolean", "description": "Search only global entries"},
				"sort_by":    map[string]any{"type": "string", "enum": []string{"created", "modified", "priority", "completed"}},
			},
		},
		Handler: handleListEntries,
	}
}

func handleListEntries(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
	if s.brain == nil {
		return nil, fmt.Errorf("brain service unavailable")
	}
	args := decodeArgs(raw)
	req := types.ListEntriesRequest{
		Project:   resolveProject(args, defaultProject),
		Type:      stringArg(args, "type"),
		Status:    stringArg(args, "status"),
		FeatureID: stringArg(args, "feature_id"),
		Tags:      stringArg(args, "tags"),
		Priority:  stringArg(args, "priority"),
		Limit:     intArg(args, "limit"),
		Offset:    intArg(args, "offset"),
		Global:    boolPtrArg(args, "global"),
		SortBy:    stringArg(args, "sort_by"),
	}
	if req.Limit <= 0 || req.Limit > readEntriesCap {
		req.Limit = readEntriesCap
	}
	resp, err := s.brain.List(ctx, req)
	if err != nil {
		return nil, err
	}
	entries, truncated := truncateEntries(resp.Entries, readEntriesCap)
	out := map[string]any{
		"total":   resp.Total,
		"limit":   resp.Limit,
		"offset":  resp.Offset,
		"count":   len(entries),
		"entries": mapEntries(entries),
	}
	if truncated {
		out["truncated"] = true
	}
	return out, nil
}

func mapEntries(in []types.BrainEntry) []map[string]any {
	out := make([]map[string]any, len(in))
	for i, e := range in {
		out[i] = summarizeEntry(e)
	}
	return out
}

// ─── get_entry ────────────────────────────────────────────────────────

func getEntryTool() ToolDefinition {
	return ToolDefinition{
		Name:        "get_entry",
		Description: "Fetch a single brain entry by path or 8-char ID. Set include_content=true for the full markdown body.",
		Tier:        TierRead,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path_or_id":      map[string]any{"type": "string"},
				"include_content": map[string]any{"type": "boolean", "description": "Include full markdown body (default false)"},
			},
			"required": []string{"path_or_id"},
		},
		Handler: handleGetEntry,
	}
}

func handleGetEntry(ctx context.Context, s *AssistantService, _ string, raw json.RawMessage) (any, error) {
	if s.brain == nil {
		return nil, fmt.Errorf("brain service unavailable")
	}
	args := decodeArgs(raw)
	id, err := requiredString(args, "path_or_id")
	if err != nil {
		return nil, err
	}
	includeContent := false
	if b := boolPtrArg(args, "include_content"); b != nil {
		includeContent = *b
	}
	entry, err := s.brain.Recall(ctx, id)
	if err != nil {
		return nil, err
	}
	summary := summarizeEntry(*entry)
	if includeContent && entry.Content != "" {
		// Cap content to avoid dumping huge files into the model's context.
		if len(entry.Content) > 8000 {
			summary["content"] = entry.Content[:8000]
			summary["content_truncated"] = true
		} else {
			summary["content"] = entry.Content
		}
	}
	if len(entry.DependsOn) > 0 {
		summary["depends_on"] = entry.DependsOn
	}
	if entry.Schedule != "" {
		summary["schedule"] = entry.Schedule
	}
	if entry.NextRun != "" {
		summary["next_run"] = entry.NextRun
	}
	if entry.Agent != "" {
		summary["agent"] = entry.Agent
	}
	if entry.Model != "" {
		summary["model"] = entry.Model
	}
	if entry.Executor != "" {
		summary["executor"] = entry.Executor
	}
	if entry.UserOriginalRequest != "" {
		summary["user_original_request"] = entry.UserOriginalRequest
	}
	return summary, nil
}

// ─── search_brain ─────────────────────────────────────────────────────

func searchBrainTool() ToolDefinition {
	return ToolDefinition{
		Name:        "search_brain",
		Description: "Full-text or semantic search across brain entries. Strategies: fts (default), exact, like, semantic, hybrid.",
		Tier:        TierRead,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"query":      map[string]any{"type": "string"},
				"project":    map[string]any{"type": "string"},
				"type":       map[string]any{"type": "string"},
				"status":     map[string]any{"type": "string"},
				"feature_id": map[string]any{"type": "string"},
				"strategy":   map[string]any{"type": "string", "enum": []string{"fts", "exact", "like", "semantic", "hybrid"}},
				"limit":      map[string]any{"type": "integer"},
				"tags":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
				"global":     map[string]any{"type": "boolean"},
			},
			"required": []string{"query"},
		},
		Handler: handleSearchBrain,
	}
}

func handleSearchBrain(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
	if s.brain == nil {
		return nil, fmt.Errorf("brain service unavailable")
	}
	args := decodeArgs(raw)
	query, err := requiredString(args, "query")
	if err != nil {
		return nil, err
	}
	limit := intArg(args, "limit")
	if limit <= 0 || limit > readEntriesCap {
		limit = readEntriesCap
	}
	req := types.SearchRequest{
		Query:     query,
		Project:   resolveProject(args, defaultProject),
		Type:      stringArg(args, "type"),
		Status:    stringArg(args, "status"),
		FeatureID: stringArg(args, "feature_id"),
		Strategy:  stringArg(args, "strategy"),
		Tags:      stringSliceArg(args, "tags"),
		Global:    boolPtrArg(args, "global"),
		Limit:     &limit,
	}
	resp, err := s.brain.Search(ctx, req)
	if err != nil {
		return nil, err
	}
	out := map[string]any{
		"total":   resp.Total,
		"count":   len(resp.Results),
		"results": resp.Results,
	}
	return out, nil
}

// ─── list_tasks ───────────────────────────────────────────────────────

func listTasksTool() ToolDefinition {
	return ToolDefinition{
		Name:        "list_tasks",
		Description: "List tasks for a project with dependency resolution (ready/waiting/blocked). Optionally filter by classification or status.",
		Tier:        TierRead,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project":        map[string]any{"type": "string"},
				"classification": map[string]any{"type": "string", "enum": []string{"ready", "waiting", "blocked"}},
				"status":         map[string]any{"type": "string"},
				"feature_id":     map[string]any{"type": "string"},
				"limit":          map[string]any{"type": "integer"},
			},
		},
		Handler: handleListTasks,
	}
}

func handleListTasks(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
	if s.tasks == nil {
		return nil, fmt.Errorf("task service unavailable")
	}
	args := decodeArgs(raw)
	project := resolveProject(args, defaultProject)
	if project == "" {
		return nil, fmt.Errorf("project is required (either arg or active project)")
	}
	resp, err := s.tasks.GetTasks(ctx, project)
	if err != nil {
		return nil, err
	}
	classification := stringArg(args, "classification")
	status := stringArg(args, "status")
	featureID := stringArg(args, "feature_id")
	limit := intArg(args, "limit")
	if limit <= 0 || limit > readEntriesCap {
		limit = readEntriesCap
	}
	filtered := make([]types.ResolvedTask, 0, len(resp.Tasks))
	for _, t := range resp.Tasks {
		if classification != "" && t.Classification != classification {
			continue
		}
		if status != "" && t.Status != status {
			continue
		}
		if featureID != "" && t.FeatureID != featureID {
			continue
		}
		filtered = append(filtered, t)
	}
	trimmed, truncated := truncateResolvedTasks(filtered, limit)
	tasks := make([]map[string]any, len(trimmed))
	for i, t := range trimmed {
		tasks[i] = summarizeTask(t)
	}
	out := map[string]any{
		"project":   project,
		"count":     len(tasks),
		"total":     len(filtered),
		"tasks":     tasks,
		"has_stats": resp.Stats != nil,
	}
	if resp.Stats != nil {
		out["stats"] = resp.Stats
	}
	if len(resp.Cycles) > 0 {
		out["cycles"] = resp.Cycles
	}
	if truncated {
		out["truncated"] = true
	}
	return out, nil
}

// ─── get_task ─────────────────────────────────────────────────────────

func getTaskTool() ToolDefinition {
	return ToolDefinition{
		Name:        "get_task",
		Description: "Fetch a single task by ID with full dependency resolution. Use this to diagnose why a task is pending/blocked/waiting.",
		Tier:        TierRead,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project": map[string]any{"type": "string"},
				"task_id": map[string]any{"type": "string"},
			},
			"required": []string{"task_id"},
		},
		Handler: handleGetTask,
	}
}

func handleGetTask(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
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
	task, err := s.tasks.GetTask(ctx, project, taskID)
	if err != nil {
		return nil, err
	}
	// Return the full ResolvedTask — the model needs blocked_by, waiting_on,
	// resolved_workdir, etc. to diagnose "why stuck".
	return task, nil
}

// ─── get_task_metadata ────────────────────────────────────────────────

func getTaskMetadataTool() ToolDefinition {
	return ToolDefinition{
		Name:        "get_task_metadata",
		Description: "Fetch the execution metadata for a task: dispatch lease, placement reasons, claim status, dependency resolution details. Use this when a task is stuck to see if it's being dispatched, claimed, or blocked by scheduling.",
		Tier:        TierRead,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project": map[string]any{"type": "string"},
				"task_id": map[string]any{"type": "string"},
			},
			"required": []string{"task_id"},
		},
		Handler: handleGetTaskMetadata,
	}
}

func handleGetTaskMetadata(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
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
	task, err := s.tasks.GetTask(ctx, project, taskID)
	if err != nil {
		return nil, err
	}
	claim, _ := s.tasks.GetClaimStatus(ctx, project, taskID)
	out := map[string]any{
		"task_id":               task.ID,
		"status":                task.Status,
		"classification":        task.Classification,
		"depends_on":            task.DependsOn,
		"resolved_deps":         task.ResolvedDeps,
		"unresolved_deps":       task.UnresolvedDeps,
		"blocked_by":            task.BlockedBy,
		"blocked_by_reason":     task.BlockedByReason,
		"waiting_on":            task.WaitingOn,
		"in_cycle":              task.InCycle,
		"resolved_workdir":      task.ResolvedWorkdir,
		"executor":              task.Executor,
		"agent":                 task.Agent,
		"model":                 task.Model,
		"schedule":              task.Schedule,
		"next_run":              task.NextRun,
		"schedule_enabled":      task.ScheduleEnabled,
		"trigger":               task.Trigger,
		"placement_reasons":     task.PlacementReasons,
		"last_placement_reason": task.LastPlacementReason,
		"dispatch_lease":        task.DispatchLease,
	}
	if claim != nil {
		out["claim"] = claim
	}
	return out, nil
}

// ─── list_features ────────────────────────────────────────────────────

func listFeaturesTool() ToolDefinition {
	return ToolDefinition{
		Name:        "list_features",
		Description: "List feature groups for a project. Set ready_only=true to only see features whose dependencies are met.",
		Tier:        TierRead,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project":    map[string]any{"type": "string"},
				"ready_only": map[string]any{"type": "boolean"},
			},
		},
		Handler: handleListFeatures,
	}
}

func handleListFeatures(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
	if s.tasks == nil {
		return nil, fmt.Errorf("task service unavailable")
	}
	args := decodeArgs(raw)
	project := resolveProject(args, defaultProject)
	if project == "" {
		return nil, fmt.Errorf("project is required")
	}
	readyOnly := false
	if b := boolPtrArg(args, "ready_only"); b != nil {
		readyOnly = *b
	}
	var resp *types.FeatureListResponse
	var err error
	if readyOnly {
		resp, err = s.tasks.GetReadyFeatures(ctx, project)
	} else {
		resp, err = s.tasks.GetFeatures(ctx, project)
	}
	if err != nil {
		return nil, err
	}
	features := make([]map[string]any, len(resp.Features))
	for i, f := range resp.Features {
		features[i] = map[string]any{
			"feature_id": f.FeatureID,
			"ready":      f.Ready,
			"task_count": len(f.Tasks),
			"stats":      f.Stats,
		}
	}
	return map[string]any{"project": project, "count": len(features), "features": features}, nil
}

// ─── get_feature ──────────────────────────────────────────────────────

func getFeatureTool() ToolDefinition {
	return ToolDefinition{
		Name:        "get_feature",
		Description: "Fetch a single feature group with all its tasks.",
		Tier:        TierRead,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project":    map[string]any{"type": "string"},
				"feature_id": map[string]any{"type": "string"},
			},
			"required": []string{"feature_id"},
		},
		Handler: handleGetFeature,
	}
}

func handleGetFeature(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
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
	resp, err := s.tasks.GetFeature(ctx, project, featureID)
	if err != nil {
		return nil, err
	}
	tasks := make([]map[string]any, len(resp.Feature.Tasks))
	for i, t := range resp.Feature.Tasks {
		tasks[i] = summarizeTask(t)
	}
	return map[string]any{
		"feature_id": resp.Feature.FeatureID,
		"ready":      resp.Feature.Ready,
		"stats":      resp.Feature.Stats,
		"task_count": len(tasks),
		"tasks":      tasks,
	}, nil
}

// ─── list_automations ─────────────────────────────────────────────────

func listAutomationsTool() ToolDefinition {
	return ToolDefinition{
		Name:        "list_automations",
		Description: "List automation entries (type=automation) for the active project. This is a convenience wrapper around list_entries.",
		Tier:        TierRead,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project": map[string]any{"type": "string"},
				"status":  map[string]any{"type": "string"},
				"limit":   map[string]any{"type": "integer"},
			},
		},
		Handler: handleListAutomations,
	}
}

func handleListAutomations(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
	if s.brain == nil {
		return nil, fmt.Errorf("brain service unavailable")
	}
	args := decodeArgs(raw)
	limit := intArg(args, "limit")
	if limit <= 0 || limit > readEntriesCap {
		limit = readEntriesCap
	}
	req := types.ListEntriesRequest{
		Project: resolveProject(args, defaultProject),
		Type:    "automation",
		Status:  stringArg(args, "status"),
		Limit:   limit,
	}
	resp, err := s.brain.List(ctx, req)
	if err != nil {
		return nil, err
	}
	entries, truncated := truncateEntries(resp.Entries, readEntriesCap)
	rows := make([]map[string]any, len(entries))
	for i, e := range entries {
		row := summarizeEntry(e)
		row["schedule"] = e.Schedule
		row["schedule_enabled"] = e.ScheduleEnabled
		row["next_run"] = e.NextRun
		rows[i] = row
	}
	out := map[string]any{
		"project":     req.Project,
		"total":       resp.Total,
		"count":       len(rows),
		"automations": rows,
	}
	if truncated {
		out["truncated"] = true
	}
	return out, nil
}

// ─── list_goals ───────────────────────────────────────────────────────

func listGoalsTool() ToolDefinition {
	return ToolDefinition{
		Name:        "list_goals",
		Description: "List goal automations. Optionally filter by feature.",
		Tier:        TierRead,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"project":    map[string]any{"type": "string"},
				"feature_id": map[string]any{"type": "string"},
			},
		},
		Handler: handleListGoals,
	}
}

func handleListGoals(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
	if s.goals == nil {
		return nil, fmt.Errorf("goal service unavailable")
	}
	args := decodeArgs(raw)
	project := resolveProject(args, defaultProject)
	goals, err := s.goals.ListGoals(ctx, project, stringArg(args, "feature_id"))
	if err != nil {
		return nil, err
	}
	return map[string]any{"count": len(goals), "goals": goals}, nil
}

// ─── goal_progress ────────────────────────────────────────────────────

func goalProgressTool() ToolDefinition {
	return ToolDefinition{
		Name:        "goal_progress",
		Description: "Return goal-scoped linked-task progress for a goal ID.",
		Tier:        TierRead,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"goal_id": map[string]any{"type": "string"},
			},
			"required": []string{"goal_id"},
		},
		Handler: handleGoalProgress,
	}
}

func handleGoalProgress(ctx context.Context, s *AssistantService, _ string, raw json.RawMessage) (any, error) {
	if s.goals == nil {
		return nil, fmt.Errorf("goal service unavailable")
	}
	args := decodeArgs(raw)
	goalID, err := requiredString(args, "goal_id")
	if err != nil {
		return nil, err
	}
	return s.goals.GoalProgress(ctx, goalID)
}

// ─── list_runners ─────────────────────────────────────────────────────

func listRunnersTool() ToolDefinition {
	return ToolDefinition{
		Name:        "list_runners",
		Description: "List all registered runners with computed status (online/stale/offline).",
		Tier:        TierRead,
		Schema:      map[string]any{"type": "object", "properties": map[string]any{}},
		Handler:     handleListRunners,
	}
}

func handleListRunners(ctx context.Context, s *AssistantService, _ string, _ json.RawMessage) (any, error) {
	if s.runners == nil {
		return nil, fmt.Errorf("runner registry service unavailable")
	}
	return s.runners.ListRunners(ctx)
}

// ─── runner_status ────────────────────────────────────────────────────

func runnerStatusTool() ToolDefinition {
	return ToolDefinition{
		Name:        "runner_status",
		Description: "Show global runner pause/running status: which projects are paused, and whether automation-generated tasks are paused.",
		Tier:        TierRead,
		Schema:      map[string]any{"type": "object", "properties": map[string]any{}},
		Handler:     handleRunnerStatus,
	}
}

func handleRunnerStatus(ctx context.Context, s *AssistantService, _ string, _ json.RawMessage) (any, error) {
	if s.runner == nil {
		return nil, fmt.Errorf("runner service unavailable")
	}
	return s.runner.GetStatus(ctx)
}

// ─── get_stats ────────────────────────────────────────────────────────

func getStatsTool() ToolDefinition {
	return ToolDefinition{
		Name:        "get_stats",
		Description: "Return brain storage statistics (entry counts by type, etc.). Set global=true for the global brain, or pass project to scope to a single project.",
		Tier:        TierRead,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"global":  map[string]any{"type": "boolean"},
				"project": map[string]any{"type": "string"},
			},
		},
		Handler: handleGetStats,
	}
}

func handleGetStats(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
	if s.brain == nil {
		return nil, fmt.Errorf("brain service unavailable")
	}
	args := decodeArgs(raw)
	globalFlag := false
	if b := boolPtrArg(args, "global"); b != nil {
		globalFlag = *b
	}
	project := resolveProject(args, defaultProject)
	if globalFlag {
		// global overrides project scoping
		project = ""
	}
	return s.brain.GetStats(ctx, globalFlag, project)
}

// ─── get_backlinks ────────────────────────────────────────────────────

func getBacklinksTool() ToolDefinition {
	return ToolDefinition{
		Name:        "get_backlinks",
		Description: "Return entries that link TO the given entry.",
		Tier:        TierRead,
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
			"required":   []string{"path"},
		},
		Handler: handleBacklinks,
	}
}

func handleBacklinks(ctx context.Context, s *AssistantService, _ string, raw json.RawMessage) (any, error) {
	if s.brain == nil {
		return nil, fmt.Errorf("brain service unavailable")
	}
	args := decodeArgs(raw)
	path, err := requiredString(args, "path")
	if err != nil {
		return nil, err
	}
	entries, err := s.brain.GetBacklinks(ctx, path)
	if err != nil {
		return nil, err
	}
	trimmed, truncated := truncateEntries(entries, readEntriesCap)
	out := map[string]any{"count": len(trimmed), "entries": mapEntries(trimmed)}
	if truncated {
		out["truncated"] = true
	}
	return out, nil
}

// ─── get_outlinks ─────────────────────────────────────────────────────

func getOutlinksTool() ToolDefinition {
	return ToolDefinition{
		Name:        "get_outlinks",
		Description: "Return entries that the given entry links TO.",
		Tier:        TierRead,
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
			"required":   []string{"path"},
		},
		Handler: handleOutlinks,
	}
}

func handleOutlinks(ctx context.Context, s *AssistantService, _ string, raw json.RawMessage) (any, error) {
	if s.brain == nil {
		return nil, fmt.Errorf("brain service unavailable")
	}
	args := decodeArgs(raw)
	path, err := requiredString(args, "path")
	if err != nil {
		return nil, err
	}
	entries, err := s.brain.GetOutlinks(ctx, path)
	if err != nil {
		return nil, err
	}
	trimmed, truncated := truncateEntries(entries, readEntriesCap)
	out := map[string]any{"count": len(trimmed), "entries": mapEntries(trimmed)}
	if truncated {
		out["truncated"] = true
	}
	return out, nil
}

// ─── get_related ──────────────────────────────────────────────────────

func getRelatedTool() ToolDefinition {
	return ToolDefinition{
		Name:        "get_related",
		Description: "Return entries related to the given entry via co-citation.",
		Tier:        TierRead,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":  map[string]any{"type": "string"},
				"limit": map[string]any{"type": "integer"},
			},
			"required": []string{"path"},
		},
		Handler: handleRelated,
	}
}

func handleRelated(ctx context.Context, s *AssistantService, _ string, raw json.RawMessage) (any, error) {
	if s.brain == nil {
		return nil, fmt.Errorf("brain service unavailable")
	}
	args := decodeArgs(raw)
	path, err := requiredString(args, "path")
	if err != nil {
		return nil, err
	}
	limit := intArg(args, "limit")
	if limit <= 0 || limit > readEntriesCap {
		limit = 10
	}
	entries, err := s.brain.GetRelated(ctx, path, limit)
	if err != nil {
		return nil, err
	}
	return map[string]any{"count": len(entries), "entries": mapEntries(entries)}, nil
}

// ─── get_sections / get_section ───────────────────────────────────────

func getSectionsTool() ToolDefinition {
	return ToolDefinition{
		Name:        "get_sections",
		Description: "List markdown section headers within an entry.",
		Tier:        TierRead,
		Schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"path": map[string]any{"type": "string"}},
			"required":   []string{"path"},
		},
		Handler: handleGetSections,
	}
}

func handleGetSections(ctx context.Context, s *AssistantService, _ string, raw json.RawMessage) (any, error) {
	if s.brain == nil {
		return nil, fmt.Errorf("brain service unavailable")
	}
	args := decodeArgs(raw)
	path, err := requiredString(args, "path")
	if err != nil {
		return nil, err
	}
	return s.brain.GetSections(ctx, path)
}

func getSectionTool() ToolDefinition {
	return ToolDefinition{
		Name:        "get_section",
		Description: "Return the full content of one section by title from an entry.",
		Tier:        TierRead,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":                map[string]any{"type": "string"},
				"title":               map[string]any{"type": "string"},
				"include_subsections": map[string]any{"type": "boolean"},
			},
			"required": []string{"path", "title"},
		},
		Handler: handleGetSection,
	}
}

func handleGetSection(ctx context.Context, s *AssistantService, _ string, raw json.RawMessage) (any, error) {
	if s.brain == nil {
		return nil, fmt.Errorf("brain service unavailable")
	}
	args := decodeArgs(raw)
	path, err := requiredString(args, "path")
	if err != nil {
		return nil, err
	}
	title, err := requiredString(args, "title")
	if err != nil {
		return nil, err
	}
	includeSub := true
	if b := boolPtrArg(args, "include_subsections"); b != nil {
		includeSub = *b
	}
	return s.brain.GetSection(ctx, path, title, includeSub)
}

// ─── recent_events ────────────────────────────────────────────────────

func recentEventsTool() ToolDefinition {
	return ToolDefinition{
		Name:        "recent_events",
		Description: "Return recent system events (task lifecycle, automation runs, feature transitions). Filter by project_id, type (glob supported like 'task.*'), or source ('runner'/'api').",
		Tier:        TierRead,
		Schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"limit":      map[string]any{"type": "integer"},
				"project_id": map[string]any{"type": "string"},
				"type":       map[string]any{"type": "string"},
				"source":     map[string]any{"type": "string"},
				"feature_id": map[string]any{"type": "string"},
			},
		},
		Handler: handleRecentEvents,
	}
}

func handleRecentEvents(ctx context.Context, s *AssistantService, defaultProject string, raw json.RawMessage) (any, error) {
	if s.events == nil {
		return nil, fmt.Errorf("event service unavailable")
	}
	args := decodeArgs(raw)
	limit := intArg(args, "limit")
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	filters := map[string]string{}
	if v := stringArg(args, "type"); v != "" {
		filters["type"] = v
	}
	if v := stringArg(args, "source"); v != "" {
		filters["source"] = v
	}
	if v := stringArg(args, "feature_id"); v != "" {
		filters["feature_id"] = v
	}
	// project_id: fall back to defaultProject if not explicitly cleared
	pid := stringArg(args, "project_id")
	if pid == "" {
		pid = defaultProject
	}
	if pid != "" {
		filters["project_id"] = pid
	}
	events, err := s.events.Recent(ctx, limit, filters)
	if err != nil {
		return nil, err
	}
	return map[string]any{"count": len(events), "events": events}, nil
}

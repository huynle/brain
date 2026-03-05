package mcp

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// RegisterBrainTools registers all 19 brain core tools on the server.
func RegisterBrainTools(s *Server, client *APIClient) {
	registerBrainSave(s, client)
	registerBrainRecall(s, client)
	registerBrainSearch(s, client)
	registerBrainList(s, client)
	registerBrainInject(s, client)
	registerBrainUpdate(s, client)
	registerBrainDelete(s, client)
	registerBrainMove(s, client)
	registerBrainStats(s, client)
	registerBrainCheckConnection(s, client)
	registerBrainLink(s, client)
	registerBrainSection(s, client)
	registerBrainPlanSections(s, client)
	registerBrainVerify(s, client)
	registerBrainStale(s, client)
	registerBrainOrphans(s, client)
	registerBrainBacklinks(s, client)
	registerBrainOutlinks(s, client)
	registerBrainRelated(s, client)
}

// =============================================================================
// brain_save
// =============================================================================

func registerBrainSave(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "brain_save",
		Description: `Save content to the brain for future reference. Use this to persist:
- summaries: Session summaries, key decisions made
- reports: Analysis reports, code reviews, investigations
- walkthroughs: Code explanations, architecture overviews
- plans: Implementation plans, designs, roadmaps
- patterns: Reusable patterns discovered (use global:true for cross-project)
- learnings: General learnings, best practices (use global:true for cross-project)
- ideas: Ideas for future exploration
- scratch: Temporary working notes
- decision: Architectural decisions, ADRs
- exploration: Investigation notes, research findings`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"type":                  {Type: "string", Enum: types.EntryTypes, Description: "Type of content being saved"},
				"title":                 {Type: "string", Description: "Short descriptive title for the entry"},
				"content":               {Type: "string", Description: "The content to save (markdown supported)"},
				"tags":                  {Type: "array", Items: &Property{Type: "string"}, Description: "Tags for categorization"},
				"status":                {Type: "string", Enum: types.EntryStatuses, Description: "Initial status. Tasks default to 'draft' (user reviews before promoting to 'pending'). Other entry types default to 'active'."},
				"priority":              {Type: "string", Enum: types.Priorities, Description: "Priority level"},
				"global":                {Type: "boolean", Description: "Save to global brain (cross-project)"},
				"project":               {Type: "string", Description: "Explicit project ID/name"},
				"depends_on":            {Type: "array", Items: &Property{Type: "string"}, Description: "Task dependencies - list of task IDs or titles"},
				"user_original_request": {Type: "string", Description: "Verbatim user request for this task. HIGHLY RECOMMENDED for tasks - enables validation during task completion. Supports multiline content, code blocks, and special characters. When creating multiple tasks from one user request, include this in EACH task."},
				"target_workdir":        {Type: "string", Description: "Explicit working directory override for task execution (absolute path). When set, the task runner will try this directory first before falling back to workdir resolution. Use for tasks that should execute in a specific directory."},
				"feature_id":            {Type: "string", Description: "Feature group ID for this task (e.g., 'auth-system', 'payment-flow'). Tasks with the same feature_id are grouped together for ordered execution."},
				"feature_priority":      {Type: "string", Enum: types.Priorities, Description: "Priority level for the feature group. Determines execution order relative to other features."},
				"feature_depends_on":    {Type: "array", Items: &Property{Type: "string"}, Description: "Feature IDs this feature depends on. All tasks in dependent features must complete before this feature's tasks can start."},
				"direct_prompt":         {Type: "string", Description: "Direct prompt to execute, bypassing default skill workflow. The prompt is sent verbatim when the task runs."},
				"agent":                 {Type: "string", Description: "Override agent for this task (e.g., 'explore', 'tdd-dev', 'build')"},
				"model":                 {Type: "string", Description: "Override model (format: 'provider/model-id', e.g., 'anthropic/claude-sonnet-4-20250514')"},
				"schedule":              {Type: "string", Description: "Cron schedule expression (e.g., '*/5 * * * *', '0 2 * * *'). When provided for tasks, automatically creates and links a cron entry titled '{task-title} (Cron)'. This simplifies recurring task setup from 3 steps to 1 step."},
				"schedule_enabled":      {Type: "boolean", Description: "Whether the schedule is active (default true when schedule exists). Set to false to pause scheduling."},
				"git_branch":            {Type: "string", Description: "Git branch for the task"},
				"merge_target_branch":   {Type: "string", Description: "Branch to merge completed work into"},
				"merge_policy":          {Type: "string", Enum: types.MergePolicies, Description: "Merge behavior at checkout completion"},
				"merge_strategy":        {Type: "string", Enum: types.MergeStrategies, Description: "Git merge strategy"},
				"remote_branch_policy":  {Type: "string", Enum: types.RemoteBranchPolicies, Description: "Remote branch cleanup after merge"},
				"open_pr_before_merge":  {Type: "boolean", Description: "Require PR before merge"},
				"execution_mode":        {Type: "string", Enum: types.ExecutionModes, Description: "Task execution mode (default: worktree)"},
				"complete_on_idle":      {Type: "boolean", Description: "Mark task as completed when agent becomes idle (default: false). Useful for fire-and-forget tasks."},
				"relatedEntries":        {Type: "array", Items: &Property{Type: "string"}, Description: "Related brain entry paths to link"},
			},
			Required: []string{"type", "title", "content"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		execCtx := GetCachedContext()
		isTask := StringArg(args, "type", "") == "task"

		body := map[string]any{
			"type":           args["type"],
			"title":          args["title"],
			"content":        args["content"],
			"tags":           args["tags"],
			"status":         args["status"],
			"priority":       args["priority"],
			"global":         args["global"],
			"project":        StringArg(args, "project", execCtx.ProjectID),
			"depends_on":     args["depends_on"],
			"relatedEntries": args["relatedEntries"],
		}

		// Task-specific enrichment
		if isTask {
			body["workdir"] = execCtx.Workdir
			body["git_remote"] = execCtx.GitRemote
			body["git_branch"] = StringArg(args, "git_branch", execCtx.GitBranch)
			body["target_workdir"] = args["target_workdir"]
			body["user_original_request"] = args["user_original_request"]
			body["feature_id"] = args["feature_id"]
			body["feature_priority"] = args["feature_priority"]
			body["feature_depends_on"] = args["feature_depends_on"]
			body["direct_prompt"] = args["direct_prompt"]
			body["agent"] = args["agent"]
			body["model"] = args["model"]
			body["schedule"] = args["schedule"]
			body["schedule_enabled"] = args["schedule_enabled"]
			body["merge_target_branch"] = args["merge_target_branch"]
			body["merge_policy"] = args["merge_policy"]
			body["merge_strategy"] = args["merge_strategy"]
			body["remote_branch_policy"] = args["remote_branch_policy"]
			body["open_pr_before_merge"] = args["open_pr_before_merge"]
			body["execution_mode"] = args["execution_mode"]
			body["complete_on_idle"] = args["complete_on_idle"]
		}

		var resp struct {
			ID     string `json:"id"`
			Path   string `json:"path"`
			Title  string `json:"title"`
			Type   string `json:"type"`
			Status string `json:"status"`
		}
		if err := client.Request(ctx, "POST", "/entries", body, nil, &resp); err != nil {
			return "", err
		}

		return fmt.Sprintf("Saved to brain\n\nPath: %s\nID: %s\nTitle: %s\nType: %s\nStatus: %s",
			resp.Path, resp.ID, resp.Title, resp.Type, resp.Status), nil
	})
}

// =============================================================================
// brain_recall
// =============================================================================

func registerBrainRecall(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "brain_recall",
		Description: "Retrieve a specific entry from the brain by path, ID, or title.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path":  {Type: "string", Description: "Path or ID to the note"},
				"title": {Type: "string", Description: "Title to search for (exact match)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		entryPath := StringArg(args, "path", "")

		// Title fallback: search then get by exact match
		if entryPath == "" {
			title := StringArg(args, "title", "")
			if title == "" {
				return "Please provide a path or title", nil
			}

			var searchResp struct {
				Results []struct {
					Path  string `json:"path"`
					Title string `json:"title"`
				} `json:"results"`
			}
			if err := client.Request(ctx, "POST", "/search", map[string]any{"query": title, "limit": 5}, nil, &searchResp); err != nil {
				return "", err
			}

			// Find exact match
			for _, r := range searchResp.Results {
				if r.Title == title {
					entryPath = r.Path
					break
				}
			}
			if entryPath == "" {
				return fmt.Sprintf("No exact match for: %q", title), nil
			}
		}

		var resp struct {
			ID                  string   `json:"id"`
			Path                string   `json:"path"`
			Title               string   `json:"title"`
			Type                string   `json:"type"`
			Status              string   `json:"status"`
			Content             string   `json:"content"`
			Tags                []string `json:"tags"`
			UserOriginalRequest string   `json:"user_original_request"`
		}
		if err := client.Request(ctx, "GET", "/entries/"+entryPath, nil, nil, &resp); err != nil {
			return "", err
		}

		tags := "none"
		if len(resp.Tags) > 0 {
			tags = strings.Join(resp.Tags, ", ")
		}

		userRequest := ""
		if resp.UserOriginalRequest != "" {
			userRequest = fmt.Sprintf("\nUser Original Request: %s", resp.UserOriginalRequest)
		}

		return fmt.Sprintf("## %s\n\nPath: %s\nType: %s\nStatus: %s\nTags: %s%s\n\n---\n\n%s",
			resp.Title, resp.Path, resp.Type, resp.Status, tags, userRequest, resp.Content), nil
	})
}

// =============================================================================
// brain_search
// =============================================================================

func registerBrainSearch(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "brain_search",
		Description: "Search the brain using full-text search.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"query":      {Type: "string", Description: "Search query"},
				"type":       {Type: "string", Enum: types.EntryTypes, Description: "Filter by entry type"},
				"status":     {Type: "string", Enum: types.EntryStatuses, Description: "Filter by status"},
				"feature_id": {Type: "string", Description: "Filter by feature group ID (e.g., 'auth-system', 'dark-mode')"},
				"tags":       {Type: "array", Items: &Property{Type: "string"}, Description: "Filter by tags (OR logic - matches entries with any of the specified tags)"},
				"limit":      {Type: "number", Description: "Maximum results (default: 10)"},
				"global":     {Type: "boolean", Description: "Search only global entries"},
			},
			Required: []string{"query"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		var resp struct {
			Results []struct {
				ID      string `json:"id"`
				Path    string `json:"path"`
				Title   string `json:"title"`
				Type    string `json:"type"`
				Status  string `json:"status"`
				Snippet string `json:"snippet"`
			} `json:"results"`
			Total int `json:"total"`
		}
		if err := client.Request(ctx, "POST", "/search", args, nil, &resp); err != nil {
			return "", err
		}

		if len(resp.Results) == 0 {
			return fmt.Sprintf("No entries found matching %q", args["query"]), nil
		}

		lines := []string{fmt.Sprintf("Found %d entries:\n", resp.Total)}
		for _, r := range resp.Results {
			lines = append(lines, fmt.Sprintf("- **%s** (%s) - %s", r.Title, r.Path, r.Type))
			if r.Snippet != "" {
				lines = append(lines, fmt.Sprintf("  > %s...", r.Snippet))
			}
		}
		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// brain_list
// =============================================================================

func registerBrainList(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "brain_list",
		Description: `List entries in the brain with optional filtering by type, status, and filename.

Filename filtering supports:
- Exact match: "abc12def" finds entry with that exact ID
- Wildcard patterns: "abc*" (prefix), "*def" (suffix), "abc*def" (contains)`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"type":       {Type: "string", Enum: types.EntryTypes, Description: "Filter by entry type"},
				"status":     {Type: "string", Enum: types.EntryStatuses, Description: "Filter by status"},
				"feature_id": {Type: "string", Description: "Filter by feature group ID (e.g., 'auth-system', 'dark-mode')"},
				"tags":       {Type: "array", Items: &Property{Type: "string"}, Description: "Filter by tags (OR logic - matches entries with any of the specified tags)"},
				"limit":      {Type: "number", Description: "Maximum entries to return (default: 20)"},
				"global":     {Type: "boolean", Description: "List only global entries"},
				"sortBy":     {Type: "string", Enum: []string{"created", "modified", "priority"}, Description: "Sort order"},
				"filename":   {Type: "string", Description: "Filter by filename/ID (supports wildcards: abc*, *def, abc*def)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		// Convert tags array to comma-separated string for GET query params
		params := make(map[string]string)
		if v := StringArg(args, "type", ""); v != "" {
			params["type"] = v
		}
		if v := StringArg(args, "status", ""); v != "" {
			params["status"] = v
		}
		if v := StringArg(args, "feature_id", ""); v != "" {
			params["feature_id"] = v
		}
		if v := StringArg(args, "filename", ""); v != "" {
			params["filename"] = v
		}
		if v := StringArg(args, "sortBy", ""); v != "" {
			params["sortBy"] = v
		}
		if tags := StringSliceArg(args, "tags"); len(tags) > 0 {
			params["tags"] = strings.Join(tags, ",")
		}
		if v, ok := args["limit"].(float64); ok {
			params["limit"] = fmt.Sprintf("%d", int(v))
		}
		if v, ok := args["global"].(bool); ok {
			params["global"] = fmt.Sprintf("%t", v)
		}

		var resp struct {
			Entries []struct {
				ID       string `json:"id"`
				Path     string `json:"path"`
				Title    string `json:"title"`
				Type     string `json:"type"`
				Status   string `json:"status"`
				Priority string `json:"priority"`
			} `json:"entries"`
			Total int `json:"total"`
		}
		if err := client.Request(ctx, "GET", "/entries", nil, params, &resp); err != nil {
			return "", err
		}

		if len(resp.Entries) == 0 {
			return "No entries found", nil
		}

		lines := []string{fmt.Sprintf("Found %d entries:\n", resp.Total)}
		for _, e := range resp.Entries {
			lines = append(lines, fmt.Sprintf("- **%s** (%s) - %s | %s", e.Title, e.Path, e.Type, e.Status))
		}
		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// brain_inject
// =============================================================================

func registerBrainInject(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "brain_inject",
		Description: "Search the brain and return relevant context for a task.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"query":      {Type: "string", Description: "What context are you looking for?"},
				"maxEntries": {Type: "number", Description: "Maximum entries to include (default: 5)"},
				"type":       {Type: "string", Enum: types.EntryTypes, Description: "Filter by entry type"},
			},
			Required: []string{"query"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		var resp struct {
			Context string `json:"context"`
			Entries []struct {
				ID    string `json:"id"`
				Path  string `json:"path"`
				Title string `json:"title"`
				Type  string `json:"type"`
			} `json:"entries"`
		}
		if err := client.Request(ctx, "POST", "/inject", args, nil, &resp); err != nil {
			return "", err
		}

		if resp.Context == "" || len(resp.Entries) == 0 {
			return fmt.Sprintf("No relevant context found for %q", args["query"]), nil
		}
		return resp.Context, nil
	})
}

// =============================================================================
// brain_update
// =============================================================================

func registerBrainUpdate(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "brain_update",
		Description: `Update an existing brain entry's status, title, dependencies, or append content.

Use cases:
- Mark a plan as completed: brain_update(path: "...", status: "completed")
- Mark as in-progress: brain_update(path: "...", status: "in_progress")  
- Block with reason: brain_update(path: "...", status: "blocked", note: "Waiting on API design")
- Append progress notes: brain_update(path: "...", append: "## Progress\n- Completed auth module")
- Update title: brain_update(path: "...", title: "New Title")
- Update dependencies: brain_update(path: "...", depends_on: ["task-id-1", "task-id-2"])
- Update tags: brain_update(path: "...", tags: ["tag1", "tag2"])
- Update priority: brain_update(path: "...", priority: "high")

Statuses: draft, active, in_progress, blocked, completed, validated, superseded, archived`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path":                 {Type: "string", Description: "Path to the entry to update"},
				"status":               {Type: "string", Enum: types.EntryStatuses, Description: "New status"},
				"title":                {Type: "string", Description: "New title"},
				"append":               {Type: "string", Description: "Content to append"},
				"note":                 {Type: "string", Description: "Short note to add"},
				"depends_on":           {Type: "array", Items: &Property{Type: "string"}, Description: "Task dependencies - list of task IDs or titles"},
				"tags":                 {Type: "array", Items: &Property{Type: "string"}, Description: "Update tags for the entry"},
				"priority":             {Type: "string", Enum: types.Priorities, Description: "Priority level"},
				"target_workdir":       {Type: "string", Description: "Explicit working directory override for task execution"},
				"git_branch":           {Type: "string", Description: "Git branch for the task"},
				"merge_target_branch":  {Type: "string", Description: "Branch to merge completed work into"},
				"merge_policy":         {Type: "string", Enum: types.MergePolicies, Description: "Merge behavior at checkout completion"},
				"merge_strategy":       {Type: "string", Enum: types.MergeStrategies, Description: "Git merge strategy"},
				"remote_branch_policy": {Type: "string", Enum: types.RemoteBranchPolicies, Description: "Remote branch cleanup after merge"},
				"open_pr_before_merge": {Type: "boolean", Description: "Require PR before merge"},
				"execution_mode":       {Type: "string", Enum: types.ExecutionModes, Description: "Task execution mode (default: worktree)"},
				"complete_on_idle":     {Type: "boolean", Description: "Mark task as completed when agent becomes idle"},
				"schedule":             {Type: "string", Description: "Cron schedule expression (e.g., '*/5 * * * *')"},
				"schedule_enabled":     {Type: "boolean", Description: "Whether the schedule is active (default true when schedule exists). Set to false to pause scheduling."},
				"feature_id":           {Type: "string", Description: "Feature group identifier (e.g., 'auth-system', 'payment-flow')"},
				"feature_priority":     {Type: "string", Enum: types.Priorities, Description: "Priority for this feature group"},
				"feature_depends_on":   {Type: "array", Items: &Property{Type: "string"}, Description: "Feature IDs this feature depends on"},
				"direct_prompt":        {Type: "string", Description: "Direct prompt to execute, bypassing default skill workflow"},
				"agent":                {Type: "string", Description: "Override agent for this task (e.g., 'explore', 'tdd-dev')"},
				"model":                {Type: "string", Description: "Override model (format: 'provider/model-id')"},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		path := StringArg(args, "path", "")

		body := map[string]any{
			"status":               args["status"],
			"title":                args["title"],
			"append":               args["append"],
			"note":                 args["note"],
			"depends_on":           args["depends_on"],
			"tags":                 args["tags"],
			"priority":             args["priority"],
			"target_workdir":       args["target_workdir"],
			"git_branch":           args["git_branch"],
			"merge_target_branch":  args["merge_target_branch"],
			"merge_policy":         args["merge_policy"],
			"merge_strategy":       args["merge_strategy"],
			"remote_branch_policy": args["remote_branch_policy"],
			"open_pr_before_merge": args["open_pr_before_merge"],
			"execution_mode":       args["execution_mode"],
			"complete_on_idle":     args["complete_on_idle"],
			"schedule":             args["schedule"],
			"schedule_enabled":     args["schedule_enabled"],
			"feature_id":           args["feature_id"],
			"feature_priority":     args["feature_priority"],
			"feature_depends_on":   args["feature_depends_on"],
			"direct_prompt":        args["direct_prompt"],
			"agent":                args["agent"],
			"model":                args["model"],
		}

		var resp struct {
			Path   string `json:"path"`
			Title  string `json:"title"`
			Status string `json:"status"`
		}
		if err := client.Request(ctx, "PATCH", "/entries/"+path, body, nil, &resp); err != nil {
			return "", err
		}

		var changes []string
		if v := StringArg(args, "status", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Status: -> %s", v))
		}
		if v := StringArg(args, "title", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Title: -> %q", v))
		}
		if v := StringArg(args, "note", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Note: %q", v))
		}
		if v := StringArg(args, "append", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Appended %d characters", len(v)))
		}
		if deps := StringSliceArg(args, "depends_on"); deps != nil {
			changes = append(changes, fmt.Sprintf("Dependencies: %d task(s)", len(deps)))
		}
		if tags := StringSliceArg(args, "tags"); tags != nil {
			if len(tags) > 0 {
				changes = append(changes, fmt.Sprintf("Tags: %s", strings.Join(tags, ", ")))
			} else {
				changes = append(changes, "Tags: (cleared)")
			}
		}
		if v := StringArg(args, "priority", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Priority: %s", v))
		}
		if v := StringArg(args, "target_workdir", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Target Workdir: %s", v))
		}
		if v := StringArg(args, "git_branch", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Git Branch: %s", v))
		}
		if v := StringArg(args, "merge_target_branch", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Merge Target Branch: %s", v))
		}
		if v := StringArg(args, "merge_policy", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Merge Policy: %s", v))
		}
		if v := StringArg(args, "merge_strategy", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Merge Strategy: %s", v))
		}
		if v := StringArg(args, "remote_branch_policy", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Remote Branch Policy: %s", v))
		}
		if _, ok := args["open_pr_before_merge"]; ok {
			changes = append(changes, fmt.Sprintf("Open PR Before Merge: %v", args["open_pr_before_merge"]))
		}
		if v := StringArg(args, "execution_mode", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Execution Mode: %s", v))
		}
		if _, ok := args["complete_on_idle"]; ok {
			changes = append(changes, fmt.Sprintf("Complete On Idle: %v", args["complete_on_idle"]))
		}
		if v := StringArg(args, "schedule", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Schedule: %s", v))
		}
		if _, ok := args["schedule_enabled"]; ok {
			changes = append(changes, fmt.Sprintf("Schedule Enabled: %v", args["schedule_enabled"]))
		}
		if v := StringArg(args, "feature_id", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Feature ID: %s", v))
		}
		if v := StringArg(args, "feature_priority", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Feature Priority: %s", v))
		}
		if deps := StringSliceArg(args, "feature_depends_on"); deps != nil {
			changes = append(changes, fmt.Sprintf("Feature Dependencies: %d feature(s)", len(deps)))
		}
		if v := StringArg(args, "direct_prompt", ""); v != "" {
			changes = append(changes, "Direct Prompt: set")
		}
		if v := StringArg(args, "agent", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Agent: %s", v))
		}
		if v := StringArg(args, "model", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Model: %s", v))
		}

		changeLines := make([]string, len(changes))
		for i, c := range changes {
			changeLines[i] = "- " + c
		}

		return fmt.Sprintf("Updated: %s\n\n**Changes:**\n%s\n\n**Current Status:** %s\n**Title:** %s\n\nUse `brain_recall` to view the full entry.",
			resp.Path, strings.Join(changeLines, "\n"), resp.Status, resp.Title), nil
	})
}

// =============================================================================
// brain_delete
// =============================================================================

func registerBrainDelete(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "brain_delete",
		Description: "Delete a specific entry from the brain by path. Use with caution.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path":    {Type: "string", Description: "Path to the entry to delete"},
				"confirm": {Type: "boolean", Description: "Must be true to confirm deletion"},
			},
			Required: []string{"path", "confirm"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		if !BoolArg(args, "confirm", false) {
			return "Please set `confirm: true` to delete the entry", nil
		}

		path := StringArg(args, "path", "")
		params := map[string]string{"confirm": "true"}
		var resp struct {
			Message string `json:"message"`
		}
		if err := client.Request(ctx, "DELETE", "/entries/"+path, nil, params, &resp); err != nil {
			return "", err
		}

		return fmt.Sprintf("Deleted: %s", path), nil
	})
}

// =============================================================================
// brain_move
// =============================================================================

func registerBrainMove(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "brain_move",
		Description: `Move a brain entry to a different project.

IMPORTANT LIMITATIONS:
- Works for: tasks, summaries, reports, plans, and other note types
- Cannot move entries currently in 'in_progress' status

Use cases:
- Bulk reassign tasks to a different project
- Move a task filed in the wrong project
- Reorganize project structure

Example: brain_move({ path: "projects/old/task/abc12def.md", project: "new-project" })`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path":    {Type: "string", Description: "Path to the entry to move (e.g., 'projects/old/task/abc12def.md')"},
				"project": {Type: "string", Description: "Target project ID to move the entry to (e.g., 'my-other-project')"},
			},
			Required: []string{"path", "project"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		path := StringArg(args, "path", "")
		project := StringArg(args, "project", "")
		if path == "" || project == "" {
			return "Please provide both path and target project", nil
		}

		var resp struct {
			OldPath string `json:"oldPath"`
			NewPath string `json:"newPath"`
			Project string `json:"project"`
			ID      string `json:"id"`
			Title   string `json:"title"`
		}
		if err := client.Request(ctx, "POST", "/entries/"+path+"/move", map[string]any{"project": project}, nil, &resp); err != nil {
			return "", err
		}

		return fmt.Sprintf("Moved: %s\nOld Path: %s\nNew Path: %s\nProject: %s",
			resp.Title, resp.OldPath, resp.NewPath, resp.Project), nil
	})
}

// =============================================================================
// brain_stats
// =============================================================================

func registerBrainStats(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "brain_stats",
		Description: "Get statistics about the brain storage.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"global": {Type: "boolean", Description: "Show only global entries stats"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		params := make(map[string]string)
		if v, ok := args["global"].(bool); ok {
			params["global"] = fmt.Sprintf("%t", v)
		}

		var resp struct {
			TotalEntries   int            `json:"totalEntries"`
			GlobalEntries  int            `json:"globalEntries"`
			ProjectEntries int            `json:"projectEntries"`
			ByType         map[string]int `json:"byType"`
		}
		if err := client.Request(ctx, "GET", "/stats", nil, params, &resp); err != nil {
			return "", err
		}

		lines := []string{
			"## Brain Statistics\n",
			fmt.Sprintf("Total: %d", resp.TotalEntries),
			fmt.Sprintf("Global: %d", resp.GlobalEntries),
			fmt.Sprintf("Project: %d", resp.ProjectEntries),
			"\n### By Type",
		}

		// Sort by count descending
		type typeCount struct {
			name  string
			count int
		}
		sorted := make([]typeCount, 0, len(resp.ByType))
		for name, count := range resp.ByType {
			sorted = append(sorted, typeCount{name, count})
		}
		sort.Slice(sorted, func(i, j int) bool { return sorted[i].count > sorted[j].count })

		for _, tc := range sorted {
			lines = append(lines, fmt.Sprintf("- %s: %d", tc.name, tc.count))
		}

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// brain_check_connection
// =============================================================================

func registerBrainCheckConnection(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "brain_check_connection",
		Description: `Check if the Brain API server is running and accessible.

Use this tool FIRST if you're unsure whether brain tools will work.
Returns connection status, server version, and helpful troubleshooting info if unavailable.

This is useful to:
- Verify the brain is available before starting a task that needs it
- Diagnose why other brain tools are failing
- Get instructions for starting the brain server`,
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		var resp struct {
			Status  string `json:"status"`
			Version string `json:"version"`
		}
		err := client.Request(ctx, "GET", "/health", nil, nil, &resp)
		if err != nil {
			return fmt.Sprintf(`Brain API Status: UNAVAILABLE

Server URL: %s
Error: %v

To start the Brain API server:
  brain start

To check server status:
  brain status

Brain tools will not work until the server is running.`, client.baseURL, err), nil
		}

		return fmt.Sprintf(`Brain API Status: CONNECTED

Server URL: %s
Version: %s
Status: Ready to use

All brain tools (save, recall, search, inject, etc.) are available.`, client.baseURL, resp.Version), nil
	})
}

// =============================================================================
// brain_link
// =============================================================================

func registerBrainLink(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "brain_link",
		Description: "Generate a markdown link to a brain entry. Use this when referencing other brain entries to ensure proper link resolution with mkdnflow.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"title":     {Type: "string", Description: "Title to search for"},
				"path":      {Type: "string", Description: "Direct path or ID (8-char alphanumeric) to the entry"},
				"withTitle": {Type: "boolean", Description: "Include title in link (default: true)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		if StringArg(args, "path", "") == "" && StringArg(args, "title", "") == "" {
			return "Please provide either a path, ID, or title to generate a link", nil
		}

		body := map[string]any{
			"title":     args["title"],
			"path":      args["path"],
			"withTitle": args["withTitle"],
		}

		var resp struct {
			Link  string `json:"link"`
			ID    string `json:"id"`
			Path  string `json:"path"`
			Title string `json:"title"`
		}
		if err := client.Request(ctx, "POST", "/link", body, nil, &resp); err != nil {
			return "", err
		}

		return fmt.Sprintf("Link: %s\nID: %s\nPath: %s\nTitle: %s",
			resp.Link, resp.ID, resp.Path, resp.Title), nil
	})
}

// =============================================================================
// brain_section
// =============================================================================

func registerBrainSection(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "brain_section",
		Description: `Retrieve a specific section's FULL CONTENT from a brain plan by section title.

Use this when you need the detailed implementation spec for your assigned task.
Returns the exact section content including all subsections, code examples, and acceptance criteria.

Example: brain_section({ planId: "projects/abc/plan/auth.md", sectionTitle: "JWT Middleware" })

This is more precise than brain_inject (which uses fuzzy search) - it extracts the exact section you need.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"planId":             {Type: "string", Description: "Brain plan path (from orchestration context or brain_plan_sections)"},
				"sectionTitle":       {Type: "string", Description: "Section title to retrieve (can be partial match)"},
				"includeSubsections": {Type: "boolean", Description: "Include nested subsections (default: true)"},
			},
			Required: []string{"planId", "sectionTitle"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		planId := StringArg(args, "planId", "")
		sectionTitle := StringArg(args, "sectionTitle", "")
		if planId == "" || sectionTitle == "" {
			return "Please provide both planId and sectionTitle", nil
		}

		encodedTitle := url.PathEscape(sectionTitle)
		params := map[string]string{}
		if BoolArg(args, "includeSubsections", true) {
			params["includeSubsections"] = "true"
		} else {
			params["includeSubsections"] = "false"
		}

		var resp struct {
			Title   string `json:"title"`
			Content string `json:"content"`
			Level   int    `json:"level"`
			Line    int    `json:"line"`
		}
		if err := client.Request(ctx, "GET", "/entries/"+planId+"/sections/"+encodedTitle, nil, params, &resp); err != nil {
			return "", err
		}

		return fmt.Sprintf("## Section: %s\n\n**Plan:** %s\n**Line:** %d\n\n---\n\n%s",
			resp.Title, planId, resp.Line, resp.Content), nil
	})
}

// =============================================================================
// brain_plan_sections
// =============================================================================

func registerBrainPlanSections(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "brain_plan_sections",
		Description: "Extract section headers from a plan entry for orchestration mapping.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path":  {Type: "string", Description: "Path to the plan entry"},
				"title": {Type: "string", Description: "Title to search for"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		entryPath := StringArg(args, "path", "")

		if entryPath == "" {
			title := StringArg(args, "title", "")
			if title == "" {
				return "Please provide either a path or title", nil
			}

			var searchResp struct {
				Results []struct {
					Path  string `json:"path"`
					Title string `json:"title"`
				} `json:"results"`
			}
			if err := client.Request(ctx, "POST", "/search", map[string]any{"query": title, "limit": 5}, nil, &searchResp); err != nil {
				return "", err
			}

			for _, r := range searchResp.Results {
				if r.Title == title {
					entryPath = r.Path
					break
				}
			}
			if entryPath == "" {
				if len(searchResp.Results) > 0 {
					suggestions := make([]string, 0, 5)
					for _, r := range searchResp.Results {
						suggestions = append(suggestions, r.Title)
					}
					return fmt.Sprintf("No exact match for title: %q\n\nDid you mean: %s", title, strings.Join(suggestions, ", ")), nil
				}
				return fmt.Sprintf("No entry found matching title: %q", title), nil
			}
		}

		var sectionsResp struct {
			Sections []struct {
				Title string `json:"title"`
				Level int    `json:"level"`
				Line  int    `json:"line"`
			} `json:"sections"`
			Total int `json:"total"`
		}
		if err := client.Request(ctx, "GET", "/entries/"+entryPath+"/sections", nil, nil, &sectionsResp); err != nil {
			return "", err
		}

		var entryResp struct {
			Title string `json:"title"`
			Type  string `json:"type"`
		}
		if err := client.Request(ctx, "GET", "/entries/"+entryPath, nil, nil, &entryResp); err != nil {
			return "", err
		}

		lines := []string{
			fmt.Sprintf("## Sections in: %s", entryResp.Title),
			"",
			fmt.Sprintf("**Path:** %s", entryPath),
			fmt.Sprintf("**Type:** %s", entryResp.Type),
			fmt.Sprintf("**Total sections:** %d", sectionsResp.Total),
			"",
		}

		for _, section := range sectionsResp.Sections {
			indent := strings.Repeat("  ", section.Level-1)
			lines = append(lines, fmt.Sprintf("%s- %s (line %d)", indent, section.Title, section.Line))
		}

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// brain_verify
// =============================================================================

func registerBrainVerify(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "brain_verify",
		Description: "Mark an entry as verified (still accurate). Updates the last_verified timestamp.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path": {Type: "string", Description: "Path to the note to verify"},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		path := StringArg(args, "path", "")
		var resp struct {
			Message string `json:"message"`
			Path    string `json:"path"`
		}
		if err := client.Request(ctx, "POST", "/entries/"+path+"/verify", nil, nil, &resp); err != nil {
			return "", err
		}

		return fmt.Sprintf("Verified: %s\n\nEntry marked as still accurate. It will not appear in stale entry lists for 30 days.", path), nil
	})
}

// =============================================================================
// brain_stale
// =============================================================================

func registerBrainStale(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "brain_stale",
		Description: "Find entries that may need verification (not verified in N days).",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"days":  {Type: "number", Description: "Days threshold (default: 30)"},
				"type":  {Type: "string", Enum: types.EntryTypes, Description: "Filter by entry type"},
				"limit": {Type: "number", Description: "Maximum results (default: 20)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		days := IntArg(args, "days", 30)
		params := map[string]string{
			"days":  fmt.Sprintf("%d", days),
			"limit": fmt.Sprintf("%d", IntArg(args, "limit", 20)),
		}
		if v := StringArg(args, "type", ""); v != "" {
			params["type"] = v
		}

		var resp struct {
			Entries []struct {
				ID                string `json:"id"`
				Path              string `json:"path"`
				Title             string `json:"title"`
				Type              string `json:"type"`
				DaysSinceVerified *int   `json:"daysSinceVerified"`
			} `json:"entries"`
			Total int `json:"total"`
		}
		if err := client.Request(ctx, "GET", "/stale", nil, params, &resp); err != nil {
			return "", err
		}

		if len(resp.Entries) == 0 {
			return fmt.Sprintf("No stale entries found (all verified within %d days)", days), nil
		}

		lines := []string{
			fmt.Sprintf("## Stale Entries (not verified in %d days)", days),
			"",
			fmt.Sprintf("Found %d entries needing verification:", resp.Total),
			"",
		}

		for _, e := range resp.Entries {
			daysSince := "never"
			if e.DaysSinceVerified != nil {
				daysSince = fmt.Sprintf("%d days ago", *e.DaysSinceVerified)
			}
			lines = append(lines, fmt.Sprintf("- **%s**", e.Title))
			lines = append(lines, fmt.Sprintf("  `%s` | Last verified: %s", e.Path, daysSince))
		}

		lines = append(lines, "")
		lines = append(lines, "*Use `brain_verify` to mark entries as still accurate.*")

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// brain_orphans
// =============================================================================

func registerBrainOrphans(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "brain_orphans",
		Description: "Find entries with no incoming links (orphans). Useful for knowledge graph health.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"type":  {Type: "string", Enum: types.EntryTypes, Description: "Filter by entry type"},
				"limit": {Type: "number", Description: "Maximum results (default: 20)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		params := map[string]string{
			"limit": fmt.Sprintf("%d", IntArg(args, "limit", 20)),
		}
		if v := StringArg(args, "type", ""); v != "" {
			params["type"] = v
		}

		var resp struct {
			Entries []struct {
				ID    string `json:"id"`
				Path  string `json:"path"`
				Title string `json:"title"`
				Type  string `json:"type"`
			} `json:"entries"`
			Total int `json:"total"`
		}
		if err := client.Request(ctx, "GET", "/orphans", nil, params, &resp); err != nil {
			return "", err
		}

		if len(resp.Entries) == 0 {
			typeFilter := ""
			if v := StringArg(args, "type", ""); v != "" {
				typeFilter = fmt.Sprintf(" of type %q", v)
			}
			return fmt.Sprintf("No orphan entries found%s", typeFilter), nil
		}

		typeLabel := ""
		if v := StringArg(args, "type", ""); v != "" {
			typeLabel = fmt.Sprintf(" (%s)", v)
		}

		lines := []string{
			fmt.Sprintf("## Orphan Entries%s", typeLabel),
			"",
			fmt.Sprintf("Found %d entries with no incoming links:", resp.Total),
			"",
		}

		for _, e := range resp.Entries {
			lines = append(lines, fmt.Sprintf("- **%s** (`%s`) - %s", e.Title, e.Path, e.Type))
		}

		lines = append(lines, "")
		lines = append(lines, "*Consider linking these notes from related entries to improve knowledge graph connectivity.*")

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// brain_backlinks
// =============================================================================

func registerBrainBacklinks(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "brain_backlinks",
		Description: "Find entries that link TO a given entry (backlinks).",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path": {Type: "string", Description: "Path to the target note"},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		path := StringArg(args, "path", "")

		var resp struct {
			Entries []struct {
				ID    string `json:"id"`
				Path  string `json:"path"`
				Title string `json:"title"`
				Type  string `json:"type"`
			} `json:"entries"`
			Total int `json:"total"`
		}
		if err := client.Request(ctx, "GET", "/entries/"+path+"/backlinks", nil, nil, &resp); err != nil {
			return "", err
		}

		if len(resp.Entries) == 0 {
			return fmt.Sprintf("No backlinks found for: %s", path), nil
		}

		lines := []string{
			fmt.Sprintf("## Backlinks to: %s", path),
			"",
			fmt.Sprintf("Found %d entries linking to this note:", resp.Total),
			"",
		}

		for _, e := range resp.Entries {
			lines = append(lines, fmt.Sprintf("- **%s** (`%s`) - %s", e.Title, e.Path, e.Type))
		}

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// brain_outlinks
// =============================================================================

func registerBrainOutlinks(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "brain_outlinks",
		Description: "Find entries that a given entry links TO (outlinks).",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path": {Type: "string", Description: "Path to the source note"},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		path := StringArg(args, "path", "")

		var resp struct {
			Entries []struct {
				ID    string `json:"id"`
				Path  string `json:"path"`
				Title string `json:"title"`
				Type  string `json:"type"`
			} `json:"entries"`
			Total int `json:"total"`
		}
		if err := client.Request(ctx, "GET", "/entries/"+path+"/outlinks", nil, nil, &resp); err != nil {
			return "", err
		}

		if len(resp.Entries) == 0 {
			return fmt.Sprintf("No outlinks found from: %s", path), nil
		}

		lines := []string{
			fmt.Sprintf("## Outlinks from: %s", path),
			"",
			fmt.Sprintf("Found %d entries linked from this note:", resp.Total),
			"",
		}

		for _, e := range resp.Entries {
			lines = append(lines, fmt.Sprintf("- **%s** (`%s`) - %s", e.Title, e.Path, e.Type))
		}

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// brain_related
// =============================================================================

func registerBrainRelated(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "brain_related",
		Description: "Find entries that share linked notes with a given entry.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path":  {Type: "string", Description: "Path to the note to find related entries for"},
				"limit": {Type: "number", Description: "Maximum results (default: 10)"},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		path := StringArg(args, "path", "")
		params := map[string]string{
			"limit": fmt.Sprintf("%d", IntArg(args, "limit", 10)),
		}

		var resp struct {
			Entries []struct {
				ID    string `json:"id"`
				Path  string `json:"path"`
				Title string `json:"title"`
				Type  string `json:"type"`
			} `json:"entries"`
			Total int `json:"total"`
		}
		if err := client.Request(ctx, "GET", "/entries/"+path+"/related", nil, params, &resp); err != nil {
			return "", err
		}

		if len(resp.Entries) == 0 {
			return fmt.Sprintf("No related entries found for: %s", path), nil
		}

		lines := []string{
			fmt.Sprintf("## Related to: %s", path),
			"",
			fmt.Sprintf("Found %d entries sharing links:", resp.Total),
			"",
		}

		for _, e := range resp.Entries {
			lines = append(lines, fmt.Sprintf("- **%s** (`%s`) - %s", e.Title, e.Path, e.Type))
		}

		return strings.Join(lines, "\n"), nil
	})
}

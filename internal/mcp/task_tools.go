package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// RegisterTaskTools registers all 10 task tools on the server.
func RegisterTaskTools(s *Server, client *APIClient) {
	registerBrainTasks(s, client)
	registerBrainTaskNext(s, client)
	registerBrainTaskGet(s, client)
	registerBrainTaskMetadata(s, client)
	registerBrainTasksStatus(s, client)
	registerBrainTaskTrigger(s, client)
	registerBrainFeatureReviewEnable(s, client)
	registerBrainFeatureReviewDisable(s, client)
	registerBrainBlockedInspectorEnable(s, client)
	registerBrainBlockedInspectorDisable(s, client)
}

// =============================================================================
// brain_tasks
// =============================================================================

func registerBrainTasks(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "brain_tasks",
		Description: `List all tasks for current project with dependency status (ready/waiting/blocked), stats, and cycles detected.

Use this to see:
- Which tasks are ready to work on (dependencies met)
- Which tasks are waiting (dependencies incomplete)
- Which tasks are blocked (circular deps or blocked deps)
- Overall task queue stats`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"status":         {Type: "string", Enum: types.EntryStatuses, Description: "Filter by task status (pending, in_progress, completed, etc.)"},
				"classification": {Type: "string", Enum: []string{"ready", "waiting", "blocked"}, Description: "Filter by dependency classification"},
				"feature_id":     {Type: "string", Description: "Filter tasks by feature group ID (e.g., 'auth-system', 'dark-mode')"},
				"limit":          {Type: "number", Description: "Maximum results to return (default: 50)"},
				"project":        {Type: "string", Description: "Override auto-detected project"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		proj := ResolveProject(args)

		var resp struct {
			Tasks []struct {
				ID             string `json:"id"`
				Title          string `json:"title"`
				Status         string `json:"status"`
				Priority       string `json:"priority"`
				FeatureID      string `json:"feature_id"`
				Classification string `json:"classification"`
				DependsOn      []struct {
					ID     string `json:"id"`
					Title  string `json:"title"`
					Status string `json:"status"`
				} `json:"dependsOn"`
				BlockedBy string `json:"blocked_by_reason"`
			} `json:"tasks"`
			Count int `json:"count"`
			Stats *struct {
				Ready     int `json:"ready"`
				Waiting   int `json:"waiting"`
				Blocked   int `json:"blocked"`
				Completed int `json:"completed"`
				Total     int `json:"total"`
			} `json:"stats"`
			Cycles []struct {
				TaskID string   `json:"taskId"`
				Cycle  []string `json:"cycle"`
			} `json:"cycles"`
		}
		if err := client.Request(ctx, "GET", "/tasks/"+url.PathEscape(proj), nil, nil, &resp); err != nil {
			return "", err
		}

		// Apply filters
		type taskEntry struct {
			ID             string
			Title          string
			Status         string
			Priority       string
			FeatureID      string
			Classification string
			DependsOn      []struct {
				ID     string `json:"id"`
				Title  string `json:"title"`
				Status string `json:"status"`
			}
			BlockedBy string
		}

		filtered := make([]taskEntry, 0, len(resp.Tasks))
		statusFilter := StringArg(args, "status", "")
		classFilter := StringArg(args, "classification", "")
		featureFilter := StringArg(args, "feature_id", "")

		for _, t := range resp.Tasks {
			if statusFilter != "" && t.Status != statusFilter {
				continue
			}
			if classFilter != "" && t.Classification != classFilter {
				continue
			}
			if featureFilter != "" && t.FeatureID != featureFilter {
				continue
			}
			filtered = append(filtered, taskEntry{
				ID:             t.ID,
				Title:          t.Title,
				Status:         t.Status,
				Priority:       t.Priority,
				FeatureID:      t.FeatureID,
				Classification: t.Classification,
				DependsOn:      t.DependsOn,
				BlockedBy:      t.BlockedBy,
			})
		}

		limit := IntArg(args, "limit", 50)
		if len(filtered) > limit {
			filtered = filtered[:limit]
		}

		// Group by classification
		var ready, waiting, blocked []taskEntry
		for _, t := range filtered {
			switch t.Classification {
			case "ready":
				ready = append(ready, t)
			case "waiting":
				waiting = append(waiting, t)
			case "blocked":
				blocked = append(blocked, t)
			}
		}

		lines := []string{
			fmt.Sprintf("## Tasks for project: %s", proj),
			"",
		}

		// Stats summary
		if resp.Stats != nil {
			st := resp.Stats
			lines = append(lines, fmt.Sprintf("**Stats:** %d ready | %d waiting | %d blocked | %d completed", st.Ready, st.Waiting, st.Blocked, st.Completed))
			lines = append(lines, "")
		}

		// Ready tasks
		if len(ready) > 0 {
			lines = append(lines, "### Ready (can start now)")
			for _, task := range ready {
				priority := priorityLabel(task.Priority)
				lines = append(lines, fmt.Sprintf("- **%s %s** (`%s`) - %s", priority, task.Title, task.ID, task.Status))
				if len(task.DependsOn) > 0 {
					deps := make([]string, len(task.DependsOn))
					for i, d := range task.DependsOn {
						deps[i] = fmt.Sprintf("%s (%s)", d.Title, d.Status)
					}
					lines = append(lines, fmt.Sprintf("  Dependencies: %s", strings.Join(deps, ", ")))
				} else {
					lines = append(lines, "  Dependencies: none")
				}
			}
			lines = append(lines, "")
		}

		// Waiting tasks
		if len(waiting) > 0 {
			lines = append(lines, "### Waiting (deps incomplete)")
			for _, task := range waiting {
				priority := priorityLabel(task.Priority)
				lines = append(lines, fmt.Sprintf("- **%s %s** (`%s`) - %s", priority, task.Title, task.ID, task.Status))
				if len(task.DependsOn) > 0 {
					var incomplete []string
					for _, d := range task.DependsOn {
						if d.Status != "completed" {
							incomplete = append(incomplete, fmt.Sprintf("%s (%s)", d.Title, d.Status))
						}
					}
					if len(incomplete) > 0 {
						lines = append(lines, fmt.Sprintf("  Waiting on: %s", strings.Join(incomplete, ", ")))
					}
				}
			}
			lines = append(lines, "")
		}

		// Blocked tasks
		if len(blocked) > 0 {
			lines = append(lines, "### Blocked")
			for _, task := range blocked {
				priority := priorityLabel(task.Priority)
				lines = append(lines, fmt.Sprintf("- **%s %s** (`%s`) - %s", priority, task.Title, task.ID, task.Status))
				blockedBy := task.BlockedBy
				if blockedBy == "" {
					blockedBy = "circular dependency or blocked deps"
				}
				lines = append(lines, fmt.Sprintf("  Blocked by: %s", blockedBy))
			}
			lines = append(lines, "")
		}

		// Cycles warning
		if len(resp.Cycles) > 0 {
			lines = append(lines, "### Circular Dependencies Detected")
			for _, cycle := range resp.Cycles {
				lines = append(lines, fmt.Sprintf("- Cycle: %s", strings.Join(cycle.Cycle, " -> ")))
			}
			lines = append(lines, "")
		}

		if len(filtered) == 0 {
			lines = append(lines, "*No tasks found matching criteria.*")
		}

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// brain_task_next
// =============================================================================

func registerBrainTaskNext(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "brain_task_next",
		Description: `Get the next actionable task (highest priority ready task) with full content.

Use this to quickly find what to work on next. Returns the complete task including:
- Full markdown content for implementation
- User's original request for validation
- Dependency information

If no ready tasks, shows current queue state.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"project": {Type: "string", Description: "Override auto-detected project"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		proj := ResolveProject(args)

		var nextResp struct {
			Task *struct {
				ID             string   `json:"id"`
				Path           string   `json:"path"`
				Title          string   `json:"title"`
				Status         string   `json:"status"`
				Priority       string   `json:"priority"`
				Classification string   `json:"classification"`
				ResolvedDeps   []string `json:"resolved_deps"`
				WaitingOn      []string `json:"waiting_on"`
				BlockedBy      []string `json:"blocked_by"`
			} `json:"task"`
			Message string `json:"message"`
		}
		if err := client.Request(ctx, "GET", "/tasks/"+url.PathEscape(proj)+"/next", nil, nil, &nextResp); err != nil {
			return "", err
		}

		// No ready task available
		if nextResp.Task == nil {
			// Get stats for context
			var statsResp struct {
				Tasks []struct {
					Classification string `json:"classification"`
					Status         string `json:"status"`
				} `json:"tasks"`
				Stats *struct {
					Ready     int `json:"ready"`
					Waiting   int `json:"waiting"`
					Blocked   int `json:"blocked"`
					Completed int `json:"completed"`
					Total     int `json:"total"`
				} `json:"stats"`
			}
			if err := client.Request(ctx, "GET", "/tasks/"+url.PathEscape(proj), nil, nil, &statsResp); err != nil {
				return "", err
			}

			waiting, blocked, completed := 0, 0, 0
			if statsResp.Stats != nil {
				waiting = statsResp.Stats.Waiting
				blocked = statsResp.Stats.Blocked
				completed = statsResp.Stats.Completed
			} else {
				for _, t := range statsResp.Tasks {
					switch t.Classification {
					case "waiting":
						waiting++
					case "blocked":
						blocked++
					}
					if t.Status == "completed" {
						completed++
					}
				}
			}

			return fmt.Sprintf(`No ready tasks available.

Current state:
- %d tasks waiting on dependencies
- %d tasks blocked
- %d tasks completed

Use brain_tasks to see the full task list and dependency status.`, waiting, blocked, completed), nil
		}

		task := nextResp.Task

		// Get full entry content
		var entry struct {
			ID                  string   `json:"id"`
			Path                string   `json:"path"`
			Title               string   `json:"title"`
			Type                string   `json:"type"`
			Status              string   `json:"status"`
			Content             string   `json:"content"`
			Tags                []string `json:"tags"`
			UserOriginalRequest string   `json:"user_original_request"`
		}
		if err := client.Request(ctx, "GET", "/entries/"+task.Path, nil, nil, &entry); err != nil {
			return "", err
		}

		priority := priorityLabelUpper(task.Priority)
		depsCount := len(task.ResolvedDeps)

		lines := []string{
			fmt.Sprintf("## Next Task: %s", entry.Title),
			"",
			fmt.Sprintf("**ID:** %s", entry.ID),
			fmt.Sprintf("**Path:** %s", entry.Path),
			fmt.Sprintf("**Priority:** %s", priority),
			fmt.Sprintf("**Status:** %s", entry.Status),
			"",
		}

		// User's original request for validation
		if entry.UserOriginalRequest != "" {
			lines = append(lines, "### User Original Request")
			quoted := "> " + strings.ReplaceAll(entry.UserOriginalRequest, "\n", "\n> ")
			lines = append(lines, quoted)
			lines = append(lines, "")
		}

		lines = append(lines, "### Quick Context")
		if depsCount > 0 {
			lines = append(lines, fmt.Sprintf("- %d dependencies (all satisfied)", depsCount))
		} else {
			lines = append(lines, "- No dependencies")
		}
		lines = append(lines, "")
		lines = append(lines, "---")
		lines = append(lines, "")
		lines = append(lines, entry.Content)

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// brain_task_get
// =============================================================================

func registerBrainTaskGet(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "brain_task_get",
		Description: `Get a specific task by ID with full dependency info, dependents list, and content.

Use this to get detailed information about a specific task including:
- Full markdown content for implementation
- User's original request for validation  
- Dependencies (what this task needs)
- Dependents (what needs this task)
- Classification (ready/waiting/blocked)`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"taskId":  {Type: "string", Description: "Task ID (8-char alphanumeric) or title"},
				"project": {Type: "string", Description: "Override auto-detected project"},
			},
			Required: []string{"taskId"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		taskID := StringArg(args, "taskId", "")
		if taskID == "" {
			return "Please provide a task ID or title", nil
		}

		proj := ResolveProject(args)

		// Get all tasks to find the specific task and calculate dependents
		var tasksResp struct {
			Tasks []resolvedTaskWithDeps `json:"tasks"`
			Count int                    `json:"count"`
		}
		if err := client.Request(ctx, "GET", "/tasks/"+url.PathEscape(proj), nil, nil, &tasksResp); err != nil {
			return "", err
		}

		// Find the task by ID or title
		taskIDLower := strings.ToLower(taskID)
		var task *resolvedTaskWithDeps
		for i := range tasksResp.Tasks {
			t := &tasksResp.Tasks[i]
			if strings.ToLower(t.ID) == taskIDLower || strings.ToLower(t.Title) == taskIDLower {
				task = t
				break
			}
		}

		if task == nil {
			// Try partial match
			var partialMatches []resolvedTaskWithDeps
			for _, t := range tasksResp.Tasks {
				if strings.Contains(strings.ToLower(t.Title), taskIDLower) ||
					strings.Contains(strings.ToLower(t.ID), taskIDLower) {
					partialMatches = append(partialMatches, t)
				}
			}

			if len(partialMatches) > 0 {
				limit := 5
				if len(partialMatches) < limit {
					limit = len(partialMatches)
				}
				suggestions := make([]string, limit)
				for i := 0; i < limit; i++ {
					suggestions[i] = fmt.Sprintf("- %s (ID: %s)", partialMatches[i].Title, partialMatches[i].ID)
				}
				return fmt.Sprintf("Task not found: %q\n\nDid you mean:\n%s", taskID, strings.Join(suggestions, "\n")), nil
			}

			return fmt.Sprintf("Task not found: %q\n\nUse brain_tasks to list all tasks.", taskID), nil
		}

		// Calculate dependents - tasks that have this task in their resolved_deps
		type depRef struct {
			ID     string
			Title  string
			Status string
		}
		var dependents []depRef
		for _, t := range tasksResp.Tasks {
			for _, dep := range t.ResolvedDeps {
				if dep == task.ID {
					dependents = append(dependents, depRef{ID: t.ID, Title: t.Title, Status: t.Status})
					break
				}
			}
		}

		// Get full entry content
		var entry struct {
			ID                  string   `json:"id"`
			Path                string   `json:"path"`
			Title               string   `json:"title"`
			Type                string   `json:"type"`
			Status              string   `json:"status"`
			Content             string   `json:"content"`
			Tags                []string `json:"tags"`
			UserOriginalRequest string   `json:"user_original_request"`
		}
		if err := client.Request(ctx, "GET", "/entries/"+task.Path, nil, nil, &entry); err != nil {
			return "", err
		}

		priority := priorityLabelUpper(task.Priority)

		lines := []string{
			fmt.Sprintf("## %s", entry.Title),
			"",
			fmt.Sprintf("**ID:** %s", entry.ID),
			fmt.Sprintf("**Path:** %s", entry.Path),
			fmt.Sprintf("**Priority:** %s", priority),
			fmt.Sprintf("**Status:** %s", entry.Status),
			fmt.Sprintf("**Classification:** %s", task.Classification),
			"",
		}

		// Dependencies section
		lines = append(lines, "### Dependencies (what this task needs)")
		if len(task.DependsOn) > 0 {
			for _, dep := range task.DependsOn {
				emoji := statusEmoji(dep.Status)
				lines = append(lines, fmt.Sprintf("- %s **%s** (%s) - %s", emoji, dep.Title, dep.ID, dep.Status))
			}
		} else {
			lines = append(lines, "*No dependencies*")
		}
		lines = append(lines, "")

		// Dependents section
		lines = append(lines, "### Dependents (what needs this task)")
		if len(dependents) > 0 {
			for _, dep := range dependents {
				emoji := statusEmoji(dep.Status)
				lines = append(lines, fmt.Sprintf("- %s **%s** (%s) - %s", emoji, dep.Title, dep.ID, dep.Status))
			}
		} else {
			lines = append(lines, "*No tasks depend on this one*")
		}
		lines = append(lines, "")

		// User's original request
		if entry.UserOriginalRequest != "" {
			lines = append(lines, "### User Original Request")
			quoted := "> " + strings.ReplaceAll(entry.UserOriginalRequest, "\n", "\n> ")
			lines = append(lines, quoted)
			lines = append(lines, "")
		}

		lines = append(lines, "---")
		lines = append(lines, "")
		lines = append(lines, entry.Content)

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// brain_task_metadata
// =============================================================================

func registerBrainTaskMetadata(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "brain_task_metadata",
		Description: `Get execution metadata for a task — fields NOT included in brain_task_get.

Returns structured JSON with:
- **Execution config:** agent, model, direct_prompt, target_workdir, resolved_workdir, git_branch, git_remote
- **Feature grouping:** feature_id, feature_priority, feature_depends_on
- **Raw dependencies:** depends_on (IDs), resolved_deps, unresolved_deps, blocked_by, blocked_by_reason, waiting_on, in_cycle
- **Timestamps:** created, modified
- **Tags and sessions:** tags[], session_ids[]

Use this when you need to know HOW a task should be executed (which agent, model, workdir, prompt)
or to inspect its dependency graph details. Complements brain_task_get which returns content and high-level status.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"taskId":  {Type: "string", Description: "Task ID (8-char alphanumeric) or title"},
				"project": {Type: "string", Description: "Override auto-detected project"},
			},
			Required: []string{"taskId"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		taskID := StringArg(args, "taskId", "")
		if taskID == "" {
			return "Please provide a task ID or title", nil
		}

		proj := ResolveProject(args)

		var tasksResp struct {
			Tasks []fullTask `json:"tasks"`
			Count int        `json:"count"`
		}
		if err := client.Request(ctx, "GET", "/tasks/"+url.PathEscape(proj), nil, nil, &tasksResp); err != nil {
			return "", err
		}

		taskIDLower := strings.ToLower(taskID)
		var task *fullTask
		for i := range tasksResp.Tasks {
			t := &tasksResp.Tasks[i]
			if strings.ToLower(t.ID) == taskIDLower || strings.ToLower(t.Title) == taskIDLower {
				task = t
				break
			}
		}

		if task == nil {
			var partialMatches []fullTask
			for _, t := range tasksResp.Tasks {
				if strings.Contains(strings.ToLower(t.Title), taskIDLower) ||
					strings.Contains(strings.ToLower(t.ID), taskIDLower) {
					partialMatches = append(partialMatches, t)
				}
			}

			if len(partialMatches) > 0 {
				limit := 5
				if len(partialMatches) < limit {
					limit = len(partialMatches)
				}
				suggestions := make([]string, limit)
				for i := 0; i < limit; i++ {
					suggestions[i] = fmt.Sprintf("- %s (ID: %s)", partialMatches[i].Title, partialMatches[i].ID)
				}
				return fmt.Sprintf("Task not found: %q\n\nDid you mean:\n%s", taskID, strings.Join(suggestions, "\n")), nil
			}

			return fmt.Sprintf("Task not found: %q\n\nUse brain_tasks to list all tasks.", taskID), nil
		}

		// Build metadata-only response (no content body)
		metadata := map[string]any{
			"id":             task.ID,
			"title":          task.Title,
			"path":           task.Path,
			"status":         task.Status,
			"priority":       task.Priority,
			"classification": task.Classification,

			// Execution config
			"execution": map[string]any{
				"agent":            task.Agent,
				"model":            task.Model,
				"direct_prompt":    task.DirectPrompt,
				"target_workdir":   task.TargetWorkdir,
				"workdir":          task.Workdir,
				"resolved_workdir": task.ResolvedWorkdir,
				"git_branch":       task.GitBranch,
				"git_remote":       task.GitRemote,
			},

			// Dependencies (raw IDs)
			"dependencies": map[string]any{
				"depends_on":        emptyIfNil(task.RawDependsOn),
				"resolved_deps":     emptyIfNil(task.ResolvedDeps),
				"unresolved_deps":   emptyIfNil(task.UnresolvedDeps),
				"blocked_by":        emptyIfNil(task.BlockedBy),
				"blocked_by_reason": nilIfEmpty(task.BlockedByReason),
				"waiting_on":        emptyIfNil(task.WaitingOn),
				"in_cycle":          task.InCycle,
			},

			// Metadata
			"tags":                  emptyIfNil(task.Tags),
			"created":               task.Created,
			"modified":              nilIfEmpty(task.Modified),
			"session_ids":           emptyIfNil(task.SessionIDs),
			"user_original_request": nilIfEmpty(task.UserOriginalRequest),
		}

		// Feature grouping
		if task.FeatureID != "" {
			metadata["feature"] = map[string]any{
				"id":         task.FeatureID,
				"priority":   nilIfEmpty(task.FeaturePriority),
				"depends_on": emptyIfNil(task.FeatureDependsOn),
			}
		} else {
			metadata["feature"] = nil
		}

		data, err := json.MarshalIndent(metadata, "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal metadata: %w", err)
		}
		return string(data), nil
	})
}

// =============================================================================
// brain_tasks_status
// =============================================================================

func registerBrainTasksStatus(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "brain_tasks_status",
		Description: `Get status of multiple tasks by ID, with optional blocking wait.

Use cases:
- Check if spawned subtasks are complete before continuing
- Wait for dependent tasks to finish before starting next phase
- Monitor multiple tasks from an orchestrator agent

Parameters:
- taskIds: Array of task IDs (8-char alphanumeric) to check
- waitFor: Optional. "completed" (default) waits until all tasks completed/validated.
           "any" returns as soon as any task status changes.
           Omit for immediate response without waiting.
- timeout: Max wait time in milliseconds (default: 60000, max: 300000)
- project: Override auto-detected project

Example - immediate check:
  brain_tasks_status({ taskIds: ["abc12def", "xyz98765"] })

Example - wait for completion:
  brain_tasks_status({ taskIds: ["abc12def"], waitFor: "completed", timeout: 120000 })`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"taskIds": {Type: "array", Items: &Property{Type: "string"}, Description: "Array of task IDs (8-char alphanumeric) to check"},
				"waitFor": {Type: "string", Enum: []string{"completed", "any"}, Description: "Wait condition: 'completed' waits for all done, 'any' waits for any change"},
				"timeout": {Type: "number", Description: "Max wait time in milliseconds (default: 60000, max: 300000)"},
				"project": {Type: "string", Description: "Override auto-detected project"},
			},
			Required: []string{"taskIds"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		taskIDs := StringSliceArg(args, "taskIds")
		if len(taskIDs) == 0 {
			return "Please provide at least one task ID", nil
		}

		proj := ResolveProject(args)
		waitFor := StringArg(args, "waitFor", "")
		timeout := IntArg(args, "timeout", 60000)
		if timeout > 300000 {
			timeout = 300000
		}

		body := map[string]any{
			"taskIds": taskIDs,
		}
		if waitFor != "" {
			body["waitFor"] = waitFor
			body["timeout"] = timeout
		}

		var resp struct {
			Tasks []struct {
				ID             string `json:"id"`
				Title          string `json:"title"`
				Status         string `json:"status"`
				Priority       string `json:"priority"`
				Classification string `json:"classification"`
			} `json:"tasks"`
			NotFound []string `json:"notFound"`
			Changed  bool     `json:"changed"`
			TimedOut bool     `json:"timedOut"`
		}
		if err := client.Request(ctx, "POST", "/tasks/"+url.PathEscape(proj)+"/status", body, nil, &resp); err != nil {
			return "", err
		}

		lines := []string{
			"## Task Status Check",
			"",
		}

		if resp.TimedOut {
			lines = append(lines, "**Status:** Timed out waiting for condition")
			lines = append(lines, "")
		} else if resp.Changed {
			lines = append(lines, "**Status:** Condition met")
			lines = append(lines, "")
		} else {
			lines = append(lines, "**Status:** Immediate check (no wait)")
			lines = append(lines, "")
		}

		// Show task statuses
		if len(resp.Tasks) > 0 {
			lines = append(lines, "### Tasks")
			for _, task := range resp.Tasks {
				emoji := statusEmojiExtended(task.Status)
				priority := priorityLabel(task.Priority)
				lines = append(lines, fmt.Sprintf("- %s **%s %s** (`%s`) - %s", emoji, priority, task.Title, task.ID, task.Status))
			}
			lines = append(lines, "")
		}

		// Show not found tasks
		if len(resp.NotFound) > 0 {
			lines = append(lines, "### Not Found")
			for _, id := range resp.NotFound {
				lines = append(lines, fmt.Sprintf("- `%s` - task not found", id))
			}
			lines = append(lines, "")
		}

		// Summary
		completed := 0
		for _, t := range resp.Tasks {
			if t.Status == "completed" || t.Status == "validated" {
				completed++
			}
		}
		lines = append(lines, fmt.Sprintf("**Summary:** %d/%d tasks completed", completed, len(resp.Tasks)))

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// brain_task_trigger
// =============================================================================

func registerBrainTaskTrigger(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "brain_task_trigger",
		Description: "Manually trigger a scheduled task and its downstream dependents.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"taskId":  {Type: "string", Description: "Task ID (8-char alphanumeric)"},
				"project": {Type: "string", Description: "Override auto-detected project"},
			},
			Required: []string{"taskId"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		proj := ResolveProject(args)
		taskID := StringArg(args, "taskId", "")

		var resp struct {
			TaskID        string `json:"taskId"`
			Run           any    `json:"run"`
			Pipeline      []any  `json:"pipeline"`
			PipelineCount int    `json:"pipelineCount"`
			Message       string `json:"message"`
		}
		err := client.Request(ctx, "POST", "/tasks/"+url.PathEscape(proj)+"/"+url.PathEscape(taskID)+"/trigger", nil, nil, &resp)
		if err != nil {
			result := map[string]any{
				"operation": "task_trigger",
				"project":   proj,
				"error":     err.Error(),
			}
			data, _ := json.MarshalIndent(result, "", "  ")
			return string(data), nil
		}

		result := map[string]any{
			"operation": "task_trigger",
			"project":   proj,
			"data":      resp,
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		return string(data), nil
	})
}

// =============================================================================
// brain_feature_review_enable
// =============================================================================

func registerBrainFeatureReviewEnable(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "brain_feature_review_enable",
		Description: "Enable Feature Code Review for a feature. Creates a one-shot review task that triggers when all tasks in the feature are completed. The review validates that the implementation matches the original requirements.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"project":    {Type: "string", Description: "Project containing the feature"},
				"feature_id": {Type: "string", Description: "Feature ID to review"},
			},
			Required: []string{"project", "feature_id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		project := StringArg(args, "project", "")
		featureID := StringArg(args, "feature_id", "")

		scope := map[string]string{
			"type":       "feature",
			"project":    project,
			"feature_id": featureID,
		}

		var resp struct {
			ID    string `json:"id"`
			Path  string `json:"path"`
			Title string `json:"title"`
		}
		err := client.Request(ctx, "POST", "/monitors", map[string]any{
			"templateId": "feature-review",
			"scope":      scope,
		}, nil, &resp)

		if err != nil {
			msg := err.Error()
			if strings.Contains(msg, "409") || strings.Contains(strings.ToLower(msg), "conflict") {
				return fmt.Sprintf("Feature Code Review is already enabled for feature %q. Use brain_feature_review_disable first to reset it.", featureID), nil
			}
			return "", err
		}

		return fmt.Sprintf("Feature Code Review enabled for feature %q:\n- **Task ID:** %s\n- **Title:** %s\nThe review will trigger automatically when all feature tasks are completed.",
			featureID, resp.ID, resp.Title), nil
	})
}

// =============================================================================
// brain_feature_review_disable
// =============================================================================

func registerBrainFeatureReviewDisable(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "brain_feature_review_disable",
		Description: "Disable Feature Code Review for a feature. Permanently removes the review task. Can be re-enabled later with brain_feature_review_enable.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"project":    {Type: "string", Description: "Project containing the feature"},
				"feature_id": {Type: "string", Description: "Feature ID"},
			},
			Required: []string{"project", "feature_id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		project := StringArg(args, "project", "")
		featureID := StringArg(args, "feature_id", "")

		scope := map[string]string{
			"type":       "feature",
			"project":    project,
			"feature_id": featureID,
		}

		var resp struct {
			Message string `json:"message"`
			TaskID  string `json:"taskId"`
			Path    string `json:"path"`
		}
		err := client.Request(ctx, "DELETE", "/monitors/by-scope", map[string]any{
			"templateId": "feature-review",
			"scope":      scope,
		}, nil, &resp)

		if err != nil {
			msg := err.Error()
			if strings.Contains(msg, "404") || strings.Contains(strings.ToLower(msg), "not found") {
				return fmt.Sprintf("Feature Code Review is not currently enabled for feature %q. Nothing to disable.", featureID), nil
			}
			return "", err
		}

		return fmt.Sprintf("Feature Code Review disabled for feature %q (task %s deleted).", featureID, resp.TaskID), nil
	})
}

// =============================================================================
// brain_blocked_inspector_enable
// =============================================================================

func registerBrainBlockedInspectorEnable(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "brain_blocked_inspector_enable",
		Description: "Enable Blocked Task Inspector for a feature. Creates a recurring scheduled task that periodically checks for blocked tasks and attempts to unblock them by analyzing dependencies, suggesting fixes, or escalating.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"project":    {Type: "string", Description: "Project containing the feature"},
				"feature_id": {Type: "string", Description: "Feature ID to inspect"},
				"schedule":   {Type: "string", Description: "Cron schedule override (default: every 30 minutes)"},
			},
			Required: []string{"project", "feature_id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		project := StringArg(args, "project", "")
		featureID := StringArg(args, "feature_id", "")

		scope := map[string]string{
			"type":       "feature",
			"project":    project,
			"feature_id": featureID,
		}

		body := map[string]any{
			"templateId": "blocked-inspector",
			"scope":      scope,
		}
		if schedule := StringArg(args, "schedule", ""); schedule != "" {
			body["schedule"] = schedule
		}

		var resp struct {
			ID    string `json:"id"`
			Path  string `json:"path"`
			Title string `json:"title"`
		}
		err := client.Request(ctx, "POST", "/monitors", body, nil, &resp)

		if err != nil {
			msg := err.Error()
			if strings.Contains(msg, "409") || strings.Contains(strings.ToLower(msg), "conflict") {
				return fmt.Sprintf("Blocked Task Inspector is already enabled for feature %q. Use brain_blocked_inspector_disable first to reset it.", featureID), nil
			}
			return "", err
		}

		return fmt.Sprintf("Blocked Task Inspector enabled for feature %q:\n- **Task ID:** %s\n- **Title:** %s\nThe inspector will periodically check for blocked tasks and attempt to unblock them.",
			featureID, resp.ID, resp.Title), nil
	})
}

// =============================================================================
// brain_blocked_inspector_disable
// =============================================================================

func registerBrainBlockedInspectorDisable(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "brain_blocked_inspector_disable",
		Description: "Disable Blocked Task Inspector for a feature. Permanently removes the inspector task. Can be re-enabled later with brain_blocked_inspector_enable.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"project":    {Type: "string", Description: "Project containing the feature"},
				"feature_id": {Type: "string", Description: "Feature ID"},
			},
			Required: []string{"project", "feature_id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		project := StringArg(args, "project", "")
		featureID := StringArg(args, "feature_id", "")

		scope := map[string]string{
			"type":       "feature",
			"project":    project,
			"feature_id": featureID,
		}

		var resp struct {
			Message string `json:"message"`
			TaskID  string `json:"taskId"`
			Path    string `json:"path"`
		}
		err := client.Request(ctx, "DELETE", "/monitors/by-scope", map[string]any{
			"templateId": "blocked-inspector",
			"scope":      scope,
		}, nil, &resp)

		if err != nil {
			msg := err.Error()
			if strings.Contains(msg, "404") || strings.Contains(strings.ToLower(msg), "not found") {
				return fmt.Sprintf("Blocked Task Inspector is not currently enabled for feature %q. Nothing to disable.", featureID), nil
			}
			return "", err
		}

		return fmt.Sprintf("Blocked Task Inspector disabled for feature %q (task %s deleted).", featureID, resp.TaskID), nil
	})
}

// =============================================================================
// Helper types
// =============================================================================

// resolvedTaskWithDeps is used by brain_task_get to find tasks and compute dependents.
type resolvedTaskWithDeps struct {
	ID             string   `json:"id"`
	Title          string   `json:"title"`
	Path           string   `json:"path"`
	Status         string   `json:"status"`
	Priority       string   `json:"priority"`
	Classification string   `json:"classification"`
	ResolvedDeps   []string `json:"resolved_deps"`
	WaitingOn      []string `json:"waiting_on"`
	BlockedBy      []string `json:"blocked_by"`
	DependsOn      []struct {
		ID     string `json:"id"`
		Title  string `json:"title"`
		Status string `json:"status"`
	} `json:"dependsOn"`
}

// fullTask is used by brain_task_metadata for the complete task representation.
type fullTask struct {
	ID                  string   `json:"id"`
	Title               string   `json:"title"`
	Path                string   `json:"path"`
	Status              string   `json:"status"`
	Priority            string   `json:"priority"`
	Classification      string   `json:"classification"`
	RawDependsOn        []string `json:"depends_on"`
	ResolvedDeps        []string `json:"resolved_deps"`
	UnresolvedDeps      []string `json:"unresolved_deps"`
	BlockedBy           []string `json:"blocked_by"`
	BlockedByReason     string   `json:"blocked_by_reason"`
	WaitingOn           []string `json:"waiting_on"`
	InCycle             bool     `json:"in_cycle"`
	Tags                []string `json:"tags"`
	Created             string   `json:"created"`
	Modified            string   `json:"modified"`
	TargetWorkdir       string   `json:"target_workdir"`
	Workdir             string   `json:"workdir"`
	ResolvedWorkdir     string   `json:"resolved_workdir"`
	GitBranch           string   `json:"git_branch"`
	GitRemote           string   `json:"git_remote"`
	Agent               string   `json:"agent"`
	Model               string   `json:"model"`
	DirectPrompt        string   `json:"direct_prompt"`
	FeatureID           string   `json:"feature_id"`
	FeaturePriority     string   `json:"feature_priority"`
	FeatureDependsOn    []string `json:"feature_depends_on"`
	SessionIDs          []string `json:"session_ids"`
	UserOriginalRequest string   `json:"user_original_request"`
}

// =============================================================================
// Helper functions
// =============================================================================

// priorityLabel returns a bracketed priority label like [HIGH], [MED], [LOW].
func priorityLabel(p string) string {
	switch p {
	case "high":
		return "[HIGH]"
	case "medium":
		return "[MED]"
	default:
		return "[LOW]"
	}
}

// priorityLabelUpper returns an uppercase priority label like HIGH, MEDIUM, LOW.
func priorityLabelUpper(p string) string {
	switch p {
	case "high":
		return "HIGH"
	case "medium":
		return "MEDIUM"
	default:
		return "LOW"
	}
}

// statusEmoji returns a status indicator emoji.
func statusEmoji(status string) string {
	switch status {
	case "completed":
		return "✓"
	case "in_progress":
		return "⋯"
	default:
		return "○"
	}
}

// statusEmojiExtended returns a status indicator emoji with blocked support.
func statusEmojiExtended(status string) string {
	switch status {
	case "completed", "validated":
		return "✓"
	case "in_progress":
		return "⋯"
	case "blocked":
		return "✗"
	default:
		return "○"
	}
}

// emptyIfNil returns an empty slice if the input is nil, otherwise returns the input.
func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// nilIfEmpty returns nil if the string is empty, otherwise returns the string.
func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

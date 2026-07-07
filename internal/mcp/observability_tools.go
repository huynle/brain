package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// RegisterObservabilityTools registers read-only observability MCP tools for
// execution and automation state.
func RegisterObservabilityTools(s *Server, client *APIClient) {
	registerBrainTaskLogs(s, client)
	registerBrainTaskDispatchLease(s, client)
	registerBrainTaskPlacementReasons(s, client)
	registerBrainEventsRecent(s, client)
	registerBrainAutomationRuns(s, client)
	registerBrainAutomationRunGet(s, client)
	registerBrainSchedulerStatus(s, client)
}

func registerBrainTaskLogs(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "task_logs",
		Description: "Get task execution logs. Returns log entries from a running or completed task.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"project": {Type: "string", Description: "Project ID (defaults to current context)"},
				"task_id": {Type: "string", Description: "Task ID"},
				"limit":   {Type: "number", Description: "Maximum log entries to return (optional)"},
			},
			Required: []string{"task_id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProject(args)
		if projectID == "" {
			return "", fmt.Errorf("project is required")
		}
		taskID := StringArgAlias(args, "", "task_id", "taskId")
		if taskID == "" {
			return "", fmt.Errorf("task_id is required")
		}

		path := fmt.Sprintf("/tasks/%s/%s/logs", url.PathEscape(projectID), url.PathEscape(taskID))
		query := make(map[string]string)
		if limit := IntArg(args, "limit", 0); limit > 0 {
			query["limit"] = fmt.Sprintf("%d", limit)
		}

		var resp types.LogQueryResponse
		if err := client.Request(ctx, http.MethodGet, path, nil, query, &resp); err != nil {
			return "", err
		}

		return formatTaskLogs(projectID, taskID, resp), nil
	})
}

func registerBrainTaskDispatchLease(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "task_dispatch_lease",
		Description: "Get task dispatch/claim information. Shows which runner claimed the task and lease state.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"project": {Type: "string", Description: "Project ID (defaults to current context)"},
				"task_id": {Type: "string", Description: "Task ID"},
			},
			Required: []string{"task_id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProject(args)
		if projectID == "" {
			return "", fmt.Errorf("project is required")
		}
		taskID := StringArgAlias(args, "", "task_id", "taskId")
		if taskID == "" {
			return "", fmt.Errorf("task_id is required")
		}

		path := fmt.Sprintf("/tasks/%s/%s/dispatch-lease", url.PathEscape(projectID), url.PathEscape(taskID))

		var resp types.DispatchLease
		if err := client.Request(ctx, http.MethodGet, path, nil, nil, &resp); err != nil {
			return "", err
		}

		return formatDispatchLease(resp), nil
	})
}

func registerBrainTaskPlacementReasons(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "task_placement_reasons",
		Description: "Get task placement decision history. Shows why runners accepted or rejected this task.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"project": {Type: "string", Description: "Project ID (defaults to current context)"},
				"task_id": {Type: "string", Description: "Task ID"},
			},
			Required: []string{"task_id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProject(args)
		if projectID == "" {
			return "", fmt.Errorf("project is required")
		}
		taskID := StringArgAlias(args, "", "task_id", "taskId")
		if taskID == "" {
			return "", fmt.Errorf("task_id is required")
		}

		path := fmt.Sprintf("/tasks/%s/%s/placement-reasons", url.PathEscape(projectID), url.PathEscape(taskID))

		var resp types.PlacementReasonListResponse
		if err := client.Request(ctx, http.MethodGet, path, nil, nil, &resp); err != nil {
			return "", err
		}

		return formatPlacementReasons(projectID, taskID, resp), nil
	})
}

func registerBrainEventsRecent(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "events_recent",
		Description: "Get recent system events. Returns task lifecycle, runner, and automation events.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"limit":      {Type: "number", Description: "Maximum events to return (default: 100, max: 1000)"},
				"type":       {Type: "string", Description: "Event type filter (e.g. 'task.started', 'task.*')"},
				"project_id": {Type: "string", Description: "Filter events by project ID"},
				"feature_id": {Type: "string", Description: "Filter events by feature ID"},
				"source":     {Type: "string", Description: "Event source filter ('runner' or 'api')"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		query := make(map[string]string)
		if limit := IntArg(args, "limit", 0); limit > 0 {
			query["limit"] = fmt.Sprintf("%d", limit)
		}
		if eventType := StringArg(args, "type", ""); eventType != "" {
			query["type"] = eventType
		}
		if projectID := StringArg(args, "project_id", ""); projectID != "" {
			query["project_id"] = projectID
		}
		if featureID := StringArg(args, "feature_id", ""); featureID != "" {
			query["feature_id"] = featureID
		}
		if source := StringArg(args, "source", ""); source != "" {
			query["source"] = source
		}

		var resp struct {
			Events []types.Event `json:"events"`
			Count  int           `json:"count"`
		}
		if err := client.Request(ctx, http.MethodGet, "/events/recent", nil, query, &resp); err != nil {
			return "", err
		}

		return formatRecentEvents(resp.Events), nil
	})
}

func registerBrainAutomationRuns(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "automation_runs",
		Description: "List automation run history. Shows executed automation instances with status and timestamps.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"project":       {Type: "string", Description: "Filter by project ID"},
				"automation_id": {Type: "string", Description: "Filter by automation ID"},
				"status":        {Type: "string", Description: "Filter by status (pending/active/completed/failed)"},
				"limit":         {Type: "number", Description: "Maximum runs to return (default: 100)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		query := make(map[string]string)
		if project := StringArg(args, "project", ""); project != "" {
			query["project"] = project
		}
		if automationID := StringArg(args, "automation_id", ""); automationID != "" {
			query["automation_id"] = automationID
		}
		if status := StringArg(args, "status", ""); status != "" {
			query["status"] = status
		}
		if limit := IntArg(args, "limit", 0); limit > 0 {
			query["limit"] = fmt.Sprintf("%d", limit)
		}

		var resp types.ListEntriesResponse
		if err := client.Request(ctx, http.MethodGet, "/automation-runs", nil, query, &resp); err != nil {
			return "", err
		}

		return formatAutomationRuns(resp), nil
	})
}

func registerBrainAutomationRunGet(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "automation_run_get",
		Description: "Get detailed automation run information. Shows full run history, trigger details, and outcome.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"run_id": {Type: "string", Description: "Automation run ID"},
			},
			Required: []string{"run_id"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		runID := StringArgAlias(args, "", "run_id", "runId")
		if runID == "" {
			return "", fmt.Errorf("run_id is required")
		}

		var resp types.BrainEntry
		if err := client.Request(ctx, http.MethodGet, "/automation-runs/"+url.PathEscape(runID), nil, nil, &resp); err != nil {
			return "", err
		}

		return formatAutomationRun(resp), nil
	})
}

func registerBrainSchedulerStatus(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "scheduler_status",
		Description: "Get scheduler state and statistics. Shows scheduling interval, tick counts, and last results.",
		InputSchema: InputSchema{
			Type:       "object",
			Properties: map[string]Property{},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		var resp types.SchedulerStatus
		if err := client.Request(ctx, http.MethodGet, "/scheduler/status", nil, nil, &resp); err != nil {
			return "", err
		}

		return formatSchedulerStatus(resp), nil
	})
}

// =============================================================================
// Formatting Functions
// =============================================================================

func formatTaskLogs(projectID, taskID string, resp types.LogQueryResponse) string {
	var b strings.Builder
	b.WriteString("## Task Logs\n\n")
	fmt.Fprintf(&b, "Project: %s\n", projectID)
	fmt.Fprintf(&b, "Task: %s\n", taskID)
	fmt.Fprintf(&b, "Total entries: %d\n\n", resp.Total)

	if len(resp.Lines) == 0 {
		b.WriteString("No log entries found.\n")
		return b.String()
	}

	for i, line := range resp.Lines {
		fmt.Fprintf(&b, "### Log Entry %d\n", i+1)
		fmt.Fprintf(&b, "- Timestamp: %s\n", line.Timestamp)
		fmt.Fprintf(&b, "- Level: %s\n", line.Level)
		if line.Content != "" {
			fmt.Fprintf(&b, "- Content: %s\n", line.Content)
		}
		b.WriteString("\n")
	}

	return b.String()
}

func formatDispatchLease(lease types.DispatchLease) string {
	var b strings.Builder
	b.WriteString("## Dispatch Lease\n\n")
	fmt.Fprintf(&b, "- Lease ID: %s\n", lease.LeaseID)
	fmt.Fprintf(&b, "- Project: %s\n", lease.ProjectID)
	fmt.Fprintf(&b, "- Task: %s\n", lease.TaskID)
	fmt.Fprintf(&b, "- State: %s\n", lease.State)
	fmt.Fprintf(&b, "- Assigned runner: %s\n", valueOrNone(lease.AssignedRunnerID))
	fmt.Fprintf(&b, "- Assigned machine: %s\n", valueOrNone(lease.AssignedMachineID))
	fmt.Fprintf(&b, "- Pushed at: %d\n", lease.PushedAt)
	if lease.AckedAt > 0 {
		fmt.Fprintf(&b, "- Acked at: %d\n", lease.AckedAt)
	}
	if lease.RejectedAt > 0 {
		fmt.Fprintf(&b, "- Rejected at: %d\n", lease.RejectedAt)
	}
	if lease.LastError != "" {
		fmt.Fprintf(&b, "- Last error: %s\n", lease.LastError)
	}
	fmt.Fprintf(&b, "- Expires at: %d\n", lease.ExpiresAt)
	return b.String()
}

func formatPlacementReasons(projectID, taskID string, resp types.PlacementReasonListResponse) string {
	var b strings.Builder
	b.WriteString("## Placement Reasons\n\n")
	fmt.Fprintf(&b, "Project: %s\n", projectID)
	fmt.Fprintf(&b, "Task: %s\n", taskID)
	fmt.Fprintf(&b, "Total decisions: %d\n\n", resp.Total)

	if len(resp.Reasons) == 0 {
		b.WriteString("No placement decisions recorded.\n")
		return b.String()
	}

	for _, reason := range resp.Reasons {
		fmt.Fprintf(&b, "### Decision\n")
		fmt.Fprintf(&b, "- Runner: %s\n", valueOrNone(reason.RunnerID))
		fmt.Fprintf(&b, "- Machine: %s\n", valueOrNone(reason.MachineID))
		fmt.Fprintf(&b, "- Decision: %s\n", reason.Decision)
		if reason.Reason != "" {
			fmt.Fprintf(&b, "- Reason: %s\n", reason.Reason)
		}
		if reason.RequiredLabels != "" {
			fmt.Fprintf(&b, "- Required labels: %s\n", reason.RequiredLabels)
		}
		if reason.RunnerLabels != "" {
			fmt.Fprintf(&b, "- Runner labels: %s\n", reason.RunnerLabels)
		}
		if reason.MissingLabels != "" {
			fmt.Fprintf(&b, "- Missing labels: %s\n", reason.MissingLabels)
		}
		fmt.Fprintf(&b, "- Created at: %d\n\n", reason.CreatedAt)
	}

	return b.String()
}

func formatRecentEvents(events []types.Event) string {
	var b strings.Builder
	b.WriteString("## Recent Events\n\n")
	fmt.Fprintf(&b, "Total: %d\n\n", len(events))

	if len(events) == 0 {
		b.WriteString("No events found.\n")
		return b.String()
	}

	for _, evt := range events {
		fmt.Fprintf(&b, "### %s\n", evt.Type)
		fmt.Fprintf(&b, "- ID: %s\n", evt.ID)
		fmt.Fprintf(&b, "- Source: %s\n", evt.Source)
		fmt.Fprintf(&b, "- Timestamp: %s\n", evt.Timestamp.Format("2006-01-02 15:04:05"))
		if evt.RunnerID != "" {
			fmt.Fprintf(&b, "- Runner: %s\n", evt.RunnerID)
		}
		if evt.ProjectID != "" {
			fmt.Fprintf(&b, "- Project: %s\n", evt.ProjectID)
		}
		if evt.TaskID != "" {
			fmt.Fprintf(&b, "- Task: %s\n", evt.TaskID)
		}
		if evt.TaskPath != "" {
			fmt.Fprintf(&b, "- Task path: %s\n", evt.TaskPath)
		}
		if evt.TaskTitle != "" {
			fmt.Fprintf(&b, "- Task title: %s\n", evt.TaskTitle)
		}
		if evt.FeatureID != "" {
			fmt.Fprintf(&b, "- Feature: %s\n", evt.FeatureID)
		}
		if evt.FromStatus != "" {
			fmt.Fprintf(&b, "- From status: %s\n", evt.FromStatus)
		}
		if evt.ToStatus != "" {
			fmt.Fprintf(&b, "- To status: %s\n", evt.ToStatus)
		}
		if len(evt.Metadata) > 0 {
			b.WriteString("- Metadata:\n")
			for k, v := range evt.Metadata {
				fmt.Fprintf(&b, "  - %s: %s\n", k, v)
			}
		}
		b.WriteString("\n")
	}

	return b.String()
}

func formatAutomationRuns(resp types.ListEntriesResponse) string {
	var b strings.Builder
	b.WriteString("## Automation Runs\n\n")
	fmt.Fprintf(&b, "Total: %d\n\n", resp.Total)

	if len(resp.Entries) == 0 {
		b.WriteString("No automation runs found.\n")
		return b.String()
	}

	for _, entry := range resp.Entries {
		fmt.Fprintf(&b, "### %s\n", entry.Title)
		fmt.Fprintf(&b, "- ID: %s\n", entry.ID)
		fmt.Fprintf(&b, "- Status: %s\n", valueOrNone(entry.Status))
		fmt.Fprintf(&b, "- Created: %s\n", valueOrNone(entry.Created))
		fmt.Fprintf(&b, "- Modified: %s\n", valueOrNone(entry.Modified))
		if len(entry.Tags) > 0 {
			fmt.Fprintf(&b, "- Tags: %s\n", strings.Join(entry.Tags, ", "))
		}
		b.WriteString("\n")
	}

	return b.String()
}

func formatAutomationRun(entry types.BrainEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Automation Run: %s\n\n", entry.Title)
	fmt.Fprintf(&b, "- ID: %s\n", entry.ID)
	fmt.Fprintf(&b, "- Path: %s\n", entry.Path)
	fmt.Fprintf(&b, "- Type: %s\n", entry.Type)
	fmt.Fprintf(&b, "- Status: %s\n", valueOrNone(entry.Status))
	fmt.Fprintf(&b, "- Created: %s\n", valueOrNone(entry.Created))
	fmt.Fprintf(&b, "- Modified: %s\n", valueOrNone(entry.Modified))
	if len(entry.Tags) > 0 {
		fmt.Fprintf(&b, "- Tags: %s\n", strings.Join(entry.Tags, ", "))
	}

	b.WriteString("\n### Content\n\n")
	b.WriteString(entry.Content)
	b.WriteString("\n")

	return b.String()
}

func formatSchedulerStatus(status types.SchedulerStatus) string {
	var b strings.Builder
	b.WriteString("## Scheduler Status\n\n")
	fmt.Fprintf(&b, "- Started: %t\n", status.Started)
	fmt.Fprintf(&b, "- Running: %t\n", status.Running)
	fmt.Fprintf(&b, "- Interval: %s\n", status.Interval)
	if status.LastTickAt != "" {
		fmt.Fprintf(&b, "- Last tick: %s\n", status.LastTickAt)
	}
	if status.LastSuccessAt != "" {
		fmt.Fprintf(&b, "- Last success: %s\n", status.LastSuccessAt)
	}
	if status.LastError != "" {
		fmt.Fprintf(&b, "- Last error: %s\n", status.LastError)
	}
	fmt.Fprintf(&b, "- Total ticks: %d\n", status.TotalTicks)
	fmt.Fprintf(&b, "- Last expired leases: %d\n", status.LastExpiredLeases)

	if len(status.LastProjectResults) > 0 {
		b.WriteString("\n### Project Results\n\n")
		for project, result := range status.LastProjectResults {
			fmt.Fprintf(&b, "**%s:**\n", project)
			fmt.Fprintf(&b, "- Considered: %d\n", result.Considered)
			fmt.Fprintf(&b, "- Dispatched: %d\n", result.Dispatched)
			fmt.Fprintf(&b, "- Skipped: %d\n", result.Skipped)
			b.WriteString("\n")
		}
	}

	return b.String()
}

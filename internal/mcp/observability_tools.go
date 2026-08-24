package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
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
		Name: "task_placement_reasons",
		Description: "Get task placement REJECTION history: the times the scheduler could not find an eligible runner for this task, and which requirements went unmet. " +
			"Acceptances are NOT recorded — recordNoCandidate is the only writer and it only ever writes 'no_candidate' — so an empty result means the scheduler has never failed to place this task, not that nothing was decided.",
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
				"type":       {Type: "string", Description: "Event type filter (e.g. 'task.started', 'task.*'). Validated against the known event types; an unrecognised value is rejected rather than silently matching nothing."},
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
			// Reject a type nothing can ever emit. An unrecognised filter used
			// to return "Found 0 events: No events found." — identical to a
			// valid filter that genuinely matched nothing, so a typo, or a
			// plausible-but-nonexistent name like "automation.run", read as
			// proof the thing never happened.
			if err := validateEventTypeFilter(eventType); err != nil {
				return "", err
			}
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
			Events   []types.Event       `json:"events"`
			Count    int                 `json:"count"`
			Coverage types.EventCoverage `json:"coverage"`
		}
		if err := client.Request(ctx, http.MethodGet, "/events/recent", nil, query, &resp); err != nil {
			return "", err
		}

		return formatRecentEvents(resp.Events, IntArg(args, "limit", 0), resp.Coverage), nil
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
				// The filter is exact-match SQL, so an advertised value the
				// server never writes returns "No automation runs found." —
				// indistinguishable from "this automation never ran".
				//
				// Real vocabulary: createRunAudit writes "queued" when a task
				// is generated and "skipped" for every non-firing evaluation
				// (paused, dedup, cooldown, max_concurrent); finalizeAutomationRun
				// (internal/api/entries.go) later writes "completed", "blocked"
				// or "cancelled" from the generated task's terminal status.
				// "pending", "active" and "failed" were advertised and are
				// never produced.
				"status": {
					Type: "string",
					Enum: []string{"queued", "skipped", "completed", "blocked", "cancelled"},
					Description: "Filter by run status. queued = a task was generated; skipped = evaluated but did not fire " +
						"(paused, dedup, cooldown, or max_concurrent); completed/blocked/cancelled = the generated task reached that terminal state.",
				},
				"limit": {Type: "number", Description: "Maximum runs to return (default: 100)"},
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

		return formatAutomationRuns(resp, IntArg(args, "limit", 0)), nil
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
	// LogQueryResponse carries Offset and Limit, and both were dropped.
	// Total is the size of the WHOLE log while Lines is a window into it,
	// so printing only Total left a caller unable to tell whether it was
	// looking at the entire log or the tail of a much longer one — and
	// unable to work out what to ask for next.
	fmt.Fprintf(&b, "Total entries: %d\n", resp.Total)
	if resp.Limit > 0 && resp.Total > len(resp.Lines) {
		end := resp.Offset + len(resp.Lines)
		fmt.Fprintf(&b, "Showing entries %d-%d of %d — pass offset=%d for the next page.\n",
			resp.Offset+1, end, resp.Total, end)
	}
	b.WriteString("\n")

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
	fmt.Fprintf(&b, "Rejections recorded: %d\n\n", resp.Total)

	if len(resp.Reasons) == 0 {
		// Only rejections are ever written here — recordNoCandidate in
		// internal/service/scheduler.go is the sole writer and its only
		// Decision value is "no_candidate". So an empty result is the GOOD
		// case, and calling it "no placement decisions recorded" made it read
		// as "nothing is known", which sent readers looking for a fault that
		// this table would never have shown them anyway.
		b.WriteString("No placement rejections recorded — the scheduler has never failed to find an eligible runner for this task.\n\n")
		b.WriteString("Note this table records rejections ONLY; a successful dispatch leaves no row here.\n")
		b.WriteString("If the task is not running, the cause is elsewhere: check runner_status(project:) for the pause dials\n")
		b.WriteString("and scheduler_status for the per-project skip reason.\n")
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

func formatRecentEvents(events []types.Event, requestedLimit int, cov types.EventCoverage) string {
	var b strings.Builder
	b.WriteString("## Recent Events\n\n")
	// Also a page size: the events endpoint returns at most `limit` rows and
	// the ring buffer holds far more.
	b.WriteString(foundLine("events", len(events), requestedLimit, 100))
	b.WriteString("\n")

	if len(events) == 0 {
		b.WriteString("No events matched.\n")
		// An empty result is only as strong as the window it was drawn from,
		// and this window is small, volatile, and mostly poll heartbeats.
		if window := formatEventCoverage(cov); window != "" {
			b.WriteString("\n" + window + "\n")
		}
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

func formatAutomationRuns(resp types.ListEntriesResponse, requestedLimit int) string {
	var b strings.Builder
	b.WriteString("## Automation Runs\n\n")
	// ListEntriesResponse.Total is len(entries) for the returned page, not a
	// grand total. On a store that is ~95% automation_run entries, printing
	// it as "Total" is badly misleading — the same defect foundLine was
	// written for on list and search.
	b.WriteString(foundLine("runs", resp.Total, requestedLimit, 100))
	if resp.Truncated {
		b.WriteString("_Filtered by automation_id: the scan window was exhausted before filling the page. " +
			"Older runs for this automation may exist beyond it._\n")
	}
	b.WriteString("\n")

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

// formatSkipBreakdown renders why a pass skipped what it skipped. "Skipped: 4"
// alone cannot distinguish work being held by a pause switch from work no
// runner will ever accept from work already dispatched by an earlier pass —
// three states that call for three different responses. The scheduler knows
// which is which at the moment it skips, so say so.
//
// Counters that are zero are left out, and any remainder the four named causes
// do not account for is reported as "other" rather than silently dropped, so
// the parts always visibly sum to the total.
func formatSkipBreakdown(result types.SchedulerResult) string {
	if result.Skipped == 0 {
		return ""
	}
	var parts []string
	if result.SkippedTasksPaused > 0 {
		parts = append(parts, fmt.Sprintf("%d held by tasks-paused", result.SkippedTasksPaused))
	}
	if result.SkippedAutomationsPaused > 0 {
		parts = append(parts, fmt.Sprintf("%d held by automations-paused", result.SkippedAutomationsPaused))
	}
	if result.SkippedNoCandidate > 0 {
		parts = append(parts, fmt.Sprintf("%d no eligible runner (see task_placement_reasons)", result.SkippedNoCandidate))
	}
	if result.SkippedAlreadyLeased > 0 {
		parts = append(parts, fmt.Sprintf("%d already dispatched by an earlier pass", result.SkippedAlreadyLeased))
	}
	accounted := result.SkippedTasksPaused + result.SkippedAutomationsPaused +
		result.SkippedNoCandidate + result.SkippedAlreadyLeased
	if remainder := result.Skipped - accounted; remainder > 0 {
		parts = append(parts, fmt.Sprintf("%d other", remainder))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
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
		// Only projects that had ready tasks on the last tick carry any
		// information. Every other project reports three zeros, and on a
		// store with dozens of projects (many of them long-dead test
		// fixtures) those zeros bury the handful of lines worth reading.
		// The omitted count is still disclosed so a project going quiet is
		// distinguishable from a project being filtered out here.
		active := make([]string, 0, len(status.LastProjectResults))
		idle := 0
		for project, result := range status.LastProjectResults {
			if result.Considered == 0 {
				idle++
				continue
			}
			active = append(active, project)
		}
		sort.Strings(active)

		b.WriteString("\n### Project Results\n\n")
		for _, project := range active {
			result := status.LastProjectResults[project]
			fmt.Fprintf(&b, "**%s:**\n", project)
			fmt.Fprintf(&b, "- Considered: %d\n", result.Considered)
			fmt.Fprintf(&b, "- Dispatched: %d\n", result.Dispatched)
			fmt.Fprintf(&b, "- Skipped: %d%s\n", result.Skipped, formatSkipBreakdown(result))
			b.WriteString("\n")
		}
		if len(active) == 0 {
			b.WriteString("No project had ready tasks on the last tick.\n\n")
		}
		if idle > 0 {
			fmt.Fprintf(&b, "_%d project(s) omitted: no ready tasks to consider on the last tick._\n", idle)
		}
	}

	return b.String()
}

// validateEventTypeFilter rejects a filter no event can ever match.
//
// Supports the documented "task.*" prefix form as well as exact types. An
// unrecognised value previously produced "Found 0 events: No events found." —
// byte-identical to a valid filter that matched nothing — so a typo, or a
// plausible-but-nonexistent name like "automation.run" (there is no automation.*
// family at all), read as evidence the thing never happened.
func validateEventTypeFilter(filter string) error {
	// The global wildcard. MatchEventPattern (internal/types/events.go)
	// short-circuits it to true for every event, and it is the documented
	// pattern language shared with webhook Events and automation Trigger.Event,
	// so an agent that learned "*" from those surfaces will pass it here.
	//
	// The first version of this validator rejected it — on a change whose whole
	// point is removing confident false negatives, with an error reading
	// "nothing emits it, so this filter can never match" about the one pattern
	// that matches everything.
	if filter == "*" {
		return nil
	}
	if strings.HasSuffix(filter, ".*") {
		prefix := strings.TrimSuffix(filter, "*")
		for _, known := range types.AllEventTypes {
			if strings.HasPrefix(known, prefix) {
				return nil
			}
		}
		return fmt.Errorf("no event type starts with %q. Known families: %s",
			prefix, strings.Join(eventTypeFamilies(), ", "))
	}
	for _, known := range types.AllEventTypes {
		if known == filter {
			return nil
		}
	}
	return fmt.Errorf("unknown event type %q — nothing emits it, so this filter can never match. Known families: %s (use e.g. \"task.*\" for a whole family)",
		filter, strings.Join(eventTypeFamilies(), ", "))
}

// eventTypeFamilies lists the distinct "<family>." prefixes, sorted. Naming the
// families rather than all ~35 types keeps the error short while still telling
// the caller where to look.
func eventTypeFamilies() []string {
	seen := map[string]struct{}{}
	var families []string
	for _, t := range types.AllEventTypes {
		if i := strings.Index(t, "."); i > 0 {
			f := t[:i] + ".*"
			if _, ok := seen[f]; !ok {
				seen[f] = struct{}{}
				families = append(families, f)
			}
		}
	}
	sort.Strings(families)
	return families
}

// formatEventCoverage renders the window that was actually searched.
//
// The buffer is in-memory, fixed-size, and empty after every restart, while
// runner.poll_complete fills it continuously — so a zero count says nothing
// about whether the event happened. Stating the window turns "it never happened"
// back into "it is not in the last N events I can see".
func formatEventCoverage(cov types.EventCoverage) string {
	if cov.Capacity == 0 {
		return ""
	}
	window := fmt.Sprintf("Searched %d buffered event(s) (ring capacity %d)", cov.Buffered, cov.Capacity)
	if cov.Oldest != "" {
		window += fmt.Sprintf("; oldest retained: %s", cov.Oldest)
	}
	if cov.Buffered >= cov.Capacity {
		window += ". The ring is FULL, so anything older than that has been overwritten"
	}
	return window + ".\nThis buffer is in-memory and starts empty after a restart; it is not a durable event log."
}

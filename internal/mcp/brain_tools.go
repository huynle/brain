package mcp

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// RegisterBrainTools registers all 22 brain core tools on the server.
func RegisterBrainTools(s *Server, client *APIClient) {
	registerBrainSave(s, client)
	registerBrainRecall(s, client)
	registerBrainSearch(s, client)
	registerBrainList(s, client)
	registerBrainInject(s, client)
	registerBrainUpdate(s, client)
	registerBrainBulkUpdate(s, client)
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
	registerBrainAutomationList(s, client)
	registerBrainAutomationTest(s, client)
	registerBrainAttachmentUpload(s, client)
	registerBrainAttachmentAttach(s, client)
	registerBrainAttachmentDetach(s, client)
	registerBrainAttachmentList(s, client)
	registerBrainAttachmentGet(s, client)
	registerBrainAttachmentDelete(s, client)
	registerBrainAttachmentBackfill(s, client)
	registerBrainAttachmentExtract(s, client)
	registerBrainAttachmentText(s, client)
	registerBrainAttachmentDownload(s, client)
}

// =============================================================================
// brain_save
// =============================================================================

func registerBrainSave(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "save",
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
- exploration: Investigation notes, research findings

Feature orchestration:
- Use feature_id to group tasks into a feature.
- Use feature_depends_on to make one feature wait for another feature to complete.
- Use trigger.event="feature.completed" with trigger.filter.feature_id to create post-feature tasks that activate after a feature completes.`,
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
				"feature_depends_on":    {Type: "array", Items: &Property{Type: "string"}, Description: "Feature IDs this feature depends on. All tasks in dependent features must complete before this feature's tasks can start. Use this for before-feature orchestration (e.g., feature 'main' depends on feature 'preflight')."},
				"trigger":               {Type: "object", Description: "Event trigger for inactive/active tasks or automation entries. For post-feature tasks use {event:'feature.completed', filter:{feature_id:'main-feature', project_id:'my-project'}}. Supports type (event, cron, webhook, session), event, schedule, webhook, filter, once_per, cooldown, max_concurrent, ignore_automation_events."},
				"action":                {Type: "object", Description: "Automation action config for automation entries. Common fields: type ('create_task' or 'script'), title_template, prompt_template, direct_prompt, command, agent, model, executor, target_workdir. Templates support Go syntax with {{.Project}}, {{.ProjectID}}, {{.EventProjectID}}, {{.FeatureID}}, {{.TaskID}}, {{.TaskPath}}, {{.TaskTitle}}, {{.FromStatus}}, {{.ToStatus}}."},
				"retry":                 {Type: "object", Description: "Automation retry policy for automation entries. Common fields: max_attempts, backoff, timeout."},
				"direct_prompt":         {Type: "string", Description: "Direct prompt to execute, bypassing default skill workflow. The prompt is sent verbatim when the task runs."},
				"agent":                 {Type: "string", Description: "Override agent for this task (e.g., 'explore', 'tdd-dev', 'build')"},
				"model":                 {Type: "string", Description: "Override model (format: 'provider/model-id', e.g., 'anthropic/claude-sonnet-4-20250514')"},
				"executor":              {Type: "string", Enum: []string{"", "opencode", "pi", "script"}, Description: "Executor backend for this task: 'opencode' (HTTP API-based), 'pi' (RPC mode), or 'script'. Empty = use runner default."},
				"extensions":            {Type: "array", Items: &Property{Type: "string"}, Description: "Additional extensions to load for this task (e.g., ['code-review', 'auto-commit'])"},
				"schedule":              {Type: "string", Description: "Cron schedule expression (e.g., '*/5 * * * *', '0 2 * * *'). When provided for tasks, automatically creates and links a cron entry titled '{task-title} (Cron)'. This simplifies recurring task setup from 3 steps to 1 step."},
				"schedule_enabled":      {Type: "boolean", Description: "Whether the schedule is active (default true when schedule exists). Set to false to pause scheduling."},
				"max_runs":              {Type: "number", Description: "Maximum number of scheduled runs before auto-disabling the schedule. When the run count reaches this limit, schedule_enabled is set to false. Omit or set to 0 for unlimited runs."},
				"run_once_at":           {Type: "string", Description: "RFC3339 timestamp for one-time execution (e.g., '2025-06-15T10:00:00Z'). Task runs once at this time then auto-disables."},
				"timezone":              {Type: "string", Description: "IANA timezone for schedule interpretation (e.g., 'America/New_York', 'UTC'). Defaults to UTC if not set."},
				"starts_at":             {Type: "string", Description: "RFC3339 timestamp for when the schedule becomes active. Schedule won't trigger before this time."},
				"expires_at":            {Type: "string", Description: "RFC3339 timestamp for when the schedule expires. Must be after starts_at if both are set."},
				"feature_schedule":      {Type: "string", Description: "Cron schedule for all tasks in this feature group (e.g., '0 2 * * *')"},
				"feature_starts_at":     {Type: "string", Description: "RFC3339 timestamp for when the feature schedule becomes active"},
				"feature_expires_at":    {Type: "string", Description: "RFC3339 timestamp for when the feature schedule expires"},
				"feature_run_once_at":   {Type: "string", Description: "RFC3339 timestamp for one-time execution of all feature tasks"},
				"feature_timezone":      {Type: "string", Description: "IANA timezone for feature schedule interpretation (e.g., 'America/New_York')"},
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
		isAutomation := StringArg(args, "type", "") == "automation"

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
			body["trigger"] = args["trigger"]
			body["action"] = args["action"]
			body["retry"] = args["retry"]
			body["direct_prompt"] = args["direct_prompt"]
			body["agent"] = args["agent"]
			body["model"] = args["model"]
			body["schedule"] = args["schedule"]
			body["schedule_enabled"] = args["schedule_enabled"]
			body["max_runs"] = args["max_runs"]
			body["run_once_at"] = args["run_once_at"]
			body["timezone"] = args["timezone"]
			body["starts_at"] = args["starts_at"]
			body["expires_at"] = args["expires_at"]
			body["feature_schedule"] = args["feature_schedule"]
			body["feature_starts_at"] = args["feature_starts_at"]
			body["feature_expires_at"] = args["feature_expires_at"]
			body["feature_run_once_at"] = args["feature_run_once_at"]
			body["feature_timezone"] = args["feature_timezone"]
			body["merge_target_branch"] = args["merge_target_branch"]
			body["merge_policy"] = args["merge_policy"]
			body["merge_strategy"] = args["merge_strategy"]
			body["remote_branch_policy"] = args["remote_branch_policy"]
			body["open_pr_before_merge"] = args["open_pr_before_merge"]
			body["execution_mode"] = args["execution_mode"]
			body["complete_on_idle"] = args["complete_on_idle"]
			if v, ok := args["executor"].(string); ok && v != "" {
				body["executor"] = v
			}
			if v, ok := args["extensions"]; ok {
				body["extensions"] = v
			}
		}

		if isTask || isAutomation {
			body["trigger"] = args["trigger"]
		}
		if isAutomation {
			body["action"] = args["action"]
			body["retry"] = args["retry"]
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
		Name:        "recall",
		Description: "Retrieve a specific entry from the brain by path, ID, or title.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path":    {Type: "string", Description: "Path or ID to the note"},
				"title":   {Type: "string", Description: "Title to search for (exact match)"},
				"project": {Type: "string", Description: "When resolving by title, restrict the match to this project ID (e.g., 'orion-ai'). Ignored when 'path' is provided."},
				"include": {Type: "array", Items: &Property{Type: "string"}, Description: "Optional related data to include, e.g. ['attachments', 'attachment_text']. Passed to the API as a comma-separated include query."},
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

			searchBody := map[string]any{"query": title, "limit": 5}
			// If a project was specified, scope the title lookup so two
			// projects with a same-titled note don't collide.
			if project := StringArg(args, "project", ""); project != "" {
				searchBody["project"] = project
			}

			var searchResp struct {
				Results []struct {
					Path  string `json:"path"`
					Title string `json:"title"`
				} `json:"results"`
			}
			if err := client.Request(ctx, "POST", "/search", searchBody, nil, &searchResp); err != nil {
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
			ID                  string                      `json:"id"`
			Path                string                      `json:"path"`
			Title               string                      `json:"title"`
			Type                string                      `json:"type"`
			Status              string                      `json:"status"`
			Content             string                      `json:"content"`
			Tags                []string                    `json:"tags"`
			UserOriginalRequest string                      `json:"user_original_request"`
			Attachments         []types.AttachmentReference `json:"attachments,omitempty"`
		}
		params := make(map[string]string)
		if include := StringSliceArg(args, "include"); len(include) > 0 {
			params["include"] = strings.Join(include, ",")
		}
		if err := client.Request(ctx, "GET", "/entries/"+entryPath, nil, params, &resp); err != nil {
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

		attachments := formatAttachmentReferences(resp.Attachments)

		return fmt.Sprintf("## %s\n\nPath: %s\nType: %s\nStatus: %s\nTags: %s%s%s\n\n---\n\n%s",
			resp.Title, resp.Path, resp.Type, resp.Status, tags, userRequest, attachments, resp.Content), nil
	})
}

// =============================================================================
// Attachment tools
// =============================================================================

func registerBrainAttachmentUpload(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "attachment_upload",
		Description: `Upload a local file as a first-class Brain attachment.

Use this for pasted-image or local-PDF workflows: save the file locally, upload it with this tool, then attach the returned attachment_id to an entry with brain_attachment_attach.`,
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project_id": {Type: "string", Description: "Project that owns the uploaded attachment"},
			"file_path":  {Type: "string", Description: "Absolute or relative path to the local file to upload"},
			"metadata":   {Type: "object", Description: "Optional string key/value metadata stored with the attachment"},
		}, Required: []string{"project_id", "file_path"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := StringArg(args, "project_id", "")
		filePath := StringArg(args, "file_path", "")
		if projectID == "" || filePath == "" {
			return "Please provide project_id and file_path", nil
		}

		resp, err := client.UploadAttachment(ctx, projectID, filePath, stringMapArg(args, "metadata"))
		if err != nil {
			return "", err
		}

		return fmt.Sprintf("Uploaded attachment\n\n%s\n\nNext: attach it to an entry with `brain_attachment_attach` using attachment_id `%s`.",
			formatAttachment(resp.Attachment), resp.Attachment.ID), nil
	})
}

func registerBrainAttachmentAttach(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "attachment_attach",
		Description: "Attach an existing Brain attachment to an entry with optional role and caption metadata.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project_id":    {Type: "string", Description: "Project containing the entry and attachment"},
			"entry_id":      {Type: "string", Description: "Entry ID or path to attach to"},
			"attachment_id": {Type: "string", Description: "Attachment ID returned by brain_attachment_upload or brain_attachment_list"},
			"role":          {Type: "string", Description: "Optional attachment role, e.g. source, inline, image, pdf"},
			"caption":       {Type: "string", Description: "Optional model-friendly caption describing the attachment"},
		}, Required: []string{"project_id", "entry_id", "attachment_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := StringArg(args, "project_id", "")
		entryID := StringArg(args, "entry_id", "")
		attachmentID := StringArg(args, "attachment_id", "")
		if projectID == "" || entryID == "" || attachmentID == "" {
			return "Please provide project_id, entry_id, and attachment_id", nil
		}

		body := map[string]any{"attachment": map[string]any{"id": attachmentID}}
		attachment := body["attachment"].(map[string]any)
		if role := StringArg(args, "role", ""); role != "" {
			attachment["role"] = role
		}
		if caption := StringArg(args, "caption", ""); caption != "" {
			attachment["caption"] = caption
		}

		var resp types.AttachEntryAttachmentResponse
		if err := client.Request(ctx, "POST", "/entries/"+entryID+"/attachments", body, map[string]string{"project_id": projectID}, &resp); err != nil {
			return "", err
		}
		return fmt.Sprintf("Attached attachment %s to entry %s\n\n%s", attachmentID, entryID, formatAttachmentReferences(resp.Attachments)), nil
	})
}

func registerBrainAttachmentDetach(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "attachment_detach",
		Description: "Detach an attachment from an entry. Provide role when detaching a role-specific reference.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project_id":    {Type: "string", Description: "Project containing the entry and attachment"},
			"entry_id":      {Type: "string", Description: "Entry ID or path to detach from"},
			"attachment_id": {Type: "string", Description: "Attachment ID to detach"},
			"role":          {Type: "string", Description: "Optional role to detach"},
		}, Required: []string{"project_id", "entry_id", "attachment_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := StringArg(args, "project_id", "")
		entryID := StringArg(args, "entry_id", "")
		attachmentID := StringArg(args, "attachment_id", "")
		if projectID == "" || entryID == "" || attachmentID == "" {
			return "Please provide project_id, entry_id, and attachment_id", nil
		}

		params := map[string]string{"project_id": projectID}
		if role := StringArg(args, "role", ""); role != "" {
			params["role"] = role
		}
		var resp types.AttachEntryAttachmentResponse
		if err := client.Request(ctx, "DELETE", "/entries/"+entryID+"/attachments/"+url.PathEscape(attachmentID), nil, params, &resp); err != nil {
			return "", err
		}
		return fmt.Sprintf("Detached attachment %s from entry %s\n\nRemaining:%s", attachmentID, entryID, formatAttachmentReferences(resp.Attachments)), nil
	})
}

func registerBrainAttachmentList(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "attachment_list",
		Description: "List attachments available in a project, or attachments linked to a specific entry when entry_id is provided.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project_id": {Type: "string", Description: "Project whose attachments should be listed"},
			"entry_id":   {Type: "string", Description: "Optional entry ID or path for entry-scoped attachment references"},
		}, Required: []string{"project_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := StringArg(args, "project_id", "")
		if projectID == "" {
			return "Please provide project_id", nil
		}
		if entryID := StringArg(args, "entry_id", ""); entryID != "" {
			var resp types.AttachEntryAttachmentResponse
			if err := client.Request(ctx, "GET", "/entries/"+entryID+"/attachments", nil, map[string]string{"project_id": projectID}, &resp); err != nil {
				return "", err
			}
			return formatEntryAttachmentList(projectID, entryID, resp), nil
		}

		var resp types.ListAttachmentsResponse
		if err := client.Request(ctx, "GET", "/attachments", nil, map[string]string{"project_id": projectID}, &resp); err != nil {
			return "", err
		}
		if len(resp.Attachments) == 0 {
			return fmt.Sprintf("No attachments found for project %s", projectID), nil
		}
		lines := []string{fmt.Sprintf("Attachments (%d)", resp.Total), "", fmt.Sprintf("Project: %s", projectID), ""}
		for _, attachment := range resp.Attachments {
			lines = append(lines, formatAttachment(attachment), "")
		}
		return strings.TrimSpace(strings.Join(lines, "\n")), nil
	})
}

func registerBrainAttachmentGet(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "attachment_get",
		Description: "Get attachment metadata, download/text URLs, and derived artifact references.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project_id":    {Type: "string", Description: "Project containing the attachment"},
			"attachment_id": {Type: "string", Description: "Attachment ID to retrieve"},
		}, Required: []string{"project_id", "attachment_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := StringArg(args, "project_id", "")
		attachmentID := StringArg(args, "attachment_id", "")
		if projectID == "" || attachmentID == "" {
			return "Please provide project_id and attachment_id", nil
		}
		var resp types.Attachment
		if err := client.Request(ctx, "GET", "/attachments/"+url.PathEscape(attachmentID), nil, map[string]string{"project_id": projectID}, &resp); err != nil {
			return "", err
		}
		return formatAttachment(resp), nil
	})
}

func registerBrainAttachmentDelete(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "attachment_delete",
		Description: "Delete an attachment from a project when it is not referenced by entries.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project_id":    {Type: "string", Description: "Project containing the attachment"},
			"attachment_id": {Type: "string", Description: "Attachment ID to delete"},
		}, Required: []string{"project_id", "attachment_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := StringArg(args, "project_id", "")
		attachmentID := StringArg(args, "attachment_id", "")
		if projectID == "" || attachmentID == "" {
			return "Please provide project_id and attachment_id", nil
		}

		var resp struct {
			Deleted bool `json:"deleted"`
		}
		if err := client.Request(ctx, "DELETE", "/attachments/"+url.PathEscape(attachmentID), nil, map[string]string{"project_id": projectID}, &resp); err != nil {
			return "", err
		}
		return fmt.Sprintf("Deleted attachment %s\n\nProject: %s\nAttachment: %s\nStatus: deleted=%t", attachmentID, projectID, attachmentID, resp.Deleted), nil
	})
}

func registerBrainAttachmentBackfill(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "attachment_backfill",
		Description: "Run project-level attachment text extraction backfill and return counts for considered attachments.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project_id":          {Type: "string", Description: "Project whose attachments should be backfilled"},
			"dry_run":             {Type: "boolean", Description: "Report candidates without extracting text"},
			"force":               {Type: "boolean", Description: "Re-extract attachments that already have derived text"},
			"batch_size":          {Type: "number", Description: "Maximum attachments to process in one run"},
			"rate_limit_delay_ms": {Type: "number", Description: "Delay between extraction requests in milliseconds"},
		}, Required: []string{"project_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := StringArg(args, "project_id", "")
		if projectID == "" {
			return "Please provide project_id", nil
		}
		req := types.AttachmentExtractionBackfillRequest{
			DryRun:           BoolArg(args, "dry_run", false),
			Force:            BoolArg(args, "force", false),
			BatchSize:        IntArg(args, "batch_size", 0),
			RateLimitDelayMs: IntArg(args, "rate_limit_delay_ms", 0),
		}
		var resp types.AttachmentExtractionBackfillResponse
		if err := client.Request(ctx, "POST", "/attachments/backfill/extraction", req, map[string]string{"project_id": projectID}, &resp); err != nil {
			return "", err
		}
		return formatAttachmentBackfill(projectID, resp), nil
	})
}

func registerBrainAttachmentExtract(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "attachment_extract",
		Description: "Trigger server-side media-to-text extraction for an attachment and return extraction status, provider/model, reason, and derived text metadata.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project_id":    {Type: "string", Description: "Project containing the attachment"},
			"attachment_id": {Type: "string", Description: "Attachment ID whose text extraction should be triggered"},
		}, Required: []string{"project_id", "attachment_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := StringArg(args, "project_id", "")
		attachmentID := StringArg(args, "attachment_id", "")
		if projectID == "" || attachmentID == "" {
			return "Please provide project_id and attachment_id", nil
		}

		resp, err := client.ExtractAttachmentText(ctx, projectID, attachmentID, types.AttachmentExtractionRequest{})
		if err != nil {
			return "", err
		}
		return formatAttachmentExtractionResult(*resp), nil
	})
}

func registerBrainAttachmentText(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "attachment_text",
		Description: "Retrieve extracted plain text for an attachment, useful for local PDF/image OCR workflows after upload.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project_id":    {Type: "string", Description: "Project containing the attachment"},
			"attachment_id": {Type: "string", Description: "Attachment ID whose extracted text should be retrieved"},
		}, Required: []string{"project_id", "attachment_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := StringArg(args, "project_id", "")
		attachmentID := StringArg(args, "attachment_id", "")
		if projectID == "" || attachmentID == "" {
			return "Please provide project_id and attachment_id", nil
		}
		text, err := client.DownloadAttachmentText(ctx, projectID, attachmentID)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(text) == "" {
			return fmt.Sprintf("No extracted text is available for attachment %s", attachmentID), nil
		}
		return fmt.Sprintf("## Attachment Text: %s\n\n%s", attachmentID, text), nil
	})
}

func registerBrainAttachmentDownload(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "attachment_download",
		Description: "Download raw attachment bytes to a local output path. Use this when an agent needs the exact original image, PDF, or media file for later processing.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project_id":    {Type: "string", Description: "Project containing the attachment"},
			"attachment_id": {Type: "string", Description: "Attachment ID whose raw content should be downloaded"},
			"output_path":   {Type: "string", Description: "Local path where the downloaded bytes should be written"},
		}, Required: []string{"project_id", "attachment_id", "output_path"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := StringArg(args, "project_id", "")
		attachmentID := StringArg(args, "attachment_id", "")
		outputPath := StringArg(args, "output_path", "")
		if projectID == "" || attachmentID == "" || outputPath == "" {
			return "Please provide project_id, attachment_id, and output_path", nil
		}
		if err := client.DownloadAttachmentToFile(ctx, projectID, attachmentID, outputPath); err != nil {
			return "", err
		}
		return fmt.Sprintf("Downloaded attachment %s to %s", attachmentID, outputPath), nil
	})
}

func stringMapArg(args map[string]any, key string) map[string]string {
	value, ok := args[key]
	if !ok || value == nil {
		return nil
	}
	result := map[string]string{}
	switch metadata := value.(type) {
	case map[string]string:
		return metadata
	case map[string]any:
		for k, v := range metadata {
			if s, ok := v.(string); ok {
				result[k] = s
			} else if v != nil {
				result[k] = fmt.Sprintf("%v", v)
			}
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func formatEntryAttachmentList(projectID, requestedEntryID string, resp types.AttachEntryAttachmentResponse) string {
	entryID := resp.EntryID
	if entryID == "" {
		entryID = requestedEntryID
	}
	lines := []string{
		fmt.Sprintf("## Entry Attachments (%d)", len(resp.Attachments)),
		"",
		fmt.Sprintf("Project: %s", projectID),
		fmt.Sprintf("Entry: %s", entryID),
	}
	if resp.Path != "" {
		lines = append(lines, "Path: "+resp.Path)
	}
	if len(resp.Attachments) == 0 {
		lines = append(lines, "", "No attachments found for entry.")
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}
	lines = append(lines, "")
	for _, attachment := range resp.Attachments {
		lines = append(lines, formatAttachmentReference(attachment))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func formatAttachmentBackfill(projectID string, resp types.AttachmentExtractionBackfillResponse) string {
	lines := []string{
		"## Attachment Extraction Backfill",
		"",
		fmt.Sprintf("Project: %s", projectID),
		fmt.Sprintf("Total: %d", resp.Total),
		fmt.Sprintf("Candidates: %d", resp.Candidates),
		fmt.Sprintf("Processed: %d", resp.Processed),
		fmt.Sprintf("Skipped: %d", resp.Skipped),
		fmt.Sprintf("Failed: %d", resp.Failed),
		fmt.Sprintf("Dry run: %t", resp.DryRun),
	}
	if len(resp.Attachments) > 0 {
		lines = append(lines, "", "Attachments:")
		for _, item := range resp.Attachments {
			line := fmt.Sprintf("- `%s`", item.AttachmentID)
			if item.Filename != "" {
				line += " — " + item.Filename
			}
			if item.Status != "" {
				line += " — Status: " + item.Status
			}
			if item.Skipped {
				line += " — skipped"
			}
			if item.Reason != "" {
				line += " — Reason: " + item.Reason
			}
			if item.Error != "" {
				line += " — Error: " + item.Error
			}
			lines = append(lines, line)
		}
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func formatAttachmentReferences(attachments []types.AttachmentReference) string {
	if len(attachments) == 0 {
		return ""
	}
	lines := []string{"", "\n### Attachments"}
	for _, attachment := range attachments {
		lines = append(lines, formatAttachmentReference(attachment))
	}
	return strings.Join(lines, "\n")
}

func formatAttachmentReference(a types.AttachmentReference) string {
	parts := []string{fmt.Sprintf("- `%s`", a.ID)}
	if a.Filename != "" {
		parts = append(parts, a.Filename)
	}
	if a.ContentType != "" {
		parts = append(parts, a.ContentType)
	}
	if a.Size > 0 {
		parts = append(parts, fmt.Sprintf("%d bytes", a.Size))
	}
	if a.Role != "" {
		parts = append(parts, "role: "+a.Role)
	}
	line := strings.Join(parts, " — ")
	if a.Caption != "" {
		line += "\n  Caption: " + a.Caption
	}
	if a.DownloadURL != "" {
		line += "\n  Download: " + a.DownloadURL
	}
	if a.TextURL != "" {
		line += "\n  Text: " + a.TextURL
	}
	if len(a.Derived) > 0 {
		line += "\n  Derived: " + formatDerived(a.Derived)
	}
	return line
}

func formatAttachment(a types.Attachment) string {
	ref := types.AttachmentReference{
		ID:          a.ID,
		Filename:    a.Filename,
		ContentType: a.ContentType,
		Size:        a.Size,
		SHA256:      a.SHA256,
		Metadata:    a.Metadata,
		Derived:     a.Derived,
	}
	line := formatAttachmentReference(ref)
	if len(a.Metadata) > 0 {
		keys := make([]string, 0, len(a.Metadata))
		for key := range a.Metadata {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		metadata := make([]string, 0, len(keys))
		for _, key := range keys {
			metadata = append(metadata, fmt.Sprintf("%s=%s", key, a.Metadata[key]))
		}
		line += "\n  Metadata: " + strings.Join(metadata, ", ")
	}
	return line
}

func formatDerived(derived []types.AttachmentDerived) string {
	items := make([]string, 0, len(derived))
	for _, item := range derived {
		parts := []string{item.ID, item.Kind}
		if item.ContentType != "" {
			parts = append(parts, item.ContentType)
		}
		if item.Size > 0 {
			parts = append(parts, fmt.Sprintf("%d bytes", item.Size))
		}
		if item.StorageKey != "" {
			parts = append(parts, item.StorageKey)
		}
		items = append(items, strings.Join(parts, " / "))
	}
	return strings.Join(items, "; ")
}

func formatAttachmentExtractionResult(result types.AttachmentExtractionResult) string {
	derived := result.DerivedText
	attachmentID := result.Attachment.ID
	if attachmentID == "" {
		attachmentID = derived.Metadata["attachment_id"]
	}
	if attachmentID == "" {
		attachmentID = "unknown"
	}

	lines := []string{
		fmt.Sprintf("## Attachment Extraction: %s", attachmentID),
		"",
		fmt.Sprintf("Status: %s", derived.Status),
	}
	if result.Attachment.Filename != "" {
		lines = append(lines, "Filename: "+result.Attachment.Filename)
	}
	provider := strings.TrimSpace(derived.Metadata["provider"])
	if provider == "" {
		provider = strings.TrimSpace(derived.Metadata["extraction_provider"])
	}
	if provider != "" {
		lines = append(lines, "Provider: "+provider)
	}
	model := strings.TrimSpace(derived.Metadata["model"])
	if model == "" {
		model = strings.TrimSpace(derived.Metadata["extraction_model"])
	}
	if model != "" {
		lines = append(lines, "Model: "+model)
	}
	if derived.Error != "" {
		lines = append(lines, "Reason: "+derived.Error)
	}
	if derived.ContentType != "" {
		lines = append(lines, "Derived content type: "+derived.ContentType)
	}
	if derived.ID != "" {
		lines = append(lines, "Derived text ID: "+derived.ID)
	}
	lines = append(lines, fmt.Sprintf("Text: %d chars", len(derived.Text)))

	if len(derived.Metadata) > 0 {
		keys := make([]string, 0, len(derived.Metadata))
		for key := range derived.Metadata {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		lines = append(lines, "", "Metadata:")
		for _, key := range keys {
			lines = append(lines, fmt.Sprintf("- %s: %s", key, derived.Metadata[key]))
		}
	}

	if len(result.LinkedEntries) > 0 {
		lines = append(lines, "", "Linked entries:")
		for _, entry := range result.LinkedEntries {
			line := "- " + entry.Path
			if entry.Role != "" {
				line += " (role: " + entry.Role + ")"
			}
			lines = append(lines, line)
		}
	}

	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// =============================================================================
// brain_search
// =============================================================================

func registerBrainSearch(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "search",
		Description: "Search the brain using full-text search.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"query":      {Type: "string", Description: "Search query"},
				"project":    {Type: "string", Description: "Filter by project ID (e.g., 'orion-ai'). Omit to search across all projects."},
				"type":       {Type: "string", Enum: types.EntryTypes, Description: "Filter by entry type"},
				"status":     {Type: "string", Enum: types.EntryStatuses, Description: "Filter by status"},
				"feature_id": {Type: "string", Description: "Filter by feature group ID (e.g., 'auth-system', 'dark-mode')"},
				"tags":       {Type: "array", Items: &Property{Type: "string"}, Description: "Filter by tags (OR logic - matches entries with any of the specified tags)"},
				"limit":      {Type: "number", Description: "Maximum results (default: 10)"},
				"global":     {Type: "boolean", Description: "Search only global entries"},
				"strategy":   {Type: "string", Enum: []string{"fts", "exact", "like", "semantic", "hybrid"}, Description: "Search strategy: 'fts' (default), 'exact', 'like', 'semantic' (embedding), or 'hybrid' (combined)"},
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
		Name: "list",
		Description: `List entries in the brain with optional filtering by type, status, and filename.

Filename filtering supports:
- Exact match: "abc12def" finds entry with that exact ID
- Wildcard patterns: "abc*" (prefix), "*def" (suffix), "abc*def" (contains)`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"project":    {Type: "string", Description: "Filter by project ID (e.g., 'orion-ai'). Omit to list across all projects."},
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
		if v := StringArg(args, "project", ""); v != "" {
			params["project"] = v
		}
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
		Name:        "inject",
		Description: "Search the brain and return relevant context for a task.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"query":      {Type: "string", Description: "What context are you looking for?"},
				"project":    {Type: "string", Description: "Filter by project ID (e.g., 'orion-ai'). Omit to search across all projects."},
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
		Name: "update",
		Description: `Update an existing brain entry's status, title, dependencies, trigger configuration, or append content.

Use cases:
- Mark a plan as completed: brain_update(path: "...", status: "completed")
- Mark as in-progress: brain_update(path: "...", status: "in_progress")  
- Block with reason: brain_update(path: "...", status: "blocked", note: "Waiting on API design")
- Append progress notes: brain_update(path: "...", append: "## Progress\n- Completed auth module")
- Update title: brain_update(path: "...", title: "New Title")
- Update dependencies: brain_update(path: "...", depends_on: ["task-id-1", "task-id-2"])
- Update feature dependencies: brain_update(path: "...", feature_depends_on: ["pre-feature"])
- Add a post-feature trigger: brain_update(path: "...", trigger: {event:"feature.completed", filter:{feature_id:"main-feature"}})
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
				"max_runs":             {Type: "number", Description: "Maximum number of scheduled runs before auto-disabling. Omit or set to 0 for unlimited."},
				"run_once_at":          {Type: "string", Description: "RFC3339 timestamp for one-time execution (e.g., '2025-06-15T10:00:00Z'). Task runs once at this time then auto-disables."},
				"timezone":             {Type: "string", Description: "IANA timezone for schedule interpretation (e.g., 'America/New_York', 'UTC'). Defaults to UTC if not set."},
				"starts_at":            {Type: "string", Description: "RFC3339 timestamp for when the schedule becomes active. Schedule won't trigger before this time."},
				"expires_at":           {Type: "string", Description: "RFC3339 timestamp for when the schedule expires. Must be after starts_at if both are set."},
				"feature_id":           {Type: "string", Description: "Feature group identifier (e.g., 'auth-system', 'payment-flow')"},
				"feature_priority":     {Type: "string", Enum: types.Priorities, Description: "Priority for this feature group"},
				"feature_depends_on":   {Type: "array", Items: &Property{Type: "string"}, Description: "Feature IDs this feature depends on. Use this for feature-to-feature ordering."},
				"trigger":              {Type: "object", Description: "Event trigger for inactive/active tasks or automation entries. For post-feature tasks use {event:'feature.completed', filter:{feature_id:'main-feature', project_id:'my-project'}}. Supports type (event, cron, webhook, session), event, schedule, webhook, filter, once_per, cooldown, max_concurrent, ignore_automation_events."},
				"action":               {Type: "object", Description: "Automation action config for automation entries. Common fields: type, title_template, prompt_template, direct_prompt, command, agent, model, executor, target_workdir. Templates support Go syntax with {{.Project}}, {{.ProjectID}}, {{.EventProjectID}}, {{.FeatureID}}, {{.TaskID}}, {{.TaskPath}}, {{.TaskTitle}}, {{.FromStatus}}, {{.ToStatus}}."},
				"retry":                {Type: "object", Description: "Automation retry policy for automation entries. Common fields: max_attempts, backoff, timeout."},
				"feature_schedule":     {Type: "string", Description: "Cron schedule for all tasks in this feature group (e.g., '0 2 * * *')"},
				"feature_starts_at":    {Type: "string", Description: "RFC3339 timestamp for when the feature schedule becomes active"},
				"feature_expires_at":   {Type: "string", Description: "RFC3339 timestamp for when the feature schedule expires"},
				"feature_run_once_at":  {Type: "string", Description: "RFC3339 timestamp for one-time execution of all feature tasks"},
				"feature_timezone":     {Type: "string", Description: "IANA timezone for feature schedule interpretation (e.g., 'America/New_York')"},
				"direct_prompt":        {Type: "string", Description: "Direct prompt to execute, bypassing default skill workflow"},
				"agent":                {Type: "string", Description: "Override agent for this task (e.g., 'explore', 'tdd-dev')"},
				"model":                {Type: "string", Description: "Override model (format: 'provider/model-id')"},
				"executor":             {Type: "string", Enum: []string{"", "opencode", "pi", "script"}, Description: "Executor backend for this task: 'opencode', 'pi', or 'script'. Empty = use runner default."},
				"extensions":           {Type: "array", Items: &Property{Type: "string"}, Description: "Additional extensions to load for this task (e.g., ['code-review', 'auto-commit'])"},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		path := StringArg(args, "path", "")
		cleanArgs := sanitizeUpdateArgs(args)

		body := map[string]any{}
		addStringUpdateFields(body, cleanArgs,
			"status", "title", "append", "note", "priority", "target_workdir", "git_branch",
			"merge_target_branch", "merge_policy", "merge_strategy", "remote_branch_policy", "execution_mode",
			"schedule", "run_once_at", "timezone", "starts_at", "expires_at", "feature_id", "feature_priority",
			"feature_schedule", "feature_starts_at", "feature_expires_at", "feature_run_once_at", "feature_timezone",
			"direct_prompt", "agent", "model", "executor",
		)
		addPresentUpdateFields(body, cleanArgs,
			"depends_on", "tags", "open_pr_before_merge", "complete_on_idle", "schedule_enabled", "max_runs",
			"feature_depends_on", "trigger", "action", "retry", "extensions",
		)

		var resp struct {
			Path   string `json:"path"`
			Title  string `json:"title"`
			Status string `json:"status"`
		}
		if err := client.Request(ctx, "PATCH", "/entries/"+path, body, nil, &resp); err != nil {
			return "", err
		}

		var changes []string
		if v := StringArg(cleanArgs, "status", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Status: -> %s", v))
		}
		if v := StringArg(cleanArgs, "title", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Title: -> %q", v))
		}
		if v := StringArg(cleanArgs, "note", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Note: %q", v))
		}
		if v := StringArg(cleanArgs, "append", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Appended %d characters", len(v)))
		}
		if deps := StringSliceArg(cleanArgs, "depends_on"); deps != nil {
			changes = append(changes, fmt.Sprintf("Dependencies: %d task(s)", len(deps)))
		}
		if tags := StringSliceArg(cleanArgs, "tags"); tags != nil {
			if len(tags) > 0 {
				changes = append(changes, fmt.Sprintf("Tags: %s", strings.Join(tags, ", ")))
			} else {
				changes = append(changes, "Tags: (cleared)")
			}
		}
		if v := StringArg(cleanArgs, "priority", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Priority: %s", v))
		}
		if v := StringArg(cleanArgs, "target_workdir", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Target Workdir: %s", v))
		}
		if v := StringArg(cleanArgs, "git_branch", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Git Branch: %s", v))
		}
		if v := StringArg(cleanArgs, "merge_target_branch", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Merge Target Branch: %s", v))
		}
		if v := StringArg(cleanArgs, "merge_policy", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Merge Policy: %s", v))
		}
		if v := StringArg(cleanArgs, "merge_strategy", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Merge Strategy: %s", v))
		}
		if v := StringArg(cleanArgs, "remote_branch_policy", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Remote Branch Policy: %s", v))
		}
		if _, ok := cleanArgs["open_pr_before_merge"]; ok {
			changes = append(changes, fmt.Sprintf("Open PR Before Merge: %v", cleanArgs["open_pr_before_merge"]))
		}
		if v := StringArg(cleanArgs, "execution_mode", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Execution Mode: %s", v))
		}
		if _, ok := cleanArgs["complete_on_idle"]; ok {
			changes = append(changes, fmt.Sprintf("Complete On Idle: %v", cleanArgs["complete_on_idle"]))
		}
		if v := StringArg(cleanArgs, "schedule", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Schedule: %s", v))
		}
		if _, ok := cleanArgs["schedule_enabled"]; ok {
			changes = append(changes, fmt.Sprintf("Schedule Enabled: %v", cleanArgs["schedule_enabled"]))
		}
		if _, ok := cleanArgs["max_runs"]; ok {
			changes = append(changes, fmt.Sprintf("Max Runs: %v", cleanArgs["max_runs"]))
		}
		if v := StringArg(cleanArgs, "run_once_at", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Run Once At: %s", v))
		}
		if v := StringArg(cleanArgs, "timezone", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Timezone: %s", v))
		}
		if v := StringArg(cleanArgs, "starts_at", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Starts At: %s", v))
		}
		if v := StringArg(cleanArgs, "expires_at", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Expires At: %s", v))
		}
		if v := StringArg(cleanArgs, "feature_id", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Feature ID: %s", v))
		}
		if v := StringArg(cleanArgs, "feature_priority", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Feature Priority: %s", v))
		}
		if deps := StringSliceArg(cleanArgs, "feature_depends_on"); deps != nil {
			changes = append(changes, fmt.Sprintf("Feature Dependencies: %d feature(s)", len(deps)))
		}
		if v := StringArg(cleanArgs, "feature_schedule", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Feature Schedule: %s", v))
		}
		if v := StringArg(cleanArgs, "feature_starts_at", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Feature Starts At: %s", v))
		}
		if v := StringArg(cleanArgs, "feature_expires_at", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Feature Expires At: %s", v))
		}
		if v := StringArg(cleanArgs, "feature_run_once_at", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Feature Run Once At: %s", v))
		}
		if v := StringArg(cleanArgs, "feature_timezone", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Feature Timezone: %s", v))
		}
		if v := StringArg(cleanArgs, "direct_prompt", ""); v != "" {
			changes = append(changes, "Direct Prompt: set")
		}
		if v := StringArg(cleanArgs, "agent", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Agent: %s", v))
		}
		if v := StringArg(cleanArgs, "model", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Model: %s", v))
		}
		if v := StringArg(cleanArgs, "executor", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Executor: %s", v))
		}
		if exts := StringSliceArg(cleanArgs, "extensions"); exts != nil {
			changes = append(changes, fmt.Sprintf("Extensions: %s", strings.Join(exts, ", ")))
		}

		changeLines := make([]string, len(changes))
		for i, c := range changes {
			changeLines[i] = "- " + c
		}

		return fmt.Sprintf("Updated: %s\n\n**Changes:**\n%s\n\n**Current Status:** %s\n**Title:** %s\n\nUse `brain_recall` to view the full entry.",
			resp.Path, strings.Join(changeLines, "\n"), resp.Status, resp.Title), nil
	})
}

func addStringUpdateFields(body, args map[string]any, keys ...string) {
	for _, key := range keys {
		if v, ok := args[key].(string); ok && v != "" {
			body[key] = v
		}
	}
}

func addPresentUpdateFields(body, args map[string]any, keys ...string) {
	for _, key := range keys {
		if v, ok := args[key]; ok && v != nil {
			body[key] = v
		}
	}
}

var openCodeOptionalDefaults = map[string]any{
	"priority":             "medium",
	"feature_priority":     "high",
	"merge_policy":         "prompt_only",
	"merge_strategy":       "squash",
	"remote_branch_policy": "keep",
	"execution_mode":       "worktree",
	"executor":             "opencode",
	"open_pr_before_merge": false,
	"complete_on_idle":     false,
	"schedule_enabled":     false,
	"max_runs":             0,
}

func sanitizeUpdateArgs(args map[string]any) map[string]any {
	clean := make(map[string]any, len(args))
	defaultCount := 0
	for key, value := range args {
		if s, ok := value.(string); ok && s == "" {
			continue
		}
		if matchesOpenCodeOptionalDefault(key, value) {
			defaultCount++
		}
		clean[key] = value
	}

	if defaultCount < 3 {
		return clean
	}

	for key, value := range clean {
		if matchesOpenCodeOptionalDefault(key, value) {
			delete(clean, key)
		}
	}
	return clean
}

func matchesOpenCodeOptionalDefault(key string, value any) bool {
	defaultValue, ok := openCodeOptionalDefaults[key]
	if !ok {
		return false
	}

	switch want := defaultValue.(type) {
	case string:
		got, ok := value.(string)
		return ok && got == want
	case bool:
		got, ok := value.(bool)
		return ok && got == want
	case int:
		switch got := value.(type) {
		case int:
			return got == want
		case int64:
			return got == int64(want)
		case float64:
			return got == float64(want)
		}
	}
	return false
}

func sanitizeObjectArg(value any) any {
	obj, ok := value.(map[string]any)
	if !ok {
		return value
	}

	clean := make(map[string]any, len(obj))
	for key, field := range obj {
		if field == nil {
			continue
		}
		if s, ok := field.(string); ok && s == "" {
			continue
		}
		if arr, ok := field.([]any); ok && len(arr) == 0 {
			continue
		}
		clean[key] = field
	}
	return clean
}

func hasFields(value any) bool {
	obj, ok := value.(map[string]any)
	if !ok {
		return value != nil
	}
	return len(obj) > 0
}

func sanitizeUpdateValue(value any) any {
	obj, ok := value.(map[string]any)
	if !ok {
		return value
	}
	return sanitizeUpdateArgs(obj)
}

func sanitizeBulkUpdateEntries(value any) any {
	switch entries := value.(type) {
	case []any:
		clean := make([]any, 0, len(entries))
		for _, entry := range entries {
			clean = append(clean, sanitizeBulkUpdateEntry(entry))
		}
		return clean
	case []map[string]any:
		clean := make([]any, 0, len(entries))
		for _, entry := range entries {
			clean = append(clean, sanitizeBulkUpdateEntry(entry))
		}
		return clean
	default:
		return value
	}
}

func sanitizeBulkUpdateEntry(value any) any {
	entry, ok := value.(map[string]any)
	if !ok {
		return value
	}

	clean := make(map[string]any, len(entry))
	for key, field := range entry {
		if key == "updates" {
			clean[key] = sanitizeUpdateValue(field)
			continue
		}
		clean[key] = field
	}
	return clean
}

// =============================================================================
// brain_bulk_update
// =============================================================================

func registerBrainBulkUpdate(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "bulk_update",
		Description: `Update multiple brain entries in a single request.

Two modes (mutually exclusive):
1. Filter mode: filter + updates — find entries matching criteria and apply the same updates
2. Explicit mode: entries — specify each entry path with its own updates

Use dry_run to preview what would be changed without applying.
Omit filter fields you do not want to match. Do not include priority in the filter unless you intentionally want to update only one priority.

Examples:
- Mark all tasks in a feature as cancelled:
  brain_bulk_update({ filter: { feature_id: "old-feature" }, updates: { status: "cancelled" } })
- Update specific entries:
  brain_bulk_update({ entries: [{ path: "projects/x/task/abc.md", updates: { status: "completed" } }] })
- Preview changes:
  brain_bulk_update({ filter: { status: "draft" }, updates: { status: "pending" }, dry_run: true })`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"project": {Type: "string", Description: "Convenience shortcut for filter.project: restrict updates to entries in this project (e.g., 'orion-ai'). Only used in filter mode; explicit-entries mode ignores this. If filter already has a project field, that value wins."},
				"filter":  {Type: "object", Description: "Filter criteria to select entries. Fields: feature_id (string), project (string), type (string), status (string), tags (string[]), priority (string). Use with 'updates'."},
				"updates": {Type: "object", Description: "Updates to apply to matched entries. Fields: status (string), priority (string), tags (string[]), append (string), note (string). Use with 'filter'."},
				"entries": {Type: "array", Items: &Property{Type: "object"}, Description: "Explicit list of entries to update. Each item: { path: string, updates: { status?, priority?, tags?, append?, note? } }"},
				"dry_run": {Type: "boolean", Description: "Preview changes without applying (default: false)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		// Validate: must have either (filter + updates) or entries, not both, not neither
		filter := sanitizeObjectArg(args["filter"])
		updates := sanitizeUpdateValue(args["updates"])

		// Merge the top-level `project` convenience shortcut into filter.project.
		// The nested value wins if the caller set both, so we don't clobber
		// intentional filter{}-only usage. This lets LLMs discover project
		// scoping without needing to know the nested filter shape.
		if topProject := StringArg(args, "project", ""); topProject != "" {
			filterMap, _ := filter.(map[string]any)
			if filterMap == nil {
				filterMap = make(map[string]any)
			}
			if _, alreadySet := filterMap["project"]; !alreadySet {
				filterMap["project"] = topProject
			}
			filter = filterMap
		}

		hasFilter := hasFields(filter)
		hasUpdates := hasFields(updates)
		_, hasEntries := args["entries"]

		if hasFilter && hasEntries {
			return "Cannot use both 'filter' and 'entries' — pick one mode", nil
		}
		if !hasFilter && !hasEntries {
			return "Please provide either 'filter' + 'updates' (filter mode) or 'entries' (explicit mode)", nil
		}
		if hasFilter && !hasUpdates {
			return "Filter mode requires 'updates' to specify what to change", nil
		}

		// Build request body — pass through to the API which handles full validation
		body := make(map[string]any)
		if hasFilter {
			body["filter"] = filter
			body["updates"] = updates
		}
		if hasEntries {
			body["entries"] = sanitizeBulkUpdateEntries(args["entries"])
		}
		body["dry_run"] = BoolArg(args, "dry_run", false)

		var resp struct {
			Updated int  `json:"updated"`
			Failed  int  `json:"failed"`
			Total   int  `json:"total"`
			DryRun  bool `json:"dry_run"`
			Results []struct {
				Path   string `json:"path"`
				ID     string `json:"id"`
				Title  string `json:"title"`
				Status string `json:"status"`
				Error  string `json:"error,omitempty"`
			} `json:"results"`
		}
		if err := client.Request(ctx, "POST", "/entries/bulk-update", body, nil, &resp); err != nil {
			return "", err
		}

		// Format response
		mode := "Applied"
		if resp.DryRun {
			mode = "Dry run"
		}

		lines := []string{
			fmt.Sprintf("## %s: Bulk Update", mode),
			"",
			fmt.Sprintf("- Total matched: %d", resp.Total),
			fmt.Sprintf("- Updated: %d", resp.Updated),
			fmt.Sprintf("- Failed: %d", resp.Failed),
			"",
		}

		if len(resp.Results) > 0 {
			lines = append(lines, "### Results")
			for _, r := range resp.Results {
				if r.Status == "ok" {
					lines = append(lines, fmt.Sprintf("- **%s** (`%s`) — updated", r.Title, r.Path))
				} else {
					lines = append(lines, fmt.Sprintf("- **%s** (`%s`) — error: %s", r.Title, r.Path, r.Error))
				}
			}
		}

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// brain_delete
// =============================================================================

func registerBrainDelete(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "delete",
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
		Name: "move",
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
		Name:        "stats",
		Description: "Get statistics about the brain storage.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"global":  {Type: "boolean", Description: "Show only global entries stats"},
				"project": {Type: "string", Description: "Scope stats to a project (e.g., 'orion-ai'). Takes precedence over 'global' when both are set."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		params := make(map[string]string)
		if v, ok := args["global"].(bool); ok {
			params["global"] = fmt.Sprintf("%t", v)
		}
		if v := StringArg(args, "project", ""); v != "" {
			params["project"] = v
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
		Name: "check_connection",
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
		Name:        "link",
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
		Name: "section",
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
		Name:        "plan_sections",
		Description: "Extract section headers from a plan entry for orchestration mapping.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path":    {Type: "string", Description: "Path to the plan entry"},
				"title":   {Type: "string", Description: "Title to search for"},
				"project": {Type: "string", Description: "When resolving by title, restrict the match to this project ID. Ignored when 'path' is provided."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		entryPath := StringArg(args, "path", "")

		if entryPath == "" {
			title := StringArg(args, "title", "")
			if title == "" {
				return "Please provide either a path or title", nil
			}

			searchBody := map[string]any{"query": title, "limit": 5}
			if project := StringArg(args, "project", ""); project != "" {
				searchBody["project"] = project
			}

			var searchResp struct {
				Results []struct {
					Path  string `json:"path"`
					Title string `json:"title"`
				} `json:"results"`
			}
			if err := client.Request(ctx, "POST", "/search", searchBody, nil, &searchResp); err != nil {
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
		Name:        "verify",
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
		Name:        "stale",
		Description: "Find entries that may need verification (not verified in N days).",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"days":    {Type: "number", Description: "Days threshold (default: 30)"},
				"type":    {Type: "string", Enum: types.EntryTypes, Description: "Filter by entry type"},
				"limit":   {Type: "number", Description: "Maximum results (default: 20)"},
				"project": {Type: "string", Description: "Restrict to entries under a specific project (e.g., 'orion-ai'). Omit for cross-project results."},
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
		if v := StringArg(args, "project", ""); v != "" {
			params["project"] = v
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
		Name:        "orphans",
		Description: "Find entries with no incoming links (orphans). Useful for knowledge graph health.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"type":    {Type: "string", Enum: types.EntryTypes, Description: "Filter by entry type"},
				"limit":   {Type: "number", Description: "Maximum results (default: 20)"},
				"project": {Type: "string", Description: "Restrict to entries under a specific project (e.g., 'orion-ai'). Omit for cross-project results."},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		params := map[string]string{
			"limit": fmt.Sprintf("%d", IntArg(args, "limit", 20)),
		}
		if v := StringArg(args, "type", ""); v != "" {
			params["type"] = v
		}
		if v := StringArg(args, "project", ""); v != "" {
			params["project"] = v
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
		Name:        "backlinks",
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
		Name:        "outlinks",
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
		Name:        "related",
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

// =============================================================================
// brain_automation_list
// =============================================================================

func registerBrainAutomationList(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "automation_list",
		Description: "List active automations with their trigger type, status, and last-fired info. Automations are event-driven behaviors stored as brain entries that replace hardcoded monitors.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"project": {Type: "string", Description: "Filter by project (optional, lists all if omitted)"},
				"status":  {Type: "string", Description: "Filter by status (default: all)", Enum: []string{"active", "archived", "draft"}},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		params := map[string]string{
			"type": "automation",
		}
		if project := StringArg(args, "project", ""); project != "" {
			params["project"] = project
		}
		if status := StringArg(args, "status", ""); status != "" {
			params["status"] = status
		}

		var resp struct {
			Entries []types.BrainEntry `json:"entries"`
			Total   int                `json:"total"`
		}
		if err := client.Request(ctx, "GET", "/entries", nil, params, &resp); err != nil {
			return "", err
		}

		if len(resp.Entries) == 0 {
			return "No automations found.\n\nCreate one with `brain automation create` or save a brain entry with type: automation.", nil
		}

		lines := []string{
			fmt.Sprintf("## Automations (%d)", resp.Total),
			"",
		}

		for _, entry := range resp.Entries {
			triggerType := ""
			triggerDetail := ""
			if entry.Trigger != nil {
				triggerType = entry.Trigger.Type
				switch entry.Trigger.Type {
				case "event":
					triggerDetail = entry.Trigger.Event
				case "cron":
					triggerDetail = entry.Trigger.Schedule
				case "webhook":
					triggerDetail = entry.Trigger.Webhook
				}
			}

			actionType := ""
			if entry.Action != nil {
				actionType = entry.Action.Type
			}

			project := entry.ProjectID
			if project == "" {
				project = "(global)"
			}

			statusIcon := "●"
			if entry.Status != "active" {
				statusIcon = "○"
			}

			lines = append(lines, fmt.Sprintf("%s **%s** (`%s`)", statusIcon, entry.Title, entry.ID))
			lines = append(lines, fmt.Sprintf("  Trigger: %s %s | Action: %s | Project: %s | Status: %s",
				triggerType, triggerDetail, actionType, project, entry.Status))
			lines = append(lines, "")
		}

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// brain_automation_test
// =============================================================================

func registerBrainAutomationTest(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "automation_test",
		Description: "Dry-run an event against active automations to see which would match. No tasks are created -- this is a simulation for debugging automation triggers.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"event":   {Type: "string", Description: "Event name to simulate (e.g., 'task.completed', 'feature.all_completed')"},
				"project": {Type: "string", Description: "Filter automations by project (optional)"},
			},
			Required: []string{"event"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		eventName := StringArg(args, "event", "")

		params := map[string]string{
			"type":   "automation",
			"status": "active",
		}
		if project := StringArg(args, "project", ""); project != "" {
			params["project"] = project
		}

		var resp struct {
			Entries []types.BrainEntry `json:"entries"`
			Total   int                `json:"total"`
		}
		if err := client.Request(ctx, "GET", "/entries", nil, params, &resp); err != nil {
			return "", err
		}

		lines := []string{
			fmt.Sprintf("## Simulating event: %q (dry-run)", eventName),
			"",
		}

		matched := 0
		for _, entry := range resp.Entries {
			if entry.Trigger == nil || entry.Trigger.Type != "event" {
				continue
			}
			if !matchesAutomationEvent(entry.Trigger.Event, eventName) {
				continue
			}
			matched++
			lines = append(lines, fmt.Sprintf("**MATCH:** %s (`%s`)", entry.Title, entry.ID))
			lines = append(lines, fmt.Sprintf("  Trigger: event=%s", entry.Trigger.Event))
			if entry.Action != nil {
				lines = append(lines, fmt.Sprintf("  Action: %s", entry.Action.Type))
				if entry.Action.DirectPrompt != "" {
					prompt := entry.Action.DirectPrompt
					if len(prompt) > 80 {
						prompt = prompt[:77] + "..."
					}
					lines = append(lines, fmt.Sprintf("  Prompt: %s", prompt))
				}
				if entry.Action.Command != "" {
					lines = append(lines, fmt.Sprintf("  Command: %s", entry.Action.Command))
				}
			}
			lines = append(lines, "")
		}

		if matched == 0 {
			lines = append(lines, fmt.Sprintf("No automations matched event %q (dry-run, no tasks created)", eventName))
		} else {
			lines = append(lines, fmt.Sprintf("---\n%d automation(s) would match (dry-run, no tasks created)", matched))
		}
		return strings.Join(lines, "\n"), nil
	})
}

// matchesAutomationEvent checks if an automation event pattern matches a given event name.
// Supports exact match and wildcard prefix (e.g., "task.*" matches "task.completed").
func matchesAutomationEvent(pattern, eventName string) bool {
	if pattern == eventName {
		return true
	}
	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, ".*")
		return strings.HasPrefix(eventName, prefix+".")
	}
	if pattern == "*" {
		return true
	}
	return false
}

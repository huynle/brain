package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// RegisterBrainTools registers the brain core entry, graph, automation,
// and attachment tools on the server.
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
// save
// =============================================================================

func registerBrainSave(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "save",
		Description: `Save content to the brain for future reference.

Entry types:
- summary, report, walkthrough, plan, decision, exploration: documentation entries (session summaries, analysis reports, code explanations, implementation plans, ADRs, research findings)
- pattern, learning: reusable knowledge and best practices (use global:true for cross-project)
- quirk: a project-specific gotcha — surprising behavior, non-obvious constraint, or required workaround (e.g. "tests hang unless FOO_ENV is unset", "the staging DB truncates timestamps"). Save one the moment you discover it, keep it short and factual, and include how to work around it. Recall a project's quirks at session start with list(type:'quirk') or inject(query, type:'quirk').
- idea, scratch: ideas for future exploration and temporary working notes
- task: a work item for the task runner (see task options below)
- automation: an event-driven behavior (see trigger, action, retry)
- reminder: something to come back to. Create these with the dedicated reminder_create tool, NOT with save — save cannot carry a reminder's date, action or prompt and will drop them silently.
- execution, dream, automation_run, merge_request: system-generated entries (rarely created by hand)

Only type, title, and content are required. The remaining parameters apply conditionally:
- Task options (depends_on, feature_*, schedule*, merge_*, executor, agent, model, direct_prompt, extensions, target_workdir, git_branch, execution_mode, complete_on_idle, checkout_mode, user_original_request) apply only when type is 'task' and are ignored for other types.
- trigger applies to 'task' and 'automation' entries; action and retry apply to 'automation' entries only.
- checkout_mode ("ai" default, or "simple") selects the feature-checkout automation path for a feature's post-completion checkout: "ai" runs the feature-checkout skill via LLM, "simple" runs a deterministic script-based squash merge.

Feature orchestration (tasks):
- Use feature_id to group tasks into a feature.
- Use feature_depends_on to make one feature wait for another feature to complete.
- Use trigger.event="feature.completed" with trigger.filter.feature_id to create post-feature tasks that activate after a feature completes.

If project is omitted, the entry is saved to the project detected from the MCP server's launch directory (see the context_get tool).`,
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
				"project":               {Type: "string", Description: "Project ID (e.g., 'orion-ai'). Defaults to the project detected from the MCP server's launch directory."},
				"depends_on":            {Type: "array", Items: &Property{Type: "string"}, Description: "Task dependencies - list of task IDs or titles"},
				"user_original_request": {Type: "string", Description: "Verbatim user request for this task. HIGHLY RECOMMENDED for tasks - enables validation during task completion. Supports multiline content, code blocks, and special characters. When creating multiple tasks from one user request, include this in EACH task."},
				"target_workdir":        {Type: "string", Description: "Explicit working directory override for task execution (absolute path). When set, the task runner will try this directory first before falling back to workdir resolution. Use for tasks that should execute in a specific directory."},
				"feature_id":            {Type: "string", Description: "Feature group ID for this task (e.g., 'auth-system', 'payment-flow'). Tasks with the same feature_id are grouped together for ordered execution."},
				"feature_priority":      {Type: "string", Enum: types.Priorities, Description: "Priority level for the feature group. Determines execution order relative to other features."},
				"feature_depends_on":    {Type: "array", Items: &Property{Type: "string"}, Description: "Feature IDs this feature depends on. All tasks in dependent features must complete before this feature's tasks can start. Use this for before-feature orchestration (e.g., feature 'main' depends on feature 'preflight')."},
				"trigger":               {Type: "object", Description: "Event trigger for inactive/active tasks or automation entries. For post-feature tasks use {event:'feature.completed', filter:{feature_id:'main-feature', project_id:'my-project'}}. Supports type (event, cron, webhook, session), event, schedule, webhook, filter, once_per, cooldown, max_concurrent, ignore_automation_events."},
				"action":                {Type: "object", Description: "Automation action config for automation entries. Common fields: type ('create_task' or 'script'), prompt_template, direct_prompt, command, agent, model, executor, target_workdir. Templates support Go syntax with {{.Project}}, {{.ProjectID}}, {{.EventProjectID}}, {{.FeatureID}}, {{.TaskID}}, {{.TaskPath}}, {{.TaskTitle}}, {{.FromStatus}}, {{.ToStatus}}."},
				"retry":                 {Type: "object", Description: "Automation retry policy. ⚠ CURRENTLY INERT: max_attempts and backoff round-trip through storage but nothing in the task lifecycle reads them — there is no attempt counter — and 'timeout' is not a field at all, so it is dropped at decode. Setting this changes nothing. Tracked for deletion or implementation."},
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
				"machine_affinity":      {Type: "string", Enum: types.MachineAffinities, Description: "Where this task may run, relative to the machine it was created on. 'preferred' (the default when the machine is known) favors a runner on this machine but allows any other. 'local' restricts it to this machine only — the task waits rather than running elsewhere. 'none' ignores the origin machine. The origin machine/client/path are stamped automatically; this only chooses the policy."},
				"complete_on_idle":      {Type: "boolean", Description: "Mark task as completed when agent becomes idle (default: false). Useful for fire-and-forget tasks."},
				"checkout_mode":         {Type: "string", Enum: types.CheckoutModes, Description: "Feature checkout automation mode: 'ai' (default) runs the feature-checkout skill; 'simple' triggers a deterministic squash-merge automation. Only meaningful on task entries whose feature completion triggers a checkout automation."},
				"related_entries":       {Type: "array", Items: &Property{Type: "string"}, Description: "Related brain entries to link, each named by title, path, or 8-char ID. Appended to the entry as a \"## Related\" section of wiki-links."},
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
			"relatedEntries": argAlias(args, "related_entries", "relatedEntries"),
		}

		// Task-specific enrichment
		if isTask {
			body["workdir"] = execCtx.Workdir
			body["git_remote"] = execCtx.GitRemote
			// Only auto-inject execCtx.GitBranch when feature_id is empty. When
			// feature_id is set, leave git_branch blank so the runner's
			// feature_id fallback (internal/runner/executor_common.go:86-88)
			// engages and worktree isolation works from any branch. Otherwise
			// a shell on main of a foreign repo would silently set
			// git_branch="main" and trip the main/master worktree skip.
			featureID, _ := args["feature_id"].(string)
			if featureID != "" {
				body["git_branch"] = StringArg(args, "git_branch", "")
			} else {
				body["git_branch"] = StringArg(args, "git_branch", execCtx.GitBranch)
			}
			body["target_workdir"] = args["target_workdir"]

			// Origin provenance: which machine, which client install, and
			// which absolute directory this task was authored in. Stamped
			// from the ambient context rather than taken from args, because
			// it describes the caller, not the caller's intent — an agent
			// cannot know these and should not be able to spoof them.
			//
			// Gated on the ambient context actually describing the caller.
			// Over the in-process HTTP transport it describes the Brain API
			// host instead, and stamping that would pin every task created
			// through it to the API server.
			if s.ambientContextDescribesCaller() {
				if execCtx.HostID != "" {
					body["origin_machine_id"] = execCtx.HostID
				}
				if execCtx.ClientID != "" {
					body["origin_client_id"] = execCtx.ClientID
				}
				if execCtx.AbsPath != "" {
					body["origin_path"] = execCtx.AbsPath
				}
			}
			// machine_affinity IS caller intent, so it comes from args
			// regardless of transport; left unset it resolves to "preferred"
			// whenever an origin machine is known, and to "none" otherwise
			// (see types.ResolveMachineAffinity).
			if v, ok := args["machine_affinity"].(string); ok && v != "" {
				// "local" needs an origin machine to be local TO. Over the
				// HTTP transport none is stamped, so the task would be
				// refused by every runner forever with
				// machine_affinity_unresolved. Fail here, where the caller
				// can still see why, instead of stranding it in the queue.
				if v == types.MachineAffinityLocal && !s.ambientContextDescribesCaller() {
					return "", fmt.Errorf("machine_affinity=%q is unavailable on this MCP server: it runs inside the Brain API and cannot identify your machine, so the task would never become runnable — use the stdio MCP server, or pass %q/%q",
						types.MachineAffinityLocal, types.MachineAffinityPreferred, types.MachineAffinityNone)
				}
				body["machine_affinity"] = v
			}

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
			if v, ok := args["checkout_mode"].(string); ok && v != "" {
				body["checkout_mode"] = v
			}
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
// recall
// =============================================================================

func registerBrainRecall(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "recall",
		Description: "Retrieve a specific entry from the brain by path, ID, or title. Provide 'path' (entry path or 8-char ID; takes precedence) or 'title' (resolved via search, then exact match).",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path":    {Type: "string", Description: "Entry path (e.g., 'projects/x/plan/abc12def.md') or 8-char entry ID. Takes precedence over 'title'."},
				"title":   {Type: "string", Description: "Title to search for (exact, case-sensitive match)"},
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
				return "", fmt.Errorf("provide 'path' or 'title'")
			}

			searchBody := map[string]any{"query": title, "limit": 20}
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
				if len(searchResp.Results) > 0 {
					suggestions := make([]string, 0, 5)
					for _, r := range searchResp.Results {
						suggestions = append(suggestions, r.Title)
						if len(suggestions) == 5 {
							break
						}
					}
					return "", fmt.Errorf("no exact title match for %q; closest titles: %s", title, strings.Join(suggestions, ", "))
				}
				return "", fmt.Errorf("no entry found with title %q", title)
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

			// Recalling a task without its priority, feature or
			// dependencies means a second call to task_get for facts that
			// were already on the wire. Rendered only when set, so a plain
			// note is not padded with task vocabulary.
			Priority  string   `json:"priority,omitempty"`
			FeatureID string   `json:"feature_id,omitempty"`
			DependsOn []string `json:"depends_on,omitempty"`
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

		// Task-shaped fields, shown only when set so a plain note is not
		// padded with vocabulary that does not apply to it.
		var taskFields strings.Builder
		if resp.Priority != "" {
			fmt.Fprintf(&taskFields, "\nPriority: %s", resp.Priority)
		}
		if resp.FeatureID != "" {
			fmt.Fprintf(&taskFields, "\nFeature: %s", resp.FeatureID)
		}
		if len(resp.DependsOn) > 0 {
			fmt.Fprintf(&taskFields, "\nDepends on: %s", strings.Join(resp.DependsOn, ", "))
		}

		return fmt.Sprintf("## %s\n\nPath: %s\nType: %s\nStatus: %s\nTags: %s%s%s%s\n\n---\n\n%s",
			resp.Title, resp.Path, resp.Type, resp.Status, tags, taskFields.String(), userRequest, attachments, resp.Content), nil
	})
}

// =============================================================================
// Attachment tools
// =============================================================================

func registerBrainAttachmentUpload(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "attachment_upload",
		Description: `Upload a file as a first-class Brain attachment.

Supply the bytes one of two ways: 'content' (base64) plus 'filename', which works from anywhere, or 'file_path', which only works when this MCP server runs on your own machine. A hosted server resolves 'file_path' on the API host and will reject it.

Use this for pasted-image or local-PDF workflows: upload the file with this tool, then attach the returned attachment_id to an entry with attachment_attach.`,
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project":   {Type: "string", Description: "Project ID that owns the uploaded attachment. Defaults to the project detected from the MCP server's launch directory."},
			"content":   {Type: "string", Description: "Base64-encoded file bytes. Requires 'filename'. Use this whenever the MCP server is not running on your own machine."},
			"filename":  {Type: "string", Description: "Filename to store the bytes under. Required with 'content'; otherwise taken from 'file_path'."},
			"file_path": {Type: "string", Description: "Path to the file to upload, resolved on the machine running the MCP server. Rejected by a hosted server — use 'content' there."},
			"metadata":  {Type: "object", Description: "Optional string key/value metadata stored with the attachment"},
		}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProjectArg(args)
		filePath := StringArg(args, "file_path", "")
		// Trim first so a whitespace-only 'content' reads as absent rather than
		// as a payload that decodes to nothing.
		encoded := strings.TrimSpace(StringArg(args, "content", ""))
		filename := StringArg(args, "filename", "")
		if projectID == "" || (filePath == "" && encoded == "") {
			return "", fmt.Errorf("provide 'content' (base64) with 'filename', or 'file_path' for a server-local file (and 'project' if no ambient project is available)")
		}
		if filePath != "" && encoded != "" {
			return "", fmt.Errorf("provide either 'content' or 'file_path', not both")
		}

		var (
			resp *types.CreateAttachmentResponse
			err  error
		)
		if encoded != "" {
			if filename == "" {
				return "", fmt.Errorf("'filename' is required when uploading with 'content'")
			}
			data, decodeErr := decodeBase64Content(encoded)
			if decodeErr != nil {
				return "", fmt.Errorf("decode 'content': %w", decodeErr)
			}
			resp, err = client.UploadAttachmentContent(ctx, projectID, filename, bytes.NewReader(data), stringMapArg(args, "metadata"))
		} else {
			if fsErr := s.requireLocalFilesystem("file_path", "read the file yourself and pass its bytes as base64 'content' with 'filename'"); fsErr != nil {
				return "", fsErr
			}
			resp, err = client.UploadAttachment(ctx, projectID, filePath, stringMapArg(args, "metadata"))
		}
		if err != nil {
			return "", err
		}

		return fmt.Sprintf("Uploaded attachment\n\n%s\n\nNext: attach it to an entry with `attachment_attach` using attachment_id `%s`.",
			formatAttachment(resp.Attachment), resp.Attachment.ID), nil
	})
}

// decodeBase64Content decodes a base64 tool argument, tolerating the shapes
// clients actually send: data URLs, line-wrapped payloads, and unpadded base64.
func decodeBase64Content(encoded string) ([]byte, error) {
	payload := strings.TrimSpace(encoded)
	if strings.HasPrefix(payload, "data:") {
		if idx := strings.Index(payload, ","); idx >= 0 {
			payload = payload[idx+1:]
		}
	}
	// Wrapped base64 carries newlines that are not part of the payload.
	payload = strings.Join(strings.Fields(payload), "")

	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		data, err = base64.RawStdEncoding.DecodeString(payload)
		if err != nil {
			return nil, fmt.Errorf("not valid base64: %w", err)
		}
	}
	if len(data) == 0 {
		return nil, fmt.Errorf("decoded to zero bytes")
	}
	return data, nil
}

func registerBrainAttachmentAttach(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "attachment_attach",
		Description: "Attach an existing Brain attachment to an entry with optional role and caption metadata.",
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project":       {Type: "string", Description: "Project ID containing the entry and attachment. Defaults to the project detected from the MCP server's launch directory."},
			"entry_id":      {Type: "string", Description: "Entry ID or path to attach to"},
			"attachment_id": {Type: "string", Description: "Attachment ID returned by attachment_upload or attachment_list"},
			"role":          {Type: "string", Description: "Optional attachment role, e.g. source, inline, image, pdf"},
			"caption":       {Type: "string", Description: "Optional model-friendly caption describing the attachment"},
		}, Required: []string{"entry_id", "attachment_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProjectArg(args)
		entryID := StringArg(args, "entry_id", "")
		attachmentID := StringArg(args, "attachment_id", "")
		if projectID == "" || entryID == "" || attachmentID == "" {
			return "", fmt.Errorf("provide 'entry_id' and 'attachment_id' (and 'project' if no ambient project is available)")
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
		if err := client.Request(ctx, "POST", "/entries/"+url.PathEscape(entryID)+"/attachments", body, map[string]string{"project_id": projectID}, &resp); err != nil {
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
			"project":       {Type: "string", Description: "Project ID containing the entry and attachment. Defaults to the project detected from the MCP server's launch directory."},
			"entry_id":      {Type: "string", Description: "Entry ID or path to detach from"},
			"attachment_id": {Type: "string", Description: "Attachment ID to detach"},
			"role":          {Type: "string", Description: "Optional role to detach"},
		}, Required: []string{"entry_id", "attachment_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProjectArg(args)
		entryID := StringArg(args, "entry_id", "")
		attachmentID := StringArg(args, "attachment_id", "")
		if projectID == "" || entryID == "" || attachmentID == "" {
			return "", fmt.Errorf("provide 'entry_id' and 'attachment_id' (and 'project' if no ambient project is available)")
		}

		params := map[string]string{"project_id": projectID}
		if role := StringArg(args, "role", ""); role != "" {
			params["role"] = role
		}
		var resp types.AttachEntryAttachmentResponse
		if err := client.Request(ctx, "DELETE", "/entries/"+url.PathEscape(entryID)+"/attachments/"+url.PathEscape(attachmentID), nil, params, &resp); err != nil {
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
			"project":  {Type: "string", Description: "Project ID whose attachments should be listed. Defaults to the project detected from the MCP server's launch directory."},
			"entry_id": {Type: "string", Description: "Optional entry ID or path for entry-scoped attachment references"},
		}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProjectArg(args)
		if projectID == "" {
			return "", fmt.Errorf("provide 'project' (no ambient project is available)")
		}
		// entry_id is documented as "entry ID or path", and the route is
		// GET /entries/{id}/attachments — a SINGLE path segment. An
		// unescaped path fell through to the GET /entries/* wildcard
		// instead, which returns a BrainEntry. That decodes cleanly into
		// AttachEntryAttachmentResponse because they share "path" and
		// "attachments", and BrainEntry.Attachments is include-gated and
		// this call sends no include — so the tool printed "No attachments
		// found for entry." for an entry with many, alongside a
		// correct-looking Path. A hard mismatch degrading into a confident
		// lie. fetchGraph below already escapes for exactly this reason.
		if entryID := StringArg(args, "entry_id", ""); entryID != "" {
			var resp types.AttachEntryAttachmentResponse
			if err := client.Request(ctx, "GET", "/entries/"+url.PathEscape(entryID)+"/attachments", nil, map[string]string{"project_id": projectID}, &resp); err != nil {
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
			"project":       {Type: "string", Description: "Project ID containing the attachment. Defaults to the project detected from the MCP server's launch directory."},
			"attachment_id": {Type: "string", Description: "Attachment ID to retrieve"},
		}, Required: []string{"attachment_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProjectArg(args)
		attachmentID := StringArg(args, "attachment_id", "")
		if projectID == "" || attachmentID == "" {
			return "", fmt.Errorf("provide 'attachment_id' (and 'project' if no ambient project is available)")
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
			"project":       {Type: "string", Description: "Project ID containing the attachment. Defaults to the project detected from the MCP server's launch directory."},
			"attachment_id": {Type: "string", Description: "Attachment ID to delete"},
		}, Required: []string{"attachment_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProjectArg(args)
		attachmentID := StringArg(args, "attachment_id", "")
		if projectID == "" || attachmentID == "" {
			return "", fmt.Errorf("provide 'attachment_id' (and 'project' if no ambient project is available)")
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
			"project":             {Type: "string", Description: "Project ID whose attachments should be backfilled. Defaults to the project detected from the MCP server's launch directory."},
			"dry_run":             {Type: "boolean", Description: "Report candidates without extracting text"},
			"force":               {Type: "boolean", Description: "Re-extract attachments that already have derived text"},
			"batch_size":          {Type: "number", Description: "Maximum attachments to process in one run"},
			"rate_limit_delay_ms": {Type: "number", Description: "Delay between extraction requests in milliseconds"},
		}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProjectArg(args)
		if projectID == "" {
			return "", fmt.Errorf("provide 'project' (no ambient project is available)")
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
			"project":       {Type: "string", Description: "Project ID containing the attachment. Defaults to the project detected from the MCP server's launch directory."},
			"attachment_id": {Type: "string", Description: "Attachment ID whose text extraction should be triggered"},
		}, Required: []string{"attachment_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProjectArg(args)
		attachmentID := StringArg(args, "attachment_id", "")
		if projectID == "" || attachmentID == "" {
			return "", fmt.Errorf("provide 'attachment_id' (and 'project' if no ambient project is available)")
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
			"project":       {Type: "string", Description: "Project ID containing the attachment. Defaults to the project detected from the MCP server's launch directory."},
			"attachment_id": {Type: "string", Description: "Attachment ID whose extracted text should be retrieved"},
		}, Required: []string{"attachment_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProjectArg(args)
		attachmentID := StringArg(args, "attachment_id", "")
		if projectID == "" || attachmentID == "" {
			return "", fmt.Errorf("provide 'attachment_id' (and 'project' if no ambient project is available)")
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

// maxInlineAttachmentBytes caps the raw bytes returned inline by
// attachment_download. Base64 inflates by 4/3, so this stays clear of the
// 10 MiB per-message ceiling both transports enforce.
const maxInlineAttachmentBytes = 5 * 1024 * 1024

func registerBrainAttachmentDownload(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "attachment_download",
		Description: `Fetch raw attachment bytes. Use this when an agent needs the exact original image, PDF, or media file for later processing.

Returns the bytes inline as base64 by default. 'output_path' writes them to a file instead, and only works when this MCP server runs on your own machine — a hosted server would write the file onto the API host.`,
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project":       {Type: "string", Description: "Project ID containing the attachment. Defaults to the project detected from the MCP server's launch directory."},
			"attachment_id": {Type: "string", Description: "Attachment ID whose raw content should be downloaded"},
			"output_path":   {Type: "string", Description: "Optional path to write the bytes to, resolved on the machine running the MCP server. Rejected by a hosted server; omit it to get base64 content inline."},
		}, Required: []string{"attachment_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProjectArg(args)
		attachmentID := StringArg(args, "attachment_id", "")
		outputPath := StringArg(args, "output_path", "")
		if projectID == "" || attachmentID == "" {
			return "", fmt.Errorf("provide 'attachment_id' (and 'project' if no ambient project is available)")
		}

		if outputPath != "" {
			if err := s.requireLocalFilesystem("output_path", "omit it to receive the bytes inline as base64"); err != nil {
				return "", err
			}
			if err := client.DownloadAttachmentToFile(ctx, projectID, attachmentID, outputPath); err != nil {
				return "", err
			}
			return fmt.Sprintf("Downloaded attachment %s to %s", attachmentID, outputPath), nil
		}

		data, contentType, err := client.DownloadAttachmentBytes(ctx, projectID, attachmentID, maxInlineAttachmentBytes)
		if err != nil {
			return "", err
		}
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		return fmt.Sprintf("## Attachment Content: %s\n\nContent-Type: %s\nSize: %d bytes\nEncoding: base64\n\n%s",
			attachmentID, contentType, len(data), base64.StdEncoding.EncodeToString(data)), nil
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
// search
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
				"limit":      {Type: "number", Description: "Maximum results (default: 20, per defaultSearchLimit in internal/storage/search.go)"},
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

		lines := []string{foundLine("entries", resp.Total, IntArg(args, "limit", 0), 20)}
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
// list
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
				"limit":      {Type: "number", Description: "Maximum entries to return (default: 100, per defaultListLimit in internal/storage/list.go)"},
				"global":     {Type: "boolean", Description: "List only global entries"},
				"sort_by":    {Type: "string", Enum: []string{"created", "modified", "priority"}, Description: "Sort order"},
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
		if v := StringArgAlias(args, "", "sort_by", "sortBy"); v != "" {
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

		// Decode the real response type. The hand-rolled struct here picked
		// six fields and silently dropped everything else the server sends —
		// including Truncated, which is the difference between "there are no
		// more" and "I stopped looking".
		var resp types.ListEntriesResponse
		if err := client.Request(ctx, "GET", "/entries", nil, params, &resp); err != nil {
			return "", err
		}

		if len(resp.Entries) == 0 {
			if resp.Truncated {
				// A filtered list runs its filter in Go over a bounded scan of
				// the table. Exhausting that window without a match is not the
				// same as "no such entry", and saying "No entries found" for it
				// is how a lookup by exact id used to deny entries that exist.
				return "No entries matched within the scan window, and the window was exhausted before the search finished — " +
					"matches may exist beyond it. Narrow the filters (project, type, status) or raise 'limit' and try again.", nil
			}
			return "No entries found", nil
		}

		lines := []string{foundLine("entries", resp.Total, IntArg(args, "limit", 0), 100)}
		if resp.Truncated {
			lines = append(lines, "_(scan window exhausted — more matches may exist beyond it; narrow the filters or raise 'limit')_")
		}
		for _, e := range resp.Entries {
			lines = append(lines, fmt.Sprintf("- **%s** (%s) - %s | %s", e.Title, e.Path, e.Type, e.Status))
		}
		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// inject
// =============================================================================

func registerBrainInject(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "inject",
		Description: "Search the brain and return relevant context for a task.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"query":       {Type: "string", Description: "What context are you looking for?"},
				"project":     {Type: "string", Description: "Filter by project ID (e.g., 'orion-ai'). Omit to search across all projects."},
				"max_entries": {Type: "number", Description: "Maximum entries to include (default: 5)"},
				"max_chars": {Type: "number", Description: "Bound the total assembled context in characters (default: 24000, roughly 6k tokens). " +
					"The budget is split evenly across the matched entries, and any entry cut short is marked with its path so you can recall it in full. " +
					"Pass a negative value for unbounded output."},
				"type": {Type: "string", Enum: types.EntryTypes, Description: "Filter by entry type"},
			},
			Required: []string{"query"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		body := map[string]any{"query": args["query"]}
		if v, ok := args["max_chars"]; ok {
			body["maxChars"] = v
		}
		if v := StringArg(args, "project", ""); v != "" {
			body["project"] = v
		}
		if v := StringArg(args, "type", ""); v != "" {
			body["type"] = v
		}
		if v := IntArgAlias(args, 0, "max_entries", "maxEntries"); v > 0 {
			body["maxEntries"] = v
		}

		var resp struct {
			Context string `json:"context"`
			Entries []struct {
				ID    string `json:"id"`
				Path  string `json:"path"`
				Title string `json:"title"`
				Type  string `json:"type"`
			} `json:"entries"`
		}
		if err := client.Request(ctx, "POST", "/inject", body, nil, &resp); err != nil {
			return "", err
		}

		if resp.Context == "" || len(resp.Entries) == 0 {
			return fmt.Sprintf("No relevant context found for %q", args["query"]), nil
		}
		return resp.Context, nil
	})
}

// =============================================================================
// update
// =============================================================================

func registerBrainUpdate(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "update",
		Description: `Update an existing brain entry: status, title, tags, priority, dependencies, trigger configuration, or body content (replace or append).

Use cases:
- Mark a plan as completed: update(path: "...", status: "completed")
- Mark as in-progress: update(path: "...", status: "in_progress")
- Block with reason: update(path: "...", status: "blocked", note: "Waiting on API design")
- Replace the full body: update(path: "...", content: "# Rewritten\n...")
- Append progress notes: update(path: "...", append: "## Progress\n- Completed auth module")
- Update title: update(path: "...", title: "New Title")
- Update dependencies: update(path: "...", depends_on: ["task-id-1", "task-id-2"])
- Update feature dependencies: update(path: "...", feature_depends_on: ["pre-feature"])
- Add a post-feature trigger: update(path: "...", trigger: {event:"feature.completed", filter:{feature_id:"main-feature"}})
- Update tags: update(path: "...", tags: ["tag1", "tag2"])
- Update priority: update(path: "...", priority: "high")

If both content and append are provided, content replaces the body first, then append is added to the end. Replacing content preserves the entry's path, ID, and incoming links (unlike delete + save).

Statuses: draft, pending, active, in_progress, blocked, cancelled, completed, validated, superseded, archived

Note: as a guard against clients that autofill every optional field, when 3 or more optional fields exactly match their documented defaults (` + formatOptionalDefaults() + `), those default-valued fields are ignored and listed in the response. To intentionally set several fields to those exact values, update them in separate calls.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path":                  {Type: "string", Description: "Path to the entry to update"},
				"status":                {Type: "string", Enum: types.EntryStatuses, Description: "New status"},
				"title":                 {Type: "string", Description: "New title"},
				"content":               {Type: "string", Description: "Replace the entry's full body content (markdown). Preserves path, ID, and links. Use 'append' to add to the end instead; if both are set, content is applied first."},
				"append":                {Type: "string", Description: "Content to append to the end of the entry body"},
				"note":                  {Type: "string", Description: "Short note to add"},
				"depends_on":            {Type: "array", Items: &Property{Type: "string"}, Description: "Task dependencies - list of task IDs or titles"},
				"tags":                  {Type: "array", Items: &Property{Type: "string"}, Description: "Update tags for the entry"},
				"priority":              {Type: "string", Enum: types.Priorities, Description: "Priority level"},
				"target_workdir":        {Type: "string", Description: "Explicit working directory override for task execution"},
				"workdir":               {Type: "string", Description: "Working directory relative to home (e.g., 'orion/orion-ai'). Used together with git_remote to resolve the repo context for execution."},
				"git_branch":            {Type: "string", Description: "Git branch for the task"},
				"git_remote":            {Type: "string", Description: "Git remote URL for the task's repo (e.g., 'git@gitlab.example.com:group/project.git'). Used together with workdir to resolve the repo context for execution."},
				"merge_target_branch":   {Type: "string", Description: "Branch to merge completed work into"},
				"merge_policy":          {Type: "string", Enum: types.MergePolicies, Description: "Merge behavior at checkout completion"},
				"merge_strategy":        {Type: "string", Enum: types.MergeStrategies, Description: "Git merge strategy"},
				"remote_branch_policy":  {Type: "string", Enum: types.RemoteBranchPolicies, Description: "Remote branch cleanup after merge"},
				"open_pr_before_merge":  {Type: "boolean", Description: "Require PR before merge"},
				"execution_mode":        {Type: "string", Enum: types.ExecutionModes, Description: "Task execution mode (default: worktree)"},
				"complete_on_idle":      {Type: "boolean", Description: "Mark task as completed when agent becomes idle"},
				"checkout_mode":         {Type: "string", Enum: types.CheckoutModes, Description: "Feature checkout automation mode: 'ai' (default) runs the feature-checkout skill; 'simple' triggers a deterministic squash-merge automation."},
				"schedule":              {Type: "string", Description: "Cron schedule expression (e.g., '*/5 * * * *')"},
				"schedule_enabled":      {Type: "boolean", Description: "Whether the schedule is active (default true when schedule exists). Set to false to pause scheduling."},
				"max_runs":              {Type: "number", Description: "Maximum number of scheduled runs before auto-disabling. Omit or set to 0 for unlimited."},
				"run_once_at":           {Type: "string", Description: "RFC3339 timestamp for one-time execution (e.g., '2025-06-15T10:00:00Z'). Task runs once at this time then auto-disables."},
				"timezone":              {Type: "string", Description: "IANA timezone for schedule interpretation (e.g., 'America/New_York', 'UTC'). Defaults to UTC if not set."},
				"starts_at":             {Type: "string", Description: "RFC3339 timestamp for when the schedule becomes active. Schedule won't trigger before this time."},
				"expires_at":            {Type: "string", Description: "RFC3339 timestamp for when the schedule expires. Must be after starts_at if both are set."},
				"feature_id":            {Type: "string", Description: "Feature group identifier (e.g., 'auth-system', 'payment-flow')"},
				"feature_priority":      {Type: "string", Enum: types.Priorities, Description: "Priority for this feature group"},
				"feature_depends_on":    {Type: "array", Items: &Property{Type: "string"}, Description: "Feature IDs this feature depends on. Use this for feature-to-feature ordering."},
				"trigger":               {Type: "object", Description: "Event trigger for inactive/active tasks or automation entries. For post-feature tasks use {event:'feature.completed', filter:{feature_id:'main-feature', project_id:'my-project'}}. Supports type (event, cron, webhook, session), event, schedule, webhook, filter, once_per, cooldown, max_concurrent, ignore_automation_events."},
				"action":                {Type: "object", Description: "Automation action config for automation entries. Common fields: type, prompt_template, direct_prompt, command, agent, model, executor, target_workdir. Templates support Go syntax with {{.Project}}, {{.ProjectID}}, {{.EventProjectID}}, {{.FeatureID}}, {{.TaskID}}, {{.TaskPath}}, {{.TaskTitle}}, {{.FromStatus}}, {{.ToStatus}}."},
				"retry":                 {Type: "object", Description: "Automation retry policy. ⚠ CURRENTLY INERT: max_attempts and backoff round-trip through storage but nothing in the task lifecycle reads them — there is no attempt counter — and 'timeout' is not a field at all, so it is dropped at decode. Setting this changes nothing. Tracked for deletion or implementation."},
				"feature_schedule":      {Type: "string", Description: "Cron schedule for all tasks in this feature group (e.g., '0 2 * * *')"},
				"feature_starts_at":     {Type: "string", Description: "RFC3339 timestamp for when the feature schedule becomes active"},
				"feature_expires_at":    {Type: "string", Description: "RFC3339 timestamp for when the feature schedule expires"},
				"feature_run_once_at":   {Type: "string", Description: "RFC3339 timestamp for one-time execution of all feature tasks"},
				"feature_timezone":      {Type: "string", Description: "IANA timezone for feature schedule interpretation (e.g., 'America/New_York')"},
				"direct_prompt":         {Type: "string", Description: "Direct prompt to execute, bypassing default skill workflow"},
				"user_original_request": {Type: "string", Description: "Verbatim user request that motivated this task, preserved for validation during feature checkout"},
				"agent":                 {Type: "string", Description: "Override agent for this task (e.g., 'explore', 'tdd-dev')"},
				"model":                 {Type: "string", Description: "Override model (format: 'provider/model-id')"},
				"executor":              {Type: "string", Enum: []string{"", "opencode", "pi", "script"}, Description: "Executor backend for this task: 'opencode', 'pi', or 'script'. Empty = use runner default."},
				"extensions":            {Type: "array", Items: &Property{Type: "string"}, Description: "Additional extensions to load for this task (e.g., ['code-review', 'auto-commit'])"},
				"machine_affinity":      {Type: "string", Enum: types.MachineAffinities, Description: "Where this task may run relative to its origin machine: 'local' (origin machine only), 'preferred' (favor it, allow others), 'none' (ignore it)."},
				"origin_machine_id":     {Type: "string", Description: "Re-home this task to a different machine. Normally stamped automatically at creation — set it only to move a task whose origin machine is gone or wrong."},
				"origin_path":           {Type: "string", Description: "Absolute directory on the origin machine that this task should run in. Normally stamped automatically at creation."},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		path := StringArg(args, "path", "")
		cleanArgs, ignoredDefaults := sanitizeUpdateArgs(args)

		body := map[string]any{}
		addStringUpdateFields(body, cleanArgs,
			"status", "title", "content", "append", "note", "priority", "target_workdir", "workdir", "git_branch", "git_remote",
			"merge_target_branch", "merge_policy", "merge_strategy", "remote_branch_policy", "execution_mode",
			"schedule", "run_once_at", "timezone", "starts_at", "expires_at", "feature_id", "feature_priority",
			"feature_schedule", "feature_starts_at", "feature_expires_at", "feature_run_once_at", "feature_timezone",
			"direct_prompt", "user_original_request", "agent", "model", "executor", "checkout_mode",
			"machine_affinity", "origin_machine_id", "origin_path",
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
		if v := StringArg(cleanArgs, "content", ""); v != "" {
			changes = append(changes, fmt.Sprintf("Replaced content (%d characters)", len(v)))
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

		ignoredNote := ""
		if len(ignoredDefaults) > 0 {
			sort.Strings(ignoredDefaults)
			ignoredNote = fmt.Sprintf("\n\n**Ignored default-valued fields** (autofill guard, see tool description): %s",
				strings.Join(ignoredDefaults, ", "))
		}

		return fmt.Sprintf("Updated: %s\n\n**Changes:**\n%s%s\n\n**Current Status:** %s\n**Title:** %s\n\nUse `recall` to view the full entry.",
			resp.Path, strings.Join(changeLines, "\n"), ignoredNote, resp.Status, resp.Title), nil
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

// formatOptionalDefaults renders openCodeOptionalDefaults as the "key: value"
// list quoted in the update tool's description.
//
// The list used to be hand-written prose sitting a couple hundred lines away
// from the table it described, and commit 64093da corrected the table without
// touching the prose - so the description advertised merge_policy:"prompt_only"
// and remote_branch_policy:"keep" while the guard actually matched "auto_merge"
// and "delete". Both were backwards, which is worse than merely stale: an agent
// reading it would believe an explicit "auto_merge" was safe from the guard
// (it is the value that trips it) and that "keep" was a default to avoid (it is
// a deliberate non-default the guard now passes through). Generating the prose
// from the map removes the possibility of them disagreeing again.
//
// Keys are sorted so the tool description is byte-identical across processes;
// Go map iteration order is randomized, and an MCP client that caches or diffs
// tool schemas should not see them churn.
func formatOptionalDefaults() string {
	keys := make([]string, 0, len(openCodeOptionalDefaults))
	for k := range openCodeOptionalDefaults {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		switch v := openCodeOptionalDefaults[k].(type) {
		case string:
			parts = append(parts, fmt.Sprintf("%s: %q", k, v))
		default:
			parts = append(parts, fmt.Sprintf("%s: %v", k, v))
		}
	}
	return strings.Join(parts, ", ")
}

var openCodeOptionalDefaults = map[string]any{
	"priority":         "medium",
	"feature_priority": "high",
	// merge_policy and remote_branch_policy were "prompt_only" and "keep",
	// which are not the defaults — normalizeFeatureCheckoutOptions in
	// internal/service/task.go turns an empty value into "auto_merge" and
	// "delete". Both entries were therefore backwards: the guard discarded
	// remote_branch_policy:"keep" (a deliberate non-default choice) and
	// never recognised merge_policy:"auto_merge" (the actual default).
	"merge_policy":         "auto_merge",
	"merge_strategy":       "squash",
	"remote_branch_policy": "delete",
	"execution_mode":       "worktree",
	"executor":             "opencode",
	"open_pr_before_merge": false,
	"complete_on_idle":     false,
	"schedule_enabled":     false,
	"max_runs":             0,
	"checkout_mode":        "ai",
}

// sanitizeUpdateArgs drops empty-string values and guards against clients
// that autofill optional fields: when 3 or more fields exactly match their
// documented defaults (openCodeOptionalDefaults), those fields are removed
// so the update doesn't clobber intentional per-task settings. The removed
// keys are returned so callers can surface them instead of failing silently.
func sanitizeUpdateArgs(args map[string]any) (map[string]any, []string) {
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
		return clean, nil
	}

	dropped := make([]string, 0, defaultCount)
	for key, value := range clean {
		if matchesOpenCodeOptionalDefault(key, value) {
			delete(clean, key)
			dropped = append(dropped, key)
		}
	}
	return clean, dropped
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

// sanitizeUpdateValue applies the defaults guard to a bulk "updates" object
// and reports which keys it removed.
//
// The dropped list used to be discarded here (`clean, _ :=`), so bulk_update
// could strip fields from every matched entry and still answer "Updated: N"
// — a success report over a write that partly did not happen. Single update
// surfaced the same list all along; only the bulk path hid it.
func sanitizeUpdateValue(value any) (any, []string) {
	obj, ok := value.(map[string]any)
	if !ok {
		return value, nil
	}
	clean, dropped := sanitizeUpdateArgs(obj)
	return clean, dropped
}

// sanitizeBulkUpdateEntries applies the defaults guard to every entry in
// explicit mode and returns the union of keys it removed.
//
// The guard runs PER ENTRY, so one call can legitimately apply different
// field sets to different entries. Reporting the union is what makes that
// visible at all — previously nothing was reported and the response simply
// claimed every entry was updated.
func sanitizeBulkUpdateEntries(value any) (any, []string) {
	var dropped []string
	sanitizeAll := func(entries []any) any {
		clean := make([]any, 0, len(entries))
		for _, entry := range entries {
			cleanEntry, entryDropped := sanitizeBulkUpdateEntry(entry)
			clean = append(clean, cleanEntry)
			dropped = append(dropped, entryDropped...)
		}
		return clean
	}

	switch entries := value.(type) {
	case []any:
		return sanitizeAll(entries), dropped
	case []map[string]any:
		asAny := make([]any, 0, len(entries))
		for _, entry := range entries {
			asAny = append(asAny, entry)
		}
		return sanitizeAll(asAny), dropped
	default:
		return value, nil
	}
}

func sanitizeBulkUpdateEntry(value any) (any, []string) {
	entry, ok := value.(map[string]any)
	if !ok {
		return value, nil
	}

	var dropped []string
	clean := make(map[string]any, len(entry))
	for key, field := range entry {
		if key == "updates" {
			cleaned, fieldDropped := sanitizeUpdateValue(field)
			clean[key] = cleaned
			dropped = append(dropped, fieldDropped...)
			continue
		}
		clean[key] = field
	}
	return clean, dropped
}

// dedupeSorted returns the distinct values of in, sorted, for stable output.
func dedupeSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// =============================================================================
// bulk_update
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
  bulk_update({ filter: { feature_id: "old-feature" }, updates: { status: "cancelled" } })
- Update specific entries:
  bulk_update({ entries: [{ path: "projects/x/task/abc.md", updates: { status: "completed" } }] })
- Preview changes:
  bulk_update({ filter: { status: "draft" }, updates: { status: "pending" }, dry_run: true })`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"project": {Type: "string", Description: "Convenience shortcut for filter.project: restrict updates to entries in this project (e.g., 'orion-ai'). Only used in filter mode; explicit-entries mode ignores this. If filter already has a project field, that value wins."},
				"filter":  {Type: "object", Description: "Filter criteria to select entries. Fields: feature_id (string), project (string), type (string), status (string), tags (string[]), priority (string), generated_by (string), generated_key (string), agent (string), executor (string), execution_mode (string). Unknown filter fields are rejected with HTTP 400 to prevent data-loss from typos. Use with 'updates'."},
				"updates": {Type: "object", Description: "Updates to apply to matched entries. Accepts the SAME fields as the 'update' tool, not only the editorial ones — the body decodes into UpdateEntryRequest. Editorial: status, title, content (replaces body), append, note, tags, priority. Execution (the reason to bulk-edit tasks at all): agent, model, executor, execution_mode, target_workdir, workdir, git_branch, git_remote, depends_on, feature_id, feature_priority, feature_depends_on, complete_on_idle, checkout_mode, extensions, direct_prompt. Merge: merge_policy, merge_strategy, merge_target_branch, remote_branch_policy, open_pr_before_merge. Scheduling: schedule, schedule_enabled, timezone, starts_at, expires_at, run_once_at, max_runs. Unknown fields inside 'updates' are rejected with HTTP 400, same as unknown filter fields. Use with 'filter'."},
				"entries": {Type: "array", Items: &Property{Type: "object"}, Description: "Explicit list of entries to update. Each item: { path: string, updates: {...} }, where updates accepts the same full field set described above — not only status/priority/tags/content/append/note."},
				"dry_run": {Type: "boolean", Description: "Preview changes without applying (default: false)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		// Validate: must have either (filter + updates) or entries, not both, not neither
		filter := sanitizeObjectArg(args["filter"])
		updates, updatesDropped := sanitizeUpdateValue(args["updates"])

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
			return "", fmt.Errorf("cannot use both 'filter' and 'entries' — pick one mode")
		}
		if !hasFilter && !hasEntries {
			return "", fmt.Errorf("provide either 'filter' + 'updates' (filter mode) or 'entries' (explicit mode)")
		}
		if hasFilter && !hasUpdates {
			// Distinguish "you sent no updates" from "the defaults guard
			// removed all of them". The second used to surface as the
			// first, telling a caller who HAD specified updates that they
			// had not.
			if len(updatesDropped) > 0 {
				return "", fmt.Errorf(
					"every field in 'updates' matched its documented default and was dropped by the autofill guard (%s); "+
						"to set these values intentionally, update them in separate calls",
					strings.Join(dedupeSorted(updatesDropped), ", "))
			}
			return "", fmt.Errorf("filter mode requires 'updates' to specify what to change")
		}

		// Build request body — pass through to the API which handles full validation
		body := make(map[string]any)
		if hasFilter {
			body["filter"] = filter
			body["updates"] = updates
		}
		entriesDropped := []string{}
		if hasEntries {
			cleanEntries, dropped := sanitizeBulkUpdateEntries(args["entries"])
			body["entries"] = cleanEntries
			entriesDropped = dropped
		}
		body["dry_run"] = BoolArg(args, "dry_run", false)

		var resp struct {
			Updated int  `json:"updated"`
			Failed  int  `json:"failed"`
			Total   int  `json:"total"`
			DryRun  bool `json:"dry_run"`
			// types.BulkUpdateResponse documents Truncated as "do not
			// proceed; narrow the filter" and MatchedTotal as how many
			// entries the filter really matched. Neither was decoded, so a
			// filter matching 500 entries reported "Total matched: 100"
			// with no hint that 400 were left untouched — reviving exactly
			// the silent truncation those fields were added to end.
			Truncated    bool `json:"truncated,omitempty"`
			MatchedTotal int  `json:"matched_total,omitempty"`
			Results      []struct {
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

		if resp.Truncated {
			matched := "more than the safety cap allows"
			if resp.MatchedTotal > 0 {
				matched = fmt.Sprintf("%d", resp.MatchedTotal)
			}
			if resp.DryRun {
				lines = append(lines,
					fmt.Sprintf("> 🛑 TRUNCATED — the filter matches %s entries but only %d fit under the safety cap.", matched, resp.Total),
					"> Do not proceed: narrow the filter, or the live run will silently leave the rest untouched.",
					"")
			} else {
				lines = append(lines,
					fmt.Sprintf("> 🛑 TRUNCATED — the filter matched %s entries; only the first %d were changed.", matched, resp.Total),
					"> The remainder are UNMODIFIED. Narrow the filter and run again to finish.",
					"")
			}
		}

		// Never report a bare "Updated: N" over fields the guard removed.
		// The write partly did not happen, and the caller cannot see that
		// from the counts alone.
		if allDropped := dedupeSorted(append(append([]string{}, updatesDropped...), entriesDropped...)); len(allDropped) > 0 {
			lines = append(lines,
				fmt.Sprintf("> ⚠ Not applied: %s — these matched their documented defaults and were dropped by the autofill guard.",
					strings.Join(allDropped, ", ")),
				"> To set them intentionally, update them in separate calls.",
				"")
		}

		if len(resp.Results) > 0 {
			lines = append(lines, "### Results")
			for _, r := range resp.Results {
				if r.Status == "ok" {
					// A dry run mutates nothing. Labelling its rows
					// "updated" reads as a completed write, which is the
					// opposite of what a preview is for.
					verb := "updated"
					if resp.DryRun {
						verb = "would update"
					}
					lines = append(lines, fmt.Sprintf("- **%s** (`%s`) — %s", r.Title, r.Path, verb))
				} else {
					lines = append(lines, fmt.Sprintf("- **%s** (`%s`) — error: %s", r.Title, r.Path, r.Error))
				}
			}
		}

		return strings.Join(lines, "\n"), nil
	})
}

// foundLine renders a result count without implying it is a grand total.
//
// The API's Total is len(entries) for the returned page, so "Found 3
// entries" against a project holding 931 is not a small inaccuracy — it is
// the wrong answer to "how many are there". Producing the real number needs
// a COUNT(*) the storage layer does not currently do, but a page that came
// back exactly full is a dependable sign that more exist, and saying so is
// both honest and actionable.
func foundLine(noun string, count, requestedLimit, defaultLimit int) string {
	effective := requestedLimit
	if effective <= 0 {
		effective = defaultLimit
	}
	if count >= effective {
		return fmt.Sprintf("Found %d %s (page is full at limit %d — there are likely more; raise 'limit' or narrow the filters):\n",
			count, noun, effective)
	}
	return fmt.Sprintf("Found %d %s:\n", count, noun)
}

// =============================================================================
// delete
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
			return "", fmt.Errorf("set confirm: true to delete the entry")
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
// move
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

Example: move({ path: "projects/old/task/abc12def.md", project: "new-project" })`,
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
			return "", fmt.Errorf("provide both 'path' and target 'project'")
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
// stats
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

		// Label each count by what it actually counts.
		//
		// TotalEntries is scoped by the request; GlobalEntries and
		// ProjectEntries are always the WHOLE-STORE totals for global/
		// and projects/ respectively, so callers can compare a scoped
		// result against them (see BrainServiceImpl.GetStats).
		//
		// Printed as a bare "Project:" directly under a project-scoped
		// "Total:", that read as a contradiction — stats(project:"x")
		// showed "Total: 930 / Project: 67935" — and the larger number
		// is the one a reader trusts. Name the scope in every line.
		scopeLabel := "Total (whole store)"
		if p := params["project"]; p != "" {
			scopeLabel = fmt.Sprintf("Total (project %s)", p)
		} else if params["global"] == "true" {
			scopeLabel = "Total (global entries)"
		}

		lines := []string{
			"## Brain Statistics\n",
			fmt.Sprintf("%s: %d", scopeLabel, resp.TotalEntries),
			fmt.Sprintf("Global entries, all of global/: %d", resp.GlobalEntries),
			fmt.Sprintf("Project entries, all of projects/ across every project: %d", resp.ProjectEntries),
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
// check_connection
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
		// types.HealthResponse is {status, timestamp, embedding} — there is
		// no version, so "Version: " printed blank. Embedding was dropped,
		// and it is the one signal telling an agent whether semantic search
		// is usable or has silently fallen back to FTS.
		var resp types.HealthResponse
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

		// Report embedding readiness. An agent choosing between
		// strategy:"semantic" and the FTS default has no other way to learn
		// that the embedding provider is down, and semantic search degrades
		// quietly rather than erroring.
		embedding := resp.Embedding.Status
		if embedding == "" {
			embedding = "unknown"
		}
		if !resp.Embedding.Enabled {
			embedding = "disabled (semantic search unavailable; searches use FTS)"
		} else if resp.Embedding.Provider != "" {
			embedding = fmt.Sprintf("%s (%s / %s)", embedding, resp.Embedding.Provider, resp.Embedding.Model)
		}

		return fmt.Sprintf(`Brain API Status: CONNECTED

Server URL: %s
Health: %s
Embeddings: %s

All brain tools (save, recall, search, inject, etc.) are available.`, client.baseURL, resp.Status, embedding), nil
	})
}

// =============================================================================
// link
// =============================================================================

func registerBrainLink(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "link",
		Description: "Generate a markdown link to a brain entry. Use this when referencing other brain entries to ensure proper link resolution with mkdnflow.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"title":      {Type: "string", Description: "Title to search for"},
				"path":       {Type: "string", Description: "Direct path or ID (8-char alphanumeric) to the entry"},
				"with_title": {Type: "boolean", Description: "Include title in link (default: true)"},
			},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		if StringArg(args, "path", "") == "" && StringArg(args, "title", "") == "" {
			return "", fmt.Errorf("provide a 'path', ID, or 'title' to generate a link")
		}

		body := map[string]any{
			"title":     args["title"],
			"path":      args["path"],
			"withTitle": argAlias(args, "with_title", "withTitle"),
		}

		// types.LinkResponse has exactly one field, Link. ID, Path and
		// Title never existed, so the output was always three blank lines
		// presented as data.
		var resp types.LinkResponse
		if err := client.Request(ctx, "POST", "/link", body, nil, &resp); err != nil {
			return "", err
		}

		return fmt.Sprintf("Link: %s", resp.Link), nil
	})
}

// =============================================================================
// section
// =============================================================================

func registerBrainSection(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "section",
		Description: `Retrieve a specific section's FULL CONTENT from a brain plan by section title.

Use this when you need the detailed implementation spec for your assigned task.
Returns the exact section content including all subsections, code examples, and acceptance criteria.

Example: section({ plan_id: "projects/abc/plan/auth.md", section_title: "JWT Middleware" })

This is more precise than inject (which uses fuzzy search) - it extracts the exact section you need.`,
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"plan_id":             {Type: "string", Description: "Brain plan path (from orchestration context or the plan_sections tool)"},
				"section_title":       {Type: "string", Description: "Section title to retrieve (can be partial match)"},
				"include_subsections": {Type: "boolean", Description: "Include nested subsections (default: true)"},
			},
			Required: []string{"plan_id", "section_title"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		planId := StringArgAlias(args, "", "plan_id", "planId")
		sectionTitle := StringArgAlias(args, "", "section_title", "sectionTitle")
		if planId == "" || sectionTitle == "" {
			return "", fmt.Errorf("provide both 'plan_id' and 'section_title'")
		}

		encodedTitle := url.PathEscape(sectionTitle)
		params := map[string]string{}
		if BoolArgAlias(args, true, "include_subsections", "includeSubsections") {
			params["includeSubsections"] = "true"
		} else {
			params["includeSubsections"] = "false"
		}

		// types.SectionContentResponse is {title, content, path,
		// includeSubsections}. The hand-rolled struct declared level and
		// line, neither of which exists, so every section rendered
		// "**Line:** 0".
		var resp types.SectionContentResponse
		if err := client.Request(ctx, "GET", "/entries/"+url.PathEscape(planId)+"/sections/"+encodedTitle, nil, params, &resp); err != nil {
			return "", err
		}

		return fmt.Sprintf("## Section: %s\n\n**Plan:** %s\n\n---\n\n%s",
			resp.Title, planId, resp.Content), nil
	})
}

// =============================================================================
// plan_sections
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
				return "", fmt.Errorf("provide either a 'path' or 'title'")
			}

			searchBody := map[string]any{"query": title, "limit": 20}
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
						if len(suggestions) == 5 {
							break
						}
					}
					return "", fmt.Errorf("no exact title match for %q; closest titles: %s", title, strings.Join(suggestions, ", "))
				}
				return "", fmt.Errorf("no entry found with title %q", title)
			}
		}

		// types.SectionsResponse is {sections, path}, and SectionHeader is
		// {title, level}. The hand-rolled struct declared a Total and a
		// per-section Line, neither of which exists — so a 40-section plan
		// reported "**Total sections:** 0" and every heading claimed
		// "(line 0)". Count the slice instead.
		var sectionsResp types.SectionsResponse
		if err := client.Request(ctx, "GET", "/entries/"+url.PathEscape(entryPath)+"/sections", nil, nil, &sectionsResp); err != nil {
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
			fmt.Sprintf("**Total sections:** %d", len(sectionsResp.Sections)),
			"",
		}

		for _, section := range sectionsResp.Sections {
			indent := strings.Repeat("  ", section.Level-1)
			lines = append(lines, fmt.Sprintf("%s- %s", indent, section.Title))
		}

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// verify
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
// stale
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

		// GET /stale writes a BARE array of BrainEntry
		// (internal/api/health_extended.go:76). Decoding it into an
		// {entries,total} envelope is an unmarshal error, which api_client
		// turns into a hard failure — so this tool returned
		// "cannot unmarshal array into Go value of type struct{...}" on
		// EVERY call and had done since it was written. GetStale builds its
		// slice with make(...,0,n), so there is no null-response path where
		// it could accidentally work.
		//
		// The old struct also carried a phantom daysSinceVerified; the real
		// field is last_verified, and the underscore defeats Go's
		// case-insensitive key fallback.
		var resp []types.BrainEntry
		if err := client.Request(ctx, "GET", "/stale", nil, params, &resp); err != nil {
			return "", err
		}

		if len(resp) == 0 {
			return fmt.Sprintf("No stale entries found (all verified within %d days)", days), nil
		}

		lines := []string{
			fmt.Sprintf("## Stale Entries (not verified in %d days)", days),
			"",
			foundLine("entries needing verification", len(resp), IntArg(args, "limit", 0), 100),
		}

		for _, e := range resp {
			lastVerified := e.LastVerified
			if lastVerified == "" {
				lastVerified = "never"
			}
			lines = append(lines, fmt.Sprintf("- **%s**", e.Title))
			lines = append(lines, fmt.Sprintf("  `%s` | Last verified: %s", e.Path, lastVerified))
		}

		lines = append(lines, "")
		lines = append(lines, "*Use `verify` to mark entries as still accurate.*")

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// orphans
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

		// GET /orphans writes a BARE array of BrainEntry
		// (internal/api/health_extended.go:47), exactly like /stale. Same
		// unmarshal error, same hard failure on every call. fetchGraph two
		// hundred lines below already carries the comment "They return a
		// BARE BrainEntry array (not {entries,total})" — the knowledge was
		// in this file and these two sites never received it.
		var resp []types.BrainEntry
		if err := client.Request(ctx, "GET", "/orphans", nil, params, &resp); err != nil {
			return "", err
		}

		if len(resp) == 0 {
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
			foundLine("entries with no incoming links", len(resp), IntArg(args, "limit", 0), 100),
		}

		for _, e := range resp {
			lines = append(lines, fmt.Sprintf("- **%s** (`%s`) - %s", e.Title, e.Path, e.Type))
		}

		lines = append(lines, "")
		lines = append(lines, "*Consider linking these notes from related entries to improve knowledge graph connectivity.*")

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// backlinks
// =============================================================================

// graphEntry is the subset of BrainEntry the graph tools render.
type graphEntry struct {
	ID    string `json:"id"`
	Path  string `json:"path"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

// fetchGraph calls one of the /entries/{id}/(backlinks|outlinks|related)
// endpoints. They return a BARE BrainEntry array (not {entries,total}),
// and their routes only carry a single path segment — so slash paths
// must be escaped into one segment (the server resolves short IDs,
// exact paths, and titles).
func fetchGraph(ctx context.Context, client *APIClient, path, kind string, params map[string]string) ([]graphEntry, error) {
	var entries []graphEntry
	if err := client.Request(ctx, "GET", "/entries/"+url.PathEscape(path)+"/"+kind, nil, params, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func registerBrainBacklinks(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "backlinks",
		Description: "Find entries that link TO a given entry (backlinks).",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"path": {Type: "string", Description: "Path to the target note (entry path or 8-char ID)"},
			},
			Required: []string{"path"},
		},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		path := StringArg(args, "path", "")

		entries, err := fetchGraph(ctx, client, path, "backlinks", nil)
		if err != nil {
			return "", err
		}

		if len(entries) == 0 {
			return fmt.Sprintf("No backlinks found for: %s", path), nil
		}

		lines := []string{
			fmt.Sprintf("## Backlinks to: %s", path),
			"",
			fmt.Sprintf("Found %d entries linking to this note:", len(entries)),
			"",
		}

		for _, e := range entries {
			lines = append(lines, fmt.Sprintf("- **%s** (`%s`) - %s", e.Title, e.Path, e.Type))
		}

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// outlinks
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

		entries, err := fetchGraph(ctx, client, path, "outlinks", nil)
		if err != nil {
			return "", err
		}

		if len(entries) == 0 {
			return fmt.Sprintf("No outlinks found from: %s", path), nil
		}

		lines := []string{
			fmt.Sprintf("## Outlinks from: %s", path),
			"",
			fmt.Sprintf("Found %d entries linked from this note:", len(entries)),
			"",
		}

		for _, e := range entries {
			lines = append(lines, fmt.Sprintf("- **%s** (`%s`) - %s", e.Title, e.Path, e.Type))
		}

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// related
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

		entries, err := fetchGraph(ctx, client, path, "related", params)
		if err != nil {
			return "", err
		}

		if len(entries) == 0 {
			return fmt.Sprintf("No related entries found for: %s", path), nil
		}

		lines := []string{
			fmt.Sprintf("## Related to: %s", path),
			"",
			fmt.Sprintf("Found %d entries sharing links:", len(entries)),
			"",
		}

		for _, e := range entries {
			lines = append(lines, fmt.Sprintf("- **%s** (`%s`) - %s", e.Title, e.Path, e.Type))
		}

		return strings.Join(lines, "\n"), nil
	})
}

// =============================================================================
// automation_list
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
			return "No automations found.\n\nCreate one with the `save` tool using type: 'automation' (or the `brain automation create` CLI).", nil
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
// automation_test
// =============================================================================

func registerBrainAutomationTest(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "automation_test",
		Description: "Dry-run an event against active automations. Reports EVENT-PATTERN matches only — see the caveat in the output. " +
			"No tasks are created; this is a simulation for debugging automation triggers.",
		InputSchema: InputSchema{
			Type: "object",
			Properties: map[string]Property{
				"event": {Type: "string", Description: "Event name to simulate (e.g., 'task.completed', 'feature.completed'). " +
					"Must be a real event type — 'feature.all_completed' is NOT one; it exists only on a dead internal bus and nothing publishes it."},
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
		skippedNonEvent := 0
		for _, entry := range resp.Entries {
			if entry.Trigger == nil {
				continue
			}
			// Non-event triggers cannot be simulated here at all. They used
			// to be dropped silently, so an agent debugging a cron or
			// webhook automation saw "no matches" and concluded its filter
			// was wrong.
			if entry.Trigger.Type != "" && entry.Trigger.Type != "event" {
				skippedNonEvent++
				continue
			}
			// Goal automations are excluded by the real matcher
			// (isGoalAutomation in automation_service.go), so reporting them
			// as matching is simply wrong.
			if entry.GeneratedBy == "brain-goal" || entry.Goal != nil {
				continue
			}
			// The server matches Trigger.Events as well as Trigger.Event.
			// Checking only the singular reported every multi-event
			// automation — which includes the default goal shape — as not
			// matching while it really does.
			patterns := entry.Trigger.EventPatterns()
			hit := false
			for _, pattern := range patterns {
				if matchesAutomationEvent(pattern, eventName) {
					hit = true
					break
				}
			}
			if !hit {
				continue
			}
			matched++
			lines = append(lines, fmt.Sprintf("**EVENT PATTERN MATCHES:** %s (`%s`)", entry.Title, entry.ID))
			lines = append(lines, fmt.Sprintf("  Trigger: event=%s", strings.Join(patterns, ", ")))
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
			lines = append(lines, fmt.Sprintf("No automation's EVENT PATTERN matched %q (dry-run, no tasks created)", eventName))
		} else {
			lines = append(lines, fmt.Sprintf("---\n%d automation(s) match on event pattern (dry-run, no tasks created)", matched))
		}

		// State what was not evaluated. This simulation reimplements only
		// the event-pattern half of the server's matcher, so a bare "MATCH"
		// verdict was authoritative-looking and wrong in both directions:
		// it reported automations whose filters exclude the event, and it
		// stayed silent about ones it could not evaluate at all. The tool
		// exists to answer "why did my automation not fire", which is
		// precisely the question a partial answer misleads on.
		lines = append(lines,
			"",
			"> ⚠ This checks the EVENT PATTERN only. Not evaluated here: trigger filters",
			"> (project scope, to_status, checkout_mode, ...), once_per dedup, cooldown, and",
			"> max_concurrent. An automation listed above can still be skipped by any of them.")
		if skippedNonEvent > 0 {
			lines = append(lines, fmt.Sprintf(
				"> %d automation(s) with cron/session/webhook triggers were not simulated — this tool only handles event triggers.",
				skippedNonEvent))
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

package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

// RegisterControlTools registers explicit side-effecting runner/control MCP tools.
func RegisterControlTools(s *Server, client *APIClient) {
	registerBrainRunnerPauseProject(s, client)
	registerBrainRunnerResumeProject(s, client)
	registerBrainRunnerPauseFeature(s, client)
	registerBrainRunnerResumeFeature(s, client)
	registerBrainRunnerPauseProjectAutomations(s, client)
	registerBrainRunnerResumeProjectAutomations(s, client)
	registerBrainRunnerPauseAll(s, client)
	registerBrainRunnerResumeAll(s, client)
	registerBrainControlSendPrompt(s, client)
	registerBrainControlAbortSession(s, client)
	registerBrainControlPermission(s, client)
	registerBrainControlSpawnInstance(s, client)
	registerBrainControlKillInstance(s, client)
}

func controlDescription(action string) string {
	return action + " Side effect: this mutates runner/control state or controls a remote session. Requires the appropriate REST auth scope and explicit identifiers."
}

// axisNote prefixes the task-axis control tools. The runner has two INDEPENDENT
// per-project pause dials, and shouldSkipTask (internal/service/scheduler.go)
// routes by task provenance: automation-generated tasks respect ONLY the
// automations dial, everything else respects ONLY the tasks dial.
//
// These tools move the tasks dial alone, but reported "Paused runner execution
// for project X" — which reads as "nothing runs for X now". On a project whose
// work is largely automation-generated that is close to the opposite of what
// happened. Until now the automations dial had no MCP tool at all, so an agent
// could not have finished the job even after noticing.
const axisNote = "Moves the TASKS pause dial only; automation-generated tasks follow a separate dial (see runner_pause_project_automations). "

func automationsStillRunNote(projectID string) string {
	return "Automation-generated tasks are NOT paused by this call - they follow a separate dial. " +
		"To stop those too: runner_pause_project_automations(project: \"" + projectID + "\"). " +
		"Check the resulting state with runner_status(project: \"" + projectID + "\")."
}

// The FEATURE dial. The one the other three cannot express: hold a single
// feature while the rest of the project keeps running.
//
// It is the answer for a MANUALLY STARTED feature specifically, because
// "run feature now" force-dispatches past the project dial by design — so
// pausing the project was never a way to stop work someone had already
// kicked off by hand.
func registerBrainRunnerPauseFeature(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name: "runner_pause_feature",
		Description: controlDescription("Pause task dispatch for ONE feature, leaving the rest of the project running. " +
			"Holds NEW dispatch only: a task a runner is already executing runs to completion, and an explicit run_task/run_feature still overrides. " +
			"Unlike the two project dials, this applies to automation-generated tasks in the feature as well - it is scoped to the WORK, not to who authored it."),
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project":    {Type: "string", Description: "Project ID. Defaults to the project detected from the MCP server's launch directory."},
			"feature_id": {Type: "string", Description: "Feature to hold."},
		}, Required: []string{"feature_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProjectArg(args)
		if projectID == "" {
			return "", fmt.Errorf("project is required")
		}
		featureID := StringArg(args, "feature_id", "")
		if featureID == "" {
			return "", fmt.Errorf("feature_id is required")
		}
		var resp controlSuccessResponse
		path := "/tasks/runner/features/pause/" + url.PathEscape(projectID) + "/" + url.PathEscape(featureID)
		if err := client.Request(ctx, http.MethodPost, path, map[string]any{}, nil, &resp); err != nil {
			return "", err
		}
		return fmt.Sprintf("Paused dispatch for feature %s in project %s. Success: %t\n\nWork already running finishes; nothing new starts. The rest of %s is unaffected. Resume with runner_resume_feature.", featureID, projectID, resp.Success, projectID), nil
	})
}

func registerBrainRunnerResumeFeature(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "runner_resume_feature",
		Description: controlDescription("Resume task dispatch for ONE feature held by runner_pause_feature."),
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project":    {Type: "string", Description: "Project ID. Defaults to the project detected from the MCP server's launch directory."},
			"feature_id": {Type: "string", Description: "Feature to release."},
		}, Required: []string{"feature_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProjectArg(args)
		if projectID == "" {
			return "", fmt.Errorf("project is required")
		}
		featureID := StringArg(args, "feature_id", "")
		if featureID == "" {
			return "", fmt.Errorf("feature_id is required")
		}
		var resp controlSuccessResponse
		path := "/tasks/runner/features/resume/" + url.PathEscape(projectID) + "/" + url.PathEscape(featureID)
		if err := client.Request(ctx, http.MethodPost, path, map[string]any{}, nil, &resp); err != nil {
			return "", err
		}
		return fmt.Sprintf("Resumed dispatch for feature %s in project %s. Success: %t", featureID, projectID, resp.Success), nil
	})
}

func registerBrainRunnerPauseProjectAutomations(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "runner_pause_project_automations",
		Description: controlDescription("Pause AUTOMATION-GENERATED task execution for one project. This is the dial that governs tasks created by automations; manual tasks follow runner_pause_project."),
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project": {Type: "string", Description: "Project ID. Defaults to the project detected from the MCP server's launch directory."},
		}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProjectArg(args)
		if projectID == "" {
			return "", fmt.Errorf("project is required")
		}
		var resp controlSuccessResponse
		if err := client.Request(ctx, http.MethodPost, "/tasks/runner/automations/pause/"+url.PathEscape(projectID), map[string]any{}, nil, &resp); err != nil {
			return "", err
		}
		return fmt.Sprintf("Paused AUTOMATION-GENERATED task execution for project %s. Success: %t\n\nManual tasks are NOT paused by this call - they follow a separate dial (runner_pause_project).", projectID, resp.Success), nil
	})
}

func registerBrainRunnerResumeProjectAutomations(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "runner_resume_project_automations",
		Description: controlDescription("Resume AUTOMATION-GENERATED task execution for one project."),
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project": {Type: "string", Description: "Project ID. Defaults to the project detected from the MCP server's launch directory."},
		}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProjectArg(args)
		if projectID == "" {
			return "", fmt.Errorf("project is required")
		}
		var resp controlSuccessResponse
		if err := client.Request(ctx, http.MethodPost, "/tasks/runner/automations/resume/"+url.PathEscape(projectID), map[string]any{}, nil, &resp); err != nil {
			return "", err
		}
		return fmt.Sprintf("Resumed AUTOMATION-GENERATED task execution for project %s. Success: %t\n\nManual tasks follow a separate dial; if they are still paused, use runner_resume_project.", projectID, resp.Success), nil
	})
}

func registerBrainRunnerPauseProject(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "runner_pause_project",
		Description: controlDescription(axisNote + "Pause MANUAL task execution for one project."),
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project": {Type: "string", Description: "Project ID. Defaults to the project detected from the MCP server's launch directory."},
		}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProjectArg(args)
		if projectID == "" {
			return "", fmt.Errorf("project is required")
		}
		var resp controlSuccessResponse
		if err := client.Request(ctx, http.MethodPost, "/tasks/runner/pause/"+url.PathEscape(projectID), map[string]any{}, nil, &resp); err != nil {
			return "", err
		}
		return fmt.Sprintf("Paused MANUAL task execution for project %s. Success: %t\n\n%s", projectID, resp.Success, automationsStillRunNote(projectID)), nil
	})
}

func registerBrainRunnerResumeProject(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "runner_resume_project",
		Description: controlDescription(axisNote + "Resume MANUAL task execution for one project."),
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"project": {Type: "string", Description: "Project ID. Defaults to the project detected from the MCP server's launch directory."},
		}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := ResolveProjectArg(args)
		if projectID == "" {
			return "", fmt.Errorf("project is required")
		}
		var resp controlSuccessResponse
		if err := client.Request(ctx, http.MethodPost, "/tasks/runner/resume/"+url.PathEscape(projectID), map[string]any{}, nil, &resp); err != nil {
			return "", err
		}
		return fmt.Sprintf("Resumed MANUAL task execution for project %s. Success: %t\n\nThe automation dial is separate and is unchanged by this call; use runner_resume_project_automations if automation-generated tasks are also paused.", projectID, resp.Success), nil
	})
}

func registerBrainRunnerPauseAll(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "runner_pause_all",
		Description: controlDescription(axisNote + "Pause MANUAL task execution for all projects; requires confirm=true."),
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"confirm": {Type: "boolean", Description: "Must be true to pause all runner task execution"},
		}, Required: []string{"confirm"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		if !BoolArg(args, "confirm", false) {
			return "", fmt.Errorf("confirm=true is required to pause runner execution for all projects")
		}
		var resp controlSuccessResponse
		if err := client.Request(ctx, http.MethodPost, "/tasks/runner/pause", map[string]any{}, nil, &resp); err != nil {
			return "", err
		}
		return fmt.Sprintf("Paused MANUAL task execution for all projects. Success: %t\n\nAutomation-generated tasks are NOT affected - they follow the separate automations dial.", resp.Success), nil
	})
}

func registerBrainRunnerResumeAll(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "runner_resume_all",
		Description: controlDescription(axisNote + "Resume MANUAL task execution for all projects; requires confirm=true."),
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"confirm": {Type: "boolean", Description: "Must be true to resume all runner task execution"},
		}, Required: []string{"confirm"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		if !BoolArg(args, "confirm", false) {
			return "", fmt.Errorf("confirm=true is required to resume runner execution for all projects")
		}
		var resp controlSuccessResponse
		if err := client.Request(ctx, http.MethodPost, "/tasks/runner/resume", map[string]any{}, nil, &resp); err != nil {
			return "", err
		}
		return fmt.Sprintf("Resumed MANUAL task execution for all projects. Success: %t\n\nThe automations dial is separate and is unchanged by this call.", resp.Success), nil
	})
}

func registerBrainControlSendPrompt(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "control_send_prompt",
		Description: controlDescription("Send a prompt to a remote runner session."),
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"runner_id":   {Type: "string", Description: "Runner ID"},
			"instance_id": {Type: "string", Description: "Remote instance ID"},
			"session_id":  {Type: "string", Description: "Session ID"},
			"text":        {Type: "string", Description: "Prompt text to send"},
			"agent":       {Type: "string", Description: "Optional agent override"},
			"provider_id": {Type: "string", Description: "Optional model provider ID; used with model_id"},
			"model_id":    {Type: "string", Description: "Optional model ID; used with provider_id"},
		}, Required: []string{"runner_id", "instance_id", "session_id", "text"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		ids, ok := requireControlSessionIDs(args)
		if !ok.valid {
			return "", fmt.Errorf("%s", ok.message)
		}
		text := strings.TrimSpace(StringArg(args, "text", ""))
		if text == "" {
			return "", fmt.Errorf("text is required")
		}
		body := controlPromptBody{Text: text}
		if agent := StringArg(args, "agent", ""); agent != "" {
			body.Agent = agent
		}
		providerID := StringArgAlias(args, "", "provider_id", "providerID")
		modelID := StringArgAlias(args, "", "model_id", "modelID")
		if providerID != "" && modelID != "" {
			body.Model = &controlPromptModel{ProviderID: providerID, ModelID: modelID}
		}
		var resp map[string]any
		path := controlSessionPath(ids.runnerID, ids.instanceID, ids.sessionID) + "/prompt"
		if err := client.Request(ctx, http.MethodPost, path, body, nil, &resp); err != nil {
			return "", err
		}
		return fmt.Sprintf("Sent prompt to session %s on runner %s instance %s.", ids.sessionID, ids.runnerID, ids.instanceID), nil
	})
}

func registerBrainControlAbortSession(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "control_abort_session",
		Description: controlDescription("Abort a remote runner session."),
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"runner_id":   {Type: "string", Description: "Runner ID"},
			"instance_id": {Type: "string", Description: "Remote instance ID"},
			"session_id":  {Type: "string", Description: "Session ID to abort"},
		}, Required: []string{"runner_id", "instance_id", "session_id"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		ids, ok := requireControlSessionIDs(args)
		if !ok.valid {
			return "", fmt.Errorf("%s", ok.message)
		}
		var resp controlSuccessResponse
		path := controlSessionPath(ids.runnerID, ids.instanceID, ids.sessionID) + "/abort"
		if err := client.Request(ctx, http.MethodPost, path, map[string]any{}, nil, &resp); err != nil {
			return "", err
		}
		return fmt.Sprintf("Abort requested for session %s on runner %s instance %s. Success: %t", ids.sessionID, ids.runnerID, ids.instanceID, resp.Success), nil
	})
}

func registerBrainControlPermission(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "control_permission",
		Description: controlDescription("Respond to a remote session permission prompt."),
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"runner_id":     {Type: "string", Description: "Runner ID"},
			"instance_id":   {Type: "string", Description: "Remote instance ID"},
			"session_id":    {Type: "string", Description: "Session ID"},
			"permission_id": {Type: "string", Description: "Permission request ID"},
			// OpenCode's own permission vocabulary, proxied untouched by
			// HandleControlPermission: {"response": "once"|"always"|"reject"}.
			//
			// This used to offer response {allow,deny} plus a separate
			// remember {once,always}. Neither response value is valid on the
			// wire, "once" and "always" ARE the response — misfiled into a
			// parameter the request shape does not have — and "reject" could
			// not be expressed at all. So the tool could neither grant a
			// permission nor deny one.
			"response": {Type: "string", Enum: []string{"once", "always", "reject"},
				Description: "Permission response: 'once' allows this request only, 'always' allows it and remembers, 'reject' denies it."},
		}, Required: []string{"runner_id", "instance_id", "session_id", "permission_id", "response"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		ids, ok := requireControlSessionIDs(args)
		if !ok.valid {
			return "", fmt.Errorf("%s", ok.message)
		}
		permissionID := StringArgAlias(args, "", "permission_id", "permissionId")
		if permissionID == "" {
			return "", fmt.Errorf("permission_id is required")
		}
		response := StringArg(args, "response", "")
		switch response {
		case "once", "always", "reject":
		default:
			return "", fmt.Errorf("response must be once, always, or reject")
		}
		body := controlPermissionBody{Response: response}
		var resp controlSuccessResponse
		path := controlSessionPath(ids.runnerID, ids.instanceID, ids.sessionID) + "/permissions/" + url.PathEscape(permissionID)
		if err := client.Request(ctx, http.MethodPost, path, body, nil, &resp); err != nil {
			return "", err
		}
		return fmt.Sprintf("Responded %s to permission %s for session %s on runner %s instance %s. Success: %t", response, permissionID, ids.sessionID, ids.runnerID, ids.instanceID, resp.Success), nil
	})
}

func registerBrainControlSpawnInstance(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "control_spawn_instance",
		Description: controlDescription("Spawn a new ad-hoc remote control instance on a runner."),
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"runner_id": {Type: "string", Description: "Runner ID"},
			"workdir":   {Type: "string", Description: "Absolute workdir on the runner machine"},
			"agent":     {Type: "string", Description: "Optional agent"},
			"model":     {Type: "string", Description: "Optional model"},
			"title":     {Type: "string", Description: "Optional instance title"},
		}, Required: []string{"runner_id", "workdir"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		runnerID := StringArgAlias(args, "", "runner_id", "runnerId")
		if runnerID == "" {
			return "", fmt.Errorf("runner_id is required")
		}
		workdir := StringArg(args, "workdir", "")
		if workdir == "" {
			return "", fmt.Errorf("workdir is required")
		}
		if !filepath.IsAbs(workdir) {
			return "", fmt.Errorf("workdir must be an absolute path")
		}
		body := controlSpawnBody{
			Agent:   StringArg(args, "agent", ""),
			Model:   StringArg(args, "model", ""),
			Title:   StringArg(args, "title", ""),
			Workdir: workdir,
		}
		var resp controlSpawnResponse
		path := "/control/runners/" + url.PathEscape(runnerID) + "/instances"
		if err := client.Request(ctx, http.MethodPost, path, body, nil, &resp); err != nil {
			return "", err
		}
		return fmt.Sprintf("Spawned control instance on runner %s. Instance: %s. Workdir: %s. Success: %t", runnerID, resp.Instance.InstanceID, workdir, resp.Success), nil
	})
}

func registerBrainControlKillInstance(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "control_kill_instance",
		Description: controlDescription("Kill an ad-hoc remote control instance; requires confirm=true."),
		InputSchema: InputSchema{Type: "object", Properties: map[string]Property{
			"runner_id":   {Type: "string", Description: "Runner ID"},
			"instance_id": {Type: "string", Description: "Instance ID to terminate"},
			"confirm":     {Type: "boolean", Description: "Must be true to kill the instance"},
		}, Required: []string{"runner_id", "instance_id", "confirm"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		runnerID := StringArgAlias(args, "", "runner_id", "runnerId")
		if runnerID == "" {
			return "", fmt.Errorf("runner_id is required")
		}
		instanceID := StringArgAlias(args, "", "instance_id", "instanceId")
		if instanceID == "" {
			return "", fmt.Errorf("instance_id is required")
		}
		if !BoolArg(args, "confirm", false) {
			return "", fmt.Errorf("confirm=true is required to kill a control instance")
		}
		var resp controlSuccessResponse
		path := "/control/runners/" + url.PathEscape(runnerID) + "/instances/" + url.PathEscape(instanceID)
		if err := client.Request(ctx, http.MethodDelete, path, nil, nil, &resp); err != nil {
			return "", err
		}
		return fmt.Sprintf("Killed control instance %s on runner %s. Success: %t", instanceID, runnerID, resp.Success), nil
	})
}

type controlSuccessResponse struct {
	Success bool `json:"success"`
}

type controlSpawnResponse struct {
	Success  bool                   `json:"success"`
	Instance types.OpencodeInstance `json:"instance"`
}

type controlSpawnBody struct {
	Agent   string `json:"agent,omitempty"`
	Model   string `json:"model,omitempty"`
	Title   string `json:"title,omitempty"`
	Workdir string `json:"workdir"`
}

type controlPromptBody struct {
	Agent string              `json:"agent,omitempty"`
	Model *controlPromptModel `json:"model,omitempty"`
	Text  string              `json:"text"`
}

type controlPromptModel struct {
	ModelID    string `json:"modelID"`
	ProviderID string `json:"providerID"`
}

// controlPermissionBody is proxied to the OpenCode instance untouched, so
// it must carry exactly OpenCode's vocabulary. The former Remember field
// was not part of that shape and was silently ignored downstream.
type controlPermissionBody struct {
	Response string `json:"response"`
}

type controlIDs struct {
	runnerID   string
	instanceID string
	sessionID  string
}

type validationResult struct {
	valid   bool
	message string
}

func requireControlSessionIDs(args map[string]any) (controlIDs, validationResult) {
	ids := controlIDs{
		runnerID:   StringArgAlias(args, "", "runner_id", "runnerId"),
		instanceID: StringArgAlias(args, "", "instance_id", "instanceId"),
		sessionID:  StringArgAlias(args, "", "session_id", "sessionId"),
	}
	if ids.runnerID == "" {
		return ids, validationResult{message: "runner_id is required"}
	}
	if ids.instanceID == "" {
		return ids, validationResult{message: "instance_id is required"}
	}
	if ids.sessionID == "" {
		return ids, validationResult{message: "session_id is required"}
	}
	return ids, validationResult{valid: true}
}

func controlSessionPath(runnerID, instanceID, sessionID string) string {
	return "/control/runners/" + url.PathEscape(runnerID) +
		"/instances/" + url.PathEscape(instanceID) +
		"/sessions/" + url.PathEscape(sessionID)
}

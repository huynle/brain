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

func registerBrainRunnerPauseProject(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "runner_pause_project",
		Description: controlDescription("Pause task execution for one project."),
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
		return fmt.Sprintf("Paused runner execution for project %s. Success: %t", projectID, resp.Success), nil
	})
}

func registerBrainRunnerResumeProject(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "runner_resume_project",
		Description: controlDescription("Resume task execution for one project."),
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
		return fmt.Sprintf("Resumed runner execution for project %s. Success: %t", projectID, resp.Success), nil
	})
}

func registerBrainRunnerPauseAll(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "runner_pause_all",
		Description: controlDescription("Pause task execution for all projects; requires confirm=true."),
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
		return fmt.Sprintf("Paused runner execution for all projects. Success: %t", resp.Success), nil
	})
}

func registerBrainRunnerResumeAll(s *Server, client *APIClient) {
	s.RegisterTool(Tool{
		Name:        "runner_resume_all",
		Description: controlDescription("Resume task execution for all projects; requires confirm=true."),
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
		return fmt.Sprintf("Resumed runner execution for all projects. Success: %t", resp.Success), nil
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

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
			"projectId": {Type: "string", Description: "Project ID whose runner task execution will be paused"},
		}, Required: []string{"projectId"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := StringArg(args, "projectId", "")
		if projectID == "" {
			return "projectId is required", nil
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
			"projectId": {Type: "string", Description: "Project ID whose runner task execution will be resumed"},
		}, Required: []string{"projectId"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		projectID := StringArg(args, "projectId", "")
		if projectID == "" {
			return "projectId is required", nil
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
			return "confirm=true is required to pause runner execution for all projects", nil
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
			return "confirm=true is required to resume runner execution for all projects", nil
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
			"runnerId":   {Type: "string", Description: "Runner ID"},
			"instanceId": {Type: "string", Description: "Remote instance ID"},
			"sessionId":  {Type: "string", Description: "Session ID"},
			"text":       {Type: "string", Description: "Prompt text to send"},
			"agent":      {Type: "string", Description: "Optional agent override"},
			"providerID": {Type: "string", Description: "Optional model provider ID; used with modelID"},
			"modelID":    {Type: "string", Description: "Optional model ID; used with providerID"},
		}, Required: []string{"runnerId", "instanceId", "sessionId", "text"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		ids, ok := requireControlSessionIDs(args)
		if !ok.valid {
			return ok.message, nil
		}
		text := strings.TrimSpace(StringArg(args, "text", ""))
		if text == "" {
			return "text is required", nil
		}
		body := controlPromptBody{Text: text}
		if agent := StringArg(args, "agent", ""); agent != "" {
			body.Agent = agent
		}
		providerID := StringArg(args, "providerID", "")
		modelID := StringArg(args, "modelID", "")
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
			"runnerId":   {Type: "string", Description: "Runner ID"},
			"instanceId": {Type: "string", Description: "Remote instance ID"},
			"sessionId":  {Type: "string", Description: "Session ID to abort"},
		}, Required: []string{"runnerId", "instanceId", "sessionId"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		ids, ok := requireControlSessionIDs(args)
		if !ok.valid {
			return ok.message, nil
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
			"runnerId":     {Type: "string", Description: "Runner ID"},
			"instanceId":   {Type: "string", Description: "Remote instance ID"},
			"sessionId":    {Type: "string", Description: "Session ID"},
			"permissionId": {Type: "string", Description: "Permission request ID"},
			"response":     {Type: "string", Enum: []string{"allow", "deny"}, Description: "Permission response"},
			"remember":     {Type: "string", Enum: []string{"once", "always"}, Description: "Optional memory duration"},
		}, Required: []string{"runnerId", "instanceId", "sessionId", "permissionId", "response"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		ids, ok := requireControlSessionIDs(args)
		if !ok.valid {
			return ok.message, nil
		}
		permissionID := StringArg(args, "permissionId", "")
		if permissionID == "" {
			return "permissionId is required", nil
		}
		response := StringArg(args, "response", "")
		if response != "allow" && response != "deny" {
			return "response must be allow or deny", nil
		}
		remember := StringArg(args, "remember", "")
		if remember != "" && remember != "once" && remember != "always" {
			return "remember must be once or always", nil
		}
		body := controlPermissionBody{Response: response, Remember: remember}
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
			"runnerId": {Type: "string", Description: "Runner ID"},
			"workdir":  {Type: "string", Description: "Absolute workdir on the runner machine"},
			"agent":    {Type: "string", Description: "Optional agent"},
			"model":    {Type: "string", Description: "Optional model"},
			"title":    {Type: "string", Description: "Optional instance title"},
		}, Required: []string{"runnerId", "workdir"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		runnerID := StringArg(args, "runnerId", "")
		if runnerID == "" {
			return "runnerId is required", nil
		}
		workdir := StringArg(args, "workdir", "")
		if workdir == "" {
			return "workdir is required", nil
		}
		if !filepath.IsAbs(workdir) {
			return "workdir must be an absolute path", nil
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
			"runnerId":   {Type: "string", Description: "Runner ID"},
			"instanceId": {Type: "string", Description: "Instance ID to terminate"},
			"confirm":    {Type: "boolean", Description: "Must be true to kill the instance"},
		}, Required: []string{"runnerId", "instanceId", "confirm"}},
	}, func(ctx context.Context, args map[string]any) (string, error) {
		runnerID := StringArg(args, "runnerId", "")
		if runnerID == "" {
			return "runnerId is required", nil
		}
		instanceID := StringArg(args, "instanceId", "")
		if instanceID == "" {
			return "instanceId is required", nil
		}
		if !BoolArg(args, "confirm", false) {
			return "confirm=true is required to kill a control instance", nil
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

type controlPermissionBody struct {
	Remember string `json:"remember,omitempty"`
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
		runnerID:   StringArg(args, "runnerId", ""),
		instanceID: StringArg(args, "instanceId", ""),
		sessionID:  StringArg(args, "sessionId", ""),
	}
	if ids.runnerID == "" {
		return ids, validationResult{message: "runnerId is required"}
	}
	if ids.instanceID == "" {
		return ids, validationResult{message: "instanceId is required"}
	}
	if ids.sessionID == "" {
		return ids, validationResult{message: "sessionId is required"}
	}
	return ids, validationResult{valid: true}
}

func controlSessionPath(runnerID, instanceID, sessionID string) string {
	return "/control/runners/" + url.PathEscape(runnerID) +
		"/instances/" + url.PathEscape(instanceID) +
		"/sessions/" + url.PathEscape(sessionID)
}

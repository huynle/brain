package mcp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/huynle/brain-api/internal/types"
)

func taskAssignmentProperties() map[string]Property {
	return map[string]Property{
		"project": {Type: "string", Description: "Override auto-detected project"},
		"task_id": {Type: "string", Description: "Standalone task ID"},
	}
}

func registerBrainRunnerCandidates(s *Server, client *APIClient) {
	props := taskAssignmentProperties()
	props["feature_id"] = Property{Type: "string", Description: "Optional feature whose unfinished tasks must also be supported"}
	props["executor"] = Property{Type: "string", Description: "Executor required by a proposed task"}
	props["requires_capability"] = Property{Type: "array", Items: &Property{Type: "string"}, Description: "Capabilities required by a proposed task"}
	props["git_remote"] = Property{Type: "string", Description: "Git remote required by a proposed task"}
	props["machine_affinity"] = Property{Type: "string", Description: "Machine affinity for a proposed task"}
	props["origin_machine_id"] = Property{Type: "string", Description: "Origin machine for a proposed task"}
	props["execution_mode"] = Property{Type: "string", Description: "Execution mode for a proposed task"}
	props["target_workdir"] = Property{Type: "string", Description: "Target workdir for a proposed task"}
	props["include_rejected"] = Property{Type: "boolean", Description: "Include incompatible runners and reasons"}
	s.RegisterTool(Tool{Name: "runner_candidates", Description: "List runners compatible with an existing standalone task or a proposed task specification.", InputSchema: InputSchema{Type: "object", Properties: props}}, func(ctx context.Context, args map[string]any) (string, error) {
		project, taskID := ResolveProject(args), StringArg(args, "task_id", "")
		var resp types.RunnerCandidatesResponse
		method := http.MethodGet
		path := "/tasks/" + url.PathEscape(project) + "/" + url.PathEscape(taskID) + "/runner-candidates"
		var body any
		if taskID == "" {
			method = http.MethodPost
			path = "/tasks/" + url.PathEscape(project) + "/runner-candidates"
			body = types.TaskRunnerCandidatesRequest{
				FeatureID: StringArg(args, "feature_id", ""), Executor: StringArg(args, "executor", ""),
				RequiresCapability: StringSliceArg(args, "requires_capability"), GitRemote: StringArg(args, "git_remote", ""),
				MachineAffinity: StringArg(args, "machine_affinity", ""), OriginMachineID: StringArg(args, "origin_machine_id", ""),
				ExecutionMode: StringArg(args, "execution_mode", ""), TargetWorkdir: StringArg(args, "target_workdir", ""),
			}
		}
		if err := client.Request(ctx, method, path, body, nil, &resp); err != nil {
			return "", err
		}
		return formatTaskRunnerCandidates(resp, BoolArg(args, "include_rejected", false)), nil
	})
}

func registerBrainTaskAssign(s *Server, client *APIClient) {
	props := taskAssignmentProperties()
	props["runner_id"] = Property{Type: "string", Description: "Compatible runner ID"}
	props["intent"] = Property{Type: "string", Enum: []string{"assign", "reassign"}, Description: "Use reassign to replace an existing pin"}
	props["force"] = Property{Type: "boolean", Description: "Allow a compatible runner that is temporarily offline"}
	s.RegisterTool(Tool{Name: "task_assign", Description: "Assign or reassign a standalone task to a compatible runner.", InputSchema: InputSchema{Type: "object", Properties: props, Required: []string{"task_id", "runner_id"}}}, func(ctx context.Context, args map[string]any) (string, error) {
		project, taskID, runnerID := ResolveProject(args), StringArg(args, "task_id", ""), StringArg(args, "runner_id", "")
		if taskID == "" || runnerID == "" {
			return "", fmt.Errorf("task_id and runner_id are required")
		}
		req := types.TaskAssignmentRequest{RunnerID: runnerID, Intent: StringArg(args, "intent", "assign"), Force: BoolArg(args, "force", false)}
		var resp types.TaskAssignmentResponse
		path := "/tasks/" + url.PathEscape(project) + "/" + url.PathEscape(taskID) + "/assignment"
		if err := client.Request(ctx, http.MethodPut, path, req, nil, &resp); err != nil {
			return "", err
		}
		return formatTaskAssignment(resp), nil
	})
}

func registerBrainTaskClearAssignment(s *Server, client *APIClient) {
	props := taskAssignmentProperties()
	s.RegisterTool(Tool{Name: "task_clear_assignment", Description: "Clear a standalone task runner assignment.", InputSchema: InputSchema{Type: "object", Properties: props, Required: []string{"task_id"}}}, func(ctx context.Context, args map[string]any) (string, error) {
		project, taskID := ResolveProject(args), StringArg(args, "task_id", "")
		if taskID == "" {
			return "", fmt.Errorf("task_id is required")
		}
		var resp types.TaskAssignmentResponse
		path := "/tasks/" + url.PathEscape(project) + "/" + url.PathEscape(taskID) + "/assignment/clear"
		if err := client.Request(ctx, http.MethodPost, path, types.ClearFeatureAssignmentRequest{Intent: "clear"}, nil, &resp); err != nil {
			return "", err
		}
		return formatTaskAssignment(resp), nil
	})
}

func formatTaskRunnerCandidates(resp types.RunnerCandidatesResponse, includeRejected bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## Runner candidates for %s/%s\n\n", resp.ProjectID, resp.TaskID)
	for _, candidate := range resp.Candidates {
		if !candidate.Compatible && !includeRejected {
			continue
		}
		fmt.Fprintf(&b, "- %s — compatible=%t available=%t", candidate.Runner.RunnerID, candidate.Compatible, candidate.Available)
		if len(candidate.Reasons) > 0 {
			fmt.Fprintf(&b, " — %s", candidate.Reasons[0].Message)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func formatTaskAssignment(resp types.TaskAssignmentResponse) string {
	return fmt.Sprintf("## Task assignment\n\n- Project: %s\n- Task: %s\n- Runner: %s\n- Scope: %s\n- Status: %s\n", resp.ProjectID, resp.TaskID, valueOrNone(resp.RunnerID), resp.Scope, resp.Status)
}

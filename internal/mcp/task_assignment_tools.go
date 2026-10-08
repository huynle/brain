package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/huynle/brain-api/internal/types"
	"github.com/huynle/brain-api/sdk/brain"
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
		project, taskID := ResolveProject(ctx, args), StringArg(args, "task_id", "")
		var resp types.RunnerCandidatesResponse
		var call func(context.Context, *brain.Client) error
		if taskID != "" {
			call = func(ctx context.Context, sc *brain.Client) error {
				_, err := sc.Tasks().RunnerCandidates(ctx, project, taskID)
				return err
			}
		} else {
			// Unset fields stay absent, as the omitempty legacy body had them.
			req := brain.TaskRunnerCandidatesRequest{
				FeatureId: optString(StringArg(args, "feature_id", "")), Executor: optString(StringArg(args, "executor", "")),
				RequiresCapability: optStrings(StringSliceArg(args, "requires_capability")), GitRemote: optString(StringArg(args, "git_remote", "")),
				MachineAffinity: optString(StringArg(args, "machine_affinity", "")), OriginMachineId: optString(StringArg(args, "origin_machine_id", "")),
				ExecutionMode: optString(StringArg(args, "execution_mode", "")), TargetWorkdir: optString(StringArg(args, "target_workdir", "")),
			}
			call = func(ctx context.Context, sc *brain.Client) error {
				_, err := sc.Tasks().ProposedRunnerCandidates(ctx, project, req)
				return err
			}
		}
		if err := sdkInto(ctx, client, &resp, call); err != nil {
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
		project, taskID, runnerID := ResolveProject(ctx, args), StringArg(args, "task_id", ""), StringArg(args, "runner_id", "")
		if taskID == "" || runnerID == "" {
			return "", fmt.Errorf("task_id and runner_id are required")
		}
		req := brain.TaskAssignmentRequest{RunnerId: runnerID, Intent: optString(StringArg(args, "intent", "assign")), Force: optTrue(BoolArg(args, "force", false))}
		var resp types.TaskAssignmentResponse
		if err := sdkInto(ctx, client, &resp, func(ctx context.Context, sc *brain.Client) error {
			_, err := sc.Tasks().Assign(ctx, project, taskID, req, brain.RequestOptions{})
			return err
		}); err != nil {
			return "", err
		}
		return formatTaskAssignment(resp), nil
	})
}

func registerBrainTaskClearAssignment(s *Server, client *APIClient) {
	props := taskAssignmentProperties()
	s.RegisterTool(Tool{Name: "task_clear_assignment", Description: "Clear a standalone task runner assignment.", InputSchema: InputSchema{Type: "object", Properties: props, Required: []string{"task_id"}}}, func(ctx context.Context, args map[string]any) (string, error) {
		project, taskID := ResolveProject(ctx, args), StringArg(args, "task_id", "")
		if taskID == "" {
			return "", fmt.Errorf("task_id is required")
		}
		var resp types.TaskAssignmentResponse
		if err := sdkInto(ctx, client, &resp, func(ctx context.Context, sc *brain.Client) error {
			_, err := sc.Tasks().ClearAssignment(ctx, project, taskID, brain.ClearFeatureAssignmentRequest{Intent: "clear"}, brain.RequestOptions{})
			return err
		}); err != nil {
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

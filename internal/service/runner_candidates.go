package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/gitremote"
	"github.com/huynle/brain-api/internal/types"
)

// GetProposedTaskRunnerCandidates evaluates a task specification before the
// task is created. When FeatureID is set, existing unfinished feature tasks are
// folded in so selecting a runner assigns a runner capable of the whole feature.
func (s *TaskServiceImpl) GetProposedTaskRunnerCandidates(ctx context.Context, projectID string, req types.TaskRunnerCandidatesRequest) (*types.RunnerCandidatesResponse, error) {
	tasks := []types.ResolvedTask{{
		ID: "proposed", FeatureID: req.FeatureID, Executor: req.Executor,
		RequiresCapability: req.RequiresCapability, GitRemote: req.GitRemote,
		MachineAffinity: req.MachineAffinity, OriginMachineID: req.OriginMachineID,
		ExecutionMode: req.ExecutionMode, TargetWorkdir: req.TargetWorkdir,
	}}
	s.applyTaskDefaults(tasks)
	if req.FeatureID != "" {
		result, err := s.GetTasks(ctx, projectID)
		if err != nil {
			return nil, err
		}
		for _, task := range result.Tasks {
			if task.FeatureID == req.FeatureID && !isTerminalCheckoutStatus(task.Status) {
				tasks = append(tasks, task)
			}
		}
	}
	resp, err := s.runnerCandidates(ctx, projectID, req.FeatureID, "", tasks)
	if err != nil {
		return nil, err
	}
	if req.FeatureID != "" {
		assignment, err := s.storage.ResolveRunnerAssignment(ctx, projectID, req.FeatureID, "")
		if err != nil {
			return nil, err
		}
		if assignment != nil {
			resp.AssignedRunnerID = assignment.RunnerID
			resp.AssignmentScope = "feature"
			ensureAssignedRunnerCandidate(resp, assignment.RunnerID)
		}
	}
	return resp, nil
}

// GetFeatureRunnerCandidates evaluates durable assignment compatibility for
// every registered runner against every unfinished task in a feature.
func (s *TaskServiceImpl) GetFeatureRunnerCandidates(ctx context.Context, projectID, featureID string) (*types.RunnerCandidatesResponse, error) {
	result, err := s.GetTasks(ctx, projectID)
	if err != nil {
		return nil, err
	}
	tasks := make([]types.ResolvedTask, 0)
	for _, task := range result.Tasks {
		if task.FeatureID == featureID {
			if !isTerminalCheckoutStatus(task.Status) {
				tasks = append(tasks, task)
			}
		}
	}
	resp, err := s.runnerCandidates(ctx, projectID, featureID, "", tasks)
	if err != nil {
		return nil, err
	}
	assignment, err := s.storage.ResolveRunnerAssignment(ctx, projectID, featureID, "")
	if err != nil {
		return nil, err
	}
	if assignment != nil {
		resp.AssignedRunnerID = assignment.RunnerID
		resp.AssignmentScope = "feature"
		ensureAssignedRunnerCandidate(resp, assignment.RunnerID)
	}
	return resp, nil
}

func ensureAssignedRunnerCandidate(resp *types.RunnerCandidatesResponse, runnerID string) {
	if resp == nil || runnerID == "" {
		return
	}
	for _, candidate := range resp.Candidates {
		if candidate.Runner.RunnerID == runnerID {
			return
		}
	}
	resp.Candidates = append([]types.RunnerCandidate{{
		Runner:     types.RunnerInfo{RunnerID: runnerID, Status: types.RunnerStatusOffline},
		Compatible: false, Available: false,
		Reasons: []types.RunnerCandidateReason{{Code: "runner_not_registered", Message: "assigned runner is no longer registered"}},
	}}, resp.Candidates...)
}

func (s *TaskServiceImpl) runnerCandidates(ctx context.Context, projectID, featureID, taskID string, tasks []types.ResolvedTask) (*types.RunnerCandidatesResponse, error) {
	snapshot, err := s.storage.LoadRunnerEligibility(ctx, projectID)
	if err != nil {
		return nil, fmt.Errorf("load runner eligibility: %w", err)
	}
	rows := snapshot.Runners
	placement := placementFromRow(snapshot.Placement)

	resp := &types.RunnerCandidatesResponse{ProjectID: projectID, FeatureID: featureID, TaskID: taskID, Candidates: make([]types.RunnerCandidate, 0, len(rows))}
	for i := range rows {
		runner := rowToRunnerInfo(&rows[i])
		runner.Status = computeRunnerStatus(rows[i].LastHeartbeat)
		reasons := durableRunnerCompatibilityReasons(projectID, tasks, *runner, placement)
		available := runner.Status == types.RunnerStatusOnline && !runner.Paused && !runner.Draining && (runner.MaxParallel <= 0 || runner.ActiveTasks < runner.MaxParallel)
		resp.Candidates = append(resp.Candidates, types.RunnerCandidate{
			Runner: *runner, Compatible: len(reasons) == 0, Available: available, Reasons: reasons,
		})
	}
	sort.Slice(resp.Candidates, func(i, j int) bool {
		left, right := resp.Candidates[i], resp.Candidates[j]
		if left.Compatible != right.Compatible {
			return left.Compatible
		}
		if left.Available != right.Available {
			return left.Available
		}
		return left.Runner.RunnerID < right.Runner.RunnerID
	})
	return resp, nil
}

func durableRunnerCompatibilityReasons(projectID string, tasks []types.ResolvedTask, runner types.RunnerInfo, placement *types.ProjectPlacement) []types.RunnerCandidateReason {
	var reasons []types.RunnerCandidateReason
	add := func(code, message, taskID string) {
		for i := range reasons {
			if reasons[i].Code == code && reasons[i].Message == message {
				if taskID != "" && !stringSliceContains(reasons[i].TaskIDs, taskID) {
					reasons[i].TaskIDs = append(reasons[i].TaskIDs, taskID)
				}
				return
			}
		}
		reason := types.RunnerCandidateReason{Code: code, Message: message}
		if taskID != "" {
			reason.TaskIDs = []string{taskID}
		}
		reasons = append(reasons, reason)
	}

	if !runnerAllowsProject(runner, projectID) {
		add("project_not_allowed", "project not allowed: runner does not accept "+projectID, "")
	}
	if missing := missingStrings(placement.RequiredCapabilities, runner.Capabilities); len(missing) > 0 {
		add("missing_project_capability", "missing project capabilities: "+strings.Join(missing, ","), "")
	}
	if missing := missingLabels(placement.RequiredLabels, runner.Labels); len(missing) > 0 {
		add("missing_required_label", "missing required labels: "+strings.Join(missing, ","), "")
	}
	if missing := missingResources(placement.Resources, runner.Resources, runner.Capacity); len(missing) > 0 {
		add("missing_project_resource", "missing project resources: "+strings.Join(missing, ","), "")
	}
	if placement.WorkspacePolicy == types.WorkspacePolicyWorktree && len(runner.WorkspaceRoots) == 0 {
		add("workspace_policy_mismatch", "runner has no workspace roots for worktree policy", "")
	}
	if placement.Affinity == types.PlacementAffinityStrict && !machineAllowedByStrictAffinity(runner.MachineID, placement) {
		add("machine_affinity_mismatch", "runner machine does not satisfy strict project affinity", "")
	}

	for _, task := range tasks {
		if task.Executor != "" && !stringSliceContains(runner.Executors, task.Executor) {
			add("unsupported_executor", "unsupported executor: "+task.Executor, task.ID)
		}
		if missing := missingStrings(task.RequiresCapability, runner.Capabilities); len(missing) > 0 {
			add("missing_task_capability", "missing task capabilities: "+strings.Join(missing, ","), task.ID)
		}
		if task.GitRemote != "" {
			if _, err := gitremote.Validate(task.GitRemote, gitremote.CredentialHosts(runner.Capabilities)); err != nil {
				add("unsupported_git_remote", err.Error(), task.ID)
			}
		}
		if reason, ok := machineAffinitySatisfied(task, runner.MachineID); !ok {
			add("machine_affinity_mismatch", reason, task.ID)
		}
	}
	return reasons
}

func (s *TaskServiceImpl) requireFeatureRunnerCompatibility(ctx context.Context, projectID, featureID, runnerID string) (*types.RunnerCandidate, error) {
	candidates, err := s.GetFeatureRunnerCandidates(ctx, projectID, featureID)
	if err != nil {
		return nil, err
	}
	for i := range candidates.Candidates {
		candidate := &candidates.Candidates[i]
		if candidate.Runner.RunnerID != runnerID {
			continue
		}
		if candidate.Compatible {
			return candidate, nil
		}
		return nil, fmt.Errorf("%w: runner %s is incompatible: %s", api.ErrConflict, runnerID, candidate.Reasons[0].Message)
	}
	return nil, api.ErrNotFound
}

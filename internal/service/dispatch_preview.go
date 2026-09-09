package service

import (
	"context"
	"fmt"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

// DispatchPreview uses the scheduler's actual ready set, pause and placement
// predicates. Runner-local filesystem/model defaults are explicitly unknown.
func (s *SchedulerService) DispatchPreview(ctx context.Context, project, taskID string, manual bool) (map[string]any, error) {
	lister, ok := s.tasks.(schedulerProjectTaskLister)
	if !ok {
		return nil, fmt.Errorf("task listing unavailable")
	}
	all, err := lister.GetTasks(ctx, project)
	if err != nil {
		return nil, err
	}
	var task *types.ResolvedTask
	for i := range all.Tasks {
		if all.Tasks[i].ID == taskID {
			task = &all.Tasks[i]
			break
		}
	}
	if task == nil {
		return nil, fmt.Errorf("task not found")
	}
	ready, err := s.tasks.GetReady(ctx, project, nil)
	if err != nil {
		return nil, err
	}
	isReady := false
	for _, t := range ready {
		if t.ID == taskID {
			isReady = true
		}
	}
	skip, reason := s.shouldSkipTask(project, *task)
	if manual {
		skip = false
		reason = "manual dispatch bypasses project and feature pause dials; runner pause still applies"
	}
	if s.runners == nil || s.placement == nil {
		return nil, fmt.Errorf("placement unavailable")
	}
	runners, err := s.candidateRunners(ctx)
	if err != nil {
		return nil, err
	}
	placement, err := s.placement.Get(ctx, project)
	if err != nil {
		return nil, err
	}
	if placement == nil {
		placement = &types.ProjectPlacement{ProjectID: project, Affinity: types.PlacementAffinitySoft}
	}
	candidates := []map[string]any{}
	for _, runner := range runners {
		reason, eligible := runnerEligibleForTask(*task, project, runner, placement)
		candidates = append(candidates, map[string]any{"runner_id": runner.RunnerID, "eligible": eligible, "reason": reason})
	}
	selected, _ := s.selectCandidate(*task, project, runners, placement, nil)
	selectedID := ""
	if selected != nil {
		selectedID = selected.RunnerID
	}
	return map[string]any{"snapshot_at": time.Now().UTC(), "project_id": project, "task_id": taskID, "ready": isReady, "paused": skip, "pause_explanation": reason, "dispatchable": isReady && !skip && selected != nil, "selected_runner": selectedID, "runners": candidates, "classification": task.Classification, "waiting_on": task.WaitingOn, "unresolved_dependencies": task.UnresolvedDeps, "configured": map[string]any{"executor": task.Executor, "model": task.Model, "workdir": task.Workdir, "target_workdir": task.TargetWorkdir, "repository": task.GitRemote, "branch": task.GitBranch, "target_branch": task.MergeTargetBranch}, "configuration_source": "task values after server task-default resolution; runner-local defaults are not observed", "unknown": []string{"runner-local model/executor defaults when omitted", "resolved filesystem permissions and checkout path", "effective runner timeout and total attempts"}, "reservation": false}, nil
}

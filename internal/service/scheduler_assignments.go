package service

import (
	"context"
	"fmt"

	"github.com/huynle/brain-api/internal/types"
)

// runnersForAssignment applies hard feature/standalone pins before every push
// placement decision. Pull claims enforce the same rule in TaskServiceImpl.
func (s *SchedulerService) runnersForAssignment(ctx context.Context, projectID string, task types.ResolvedTask, runners []types.RunnerInfo) ([]types.RunnerInfo, error) {
	assignments, ok := s.leases.(schedulerAssignmentStore)
	if !ok {
		return runners, nil
	}
	assignment, err := assignments.ResolveRunnerAssignment(ctx, projectID, task.FeatureID, task.ID)
	if err != nil {
		return nil, fmt.Errorf("get runner assignment for %s: %w", task.ID, err)
	}
	assignedRunnerID := ""
	if assignment != nil {
		assignedRunnerID = assignment.RunnerID
	}
	if assignedRunnerID == "" {
		return runners, nil
	}
	filtered := make([]types.RunnerInfo, 0, 1)
	for _, runner := range runners {
		if runner.RunnerID == assignedRunnerID {
			filtered = append(filtered, runner)
			break
		}
	}
	return filtered, nil
}

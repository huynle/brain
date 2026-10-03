package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/huynle/brain-api/internal/api"
	"github.com/huynle/brain-api/internal/storage"
	"github.com/huynle/brain-api/internal/types"
)

func (s *TaskServiceImpl) GetTaskRunnerCandidates(ctx context.Context, projectID, taskID string) (*types.RunnerCandidatesResponse, error) {
	task, err := s.GetTask(ctx, projectID, taskID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, api.ErrNotFound
	}
	resp, err := s.runnerCandidates(ctx, projectID, "", taskID, []types.ResolvedTask{*task})
	if err != nil {
		return nil, err
	}
	assignment, err := s.storage.GetTaskAssignment(ctx, projectID, taskID)
	if err != nil {
		return nil, err
	}
	if assignment != nil {
		resp.AssignedRunnerID = assignment.RunnerID
		resp.AssignmentScope = "task"
		ensureAssignedRunnerCandidate(resp, assignment.RunnerID)
	}
	return resp, nil
}

func (s *TaskServiceImpl) AssignTaskToRunner(ctx context.Context, projectID, taskID string, req types.TaskAssignmentRequest) (*types.TaskAssignmentResponse, error) {
	runnerID := strings.TrimSpace(req.RunnerID)
	if runnerID == "" {
		return nil, fmt.Errorf("runner_id is required")
	}
	task, err := s.GetTask(ctx, projectID, taskID)
	if err != nil {
		return nil, err
	}
	if task == nil {
		return nil, api.ErrNotFound
	}
	if task.FeatureID != "" {
		return nil, fmt.Errorf("%w: feature tasks must be assigned through their feature", api.ErrConflict)
	}
	candidates, err := s.GetTaskRunnerCandidates(ctx, projectID, taskID)
	if err != nil {
		return nil, err
	}
	compatible := false
	available := false
	for _, candidate := range candidates.Candidates {
		if candidate.Runner.RunnerID == runnerID {
			compatible = candidate.Compatible
			available = candidate.Available
			if !compatible {
				return nil, fmt.Errorf("%w: runner %s is incompatible: %s", api.ErrConflict, runnerID, candidate.Reasons[0].Message)
			}
			break
		}
	}
	if !compatible {
		return nil, api.ErrNotFound
	}
	if !req.Force && !available {
		return nil, api.ErrConflict
	}
	assigned, existing, err := s.storage.AssignTaskIfEmpty(ctx, projectID, taskID, runnerID, "manual", "active")
	if err != nil {
		return nil, err
	}
	if assigned {
		existing, err = s.storage.GetTaskAssignment(ctx, projectID, taskID)
		if err != nil {
			return nil, err
		}
	}
	if existing == nil {
		return nil, api.ErrNotFound
	}
	if existing.RunnerID == runnerID {
		return taskAssignmentRowToResponse(existing, ""), nil
	}
	if strings.TrimSpace(req.Intent) != "reassign" {
		return nil, api.ErrConflict
	}
	previous := existing.RunnerID
	reassigned, err := s.storage.ForceAssignTask(ctx, projectID, taskID, runnerID, "manual", "active")
	if err != nil {
		return nil, err
	}
	return taskAssignmentRowToResponse(reassigned, previous), nil
}

func taskAssignmentRowToResponse(row *storage.TaskAssignmentRow, previous string) *types.TaskAssignmentResponse {
	return &types.TaskAssignmentResponse{
		ProjectID: row.ProjectID, TaskID: row.TaskID, RunnerID: row.RunnerID,
		PreviousRunner: previous, Scope: "task", Source: row.Source, Status: row.Status,
		AssignedAt: time.UnixMilli(row.AssignedAt).UTC().Format(time.RFC3339),
		UpdatedAt:  time.UnixMilli(row.UpdatedAt).UTC().Format(time.RFC3339),
	}
}

func (s *TaskServiceImpl) ClearTaskAssignment(ctx context.Context, projectID, taskID string, req types.ClearFeatureAssignmentRequest) (*types.TaskAssignmentResponse, error) {
	if strings.TrimSpace(req.Intent) != "clear" {
		return nil, api.ErrConflict
	}
	existing, err := s.storage.GetTaskAssignment(ctx, projectID, taskID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, api.ErrNotFound
	}
	cleared, err := s.storage.ClearTaskAssignment(ctx, projectID, taskID)
	if err != nil {
		return nil, err
	}
	if !cleared {
		return nil, api.ErrNotFound
	}
	resp := taskAssignmentRowToResponse(existing, existing.RunnerID)
	resp.RunnerID = ""
	resp.Status = "cleared"
	resp.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	return resp, nil
}

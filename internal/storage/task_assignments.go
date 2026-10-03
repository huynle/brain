package storage

import (
	"context"
	"strings"
)

// RunnerEligibilitySnapshot contains the tenant-scoped registry and placement
// inputs required to evaluate contextual assignment candidates.
type RunnerEligibilitySnapshot struct {
	Runners   []RunnerRow
	Placement *ProjectPlacementRow
}

func (s *TenantStore) LoadRunnerEligibility(ctx context.Context, projectID string) (*RunnerEligibilitySnapshot, error) {
	runners, err := s.ListRunners(ctx)
	if err != nil {
		return nil, err
	}
	placement, err := s.GetProjectPlacement(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return &RunnerEligibilitySnapshot{Runners: runners, Placement: placement}, nil
}

// Standalone task pins share the tenant-safe assignment relation under a
// reserved key namespace. The typed API keeps that representation private and
// avoids changing the provenance-pinned v30 schema.
const taskAssignmentFeaturePrefix = "brain:task-assignment:"

// TaskAssignmentRow is a hard runner pin for a standalone task.
type TaskAssignmentRow struct {
	ProjectID  string
	TaskID     string
	RunnerID   string
	Source     string
	Status     string
	AssignedAt int64
	UpdatedAt  int64
}

func taskAssignmentKey(taskID string) string { return taskAssignmentFeaturePrefix + taskID }

func taskAssignmentFromFeature(row *FeatureAssignmentRow) *TaskAssignmentRow {
	if row == nil || !strings.HasPrefix(row.FeatureID, taskAssignmentFeaturePrefix) {
		return nil
	}
	return &TaskAssignmentRow{
		ProjectID: row.ProjectID, TaskID: strings.TrimPrefix(row.FeatureID, taskAssignmentFeaturePrefix),
		RunnerID: row.RunnerID, Source: row.Source, Status: row.Status,
		AssignedAt: row.AssignedAt, UpdatedAt: row.UpdatedAt,
	}
}

// RunnerAssignmentRow is the effective hard pin for a resolved task.
type RunnerAssignmentRow struct {
	RunnerID string
	Scope    string
}

// ResolveRunnerAssignment applies feature precedence without exposing the
// task-assignment representation to service callers.
func (s *TenantStore) ResolveRunnerAssignment(ctx context.Context, projectID, featureID, taskID string) (*RunnerAssignmentRow, error) {
	if featureID != "" {
		row, err := s.GetFeatureAssignment(ctx, projectID, featureID)
		if err != nil || row == nil {
			return nil, err
		}
		return &RunnerAssignmentRow{RunnerID: row.RunnerID, Scope: "feature"}, nil
	}
	row, err := s.GetTaskAssignment(ctx, projectID, taskID)
	if err != nil || row == nil {
		return nil, err
	}
	return &RunnerAssignmentRow{RunnerID: row.RunnerID, Scope: "task"}, nil
}

func (s *TenantStore) AssignTaskIfEmpty(ctx context.Context, projectID, taskID, runnerID, source, status string) (bool, *TaskAssignmentRow, error) {
	assigned, existing, err := s.AssignFeatureIfEmpty(ctx, projectID, taskAssignmentKey(taskID), runnerID, source, status)
	return assigned, taskAssignmentFromFeature(existing), err
}

func (s *TenantStore) ForceAssignTask(ctx context.Context, projectID, taskID, runnerID, source, status string) (*TaskAssignmentRow, error) {
	row, err := s.ForceAssignFeature(ctx, projectID, taskAssignmentKey(taskID), runnerID, source, status)
	return taskAssignmentFromFeature(row), err
}

func (s *TenantStore) GetTaskAssignment(ctx context.Context, projectID, taskID string) (*TaskAssignmentRow, error) {
	row, err := s.GetFeatureAssignment(ctx, projectID, taskAssignmentKey(taskID))
	return taskAssignmentFromFeature(row), err
}

func (s *TenantStore) ClearTaskAssignment(ctx context.Context, projectID, taskID string) (bool, error) {
	return s.ClearFeatureAssignment(ctx, projectID, taskAssignmentKey(taskID))
}

package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// FeatureAssignmentRow represents a row in the feature_assignments table.
type FeatureAssignmentRow struct {
	ProjectID  string
	FeatureID  string
	RunnerID   string
	Source     string
	Status     string
	AssignedAt int64 // Unix milliseconds
	UpdatedAt  int64 // Unix milliseconds
}

func (s *StorageLayer) AssignFeatureIfEmpty(ctx context.Context, projectID, featureID, runnerID, source, status string) (bool, *FeatureAssignmentRow, error) {
	now := time.Now().UnixMilli()
	result, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO feature_assignments
			(project_id, feature_id, runner_id, source, status, assigned_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		projectID, featureID, runnerID, source, status, now, now,
	)
	if err != nil {
		return false, nil, fmt.Errorf("assign feature if empty: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, nil, fmt.Errorf("assign feature rows affected: %w", err)
	}
	if rows > 0 {
		return true, nil, nil
	}

	existing, err := s.GetFeatureAssignment(ctx, projectID, featureID)
	if err != nil {
		return false, nil, fmt.Errorf("get existing feature assignment: %w", err)
	}
	return false, existing, nil
}

func (s *StorageLayer) ForceAssignFeature(ctx context.Context, projectID, featureID, runnerID, source, status string) (*FeatureAssignmentRow, error) {
	now := time.Now().UnixMilli()
	if existing, err := s.GetFeatureAssignment(ctx, projectID, featureID); err != nil {
		return nil, err
	} else if existing != nil && now <= existing.AssignedAt {
		now = existing.AssignedAt + 1
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO feature_assignments
			(project_id, feature_id, runner_id, source, status, assigned_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (project_id, feature_id) DO UPDATE SET
			runner_id = excluded.runner_id,
			source = excluded.source,
			status = excluded.status,
			assigned_at = excluded.assigned_at,
			updated_at = excluded.updated_at`,
		projectID, featureID, runnerID, source, status, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("force assign feature: %w", err)
	}
	return s.GetFeatureAssignment(ctx, projectID, featureID)
}

func (s *StorageLayer) GetFeatureAssignment(ctx context.Context, projectID, featureID string) (*FeatureAssignmentRow, error) {
	var a FeatureAssignmentRow
	err := s.db.QueryRowContext(ctx, `
		SELECT project_id, feature_id, runner_id, source, status, assigned_at, updated_at
		FROM feature_assignments
		WHERE project_id = ? AND feature_id = ?`,
		projectID, featureID,
	).Scan(&a.ProjectID, &a.FeatureID, &a.RunnerID, &a.Source, &a.Status, &a.AssignedAt, &a.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get feature assignment: %w", err)
	}
	return &a, nil
}

func (s *StorageLayer) ClearFeatureAssignment(ctx context.Context, projectID, featureID string) (bool, error) {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM feature_assignments WHERE project_id = ? AND feature_id = ?",
		projectID, featureID,
	)
	if err != nil {
		return false, fmt.Errorf("clear feature assignment: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("clear feature assignment rows affected: %w", err)
	}
	return rows > 0, nil
}

func (s *StorageLayer) ClearFeatureAssignmentsByRunner(ctx context.Context, runnerID string) (int64, error) {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM feature_assignments WHERE runner_id = ?",
		runnerID,
	)
	if err != nil {
		return 0, fmt.Errorf("clear feature assignments by runner: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("clear feature assignments by runner rows affected: %w", err)
	}
	return rows, nil
}

func (s *StorageLayer) ListFeatureAssignmentsByRunner(ctx context.Context, runnerID string) ([]FeatureAssignmentRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT project_id, feature_id, runner_id, source, status, assigned_at, updated_at
		FROM feature_assignments
		WHERE runner_id = ?
		ORDER BY project_id, feature_id`,
		runnerID,
	)
	if err != nil {
		return nil, fmt.Errorf("list feature assignments by runner: %w", err)
	}
	defer rows.Close()

	return scanFeatureAssignments(rows)
}

func (s *StorageLayer) ListFeatureAssignmentsByProject(ctx context.Context, projectID string) ([]FeatureAssignmentRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT project_id, feature_id, runner_id, source, status, assigned_at, updated_at
		FROM feature_assignments
		WHERE project_id = ?
		ORDER BY feature_id`,
		projectID,
	)
	if err != nil {
		return nil, fmt.Errorf("list feature assignments by project: %w", err)
	}
	defer rows.Close()

	return scanFeatureAssignments(rows)
}

func scanFeatureAssignments(rows *sql.Rows) ([]FeatureAssignmentRow, error) {
	var assignments []FeatureAssignmentRow
	for rows.Next() {
		var a FeatureAssignmentRow
		if err := rows.Scan(&a.ProjectID, &a.FeatureID, &a.RunnerID, &a.Source, &a.Status, &a.AssignedAt, &a.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan feature assignment: %w", err)
		}
		assignments = append(assignments, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("feature assignment rows error: %w", err)
	}
	return assignments, nil
}

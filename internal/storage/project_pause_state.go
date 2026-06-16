package storage

import (
	"context"
	"fmt"
	"time"
)

// ProjectPauseStateRow stores Brain-owned per-project pause switches.
type ProjectPauseStateRow struct {
	ProjectID         string
	TasksPaused       bool
	AutomationsPaused bool
	UpdatedAt         int64
}

func (s *StorageLayer) SetProjectTaskPaused(ctx context.Context, projectID string, paused bool) error {
	return s.setProjectPauseColumn(ctx, projectID, "tasks_paused", paused)
}

func (s *StorageLayer) SetProjectAutomationsPaused(ctx context.Context, projectID string, paused bool) error {
	return s.setProjectPauseColumn(ctx, projectID, "automations_paused", paused)
}

func (s *StorageLayer) setProjectPauseColumn(ctx context.Context, projectID, column string, paused bool) error {
	if projectID == "" {
		return fmt.Errorf("project id is required")
	}
	if column != "tasks_paused" && column != "automations_paused" {
		return fmt.Errorf("invalid pause column %q", column)
	}
	now := time.Now().UnixMilli()
	value := boolToInt(paused)
	_, err := s.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO project_pause_state (project_id, %s, updated_at)
		VALUES (?, ?, ?)
		ON CONFLICT(project_id) DO UPDATE SET
		  %s = excluded.%s,
		  updated_at = excluded.updated_at`, column, column, column), projectID, value, now)
	if err != nil {
		return fmt.Errorf("set project pause state: %w", err)
	}
	return nil
}

func (s *StorageLayer) SetAllProjectTasksPaused(ctx context.Context, paused bool) error {
	projects, err := s.listKnownProjectIDs(ctx)
	if err != nil {
		return err
	}
	for _, projectID := range projects {
		if err := s.SetProjectTaskPaused(ctx, projectID, paused); err != nil {
			return err
		}
	}
	return nil
}

func (s *StorageLayer) SetAllProjectAutomationsPaused(ctx context.Context, paused bool) error {
	projects, err := s.listKnownProjectIDs(ctx)
	if err != nil {
		return err
	}
	for _, projectID := range projects {
		if err := s.SetProjectAutomationsPaused(ctx, projectID, paused); err != nil {
			return err
		}
	}
	return nil
}

func (s *StorageLayer) IsProjectTaskPaused(ctx context.Context, projectID string) (bool, error) {
	return s.isProjectPauseColumn(ctx, projectID, "tasks_paused")
}

func (s *StorageLayer) IsProjectAutomationsPaused(ctx context.Context, projectID string) (bool, error) {
	return s.isProjectPauseColumn(ctx, projectID, "automations_paused")
}

func (s *StorageLayer) isProjectPauseColumn(ctx context.Context, projectID, column string) (bool, error) {
	var value int
	err := s.db.QueryRowContext(ctx, "SELECT "+column+" FROM project_pause_state WHERE project_id = ?", projectID).Scan(&value)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return false, nil
		}
		return false, fmt.Errorf("get project pause state: %w", err)
	}
	return value != 0, nil
}

func (s *StorageLayer) ListProjectPauseStates(ctx context.Context) ([]ProjectPauseStateRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT project_id, tasks_paused, automations_paused, updated_at
		FROM project_pause_state
		WHERE tasks_paused != 0 OR automations_paused != 0
		ORDER BY project_id`)
	if err != nil {
		return nil, fmt.Errorf("list project pause states: %w", err)
	}
	defer rows.Close()

	var result []ProjectPauseStateRow
	for rows.Next() {
		var row ProjectPauseStateRow
		var tasksPaused, automationsPaused int
		if err := rows.Scan(&row.ProjectID, &tasksPaused, &automationsPaused, &row.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan project pause state: %w", err)
		}
		row.TasksPaused = tasksPaused != 0
		row.AutomationsPaused = automationsPaused != 0
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("project pause state rows: %w", err)
	}
	return result, nil
}

func (s *StorageLayer) listKnownProjectIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT project_id FROM notes WHERE project_id IS NOT NULL AND project_id != ''
		UNION SELECT DISTINCT project_id FROM project_pause_state WHERE project_id != ''
		ORDER BY project_id`)
	if err != nil {
		return nil, fmt.Errorf("list known project ids: %w", err)
	}
	defer rows.Close()
	var projects []string
	for rows.Next() {
		var projectID string
		if err := rows.Scan(&projectID); err != nil {
			return nil, fmt.Errorf("scan project id: %w", err)
		}
		projects = append(projects, projectID)
	}
	return projects, rows.Err()
}

func boolToInt(v bool) int {
	if v {

		return 1
	}
	return 0
}

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

func (s *TenantStore) SetProjectTaskPaused(ctx context.Context, projectID string, paused bool) error {
	return s.setProjectPauseColumn(ctx, projectID, "tasks_paused", paused)
}

func (s *TenantStore) SetProjectAutomationsPaused(ctx context.Context, projectID string, paused bool) error {
	return s.setProjectPauseColumn(ctx, projectID, "automations_paused", paused)
}

func (s *TenantStore) setProjectPauseColumn(ctx context.Context, projectID, column string, paused bool) error {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return err
	}
	if projectID == "" {
		return fmt.Errorf("project id is required")
	}
	if column != "tasks_paused" && column != "automations_paused" {
		return fmt.Errorf("invalid pause column %q", column)
	}
	now := time.Now().UnixMilli()
	value := boolToInt(paused)
	columns, values, conflict := "project_id, "+column+", updated_at", "?, ?, ?", "project_id"
	args := []interface{}{projectID, value, now}
	if scope.owner != "" {
		columns += ", tenant_id"
		values += ", ?"
		conflict = "tenant_id, project_id"
		args = append(args, scope.owner)
	}
	_, err = s.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO project_pause_state (%s)
		VALUES (%s)
		ON CONFLICT(%s) DO UPDATE SET
		  %s = excluded.%s,
		  updated_at = excluded.updated_at`, columns, values, conflict, column, column), args...)
	if err != nil {
		return fmt.Errorf("set project pause state: %w", err)
	}
	return nil
}

func (s *TenantStore) SetAllProjectTasksPaused(ctx context.Context, paused bool) error {
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

func (s *TenantStore) SetAllProjectAutomationsPaused(ctx context.Context, paused bool) error {
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

func (s *TenantStore) IsProjectTaskPaused(ctx context.Context, projectID string) (bool, error) {
	return s.isProjectPauseColumn(ctx, projectID, "tasks_paused")
}

func (s *TenantStore) IsProjectAutomationsPaused(ctx context.Context, projectID string) (bool, error) {
	return s.isProjectPauseColumn(ctx, projectID, "automations_paused")
}

func (s *TenantStore) isProjectPauseColumn(ctx context.Context, projectID, column string) (bool, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return false, err
	}
	if column != "tasks_paused" && column != "automations_paused" {
		return false, fmt.Errorf("invalid pause column %q", column)
	}
	where, args := scope.where("project_id = ?", projectID)
	var value int
	err = s.db.QueryRowContext(ctx, "SELECT "+column+" FROM project_pause_state WHERE "+where, args...).Scan(&value)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return false, nil
		}
		return false, fmt.Errorf("get project pause state: %w", err)
	}
	return value != 0, nil
}

func (s *TenantStore) ListProjectPauseStates(ctx context.Context) ([]ProjectPauseStateRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	where, args := scope.where("(tasks_paused != 0 OR automations_paused != 0)")
	rows, err := s.db.QueryContext(ctx, `
		SELECT project_id, tasks_paused, automations_paused, updated_at
		FROM project_pause_state
		WHERE `+where+` ORDER BY project_id`, args...)
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

func (s *TenantStore) listKnownProjectIDs(ctx context.Context) ([]string, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	notesWhere, args := scope.where("project_id IS NOT NULL AND project_id != ''")
	pauseWhere, pauseArgs := scope.where("project_id != ''")
	args = append(args, pauseArgs...)
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT project_id FROM notes WHERE `+notesWhere+`
		UNION SELECT DISTINCT project_id FROM project_pause_state WHERE `+pauseWhere+`
		ORDER BY project_id`, args...)
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

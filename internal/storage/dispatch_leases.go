package storage

import (
	"context"
	"database/sql"
	"fmt"
)

const (
	DispatchLeaseStatePushed   = "pushed"
	DispatchLeaseStateAcked    = "acked"
	DispatchLeaseStateRejected = "rejected"
	DispatchLeaseStateExpired  = "expired"
)

type DispatchLeaseCreate struct {
	ProjectID         string
	TaskID            string
	AssignedRunnerID  string
	AssignedMachineID string
	PushedAt          int64
	ExpiresAt         int64
}

type DispatchLeaseRow struct {
	ProjectID         string
	TaskID            string
	AssignedRunnerID  string
	AssignedMachineID string
	State             string
	PushedAt          int64
	AckedAt           int64
	RejectedAt        int64
	LastError         string
	ExpiresAt         int64
}

func (s *StorageLayer) CreateDispatchLease(ctx context.Context, in DispatchLeaseCreate) (*DispatchLeaseRow, bool, error) {
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO task_dispatch_leases
		  (project_id, task_id, assigned_runner_id, assigned_machine_id, state, pushed_at, acked_at, rejected_at, last_error, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, 0, 0, '', ?)
		ON CONFLICT(project_id, task_id) DO UPDATE SET
		  assigned_runner_id = excluded.assigned_runner_id,
		  assigned_machine_id = excluded.assigned_machine_id,
		  state = excluded.state,
		  pushed_at = excluded.pushed_at,
		  acked_at = 0,
		  rejected_at = 0,
		  last_error = '',
		  expires_at = excluded.expires_at
		WHERE task_dispatch_leases.state IN (?, ?) OR task_dispatch_leases.expires_at < ?`,
		in.ProjectID, in.TaskID, in.AssignedRunnerID, in.AssignedMachineID, DispatchLeaseStatePushed, in.PushedAt, in.ExpiresAt,
		DispatchLeaseStateRejected, DispatchLeaseStateExpired, in.PushedAt,
	)
	if err != nil {
		return nil, false, fmt.Errorf("create dispatch lease: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, false, fmt.Errorf("create dispatch lease rows affected: %w", err)
	}
	lease, err := s.GetDispatchLease(ctx, in.ProjectID, in.TaskID)
	if err != nil {
		return nil, false, err
	}
	return lease, rows > 0, nil
}

func (s *StorageLayer) GetDispatchLease(ctx context.Context, projectID, taskID string) (*DispatchLeaseRow, error) {
	var row DispatchLeaseRow
	err := s.db.QueryRowContext(ctx, `
		SELECT project_id, task_id, assigned_runner_id, assigned_machine_id, state,
		       pushed_at, acked_at, rejected_at, last_error, expires_at
		FROM task_dispatch_leases WHERE project_id = ? AND task_id = ?`, projectID, taskID).Scan(
		&row.ProjectID, &row.TaskID, &row.AssignedRunnerID, &row.AssignedMachineID, &row.State,
		&row.PushedAt, &row.AckedAt, &row.RejectedAt, &row.LastError, &row.ExpiresAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get dispatch lease: %w", err)
	}
	return &row, nil
}

func (s *StorageLayer) AckDispatchLease(ctx context.Context, projectID, taskID, runnerID string, ackedAt int64) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE task_dispatch_leases
		SET state = ?, acked_at = ?, last_error = ''
		WHERE project_id = ? AND task_id = ? AND assigned_runner_id = ? AND state = ? AND expires_at >= ?`,
		DispatchLeaseStateAcked, ackedAt, projectID, taskID, runnerID, DispatchLeaseStatePushed, ackedAt,
	)
	if err != nil {
		return false, fmt.Errorf("ack dispatch lease: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("ack dispatch lease rows affected: %w", err)
	}
	return rows > 0, nil
}

func (s *StorageLayer) RejectDispatchLease(ctx context.Context, projectID, taskID, runnerID string, rejectedAt int64, lastError string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE task_dispatch_leases
		SET state = ?, rejected_at = ?, last_error = ?
		WHERE project_id = ? AND task_id = ? AND assigned_runner_id = ? AND state = ?`,
		DispatchLeaseStateRejected, rejectedAt, lastError, projectID, taskID, runnerID, DispatchLeaseStatePushed,
	)
	if err != nil {
		return false, fmt.Errorf("reject dispatch lease: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("reject dispatch lease rows affected: %w", err)
	}
	return rows > 0, nil
}

func (s *StorageLayer) ReleaseDispatchLease(ctx context.Context, projectID, taskID, runnerID string) (bool, error) {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM task_dispatch_leases WHERE project_id = ? AND task_id = ? AND assigned_runner_id = ?",
		projectID, taskID, runnerID,
	)
	if err != nil {
		return false, fmt.Errorf("release dispatch lease: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("release dispatch lease rows affected: %w", err)
	}
	return rows > 0, nil
}

func (s *StorageLayer) ExpireDispatchLeases(ctx context.Context, now int64) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE task_dispatch_leases
		SET state = ?, last_error = CASE WHEN last_error = '' THEN 'dispatch lease expired' ELSE last_error END
		WHERE expires_at < ? AND state IN (?, ?)`,
		DispatchLeaseStateExpired, now, DispatchLeaseStatePushed, DispatchLeaseStateAcked,
	)
	if err != nil {
		return 0, fmt.Errorf("expire dispatch leases: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("expire dispatch leases rows affected: %w", err)
	}
	return rows, nil
}

type PlacementReasonRow struct {
	ID             int64
	ProjectID      string
	TaskID         string
	RunnerID       string
	MachineID      string
	Decision       string
	Reason         string
	RequiredLabels string
	RunnerLabels   string
	MissingLabels  string
	CreatedAt      int64
}

func (s *StorageLayer) RecordPlacementReason(ctx context.Context, row *PlacementReasonRow) error {
	if row == nil {
		return fmt.Errorf("placement reason row is nil")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO task_placement_reasons
		  (project_id, task_id, runner_id, machine_id, decision, reason, required_labels, runner_labels, missing_labels, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ProjectID, row.TaskID, row.RunnerID, row.MachineID, row.Decision, row.Reason,
		row.RequiredLabels, row.RunnerLabels, row.MissingLabels, row.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("record placement reason: %w", err)
	}
	return nil
}

func (s *StorageLayer) ListPlacementReasons(ctx context.Context, projectID, taskID string) ([]PlacementReasonRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, task_id, runner_id, machine_id, decision, reason,
		       required_labels, runner_labels, missing_labels, created_at
		FROM task_placement_reasons
		WHERE project_id = ? AND task_id = ?
		ORDER BY created_at, id`, projectID, taskID)
	if err != nil {
		return nil, fmt.Errorf("list placement reasons: %w", err)
	}
	defer rows.Close()

	var reasons []PlacementReasonRow
	for rows.Next() {
		var row PlacementReasonRow
		if err := rows.Scan(&row.ID, &row.ProjectID, &row.TaskID, &row.RunnerID, &row.MachineID, &row.Decision, &row.Reason, &row.RequiredLabels, &row.RunnerLabels, &row.MissingLabels, &row.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan placement reason: %w", err)
		}
		reasons = append(reasons, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("placement reason rows: %w", err)
	}
	return reasons, nil
}

func (s *StorageLayer) ListExpiredDispatchLeases(ctx context.Context, projectID string, now int64, limit int) ([]DispatchLeaseRow, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT project_id, task_id, assigned_runner_id, assigned_machine_id, state,
		       pushed_at, acked_at, rejected_at, last_error, expires_at
		FROM task_dispatch_leases
		WHERE project_id = ? AND expires_at < ? AND state IN (?, ?)
		ORDER BY expires_at, pushed_at
		LIMIT ?`, projectID, now, DispatchLeaseStatePushed, DispatchLeaseStateAcked, limit)
	if err != nil {
		return nil, fmt.Errorf("list expired dispatch leases: %w", err)
	}
	defer rows.Close()

	var leases []DispatchLeaseRow
	for rows.Next() {
		var row DispatchLeaseRow
		if err := rows.Scan(&row.ProjectID, &row.TaskID, &row.AssignedRunnerID, &row.AssignedMachineID, &row.State, &row.PushedAt, &row.AckedAt, &row.RejectedAt, &row.LastError, &row.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan expired dispatch lease: %w", err)
		}
		leases = append(leases, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("expired dispatch lease rows: %w", err)
	}
	return leases, nil
}

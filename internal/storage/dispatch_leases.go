package storage

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"

	"github.com/huynle/brain-api/internal/types"
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
	LeaseID           string
	AssignedRunnerID  string
	AssignedMachineID string
	PushedAt          int64
	ExpiresAt         int64
}

type DispatchLeaseRow struct {
	ProjectID         string
	TaskID            string
	LeaseID           string
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
	if in.LeaseID == "" {
		leaseID, err := generateDispatchLeaseID()
		if err != nil {
			return nil, false, err
		}
		in.LeaseID = leaseID
	}
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO task_dispatch_leases
		  (project_id, task_id, lease_id, assigned_runner_id, assigned_machine_id, state, pushed_at, acked_at, rejected_at, last_error, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 0, 0, '', ?)
		ON CONFLICT(project_id, task_id) DO UPDATE SET
		  lease_id = excluded.lease_id,
		  assigned_runner_id = excluded.assigned_runner_id,
		  assigned_machine_id = excluded.assigned_machine_id,
		  state = excluded.state,
		  pushed_at = excluded.pushed_at,
		  acked_at = 0,
		  rejected_at = 0,
		  last_error = '',
		  expires_at = excluded.expires_at
		WHERE task_dispatch_leases.state IN (?, ?) OR task_dispatch_leases.expires_at < ?`,
		in.ProjectID, in.TaskID, in.LeaseID, in.AssignedRunnerID, in.AssignedMachineID, DispatchLeaseStatePushed, in.PushedAt, in.ExpiresAt,
		DispatchLeaseStateRejected, DispatchLeaseStateExpired, in.PushedAt,
	)
	if err != nil {
		return nil, false, fmt.Errorf("create dispatch lease: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return nil, false, fmt.Errorf("create dispatch lease rows affected: %w", err)
	}
	lease, err := s.GetDispatchLeaseRow(ctx, in.ProjectID, in.TaskID)
	if err != nil {
		return nil, false, err
	}
	return lease, rows > 0, nil
}

func (s *StorageLayer) GetDispatchLeaseRow(ctx context.Context, projectID, taskID string) (*DispatchLeaseRow, error) {
	var row DispatchLeaseRow
	err := s.db.QueryRowContext(ctx, `
		SELECT project_id, task_id, lease_id, assigned_runner_id, assigned_machine_id, state,
		       pushed_at, acked_at, rejected_at, last_error, expires_at
		FROM task_dispatch_leases WHERE project_id = ? AND task_id = ?`, projectID, taskID).Scan(
		&row.ProjectID, &row.TaskID, &row.LeaseID, &row.AssignedRunnerID, &row.AssignedMachineID, &row.State,
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

func generateDispatchLeaseID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate dispatch lease id: %w", err)
	}
	return "dl_" + hex.EncodeToString(b[:]), nil
}

func (s *StorageLayer) AckDispatchLease(ctx context.Context, projectID, taskID, runnerID, leaseID string, ackedAt int64) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE task_dispatch_leases
		SET state = ?, acked_at = ?, last_error = ''
		WHERE project_id = ? AND task_id = ? AND assigned_runner_id = ? AND lease_id = ? AND state = ? AND expires_at >= ?`,
		DispatchLeaseStateAcked, ackedAt, projectID, taskID, runnerID, leaseID, DispatchLeaseStatePushed, ackedAt,
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

func (s *StorageLayer) RejectDispatchLease(ctx context.Context, projectID, taskID, runnerID, leaseID string, rejectedAt int64, lastError string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE task_dispatch_leases
		SET state = ?, rejected_at = ?, last_error = ?
		WHERE project_id = ? AND task_id = ? AND assigned_runner_id = ? AND lease_id = ? AND state = ? AND expires_at >= ?`,
		DispatchLeaseStateRejected, rejectedAt, lastError, projectID, taskID, runnerID, leaseID, DispatchLeaseStatePushed, rejectedAt,
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
	// Opportunistic per-task prune: cap history at
	// PlacementReasonRetention rows so scheduler retry storms can't
	// grow the table unboundedly (production wedge: 894k rows, 75k+ per
	// task, GetReady endpoint at 5+ seconds). We prune every insert but
	// the delete is a no-op below the threshold, so steady-state cost
	// is one indexed count query per record. Prune failure is logged
	// but does not fail the insert — the row is more important than
	// the trim.
	if _, err := s.PrunePlacementReasonsForTask(ctx, row.ProjectID, row.TaskID, PlacementReasonRetention); err != nil {
		// Non-fatal: log via context, don't propagate.
		_ = err
	}
	return nil
}

// PlacementReasonRetention is the number of most-recent placement
// decisions kept per (project_id, task_id) after each
// RecordPlacementReason call. Older rows are pruned opportunistically.
// See PrunePlacementReasonsForTask.
const PlacementReasonRetention = 20

func (s *StorageLayer) ListPlacementReasonRows(ctx context.Context, projectID, taskID string) ([]PlacementReasonRow, error) {
	return s.ListPlacementReasonRowsLimit(ctx, projectID, taskID, 0)
}

// ListPlacementReasonRowsLimit returns placement decisions for a task,
// optionally capped at the most recent `limit` rows. When limit <= 0
// the full history is returned (matches legacy ListPlacementReasonRows
// behavior for diagnostic tools like brain_task_placement_reasons).
//
// The result is always sorted by (created_at, id) ascending — even in
// the limited case, so callers receive the newest-N in chronological
// order. Under the hood the SQL uses ORDER BY created_at DESC LIMIT N
// for the limited case then reverses in-memory, which keeps the query
// planner on the idx_task_placement_reasons_task index and avoids a
// full sort of the matching rows.
func (s *StorageLayer) ListPlacementReasonRowsLimit(ctx context.Context, projectID, taskID string, limit int) ([]PlacementReasonRow, error) {
	if limit <= 0 {
		rows, err := s.db.QueryContext(ctx, `
			SELECT id, project_id, task_id, runner_id, machine_id, decision, reason,
			       required_labels, runner_labels, missing_labels, created_at
			FROM task_placement_reasons
			WHERE project_id = ? AND task_id = ?
			ORDER BY created_at, id`, projectID, taskID)
		if err != nil {
			return nil, fmt.Errorf("list placement reasons: %w", err)
		}
		return scanPlacementReasonRows(rows)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, project_id, task_id, runner_id, machine_id, decision, reason,
		       required_labels, runner_labels, missing_labels, created_at
		FROM task_placement_reasons
		WHERE project_id = ? AND task_id = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ?`, projectID, taskID, limit)
	if err != nil {
		return nil, fmt.Errorf("list placement reasons (limited): %w", err)
	}
	reasons, err := scanPlacementReasonRows(rows)
	if err != nil {
		return nil, err
	}
	// Reverse to restore ascending order for callers that expect
	// chronological output (matches unlimited path's ORDER BY).
	for i, j := 0, len(reasons)-1; i < j; i, j = i+1, j-1 {
		reasons[i], reasons[j] = reasons[j], reasons[i]
	}
	return reasons, nil
}

// scanPlacementReasonRows materializes rows from a placement reason
// query. Extracted so ListPlacementReasonRows and its limited variant
// share the scan/close logic.
func scanPlacementReasonRows(rows *sql.Rows) ([]PlacementReasonRow, error) {
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

// PrunePlacementReasonsForTask deletes older placement decisions for a
// given task, keeping only the most recent `keep` rows. Returns the
// number of rows deleted. Called opportunistically after each new
// RecordPlacementReason so per-task history stays bounded even under
// scheduler retry storms (see task_placement_reasons production wedge:
// 894k rows accumulated, 75k+ per task).
//
// When keep <= 0 nothing is deleted. When the task has fewer than
// `keep` rows the query is a no-op (returns 0).
func (s *StorageLayer) PrunePlacementReasonsForTask(ctx context.Context, projectID, taskID string, keep int) (int64, error) {
	if keep <= 0 {
		return 0, nil
	}
	// Delete rows whose id is NOT among the newest `keep` for this
	// (project_id, task_id). Using id as the secondary key means the
	// deletion is stable under ties on created_at.
	result, err := s.db.ExecContext(ctx, `
		DELETE FROM task_placement_reasons
		WHERE project_id = ? AND task_id = ?
		  AND id NOT IN (
		    SELECT id FROM task_placement_reasons
		    WHERE project_id = ? AND task_id = ?
		    ORDER BY created_at DESC, id DESC
		    LIMIT ?
		  )`, projectID, taskID, projectID, taskID, keep)
	if err != nil {
		return 0, fmt.Errorf("prune placement reasons: %w", err)
	}
	deleted, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("prune placement reasons rows affected: %w", err)
	}
	return deleted, nil
}

func (s *StorageLayer) GetDispatchLease(ctx context.Context, projectID, taskID string) (*types.DispatchLease, error) {
	row, err := s.GetDispatchLeaseRow(ctx, projectID, taskID)
	if err != nil || row == nil {
		return nil, err
	}
	return dispatchLeaseFromRow(row), nil
}

func (s *StorageLayer) ListPlacementReasons(ctx context.Context, projectID, taskID string) ([]types.PlacementReason, error) {
	rows, err := s.ListPlacementReasonRows(ctx, projectID, taskID)
	if err != nil {
		return nil, err
	}
	reasons := make([]types.PlacementReason, 0, len(rows))
	for i := range rows {
		reasons = append(reasons, placementReasonFromRow(&rows[i]))
	}
	return reasons, nil
}

// ListPlacementReasonsLimit is the bounded variant of ListPlacementReasons.
// Hot paths like GetReady's enrichDispatchDiagnostics use this so a
// runaway task_placement_reasons table can't slow every task list
// response to 5+ seconds (production wedge: 894k rows total, 75k+ per
// task before pruning was introduced). Pass limit=0 for full history.
func (s *StorageLayer) ListPlacementReasonsLimit(ctx context.Context, projectID, taskID string, limit int) ([]types.PlacementReason, error) {
	rows, err := s.ListPlacementReasonRowsLimit(ctx, projectID, taskID, limit)
	if err != nil {
		return nil, err
	}
	reasons := make([]types.PlacementReason, 0, len(rows))
	for i := range rows {
		reasons = append(reasons, placementReasonFromRow(&rows[i]))
	}
	return reasons, nil
}

func dispatchLeaseFromRow(row *DispatchLeaseRow) *types.DispatchLease {
	return &types.DispatchLease{
		LeaseID:           row.LeaseID,
		ID:                row.LeaseID,
		ProjectID:         row.ProjectID,
		TaskID:            row.TaskID,
		AssignedRunnerID:  row.AssignedRunnerID,
		AssignedMachineID: row.AssignedMachineID,
		State:             row.State,
		PushedAt:          row.PushedAt,
		AckedAt:           row.AckedAt,
		RejectedAt:        row.RejectedAt,
		LastError:         row.LastError,
		ExpiresAt:         row.ExpiresAt,
	}
}

func placementReasonFromRow(row *PlacementReasonRow) types.PlacementReason {
	return types.PlacementReason{
		ID:             row.ID,
		ProjectID:      row.ProjectID,
		TaskID:         row.TaskID,
		RunnerID:       row.RunnerID,
		MachineID:      row.MachineID,
		Decision:       row.Decision,
		Reason:         row.Reason,
		RequiredLabels: row.RequiredLabels,
		RunnerLabels:   row.RunnerLabels,
		MissingLabels:  row.MissingLabels,
		CreatedAt:      row.CreatedAt,
	}
}

func (s *StorageLayer) ListExpiredDispatchLeases(ctx context.Context, projectID string, now int64, limit int) ([]DispatchLeaseRow, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT project_id, task_id, lease_id, assigned_runner_id, assigned_machine_id, state,
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
		if err := rows.Scan(&row.ProjectID, &row.TaskID, &row.LeaseID, &row.AssignedRunnerID, &row.AssignedMachineID, &row.State, &row.PushedAt, &row.AckedAt, &row.RejectedAt, &row.LastError, &row.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan expired dispatch lease: %w", err)
		}
		leases = append(leases, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("expired dispatch lease rows: %w", err)
	}
	return leases, nil
}

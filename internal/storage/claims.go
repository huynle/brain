package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// TaskClaimRow represents a row in the task_claims table.
type TaskClaimRow struct {
	ProjectID string
	TaskID    string
	RunnerID  string
	ClaimedAt int64 // Unix milliseconds
	ExpiresAt int64 // Unix milliseconds
}

// ClaimTask atomically claims a task for a runner. Uses INSERT ... ON CONFLICT
// to prevent race conditions — SQLite serializes writes so only one runner can
// win the claim.
//
// Returns:
//   - (true, nil, nil) if the claim was successful (new or re-claimed expired/own)
//   - (false, existingClaim, nil) if claimed by a different active runner
//   - (false, nil, err) on database error
func (s *TenantStore) ClaimTask(ctx context.Context, projectID, taskID, runnerID string, leaseDuration time.Duration) (bool, *TaskClaimRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return false, nil, err
	}
	now := time.Now().UnixMilli()
	expiresAt := now + leaseDuration.Milliseconds()
	columns, values, conflict := "", "", "project_id, task_id"
	args := []interface{}{}
	if scope.owner != "" {
		columns, values, conflict = "tenant_id,", "?,", "tenant_id, project_id, task_id"
		args = append(args, scope.owner)
	}
	args = append(args, projectID, taskID, runnerID, now, expiresAt, now)

	// Atomic upsert: insert the claim, or on conflict update ONLY if:
	// - the existing claim is by the same runner, OR
	// - the existing claim has expired
	// The v29 composite FK requires an existing same-tenant durable runner key;
	// claiming work must never manufacture reference ownership.
	result, err := s.db.ExecContext(ctx, `
		INSERT INTO task_claims (`+columns+`project_id, task_id, runner_id, claimed_at, expires_at)
		VALUES (`+values+`?, ?, ?, ?, ?)
		ON CONFLICT (`+conflict+`) DO UPDATE SET
			runner_id  = excluded.runner_id,
			claimed_at = excluded.claimed_at,
			expires_at = excluded.expires_at
		WHERE task_claims.runner_id = excluded.runner_id
		   OR task_claims.expires_at < ?`,
		args...,
	)
	if err != nil {
		return false, nil, fmt.Errorf("claim task: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, nil, fmt.Errorf("claim rows affected: %w", err)
	}

	if rows > 0 {
		// Claim succeeded (insert or update)
		return true, nil, nil
	}

	// Claim failed — another active runner holds it. Fetch the existing claim.
	existing, err := s.GetClaim(ctx, projectID, taskID)
	if err != nil {
		return false, nil, fmt.Errorf("get existing claim: %w", err)
	}
	return false, existing, nil
}

// ReleaseClaim deletes a claim only if the given runnerID matches the current
// holder. Returns true if the claim was released, false if not found or not
// owned by this runner.
func (s *TenantStore) ReleaseClaim(ctx context.Context, projectID, taskID, runnerID string) (bool, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return false, err
	}
	pred, args := scope.where("project_id = ? AND task_id = ? AND runner_id = ?", projectID, taskID, runnerID)
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM task_claims WHERE "+pred,
		args...,
	)
	if err != nil {
		return false, fmt.Errorf("release claim: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("release rows affected: %w", err)
	}
	return rows > 0, nil
}

// GetClaim returns the current claim for a task, or nil if unclaimed.
func (s *TenantStore) GetClaim(ctx context.Context, projectID, taskID string) (*TaskClaimRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	pred, args := scope.where("project_id = ? AND task_id = ?", projectID, taskID)
	var c TaskClaimRow
	err = s.db.QueryRowContext(ctx,
		"SELECT project_id, task_id, runner_id, claimed_at, expires_at FROM task_claims WHERE "+pred,
		args...,
	).Scan(&c.ProjectID, &c.TaskID, &c.RunnerID, &c.ClaimedAt, &c.ExpiresAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get claim: %w", err)
	}
	return &c, nil
}

// GetClaimsByRunner returns all claims currently held by the given runner.
func (s *TenantStore) GetClaimsByRunner(ctx context.Context, runnerID string) ([]TaskClaimRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	pred, args := scope.where("runner_id = ?", runnerID)
	rows, err := s.db.QueryContext(ctx,
		"SELECT project_id, task_id, runner_id, claimed_at, expires_at FROM task_claims WHERE "+pred+" ORDER BY claimed_at",
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("get claims by runner: %w", err)
	}
	defer rows.Close()

	var claims []TaskClaimRow
	for rows.Next() {
		var c TaskClaimRow
		if err := rows.Scan(&c.ProjectID, &c.TaskID, &c.RunnerID, &c.ClaimedAt, &c.ExpiresAt); err != nil {
			return nil, fmt.Errorf("scan claim: %w", err)
		}
		claims = append(claims, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("claims rows error: %w", err)
	}
	return claims, nil
}

// ExpireStaleClaims deletes all claims where expires_at < now.
// Returns the number of expired claims removed.
func (s *TenantStore) ExpireStaleClaims(ctx context.Context) (int64, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	pred, args := scope.where("expires_at < ?", now)
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM task_claims WHERE "+pred,
		args...,
	)
	if err != nil {
		return 0, fmt.Errorf("expire stale claims: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("expire rows affected: %w", err)
	}
	return count, nil
}

// ReleaseAllByRunner deletes all claims held by the given runner.
// Returns the number of claims released.
func (s *TenantStore) ReleaseAllByRunner(ctx context.Context, runnerID string) (int64, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return 0, err
	}
	pred, args := scope.where("runner_id = ?", runnerID)
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM task_claims WHERE "+pred,
		args...,
	)
	if err != nil {
		return 0, fmt.Errorf("release all by runner: %w", err)
	}

	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("release all rows affected: %w", err)
	}
	return count, nil
}

// RenewClaim extends the expiry of a claim. Returns an error if the claim
// does not exist or is not owned by the given runner.
func (s *TenantStore) RenewClaim(ctx context.Context, projectID, taskID, runnerID string, newExpiry time.Time) error {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return err
	}
	pred, args := scope.where("project_id = ? AND task_id = ? AND runner_id = ?", projectID, taskID, runnerID)
	args = append([]interface{}{newExpiry.UnixMilli()}, args...)
	result, err := s.db.ExecContext(ctx,
		"UPDATE task_claims SET expires_at = ? WHERE "+pred,
		args...,
	)
	if err != nil {
		return fmt.Errorf("renew claim: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("renew rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("claim not found or not owned by runner %q", runnerID)
	}
	return nil
}

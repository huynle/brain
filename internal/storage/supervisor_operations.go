package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/huynle/brain-api/internal/types"
)

const createSupervisorOperations = `CREATE TABLE IF NOT EXISTS supervisor_operations (
 tenant_id TEXT NOT NULL, actor TEXT NOT NULL, id TEXT NOT NULL, operation TEXT NOT NULL,
 digest TEXT NOT NULL, state TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, detail TEXT NOT NULL DEFAULT '',
 PRIMARY KEY(tenant_id,actor,id))`

func (s *TenantStore) BeginSupervisorOperation(ctx context.Context, actor, id, operation, digest string) (bool, error) {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return false, err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	result, err := s.db.ExecContext(ctx, `INSERT INTO supervisor_operations VALUES(?,?,?,?,?,'running',?,?, '') ON CONFLICT DO NOTHING`, scope, actor, id, operation, digest, now, now)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}
func (s *TenantStore) SupervisorOperation(ctx context.Context, actor, id string) (*types.SupervisorOperation, string, error) {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return nil, "", err
	}
	var out types.SupervisorOperation
	var digest, created, updated string
	err = s.db.QueryRowContext(ctx, `SELECT id,operation,digest,state,created_at,updated_at,detail FROM supervisor_operations WHERE tenant_id=? AND actor=? AND id=?`, scope, actor, id).Scan(&out.ID, &out.Operation, &digest, &out.State, &created, &updated, &out.Detail)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	out.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	out.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if out.State == "running" && time.Since(out.CreatedAt) > 30*time.Second {
		out.State = "outcome_unknown"
		out.Detail = "delivery was interrupted or exceeded its bound; reconcile session/task state before issuing a new operation"
	}
	return &out, digest, nil
}
func (s *TenantStore) FinishSupervisorOperation(ctx context.Context, actor, id, state, detail string) error {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE supervisor_operations SET state=?,detail=?,updated_at=? WHERE tenant_id=? AND actor=? AND id=? AND state='running'`, state, detail, time.Now().UTC().Format(time.RFC3339Nano), scope, actor, id)
	return err
}

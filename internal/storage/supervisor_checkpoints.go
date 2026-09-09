package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/huynle/brain-api/internal/types"
)

const createSupervisorCheckpoints = `CREATE TABLE IF NOT EXISTS supervisor_checkpoints (tenant_id TEXT NOT NULL, project TEXT NOT NULL, id TEXT NOT NULL, revision INTEGER NOT NULL, payload TEXT NOT NULL, PRIMARY KEY(tenant_id,project,id))`

func (s *TenantStore) SupervisorCheckpoints(ctx context.Context, project, after string) ([]types.SupervisorCheckpoint, error) {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM supervisor_checkpoints WHERE tenant_id=? AND project=? AND id>? ORDER BY id LIMIT 101`, scope, project, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []types.SupervisorCheckpoint{}
	for rows.Next() {
		var raw string
		var checkpoint types.SupervisorCheckpoint
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &checkpoint); err != nil {
			return nil, err
		}
		out = append(out, checkpoint)
	}
	return out, rows.Err()
}
func (s *TenantStore) SupervisorCheckpoint(ctx context.Context, project, id string) (*types.SupervisorCheckpoint, error) {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return nil, err
	}
	var raw string
	err = s.db.QueryRowContext(ctx, `SELECT payload FROM supervisor_checkpoints WHERE tenant_id=? AND project=? AND id=?`, scope, project, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out types.SupervisorCheckpoint
	err = json.Unmarshal([]byte(raw), &out)
	return &out, err
}
func (s *TenantStore) CompareSupervisorCheckpoint(ctx context.Context, expected int, value *types.SupervisorCheckpoint) (bool, error) {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return false, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback() //nolint:errcheck
	var result sql.Result
	if expected == 0 {
		result, err = tx.ExecContext(ctx, `INSERT INTO supervisor_checkpoints VALUES(?,?,?,?,?) ON CONFLICT DO NOTHING`, scope, value.Project, value.ID, value.Revision, string(raw))
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE supervisor_checkpoints SET revision=?,payload=? WHERE tenant_id=? AND project=? AND id=? AND revision=?`, value.Revision, string(raw), scope, value.Project, value.ID, expected)
	}
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO supervisor_checkpoint_versions VALUES(?,?,?,?,?)`, scope, value.Project, value.ID, value.Revision, string(raw)); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

const createSupervisorCheckpointVersions = `CREATE TABLE IF NOT EXISTS supervisor_checkpoint_versions (tenant_id TEXT NOT NULL,project TEXT NOT NULL,id TEXT NOT NULL,revision INTEGER NOT NULL,payload TEXT NOT NULL,PRIMARY KEY(tenant_id,project,id,revision))`

func (s *TenantStore) SupervisorCheckpointVersions(ctx context.Context, project, id string) ([]types.SupervisorCheckpoint, error) {
	scope, err := s.bulkScope(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT payload FROM supervisor_checkpoint_versions WHERE tenant_id=? AND project=? AND id=? ORDER BY revision DESC LIMIT 100`, scope, project, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []types.SupervisorCheckpoint{}
	for rows.Next() {
		var raw string
		var value types.SupervisorCheckpoint
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

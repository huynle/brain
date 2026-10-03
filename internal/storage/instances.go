package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// InstanceRow represents a row in the opencode_instances table.
type InstanceRow struct {
	InstanceID string   `json:"instance_id"`
	RunnerID   string   `json:"runner_id"`
	Hostname   string   `json:"hostname"`
	Kind       string   `json:"kind"`
	ProjectID  string   `json:"project_id"`
	TaskID     string   `json:"task_id"`
	FeatureID  string   `json:"feature_id"`
	Priority   string   `json:"priority"`
	Title      string   `json:"title"`
	Workdir    string   `json:"workdir"`
	Port       int      `json:"port"`
	PID        int      `json:"pid"`
	SessionIDs []string `json:"session_ids"`
	Status     string   `json:"status"`
	Executor   string   `json:"executor"`
	Agent      string   `json:"agent"`
	Model      string   `json:"model"`
	StartedAt  int64    `json:"started_at"` // Unix milliseconds
	LastSeen   int64    `json:"last_seen"`  // Unix milliseconds
}

const instanceColumns = `instance_id, runner_id, hostname, kind, project_id, task_id,
       feature_id, priority, title, workdir, port, pid, session_ids, status, executor, agent, model, started_at, last_seen`

// UpsertInstance inserts or replaces an OpenCode instance record.
func (s *TenantStore) UpsertInstance(ctx context.Context, inst *InstanceRow) error {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return err
	}
	sessionsJSON, err := marshalSessionIDs(inst.SessionIDs)
	if err != nil {
		return fmt.Errorf("marshal session ids: %w", err)
	}

	columns, values, conflict := "", "", "instance_id"
	args := []interface{}{}
	if scope.owner != "" {
		columns, values, conflict = "tenant_id,", "?,", "tenant_id,instance_id"
		args = append(args, scope.owner)
	}
	args = append(args, inst.InstanceID, inst.RunnerID, inst.Hostname, inst.Kind, inst.ProjectID, inst.TaskID, inst.FeatureID, inst.Priority,
		inst.Title, inst.Workdir, inst.Port, inst.PID, sessionsJSON, inst.Status, inst.Executor,
		inst.Agent, inst.Model, inst.StartedAt, inst.LastSeen)
	// The composite FK requires an existing same-tenant durable runner key.
	// Instance reports must never manufacture reference ownership.
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO opencode_instances
			(`+columns+`instance_id, runner_id, hostname, kind, project_id, task_id, feature_id, priority,
			 title, workdir, port, pid, session_ids, status, executor, agent, model, started_at, last_seen)
		VALUES (`+values+`?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (`+conflict+`) DO UPDATE SET
			runner_id   = excluded.runner_id,
			hostname    = excluded.hostname,
			kind        = excluded.kind,
			project_id  = excluded.project_id,
			task_id     = excluded.task_id,
			feature_id  = excluded.feature_id,
			priority    = excluded.priority,
			title       = excluded.title,
			workdir     = excluded.workdir,
			port        = excluded.port,
			pid         = excluded.pid,
			session_ids = excluded.session_ids,
			status      = excluded.status,
			executor    = excluded.executor,
			agent       = excluded.agent,
			model       = excluded.model,
			started_at  = excluded.started_at,
			last_seen   = excluded.last_seen`,
		args...,
	)
	if err != nil {
		return fmt.Errorf("upsert instance: %w", err)
	}
	return nil
}

// DeleteInstance removes an instance by ID, scoped to a runner.
// Returns true if a row was deleted.
func (s *TenantStore) DeleteInstance(ctx context.Context, runnerID, instanceID string) (bool, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return false, err
	}
	pred, args := scope.where("runner_id = ? AND instance_id = ?", runnerID, instanceID)
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM opencode_instances WHERE "+pred,
		args...,
	)
	if err != nil {
		return false, fmt.Errorf("delete instance: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete instance rows affected: %w", err)
	}
	return n > 0, nil
}

// DeleteInstancesByRunner removes all instances reported by a runner.
// Used by the lifecycle sweep when a runner goes offline or deregisters.
func (s *TenantStore) DeleteInstancesByRunner(ctx context.Context, runnerID string) (int64, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return 0, err
	}
	pred, args := scope.where("runner_id = ?", runnerID)
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM opencode_instances WHERE "+pred,
		args...,
	)
	if err != nil {
		return 0, fmt.Errorf("delete instances by runner: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete instances rows affected: %w", err)
	}
	return n, nil
}

// GetInstance returns an instance by ID, or nil if not found.
func (s *TenantStore) GetInstance(ctx context.Context, instanceID string) (*InstanceRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	pred, args := scope.where("instance_id = ?", instanceID)
	row := s.db.QueryRowContext(ctx,
		"SELECT "+instanceColumns+" FROM opencode_instances WHERE "+pred,
		args...,
	)
	inst, err := scanInstance(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get instance: %w", err)
	}
	return inst, nil
}

// ListInstancesByRunner returns all instances reported by a runner,
// newest first.
func (s *TenantStore) ListInstancesByRunner(ctx context.Context, runnerID string) ([]InstanceRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	pred, args := scope.where("runner_id = ?", runnerID)
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+instanceColumns+" FROM opencode_instances WHERE "+pred+" ORDER BY started_at DESC",
		args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list instances by runner: %w", err)
	}
	defer rows.Close()
	return scanInstances(rows)
}

// ListAllInstances returns every instance across all runners, newest first.
func (s *TenantStore) ListAllInstances(ctx context.Context) ([]InstanceRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	pred, args := scope.where("1=1")
	rows, err := s.db.QueryContext(ctx,
		"SELECT "+instanceColumns+" FROM opencode_instances WHERE "+pred+" ORDER BY started_at DESC", args...,
	)
	if err != nil {
		return nil, fmt.Errorf("list all instances: %w", err)
	}
	defer rows.Close()
	return scanInstances(rows)
}

// ReplaceInstancesForRunner atomically replaces all instances for a runner
// with the given set. Used by heartbeat reconciliation so server state
// self-heals from missed upserts/deletes.
func (s *TenantStore) ReplaceInstancesForRunner(ctx context.Context, runnerID string, instances []InstanceRow) error {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return err
	}
	tx, err := beginResilientTx(ctx, s.db, nil)
	if err != nil {
		return fmt.Errorf("begin replace instances: %w", err)
	}
	// Rolling back an already-committed tx returns sql.ErrTxDone; the
	// commit result above is what callers act on.
	defer func() { _ = tx.Rollback() }()
	if scope.owner != "" {
		// Validate even an empty replacement. Durable ownership is enough for
		// reconciliation; it is not evidence of active enrollment.
		var known int
		if err := tx.QueryRowContext(ctx, `SELECT 1 FROM tenant_runner_keys WHERE tenant_id=? AND runner_id=?`, scope.owner, runnerID).Scan(&known); err != nil {
			return fmt.Errorf("replace instances runner reference: %w", err)
		}
	}

	pred, args := scope.where("runner_id = ?", runnerID)
	if _, err := tx.ExecContext(ctx,
		"DELETE FROM opencode_instances WHERE "+pred, args...); err != nil {
		return fmt.Errorf("replace instances delete: %w", err)
	}

	for i := range instances {
		inst := &instances[i]
		sessionsJSON, err := marshalSessionIDs(inst.SessionIDs)
		if err != nil {
			return fmt.Errorf("marshal session ids: %w", err)
		}
		columns, values := "", ""
		args := []interface{}{}
		if scope.owner != "" {
			columns, values = "tenant_id,", "?,"
			args = append(args, scope.owner)
		}
		args = append(args, inst.InstanceID, runnerID, inst.Hostname, inst.Kind, inst.ProjectID, inst.TaskID, inst.FeatureID, inst.Priority,
			inst.Title, inst.Workdir, inst.Port, inst.PID, sessionsJSON, inst.Status, inst.Executor,
			inst.Agent, inst.Model, inst.StartedAt, inst.LastSeen)
		if _, err := tx.ExecContext(ctx, `
			INSERT OR REPLACE INTO opencode_instances
				(`+columns+`instance_id, runner_id, hostname, kind, project_id, task_id, feature_id, priority,
				 title, workdir, port, pid, session_ids, status, executor, agent, model, started_at, last_seen)
			VALUES (`+values+`?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			args...,
		); err != nil {
			return fmt.Errorf("replace instances insert: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace instances: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func marshalSessionIDs(ids []string) (string, error) {
	if ids == nil {
		ids = []string{}
	}
	b, err := json.Marshal(ids)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// rowScanner abstracts *sql.Row and *sql.Rows for shared scan logic.
type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanInstance(row rowScanner) (*InstanceRow, error) {
	var inst InstanceRow
	var sessionsJSON string
	if err := row.Scan(&inst.InstanceID, &inst.RunnerID, &inst.Hostname, &inst.Kind,
		&inst.ProjectID, &inst.TaskID, &inst.FeatureID, &inst.Priority, &inst.Title, &inst.Workdir, &inst.Port, &inst.PID,
		&sessionsJSON, &inst.Status, &inst.Executor, &inst.Agent, &inst.Model, &inst.StartedAt, &inst.LastSeen); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(sessionsJSON), &inst.SessionIDs); err != nil {
		return nil, fmt.Errorf("unmarshal session ids: %w", err)
	}
	return &inst, nil
}

func scanInstances(rows *sql.Rows) ([]InstanceRow, error) {
	var instances []InstanceRow
	for rows.Next() {
		inst, err := scanInstance(rows)
		if err != nil {
			return nil, fmt.Errorf("scan instance: %w", err)
		}
		instances = append(instances, *inst)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("instances rows error: %w", err)
	}
	return instances, nil
}

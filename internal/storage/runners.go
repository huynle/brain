package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// RunnerRow represents a row in the runners table.
type RunnerRow struct {
	RunnerID      string            `json:"runner_id"`
	Hostname      string            `json:"hostname"`
	Labels        map[string]string `json:"labels"`
	Executors     []string          `json:"executors"`
	Capabilities  []string          `json:"capabilities"`
	MaxParallel   int               `json:"max_parallel"`
	FeatureIDs    string            `json:"feature_ids"`
	RegisteredAt  int64             `json:"registered_at"`  // Unix milliseconds
	LastHeartbeat int64             `json:"last_heartbeat"` // Unix milliseconds
	Status        string            `json:"status"`
}

// UpsertRunner inserts a new runner or replaces an existing one with the same ID.
// This is the primary registration/re-registration method.
func (s *StorageLayer) UpsertRunner(ctx context.Context, runner *RunnerRow) error {
	labelsJSON, err := json.Marshal(runner.Labels)
	if err != nil {
		return fmt.Errorf("marshal labels: %w", err)
	}
	executorsJSON, err := json.Marshal(runner.Executors)
	if err != nil {
		return fmt.Errorf("marshal executors: %w", err)
	}
	capabilitiesJSON, err := json.Marshal(runner.Capabilities)
	if err != nil {
		return fmt.Errorf("marshal capabilities: %w", err)
	}

	_, err = s.db.ExecContext(ctx, `
		INSERT INTO runners
			(runner_id, hostname, labels, executors, capabilities, max_parallel,
			 feature_ids, registered_at, last_heartbeat, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (runner_id) DO UPDATE SET
			hostname       = excluded.hostname,
			labels         = excluded.labels,
			executors      = excluded.executors,
			capabilities  = excluded.capabilities,
			max_parallel   = excluded.max_parallel,
			feature_ids    = excluded.feature_ids,
			registered_at  = excluded.registered_at,
			last_heartbeat = excluded.last_heartbeat,
			status         = excluded.status`,
		runner.RunnerID, runner.Hostname, string(labelsJSON), string(executorsJSON), string(capabilitiesJSON),
		runner.MaxParallel, runner.FeatureIDs, runner.RegisteredAt, runner.LastHeartbeat,
		runner.Status,
	)
	if err != nil {
		return fmt.Errorf("upsert runner: %w", err)
	}
	return nil
}

// GetRunner returns a runner by ID, or nil if not found.
func (s *StorageLayer) GetRunner(ctx context.Context, runnerID string) (*RunnerRow, error) {
	var r RunnerRow
	var labelsJSON, executorsJSON, capabilitiesJSON string

	err := s.db.QueryRowContext(ctx,
		`SELECT runner_id, hostname, labels, executors, capabilities, max_parallel,
		        feature_ids, registered_at, last_heartbeat, status
		 FROM runners WHERE runner_id = ?`,
		runnerID,
	).Scan(&r.RunnerID, &r.Hostname, &labelsJSON, &executorsJSON, &capabilitiesJSON,
		&r.MaxParallel, &r.FeatureIDs, &r.RegisteredAt, &r.LastHeartbeat,
		&r.Status)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get runner: %w", err)
	}

	if err := json.Unmarshal([]byte(labelsJSON), &r.Labels); err != nil {
		return nil, fmt.Errorf("unmarshal labels: %w", err)
	}
	if err := json.Unmarshal([]byte(executorsJSON), &r.Executors); err != nil {
		return nil, fmt.Errorf("unmarshal executors: %w", err)
	}
	if err := json.Unmarshal([]byte(capabilitiesJSON), &r.Capabilities); err != nil {
		return nil, fmt.Errorf("unmarshal capabilities: %w", err)
	}

	return &r, nil
}

// ListRunners returns all runners ordered by registered_at descending (newest first).
func (s *StorageLayer) ListRunners(ctx context.Context) ([]RunnerRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT runner_id, hostname, labels, executors, capabilities, max_parallel,
		        feature_ids, registered_at, last_heartbeat, status
		 FROM runners ORDER BY registered_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list runners: %w", err)
	}
	defer rows.Close()

	return scanRunners(rows)
}

// ListRunnersByStatus returns runners filtered by status.
func (s *StorageLayer) ListRunnersByStatus(ctx context.Context, status string) ([]RunnerRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT runner_id, hostname, labels, executors, capabilities, max_parallel,
		        feature_ids, registered_at, last_heartbeat, status
		 FROM runners WHERE status = ? ORDER BY registered_at DESC`,
		status,
	)
	if err != nil {
		return nil, fmt.Errorf("list runners by status: %w", err)
	}
	defer rows.Close()

	return scanRunners(rows)
}

// DeleteRunner removes a runner by ID. Returns true if a row was deleted.
func (s *StorageLayer) DeleteRunner(ctx context.Context, runnerID string) (bool, error) {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM runners WHERE runner_id = ?",
		runnerID,
	)
	if err != nil {
		return false, fmt.Errorf("delete runner: %w", err)
	}

	n, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete runner rows affected: %w", err)
	}
	return n > 0, nil
}

// UpdateHeartbeat updates a runner's last_heartbeat timestamp and optionally
// its running task count (stored in labels as "_running_tasks").
// Returns an error if the runner does not exist.
func (s *StorageLayer) UpdateHeartbeat(ctx context.Context, runnerID string, runningTasks int, stats map[string]interface{}) error {
	now := time.Now().UnixMilli()

	// If stats are provided, encode them into the labels field alongside existing labels.
	if stats != nil {
		// Read current runner to merge stats into labels
		runner, err := s.GetRunner(ctx, runnerID)
		if err != nil {
			return fmt.Errorf("update heartbeat get runner: %w", err)
		}
		if runner == nil {
			return fmt.Errorf("runner %q not found", runnerID)
		}

		if runner.Labels == nil {
			runner.Labels = make(map[string]string)
		}
		runner.Labels["_running_tasks"] = fmt.Sprintf("%d", runningTasks)
		for k, v := range stats {
			runner.Labels["_stat_"+k] = fmt.Sprintf("%v", v)
		}

		labelsJSON, err := json.Marshal(runner.Labels)
		if err != nil {
			return fmt.Errorf("marshal labels: %w", err)
		}

		result, err := s.db.ExecContext(ctx,
			"UPDATE runners SET last_heartbeat = ?, labels = ? WHERE runner_id = ?",
			now, string(labelsJSON), runnerID,
		)
		if err != nil {
			return fmt.Errorf("update heartbeat with stats: %w", err)
		}
		n, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("heartbeat rows affected: %w", err)
		}
		if n == 0 {
			return fmt.Errorf("runner %q not found", runnerID)
		}
		return nil
	}

	// Simple heartbeat update (no stats)
	result, err := s.db.ExecContext(ctx,
		"UPDATE runners SET last_heartbeat = ? WHERE runner_id = ?",
		now, runnerID,
	)
	if err != nil {
		return fmt.Errorf("update heartbeat: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("heartbeat rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("runner %q not found", runnerID)
	}
	return nil
}

// UpdateAffinity updates a runner's feature_ids (comma-separated list of feature IDs
// this runner has affinity for).
func (s *StorageLayer) UpdateAffinity(ctx context.Context, runnerID string, featureIDs []string) error {
	featureStr := strings.Join(featureIDs, ",")
	result, err := s.db.ExecContext(ctx,
		"UPDATE runners SET feature_ids = ? WHERE runner_id = ?",
		featureStr, runnerID,
	)
	if err != nil {
		return fmt.Errorf("update affinity: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("affinity rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("runner %q not found", runnerID)
	}
	return nil
}

// SetRunnerStatus updates a runner's status. Returns an error if the runner
// does not exist.
func (s *StorageLayer) SetRunnerStatus(ctx context.Context, runnerID, status string) error {
	result, err := s.db.ExecContext(ctx,
		"UPDATE runners SET status = ? WHERE runner_id = ?",
		status, runnerID,
	)
	if err != nil {
		return fmt.Errorf("set runner status: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("set status rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("runner %q not found", runnerID)
	}
	return nil
}

// UpdateRunnerMaxParallel updates a runner's max_parallel setting.
// Returns an error if the runner does not exist.
func (s *StorageLayer) UpdateRunnerMaxParallel(ctx context.Context, runnerID string, maxParallel int) error {
	result, err := s.db.ExecContext(ctx,
		"UPDATE runners SET max_parallel = ? WHERE runner_id = ?",
		maxParallel, runnerID,
	)
	if err != nil {
		return fmt.Errorf("update runner max_parallel: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("max_parallel rows affected: %w", err)
	}
	if n == 0 {
		return fmt.Errorf("runner %q not found", runnerID)
	}
	return nil
}

// ExpireStaleRunners marks runners as "offline" if their last heartbeat is
// older than the given threshold. Returns the number of runners updated.
func (s *StorageLayer) ExpireStaleRunners(ctx context.Context, threshold time.Duration) (int64, error) {
	cutoff := time.Now().UnixMilli() - threshold.Milliseconds()
	result, err := s.db.ExecContext(ctx,
		"UPDATE runners SET status = 'offline' WHERE status = 'online' AND last_heartbeat < ?",
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("expire stale runners: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("expire stale rows affected: %w", err)
	}
	return count, nil
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// scanRunners scans multiple runner rows from a query result.
func scanRunners(rows *sql.Rows) ([]RunnerRow, error) {
	var runners []RunnerRow
	for rows.Next() {
		var r RunnerRow
		var labelsJSON, executorsJSON, capabilitiesJSON string

		if err := rows.Scan(&r.RunnerID, &r.Hostname, &labelsJSON, &executorsJSON, &capabilitiesJSON,
			&r.MaxParallel, &r.FeatureIDs, &r.RegisteredAt, &r.LastHeartbeat,
			&r.Status); err != nil {
			return nil, fmt.Errorf("scan runner: %w", err)
		}

		if err := json.Unmarshal([]byte(labelsJSON), &r.Labels); err != nil {
			return nil, fmt.Errorf("unmarshal labels: %w", err)
		}
		if err := json.Unmarshal([]byte(executorsJSON), &r.Executors); err != nil {
			return nil, fmt.Errorf("unmarshal executors: %w", err)
		}
		if err := json.Unmarshal([]byte(capabilitiesJSON), &r.Capabilities); err != nil {
			return nil, fmt.Errorf("unmarshal capabilities: %w", err)
		}

		runners = append(runners, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runners rows error: %w", err)
	}
	return runners, nil
}

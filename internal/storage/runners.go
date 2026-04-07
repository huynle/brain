package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
)

// RunnerRow represents a row in the runners table.
type RunnerRow struct {
	RunnerID      string
	Hostname      string
	Projects      string // JSON array
	Capabilities  string // JSON array
	MaxParallel   int
	ActiveTasks   int
	Status        string
	Version       string
	RegisteredAt  string
	LastHeartbeat string
}

// RegisterRunner inserts or updates a runner registration.
// If the runner already exists, it updates hostname, projects, capabilities,
// max_parallel, version, status, and last_heartbeat (acts as upsert).
func (s *StorageLayer) RegisterRunner(ctx context.Context, row RunnerRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO runners (runner_id, hostname, projects, capabilities, max_parallel, active_tasks, status, version, registered_at, last_heartbeat)
		 VALUES (?, ?, ?, ?, ?, ?, 'online', ?, datetime('now'), datetime('now'))
		 ON CONFLICT(runner_id) DO UPDATE SET
		   hostname = excluded.hostname,
		   projects = excluded.projects,
		   capabilities = excluded.capabilities,
		   max_parallel = excluded.max_parallel,
		   status = 'online',
		   version = excluded.version,
		   last_heartbeat = datetime('now')`,
		row.RunnerID, row.Hostname, row.Projects, row.Capabilities,
		row.MaxParallel, row.ActiveTasks, row.Version,
	)
	if err != nil {
		return fmt.Errorf("register runner: %w", err)
	}
	return nil
}

// HeartbeatRunner updates a runner's last_heartbeat and optionally active_tasks.
// Returns an error if the runner is not found.
func (s *StorageLayer) HeartbeatRunner(ctx context.Context, runnerID string, activeTasks int, version string) error {
	setClauses := "last_heartbeat = datetime('now'), status = 'online', active_tasks = ?"
	args := []interface{}{activeTasks}

	if version != "" {
		setClauses += ", version = ?"
		args = append(args, version)
	}

	args = append(args, runnerID)

	result, err := s.db.ExecContext(ctx,
		"UPDATE runners SET "+setClauses+" WHERE runner_id = ?",
		args...,
	)
	if err != nil {
		return fmt.Errorf("heartbeat runner: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("runner not found: %s", runnerID)
	}
	return nil
}

// ListRunners returns all runners, marking stale ones as "lost".
// A runner is considered stale if its last heartbeat was more than
// staleDuration ago.
func (s *StorageLayer) ListRunners(ctx context.Context, staleDuration time.Duration) ([]RunnerRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT runner_id, hostname, projects, capabilities, max_parallel,
		        active_tasks, status, version, registered_at, last_heartbeat
		 FROM runners
		 ORDER BY registered_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("query runners: %w", err)
	}
	defer rows.Close()

	var runners []RunnerRow
	now := time.Now().UTC()
	for rows.Next() {
		var r RunnerRow
		if err := rows.Scan(&r.RunnerID, &r.Hostname, &r.Projects, &r.Capabilities,
			&r.MaxParallel, &r.ActiveTasks, &r.Status, &r.Version,
			&r.RegisteredAt, &r.LastHeartbeat); err != nil {
			return nil, fmt.Errorf("scan runner: %w", err)
		}

		// Compute status based on heartbeat freshness
		if hb, err := time.Parse("2006-01-02 15:04:05", r.LastHeartbeat); err == nil {
			if now.Sub(hb) > staleDuration {
				r.Status = "lost"
			} else {
				r.Status = "online"
			}
		}

		runners = append(runners, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	if runners == nil {
		return []RunnerRow{}, nil
	}
	return runners, nil
}

// MarkStaleRunners marks runners whose last heartbeat is older than
// staleDuration as "lost" and returns their IDs.
func (s *StorageLayer) MarkStaleRunners(ctx context.Context, staleDuration time.Duration) ([]string, error) {
	cutoff := time.Now().UTC().Add(-staleDuration).Format("2006-01-02 15:04:05")

	// First, find stale runners
	rows, err := s.db.QueryContext(ctx,
		"SELECT runner_id FROM runners WHERE last_heartbeat < ? AND status = 'online'",
		cutoff,
	)
	if err != nil {
		return nil, fmt.Errorf("query stale runners: %w", err)
	}
	defer rows.Close()

	var staleIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan stale runner: %w", err)
		}
		staleIDs = append(staleIDs, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	// Mark them as lost
	if len(staleIDs) > 0 {
		_, err = s.db.ExecContext(ctx,
			"UPDATE runners SET status = 'lost' WHERE last_heartbeat < ? AND status = 'online'",
			cutoff,
		)
		if err != nil {
			return nil, fmt.Errorf("mark stale runners: %w", err)
		}
	}

	return staleIDs, nil
}

// GetRunnerByID returns a single runner by ID, or nil if not found.
func (s *StorageLayer) GetRunnerByID(ctx context.Context, runnerID string) (*RunnerRow, error) {
	var r RunnerRow
	err := s.db.QueryRowContext(ctx,
		`SELECT runner_id, hostname, projects, capabilities, max_parallel,
		        active_tasks, status, version, registered_at, last_heartbeat
		 FROM runners WHERE runner_id = ?`,
		runnerID,
	).Scan(&r.RunnerID, &r.Hostname, &r.Projects, &r.Capabilities,
		&r.MaxParallel, &r.ActiveTasks, &r.Status, &r.Version,
		&r.RegisteredAt, &r.LastHeartbeat)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get runner: %w", err)
	}
	return &r, nil
}

// DeleteRunner removes a runner registration.
func (s *StorageLayer) DeleteRunner(ctx context.Context, runnerID string) error {
	result, err := s.db.ExecContext(ctx,
		"DELETE FROM runners WHERE runner_id = ?",
		runnerID,
	)
	if err != nil {
		return fmt.Errorf("delete runner: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("get rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("runner not found: %s", runnerID)
	}
	return nil
}

// ParseJSONStringArray parses a JSON array string into a Go string slice.
func ParseJSONStringArray(s string) []string {
	if s == "" || s == "[]" {
		return nil
	}
	var result []string
	if err := json.Unmarshal([]byte(s), &result); err != nil {
		return nil
	}
	return result
}

// ToJSONStringArray serializes a string slice to a JSON array string.
func ToJSONStringArray(ss []string) string {
	if len(ss) == 0 {
		return "[]"
	}
	data, err := json.Marshal(ss)
	if err != nil {
		return "[]"
	}
	return string(data)
}

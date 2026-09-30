package storage

import (
	"context"
	"fmt"
	"time"
)

// FeatureCascadeRootRow is a standing user request to run a feature and keep
// running whatever depends on it.
//
// Only the ROOT is stored. The chain is derived from feature_depends_on on
// every sweep rather than frozen here, for two reasons:
//
//   - A stored member list goes stale the instant someone edits
//     feature_depends_on, and the server would then be dispatching a chain
//     that no longer matches what the project declares.
//   - Features whose tasks are generated mid-run (feature-checkout
//     follow-ups, goal-generated work) do not exist at click time.
//     ComputeFeatures derives features from tasks, so a feature with no tasks
//     yet is invisible; freezing the closure would exclude it permanently.
type FeatureCascadeRootRow struct {
	ProjectID     string
	RootFeatureID string
	RequestedAt   int64
	// PausedAtRequest records whether the project's task dial was already
	// off when the user asked.
	//
	// It is the whole basis of the pause rule: propagation force-dispatches
	// past a pause that was already on (that is the isolate workflow — pause
	// the project, run one chain), but a pause applied AFTER the click stops
	// the chain spreading into features that have not started. Without this
	// flag the two cases are indistinguishable at sweep time.
	PausedAtRequest bool
}

// UpsertFeatureCascadeRoot records (or refreshes) a standing request.
// Idempotent: re-clicking "run + dependents" on the same feature restamps
// requested_at and the pause snapshot rather than creating a second chain.
func (s *TenantStore) UpsertFeatureCascadeRoot(ctx context.Context, projectID, rootFeatureID string, pausedAtRequest bool) error {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return err
	}
	if projectID == "" || rootFeatureID == "" {
		return fmt.Errorf("project id and root feature id are required")
	}
	columns, values, conflict := "project_id, root_feature_id, requested_at, paused_at_request", "?, ?, ?, ?", "project_id, root_feature_id"
	args := []interface{}{projectID, rootFeatureID, time.Now().UnixMilli(), boolToInt(pausedAtRequest)}
	if scope.owner != "" {
		columns += ", tenant_id"
		values += ", ?"
		conflict = "tenant_id, project_id, root_feature_id"
		args = append(args, scope.owner)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO feature_cascade_roots (`+columns+`)
VALUES (`+values+`)
ON CONFLICT(`+conflict+`) DO UPDATE SET
  requested_at = excluded.requested_at,
  paused_at_request = excluded.paused_at_request`,
		args...)
	if err != nil {
		return fmt.Errorf("upsert feature cascade root: %w", err)
	}
	return nil
}

// DeleteFeatureCascadeRoot cancels a standing request. Reports whether a row
// was actually removed so callers can tell "cancelled" from "there was
// nothing to cancel".
func (s *TenantStore) DeleteFeatureCascadeRoot(ctx context.Context, projectID, rootFeatureID string) (bool, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return false, err
	}
	if projectID == "" || rootFeatureID == "" {
		return false, fmt.Errorf("project id and root feature id are required")
	}
	where, args := scope.where("project_id = ? AND root_feature_id = ?", projectID, rootFeatureID)
	res, err := s.db.ExecContext(ctx,
		"DELETE FROM feature_cascade_roots WHERE "+where, args...)
	if err != nil {
		return false, fmt.Errorf("delete feature cascade root: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("delete feature cascade root (rows): %w", err)
	}
	return n > 0, nil
}

// ListFeatureCascadeRoots returns standing requests for one project, or for
// every project when projectID is empty (the boot path).
func (s *TenantStore) ListFeatureCascadeRoots(ctx context.Context, projectID string) ([]FeatureCascadeRootRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	where, args := scope.where("1=1")
	query := "SELECT project_id, root_feature_id, requested_at, paused_at_request FROM feature_cascade_roots WHERE " + where
	if projectID != "" {
		query += " AND project_id = ?"
		args = append(args, projectID)
	}
	query += " ORDER BY requested_at"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list feature cascade roots: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []FeatureCascadeRootRow
	for rows.Next() {
		var r FeatureCascadeRootRow
		var paused int
		if err := rows.Scan(&r.ProjectID, &r.RootFeatureID, &r.RequestedAt, &paused); err != nil {
			return nil, fmt.Errorf("scan feature cascade root: %w", err)
		}
		r.PausedAtRequest = paused != 0
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate feature cascade roots: %w", err)
	}
	return out, nil
}

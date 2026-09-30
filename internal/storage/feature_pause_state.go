package storage

import (
	"context"
	"fmt"
	"time"
)

// Feature-scoped pause state.
//
// The fourth dial, after the two project ones and the runner one. It holds
// a single feature's tasks out of automatic dispatch while leaving the rest
// of the project running — the case the project dial is too coarse for, and
// the one a manually started feature lands in.
//
// A feature is a COMPUTED grouping (tasks sharing a feature_id), not a
// stored entity, so there is no row to put a flag on; hence a table of its
// own. That also makes the hold survive its tasks: a feature that is
// archived and later re-created under the same id stays held, which is the
// safe direction for a switch a human flipped.

// SetFeaturePaused turns the feature dial on or off for one feature.
func (s *TenantStore) SetFeaturePaused(ctx context.Context, projectID, featureID string, paused bool) error {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return err
	}
	if projectID == "" {
		return fmt.Errorf("project id is required")
	}
	if featureID == "" {
		// An empty feature id would key a row that matches every task with
		// no feature — a hold nobody asked for on the ungrouped bucket.
		return fmt.Errorf("feature id is required")
	}
	now := time.Now().UnixMilli()
	columns, values, conflict := "project_id, feature_id, paused, updated_at", "?, ?, ?, ?", "project_id, feature_id"
	args := []interface{}{projectID, featureID, boolToInt(paused), now}
	if scope.owner != "" {
		columns += ", tenant_id"
		values += ", ?"
		conflict = "tenant_id, project_id, feature_id"
		args = append(args, scope.owner)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO feature_pause_state (`+columns+`)
		VALUES (`+values+`)
		ON CONFLICT(`+conflict+`) DO UPDATE SET
		  paused = excluded.paused,
		  updated_at = excluded.updated_at`,
		args...)
	if err != nil {
		return fmt.Errorf("set feature pause state: %w", err)
	}
	return nil
}

// PausedFeature identifies one held feature.
type PausedFeature struct {
	ProjectID string
	FeatureID string
}

// ListPausedFeatures returns every feature whose dial is off, across all
// projects. Read whole rather than per-feature because the scheduler asks
// about one task at a time and a query per task would be one query per
// dispatch decision.
func (s *TenantStore) ListPausedFeatures(ctx context.Context) ([]PausedFeature, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	where, args := scope.where("paused = 1")
	rows, err := s.db.QueryContext(ctx, `
		SELECT project_id, feature_id FROM feature_pause_state WHERE `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("list paused features: %w", err)
	}
	defer rows.Close()

	var out []PausedFeature
	for rows.Next() {
		var f PausedFeature
		if err := rows.Scan(&f.ProjectID, &f.FeatureID); err != nil {
			return nil, fmt.Errorf("scan paused feature: %w", err)
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// IsFeaturePaused reports whether one feature's dial is off.
func (s *TenantStore) IsFeaturePaused(ctx context.Context, projectID, featureID string) (bool, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return false, err
	}
	if projectID == "" || featureID == "" {
		return false, nil
	}
	var value int
	where, args := scope.where("project_id = ? AND feature_id = ?", projectID, featureID)
	err = s.db.QueryRowContext(ctx,
		"SELECT paused FROM feature_pause_state WHERE "+where,
		args...).Scan(&value)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return false, nil
		}
		return false, fmt.Errorf("get feature pause state: %w", err)
	}
	return value != 0, nil
}

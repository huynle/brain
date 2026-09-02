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
func (s *StorageLayer) SetFeaturePaused(ctx context.Context, projectID, featureID string, paused bool) error {
	if projectID == "" {
		return fmt.Errorf("project id is required")
	}
	if featureID == "" {
		// An empty feature id would key a row that matches every task with
		// no feature — a hold nobody asked for on the ungrouped bucket.
		return fmt.Errorf("feature id is required")
	}
	now := time.Now().UnixMilli()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO feature_pause_state (project_id, feature_id, paused, updated_at)
		VALUES (?, ?, ?, ?)
		ON CONFLICT(project_id, feature_id) DO UPDATE SET
		  paused = excluded.paused,
		  updated_at = excluded.updated_at`,
		projectID, featureID, boolToInt(paused), now)
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
func (s *StorageLayer) ListPausedFeatures(ctx context.Context) ([]PausedFeature, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT project_id, feature_id FROM feature_pause_state WHERE paused = 1`)
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
func (s *StorageLayer) IsFeaturePaused(ctx context.Context, projectID, featureID string) (bool, error) {
	if projectID == "" || featureID == "" {
		return false, nil
	}
	var value int
	err := s.db.QueryRowContext(ctx,
		"SELECT paused FROM feature_pause_state WHERE project_id = ? AND feature_id = ?",
		projectID, featureID).Scan(&value)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return false, nil
		}
		return false, fmt.Errorf("get feature pause state: %w", err)
	}
	return value != 0, nil
}

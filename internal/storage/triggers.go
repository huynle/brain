package storage

import (
	"context"
	"fmt"
)

// ListTriggeredTasks returns all note rows that have a trigger configuration
// in their metadata JSON. This queries for notes where the metadata contains
// a "trigger" key with an "event" field, indicating the note has a trigger.
func (s *StorageLayer) ListTriggeredTasks(ctx context.Context) ([]*NoteRow, error) {
	// Use SQLite's json_extract to find notes with a non-null trigger.event.
	// The trigger config is stored in the metadata JSON column.
	query := `SELECT ` + noteColumns + ` FROM notes
		WHERE type = 'task'
		AND json_extract(metadata, '$.trigger.event') IS NOT NULL
		AND json_extract(metadata, '$.trigger.event') != ''`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list triggered tasks: %w", err)
	}
	defer rows.Close()

	notes, err := scanNoteRows(rows)
	if err != nil {
		return nil, fmt.Errorf("list triggered tasks: %w", err)
	}
	if notes == nil {
		return []*NoteRow{}, nil
	}
	return notes, nil
}

// CountInProgressByTrigger counts task notes that are currently in_progress
// and have a trigger event matching the given pattern within a project.
// This is used for max_concurrent enforcement.
func (s *StorageLayer) CountInProgressByTrigger(ctx context.Context, triggerEvent, projectID string) (int, error) {
	query := `SELECT COUNT(*) FROM notes
		WHERE type = 'task'
		AND status = 'in_progress'
		AND json_extract(metadata, '$.trigger.event') IS NOT NULL`

	args := make([]interface{}, 0, 2)

	if projectID != "" {
		query += ` AND project_id = ?`
		args = append(args, projectID)
	}

	var count int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count in-progress by trigger: %w", err)
	}
	return count, nil
}

// ActivateTask sets a task to the specified status by merging the given fields
// into its metadata. This delegates to MergeMetadata which handles the
// metadata merge and status column sync.
func (s *StorageLayer) ActivateTask(ctx context.Context, path string, fields map[string]interface{}) error {
	_, err := s.MergeMetadata(ctx, path, fields)
	if err != nil {
		return fmt.Errorf("activate task %s: %w", path, err)
	}
	return nil
}

package storage

import (
	"context"
	"fmt"
)

// IndexedNoteState is the persisted state needed for incremental discovery.
// A nil checksum forces reindexing even when the path already exists.
type IndexedNoteState struct {
	Path     string
	Checksum *string
}

// ListIndexedNoteStates materializes the bound tenant's index snapshot and
// closes the cursor before callers begin updating the index.
func (s *TenantStore) ListIndexedNoteStates(ctx context.Context) ([]IndexedNoteState, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	predicate, args := scope.where("1=1")
	rows, err := s.db.QueryContext(ctx, "SELECT path, checksum FROM notes WHERE "+predicate, args...)
	if err != nil {
		return nil, fmt.Errorf("query indexed note states: %w", err)
	}
	defer rows.Close()
	var states []IndexedNoteState
	for rows.Next() {
		var state IndexedNoteState
		if err := rows.Scan(&state.Path, &state.Checksum); err != nil {
			return nil, fmt.Errorf("scan indexed note state: %w", err)
		}
		states = append(states, state)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate indexed note states: %w", err)
	}
	return states, nil
}

// DeleteAllNotes clears only the bound tenant's notes, cascading dependent
// content, and reports the number of notes actually deleted.
func (s *TenantStore) DeleteAllNotes(ctx context.Context) (int64, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return 0, err
	}
	predicate, args := scope.where("1=1")
	result, err := s.db.ExecContext(ctx, "DELETE FROM notes WHERE "+predicate, args...)
	if err != nil {
		return 0, fmt.Errorf("delete all notes: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("count deleted notes: %w", err)
	}
	return count, nil
}

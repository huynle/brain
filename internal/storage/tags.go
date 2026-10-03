package storage

import (
	"context"
	"fmt"
)

// SetTags replaces all tags for the note at notePath.
// In a transaction: deletes existing tags, inserts new ones.
// Returns an error if the note is not found.
func (s *TenantStore) SetTags(ctx context.Context, notePath string, tags []string) error {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return err
	}
	note, err := s.GetNoteByPath(ctx, notePath)
	if err != nil {
		return fmt.Errorf("set tags: %w", err)
	}
	if note == nil {
		return fmt.Errorf("note not found: %s", notePath)
	}

	tx, err := beginResilientTx(ctx, s.db, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck
	if err := requireOwnedNote(ctx, tx.Tx, scope, note.ID); err != nil {
		return err
	}

	// Delete all existing tags for this note.
	where, args := scope.where("note_id = ?", note.ID)
	if _, err := tx.ExecContext(ctx, "DELETE FROM tags WHERE "+where, args...); err != nil {
		return fmt.Errorf("delete tags: %w", err)
	}

	// Insert new tags.
	for _, tag := range tags {
		query := "INSERT INTO tags (note_id, tag) VALUES (?, ?)"
		args := []interface{}{note.ID, tag}
		if scope.owner != "" {
			query = "INSERT INTO tags (note_id, tag, tenant_id) VALUES (?, ?, ?)"
			args = append(args, scope.owner)
		}
		if _, err := tx.ExecContext(ctx, query, args...); err != nil {
			return fmt.Errorf("insert tag %q: %w", tag, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tags: %w", err)
	}
	return nil
}

// GetTags returns all tags for the note at notePath.
// Returns an error if the note is not found.
// Returns a non-nil empty slice if the note has no tags.
func (s *TenantStore) GetTags(ctx context.Context, notePath string) ([]string, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	note, err := s.GetNoteByPath(ctx, notePath)
	if err != nil {
		return nil, fmt.Errorf("get tags: %w", err)
	}
	if note == nil {
		return nil, fmt.Errorf("note not found: %s", notePath)
	}

	where, args := scope.where("note_id = ?", note.ID)
	noteWhere, noteArgs := scope.where("id = ?", note.ID)
	where += " AND EXISTS (SELECT 1 FROM notes WHERE " + noteWhere + ")"
	rows, err := s.db.QueryContext(ctx, "SELECT tag FROM tags WHERE "+where, append(args, noteArgs...)...)
	if err != nil {
		return nil, fmt.Errorf("query tags: %w", err)
	}
	defer rows.Close()

	tags := make([]string, 0)
	for rows.Next() {
		var tag string
		if err := rows.Scan(&tag); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tags: %w", err)
	}
	return tags, nil
}

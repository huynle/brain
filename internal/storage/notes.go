package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// noteColumns is the canonical column order for scanning NoteRow fields.
const noteColumns = `id, path, short_id, title, lead, body, raw_content, word_count, checksum, metadata, type, status, priority, project_id, feature_id, created, modified, indexed_at`

// allowedUpdateFields is the set of fields that UpdateNote accepts.
// This prevents SQL injection via dynamic field names.
var allowedUpdateFields = map[string]bool{
	"title":       true,
	"lead":        true,
	"body":        true,
	"raw_content": true,
	"word_count":  true,
	"checksum":    true,
	"metadata":    true,
	"type":        true,
	"status":      true,
	"priority":    true,
	"project_id":  true,
	"feature_id":  true,
	"created":     true,
	"modified":    true,
}

// scanNoteRow scans a single row into a NoteRow.
func scanNoteRow(row *sql.Row) (*NoteRow, error) {
	var n NoteRow
	err := row.Scan(
		&n.ID, &n.Path, &n.ShortID, &n.Title,
		&n.Lead, &n.Body, &n.RawContent, &n.WordCount,
		&n.Checksum, &n.Metadata, &n.Type, &n.Status,
		&n.Priority, &n.ProjectID, &n.FeatureID,
		&n.Created, &n.Modified, &n.IndexedAt,
	)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

// scanNoteRows scans multiple rows into a slice of NoteRow pointers.
func scanNoteRows(rows *sql.Rows) ([]*NoteRow, error) {
	var notes []*NoteRow
	for rows.Next() {
		var n NoteRow
		err := rows.Scan(
			&n.ID, &n.Path, &n.ShortID, &n.Title,
			&n.Lead, &n.Body, &n.RawContent, &n.WordCount,
			&n.Checksum, &n.Metadata, &n.Type, &n.Status,
			&n.Priority, &n.ProjectID, &n.FeatureID,
			&n.Created, &n.Modified, &n.IndexedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan note row: %w", err)
		}
		notes = append(notes, &n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate note rows: %w", err)
	}
	return notes, nil
}

// InsertNote inserts a new note and returns the inserted row with ID and IndexedAt populated.
// Returns a descriptive error if the path already exists (UNIQUE constraint).
func (s *StorageLayer) InsertNote(ctx context.Context, note *NoteRow) (*NoteRow, error) {
	query := `
		INSERT INTO notes (path, short_id, title, lead, body, raw_content, word_count, checksum, metadata, type, status, priority, project_id, feature_id, created, modified)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	res, err := s.db.ExecContext(ctx, query,
		note.Path, note.ShortID, note.Title,
		note.Lead, note.Body, note.RawContent, note.WordCount,
		note.Checksum, note.Metadata, note.Type, note.Status,
		note.Priority, note.ProjectID, note.FeatureID,
		note.Created, note.Modified,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, fmt.Errorf("duplicate path %q: %w", note.Path, err)
		}
		return nil, fmt.Errorf("insert note: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("get last insert id: %w", err)
	}

	// Query back the full row to get ID and IndexedAt (set by SQLite DEFAULT).
	row := s.db.QueryRowContext(ctx,
		"SELECT "+noteColumns+" FROM notes WHERE id = ?", id,
	)
	inserted, err := scanNoteRow(row)
	if err != nil {
		return nil, fmt.Errorf("read back inserted note: %w", err)
	}
	return inserted, nil
}

// GetNoteByPath retrieves a note by exact path. Returns nil, nil if not found.
func (s *StorageLayer) GetNoteByPath(ctx context.Context, path string) (*NoteRow, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT "+noteColumns+" FROM notes WHERE path = ?", path,
	)
	n, err := scanNoteRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get note by path: %w", err)
	}
	return n, nil
}

// GetNoteByShortID retrieves a note by short_id. Returns nil, nil if not found.
// Returns the first match (short_id is not unique).
func (s *StorageLayer) GetNoteByShortID(ctx context.Context, shortID string) (*NoteRow, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT "+noteColumns+" FROM notes WHERE short_id = ? LIMIT 1", shortID,
	)
	n, err := scanNoteRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get note by short_id: %w", err)
	}
	return n, nil
}

// GetNoteByTitle retrieves a note by exact title match. Returns nil, nil if not found.
func (s *StorageLayer) GetNoteByTitle(ctx context.Context, title string) (*NoteRow, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT "+noteColumns+" FROM notes WHERE title = ? LIMIT 1", title,
	)
	n, err := scanNoteRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get note by title: %w", err)
	}
	return n, nil
}

// UpdateNote updates a note by path with the given field updates.
// Only fields in the allowlist are accepted to prevent SQL injection.
// Auto-updates indexed_at to datetime('now').
// Returns nil, nil if the path is not found.
func (s *StorageLayer) UpdateNote(ctx context.Context, path string, updates map[string]interface{}) (*NoteRow, error) {
	if len(updates) == 0 {
		return s.GetNoteByPath(ctx, path)
	}

	// Validate all field names against the allowlist.
	for field := range updates {
		if !allowedUpdateFields[field] {
			return nil, fmt.Errorf("field %q is not allowed for update", field)
		}
	}

	// Build dynamic SET clause.
	setClauses := make([]string, 0, len(updates)+1)
	args := make([]interface{}, 0, len(updates)+1)

	for field, value := range updates {
		setClauses = append(setClauses, field+" = ?")
		args = append(args, value)
	}

	// Always update indexed_at.
	setClauses = append(setClauses, "indexed_at = datetime('now')")

	// Path is the WHERE condition.
	args = append(args, path)

	query := "UPDATE notes SET " + strings.Join(setClauses, ", ") + " WHERE path = ?"
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("update note: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("rows affected: %w", err)
	}
	if rowsAffected == 0 {
		return nil, nil
	}

	return s.GetNoteByPath(ctx, path)
}

// DeleteNote deletes a note by path. Returns true if deleted, false if not found.
// CASCADE handles cleanup of associated links and tags.
func (s *StorageLayer) DeleteNote(ctx context.Context, path string) (bool, error) {
	res, err := s.db.ExecContext(ctx, "DELETE FROM notes WHERE path = ?", path)
	if err != nil {
		return false, fmt.Errorf("delete note: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return rowsAffected > 0, nil
}

package storage

import (
	"context"
	"database/sql"
	"encoding/json"
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

	// Repair links that were indexed before this note existed and are still
	// dangling on its path, short ID, or (for wiki-links) its title.
	if err := s.ResolveLinksToNote(ctx, inserted.ID, inserted.Path, inserted.ShortID, inserted.Title, inserted.ProjectID); err != nil {
		return nil, err
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
// short_id is not unique, so the lowest row id wins — an arbitrary choice, but
// a stable one. Without the ORDER BY, a collision resolved to whichever row
// SQLite happened to visit first, which could change between queries.
func (s *StorageLayer) GetNoteByShortID(ctx context.Context, shortID string) (*NoteRow, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT "+noteColumns+" FROM notes WHERE short_id = ? ORDER BY id LIMIT 1", shortID,
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

// GetNoteByTitle retrieves a note by exact title match. Returns nil, nil if not
// found. Titles are not unique either; the lowest row id wins.
func (s *StorageLayer) GetNoteByTitle(ctx context.Context, title string) (*NoteRow, error) {
	return s.GetNoteByTitleScoped(ctx, title, nil)
}

// GetNoteByTitleScoped retrieves a note by exact title, preferring one that
// lives in projectID, then a global entry, then anything else. Returns nil, nil
// if no note carries the title.
//
// Titles are not unique across the brain — "Summary" or "Notes" recur in every
// project — so an unscoped lookup would hand a wiki-link in project A to a
// same-titled note in project B. Ranking by scope keeps a link pointing at the
// entry the author could actually see from where they wrote it, and the
// trailing id keeps the answer stable when the rank ties.
func (s *StorageLayer) GetNoteByTitleScoped(ctx context.Context, title string, projectID *string) (*NoteRow, error) {
	query := "SELECT " + noteColumns + ` FROM notes WHERE title = ?
		ORDER BY
			CASE
				WHEN ? IS NOT NULL AND project_id = ? THEN 0
				WHEN project_id IS NULL THEN 1
				ELSE 2
			END,
			id
		LIMIT 1`
	row := s.db.QueryRowContext(ctx, query, title, projectID, projectID)
	n, err := scanNoteRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get note by title: %w", err)
	}
	return n, nil
}

// MergeMetadata performs a shallow JSON merge on the metadata column for a note.
// It reads the current metadata, merges the provided fields, and writes back.
// This operates entirely in SQLite without touching the filesystem.
func (s *StorageLayer) MergeMetadata(ctx context.Context, path string, fields map[string]interface{}) (*NoteRow, error) {
	// Read and write inside ONE transaction.
	//
	// This is a read-modify-write: the merge happens in Go between a SELECT and
	// an UPDATE. Without a transaction the two statements do not stay on the
	// same connection — database/sql returns it to the pool in between — so
	// concurrent merges read the same "before" value and the last writer wins.
	//
	// Measured, not theorised: 50 goroutines merging 50 DISTINCT keys into one
	// note left 2 keys. Forty-eight updates silently lost, at
	// SetMaxOpenConns(1), which is production's setting today. The connection
	// cap serialises TRANSACTIONS, because a *sql.Tx pins its connection for
	// its lifetime — it does not serialise consecutive statements.
	//
	// That matters here beyond bookkeeping: task.go writes resume_requested
	// through this function, the flag the runner reads at claim time to route a
	// task through the resume prompt, and the runner CLEARS that flag after a
	// successful spawn. Two writers, one note, a flag whose loss silently turns
	// a resume back into an ordinary run.
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin merge metadata tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var currentMetadata string
	err = tx.QueryRowContext(ctx, "SELECT metadata FROM notes WHERE path = ?", path).Scan(&currentMetadata)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read metadata: %w", err)
	}

	// Parse existing metadata
	existing := make(map[string]interface{})
	if currentMetadata != "" && currentMetadata != "{}" {
		if err := json.Unmarshal([]byte(currentMetadata), &existing); err != nil {
			existing = make(map[string]interface{})
		}
	}

	// Merge new fields (shallow: top-level keys from fields override existing,
	// but for map values like "sessions", we do a deep merge one level)
	for k, v := range fields {
		if newMap, ok := v.(map[string]interface{}); ok {
			if existingMap, ok := existing[k].(map[string]interface{}); ok {
				// Merge maps one level deep
				for mk, mv := range newMap {
					existingMap[mk] = mv
				}
				existing[k] = existingMap
				continue
			}
		}
		existing[k] = v
	}

	// Serialize and write back
	metaJSON, err := json.Marshal(existing)
	if err != nil {
		return nil, fmt.Errorf("marshal metadata: %w", err)
	}

	updates := map[string]interface{}{
		"metadata": string(metaJSON),
	}

	// If "status" is in the fields, also update the status column directly.
	// This ensures the DB status column stays in sync with the metadata JSON.
	if statusVal, ok := fields["status"]; ok {
		if statusStr, ok := statusVal.(string); ok && statusStr != "" {
			updates["status"] = statusStr
		}
	}

	setClauses, args, err := buildNoteUpdate(updates)
	if err != nil {
		return nil, err
	}
	args = append(args, path)

	rowsAffected, err := execNoteUpdate(ctx, tx, path, setClauses, args)
	if err != nil {
		return nil, err
	}
	if rowsAffected == 0 {
		return nil, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit merge metadata: %w", err)
	}

	return s.GetNoteByPath(ctx, path)
}

// UpdateNote updates a note by path with the given field updates.
// Only fields in the allowlist are accepted to prevent SQL injection.
// Auto-updates indexed_at to datetime('now').
// Returns nil, nil if the path is not found.
func (s *StorageLayer) UpdateNote(ctx context.Context, path string, updates map[string]interface{}) (*NoteRow, error) {
	if len(updates) == 0 {
		return s.GetNoteByPath(ctx, path)
	}

	setClauses, args, err := buildNoteUpdate(updates)
	if err != nil {
		return nil, err
	}
	// Path is the WHERE condition.
	args = append(args, path)

	rowsAffected, err := execNoteUpdate(ctx, s.db, path, setClauses, args)
	if err != nil {
		return nil, err
	}
	if rowsAffected == 0 {
		return nil, nil
	}

	return s.GetNoteByPath(ctx, path)
}

// noteExecer is satisfied by both *sql.DB and *sql.Tx, so the UPDATE below can
// run standalone or inside a caller's transaction without duplicating it.
type noteExecer interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
}

// execNoteUpdate runs the dynamic UPDATE shared by UpdateNote and
// MergeMetadata and reports how many rows it touched.
func execNoteUpdate(ctx context.Context, ex noteExecer, path string, setClauses []string, args []interface{}) (int64, error) {
	query := "UPDATE notes SET " + strings.Join(setClauses, ", ") + " WHERE path = ?"
	res, err := ex.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, fmt.Errorf("update note: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return rowsAffected, nil
}

// buildNoteUpdate validates the field allowlist and builds the SET clause.
func buildNoteUpdate(updates map[string]interface{}) ([]string, []interface{}, error) {
	for field := range updates {
		if !allowedUpdateFields[field] {
			return nil, nil, fmt.Errorf("field %q is not allowed for update", field)
		}
	}
	setClauses := make([]string, 0, len(updates)+1)
	args := make([]interface{}, 0, len(updates)+1)
	for field, value := range updates {
		setClauses = append(setClauses, field+" = ?")
		args = append(args, value)
	}
	// Always update indexed_at.
	setClauses = append(setClauses, "indexed_at = datetime('now')")
	return setClauses, args, nil
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

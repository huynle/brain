package storage

import (
	"context"
	"fmt"
	"strings"
)

// ProjectPathPrefix returns the path prefix every entry of a project lives
// under. Entries are addressed by path, not only by project_id: a row whose
// frontmatter never carried a project still sits under projects/<id>/, and a
// project purge that queried project_id alone would leave it behind.
func ProjectPathPrefix(projectID string) string {
	return "projects/" + projectID + "/"
}

// ListProjectNotePaths returns the indexed path of every note belonging to a
// project — matched by path prefix OR project_id, so an entry that is missing
// one of the two is still found.
//
// Ordered by path so a caller reporting per-entry outcomes produces a stable
// list, and so directory contents delete in a predictable order.
func (s *StorageLayer) ListProjectNotePaths(ctx context.Context, projectID string) ([]string, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project id required")
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT path FROM notes WHERE path LIKE ? ESCAPE '\' OR project_id = ? ORDER BY path`,
		escapeLikePrefix(ProjectPathPrefix(projectID))+"%", projectID)
	if err != nil {
		return nil, fmt.Errorf("query project note paths: %w", err)
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, fmt.Errorf("scan note path: %w", err)
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

// escapeLikePrefix neutralises the LIKE wildcards in a literal prefix. A
// project literally named "foo_bar" would otherwise also match "fooXbar" —
// harmless for a listing, unacceptable for a delete.
func escapeLikePrefix(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s)
}

// projectScopedTables are the tables keyed by project_id that describe
// runtime state for a project's tasks. Deleting the project's entries without
// clearing these leaves claims and dispatch leases pointing at task ids that
// no longer exist — rows the scheduler would keep honouring as "work someone
// is holding".
//
// Deliberately excluded:
//   - attachments/entry_attachments: blobs are content-addressed and shared
//     across projects; removing one project's rows could orphan bytes another
//     project still references. Entry links go with the notes row (CASCADE).
//   - event_log: history of what happened, not state. It survives the project
//     the same way a git log survives a deleted file.
var projectScopedTables = []string{
	"task_claims",
	"task_dispatch_leases",
	"task_placement_reasons",
	"feature_assignments",
	"feature_cascade_roots",
	"project_pause_state",
	"project_placement",
}

// PurgeProjectState deletes every project-scoped runtime row for a project
// and returns the number of rows removed per table.
//
// Runs as one transaction: a half-purged project (claims gone, leases held)
// is worse than an untouched one, because the scheduler reads the two
// together.
func (s *StorageLayer) PurgeProjectState(ctx context.Context, projectID string) (map[string]int64, error) {
	if projectID == "" {
		return nil, fmt.Errorf("project id required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin purge tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	removed := make(map[string]int64, len(projectScopedTables))
	for _, table := range projectScopedTables {
		// Table names come from the constant list above, never from input.
		res, err := tx.ExecContext(ctx,
			fmt.Sprintf("DELETE FROM %s WHERE project_id = ?", table), projectID)
		if err != nil {
			return nil, fmt.Errorf("purge %s: %w", table, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return nil, fmt.Errorf("purge %s rows affected: %w", table, err)
		}
		if n > 0 {
			removed[table] = n
		}
	}

	// opencode_instances is project-scoped but its column is nullable and
	// defaults to '', so it is filtered separately rather than folded into
	// the loop above.
	res, err := tx.ExecContext(ctx,
		"DELETE FROM opencode_instances WHERE project_id = ?", projectID)
	if err != nil {
		return nil, fmt.Errorf("purge opencode_instances: %w", err)
	}
	if n, err := res.RowsAffected(); err == nil && n > 0 {
		removed["opencode_instances"] = n
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit purge tx: %w", err)
	}
	return removed, nil
}

// DeleteProjectNotes removes every notes row for a project by path prefix or
// project_id. CASCADE clears links, tags, entry_meta and embeddings.
//
// The per-entry Delete path is still the primary one — it also removes the
// file from disk and publishes an event. This is the sweep afterwards, for
// index rows whose file was already gone.
func (s *StorageLayer) DeleteProjectNotes(ctx context.Context, projectID string) (int64, error) {
	if projectID == "" {
		return 0, fmt.Errorf("project id required")
	}
	res, err := s.db.ExecContext(ctx,
		`DELETE FROM notes WHERE path LIKE ? ESCAPE '\' OR project_id = ?`,
		escapeLikePrefix(ProjectPathPrefix(projectID))+"%", projectID)
	if err != nil {
		return 0, fmt.Errorf("delete project notes: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("rows affected: %w", err)
	}
	return n, nil
}

package storage

import (
	"context"
	"fmt"
	"strings"
)

// DefaultListLimit is the default number of results returned by ListNotes.
// Exported because the service layer over-fetches relative to it when a
// post-SQL filter is in play, and a second hand-copied 100 would be free to
// drift away from this one.
const DefaultListLimit = 100

// allowedSortColumns maps user-facing sort names to SQL sort expressions.
// Values are trusted constants keyed by an allowlist, which prevents SQL
// injection via dynamic column names.
//
// "completed" sorts by the completed_at metadata stamp, falling back to
// modified for entries completed before completed_at existed — for those,
// modified IS the completion time (status flips were their last write), so
// the COALESCE doubles as the backfill.
var allowedSortColumns = map[string]string{
	"modified":  "modified",
	"created":   "created",
	"priority":  "priority",
	"title":     "title",
	"completed": "COALESCE(NULLIF(json_extract(metadata, '$.completed_at'), ''), modified)",
}

// ListNotes returns notes matching the given filter options.
// If opts is nil, returns all notes with default sort (modified DESC) and limit (100).
func (s *StorageLayer) ListNotes(ctx context.Context, opts *ListOptions) ([]*NoteRow, error) {
	where := make([]string, 0)
	params := make([]interface{}, 0)

	if opts != nil {
		if opts.Type != "" {
			where = append(where, "type = ?")
			params = append(params, opts.Type)
		}
		if opts.Status != "" {
			where = append(where, "status = ?")
			params = append(params, opts.Status)
		}
		if opts.ProjectID != "" {
			where = append(where, "project_id = ?")
			params = append(params, opts.ProjectID)
		} else if clause, scopeParams := projectScopeClause(
			"project_id", "path", opts.ProjectIDs, opts.IncludeGlobalPath,
		); clause != "" {
			where = append(where, clause)
			params = append(params, scopeParams...)
		}
		if opts.FeatureID != "" {
			where = append(where, "feature_id = ?")
			params = append(params, opts.FeatureID)
		}
		if opts.Priority != "" {
			where = append(where, "priority = ?")
			params = append(params, opts.Priority)
		}
		if opts.PathPrefix != "" {
			where = append(where, "path LIKE ?")
			params = append(params, opts.PathPrefix+"%")
		}
		if opts.Tag != "" {
			where = append(where, "id IN (SELECT note_id FROM tags WHERE tag = ?)")
			params = append(params, opts.Tag)
		}
		if len(opts.Tags) > 0 {
			placeholders := make([]string, len(opts.Tags))
			for i := range opts.Tags {
				placeholders[i] = "?"
				params = append(params, opts.Tags[i])
			}
			where = append(where,
				fmt.Sprintf("id IN (SELECT note_id FROM tags WHERE tag IN (%s) GROUP BY note_id HAVING COUNT(DISTINCT tag) = ?)",
					strings.Join(placeholders, ",")))
			params = append(params, len(opts.Tags))
		}
	}

	// Build query.
	query := "SELECT " + noteColumns + " FROM notes"
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}

	// Sort.
	sortBy := "modified"
	sortOrder := "DESC"
	if opts != nil {
		if col, ok := allowedSortColumns[opts.SortBy]; ok {
			sortBy = col
		}
		if strings.EqualFold(opts.SortOrder, "asc") {
			sortOrder = "ASC"
		}
	}
	query += fmt.Sprintf(" ORDER BY %s %s", sortBy, sortOrder)

	// Pagination.
	limit := DefaultListLimit
	if opts != nil && opts.Limit > 0 {
		limit = opts.Limit
	}
	query += " LIMIT ?"
	params = append(params, limit)

	if opts != nil && opts.Offset > 0 {
		query += " OFFSET ?"
		params = append(params, opts.Offset)
	}

	rows, err := s.db.QueryContext(ctx, query, params...)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	defer rows.Close()

	notes, err := scanNoteRows(rows)
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}
	if notes == nil {
		return []*NoteRow{}, nil
	}
	return notes, nil
}

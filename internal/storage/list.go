package storage

import (
	"context"
	"fmt"
	"strings"
)

// defaultListLimit is the default number of results returned by ListNotes.
const defaultListLimit = 100

// allowedSortColumns maps user-facing sort names to actual SQL column names.
// This prevents SQL injection via dynamic column names.
var allowedSortColumns = map[string]string{
	"modified": "modified",
	"created":  "created",
	"priority": "priority",
	"title":    "title",
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
		}
		if opts.FeatureID != "" {
			where = append(where, "feature_id = ?")
			params = append(params, opts.FeatureID)
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
	limit := defaultListLimit
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

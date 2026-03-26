package storage

import (
	"context"
	"fmt"
)

// SetLinks replaces all links for the note at notePath.
// For each link, tries to resolve target_path to an existing note (sets target_id if found).
// Returns an error if the source note is not found.
func (s *StorageLayer) SetLinks(ctx context.Context, notePath string, links []LinkInput) error {
	note, err := s.GetNoteByPath(ctx, notePath)
	if err != nil {
		return fmt.Errorf("set links: %w", err)
	}
	if note == nil {
		return fmt.Errorf("note not found: %s", notePath)
	}

	// Resolve all targets BEFORE starting the transaction.
	// With MaxOpenConns=1, calling GetNoteByPath inside a tx would deadlock.
	type resolvedLink struct {
		input    LinkInput
		targetID *int64
	}
	resolved := make([]resolvedLink, len(links))
	for i, link := range links {
		resolved[i].input = link
		target, err := s.GetNoteByPath(ctx, link.TargetPath)
		if err != nil {
			return fmt.Errorf("resolve target %q: %w", link.TargetPath, err)
		}
		if target != nil {
			id := target.ID
			resolved[i].targetID = &id
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// Delete all existing links for this note.
	if _, err := tx.ExecContext(ctx, "DELETE FROM links WHERE source_id = ?", note.ID); err != nil {
		return fmt.Errorf("delete links: %w", err)
	}

	// Insert new links.
	for _, rl := range resolved {
		// Apply defaults.
		linkType := rl.input.Type
		if linkType == "" {
			linkType = "markdown"
		}

		_, err = tx.ExecContext(ctx,
			"INSERT INTO links (source_id, target_path, target_id, title, href, type, snippet) VALUES (?, ?, ?, ?, ?, ?, ?)",
			note.ID, rl.input.TargetPath, rl.targetID, rl.input.Title, rl.input.Href, linkType, rl.input.Snippet,
		)
		if err != nil {
			return fmt.Errorf("insert link to %q: %w", rl.input.TargetPath, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit links: %w", err)
	}
	return nil
}

// GetLinks returns all links from the note at notePath.
// Returns an error if the note is not found.
// Returns a non-nil empty slice if the note has no links.
func (s *StorageLayer) GetLinks(ctx context.Context, notePath string) ([]*LinkRow, error) {
	note, err := s.GetNoteByPath(ctx, notePath)
	if err != nil {
		return nil, fmt.Errorf("get links: %w", err)
	}
	if note == nil {
		return nil, fmt.Errorf("note not found: %s", notePath)
	}

	rows, err := s.db.QueryContext(ctx,
		"SELECT id, source_id, target_path, target_id, title, href, type, snippet FROM links WHERE source_id = ?",
		note.ID,
	)
	if err != nil {
		return nil, fmt.Errorf("query links: %w", err)
	}
	defer rows.Close()

	links := make([]*LinkRow, 0)
	for rows.Next() {
		var l LinkRow
		if err := rows.Scan(&l.ID, &l.SourceID, &l.TargetPath, &l.TargetID, &l.Title, &l.Href, &l.Type, &l.Snippet); err != nil {
			return nil, fmt.Errorf("scan link: %w", err)
		}
		links = append(links, &l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate links: %w", err)
	}
	return links, nil
}

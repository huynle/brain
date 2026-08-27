package storage

import (
	"context"
	"fmt"
	"strings"
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
		if target == nil {
			// Links emitted by the API's own link formatter use the bare
			// short ID ("[Title](n8eox9v4)"); resolve those too so
			// backlinks work for both href styles.
			if shortID := shortIDFromHref(link.TargetPath); shortID != "" {
				target, err = s.GetNoteByShortID(ctx, shortID)
				if err != nil {
					return fmt.Errorf("resolve target %q: %w", link.TargetPath, err)
				}
			}
		}
		if target == nil && link.Type == LinkTypeWiki {
			// Wiki-links are title-addressed by definition, so a title lookup
			// is the right last resort for them. Markdown hrefs deliberately
			// do NOT get this fallback: those name a location (a path or a
			// short ID), and matching them against titles would manufacture
			// links the author never wrote — "[see the plan](plan-id)" in a
			// syntax example would bind to any entry titled "plan-id".
			target, err = s.GetNoteByTitleScoped(ctx, link.TargetPath, note.ProjectID)
			if err != nil {
				return fmt.Errorf("resolve target %q: %w", link.TargetPath, err)
			}
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
			linkType = LinkTypeMarkdown
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

// ResolveLinksTo repairs dangling links that point at the given note by raw
// path or bare short-ID href. Links are resolved inline at index time, so a
// link indexed before its target existed (WalkDir order during a bulk
// reindex, or an entry created after the entries that reference it) stays
// dangling forever without this. Called from InsertNote so every path that
// materializes a note repairs inbound links.
func (s *StorageLayer) ResolveLinksTo(ctx context.Context, noteID int64, path, shortID string) error {
	return s.ResolveLinksToNote(ctx, noteID, path, shortID, "", nil)
}

// ResolveLinksToNote back-fills dangling links that name a note which has just
// appeared, matching on its path, its short ID, or — for wiki-links only — its
// title.
//
// The title arm is scoped: it only claims a dangling wiki-link when the new
// note is global, or when it shares the linking note's project. Titles repeat
// across projects, and without that guard the arrival of a "Summary" in one
// project would silently capture every unresolved [[Summary]] in the brain.
// Only links that nothing else resolved are touched, so a same-project match
// found at write time by SetLinks always wins.
func (s *StorageLayer) ResolveLinksToNote(ctx context.Context, noteID int64, path, shortID, title string, projectID *string) error {
	targets := []interface{}{noteID, path}
	placeholders := "?"
	if shortID != "" {
		targets = append(targets, shortID, shortID+".md")
		placeholders = "?, ?, ?"
	}

	clause := "target_path IN (" + placeholders + ")"
	if title != "" {
		clause += ` OR (
			type = ? AND target_path = ? AND (
				? IS NULL
				OR (SELECT n.project_id FROM notes n WHERE n.id = links.source_id) IS ?
			)
		)`
		targets = append(targets, LinkTypeWiki, title, projectID, projectID)
	}

	_, err := s.db.ExecContext(ctx,
		"UPDATE links SET target_id = ? WHERE target_id IS NULL AND ("+clause+")",
		targets...,
	)
	if err != nil {
		return fmt.Errorf("resolve links to %q: %w", path, err)
	}
	return nil
}

// shortIDFromHref extracts a bare 8-character short ID from a link href
// ("n8eox9v4" or "n8eox9v4.md"). Returns "" for anything else (full
// paths, URLs, anchors).
func shortIDFromHref(href string) string {
	trimmed := strings.TrimSuffix(href, ".md")
	if len(trimmed) != 8 || strings.ContainsAny(trimmed, "/\\#?:") {
		return ""
	}
	for _, r := range trimmed {
		isLower := r >= 'a' && r <= 'z'
		isDigit := r >= '0' && r <= '9'
		if !isLower && !isDigit {
			return ""
		}
	}
	return trimmed
}

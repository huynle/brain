package storage

import (
	"context"
	"fmt"
	"strings"
)

// embeddingIndexFrom scopes each aggregate before it can contribute to note
// freshness. v28 is admitted only by contentScope's local compatibility guard.
func embeddingIndexFrom(scope contentScope) (string, []interface{}) {
	metaWhere, derivedWhere, noteWhere := "", "", "1=1"
	var args []interface{}
	if scope.owner != "" {
		metaWhere = " WHERE tenant_id = ?"
		derivedWhere = " AND ea.tenant_id = ? AND ad.tenant_id = ea.tenant_id"
		noteWhere = "n.tenant_id = ?"
		args = []interface{}{scope.owner, scope.owner, scope.owner}
	}
	return ` FROM notes n
	LEFT JOIN (SELECT note_id, MAX(embedding_indexed_at) AS latest_indexed
		FROM note_embeddings_meta` + metaWhere + ` GROUP BY note_id) m ON m.note_id = n.id
	LEFT JOIN (SELECT ea.note_id, MAX(ad.updated_at) AS latest_ready_derived
		FROM entry_attachments ea JOIN attachment_derived ad ON ad.attachment_id = ea.attachment_id
		WHERE ad.kind = 'text' AND ad.status = 'ready' AND TRIM(ad.text) <> ''` + derivedWhere + `
		GROUP BY ea.note_id) d ON d.note_id = n.id
	WHERE ` + noteWhere, args
}

const embeddingNeedsRefresh = "(m.note_id IS NULL OR n.indexed_at > m.latest_indexed OR d.latest_ready_derived > m.latest_indexed)"

// ListEmbeddingNotes materializes owned candidates before callers perform any
// embedding-provider I/O or forced deletion. No live DB handle escapes storage.
func (s *TenantStore) ListEmbeddingNotes(ctx context.Context, project, path string, force bool) ([]*NoteRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	from, args := embeddingIndexFrom(scope)
	if !force {
		from += " AND " + embeddingNeedsRefresh
	}
	if project != "" {
		from += " AND n.project_id = ?"
		args = append(args, project)
	}
	if path != "" {
		from += " AND n.path LIKE ?"
		args = append(args, path+"%")
	}
	columns := "n." + strings.ReplaceAll(noteColumns, ", ", ", n.")
	rows, err := s.db.QueryContext(ctx, "SELECT "+columns+from, args...)
	if err != nil {
		return nil, fmt.Errorf("query embedding candidates: %w", err)
	}
	defer rows.Close()
	return scanNoteRows(rows)
}

// EmbeddingSource reads the owned body and ready attachment text in one pinned
// transaction. A foreign ID cannot contribute even caller-supplied body text.
func (s *TenantStore) EmbeddingSource(ctx context.Context, noteID int64) (string, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return "", err
	}
	tx, err := beginResilientTx(ctx, s.db, nil)
	if err != nil {
		return "", err
	}
	defer func() { _ = tx.Rollback() }()
	where, args := scope.where("id = ?", noteID)
	var body string
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(body, '') FROM notes WHERE "+where, args...).Scan(&body); err != nil {
		return "", fmt.Errorf("embedding source note not owned: %w", err)
	}
	query := `SELECT ad.text FROM entry_attachments ea
	JOIN attachment_derived ad ON ad.attachment_id = ea.attachment_id
	WHERE ea.note_id = ? AND ad.kind = 'text' AND ad.status = 'ready' AND TRIM(ad.text) <> ''`
	args = []interface{}{noteID}
	if scope.owner != "" {
		query += " AND ea.tenant_id = ? AND ad.tenant_id = ea.tenant_id"
		args = append(args, scope.owner)
	}
	rows, err := tx.QueryContext(ctx, query+" ORDER BY ea.id, ad.id", args...)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	var texts []string
	for rows.Next() {
		var text string
		if err := rows.Scan(&text); err != nil {
			return "", err
		}
		if text = strings.TrimSpace(text); text != "" {
			texts = append(texts, text)
		}
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	if len(texts) == 0 {
		return body, nil
	}
	var out strings.Builder
	if strings.TrimSpace(body) != "" {
		out.WriteString(body)
		out.WriteString("\n\n")
	}
	out.WriteString("Attachments:\n")
	out.WriteString(strings.Join(texts, "\n\n"))
	return out.String(), nil
}

// EmbeddingHealthCounts reports only the bound tenant's note and embedding
// population, including staleness from owned ready attachment-derived text.
func (s *TenantStore) EmbeddingHealthCounts(ctx context.Context) (total, with, stale int, err error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return 0, 0, 0, err
	}
	from, args := embeddingIndexFrom(scope)
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*), COUNT(m.note_id),
	COALESCE(SUM(CASE WHEN m.note_id IS NOT NULL AND
	(n.indexed_at > m.latest_indexed OR d.latest_ready_derived > m.latest_indexed)
	THEN 1 ELSE 0 END), 0)`+from, args...).Scan(&total, &with, &stale)
	return
}

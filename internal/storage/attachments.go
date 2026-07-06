package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const attachmentColumns = `id, digest, size, media_type, metadata, created_at`
const attachmentDerivedColumns = `id, attachment_id, kind, status, content_type, text, error, metadata, created_at, updated_at`

func (s *StorageLayer) CreateAttachment(ctx context.Context, in AttachmentInput) (*AttachmentRow, error) {
	if err := validateAttachmentInput(in); err != nil {
		return nil, err
	}
	metadata := in.Metadata
	if metadata == "" {
		metadata = "{}"
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO attachments (digest, size, media_type, metadata)
		VALUES (?, ?, ?, ?)
	`, strings.TrimSpace(in.Digest), in.Size, in.MediaType, metadata)
	if err != nil {
		return nil, fmt.Errorf("create attachment: %w", err)
	}

	row, err := s.GetAttachmentByDigest(ctx, strings.TrimSpace(in.Digest))
	if err != nil {
		return nil, fmt.Errorf("read attachment by digest: %w", err)
	}
	return row, nil
}

func (s *StorageLayer) GetAttachment(ctx context.Context, id int64) (*AttachmentRow, error) {
	if id <= 0 {
		return nil, errors.New("attachment id must be positive")
	}
	row := s.db.QueryRowContext(ctx, "SELECT "+attachmentColumns+" FROM attachments WHERE id = ?", id)
	att, err := scanAttachmentRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get attachment: %w", err)
	}
	return att, nil
}

func (s *StorageLayer) GetAttachmentByDigest(ctx context.Context, digest string) (*AttachmentRow, error) {
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return nil, errors.New("attachment digest must not be empty")
	}
	row := s.db.QueryRowContext(ctx, "SELECT "+attachmentColumns+" FROM attachments WHERE digest = ?", digest)
	att, err := scanAttachmentRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get attachment by digest: %w", err)
	}
	return att, nil
}

func (s *StorageLayer) ListAttachments(ctx context.Context) ([]*AttachmentRow, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT "+attachmentColumns+" FROM attachments ORDER BY id")
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	defer rows.Close()
	return scanAttachmentRows(rows)
}

func (s *StorageLayer) LinkAttachmentToEntry(ctx context.Context, notePath string, attachmentID int64, role string) error {
	role = strings.TrimSpace(role)
	if err := validateReferenceInput(notePath, attachmentID, role); err != nil {
		return err
	}
	note, err := s.GetNoteByPath(ctx, notePath)
	if err != nil {
		return fmt.Errorf("link attachment: %w", err)
	}
	if note == nil {
		return fmt.Errorf("note not found: %s", notePath)
	}
	att, err := s.GetAttachment(ctx, attachmentID)
	if err != nil {
		return fmt.Errorf("link attachment: %w", err)
	}
	if att == nil {
		return fmt.Errorf("attachment not found: %d", attachmentID)
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO entry_attachments (note_id, attachment_id, role)
		VALUES (?, ?, ?)
	`, note.ID, attachmentID, role)
	if err != nil {
		return fmt.Errorf("insert entry attachment: %w", err)
	}
	return nil
}

func (s *StorageLayer) UnlinkAttachmentFromEntry(ctx context.Context, notePath string, attachmentID int64, role string) (bool, error) {
	role = strings.TrimSpace(role)
	if err := validateReferenceInput(notePath, attachmentID, role); err != nil {
		return false, err
	}
	note, err := s.GetNoteByPath(ctx, notePath)
	if err != nil {
		return false, fmt.Errorf("unlink attachment: %w", err)
	}
	if note == nil {
		return false, fmt.Errorf("note not found: %s", notePath)
	}
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM entry_attachments
		WHERE note_id = ? AND attachment_id = ? AND role = ?
	`, note.ID, attachmentID, role)
	if err != nil {
		return false, fmt.Errorf("unlink attachment: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return rowsAffected > 0, nil
}

func (s *StorageLayer) ListAttachmentsForEntry(ctx context.Context, notePath string) ([]*AttachmentRow, error) {
	if strings.TrimSpace(notePath) == "" {
		return nil, errors.New("note path must not be empty")
	}
	note, err := s.GetNoteByPath(ctx, notePath)
	if err != nil {
		return nil, fmt.Errorf("list entry attachments: %w", err)
	}
	if note == nil {
		return nil, fmt.Errorf("note not found: %s", notePath)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.digest, a.size, a.media_type, a.metadata, a.created_at
		FROM attachments a
		JOIN entry_attachments ea ON ea.attachment_id = a.id
		WHERE ea.note_id = ?
		ORDER BY ea.id
	`, note.ID)
	if err != nil {
		return nil, fmt.Errorf("query entry attachments: %w", err)
	}
	defer rows.Close()
	return scanAttachmentRows(rows)
}

func (s *StorageLayer) ListEntryReferencesForAttachment(ctx context.Context, attachmentID int64) ([]*EntryAttachmentRow, error) {
	if attachmentID <= 0 {
		return nil, errors.New("attachment id must be positive")
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT ea.id, ea.note_id, n.path, ea.attachment_id, ea.role, ea.created_at
		FROM entry_attachments ea
		JOIN notes n ON n.id = ea.note_id
		WHERE ea.attachment_id = ?
		ORDER BY ea.id
	`, attachmentID)
	if err != nil {
		return nil, fmt.Errorf("list attachment references: %w", err)
	}
	defer rows.Close()

	refs := make([]*EntryAttachmentRow, 0)
	for rows.Next() {
		var ref EntryAttachmentRow
		if err := rows.Scan(&ref.ID, &ref.NoteID, &ref.NotePath, &ref.AttachmentID, &ref.Role, &ref.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan attachment reference: %w", err)
		}
		refs = append(refs, &ref)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attachment references: %w", err)
	}
	return refs, nil
}

func (s *StorageLayer) CountAttachmentReferences(ctx context.Context, attachmentID int64) (int, error) {
	if attachmentID <= 0 {
		return 0, errors.New("attachment id must be positive")
	}
	var count int
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM entry_attachments WHERE attachment_id = ?", attachmentID).Scan(&count); err != nil {
		return 0, fmt.Errorf("count attachment references: %w", err)
	}
	return count, nil
}

func (s *StorageLayer) UpsertAttachmentDerived(ctx context.Context, in AttachmentDerivedInput) (*AttachmentDerivedRow, error) {
	if err := validateAttachmentDerivedInput(in); err != nil {
		return nil, err
	}
	metadata := in.Metadata
	if metadata == "" {
		metadata = "{}"
	}
	kind := strings.TrimSpace(in.Kind)
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO attachment_derived (attachment_id, kind, status, content_type, text, error, metadata)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(attachment_id, kind) DO UPDATE SET
			status = excluded.status,
			content_type = excluded.content_type,
			text = excluded.text,
			error = excluded.error,
			metadata = excluded.metadata,
			updated_at = datetime('now')
	`, in.AttachmentID, kind, strings.TrimSpace(in.Status), strings.TrimSpace(in.ContentType), in.Text, in.Error, metadata)
	if err != nil {
		return nil, fmt.Errorf("upsert attachment derived: %w", err)
	}
	row, err := s.GetAttachmentDerived(ctx, in.AttachmentID, kind)
	if err != nil {
		return nil, fmt.Errorf("read attachment derived: %w", err)
	}
	return row, nil
}

func (s *StorageLayer) GetAttachmentDerived(ctx context.Context, attachmentID int64, kind string) (*AttachmentDerivedRow, error) {
	if attachmentID <= 0 {
		return nil, errors.New("attachment id must be positive")
	}
	kind = strings.TrimSpace(kind)
	if !isSafeAttachmentRole(kind) {
		return nil, fmt.Errorf("attachment derived kind %q is unsafe", kind)
	}
	row := s.db.QueryRowContext(ctx, "SELECT "+attachmentDerivedColumns+" FROM attachment_derived WHERE attachment_id = ? AND kind = ?", attachmentID, kind)
	derived, err := scanAttachmentDerivedRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get attachment derived: %w", err)
	}
	return derived, nil
}

func (s *StorageLayer) ListAttachmentDerived(ctx context.Context, attachmentID int64) ([]*AttachmentDerivedRow, error) {
	if attachmentID <= 0 {
		return nil, errors.New("attachment id must be positive")
	}
	rows, err := s.db.QueryContext(ctx, "SELECT "+attachmentDerivedColumns+" FROM attachment_derived WHERE attachment_id = ? ORDER BY id", attachmentID)
	if err != nil {
		return nil, fmt.Errorf("list attachment derived: %w", err)
	}
	defer rows.Close()
	return scanAttachmentDerivedRows(rows)
}

func (s *StorageLayer) DeleteAttachmentIfUnreferenced(ctx context.Context, attachmentID int64) (bool, error) {
	if attachmentID <= 0 {
		return false, errors.New("attachment id must be positive")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin delete attachment tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	var count int
	if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM entry_attachments WHERE attachment_id = ?", attachmentID).Scan(&count); err != nil {
		return false, fmt.Errorf("count attachment references: %w", err)
	}
	if count > 0 {
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("commit delete attachment tx: %w", err)
		}
		return false, nil
	}
	res, err := tx.ExecContext(ctx, "DELETE FROM attachments WHERE id = ?", attachmentID)
	if err != nil {
		return false, fmt.Errorf("delete attachment: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit delete attachment tx: %w", err)
	}
	return rowsAffected > 0, nil
}

func validateAttachmentInput(in AttachmentInput) error {
	if strings.TrimSpace(in.Digest) == "" {
		return errors.New("attachment digest must not be empty")
	}
	if in.Size < 0 {
		return errors.New("attachment size must be non-negative")
	}
	metadata := in.Metadata
	if metadata == "" {
		metadata = "{}"
	}
	if !json.Valid([]byte(metadata)) {
		return errors.New("attachment metadata must be valid JSON")
	}
	return nil
}

func validateReferenceInput(notePath string, attachmentID int64, role string) error {
	if strings.TrimSpace(notePath) == "" {
		return errors.New("note path must not be empty")
	}
	if attachmentID <= 0 {
		return errors.New("attachment id must be positive")
	}
	if !isSafeAttachmentRole(role) {
		return fmt.Errorf("attachment role %q is unsafe", role)
	}
	return nil
}

func validateAttachmentDerivedInput(in AttachmentDerivedInput) error {
	if in.AttachmentID <= 0 {
		return errors.New("attachment id must be positive")
	}
	if !isSafeAttachmentRole(strings.TrimSpace(in.Kind)) {
		return fmt.Errorf("attachment derived kind %q is unsafe", in.Kind)
	}
	if strings.TrimSpace(in.Status) == "" {
		return errors.New("attachment derived status must not be empty")
	}
	metadata := in.Metadata
	if metadata == "" {
		metadata = "{}"
	}
	if !json.Valid([]byte(metadata)) {
		return errors.New("attachment derived metadata must be valid JSON")
	}
	return nil
}

func isSafeAttachmentRole(role string) bool {
	if role == "" {
		return false
	}
	for _, r := range role {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}

func scanAttachmentRow(row *sql.Row) (*AttachmentRow, error) {
	var att AttachmentRow
	if err := row.Scan(&att.ID, &att.Digest, &att.Size, &att.MediaType, &att.Metadata, &att.CreatedAt); err != nil {
		return nil, err
	}
	return &att, nil
}

func scanAttachmentRows(rows *sql.Rows) ([]*AttachmentRow, error) {
	attachments := make([]*AttachmentRow, 0)
	for rows.Next() {
		var att AttachmentRow
		if err := rows.Scan(&att.ID, &att.Digest, &att.Size, &att.MediaType, &att.Metadata, &att.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan attachment: %w", err)
		}
		attachments = append(attachments, &att)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attachments: %w", err)
	}
	return attachments, nil
}

func scanAttachmentDerivedRow(row *sql.Row) (*AttachmentDerivedRow, error) {
	var derived AttachmentDerivedRow
	if err := row.Scan(&derived.ID, &derived.AttachmentID, &derived.Kind, &derived.Status, &derived.ContentType, &derived.Text, &derived.Error, &derived.Metadata, &derived.CreatedAt, &derived.UpdatedAt); err != nil {
		return nil, err
	}
	return &derived, nil
}

func scanAttachmentDerivedRows(rows *sql.Rows) ([]*AttachmentDerivedRow, error) {
	derivedRows := make([]*AttachmentDerivedRow, 0)
	for rows.Next() {
		var derived AttachmentDerivedRow
		if err := rows.Scan(&derived.ID, &derived.AttachmentID, &derived.Kind, &derived.Status, &derived.ContentType, &derived.Text, &derived.Error, &derived.Metadata, &derived.CreatedAt, &derived.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan attachment derived: %w", err)
		}
		derivedRows = append(derivedRows, &derived)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate attachment derived: %w", err)
	}
	return derivedRows, nil
}

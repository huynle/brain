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

func (s *TenantStore) CreateAttachment(ctx context.Context, in AttachmentInput) (*AttachmentRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateAttachmentInput(in); err != nil {
		return nil, err
	}
	metadata := in.Metadata
	if metadata == "" {
		metadata = "{}"
	}

	columns, values, conflict := "digest, size, media_type, metadata", "?, ?, ?, ?", "digest"
	args := []interface{}{strings.TrimSpace(in.Digest), in.Size, in.MediaType, metadata}
	if scope.owner != "" {
		columns += ", tenant_id"
		values += ", ?"
		conflict = "tenant_id, digest"
		args = append(args, scope.owner)
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO attachments ("+columns+") VALUES ("+values+") ON CONFLICT("+conflict+") DO NOTHING", args...)
	if err != nil {
		return nil, fmt.Errorf("create attachment: %w", err)
	}

	row, err := s.GetAttachmentByDigest(ctx, strings.TrimSpace(in.Digest))
	if err != nil {
		return nil, fmt.Errorf("read attachment by digest: %w", err)
	}
	return row, nil
}

func (s *TenantStore) GetAttachment(ctx context.Context, id int64) (*AttachmentRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	if id <= 0 {
		return nil, errors.New("attachment id must be positive")
	}
	where, args := scope.where("id = ?", id)
	row := s.db.QueryRowContext(ctx, "SELECT "+attachmentColumns+" FROM attachments WHERE "+where, args...)
	att, err := scanAttachmentRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get attachment: %w", err)
	}
	return att, nil
}

func (s *TenantStore) GetAttachmentByDigest(ctx context.Context, digest string) (*AttachmentRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	digest = strings.TrimSpace(digest)
	if digest == "" {
		return nil, errors.New("attachment digest must not be empty")
	}
	where, args := scope.where("digest = ?", digest)
	row := s.db.QueryRowContext(ctx, "SELECT "+attachmentColumns+" FROM attachments WHERE "+where, args...)
	att, err := scanAttachmentRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get attachment by digest: %w", err)
	}
	return att, nil
}

// ListAttachments selects the bound tenant's shared attachment catalog in SQL.
// Projects are not an attachment ACL: metadata.project_id is upload provenance.
func (s *TenantStore) ListAttachments(ctx context.Context) ([]*AttachmentRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	where, args := scope.where("1=1")
	rows, err := s.db.QueryContext(ctx, "SELECT "+attachmentColumns+" FROM attachments WHERE "+where+" ORDER BY id", args...)
	if err != nil {
		return nil, fmt.Errorf("list attachments: %w", err)
	}
	defer rows.Close()
	return scanAttachmentRows(rows)
}

func (s *TenantStore) LinkAttachmentToEntry(ctx context.Context, notePath string, attachmentID int64, role string) error {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return err
	}
	role = strings.TrimSpace(role)
	if err := validateReferenceInput(notePath, attachmentID, role); err != nil {
		return err
	}
	// Parent selection and insertion are one SQLite write statement: deletion
	// cannot slip between a successful parent check and the reference write.
	columns, selection, conflict := "note_id, attachment_id, role", "n.id, a.id, ?", "note_id, attachment_id, role"
	where := "n.path = ? AND a.id = ?"
	args := []interface{}{role, notePath, attachmentID}
	if scope.owner != "" {
		columns += ", tenant_id"
		selection += ", n.tenant_id"
		where += " AND n.tenant_id = ? AND a.tenant_id = n.tenant_id"
		conflict = "tenant_id, " + conflict
		args = append(args, scope.owner)
	}
	res, err := s.db.ExecContext(ctx, "INSERT INTO entry_attachments ("+columns+") SELECT "+selection+" FROM notes n CROSS JOIN attachments a WHERE "+where+" ON CONFLICT("+conflict+") DO UPDATE SET role = excluded.role", args...)
	if err != nil {
		return fmt.Errorf("insert entry attachment: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("note or attachment not found: %s, %d", notePath, attachmentID)
	}
	return nil
}

func (s *TenantStore) UnlinkAttachmentFromEntry(ctx context.Context, notePath string, attachmentID int64, role string) (bool, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return false, err
	}
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
	where, args := scope.where("note_id = ? AND attachment_id = ? AND role = ?", note.ID, attachmentID, role)
	res, err := s.db.ExecContext(ctx, "DELETE FROM entry_attachments WHERE "+where, args...)
	if err != nil {
		return false, fmt.Errorf("unlink attachment: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
	}
	return rowsAffected > 0, nil
}

func (s *TenantStore) ListAttachmentsForEntry(ctx context.Context, notePath string) ([]*AttachmentRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
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
	where := "ea.note_id = ?"
	args := []interface{}{note.ID}
	if scope.owner != "" {
		where += " AND ea.tenant_id = ? AND a.tenant_id = ea.tenant_id"
		args = append(args, scope.owner)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT a.id, a.digest, a.size, a.media_type, a.metadata, a.created_at
		FROM attachments a
		JOIN entry_attachments ea ON ea.attachment_id = a.id
		WHERE `+where+` ORDER BY ea.id`, args...)
	if err != nil {
		return nil, fmt.Errorf("query entry attachments: %w", err)
	}
	defer rows.Close()
	return scanAttachmentRows(rows)
}

func (s *TenantStore) ListEntryReferencesForAttachment(ctx context.Context, attachmentID int64) ([]*EntryAttachmentRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	if attachmentID <= 0 {
		return nil, errors.New("attachment id must be positive")
	}
	where := "ea.attachment_id = ?"
	args := []interface{}{attachmentID}
	if scope.owner != "" {
		where += " AND ea.tenant_id = ? AND n.tenant_id = ea.tenant_id"
		args = append(args, scope.owner)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT ea.id, ea.note_id, n.path, ea.attachment_id, ea.role, ea.created_at
		FROM entry_attachments ea
		JOIN notes n ON n.id = ea.note_id
		WHERE `+where+` ORDER BY ea.id`, args...)
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

func (s *TenantStore) CountAttachmentReferences(ctx context.Context, attachmentID int64) (int, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return 0, err
	}
	if attachmentID <= 0 {
		return 0, errors.New("attachment id must be positive")
	}
	var count int
	where, args := scope.where("attachment_id = ?", attachmentID)
	if err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM entry_attachments WHERE "+where, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("count attachment references: %w", err)
	}
	return count, nil
}

func (s *TenantStore) UpsertAttachmentDerived(ctx context.Context, in AttachmentDerivedInput) (*AttachmentDerivedRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	if err := validateAttachmentDerivedInput(in); err != nil {
		return nil, err
	}
	metadata := in.Metadata
	if metadata == "" {
		metadata = "{}"
	}
	kind := strings.TrimSpace(in.Kind)
	columns := "attachment_id, kind, status, content_type, text, error, metadata"
	selection, conflict := "id, ?, ?, ?, ?, ?, ?", "attachment_id, kind"
	args := []interface{}{kind, strings.TrimSpace(in.Status), strings.TrimSpace(in.ContentType), in.Text, in.Error, metadata}
	where, parentArgs := scope.where("id = ?", in.AttachmentID)
	args = append(args, parentArgs...)
	if scope.owner != "" {
		columns += ", tenant_id"
		selection += ", tenant_id"
		conflict = "tenant_id, " + conflict
	}
	res, err := s.db.ExecContext(ctx, "INSERT INTO attachment_derived ("+columns+") SELECT "+selection+" FROM attachments WHERE "+where+" ON CONFLICT("+conflict+`) DO UPDATE SET
			status = excluded.status,
			content_type = excluded.content_type,
			text = excluded.text,
			error = excluded.error,
			metadata = excluded.metadata,
			updated_at = datetime('now')
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("upsert attachment derived: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, fmt.Errorf("attachment not found: %d", in.AttachmentID)
	}
	row, err := s.GetAttachmentDerived(ctx, in.AttachmentID, kind)
	if err != nil {
		return nil, fmt.Errorf("read attachment derived: %w", err)
	}
	return row, nil
}

func (s *TenantStore) GetAttachmentDerived(ctx context.Context, attachmentID int64, kind string) (*AttachmentDerivedRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	if attachmentID <= 0 {
		return nil, errors.New("attachment id must be positive")
	}
	kind = strings.TrimSpace(kind)
	if !isSafeAttachmentRole(kind) {
		return nil, fmt.Errorf("attachment derived kind %q is unsafe", kind)
	}
	where, args := scope.where("attachment_id = ? AND kind = ?", attachmentID, kind)
	row := s.db.QueryRowContext(ctx, "SELECT "+attachmentDerivedColumns+" FROM attachment_derived WHERE "+where, args...)
	derived, err := scanAttachmentDerivedRow(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get attachment derived: %w", err)
	}
	return derived, nil
}

func (s *TenantStore) ListAttachmentDerived(ctx context.Context, attachmentID int64) ([]*AttachmentDerivedRow, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return nil, err
	}
	if attachmentID <= 0 {
		return nil, errors.New("attachment id must be positive")
	}
	where, args := scope.where("attachment_id = ?", attachmentID)
	rows, err := s.db.QueryContext(ctx, "SELECT "+attachmentDerivedColumns+" FROM attachment_derived WHERE "+where+" ORDER BY id", args...)
	if err != nil {
		return nil, fmt.Errorf("list attachment derived: %w", err)
	}
	defer rows.Close()
	return scanAttachmentDerivedRows(rows)
}

func (s *TenantStore) DeleteAttachmentIfUnreferenced(ctx context.Context, attachmentID int64) (bool, error) {
	scope, err := s.contentScope(ctx)
	if err != nil {
		return false, err
	}
	if attachmentID <= 0 {
		return false, errors.New("attachment id must be positive")
	}
	where, args := scope.where("id = ?", attachmentID)
	refs := "ea.attachment_id = attachments.id"
	if scope.owner != "" {
		refs += " AND ea.tenant_id = attachments.tenant_id"
	}
	// One conditional write serializes against LinkAttachmentToEntry; never
	// race a separate reference count against a later delete.
	res, err := s.db.ExecContext(ctx, "DELETE FROM attachments WHERE "+where+" AND NOT EXISTS (SELECT 1 FROM entry_attachments ea WHERE "+refs+")", args...)
	if err != nil {
		return false, fmt.Errorf("delete attachment: %w", err)
	}
	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("rows affected: %w", err)
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

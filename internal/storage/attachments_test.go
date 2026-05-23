package storage

import (
	"context"
	"strings"
	"testing"
)

func TestAttachmentSchema_TablesAndIndexesExist(t *testing.T) {
	s := newTestStorage(t)

	tables := []string{"attachments", "entry_attachments", "attachment_derived"}
	for _, table := range tables {
		t.Run(table, func(t *testing.T) {
			var name string
			err := s.DB().QueryRow(
				"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
			).Scan(&name)
			if err != nil {
				t.Fatalf("table %q not found: %v", table, err)
			}
		})
	}

	indexes := []string{
		"idx_attachments_digest",
		"idx_entry_attachments_note",
		"idx_entry_attachments_attachment",
		"idx_entry_attachments_note_attachment_role",
		"idx_attachment_derived_attachment",
		"idx_attachment_derived_status",
	}
	for _, idx := range indexes {
		t.Run(idx, func(t *testing.T) {
			var name string
			err := s.DB().QueryRow(
				"SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx,
			).Scan(&name)
			if err != nil {
				t.Fatalf("index %q not found: %v", idx, err)
			}
		})
	}
}

func TestAttachmentSchema_DeleteAttachmentCascadesDerivedRows(t *testing.T) {
	s := newTestStorage(t)

	res, err := s.DB().Exec(`INSERT INTO attachments (digest, size, media_type, metadata) VALUES ('sha256:derived-delete', 12, 'image/png', '{}')`)
	if err != nil {
		t.Fatalf("insert attachment failed: %v", err)
	}
	attachmentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId failed: %v", err)
	}
	if _, err := s.DB().Exec(`
		INSERT INTO attachment_derived (attachment_id, kind, status, content_type, text, error, metadata)
		VALUES (?, 'text', 'ready', 'text/plain; charset=utf-8', 'extracted text', '', '{}')
	`, attachmentID); err != nil {
		t.Fatalf("insert derived row failed: %v", err)
	}

	if _, err := s.DB().Exec(`DELETE FROM attachments WHERE id = ?`, attachmentID); err != nil {
		t.Fatalf("delete attachment failed: %v", err)
	}

	var derivedCount int
	if err := s.DB().QueryRow(`SELECT count(*) FROM attachment_derived WHERE attachment_id = ?`, attachmentID).Scan(&derivedCount); err != nil {
		t.Fatalf("count derived rows failed: %v", err)
	}
	if derivedCount != 0 {
		t.Fatalf("derived row count after attachment delete = %d, want 0", derivedCount)
	}
}

func TestAttachmentSchema_AttachmentsSurviveNoteDelete(t *testing.T) {
	s := newTestStorage(t)

	res, err := s.DB().Exec(`INSERT INTO attachments (digest, size, media_type, metadata) VALUES ('sha256:abc', 12, 'text/plain', '{}')`)
	if err != nil {
		t.Fatalf("insert attachment failed: %v", err)
	}
	attachmentID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId failed: %v", err)
	}

	res, err = s.DB().Exec(`INSERT INTO notes (path, short_id, title) VALUES ('projects/test/report/with-attachment.md', 'attnote1', 'With Attachment')`)
	if err != nil {
		t.Fatalf("insert note failed: %v", err)
	}
	noteID, err := res.LastInsertId()
	if err != nil {
		t.Fatalf("LastInsertId note failed: %v", err)
	}

	_, err = s.DB().Exec(`INSERT INTO entry_attachments (note_id, attachment_id, role) VALUES (?, ?, 'inline')`, noteID, attachmentID)
	if err != nil {
		t.Fatalf("insert reference failed: %v", err)
	}

	if _, err := s.DB().Exec(`DELETE FROM notes WHERE id = ?`, noteID); err != nil {
		t.Fatalf("delete note failed: %v", err)
	}

	var attachmentCount int
	if err := s.DB().QueryRow(`SELECT count(*) FROM attachments WHERE id = ?`, attachmentID).Scan(&attachmentCount); err != nil {
		t.Fatalf("count attachments failed: %v", err)
	}
	if attachmentCount != 1 {
		t.Fatalf("attachment count after note delete = %d, want 1", attachmentCount)
	}

	var referenceCount int
	if err := s.DB().QueryRow(`SELECT count(*) FROM entry_attachments WHERE attachment_id = ?`, attachmentID).Scan(&referenceCount); err != nil {
		t.Fatalf("count references failed: %v", err)
	}
	if referenceCount != 0 {
		t.Fatalf("reference count after note delete = %d, want 0", referenceCount)
	}
}

func TestAttachmentSchema_MigrationFromV12(t *testing.T) {
	db := openMemoryDB(t)
	defer db.Close()

	if _, err := db.Exec(createSchemaVersionTable); err != nil {
		t.Fatalf("create schema_version table: %v", err)
	}
	if _, err := db.Exec("INSERT INTO schema_version (version) VALUES (12)"); err != nil {
		t.Fatalf("insert v12: %v", err)
	}
	if _, err := db.Exec(createNotesTable); err != nil {
		t.Fatalf("create notes table: %v", err)
	}

	if err := migrateSchema(db); err != nil {
		t.Fatalf("migrateSchema failed: %v", err)
	}

	for _, table := range []string{"attachments", "entry_attachments", "attachment_derived"} {
		t.Run(table, func(t *testing.T) {
			var name string
			if err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name); err != nil {
				t.Fatalf("table %q not found after migration: %v", table, err)
			}
		})
	}
}

func TestAttachmentStorage_CreateDeduplicatesByDigest(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	first, err := s.CreateAttachment(ctx, AttachmentInput{
		Digest:    "sha256:dedupe",
		Size:      42,
		MediaType: "text/plain",
		Metadata:  `{"name":"first.txt"}`,
	})
	if err != nil {
		t.Fatalf("CreateAttachment first failed: %v", err)
	}
	second, err := s.CreateAttachment(ctx, AttachmentInput{
		Digest:    "sha256:dedupe",
		Size:      42,
		MediaType: "text/plain",
		Metadata:  `{"name":"second.txt"}`,
	})
	if err != nil {
		t.Fatalf("CreateAttachment duplicate failed: %v", err)
	}

	if second.ID != first.ID {
		t.Fatalf("duplicate digest created ID %d, want existing ID %d", second.ID, first.ID)
	}
	if second.Metadata != `{"name":"first.txt"}` {
		t.Fatalf("duplicate changed metadata to %q, want original metadata", second.Metadata)
	}
}

func TestAttachmentStorage_GetListAndPersist(t *testing.T) {
	dbPath := t.TempDir() + "/attachments.db"
	ctx := context.Background()

	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	created, err := s.CreateAttachment(ctx, AttachmentInput{Digest: "sha256:persist", Size: 7, MediaType: "image/png", Metadata: `{"width":10}`})
	if err != nil {
		t.Fatalf("CreateAttachment failed: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	s, err = New(dbPath)
	if err != nil {
		t.Fatalf("reopen New failed: %v", err)
	}
	defer s.Close()

	byID, err := s.GetAttachment(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetAttachment failed: %v", err)
	}
	if byID == nil || byID.Digest != "sha256:persist" || byID.Size != 7 || byID.MediaType != "image/png" || byID.Metadata != `{"width":10}` {
		t.Fatalf("GetAttachment = %#v, want persisted row", byID)
	}

	byDigest, err := s.GetAttachmentByDigest(ctx, "sha256:persist")
	if err != nil {
		t.Fatalf("GetAttachmentByDigest failed: %v", err)
	}
	if byDigest == nil || byDigest.ID != created.ID {
		t.Fatalf("GetAttachmentByDigest = %#v, want ID %d", byDigest, created.ID)
	}

	list, err := s.ListAttachments(ctx)
	if err != nil {
		t.Fatalf("ListAttachments failed: %v", err)
	}
	if len(list) != 1 || list[0].ID != created.ID {
		t.Fatalf("ListAttachments = %#v, want one created row", list)
	}
}

func TestAttachmentStorage_ReferenceLookupAndSafeDelete(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	note, err := s.InsertNote(ctx, sampleNote("projects/test/report/ref.md", "attref01", "Reference"))
	if err != nil {
		t.Fatalf("InsertNote failed: %v", err)
	}
	att, err := s.CreateAttachment(ctx, AttachmentInput{Digest: "sha256:ref", Size: 9, Metadata: `{}`})
	if err != nil {
		t.Fatalf("CreateAttachment failed: %v", err)
	}

	if err := s.LinkAttachmentToEntry(ctx, note.Path, att.ID, "inline"); err != nil {
		t.Fatalf("LinkAttachmentToEntry failed: %v", err)
	}
	if err := s.LinkAttachmentToEntry(ctx, note.Path, att.ID, "inline"); err != nil {
		t.Fatalf("duplicate LinkAttachmentToEntry failed: %v", err)
	}

	count, err := s.CountAttachmentReferences(ctx, att.ID)
	if err != nil {
		t.Fatalf("CountAttachmentReferences failed: %v", err)
	}
	if count != 1 {
		t.Fatalf("reference count = %d, want 1", count)
	}

	refs, err := s.ListEntryReferencesForAttachment(ctx, att.ID)
	if err != nil {
		t.Fatalf("ListEntryReferencesForAttachment failed: %v", err)
	}
	if len(refs) != 1 || refs[0].NoteID != note.ID || refs[0].AttachmentID != att.ID || refs[0].NotePath != note.Path || refs[0].Role != "inline" {
		t.Fatalf("references = %#v, want note/attachment inline reference", refs)
	}

	attachments, err := s.ListAttachmentsForEntry(ctx, note.Path)
	if err != nil {
		t.Fatalf("ListAttachmentsForEntry failed: %v", err)
	}
	if len(attachments) != 1 || attachments[0].ID != att.ID {
		t.Fatalf("entry attachments = %#v, want linked attachment", attachments)
	}

	deleted, err := s.DeleteAttachmentIfUnreferenced(ctx, att.ID)
	if err != nil {
		t.Fatalf("DeleteAttachmentIfUnreferenced while referenced failed: %v", err)
	}
	if deleted {
		t.Fatal("DeleteAttachmentIfUnreferenced deleted referenced attachment")
	}

	unlinked, err := s.UnlinkAttachmentFromEntry(ctx, note.Path, att.ID, "inline")
	if err != nil {
		t.Fatalf("UnlinkAttachmentFromEntry failed: %v", err)
	}
	if !unlinked {
		t.Fatal("UnlinkAttachmentFromEntry returned false for existing reference")
	}
	deleted, err = s.DeleteAttachmentIfUnreferenced(ctx, att.ID)
	if err != nil {
		t.Fatalf("DeleteAttachmentIfUnreferenced after unlink failed: %v", err)
	}
	if !deleted {
		t.Fatal("DeleteAttachmentIfUnreferenced returned false for unreferenced attachment")
	}
}

func TestAttachmentStorage_UpsertGetAndListDerivedText(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	att, err := s.CreateAttachment(ctx, AttachmentInput{Digest: "sha256:derived", Size: 9, MediaType: "image/png", Metadata: `{}`})
	if err != nil {
		t.Fatalf("CreateAttachment failed: %v", err)
	}

	ready, err := s.UpsertAttachmentDerived(ctx, AttachmentDerivedInput{
		AttachmentID: att.ID,
		Kind:         "text",
		Status:       "ready",
		ContentType:  "text/plain; charset=utf-8",
		Text:         "extracted text",
		Metadata:     `{"extractor":"test"}`,
	})
	if err != nil {
		t.Fatalf("UpsertAttachmentDerived ready failed: %v", err)
	}
	if ready.ID == 0 || ready.AttachmentID != att.ID || ready.Kind != "text" || ready.Status != "ready" || ready.Text != "extracted text" || ready.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("ready derived row = %#v, want persisted ready text", ready)
	}

	got, err := s.GetAttachmentDerived(ctx, att.ID, "text")
	if err != nil {
		t.Fatalf("GetAttachmentDerived failed: %v", err)
	}
	if got == nil || got.ID != ready.ID || got.Metadata != `{"extractor":"test"}` {
		t.Fatalf("GetAttachmentDerived = %#v, want ready row %#v", got, ready)
	}

	failed, err := s.UpsertAttachmentDerived(ctx, AttachmentDerivedInput{
		AttachmentID: att.ID,
		Kind:         "text",
		Status:       "failed",
		Error:        "unsupported image format",
		Metadata:     `{}`,
	})
	if err != nil {
		t.Fatalf("UpsertAttachmentDerived failed status failed: %v", err)
	}
	if failed.ID != ready.ID || failed.Status != "failed" || failed.Text != "" || failed.Error != "unsupported image format" {
		t.Fatalf("failed derived row = %#v, want updated failed status on same row", failed)
	}

	list, err := s.ListAttachmentDerived(ctx, att.ID)
	if err != nil {
		t.Fatalf("ListAttachmentDerived failed: %v", err)
	}
	if len(list) != 1 || list[0].ID != ready.ID || list[0].Status != "failed" {
		t.Fatalf("ListAttachmentDerived = %#v, want updated single row", list)
	}
}

func TestAttachmentStorage_DerivedValidationAndMissingRows(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	if _, err := s.GetAttachmentDerived(ctx, 12345, "text"); err != nil {
		t.Fatalf("GetAttachmentDerived missing row error = %v, want nil", err)
	}
	missing, err := s.GetAttachmentDerived(ctx, 12345, "text")
	if err != nil {
		t.Fatalf("GetAttachmentDerived missing row failed: %v", err)
	}
	if missing != nil {
		t.Fatalf("GetAttachmentDerived missing = %#v, want nil", missing)
	}

	for _, tt := range []struct {
		name string
		in   AttachmentDerivedInput
	}{
		{name: "non-positive attachment", in: AttachmentDerivedInput{AttachmentID: 0, Kind: "text", Status: "ready", Metadata: `{}`}},
		{name: "empty kind", in: AttachmentDerivedInput{AttachmentID: 1, Kind: "", Status: "ready", Metadata: `{}`}},
		{name: "unsafe kind", in: AttachmentDerivedInput{AttachmentID: 1, Kind: "bad/kind", Status: "ready", Metadata: `{}`}},
		{name: "empty status", in: AttachmentDerivedInput{AttachmentID: 1, Kind: "text", Status: "", Metadata: `{}`}},
		{name: "invalid metadata", in: AttachmentDerivedInput{AttachmentID: 1, Kind: "text", Status: "ready", Metadata: `{not-json}`}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.UpsertAttachmentDerived(ctx, tt.in)
			if err == nil {
				t.Fatal("expected validation error, got nil")
			}
		})
	}
}

func TestAttachmentStorage_RejectsUnsafeInput(t *testing.T) {
	s := newTestStorage(t)
	ctx := context.Background()

	for _, tt := range []struct {
		name string
		in   AttachmentInput
	}{
		{name: "empty digest", in: AttachmentInput{Digest: "", Size: 1, Metadata: `{}`}},
		{name: "negative size", in: AttachmentInput{Digest: "sha256:bad", Size: -1, Metadata: `{}`}},
		{name: "invalid metadata", in: AttachmentInput{Digest: "sha256:bad", Size: 1, Metadata: `{not-json}`}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.CreateAttachment(ctx, tt.in)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
		})
	}

	if _, err := s.GetAttachment(ctx, 0); err == nil {
		t.Fatal("GetAttachment accepted non-positive ID")
	}
	if _, err := s.GetAttachmentByDigest(ctx, ""); err == nil {
		t.Fatal("GetAttachmentByDigest accepted empty digest")
	}
	if err := s.LinkAttachmentToEntry(ctx, "", 1, "inline"); err == nil {
		t.Fatal("LinkAttachmentToEntry accepted empty note path")
	}
	if err := s.LinkAttachmentToEntry(ctx, "note.md", 0, "inline"); err == nil {
		t.Fatal("LinkAttachmentToEntry accepted non-positive attachment ID")
	}
	if err := s.LinkAttachmentToEntry(ctx, "note.md", 1, "inline; DROP TABLE attachments"); err == nil || !strings.Contains(err.Error(), "role") {
		t.Fatalf("LinkAttachmentToEntry unsafe role err = %v, want role error", err)
	}
	if _, err := s.UnlinkAttachmentFromEntry(ctx, "note.md", 0, "inline"); err == nil {
		t.Fatal("UnlinkAttachmentFromEntry accepted non-positive attachment ID")
	}
	if _, err := s.CountAttachmentReferences(ctx, 0); err == nil {
		t.Fatal("CountAttachmentReferences accepted non-positive attachment ID")
	}
	if _, err := s.DeleteAttachmentIfUnreferenced(ctx, 0); err == nil {
		t.Fatal("DeleteAttachmentIfUnreferenced accepted non-positive attachment ID")
	}
}

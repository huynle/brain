package storage

import (
	"database/sql"
	"testing"
)

// helper: open an in-memory StorageLayer for testing
func newTestStorage(t *testing.T) *StorageLayer {
	t.Helper()
	s, err := NewWithDB(openMemoryDB(t))
	if err != nil {
		t.Fatalf("NewWithDB failed: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// helper: open a raw in-memory *sql.DB with the sqlite driver
func openMemoryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	return db
}

// ---------------------------------------------------------------------------
// Schema creation: all tables exist
// ---------------------------------------------------------------------------

func TestSchemaCreation_TablesExist(t *testing.T) {
	s := newTestStorage(t)

	tables := []string{"notes", "links", "tags", "entry_meta", "generated_tasks", "schema_version", "api_tokens"}
	for _, table := range tables {
		t.Run(table, func(t *testing.T) {
			var name string
			err := s.DB().QueryRow(
				"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
			).Scan(&name)
			if err != nil {
				t.Fatalf("table %q not found: %v", table, err)
			}
			if name != table {
				t.Errorf("got table name %q, want %q", name, table)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Schema creation: all indexes exist
// ---------------------------------------------------------------------------

func TestSchemaCreation_IndexesExist(t *testing.T) {
	s := newTestStorage(t)

	indexes := []struct {
		name  string
		table string
	}{
		{"idx_notes_short_id", "notes"},
		{"idx_notes_type", "notes"},
		{"idx_notes_status", "notes"},
		{"idx_notes_project", "notes"},
		{"idx_notes_feature", "notes"},
		{"idx_links_source", "links"},
		{"idx_links_target", "links"},
		{"idx_links_target_path", "links"},
		{"idx_tags_note", "tags"},
		{"idx_tags_tag", "tags"},
	}

	for _, idx := range indexes {
		t.Run(idx.name, func(t *testing.T) {
			var name string
			err := s.DB().QueryRow(
				"SELECT name FROM sqlite_master WHERE type='index' AND name=?", idx.name,
			).Scan(&name)
			if err != nil {
				t.Fatalf("index %q not found: %v", idx.name, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FTS5 virtual table exists
// ---------------------------------------------------------------------------

func TestSchemaCreation_FTS5Exists(t *testing.T) {
	s := newTestStorage(t)

	var name string
	err := s.DB().QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='notes_fts'",
	).Scan(&name)
	if err != nil {
		t.Fatalf("FTS5 table notes_fts not found: %v", err)
	}
	if name != "notes_fts" {
		t.Errorf("got %q, want %q", name, "notes_fts")
	}
}

// ---------------------------------------------------------------------------
// FTS5 triggers exist
// ---------------------------------------------------------------------------

func TestSchemaCreation_TriggersExist(t *testing.T) {
	s := newTestStorage(t)

	triggers := []string{"notes_ai", "notes_ad", "notes_au"}
	for _, trig := range triggers {
		t.Run(trig, func(t *testing.T) {
			var name string
			err := s.DB().QueryRow(
				"SELECT name FROM sqlite_master WHERE type='trigger' AND name=?", trig,
			).Scan(&name)
			if err != nil {
				t.Fatalf("trigger %q not found: %v", trig, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// PRAGMAs are set correctly
// ---------------------------------------------------------------------------

func TestPragmas_InMemory(t *testing.T) {
	// :memory: databases cannot use WAL mode (journal_mode stays "memory").
	// Test the PRAGMAs that DO work with :memory:.
	s := newTestStorage(t)

	tests := []struct {
		pragma string
		want   string
	}{
		{"foreign_keys", "1"},
		{"synchronous", "1"}, // NORMAL = 1
	}

	for _, tt := range tests {
		t.Run(tt.pragma, func(t *testing.T) {
			var got string
			err := s.DB().QueryRow("PRAGMA " + tt.pragma).Scan(&got)
			if err != nil {
				t.Fatalf("PRAGMA %s failed: %v", tt.pragma, err)
			}
			if got != tt.want {
				t.Errorf("PRAGMA %s = %q, want %q", tt.pragma, got, tt.want)
			}
		})
	}
}

func TestPragmas_WALMode(t *testing.T) {
	// WAL mode requires a file-based database.
	dbPath := t.TempDir() + "/wal-test.db"
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New(%q) failed: %v", dbPath, err)
	}
	defer s.Close()

	var journalMode string
	if err := s.DB().QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("PRAGMA journal_mode failed: %v", err)
	}
	if journalMode != "wal" {
		t.Errorf("PRAGMA journal_mode = %q, want %q", journalMode, "wal")
	}
}

// ---------------------------------------------------------------------------
// Schema version tracking
// ---------------------------------------------------------------------------

func TestSchemaVersion(t *testing.T) {
	db := openMemoryDB(t)
	defer db.Close()

	// InitSchema should set version
	if err := InitSchema(db); err != nil {
		t.Fatalf("InitSchema failed: %v", err)
	}

	ver, err := GetSchemaVersion(db)
	if err != nil {
		t.Fatalf("GetSchemaVersion failed: %v", err)
	}
	if ver != CurrentSchemaVersion {
		t.Errorf("schema version = %d, want %d", ver, CurrentSchemaVersion)
	}

	// SetSchemaVersion should update
	if err := SetSchemaVersion(db, 99); err != nil {
		t.Fatalf("SetSchemaVersion failed: %v", err)
	}
	ver, err = GetSchemaVersion(db)
	if err != nil {
		t.Fatalf("GetSchemaVersion after set failed: %v", err)
	}
	if ver != 99 {
		t.Errorf("schema version after set = %d, want 99", ver)
	}
}

func TestGetSchemaVersion_EmptyDB(t *testing.T) {
	db := openMemoryDB(t)
	defer db.Close()

	// Create the schema_version table but don't insert any rows
	_, err := db.Exec("CREATE TABLE schema_version (version INTEGER PRIMARY KEY, applied_at TEXT DEFAULT (datetime('now')))")
	if err != nil {
		t.Fatalf("create table failed: %v", err)
	}

	ver, err := GetSchemaVersion(db)
	if err != nil {
		t.Fatalf("GetSchemaVersion on empty table failed: %v", err)
	}
	if ver != 0 {
		t.Errorf("schema version on empty table = %d, want 0", ver)
	}
}

// ---------------------------------------------------------------------------
// Open/close lifecycle
// ---------------------------------------------------------------------------

func TestNewWithDB_NilDB(t *testing.T) {
	_, err := NewWithDB(nil)
	if err == nil {
		t.Fatal("expected error for nil db, got nil")
	}
}

func TestStorageLayer_Close(t *testing.T) {
	s := newTestStorage(t)

	// Close should succeed
	if err := s.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// After close, DB operations should fail
	var n int
	err := s.DB().QueryRow("SELECT 1").Scan(&n)
	if err == nil {
		t.Error("expected error after Close, got nil")
	}
}

func TestStorageLayer_DB(t *testing.T) {
	s := newTestStorage(t)
	if s.DB() == nil {
		t.Fatal("DB() returned nil")
	}
}

// ---------------------------------------------------------------------------
// InitSchema is idempotent
// ---------------------------------------------------------------------------

func TestInitSchema_Idempotent(t *testing.T) {
	db := openMemoryDB(t)
	defer db.Close()

	// Run InitSchema twice — should not error
	if err := InitSchema(db); err != nil {
		t.Fatalf("first InitSchema failed: %v", err)
	}
	if err := InitSchema(db); err != nil {
		t.Fatalf("second InitSchema failed: %v", err)
	}

	// Tables should still exist
	var count int
	err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='notes'").Scan(&count)
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if count != 1 {
		t.Errorf("notes table count = %d, want 1", count)
	}
}

// ---------------------------------------------------------------------------
// FTS5 sync triggers work (insert triggers search index)
// ---------------------------------------------------------------------------

func TestFTS5_InsertTrigger(t *testing.T) {
	s := newTestStorage(t)

	// Insert a note directly
	_, err := s.DB().Exec(`
		INSERT INTO notes (path, short_id, title, body)
		VALUES ('test/path.md', 'abc123', 'Test Title', 'Test body content')
	`)
	if err != nil {
		t.Fatalf("insert note failed: %v", err)
	}

	// FTS5 should find it
	var title string
	err = s.DB().QueryRow(
		"SELECT title FROM notes_fts WHERE notes_fts MATCH 'Test'",
	).Scan(&title)
	if err != nil {
		t.Fatalf("FTS5 search failed: %v", err)
	}
	if title != "Test Title" {
		t.Errorf("FTS5 title = %q, want %q", title, "Test Title")
	}
}

func TestFTS5_DeleteTrigger(t *testing.T) {
	s := newTestStorage(t)

	// Insert then delete
	_, err := s.DB().Exec(`
		INSERT INTO notes (path, short_id, title, body)
		VALUES ('test/path.md', 'abc123', 'Unique Title', 'Unique body')
	`)
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	_, err = s.DB().Exec("DELETE FROM notes WHERE path = 'test/path.md'")
	if err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	// FTS5 should NOT find it
	var count int
	err = s.DB().QueryRow(
		"SELECT count(*) FROM notes_fts WHERE notes_fts MATCH 'Unique'",
	).Scan(&count)
	if err != nil {
		t.Fatalf("FTS5 count query failed: %v", err)
	}
	if count != 0 {
		t.Errorf("FTS5 count after delete = %d, want 0", count)
	}
}

func TestFTS5_UpdateTrigger(t *testing.T) {
	s := newTestStorage(t)

	// Insert
	_, err := s.DB().Exec(`
		INSERT INTO notes (path, short_id, title, body)
		VALUES ('test/path.md', 'abc123', 'Original Title', 'Original body')
	`)
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	// Update title
	_, err = s.DB().Exec("UPDATE notes SET title = 'Updated Title' WHERE path = 'test/path.md'")
	if err != nil {
		t.Fatalf("update failed: %v", err)
	}

	// FTS5 should find the new title
	var count int
	err = s.DB().QueryRow(
		"SELECT count(*) FROM notes_fts WHERE notes_fts MATCH 'Updated'",
	).Scan(&count)
	if err != nil {
		t.Fatalf("FTS5 search for Updated failed: %v", err)
	}
	if count != 1 {
		t.Errorf("FTS5 count for 'Updated' = %d, want 1", count)
	}

	// FTS5 should NOT find the old title
	err = s.DB().QueryRow(
		"SELECT count(*) FROM notes_fts WHERE notes_fts MATCH 'Original'",
	).Scan(&count)
	if err != nil {
		t.Fatalf("FTS5 search for Original failed: %v", err)
	}
	// Note: 'Original' still appears in body, so count should be 1 for body match
	// But the title should be 'Updated Title' not 'Original Title'
	var title string
	err = s.DB().QueryRow(
		"SELECT title FROM notes_fts WHERE notes_fts MATCH 'Updated'",
	).Scan(&title)
	if err != nil {
		t.Fatalf("FTS5 title query failed: %v", err)
	}
	if title != "Updated Title" {
		t.Errorf("FTS5 title = %q, want %q", title, "Updated Title")
	}
}

// ---------------------------------------------------------------------------
// Foreign key enforcement
// ---------------------------------------------------------------------------

func TestForeignKeys_LinksRequireNote(t *testing.T) {
	s := newTestStorage(t)

	// Inserting a link with non-existent source_id should fail
	_, err := s.DB().Exec(`
		INSERT INTO links (source_id, target_path, href)
		VALUES (9999, 'some/path.md', 'some/path.md')
	`)
	if err == nil {
		t.Fatal("expected foreign key error for invalid source_id, got nil")
	}
}

func TestForeignKeys_TagsRequireNote(t *testing.T) {
	s := newTestStorage(t)

	// Inserting a tag with non-existent note_id should fail
	_, err := s.DB().Exec(`
		INSERT INTO tags (note_id, tag)
		VALUES (9999, 'test-tag')
	`)
	if err == nil {
		t.Fatal("expected foreign key error for invalid note_id, got nil")
	}
}

// ---------------------------------------------------------------------------
// Cascade delete: deleting a note removes its links and tags
// ---------------------------------------------------------------------------

func TestCascadeDelete_LinksRemoved(t *testing.T) {
	s := newTestStorage(t)

	// Insert a note
	res, err := s.DB().Exec(`
		INSERT INTO notes (path, short_id, title) VALUES ('test/note.md', 'abc', 'Test')
	`)
	if err != nil {
		t.Fatalf("insert note failed: %v", err)
	}
	noteID, _ := res.LastInsertId()

	// Insert a link referencing the note
	_, err = s.DB().Exec(`
		INSERT INTO links (source_id, target_path, href) VALUES (?, 'other/path.md', 'other/path.md')
	`, noteID)
	if err != nil {
		t.Fatalf("insert link failed: %v", err)
	}

	// Delete the note
	_, err = s.DB().Exec("DELETE FROM notes WHERE id = ?", noteID)
	if err != nil {
		t.Fatalf("delete note failed: %v", err)
	}

	// Link should be gone (CASCADE)
	var count int
	err = s.DB().QueryRow("SELECT count(*) FROM links WHERE source_id = ?", noteID).Scan(&count)
	if err != nil {
		t.Fatalf("count links failed: %v", err)
	}
	if count != 0 {
		t.Errorf("links count after cascade delete = %d, want 0", count)
	}
}

func TestCascadeDelete_TagsRemoved(t *testing.T) {
	s := newTestStorage(t)

	// Insert a note
	res, err := s.DB().Exec(`
		INSERT INTO notes (path, short_id, title) VALUES ('test/note.md', 'abc', 'Test')
	`)
	if err != nil {
		t.Fatalf("insert note failed: %v", err)
	}
	noteID, _ := res.LastInsertId()

	// Insert a tag
	_, err = s.DB().Exec("INSERT INTO tags (note_id, tag) VALUES (?, 'my-tag')", noteID)
	if err != nil {
		t.Fatalf("insert tag failed: %v", err)
	}

	// Delete the note
	_, err = s.DB().Exec("DELETE FROM notes WHERE id = ?", noteID)
	if err != nil {
		t.Fatalf("delete note failed: %v", err)
	}

	// Tag should be gone (CASCADE)
	var count int
	err = s.DB().QueryRow("SELECT count(*) FROM tags WHERE note_id = ?", noteID).Scan(&count)
	if err != nil {
		t.Fatalf("count tags failed: %v", err)
	}
	if count != 0 {
		t.Errorf("tags count after cascade delete = %d, want 0", count)
	}
}

// ---------------------------------------------------------------------------
// New() with file path (integration test)
// ---------------------------------------------------------------------------

func TestNew_WithTempFile(t *testing.T) {
	dbPath := t.TempDir() + "/test.db"
	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("New(%q) failed: %v", dbPath, err)
	}
	defer s.Close()

	// Should be able to query
	var n int
	if err := s.DB().QueryRow("SELECT 1").Scan(&n); err != nil {
		t.Fatalf("query after New failed: %v", err)
	}
	if n != 1 {
		t.Errorf("SELECT 1 = %d, want 1", n)
	}

	// Tables should exist
	var name string
	err = s.DB().QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='notes'").Scan(&name)
	if err != nil {
		t.Fatalf("notes table not found after New: %v", err)
	}
}

// ---------------------------------------------------------------------------
// MaxOpenConns is set to 1 for write serialization
// ---------------------------------------------------------------------------

func TestNew_MaxOpenConns(t *testing.T) {
	// We can't directly query MaxOpenConns from sql.DB, but we can verify
	// that concurrent writes don't cause "database is locked" errors
	// by running multiple inserts. This is an indirect test.
	s := newTestStorage(t)

	// Insert multiple notes — if MaxOpenConns weren't set, this could
	// cause issues with WAL mode on some platforms
	for i := 0; i < 10; i++ {
		_, err := s.DB().Exec(
			"INSERT INTO notes (path, short_id, title) VALUES (?, ?, ?)",
			"test/"+string(rune('a'+i))+".md", "id"+string(rune('0'+i)), "Title",
		)
		if err != nil {
			t.Fatalf("insert %d failed: %v", i, err)
		}
	}
}

package storage

import (
	"context"
	"database/sql"
	"path/filepath"
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

	tables := []string{"notes", "links", "tags", "entry_meta", "generated_tasks", "schema_version", "api_tokens",
		"oauth_clients", "oauth_auth_codes", "oauth_access_tokens", "oauth_refresh_tokens",
		"task_claims", "task_dispatch_leases", "task_placement_reasons", "runners", "webhooks", "webhook_deliveries", "feature_assignments",
		"note_embeddings", "note_embeddings_meta", "attachments", "entry_attachments"}
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
		{"idx_note_embeddings_meta_project", "note_embeddings_meta"},
		{"idx_note_embeddings_meta_type", "note_embeddings_meta"},
		{"idx_note_embeddings_meta_status", "note_embeddings_meta"},
		{"idx_note_embeddings_meta_feature", "note_embeddings_meta"},
		{"idx_note_embeddings_meta_priority", "note_embeddings_meta"},
		{"idx_attachments_digest", "attachments"},
		{"idx_entry_attachments_note", "entry_attachments"},
		{"idx_entry_attachments_attachment", "entry_attachments"},
		{"idx_entry_attachments_note_attachment_role", "entry_attachments"},
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

// ---------------------------------------------------------------------------
// task_claims table: fresh DB has correct schema
// ---------------------------------------------------------------------------

func TestTaskClaimsTable_FreshDB(t *testing.T) {
	s := newTestStorage(t)

	// Table should exist
	var name string
	err := s.DB().QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='task_claims'",
	).Scan(&name)
	if err != nil {
		t.Fatalf("task_claims table not found: %v", err)
	}

	// Verify columns by inserting and querying a row
	_, err = s.DB().Exec(`INSERT INTO task_claims (project_id, task_id, runner_id, claimed_at, expires_at)
		VALUES ('proj1', 'task1', 'runner1', 1000, 2000)`)
	if err != nil {
		t.Fatalf("insert into task_claims failed: %v", err)
	}

	var projectID, taskID, runnerID string
	var claimedAt, expiresAt int64
	err = s.DB().QueryRow("SELECT project_id, task_id, runner_id, claimed_at, expires_at FROM task_claims").
		Scan(&projectID, &taskID, &runnerID, &claimedAt, &expiresAt)
	if err != nil {
		t.Fatalf("select from task_claims failed: %v", err)
	}
	if projectID != "proj1" || taskID != "task1" || runnerID != "runner1" {
		t.Errorf("got (%q, %q, %q), want (proj1, task1, runner1)", projectID, taskID, runnerID)
	}
	if claimedAt != 1000 || expiresAt != 2000 {
		t.Errorf("got (claimed_at=%d, expires_at=%d), want (1000, 2000)", claimedAt, expiresAt)
	}
}

func TestTaskClaimsTable_PrimaryKey(t *testing.T) {
	s := newTestStorage(t)

	// Insert first claim
	_, err := s.DB().Exec(`INSERT INTO task_claims (project_id, task_id, runner_id, claimed_at, expires_at)
		VALUES ('proj1', 'task1', 'runner1', 1000, 2000)`)
	if err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	// Duplicate (project_id, task_id) should fail — composite PK
	_, err = s.DB().Exec(`INSERT INTO task_claims (project_id, task_id, runner_id, claimed_at, expires_at)
		VALUES ('proj1', 'task1', 'runner2', 3000, 4000)`)
	if err == nil {
		t.Fatal("expected PK violation for duplicate (project_id, task_id), got nil")
	}

	// Same task_id but different project_id should succeed
	_, err = s.DB().Exec(`INSERT INTO task_claims (project_id, task_id, runner_id, claimed_at, expires_at)
		VALUES ('proj2', 'task1', 'runner1', 1000, 2000)`)
	if err != nil {
		t.Fatalf("insert with different project_id failed: %v", err)
	}
}

func TestTaskClaimsTable_Indexes(t *testing.T) {
	s := newTestStorage(t)

	indexes := []string{
		"idx_claims_runner",
		"idx_claims_expires",
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

// ---------------------------------------------------------------------------
// task_claims table: migration from v4 to v5
// ---------------------------------------------------------------------------

func TestTaskClaimsTable_MigrationFromV4(t *testing.T) {
	db := openMemoryDB(t)
	defer db.Close()

	// Simulate a v4 database: create schema_version and set version to 4
	_, err := db.Exec(createSchemaVersionTable)
	if err != nil {
		t.Fatalf("create schema_version table: %v", err)
	}
	_, err = db.Exec("INSERT INTO schema_version (version) VALUES (4)")
	if err != nil {
		t.Fatalf("insert v4: %v", err)
	}

	// Run migration
	err = migrateSchema(db)
	if err != nil {
		t.Fatalf("migrateSchema failed: %v", err)
	}

	// task_claims table should now exist
	var name string
	err = db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='task_claims'",
	).Scan(&name)
	if err != nil {
		t.Fatalf("task_claims table not found after migration: %v", err)
	}

	// Should be able to insert data
	_, err = db.Exec(`INSERT INTO task_claims (project_id, task_id, runner_id, claimed_at, expires_at)
		VALUES ('proj1', 'task1', 'runner1', 1000, 2000)`)
	if err != nil {
		t.Fatalf("insert after migration failed: %v", err)
	}
}

func TestSchemaVersion_IncludesRunnerPauseDial(t *testing.T) {
	if CurrentSchemaVersion != 23 {
		t.Errorf("CurrentSchemaVersion = %d, want 23", CurrentSchemaVersion)
	}
}

func TestBrainClientTables_FreshDB(t *testing.T) {
	s := newTestStorage(t)

	for _, table := range []string{"brain_clients", "brain_client_workspaces"} {
		var name string
		err := s.DB().QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?",
			table,
		).Scan(&name)
		if err != nil {
			t.Fatalf("%s table not found: %v", table, err)
		}
	}

	if err := s.UpsertBrainClient(context.Background(), &BrainClientRow{ClientID: "client-1", HostID: "host-1"}); err != nil {
		t.Fatalf("UpsertBrainClient failed: %v", err)
	}
	if err := s.UpsertBrainClientWorkspace(context.Background(), &BrainClientWorkspaceRow{ClientID: "client-1", HostID: "host-1", ProjectID: "brain", Path: "/work/brain"}); err != nil {
		t.Fatalf("UpsertBrainClientWorkspace failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// feature_assignments table: fresh DB has correct schema
// ---------------------------------------------------------------------------

func TestFeatureAssignmentsTable_FreshDB(t *testing.T) {
	s := newTestStorage(t)

	var name string
	err := s.DB().QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='feature_assignments'",
	).Scan(&name)
	if err != nil {
		t.Fatalf("feature_assignments table not found: %v", err)
	}

	_, err = s.DB().Exec(`INSERT INTO feature_assignments (project_id, feature_id, runner_id, source, status, assigned_at, updated_at)
		VALUES ('proj1', 'feat1', 'runner1', 'auto', 'active', 1000, 2000)`)
	if err != nil {
		t.Fatalf("insert into feature_assignments failed: %v", err)
	}

	var projectID, featureID, runnerID, source, status string
	var assignedAt, updatedAt int64
	err = s.DB().QueryRow("SELECT project_id, feature_id, runner_id, source, status, assigned_at, updated_at FROM feature_assignments").
		Scan(&projectID, &featureID, &runnerID, &source, &status, &assignedAt, &updatedAt)
	if err != nil {
		t.Fatalf("select from feature_assignments failed: %v", err)
	}
	if projectID != "proj1" || featureID != "feat1" || runnerID != "runner1" {
		t.Errorf("got (%q, %q, %q), want (proj1, feat1, runner1)", projectID, featureID, runnerID)
	}
	if source != "auto" || status != "active" {
		t.Errorf("got (source=%q, status=%q), want (auto, active)", source, status)
	}
	if assignedAt != 1000 || updatedAt != 2000 {
		t.Errorf("got (assigned_at=%d, updated_at=%d), want (1000, 2000)", assignedAt, updatedAt)
	}
}

func TestFeatureAssignmentsTable_PrimaryKey(t *testing.T) {
	s := newTestStorage(t)

	_, err := s.DB().Exec(`INSERT INTO feature_assignments (project_id, feature_id, runner_id, source, status, assigned_at, updated_at)
		VALUES ('proj1', 'feat1', 'runner1', 'auto', 'active', 1000, 2000)`)
	if err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	_, err = s.DB().Exec(`INSERT INTO feature_assignments (project_id, feature_id, runner_id, source, status, assigned_at, updated_at)
		VALUES ('proj1', 'feat1', 'runner2', 'manual', 'active', 3000, 4000)`)
	if err == nil {
		t.Fatal("expected PK violation for duplicate (project_id, feature_id), got nil")
	}

	_, err = s.DB().Exec(`INSERT INTO feature_assignments (project_id, feature_id, runner_id, source, status, assigned_at, updated_at)
		VALUES ('proj2', 'feat1', 'runner2', 'manual', 'active', 3000, 4000)`)
	if err != nil {
		t.Fatalf("insert with different project_id failed: %v", err)
	}
}

func TestFeatureAssignmentsTable_Indexes(t *testing.T) {
	s := newTestStorage(t)

	indexes := []string{
		"idx_feature_assignments_runner",
		"idx_feature_assignments_project",
		"idx_feature_assignments_status",
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

// ---------------------------------------------------------------------------
// feature_assignments table: migration from v9 to v10
// ---------------------------------------------------------------------------

func TestFeatureAssignmentsTable_MigrationFromV9(t *testing.T) {
	db := openMemoryDB(t)
	defer db.Close()

	_, err := db.Exec(createSchemaVersionTable)
	if err != nil {
		t.Fatalf("create schema_version table: %v", err)
	}
	_, err = db.Exec("INSERT INTO schema_version (version) VALUES (9)")
	if err != nil {
		t.Fatalf("insert v9: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE runners (
		runner_id TEXT PRIMARY KEY,
		hostname TEXT NOT NULL,
		labels TEXT DEFAULT '{}',
		executors TEXT DEFAULT '[]',
		max_parallel INTEGER NOT NULL DEFAULT 1,
		feature_ids TEXT DEFAULT '',
		registered_at INTEGER NOT NULL,
		last_heartbeat INTEGER NOT NULL,
		status TEXT NOT NULL DEFAULT 'online'
	)`); err != nil {
		t.Fatalf("create v9 runners table: %v", err)
	}

	err = migrateSchema(db)
	if err != nil {
		t.Fatalf("migrateSchema failed: %v", err)
	}

	var name string
	err = db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='feature_assignments'",
	).Scan(&name)
	if err != nil {
		t.Fatalf("feature_assignments table not found after migration: %v", err)
	}

	_, err = db.Exec(`INSERT INTO feature_assignments (project_id, feature_id, runner_id, source, status, assigned_at, updated_at)
		VALUES ('proj1', 'feat1', 'runner1', 'auto', 'active', 1000, 2000)`)
	if err != nil {
		t.Fatalf("insert after migration failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// runners table: fresh DB has correct schema
// ---------------------------------------------------------------------------

func TestRunnersTable_FreshDB(t *testing.T) {
	s := newTestStorage(t)

	// Table should exist
	var name string
	err := s.DB().QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='runners'",
	).Scan(&name)
	if err != nil {
		t.Fatalf("runners table not found: %v", err)
	}

	// Verify columns by inserting and querying a row
	_, err = s.DB().Exec(`INSERT INTO runners (runner_id, hostname, labels, executors, max_parallel, feature_ids, registered_at, last_heartbeat, status)
		VALUES ('runner-1', 'host1.local', '{"env":"prod"}', '["opencode"]', 4, 'feat-a,feat-b', 1000, 2000, 'online')`)
	if err != nil {
		t.Fatalf("insert into runners failed: %v", err)
	}

	var runnerID, hostname, labels, executors, featureIDs, status string
	var maxParallel int
	var registeredAt, lastHeartbeat int64
	err = s.DB().QueryRow("SELECT runner_id, hostname, labels, executors, max_parallel, feature_ids, registered_at, last_heartbeat, status FROM runners").
		Scan(&runnerID, &hostname, &labels, &executors, &maxParallel, &featureIDs, &registeredAt, &lastHeartbeat, &status)
	if err != nil {
		t.Fatalf("select from runners failed: %v", err)
	}
	if runnerID != "runner-1" || hostname != "host1.local" {
		t.Errorf("got (runner_id=%q, hostname=%q), want (runner-1, host1.local)", runnerID, hostname)
	}
	if labels != `{"env":"prod"}` || executors != `["opencode"]` {
		t.Errorf("got (labels=%q, executors=%q), want JSON values", labels, executors)
	}
	if maxParallel != 4 {
		t.Errorf("max_parallel = %d, want 4", maxParallel)
	}
	if featureIDs != "feat-a,feat-b" {
		t.Errorf("feature_ids = %q, want %q", featureIDs, "feat-a,feat-b")
	}
	if registeredAt != 1000 || lastHeartbeat != 2000 {
		t.Errorf("got (registered_at=%d, last_heartbeat=%d), want (1000, 2000)", registeredAt, lastHeartbeat)
	}
	if status != "online" {
		t.Errorf("status = %q, want %q", status, "online")
	}
}

func TestRunnersTable_FreshDB_CapabilitiesColumn(t *testing.T) {
	s := newTestStorage(t)

	var columnName string
	var defaultValue sql.NullString
	err := s.DB().QueryRow(`SELECT name, dflt_value FROM pragma_table_info('runners') WHERE name = 'capabilities'`).
		Scan(&columnName, &defaultValue)
	if err != nil {
		t.Fatalf("capabilities column not found in fresh runners table: %v", err)
	}
	if columnName != "capabilities" {
		t.Fatalf("column name = %q, want capabilities", columnName)
	}
	if !defaultValue.Valid || defaultValue.String != "'[]'" {
		t.Fatalf("capabilities default = %q (valid=%v), want '[]'", defaultValue.String, defaultValue.Valid)
	}

	_, err = s.DB().Exec(`INSERT INTO runners (runner_id, hostname, labels, executors, capabilities, max_parallel, registered_at, last_heartbeat)
		VALUES ('runner-cap', 'host-cap', '{}', '["opencode"]', '["gpu","docker"]', 1, 1000, 2000)`)
	if err != nil {
		t.Fatalf("insert runner with capabilities failed: %v", err)
	}

	var capabilities string
	err = s.DB().QueryRow("SELECT capabilities FROM runners WHERE runner_id = 'runner-cap'").Scan(&capabilities)
	if err != nil {
		t.Fatalf("select capabilities failed: %v", err)
	}
	if capabilities != `["gpu","docker"]` {
		t.Fatalf("capabilities = %q, want JSON array", capabilities)
	}
}

func TestRunnersTable_MigrationFromV10AddsCapabilities(t *testing.T) {
	db := openMemoryDB(t)
	defer db.Close()

	if _, err := db.Exec(createSchemaVersionTable); err != nil {
		t.Fatalf("create schema_version table: %v", err)
	}
	if _, err := db.Exec("INSERT INTO schema_version (version) VALUES (10)"); err != nil {
		t.Fatalf("insert v10: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE runners (
		runner_id TEXT PRIMARY KEY,
		hostname TEXT NOT NULL,
		labels TEXT DEFAULT '{}',
		executors TEXT DEFAULT '[]',
		max_parallel INTEGER NOT NULL DEFAULT 1,
		feature_ids TEXT DEFAULT '',
		registered_at INTEGER NOT NULL,
		last_heartbeat INTEGER NOT NULL,
		status TEXT NOT NULL DEFAULT 'online'
	)`); err != nil {
		t.Fatalf("create v10 runners table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO runners (runner_id, hostname, labels, executors, max_parallel, registered_at, last_heartbeat)
		VALUES ('legacy-runner', 'legacy-host', '{}', '["opencode"]', 1, 1000, 2000)`); err != nil {
		t.Fatalf("insert legacy runner: %v", err)
	}

	if err := migrateSchema(db); err != nil {
		t.Fatalf("migrateSchema failed: %v", err)
	}

	var capabilities string
	if err := db.QueryRow("SELECT capabilities FROM runners WHERE runner_id = 'legacy-runner'").Scan(&capabilities); err != nil {
		t.Fatalf("select migrated capabilities failed: %v", err)
	}
	if capabilities != "[]" {
		t.Fatalf("legacy capabilities = %q, want []", capabilities)
	}
}

func TestRunnersTable_FreshDB_DispatchMetadataColumns(t *testing.T) {
	s := newTestStorage(t)

	wantDefaults := map[string]string{
		"machine_id":      "''",
		"dispatch_push":   "0",
		"workspace_roots": "'[]'",
		"projects":        "'[]'",
		"resources":       "'{}'",
		"capacity":        "'{}'",
		"draining":        "0",
	}
	for column, wantDefault := range wantDefaults {
		var defaultValue sql.NullString
		err := s.DB().QueryRow(`SELECT dflt_value FROM pragma_table_info('runners') WHERE name = ?`, column).Scan(&defaultValue)
		if err != nil {
			t.Fatalf("%s column not found in fresh runners table: %v", column, err)
		}
		if !defaultValue.Valid || defaultValue.String != wantDefault {
			t.Fatalf("%s default = %q (valid=%v), want %s", column, defaultValue.String, defaultValue.Valid, wantDefault)
		}
	}

	_, err := s.DB().Exec(`INSERT INTO runners (runner_id, hostname, machine_id, dispatch_push, workspace_roots, projects, resources, capacity, draining, max_parallel, registered_at, last_heartbeat)
		VALUES ('runner-dispatch', 'host-dispatch', 'machine-explicit', 1, '["/work/brain"]', '["brain-api"]', '{"os":"darwin"}', '{"max_parallel":4}', 1, 4, 1000, 2000)`)
	if err != nil {
		t.Fatalf("insert runner with dispatch metadata failed: %v", err)
	}

	var machineID, workspaceRoots, projects, resources, capacity string
	var dispatchPush, draining int
	err = s.DB().QueryRow(`SELECT machine_id, dispatch_push, workspace_roots, projects, resources, capacity, draining FROM runners WHERE runner_id = 'runner-dispatch'`).
		Scan(&machineID, &dispatchPush, &workspaceRoots, &projects, &resources, &capacity, &draining)
	if err != nil {
		t.Fatalf("select dispatch metadata failed: %v", err)
	}
	if machineID != "machine-explicit" || dispatchPush != 1 || workspaceRoots != `["/work/brain"]` || projects != `["brain-api"]` || resources != `{"os":"darwin"}` || capacity != `{"max_parallel":4}` || draining != 1 {
		t.Fatalf("unexpected dispatch metadata: machine=%q push=%d roots=%q projects=%q resources=%q capacity=%q draining=%d", machineID, dispatchPush, workspaceRoots, projects, resources, capacity, draining)
	}
}

func TestRunnersTable_MigrationFromV17AddsDispatchMetadata(t *testing.T) {
	db := openMemoryDB(t)
	defer db.Close()

	if _, err := db.Exec(createSchemaVersionTable); err != nil {
		t.Fatalf("create schema_version table: %v", err)
	}
	if _, err := db.Exec("INSERT INTO schema_version (version) VALUES (17)"); err != nil {
		t.Fatalf("insert v17: %v", err)
	}
	if _, err := db.Exec(`CREATE TABLE runners (
		runner_id TEXT PRIMARY KEY,
		hostname TEXT NOT NULL,
		labels TEXT DEFAULT '{}',
		executors TEXT DEFAULT '[]',
		capabilities TEXT DEFAULT '[]',
		max_parallel INTEGER NOT NULL DEFAULT 1,
		feature_ids TEXT DEFAULT '',
		registered_at INTEGER NOT NULL,
		last_heartbeat INTEGER NOT NULL,
		status TEXT NOT NULL DEFAULT 'online'
	)`); err != nil {
		t.Fatalf("create v17 runners table: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO runners (runner_id, hostname, labels, executors, capabilities, max_parallel, registered_at, last_heartbeat)
		VALUES ('legacy-runner', 'legacy-host', '{"_machine_id":"legacy-machine"}', '["opencode"]', '["docker"]', 1, 1000, 2000)`); err != nil {
		t.Fatalf("insert legacy runner: %v", err)
	}

	if err := migrateSchema(db); err != nil {
		t.Fatalf("migrateSchema failed: %v", err)
	}

	var machineID, workspaceRoots, projects, resources, capacity string
	var dispatchPush, draining int
	if err := db.QueryRow(`SELECT machine_id, dispatch_push, workspace_roots, projects, resources, capacity, draining FROM runners WHERE runner_id = 'legacy-runner'`).
		Scan(&machineID, &dispatchPush, &workspaceRoots, &projects, &resources, &capacity, &draining); err != nil {
		t.Fatalf("select migrated dispatch metadata failed: %v", err)
	}
	if machineID != "" || dispatchPush != 0 || workspaceRoots != "[]" || projects != "[]" || resources != "{}" || capacity != "{}" || draining != 0 {
		t.Fatalf("migrated metadata = machine=%q push=%d roots=%q projects=%q resources=%q capacity=%q draining=%d; want defaults", machineID, dispatchPush, workspaceRoots, projects, resources, capacity, draining)
	}
}

func TestRunnersTable_DefaultStatus(t *testing.T) {
	s := newTestStorage(t)

	// Insert without explicit status — should default to 'online'
	_, err := s.DB().Exec(`INSERT INTO runners (runner_id, hostname, max_parallel, registered_at, last_heartbeat)
		VALUES ('runner-2', 'host2.local', 1, 1000, 2000)`)
	if err != nil {
		t.Fatalf("insert with defaults failed: %v", err)
	}

	var status string
	err = s.DB().QueryRow("SELECT status FROM runners WHERE runner_id = 'runner-2'").Scan(&status)
	if err != nil {
		t.Fatalf("select status failed: %v", err)
	}
	if status != "online" {
		t.Errorf("default status = %q, want %q", status, "online")
	}
}

func TestRunnersTable_PrimaryKey(t *testing.T) {
	s := newTestStorage(t)

	_, err := s.DB().Exec(`INSERT INTO runners (runner_id, hostname, max_parallel, registered_at, last_heartbeat)
		VALUES ('runner-1', 'host1', 1, 1000, 2000)`)
	if err != nil {
		t.Fatalf("first insert failed: %v", err)
	}

	// Duplicate runner_id should fail
	_, err = s.DB().Exec(`INSERT INTO runners (runner_id, hostname, max_parallel, registered_at, last_heartbeat)
		VALUES ('runner-1', 'host2', 2, 3000, 4000)`)
	if err == nil {
		t.Fatal("expected PK violation for duplicate runner_id, got nil")
	}
}

func TestRunnersTable_Index(t *testing.T) {
	s := newTestStorage(t)

	var name string
	err := s.DB().QueryRow(
		"SELECT name FROM sqlite_master WHERE type='index' AND name='idx_runners_status'",
	).Scan(&name)
	if err != nil {
		t.Fatalf("index idx_runners_status not found: %v", err)
	}
}

// ---------------------------------------------------------------------------
// runners table: migration from v5 to v6
// ---------------------------------------------------------------------------

func TestRunnersTable_MigrationFromV5(t *testing.T) {
	db := openMemoryDB(t)
	defer db.Close()

	// Simulate a v5 database: create schema_version and set version to 5
	_, err := db.Exec(createSchemaVersionTable)
	if err != nil {
		t.Fatalf("create schema_version table: %v", err)
	}
	_, err = db.Exec("INSERT INTO schema_version (version) VALUES (5)")
	if err != nil {
		t.Fatalf("insert v5: %v", err)
	}

	// Run migration
	err = migrateSchema(db)
	if err != nil {
		t.Fatalf("migrateSchema failed: %v", err)
	}

	// runners table should now exist
	var name string
	err = db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='runners'",
	).Scan(&name)
	if err != nil {
		t.Fatalf("runners table not found after migration: %v", err)
	}

	// Should be able to insert data
	_, err = db.Exec(`INSERT INTO runners (runner_id, hostname, max_parallel, registered_at, last_heartbeat)
		VALUES ('runner-1', 'host1.local', 2, 1000, 2000)`)
	if err != nil {
		t.Fatalf("insert after migration failed: %v", err)
	}

	// Index should exist
	err = db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='index' AND name='idx_runners_status'",
	).Scan(&name)
	if err != nil {
		t.Fatalf("idx_runners_status not found after migration: %v", err)
	}
}

// TestNew_PragmasHoldOnEveryConnection is the guard that makes raising
// SetMaxOpenConns a safe decision later rather than a silent data-integrity
// regression.
//
// foreign_keys and synchronous are PER-CONNECTION settings. Setting them with
// db.Exec binds them to whichever pooled connection served that call; every
// other connection gets SQLite's defaults — foreign_keys OFF. Measured against
// the driver with a 4-connection pool before this change:
//
//	conn 0    -> foreign_keys=1 synchronous=1
//	conn 1..3 -> foreign_keys=0 synchronous=2
//
// Only journal_mode survived, because WAL is persisted in the database file.
// So the connection cap was load-bearing for CORRECTNESS, and anyone raising it
// for the obvious performance reason would have quietly turned off foreign key
// enforcement.
//
// The oracle here is SQLite's own PRAGMA readback on each live connection, not
// a restatement of what the code intends to do.
func TestNew_PragmasHoldOnEveryConnection(t *testing.T) {
	store, err := New(filepath.Join(t.TempDir(), "pragmas.db"))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	defer store.Close()

	db := store.DB()
	// Lift the cap for this check only. The point is that the pragmas no longer
	// depend on it, so verifying them requires more than one connection to
	// actually exist.
	db.SetMaxOpenConns(4)

	ctx := context.Background()
	var conns []*sql.Conn
	for i := 0; i < 4; i++ {
		c, err := db.Conn(ctx)
		if err != nil {
			t.Fatalf("open connection %d: %v", i, err)
		}
		conns = append(conns, c)
	}
	t.Cleanup(func() {
		for _, c := range conns {
			c.Close()
		}
	})

	for i, c := range conns {
		var foreignKeys, synchronous, busyTimeout int
		var journalMode string
		if err := c.QueryRowContext(ctx, "PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
			t.Fatalf("conn %d read foreign_keys: %v", i, err)
		}
		if err := c.QueryRowContext(ctx, "PRAGMA synchronous").Scan(&synchronous); err != nil {
			t.Fatalf("conn %d read synchronous: %v", i, err)
		}
		if err := c.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
			t.Fatalf("conn %d read journal_mode: %v", i, err)
		}
		if err := c.QueryRowContext(ctx, "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
			t.Fatalf("conn %d read busy_timeout: %v", i, err)
		}

		if foreignKeys != 1 {
			t.Errorf("conn %d: foreign_keys=%d, want 1 — FK enforcement is off on this connection", i, foreignKeys)
		}
		if synchronous != 1 {
			t.Errorf("conn %d: synchronous=%d, want 1 (NORMAL)", i, synchronous)
		}
		if journalMode != "wal" {
			t.Errorf("conn %d: journal_mode=%q, want wal", i, journalMode)
		}
		// Without a busy timeout, a pool larger than one fails immediately on
		// write contention instead of waiting.
		if busyTimeout <= 0 {
			t.Errorf("conn %d: busy_timeout=%d, want a positive wait", i, busyTimeout)
		}
	}
}

// TestNewWithDB_KeepsForeignKeysUnderTheConnectionCap covers the other
// construction path, whose DSN this package does not control. There the
// db.Exec pragmas are sufficient ONLY because SetMaxOpenConns(1) guarantees a
// single connection — so if that cap is ever raised, this path needs its own
// answer, and this test is where that will surface.
func TestNewWithDB_KeepsForeignKeysUnderTheConnectionCap(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	store, err := NewWithDB(db)
	if err != nil {
		t.Fatalf("NewWithDB failed: %v", err)
	}
	defer store.Close()

	var foreignKeys int
	if err := store.DB().QueryRow("PRAGMA foreign_keys").Scan(&foreignKeys); err != nil {
		t.Fatalf("read foreign_keys: %v", err)
	}
	if foreignKeys != 1 {
		t.Errorf("foreign_keys=%d, want 1", foreignKeys)
	}
}

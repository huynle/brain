package storage

import (
	"database/sql"
	"fmt"
)

// CurrentSchemaVersion is the latest schema version.
const CurrentSchemaVersion = 1

// ---------------------------------------------------------------------------
// DDL statements
// ---------------------------------------------------------------------------

const createNotesTable = `
CREATE TABLE IF NOT EXISTS notes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  path TEXT UNIQUE NOT NULL,
  short_id TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  lead TEXT DEFAULT '',
  body TEXT DEFAULT '',
  raw_content TEXT DEFAULT '',
  word_count INTEGER DEFAULT 0,
  checksum TEXT,
  metadata TEXT DEFAULT '{}',
  type TEXT,
  status TEXT,
  priority TEXT,
  project_id TEXT,
  feature_id TEXT,
  created TEXT,
  modified TEXT,
  indexed_at TEXT DEFAULT (datetime('now'))
);`

const createLinksTable = `
CREATE TABLE IF NOT EXISTS links (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  target_path TEXT NOT NULL,
  target_id INTEGER REFERENCES notes(id) ON DELETE SET NULL,
  title TEXT DEFAULT '',
  href TEXT NOT NULL,
  type TEXT DEFAULT 'markdown',
  snippet TEXT DEFAULT ''
);`

const createTagsTable = `
CREATE TABLE IF NOT EXISTS tags (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  tag TEXT NOT NULL
);`

const createEntryMetaTable = `
CREATE TABLE IF NOT EXISTS entry_meta (
  path TEXT PRIMARY KEY,
  project_id TEXT,
  access_count INTEGER DEFAULT 0,
  last_accessed TEXT,
  last_verified TEXT,
  created_at TEXT DEFAULT (datetime('now'))
);`

const createGeneratedTasksTable = `
CREATE TABLE IF NOT EXISTS generated_tasks (
  key TEXT PRIMARY KEY,
  task_path TEXT NOT NULL,
  feature_id TEXT,
  created_at TEXT DEFAULT (datetime('now'))
);`

const createSchemaVersionTable = `
CREATE TABLE IF NOT EXISTS schema_version (
  version INTEGER PRIMARY KEY,
  applied_at TEXT DEFAULT (datetime('now'))
);`

// ---------------------------------------------------------------------------
// Indexes
// ---------------------------------------------------------------------------

var createIndexes = []string{
	"CREATE INDEX IF NOT EXISTS idx_notes_short_id ON notes(short_id);",
	"CREATE INDEX IF NOT EXISTS idx_notes_type ON notes(type);",
	"CREATE INDEX IF NOT EXISTS idx_notes_status ON notes(status);",
	"CREATE INDEX IF NOT EXISTS idx_notes_project ON notes(project_id);",
	"CREATE INDEX IF NOT EXISTS idx_notes_feature ON notes(feature_id);",
	"CREATE INDEX IF NOT EXISTS idx_links_source ON links(source_id);",
	"CREATE INDEX IF NOT EXISTS idx_links_target ON links(target_id);",
	"CREATE INDEX IF NOT EXISTS idx_links_target_path ON links(target_path);",
	"CREATE INDEX IF NOT EXISTS idx_tags_note ON tags(note_id);",
	"CREATE INDEX IF NOT EXISTS idx_tags_tag ON tags(tag);",
}

// ---------------------------------------------------------------------------
// FTS5
// ---------------------------------------------------------------------------

const createFTS5Table = `
CREATE VIRTUAL TABLE IF NOT EXISTS notes_fts USING fts5(
  title, body, path,
  content=notes, content_rowid=id,
  tokenize='porter unicode61'
);`

// ---------------------------------------------------------------------------
// FTS5 sync triggers
// ---------------------------------------------------------------------------

const createTriggerAfterInsert = `
CREATE TRIGGER IF NOT EXISTS notes_ai AFTER INSERT ON notes BEGIN
  INSERT INTO notes_fts(rowid, title, body, path) VALUES (new.id, new.title, new.body, new.path);
END;`

const createTriggerAfterDelete = `
CREATE TRIGGER IF NOT EXISTS notes_ad AFTER DELETE ON notes BEGIN
  INSERT INTO notes_fts(notes_fts, rowid, title, body, path) VALUES('delete', old.id, old.title, old.body, old.path);
END;`

const createTriggerAfterUpdate = `
CREATE TRIGGER IF NOT EXISTS notes_au AFTER UPDATE ON notes BEGIN
  INSERT INTO notes_fts(notes_fts, rowid, title, body, path) VALUES('delete', old.id, old.title, old.body, old.path);
  INSERT INTO notes_fts(rowid, title, body, path) VALUES (new.id, new.title, new.body, new.path);
END;`

// ---------------------------------------------------------------------------
// Schema initialization
// ---------------------------------------------------------------------------

// InitSchema creates all tables, indexes, FTS5 virtual table, and triggers.
// It is idempotent — safe to call multiple times.
func InitSchema(db *sql.DB) error {
	// Tables (order matters for foreign keys)
	tables := []string{
		createNotesTable,
		createLinksTable,
		createTagsTable,
		createEntryMetaTable,
		createGeneratedTasksTable,
		createSchemaVersionTable,
	}
	for _, ddl := range tables {
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("create table: %w", err)
		}
	}

	// Indexes
	for _, ddl := range createIndexes {
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("create index: %w", err)
		}
	}

	// FTS5 virtual table
	if _, err := db.Exec(createFTS5Table); err != nil {
		return fmt.Errorf("create FTS5 table: %w", err)
	}

	// FTS5 sync triggers
	triggers := []string{
		createTriggerAfterInsert,
		createTriggerAfterDelete,
		createTriggerAfterUpdate,
	}
	for _, ddl := range triggers {
		if _, err := db.Exec(ddl); err != nil {
			return fmt.Errorf("create trigger: %w", err)
		}
	}

	// Set schema version (idempotent: INSERT OR REPLACE)
	if _, err := db.Exec(
		"INSERT OR REPLACE INTO schema_version (version) VALUES (?)",
		CurrentSchemaVersion,
	); err != nil {
		return fmt.Errorf("set schema version: %w", err)
	}

	return nil
}

// GetSchemaVersion returns the highest schema version, or 0 if none set.
func GetSchemaVersion(db *sql.DB) (int, error) {
	var version int
	err := db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_version").Scan(&version)
	if err != nil {
		return 0, fmt.Errorf("get schema version: %w", err)
	}
	return version, nil
}

// SetSchemaVersion records a schema version.
func SetSchemaVersion(db *sql.DB, version int) error {
	_, err := db.Exec(
		"INSERT OR REPLACE INTO schema_version (version) VALUES (?)",
		version,
	)
	if err != nil {
		return fmt.Errorf("set schema version: %w", err)
	}
	return nil
}

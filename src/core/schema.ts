/**
 * Brain API - Unified Database Schema
 *
 * Replaces both ZK's internal .zk/zk.db and the existing living-brain.db.
 * Provides full-text search via FTS5 with BM25 ranking.
 */

import { Database } from "bun:sqlite";

// =============================================================================
// Schema Version
// =============================================================================

export const SCHEMA_VERSION = 1;

// =============================================================================
// DDL Constants
// =============================================================================

const NOTES_TABLE = `
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
)`;

const NOTES_INDEXES = [
  "CREATE INDEX IF NOT EXISTS idx_notes_short_id ON notes(short_id)",
  "CREATE INDEX IF NOT EXISTS idx_notes_type ON notes(type)",
  "CREATE INDEX IF NOT EXISTS idx_notes_status ON notes(status)",
  "CREATE INDEX IF NOT EXISTS idx_notes_project ON notes(project_id)",
  "CREATE INDEX IF NOT EXISTS idx_notes_feature ON notes(feature_id)",
];

const NOTES_FTS = `
CREATE VIRTUAL TABLE IF NOT EXISTS notes_fts USING fts5(
  title,
  body,
  path,
  content=notes,
  content_rowid=id,
  tokenize='porter unicode61'
)`;

const LINKS_TABLE = `
CREATE TABLE IF NOT EXISTS links (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  target_path TEXT NOT NULL,
  target_id INTEGER REFERENCES notes(id) ON DELETE SET NULL,
  title TEXT DEFAULT '',
  href TEXT NOT NULL,
  type TEXT DEFAULT 'markdown',
  snippet TEXT DEFAULT ''
)`;

const LINKS_INDEXES = [
  "CREATE INDEX IF NOT EXISTS idx_links_source ON links(source_id)",
  "CREATE INDEX IF NOT EXISTS idx_links_target ON links(target_id)",
  "CREATE INDEX IF NOT EXISTS idx_links_target_path ON links(target_path)",
];

const TAGS_TABLE = `
CREATE TABLE IF NOT EXISTS tags (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  note_id INTEGER NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
  tag TEXT NOT NULL
)`;

const TAGS_INDEXES = [
  "CREATE INDEX IF NOT EXISTS idx_tags_note ON tags(note_id)",
  "CREATE INDEX IF NOT EXISTS idx_tags_tag ON tags(tag)",
];

const ENTRY_META_TABLE = `
CREATE TABLE IF NOT EXISTS entry_meta (
  path TEXT PRIMARY KEY,
  project_id TEXT,
  access_count INTEGER DEFAULT 0,
  last_accessed TEXT,
  last_verified TEXT,
  created_at TEXT DEFAULT (datetime('now'))
)`;

const GENERATED_TASKS_TABLE = `
CREATE TABLE IF NOT EXISTS generated_tasks (
  key TEXT PRIMARY KEY,
  task_path TEXT NOT NULL,
  feature_id TEXT,
  created_at TEXT DEFAULT (datetime('now'))
)`;

const SCHEMA_VERSION_TABLE = `
CREATE TABLE IF NOT EXISTS schema_version (
  version INTEGER PRIMARY KEY,
  applied_at TEXT DEFAULT (datetime('now'))
)`;

// =============================================================================
// FTS5 Trigger Helpers
// =============================================================================

/**
 * Check if a trigger exists in the database.
 */
function triggerExists(db: Database, name: string): boolean {
  const row = db
    .prepare(
      "SELECT name FROM sqlite_master WHERE type = 'trigger' AND name = ?"
    )
    .get(name) as { name: string } | null;
  return row !== null;
}

/**
 * Create FTS5 sync triggers if they don't already exist.
 * SQLite doesn't support IF NOT EXISTS for triggers, so we check manually.
 */
function createFtsTriggers(db: Database): void {
  if (!triggerExists(db, "notes_ai")) {
    db.exec(`
      CREATE TRIGGER notes_ai AFTER INSERT ON notes BEGIN
        INSERT INTO notes_fts(rowid, title, body, path)
        VALUES (new.id, new.title, new.body, new.path);
      END
    `);
  }

  if (!triggerExists(db, "notes_ad")) {
    db.exec(`
      CREATE TRIGGER notes_ad AFTER DELETE ON notes BEGIN
        INSERT INTO notes_fts(notes_fts, rowid, title, body, path)
        VALUES('delete', old.id, old.title, old.body, old.path);
      END
    `);
  }

  if (!triggerExists(db, "notes_au")) {
    db.exec(`
      CREATE TRIGGER notes_au AFTER UPDATE ON notes BEGIN
        INSERT INTO notes_fts(notes_fts, rowid, title, body, path)
        VALUES('delete', old.id, old.title, old.body, old.path);
        INSERT INTO notes_fts(rowid, title, body, path)
        VALUES (new.id, new.title, new.body, new.path);
      END
    `);
  }
}

// =============================================================================
// Schema Functions
// =============================================================================

/**
 * Create all tables, indexes, FTS5 virtual table, and triggers from scratch.
 * Wraps everything in a transaction for atomicity.
 */
export function createSchema(db: Database): void {
  db.exec("PRAGMA foreign_keys = ON");

  // Create tables, indexes, and FTS5 virtual table in a transaction
  db.transaction(() => {
    // Notes table and indexes
    db.exec(NOTES_TABLE);
    for (const idx of NOTES_INDEXES) {
      db.exec(idx);
    }

    // FTS5 virtual table
    db.exec(NOTES_FTS);

    // Links table and indexes
    db.exec(LINKS_TABLE);
    for (const idx of LINKS_INDEXES) {
      db.exec(idx);
    }

    // Tags table and indexes
    db.exec(TAGS_TABLE);
    for (const idx of TAGS_INDEXES) {
      db.exec(idx);
    }

    // Entry metadata
    db.exec(ENTRY_META_TABLE);

    // Generated tasks
    db.exec(GENERATED_TASKS_TABLE);

    // Schema version tracking
    db.exec(SCHEMA_VERSION_TABLE);

    // Record schema version
    db.exec(
      `INSERT OR IGNORE INTO schema_version (version) VALUES (${SCHEMA_VERSION})`
    );
  })();

  // FTS5 sync triggers must be created outside the transaction
  // (SQLite trigger creation via bun:sqlite requires separate execution)
  createFtsTriggers(db);
}

/**
 * Get the current schema version.
 * Returns 0 if no schema_version table exists (fresh database).
 */
export function getSchemaVersion(db: Database): number {
  // Check if schema_version table exists
  const table = db
    .prepare(
      "SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'schema_version'"
    )
    .get() as { name: string } | null;

  if (!table) {
    return 0;
  }

  const row = db
    .prepare("SELECT MAX(version) as version FROM schema_version")
    .get() as { version: number | null } | null;

  return row?.version ?? 0;
}

/**
 * Apply any needed migrations by checking current version vs SCHEMA_VERSION.
 * If no schema exists (version 0), creates everything from scratch.
 * Safe to call multiple times (idempotent).
 */
export function migrateSchema(db: Database): void {
  const currentVersion = getSchemaVersion(db);

  if (currentVersion >= SCHEMA_VERSION) {
    // Already up to date
    return;
  }

  if (currentVersion === 0) {
    // Fresh database — create everything
    createSchema(db);
    return;
  }

  // Future migrations would go here:
  // if (currentVersion < 2) { migrateV1toV2(db); }
  // if (currentVersion < 3) { migrateV2toV3(db); }
}

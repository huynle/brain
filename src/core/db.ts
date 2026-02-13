/**
 * Brain API - Database Layer
 *
 * SQLite database operations for brain entry metadata tracking.
 * Ported from OpenCode brain plugin.
 */

import { Database } from "bun:sqlite";
import { existsSync, mkdirSync } from "fs";
import { dirname } from "path";
import { getConfig } from "../config";
import type { EntryMeta } from "./types";

// =============================================================================
// Constants
// =============================================================================

const MS_PER_DAY = 24 * 60 * 60 * 1000;

// =============================================================================
// Database Singleton
// =============================================================================

let db: Database | null = null;

/**
 * Initialize the SQLite database with WAL mode.
 * Creates the database file and directory if they don't exist.
 */
export function initDatabase(): Database {
  if (db) return db;

  const config = getConfig();
  const dbPath = config.brain.dbPath;
  const dbDir = dirname(dbPath);

  // Ensure directory exists
  if (!existsSync(dbDir)) {
    mkdirSync(dbDir, { recursive: true });
  }

  // Create database with WAL mode for better concurrent performance
  db = new Database(dbPath, { create: true });
  db.run("PRAGMA journal_mode = WAL");
  db.run("PRAGMA synchronous = NORMAL");

  // Create entries_meta table
  db.run(`
    CREATE TABLE IF NOT EXISTS entries_meta (
      path TEXT PRIMARY KEY,
      project_id TEXT NOT NULL,
      access_count INTEGER DEFAULT 0,
      accessed_at INTEGER,
      last_verified INTEGER,
      created_at INTEGER NOT NULL
    )
  `);

  // Create indexes for common queries
  db.run(
    "CREATE INDEX IF NOT EXISTS idx_entries_project ON entries_meta(project_id)"
  );
  db.run(
    "CREATE INDEX IF NOT EXISTS idx_entries_accessed ON entries_meta(accessed_at)"
  );
  db.run(
    "CREATE INDEX IF NOT EXISTS idx_entries_verified ON entries_meta(last_verified)"
  );

  // OAuth 2.1 tables for MCP authentication
  db.run(`
    CREATE TABLE IF NOT EXISTS oauth_clients (
      client_id TEXT PRIMARY KEY,
      client_secret TEXT NOT NULL,
      redirect_uris TEXT NOT NULL,
      client_name TEXT,
      client_uri TEXT,
      logo_uri TEXT,
      scope TEXT,
      grant_types TEXT NOT NULL,
      response_types TEXT NOT NULL,
      token_endpoint_auth_method TEXT NOT NULL DEFAULT 'client_secret_post',
      created_at INTEGER NOT NULL
    )
  `);

  db.run(`
    CREATE TABLE IF NOT EXISTS oauth_auth_codes (
      code TEXT PRIMARY KEY,
      client_id TEXT NOT NULL,
      redirect_uri TEXT NOT NULL,
      scope TEXT,
      code_challenge TEXT NOT NULL,
      code_challenge_method TEXT NOT NULL DEFAULT 'S256',
      user_id TEXT,
      expires_at INTEGER NOT NULL,
      created_at INTEGER NOT NULL,
      FOREIGN KEY (client_id) REFERENCES oauth_clients(client_id)
    )
  `);

  db.run(`
    CREATE TABLE IF NOT EXISTS oauth_access_tokens (
      token TEXT PRIMARY KEY,
      client_id TEXT NOT NULL,
      scope TEXT,
      user_id TEXT,
      expires_at INTEGER NOT NULL,
      created_at INTEGER NOT NULL,
      FOREIGN KEY (client_id) REFERENCES oauth_clients(client_id)
    )
  `);

  db.run(`
    CREATE TABLE IF NOT EXISTS oauth_refresh_tokens (
      token TEXT PRIMARY KEY,
      client_id TEXT NOT NULL,
      scope TEXT,
      user_id TEXT,
      expires_at INTEGER NOT NULL,
      created_at INTEGER NOT NULL,
      FOREIGN KEY (client_id) REFERENCES oauth_clients(client_id)
    )
  `);

  // OAuth indexes
  db.run(
    "CREATE INDEX IF NOT EXISTS idx_oauth_tokens_client ON oauth_access_tokens(client_id)"
  );
  db.run(
    "CREATE INDEX IF NOT EXISTS idx_oauth_tokens_expires ON oauth_access_tokens(expires_at)"
  );
  db.run(
    "CREATE INDEX IF NOT EXISTS idx_oauth_codes_expires ON oauth_auth_codes(expires_at)"
  );

  return db;
}

/**
 * Get the database singleton, initializing if needed.
 */
export function getDb(): Database {
  return initDatabase();
}

/**
 * Close the database connection.
 * Call this during shutdown for clean cleanup.
 */
export function closeDatabase(): void {
  if (db) {
    db.close();
    db = null;
  }
}

// =============================================================================
// Entry Metadata Operations
// =============================================================================

/**
 * Get metadata for an entry by path.
 * Returns null if the entry doesn't exist in the metadata table.
 */
export function getEntryMeta(path: string): EntryMeta | null {
  const database = getDb();
  const row = database
    .prepare("SELECT * FROM entries_meta WHERE path = ?")
    .get(path) as EntryMeta | undefined;
  return row || null;
}

/**
 * Record an access to an entry, incrementing its access count.
 * Creates the entry if it doesn't exist.
 */
export function recordAccess(path: string): void {
  const database = getDb();
  const now = Date.now();

  const result = database.run(
    "UPDATE entries_meta SET access_count = access_count + 1, accessed_at = ? WHERE path = ?",
    [now, path]
  );

  // If no rows updated, entry doesn't exist - create it
  if (result.changes === 0) {
    database.run(
      "INSERT INTO entries_meta (path, project_id, access_count, accessed_at, created_at) VALUES (?, 'unknown', 1, ?, ?)",
      [path, now, now]
    );
  }
}

/**
 * Initialize metadata for a new entry.
 * Uses INSERT OR IGNORE to avoid overwriting existing entries.
 */
export function initEntry(path: string, projectId: string): void {
  const database = getDb();
  const now = Date.now();
  database.run(
    "INSERT OR IGNORE INTO entries_meta (path, project_id, access_count, accessed_at, last_verified, created_at) VALUES (?, ?, 0, NULL, NULL, ?)",
    [path, projectId, now]
  );
}

/**
 * Mark an entry as verified (still accurate).
 * Creates the entry if it doesn't exist.
 */
export function setVerified(path: string): void {
  const database = getDb();
  const now = Date.now();

  const result = database.run(
    "UPDATE entries_meta SET last_verified = ? WHERE path = ?",
    [now, path]
  );

  // If no rows updated, entry doesn't exist - create it
  if (result.changes === 0) {
    database.run(
      "INSERT INTO entries_meta (path, project_id, access_count, accessed_at, last_verified, created_at) VALUES (?, 'unknown', 0, NULL, ?, ?)",
      [path, now, now]
    );
  }
}

/**
 * Get entries that haven't been verified in N days.
 * Returns paths of entries that may need review.
 */
export function getStaleEntries(days: number): string[] {
  const database = getDb();
  const threshold = Date.now() - days * MS_PER_DAY;

  const rows = database
    .prepare(
      "SELECT path FROM entries_meta WHERE last_verified IS NULL OR last_verified < ?"
    )
    .all(threshold) as { path: string }[];

  return rows.map((row) => row.path);
}

// =============================================================================
// Additional Query Helpers
// =============================================================================

/**
 * Get all entries for a specific project.
 */
export function getEntriesByProject(projectId: string): EntryMeta[] {
  const database = getDb();
  return database
    .prepare("SELECT * FROM entries_meta WHERE project_id = ?")
    .all(projectId) as EntryMeta[];
}

/**
 * Get total count of tracked entries.
 */
export function getTrackedEntryCount(): number {
  const database = getDb();
  const result = database
    .prepare("SELECT COUNT(*) as count FROM entries_meta")
    .get() as { count: number };
  return result.count;
}

/**
 * Delete entry metadata by path.
 */
export function deleteEntryMeta(path: string): boolean {
  const database = getDb();
  const result = database.run("DELETE FROM entries_meta WHERE path = ?", [
    path,
  ]);
  return result.changes > 0;
}

/**
 * Update the project_id for an entry.
 */
export function updateEntryProject(path: string, projectId: string): boolean {
  const database = getDb();
  const result = database.run(
    "UPDATE entries_meta SET project_id = ? WHERE path = ?",
    [projectId, path]
  );
  return result.changes > 0;
}

/**
 * Move an entry to a new path and project atomically.
 * Deletes old row and inserts new row in a transaction.
 * Returns true if successful, false if old path not found.
 */
export function moveEntryMeta(
  oldPath: string,
  newPath: string,
  newProjectId: string
): boolean {
  const database = getDb();

  // Get existing metadata
  const existing = getEntryMeta(oldPath);
  if (!existing) {
    return false;
  }

  const now = Date.now();

  // Use a transaction to ensure atomicity
  const moveTransaction = database.transaction(() => {
    // Delete old entry
    database.run("DELETE FROM entries_meta WHERE path = ?", [oldPath]);

    // Insert new entry with preserved metadata
    database.run(
      `INSERT INTO entries_meta (path, project_id, access_count, accessed_at, last_verified, created_at) 
       VALUES (?, ?, ?, ?, ?, ?)`,
      [
        newPath,
        newProjectId,
        existing.access_count,
        existing.accessed_at,
        existing.last_verified,
        existing.created_at,
      ]
    );
  });

  moveTransaction();
  return true;
}

/**
 * Brain API - Database Migration Module
 *
 * Imports data from legacy databases (living-brain.db, zk.db) into
 * the unified brain.db schema.
 *
 * Phase 1: living-brain.db import (entry_meta + generated_tasks)
 * Phase 2: ZK database import (notes, links, tags) — future
 * Phase 3: Auto-detection, rebuild, dry-run — future
 * Phase 4: CLI command — future
 */

import type { Database } from "bun:sqlite";

// =============================================================================
// Types
// =============================================================================

export interface MigrationResult {
  strategy: "import" | "rebuild";
  notes: number;
  links: number;
  tags: number;
  entryMeta: number;
  generatedTasks: number;
  errors: string[];
  duration: number;
}

// =============================================================================
// Source Row Interfaces (legacy living-brain.db)
// =============================================================================

interface LegacyEntryMetaRow {
  path: string;
  project_id: string;
  access_count: number;
  accessed_at: number | null;
  last_verified: number | null;
  created_at: number;
}

interface LegacyGeneratedTaskRow {
  project_id: string;
  generated_key: string;
  task_path: string;
  created_at: number;
}

// =============================================================================
// Helpers
// =============================================================================

/**
 * Convert a Unix millisecond timestamp to an ISO-style datetime string
 * compatible with SQLite's datetime format: "YYYY-MM-DD HH:MM:SS".
 *
 * Returns null for null, undefined, or 0 inputs.
 */
export function unixMillisToIso(ms: number | null): string | null {
  if (ms == null || ms === 0) {
    return null;
  }
  const date = new Date(ms);
  // Format as "YYYY-MM-DD HH:MM:SS" (SQLite datetime format)
  const year = date.getUTCFullYear();
  const month = String(date.getUTCMonth() + 1).padStart(2, "0");
  const day = String(date.getUTCDate()).padStart(2, "0");
  const hours = String(date.getUTCHours()).padStart(2, "0");
  const minutes = String(date.getUTCMinutes()).padStart(2, "0");
  const seconds = String(date.getUTCSeconds()).padStart(2, "0");
  return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`;
}

// =============================================================================
// DatabaseMigration
// =============================================================================

export class DatabaseMigration {
  /**
   * Migrate entries_meta from living-brain.db to entry_meta in brain.db.
   * Converts INTEGER timestamps to ISO TEXT format.
   * Uses INSERT OR IGNORE for idempotency.
   *
   * @returns Number of rows read from source
   */
  migrateEntryMeta(sourceDb: Database, targetDb: Database): number {
    const rows = sourceDb
      .prepare("SELECT path, project_id, access_count, accessed_at, last_verified, created_at FROM entries_meta")
      .all() as LegacyEntryMetaRow[];

    if (rows.length === 0) {
      return 0;
    }

    const insertStmt = targetDb.prepare(
      `INSERT OR IGNORE INTO entry_meta (path, project_id, access_count, last_accessed, last_verified, created_at)
       VALUES (?, ?, ?, ?, ?, ?)`
    );

    targetDb.transaction(() => {
      for (const row of rows) {
        insertStmt.run(
          row.path,
          row.project_id,
          row.access_count,
          unixMillisToIso(row.accessed_at),
          unixMillisToIso(row.last_verified),
          unixMillisToIso(row.created_at)
        );
      }
    })();

    return rows.length;
  }

  /**
   * Migrate generated_task_keys from living-brain.db to generated_tasks in brain.db.
   * Only migrates rows where task_path IS NOT NULL.
   * Transforms compound PK (project_id, generated_key) to single key "project_id:generated_key".
   * Uses INSERT OR IGNORE for idempotency.
   *
   * @returns Number of rows read from source (with task_path)
   */
  migrateGeneratedTasks(sourceDb: Database, targetDb: Database): number {
    const rows = sourceDb
      .prepare(
        "SELECT project_id, generated_key, task_path, created_at FROM generated_task_keys WHERE task_path IS NOT NULL"
      )
      .all() as LegacyGeneratedTaskRow[];

    if (rows.length === 0) {
      return 0;
    }

    const insertStmt = targetDb.prepare(
      `INSERT OR IGNORE INTO generated_tasks (key, task_path, feature_id, created_at)
       VALUES (?, ?, ?, ?)`
    );

    targetDb.transaction(() => {
      for (const row of rows) {
        const compositeKey = `${row.project_id}:${row.generated_key}`;
        insertStmt.run(
          compositeKey,
          row.task_path,
          null, // feature_id not present in legacy schema
          unixMillisToIso(row.created_at)
        );
      }
    })();

    return rows.length;
  }

  /**
   * Orchestrate migration from a living-brain.db source to the unified brain.db target.
   * Wraps both sub-migrations and captures errors.
   *
   * @param sourceDb - Open Database handle to living-brain.db
   * @param targetDb - Open Database handle to brain.db (schema already created)
   * @returns MigrationResult with counts and any errors
   */
  migrateFromLivingBrainDb(sourceDb: Database, targetDb: Database): MigrationResult {
    const start = performance.now();
    const result: MigrationResult = {
      strategy: "import",
      notes: 0,
      links: 0,
      tags: 0,
      entryMeta: 0,
      generatedTasks: 0,
      errors: [],
      duration: 0,
    };

    try {
      result.entryMeta = this.migrateEntryMeta(sourceDb, targetDb);
    } catch (err) {
      result.errors.push(`entry_meta migration failed: ${err instanceof Error ? err.message : String(err)}`);
    }

    try {
      result.generatedTasks = this.migrateGeneratedTasks(sourceDb, targetDb);
    } catch (err) {
      result.errors.push(`generated_tasks migration failed: ${err instanceof Error ? err.message : String(err)}`);
    }

    result.duration = Math.round(performance.now() - start);
    return result;
  }
}

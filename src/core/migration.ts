/**
 * Brain API - Database Migration Module
 *
 * Imports data from legacy databases (living-brain.db, zk.db) into
 * the unified brain.db schema.
 *
 * Phase 1: living-brain.db import (entry_meta + generated_tasks)
 * Phase 2: ZK database import (notes, links, tags)
 * Phase 3: Auto-detection, rebuild, dry-run
 * Phase 4: CLI command (src/cli/brain-migrate.ts)
 */

import { Database } from "bun:sqlite";
import { existsSync } from "fs";
import { join } from "path";
import { Glob } from "bun";
import { Indexer, type IndexResult } from "./indexer";
import { parseFile } from "./file-parser";
import { createStorageLayer } from "./storage";

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

export interface MigrationOptions {
  /** When true, count what would be migrated without writing to target DB. */
  dryRun?: boolean;
  /** When true, always use rebuild-from-disk even if .zk/zk.db exists. */
  forceRebuild?: boolean;
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
// Source Row Interfaces (ZK .zk/zk.db)
// =============================================================================

interface ZkNoteRow {
  id: number;
  path: string;
  title: string;
  lead: string;
  body: string;
  raw_content: string;
  word_count: number;
  metadata: string;
  checksum: string | null;
  created: string | null;
  modified: string | null;
}

interface ZkLinkRow {
  id: number;
  source_id: number;
  target_id: number | null;
  title: string;
  href: string;
  type: string;
  external: number;
  snippet: string;
}

interface ZkTagRow {
  note_id: number;
  tag_name: string;
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
// Path Helpers
// =============================================================================

/**
 * Extract the short ID from a note path.
 * e.g., "projects/test/plan/abc12def.md" → "abc12def"
 *
 * Same logic as extractIdFromPath in zk-client.ts, inlined here
 * to avoid importing the full zk-client module (which has runtime deps).
 */
function extractShortId(path: string): string {
  const filename = path.split("/").pop() || path;
  return filename.replace(/\.md$/, "");
}

/**
 * Safely parse a JSON metadata string, returning null on failure.
 */
function parseMetadataJson(raw: string): Record<string, unknown> | null {
  try {
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed === "object" && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>;
    }
    return null;
  } catch {
    return null;
  }
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

  /**
   * Migrate notes, links, and tags from a ZK database (.zk/zk.db) into brain.db.
   *
   * Transformations:
   * - Notes: extracts short_id from path, parses metadata JSON for type/status/priority/project_id/feature_id
   * - Links: maps ZK integer IDs to brain IDs via path lookup, resolves target_path from ZK notes
   * - Tags: joins collections (kind='tag') with notes_collections, maps note_id to brain IDs
   *
   * Uses INSERT OR IGNORE for notes (idempotent on path uniqueness).
   * Only migrates internal links (external = 0 or NULL).
   *
   * @param sourceDb - Open Database handle to .zk/zk.db (read-only recommended)
   * @param targetDb - Open Database handle to brain.db (schema already created)
   * @returns MigrationResult with counts and any errors
   */
  migrateFromZkDb(sourceDb: Database, targetDb: Database): MigrationResult {
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

    // -------------------------------------------------------------------------
    // Step 1: Migrate notes
    // -------------------------------------------------------------------------
    const zkNotes = sourceDb
      .prepare(
        "SELECT id, path, title, lead, body, raw_content, word_count, metadata, checksum, created, modified FROM notes"
      )
      .all() as ZkNoteRow[];

    result.notes = zkNotes.length;

    if (zkNotes.length > 0) {
      const insertNote = targetDb.prepare(
        `INSERT OR IGNORE INTO notes
           (path, short_id, title, lead, body, raw_content, word_count, checksum, metadata, type, status, priority, project_id, feature_id, created, modified)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
      );

      targetDb.transaction(() => {
        for (const note of zkNotes) {
          const shortId = extractShortId(note.path);
          const meta = parseMetadataJson(note.metadata);

          insertNote.run(
            note.path,
            shortId,
            note.title,
            note.lead,
            note.body,
            note.raw_content,
            note.word_count,
            note.checksum,
            note.metadata, // preserve raw metadata string
            meta?.type as string ?? null,
            meta?.status as string ?? null,
            meta?.priority as string ?? null,
            meta?.project_id as string ?? null,
            meta?.feature_id as string ?? null,
            note.created,
            note.modified
          );
        }
      })();
    }

    // -------------------------------------------------------------------------
    // Step 2: Build ZK id → brain id mapping (and ZK id → path mapping)
    // -------------------------------------------------------------------------
    // Map ZK note IDs to their paths (from source)
    const zkIdToPath = new Map<number, string>();
    for (const note of zkNotes) {
      zkIdToPath.set(note.id, note.path);
    }

    // Map paths to brain note IDs (from target)
    const pathToBrainId = new Map<string, number>();
    if (zkNotes.length > 0) {
      const brainNotes = targetDb
        .prepare("SELECT id, path FROM notes")
        .all() as { id: number; path: string }[];
      for (const bn of brainNotes) {
        pathToBrainId.set(bn.path, bn.id);
      }
    }

    // Composite mapping: ZK id → brain id
    const zkIdToBrainId = new Map<number, number>();
    for (const [zkId, path] of zkIdToPath) {
      const brainId = pathToBrainId.get(path);
      if (brainId !== undefined) {
        zkIdToBrainId.set(zkId, brainId);
      }
    }

    // -------------------------------------------------------------------------
    // Step 3: Migrate internal links
    // -------------------------------------------------------------------------
    const zkLinks = sourceDb
      .prepare(
        "SELECT id, source_id, target_id, title, href, type, external, snippet FROM links WHERE external = 0 OR external IS NULL"
      )
      .all() as ZkLinkRow[];

    if (zkLinks.length > 0) {
      const insertLink = targetDb.prepare(
        `INSERT INTO links (source_id, target_path, target_id, title, href, type, snippet)
         VALUES (?, ?, ?, ?, ?, ?, ?)`
      );

      targetDb.transaction(() => {
        for (const link of zkLinks) {
          // Resolve source
          const brainSourceId = zkIdToBrainId.get(link.source_id);
          if (brainSourceId === undefined) {
            result.errors.push(
              `link id=${link.id}: source note ZK id=${link.source_id} not found in target`
            );
            continue;
          }

          // Resolve target
          if (link.target_id == null) {
            result.errors.push(
              `link id=${link.id}: target note ZK id=null not found in source`
            );
            continue;
          }

          const targetPath = zkIdToPath.get(link.target_id);
          if (targetPath === undefined) {
            result.errors.push(
              `link id=${link.id}: target note ZK id=${link.target_id} not found in source`
            );
            continue;
          }

          const brainTargetId = zkIdToBrainId.get(link.target_id);
          if (brainTargetId === undefined) {
            result.errors.push(
              `link id=${link.id}: target note ZK id=${link.target_id} not found in target`
            );
            continue;
          }

          insertLink.run(
            brainSourceId,
            targetPath,
            brainTargetId,
            link.title,
            link.href,
            link.type || "markdown",
            link.snippet
          );
          result.links++;
        }
      })();
    }

    // -------------------------------------------------------------------------
    // Step 4: Migrate tags (collections + notes_collections)
    // -------------------------------------------------------------------------
    const zkTags = sourceDb
      .prepare(
        `SELECT nc.note_id, c.name AS tag_name
         FROM notes_collections nc
         JOIN collections c ON c.id = nc.collection_id
         WHERE c.kind = 'tag'`
      )
      .all() as ZkTagRow[];

    if (zkTags.length > 0) {
      const insertTag = targetDb.prepare(
        "INSERT INTO tags (note_id, tag) VALUES (?, ?)"
      );

      targetDb.transaction(() => {
        for (const tag of zkTags) {
          const brainNoteId = zkIdToBrainId.get(tag.note_id);
          if (brainNoteId === undefined) {
            result.errors.push(
              `tag "${tag.tag_name}": note ZK id=${tag.note_id} not found in target`
            );
            continue;
          }

          insertTag.run(brainNoteId, tag.tag_name);
          result.tags++;
        }
      })();
    }

    result.duration = Math.round(performance.now() - start);
    return result;
  }

  // ===========================================================================
  // Phase 3: Rebuild from disk
  // ===========================================================================

  /**
   * Rebuild the target database by indexing all .md files from disk.
   *
   * Creates a StorageLayer for the target DB, uses the Indexer to parse
   * and insert all markdown files, then closes the storage.
   *
   * @param brainDir - Absolute path to the brain root directory
   * @param targetDbPath - Path to the target brain.db file (will be created)
   * @returns MigrationResult with strategy "rebuild" and counts
   */
  async rebuildFromDisk(brainDir: string, targetDbPath: string): Promise<MigrationResult> {
    const start = performance.now();
    const result: MigrationResult = {
      strategy: "rebuild",
      notes: 0,
      links: 0,
      tags: 0,
      entryMeta: 0,
      generatedTasks: 0,
      errors: [],
      duration: 0,
    };

    const storage = createStorageLayer(targetDbPath);
    try {
      const indexer = new Indexer(brainDir, storage, parseFile);
      const indexResult: IndexResult = await indexer.rebuildAll();

      result.notes = indexResult.added;
      result.errors = indexResult.errors.map((e) => `${e.path}: ${e.error}`);
    } finally {
      storage.close();
    }

    result.duration = Math.round(performance.now() - start);
    return result;
  }

  // ===========================================================================
  // Phase 3: Auto-detection
  // ===========================================================================

  /**
   * Auto-detect the best migration strategy and execute it.
   *
   * Detection logic:
   * 1. If .zk/zk.db exists and is valid SQLite → import from ZK
   * 2. Else → rebuild from disk
   * 3. Always import living-brain.db metadata if it exists
   *
   * Options:
   * - dryRun: Count what would be migrated without writing
   * - forceRebuild: Always use rebuild even if .zk/zk.db exists
   *
   * @param brainDir - Absolute path to the brain root directory
   * @param targetDbPath - Path to the target brain.db file
   * @param options - Migration options
   * @returns Combined MigrationResult
   */
  async autoMigrate(
    brainDir: string,
    targetDbPath: string,
    options?: MigrationOptions
  ): Promise<MigrationResult> {
    const start = performance.now();
    const dryRun = options?.dryRun ?? false;
    const forceRebuild = options?.forceRebuild ?? false;

    const zkDbPath = join(brainDir, ".zk", "zk.db");
    const livingBrainDbPath = join(brainDir, "living-brain.db");

    const hasZkDb = existsSync(zkDbPath) && this.isValidSqlite(zkDbPath);
    const hasLivingBrainDb = existsSync(livingBrainDbPath);
    const useZkImport = hasZkDb && !forceRebuild;

    const result: MigrationResult = {
      strategy: useZkImport ? "import" : "rebuild",
      notes: 0,
      links: 0,
      tags: 0,
      entryMeta: 0,
      generatedTasks: 0,
      errors: [],
      duration: 0,
    };

    if (dryRun) {
      // --- Dry-run mode: count without writing ---
      if (useZkImport) {
        this.dryRunCountZk(zkDbPath, result);
      } else {
        // Count .md files on disk (excluding .zk/)
        result.notes = this.countMarkdownFiles(brainDir);
      }

      if (hasLivingBrainDb) {
        this.dryRunCountLivingBrain(livingBrainDbPath, result);
      }

      result.duration = Math.round(performance.now() - start);
      return result;
    }

    // --- Actual migration ---
    if (useZkImport) {
      // Import from ZK database
      const sourceDb = new Database(zkDbPath, { readonly: true });
      const targetDb = this.openTargetDb(targetDbPath);
      try {
        const zkResult = this.migrateFromZkDb(sourceDb, targetDb);
        result.notes = zkResult.notes;
        result.links = zkResult.links;
        result.tags = zkResult.tags;
        result.errors.push(...zkResult.errors);
      } finally {
        sourceDb.close();
        targetDb.close();
      }
    } else {
      // Rebuild from disk
      const rebuildResult = await this.rebuildFromDisk(brainDir, targetDbPath);
      result.notes = rebuildResult.notes;
      result.errors.push(...rebuildResult.errors);
    }

    // Always import living-brain.db metadata if it exists
    if (hasLivingBrainDb) {
      const sourceDb = new Database(livingBrainDbPath, { readonly: true });
      const targetDb = this.openTargetDb(targetDbPath);
      try {
        const lbResult = this.migrateFromLivingBrainDb(sourceDb, targetDb);
        result.entryMeta = lbResult.entryMeta;
        result.generatedTasks = lbResult.generatedTasks;
        result.errors.push(...lbResult.errors);
      } finally {
        sourceDb.close();
        targetDb.close();
      }
    }

    result.duration = Math.round(performance.now() - start);
    return result;
  }

  // ===========================================================================
  // Private helpers
  // ===========================================================================

  /**
   * Check if a file is a valid SQLite database by attempting to open it.
   */
  private isValidSqlite(dbPath: string): boolean {
    try {
      const db = new Database(dbPath, { readonly: true });
      db.prepare("SELECT 1").get();
      db.close();
      return true;
    } catch {
      return false;
    }
  }

  /**
   * Open (or create) the target database with schema applied.
   */
  private openTargetDb(targetDbPath: string): Database {
    const storage = createStorageLayer(targetDbPath);
    return storage.getDb();
  }

  /**
   * Count .md files in brainDir, excluding .zk/ directory.
   */
  private countMarkdownFiles(brainDir: string): number {
    const glob = new Glob("**/*.md");
    let count = 0;
    for (const file of glob.scanSync({ cwd: brainDir })) {
      if (!file.startsWith(".zk/")) {
        count++;
      }
    }
    return count;
  }

  /**
   * Dry-run: count rows in a ZK source database.
   */
  private dryRunCountZk(zkDbPath: string, result: MigrationResult): void {
    const db = new Database(zkDbPath, { readonly: true });
    try {
      const noteCount = db.prepare("SELECT COUNT(*) as count FROM notes").get() as { count: number };
      result.notes = noteCount.count;

      const linkCount = db
        .prepare("SELECT COUNT(*) as count FROM links WHERE external = 0 OR external IS NULL")
        .get() as { count: number };
      result.links = linkCount.count;

      const tagCount = db
        .prepare(
          `SELECT COUNT(*) as count FROM notes_collections nc
           JOIN collections c ON c.id = nc.collection_id
           WHERE c.kind = 'tag'`
        )
        .get() as { count: number };
      result.tags = tagCount.count;
    } finally {
      db.close();
    }
  }

  /**
   * Dry-run: count rows in a living-brain.db source database.
   */
  private dryRunCountLivingBrain(lbDbPath: string, result: MigrationResult): void {
    const db = new Database(lbDbPath, { readonly: true });
    try {
      const metaCount = db
        .prepare("SELECT COUNT(*) as count FROM entries_meta")
        .get() as { count: number };
      result.entryMeta = metaCount.count;

      const taskCount = db
        .prepare("SELECT COUNT(*) as count FROM generated_task_keys WHERE task_path IS NOT NULL")
        .get() as { count: number };
      result.generatedTasks = taskCount.count;
    } finally {
      db.close();
    }
  }
}

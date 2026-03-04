/**
 * Brain API - Indexer
 *
 * Synchronizes markdown files on disk with the SQLite database.
 * Supports full rebuild, incremental updates, and single-file operations.
 */

import { Glob } from "bun";
import type { StorageLayer, NoteRow, LinkRow } from "./storage";
import type { ParsedFile } from "./file-parser";

// =============================================================================
// Types
// =============================================================================

export interface IndexResult {
  added: number;
  updated: number;
  deleted: number;
  skipped: number;
  errors: Array<{ path: string; error: string }>;
  duration: number;
}

// =============================================================================
// Internal Helpers
// =============================================================================

/** Map ParsedFile fields to NoteRow fields for DB insertion. */
function toNoteRow(pf: ParsedFile): Omit<NoteRow, "id" | "indexed_at"> {
  return {
    path: pf.path,
    short_id: pf.shortId,
    title: pf.title,
    lead: pf.lead,
    body: pf.body,
    raw_content: pf.rawContent,
    word_count: pf.wordCount,
    checksum: pf.checksum,
    metadata: JSON.stringify(pf.metadata),
    type: pf.type ?? null,
    status: pf.status ?? null,
    priority: pf.priority ?? null,
    project_id: pf.projectId ?? null,
    feature_id: pf.featureId ?? null,
    created: pf.created ?? null,
    modified: pf.modified ?? null,
  };
}

/** Map ExtractedLink[] to the shape expected by StorageLayer.setLinks(). */
function toLinkRows(
  links: ParsedFile["links"]
): Array<Omit<LinkRow, "id" | "source_id">> {
  return links.map((link) => ({
    target_path: link.href,
    target_id: null,
    title: link.title,
    href: link.href,
    type: link.type,
    snippet: link.snippet,
  }));
}

/** Upsert a parsed file into the DB: insert or update note, then replace tags and links. */
function upsertParsedFile(
  storage: StorageLayer,
  parsed: ParsedFile,
  exists: boolean
): void {
  if (exists) {
    storage.updateNote(parsed.path, toNoteRow(parsed));
  } else {
    storage.insertNote(toNoteRow(parsed));
  }
  storage.setTags(parsed.path, parsed.tags);
  storage.setLinks(parsed.path, toLinkRows(parsed.links));
}

/** Glob all .md files in brainDir, excluding .zk/ directory. */
function globMarkdownFiles(brainDir: string): string[] {
  const glob = new Glob("**/*.md");
  const files: string[] = [];
  for (const file of glob.scanSync({ cwd: brainDir })) {
    if (!file.startsWith(".zk/")) {
      files.push(file);
    }
  }
  return files;
}

// =============================================================================
// Indexer Class
// =============================================================================

export class Indexer {
  constructor(
    private brainDir: string,
    private storage: StorageLayer,
    private parser: (filePath: string, brainDir: string) => ParsedFile
  ) {}

  /**
   * Full rebuild: delete all existing data and re-index every .md file on disk.
   *
   * - Globs all .md files in brainDir, excluding .zk/ directory
   * - Parses each file with the injected parser
   * - Wraps all DB writes in a transaction
   * - Catches per-file errors without aborting the whole rebuild
   * - Returns stats about what was added/deleted/errored
   */
  async rebuildAll(): Promise<IndexResult> {
    const start = performance.now();
    const errors: IndexResult["errors"] = [];

    // 1. Discover files
    const files = globMarkdownFiles(this.brainDir);

    // 2. Parse all files, collecting results and errors
    const parsed: ParsedFile[] = [];
    for (const file of files) {
      try {
        parsed.push(this.parser(file, this.brainDir));
      } catch (err) {
        errors.push({
          path: file,
          error: err instanceof Error ? err.message : String(err),
        });
      }
    }

    // 3. Count existing notes before clearing (for deleted stat)
    const db = this.storage.getDb();
    const existingCount = (
      db.prepare("SELECT COUNT(*) as count FROM notes").get() as {
        count: number;
      }
    ).count;

    // 4. Transaction: clear all data, then insert parsed files
    db.transaction(() => {
      // Delete all existing notes (CASCADE cleans links and tags)
      db.prepare("DELETE FROM notes").run();

      for (const pf of parsed) {
        this.storage.insertNote(toNoteRow(pf));

        if (pf.tags.length > 0) {
          this.storage.setTags(pf.path, pf.tags);
        }

        if (pf.links.length > 0) {
          this.storage.setLinks(pf.path, toLinkRows(pf.links));
        }
      }
    })();

    return {
      added: parsed.length,
      updated: 0,
      deleted: existingCount,
      skipped: 0,
      errors,
      duration: performance.now() - start,
    };
  }

  /**
   * Incremental index: compare files on disk with DB, only process changes.
   *
   * - New files (on disk but not in DB): insert
   * - Modified files (checksum differs): update
   * - Unchanged files (checksum matches): skip
   * - Deleted files (in DB but not on disk): delete
   * - Per-file parse errors are caught and collected
   */
  async indexChanged(): Promise<IndexResult> {
    const start = performance.now();
    const errors: IndexResult["errors"] = [];
    let added = 0;
    let updated = 0;
    let deleted = 0;
    let skipped = 0;

    // 1. Discover files on disk
    const diskFiles = globMarkdownFiles(this.brainDir);
    const diskSet = new Set(diskFiles);

    // 2. Get all existing notes from DB (path + checksum)
    const db = this.storage.getDb();
    const dbRows = db
      .prepare("SELECT path, checksum FROM notes")
      .all() as { path: string; checksum: string | null }[];
    const dbMap = new Map<string, string | null>();
    for (const row of dbRows) {
      dbMap.set(row.path, row.checksum);
    }

    // 3. Process each file on disk
    for (const file of diskFiles) {
      try {
        const parsed = this.parser(file, this.brainDir);
        const existingChecksum = dbMap.get(file);

        if (existingChecksum === undefined) {
          // New file — not in DB
          upsertParsedFile(this.storage, parsed, false);
          added++;
        } else if (existingChecksum !== parsed.checksum) {
          // Modified file — checksum differs
          upsertParsedFile(this.storage, parsed, true);
          updated++;
        } else {
          // Unchanged — skip
          skipped++;
        }
      } catch (err) {
        errors.push({
          path: file,
          error: err instanceof Error ? err.message : String(err),
        });
      }
    }

    // 4. Delete DB entries with no corresponding file on disk
    for (const [dbPath] of dbMap) {
      if (!diskSet.has(dbPath)) {
        this.storage.deleteNote(dbPath);
        deleted++;
      }
    }

    return {
      added,
      updated,
      deleted,
      skipped,
      errors,
      duration: performance.now() - start,
    };
  }

  /**
   * Index a single file by relative path.
   *
   * - If note exists in DB: update it
   * - If note does not exist: insert it
   * - Always replaces tags and links
   */
  async indexFile(relativePath: string): Promise<void> {
    const parsed = this.parser(relativePath, this.brainDir);
    const exists = this.storage.getNoteByPath(relativePath) !== null;
    upsertParsedFile(this.storage, parsed, exists);
  }

  /**
   * Remove a single file from the index by relative path.
   * CASCADE handles cleaning up associated links and tags.
   */
  async removeFile(relativePath: string): Promise<void> {
    this.storage.deleteNote(relativePath);
  }

  /**
   * Get health statistics about the index.
   *
   * - totalFiles: count of .md files on disk (excluding .zk/)
   * - totalIndexed: count of notes in DB
   * - staleCount: DB entries with no corresponding file on disk
   */
  getHealth(): { totalFiles: number; totalIndexed: number; staleCount: number } {
    const diskFiles = globMarkdownFiles(this.brainDir);
    const diskSet = new Set(diskFiles);

    const db = this.storage.getDb();
    const totalIndexed = (
      db.prepare("SELECT COUNT(*) as count FROM notes").get() as {
        count: number;
      }
    ).count;

    const dbPaths = db
      .prepare("SELECT path FROM notes")
      .all() as { path: string }[];

    let staleCount = 0;
    for (const row of dbPaths) {
      if (!diskSet.has(row.path)) {
        staleCount++;
      }
    }

    return {
      totalFiles: diskFiles.length,
      totalIndexed,
      staleCount,
    };
  }
}

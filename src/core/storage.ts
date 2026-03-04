/**
 * Brain API - Storage Layer
 *
 * Provides all database operations needed by brain-service,
 * replacing ZK CLI calls with direct SQLite queries.
 *
 * Phase 1: Core skeleton + database initialization.
 * Phase 2: Note CRUD operations.
 * Phase 3: Tags, Links, and link resolution.
 * Phase 4: Search (FTS5, exact, like).
 * Phase 5: Filtering / List (dynamic WHERE, sort, limit).
 * Phase 6: Graph operations (backlinks, outlinks, related, orphans).
 * Phase 7: Entry metadata (access tracking, verification, stale detection).
 * Phase 8: Stats (aggregate counts, orphans, tracked, stale).
 */

import { Database } from "bun:sqlite";
import { readFileSync, existsSync } from "fs";
import { join } from "path";
import { Glob } from "bun";
import { migrateSchema } from "./schema";
import { parseFrontmatter, extractIdFromPath } from "./zk-client";

// =============================================================================
// Row Interfaces (internal to storage layer)
// =============================================================================

export interface NoteRow {
  id?: number;
  path: string;
  short_id: string;
  title: string;
  lead: string;
  body: string;
  raw_content: string;
  word_count: number;
  checksum: string | null;
  metadata: string; // JSON string
  type: string | null;
  status: string | null;
  priority: string | null;
  project_id: string | null;
  feature_id: string | null;
  created: string | null;
  modified: string | null;
  indexed_at?: string;
}

export interface LinkRow {
  id?: number;
  source_id: number;
  target_path: string;
  target_id: number | null;
  title: string;
  href: string;
  type: string;
  snippet: string;
}

export interface TagRow {
  id?: number;
  note_id: number;
  tag: string;
}

export interface SearchOptions {
  limit?: number;
  matchStrategy?: "fts" | "exact" | "like";
  path?: string;
}

export interface ListOptions {
  tag?: string;
  type?: string;
  status?: string;
  project?: string;
  feature?: string;
  path?: string;
  limit?: number;
  sortBy?: "created" | "modified" | "priority" | "title";
  sortOrder?: "asc" | "desc";
}

export interface EntryMetaRow {
  path: string;
  project_id: string | null;
  access_count: number;
  last_accessed: string | null;
  last_verified: string | null;
  created_at: string;
}

export interface StorageStats {
  total: number;
  byType: Record<string, number>;
  orphanCount: number;
  trackedCount: number;   // entries in entry_meta
  staleCount: number;     // entries never verified or verified > 30 days ago
}

// =============================================================================
// StorageLayer Class
// =============================================================================

/**
 * Wraps a bun:sqlite Database instance and provides all database operations
 * needed by brain-service.
 *
 * Use the constructor directly for testing (pass an in-memory Database),
 * or use `createStorageLayer()` for production (creates file-backed DB
 * with PRAGMAs and schema migration).
 */
export class StorageLayer {
  private db: Database;

  constructor(db: Database) {
    this.db = db;
  }

  /** Get the underlying Database instance. */
  getDb(): Database {
    return this.db;
  }

  /** Close the database connection. */
  close(): void {
    this.db.close();
  }

  // ===========================================================================
  // Note CRUD (Phase 2)
  // ===========================================================================

  /**
   * Insert a new note into the database.
   * Returns the inserted row with `id` and `indexed_at` populated.
   * Throws on duplicate `path` (UNIQUE constraint).
   */
  insertNote(note: Omit<NoteRow, "id" | "indexed_at">): NoteRow {
    const stmt = this.db.prepare(`
      INSERT INTO notes (
        path, short_id, title, lead, body, raw_content,
        word_count, checksum, metadata, type, status, priority,
        project_id, feature_id, created, modified
      ) VALUES (
        $path, $short_id, $title, $lead, $body, $raw_content,
        $word_count, $checksum, $metadata, $type, $status, $priority,
        $project_id, $feature_id, $created, $modified
      )
    `);

    stmt.run({
      $path: note.path,
      $short_id: note.short_id,
      $title: note.title,
      $lead: note.lead,
      $body: note.body,
      $raw_content: note.raw_content,
      $word_count: note.word_count,
      $checksum: note.checksum,
      $metadata: note.metadata,
      $type: note.type,
      $status: note.status,
      $priority: note.priority,
      $project_id: note.project_id,
      $feature_id: note.feature_id,
      $created: note.created,
      $modified: note.modified,
    });

    const lastId = this.db
      .prepare("SELECT last_insert_rowid() as id")
      .get() as { id: number };

    return this.db
      .prepare("SELECT * FROM notes WHERE id = ?")
      .get(lastId.id) as NoteRow;
  }

  /**
   * Get a note by its unique path.
   * Returns null if not found.
   */
  getNoteByPath(path: string): NoteRow | null {
    const row = this.db
      .prepare("SELECT * FROM notes WHERE path = ?")
      .get(path) as NoteRow | null;
    return row ?? null;
  }

  /**
   * Get a note by its short_id.
   * short_id is indexed but NOT unique — returns first match.
   * Returns null if not found.
   */
  getNoteByShortId(shortId: string): NoteRow | null {
    const row = this.db
      .prepare("SELECT * FROM notes WHERE short_id = ? LIMIT 1")
      .get(shortId) as NoteRow | null;
    return row ?? null;
  }

  /**
   * Update a note by path with partial fields.
   * Automatically updates `indexed_at` to current datetime.
   * Returns the updated row, or null if path not found.
   */
  updateNote(
    path: string,
    updates: Partial<Omit<NoteRow, "id" | "path" | "indexed_at">>
  ): NoteRow | null {
    const fields = Object.keys(updates).filter(
      (k) => k !== "id" && k !== "path" && k !== "indexed_at"
    );

    if (fields.length === 0) {
      return this.getNoteByPath(path);
    }

    const setClauses = fields.map((f) => `${f} = $${f}`);
    setClauses.push("indexed_at = datetime('now')");

    const sql = `UPDATE notes SET ${setClauses.join(", ")} WHERE path = $path`;

    const params: Record<string, string | number | null> = { $path: path };
    for (const f of fields) {
      params[`$${f}`] = (updates as Record<string, string | number | null>)[f];
    }

    const result = this.db.prepare(sql).run(params as any);

    if (result.changes === 0) {
      return null;
    }

    return this.getNoteByPath(path);
  }

  /**
   * Delete a note by path.
   * Returns true if a row was deleted, false otherwise.
   * CASCADE will auto-delete related links and tags.
   */
  deleteNote(path: string): boolean {
    const result = this.db
      .prepare("DELETE FROM notes WHERE path = ?")
      .run(path);
    return result.changes > 0;
  }

  // ===========================================================================
  // Tags & Links (Phase 3)
  // ===========================================================================

  /**
   * Replace all tags for a note (identified by path) with the given array.
   * Wraps delete + insert in a transaction for atomicity.
   * Throws if the note path does not exist.
   */
  setTags(notePath: string, tags: string[]): void {
    const note = this.getNoteByPath(notePath);
    if (!note) {
      throw new Error(`Note not found: ${notePath}`);
    }

    const noteId = note.id!;

    this.db.transaction(() => {
      this.db.prepare("DELETE FROM tags WHERE note_id = ?").run(noteId);

      if (tags.length > 0) {
        const insertStmt = this.db.prepare(
          "INSERT INTO tags (note_id, tag) VALUES (?, ?)"
        );
        for (const tag of tags) {
          insertStmt.run(noteId, tag);
        }
      }
    })();
  }

  /**
   * Replace all links for a note (identified by path) with the given array.
   * Resolves target_id by looking up target_path in the notes table.
   * Wraps delete + insert in a transaction for atomicity.
   * Throws if the source note path does not exist.
   */
  setLinks(
    notePath: string,
    links: Array<Omit<LinkRow, "id" | "source_id">>
  ): void {
    const note = this.getNoteByPath(notePath);
    if (!note) {
      throw new Error(`Note not found: ${notePath}`);
    }

    const sourceId = note.id!;
    const resolveTargetStmt = this.db.prepare(
      "SELECT id FROM notes WHERE path = ?"
    );

    this.db.transaction(() => {
      this.db.prepare("DELETE FROM links WHERE source_id = ?").run(sourceId);

      if (links.length > 0) {
        const insertStmt = this.db.prepare(`
          INSERT INTO links (source_id, target_path, target_id, title, href, type, snippet)
          VALUES (?, ?, ?, ?, ?, ?, ?)
        `);

        for (const link of links) {
          // Try to resolve target_id from target_path
          const targetRow = resolveTargetStmt.get(link.target_path) as {
            id: number;
          } | null;
          const targetId = targetRow?.id ?? null;

          insertStmt.run(
            sourceId,
            link.target_path,
            targetId,
            link.title,
            link.href,
            link.type,
            link.snippet
          );
        }
      }
    })();
  }

  // ===========================================================================
  // Search (Phase 4)
  // ===========================================================================

  /**
   * Search notes using the specified match strategy.
   *
   * Strategies:
   * - 'fts' (default): Full-text search via FTS5 with BM25 ranking.
   *   Title matches weighted 10x, path 5x, body 1x.
   * - 'exact': Exact title match or body substring match.
   * - 'like': LIKE substring match across title, body, and path.
   *
   * Options:
   * - limit: Max results (default 50).
   * - path: Filter to notes whose path starts with this prefix.
   *
   * Returns empty array on no matches or invalid FTS query syntax.
   */
  searchNotes(query: string, options?: SearchOptions): NoteRow[] {
    const limit = options?.limit ?? 50;
    const strategy = options?.matchStrategy ?? "fts";
    const pathPrefix = options?.path;

    if (!query || query.trim() === "") {
      return [];
    }

    try {
      switch (strategy) {
        case "fts":
          return this.searchFts(query, limit, pathPrefix);
        case "exact":
          return this.searchExact(query, limit, pathPrefix);
        case "like":
          return this.searchLike(query, limit, pathPrefix);
        default:
          return [];
      }
    } catch {
      // FTS5 query syntax errors or other DB errors — return empty
      return [];
    }
  }

  private searchFts(
    query: string,
    limit: number,
    pathPrefix?: string
  ): NoteRow[] {
    let sql = `
      SELECT n.* FROM notes n
      JOIN notes_fts ON notes_fts.rowid = n.id
      WHERE notes_fts MATCH ?
    `;
    const params: (string | number)[] = [query];

    if (pathPrefix) {
      sql += " AND n.path LIKE ?";
      params.push(pathPrefix + "%");
    }

    sql += " ORDER BY bm25(notes_fts, 10.0, 1.0, 5.0) LIMIT ?";
    params.push(limit);

    return this.db.prepare(sql).all(...params) as NoteRow[];
  }

  private searchExact(
    query: string,
    limit: number,
    pathPrefix?: string
  ): NoteRow[] {
    let sql = `
      SELECT * FROM notes
      WHERE (title = ? OR body LIKE '%' || ? || '%')
    `;
    const params: (string | number)[] = [query, query];

    if (pathPrefix) {
      sql += " AND path LIKE ?";
      params.push(pathPrefix + "%");
    }

    sql += " LIMIT ?";
    params.push(limit);

    return this.db.prepare(sql).all(...params) as NoteRow[];
  }

  private searchLike(
    query: string,
    limit: number,
    pathPrefix?: string
  ): NoteRow[] {
    let sql = `
      SELECT * FROM notes
      WHERE (title LIKE '%' || ? || '%'
        OR body LIKE '%' || ? || '%'
        OR path LIKE '%' || ? || '%')
    `;
    const params: (string | number)[] = [query, query, query];

    if (pathPrefix) {
      sql += " AND path LIKE ?";
      params.push(pathPrefix + "%");
    }

    sql += " LIMIT ?";
    params.push(limit);

    return this.db.prepare(sql).all(...params) as NoteRow[];
  }

  // ===========================================================================
  // List / Filter (Phase 5)
  // ===========================================================================

  /**
   * List notes with optional filtering, sorting, and pagination.
   *
   * Filters: type, status, project, feature, path (prefix), tag (via subquery).
   * Sorting: created, modified (default), priority (CASE expression), title.
   * Default sort order: desc. Default limit: 100.
   */
  listNotes(options?: ListOptions): NoteRow[] {
    const clauses: string[] = [];
    const params: (string | number)[] = [];

    if (options?.type) {
      clauses.push("n.type = ?");
      params.push(options.type);
    }

    if (options?.status) {
      clauses.push("n.status = ?");
      params.push(options.status);
    }

    if (options?.project) {
      clauses.push("n.project_id = ?");
      params.push(options.project);
    }

    if (options?.feature) {
      clauses.push("n.feature_id = ?");
      params.push(options.feature);
    }

    if (options?.path) {
      clauses.push("n.path LIKE ? || '%'");
      params.push(options.path);
    }

    if (options?.tag) {
      clauses.push("n.id IN (SELECT note_id FROM tags WHERE tag = ?)");
      params.push(options.tag);
    }

    let sql = "SELECT n.* FROM notes n";

    if (clauses.length > 0) {
      sql += " WHERE " + clauses.join(" AND ");
    }

    // Sorting
    const sortOrder = options?.sortOrder ?? "desc";
    const sortDirection = sortOrder.toUpperCase();

    switch (options?.sortBy) {
      case "created":
        sql += ` ORDER BY n.created ${sortDirection}`;
        break;
      case "title":
        sql += ` ORDER BY n.title ${sortDirection}`;
        break;
      case "priority":
        sql += ` ORDER BY CASE n.priority WHEN 'high' THEN 0 WHEN 'medium' THEN 1 WHEN 'low' THEN 2 ELSE 3 END ${sortDirection}`;
        break;
      case "modified":
      default:
        sql += ` ORDER BY n.modified ${sortDirection}`;
        break;
    }

    // Limit
    const limit = options?.limit ?? 100;
    sql += " LIMIT ?";
    params.push(limit);

    return this.db.prepare(sql).all(...params) as NoteRow[];
  }

  // ===========================================================================
  // Graph Operations (Phase 6)
  // ===========================================================================

  /**
   * Find all notes that link TO this note (backlinks).
   * Matches by target_id (resolved) or target_path (unresolved).
   * Returns empty array if note not found.
   */
  getBacklinks(path: string): NoteRow[] {
    const note = this.getNoteByPath(path);
    if (!note) return [];

    return this.db
      .prepare(
        `SELECT DISTINCT n.* FROM notes n
         JOIN links l ON l.source_id = n.id
         WHERE l.target_id = ? OR l.target_path = ?`
      )
      .all(note.id!, path) as NoteRow[];
  }

  /**
   * Find all notes linked BY this note (outlinks).
   * Only returns resolved links (where target note exists).
   * Returns empty array if source note not found.
   */
  getOutlinks(path: string): NoteRow[] {
    const note = this.getNoteByPath(path);
    if (!note) return [];

    return this.db
      .prepare(
        `SELECT DISTINCT n.* FROM notes n
         JOIN links l ON l.target_id = n.id
         WHERE l.source_id = ?`
      )
      .all(note.id!) as NoteRow[];
  }

  /**
   * Find notes that share link targets with this note.
   * i.e., other notes that also link to the same targets.
   * Returns empty array if note not found.
   */
  getRelated(path: string, limit?: number): NoteRow[] {
    const note = this.getNoteByPath(path);
    if (!note) return [];

    const effectiveLimit = limit ?? 10;

    return this.db
      .prepare(
        `SELECT DISTINCT n.* FROM notes n
         JOIN links l2 ON l2.source_id = n.id
         WHERE l2.target_id IN (
           SELECT l1.target_id FROM links l1 WHERE l1.source_id = ?
         )
         AND n.id != ?
         LIMIT ?`
      )
      .all(note.id!, note.id!, effectiveLimit) as NoteRow[];
  }

  /**
   * Find notes with no incoming links (orphans).
   * Optionally filter by type and limit results.
   */
  getOrphans(options?: { type?: string; limit?: number }): NoteRow[] {
    const limit = options?.limit ?? 50;
    const type = options?.type;

    let sql = `
      SELECT n.* FROM notes n
      LEFT JOIN links l ON (l.target_id = n.id OR l.target_path = n.path)
      WHERE l.id IS NULL
    `;
    const params: (string | number)[] = [];

    if (type) {
      sql += " AND n.type = ?";
      params.push(type);
    }

    sql += " LIMIT ?";
    params.push(limit);

    return this.db.prepare(sql).all(...params) as NoteRow[];
  }

  // ===========================================================================
  // Entry Metadata (Phase 7)
  // ===========================================================================

  /**
   * Record an access for a path.
   * UPSERT: if exists, increment access_count and update last_accessed.
   * If not exists, insert with access_count=1.
   */
  recordAccess(path: string): void {
    this.db
      .prepare(
        `INSERT INTO entry_meta (path, access_count, last_accessed, created_at)
         VALUES (?, 1, datetime('now'), datetime('now'))
         ON CONFLICT(path) DO UPDATE SET
           access_count = access_count + 1,
           last_accessed = datetime('now')`
      )
      .run(path);
  }

  /**
   * Get access statistics for a path.
   * Returns null if no entry_meta record exists.
   */
  getAccessStats(path: string): EntryMetaRow | null {
    const row = this.db
      .prepare("SELECT * FROM entry_meta WHERE path = ?")
      .get(path) as EntryMetaRow | null;
    return row ?? null;
  }

  /**
   * Mark a path as verified now.
   * UPSERT: if exists, update last_verified. If not, insert with last_verified=now.
   */
  setVerified(path: string): void {
    this.db
      .prepare(
        `INSERT INTO entry_meta (path, last_verified, created_at)
         VALUES (?, datetime('now'), datetime('now'))
         ON CONFLICT(path) DO UPDATE SET
           last_verified = datetime('now')`
      )
      .run(path);
  }

  /**
   * Find notes whose last_verified is NULL or older than N days.
   * Joins entry_meta with notes to return full note data.
   * Optional type filter and limit (default 50).
   */
  getStaleEntries(
    days: number,
    options?: { type?: string; limit?: number }
  ): NoteRow[] {
    const limit = options?.limit ?? 50;
    const type = options?.type;

    let sql = `
      SELECT n.* FROM notes n
      LEFT JOIN entry_meta em ON em.path = n.path
      WHERE (em.last_verified IS NULL
         OR em.last_verified < datetime('now', '-' || ? || ' days'))
    `;
    const params: (string | number)[] = [days];

    if (type) {
      sql += " AND n.type = ?";
      params.push(type);
    }

    sql += " LIMIT ?";
    params.push(limit);

    return this.db.prepare(sql).all(...params) as NoteRow[];
  }

  // ===========================================================================
  // Stats (Phase 8)
  // ===========================================================================

  /**
   * Get aggregate statistics about the database.
   * Optionally scope to a path prefix.
   */
  getStats(options?: { path?: string }): StorageStats {
    const pathPrefix = options?.path;

    // 1. Total count
    let totalSql = "SELECT COUNT(*) as count FROM notes";
    const totalParams: string[] = [];
    if (pathPrefix) {
      totalSql += " WHERE path LIKE ? || '%'";
      totalParams.push(pathPrefix);
    }
    const totalRow = this.db.prepare(totalSql).get(...totalParams) as {
      count: number;
    };

    // 2. By type
    let typeSql =
      "SELECT type, COUNT(*) as count FROM notes WHERE type IS NOT NULL";
    const typeParams: string[] = [];
    if (pathPrefix) {
      typeSql += " AND path LIKE ? || '%'";
      typeParams.push(pathPrefix);
    }
    typeSql += " GROUP BY type";
    const typeRows = this.db.prepare(typeSql).all(...typeParams) as {
      type: string;
      count: number;
    }[];
    const byType: Record<string, number> = {};
    for (const row of typeRows) {
      byType[row.type] = row.count;
    }

    // 3. Orphan count
    let orphanSql = `
      SELECT COUNT(*) as count FROM notes n
      LEFT JOIN links l ON (l.target_id = n.id OR l.target_path = n.path)
      WHERE l.id IS NULL
    `;
    const orphanParams: string[] = [];
    if (pathPrefix) {
      orphanSql += " AND n.path LIKE ? || '%'";
      orphanParams.push(pathPrefix);
    }
    const orphanRow = this.db.prepare(orphanSql).get(...orphanParams) as {
      count: number;
    };

    // 4. Tracked count
    let trackedSql = "SELECT COUNT(*) as count FROM entry_meta";
    const trackedParams: string[] = [];
    if (pathPrefix) {
      trackedSql += " WHERE path LIKE ? || '%'";
      trackedParams.push(pathPrefix);
    }
    const trackedRow = this.db.prepare(trackedSql).get(...trackedParams) as {
      count: number;
    };

    // 5. Stale count (never verified or verified > 30 days ago)
    let staleSql = `
      SELECT COUNT(*) as count FROM notes n
      LEFT JOIN entry_meta em ON em.path = n.path
      WHERE (em.last_verified IS NULL
         OR em.last_verified < datetime('now', '-30 days'))
    `;
    const staleParams: string[] = [];
    if (pathPrefix) {
      staleSql += " AND n.path LIKE ? || '%'";
      staleParams.push(pathPrefix);
    }
    const staleRow = this.db.prepare(staleSql).get(...staleParams) as {
      count: number;
    };

    return {
      total: totalRow.count,
      byType,
      orphanCount: orphanRow.count,
      trackedCount: trackedRow.count,
      staleCount: staleRow.count,
    };
  }

  // ===========================================================================
  // Index Management (Phase 9)
  // ===========================================================================

  /**
   * Reindex a single markdown file into the database.
   * Reads the file, parses frontmatter, computes checksum, and upserts.
   * Skips if checksum hasn't changed (optimization).
   */
  reindex(filePath: string, brainDir: string): void {
    const fullPath = join(brainDir, filePath);
    const content = readFileSync(fullPath, "utf-8");

    // Compute checksum for change detection
    const checksum = new Bun.CryptoHasher("md5").update(content).digest("hex");

    // Check if note already exists with same checksum — skip if unchanged
    const existing = this.getNoteByPath(filePath);
    if (existing && existing.checksum === checksum) {
      return;
    }

    // Parse frontmatter and body
    const { frontmatter, body } = parseFrontmatter(content);
    const shortId = extractIdFromPath(filePath);

    // Extract tags from frontmatter
    const tags: string[] = Array.isArray(frontmatter.tags)
      ? (frontmatter.tags as string[])
      : [];

    // Extract markdown links from body: [title](href)
    const linkRegex = /\[([^\]]*)\]\(([^)]+)\)/g;
    const links: Array<{ title: string; href: string }> = [];
    let match: RegExpExecArray | null;
    while ((match = linkRegex.exec(body)) !== null) {
      links.push({ title: match[1], href: match[2] });
    }

    // Compute word count from body
    const wordCount = body
      .split(/\s+/)
      .filter((w) => w.length > 0).length;

    // Build the lead (first line of body, truncated)
    const leadLine = body.split("\n").find((l) => l.trim().length > 0) || "";
    const lead = leadLine.slice(0, 200);

    // Build NoteRow fields from frontmatter
    const noteData: Omit<NoteRow, "id" | "indexed_at"> = {
      path: filePath,
      short_id: shortId,
      title: (frontmatter.title as string) || shortId,
      lead,
      body,
      raw_content: content,
      word_count: wordCount,
      checksum,
      metadata: JSON.stringify(frontmatter),
      type: (frontmatter.type as string) || null,
      status: (frontmatter.status as string) || null,
      priority: (frontmatter.priority as string) || null,
      project_id: (frontmatter.projectId as string) || null,
      feature_id: (frontmatter.feature_id as string) || null,
      created: (frontmatter.created as string) || null,
      modified: (frontmatter.modified as string) || null,
    };

    if (existing) {
      // Update existing note
      this.updateNote(filePath, {
        short_id: noteData.short_id,
        title: noteData.title,
        lead: noteData.lead,
        body: noteData.body,
        raw_content: noteData.raw_content,
        word_count: noteData.word_count,
        checksum: noteData.checksum,
        metadata: noteData.metadata,
        type: noteData.type,
        status: noteData.status,
        priority: noteData.priority,
        project_id: noteData.project_id,
        feature_id: noteData.feature_id,
        created: noteData.created,
        modified: noteData.modified,
      });
    } else {
      // Insert new note
      this.insertNote(noteData);
    }

    // Replace tags
    this.setTags(filePath, tags);

    // Replace links
    this.setLinks(
      filePath,
      links.map((l) => ({
        target_path: l.href,
        target_id: null,
        title: l.title,
        href: l.href,
        type: "markdown",
        snippet: "",
      }))
    );
  }

  /**
   * Reindex all markdown files in a brain directory.
   * Wraps in a transaction for atomicity.
   * After indexing, removes stale entries.
   */
  reindexAll(brainDir: string): void {
    const glob = new Glob("**/*.md");
    const files: string[] = [];

    for (const file of glob.scanSync({ cwd: brainDir })) {
      files.push(file);
    }

    this.db.transaction(() => {
      for (const file of files) {
        this.reindex(file, brainDir);
      }
      this.removeStale(brainDir);
    })();
  }

  /**
   * Remove notes whose files no longer exist on disk.
   * Returns count of removed entries.
   */
  removeStale(brainDir: string): number {
    const allNotes = this.db
      .prepare("SELECT path FROM notes")
      .all() as { path: string }[];

    let removed = 0;
    for (const note of allNotes) {
      if (!existsSync(join(brainDir, note.path))) {
        this.deleteNote(note.path);
        removed++;
      }
    }

    return removed;
  }

  /**
   * Resolve a link href to a NoteRow.
   * Tries in order: exact path match, short_id match, path ending match.
   * Returns null if no match found.
   */
  resolveLink(href: string): NoteRow | null {
    // 1. Exact path match
    const byPath = this.getNoteByPath(href);
    if (byPath) return byPath;

    // 2. short_id match
    const byShortId = this.getNoteByShortId(href);
    if (byShortId) return byShortId;

    // 3. Path ending match (e.g., "task/abc12def.md" matches "projects/test/task/abc12def.md")
    const byEnding = this.db
      .prepare("SELECT * FROM notes WHERE path LIKE ? LIMIT 1")
      .get(`%/${href}`) as NoteRow | null;
    if (byEnding) return byEnding;

    return null;
  }
}

// =============================================================================
// Factory Function
// =============================================================================

/**
 * Create a StorageLayer with a properly configured SQLite database.
 *
 * - Creates a new Database at `dbPath` (use ":memory:" for tests)
 * - Sets PRAGMAs: WAL mode, foreign keys ON, synchronous = NORMAL
 * - Runs schema migration (creates tables if needed)
 *
 * @param dbPath - Path to the SQLite database file, or ":memory:" for in-memory
 * @returns A configured StorageLayer instance
 */
export function createStorageLayer(dbPath: string): StorageLayer {
  const db = new Database(dbPath, { create: true });

  // Set PRAGMAs for performance and correctness
  db.exec("PRAGMA journal_mode = WAL");
  db.exec("PRAGMA foreign_keys = ON");
  db.exec("PRAGMA synchronous = NORMAL");

  // Apply schema (creates tables on fresh DB, migrates if needed)
  migrateSchema(db);

  return new StorageLayer(db);
}

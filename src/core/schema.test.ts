/**
 * Brain API - Unified Schema Tests
 *
 * Tests for schema creation, FTS5 search, triggers, migrations,
 * and all table operations.
 */

import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import { Database } from "bun:sqlite";
import {
  createSchema,
  getSchemaVersion,
  migrateSchema,
  SCHEMA_VERSION,
} from "./schema";

// Use in-memory database for fast tests
let db: Database;

beforeEach(() => {
  db = new Database(":memory:");
  db.exec("PRAGMA foreign_keys = ON;");
});

afterEach(() => {
  db.close();
});

// =============================================================================
// Schema Creation
// =============================================================================

describe("createSchema", () => {
  test("creates all tables", () => {
    createSchema(db);

    const tables = db
      .prepare(
        "SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name"
      )
      .all() as { name: string }[];

    const tableNames = tables.map((t) => t.name);
    expect(tableNames).toContain("notes");
    expect(tableNames).toContain("notes_fts");
    expect(tableNames).toContain("links");
    expect(tableNames).toContain("tags");
    expect(tableNames).toContain("entry_meta");
    expect(tableNames).toContain("generated_tasks");
    expect(tableNames).toContain("schema_version");
  });

  test("creates all indexes on notes", () => {
    createSchema(db);

    const indexes = db
      .prepare(
        "SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'notes'"
      )
      .all() as { name: string }[];

    const indexNames = indexes.map((i) => i.name);
    expect(indexNames).toContain("idx_notes_short_id");
    expect(indexNames).toContain("idx_notes_type");
    expect(indexNames).toContain("idx_notes_status");
    expect(indexNames).toContain("idx_notes_project");
    expect(indexNames).toContain("idx_notes_feature");
  });

  test("creates all indexes on links", () => {
    createSchema(db);

    const indexes = db
      .prepare(
        "SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'links'"
      )
      .all() as { name: string }[];

    const indexNames = indexes.map((i) => i.name);
    expect(indexNames).toContain("idx_links_source");
    expect(indexNames).toContain("idx_links_target");
    expect(indexNames).toContain("idx_links_target_path");
  });

  test("creates all indexes on tags", () => {
    createSchema(db);

    const indexes = db
      .prepare(
        "SELECT name FROM sqlite_master WHERE type = 'index' AND tbl_name = 'tags'"
      )
      .all() as { name: string }[];

    const indexNames = indexes.map((i) => i.name);
    expect(indexNames).toContain("idx_tags_note");
    expect(indexNames).toContain("idx_tags_tag");
  });

  test("creates FTS5 triggers", () => {
    createSchema(db);

    const triggers = db
      .prepare("SELECT name FROM sqlite_master WHERE type = 'trigger'")
      .all() as { name: string }[];

    const triggerNames = triggers.map((t) => t.name);
    expect(triggerNames).toContain("notes_ai");
    expect(triggerNames).toContain("notes_ad");
    expect(triggerNames).toContain("notes_au");
  });

  test("is idempotent — calling twice does not error", () => {
    createSchema(db);
    expect(() => createSchema(db)).not.toThrow();
  });
});

// =============================================================================
// Schema Version
// =============================================================================

describe("getSchemaVersion", () => {
  test("returns 0 for fresh database", () => {
    expect(getSchemaVersion(db)).toBe(0);
  });

  test("returns correct version after createSchema", () => {
    createSchema(db);
    expect(getSchemaVersion(db)).toBe(SCHEMA_VERSION);
  });
});

// =============================================================================
// Migration
// =============================================================================

describe("migrateSchema", () => {
  test("creates schema from scratch on fresh database", () => {
    migrateSchema(db);

    expect(getSchemaVersion(db)).toBe(SCHEMA_VERSION);

    // Verify notes table exists
    const table = db
      .prepare(
        "SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'notes'"
      )
      .get() as { name: string } | undefined;
    expect(table?.name).toBe("notes");
  });

  test("is idempotent — running twice does not error", () => {
    migrateSchema(db);
    expect(() => migrateSchema(db)).not.toThrow();
    expect(getSchemaVersion(db)).toBe(SCHEMA_VERSION);
  });

  test("does nothing when already at current version", () => {
    createSchema(db);
    const versionBefore = getSchemaVersion(db);

    migrateSchema(db);
    const versionAfter = getSchemaVersion(db);

    expect(versionBefore).toBe(versionAfter);
    expect(versionAfter).toBe(SCHEMA_VERSION);
  });
});

// =============================================================================
// FTS5 Search
// =============================================================================

describe("FTS5 search", () => {
  test("basic search finds inserted note", () => {
    createSchema(db);

    db.exec(`
      INSERT INTO notes (path, short_id, title, body)
      VALUES ('projects/test/plan/abc12345.md', 'abc12345', 'Authentication System', 'Implement JWT-based auth with refresh tokens')
    `);

    const results = db
      .prepare(
        `SELECT path, title, bm25(notes_fts, 10.0, 1.0, 5.0) as rank
         FROM notes_fts
         WHERE notes_fts MATCH ?
         ORDER BY rank
         LIMIT 10`
      )
      .all("authentication") as { path: string; title: string; rank: number }[];

    expect(results.length).toBe(1);
    expect(results[0].path).toBe("projects/test/plan/abc12345.md");
    expect(results[0].title).toBe("Authentication System");
  });

  test("BM25 ranking — title matches rank higher than body matches", () => {
    createSchema(db);

    // Note with "database" in title
    db.exec(`
      INSERT INTO notes (path, short_id, title, body)
      VALUES ('note-title.md', 'aaa11111', 'Database Migration Guide', 'This guide covers schema changes')
    `);

    // Note with "database" only in body
    db.exec(`
      INSERT INTO notes (path, short_id, title, body)
      VALUES ('note-body.md', 'bbb22222', 'System Architecture', 'The database layer handles persistence and caching')
    `);

    const results = db
      .prepare(
        `SELECT path, title, bm25(notes_fts, 10.0, 1.0, 5.0) as rank
         FROM notes_fts
         WHERE notes_fts MATCH ?
         ORDER BY rank
         LIMIT 10`
      )
      .all("database") as { path: string; title: string; rank: number }[];

    expect(results.length).toBe(2);
    // Title match should rank higher (lower BM25 score = better match)
    expect(results[0].path).toBe("note-title.md");
    expect(results[1].path).toBe("note-body.md");
  });

  test("porter stemming matches word variants", () => {
    createSchema(db);

    db.exec(`
      INSERT INTO notes (path, short_id, title, body)
      VALUES ('stemming.md', 'ccc33333', 'Running Tests', 'The test runner executes all tests')
    `);

    // Search for "run" should match "running" and "runner" via porter stemming
    const results = db
      .prepare(
        `SELECT path FROM notes_fts WHERE notes_fts MATCH ?`
      )
      .all("run") as { path: string }[];

    expect(results.length).toBe(1);
    expect(results[0].path).toBe("stemming.md");
  });
});

// =============================================================================
// FTS5 Trigger Sync
// =============================================================================

describe("FTS5 triggers", () => {
  test("INSERT trigger syncs to FTS", () => {
    createSchema(db);

    db.exec(`
      INSERT INTO notes (path, short_id, title, body)
      VALUES ('trigger-insert.md', 'ddd44444', 'Trigger Test', 'Testing insert trigger')
    `);

    const results = db
      .prepare("SELECT path FROM notes_fts WHERE notes_fts MATCH ?")
      .all("trigger") as { path: string }[];

    expect(results.length).toBe(1);
    expect(results[0].path).toBe("trigger-insert.md");
  });

  test("UPDATE trigger syncs to FTS", () => {
    createSchema(db);

    db.exec(`
      INSERT INTO notes (path, short_id, title, body)
      VALUES ('trigger-update.md', 'eee55555', 'Original Title', 'Original body')
    `);

    // Verify original is searchable
    let results = db
      .prepare("SELECT path FROM notes_fts WHERE notes_fts MATCH ?")
      .all("original") as { path: string }[];
    expect(results.length).toBe(1);

    // Update the note
    db.exec(`
      UPDATE notes SET title = 'Updated Title', body = 'Completely new content'
      WHERE path = 'trigger-update.md'
    `);

    // Old content should no longer match
    results = db
      .prepare("SELECT path FROM notes_fts WHERE notes_fts MATCH ?")
      .all("original") as { path: string }[];
    expect(results.length).toBe(0);

    // New content should match
    results = db
      .prepare("SELECT path FROM notes_fts WHERE notes_fts MATCH ?")
      .all("updated") as { path: string }[];
    expect(results.length).toBe(1);
    expect(results[0].path).toBe("trigger-update.md");
  });

  test("DELETE trigger syncs to FTS", () => {
    createSchema(db);

    db.exec(`
      INSERT INTO notes (path, short_id, title, body)
      VALUES ('trigger-delete.md', 'fff66666', 'Delete Me', 'This will be deleted')
    `);

    // Verify it's searchable
    let results = db
      .prepare("SELECT path FROM notes_fts WHERE notes_fts MATCH ?")
      .all("deleted") as { path: string }[];
    expect(results.length).toBe(1);

    // Delete the note
    db.exec("DELETE FROM notes WHERE path = 'trigger-delete.md'");

    // Should no longer be searchable
    results = db
      .prepare("SELECT path FROM notes_fts WHERE notes_fts MATCH ?")
      .all("deleted") as { path: string }[];
    expect(results.length).toBe(0);
  });
});

// =============================================================================
// Links Table
// =============================================================================

describe("links table", () => {
  test("insert and query links", () => {
    createSchema(db);

    // Create source and target notes
    db.exec(`
      INSERT INTO notes (path, short_id, title) VALUES ('source.md', 'src11111', 'Source Note');
      INSERT INTO notes (path, short_id, title) VALUES ('target.md', 'tgt22222', 'Target Note');
    `);

    const sourceId = (
      db
        .prepare("SELECT id FROM notes WHERE path = 'source.md'")
        .get() as { id: number }
    ).id;
    const targetId = (
      db
        .prepare("SELECT id FROM notes WHERE path = 'target.md'")
        .get() as { id: number }
    ).id;

    db.prepare(
      "INSERT INTO links (source_id, target_path, target_id, href) VALUES (?, 'target.md', ?, 'target.md')"
    ).run(sourceId, targetId);

    const links = db
      .prepare("SELECT * FROM links WHERE source_id = ?")
      .all(sourceId) as { source_id: number; target_path: string; target_id: number }[];

    expect(links.length).toBe(1);
    expect(links[0].target_path).toBe("target.md");
    expect(links[0].target_id).toBe(targetId);
  });

  test("ON DELETE CASCADE removes links when source note deleted", () => {
    createSchema(db);

    db.exec(`
      INSERT INTO notes (path, short_id, title) VALUES ('cascade-src.md', 'cas11111', 'Source');
    `);

    const sourceId = (
      db
        .prepare("SELECT id FROM notes WHERE path = 'cascade-src.md'")
        .get() as { id: number }
    ).id;

    db.prepare(
      "INSERT INTO links (source_id, target_path, href) VALUES (?, 'some-target.md', 'some-target.md')"
    ).run(sourceId);

    // Verify link exists
    let links = db.prepare("SELECT * FROM links").all();
    expect(links.length).toBe(1);

    // Delete source note — should cascade
    db.exec("DELETE FROM notes WHERE path = 'cascade-src.md'");

    links = db.prepare("SELECT * FROM links").all();
    expect(links.length).toBe(0);
  });

  test("ON DELETE SET NULL nullifies target_id when target note deleted", () => {
    createSchema(db);

    db.exec(`
      INSERT INTO notes (path, short_id, title) VALUES ('link-src.md', 'lnk11111', 'Source');
      INSERT INTO notes (path, short_id, title) VALUES ('link-tgt.md', 'lnk22222', 'Target');
    `);

    const sourceId = (
      db
        .prepare("SELECT id FROM notes WHERE path = 'link-src.md'")
        .get() as { id: number }
    ).id;
    const targetId = (
      db
        .prepare("SELECT id FROM notes WHERE path = 'link-tgt.md'")
        .get() as { id: number }
    ).id;

    db.prepare(
      "INSERT INTO links (source_id, target_path, target_id, href) VALUES (?, 'link-tgt.md', ?, 'link-tgt.md')"
    ).run(sourceId, targetId);

    // Delete target note — should SET NULL on target_id
    db.exec("DELETE FROM notes WHERE path = 'link-tgt.md'");

    const link = db.prepare("SELECT * FROM links").get() as {
      source_id: number;
      target_id: number | null;
      target_path: string;
    };

    expect(link).toBeTruthy();
    expect(link.source_id).toBe(sourceId);
    expect(link.target_id).toBeNull();
    expect(link.target_path).toBe("link-tgt.md"); // path preserved
  });
});

// =============================================================================
// Tags Table
// =============================================================================

describe("tags table", () => {
  test("insert and filter by tag", () => {
    createSchema(db);

    db.exec(`
      INSERT INTO notes (path, short_id, title) VALUES ('tagged.md', 'tag11111', 'Tagged Note');
    `);

    const noteId = (
      db
        .prepare("SELECT id FROM notes WHERE path = 'tagged.md'")
        .get() as { id: number }
    ).id;

    db.prepare("INSERT INTO tags (note_id, tag) VALUES (?, ?)").run(
      noteId,
      "typescript"
    );
    db.prepare("INSERT INTO tags (note_id, tag) VALUES (?, ?)").run(
      noteId,
      "testing"
    );

    const tags = db
      .prepare("SELECT tag FROM tags WHERE note_id = ? ORDER BY tag")
      .all(noteId) as { tag: string }[];

    expect(tags.length).toBe(2);
    expect(tags[0].tag).toBe("testing");
    expect(tags[1].tag).toBe("typescript");
  });

  test("ON DELETE CASCADE removes tags when note deleted", () => {
    createSchema(db);

    db.exec(`
      INSERT INTO notes (path, short_id, title) VALUES ('tag-cascade.md', 'tgc11111', 'Will Delete');
    `);

    const noteId = (
      db
        .prepare("SELECT id FROM notes WHERE path = 'tag-cascade.md'")
        .get() as { id: number }
    ).id;

    db.prepare("INSERT INTO tags (note_id, tag) VALUES (?, 'go')").run(noteId);
    db.prepare("INSERT INTO tags (note_id, tag) VALUES (?, 'api')").run(noteId);

    // Verify tags exist
    let tags = db.prepare("SELECT * FROM tags").all();
    expect(tags.length).toBe(2);

    // Delete note — should cascade
    db.exec("DELETE FROM notes WHERE path = 'tag-cascade.md'");

    tags = db.prepare("SELECT * FROM tags").all();
    expect(tags.length).toBe(0);
  });

  test("find notes by tag", () => {
    createSchema(db);

    db.exec(`
      INSERT INTO notes (path, short_id, title) VALUES ('note-a.md', 'nta11111', 'Note A');
      INSERT INTO notes (path, short_id, title) VALUES ('note-b.md', 'ntb22222', 'Note B');
      INSERT INTO notes (path, short_id, title) VALUES ('note-c.md', 'ntc33333', 'Note C');
    `);

    const noteA = (db.prepare("SELECT id FROM notes WHERE path = 'note-a.md'").get() as { id: number }).id;
    const noteB = (db.prepare("SELECT id FROM notes WHERE path = 'note-b.md'").get() as { id: number }).id;
    const noteC = (db.prepare("SELECT id FROM notes WHERE path = 'note-c.md'").get() as { id: number }).id;

    db.prepare("INSERT INTO tags (note_id, tag) VALUES (?, 'rust')").run(noteA);
    db.prepare("INSERT INTO tags (note_id, tag) VALUES (?, 'rust')").run(noteB);
    db.prepare("INSERT INTO tags (note_id, tag) VALUES (?, 'python')").run(noteC);

    const rustNotes = db
      .prepare(
        "SELECT n.path FROM notes n JOIN tags t ON n.id = t.note_id WHERE t.tag = ? ORDER BY n.path"
      )
      .all("rust") as { path: string }[];

    expect(rustNotes.length).toBe(2);
    expect(rustNotes[0].path).toBe("note-a.md");
    expect(rustNotes[1].path).toBe("note-b.md");
  });
});

// =============================================================================
// Entry Metadata
// =============================================================================

describe("entry_meta table", () => {
  test("basic CRUD operations", () => {
    createSchema(db);

    // Insert
    db.exec(`
      INSERT INTO entry_meta (path, project_id, access_count)
      VALUES ('projects/test/plan/abc.md', 'test-project', 5)
    `);

    // Read
    const entry = db
      .prepare("SELECT * FROM entry_meta WHERE path = ?")
      .get("projects/test/plan/abc.md") as {
      path: string;
      project_id: string;
      access_count: number;
      created_at: string;
    };

    expect(entry.path).toBe("projects/test/plan/abc.md");
    expect(entry.project_id).toBe("test-project");
    expect(entry.access_count).toBe(5);
    expect(entry.created_at).toBeTruthy(); // DEFAULT datetime('now')

    // Update
    db.exec(`
      UPDATE entry_meta SET access_count = 10, last_accessed = datetime('now')
      WHERE path = 'projects/test/plan/abc.md'
    `);

    const updated = db
      .prepare("SELECT access_count, last_accessed FROM entry_meta WHERE path = ?")
      .get("projects/test/plan/abc.md") as {
      access_count: number;
      last_accessed: string;
    };
    expect(updated.access_count).toBe(10);
    expect(updated.last_accessed).toBeTruthy();

    // Delete
    db.exec("DELETE FROM entry_meta WHERE path = 'projects/test/plan/abc.md'");
    const deleted = db
      .prepare("SELECT * FROM entry_meta WHERE path = ?")
      .get("projects/test/plan/abc.md");
    expect(deleted).toBeNull();
  });
});

// =============================================================================
// Generated Tasks
// =============================================================================

describe("generated_tasks table", () => {
  test("basic CRUD operations", () => {
    createSchema(db);

    // Insert
    db.exec(`
      INSERT INTO generated_tasks (key, task_path, feature_id)
      VALUES ('feature-review:auth:round-1', 'projects/auth/task/rev123.md', 'auth-system')
    `);

    // Read
    const task = db
      .prepare("SELECT * FROM generated_tasks WHERE key = ?")
      .get("feature-review:auth:round-1") as {
      key: string;
      task_path: string;
      feature_id: string;
      created_at: string;
    };

    expect(task.key).toBe("feature-review:auth:round-1");
    expect(task.task_path).toBe("projects/auth/task/rev123.md");
    expect(task.feature_id).toBe("auth-system");
    expect(task.created_at).toBeTruthy();

    // Delete
    db.exec(
      "DELETE FROM generated_tasks WHERE key = 'feature-review:auth:round-1'"
    );
    const deleted = db
      .prepare("SELECT * FROM generated_tasks WHERE key = ?")
      .get("feature-review:auth:round-1");
    expect(deleted).toBeNull();
  });
});

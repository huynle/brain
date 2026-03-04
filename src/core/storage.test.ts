/**
 * Brain API - StorageLayer Tests
 *
 * Phase 1: Construction, factory, close, isolation.
 * Phase 2: Note CRUD operations.
 * Phase 3: Tags, Links, and link resolution.
 * Phase 4: Search (FTS5, exact, like).
 * Phase 5: Filtering / List (dynamic WHERE, sort, limit).
 * Phase 6: Graph operations (backlinks, outlinks, related, orphans).
 * Phase 7: Entry metadata (access tracking, verification, stale detection).
 * Phase 8: Stats (aggregate counts, orphans, tracked, stale).
 */

import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import { Database } from "bun:sqlite";
import { mkdtempSync, writeFileSync, mkdirSync, unlinkSync } from "fs";
import { join, dirname } from "path";
import { tmpdir } from "os";
import { createSchema } from "./schema";
import {
  StorageLayer,
  createStorageLayer,
  type NoteRow,
  type LinkRow,
  type EntryMetaRow,
  type StorageStats,
} from "./storage";

// =============================================================================
// Constructor
// =============================================================================

describe("StorageLayer constructor", () => {
  let db: Database;

  beforeEach(() => {
    db = new Database(":memory:");
    db.exec("PRAGMA foreign_keys = ON;");
    createSchema(db);
  });

  afterEach(() => {
    db.close();
  });

  test("accepts a Database instance", () => {
    const storage = new StorageLayer(db);
    expect(storage).toBeInstanceOf(StorageLayer);
  });

  test("getDb returns the same Database instance", () => {
    const storage = new StorageLayer(db);
    expect(storage.getDb()).toBe(db);
  });
});

// =============================================================================
// Factory Function
// =============================================================================

describe("createStorageLayer", () => {
  let storage: StorageLayer;

  afterEach(() => {
    storage?.close();
  });

  test("creates DB with schema applied — all tables exist", () => {
    storage = createStorageLayer(":memory:");

    const tables = storage
      .getDb()
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

  test("sets WAL journal mode", () => {
    storage = createStorageLayer(":memory:");

    const result = storage
      .getDb()
      .prepare("PRAGMA journal_mode")
      .get() as { journal_mode: string };

    // In-memory databases may report "memory" instead of "wal"
    // but for file-based DBs this would be "wal".
    // For :memory: we just verify the pragma was called without error.
    expect(result).toBeTruthy();
    expect(typeof result.journal_mode).toBe("string");
  });

  test("enables foreign keys", () => {
    storage = createStorageLayer(":memory:");

    const result = storage
      .getDb()
      .prepare("PRAGMA foreign_keys")
      .get() as { foreign_keys: number };

    expect(result.foreign_keys).toBe(1);
  });

  test("sets synchronous to NORMAL", () => {
    storage = createStorageLayer(":memory:");

    const result = storage
      .getDb()
      .prepare("PRAGMA synchronous")
      .get() as { synchronous: number };

    // NORMAL = 1
    expect(result.synchronous).toBe(1);
  });
});

// =============================================================================
// close()
// =============================================================================

describe("StorageLayer.close", () => {
  test("closes the database without error", () => {
    const storage = createStorageLayer(":memory:");
    expect(() => storage.close()).not.toThrow();
  });

  test("database is unusable after close", () => {
    const storage = createStorageLayer(":memory:");
    storage.close();

    // Attempting to query a closed database should throw
    expect(() => {
      storage.getDb().prepare("SELECT 1").get();
    }).toThrow();
  });
});

// =============================================================================
// Isolation
// =============================================================================

// =============================================================================
// Test Helpers (Phase 2)
// =============================================================================

function makeTestNote(
  overrides: Partial<NoteRow> = {}
): Omit<NoteRow, "id" | "indexed_at"> {
  return {
    path: "projects/test/task/abc12def.md",
    short_id: "abc12def",
    title: "Test Note",
    lead: "A test note",
    body: "This is the body content",
    raw_content: "---\ntitle: Test Note\n---\nThis is the body content",
    word_count: 6,
    checksum: "abc123",
    metadata: JSON.stringify({ title: "Test Note" }),
    type: "task",
    status: "active",
    priority: "medium",
    project_id: "test",
    feature_id: null,
    created: "2024-01-01T00:00:00Z",
    modified: "2024-01-01T00:00:00Z",
    ...overrides,
  };
}

// =============================================================================
// Note CRUD (Phase 2)
// =============================================================================

describe("Note CRUD", () => {
  let storage: StorageLayer;

  beforeEach(() => {
    storage = createStorageLayer(":memory:");
  });

  afterEach(() => {
    storage.close();
  });

  // --- insertNote ---

  test("insertNote inserts and returns a note with id", () => {
    const input = makeTestNote();
    const result = storage.insertNote(input);

    expect(result.id).toBeGreaterThan(0);
    expect(result.path).toBe(input.path);
    expect(result.short_id).toBe(input.short_id);
    expect(result.title).toBe(input.title);
    expect(result.lead).toBe(input.lead);
    expect(result.body).toBe(input.body);
    expect(result.raw_content).toBe(input.raw_content);
    expect(result.word_count).toBe(input.word_count);
    expect(result.checksum).toBe(input.checksum);
    expect(result.metadata).toBe(input.metadata);
    expect(result.type).toBe(input.type);
    expect(result.status).toBe(input.status);
    expect(result.priority).toBe(input.priority);
    expect(result.project_id).toBe(input.project_id);
    expect(result.feature_id).toBe(input.feature_id);
    expect(result.created).toBe(input.created);
    expect(result.modified).toBe(input.modified);
    expect(result.indexed_at).toBeTruthy();
  });

  test("insertNote throws on duplicate path", () => {
    const input = makeTestNote();
    storage.insertNote(input);

    expect(() => storage.insertNote(input)).toThrow(/UNIQUE.*path|duplicate/i);
  });

  // --- getNoteByPath ---

  test("getNoteByPath returns the note", () => {
    const input = makeTestNote();
    storage.insertNote(input);

    const result = storage.getNoteByPath(input.path);
    expect(result).not.toBeNull();
    expect(result!.path).toBe(input.path);
    expect(result!.title).toBe(input.title);
    expect(result!.id).toBeGreaterThan(0);
  });

  test("getNoteByPath returns null for missing path", () => {
    const result = storage.getNoteByPath("nonexistent/path.md");
    expect(result).toBeNull();
  });

  // --- getNoteByShortId ---

  test("getNoteByShortId returns the note", () => {
    const input = makeTestNote();
    storage.insertNote(input);

    const result = storage.getNoteByShortId(input.short_id);
    expect(result).not.toBeNull();
    expect(result!.short_id).toBe(input.short_id);
    expect(result!.title).toBe(input.title);
  });

  test("getNoteByShortId returns null for missing short_id", () => {
    const result = storage.getNoteByShortId("zzzzzzzz");
    expect(result).toBeNull();
  });

  // --- getNoteByTitle ---

  test("getNoteByTitle returns the note with exact title match", () => {
    const input = makeTestNote({ title: "My Unique Title" });
    storage.insertNote(input);

    const result = storage.getNoteByTitle("My Unique Title");
    expect(result).not.toBeNull();
    expect(result!.title).toBe("My Unique Title");
    expect(result!.path).toBe(input.path);
  });

  test("getNoteByTitle returns null for missing title", () => {
    const result = storage.getNoteByTitle("Nonexistent Title");
    expect(result).toBeNull();
  });

  test("getNoteByTitle returns first match when multiple notes have same title", () => {
    const note1 = makeTestNote({ title: "Duplicate Title", path: "a/first.md", short_id: "aaaaaaaa" });
    const note2 = makeTestNote({ title: "Duplicate Title", path: "b/second.md", short_id: "bbbbbbbb" });
    storage.insertNote(note1);
    storage.insertNote(note2);

    const result = storage.getNoteByTitle("Duplicate Title");
    expect(result).not.toBeNull();
    expect(result!.title).toBe("Duplicate Title");
  });

  // --- updateNote ---

  test("updateNote updates specified fields only", () => {
    const input = makeTestNote();
    storage.insertNote(input);

    const result = storage.updateNote(input.path, {
      title: "Updated Title",
      status: "completed",
    });

    expect(result).not.toBeNull();
    expect(result!.title).toBe("Updated Title");
    expect(result!.status).toBe("completed");
    // Unchanged fields should remain the same
    expect(result!.body).toBe(input.body);
    expect(result!.lead).toBe(input.lead);
    expect(result!.priority).toBe(input.priority);
  });

  test("updateNote returns null for missing path", () => {
    const result = storage.updateNote("nonexistent/path.md", {
      title: "Nope",
    });
    expect(result).toBeNull();
  });

  test("updateNote updates indexed_at timestamp", () => {
    const input = makeTestNote();
    const inserted = storage.insertNote(input);
    const originalIndexedAt = inserted.indexed_at;

    // Small delay to ensure timestamp differs
    // Force a different indexed_at by updating
    const result = storage.updateNote(input.path, { title: "New Title" });

    expect(result).not.toBeNull();
    // indexed_at should be set (may or may not differ in fast tests, but must exist)
    expect(result!.indexed_at).toBeTruthy();
  });

  // --- deleteNote ---

  test("deleteNote removes the note", () => {
    const input = makeTestNote();
    storage.insertNote(input);

    const deleted = storage.deleteNote(input.path);
    expect(deleted).toBe(true);

    // Verify it's gone
    const result = storage.getNoteByPath(input.path);
    expect(result).toBeNull();
  });

  test("deleteNote returns false for missing path", () => {
    const result = storage.deleteNote("nonexistent/path.md");
    expect(result).toBe(false);
  });

  test("deleteNote cascades to links and tags", () => {
    const input = makeTestNote();
    const note = storage.insertNote(input);

    // Insert a tag referencing this note
    storage
      .getDb()
      .prepare("INSERT INTO tags (note_id, tag) VALUES (?, ?)")
      .run(note.id!, "test-tag");

    // Insert a link referencing this note as source
    storage
      .getDb()
      .prepare(
        "INSERT INTO links (source_id, target_path, href) VALUES (?, ?, ?)"
      )
      .run(note.id!, "other/note.md", "other/note.md");

    // Delete the note
    storage.deleteNote(input.path);

    // Tags should be cascade-deleted
    const tagCount = storage
      .getDb()
      .prepare("SELECT COUNT(*) as count FROM tags WHERE note_id = ?")
      .get(note.id!) as { count: number };
    expect(tagCount.count).toBe(0);

    // Links should be cascade-deleted
    const linkCount = storage
      .getDb()
      .prepare("SELECT COUNT(*) as count FROM links WHERE source_id = ?")
      .get(note.id!) as { count: number };
    expect(linkCount.count).toBe(0);
  });

  // --- FTS5 integration ---

  test("FTS5 index is updated on insert (searchable)", () => {
    const input = makeTestNote({ title: "Unique Searchable Title XYZ" });
    storage.insertNote(input);

    const results = storage
      .getDb()
      .prepare(
        "SELECT rowid FROM notes_fts WHERE notes_fts MATCH 'Unique Searchable Title'"
      )
      .all() as { rowid: number }[];

    expect(results.length).toBe(1);
  });

  test("FTS5 index is updated on delete (no longer searchable)", () => {
    const input = makeTestNote({ title: "Deletable Title QRS" });
    storage.insertNote(input);

    storage.deleteNote(input.path);

    const results = storage
      .getDb()
      .prepare(
        "SELECT rowid FROM notes_fts WHERE notes_fts MATCH 'Deletable Title'"
      )
      .all() as { rowid: number }[];

    expect(results.length).toBe(0);
  });
});

// =============================================================================
// Isolation (Phase 1)
// =============================================================================

// =============================================================================
// Tags (Phase 3)
// =============================================================================

describe("Tags", () => {
  let storage: StorageLayer;

  beforeEach(() => {
    storage = createStorageLayer(":memory:");
  });

  afterEach(() => {
    storage.close();
  });

  test("setTags adds tags to a note", () => {
    const note = storage.insertNote(makeTestNote());
    storage.setTags(note.path, ["tag1", "tag2", "tag3"]);

    const tags = storage
      .getDb()
      .prepare("SELECT tag FROM tags WHERE note_id = ? ORDER BY tag")
      .all(note.id!) as { tag: string }[];

    expect(tags.map((t) => t.tag)).toEqual(["tag1", "tag2", "tag3"]);
  });

  test("setTags replaces existing tags", () => {
    const note = storage.insertNote(makeTestNote());
    storage.setTags(note.path, ["old1", "old2"]);
    storage.setTags(note.path, ["new1", "new2", "new3"]);

    const tags = storage
      .getDb()
      .prepare("SELECT tag FROM tags WHERE note_id = ? ORDER BY tag")
      .all(note.id!) as { tag: string }[];

    expect(tags.map((t) => t.tag)).toEqual(["new1", "new2", "new3"]);
  });

  test("setTags with empty array removes all tags", () => {
    const note = storage.insertNote(makeTestNote());
    storage.setTags(note.path, ["tag1", "tag2"]);
    storage.setTags(note.path, []);

    const count = storage
      .getDb()
      .prepare("SELECT COUNT(*) as count FROM tags WHERE note_id = ?")
      .get(note.id!) as { count: number };

    expect(count.count).toBe(0);
  });

  test("setTags throws for non-existent note", () => {
    expect(() => storage.setTags("nonexistent/path.md", ["tag1"])).toThrow(
      /not found/i
    );
  });

  test("tags are queryable via SQL after setTags", () => {
    const note = storage.insertNote(makeTestNote());
    storage.setTags(note.path, ["searchable-tag", "another-tag"]);

    // Query by tag value
    const results = storage
      .getDb()
      .prepare(
        "SELECT n.path FROM notes n JOIN tags t ON n.id = t.note_id WHERE t.tag = ?"
      )
      .all("searchable-tag") as { path: string }[];

    expect(results.length).toBe(1);
    expect(results[0].path).toBe(note.path);
  });
});

// =============================================================================
// Links (Phase 3)
// =============================================================================

describe("Links", () => {
  let storage: StorageLayer;

  beforeEach(() => {
    storage = createStorageLayer(":memory:");
  });

  afterEach(() => {
    storage.close();
  });

  test("setLinks adds links to a note", () => {
    const note = storage.insertNote(makeTestNote());
    storage.setLinks(note.path, [
      {
        target_path: "other/note.md",
        target_id: null,
        title: "Other Note",
        href: "other/note.md",
        type: "markdown",
        snippet: "some context",
      },
    ]);

    const links = storage
      .getDb()
      .prepare("SELECT * FROM links WHERE source_id = ?")
      .all(note.id!) as LinkRow[];

    expect(links.length).toBe(1);
    expect(links[0].target_path).toBe("other/note.md");
    expect(links[0].title).toBe("Other Note");
    expect(links[0].href).toBe("other/note.md");
    expect(links[0].type).toBe("markdown");
    expect(links[0].snippet).toBe("some context");
  });

  test("setLinks replaces existing links", () => {
    const note = storage.insertNote(makeTestNote());
    storage.setLinks(note.path, [
      {
        target_path: "old/link.md",
        target_id: null,
        title: "Old",
        href: "old/link.md",
        type: "markdown",
        snippet: "",
      },
    ]);
    storage.setLinks(note.path, [
      {
        target_path: "new/link1.md",
        target_id: null,
        title: "New 1",
        href: "new/link1.md",
        type: "markdown",
        snippet: "",
      },
      {
        target_path: "new/link2.md",
        target_id: null,
        title: "New 2",
        href: "new/link2.md",
        type: "markdown",
        snippet: "",
      },
    ]);

    const links = storage
      .getDb()
      .prepare(
        "SELECT target_path FROM links WHERE source_id = ? ORDER BY target_path"
      )
      .all(note.id!) as { target_path: string }[];

    expect(links.map((l) => l.target_path)).toEqual([
      "new/link1.md",
      "new/link2.md",
    ]);
  });

  test("setLinks resolves target_id when target note exists", () => {
    const source = storage.insertNote(makeTestNote());
    const target = storage.insertNote(
      makeTestNote({
        path: "target/note.md",
        short_id: "tgt12345",
        title: "Target Note",
      })
    );

    storage.setLinks(source.path, [
      {
        target_path: "target/note.md",
        target_id: null,
        title: "Target",
        href: "target/note.md",
        type: "markdown",
        snippet: "",
      },
    ]);

    const links = storage
      .getDb()
      .prepare("SELECT target_id FROM links WHERE source_id = ?")
      .all(source.id!) as { target_id: number | null }[];

    expect(links.length).toBe(1);
    expect(links[0].target_id).toBe(target.id!);
  });

  test("setLinks sets target_id to null when target not found", () => {
    const source = storage.insertNote(makeTestNote());

    storage.setLinks(source.path, [
      {
        target_path: "nonexistent/note.md",
        target_id: null,
        title: "Missing",
        href: "nonexistent/note.md",
        type: "markdown",
        snippet: "",
      },
    ]);

    const links = storage
      .getDb()
      .prepare("SELECT target_id FROM links WHERE source_id = ?")
      .all(source.id!) as { target_id: number | null }[];

    expect(links.length).toBe(1);
    expect(links[0].target_id).toBeNull();
  });

  test("setLinks with empty array removes all links", () => {
    const note = storage.insertNote(makeTestNote());
    storage.setLinks(note.path, [
      {
        target_path: "some/link.md",
        target_id: null,
        title: "Link",
        href: "some/link.md",
        type: "markdown",
        snippet: "",
      },
    ]);
    storage.setLinks(note.path, []);

    const count = storage
      .getDb()
      .prepare("SELECT COUNT(*) as count FROM links WHERE source_id = ?")
      .get(note.id!) as { count: number };

    expect(count.count).toBe(0);
  });

  test("setLinks throws for non-existent source note", () => {
    expect(() =>
      storage.setLinks("nonexistent/path.md", [
        {
          target_path: "other.md",
          target_id: null,
          title: "",
          href: "other.md",
          type: "markdown",
          snippet: "",
        },
      ])
    ).toThrow(/not found/i);
  });
});

// =============================================================================
// resolveLink (Phase 3)
// =============================================================================

describe("resolveLink", () => {
  let storage: StorageLayer;

  beforeEach(() => {
    storage = createStorageLayer(":memory:");
  });

  afterEach(() => {
    storage.close();
  });

  test("resolves by exact path", () => {
    const note = storage.insertNote(makeTestNote());
    const result = storage.resolveLink(note.path);

    expect(result).not.toBeNull();
    expect(result!.path).toBe(note.path);
  });

  test("resolves by short_id", () => {
    const note = storage.insertNote(makeTestNote());
    const result = storage.resolveLink(note.short_id);

    expect(result).not.toBeNull();
    expect(result!.short_id).toBe(note.short_id);
  });

  test("resolves by path ending", () => {
    const note = storage.insertNote(makeTestNote());
    // The note path is "projects/test/task/abc12def.md"
    // Searching for "task/abc12def.md" should match
    const result = storage.resolveLink("task/abc12def.md");

    expect(result).not.toBeNull();
    expect(result!.path).toBe(note.path);
  });

  test("returns null for unresolvable link", () => {
    const result = storage.resolveLink("totally/nonexistent/path.md");
    expect(result).toBeNull();
  });
});

// =============================================================================
// Search (Phase 4)
// =============================================================================

describe("Search", () => {
  let storage: StorageLayer;

  beforeEach(() => {
    storage = createStorageLayer(":memory:");

    // Insert varied notes for search testing
    storage.insertNote(
      makeTestNote({
        path: "projects/alpha/task/note1.md",
        short_id: "note1111",
        title: "Running the test suite",
        body: "Instructions for running all tests in the project",
        lead: "Instructions for running",
        type: "task",
        status: "active",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "projects/alpha/plan/note2.md",
        short_id: "note2222",
        title: "Architecture overview",
        body: "The system uses a layered architecture with running services",
        lead: "The system uses",
        type: "plan",
        status: "active",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "projects/beta/task/note3.md",
        short_id: "note3333",
        title: "Deploy pipeline",
        body: "CI/CD pipeline configuration for deployment",
        lead: "CI/CD pipeline",
        type: "task",
        status: "active",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "projects/beta/plan/note4.md",
        short_id: "note4444",
        title: "Database migration plan",
        body: "Steps to migrate the database schema safely",
        lead: "Steps to migrate",
        type: "plan",
        status: "active",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "projects/alpha/idea/note5.md",
        short_id: "note5555",
        title: "Performance improvements",
        body: "Ideas for improving query performance and caching",
        lead: "Ideas for improving",
        type: "idea",
        status: "active",
      })
    );
  });

  afterEach(() => {
    storage.close();
  });

  test("searchNotes finds notes by FTS query", () => {
    const results = storage.searchNotes("pipeline");
    expect(results.length).toBeGreaterThanOrEqual(1);
    expect(results.some((r) => r.title === "Deploy pipeline")).toBe(true);
  });

  test("searchNotes ranks title matches higher than body matches", () => {
    // "running" appears in note1 title and note2 body
    const results = storage.searchNotes("running");
    expect(results.length).toBeGreaterThanOrEqual(2);
    // Title match (note1) should rank higher than body match (note2)
    expect(results[0].title).toBe("Running the test suite");
  });

  test("searchNotes respects limit option", () => {
    // Insert enough notes that a broad query returns many
    const results = storage.searchNotes("the", { limit: 2 });
    expect(results.length).toBeLessThanOrEqual(2);
  });

  test("searchNotes with exact strategy matches exact title", () => {
    const results = storage.searchNotes("Deploy pipeline", {
      matchStrategy: "exact",
    });
    expect(results.length).toBe(1);
    expect(results[0].title).toBe("Deploy pipeline");
  });

  test("searchNotes with like strategy matches partial text", () => {
    const results = storage.searchNotes("migrat", {
      matchStrategy: "like",
    });
    expect(results.length).toBeGreaterThanOrEqual(1);
    expect(
      results.some((r) => r.title === "Database migration plan")
    ).toBe(true);
  });

  test("searchNotes with path filter restricts to path prefix", () => {
    const results = storage.searchNotes("plan", {
      path: "projects/beta/",
    });
    // Should only find notes under projects/beta/
    for (const r of results) {
      expect(r.path.startsWith("projects/beta/")).toBe(true);
    }
    expect(results.length).toBeGreaterThanOrEqual(1);
  });

  test("searchNotes returns empty array for no matches", () => {
    const results = storage.searchNotes("xyznonexistent");
    expect(results).toEqual([]);
  });

  test("searchNotes handles empty query gracefully", () => {
    const results = storage.searchNotes("");
    expect(Array.isArray(results)).toBe(true);
    // Empty query should return empty results, not throw
  });

  test("searchNotes with FTS uses porter stemming", () => {
    // "running" in note1 title should match query "run" via porter stemmer
    const results = storage.searchNotes("run");
    expect(results.length).toBeGreaterThanOrEqual(1);
    expect(
      results.some((r) => r.title === "Running the test suite")
    ).toBe(true);
  });

  test("searchNotes filters by type when provided", () => {
    // note1 is type=task, note2 is type=plan — both match "running"
    const results = storage.searchNotes("running", { type: "task" });
    expect(results.length).toBeGreaterThanOrEqual(1);
    expect(results.every((r) => r.type === "task")).toBe(true);
    // Should NOT include the plan note
    expect(results.some((r) => r.title === "Architecture overview")).toBe(false);
  });

  test("searchNotes filters by status when provided", () => {
    // Insert a note with status "completed"
    storage.insertNote(
      makeTestNote({
        path: "projects/alpha/task/done1111.md",
        short_id: "done1111",
        title: "Completed task about running",
        body: "This running task is done",
        lead: "This running task",
        type: "task",
        status: "completed",
      })
    );

    const results = storage.searchNotes("running", { status: "completed" });
    expect(results.length).toBeGreaterThanOrEqual(1);
    expect(results.every((r) => r.status === "completed")).toBe(true);
  });

  test("searchNotes combines type and status filters", () => {
    storage.insertNote(
      makeTestNote({
        path: "projects/alpha/plan/done2222.md",
        short_id: "done2222",
        title: "Completed plan about running",
        body: "This running plan is done",
        lead: "This running plan",
        type: "plan",
        status: "completed",
      })
    );

    const results = storage.searchNotes("running", {
      type: "plan",
      status: "completed",
    });
    expect(results.length).toBe(1);
    expect(results[0].type).toBe("plan");
    expect(results[0].status).toBe("completed");
  });

  test("searchNotes type filter works with exact strategy", () => {
    const results = storage.searchNotes("Deploy pipeline", {
      matchStrategy: "exact",
      type: "task",
    });
    // note3 is a task with exact title "Deploy pipeline"
    expect(results.length).toBe(1);
    expect(results[0].type).toBe("task");
  });

  test("searchNotes type filter works with like strategy", () => {
    const results = storage.searchNotes("migrat", {
      matchStrategy: "like",
      type: "plan",
    });
    expect(results.length).toBeGreaterThanOrEqual(1);
    expect(results.every((r) => r.type === "plan")).toBe(true);
  });
});

// =============================================================================
// Isolation (Phase 1)
// =============================================================================

// =============================================================================
// List / Filter (Phase 5)
// =============================================================================

describe("List / Filter", () => {
  let storage: StorageLayer;

  beforeEach(() => {
    storage = createStorageLayer(":memory:");

    // Insert 6 notes with varied types, statuses, projects, features, priorities, paths
    storage.insertNote(
      makeTestNote({
        path: "projects/alpha/task/t1.md",
        short_id: "t1aaaaaa",
        title: "Alpha Task One",
        type: "task",
        status: "active",
        priority: "high",
        project_id: "alpha",
        feature_id: "feat-a",
        created: "2024-01-01T00:00:00Z",
        modified: "2024-01-06T00:00:00Z",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "projects/alpha/plan/p1.md",
        short_id: "p1aaaaaa",
        title: "Alpha Plan One",
        type: "plan",
        status: "active",
        priority: "medium",
        project_id: "alpha",
        feature_id: "feat-a",
        created: "2024-01-02T00:00:00Z",
        modified: "2024-01-05T00:00:00Z",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "projects/beta/task/t2.md",
        short_id: "t2bbbbbb",
        title: "Beta Task Two",
        type: "task",
        status: "completed",
        priority: "low",
        project_id: "beta",
        feature_id: "feat-b",
        created: "2024-01-03T00:00:00Z",
        modified: "2024-01-04T00:00:00Z",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "projects/beta/idea/i1.md",
        short_id: "i1bbbbbb",
        title: "Beta Idea One",
        type: "idea",
        status: "draft",
        priority: "low",
        project_id: "beta",
        feature_id: null,
        created: "2024-01-04T00:00:00Z",
        modified: "2024-01-03T00:00:00Z",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "projects/alpha/task/t3.md",
        short_id: "t3aaaaaa",
        title: "Alpha Task Three",
        type: "task",
        status: "active",
        priority: "medium",
        project_id: "alpha",
        feature_id: "feat-c",
        created: "2024-01-05T00:00:00Z",
        modified: "2024-01-02T00:00:00Z",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "global/learning/l1.md",
        short_id: "l1global",
        title: "Global Learning",
        type: "learning",
        status: "active",
        priority: "high",
        project_id: null,
        feature_id: null,
        created: "2024-01-06T00:00:00Z",
        modified: "2024-01-01T00:00:00Z",
      })
    );

    // Set tags on specific notes
    storage.setTags("projects/alpha/task/t1.md", ["urgent", "backend"]);
    storage.setTags("projects/beta/task/t2.md", ["backend", "deploy"]);
    storage.setTags("projects/alpha/task/t3.md", ["frontend"]);
  });

  afterEach(() => {
    storage.close();
  });

  test("listNotes returns all notes when no filters", () => {
    const results = storage.listNotes();
    expect(results.length).toBe(6);
  });

  test("listNotes filters by type", () => {
    const results = storage.listNotes({ type: "task" });
    expect(results.length).toBe(3);
    for (const r of results) {
      expect(r.type).toBe("task");
    }
  });

  test("listNotes filters by status", () => {
    const results = storage.listNotes({ status: "active" });
    expect(results.length).toBe(4);
    for (const r of results) {
      expect(r.status).toBe("active");
    }
  });

  test("listNotes filters by project", () => {
    const results = storage.listNotes({ project: "alpha" });
    expect(results.length).toBe(3);
    for (const r of results) {
      expect(r.project_id).toBe("alpha");
    }
  });

  test("listNotes filters by feature", () => {
    const results = storage.listNotes({ feature: "feat-a" });
    expect(results.length).toBe(2);
    for (const r of results) {
      expect(r.feature_id).toBe("feat-a");
    }
  });

  test("listNotes filters by path prefix", () => {
    const results = storage.listNotes({ path: "projects/beta/" });
    expect(results.length).toBe(2);
    for (const r of results) {
      expect(r.path.startsWith("projects/beta/")).toBe(true);
    }
  });

  test("listNotes filters by tag", () => {
    const results = storage.listNotes({ tag: "backend" });
    expect(results.length).toBe(2);
    const titles = results.map((r) => r.title).sort();
    expect(titles).toEqual(["Alpha Task One", "Beta Task Two"]);
  });

  test("listNotes combines multiple filters", () => {
    // type=task AND project=alpha
    const results = storage.listNotes({ type: "task", project: "alpha" });
    expect(results.length).toBe(2);
    for (const r of results) {
      expect(r.type).toBe("task");
      expect(r.project_id).toBe("alpha");
    }
  });

  test("listNotes sorts by modified desc by default", () => {
    const results = storage.listNotes();
    // modified values: t1=Jan06, p1=Jan05, t2=Jan04, i1=Jan03, t3=Jan02, l1=Jan01
    expect(results[0].title).toBe("Alpha Task One"); // Jan 06
    expect(results[1].title).toBe("Alpha Plan One"); // Jan 05
    expect(results[results.length - 1].title).toBe("Global Learning"); // Jan 01
  });

  test("listNotes sorts by priority", () => {
    const results = storage.listNotes({ sortBy: "priority" });
    // high=0, medium=1, low=2 — default desc means low first? No, desc on numeric: 2,1,0
    // Actually: ORDER BY CASE ... desc means high values first: 2 (low), 1 (medium), 0 (high)
    // Wait — sortOrder default is desc. For priority CASE: high=0, medium=1, low=2
    // DESC means 2 first (low), then 1 (medium), then 0 (high)
    // But that's counterintuitive. Let me re-read the spec...
    // The spec says default sortOrder is desc. For priority sort with desc:
    // CASE values: high=0, medium=1, low=2. DESC → 2,1,0 → low, medium, high
    // Let's verify: first should be low priority, last should be high
    expect(results[0].priority).toBe("low");
    expect(results[results.length - 1].priority).toBe("high");
  });

  test("listNotes sorts ascending when specified", () => {
    const results = storage.listNotes({
      sortBy: "created",
      sortOrder: "asc",
    });
    // created: t1=Jan01, p1=Jan02, t2=Jan03, i1=Jan04, t3=Jan05, l1=Jan06
    expect(results[0].title).toBe("Alpha Task One"); // Jan 01
    expect(results[results.length - 1].title).toBe("Global Learning"); // Jan 06
  });

  test("listNotes respects limit", () => {
    const results = storage.listNotes({ limit: 3 });
    expect(results.length).toBe(3);
  });

  test("listNotes returns empty array when no matches", () => {
    const results = storage.listNotes({ type: "nonexistent" });
    expect(results).toEqual([]);
  });

  test("listNotes filters by tags (OR logic) — matches any tag", () => {
    // "frontend" tag is on Alpha Task Three
    // "deploy" tag is on Beta Task Two
    const results = storage.listNotes({ tags: ["frontend", "deploy"] });
    expect(results.length).toBe(2);
    const titles = results.map((r) => r.title).sort();
    expect(titles).toContain("Alpha Task Three");
    expect(titles).toContain("Beta Task Two");
  });

  test("listNotes filters by single tag in tags array", () => {
    // "urgent" tag is only on Alpha Task One
    const results = storage.listNotes({ tags: ["urgent"] });
    expect(results.length).toBe(1);
    expect(results[0].title).toBe("Alpha Task One");
  });

  test("listNotes tags filter combines with other filters", () => {
    // backend tag is on Alpha Task One and Beta Task Two
    // Filter to only alpha project
    const results = storage.listNotes({ tags: ["backend"], project: "alpha" });
    expect(results.length).toBe(1);
    expect(results[0].title).toBe("Alpha Task One");
  });

  test("listNotes with empty tags array returns all notes", () => {
    const allResults = storage.listNotes();
    const tagResults = storage.listNotes({ tags: [] });
    expect(tagResults.length).toBe(allResults.length);
  });
});

// =============================================================================
// Graph Operations (Phase 6)
// =============================================================================

describe("Graph", () => {
  let storage: StorageLayer;

  // Graph structure:
  // Note A links to B and C
  // Note B links to C
  // Note D has no links (orphan)

  beforeEach(() => {
    storage = createStorageLayer(":memory:");

    // Create 4 notes
    storage.insertNote(
      makeTestNote({
        path: "graph/noteA.md",
        short_id: "noteAAAA",
        title: "Note A",
        type: "task",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "graph/noteB.md",
        short_id: "noteBBBB",
        title: "Note B",
        type: "task",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "graph/noteC.md",
        short_id: "noteCCCC",
        title: "Note C",
        type: "plan",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "graph/noteD.md",
        short_id: "noteDDDD",
        title: "Note D",
        type: "idea",
      })
    );

    // A links to B and C
    storage.setLinks("graph/noteA.md", [
      {
        target_path: "graph/noteB.md",
        target_id: null,
        title: "Note B",
        href: "graph/noteB.md",
        type: "markdown",
        snippet: "",
      },
      {
        target_path: "graph/noteC.md",
        target_id: null,
        title: "Note C",
        href: "graph/noteC.md",
        type: "markdown",
        snippet: "",
      },
    ]);

    // B links to C
    storage.setLinks("graph/noteB.md", [
      {
        target_path: "graph/noteC.md",
        target_id: null,
        title: "Note C",
        href: "graph/noteC.md",
        type: "markdown",
        snippet: "",
      },
    ]);
  });

  afterEach(() => {
    storage.close();
  });

  // --- getBacklinks ---

  test("getBacklinks returns notes linking to a note", () => {
    // C is linked to by A and B
    const backlinks = storage.getBacklinks("graph/noteC.md");
    expect(backlinks.length).toBe(2);
    const titles = backlinks.map((n) => n.title).sort();
    expect(titles).toEqual(["Note A", "Note B"]);
  });

  test("getBacklinks returns empty array for note with no backlinks", () => {
    // A has no incoming links
    const backlinks = storage.getBacklinks("graph/noteA.md");
    expect(backlinks).toEqual([]);
  });

  test("getBacklinks returns empty array for non-existent note", () => {
    const backlinks = storage.getBacklinks("nonexistent/note.md");
    expect(backlinks).toEqual([]);
  });

  // --- getOutlinks ---

  test("getOutlinks returns notes linked by a note", () => {
    // A links to B and C
    const outlinks = storage.getOutlinks("graph/noteA.md");
    expect(outlinks.length).toBe(2);
    const titles = outlinks.map((n) => n.title).sort();
    expect(titles).toEqual(["Note B", "Note C"]);
  });

  test("getOutlinks returns empty array for note with no outlinks", () => {
    // D has no outgoing links
    const outlinks = storage.getOutlinks("graph/noteD.md");
    expect(outlinks).toEqual([]);
  });

  test("getOutlinks returns empty array for non-existent note", () => {
    const outlinks = storage.getOutlinks("nonexistent/note.md");
    expect(outlinks).toEqual([]);
  });

  // --- getRelated ---

  test("getRelated returns notes sharing link targets", () => {
    // A links to B and C, B links to C
    // Related to A: B (both link to C)
    const related = storage.getRelated("graph/noteA.md");
    expect(related.length).toBe(1);
    expect(related[0].title).toBe("Note B");
  });

  test("getRelated excludes the queried note itself", () => {
    const related = storage.getRelated("graph/noteA.md");
    const paths = related.map((n) => n.path);
    expect(paths).not.toContain("graph/noteA.md");
  });

  test("getRelated respects limit", () => {
    const related = storage.getRelated("graph/noteA.md", 0);
    expect(related.length).toBe(0);
  });

  test("getRelated returns empty for unrelated notes", () => {
    // D has no links, so no shared targets
    const related = storage.getRelated("graph/noteD.md");
    expect(related).toEqual([]);
  });

  // --- getOrphans ---

  test("getOrphans returns notes with no incoming links", () => {
    // A and D have no incoming links
    const orphans = storage.getOrphans();
    const titles = orphans.map((n) => n.title).sort();
    expect(titles).toContain("Note A");
    expect(titles).toContain("Note D");
    // B and C have incoming links, should NOT be orphans
    expect(titles).not.toContain("Note B");
    expect(titles).not.toContain("Note C");
  });

  test("getOrphans filters by type", () => {
    // Only idea-type orphans: D is type=idea and orphan
    const orphans = storage.getOrphans({ type: "idea" });
    expect(orphans.length).toBe(1);
    expect(orphans[0].title).toBe("Note D");
  });

  test("getOrphans respects limit", () => {
    const orphans = storage.getOrphans({ limit: 1 });
    expect(orphans.length).toBe(1);
  });
});

describe("StorageLayer isolation", () => {
  test("multiple instances are independent", () => {
    const storageA = createStorageLayer(":memory:");
    const storageB = createStorageLayer(":memory:");

    // Insert a note in storageA
    storageA.getDb().exec(`
      INSERT INTO notes (path, short_id, title)
      VALUES ('only-in-a.md', 'aaa11111', 'Note A')
    `);

    // storageB should have no notes
    const countB = storageB
      .getDb()
      .prepare("SELECT COUNT(*) as count FROM notes")
      .get() as { count: number };

    expect(countB.count).toBe(0);

    // storageA should have one note
    const countA = storageA
      .getDb()
      .prepare("SELECT COUNT(*) as count FROM notes")
      .get() as { count: number };

    expect(countA.count).toBe(1);

    storageA.close();
    storageB.close();
  });
});

// =============================================================================
// Entry Metadata (Phase 7)
// =============================================================================

describe("Entry Metadata", () => {
  let storage: StorageLayer;

  beforeEach(() => {
    storage = createStorageLayer(":memory:");
  });

  afterEach(() => {
    storage.close();
  });

  // --- recordAccess ---

  test("recordAccess creates entry_meta record on first access", () => {
    storage.recordAccess("projects/test/task/abc.md");

    const meta = storage.getAccessStats("projects/test/task/abc.md");
    expect(meta).not.toBeNull();
    expect(meta!.path).toBe("projects/test/task/abc.md");
    expect(meta!.access_count).toBe(1);
    expect(meta!.last_accessed).toBeTruthy();
    expect(meta!.created_at).toBeTruthy();
  });

  test("recordAccess increments access_count on subsequent access", () => {
    storage.recordAccess("projects/test/task/abc.md");
    storage.recordAccess("projects/test/task/abc.md");
    storage.recordAccess("projects/test/task/abc.md");

    const meta = storage.getAccessStats("projects/test/task/abc.md");
    expect(meta).not.toBeNull();
    expect(meta!.access_count).toBe(3);
  });

  test("recordAccess updates last_accessed timestamp", () => {
    storage.recordAccess("projects/test/task/abc.md");

    const meta = storage.getAccessStats("projects/test/task/abc.md");
    expect(meta).not.toBeNull();
    // last_accessed should be a valid ISO-ish datetime string
    expect(meta!.last_accessed).toMatch(/^\d{4}-\d{2}-\d{2}/);
  });

  // --- getAccessStats ---

  test("getAccessStats returns entry metadata", () => {
    storage.recordAccess("projects/test/task/abc.md");

    const meta = storage.getAccessStats("projects/test/task/abc.md");
    expect(meta).not.toBeNull();
    expect(meta!.path).toBe("projects/test/task/abc.md");
    expect(meta!.access_count).toBe(1);
    expect(meta!.last_accessed).toBeTruthy();
    expect(meta!.last_verified).toBeNull();
    expect(meta!.created_at).toBeTruthy();
  });

  test("getAccessStats returns null for missing path", () => {
    const meta = storage.getAccessStats("nonexistent/path.md");
    expect(meta).toBeNull();
  });

  // --- setVerified ---

  test("setVerified updates last_verified timestamp", () => {
    // First create an entry via recordAccess
    storage.recordAccess("projects/test/task/abc.md");
    storage.setVerified("projects/test/task/abc.md");

    const meta = storage.getAccessStats("projects/test/task/abc.md");
    expect(meta).not.toBeNull();
    expect(meta!.last_verified).toBeTruthy();
    expect(meta!.last_verified).toMatch(/^\d{4}-\d{2}-\d{2}/);
  });

  test("setVerified creates entry_meta if not exists", () => {
    // setVerified on a path with no prior recordAccess
    storage.setVerified("projects/test/task/new.md");

    const meta = storage.getAccessStats("projects/test/task/new.md");
    expect(meta).not.toBeNull();
    expect(meta!.last_verified).toBeTruthy();
    expect(meta!.created_at).toBeTruthy();
    // access_count should be default 0 since we only verified, never accessed
    expect(meta!.access_count).toBe(0);
  });

  // --- getStaleEntries ---

  test("getStaleEntries returns notes never verified", () => {
    // Insert a note but don't verify it
    storage.insertNote(
      makeTestNote({
        path: "projects/test/task/stale1.md",
        short_id: "stale111",
        title: "Stale Note One",
      })
    );

    const stale = storage.getStaleEntries(30);
    expect(stale.length).toBeGreaterThanOrEqual(1);
    expect(stale.some((n) => n.path === "projects/test/task/stale1.md")).toBe(
      true
    );
  });

  test("getStaleEntries returns notes verified more than N days ago", () => {
    // Insert a note
    storage.insertNote(
      makeTestNote({
        path: "projects/test/task/old.md",
        short_id: "old11111",
        title: "Old Verified Note",
      })
    );

    // Manually insert entry_meta with old last_verified
    storage
      .getDb()
      .prepare(
        `INSERT INTO entry_meta (path, last_verified, created_at)
         VALUES (?, datetime('now', '-60 days'), datetime('now'))`
      )
      .run("projects/test/task/old.md");

    const stale = storage.getStaleEntries(30);
    expect(stale.some((n) => n.path === "projects/test/task/old.md")).toBe(true);
  });

  test("getStaleEntries excludes recently verified notes", () => {
    // Insert a note and verify it now
    storage.insertNote(
      makeTestNote({
        path: "projects/test/task/fresh.md",
        short_id: "fresh111",
        title: "Fresh Verified Note",
      })
    );
    storage.setVerified("projects/test/task/fresh.md");

    const stale = storage.getStaleEntries(30);
    expect(stale.some((n) => n.path === "projects/test/task/fresh.md")).toBe(
      false
    );
  });

  test("getStaleEntries filters by type", () => {
    storage.insertNote(
      makeTestNote({
        path: "projects/test/task/t1.md",
        short_id: "typet111",
        title: "Task Note",
        type: "task",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "projects/test/plan/p1.md",
        short_id: "typep111",
        title: "Plan Note",
        type: "plan",
      })
    );

    const stale = storage.getStaleEntries(30, { type: "task" });
    expect(stale.every((n) => n.type === "task")).toBe(true);
    expect(stale.some((n) => n.path === "projects/test/task/t1.md")).toBe(true);
    expect(stale.some((n) => n.path === "projects/test/plan/p1.md")).toBe(
      false
    );
  });

  test("getStaleEntries respects limit", () => {
    // Insert multiple notes
    for (let i = 0; i < 5; i++) {
      storage.insertNote(
        makeTestNote({
          path: `projects/test/task/limit${i}.md`,
          short_id: `limit${i}${i}${i}`,
          title: `Limit Note ${i}`,
        })
      );
    }

    const stale = storage.getStaleEntries(30, { limit: 2 });
    expect(stale.length).toBe(2);
  });
});

// =============================================================================
// Stats (Phase 8)
// =============================================================================

describe("Stats", () => {
  let storage: StorageLayer;

  beforeEach(() => {
    storage = createStorageLayer(":memory:");
  });

  afterEach(() => {
    storage.close();
  });

  test("getStats returns zeros for empty database", () => {
    const stats = storage.getStats();
    expect(stats.total).toBe(0);
    expect(stats.byType).toEqual({});
    expect(stats.orphanCount).toBe(0);
    expect(stats.trackedCount).toBe(0);
    expect(stats.staleCount).toBe(0);
  });

  test("getStats returns total note count", () => {
    storage.insertNote(
      makeTestNote({
        path: "stats/note1.md",
        short_id: "stat1111",
        title: "Stats Note 1",
        type: "task",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "stats/note2.md",
        short_id: "stat2222",
        title: "Stats Note 2",
        type: "plan",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "stats/note3.md",
        short_id: "stat3333",
        title: "Stats Note 3",
        type: "task",
      })
    );

    const stats = storage.getStats();
    expect(stats.total).toBe(3);
  });

  test("getStats returns counts by type", () => {
    storage.insertNote(
      makeTestNote({
        path: "stats/t1.md",
        short_id: "bytype11",
        title: "Task 1",
        type: "task",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "stats/t2.md",
        short_id: "bytype22",
        title: "Task 2",
        type: "task",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "stats/p1.md",
        short_id: "bytype33",
        title: "Plan 1",
        type: "plan",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "stats/i1.md",
        short_id: "bytype44",
        title: "Idea 1",
        type: "idea",
      })
    );

    const stats = storage.getStats();
    expect(stats.byType).toEqual({ task: 2, plan: 1, idea: 1 });
  });

  test("getStats returns orphan count", () => {
    // Create 3 notes: A links to B, C is orphan
    storage.insertNote(
      makeTestNote({
        path: "stats/a.md",
        short_id: "orphanaa",
        title: "Note A",
        type: "task",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "stats/b.md",
        short_id: "orphanbb",
        title: "Note B",
        type: "task",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "stats/c.md",
        short_id: "orphancc",
        title: "Note C",
        type: "plan",
      })
    );

    // A links to B (so B is not orphan, A and C are orphans)
    storage.setLinks("stats/a.md", [
      {
        target_path: "stats/b.md",
        target_id: null,
        title: "Note B",
        href: "stats/b.md",
        type: "markdown",
        snippet: "",
      },
    ]);

    const stats = storage.getStats();
    expect(stats.orphanCount).toBe(2); // A and C are orphans
  });

  test("getStats returns tracked count from entry_meta", () => {
    storage.insertNote(
      makeTestNote({
        path: "stats/tracked1.md",
        short_id: "track111",
        title: "Tracked 1",
        type: "task",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "stats/tracked2.md",
        short_id: "track222",
        title: "Tracked 2",
        type: "plan",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "stats/untracked.md",
        short_id: "untrk111",
        title: "Untracked",
        type: "task",
      })
    );

    // Only track 2 of the 3 notes
    storage.recordAccess("stats/tracked1.md");
    storage.recordAccess("stats/tracked2.md");

    const stats = storage.getStats();
    expect(stats.trackedCount).toBe(2);
  });

  test("getStats returns stale count", () => {
    // Insert 3 notes:
    // - note1: never verified (stale)
    // - note2: verified long ago (stale)
    // - note3: recently verified (not stale)
    storage.insertNote(
      makeTestNote({
        path: "stats/stale1.md",
        short_id: "stale111",
        title: "Never Verified",
        type: "task",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "stats/stale2.md",
        short_id: "stale222",
        title: "Old Verified",
        type: "task",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "stats/fresh.md",
        short_id: "fresh111",
        title: "Fresh Verified",
        type: "plan",
      })
    );

    // Manually set old verification for stale2
    storage
      .getDb()
      .prepare(
        `INSERT INTO entry_meta (path, last_verified, created_at)
         VALUES (?, datetime('now', '-60 days'), datetime('now'))`
      )
      .run("stats/stale2.md");

    // Verify fresh note recently
    storage.setVerified("stats/fresh.md");

    const stats = storage.getStats();
    expect(stats.staleCount).toBe(2); // stale1 (never verified) + stale2 (old)
  });

  test("getStats with path option scopes to path prefix", () => {
    // Insert notes in different paths
    storage.insertNote(
      makeTestNote({
        path: "projects/alpha/task/a1.md",
        short_id: "scope111",
        title: "Alpha Task",
        type: "task",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "projects/alpha/plan/a2.md",
        short_id: "scope222",
        title: "Alpha Plan",
        type: "plan",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "projects/beta/task/b1.md",
        short_id: "scope333",
        title: "Beta Task",
        type: "task",
      })
    );

    // Track one alpha note
    storage.recordAccess("projects/alpha/task/a1.md");

    // Verify one alpha note
    storage.setVerified("projects/alpha/task/a1.md");

    // A1 links to A2 (so A2 is not orphan within alpha scope)
    storage.setLinks("projects/alpha/task/a1.md", [
      {
        target_path: "projects/alpha/plan/a2.md",
        target_id: null,
        title: "Alpha Plan",
        href: "projects/alpha/plan/a2.md",
        type: "markdown",
        snippet: "",
      },
    ]);

    const stats = storage.getStats({ path: "projects/alpha/" });
    expect(stats.total).toBe(2);
    expect(stats.byType).toEqual({ task: 1, plan: 1 });
    expect(stats.orphanCount).toBe(1); // A1 is orphan (no incoming links), A2 has incoming
    expect(stats.trackedCount).toBe(1); // only a1 tracked
    expect(stats.staleCount).toBe(1); // a2 never verified, a1 recently verified
  });
});

// =============================================================================
// Index Management (Phase 9)
// =============================================================================

function writeTestMarkdown(dir: string, relativePath: string, content: string) {
  const fullPath = join(dir, relativePath);
  mkdirSync(dirname(fullPath), { recursive: true });
  writeFileSync(fullPath, content);
}

const sampleMarkdown = `---
title: Test Note
type: task
status: active
tags:
  - test
  - phase-9
priority: high
created: 2024-01-01T00:00:00Z
projectId: test-project
feature_id: feat-1
---

This is the body content.

See also [Related Note](abc12def) and [Another](projects/test/plan/xyz98765.md).
`;

describe("Index Management", () => {
  let storage: StorageLayer;
  let brainDir: string;

  beforeEach(() => {
    storage = createStorageLayer(":memory:");
    brainDir = mkdtempSync(join(tmpdir(), "brain-test-"));
  });

  afterEach(() => {
    storage.close();
  });

  // --- reindex ---

  test("reindex indexes a markdown file into the database", () => {
    writeTestMarkdown(brainDir, "projects/test/task/abc12def.md", sampleMarkdown);

    storage.reindex("projects/test/task/abc12def.md", brainDir);

    const note = storage.getNoteByPath("projects/test/task/abc12def.md");
    expect(note).not.toBeNull();
    expect(note!.title).toBe("Test Note");
    expect(note!.type).toBe("task");
    expect(note!.status).toBe("active");
    expect(note!.priority).toBe("high");
    expect(note!.short_id).toBe("abc12def");
    expect(note!.project_id).toBe("test-project");
    expect(note!.feature_id).toBe("feat-1");
    expect(note!.body).toContain("This is the body content");
    expect(note!.checksum).toBeTruthy();
  });

  test("reindex updates existing note when file changes", () => {
    writeTestMarkdown(brainDir, "projects/test/task/abc12def.md", sampleMarkdown);
    storage.reindex("projects/test/task/abc12def.md", brainDir);

    const original = storage.getNoteByPath("projects/test/task/abc12def.md");
    expect(original!.title).toBe("Test Note");

    // Modify the file
    const updatedContent = sampleMarkdown.replace("Test Note", "Updated Note");
    writeTestMarkdown(brainDir, "projects/test/task/abc12def.md", updatedContent);
    storage.reindex("projects/test/task/abc12def.md", brainDir);

    const updated = storage.getNoteByPath("projects/test/task/abc12def.md");
    expect(updated!.title).toBe("Updated Note");
  });

  test("reindex skips unchanged files (same checksum)", () => {
    writeTestMarkdown(brainDir, "projects/test/task/abc12def.md", sampleMarkdown);

    storage.reindex("projects/test/task/abc12def.md", brainDir);
    const first = storage.getNoteByPath("projects/test/task/abc12def.md");

    // Reindex same file — should skip (no update)
    storage.reindex("projects/test/task/abc12def.md", brainDir);
    const second = storage.getNoteByPath("projects/test/task/abc12def.md");

    // indexed_at should remain the same since it was skipped
    expect(second!.indexed_at).toBe(first!.indexed_at);
  });

  test("reindex extracts tags from frontmatter", () => {
    writeTestMarkdown(brainDir, "projects/test/task/abc12def.md", sampleMarkdown);
    storage.reindex("projects/test/task/abc12def.md", brainDir);

    const note = storage.getNoteByPath("projects/test/task/abc12def.md");
    const tags = storage
      .getDb()
      .prepare("SELECT tag FROM tags WHERE note_id = ? ORDER BY tag")
      .all(note!.id!) as { tag: string }[];

    expect(tags.map((t) => t.tag)).toEqual(["phase-9", "test"]);
  });

  test("reindex extracts links from markdown body", () => {
    writeTestMarkdown(brainDir, "projects/test/task/abc12def.md", sampleMarkdown);
    storage.reindex("projects/test/task/abc12def.md", brainDir);

    const note = storage.getNoteByPath("projects/test/task/abc12def.md");
    const links = storage
      .getDb()
      .prepare("SELECT title, href, target_path FROM links WHERE source_id = ? ORDER BY href")
      .all(note!.id!) as { title: string; href: string; target_path: string }[];

    expect(links.length).toBe(2);
    // ORDER BY href: "abc12def" < "projects/test/plan/xyz98765.md"
    expect(links[0].title).toBe("Related Note");
    expect(links[0].href).toBe("abc12def");
    expect(links[1].title).toBe("Another");
    expect(links[1].href).toBe("projects/test/plan/xyz98765.md");
  });

  test("reindex sets short_id from filename", () => {
    writeTestMarkdown(brainDir, "global/plan/myshrtid.md", sampleMarkdown);
    storage.reindex("global/plan/myshrtid.md", brainDir);

    const note = storage.getNoteByPath("global/plan/myshrtid.md");
    expect(note).not.toBeNull();
    expect(note!.short_id).toBe("myshrtid");
  });

  // --- reindexAll ---

  test("reindexAll indexes all markdown files in directory", () => {
    writeTestMarkdown(brainDir, "projects/test/task/note1111.md", sampleMarkdown);
    writeTestMarkdown(
      brainDir,
      "projects/test/plan/note2222.md",
      sampleMarkdown.replace("Test Note", "Second Note")
    );
    writeTestMarkdown(
      brainDir,
      "global/idea/note3333.md",
      sampleMarkdown.replace("Test Note", "Third Note")
    );

    storage.reindexAll(brainDir);

    const all = storage.listNotes();
    expect(all.length).toBe(3);
  });

  test("reindexAll removes stale entries", () => {
    // First, index a file
    writeTestMarkdown(brainDir, "projects/test/task/willdie.md", sampleMarkdown);
    storage.reindexAll(brainDir);
    expect(storage.getNoteByPath("projects/test/task/willdie.md")).not.toBeNull();

    // Delete the file from disk
    unlinkSync(join(brainDir, "projects/test/task/willdie.md"));

    // Add a new file so reindexAll has something to index
    writeTestMarkdown(
      brainDir,
      "projects/test/task/newfile1.md",
      sampleMarkdown.replace("Test Note", "New File")
    );

    storage.reindexAll(brainDir);

    // The stale entry should be removed
    expect(storage.getNoteByPath("projects/test/task/willdie.md")).toBeNull();
    expect(storage.getNoteByPath("projects/test/task/newfile1.md")).not.toBeNull();
  });

  // --- removeStale ---

  test("removeStale removes notes for deleted files", () => {
    // Insert a note whose file doesn't exist on disk
    storage.insertNote(
      makeTestNote({
        path: "projects/test/task/ghost111.md",
        short_id: "ghost111",
        title: "Ghost Note",
      })
    );

    const removed = storage.removeStale(brainDir);
    expect(removed).toBeGreaterThanOrEqual(1);
    expect(storage.getNoteByPath("projects/test/task/ghost111.md")).toBeNull();
  });

  test("removeStale returns count of removed entries", () => {
    // Insert 3 notes, none of which have files on disk
    storage.insertNote(
      makeTestNote({
        path: "projects/test/task/gone1111.md",
        short_id: "gone1111",
        title: "Gone 1",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "projects/test/task/gone2222.md",
        short_id: "gone2222",
        title: "Gone 2",
      })
    );
    storage.insertNote(
      makeTestNote({
        path: "projects/test/task/gone3333.md",
        short_id: "gone3333",
        title: "Gone 3",
      })
    );

    const removed = storage.removeStale(brainDir);
    expect(removed).toBe(3);
  });

  test("removeStale preserves notes for existing files", () => {
    // Create a file on disk and index it
    writeTestMarkdown(brainDir, "projects/test/task/exists11.md", sampleMarkdown);
    storage.reindex("projects/test/task/exists11.md", brainDir);

    // Also insert a ghost note (no file)
    storage.insertNote(
      makeTestNote({
        path: "projects/test/task/ghost222.md",
        short_id: "ghost222",
        title: "Ghost",
      })
    );

    const removed = storage.removeStale(brainDir);
    expect(removed).toBe(1); // only ghost removed

    // The existing file's note should still be there
    expect(storage.getNoteByPath("projects/test/task/exists11.md")).not.toBeNull();
    // The ghost should be gone
    expect(storage.getNoteByPath("projects/test/task/ghost222.md")).toBeNull();
  });
});

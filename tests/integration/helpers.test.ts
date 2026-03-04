/**
 * Integration Test Helpers — Verification Tests
 *
 * Verifies that the test infrastructure (helpers + fixtures) works correctly
 * before other integration test phases depend on them.
 */

import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import { existsSync, readFileSync } from "fs";
import { join } from "path";

import {
  createTestStorage,
  makeTestNote,
  createTempBrainDir,
  cleanupTempDir,
  writeTestMarkdownFile,
  type StorageLayer,
  type NoteRow,
} from "./helpers";

import {
  FIXTURE_NOTES,
  FIXTURE_LINKS,
  seedStorage,
  seedBrainDir,
} from "./fixtures";

// =============================================================================
// createTestStorage
// =============================================================================

describe("createTestStorage", () => {
  let storage: StorageLayer;

  afterEach(() => {
    storage?.close();
  });

  test("returns a StorageLayer instance", () => {
    storage = createTestStorage();
    expect(storage).toBeDefined();
    expect(storage.getDb).toBeDefined();
  });

  test("has schema applied — notes table exists", () => {
    storage = createTestStorage();
    const tables = storage
      .getDb()
      .prepare(
        "SELECT name FROM sqlite_master WHERE type='table' AND name='notes'"
      )
      .all() as { name: string }[];
    expect(tables.length).toBe(1);
  });

  test("can insert and retrieve a note", () => {
    storage = createTestStorage();
    const note = makeTestNote();
    const inserted = storage.insertNote(note);
    expect(inserted.id).toBeGreaterThan(0);

    const retrieved = storage.getNoteByPath(note.path);
    expect(retrieved).not.toBeNull();
    expect(retrieved!.title).toBe(note.title);
  });

  test("multiple instances are isolated", () => {
    storage = createTestStorage();
    const storage2 = createTestStorage();

    storage.insertNote(makeTestNote({ path: "a.md", short_id: "aaaaaaaa" }));

    const count = storage2
      .getDb()
      .prepare("SELECT COUNT(*) as c FROM notes")
      .get() as { c: number };
    expect(count.c).toBe(0);

    storage2.close();
  });
});

// =============================================================================
// makeTestNote
// =============================================================================

describe("makeTestNote", () => {
  test("returns a note with all required fields", () => {
    const note = makeTestNote();
    expect(note.path).toBe("projects/test/task/abc12def.md");
    expect(note.short_id).toBe("abc12def");
    expect(note.title).toBe("Test Note");
    expect(note.lead).toBe("A test note");
    expect(note.body).toBe("This is the body content");
    expect(note.raw_content).toContain("title: Test Note");
    expect(note.word_count).toBe(6);
    expect(note.checksum).toBe("abc123");
    expect(note.metadata).toBeTruthy();
    expect(note.type).toBe("task");
    expect(note.status).toBe("active");
    expect(note.priority).toBe("medium");
    expect(note.project_id).toBe("test");
    expect(note.feature_id).toBeNull();
    expect(note.created).toBeTruthy();
    expect(note.modified).toBeTruthy();
  });

  test("accepts overrides for any field", () => {
    const note = makeTestNote({
      path: "custom/path.md",
      short_id: "custom11",
      title: "Custom Title",
      type: "plan",
      status: "completed",
      priority: "high",
      project_id: "custom-project",
      feature_id: "feat-x",
    });

    expect(note.path).toBe("custom/path.md");
    expect(note.short_id).toBe("custom11");
    expect(note.title).toBe("Custom Title");
    expect(note.type).toBe("plan");
    expect(note.status).toBe("completed");
    expect(note.priority).toBe("high");
    expect(note.project_id).toBe("custom-project");
    expect(note.feature_id).toBe("feat-x");
  });

  test("does not include id or indexed_at", () => {
    const note = makeTestNote();
    expect("id" in note).toBe(false);
    expect("indexed_at" in note).toBe(false);
  });

  test("is insertable into storage", () => {
    const storage = createTestStorage();
    const note = makeTestNote();
    const inserted = storage.insertNote(note);
    expect(inserted.id).toBeGreaterThan(0);
    expect(inserted.indexed_at).toBeTruthy();
    storage.close();
  });
});

// =============================================================================
// createTempBrainDir / cleanupTempDir
// =============================================================================

describe("createTempBrainDir / cleanupTempDir", () => {
  let tempDir: string | null = null;

  afterEach(() => {
    if (tempDir) {
      cleanupTempDir(tempDir);
      tempDir = null;
    }
  });

  test("createTempBrainDir creates a directory that exists", () => {
    tempDir = createTempBrainDir();
    expect(existsSync(tempDir!)).toBe(true);
  });

  test("createTempBrainDir returns unique paths on each call", () => {
    tempDir = createTempBrainDir();
    const tempDir2 = createTempBrainDir();
    expect(tempDir).not.toBe(tempDir2);
    cleanupTempDir(tempDir2);
  });

  test("cleanupTempDir removes the directory", () => {
    tempDir = createTempBrainDir();
    const dirPath = tempDir!;
    cleanupTempDir(dirPath);
    tempDir = null;
    expect(existsSync(dirPath)).toBe(false);
  });

  test("cleanupTempDir is safe to call on non-existent path", () => {
    expect(() => cleanupTempDir("/tmp/nonexistent-brain-test-dir-xyz")).not.toThrow();
  });
});

// =============================================================================
// writeTestMarkdownFile
// =============================================================================

describe("writeTestMarkdownFile", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = createTempBrainDir();
  });

  afterEach(() => {
    cleanupTempDir(tempDir);
  });

  test("creates a markdown file with frontmatter and body", () => {
    writeTestMarkdownFile(
      tempDir,
      "projects/alpha/task/abc12def.md",
      { title: "Test Note", type: "task", status: "active" },
      "This is the body."
    );

    const fullPath = join(tempDir, "projects/alpha/task/abc12def.md");
    expect(existsSync(fullPath)).toBe(true);

    const content = readFileSync(fullPath, "utf-8");
    expect(content).toContain("---");
    expect(content).toContain("title: Test Note");
    expect(content).toContain("type: task");
    expect(content).toContain("status: active");
    expect(content).toContain("This is the body.");
  });

  test("creates nested directories automatically", () => {
    writeTestMarkdownFile(
      tempDir,
      "deeply/nested/path/note.md",
      { title: "Deep Note" },
      "Body"
    );

    const fullPath = join(tempDir, "deeply/nested/path/note.md");
    expect(existsSync(fullPath)).toBe(true);
  });

  test("handles array values in frontmatter (tags)", () => {
    writeTestMarkdownFile(
      tempDir,
      "note.md",
      { title: "Tagged", tags: ["tag1", "tag2", "tag3"] },
      "Body"
    );

    const content = readFileSync(join(tempDir, "note.md"), "utf-8");
    expect(content).toContain("tags:");
    expect(content).toContain("  - tag1");
    expect(content).toContain("  - tag2");
    expect(content).toContain("  - tag3");
  });

  test("skips null/undefined frontmatter values", () => {
    writeTestMarkdownFile(
      tempDir,
      "note.md",
      { title: "Note", feature_id: null, project_id: undefined },
      "Body"
    );

    const content = readFileSync(join(tempDir, "note.md"), "utf-8");
    expect(content).toContain("title: Note");
    expect(content).not.toContain("feature_id");
    expect(content).not.toContain("project_id");
  });

  test("file is parseable by storage reindex", () => {
    writeTestMarkdownFile(
      tempDir,
      "projects/test/task/abc12def.md",
      {
        title: "Reindexable Note",
        type: "task",
        status: "active",
        priority: "high",
        tags: ["test", "integration"],
        created: "2024-01-01T00:00:00Z",
      },
      "This is the body content for reindexing."
    );

    const storage = createTestStorage();
    storage.reindex("projects/test/task/abc12def.md", tempDir);

    const note = storage.getNoteByPath("projects/test/task/abc12def.md");
    expect(note).not.toBeNull();
    expect(note!.title).toBe("Reindexable Note");
    expect(note!.type).toBe("task");
    expect(note!.status).toBe("active");
    expect(note!.body).toContain("This is the body content for reindexing.");

    storage.close();
  });
});

// =============================================================================
// FIXTURE_NOTES
// =============================================================================

describe("FIXTURE_NOTES", () => {
  test("contains at least 20 notes", () => {
    expect(FIXTURE_NOTES.length).toBeGreaterThanOrEqual(20);
  });

  test("all notes have unique paths", () => {
    const paths = FIXTURE_NOTES.map((n) => n.path);
    const uniquePaths = new Set(paths);
    expect(uniquePaths.size).toBe(paths.length);
  });

  test("all notes have unique short_ids", () => {
    const ids = FIXTURE_NOTES.map((n) => n.short_id);
    const uniqueIds = new Set(ids);
    expect(uniqueIds.size).toBe(ids.length);
  });

  test("covers multiple projects (alpha, beta, gamma)", () => {
    const projects = new Set(
      FIXTURE_NOTES.map((n) => n.project_id).filter(Boolean)
    );
    expect(projects.has("alpha")).toBe(true);
    expect(projects.has("beta")).toBe(true);
    expect(projects.has("gamma")).toBe(true);
  });

  test("covers various types", () => {
    const types = new Set(FIXTURE_NOTES.map((n) => n.type).filter(Boolean));
    expect(types.has("task")).toBe(true);
    expect(types.has("plan")).toBe(true);
    expect(types.has("exploration")).toBe(true);
    expect(types.has("idea")).toBe(true);
    expect(types.has("learning")).toBe(true);
    expect(types.has("summary")).toBe(true);
    expect(types.has("decision")).toBe(true);
  });

  test("covers various statuses", () => {
    const statuses = new Set(
      FIXTURE_NOTES.map((n) => n.status).filter(Boolean)
    );
    expect(statuses.has("active")).toBe(true);
    expect(statuses.has("completed")).toBe(true);
    expect(statuses.has("draft")).toBe(true);
    expect(statuses.has("in_progress")).toBe(true);
    expect(statuses.has("blocked")).toBe(true);
  });

  test("covers various priorities", () => {
    const priorities = new Set(
      FIXTURE_NOTES.map((n) => n.priority).filter(Boolean)
    );
    expect(priorities.has("high")).toBe(true);
    expect(priorities.has("medium")).toBe(true);
    expect(priorities.has("low")).toBe(true);
  });

  test("includes notes with unicode in titles", () => {
    const hasUnicode = FIXTURE_NOTES.some(
      (n) => /[^\x00-\x7F]/.test(n.title)
    );
    expect(hasUnicode).toBe(true);
  });

  test("all notes are insertable into storage", () => {
    const storage = createTestStorage();
    for (const note of FIXTURE_NOTES) {
      const inserted = storage.insertNote(note);
      expect(inserted.id).toBeGreaterThan(0);
    }
    storage.close();
  });
});

// =============================================================================
// FIXTURE_LINKS
// =============================================================================

describe("FIXTURE_LINKS", () => {
  test("contains at least 5 link relationships", () => {
    expect(FIXTURE_LINKS.length).toBeGreaterThanOrEqual(5);
  });

  test("all source_paths reference fixture notes", () => {
    const notePaths = new Set(FIXTURE_NOTES.map((n) => n.path));
    for (const link of FIXTURE_LINKS) {
      expect(notePaths.has(link.source_path)).toBe(true);
    }
  });

  test("all target_paths reference fixture notes", () => {
    const notePaths = new Set(FIXTURE_NOTES.map((n) => n.path));
    for (const link of FIXTURE_LINKS) {
      expect(notePaths.has(link.target_path)).toBe(true);
    }
  });
});

// =============================================================================
// seedStorage
// =============================================================================

describe("seedStorage", () => {
  let storage: StorageLayer;

  beforeEach(() => {
    storage = createTestStorage();
  });

  afterEach(() => {
    storage.close();
  });

  test("inserts all fixture notes into storage", () => {
    seedStorage(storage);

    const count = storage
      .getDb()
      .prepare("SELECT COUNT(*) as c FROM notes")
      .get() as { c: number };
    expect(count.c).toBe(FIXTURE_NOTES.length);
  });

  test("inserts all fixture links into storage", () => {
    seedStorage(storage);

    const count = storage
      .getDb()
      .prepare("SELECT COUNT(*) as c FROM links")
      .get() as { c: number };
    expect(count.c).toBe(FIXTURE_LINKS.length);
  });

  test("notes are retrievable by path after seeding", () => {
    seedStorage(storage);

    for (const fixture of FIXTURE_NOTES) {
      const note = storage.getNoteByPath(fixture.path);
      expect(note).not.toBeNull();
      expect(note!.title).toBe(fixture.title);
      expect(note!.type).toBe(fixture.type);
    }
  });

  test("links have resolved target_ids where targets exist", () => {
    seedStorage(storage);

    const links = storage
      .getDb()
      .prepare("SELECT * FROM links WHERE target_id IS NOT NULL")
      .all() as { target_id: number; target_path: string }[];

    // All fixture links point to fixture notes, so all should be resolved
    expect(links.length).toBe(FIXTURE_LINKS.length);
  });
});

// =============================================================================
// seedBrainDir
// =============================================================================

describe("seedBrainDir", () => {
  let tempDir: string;

  beforeEach(() => {
    tempDir = createTempBrainDir();
  });

  afterEach(() => {
    cleanupTempDir(tempDir);
  });

  test("writes all fixture notes as markdown files", () => {
    seedBrainDir(tempDir);

    for (const fixture of FIXTURE_NOTES) {
      const fullPath = join(tempDir, fixture.path);
      expect(existsSync(fullPath)).toBe(true);
    }
  });

  test("written files contain proper frontmatter", () => {
    seedBrainDir(tempDir);

    // Check a specific fixture note
    const firstNote = FIXTURE_NOTES[0];
    const content = readFileSync(join(tempDir, firstNote.path), "utf-8");
    expect(content).toContain("---");
    expect(content).toContain(`title: ${firstNote.title}`);
    if (firstNote.type) {
      expect(content).toContain(`type: ${firstNote.type}`);
    }
  });

  test("written files are reindexable by storage", () => {
    seedBrainDir(tempDir);

    const storage = createTestStorage();
    storage.reindexAll(tempDir);

    const count = storage
      .getDb()
      .prepare("SELECT COUNT(*) as c FROM notes")
      .get() as { c: number };
    expect(count.c).toBe(FIXTURE_NOTES.length);

    storage.close();
  });
});

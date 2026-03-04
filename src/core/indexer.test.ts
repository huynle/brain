/**
 * Tests for indexer.ts
 *
 * Covers: Indexer.rebuildAll(), indexChanged(), indexFile(), removeFile(), getHealth()
 * Integration tests: real parser + on-disk DB + real markdown files
 */

import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import {
  mkdtempSync,
  writeFileSync,
  mkdirSync,
  rmSync,
  unlinkSync,
} from "node:fs";
import { join, dirname } from "node:path";
import { tmpdir } from "node:os";
import { createStorageLayer, type StorageLayer } from "./storage";
import { parseFile } from "./file-parser";
import { Indexer, type IndexResult } from "./indexer";

// =============================================================================
// Test Helpers
// =============================================================================

let tempDir: string;
let storage: StorageLayer;

beforeEach(() => {
  tempDir = mkdtempSync(join(tmpdir(), "indexer-test-"));
  storage = createStorageLayer(":memory:");
});

afterEach(() => {
  storage.close();
  rmSync(tempDir, { recursive: true, force: true });
});

/**
 * Helper: write a markdown file in the temp dir and return its relative path.
 */
function writeTestFile(relativePath: string, content: string): string {
  const fullPath = join(tempDir, relativePath);
  const dir = fullPath.substring(0, fullPath.lastIndexOf("/"));
  if (dir) {
    mkdirSync(dir, { recursive: true });
  }
  writeFileSync(fullPath, content, "utf-8");
  return relativePath;
}

// =============================================================================
// rebuildAll
// =============================================================================

describe("Indexer.rebuildAll()", () => {
  test("indexes all .md files correctly", async () => {
    writeTestFile(
      "projects/test/task/abc12def.md",
      `---
title: Task One
type: task
status: active
tags:
  - urgent
  - backend
---

This is task one body with a [link](xyz99abc).
`
    );

    writeTestFile(
      "projects/test/summary/xyz99abc.md",
      `---
title: Summary Note
type: summary
---

Summary body text.
`
    );

    const indexer = new Indexer(tempDir, storage, parseFile);
    const result = await indexer.rebuildAll();

    // Should have indexed 2 files
    expect(result.added).toBe(2);
    expect(result.errors).toHaveLength(0);

    // Verify notes in DB
    const note1 = storage.getNoteByPath("projects/test/task/abc12def.md");
    expect(note1).not.toBeNull();
    expect(note1!.title).toBe("Task One");
    expect(note1!.type).toBe("task");
    expect(note1!.status).toBe("active");
    expect(note1!.short_id).toBe("abc12def");

    const note2 = storage.getNoteByPath("projects/test/summary/xyz99abc.md");
    expect(note2).not.toBeNull();
    expect(note2!.title).toBe("Summary Note");
    expect(note2!.type).toBe("summary");

    // Verify tags were set
    const db = storage.getDb();
    const tags = db
      .prepare(
        "SELECT t.tag FROM tags t JOIN notes n ON t.note_id = n.id WHERE n.path = ? ORDER BY t.tag"
      )
      .all("projects/test/task/abc12def.md") as { tag: string }[];
    expect(tags.map((t) => t.tag)).toEqual(["backend", "urgent"]);

    // Verify links were set
    const links = db
      .prepare(
        "SELECT l.href, l.type FROM links l JOIN notes n ON l.source_id = n.id WHERE n.path = ?"
      )
      .all("projects/test/task/abc12def.md") as {
      href: string;
      type: string;
    }[];
    expect(links).toHaveLength(1);
    expect(links[0].href).toBe("xyz99abc");
    expect(links[0].type).toBe("markdown");
  });

  test("excludes .zk/ directory files", async () => {
    writeTestFile(
      "note12345.md",
      "---\ntitle: Regular Note\n---\nBody."
    );
    writeTestFile(
      ".zk/templates/default.md",
      "---\ntitle: ZK Template\n---\nTemplate body."
    );

    const indexer = new Indexer(tempDir, storage, parseFile);
    const result = await indexer.rebuildAll();

    expect(result.added).toBe(1);

    // Regular note should exist
    expect(storage.getNoteByPath("note12345.md")).not.toBeNull();

    // .zk template should NOT exist
    expect(
      storage.getNoteByPath(".zk/templates/default.md")
    ).toBeNull();
  });

  test("returns correct stats", async () => {
    writeTestFile("aaa11111.md", "---\ntitle: A\n---\nBody A.");
    writeTestFile("bbb22222.md", "---\ntitle: B\n---\nBody B.");
    writeTestFile("ccc33333.md", "---\ntitle: C\n---\nBody C.");

    const indexer = new Indexer(tempDir, storage, parseFile);
    const result = await indexer.rebuildAll();

    expect(result.added).toBe(3);
    expect(result.updated).toBe(0);
    expect(result.deleted).toBe(0);
    expect(result.skipped).toBe(0);
    expect(result.errors).toHaveLength(0);
    expect(result.duration).toBeGreaterThan(0);
  });

  test("handles malformed files gracefully", async () => {
    // Good file
    writeTestFile("good1234.md", "---\ntitle: Good\n---\nGood body.");

    // Create a file that will cause parseFile to throw
    // (binary content that can't be parsed as markdown frontmatter is fine,
    //  but we'll use a mock parser to simulate failure)
    writeTestFile("bad12345.md", "---\ntitle: Bad\n---\nBad body.");

    // Use a parser that throws for specific files
    const failingParser = (filePath: string, brainDir: string) => {
      if (filePath.includes("bad12345")) {
        throw new Error("Simulated parse failure");
      }
      return parseFile(filePath, brainDir);
    };

    const indexer = new Indexer(tempDir, storage, failingParser);
    const result = await indexer.rebuildAll();

    // Good file should be indexed
    expect(result.added).toBe(1);
    expect(storage.getNoteByPath("good1234.md")).not.toBeNull();

    // Bad file should be in errors
    expect(result.errors).toHaveLength(1);
    expect(result.errors[0].path).toBe("bad12345.md");
    expect(result.errors[0].error).toContain("Simulated parse failure");

    // Bad file should NOT be in DB
    expect(storage.getNoteByPath("bad12345.md")).toBeNull();
  });

  test("clears old data before rebuilding (no duplicates)", async () => {
    writeTestFile("dup12345.md", "---\ntitle: Original\n---\nOriginal body.");

    const indexer = new Indexer(tempDir, storage, parseFile);

    // First rebuild
    const result1 = await indexer.rebuildAll();
    expect(result1.added).toBe(1);

    // Modify the file
    writeTestFile("dup12345.md", "---\ntitle: Updated\n---\nUpdated body.");

    // Second rebuild should clear and re-add
    const result2 = await indexer.rebuildAll();
    expect(result2.added).toBe(1);
    expect(result2.errors).toHaveLength(0);

    // Verify only one note exists (no duplicates)
    const db = storage.getDb();
    const count = db
      .prepare("SELECT COUNT(*) as count FROM notes")
      .get() as { count: number };
    expect(count.count).toBe(1);

    // Verify it has the updated title
    const note = storage.getNoteByPath("dup12345.md");
    expect(note).not.toBeNull();
    expect(note!.title).toBe("Updated");
  });

  test("maps ParsedFile fields to NoteRow correctly", async () => {
    writeTestFile(
      "projects/myproj/task/map12345.md",
      `---
title: Field Mapping Test
type: task
status: pending
priority: high
projectId: myproj
feature_id: feat-42
tags:
  - mapping
created: 2026-01-15T10:00:00.000Z
---

Body with [a link](target12) here.
`
    );

    const indexer = new Indexer(tempDir, storage, parseFile);
    await indexer.rebuildAll();

    const note = storage.getNoteByPath("projects/myproj/task/map12345.md");
    expect(note).not.toBeNull();
    expect(note!.short_id).toBe("map12345");
    expect(note!.title).toBe("Field Mapping Test");
    expect(note!.type).toBe("task");
    expect(note!.status).toBe("pending");
    expect(note!.priority).toBe("high");
    expect(note!.project_id).toBe("myproj");
    expect(note!.feature_id).toBe("feat-42");
    expect(note!.created).toBe("2026-01-15T10:00:00.000Z");
    expect(note!.modified).toBeTruthy();
    expect(note!.word_count).toBeGreaterThan(0);
    expect(note!.checksum).toHaveLength(64); // SHA-256 hex
    expect(note!.body).toContain("Body with");
    expect(note!.raw_content).toContain("---");

    // Metadata should be valid JSON
    const metadata = JSON.parse(note!.metadata);
    expect(metadata.title).toBe("Field Mapping Test");
  });

  test("handles empty brain directory", async () => {
    // tempDir exists but has no .md files
    const indexer = new Indexer(tempDir, storage, parseFile);
    const result = await indexer.rebuildAll();

    expect(result.added).toBe(0);
    expect(result.errors).toHaveLength(0);
    expect(result.duration).toBeGreaterThan(0);
  });

  test("deleted count reflects stale entries removed", async () => {
    // First, manually insert a note that has no file on disk
    storage.insertNote({
      path: "ghost123.md",
      short_id: "ghost123",
      title: "Ghost Note",
      lead: "",
      body: "",
      raw_content: "",
      word_count: 0,
      checksum: null,
      metadata: "{}",
      type: null,
      status: null,
      priority: null,
      project_id: null,
      feature_id: null,
      created: null,
      modified: null,
    });

    // Write one real file
    writeTestFile("real1234.md", "---\ntitle: Real\n---\nReal body.");

    const indexer = new Indexer(tempDir, storage, parseFile);
    const result = await indexer.rebuildAll();

    // rebuildAll deletes everything first, then re-adds
    // So the ghost note is gone (counted in deleted), real file is added
    expect(result.added).toBe(1);
    expect(result.deleted).toBe(1); // the ghost note was cleared
    expect(storage.getNoteByPath("ghost123.md")).toBeNull();
    expect(storage.getNoteByPath("real1234.md")).not.toBeNull();
  });
});

// =============================================================================
// indexChanged
// =============================================================================

describe("Indexer.indexChanged()", () => {
  test("detects new files (added)", async () => {
    // Start with empty DB, add files on disk
    writeTestFile("new11111.md", "---\ntitle: New File\n---\nNew body.");

    const indexer = new Indexer(tempDir, storage, parseFile);
    const result = await indexer.indexChanged();

    expect(result.added).toBe(1);
    expect(result.updated).toBe(0);
    expect(result.deleted).toBe(0);
    expect(result.skipped).toBe(0);
    expect(result.errors).toHaveLength(0);

    // Verify note is in DB
    const note = storage.getNoteByPath("new11111.md");
    expect(note).not.toBeNull();
    expect(note!.title).toBe("New File");
  });

  test("detects modified files (updated)", async () => {
    // First, index a file
    writeTestFile("mod11111.md", "---\ntitle: Original\n---\nOriginal body.");
    const indexer = new Indexer(tempDir, storage, parseFile);
    await indexer.indexChanged();

    // Modify the file content (changes checksum)
    writeTestFile("mod11111.md", "---\ntitle: Modified\n---\nModified body.");
    const result = await indexer.indexChanged();

    expect(result.updated).toBe(1);
    expect(result.added).toBe(0);
    expect(result.skipped).toBe(0);
    expect(result.errors).toHaveLength(0);

    // Verify updated title in DB
    const note = storage.getNoteByPath("mod11111.md");
    expect(note).not.toBeNull();
    expect(note!.title).toBe("Modified");
  });

  test("skips unchanged files (skipped)", async () => {
    // Index a file
    writeTestFile("skip1111.md", "---\ntitle: Unchanged\n---\nSame body.");
    const indexer = new Indexer(tempDir, storage, parseFile);
    await indexer.indexChanged();

    // Run again without changing anything
    const result = await indexer.indexChanged();

    expect(result.skipped).toBe(1);
    expect(result.added).toBe(0);
    expect(result.updated).toBe(0);
    expect(result.deleted).toBe(0);
    expect(result.errors).toHaveLength(0);
  });

  test("detects deleted files (deleted)", async () => {
    // Index a file
    writeTestFile("del11111.md", "---\ntitle: To Delete\n---\nDelete body.");
    const indexer = new Indexer(tempDir, storage, parseFile);
    await indexer.indexChanged();

    // Remove the file from disk
    rmSync(join(tempDir, "del11111.md"));

    const result = await indexer.indexChanged();

    expect(result.deleted).toBe(1);
    expect(result.added).toBe(0);
    expect(result.errors).toHaveLength(0);

    // Verify note is gone from DB
    expect(storage.getNoteByPath("del11111.md")).toBeNull();
  });

  test("handles parse errors gracefully", async () => {
    writeTestFile("ok111111.md", "---\ntitle: OK\n---\nOK body.");
    writeTestFile("err11111.md", "---\ntitle: Error\n---\nError body.");

    const failingParser = (filePath: string, brainDir: string) => {
      if (filePath.includes("err11111")) {
        throw new Error("Parse boom");
      }
      return parseFile(filePath, brainDir);
    };

    const indexer = new Indexer(tempDir, storage, failingParser);
    const result = await indexer.indexChanged();

    expect(result.added).toBe(1);
    expect(result.errors).toHaveLength(1);
    expect(result.errors[0].path).toBe("err11111.md");
    expect(result.errors[0].error).toContain("Parse boom");
  });
});

// =============================================================================
// indexFile
// =============================================================================

describe("Indexer.indexFile()", () => {
  test("adds a new file to the index", async () => {
    writeTestFile("single11.md", "---\ntitle: Single\ntags:\n  - alpha\n---\nSingle body with [link](target11).");

    const indexer = new Indexer(tempDir, storage, parseFile);
    await indexer.indexFile("single11.md");

    const note = storage.getNoteByPath("single11.md");
    expect(note).not.toBeNull();
    expect(note!.title).toBe("Single");

    // Verify tags
    const db = storage.getDb();
    const tags = db
      .prepare("SELECT t.tag FROM tags t JOIN notes n ON t.note_id = n.id WHERE n.path = ?")
      .all("single11.md") as { tag: string }[];
    expect(tags.map((t) => t.tag)).toEqual(["alpha"]);

    // Verify links
    const links = db
      .prepare("SELECT l.href FROM links l JOIN notes n ON l.source_id = n.id WHERE n.path = ?")
      .all("single11.md") as { href: string }[];
    expect(links).toHaveLength(1);
    expect(links[0].href).toBe("target11");
  });

  test("updates an existing file", async () => {
    writeTestFile("upd11111.md", "---\ntitle: Before\n---\nBefore body.");

    const indexer = new Indexer(tempDir, storage, parseFile);
    await indexer.indexFile("upd11111.md");

    // Modify and re-index
    writeTestFile("upd11111.md", "---\ntitle: After\ntags:\n  - beta\n---\nAfter body.");
    await indexer.indexFile("upd11111.md");

    const note = storage.getNoteByPath("upd11111.md");
    expect(note).not.toBeNull();
    expect(note!.title).toBe("After");

    // Verify tags updated
    const db = storage.getDb();
    const tags = db
      .prepare("SELECT t.tag FROM tags t JOIN notes n ON t.note_id = n.id WHERE n.path = ?")
      .all("upd11111.md") as { tag: string }[];
    expect(tags.map((t) => t.tag)).toEqual(["beta"]);
  });
});

// =============================================================================
// removeFile
// =============================================================================

describe("Indexer.removeFile()", () => {
  test("removes a file from the index", async () => {
    writeTestFile("rem11111.md", "---\ntitle: Remove Me\ntags:\n  - gamma\n---\nRemove body.");

    const indexer = new Indexer(tempDir, storage, parseFile);
    await indexer.indexFile("rem11111.md");

    // Verify it exists
    expect(storage.getNoteByPath("rem11111.md")).not.toBeNull();

    // Remove it
    await indexer.removeFile("rem11111.md");

    // Verify it's gone
    expect(storage.getNoteByPath("rem11111.md")).toBeNull();

    // Verify tags are also gone (CASCADE)
    const db = storage.getDb();
    const tags = db
      .prepare("SELECT COUNT(*) as count FROM tags WHERE note_id NOT IN (SELECT id FROM notes)")
      .get() as { count: number };
    expect(tags.count).toBe(0);
  });
});

// =============================================================================
// getHealth
// =============================================================================

describe("Indexer.getHealth()", () => {
  test("returns correct stats", async () => {
    // Write 2 files on disk
    writeTestFile("hlth1111.md", "---\ntitle: Health A\n---\nBody A.");
    writeTestFile("hlth2222.md", "---\ntitle: Health B\n---\nBody B.");

    const indexer = new Indexer(tempDir, storage, parseFile);
    await indexer.rebuildAll();

    // Manually insert a stale entry (no file on disk)
    storage.insertNote({
      path: "stale111.md",
      short_id: "stale111",
      title: "Stale",
      lead: "",
      body: "",
      raw_content: "",
      word_count: 0,
      checksum: null,
      metadata: "{}",
      type: null,
      status: null,
      priority: null,
      project_id: null,
      feature_id: null,
      created: null,
      modified: null,
    });

    const health = indexer.getHealth();

    expect(health.totalFiles).toBe(2);     // 2 .md files on disk
    expect(health.totalIndexed).toBe(3);   // 2 real + 1 stale in DB
    expect(health.staleCount).toBe(1);     // 1 DB entry with no file
  });
});

// =============================================================================
// Integration Tests: Real parser + on-disk DB + real markdown files
// =============================================================================

describe("Indexer integration", () => {
  let intDir: string;
  let intStorage: StorageLayer;

  /**
   * Helper: write a markdown file in the integration temp dir.
   */
  function writeMarkdown(relativePath: string, content: string): void {
    const fullPath = join(intDir, relativePath);
    mkdirSync(dirname(fullPath), { recursive: true });
    writeFileSync(fullPath, content, "utf-8");
  }

  beforeEach(() => {
    intDir = mkdtempSync(join(tmpdir(), "indexer-integ-"));
    intStorage = createStorageLayer(join(intDir, "test.db"));
  });

  afterEach(() => {
    intStorage.close();
    rmSync(intDir, { recursive: true, force: true });
  });

  test("full lifecycle: create, rebuild, modify, add, delete with incremental indexing", async () => {
    // --- Step 1: Create 3 markdown files with frontmatter, tags, and inter-links ---
    writeMarkdown(
      "projects/demo/task/noteaaaa.md",
      `---
title: Note A
type: task
status: active
tags:
  - alpha
  - shared
---

This is Note A. It links to [Note B](notebbb1.md) and [Note C](noteccc1.md).
`
    );

    writeMarkdown(
      "projects/demo/summary/notebbb1.md",
      `---
title: Note B
type: summary
tags:
  - beta
  - shared
---

This is Note B. It links back to [Note A](noteaaaa.md).
`
    );

    writeMarkdown(
      "noteccc1.md",
      `---
title: Note C
type: exploration
tags:
  - gamma
---

This is Note C with no outgoing links.
`
    );

    const indexer = new Indexer(intDir, intStorage, parseFile);

    // --- Step 2: rebuildAll → verify all 3 notes in DB ---
    const rebuildResult = await indexer.rebuildAll();
    expect(rebuildResult.added).toBe(3);
    expect(rebuildResult.errors).toHaveLength(0);

    // Verify Note A
    const noteA = intStorage.getNoteByPath("projects/demo/task/noteaaaa.md");
    expect(noteA).not.toBeNull();
    expect(noteA!.title).toBe("Note A");
    expect(noteA!.type).toBe("task");
    expect(noteA!.status).toBe("active");

    // Verify Note A tags
    const db = intStorage.getDb();
    const tagsA = db
      .prepare(
        "SELECT t.tag FROM tags t JOIN notes n ON t.note_id = n.id WHERE n.path = ? ORDER BY t.tag"
      )
      .all("projects/demo/task/noteaaaa.md") as { tag: string }[];
    expect(tagsA.map((t) => t.tag)).toEqual(["alpha", "shared"]);

    // Verify Note A links
    const linksA = db
      .prepare(
        "SELECT l.href, l.type FROM links l JOIN notes n ON l.source_id = n.id WHERE n.path = ? ORDER BY l.href"
      )
      .all("projects/demo/task/noteaaaa.md") as { href: string; type: string }[];
    expect(linksA).toHaveLength(2);
    expect(linksA[0].href).toBe("notebbb1.md");
    expect(linksA[1].href).toBe("noteccc1.md");

    // Verify Note B
    const noteB = intStorage.getNoteByPath("projects/demo/summary/notebbb1.md");
    expect(noteB).not.toBeNull();
    expect(noteB!.title).toBe("Note B");
    expect(noteB!.type).toBe("summary");

    // Verify Note C
    const noteC = intStorage.getNoteByPath("noteccc1.md");
    expect(noteC).not.toBeNull();
    expect(noteC!.title).toBe("Note C");
    expect(noteC!.type).toBe("exploration");

    // --- Step 3: Modify Note A (change title) → indexChanged → verify update ---
    writeMarkdown(
      "projects/demo/task/noteaaaa.md",
      `---
title: Note A Updated
type: task
status: active
tags:
  - alpha
  - shared
---

This is Note A updated. It links to [Note B](notebbb1.md) and [Note C](noteccc1.md).
`
    );

    const incResult1 = await indexer.indexChanged();
    expect(incResult1.updated).toBe(1);
    expect(incResult1.skipped).toBe(2); // B and C unchanged
    expect(incResult1.added).toBe(0);
    expect(incResult1.deleted).toBe(0);
    expect(incResult1.errors).toHaveLength(0);

    // Verify updated title
    const noteAUpdated = intStorage.getNoteByPath("projects/demo/task/noteaaaa.md");
    expect(noteAUpdated!.title).toBe("Note A Updated");

    // Verify unchanged notes still have correct titles
    expect(intStorage.getNoteByPath("projects/demo/summary/notebbb1.md")!.title).toBe("Note B");
    expect(intStorage.getNoteByPath("noteccc1.md")!.title).toBe("Note C");

    // --- Step 4: Add a new file → indexChanged → verify 4 notes total ---
    writeMarkdown(
      "noteddd1.md",
      `---
title: Note D
type: idea
tags:
  - delta
---

This is a brand new Note D.
`
    );

    const incResult2 = await indexer.indexChanged();
    expect(incResult2.added).toBe(1);
    expect(incResult2.skipped).toBe(3); // A, B, C unchanged
    expect(incResult2.updated).toBe(0);
    expect(incResult2.deleted).toBe(0);
    expect(incResult2.errors).toHaveLength(0);

    // Verify 4 notes total
    const totalCount = (
      db.prepare("SELECT COUNT(*) as count FROM notes").get() as { count: number }
    ).count;
    expect(totalCount).toBe(4);

    const noteD = intStorage.getNoteByPath("noteddd1.md");
    expect(noteD).not.toBeNull();
    expect(noteD!.title).toBe("Note D");
    expect(noteD!.type).toBe("idea");

    // --- Step 5: Delete Note C from disk → indexChanged → verify 3 notes ---
    unlinkSync(join(intDir, "noteccc1.md"));

    const incResult3 = await indexer.indexChanged();
    expect(incResult3.deleted).toBe(1);
    expect(incResult3.skipped).toBe(3); // A, B, D unchanged
    expect(incResult3.added).toBe(0);
    expect(incResult3.updated).toBe(0);
    expect(incResult3.errors).toHaveLength(0);

    // Verify Note C is gone
    expect(intStorage.getNoteByPath("noteccc1.md")).toBeNull();

    // Verify remaining 3 notes
    const finalCount = (
      db.prepare("SELECT COUNT(*) as count FROM notes").get() as { count: number }
    ).count;
    expect(finalCount).toBe(3);
    expect(intStorage.getNoteByPath("projects/demo/task/noteaaaa.md")).not.toBeNull();
    expect(intStorage.getNoteByPath("projects/demo/summary/notebbb1.md")).not.toBeNull();
    expect(intStorage.getNoteByPath("noteddd1.md")).not.toBeNull();
  });

  test("link resolution: outlinks from A include B via target_path", async () => {
    writeMarkdown(
      "lnkaaaaa.md",
      `---
title: Link Source
type: task
---

This note links to [Link Target](lnkbbbbb.md).
`
    );

    writeMarkdown(
      "lnkbbbbb.md",
      `---
title: Link Target
type: summary
---

This is the target note.
`
    );

    const indexer = new Indexer(intDir, intStorage, parseFile);
    await indexer.rebuildAll();

    // Verify link has correct target_path in DB
    const db = intStorage.getDb();
    const links = db
      .prepare(
        "SELECT l.target_path, l.href, l.type FROM links l JOIN notes n ON l.source_id = n.id WHERE n.path = ?"
      )
      .all("lnkaaaaa.md") as { target_path: string; href: string; type: string }[];

    expect(links).toHaveLength(1);
    expect(links[0].href).toBe("lnkbbbbb.md");
    expect(links[0].target_path).toBe("lnkbbbbb.md");
    expect(links[0].type).toBe("markdown");

    // Verify outlinks from A include B (via storage graph method)
    const outlinks = intStorage.getOutlinks("lnkaaaaa.md");
    // Note: getOutlinks only returns resolved links (where target_id is set).
    // Since target_path "lnkbbbbb.md" matches the actual note path, it should resolve.
    expect(outlinks).toHaveLength(1);
    expect(outlinks[0].path).toBe("lnkbbbbb.md");
    expect(outlinks[0].title).toBe("Link Target");
  });

  test("error handling: valid file indexed, malformed file in errors, no crash", async () => {
    // Good file with valid frontmatter
    writeMarkdown(
      "goodfile.md",
      `---
title: Good File
type: task
tags:
  - valid
---

This file has valid frontmatter.
`
    );

    // Malformed file: use a parser that throws for this specific file
    // (The real parseFile handles most content gracefully, so we simulate
    //  a parse error with a custom parser that delegates to real parseFile
    //  but throws for the bad file)
    writeMarkdown("badfile1.md", "---\ntitle: Bad\n---\nBad body.");

    const errorParser = (filePath: string, brainDir: string) => {
      if (filePath.includes("badfile1")) {
        throw new Error("Integration parse failure");
      }
      return parseFile(filePath, brainDir);
    };

    const indexer = new Indexer(intDir, intStorage, errorParser);
    const result = await indexer.rebuildAll();

    // Good file should be indexed
    expect(result.added).toBe(1);
    const goodNote = intStorage.getNoteByPath("goodfile.md");
    expect(goodNote).not.toBeNull();
    expect(goodNote!.title).toBe("Good File");

    // Bad file should be in errors
    expect(result.errors).toHaveLength(1);
    expect(result.errors[0].path).toBe("badfile1.md");
    expect(result.errors[0].error).toContain("Integration parse failure");

    // Bad file should NOT be in DB
    expect(intStorage.getNoteByPath("badfile1.md")).toBeNull();
  });

  test("empty directory: rebuildAll and indexChanged return 0 notes, no errors", async () => {
    // intDir exists but has no .md files (only test.db)
    const indexer = new Indexer(intDir, intStorage, parseFile);

    const rebuildResult = await indexer.rebuildAll();
    expect(rebuildResult.added).toBe(0);
    expect(rebuildResult.updated).toBe(0);
    expect(rebuildResult.deleted).toBe(0);
    expect(rebuildResult.skipped).toBe(0);
    expect(rebuildResult.errors).toHaveLength(0);

    const changedResult = await indexer.indexChanged();
    expect(changedResult.added).toBe(0);
    expect(changedResult.updated).toBe(0);
    expect(changedResult.deleted).toBe(0);
    expect(changedResult.skipped).toBe(0);
    expect(changedResult.errors).toHaveLength(0);
  });

  test("getHealth accuracy: reflects disk vs DB state including stale entries", async () => {
    // Create 2 files
    writeMarkdown(
      "hlthaaaa.md",
      `---
title: Health Note A
type: task
---

Health body A.
`
    );

    writeMarkdown(
      "hlthbbbb.md",
      `---
title: Health Note B
type: summary
---

Health body B.
`
    );

    const indexer = new Indexer(intDir, intStorage, parseFile);
    await indexer.rebuildAll();

    // Verify health after rebuild
    const health1 = indexer.getHealth();
    expect(health1.totalFiles).toBe(2);
    expect(health1.totalIndexed).toBe(2);
    expect(health1.staleCount).toBe(0);

    // Delete 1 file from disk (but NOT from DB) to create a stale entry
    unlinkSync(join(intDir, "hlthbbbb.md"));

    const health2 = indexer.getHealth();
    expect(health2.totalFiles).toBe(1);     // only 1 file on disk now
    expect(health2.totalIndexed).toBe(2);   // still 2 in DB
    expect(health2.staleCount).toBe(1);     // 1 stale (DB entry with no file)
  });
});

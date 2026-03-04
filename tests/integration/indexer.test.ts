/**
 * Indexer Integration Tests
 *
 * Tests for the Indexer class with real markdown files on disk and the
 * SQLite storage backend. Covers full rebuild, incremental updates,
 * file modification, deletion, link/tag extraction, checksum optimization,
 * stale removal, and error handling.
 */

import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import { unlinkSync, writeFileSync } from "node:fs";
import { join } from "node:path";

import {
  createTestStorage,
  createTempBrainDir,
  cleanupTempDir,
  writeTestMarkdownFile,
  type StorageLayer,
} from "./helpers";

import {
  seedBrainDir,
  FIXTURE_NOTES,
} from "./fixtures";

import { Indexer, type IndexResult } from "../../src/core/indexer";
import { parseFile } from "../../src/core/file-parser";

// =============================================================================
// Test Setup
// =============================================================================

let storage: StorageLayer;
let brainDir: string;

beforeEach(() => {
  storage = createTestStorage();
  brainDir = createTempBrainDir();
});

afterEach(() => {
  storage?.close();
  cleanupTempDir(brainDir);
});

// =============================================================================
// Full Rebuild
// =============================================================================

describe("Full rebuild", () => {
  test("rebuildAll indexes all fixture files correctly", async () => {
    // Write all 24 fixture notes to disk
    seedBrainDir(brainDir);

    const indexer = new Indexer(brainDir, storage, parseFile);
    const result = await indexer.rebuildAll();

    expect(result.added).toBe(FIXTURE_NOTES.length);
    expect(result.errors).toHaveLength(0);
    expect(result.duration).toBeGreaterThan(0);
  });

  test("rebuildAll populates correct fields for each note", async () => {
    seedBrainDir(brainDir);

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.rebuildAll();

    // Verify a specific note has correct fields
    const note = storage.getNoteByPath("projects/alpha/task/alph0001.md");
    expect(note).not.toBeNull();
    expect(note!.title).toBe("Implement authentication module");
    expect(note!.type).toBe("task");
    expect(note!.status).toBe("active");
    expect(note!.priority).toBe("high");
    expect(note!.project_id).toBe("alpha");
    expect(note!.feature_id).toBe("auth");
    expect(note!.short_id).toBe("alph0001");
    expect(note!.word_count).toBeGreaterThan(0);
    expect(note!.checksum).toBeTruthy();
    expect(note!.body).toContain("JWT authentication");
  });

  test("rebuildAll indexes notes from all projects", async () => {
    seedBrainDir(brainDir);

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.rebuildAll();

    // Verify notes from each project
    expect(storage.getNoteByPath("projects/alpha/task/alph0001.md")).not.toBeNull();
    expect(storage.getNoteByPath("projects/beta/task/beta0001.md")).not.toBeNull();
    expect(storage.getNoteByPath("projects/gamma/task/gamm0001.md")).not.toBeNull();
    expect(storage.getNoteByPath("global/learning/glob0001.md")).not.toBeNull();
  });

  test("rebuildAll with 10+ files indexes all correctly", async () => {
    // Write 12 files to temp dir
    for (let i = 1; i <= 12; i++) {
      const id = `bulk${String(i).padStart(4, "0")}`;
      writeTestMarkdownFile(brainDir, `projects/test/task/${id}.md`, {
        title: `Bulk Note ${i}`,
        type: "task",
        status: "active",
        priority: "medium",
        projectId: "test",
      }, `Body content for bulk note ${i}.`);
    }

    const indexer = new Indexer(brainDir, storage, parseFile);
    const result = await indexer.rebuildAll();

    expect(result.added).toBe(12);
    expect(result.errors).toHaveLength(0);

    // Verify all 12 are in DB
    const db = storage.getDb();
    const count = db.prepare("SELECT COUNT(*) as count FROM notes").get() as { count: number };
    expect(count.count).toBe(12);
  });
});

// =============================================================================
// Incremental Update
// =============================================================================

describe("Incremental update", () => {
  test("indexChanged detects new files added after initial index", async () => {
    // Start with 2 files
    writeTestMarkdownFile(brainDir, "projects/test/task/init0001.md", {
      title: "Initial Note 1",
      type: "task",
    }, "Initial body 1.");

    writeTestMarkdownFile(brainDir, "projects/test/task/init0002.md", {
      title: "Initial Note 2",
      type: "task",
    }, "Initial body 2.");

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.rebuildAll();

    // Add a new file
    writeTestMarkdownFile(brainDir, "projects/test/task/newf0001.md", {
      title: "New File After Rebuild",
      type: "task",
    }, "New file body.");

    const result = await indexer.indexChanged();

    expect(result.added).toBe(1);
    expect(result.skipped).toBe(2); // original 2 unchanged
    expect(result.updated).toBe(0);
    expect(result.deleted).toBe(0);
    expect(result.errors).toHaveLength(0);

    // Verify new file is in DB
    const newNote = storage.getNoteByPath("projects/test/task/newf0001.md");
    expect(newNote).not.toBeNull();
    expect(newNote!.title).toBe("New File After Rebuild");

    // Verify original files still intact
    expect(storage.getNoteByPath("projects/test/task/init0001.md")).not.toBeNull();
    expect(storage.getNoteByPath("projects/test/task/init0002.md")).not.toBeNull();
  });
});

// =============================================================================
// File Modification
// =============================================================================

describe("File modification", () => {
  test("indexChanged detects modified file content", async () => {
    writeTestMarkdownFile(brainDir, "projects/test/task/modf0001.md", {
      title: "Original Title",
      type: "task",
      status: "active",
    }, "Original body content.");

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.rebuildAll();

    // Verify original
    const original = storage.getNoteByPath("projects/test/task/modf0001.md");
    expect(original!.title).toBe("Original Title");
    expect(original!.status).toBe("active");

    // Modify the file (change title and status)
    writeTestMarkdownFile(brainDir, "projects/test/task/modf0001.md", {
      title: "Updated Title",
      type: "task",
      status: "completed",
    }, "Updated body content.");

    const result = await indexer.indexChanged();

    expect(result.updated).toBe(1);
    expect(result.added).toBe(0);
    expect(result.skipped).toBe(0);
    expect(result.deleted).toBe(0);
    expect(result.errors).toHaveLength(0);

    // Verify updated fields
    const updated = storage.getNoteByPath("projects/test/task/modf0001.md");
    expect(updated!.title).toBe("Updated Title");
    expect(updated!.status).toBe("completed");
    expect(updated!.body).toContain("Updated body content");
  });

  test("indexFile updates a single file in-place", async () => {
    writeTestMarkdownFile(brainDir, "projects/test/task/sinf0001.md", {
      title: "Before Update",
      type: "task",
    }, "Before body.");

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.indexFile("projects/test/task/sinf0001.md");

    expect(storage.getNoteByPath("projects/test/task/sinf0001.md")!.title).toBe("Before Update");

    // Modify and re-index single file
    writeTestMarkdownFile(brainDir, "projects/test/task/sinf0001.md", {
      title: "After Update",
      type: "task",
    }, "After body.");

    await indexer.indexFile("projects/test/task/sinf0001.md");

    const updated = storage.getNoteByPath("projects/test/task/sinf0001.md");
    expect(updated!.title).toBe("After Update");
    expect(updated!.body).toContain("After body");
  });
});

// =============================================================================
// File Deletion
// =============================================================================

describe("File deletion", () => {
  test("removeFile removes note from DB", async () => {
    writeTestMarkdownFile(brainDir, "projects/test/task/delf0001.md", {
      title: "To Be Deleted",
      type: "task",
    }, "Delete me.");

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.indexFile("projects/test/task/delf0001.md");

    // Verify it exists
    expect(storage.getNoteByPath("projects/test/task/delf0001.md")).not.toBeNull();

    // Remove it
    await indexer.removeFile("projects/test/task/delf0001.md");

    // Verify it's gone
    expect(storage.getNoteByPath("projects/test/task/delf0001.md")).toBeNull();
  });

  test("indexChanged detects deleted files from disk", async () => {
    writeTestMarkdownFile(brainDir, "projects/test/task/del10001.md", {
      title: "Keep Me",
      type: "task",
    }, "Keep body.");

    writeTestMarkdownFile(brainDir, "projects/test/task/del20001.md", {
      title: "Delete Me",
      type: "task",
    }, "Delete body.");

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.rebuildAll();

    // Delete one file from disk
    unlinkSync(join(brainDir, "projects/test/task/del20001.md"));

    const result = await indexer.indexChanged();

    expect(result.deleted).toBe(1);
    expect(result.skipped).toBe(1); // the kept file
    expect(result.errors).toHaveLength(0);

    // Verify deleted note is gone
    expect(storage.getNoteByPath("projects/test/task/del20001.md")).toBeNull();

    // Verify kept note still exists
    expect(storage.getNoteByPath("projects/test/task/del10001.md")).not.toBeNull();
  });

  test("removeFile cascades to clean up tags and links", async () => {
    writeTestMarkdownFile(brainDir, "projects/test/task/casc0001.md", {
      title: "Cascade Test",
      type: "task",
      tags: ["tag-a", "tag-b"],
    }, "Body with [a link](some/target.md).");

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.indexFile("projects/test/task/casc0001.md");

    // Verify tags and links exist
    const db = storage.getDb();
    const tagCount = db.prepare(
      "SELECT COUNT(*) as c FROM tags WHERE note_id = (SELECT id FROM notes WHERE path = ?)"
    ).get("projects/test/task/casc0001.md") as { c: number };
    expect(tagCount.c).toBeGreaterThan(0);

    const linkCount = db.prepare(
      "SELECT COUNT(*) as c FROM links WHERE source_id = (SELECT id FROM notes WHERE path = ?)"
    ).get("projects/test/task/casc0001.md") as { c: number };
    expect(linkCount.c).toBeGreaterThan(0);

    // Remove the file
    await indexer.removeFile("projects/test/task/casc0001.md");

    // Verify tags and links are cleaned up (CASCADE)
    const orphanTags = db.prepare(
      "SELECT COUNT(*) as c FROM tags WHERE note_id NOT IN (SELECT id FROM notes)"
    ).get() as { c: number };
    expect(orphanTags.c).toBe(0);

    const orphanLinks = db.prepare(
      "SELECT COUNT(*) as c FROM links WHERE source_id NOT IN (SELECT id FROM notes)"
    ).get() as { c: number };
    expect(orphanLinks.c).toBe(0);
  });
});

// =============================================================================
// Link Extraction
// =============================================================================

describe("Link extraction", () => {
  test("markdown links in body are extracted and stored in links table", async () => {
    writeTestMarkdownFile(brainDir, "projects/test/task/link0001.md", {
      title: "Link Source",
      type: "task",
    }, "See [Target A](projects/test/task/tgta0001.md) and [Target B](projects/test/task/tgtb0001.md).");

    // Create target notes so links can resolve
    writeTestMarkdownFile(brainDir, "projects/test/task/tgta0001.md", {
      title: "Target A",
      type: "task",
    }, "Target A body.");

    writeTestMarkdownFile(brainDir, "projects/test/task/tgtb0001.md", {
      title: "Target B",
      type: "task",
    }, "Target B body.");

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.rebuildAll();

    // Verify links are in DB
    const db = storage.getDb();
    const links = db.prepare(
      "SELECT l.href, l.title, l.type FROM links l JOIN notes n ON l.source_id = n.id WHERE n.path = ? ORDER BY l.href"
    ).all("projects/test/task/link0001.md") as { href: string; title: string; type: string }[];

    expect(links.length).toBe(2);
    expect(links[0].href).toBe("projects/test/task/tgta0001.md");
    expect(links[0].title).toBe("Target A");
    expect(links[0].type).toBe("markdown");
    expect(links[1].href).toBe("projects/test/task/tgtb0001.md");
    expect(links[1].title).toBe("Target B");
  });

  test("external URLs are classified as url type links", async () => {
    writeTestMarkdownFile(brainDir, "projects/test/task/extl0001.md", {
      title: "External Links",
      type: "task",
    }, "Check [Google](https://google.com) and [local note](local0001.md).");

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.rebuildAll();

    const db = storage.getDb();
    const links = db.prepare(
      "SELECT l.href, l.type FROM links l JOIN notes n ON l.source_id = n.id WHERE n.path = ? ORDER BY l.href"
    ).all("projects/test/task/extl0001.md") as { href: string; type: string }[];

    expect(links.length).toBe(2);

    const urlLink = links.find((l) => l.href.startsWith("https://"));
    expect(urlLink).toBeTruthy();
    expect(urlLink!.type).toBe("url");

    const mdLink = links.find((l) => l.href === "local0001.md");
    expect(mdLink).toBeTruthy();
    expect(mdLink!.type).toBe("markdown");
  });
});

// =============================================================================
// Tag Extraction
// =============================================================================

describe("Tag extraction", () => {
  test("tags in frontmatter are extracted and stored in tags table", async () => {
    writeTestMarkdownFile(brainDir, "projects/test/task/tags0001.md", {
      title: "Tagged Note",
      type: "task",
      tags: ["important", "backend", "auth"],
    }, "A note with tags.");

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.rebuildAll();

    const db = storage.getDb();
    const tags = db.prepare(
      "SELECT t.tag FROM tags t JOIN notes n ON t.note_id = n.id WHERE n.path = ? ORDER BY t.tag"
    ).all("projects/test/task/tags0001.md") as { tag: string }[];

    expect(tags.length).toBe(3);
    expect(tags.map((t) => t.tag)).toEqual(["auth", "backend", "important"]);
  });

  test("notes without tags have no entries in tags table", async () => {
    writeTestMarkdownFile(brainDir, "projects/test/task/notg0001.md", {
      title: "No Tags Note",
      type: "task",
    }, "A note without tags.");

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.rebuildAll();

    const db = storage.getDb();
    const tags = db.prepare(
      "SELECT COUNT(*) as c FROM tags WHERE note_id = (SELECT id FROM notes WHERE path = ?)"
    ).get("projects/test/task/notg0001.md") as { c: number };

    expect(tags.c).toBe(0);
  });

  test("tags are replaced on re-index (not duplicated)", async () => {
    writeTestMarkdownFile(brainDir, "projects/test/task/rtag0001.md", {
      title: "Tag Replace Test",
      type: "task",
      tags: ["old-tag-1", "old-tag-2"],
    }, "Original body.");

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.rebuildAll();

    // Modify tags
    writeTestMarkdownFile(brainDir, "projects/test/task/rtag0001.md", {
      title: "Tag Replace Test",
      type: "task",
      tags: ["new-tag-1"],
    }, "Updated body.");

    await indexer.indexChanged();

    const db = storage.getDb();
    const tags = db.prepare(
      "SELECT t.tag FROM tags t JOIN notes n ON t.note_id = n.id WHERE n.path = ?"
    ).all("projects/test/task/rtag0001.md") as { tag: string }[];

    expect(tags.length).toBe(1);
    expect(tags[0].tag).toBe("new-tag-1");
  });
});

// =============================================================================
// Checksum Optimization
// =============================================================================

describe("Checksum optimization", () => {
  test("indexChanged skips unchanged files (same checksum)", async () => {
    writeTestMarkdownFile(brainDir, "projects/test/task/cksm0001.md", {
      title: "Checksum Test",
      type: "task",
    }, "Unchanged body.");

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.rebuildAll();

    // Run indexChanged again without modifying anything
    const result = await indexer.indexChanged();

    expect(result.skipped).toBe(1);
    expect(result.added).toBe(0);
    expect(result.updated).toBe(0);
    expect(result.deleted).toBe(0);
    expect(result.errors).toHaveLength(0);
  });

  test("indexChanged detects change when content differs (different checksum)", async () => {
    writeTestMarkdownFile(brainDir, "projects/test/task/cksm0002.md", {
      title: "Checksum Change",
      type: "task",
    }, "Original content.");

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.rebuildAll();

    // Modify the file content
    writeTestMarkdownFile(brainDir, "projects/test/task/cksm0002.md", {
      title: "Checksum Change",
      type: "task",
    }, "Modified content — different checksum.");

    const result = await indexer.indexChanged();

    expect(result.updated).toBe(1);
    expect(result.skipped).toBe(0);
  });

  test("multiple unchanged files are all skipped", async () => {
    for (let i = 1; i <= 5; i++) {
      const id = `skip${String(i).padStart(4, "0")}`;
      writeTestMarkdownFile(brainDir, `projects/test/task/${id}.md`, {
        title: `Skip Note ${i}`,
        type: "task",
      }, `Body for skip note ${i}.`);
    }

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.rebuildAll();

    // Run again — all should be skipped
    const result = await indexer.indexChanged();

    expect(result.skipped).toBe(5);
    expect(result.added).toBe(0);
    expect(result.updated).toBe(0);
    expect(result.deleted).toBe(0);
  });
});

// =============================================================================
// Stale Removal
// =============================================================================

describe("Stale removal", () => {
  test("rebuildAll after deleting files removes stale entries", async () => {
    // Create 3 files
    writeTestMarkdownFile(brainDir, "projects/test/task/stal0001.md", {
      title: "Stale Test 1",
      type: "task",
    }, "Body 1.");

    writeTestMarkdownFile(brainDir, "projects/test/task/stal0002.md", {
      title: "Stale Test 2",
      type: "task",
    }, "Body 2.");

    writeTestMarkdownFile(brainDir, "projects/test/task/stal0003.md", {
      title: "Stale Test 3",
      type: "task",
    }, "Body 3.");

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.rebuildAll();

    // Verify 3 notes
    const db = storage.getDb();
    let count = (db.prepare("SELECT COUNT(*) as c FROM notes").get() as { c: number }).c;
    expect(count).toBe(3);

    // Delete 2 files from disk
    unlinkSync(join(brainDir, "projects/test/task/stal0002.md"));
    unlinkSync(join(brainDir, "projects/test/task/stal0003.md"));

    // Rebuild — should only have 1 note
    const result = await indexer.rebuildAll();

    expect(result.added).toBe(1);
    expect(result.errors).toHaveLength(0);

    count = (db.prepare("SELECT COUNT(*) as c FROM notes").get() as { c: number }).c;
    expect(count).toBe(1);

    // Verify only the remaining file is in DB
    expect(storage.getNoteByPath("projects/test/task/stal0001.md")).not.toBeNull();
    expect(storage.getNoteByPath("projects/test/task/stal0002.md")).toBeNull();
    expect(storage.getNoteByPath("projects/test/task/stal0003.md")).toBeNull();
  });

  test("indexChanged removes DB entries for deleted files", async () => {
    writeTestMarkdownFile(brainDir, "projects/test/task/stlc0001.md", {
      title: "Stale Changed 1",
      type: "task",
    }, "Body 1.");

    writeTestMarkdownFile(brainDir, "projects/test/task/stlc0002.md", {
      title: "Stale Changed 2",
      type: "task",
    }, "Body 2.");

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.rebuildAll();

    // Delete one file
    unlinkSync(join(brainDir, "projects/test/task/stlc0002.md"));

    const result = await indexer.indexChanged();

    expect(result.deleted).toBe(1);
    expect(result.skipped).toBe(1);

    expect(storage.getNoteByPath("projects/test/task/stlc0001.md")).not.toBeNull();
    expect(storage.getNoteByPath("projects/test/task/stlc0002.md")).toBeNull();
  });

  test("getHealth reports stale entries accurately", async () => {
    writeTestMarkdownFile(brainDir, "projects/test/task/hlth0001.md", {
      title: "Health 1",
      type: "task",
    }, "Body 1.");

    writeTestMarkdownFile(brainDir, "projects/test/task/hlth0002.md", {
      title: "Health 2",
      type: "task",
    }, "Body 2.");

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.rebuildAll();

    // Health should show 0 stale
    let health = indexer.getHealth();
    expect(health.totalFiles).toBe(2);
    expect(health.totalIndexed).toBe(2);
    expect(health.staleCount).toBe(0);

    // Delete one file from disk (but not from DB)
    unlinkSync(join(brainDir, "projects/test/task/hlth0002.md"));

    // Health should show 1 stale
    health = indexer.getHealth();
    expect(health.totalFiles).toBe(1);
    expect(health.totalIndexed).toBe(2);
    expect(health.staleCount).toBe(1);
  });
});

// =============================================================================
// Error Handling
// =============================================================================

describe("Error handling", () => {
  test("invalid frontmatter file is recorded as error but doesn't crash", async () => {
    // Good file
    writeTestMarkdownFile(brainDir, "projects/test/task/good0001.md", {
      title: "Good Note",
      type: "task",
    }, "Good body.");

    // Bad file — use a custom parser that throws for this file
    writeTestMarkdownFile(brainDir, "projects/test/task/badf0001.md", {
      title: "Bad Note",
      type: "task",
    }, "Bad body.");

    const failingParser = (filePath: string, dir: string) => {
      if (filePath.includes("badf0001")) {
        throw new Error("Simulated frontmatter parse error");
      }
      return parseFile(filePath, dir);
    };

    const indexer = new Indexer(brainDir, storage, failingParser);
    const result = await indexer.rebuildAll();

    // Good file should be indexed
    expect(result.added).toBe(1);
    expect(storage.getNoteByPath("projects/test/task/good0001.md")).not.toBeNull();

    // Bad file should be in errors
    expect(result.errors).toHaveLength(1);
    expect(result.errors[0].path).toBe("projects/test/task/badf0001.md");
    expect(result.errors[0].error).toContain("Simulated frontmatter parse error");

    // Bad file should NOT be in DB
    expect(storage.getNoteByPath("projects/test/task/badf0001.md")).toBeNull();
  });

  test("error in one file during indexChanged doesn't affect others", async () => {
    writeTestMarkdownFile(brainDir, "projects/test/task/errc0001.md", {
      title: "Error Changed 1",
      type: "task",
    }, "Body 1.");

    writeTestMarkdownFile(brainDir, "projects/test/task/errc0002.md", {
      title: "Error Changed 2",
      type: "task",
    }, "Body 2.");

    const failingParser = (filePath: string, dir: string) => {
      if (filePath.includes("errc0002")) {
        throw new Error("Parse failure during indexChanged");
      }
      return parseFile(filePath, dir);
    };

    const indexer = new Indexer(brainDir, storage, failingParser);
    const result = await indexer.indexChanged();

    expect(result.added).toBe(1);
    expect(result.errors).toHaveLength(1);
    expect(result.errors[0].path).toBe("projects/test/task/errc0002.md");

    // Good file should be in DB
    expect(storage.getNoteByPath("projects/test/task/errc0001.md")).not.toBeNull();
  });

  test("empty brain directory indexes zero files without error", async () => {
    // brainDir exists but has no .md files
    const indexer = new Indexer(brainDir, storage, parseFile);

    const rebuildResult = await indexer.rebuildAll();
    expect(rebuildResult.added).toBe(0);
    expect(rebuildResult.errors).toHaveLength(0);

    const changedResult = await indexer.indexChanged();
    expect(changedResult.added).toBe(0);
    expect(changedResult.errors).toHaveLength(0);
  });
});

// =============================================================================
// Full Lifecycle Integration
// =============================================================================

describe("Full lifecycle", () => {
  test("rebuild → add → modify → delete → verify graph integrity", async () => {
    // Step 1: Create initial files with links
    writeTestMarkdownFile(brainDir, "projects/test/task/life0001.md", {
      title: "Lifecycle Note A",
      type: "task",
      status: "active",
      tags: ["lifecycle"],
    }, "Note A links to [Note B](projects/test/task/life0002.md).");

    writeTestMarkdownFile(brainDir, "projects/test/task/life0002.md", {
      title: "Lifecycle Note B",
      type: "task",
      status: "active",
    }, "Note B is a target.");

    const indexer = new Indexer(brainDir, storage, parseFile);
    await indexer.rebuildAll();

    // Verify initial state
    expect(storage.getNoteByPath("projects/test/task/life0001.md")).not.toBeNull();
    expect(storage.getNoteByPath("projects/test/task/life0002.md")).not.toBeNull();

    // Verify link from A to B exists in links table
    // Note: rebuildAll() inserts notes sequentially, so target_id resolution
    // depends on insertion order. We verify via raw SQL and getBacklinks
    // (which matches on target_path), not getOutlinks (which requires target_id).
    const db = storage.getDb();
    const linkRows = db.prepare(
      "SELECT l.target_path FROM links l JOIN notes n ON l.source_id = n.id WHERE n.path = ?"
    ).all("projects/test/task/life0001.md") as { target_path: string }[];
    expect(linkRows.length).toBe(1);
    expect(linkRows[0].target_path).toBe("projects/test/task/life0002.md");

    // getBacklinks works regardless of target_id resolution (matches on target_path)
    const backlinks = storage.getBacklinks("projects/test/task/life0002.md");
    expect(backlinks.length).toBe(1);
    expect(backlinks[0].path).toBe("projects/test/task/life0001.md");

    // Step 2: Add a new file that also links to B
    writeTestMarkdownFile(brainDir, "projects/test/task/life0003.md", {
      title: "Lifecycle Note C",
      type: "task",
      status: "active",
    }, "Note C also links to [Note B](projects/test/task/life0002.md).");

    await indexer.indexChanged();

    // B should now have 2 backlinks
    const backlinksAfterAdd = storage.getBacklinks("projects/test/task/life0002.md");
    expect(backlinksAfterAdd.length).toBe(2);

    // Verify both A and C link to B via raw links table
    // (getRelated requires resolved target_id on both links, which depends on insertion order)
    const linksToB = db.prepare(
      `SELECT n.path FROM notes n
       JOIN links l ON l.source_id = n.id
       WHERE l.target_path = ?
       ORDER BY n.path`
    ).all("projects/test/task/life0002.md") as { path: string }[];
    expect(linksToB.length).toBe(2);
    expect(linksToB.map(r => r.path)).toContain("projects/test/task/life0001.md");
    expect(linksToB.map(r => r.path)).toContain("projects/test/task/life0003.md");

    // Step 3: Modify A to remove its link to B
    writeTestMarkdownFile(brainDir, "projects/test/task/life0001.md", {
      title: "Lifecycle Note A (Updated)",
      type: "task",
      status: "completed",
      tags: ["lifecycle", "updated"],
    }, "Note A no longer links to B.");

    await indexer.indexChanged();

    // A should have no outlinks now
    const outlinksAfterMod = storage.getOutlinks("projects/test/task/life0001.md");
    expect(outlinksAfterMod.length).toBe(0);

    // B should have only 1 backlink (from C)
    const backlinksAfterMod = storage.getBacklinks("projects/test/task/life0002.md");
    expect(backlinksAfterMod.length).toBe(1);
    expect(backlinksAfterMod[0].path).toBe("projects/test/task/life0003.md");

    // Step 4: Delete C from disk
    unlinkSync(join(brainDir, "projects/test/task/life0003.md"));
    await indexer.indexChanged();

    // C should be gone
    expect(storage.getNoteByPath("projects/test/task/life0003.md")).toBeNull();

    // B should now be an orphan (no incoming links)
    const backlinksAfterDel = storage.getBacklinks("projects/test/task/life0002.md");
    expect(backlinksAfterDel.length).toBe(0);

    const orphans = storage.getOrphans();
    const orphanPaths = orphans.map((n) => n.path);
    expect(orphanPaths).toContain("projects/test/task/life0002.md");
  });
});

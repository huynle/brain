/**
 * Storage CRUD Integration Tests
 *
 * Full lifecycle tests for the StorageLayer CRUD operations using seeded
 * fixture data. Covers create, recall, update, delete, tags, list/filter,
 * sorting, pagination, entry metadata, stats, and edge cases.
 */

import { describe, test, expect, beforeEach, afterEach } from "bun:test";

import {
  createTestStorage,
  makeTestNote,
  type StorageLayer,
  type NoteRow,
} from "./helpers";

import {
  FIXTURE_NOTES,
  FIXTURE_LINKS,
  seedStorage,
} from "./fixtures";

// =============================================================================
// Test Setup
// =============================================================================

let storage: StorageLayer;

beforeEach(() => {
  storage = createTestStorage();
  seedStorage(storage);
});

afterEach(() => {
  storage?.close();
});

// =============================================================================
// Create Lifecycle
// =============================================================================

describe("Create lifecycle", () => {
  test("insertNote returns row with id and indexed_at populated", () => {
    const fresh = createTestStorage();
    const note = makeTestNote({ path: "new/note.md", short_id: "newnote1" });
    const inserted = fresh.insertNote(note);

    expect(inserted.id).toBeGreaterThan(0);
    expect(inserted.indexed_at).toBeTruthy();
    expect(inserted.path).toBe("new/note.md");
    expect(inserted.short_id).toBe("newnote1");
    fresh.close();
  });

  test("insertNote preserves all fields", () => {
    const fresh = createTestStorage();
    const note = makeTestNote({
      path: "projects/x/task/pres0001.md",
      short_id: "pres0001",
      title: "Preserve All Fields",
      lead: "Lead text here",
      body: "Body content here",
      raw_content: "---\ntitle: Preserve All Fields\n---\nBody content here",
      word_count: 3,
      checksum: "checksum_pres",
      metadata: JSON.stringify({ title: "Preserve All Fields", extra: true }),
      type: "task",
      status: "active",
      priority: "high",
      project_id: "x",
      feature_id: "feat-y",
      created: "2024-06-01T00:00:00Z",
      modified: "2024-06-15T00:00:00Z",
    });

    const inserted = fresh.insertNote(note);

    expect(inserted.title).toBe("Preserve All Fields");
    expect(inserted.lead).toBe("Lead text here");
    expect(inserted.body).toBe("Body content here");
    expect(inserted.raw_content).toContain("Preserve All Fields");
    expect(inserted.word_count).toBe(3);
    expect(inserted.checksum).toBe("checksum_pres");
    expect(inserted.type).toBe("task");
    expect(inserted.status).toBe("active");
    expect(inserted.priority).toBe("high");
    expect(inserted.project_id).toBe("x");
    expect(inserted.feature_id).toBe("feat-y");
    expect(inserted.created).toBe("2024-06-01T00:00:00Z");
    expect(inserted.modified).toBe("2024-06-15T00:00:00Z");

    const meta = JSON.parse(inserted.metadata);
    expect(meta.title).toBe("Preserve All Fields");
    expect(meta.extra).toBe(true);

    fresh.close();
  });

  test("inserted note is verifiable in DB", () => {
    const fresh = createTestStorage();
    const note = makeTestNote({ path: "verify/db.md", short_id: "verifydb" });
    fresh.insertNote(note);

    const row = fresh
      .getDb()
      .prepare("SELECT * FROM notes WHERE path = ?")
      .get("verify/db.md") as NoteRow | null;

    expect(row).not.toBeNull();
    expect(row!.title).toBe("Test Note");
    fresh.close();
  });
});

// =============================================================================
// Recall by Path
// =============================================================================

describe("Recall by path", () => {
  test("getNoteByPath returns correct note for each fixture", () => {
    for (const fixture of FIXTURE_NOTES) {
      const note = storage.getNoteByPath(fixture.path);
      expect(note).not.toBeNull();
      expect(note!.path).toBe(fixture.path);
      expect(note!.title).toBe(fixture.title);
      expect(note!.type).toBe(fixture.type);
      expect(note!.status).toBe(fixture.status);
    }
  });

  test("getNoteByPath returns null for non-existent path", () => {
    const note = storage.getNoteByPath("does/not/exist.md");
    expect(note).toBeNull();
  });
});

// =============================================================================
// Recall by Short ID
// =============================================================================

describe("Recall by short_id", () => {
  test("getNoteByShortId returns correct note for each fixture", () => {
    for (const fixture of FIXTURE_NOTES) {
      const note = storage.getNoteByShortId(fixture.short_id);
      expect(note).not.toBeNull();
      expect(note!.short_id).toBe(fixture.short_id);
      expect(note!.title).toBe(fixture.title);
    }
  });

  test("getNoteByShortId returns null for non-existent id", () => {
    const note = storage.getNoteByShortId("zzzzzzzz");
    expect(note).toBeNull();
  });
});

// =============================================================================
// Recall by Title
// =============================================================================

describe("Recall by title", () => {
  test("getNoteByTitle returns correct note for fixture titles", () => {
    const testCases = [
      { title: "Implement authentication module", expectedPath: "projects/alpha/task/alph0001.md" },
      { title: "Set up CI/CD pipeline", expectedPath: "projects/beta/task/beta0001.md" },
      { title: "Design user onboarding flow", expectedPath: "projects/gamma/task/gamm0001.md" },
      { title: "ADR: Adopt Bun as runtime", expectedPath: "global/decision/glob0003.md" },
    ];

    for (const tc of testCases) {
      const note = storage.getNoteByTitle(tc.title);
      expect(note).not.toBeNull();
      expect(note!.path).toBe(tc.expectedPath);
    }
  });

  test("getNoteByTitle returns null for non-existent title", () => {
    const note = storage.getNoteByTitle("This title does not exist anywhere");
    expect(note).toBeNull();
  });

  test("getNoteByTitle handles unicode titles", () => {
    const note = storage.getNoteByTitle("研究: Internationalization approaches");
    expect(note).not.toBeNull();
    expect(note!.short_id).toBe("gamm0004");
  });
});

// =============================================================================
// Update Lifecycle
// =============================================================================

describe("Update lifecycle", () => {
  test("updateNote changes specified fields", () => {
    const updated = storage.updateNote("projects/alpha/task/alph0001.md", {
      title: "Updated Auth Module",
      status: "completed",
    });

    expect(updated).not.toBeNull();
    expect(updated!.title).toBe("Updated Auth Module");
    expect(updated!.status).toBe("completed");
  });

  test("updateNote preserves unmodified fields", () => {
    const original = storage.getNoteByPath("projects/alpha/task/alph0001.md")!;

    const updated = storage.updateNote("projects/alpha/task/alph0001.md", {
      title: "Changed Title Only",
    });

    expect(updated).not.toBeNull();
    expect(updated!.title).toBe("Changed Title Only");
    expect(updated!.body).toBe(original.body);
    expect(updated!.type).toBe(original.type);
    expect(updated!.priority).toBe(original.priority);
    expect(updated!.project_id).toBe(original.project_id);
    expect(updated!.feature_id).toBe(original.feature_id);
    expect(updated!.created).toBe(original.created);
  });

  test("updateNote updates indexed_at timestamp", () => {
    const updated = storage.updateNote("projects/alpha/task/alph0001.md", {
      title: "Trigger indexed_at update",
    });

    expect(updated).not.toBeNull();
    expect(updated!.indexed_at).toBeTruthy();
  });

  test("updateNote returns null for non-existent path", () => {
    const result = storage.updateNote("does/not/exist.md", {
      title: "Won't work",
    });
    expect(result).toBeNull();
  });

  test("updateNote with empty updates returns current note", () => {
    const original = storage.getNoteByPath("projects/alpha/task/alph0001.md")!;
    const result = storage.updateNote("projects/alpha/task/alph0001.md", {});
    expect(result).not.toBeNull();
    expect(result!.title).toBe(original.title);
  });
});

// =============================================================================
// Delete Lifecycle
// =============================================================================

describe("Delete lifecycle", () => {
  test("deleteNote removes the note", () => {
    const before = storage.getNoteByPath("projects/alpha/idea/alph0006.md");
    expect(before).not.toBeNull();

    const deleted = storage.deleteNote("projects/alpha/idea/alph0006.md");
    expect(deleted).toBe(true);

    const after = storage.getNoteByPath("projects/alpha/idea/alph0006.md");
    expect(after).toBeNull();
  });

  test("deleteNote returns false for non-existent path", () => {
    const deleted = storage.deleteNote("does/not/exist.md");
    expect(deleted).toBe(false);
  });

  test("deleteNote cascades to remove tags", () => {
    storage.setTags("projects/alpha/task/alph0001.md", ["auth", "jwt", "api"]);

    const tagsBefore = storage
      .getDb()
      .prepare(
        "SELECT COUNT(*) as c FROM tags WHERE note_id = (SELECT id FROM notes WHERE path = ?)"
      )
      .get("projects/alpha/task/alph0001.md") as { c: number };
    expect(tagsBefore.c).toBe(3);

    storage.deleteNote("projects/alpha/task/alph0001.md");

    // Tags for the deleted note should be cascade-deleted
    const tagsAfter = storage
      .getDb()
      .prepare("SELECT COUNT(*) as c FROM tags")
      .get() as { c: number };
    expect(tagsAfter.c).toBe(0);
  });

  test("deleteNote cascades to remove links where note is source", () => {
    const linksBefore = storage
      .getDb()
      .prepare(
        "SELECT COUNT(*) as c FROM links WHERE source_id = (SELECT id FROM notes WHERE path = ?)"
      )
      .get("projects/alpha/task/alph0001.md") as { c: number };
    expect(linksBefore.c).toBeGreaterThan(0);

    storage.deleteNote("projects/alpha/task/alph0001.md");

    const totalLinks = storage
      .getDb()
      .prepare("SELECT COUNT(*) as c FROM links")
      .get() as { c: number };
    expect(totalLinks.c).toBeLessThan(FIXTURE_LINKS.length);
  });
});

// =============================================================================
// Tags Lifecycle
// =============================================================================

describe("Tags lifecycle", () => {
  test("setTags adds tags that are queryable", () => {
    storage.setTags("projects/alpha/task/alph0001.md", ["auth", "jwt", "security"]);

    const tags = storage
      .getDb()
      .prepare(
        "SELECT tag FROM tags WHERE note_id = (SELECT id FROM notes WHERE path = ?) ORDER BY tag"
      )
      .all("projects/alpha/task/alph0001.md") as { tag: string }[];

    expect(tags.length).toBe(3);
    expect(tags.map((t) => t.tag)).toEqual(["auth", "jwt", "security"]);
  });

  test("setTags replaces existing tags", () => {
    storage.setTags("projects/alpha/task/alph0001.md", ["old-tag-1", "old-tag-2"]);
    storage.setTags("projects/alpha/task/alph0001.md", ["new-tag-1"]);

    const tags = storage
      .getDb()
      .prepare(
        "SELECT tag FROM tags WHERE note_id = (SELECT id FROM notes WHERE path = ?)"
      )
      .all("projects/alpha/task/alph0001.md") as { tag: string }[];

    expect(tags.length).toBe(1);
    expect(tags[0].tag).toBe("new-tag-1");
  });

  test("setTags with empty array removes all tags", () => {
    storage.setTags("projects/alpha/task/alph0001.md", ["tag1", "tag2"]);
    storage.setTags("projects/alpha/task/alph0001.md", []);

    const tags = storage
      .getDb()
      .prepare(
        "SELECT COUNT(*) as c FROM tags WHERE note_id = (SELECT id FROM notes WHERE path = ?)"
      )
      .get("projects/alpha/task/alph0001.md") as { c: number };

    expect(tags.c).toBe(0);
  });

  test("setTags throws for non-existent note", () => {
    expect(() => {
      storage.setTags("does/not/exist.md", ["tag1"]);
    }).toThrow("Note not found");
  });

  test("tags are queryable via listNotes tag filter", () => {
    storage.setTags("projects/alpha/task/alph0001.md", ["important", "auth"]);
    storage.setTags("projects/alpha/task/alph0002.md", ["important", "testing"]);
    storage.setTags("projects/beta/task/beta0001.md", ["devops"]);

    const importantNotes = storage.listNotes({ tag: "important" });
    expect(importantNotes.length).toBe(2);

    const devopsNotes = storage.listNotes({ tag: "devops" });
    expect(devopsNotes.length).toBe(1);
    expect(devopsNotes[0].path).toBe("projects/beta/task/beta0001.md");
  });
});

// =============================================================================
// List with Filters
// =============================================================================

describe("List with filters", () => {
  test("type filter returns only matching type", () => {
    const tasks = storage.listNotes({ type: "task" });
    expect(tasks.length).toBeGreaterThan(0);
    for (const note of tasks) {
      expect(note.type).toBe("task");
    }

    const expectedTaskCount = FIXTURE_NOTES.filter((n) => n.type === "task").length;
    expect(tasks.length).toBe(expectedTaskCount);
  });

  test("status filter returns only matching status", () => {
    const active = storage.listNotes({ status: "active" });
    expect(active.length).toBeGreaterThan(0);
    for (const note of active) {
      expect(note.status).toBe("active");
    }

    const expectedActiveCount = FIXTURE_NOTES.filter((n) => n.status === "active").length;
    expect(active.length).toBe(expectedActiveCount);
  });

  test("project filter returns only matching project", () => {
    const alpha = storage.listNotes({ project: "alpha" });
    expect(alpha.length).toBeGreaterThan(0);
    for (const note of alpha) {
      expect(note.project_id).toBe("alpha");
    }

    const expectedAlphaCount = FIXTURE_NOTES.filter((n) => n.project_id === "alpha").length;
    expect(alpha.length).toBe(expectedAlphaCount);
  });

  test("combined filters narrow results correctly", () => {
    const results = storage.listNotes({
      type: "task",
      status: "active",
      project: "alpha",
    });

    const expected = FIXTURE_NOTES.filter(
      (n) => n.type === "task" && n.status === "active" && n.project_id === "alpha"
    );

    expect(results.length).toBe(expected.length);
    for (const note of results) {
      expect(note.type).toBe("task");
      expect(note.status).toBe("active");
      expect(note.project_id).toBe("alpha");
    }
  });

  test("path prefix filter restricts to matching paths", () => {
    const betaNotes = storage.listNotes({ path: "projects/beta/" });
    expect(betaNotes.length).toBeGreaterThan(0);
    for (const note of betaNotes) {
      expect(note.path.startsWith("projects/beta/")).toBe(true);
    }

    const expectedBetaCount = FIXTURE_NOTES.filter((n) =>
      n.path.startsWith("projects/beta/")
    ).length;
    expect(betaNotes.length).toBe(expectedBetaCount);
  });

  test("tag filter returns notes with matching tag", () => {
    storage.setTags("projects/alpha/task/alph0001.md", ["priority", "auth"]);
    storage.setTags("projects/beta/task/beta0001.md", ["priority", "devops"]);
    storage.setTags("projects/gamma/task/gamm0001.md", ["ux"]);

    const priorityNotes = storage.listNotes({ tag: "priority" });
    expect(priorityNotes.length).toBe(2);
  });

  test("tags (OR) filter returns notes with any matching tag", () => {
    storage.setTags("projects/alpha/task/alph0001.md", ["auth"]);
    storage.setTags("projects/beta/task/beta0001.md", ["devops"]);
    storage.setTags("projects/gamma/task/gamm0001.md", ["ux"]);

    const results = storage.listNotes({ tags: ["auth", "ux"] });
    expect(results.length).toBe(2);

    const paths = results.map((n) => n.path);
    expect(paths).toContain("projects/alpha/task/alph0001.md");
    expect(paths).toContain("projects/gamma/task/gamm0001.md");
  });

  test("feature filter returns only matching feature", () => {
    const authNotes = storage.listNotes({ feature: "auth" });
    expect(authNotes.length).toBeGreaterThan(0);
    for (const note of authNotes) {
      expect(note.feature_id).toBe("auth");
    }

    const expectedAuthCount = FIXTURE_NOTES.filter((n) => n.feature_id === "auth").length;
    expect(authNotes.length).toBe(expectedAuthCount);
  });

  test("no matching filter returns empty array", () => {
    const results = storage.listNotes({ type: "nonexistent_type" });
    expect(results.length).toBe(0);
  });

  test("no filters returns all notes (up to limit)", () => {
    const all = storage.listNotes({});
    expect(all.length).toBe(FIXTURE_NOTES.length);
  });
});

// =============================================================================
// Sorting
// =============================================================================

describe("Sorting", () => {
  test("sortBy modified desc (default)", () => {
    const notes = storage.listNotes({ sortBy: "modified", sortOrder: "desc" });
    expect(notes.length).toBeGreaterThan(1);

    for (let i = 1; i < notes.length; i++) {
      expect(notes[i - 1].modified! >= notes[i].modified!).toBe(true);
    }
  });

  test("sortBy modified asc", () => {
    const notes = storage.listNotes({ sortBy: "modified", sortOrder: "asc" });
    expect(notes.length).toBeGreaterThan(1);

    for (let i = 1; i < notes.length; i++) {
      expect(notes[i - 1].modified! <= notes[i].modified!).toBe(true);
    }
  });

  test("sortBy created desc", () => {
    const notes = storage.listNotes({ sortBy: "created", sortOrder: "desc" });
    expect(notes.length).toBeGreaterThan(1);

    for (let i = 1; i < notes.length; i++) {
      expect(notes[i - 1].created! >= notes[i].created!).toBe(true);
    }
  });

  test("sortBy created asc", () => {
    const notes = storage.listNotes({ sortBy: "created", sortOrder: "asc" });
    expect(notes.length).toBeGreaterThan(1);

    for (let i = 1; i < notes.length; i++) {
      expect(notes[i - 1].created! <= notes[i].created!).toBe(true);
    }
  });

  test("sortBy priority asc (high first)", () => {
    const notes = storage.listNotes({ sortBy: "priority", sortOrder: "asc" });
    expect(notes.length).toBeGreaterThan(1);

    const priorityOrder = { high: 0, medium: 1, low: 2 } as Record<string, number>;
    for (let i = 1; i < notes.length; i++) {
      const prev = priorityOrder[notes[i - 1].priority ?? ""] ?? 3;
      const curr = priorityOrder[notes[i].priority ?? ""] ?? 3;
      expect(prev <= curr).toBe(true);
    }
  });

  test("sortBy priority desc (low first)", () => {
    const notes = storage.listNotes({ sortBy: "priority", sortOrder: "desc" });
    expect(notes.length).toBeGreaterThan(1);

    const priorityOrder = { high: 0, medium: 1, low: 2 } as Record<string, number>;
    for (let i = 1; i < notes.length; i++) {
      const prev = priorityOrder[notes[i - 1].priority ?? ""] ?? 3;
      const curr = priorityOrder[notes[i].priority ?? ""] ?? 3;
      expect(prev >= curr).toBe(true);
    }
  });

  test("sortBy title asc", () => {
    const notes = storage.listNotes({ sortBy: "title", sortOrder: "asc" });
    expect(notes.length).toBeGreaterThan(1);

    for (let i = 1; i < notes.length; i++) {
      expect(notes[i - 1].title <= notes[i].title).toBe(true);
    }
  });

  test("sortBy title desc", () => {
    const notes = storage.listNotes({ sortBy: "title", sortOrder: "desc" });
    expect(notes.length).toBeGreaterThan(1);

    for (let i = 1; i < notes.length; i++) {
      expect(notes[i - 1].title >= notes[i].title).toBe(true);
    }
  });
});

// =============================================================================
// Pagination
// =============================================================================

describe("Pagination", () => {
  test("limit parameter restricts result count", () => {
    const limited = storage.listNotes({ limit: 5 });
    expect(limited.length).toBe(5);
  });

  test("limit of 1 returns exactly one result", () => {
    const single = storage.listNotes({ limit: 1 });
    expect(single.length).toBe(1);
  });

  test("limit larger than total returns all results", () => {
    const all = storage.listNotes({ limit: 1000 });
    expect(all.length).toBe(FIXTURE_NOTES.length);
  });

  test("default limit returns all fixture notes (under 100)", () => {
    const all = storage.listNotes({});
    expect(all.length).toBe(FIXTURE_NOTES.length);
  });
});

// =============================================================================
// Entry Metadata
// =============================================================================

describe("Entry metadata", () => {
  test("recordAccess creates entry_meta record on first access", () => {
    const path = "projects/alpha/task/alph0001.md";
    storage.recordAccess(path);

    const stats = storage.getAccessStats(path);
    expect(stats).not.toBeNull();
    expect(stats!.access_count).toBe(1);
    expect(stats!.last_accessed).toBeTruthy();
    expect(stats!.created_at).toBeTruthy();
  });

  test("recordAccess increments access_count on subsequent calls", () => {
    const path = "projects/alpha/task/alph0001.md";
    storage.recordAccess(path);
    storage.recordAccess(path);
    storage.recordAccess(path);

    const stats = storage.getAccessStats(path);
    expect(stats).not.toBeNull();
    expect(stats!.access_count).toBe(3);
  });

  test("getAccessStats returns null for untracked path", () => {
    const stats = storage.getAccessStats("never/accessed.md");
    expect(stats).toBeNull();
  });

  test("setVerified marks entry as verified", () => {
    const path = "projects/alpha/task/alph0001.md";
    storage.setVerified(path);

    const meta = storage
      .getDb()
      .prepare("SELECT * FROM entry_meta WHERE path = ?")
      .get(path) as { last_verified: string | null } | null;

    expect(meta).not.toBeNull();
    expect(meta!.last_verified).toBeTruthy();
  });

  test("setVerified updates existing entry_meta record", () => {
    const path = "projects/alpha/task/alph0001.md";
    storage.recordAccess(path);
    storage.setVerified(path);

    const stats = storage.getAccessStats(path);
    expect(stats).not.toBeNull();
    expect(stats!.access_count).toBe(1);
    expect(stats!.last_verified).toBeTruthy();
  });

  test("getStaleEntries returns notes never verified", () => {
    const stale = storage.getStaleEntries(30);
    expect(stale.length).toBe(FIXTURE_NOTES.length);
  });

  test("getStaleEntries excludes recently verified notes", () => {
    storage.setVerified("projects/alpha/task/alph0001.md");
    storage.setVerified("projects/alpha/task/alph0002.md");

    const stale = storage.getStaleEntries(30);
    expect(stale.length).toBe(FIXTURE_NOTES.length - 2);

    const stalePaths = stale.map((n) => n.path);
    expect(stalePaths).not.toContain("projects/alpha/task/alph0001.md");
    expect(stalePaths).not.toContain("projects/alpha/task/alph0002.md");
  });

  test("getStaleEntries respects type filter", () => {
    const staleTasks = storage.getStaleEntries(30, { type: "task" });
    const expectedTaskCount = FIXTURE_NOTES.filter((n) => n.type === "task").length;
    expect(staleTasks.length).toBe(expectedTaskCount);

    for (const note of staleTasks) {
      expect(note.type).toBe("task");
    }
  });

  test("getStaleEntries respects limit", () => {
    const stale = storage.getStaleEntries(30, { limit: 3 });
    expect(stale.length).toBe(3);
  });
});

// =============================================================================
// Stats
// =============================================================================

describe("Stats", () => {
  test("getStats returns correct total count", () => {
    const stats = storage.getStats();
    expect(stats.total).toBe(FIXTURE_NOTES.length);
  });

  test("getStats returns correct byType breakdown", () => {
    const stats = storage.getStats();

    const expectedByType: Record<string, number> = {};
    for (const note of FIXTURE_NOTES) {
      if (note.type) {
        expectedByType[note.type] = (expectedByType[note.type] || 0) + 1;
      }
    }

    for (const [type, count] of Object.entries(expectedByType)) {
      expect(stats.byType[type]).toBe(count);
    }
  });

  test("getStats returns orphan count", () => {
    const stats = storage.getStats();
    expect(stats.orphanCount).toBeGreaterThan(0);
    expect(stats.orphanCount).toBeLessThanOrEqual(FIXTURE_NOTES.length);
  });

  test("getStats returns tracked count (entry_meta)", () => {
    let stats = storage.getStats();
    expect(stats.trackedCount).toBe(0);

    storage.recordAccess("projects/alpha/task/alph0001.md");
    storage.recordAccess("projects/beta/task/beta0001.md");

    stats = storage.getStats();
    expect(stats.trackedCount).toBe(2);
  });

  test("getStats returns stale count", () => {
    const stats = storage.getStats();
    expect(stats.staleCount).toBe(FIXTURE_NOTES.length);
  });

  test("getStats with path filter scopes to prefix", () => {
    const alphaStats = storage.getStats({ path: "projects/alpha/" });
    const expectedAlphaCount = FIXTURE_NOTES.filter((n) =>
      n.path.startsWith("projects/alpha/")
    ).length;
    expect(alphaStats.total).toBe(expectedAlphaCount);

    const betaStats = storage.getStats({ path: "projects/beta/" });
    const expectedBetaCount = FIXTURE_NOTES.filter((n) =>
      n.path.startsWith("projects/beta/")
    ).length;
    expect(betaStats.total).toBe(expectedBetaCount);
  });

  test("getStats with path filter scopes byType", () => {
    const alphaStats = storage.getStats({ path: "projects/alpha/" });

    const expectedAlphaByType: Record<string, number> = {};
    for (const note of FIXTURE_NOTES) {
      if (note.path.startsWith("projects/alpha/") && note.type) {
        expectedAlphaByType[note.type] = (expectedAlphaByType[note.type] || 0) + 1;
      }
    }

    for (const [type, count] of Object.entries(expectedAlphaByType)) {
      expect(alphaStats.byType[type]).toBe(count);
    }
  });
});

// =============================================================================
// Edge Cases
// =============================================================================

describe("Edge cases", () => {
  test("duplicate path insert throws", () => {
    const note = makeTestNote({
      path: "projects/alpha/task/alph0001.md",
      short_id: "dupl0001",
    });

    expect(() => {
      storage.insertNote(note);
    }).toThrow();
  });

  test("update non-existent path returns null", () => {
    const result = storage.updateNote("nonexistent/path.md", {
      title: "Won't work",
    });
    expect(result).toBeNull();
  });

  test("unicode in content is preserved", () => {
    const note = storage.getNoteByPath("projects/gamma/exploration/gamm0004.md");
    expect(note).not.toBeNull();
    expect(note!.title).toBe("研究: Internationalization approaches");
    expect(note!.body).toContain("多言語サポート");
  });

  test("unicode in title is preserved through insert and retrieve", () => {
    const fresh = createTestStorage();
    const note = makeTestNote({
      path: "unicode/test.md",
      short_id: "unic0001",
      title: "Ünïcödé Tëst: 日本語テスト",
      body: "Content with spëcial chars: àáâãäå",
    });

    fresh.insertNote(note);
    const retrieved = fresh.getNoteByPath("unicode/test.md");
    expect(retrieved).not.toBeNull();
    expect(retrieved!.title).toBe("Ünïcödé Tëst: 日本語テスト");
    expect(retrieved!.body).toContain("spëcial chars");
    fresh.close();
  });

  test("large body (>10KB) is stored and retrieved correctly", () => {
    const fresh = createTestStorage();
    const largeBody = "A".repeat(15000);
    const note = makeTestNote({
      path: "large/body.md",
      short_id: "larg0001",
      body: largeBody,
      raw_content: `---\ntitle: Large Body\n---\n${largeBody}`,
      word_count: 1,
    });

    fresh.insertNote(note);
    const retrieved = fresh.getNoteByPath("large/body.md");
    expect(retrieved).not.toBeNull();
    expect(retrieved!.body.length).toBe(15000);
    expect(retrieved!.body).toBe(largeBody);
    fresh.close();
  });

  test("null fields are stored and retrieved as null", () => {
    const fresh = createTestStorage();
    const note = makeTestNote({
      path: "null/fields.md",
      short_id: "null0001",
      type: null,
      status: null,
      priority: null,
      project_id: null,
      feature_id: null,
      created: null,
      modified: null,
      checksum: null,
    });

    fresh.insertNote(note);
    const retrieved = fresh.getNoteByPath("null/fields.md");
    expect(retrieved).not.toBeNull();
    expect(retrieved!.type).toBeNull();
    expect(retrieved!.status).toBeNull();
    expect(retrieved!.priority).toBeNull();
    expect(retrieved!.project_id).toBeNull();
    expect(retrieved!.feature_id).toBeNull();
    expect(retrieved!.created).toBeNull();
    expect(retrieved!.modified).toBeNull();
    expect(retrieved!.checksum).toBeNull();
    fresh.close();
  });
});

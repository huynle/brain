/**
 * Storage Graph Integration Tests
 *
 * Tests for graph operations (backlinks, outlinks, related, orphans) and
 * link resolution using seeded fixture data. Covers dynamic graph updates,
 * circular references, and edge cases.
 */

import { describe, test, expect, beforeEach, afterEach } from "bun:test";

import {
  createTestStorage,
  makeTestNote,
  type StorageLayer,
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
// Backlinks
// =============================================================================

describe("Backlinks", () => {
  test("getBacklinks returns notes that link TO a target", () => {
    // alph0001 (auth task) is linked to by:
    //   - alph0003 (plan links to alph0001)
    //   - alph0002 (task depends on alph0001)
    const backlinks = storage.getBacklinks("projects/alpha/task/alph0001.md");
    expect(backlinks.length).toBe(2);

    const backlinkPaths = backlinks.map((n) => n.path).sort();
    expect(backlinkPaths).toContain("projects/alpha/plan/alph0003.md");
    expect(backlinkPaths).toContain("projects/alpha/task/alph0002.md");
  });

  test("getBacklinks returns multiple sources for heavily-linked note", () => {
    // alph0005 (decision) is linked to by:
    //   - alph0001 (task links to decision)
    //   - alph0004 (exploration informs decision)
    const backlinks = storage.getBacklinks("projects/alpha/decision/alph0005.md");
    expect(backlinks.length).toBe(2);

    const backlinkPaths = backlinks.map((n) => n.path).sort();
    expect(backlinkPaths).toContain("projects/alpha/task/alph0001.md");
    expect(backlinkPaths).toContain("projects/alpha/exploration/alph0004.md");
  });

  test("getBacklinks returns empty array for orphan node (no incoming links)", () => {
    // alph0006 (idea: GraphQL gateway) has no incoming links in fixtures
    const backlinks = storage.getBacklinks("projects/alpha/idea/alph0006.md");
    expect(backlinks.length).toBe(0);
  });

  test("getBacklinks returns empty array for non-existent path", () => {
    const backlinks = storage.getBacklinks("does/not/exist.md");
    expect(backlinks.length).toBe(0);
  });

  test("getBacklinks includes cross-project links", () => {
    // alph0007 (learning) is linked to by gamm0001 (cross-project)
    const backlinks = storage.getBacklinks("projects/alpha/learning/alph0007.md");
    expect(backlinks.length).toBe(1);
    expect(backlinks[0].path).toBe("projects/gamma/task/gamm0001.md");
  });

  test("getBacklinks includes global-to-global links", () => {
    // glob0001 (learning) is linked to by glob0003 (decision)
    const backlinks = storage.getBacklinks("global/learning/glob0001.md");
    expect(backlinks.length).toBe(1);
    expect(backlinks[0].path).toBe("global/decision/glob0003.md");
  });
});

// =============================================================================
// Outlinks
// =============================================================================

describe("Outlinks", () => {
  test("getOutlinks returns notes linked BY a source", () => {
    // alph0003 (plan) links to alph0001 and alph0002
    const outlinks = storage.getOutlinks("projects/alpha/plan/alph0003.md");
    expect(outlinks.length).toBe(2);

    const outlinkPaths = outlinks.map((n) => n.path).sort();
    expect(outlinkPaths).toContain("projects/alpha/task/alph0001.md");
    expect(outlinkPaths).toContain("projects/alpha/task/alph0002.md");
  });

  test("getOutlinks returns single outlink", () => {
    // alph0001 links to alph0005 (decision)
    const outlinks = storage.getOutlinks("projects/alpha/task/alph0001.md");
    expect(outlinks.length).toBe(1);
    expect(outlinks[0].path).toBe("projects/alpha/decision/alph0005.md");
  });

  test("getOutlinks returns empty for note with no outgoing links", () => {
    // alph0006 (idea) has no outgoing links in fixtures
    const outlinks = storage.getOutlinks("projects/alpha/idea/alph0006.md");
    expect(outlinks.length).toBe(0);
  });

  test("getOutlinks returns empty for non-existent path", () => {
    const outlinks = storage.getOutlinks("does/not/exist.md");
    expect(outlinks.length).toBe(0);
  });

  test("getOutlinks only returns resolved links (target exists in DB)", () => {
    // Create a note with a link to a non-existent target
    const fresh = createTestStorage();
    fresh.insertNote(makeTestNote({
      path: "source/note.md",
      short_id: "srcnote1",
      body: "Links to [missing](missing/target.md)",
    }));
    fresh.setLinks("source/note.md", [{
      target_path: "missing/target.md",
      target_id: null,
      title: "missing",
      href: "missing/target.md",
      type: "markdown",
      snippet: "",
    }]);

    const outlinks = fresh.getOutlinks("source/note.md");
    expect(outlinks.length).toBe(0); // target doesn't exist, so not resolved
    fresh.close();
  });
});

// =============================================================================
// Related
// =============================================================================

describe("Related", () => {
  test("notes linking to the same target appear as related", () => {
    // alph0001 and alph0004 both link to alph0005 (decision)
    // So alph0001 and alph0004 should be related
    const relatedToAlph0001 = storage.getRelated("projects/alpha/task/alph0001.md");
    const relatedPaths = relatedToAlph0001.map((n) => n.path);
    expect(relatedPaths).toContain("projects/alpha/exploration/alph0004.md");
  });

  test("related excludes self", () => {
    const related = storage.getRelated("projects/alpha/task/alph0001.md");
    const relatedPaths = related.map((n) => n.path);
    expect(relatedPaths).not.toContain("projects/alpha/task/alph0001.md");
  });

  test("related respects limit parameter", () => {
    const related = storage.getRelated("projects/alpha/task/alph0001.md", 1);
    expect(related.length).toBeLessThanOrEqual(1);
  });

  test("related returns empty for note with no shared targets", () => {
    // alph0006 (idea) has no outgoing links, so no shared targets
    const related = storage.getRelated("projects/alpha/idea/alph0006.md");
    expect(related.length).toBe(0);
  });

  test("related returns empty for non-existent path", () => {
    const related = storage.getRelated("does/not/exist.md");
    expect(related.length).toBe(0);
  });

  test("related default limit is 10", () => {
    // With fixture data, we won't exceed 10, but verify it doesn't crash
    const related = storage.getRelated("projects/alpha/plan/alph0003.md");
    expect(related.length).toBeLessThanOrEqual(10);
  });
});

// =============================================================================
// Orphans
// =============================================================================

describe("Orphans", () => {
  test("getOrphans returns notes with no incoming links", () => {
    const orphans = storage.getOrphans();
    expect(orphans.length).toBeGreaterThan(0);

    // Every orphan should have no incoming links
    for (const orphan of orphans) {
      const backlinks = storage.getBacklinks(orphan.path);
      expect(backlinks.length).toBe(0);
    }
  });

  test("getOrphans does NOT include notes that have incoming links", () => {
    const orphans = storage.getOrphans();
    const orphanPaths = orphans.map((n) => n.path);

    // alph0001 has backlinks from alph0003 and alph0002 — should NOT be orphan
    expect(orphanPaths).not.toContain("projects/alpha/task/alph0001.md");

    // alph0005 has backlinks from alph0001 and alph0004 — should NOT be orphan
    expect(orphanPaths).not.toContain("projects/alpha/decision/alph0005.md");
  });

  test("getOrphans includes notes that are never linked to", () => {
    const orphans = storage.getOrphans();
    const orphanPaths = orphans.map((n) => n.path);

    // alph0006 (idea: GraphQL gateway) has no incoming links
    expect(orphanPaths).toContain("projects/alpha/idea/alph0006.md");
  });

  test("getOrphans with type filter returns only matching type", () => {
    const orphanIdeas = storage.getOrphans({ type: "idea" });
    expect(orphanIdeas.length).toBeGreaterThan(0);

    for (const orphan of orphanIdeas) {
      expect(orphan.type).toBe("idea");
    }
  });

  test("getOrphans with limit restricts results", () => {
    const orphans = storage.getOrphans({ limit: 2 });
    expect(orphans.length).toBeLessThanOrEqual(2);
  });

  test("getOrphans with non-matching type returns empty", () => {
    const orphans = storage.getOrphans({ type: "nonexistent_type" });
    expect(orphans.length).toBe(0);
  });
});

// =============================================================================
// Link Resolution
// =============================================================================

describe("Link resolution", () => {
  test("resolveLink by exact path", () => {
    const note = storage.resolveLink("projects/alpha/task/alph0001.md");
    expect(note).not.toBeNull();
    expect(note!.short_id).toBe("alph0001");
    expect(note!.title).toBe("Implement authentication module");
  });

  test("resolveLink by short_id", () => {
    const note = storage.resolveLink("alph0001");
    expect(note).not.toBeNull();
    expect(note!.path).toBe("projects/alpha/task/alph0001.md");
  });

  test("resolveLink by path ending", () => {
    // "task/alph0001.md" should match "projects/alpha/task/alph0001.md"
    const note = storage.resolveLink("task/alph0001.md");
    expect(note).not.toBeNull();
    expect(note!.path).toBe("projects/alpha/task/alph0001.md");
  });

  test("resolveLink returns null for nonexistent href", () => {
    const note = storage.resolveLink("nonexistent/path.md");
    expect(note).toBeNull();
  });

  test("resolveLink returns null for nonexistent short_id", () => {
    const note = storage.resolveLink("zzzzzzzz");
    expect(note).toBeNull();
  });

  test("resolveLink prefers exact path over short_id", () => {
    // If a path exactly matches, it should be returned even if short_id also matches
    const note = storage.resolveLink("projects/alpha/task/alph0001.md");
    expect(note).not.toBeNull();
    expect(note!.path).toBe("projects/alpha/task/alph0001.md");
  });

  test("resolveLink works for global notes", () => {
    const note = storage.resolveLink("glob0001");
    expect(note).not.toBeNull();
    expect(note!.path).toBe("global/learning/glob0001.md");
  });

  test("resolveLink by path ending for global notes", () => {
    const note = storage.resolveLink("decision/glob0003.md");
    expect(note).not.toBeNull();
    expect(note!.path).toBe("global/decision/glob0003.md");
  });
});

// =============================================================================
// Dynamic Graph Updates
// =============================================================================

describe("Dynamic graph updates", () => {
  test("adding a new note with links updates backlinks of targets", () => {
    // Before: alph0001 has 2 backlinks (from alph0003 and alph0002)
    const backlinksBefore = storage.getBacklinks("projects/alpha/task/alph0001.md");
    expect(backlinksBefore.length).toBe(2);

    // Add a new note that links to alph0001
    storage.insertNote(makeTestNote({
      path: "projects/alpha/task/newn0001.md",
      short_id: "newn0001",
      title: "New Task Linking to Auth",
      body: "This depends on auth module",
      project_id: "alpha",
    }));
    storage.setLinks("projects/alpha/task/newn0001.md", [{
      target_path: "projects/alpha/task/alph0001.md",
      target_id: null,
      title: "Auth Module",
      href: "projects/alpha/task/alph0001.md",
      type: "markdown",
      snippet: "",
    }]);

    // After: alph0001 should have 3 backlinks
    const backlinksAfter = storage.getBacklinks("projects/alpha/task/alph0001.md");
    expect(backlinksAfter.length).toBe(3);

    const backlinkPaths = backlinksAfter.map((n) => n.path);
    expect(backlinkPaths).toContain("projects/alpha/task/newn0001.md");
  });

  test("deleting a note changes orphan graph", () => {
    // alph0002 is linked to by alph0003 (plan). After deleting alph0003,
    // alph0002 should become an orphan.
    const orphansBefore = storage.getOrphans();
    const orphanPathsBefore = orphansBefore.map((n) => n.path);
    expect(orphanPathsBefore).not.toContain("projects/alpha/task/alph0002.md");

    storage.deleteNote("projects/alpha/plan/alph0003.md");

    const orphansAfter = storage.getOrphans();
    const orphanPathsAfter = orphansAfter.map((n) => n.path);

    // alph0003 should no longer appear (deleted)
    expect(orphanPathsAfter).not.toContain("projects/alpha/plan/alph0003.md");

    // alph0002 should now be an orphan (its only incoming link source was deleted)
    expect(orphanPathsAfter).toContain("projects/alpha/task/alph0002.md");
  });

  test("adding a link between existing notes updates related list", () => {
    // Create two notes that don't share any targets initially
    const fresh = createTestStorage();
    fresh.insertNote(makeTestNote({
      path: "note-a.md",
      short_id: "noteaaaa",
      title: "Note A",
    }));
    fresh.insertNote(makeTestNote({
      path: "note-b.md",
      short_id: "notebbbb",
      title: "Note B",
    }));
    fresh.insertNote(makeTestNote({
      path: "note-c.md",
      short_id: "notecccc",
      title: "Note C (shared target)",
    }));

    // Initially, A and B are not related (no shared targets)
    expect(fresh.getRelated("note-a.md").length).toBe(0);

    // Add link from A to C
    fresh.setLinks("note-a.md", [{
      target_path: "note-c.md",
      target_id: null,
      title: "Note C",
      href: "note-c.md",
      type: "markdown",
      snippet: "",
    }]);

    // Add link from B to C (same target)
    fresh.setLinks("note-b.md", [{
      target_path: "note-c.md",
      target_id: null,
      title: "Note C",
      href: "note-c.md",
      type: "markdown",
      snippet: "",
    }]);

    // Now A and B should be related (both link to C)
    const relatedToA = fresh.getRelated("note-a.md");
    expect(relatedToA.length).toBe(1);
    expect(relatedToA[0].path).toBe("note-b.md");

    const relatedToB = fresh.getRelated("note-b.md");
    expect(relatedToB.length).toBe(1);
    expect(relatedToB[0].path).toBe("note-a.md");

    fresh.close();
  });
});

// =============================================================================
// Circular References
// =============================================================================

describe("Circular references", () => {
  test("A→B→C→A cycle: backlinks and outlinks work without infinite loops", () => {
    const fresh = createTestStorage();

    // Create three notes forming a cycle
    fresh.insertNote(makeTestNote({
      path: "cycle/a.md",
      short_id: "cycleaaa",
      title: "Cycle A",
    }));
    fresh.insertNote(makeTestNote({
      path: "cycle/b.md",
      short_id: "cyclebbb",
      title: "Cycle B",
    }));
    fresh.insertNote(makeTestNote({
      path: "cycle/c.md",
      short_id: "cycleccc",
      title: "Cycle C",
    }));

    // A → B
    fresh.setLinks("cycle/a.md", [{
      target_path: "cycle/b.md",
      target_id: null,
      title: "B",
      href: "cycle/b.md",
      type: "markdown",
      snippet: "",
    }]);

    // B → C
    fresh.setLinks("cycle/b.md", [{
      target_path: "cycle/c.md",
      target_id: null,
      title: "C",
      href: "cycle/c.md",
      type: "markdown",
      snippet: "",
    }]);

    // C → A (completes the cycle)
    fresh.setLinks("cycle/c.md", [{
      target_path: "cycle/a.md",
      target_id: null,
      title: "A",
      href: "cycle/a.md",
      type: "markdown",
      snippet: "",
    }]);

    // Backlinks should work correctly
    const backlinksA = fresh.getBacklinks("cycle/a.md");
    expect(backlinksA.length).toBe(1);
    expect(backlinksA[0].path).toBe("cycle/c.md");

    const backlinksB = fresh.getBacklinks("cycle/b.md");
    expect(backlinksB.length).toBe(1);
    expect(backlinksB[0].path).toBe("cycle/a.md");

    const backlinksC = fresh.getBacklinks("cycle/c.md");
    expect(backlinksC.length).toBe(1);
    expect(backlinksC[0].path).toBe("cycle/b.md");

    // Outlinks should work correctly
    const outlinksA = fresh.getOutlinks("cycle/a.md");
    expect(outlinksA.length).toBe(1);
    expect(outlinksA[0].path).toBe("cycle/b.md");

    const outlinksB = fresh.getOutlinks("cycle/b.md");
    expect(outlinksB.length).toBe(1);
    expect(outlinksB[0].path).toBe("cycle/c.md");

    const outlinksC = fresh.getOutlinks("cycle/c.md");
    expect(outlinksC.length).toBe(1);
    expect(outlinksC[0].path).toBe("cycle/a.md");

    // No orphans in a cycle (every node has at least one incoming link)
    const orphans = fresh.getOrphans();
    expect(orphans.length).toBe(0);

    fresh.close();
  });

  test("self-referencing note: A→A does not cause issues", () => {
    const fresh = createTestStorage();

    fresh.insertNote(makeTestNote({
      path: "self/ref.md",
      short_id: "selfref1",
      title: "Self Reference",
    }));

    // A → A (self-link)
    fresh.setLinks("self/ref.md", [{
      target_path: "self/ref.md",
      target_id: null,
      title: "Self",
      href: "self/ref.md",
      type: "markdown",
      snippet: "",
    }]);

    // Backlinks: A links to itself, so A is a backlink of A
    const backlinks = fresh.getBacklinks("self/ref.md");
    expect(backlinks.length).toBe(1);
    expect(backlinks[0].path).toBe("self/ref.md");

    // Outlinks: A links to itself
    const outlinks = fresh.getOutlinks("self/ref.md");
    expect(outlinks.length).toBe(1);
    expect(outlinks[0].path).toBe("self/ref.md");

    // Not an orphan (has incoming link from itself)
    const orphans = fresh.getOrphans();
    expect(orphans.length).toBe(0);

    fresh.close();
  });

  test("related in a cycle: A→B→C→A, each has unique target so no related", () => {
    const fresh = createTestStorage();

    fresh.insertNote(makeTestNote({
      path: "rel/a.md",
      short_id: "relaaa01",
      title: "Rel A",
    }));
    fresh.insertNote(makeTestNote({
      path: "rel/b.md",
      short_id: "relbbb01",
      title: "Rel B",
    }));
    fresh.insertNote(makeTestNote({
      path: "rel/c.md",
      short_id: "relccc01",
      title: "Rel C",
    }));

    // A → B, B → C, C → A
    fresh.setLinks("rel/a.md", [{
      target_path: "rel/b.md", target_id: null, title: "B",
      href: "rel/b.md", type: "markdown", snippet: "",
    }]);
    fresh.setLinks("rel/b.md", [{
      target_path: "rel/c.md", target_id: null, title: "C",
      href: "rel/c.md", type: "markdown", snippet: "",
    }]);
    fresh.setLinks("rel/c.md", [{
      target_path: "rel/a.md", target_id: null, title: "A",
      href: "rel/a.md", type: "markdown", snippet: "",
    }]);

    // In a simple cycle, each node has a unique target, so no related notes.
    const relatedA = fresh.getRelated("rel/a.md");
    expect(relatedA.length).toBe(0);

    fresh.close();
  });
});

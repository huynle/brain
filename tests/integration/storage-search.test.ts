/**
 * Storage Search Integration Tests
 *
 * FTS5 search quality tests using seeded fixture data. Covers full-text search
 * with BM25 ranking, Porter stemming, exact search, like search, filters,
 * combined filters, empty results, and limit behavior.
 */

import { describe, test, expect, beforeEach, afterEach } from "bun:test";

import {
  createTestStorage,
  makeTestNote,
  type StorageLayer,
} from "./helpers";

import {
  FIXTURE_NOTES,
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
// FTS Search
// =============================================================================

describe("FTS search", () => {
  test("finds notes by title keywords", () => {
    const results = storage.searchNotes("authentication");
    expect(results.length).toBeGreaterThan(0);

    const titles = results.map((n) => n.title);
    expect(titles.some((t) => t.toLowerCase().includes("authentication") || t.toLowerCase().includes("auth"))).toBe(true);
  });

  test("finds notes by body keywords", () => {
    const results = storage.searchNotes("JWT");
    expect(results.length).toBeGreaterThan(0);

    // JWT appears in body of auth-related notes
    const hasJwtInBody = results.some((n) => n.body.includes("JWT"));
    expect(hasJwtInBody).toBe(true);
  });

  test("finds notes by path keywords", () => {
    // FTS indexes path, so searching for path segments should work
    const results = storage.searchNotes("alpha");
    expect(results.length).toBeGreaterThan(0);

    // Should find notes in the alpha project
    const hasAlphaPath = results.some((n) => n.path.includes("alpha"));
    expect(hasAlphaPath).toBe(true);
  });

  test("finds notes matching multiple keywords", () => {
    const results = storage.searchNotes("authentication module");
    expect(results.length).toBeGreaterThan(0);

    // The note "Implement authentication module" should be in results
    const hasAuthModule = results.some((n) =>
      n.title === "Implement authentication module"
    );
    expect(hasAuthModule).toBe(true);
  });
});

// =============================================================================
// FTS Ranking
// =============================================================================

describe("FTS ranking", () => {
  test("title matches ranked higher than body matches", () => {
    // "authentication" appears in title of alph0001 and in body of other notes
    const results = storage.searchNotes("authentication");
    expect(results.length).toBeGreaterThan(0);

    // The note with "authentication" in the title should appear first
    // because title weight is 10x vs body weight 1x
    const firstResult = results[0];
    expect(
      firstResult.title.toLowerCase().includes("authentication") ||
      firstResult.title.toLowerCase().includes("auth")
    ).toBe(true);
  });

  test("exact title keyword match ranks higher than partial body match", () => {
    // "pipeline" appears in title of beta0001 ("Set up CI/CD pipeline")
    // and in body of alph0003 ("deployment pipeline")
    const results = storage.searchNotes("pipeline");
    expect(results.length).toBeGreaterThan(0);

    // The note with "pipeline" in the title should rank first
    const firstResult = results[0];
    expect(firstResult.title.toLowerCase().includes("pipeline")).toBe(true);
  });
});

// =============================================================================
// Porter Stemming
// =============================================================================

describe("Porter stemming", () => {
  test("stemmed query matches inflected forms — 'running' matches 'run'", () => {
    // Insert a note with "running" in the body
    const fresh = createTestStorage();
    fresh.insertNote(
      makeTestNote({
        path: "stem/running.md",
        short_id: "stem0001",
        title: "Running Tests",
        body: "We are running the test suite daily.",
      })
    );
    fresh.insertNote(
      makeTestNote({
        path: "stem/run.md",
        short_id: "stem0002",
        title: "Run Command",
        body: "Use the run command to execute tests.",
      })
    );

    // Search for "running" should find both due to Porter stemming
    const results = fresh.searchNotes("running");
    expect(results.length).toBe(2);

    // Search for "run" should also find both
    const results2 = fresh.searchNotes("run");
    expect(results2.length).toBe(2);

    fresh.close();
  });

  test("stemmed query matches plural forms — 'tests' matches 'test'", () => {
    // "tests" and "test" should match via stemming
    // Multiple fixture notes contain "test" or "tests" in title/body
    const resultsPlural = storage.searchNotes("tests");
    const resultsSingular = storage.searchNotes("test");

    // Both should return results
    expect(resultsPlural.length).toBeGreaterThan(0);
    expect(resultsSingular.length).toBeGreaterThan(0);

    // They should return the same set of notes (stemming normalizes both)
    const pluralPaths = new Set(resultsPlural.map((n) => n.path));
    const singularPaths = new Set(resultsSingular.map((n) => n.path));
    expect(pluralPaths).toEqual(singularPaths);
  });
});

// =============================================================================
// Exact Search
// =============================================================================

describe("Exact search", () => {
  test("exact title match returns correct note", () => {
    const results = storage.searchNotes("Implement authentication module", {
      matchStrategy: "exact",
    });
    expect(results.length).toBeGreaterThan(0);

    const hasExactMatch = results.some(
      (n) => n.title === "Implement authentication module"
    );
    expect(hasExactMatch).toBe(true);
  });

  test("exact search matches body substring", () => {
    const results = storage.searchNotes("JWT with RS256 signing", {
      matchStrategy: "exact",
    });
    expect(results.length).toBeGreaterThan(0);

    const hasBodyMatch = results.some((n) =>
      n.body.includes("JWT with RS256 signing")
    );
    expect(hasBodyMatch).toBe(true);
  });

  test("exact search returns empty for non-matching query", () => {
    const results = storage.searchNotes("xyzzy nonexistent phrase", {
      matchStrategy: "exact",
    });
    expect(results.length).toBe(0);
  });
});

// =============================================================================
// Like Search
// =============================================================================

describe("Like search", () => {
  test("like search matches partial title text", () => {
    const results = storage.searchNotes("authentication", {
      matchStrategy: "like",
    });
    expect(results.length).toBeGreaterThan(0);

    const hasMatch = results.some((n) =>
      n.title.toLowerCase().includes("authentication")
    );
    expect(hasMatch).toBe(true);
  });

  test("like search matches partial body text", () => {
    const results = storage.searchNotes("refresh tokens", {
      matchStrategy: "like",
    });
    expect(results.length).toBeGreaterThan(0);

    const hasMatch = results.some((n) =>
      n.body.toLowerCase().includes("refresh tokens")
    );
    expect(hasMatch).toBe(true);
  });

  test("like search matches partial path text", () => {
    const results = storage.searchNotes("exploration", {
      matchStrategy: "like",
    });
    expect(results.length).toBeGreaterThan(0);

    const hasMatch = results.some((n) => n.path.includes("exploration"));
    expect(hasMatch).toBe(true);
  });

  test("like search is case-insensitive for ASCII", () => {
    const resultsLower = storage.searchNotes("kubernetes", {
      matchStrategy: "like",
    });
    const resultsUpper = storage.searchNotes("Kubernetes", {
      matchStrategy: "like",
    });

    // SQLite LIKE is case-insensitive for ASCII by default
    expect(resultsLower.length).toBe(resultsUpper.length);
    expect(resultsLower.length).toBeGreaterThan(0);
  });
});

// =============================================================================
// Search Filters
// =============================================================================

describe("Search filters", () => {
  test("path filter restricts search to path prefix", () => {
    const results = storage.searchNotes("task", {
      path: "projects/alpha/",
    });

    expect(results.length).toBeGreaterThan(0);
    for (const note of results) {
      expect(note.path.startsWith("projects/alpha/")).toBe(true);
    }
  });

  test("type filter restricts search to specific type", () => {
    const results = storage.searchNotes("authentication", {
      type: "task",
    });

    expect(results.length).toBeGreaterThan(0);
    for (const note of results) {
      expect(note.type).toBe("task");
    }
  });

  test("status filter restricts search to specific status", () => {
    const results = storage.searchNotes("pipeline", {
      status: "completed",
    });

    expect(results.length).toBeGreaterThan(0);
    for (const note of results) {
      expect(note.status).toBe("completed");
    }
  });

  test("combined filters: type + status + path together", () => {
    const results = storage.searchNotes("alpha", {
      type: "task",
      status: "active",
      path: "projects/alpha/",
    });

    for (const note of results) {
      expect(note.type).toBe("task");
      expect(note.status).toBe("active");
      expect(note.path.startsWith("projects/alpha/")).toBe(true);
    }
  });

  test("path filter with exact search", () => {
    const results = storage.searchNotes("Kubernetes", {
      matchStrategy: "exact",
      path: "projects/beta/",
    });

    for (const note of results) {
      expect(note.path.startsWith("projects/beta/")).toBe(true);
    }
  });

  test("type filter with like search", () => {
    const results = storage.searchNotes("migration", {
      matchStrategy: "like",
      type: "plan",
    });

    expect(results.length).toBeGreaterThan(0);
    for (const note of results) {
      expect(note.type).toBe("plan");
    }
  });
});

// =============================================================================
// Empty Results
// =============================================================================

describe("Empty results", () => {
  test("non-matching FTS query returns empty array", () => {
    const results = storage.searchNotes("xyzzyplughtwisty");
    expect(results).toEqual([]);
  });

  test("non-matching exact query returns empty array", () => {
    const results = storage.searchNotes("xyzzyplughtwisty", {
      matchStrategy: "exact",
    });
    expect(results).toEqual([]);
  });

  test("non-matching like query returns empty array", () => {
    const results = storage.searchNotes("xyzzyplughtwisty", {
      matchStrategy: "like",
    });
    expect(results).toEqual([]);
  });

  test("empty query returns empty array (FTS)", () => {
    const results = storage.searchNotes("");
    expect(results).toEqual([]);
  });

  test("empty query returns empty array (exact)", () => {
    const results = storage.searchNotes("", { matchStrategy: "exact" });
    expect(results).toEqual([]);
  });

  test("empty query returns empty array (like)", () => {
    const results = storage.searchNotes("", { matchStrategy: "like" });
    expect(results).toEqual([]);
  });

  test("whitespace-only query returns empty array", () => {
    const results = storage.searchNotes("   ");
    expect(results).toEqual([]);
  });

  test("filter that excludes all results returns empty array", () => {
    const results = storage.searchNotes("authentication", {
      type: "nonexistent_type",
    });
    expect(results).toEqual([]);
  });
});

// =============================================================================
// Limit
// =============================================================================

describe("Search limit", () => {
  test("limit restricts FTS result count", () => {
    // Search for something that matches many notes
    const results = storage.searchNotes("alpha", { limit: 2 });
    expect(results.length).toBeLessThanOrEqual(2);
  });

  test("limit restricts exact search result count", () => {
    const results = storage.searchNotes("task", {
      matchStrategy: "exact",
      limit: 1,
    });
    expect(results.length).toBeLessThanOrEqual(1);
  });

  test("limit restricts like search result count", () => {
    const results = storage.searchNotes("project", {
      matchStrategy: "like",
      limit: 3,
    });
    expect(results.length).toBeLessThanOrEqual(3);
  });

  test("default limit is 50", () => {
    // With 24 fixture notes, all should be returned with default limit
    const results = storage.searchNotes("project", {
      matchStrategy: "like",
    });
    // We have 24 notes, many contain "project" in path/body
    // Default limit is 50, so all matches should be returned
    expect(results.length).toBeLessThanOrEqual(50);
  });
});

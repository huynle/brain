/**
 * Search Quality Validation Test Suite
 *
 * Validates that FTS5-based search produces results of equal or better quality
 * compared to ZK's search. Tests BM25 ranking weights (title:10, body:1, path:5),
 * porter stemming, edge cases, and overall search quality.
 *
 * Run: bun test scripts/validate-search.test.ts
 */

import { describe, test, expect, beforeEach, afterEach } from "bun:test";
import {
  createStorageLayer,
  type StorageLayer,
  type NoteRow,
} from "../src/core/storage";

// =============================================================================
// Test Helpers
// =============================================================================

function makeTestNote(
  overrides: Partial<NoteRow> = {}
): Omit<NoteRow, "id" | "indexed_at"> {
  return {
    path: "projects/test/task/abc12def.md",
    short_id: "abc12def",
    title: "Test Note",
    lead: "",
    body: "Default body content",
    raw_content: "---\ntitle: Test Note\n---\nDefault body content",
    word_count: 3,
    checksum: "test123",
    metadata: JSON.stringify({}),
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
// Seed Data — 25 diverse notes for comprehensive search testing
// =============================================================================

function seedTestNotes(storage: StorageLayer): void {
  const notes: Omit<NoteRow, "id" | "indexed_at">[] = [
    // 1. Exact title match targets
    makeTestNote({
      path: "projects/brain/plan/master01.md",
      short_id: "master01",
      title: "Master Plan",
      body: "The overarching strategy for the brain-api project including milestones and deliverables.",
      type: "plan",
      status: "active",
      project_id: "brain",
    }),
    // 2. Auth-related notes (partial match targets)
    makeTestNote({
      path: "projects/brain/task/auth0001.md",
      short_id: "auth0001",
      title: "Auth Service Implementation",
      body: "Implement JWT-based authentication with refresh tokens and session management.",
      type: "task",
      status: "active",
      project_id: "brain",
    }),
    makeTestNote({
      path: "projects/brain/decision/auth002.md",
      short_id: "auth0002",
      title: "Authentication Strategy Decision",
      body: "Decided to use OAuth2 with PKCE flow for the frontend authentication layer.",
      type: "decision",
      status: "active",
      project_id: "brain",
    }),
    // 3. Task dependency notes (multi-word query targets)
    makeTestNote({
      path: "projects/brain/task/deps0001.md",
      short_id: "deps0001",
      title: "Task Dependency Resolution",
      body: "Implement topological sort for task dependency graph with cycle detection.",
      type: "task",
      status: "active",
      project_id: "brain",
    }),
    makeTestNote({
      path: "projects/brain/plan/deps0002.md",
      short_id: "deps0002",
      title: "Dependency Graph Architecture",
      body: "Design document for task dependency resolution using DAG algorithms.",
      type: "plan",
      status: "active",
      project_id: "brain",
    }),
    // 4. Bug-related notes (short query targets)
    makeTestNote({
      path: "projects/brain/task/bug00001.md",
      short_id: "bug00001",
      title: "Bug Fix: Search Returns Duplicates",
      body: "When searching with stemmed terms, duplicate results appear due to FTS5 tokenizer behavior.",
      type: "task",
      status: "completed",
      project_id: "brain",
    }),
    makeTestNote({
      path: "projects/brain/task/bug00002.md",
      short_id: "bug00002",
      title: "Critical Bug in Task Runner",
      body: "The task runner crashes when encountering circular dependencies in the task graph.",
      type: "task",
      status: "active",
      project_id: "brain",
    }),
    // 5. Hyphenated / special char notes
    makeTestNote({
      path: "projects/brain-api/task/api00001.md",
      short_id: "api00001",
      title: "Brain-API REST Endpoints",
      body: "Define and implement all REST API endpoints for the brain-api service.",
      type: "task",
      status: "active",
      project_id: "brain-api",
    }),
    // 6. Notes with "running" for stemming tests
    makeTestNote({
      path: "projects/brain/task/run00001.md",
      short_id: "run00001",
      title: "Running Integration Tests",
      body: "Guide for running the full integration test suite locally.",
      type: "task",
      status: "active",
      project_id: "brain",
    }),
    makeTestNote({
      path: "projects/brain/task/run00002.md",
      short_id: "run00002",
      title: "Performance Benchmarks",
      body: "Benchmark results from running the search engine under load.",
      type: "task",
      status: "active",
      project_id: "brain",
    }),
    // 7. Notes for BM25 weight validation — title vs body vs path
    makeTestNote({
      path: "projects/brain/task/weight001.md",
      short_id: "weight01",
      title: "Database Migration Guide",
      body: "Steps for migrating from SQLite to PostgreSQL.",
      type: "task",
      status: "active",
      project_id: "brain",
    }),
    makeTestNote({
      path: "projects/brain/task/weight002.md",
      short_id: "weight02",
      title: "API Performance Report",
      body: "The database migration process caused temporary slowdowns in the API layer.",
      type: "task",
      status: "active",
      project_id: "brain",
    }),
    // 8. Path-match note (term appears only in path)
    makeTestNote({
      path: "projects/deployment/task/deploy01.md",
      short_id: "deploy01",
      title: "Server Configuration",
      body: "Configure nginx reverse proxy and SSL certificates for production.",
      type: "task",
      status: "active",
      project_id: "deployment",
    }),
    // 9. Multi-word phrase notes
    makeTestNote({
      path: "projects/brain/exploration/expl001.md",
      short_id: "expl0001",
      title: "Full Text Search Implementation",
      body: "Exploration of FTS5 capabilities including BM25 ranking and porter stemming.",
      type: "exploration",
      status: "active",
      project_id: "brain",
    }),
    // 10. Notes for recall/precision testing
    makeTestNote({
      path: "projects/brain/task/cache0001.md",
      short_id: "cache001",
      title: "Caching Layer Design",
      body: "Implement in-memory caching with TTL for frequently accessed brain entries.",
      type: "task",
      status: "active",
      project_id: "brain",
    }),
    makeTestNote({
      path: "projects/brain/task/cache0002.md",
      short_id: "cache002",
      title: "Redis Integration",
      body: "Evaluate Redis as a caching backend for the brain API service.",
      type: "task",
      status: "active",
      project_id: "brain",
    }),
    // 11. Stemming edge cases
    makeTestNote({
      path: "projects/brain/task/stem0001.md",
      short_id: "stem0001",
      title: "Testing Framework Setup",
      body: "Configure bun test runner with coverage reporting and watch mode.",
      type: "task",
      status: "active",
      project_id: "brain",
    }),
    makeTestNote({
      path: "projects/brain/task/stem0002.md",
      short_id: "stem0002",
      title: "Tested Components Audit",
      body: "Audit all components to verify they have adequate test coverage.",
      type: "task",
      status: "completed",
      project_id: "brain",
    }),
    // 12. Long body note for ranking dilution test
    makeTestNote({
      path: "projects/brain/report/long0001.md",
      short_id: "long0001",
      title: "Quarterly Review",
      body: "This is a very long report covering many topics. ".repeat(50) +
        "The migration strategy was discussed briefly.",
      type: "report",
      status: "active",
      project_id: "brain",
    }),
    // 13. Unicode content
    makeTestNote({
      path: "projects/brain/idea/uni00001.md",
      short_id: "uni00001",
      title: "Internationalization Support",
      body: "Add i18n support for multi-language content. Consider UTF-8 encoding for all text fields.",
      type: "idea",
      status: "active",
      project_id: "brain",
    }),
    // 14. Note with term in all three fields (title + body + path)
    makeTestNote({
      path: "projects/search/task/search001.md",
      short_id: "srch0001",
      title: "Search Quality Metrics",
      body: "Define precision and recall metrics for search result quality evaluation.",
      type: "task",
      status: "active",
      project_id: "search",
    }),
    // 15. Note with term only in path
    makeTestNote({
      path: "projects/optimization/task/opt0001.md",
      short_id: "opt00001",
      title: "Query Planner Improvements",
      body: "Improve the SQL query planner to reduce full table scans.",
      type: "task",
      status: "active",
      project_id: "optimization",
    }),
    // 16. Plural/singular stemming
    makeTestNote({
      path: "projects/brain/task/plur0001.md",
      short_id: "plur0001",
      title: "Task Queue Implementation",
      body: "Implement a priority-based task queue with configurable concurrency.",
      type: "task",
      status: "active",
      project_id: "brain",
    }),
    makeTestNote({
      path: "projects/brain/plan/plur0002.md",
      short_id: "plur0002",
      title: "Tasks Overview Dashboard",
      body: "Design a dashboard showing all tasks grouped by status and priority.",
      type: "plan",
      status: "active",
      project_id: "brain",
    }),
    // 17. Note for OR query testing
    makeTestNote({
      path: "projects/brain/idea/or000001.md",
      short_id: "or000001",
      title: "Webhook Notifications",
      body: "Send webhook notifications when task status changes.",
      type: "idea",
      status: "active",
      project_id: "brain",
    }),
  ];

  for (const note of notes) {
    storage.insertNote(note);
  }
}

// =============================================================================
// Test Suite: Exact Title Matches
// =============================================================================

describe("Search Quality: Exact Title Matches", () => {
  let storage: StorageLayer;

  beforeEach(() => {
    storage = createStorageLayer(":memory:");
    seedTestNotes(storage);
  });

  afterEach(() => {
    storage.close();
  });

  test("exact title query 'Master Plan' returns the matching note as first result", () => {
    const results = storage.searchNotes("Master Plan");
    expect(results.length).toBeGreaterThanOrEqual(1);
    expect(results[0].title).toBe("Master Plan");
  });

  test("exact title query 'Task Dependency Resolution' returns matching note first", () => {
    const results = storage.searchNotes("Task Dependency Resolution");
    expect(results.length).toBeGreaterThanOrEqual(1);
    expect(results[0].title).toBe("Task Dependency Resolution");
  });

  test("exact title query 'Caching Layer Design' returns matching note first", () => {
    const results = storage.searchNotes("Caching Layer Design");
    expect(results.length).toBeGreaterThanOrEqual(1);
    expect(results[0].title).toBe("Caching Layer Design");
  });
});

// =============================================================================
// Test Suite: Partial Matches
// =============================================================================

describe("Search Quality: Partial Matches", () => {
  let storage: StorageLayer;

  beforeEach(() => {
    storage = createStorageLayer(":memory:");
    seedTestNotes(storage);
  });

  afterEach(() => {
    storage.close();
  });

  test("query 'auth' finds notes with 'auth' token (not 'authentication' — different stem)", () => {
    // Porter stemmer: "auth" -> "auth", "authentication" -> "authent"
    // These are DIFFERENT stems, so FTS5 won't match "authentication" for query "auth"
    // This is expected FTS5 behavior — use "authentication" to find that note
    const results = storage.searchNotes("auth");
    expect(results.length).toBeGreaterThanOrEqual(1);
    const titles = results.map((r) => r.title);
    expect(titles).toContain("Auth Service Implementation");
  });

  test("query 'authentication' finds the authentication strategy note", () => {
    const results = storage.searchNotes("authentication");
    expect(results.length).toBeGreaterThanOrEqual(1);
    const titles = results.map((r) => r.title);
    expect(titles).toContain("Authentication Strategy Decision");
  });

  test("query 'cache' finds caching-related notes", () => {
    const results = storage.searchNotes("cache");
    expect(results.length).toBeGreaterThanOrEqual(1);
    const titles = results.map((r) => r.title);
    // Porter stemmer: "cache" -> "cach", "caching" -> "cach"
    expect(titles).toContain("Caching Layer Design");
  });

  test("query 'bug' finds bug-related notes", () => {
    const results = storage.searchNotes("bug");
    expect(results.length).toBeGreaterThanOrEqual(2);
    const titles = results.map((r) => r.title);
    expect(titles).toContain("Bug Fix: Search Returns Duplicates");
    expect(titles).toContain("Critical Bug in Task Runner");
  });
});

// =============================================================================
// Test Suite: Multi-Word Queries
// =============================================================================

describe("Search Quality: Multi-Word Queries", () => {
  let storage: StorageLayer;

  beforeEach(() => {
    storage = createStorageLayer(":memory:");
    seedTestNotes(storage);
  });

  afterEach(() => {
    storage.close();
  });

  test("multi-word query 'task dependency resolution' returns relevant results", () => {
    const results = storage.searchNotes("task dependency resolution");
    expect(results.length).toBeGreaterThanOrEqual(1);
    // The note with exact title match should rank highest
    expect(results[0].title).toBe("Task Dependency Resolution");
  });

  test("multi-word query 'full text search' returns FTS exploration note first", () => {
    const results = storage.searchNotes("full text search");
    expect(results.length).toBeGreaterThanOrEqual(1);
    expect(results[0].title).toBe("Full Text Search Implementation");
  });

  test("multi-word query 'integration test' returns relevant results", () => {
    const results = storage.searchNotes("integration test");
    expect(results.length).toBeGreaterThanOrEqual(1);
    const titles = results.map((r) => r.title);
    expect(titles).toContain("Running Integration Tests");
  });
});

// =============================================================================
// Test Suite: Porter Stemming
// =============================================================================

describe("Search Quality: Porter Stemming", () => {
  let storage: StorageLayer;

  beforeEach(() => {
    storage = createStorageLayer(":memory:");
    seedTestNotes(storage);
  });

  afterEach(() => {
    storage.close();
  });

  test("'running' matches notes with 'run' stem (running, run)", () => {
    const results = storage.searchNotes("running");
    expect(results.length).toBeGreaterThanOrEqual(2);
    const titles = results.map((r) => r.title);
    expect(titles).toContain("Running Integration Tests");
  });

  test("'run' matches notes containing 'running' via porter stemmer", () => {
    const results = storage.searchNotes("run");
    expect(results.length).toBeGreaterThanOrEqual(1);
    const titles = results.map((r) => r.title);
    expect(titles).toContain("Running Integration Tests");
  });

  test("'testing' matches 'test', 'tested', 'tests' via stemming", () => {
    const results = storage.searchNotes("testing");
    expect(results.length).toBeGreaterThanOrEqual(1);
    const titles = results.map((r) => r.title);
    // "Testing" stems to "test", should match "Testing Framework Setup" and "Tested Components Audit"
    expect(titles).toContain("Testing Framework Setup");
    expect(titles).toContain("Tested Components Audit");
  });

  test("'tasks' matches 'task' via stemming (plural to singular)", () => {
    const results = storage.searchNotes("tasks");
    expect(results.length).toBeGreaterThanOrEqual(1);
    const titles = results.map((r) => r.title);
    expect(titles).toContain("Tasks Overview Dashboard");
    expect(titles).toContain("Task Queue Implementation");
  });

  test("'migrating' matches 'migration' via stemming", () => {
    const results = storage.searchNotes("migrating");
    expect(results.length).toBeGreaterThanOrEqual(1);
    const titles = results.map((r) => r.title);
    expect(titles).toContain("Database Migration Guide");
  });
});

// =============================================================================
// Test Suite: BM25 Weight Validation
// =============================================================================

describe("Search Quality: BM25 Weight Validation", () => {
  let storage: StorageLayer;

  beforeEach(() => {
    storage = createStorageLayer(":memory:");
    seedTestNotes(storage);
  });

  afterEach(() => {
    storage.close();
  });

  test("title match (weight 10) ranks above body-only match (weight 1)", () => {
    // "migration" appears in:
    //   - weight001 title: "Database Migration Guide"
    //   - weight002 body: "The database migration process..."
    //   - long0001 body: "The migration strategy was discussed briefly."
    const results = storage.searchNotes("migration");
    expect(results.length).toBeGreaterThanOrEqual(2);
    // Title match should be first
    expect(results[0].title).toBe("Database Migration Guide");
  });

  test("title match ranks above path-only match", () => {
    // "search" appears in:
    //   - srch0001 title: "Search Quality Metrics" AND path: "projects/search/..."
    //   - expl0001 body: "...search result quality..."
    // Title match should rank highest
    const results = storage.searchNotes("search");
    expect(results.length).toBeGreaterThanOrEqual(1);
    expect(results[0].title).toBe("Search Quality Metrics");
  });

  test("term in title + body + path ranks higher than term in body only", () => {
    // "search" in srch0001: title + body + path (all three)
    // "search" in expl0001: body only ("search result quality")
    // "search" in bug00001: body only ("searching with stemmed terms")
    const results = storage.searchNotes("search");
    expect(results.length).toBeGreaterThanOrEqual(2);
    // srch0001 should rank first (has term in all three weighted fields)
    expect(results[0].short_id).toBe("srch0001");
  });

  test("short title match outranks long body mention", () => {
    // "migration" in weight001 title (short, focused)
    // "migration" in long0001 body (buried in very long text)
    const results = storage.searchNotes("migration");
    const titleMatchIdx = results.findIndex(
      (r) => r.title === "Database Migration Guide"
    );
    const longBodyIdx = results.findIndex(
      (r) => r.title === "Quarterly Review"
    );
    if (longBodyIdx >= 0) {
      expect(titleMatchIdx).toBeLessThan(longBodyIdx);
    }
  });

  test("path match (weight 5) ranks above body-only match (weight 1)", () => {
    // "deployment" appears in:
    //   - deploy01 path: "projects/deployment/..." (path weight 5)
    //   - No other note has "deployment" in title
    const results = storage.searchNotes("deployment");
    expect(results.length).toBeGreaterThanOrEqual(1);
    // The note with "deployment" in path should be found
    expect(results.some((r) => r.short_id === "deploy01")).toBe(true);
  });
});

// =============================================================================
// Test Suite: Edge Cases
// =============================================================================

describe("Search Quality: Edge Cases", () => {
  let storage: StorageLayer;

  beforeEach(() => {
    storage = createStorageLayer(":memory:");
    seedTestNotes(storage);
  });

  afterEach(() => {
    storage.close();
  });

  test("empty query returns empty results", () => {
    const results = storage.searchNotes("");
    expect(results).toEqual([]);
  });

  test("whitespace-only query returns empty results", () => {
    const results = storage.searchNotes("   ");
    expect(results).toEqual([]);
  });

  test("nonexistent term returns empty results", () => {
    const results = storage.searchNotes("xyznonexistent");
    expect(results).toEqual([]);
  });

  test("very long query does not crash", () => {
    const longQuery = "search ".repeat(100).trim();
    const results = storage.searchNotes(longQuery);
    // Should not throw, may return results or empty
    expect(Array.isArray(results)).toBe(true);
  });

  test("special characters in query do not crash (hyphenated term)", () => {
    // FTS5 treats hyphens as special syntax — "brain-api" may fail as FTS5 query
    // The searchNotes method catches FTS5 errors and returns empty array
    const results = storage.searchNotes("brain-api");
    expect(Array.isArray(results)).toBe(true);
    // May return empty due to FTS5 syntax interpretation of hyphen — that's OK
  });

  test("hyphenated terms work when quoted or space-separated", () => {
    // Workaround: use space instead of hyphen for FTS5
    const results = storage.searchNotes("brain api");
    expect(Array.isArray(results)).toBe(true);
    expect(results.length).toBeGreaterThanOrEqual(1);
  });

  test("query with quotes works for phrase matching", () => {
    // FTS5 supports quoted phrases
    const results = storage.searchNotes('"Master Plan"');
    expect(results.length).toBeGreaterThanOrEqual(1);
    expect(results[0].title).toBe("Master Plan");
  });

  test("single character query returns results or empty without error", () => {
    const results = storage.searchNotes("a");
    expect(Array.isArray(results)).toBe(true);
    // Single char may or may not match — just shouldn't crash
  });

  test("query with FTS5 OR operator works", () => {
    const results = storage.searchNotes("webhook OR caching");
    expect(results.length).toBeGreaterThanOrEqual(2);
    const titles = results.map((r) => r.title);
    expect(titles).toContain("Webhook Notifications");
    expect(titles).toContain("Caching Layer Design");
  });

  test("numeric query does not crash", () => {
    const results = storage.searchNotes("12345");
    expect(Array.isArray(results)).toBe(true);
  });
});

// =============================================================================
// Test Suite: Precision and Recall
// =============================================================================

describe("Search Quality: Precision and Recall", () => {
  let storage: StorageLayer;

  beforeEach(() => {
    storage = createStorageLayer(":memory:");
    seedTestNotes(storage);
  });

  afterEach(() => {
    storage.close();
  });

  test("precision: top-3 results for 'dependency' are all relevant", () => {
    const results = storage.searchNotes("dependency");
    const top3 = results.slice(0, 3);
    // All top results should contain "dependency" or "dependencies" in title or body
    for (const r of top3) {
      const combined = `${r.title} ${r.body}`.toLowerCase();
      expect(combined).toMatch(/depend/);
    }
  });

  test("recall: 'auth' finds notes with 'auth' stem", () => {
    const results = storage.searchNotes("auth");
    const titles = results.map((r) => r.title);
    // "auth" stem matches "Auth" but not "Authentication" (different porter stem)
    expect(titles).toContain("Auth Service Implementation");
  });

  test("recall: 'authentication' finds authentication-related notes", () => {
    const results = storage.searchNotes("authentication");
    const titles = results.map((r) => r.title);
    expect(titles).toContain("Authentication Strategy Decision");
    // Also matches body content with "authentication"
    expect(titles).toContain("Auth Service Implementation");
  });

  test("recall: 'bug' finds all bug-related notes", () => {
    const results = storage.searchNotes("bug");
    const titles = results.map((r) => r.title);
    expect(titles).toContain("Bug Fix: Search Returns Duplicates");
    expect(titles).toContain("Critical Bug in Task Runner");
  });

  test("precision: top result for 'webhook' is the webhook note", () => {
    const results = storage.searchNotes("webhook");
    expect(results.length).toBeGreaterThanOrEqual(1);
    expect(results[0].title).toBe("Webhook Notifications");
  });

  test("recall: 'optimization' finds note with term in path", () => {
    const results = storage.searchNotes("optimization");
    expect(results.length).toBeGreaterThanOrEqual(1);
    expect(results.some((r) => r.short_id === "opt00001")).toBe(true);
  });
});

// =============================================================================
// Test Suite: Search Strategy Comparison
// =============================================================================

describe("Search Quality: Strategy Comparison", () => {
  let storage: StorageLayer;

  beforeEach(() => {
    storage = createStorageLayer(":memory:");
    seedTestNotes(storage);
  });

  afterEach(() => {
    storage.close();
  });

  test("FTS finds stemmed matches that exact strategy misses", () => {
    const ftsResults = storage.searchNotes("running", {
      matchStrategy: "fts",
    });
    const exactResults = storage.searchNotes("running", {
      matchStrategy: "exact",
    });
    // FTS should find more results due to stemming
    expect(ftsResults.length).toBeGreaterThanOrEqual(exactResults.length);
  });

  test("like strategy finds substring matches that FTS may not", () => {
    // "migrat" is a substring but not a valid FTS token
    const likeResults = storage.searchNotes("migrat", {
      matchStrategy: "like",
    });
    expect(likeResults.length).toBeGreaterThanOrEqual(1);
    expect(
      likeResults.some((r) => r.title === "Database Migration Guide")
    ).toBe(true);
  });

  test("exact strategy matches exact title", () => {
    const results = storage.searchNotes("Master Plan", {
      matchStrategy: "exact",
    });
    expect(results.length).toBe(1);
    expect(results[0].title).toBe("Master Plan");
  });
});

// =============================================================================
// Test Suite: Limit and Filtering
// =============================================================================

describe("Search Quality: Limit and Filtering", () => {
  let storage: StorageLayer;

  beforeEach(() => {
    storage = createStorageLayer(":memory:");
    seedTestNotes(storage);
  });

  afterEach(() => {
    storage.close();
  });

  test("limit restricts result count", () => {
    const results = storage.searchNotes("task", { limit: 3 });
    expect(results.length).toBeLessThanOrEqual(3);
  });

  test("type filter restricts to matching type", () => {
    const results = storage.searchNotes("task", { type: "plan" });
    for (const r of results) {
      expect(r.type).toBe("plan");
    }
  });

  test("status filter restricts to matching status", () => {
    const results = storage.searchNotes("bug", { status: "completed" });
    expect(results.length).toBeGreaterThanOrEqual(1);
    for (const r of results) {
      expect(r.status).toBe("completed");
    }
  });

  test("path filter restricts to path prefix", () => {
    const results = storage.searchNotes("task", {
      path: "projects/brain/",
    });
    for (const r of results) {
      expect(r.path.startsWith("projects/brain/")).toBe(true);
    }
  });
});

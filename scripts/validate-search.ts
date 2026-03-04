#!/usr/bin/env bun
/**
 * Search Quality Validation Script
 *
 * Standalone script that validates FTS5 search quality against a diverse
 * set of test notes. Reports pass/fail for each query, ranking quality,
 * and an overall quality score.
 *
 * Run: bun run scripts/validate-search.ts
 *
 * BM25 weights: title:10, body:1, path:5
 * Tokenizer: porter unicode61
 */

import { createStorageLayer, type StorageLayer, type NoteRow } from "../src/core/storage";

// =============================================================================
// Types
// =============================================================================

interface QueryTest {
  name: string;
  query: string;
  category: string;
  validate: (results: NoteRow[]) => { pass: boolean; detail: string };
}

interface TestResult {
  name: string;
  category: string;
  query: string;
  pass: boolean;
  detail: string;
  resultCount: number;
  topResults: string[];
}

// =============================================================================
// Test Note Factory
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
// Seed Data
// =============================================================================

function seedTestNotes(storage: StorageLayer): number {
  const notes: Omit<NoteRow, "id" | "indexed_at">[] = [
    makeTestNote({
      path: "projects/brain/plan/master01.md",
      short_id: "master01",
      title: "Master Plan",
      body: "The overarching strategy for the brain-api project including milestones and deliverables.",
      type: "plan",
      status: "active",
      project_id: "brain",
    }),
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
    makeTestNote({
      path: "projects/brain-api/task/api00001.md",
      short_id: "api00001",
      title: "Brain-API REST Endpoints",
      body: "Define and implement all REST API endpoints for the brain-api service.",
      type: "task",
      status: "active",
      project_id: "brain-api",
    }),
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
    makeTestNote({
      path: "projects/deployment/task/deploy01.md",
      short_id: "deploy01",
      title: "Server Configuration",
      body: "Configure nginx reverse proxy and SSL certificates for production.",
      type: "task",
      status: "active",
      project_id: "deployment",
    }),
    makeTestNote({
      path: "projects/brain/exploration/expl001.md",
      short_id: "expl0001",
      title: "Full Text Search Implementation",
      body: "Exploration of FTS5 capabilities including BM25 ranking and porter stemming.",
      type: "exploration",
      status: "active",
      project_id: "brain",
    }),
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
    makeTestNote({
      path: "projects/brain/idea/uni00001.md",
      short_id: "uni00001",
      title: "Internationalization Support",
      body: "Add i18n support for multi-language content. Consider UTF-8 encoding for all text fields.",
      type: "idea",
      status: "active",
      project_id: "brain",
    }),
    makeTestNote({
      path: "projects/search/task/search001.md",
      short_id: "srch0001",
      title: "Search Quality Metrics",
      body: "Define precision and recall metrics for search result quality evaluation.",
      type: "task",
      status: "active",
      project_id: "search",
    }),
    makeTestNote({
      path: "projects/optimization/task/opt0001.md",
      short_id: "opt00001",
      title: "Query Planner Improvements",
      body: "Improve the SQL query planner to reduce full table scans.",
      type: "task",
      status: "active",
      project_id: "optimization",
    }),
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
  return notes.length;
}

// =============================================================================
// Query Test Definitions
// =============================================================================

function defineQueryTests(): QueryTest[] {
  return [
    // --- Exact Title Matches ---
    {
      name: "Exact title: 'Master Plan'",
      query: "Master Plan",
      category: "exact-title",
      validate: (results) => {
        if (results.length === 0) return { pass: false, detail: "No results" };
        if (results[0].title === "Master Plan")
          return { pass: true, detail: "First result is exact title match" };
        return { pass: false, detail: `First result: "${results[0].title}"` };
      },
    },
    {
      name: "Exact title: 'Task Dependency Resolution'",
      query: "Task Dependency Resolution",
      category: "exact-title",
      validate: (results) => {
        if (results.length === 0) return { pass: false, detail: "No results" };
        if (results[0].title === "Task Dependency Resolution")
          return { pass: true, detail: "First result is exact title match" };
        return { pass: false, detail: `First result: "${results[0].title}"` };
      },
    },

    // --- Partial Matches ---
    {
      name: "Partial: 'auth' finds Auth Service",
      query: "auth",
      category: "partial",
      validate: (results) => {
        const found = results.some((r) => r.title === "Auth Service Implementation");
        return { pass: found, detail: found ? "Found Auth Service" : "Auth Service not found" };
      },
    },
    {
      name: "Partial: 'cache' finds Caching Layer",
      query: "cache",
      category: "partial",
      validate: (results) => {
        const found = results.some((r) => r.title === "Caching Layer Design");
        return { pass: found, detail: found ? "Found via stemming" : "Not found" };
      },
    },

    // --- Multi-Word Queries ---
    {
      name: "Multi-word: 'task dependency resolution'",
      query: "task dependency resolution",
      category: "multi-word",
      validate: (results) => {
        if (results.length === 0) return { pass: false, detail: "No results" };
        if (results[0].title === "Task Dependency Resolution")
          return { pass: true, detail: "Exact title match ranked first" };
        return { pass: false, detail: `First: "${results[0].title}"` };
      },
    },
    {
      name: "Multi-word: 'full text search'",
      query: "full text search",
      category: "multi-word",
      validate: (results) => {
        if (results.length === 0) return { pass: false, detail: "No results" };
        if (results[0].title === "Full Text Search Implementation")
          return { pass: true, detail: "FTS note ranked first" };
        return { pass: false, detail: `First: "${results[0].title}"` };
      },
    },

    // --- Short Queries ---
    {
      name: "Short: 'bug' finds bug notes",
      query: "bug",
      category: "short",
      validate: (results) => {
        const bugTitles = results.filter((r) => r.title.toLowerCase().includes("bug"));
        return {
          pass: bugTitles.length >= 2,
          detail: `Found ${bugTitles.length} bug-related notes`,
        };
      },
    },

    // --- Porter Stemming ---
    {
      name: "Stemming: 'running' matches 'Running Integration Tests'",
      query: "running",
      category: "stemming",
      validate: (results) => {
        const found = results.some((r) => r.title === "Running Integration Tests");
        return { pass: found, detail: found ? "Stemming works" : "Not found" };
      },
    },
    {
      name: "Stemming: 'run' matches 'Running' via porter",
      query: "run",
      category: "stemming",
      validate: (results) => {
        const found = results.some((r) => r.title === "Running Integration Tests");
        return { pass: found, detail: found ? "run->running via stemmer" : "Not found" };
      },
    },
    {
      name: "Stemming: 'tasks' matches 'Task' (plural->singular)",
      query: "tasks",
      category: "stemming",
      validate: (results) => {
        const found = results.some((r) => r.title.includes("Task"));
        return { pass: found, detail: found ? "Plural stemming works" : "Not found" };
      },
    },
    {
      name: "Stemming: 'migrating' matches 'migration'",
      query: "migrating",
      category: "stemming",
      validate: (results) => {
        const found = results.some((r) => r.title === "Database Migration Guide");
        return { pass: found, detail: found ? "Verb->noun stemming works" : "Not found" };
      },
    },

    // --- BM25 Ranking ---
    {
      name: "BM25: title match (10x) beats body match (1x) for 'migration'",
      query: "migration",
      category: "bm25",
      validate: (results) => {
        if (results.length < 2) return { pass: false, detail: "Need 2+ results" };
        if (results[0].title === "Database Migration Guide")
          return { pass: true, detail: "Title match ranked first" };
        return { pass: false, detail: `First: "${results[0].title}"` };
      },
    },
    {
      name: "BM25: title+body+path beats body-only for 'search'",
      query: "search",
      category: "bm25",
      validate: (results) => {
        if (results.length < 2) return { pass: false, detail: "Need 2+ results" };
        if (results[0].short_id === "srch0001")
          return { pass: true, detail: "All-fields match ranked first" };
        return { pass: false, detail: `First: "${results[0].title}" (${results[0].short_id})` };
      },
    },
    {
      name: "BM25: path match (5x) finds 'deployment' note",
      query: "deployment",
      category: "bm25",
      validate: (results) => {
        const found = results.some((r) => r.short_id === "deploy01");
        return { pass: found, detail: found ? "Path-match note found" : "Not found" };
      },
    },

    // --- Edge Cases ---
    {
      name: "Edge: empty query returns empty",
      query: "",
      category: "edge",
      validate: (results) => ({
        pass: results.length === 0,
        detail: results.length === 0 ? "Empty as expected" : `Got ${results.length} results`,
      }),
    },
    {
      name: "Edge: nonexistent term returns empty",
      query: "xyznonexistent",
      category: "edge",
      validate: (results) => ({
        pass: results.length === 0,
        detail: results.length === 0 ? "Empty as expected" : `Got ${results.length} results`,
      }),
    },
    {
      name: "Edge: very long query doesn't crash",
      query: "search ".repeat(100).trim(),
      category: "edge",
      validate: (results) => ({
        pass: Array.isArray(results),
        detail: `Returned ${results.length} results without crash`,
      }),
    },
    {
      name: "Edge: FTS5 OR operator works",
      query: "webhook OR caching",
      category: "edge",
      validate: (results) => {
        const hasWebhook = results.some((r) => r.title === "Webhook Notifications");
        const hasCaching = results.some((r) => r.title === "Caching Layer Design");
        return {
          pass: hasWebhook && hasCaching,
          detail: `webhook:${hasWebhook}, caching:${hasCaching}`,
        };
      },
    },
    {
      name: "Edge: quoted phrase match",
      query: '"Master Plan"',
      category: "edge",
      validate: (results) => {
        if (results.length === 0) return { pass: false, detail: "No results" };
        return {
          pass: results[0].title === "Master Plan",
          detail: results[0].title === "Master Plan" ? "Phrase match works" : `Got: "${results[0].title}"`,
        };
      },
    },

    // --- Precision ---
    {
      name: "Precision: top-3 for 'dependency' all contain 'depend'",
      query: "dependency",
      category: "precision",
      validate: (results) => {
        const top3 = results.slice(0, 3);
        const allRelevant = top3.every((r) =>
          `${r.title} ${r.body}`.toLowerCase().includes("depend")
        );
        return {
          pass: allRelevant,
          detail: allRelevant
            ? `All ${top3.length} top results relevant`
            : "Some top results irrelevant",
        };
      },
    },
    {
      name: "Precision: top result for 'webhook' is the webhook note",
      query: "webhook",
      category: "precision",
      validate: (results) => {
        if (results.length === 0) return { pass: false, detail: "No results" };
        return {
          pass: results[0].title === "Webhook Notifications",
          detail: results[0].title === "Webhook Notifications"
            ? "Correct top result"
            : `Got: "${results[0].title}"`,
        };
      },
    },
  ];
}

// =============================================================================
// Runner
// =============================================================================

function runValidation(): void {
  console.log("=".repeat(72));
  console.log("  FTS5 Search Quality Validation");
  console.log("  BM25 weights: title=10, body=1, path=5");
  console.log("  Tokenizer: porter unicode61");
  console.log("=".repeat(72));
  console.log();

  // Setup
  const storage = createStorageLayer(":memory:");
  const noteCount = seedTestNotes(storage);
  console.log(`Seeded ${noteCount} test notes into in-memory database.\n`);

  // Run tests
  const tests = defineQueryTests();
  const results: TestResult[] = [];

  for (const t of tests) {
    const searchResults = storage.searchNotes(t.query);
    const validation = t.validate(searchResults);
    results.push({
      name: t.name,
      category: t.category,
      query: t.query,
      pass: validation.pass,
      detail: validation.detail,
      resultCount: searchResults.length,
      topResults: searchResults.slice(0, 3).map((r) => r.title),
    });
  }

  // Report by category
  const categories = [...new Set(results.map((r) => r.category))];
  let totalPass = 0;
  let totalFail = 0;

  for (const cat of categories) {
    const catResults = results.filter((r) => r.category === cat);
    const catPass = catResults.filter((r) => r.pass).length;
    const catFail = catResults.filter((r) => !r.pass).length;

    console.log(`--- ${cat.toUpperCase()} (${catPass}/${catResults.length} pass) ---`);
    for (const r of catResults) {
      const icon = r.pass ? "✅" : "❌";
      console.log(`  ${icon} ${r.name}`);
      console.log(`     Query: "${r.query.length > 60 ? r.query.slice(0, 60) + "..." : r.query}"`);
      console.log(`     Results: ${r.resultCount} | ${r.detail}`);
      if (r.topResults.length > 0) {
        console.log(`     Top-3: ${r.topResults.map((t) => `"${t}"`).join(", ")}`);
      }
    }
    console.log();

    totalPass += catPass;
    totalFail += catFail;
  }

  // Summary
  const total = totalPass + totalFail;
  const score = total > 0 ? ((totalPass / total) * 100).toFixed(1) : "0.0";

  console.log("=".repeat(72));
  console.log(`  RESULTS: ${totalPass}/${total} pass (${score}% quality score)`);
  console.log(`  Pass: ${totalPass} | Fail: ${totalFail}`);
  console.log("=".repeat(72));

  // Cleanup
  storage.close();

  // Exit with error code if any failures
  if (totalFail > 0) {
    process.exit(1);
  }
}

// Run
runValidation();

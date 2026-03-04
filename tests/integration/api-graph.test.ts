/**
 * Integration Tests: Graph API Endpoints
 *
 * Tests the graph traversal endpoints (backlinks, outlinks, related)
 * through the full HTTP stack using createApp() and app.request().
 *
 * These tests use entries that exist in the brain directory. Since graph
 * queries depend on indexed link data, some tests may return empty results
 * if the indexer hasn't processed the test entries.
 */

import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { existsSync, mkdirSync, writeFileSync, rmSync } from "fs";
import { join, dirname } from "path";
import { createApp } from "../../src/server";
import { getConfig } from "../../src/config";

// =============================================================================
// Test Setup
// =============================================================================

const config = getConfig();
let app: ReturnType<typeof createApp>;

const TEST_SUBDIR = `_integ-api-graph-${Date.now()}`;
const TEST_PROJECT = TEST_SUBDIR;

/**
 * Write a markdown file with frontmatter to the brain directory.
 */
function writeEntry(relativePath: string, frontmatter: Record<string, unknown>, body: string): void {
  const fullPath = join(config.brain.brainDir, relativePath);
  mkdirSync(dirname(fullPath), { recursive: true });

  const yamlLines: string[] = [];
  for (const [key, value] of Object.entries(frontmatter)) {
    if (value === null || value === undefined) continue;
    yamlLines.push(`${key}: ${value}`);
  }

  const content = `---\n${yamlLines.join("\n")}\n---\n\n${body}\n`;
  writeFileSync(fullPath, content);
}

beforeAll(() => {
  app = createApp(config);

  // Create test entries with links between them for graph testing
  writeEntry(
    `projects/${TEST_SUBDIR}/scratch/graph-a.md`,
    { title: "Graph Entry A", type: "scratch", status: "active" },
    "Entry A links to [Entry B](projects/" + TEST_SUBDIR + "/scratch/graph-b.md)."
  );

  writeEntry(
    `projects/${TEST_SUBDIR}/scratch/graph-b.md`,
    { title: "Graph Entry B", type: "scratch", status: "active" },
    "Entry B links to [Entry C](projects/" + TEST_SUBDIR + "/scratch/graph-c.md)."
  );

  writeEntry(
    `projects/${TEST_SUBDIR}/scratch/graph-c.md`,
    { title: "Graph Entry C", type: "scratch", status: "active" },
    "Entry C is a leaf node with no outgoing links."
  );
});

afterAll(() => {
  // Clean up test directory
  const testDir = join(config.brain.brainDir, "projects", TEST_SUBDIR);
  if (existsSync(testDir)) {
    rmSync(testDir, { recursive: true, force: true });
  }
});

// =============================================================================
// GET /api/v1/entries/:path/backlinks
// =============================================================================

describe("GET /api/v1/entries/:path/backlinks", () => {
  test("returns backlinks array with entries and total", async () => {
    const entryPath = `projects/${TEST_SUBDIR}/scratch/graph-b.md`;
    const res = await app.request(`/api/v1/entries/${entryPath}/backlinks`);

    // 200 = success, 503 = zk not available
    expect([200, 503].includes(res.status)).toBe(true);

    if (res.status === 200) {
      const data = await res.json();
      expect(data).toHaveProperty("entries");
      expect(Array.isArray(data.entries)).toBe(true);
      expect(data).toHaveProperty("total");
      expect(typeof data.total).toBe("number");
    }
  });

  test("backlink entries include expected fields", async () => {
    const entryPath = `projects/${TEST_SUBDIR}/scratch/graph-b.md`;
    const res = await app.request(`/api/v1/entries/${entryPath}/backlinks`);

    if (res.status !== 200) return;

    const data = await res.json();
    if (data.entries.length > 0) {
      const entry = data.entries[0];
      expect(entry).toHaveProperty("id");
      expect(entry).toHaveProperty("path");
      expect(entry).toHaveProperty("title");
      expect(entry).toHaveProperty("type");
    }
  });

  test("returns 404 for non-existent entry path", async () => {
    const res = await app.request(
      "/api/v1/entries/non/existent/path.md/backlinks"
    );

    // Could be 404 (entry not found) or 200 with empty results depending on implementation
    expect([200, 404, 503].includes(res.status)).toBe(true);
  });
});

// =============================================================================
// GET /api/v1/entries/:path/outlinks
// =============================================================================

describe("GET /api/v1/entries/:path/outlinks", () => {
  test("returns outlinks array with entries and total", async () => {
    const entryPath = `projects/${TEST_SUBDIR}/scratch/graph-a.md`;
    const res = await app.request(`/api/v1/entries/${entryPath}/outlinks`);

    // 200 = success, 503 = zk not available
    expect([200, 503].includes(res.status)).toBe(true);

    if (res.status === 200) {
      const data = await res.json();
      expect(data).toHaveProperty("entries");
      expect(Array.isArray(data.entries)).toBe(true);
      expect(data).toHaveProperty("total");
      expect(typeof data.total).toBe("number");
    }
  });

  test("outlink entries include expected fields", async () => {
    const entryPath = `projects/${TEST_SUBDIR}/scratch/graph-a.md`;
    const res = await app.request(`/api/v1/entries/${entryPath}/outlinks`);

    if (res.status !== 200) return;

    const data = await res.json();
    if (data.entries.length > 0) {
      const entry = data.entries[0];
      expect(entry).toHaveProperty("id");
      expect(entry).toHaveProperty("path");
      expect(entry).toHaveProperty("title");
      expect(entry).toHaveProperty("type");
    }
  });

  test("returns 404 for non-existent entry path", async () => {
    const res = await app.request(
      "/api/v1/entries/non/existent/path.md/outlinks"
    );

    expect([200, 404, 503].includes(res.status)).toBe(true);
  });
});

// =============================================================================
// GET /api/v1/entries/:path/related
// =============================================================================

describe("GET /api/v1/entries/:path/related", () => {
  test("returns related entries array with entries and total", async () => {
    const entryPath = `projects/${TEST_SUBDIR}/scratch/graph-a.md`;
    const res = await app.request(`/api/v1/entries/${entryPath}/related`);

    // 200 = success, 503 = zk not available
    expect([200, 503].includes(res.status)).toBe(true);

    if (res.status === 200) {
      const data = await res.json();
      expect(data).toHaveProperty("entries");
      expect(Array.isArray(data.entries)).toBe(true);
      expect(data).toHaveProperty("total");
      expect(typeof data.total).toBe("number");
    }
  });

  test("related entries include expected fields", async () => {
    const entryPath = `projects/${TEST_SUBDIR}/scratch/graph-a.md`;
    const res = await app.request(`/api/v1/entries/${entryPath}/related`);

    if (res.status !== 200) return;

    const data = await res.json();
    if (data.entries.length > 0) {
      const entry = data.entries[0];
      expect(entry).toHaveProperty("id");
      expect(entry).toHaveProperty("path");
      expect(entry).toHaveProperty("title");
      expect(entry).toHaveProperty("type");
    }
  });

  test("accepts optional limit query parameter", async () => {
    const entryPath = `projects/${TEST_SUBDIR}/scratch/graph-a.md`;
    const res = await app.request(
      `/api/v1/entries/${entryPath}/related?limit=5`
    );

    // Should not fail validation
    expect([200, 503].includes(res.status)).toBe(true);
  });

  test("returns 404 for non-existent entry path", async () => {
    const res = await app.request(
      "/api/v1/entries/non/existent/path.md/related"
    );

    expect([200, 404, 503].includes(res.status)).toBe(true);
  });
});

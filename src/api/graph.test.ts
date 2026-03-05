/**
 * Graph Query API - Integration Tests
 *
 * Tests for the graph query REST API endpoints (backlinks, outlinks, related).
 *
 * NOTE: These tests validate HTTP request/response handling.
 * Graph operations require zk CLI to be available. Tests gracefully handle
 * the case where zk is not available (503 Service Unavailable).
 */

import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { existsSync, mkdirSync, rmSync, writeFileSync } from "fs";
import { join } from "path";
import { Hono } from "hono";
import { createGraphRoutes } from "./graph";
import { createEntriesRoutes } from "./entries";
import { getConfig } from "../config";

// =============================================================================
// Test Setup
// =============================================================================

const config = getConfig();
const TEST_SUBDIR = `_graph-test-${Date.now()}`;
const TEST_PATH_PREFIX = `projects/${TEST_SUBDIR}`;

// Create test app with both entry and graph routes
// NOTE: Graph routes must be registered FIRST because entries routes use /:id{.+}
// which would catch /id/backlinks etc.
const app = new Hono();
app.route("/entries", createGraphRoutes());
app.route("/entries", createEntriesRoutes());

// Track created files for cleanup
const createdPaths: string[] = [];

beforeAll(() => {
  // Create test subdirectory in the real brain dir
  const testDir = join(config.brain.brainDir, TEST_PATH_PREFIX, "scratch");
  if (!existsSync(testDir)) {
    mkdirSync(testDir, { recursive: true });
  }
});

afterAll(() => {
  // Clean up test directory
  const testDir = join(config.brain.brainDir, "projects", TEST_SUBDIR);
  if (existsSync(testDir)) {
    rmSync(testDir, { recursive: true, force: true });
  }
});

// Helper to create a test entry file
function createTestEntry(relativePath: string, content: string): void {
  const fullPath = join(config.brain.brainDir, relativePath);
  const dir = fullPath.substring(0, fullPath.lastIndexOf("/"));
  if (!existsSync(dir)) {
    mkdirSync(dir, { recursive: true });
  }
  writeFileSync(fullPath, content);
  createdPaths.push(relativePath);
}

// =============================================================================
// Tests
// =============================================================================

describe("Graph Query API", () => {
  beforeAll(() => {
    // Create test entries with links
    createTestEntry(
      `${TEST_PATH_PREFIX}/scratch/entry-a.md`,
      `---
title: Entry A
type: scratch
tags:
  - scratch
status: active
---

This is Entry A.

Links to [Entry B](entry-b) and [Entry C](entry-c).
`
    );

    createTestEntry(
      `${TEST_PATH_PREFIX}/scratch/entry-b.md`,
      `---
title: Entry B
type: scratch
tags:
  - scratch
status: active
---

This is Entry B.

Links back to [Entry A](entry-a).
`
    );

    createTestEntry(
      `${TEST_PATH_PREFIX}/scratch/entry-c.md`,
      `---
title: Entry C
type: scratch
tags:
  - scratch
status: active
---

This is Entry C.

Links to [Entry B](entry-b).
`
    );
  });

  describe("GET /entries/:id/backlinks - Get Backlinks", () => {
    test("should return backlinks or service unavailable", async () => {
      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/scratch/entry-b.md/backlinks`
      );

      // 200 (success with zk) or 503 (zk unavailable)
      expect([200, 503].includes(res.status)).toBe(true);

      const json = await res.json();
      if (res.status === 200) {
        expect(json).toHaveProperty("entries");
        expect(json).toHaveProperty("total");
        expect(Array.isArray(json.entries)).toBe(true);
      } else {
        expect(json.error).toBe("Service Unavailable");
      }
    });

    test("should return empty results for non-existent entry", async () => {
      const res = await app.request("/entries/non-existent-id/backlinks");

      // Returns 200 with empty results (graceful handling)
      // or 503 if zk CLI is unavailable
      expect([200, 503].includes(res.status)).toBe(true);

      if (res.status === 200) {
        const json = await res.json();
        expect(json.entries).toEqual([]);
        expect(json.total).toBe(0);
      }
    });

    test("should return entries array with correct structure", async () => {
      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/scratch/entry-a.md/backlinks`
      );

      if (res.status === 200) {
        const json = await res.json();
        expect(json.entries).toBeInstanceOf(Array);
        expect(typeof json.total).toBe("number");

        // If there are entries, check structure
        if (json.entries.length > 0) {
          const entry = json.entries[0];
          expect(entry).toHaveProperty("id");
          expect(entry).toHaveProperty("path");
          expect(entry).toHaveProperty("title");
          expect(entry).toHaveProperty("type");
        }
      }
    });
  });

  describe("GET /entries/:id/outlinks - Get Outlinks", () => {
    test("should return outlinks or service unavailable", async () => {
      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/scratch/entry-a.md/outlinks`
      );

      // 200 (success with zk) or 503 (zk unavailable)
      expect([200, 503].includes(res.status)).toBe(true);

      const json = await res.json();
      if (res.status === 200) {
        expect(json).toHaveProperty("entries");
        expect(json).toHaveProperty("total");
        expect(Array.isArray(json.entries)).toBe(true);
      } else {
        expect(json.error).toBe("Service Unavailable");
      }
    });

    test("should return empty results for non-existent entry", async () => {
      const res = await app.request("/entries/non-existent-id/outlinks");

      // Returns 200 with empty results (graceful handling)
      // or 503 if zk CLI is unavailable
      expect([200, 503].includes(res.status)).toBe(true);

      if (res.status === 200) {
        const json = await res.json();
        expect(json.entries).toEqual([]);
        expect(json.total).toBe(0);
      }
    });

    test("should return entries array with correct structure", async () => {
      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/scratch/entry-a.md/outlinks`
      );

      if (res.status === 200) {
        const json = await res.json();
        expect(json.entries).toBeInstanceOf(Array);
        expect(typeof json.total).toBe("number");

        // If there are entries, check structure
        if (json.entries.length > 0) {
          const entry = json.entries[0];
          expect(entry).toHaveProperty("id");
          expect(entry).toHaveProperty("path");
          expect(entry).toHaveProperty("title");
          expect(entry).toHaveProperty("type");
        }
      }
    });
  });

  describe("GET /entries/:id/related - Get Related Entries", () => {
    test("should return related entries or service unavailable", async () => {
      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/scratch/entry-a.md/related`
      );

      // 200 (success with zk) or 503 (zk unavailable)
      expect([200, 503].includes(res.status)).toBe(true);

      const json = await res.json();
      if (res.status === 200) {
        expect(json).toHaveProperty("entries");
        expect(json).toHaveProperty("total");
        expect(Array.isArray(json.entries)).toBe(true);
      } else {
        expect(json.error).toBe("Service Unavailable");
      }
    });

    test("should return empty results for non-existent entry", async () => {
      const res = await app.request("/entries/non-existent-id/related");

      // Returns 200 with empty results (graceful handling)
      // or 503 if zk CLI is unavailable
      expect([200, 503].includes(res.status)).toBe(true);

      if (res.status === 200) {
        const json = await res.json();
        expect(json.entries).toEqual([]);
        expect(json.total).toBe(0);
      }
    });

    test("should accept limit query parameter", async () => {
      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/scratch/entry-a.md/related?limit=5`
      );

      // 200 (success with zk) or 503 (zk unavailable)
      expect([200, 503].includes(res.status)).toBe(true);

      if (res.status === 200) {
        const json = await res.json();
        expect(json.entries.length).toBeLessThanOrEqual(5);
      }
    });

    test("should reject invalid limit parameter", async () => {
      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/scratch/entry-a.md/related?limit=abc`
      );

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
    });

    test("should reject negative limit parameter", async () => {
      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/scratch/entry-a.md/related?limit=-5`
      );

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
    });

    test("should return entries array with correct structure", async () => {
      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/scratch/entry-a.md/related`
      );

      if (res.status === 200) {
        const json = await res.json();
        expect(json.entries).toBeInstanceOf(Array);
        expect(typeof json.total).toBe("number");

        // If there are entries, check structure
        if (json.entries.length > 0) {
          const entry = json.entries[0];
          expect(entry).toHaveProperty("id");
          expect(entry).toHaveProperty("path");
          expect(entry).toHaveProperty("title");
          expect(entry).toHaveProperty("type");
        }
      }
    });
  });
});

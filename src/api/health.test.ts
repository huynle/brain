/**
 * Health and Stats API - Integration Tests
 *
 * Tests for the health, stats, orphans, stale, verify, and link endpoints.
 */

import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { existsSync, mkdirSync, rmSync, writeFileSync } from "fs";
import { join } from "path";
import { Hono } from "hono";
import { createHealthRoutes, getHealthStatus } from "./health";
import { getConfig } from "../config";

// =============================================================================
// Test Setup
// =============================================================================

const config = getConfig();
const TEST_SUBDIR = `_test-health-${Date.now()}`;
const TEST_PATH_PREFIX = `projects/${TEST_SUBDIR}`;

// Create test app with health routes
const app = new Hono();
app.route("/", createHealthRoutes());

beforeAll(() => {
  // Create test subdirectory
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
}

// =============================================================================
// Tests
// =============================================================================

describe("Health API", () => {
  describe("getHealthStatus()", () => {
    test("should return health status object", async () => {
      const health = await getHealthStatus();

      expect(health).toHaveProperty("status");
      expect(health).toHaveProperty("zkAvailable");
      expect(health).toHaveProperty("dbAvailable");
      expect(health).toHaveProperty("timestamp");
      expect(["healthy", "degraded", "unhealthy"]).toContain(health.status);
      expect(typeof health.zkAvailable).toBe("boolean");
      expect(typeof health.dbAvailable).toBe("boolean");
    });

    test("should return ISO timestamp", async () => {
      const health = await getHealthStatus();
      const date = new Date(health.timestamp);
      expect(date.toISOString()).toBe(health.timestamp);
    });
  });

  describe("GET /stats", () => {
    test("should return stats object", async () => {
      const res = await app.request("/stats");

      // May be 200 or 503 depending on zk availability
      if (res.status === 200) {
        const json = await res.json();
        expect(json).toHaveProperty("zkAvailable");
        expect(json).toHaveProperty("brainDir");
        expect(json).toHaveProperty("dbPath");
        expect(json).toHaveProperty("totalEntries");
        expect(json).toHaveProperty("byType");
      } else {
        expect(res.status).toBe(503);
      }
    });

    test("should accept global parameter", async () => {
      const res = await app.request("/stats?global=true");
      expect([200, 503]).toContain(res.status);
    });
  });

  describe("GET /orphans", () => {
    test("should return orphans array", async () => {
      const res = await app.request("/orphans");

      if (res.status === 200) {
        const json = await res.json();
        expect(json).toHaveProperty("entries");
        expect(json).toHaveProperty("total");
        expect(json).toHaveProperty("message");
        expect(Array.isArray(json.entries)).toBe(true);
      } else {
        expect(res.status).toBe(503);
      }
    });

    test("should accept type parameter", async () => {
      const res = await app.request("/orphans?type=scratch");
      expect([200, 503]).toContain(res.status);
    });

    test("should accept limit parameter", async () => {
      const res = await app.request("/orphans?limit=5");
      expect([200, 503]).toContain(res.status);
    });

    test("should reject invalid type", async () => {
      const res = await app.request("/orphans?type=invalid");
      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
    });

    test("should reject limit below 1", async () => {
      const res = await app.request("/orphans?limit=0");
      expect(res.status).toBe(400);
    });

    test("should reject limit above 100", async () => {
      const res = await app.request("/orphans?limit=101");
      expect(res.status).toBe(400);
    });
  });

  describe("GET /stale", () => {
    test("should return stale entries", async () => {
      const res = await app.request("/stale");

      if (res.status === 200) {
        const json = await res.json();
        expect(json).toHaveProperty("entries");
        expect(json).toHaveProperty("total");
        expect(Array.isArray(json.entries)).toBe(true);
      } else {
        expect(res.status).toBe(503);
      }
    });

    test("should accept days parameter", async () => {
      const res = await app.request("/stale?days=7");
      expect([200, 503]).toContain(res.status);
    });

    test("should accept type parameter", async () => {
      const res = await app.request("/stale?type=plan");
      expect([200, 503]).toContain(res.status);
    });

    test("should reject invalid days", async () => {
      const res = await app.request("/stale?days=0");
      expect(res.status).toBe(400);
    });

    test("should reject days above 365", async () => {
      const res = await app.request("/stale?days=400");
      expect(res.status).toBe(400);
    });
  });

  describe("POST /entries/:id/verify", () => {
    beforeAll(() => {
      createTestEntry(
        `${TEST_PATH_PREFIX}/scratch/verify-test.md`,
        `---
title: Verify Test
type: scratch
status: active
---

Content to verify.
`
      );
    });

    test("should verify an entry by path", async () => {
      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/scratch/verify-test.md/verify`,
        { method: "POST" }
      );

      expect(res.status).toBe(200);
      const json = await res.json();
      expect(json.message).toBe("Entry verified");
      expect(json).toHaveProperty("path");
      expect(json).toHaveProperty("verifiedAt");
    });

    test("should return 404 for non-existent entry", async () => {
      const res = await app.request("/entries/non/existent/path.md/verify", {
        method: "POST",
      });

      expect(res.status).toBe(404);
    });
  });

  describe("POST /link", () => {
    beforeAll(() => {
      createTestEntry(
        `${TEST_PATH_PREFIX}/scratch/link-test.md`,
        `---
title: Link Test Entry
type: scratch
status: active
---

Entry for link generation.
`
      );
    });

    test("should reject empty body", async () => {
      const res = await app.request("/link", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "{}",
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
    });

    test("should reject invalid JSON", async () => {
      const res = await app.request("/link", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "not-json",
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Bad Request");
    });

    test("should generate link for existing path", async () => {
      const res = await app.request("/link", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          path: `${TEST_PATH_PREFIX}/scratch/link-test.md`,
          withTitle: true,
        }),
      });

      // May succeed or fail depending on zk availability
      if (res.status === 200) {
        const json = await res.json();
        expect(json).toHaveProperty("link");
        expect(json).toHaveProperty("id");
        expect(json).toHaveProperty("path");
        expect(json).toHaveProperty("title");
        expect(json.link).toContain("Link Test Entry");
      } else {
        expect([404, 503]).toContain(res.status);
      }
    });

    test("should reject invalid title type", async () => {
      const res = await app.request("/link", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ title: 123 }),
      });

      expect(res.status).toBe(400);
    });

    test("should reject invalid path type", async () => {
      const res = await app.request("/link", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path: 123 }),
      });

      expect(res.status).toBe(400);
    });

    test("should reject invalid withTitle type", async () => {
      const res = await app.request("/link", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ path: "test.md", withTitle: "yes" }),
      });

      expect(res.status).toBe(400);
    });
  });
});

/**
 * Entry CRUD API - Integration Tests
 *
 * Tests for the entry REST API endpoints.
 *
 * NOTE: These tests validate HTTP request/response handling and validation logic.
 * Tests that require file system operations use the actual brain directory from config.
 * For full integration testing, run with a properly configured brain directory.
 */

import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { existsSync, mkdirSync, rmSync, writeFileSync } from "fs";
import { join } from "path";
import { Hono } from "hono";
import { createEntriesRoutes } from "./entries";
import { getConfig } from "../config";

// =============================================================================
// Test Setup
// =============================================================================

// Use actual config - tests will create files in the real brain dir
const config = getConfig();
const TEST_SUBDIR = `_test-${Date.now()}`;
const TEST_PATH_PREFIX = `projects/${TEST_SUBDIR}`;

// Create test app
const app = new Hono();
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
function createTestEntry(
  relativePath: string,
  content: string
): void {
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

describe("Entry CRUD API", () => {
  describe("POST /entries - Create Entry", () => {
    test("should reject empty body", async () => {
      const res = await app.request("/entries", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "{}",
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
    });

    test("should reject missing required fields", async () => {
      const res = await app.request("/entries", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          title: "Test",
          // missing type and content
        }),
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
      expect(json.details).toBeInstanceOf(Array);
    });

    test("should reject invalid type", async () => {
      const res = await app.request("/entries", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          type: "invalid-type",
          title: "Test",
          content: "Content",
        }),
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.details.some((d: { field: string }) => d.field === "type")).toBe(true);
    });

    test("should reject invalid status", async () => {
      const res = await app.request("/entries", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          type: "scratch",
          title: "Test",
          content: "Content",
          status: "invalid-status",
        }),
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.details.some((d: { field: string }) => d.field === "status")).toBe(true);
    });

    test("should reject invalid JSON", async () => {
      const res = await app.request("/entries", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "not-json",
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Bad Request");
    });

    test("should accept valid entry with all optional fields", async () => {
      // Note: This test will fail without a real zk setup, but validates the request parsing
      const validRequest = {
        type: "scratch",
        title: "Test Entry",
        content: "This is test content",
        tags: ["test", "api"],
        status: "active",
        priority: "medium",
        global: false,
        project: "test-project",
      };

      // Validation should pass even if save fails due to missing zk
      const res = await app.request("/entries", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(validRequest),
      });

      // Could be 201 (success) or 500 (zk not available) - either means validation passed
      expect([201, 500].includes(res.status)).toBe(true);
    });
  });

  describe("GET /entries/:id - Get Entry", () => {
    beforeAll(() => {
      // Create test entries
      createTestEntry(
        `${TEST_PATH_PREFIX}/scratch/get-test.md`,
        `---
title: Get Test Entry
type: scratch
tags:
  - scratch
  - test
status: active
---

This is test content for GET endpoint.
`
      );
    });

    test("should return entry by path", async () => {
      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/scratch/get-test.md`
      );

      expect(res.status).toBe(200);
      const json = await res.json();
      expect(json.title).toBe("Get Test Entry");
      expect(json.type).toBe("scratch");
      expect(json.status).toBe("active");
      expect(json.content).toContain("test content for GET");
    });

    test("should return 404 for non-existent entry", async () => {
      const res = await app.request("/entries/non/existent/path.md");

      expect(res.status).toBe(404);
      const json = await res.json();
      expect(json.error).toBe("Not Found");
    });
  });

  describe("GET /entries - List Entries", () => {
    test("should handle invalid type parameter", async () => {
      const res = await app.request("/entries?type=invalid-type");

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
    });

    test("should handle invalid status parameter", async () => {
      const res = await app.request("/entries?status=invalid-status");

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
    });

    test("should handle invalid sortBy parameter", async () => {
      const res = await app.request("/entries?sortBy=invalid");

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
    });

    test("should accept valid query parameters", async () => {
      // Will fail with 503 if zk is not available, but validates parameter parsing
      const res = await app.request(
        "/entries?type=scratch&status=active&limit=10&offset=0&sortBy=priority"
      );

      // 200 (success) or 503 (zk unavailable)
      expect([200, 503].includes(res.status)).toBe(true);
    });
  });

  describe("PATCH /entries/:id - Update Entry", () => {
    beforeAll(() => {
      // Create test entry for update
      createTestEntry(
        `${TEST_PATH_PREFIX}/scratch/update-test.md`,
        `---
title: Update Test Entry
type: scratch
tags:
  - scratch
status: active
---

Original content.
`
      );
    });

    test("should reject empty update body", async () => {
      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/scratch/update-test.md`,
        {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: "{}",
        }
      );

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
    });

    test("should reject invalid status in update", async () => {
      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/scratch/update-test.md`,
        {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ status: "invalid-status" }),
        }
      );

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.details.some((d: { field: string }) => d.field === "status")).toBe(true);
    });

    test("should update entry status", async () => {
      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/scratch/update-test.md`,
        {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ status: "completed" }),
        }
      );

      expect(res.status).toBe(200);
      const json = await res.json();
      expect(json.status).toBe("completed");
    });

    test("should append content to entry", async () => {
      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/scratch/update-test.md`,
        {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ append: "## New Section\n\nAppended content." }),
        }
      );

      expect(res.status).toBe(200);
      const json = await res.json();
      expect(json.content).toContain("New Section");
      expect(json.content).toContain("Appended content");
    });

    test("should return 404 for non-existent entry", async () => {
      const res = await app.request("/entries/non/existent/path.md", {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status: "completed" }),
      });

      expect(res.status).toBe(404);
    });
  });

  describe("DELETE /entries/:id - Delete Entry", () => {
    test("should require confirm parameter", async () => {
      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/scratch/some-entry.md`,
        { method: "DELETE" }
      );

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Confirmation Required");
    });

    test("should delete entry with confirm=true", async () => {
      // Create entry to delete
      createTestEntry(
        `${TEST_PATH_PREFIX}/scratch/delete-me.md`,
        `---
title: Delete Me
type: scratch
status: active
---

Content to delete.
`
      );

      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/scratch/delete-me.md?confirm=true`,
        { method: "DELETE" }
      );

      expect(res.status).toBe(200);
      const json = await res.json();
      expect(json.message).toBe("Entry deleted successfully");

      // Verify file is gone
      expect(
        existsSync(join(config.brain.brainDir, `${TEST_PATH_PREFIX}/scratch/delete-me.md`))
      ).toBe(false);
    });

    test("should return 404 for non-existent entry", async () => {
      const res = await app.request(
        "/entries/non/existent/path.md?confirm=true",
        { method: "DELETE" }
      );

      expect(res.status).toBe(404);
    });
  });

  describe("parent_id - Hierarchical Task Grouping", () => {
    test("should accept valid parent_id in create request", async () => {
      const validRequest = {
        type: "task",
        title: "Child Task",
        content: "This is a child task",
        parent_id: "abc12def",
      };

      const res = await app.request("/entries", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(validRequest),
      });

      // Could be 201 (success) or 500 (zk not available) - either means validation passed
      expect([201, 500].includes(res.status)).toBe(true);
    });

    test("should reject invalid parent_id format - too short", async () => {
      const res = await app.request("/entries", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          type: "task",
          title: "Test",
          content: "Content",
          parent_id: "abc123",  // Only 6 chars, should be 8
        }),
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.details.some((d: { field: string }) => d.field === "parent_id")).toBe(true);
    });

    test("should reject invalid parent_id format - invalid characters", async () => {
      const res = await app.request("/entries", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          type: "task",
          title: "Test",
          content: "Content",
          parent_id: "ABC12DEF",  // Uppercase not allowed
        }),
      });

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.details.some((d: { field: string }) => d.field === "parent_id")).toBe(true);
    });

    test("should reject invalid parent_id in list endpoint", async () => {
      const res = await app.request("/entries?parent_id=invalid");

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
      expect(json.message).toContain("parent_id");
    });

    test("should accept valid parent_id in list endpoint", async () => {
      // Will succeed with 200 if zk is available, or 503 if not
      const res = await app.request("/entries?parent_id=abc12def");

      expect([200, 503].includes(res.status)).toBe(true);
    });

    test("should return parent_id when retrieving entry with parent_id", async () => {
      // Create test entry with parent_id
      createTestEntry(
        `${TEST_PATH_PREFIX}/task/child-task.md`,
        `---
title: Child Task with Parent
type: task
tags:
  - task
status: pending
parent_id: abc12def
---

This is a child task under a parent.
`
      );

      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/task/child-task.md`
      );

      expect(res.status).toBe(200);
      const json = await res.json();
      expect(json.title).toBe("Child Task with Parent");
      expect(json.type).toBe("task");
      expect(json.parent_id).toBe("abc12def");
    });

    test("should not have parent_id when entry has no parent", async () => {
      // Create test entry without parent_id
      createTestEntry(
        `${TEST_PATH_PREFIX}/task/orphan-task.md`,
        `---
title: Orphan Task
type: task
tags:
  - task
status: pending
---

This is a task without a parent.
`
      );

      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/task/orphan-task.md`
      );

      expect(res.status).toBe(200);
      const json = await res.json();
      expect(json.title).toBe("Orphan Task");
      expect(json.parent_id).toBeUndefined();
    });
  });
});

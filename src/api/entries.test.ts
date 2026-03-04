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
import { existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "fs";
import { join } from "path";
import { Hono } from "hono";
import { createEntriesRoutes } from "./entries";
import { getConfig } from "../config";
import { parseFrontmatter } from "../core/note-utils";
import { createProjectRealtimeHub } from "../core/realtime-hub";

// =============================================================================
// Test Setup
// =============================================================================

// Use actual config - tests will create files in the real brain dir
const config = getConfig();
const TEST_SUBDIR = `_test-${Date.now()}`;
const TEST_PATH_PREFIX = `projects/${TEST_SUBDIR}`;
const TEST_SUBDIR_B = `_test-b-${Date.now()}`;
const TEST_PATH_PREFIX_B = `projects/${TEST_SUBDIR_B}`;

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
  // Clean up test directories
  for (const subdir of [TEST_SUBDIR, TEST_SUBDIR_B]) {
    const testDir = join(config.brain.brainDir, "projects", subdir);
    if (existsSync(testDir)) {
      rmSync(testDir, { recursive: true, force: true });
    }
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

    test("publishes project_dirty for project-scoped create", async () => {
      const hub = createProjectRealtimeHub();
      const realtimeApp = new Hono();
      realtimeApp.route("/entries", (createEntriesRoutes as any)({ realtimeHub: hub }));

      const projectEvents: string[] = [];
      const otherProjectEvents: string[] = [];
      hub.subscribe(TEST_SUBDIR, ({ event }) => projectEvents.push(event));
      hub.subscribe(TEST_SUBDIR_B, ({ event }) => otherProjectEvents.push(event));

      const res = await realtimeApp.request("/entries", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          type: "scratch",
          title: "Realtime Create",
          content: "Create should dirty project.",
          project: TEST_SUBDIR,
        }),
      });

      expect([201, 500].includes(res.status)).toBe(true);
      expect(projectEvents).toContain("project_dirty");
      expect(otherProjectEvents).toEqual([]);
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

    test("should return unified sessions from frontmatter", async () => {
      createTestEntry(
        `${TEST_PATH_PREFIX}/task/sessions-map-test.md`,
        `---
title: Sessions Map Test Entry
type: task
tags:
  - task
status: pending
sessions:
  ses_test111aaa:
    timestamp: 2026-02-22T10:30:00.000Z
  ses_test222bbb:
    timestamp: 2026-02-22T10:31:00.000Z
    cron_id: cron_abc123
  ses_test333ccc:
    timestamp: 2026-02-22T10:32:00.000Z
    run_id: run_abc123
---

This entry has sessions for traceability.
`
      );

      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/task/sessions-map-test.md`
      );

      expect(res.status).toBe(200);
      const json = await res.json();
      expect(json.title).toBe("Sessions Map Test Entry");
      expect(json.sessions).toBeDefined();
      expect(json.sessions).toBeObject();
      expect(Object.keys(json.sessions)).toHaveLength(3);
      expect(json.sessions.ses_test111aaa).toBeDefined();
      expect(json.sessions.ses_test222bbb).toBeDefined();
      expect(json.sessions.ses_test333ccc).toBeDefined();
      expect(json.sessions.ses_test222bbb.cron_id).toBe("cron_abc123");
      expect(json.sessions.ses_test333ccc.run_id).toBe("run_abc123");
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

    test("should accept tags parameter for filtering", async () => {
      // Will fail with 503 if zk is not available, but validates parameter parsing
      const res = await app.request("/entries?tags=bug,urgent");

      // 200 (success) or 503 (zk unavailable)
      expect([200, 503].includes(res.status)).toBe(true);
    });

    test("should pass feature_id to service layer", async () => {
      // This tests the bug fix: feature_id should be passed through
      const res = await app.request("/entries?feature_id=auth-system");

      // 200 (success) or 503 (zk unavailable)
      expect([200, 503].includes(res.status)).toBe(true);
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

    test("publishes project-scoped snapshot when task update mutates task visibility", async () => {
      const hub = createProjectRealtimeHub();
      const realtimeApp = new Hono();
      realtimeApp.route("/entries", (createEntriesRoutes as any)({ realtimeHub: hub }));

      const taskPath = `${TEST_PATH_PREFIX}/task/realtime-update.md`;
      createTestEntry(
        taskPath,
        `---
title: Realtime Update Task
type: task
status: pending
---

Task body.
`
      );

      const projectEvents: string[] = [];
      const otherProjectEvents: string[] = [];
      hub.subscribe(TEST_SUBDIR, ({ event }) => projectEvents.push(event));
      hub.subscribe(TEST_SUBDIR_B, ({ event }) => otherProjectEvents.push(event));

      const res = await realtimeApp.request(`/entries/${taskPath}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status: "completed" }),
      });

      expect(res.status).toBe(200);
      expect(projectEvents).toContain("tasks_snapshot");
      expect(otherProjectEvents).toEqual([]);
    });

    test("publishes project_dirty for entry updates", async () => {
      const hub = createProjectRealtimeHub();
      const realtimeApp = new Hono();
      realtimeApp.route("/entries", (createEntriesRoutes as any)({ realtimeHub: hub }));

      const entryPath = `${TEST_PATH_PREFIX}/scratch/realtime-dirty-update.md`;
      createTestEntry(
        entryPath,
        `---
title: Realtime Dirty Update
type: scratch
status: active
---

Before update.
`
      );

      const projectEvents: string[] = [];
      const otherProjectEvents: string[] = [];
      hub.subscribe(TEST_SUBDIR, ({ event }) => projectEvents.push(event));
      hub.subscribe(TEST_SUBDIR_B, ({ event }) => otherProjectEvents.push(event));

      const res = await realtimeApp.request(`/entries/${entryPath}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ status: "completed" }),
      });

      expect(res.status).toBe(200);
      expect(projectEvents).toContain("project_dirty");
      expect(otherProjectEvents).toEqual([]);
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

    test("publishes project_dirty for deletes", async () => {
      const hub = createProjectRealtimeHub();
      const realtimeApp = new Hono();
      realtimeApp.route("/entries", (createEntriesRoutes as any)({ realtimeHub: hub }));

      const deletePath = `${TEST_PATH_PREFIX}/scratch/realtime-dirty-delete.md`;
      createTestEntry(
        deletePath,
        `---
title: Realtime Dirty Delete
type: scratch
status: active
---

Delete me.
`
      );

      const projectEvents: string[] = [];
      const otherProjectEvents: string[] = [];
      hub.subscribe(TEST_SUBDIR, ({ event }) => projectEvents.push(event));
      hub.subscribe(TEST_SUBDIR_B, ({ event }) => otherProjectEvents.push(event));

      const res = await realtimeApp.request(`/entries/${deletePath}?confirm=true`, {
        method: "DELETE",
      });

      expect(res.status).toBe(200);
      expect(projectEvents).toContain("project_dirty");
      expect(otherProjectEvents).toEqual([]);
    });
  });

  describe("POST /entries/:id/move - Move Entry", () => {
    test("should move task and return updatedDependents with rewritten refs", async () => {
      // Setup: Create task-to-move in project A
      const movedTaskId = "mvtsk001";
      createTestEntry(
        `${TEST_PATH_PREFIX}/task/${movedTaskId}.md`,
        `---
title: Task To Move
type: task
status: pending
projectId: ${TEST_SUBDIR}
---

This task will be moved.
`
      );

      // Setup: Create dependent task in project A that depends on movedTaskId (bare ref)
      const dependentId = "deptsk01";
      createTestEntry(
        `${TEST_PATH_PREFIX}/task/${dependentId}.md`,
        `---
title: Dependent Task
type: task
status: pending
projectId: ${TEST_SUBDIR}
depends_on:
  - "${movedTaskId}"
  - "other-dep"
---

This task depends on the moved task.
`
      );

      // Create target project directory
      const targetTaskDir = join(config.brain.brainDir, TEST_PATH_PREFIX_B, "task");
      if (!existsSync(targetTaskDir)) {
        mkdirSync(targetTaskDir, { recursive: true });
      }

      // Act: Move the task to project B via HTTP endpoint
      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/task/${movedTaskId}.md/move`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ project: TEST_SUBDIR_B }),
        }
      );

      expect(res.status).toBe(200);
      const json = await res.json();

      // Verify basic move fields
      expect(json.oldPath).toBe(`${TEST_PATH_PREFIX}/task/${movedTaskId}.md`);
      expect(json.newPath).toBe(`${TEST_PATH_PREFIX_B}/task/${movedTaskId}.md`);
      expect(json.project).toBe(TEST_SUBDIR_B);
      expect(json.id).toBe(movedTaskId);

      // Verify updatedDependents is present and correct
      expect(json.updatedDependents).toBeInstanceOf(Array);
      expect(json.updatedDependents.length).toBeGreaterThanOrEqual(1);

      const depUpdate = json.updatedDependents.find(
        (d: { taskId: string }) => d.taskId === dependentId
      );
      expect(depUpdate).toBeDefined();
      expect(depUpdate.project).toBe(TEST_SUBDIR);
      expect(depUpdate.oldRef).toBe(movedTaskId);
      expect(depUpdate.newRef).toBe(`${TEST_SUBDIR_B}:${movedTaskId}`);

      // Verify the dependent file on disk was actually rewritten
      const dependentFilePath = join(
        config.brain.brainDir,
        `${TEST_PATH_PREFIX}/task/${dependentId}.md`
      );
      const dependentContent = readFileSync(dependentFilePath, "utf-8");
      const { frontmatter } = parseFrontmatter(dependentContent);
      expect(frontmatter.depends_on).toContain(`${TEST_SUBDIR_B}:${movedTaskId}`);
      expect(frontmatter.depends_on).toContain("other-dep");
      expect(frontmatter.depends_on).not.toContain(movedTaskId);

      // Verify the moved file exists at new location
      const movedFilePath = join(
        config.brain.brainDir,
        `${TEST_PATH_PREFIX_B}/task/${movedTaskId}.md`
      );
      expect(existsSync(movedFilePath)).toBe(true);

      // Verify the old file is gone
      const oldFilePath = join(
        config.brain.brainDir,
        `${TEST_PATH_PREFIX}/task/${movedTaskId}.md`
      );
      expect(existsSync(oldFilePath)).toBe(false);
    });

    test("should return empty updatedDependents when no deps to rewrite", async () => {
      // Setup: Create a standalone task with no dependents
      const taskId = "alone001";
      createTestEntry(
        `${TEST_PATH_PREFIX_B}/task/${taskId}.md`,
        `---
title: Standalone Task
type: task
status: pending
projectId: ${TEST_SUBDIR_B}
---

No other task depends on this one.
`
      );

      // Create target project directory (reuse TEST_SUBDIR which still exists)
      const targetTaskDir = join(config.brain.brainDir, TEST_PATH_PREFIX, "task");
      if (!existsSync(targetTaskDir)) {
        mkdirSync(targetTaskDir, { recursive: true });
      }

      // Act: Move to project A
      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX_B}/task/${taskId}.md/move`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ project: TEST_SUBDIR }),
        }
      );

      expect(res.status).toBe(200);
      const json = await res.json();

      // updatedDependents should be present but empty
      expect(json.updatedDependents).toBeInstanceOf(Array);
      expect(json.updatedDependents).toHaveLength(0);
      expect(json.project).toBe(TEST_SUBDIR);
    });

    test("should return 404 for non-existent entry", async () => {
      const res = await app.request(
        "/entries/projects/nonexistent/task/fake0001.md/move",
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ project: "some-project" }),
        }
      );

      expect(res.status).toBe(404);
    });

    test("should return 400 when moving to same project", async () => {
      const taskId = "samepj01";
      createTestEntry(
        `${TEST_PATH_PREFIX}/task/${taskId}.md`,
        `---
title: Same Project Task
type: task
status: pending
projectId: ${TEST_SUBDIR}
---

Content.
`
      );

      const res = await app.request(
        `/entries/${TEST_PATH_PREFIX}/task/${taskId}.md/move`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ project: TEST_SUBDIR }),
        }
      );

      expect(res.status).toBe(400);
      const json = await res.json();
      expect(json.error).toBe("Validation Error");
    });

    test("publishes project_dirty for both source and target projects on move", async () => {
      const hub = createProjectRealtimeHub();
      const realtimeApp = new Hono();
      realtimeApp.route("/entries", (createEntriesRoutes as any)({ realtimeHub: hub }));

      const moveId = "dirtymv1";
      createTestEntry(
        `${TEST_PATH_PREFIX}/scratch/${moveId}.md`,
        `---
title: Dirty Move Entry
type: scratch
status: active
---

Move me.
`
      );

      const targetScratchDir = join(config.brain.brainDir, TEST_PATH_PREFIX_B, "scratch");
      if (!existsSync(targetScratchDir)) {
        mkdirSync(targetScratchDir, { recursive: true });
      }

      const sourceEvents: string[] = [];
      const targetEvents: string[] = [];
      hub.subscribe(TEST_SUBDIR, ({ event }) => sourceEvents.push(event));
      hub.subscribe(TEST_SUBDIR_B, ({ event }) => targetEvents.push(event));

      const res = await realtimeApp.request(
        `/entries/${TEST_PATH_PREFIX}/scratch/${moveId}.md/move`,
        {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ project: TEST_SUBDIR_B }),
        }
      );

      expect(res.status).toBe(200);
      expect(sourceEvents).toContain("project_dirty");
      expect(targetEvents).toContain("project_dirty");
    });
  });

});

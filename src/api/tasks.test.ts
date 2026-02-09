/**
 * Task API Routes - Integration Tests
 *
 * Tests for the task REST API endpoints, particularly the POST /tasks/:projectId/status
 * endpoint for multi-task status checking with optional long-polling.
 *
 * NOTE: Tests that require actual task data use the real brain directory.
 * For full integration testing, run with a properly configured brain directory with tasks.
 */

import { describe, test, expect, beforeAll, afterAll } from "bun:test";
import { existsSync, mkdirSync, rmSync, writeFileSync } from "fs";
import { join } from "path";
import { Hono } from "hono";
import { createTaskRoutes } from "./tasks";
import { getConfig } from "../config";

// =============================================================================
// Test Setup
// =============================================================================

const config = getConfig();
const TEST_PROJECT = `_test-tasks-${Date.now()}`;
const TEST_TASK_DIR = `projects/${TEST_PROJECT}/task`;

// Create test app
const app = new Hono();
app.route("/tasks", createTaskRoutes());

// Track created files for cleanup
const createdPaths: string[] = [];

beforeAll(() => {
  // Create test task directory in the real brain dir
  const taskDir = join(config.brain.brainDir, TEST_TASK_DIR);
  if (!existsSync(taskDir)) {
    mkdirSync(taskDir, { recursive: true });
  }
});

afterAll(() => {
  // Clean up test project directory
  const projectDir = join(config.brain.brainDir, "projects", TEST_PROJECT);
  if (existsSync(projectDir)) {
    rmSync(projectDir, { recursive: true, force: true });
  }
});

// Helper to create a test task file
function createTestTask(
  taskId: string,
  content: {
    title: string;
    status: string;
    priority?: string;
    depends_on?: string[];
  }
): string {
  const relativePath = `${TEST_TASK_DIR}/${taskId}.md`;
  const fullPath = join(config.brain.brainDir, relativePath);

  const frontmatter = [
    "---",
    `title: ${content.title}`,
    "type: task",
    `status: ${content.status}`,
    content.priority ? `priority: ${content.priority}` : "priority: medium",
    "tags:",
    "  - task",
  ];

  if (content.depends_on && content.depends_on.length > 0) {
    frontmatter.push("depends_on:");
    for (const dep of content.depends_on) {
      frontmatter.push(`  - ${dep}`);
    }
  }

  frontmatter.push("---", "", `# ${content.title}`, "", "Task content here.");

  writeFileSync(fullPath, frontmatter.join("\n"));
  createdPaths.push(relativePath);
  return taskId;
}

// =============================================================================
// Tests: POST /:projectId/status - Task Status Endpoint
// =============================================================================

describe("Task API", () => {
  describe("POST /:projectId/status - Task Status Check", () => {
    // =========================================================================
    // Validation Tests (No external dependencies)
    // =========================================================================

    describe("Request Validation", () => {
      test("should return 400 for empty taskIds array", async () => {
        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            taskIds: [],
          }),
        });

        expect(res.status).toBe(400);
        const json = await res.json();
        expect(json.error).toBe("Validation Error");
      });

      test("should return 400 for missing taskIds", async () => {
        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({}),
        });

        expect(res.status).toBe(400);
        const json = await res.json();
        expect(json.error).toBe("Validation Error");
      });

      test("should return 400 for invalid waitFor value", async () => {
        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            taskIds: ["task1"],
            waitFor: "invalid",
          }),
        });

        expect(res.status).toBe(400);
        const json = await res.json();
        expect(json.error).toBe("Validation Error");
        expect(json.details.some((d: { field: string }) => d.field === "waitFor")).toBe(true);
      });

      test("should return 400 for invalid task ID format", async () => {
        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            taskIds: ["valid-id", "invalid id with spaces"],
          }),
        });

        expect(res.status).toBe(400);
        const json = await res.json();
        expect(json.error).toBe("Validation Error");
      });

      test("should return 400 for timeout exceeding max", async () => {
        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            taskIds: ["task1"],
            timeout: 999999999, // Exceeds max of 300000
          }),
        });

        expect(res.status).toBe(400);
        const json = await res.json();
        expect(json.error).toBe("Validation Error");
      });

      test("should return 400 for negative timeout", async () => {
        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            taskIds: ["task1"],
            timeout: -1000,
          }),
        });

        expect(res.status).toBe(400);
        const json = await res.json();
        expect(json.error).toBe("Validation Error");
      });

      test("should accept valid request with taskIds only", async () => {
        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            taskIds: ["abc12def"],
          }),
        });

        // Should pass validation - could be 200 (success) or 503 (zk unavailable)
        expect([200, 503].includes(res.status)).toBe(true);
      });

      test("should accept valid waitFor: completed", async () => {
        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            taskIds: ["abc12def"],
            waitFor: "completed",
            timeout: 1000,
          }),
        });

        // Should pass validation
        expect([200, 503].includes(res.status)).toBe(true);
      });

      test("should accept valid waitFor: any", async () => {
        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            taskIds: ["abc12def"],
            waitFor: "any",
            timeout: 1000,
          }),
        });

        // Should pass validation
        expect([200, 503].includes(res.status)).toBe(true);
      });
    });

    // =========================================================================
    // Response Structure Tests
    // =========================================================================

    describe("Response Structure", () => {
      test("should return proper structure for immediate check", async () => {
        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            taskIds: ["nonexistent-task"],
          }),
        });

        if (res.status === 503) {
          // zk not available - skip this test
          return;
        }

        expect(res.status).toBe(200);
        const json = await res.json();

        // Verify response structure
        expect(json).toHaveProperty("tasks");
        expect(json).toHaveProperty("notFound");
        expect(json).toHaveProperty("changed");
        expect(json).toHaveProperty("timedOut");
        expect(Array.isArray(json.tasks)).toBe(true);
        expect(Array.isArray(json.notFound)).toBe(true);
        expect(typeof json.changed).toBe("boolean");
        expect(typeof json.timedOut).toBe("boolean");
      });

      test("should return notFound for missing task IDs (immediate check)", async () => {
        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            taskIds: ["definitely-not-exist-abc123"],
          }),
        });

        if (res.status === 503) {
          return; // zk not available
        }

        expect(res.status).toBe(200);
        const json = await res.json();
        expect(json.notFound).toContain("definitely-not-exist-abc123");
        expect(json.tasks).toHaveLength(0);
        expect(json.timedOut).toBe(false);
        expect(json.changed).toBe(false);
      });

      test("should not mark as changed on immediate check", async () => {
        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            taskIds: ["task1", "task2"],
          }),
        });

        if (res.status === 503) {
          return; // zk not available
        }

        expect(res.status).toBe(200);
        const json = await res.json();
        expect(json.changed).toBe(false);
        expect(json.timedOut).toBe(false);
      });
    });

    // =========================================================================
    // Timeout Behavior Tests
    // =========================================================================

    describe("Timeout Behavior", () => {
      test("should return immediately when no tasks found (vacuous truth for completed)", async () => {
        // When tasks don't exist, tasks array is empty
        // Array.every() on empty array returns true (vacuous truth)
        // So waitFor: completed is immediately satisfied
        const startTime = Date.now();

        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            taskIds: ["nonexistent-pending-task"],
            waitFor: "completed",
            timeout: 30000,
          }),
        });

        const elapsed = Date.now() - startTime;

        if (res.status === 503) {
          return; // zk not available
        }

        expect(res.status).toBe(200);
        const json = await res.json();

        // Should return immediately - no tasks to wait for
        expect(json.timedOut).toBe(false);
        expect(json.changed).toBe(true); // Condition is met (vacuously)
        expect(json.notFound).toContain("nonexistent-pending-task");
        expect(elapsed).toBeLessThan(5000); // Should be fast
      }, 10000);

      test("should return notFound for missing tasks with waitFor", async () => {
        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            taskIds: ["definitely-missing-abc"],
            waitFor: "any",
            timeout: 1000,
          }),
        });

        if (res.status === 503) {
          return;
        }

        expect(res.status).toBe(200);
        const json = await res.json();
        expect(json.notFound).toContain("definitely-missing-abc");
      }, 10000);
    });

    // =========================================================================
    // Integration Tests with Real Tasks
    // =========================================================================

    describe("With Real Tasks", () => {
      beforeAll(async () => {
        // Create test tasks with various statuses
        createTestTask("task-completed-1", {
          title: "Completed Task 1",
          status: "completed",
          priority: "high",
        });

        createTestTask("task-pending-1", {
          title: "Pending Task 1",
          status: "pending",
          priority: "medium",
        });

        createTestTask("task-in-progress-1", {
          title: "In Progress Task 1",
          status: "in_progress",
          priority: "high",
        });

        createTestTask("task-validated-1", {
          title: "Validated Task 1",
          status: "validated",
          priority: "low",
        });

        // Re-index zk to pick up new files
        const { execSync } = await import("child_process");
        try {
          execSync("zk index --quiet", {
            cwd: config.brain.brainDir,
            timeout: 10000,
          });
        } catch {
          // zk might not be available
        }
      });

      test("should return current status for existing tasks", async () => {
        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            taskIds: ["task-completed-1", "task-pending-1"],
          }),
        });

        if (res.status === 503) {
          return; // zk not available
        }

        expect(res.status).toBe(200);
        const json = await res.json();

        expect(json.notFound).toHaveLength(0);
        expect(json.tasks).toHaveLength(2);
        expect(json.changed).toBe(false);
        expect(json.timedOut).toBe(false);

        // Verify task data
        const completedTask = json.tasks.find((t: { id: string }) => t.id === "task-completed-1");
        const pendingTask = json.tasks.find((t: { id: string }) => t.id === "task-pending-1");

        expect(completedTask).toBeDefined();
        expect(completedTask.status).toBe("completed");
        expect(completedTask.title).toBe("Completed Task 1");

        expect(pendingTask).toBeDefined();
        expect(pendingTask.status).toBe("pending");
      });

      test("should return immediately for completed tasks with waitFor: completed", async () => {
        const startTime = Date.now();

        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            taskIds: ["task-completed-1", "task-validated-1"],
            waitFor: "completed",
            timeout: 30000,
          }),
        });

        const elapsed = Date.now() - startTime;

        if (res.status === 503) {
          return;
        }

        expect(res.status).toBe(200);
        const json = await res.json();

        // Should return immediately since both tasks are done
        expect(json.timedOut).toBe(false);
        expect(json.changed).toBe(true);
        expect(elapsed).toBeLessThan(5000); // Should be very fast
      }, 10000);

      test("should handle mix of existing and non-existing tasks", async () => {
        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            taskIds: ["task-completed-1", "does-not-exist-xyz"],
          }),
        });

        if (res.status === 503) {
          return;
        }

        expect(res.status).toBe(200);
        const json = await res.json();

        expect(json.tasks).toHaveLength(1);
        expect(json.tasks[0].id).toBe("task-completed-1");
        expect(json.notFound).toContain("does-not-exist-xyz");
      });

      test("should include task classification in response", async () => {
        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            taskIds: ["task-pending-1"],
          }),
        });

        if (res.status === 503) {
          return;
        }

        expect(res.status).toBe(200);
        const json = await res.json();

        expect(json.tasks).toHaveLength(1);
        expect(json.tasks[0]).toHaveProperty("classification");
        // Classification should be one of: ready, waiting, blocked
        expect(["ready", "waiting", "blocked"]).toContain(json.tasks[0].classification);
      });

      test("should include priority in response", async () => {
        const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            taskIds: ["task-completed-1"],
          }),
        });

        if (res.status === 503) {
          return;
        }

        expect(res.status).toBe(200);
        const json = await res.json();

        expect(json.tasks).toHaveLength(1);
        expect(json.tasks[0].priority).toBe("high");
      });
    });
  });

  // ===========================================================================
  // Edge Cases
  // ===========================================================================

  describe("Edge Cases", () => {
    test("should handle empty JSON body gracefully", async () => {
      const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "{}",
      });

      expect(res.status).toBe(400);
    });

    test("should handle invalid JSON", async () => {
      const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "not-json",
      });

      expect(res.status).toBe(400);
    });

    test("should handle very large task ID arrays", async () => {
      const manyIds = Array.from({ length: 100 }, (_, i) => `task-${i}`);

      const res = await app.request(`/tasks/${TEST_PROJECT}/status`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          taskIds: manyIds,
        }),
      });

      // Should handle gracefully - 200 or 503
      expect([200, 503].includes(res.status)).toBe(true);
    });
  });
});

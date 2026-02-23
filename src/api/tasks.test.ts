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
import { createProjectRealtimeHub } from "../core/realtime-hub";

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
  describe("GET /:projectId/stream - SSE Task Stream", () => {
    test("should set SSE hardening headers", async () => {
      const streamApp = new Hono();
      streamApp.route("/tasks", (createTaskRoutes as any)({ heartbeatIntervalMs: 25 }));

      const res = await streamApp.request(`/tasks/${TEST_PROJECT}/stream`);

      expect(res.status).toBe(200);
      expect(res.headers.get("content-type")).toBe("text/event-stream; charset=utf-8");
      expect(res.headers.get("cache-control")).toBe("no-cache, no-transform");
      expect(res.headers.get("connection")).toBe("keep-alive");
      expect(res.headers.get("x-accel-buffering")).toBe("no");
      expect(res.headers.get("pragma")).toBe("no-cache");

      await res.body?.cancel();
    });

    test("should open SSE stream with connected and snapshot events", async () => {
      const streamApp = new Hono();
      streamApp.route("/tasks", (createTaskRoutes as any)({ heartbeatIntervalMs: 25 }));

      const res = await streamApp.request(`/tasks/${TEST_PROJECT}/stream`);

      expect(res.status).toBe(200);
      expect(res.headers.get("content-type") || "").toContain("text/event-stream");
      expect(res.headers.get("cache-control") || "").toContain("no-cache");

      const reader = res.body?.getReader();
      expect(reader).toBeDefined();

      const decoder = new TextDecoder();
      let text = "";
      const deadline = Date.now() + 2000;

      while (Date.now() < deadline) {
        const result = await reader!.read();
        if (result.done) {
          break;
        }

        text += decoder.decode(result.value, { stream: true });

        if (text.includes("event: connected") && text.includes("event: tasks_snapshot")) {
          break;
        }
      }

      expect(text).toContain("event: connected");
      expect(text).toContain("event: tasks_snapshot");
      expect(text.indexOf("event: connected")).toBeLessThan(text.indexOf("event: tasks_snapshot"));

      const firstSnapshotDataLine = text
        .split("\n")
        .find((line) => line.startsWith("data: {") && line.includes("\"type\":\"tasks_snapshot\""));

      expect(firstSnapshotDataLine).toBeDefined();
      expect(firstSnapshotDataLine).toContain(`\"projectId\":\"${TEST_PROJECT}\"`);
      expect(firstSnapshotDataLine).toContain("\"transport\":\"sse\"");

      await reader?.cancel();
    });

    test("should emit heartbeat events while stream is open", async () => {
      const streamApp = new Hono();
      streamApp.route("/tasks", (createTaskRoutes as any)({ heartbeatIntervalMs: 20 }));

      const res = await streamApp.request(`/tasks/${TEST_PROJECT}/stream`);
      expect(res.status).toBe(200);

      const reader = res.body?.getReader();
      expect(reader).toBeDefined();

      const decoder = new TextDecoder();
      let text = "";
      const deadline = Date.now() + 2500;

      while (Date.now() < deadline) {
        const result = await reader!.read();
        if (result.done) {
          break;
        }

        text += decoder.decode(result.value, { stream: true });

        if (text.includes("event: heartbeat")) {
          break;
        }
      }

      expect(text).toContain("event: heartbeat");

      await reader?.cancel();
    });

    test("should clean up hub subscription when client disconnects", async () => {
      const hub = createProjectRealtimeHub();
      const streamApp = new Hono();
      streamApp.route(
        "/tasks",
        createTaskRoutes({ realtimeHub: hub, heartbeatIntervalMs: 20 })
      );

      const res = await streamApp.request(`/tasks/${TEST_PROJECT}/stream`);
      expect(res.status).toBe(200);

      const reader = res.body?.getReader();
      expect(reader).toBeDefined();

      await reader!.read();

      const subscribedDeadline = Date.now() + 1000;
      while (Date.now() < subscribedDeadline && hub.getSubscriberCount(TEST_PROJECT) !== 1) {
        await new Promise((resolve) => setTimeout(resolve, 10));
      }

      expect(hub.getSubscriberCount(TEST_PROJECT)).toBe(1);

      await reader!.cancel();

      const deadline = Date.now() + 1000;
      while (Date.now() < deadline && hub.getSubscriberCount(TEST_PROJECT) !== 0) {
        await new Promise((resolve) => setTimeout(resolve, 10));
      }

      expect(hub.getSubscriberCount(TEST_PROJECT)).toBe(0);
    });

    test("should emit error event when initial snapshot retrieval fails", async () => {
      const failingTaskService = {
        getTasksWithDependencies: async () => {
          throw new Error("Initial snapshot failed in test");
        },
      };

      const streamApp = new Hono();
      streamApp.route(
        "/tasks",
        (createTaskRoutes as any)({
          heartbeatIntervalMs: 25,
          taskService: failingTaskService,
        })
      );

      const res = await streamApp.request(`/tasks/${TEST_PROJECT}/stream`);
      expect(res.status).toBe(200);

      const reader = res.body?.getReader();
      expect(reader).toBeDefined();

      const decoder = new TextDecoder();
      let text = "";
      const deadline = Date.now() + 2000;

      while (Date.now() < deadline) {
        const result = await reader!.read();
        if (result.done) {
          break;
        }

        text += decoder.decode(result.value, { stream: true });

        if (text.includes("event: error")) {
          break;
        }
      }

      expect(text).toContain("event: connected");
      expect(text).toContain("event: error");

      const errorDataLine = text
        .split("\n")
        .find((line) => line.startsWith("data: {") && line.includes('"type":"error"'));

      expect(errorDataLine).toBeDefined();
      expect(errorDataLine).toContain(`"projectId":"${TEST_PROJECT}"`);
      expect(errorDataLine).toContain('"transport":"sse"');
      expect(errorDataLine).toContain('"message":"Initial snapshot failed in test"');

      await reader?.cancel();
    });

    test("publishes project-scoped dirty + snapshot events when task claim mutates state", async () => {
      const hub = createProjectRealtimeHub();
      const claimApp = new Hono();
      claimApp.route("/tasks", createTaskRoutes({ realtimeHub: hub }));

      const projectEvents: string[] = [];
      const otherProjectEvents: string[] = [];
      hub.subscribe(TEST_PROJECT, ({ event }) => projectEvents.push(event));
      hub.subscribe("_other-project", ({ event }) => otherProjectEvents.push(event));

      const claimRes = await claimApp.request(`/tasks/${TEST_PROJECT}/task-123/claim`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ runnerId: "runner-a" }),
      });

      expect(claimRes.status).toBe(200);
      expect(projectEvents).toContain("project_dirty");
      expect(projectEvents).toContain("tasks_snapshot");
      expect(otherProjectEvents).toEqual([]);
    });

    test("publishes project-scoped dirty + snapshot events when task release mutates state", async () => {
      const hub = createProjectRealtimeHub();
      const releaseApp = new Hono();
      releaseApp.route("/tasks", createTaskRoutes({ realtimeHub: hub }));

      const projectEvents: string[] = [];
      const otherProjectEvents: string[] = [];
      hub.subscribe(TEST_PROJECT, ({ event }) => projectEvents.push(event));
      hub.subscribe("_other-project", ({ event }) => otherProjectEvents.push(event));

      const releaseRes = await releaseApp.request(`/tasks/${TEST_PROJECT}/task-123/release`, {
        method: "POST",
      });

      expect(releaseRes.status).toBe(200);
      expect(projectEvents).toContain("project_dirty");
      expect(projectEvents).toContain("tasks_snapshot");
      expect(otherProjectEvents).toEqual([]);
    });

    test("fanout stays project-scoped for concurrent SSE subscribers", async () => {
      const projectA = `${TEST_PROJECT}-a`;
      const projectB = `${TEST_PROJECT}-b`;
      const hub = createProjectRealtimeHub();
      const streamApp = new Hono();
      streamApp.route("/tasks", createTaskRoutes({ realtimeHub: hub, heartbeatIntervalMs: 5000 }));

      const [resA, resB] = await Promise.all([
        streamApp.request(`/tasks/${projectA}/stream`),
        streamApp.request(`/tasks/${projectB}/stream`),
      ]);

      expect(resA.status).toBe(200);
      expect(resB.status).toBe(200);

      const readerA = resA.body?.getReader();
      const readerB = resB.body?.getReader();
      expect(readerA).toBeDefined();
      expect(readerB).toBeDefined();

      const decoder = new TextDecoder();
      let textA = "";
      let textB = "";

      const readUntilSnapshot = async (reader: ReadableStreamDefaultReader<Uint8Array>) => {
        const deadline = Date.now() + 2000;
        let text = "";
        while (Date.now() < deadline) {
          const result = await reader.read();
          if (result.done) {
            break;
          }
          text += decoder.decode(result.value, { stream: true });
          if (text.includes("event: tasks_snapshot")) {
            break;
          }
        }
        return text;
      };

      [textA, textB] = await Promise.all([
        readUntilSnapshot(readerA!),
        readUntilSnapshot(readerB!),
      ]);

      const projectASnapshotsBefore = (textA.match(/event: tasks_snapshot/g) || []).length;
      const projectBSnapshotsBefore = (textB.match(/event: tasks_snapshot/g) || []).length;

      hub.publish(projectA, {
        event: "tasks_snapshot",
        payload: {
          type: "tasks_snapshot",
          transport: "sse",
          timestamp: new Date().toISOString(),
          projectId: projectA,
          tasks: [],
          count: 0,
          stats: {
            total: 0,
            ready: 0,
            waiting: 0,
            blocked: 0,
            in_progress: 0,
            completed: 0,
          },
          cycles: [],
        },
      });

      const fanoutDeadline = Date.now() + 1500;
      while (Date.now() < fanoutDeadline) {
        const readResult = await Promise.race([
          readerA!.read(),
          new Promise<{ done: true; value?: undefined }>((resolve) =>
            setTimeout(() => resolve({ done: true, value: undefined }), 50)
          ),
        ]);

        if (readResult.done || !readResult.value) {
          break;
        }

        textA += decoder.decode(readResult.value, { stream: true });
        if ((textA.match(/event: tasks_snapshot/g) || []).length > projectASnapshotsBefore) {
          break;
        }
      }

      const projectASnapshotsAfter = (textA.match(/event: tasks_snapshot/g) || []).length;
      const projectBSnapshotsAfter = (textB.match(/event: tasks_snapshot/g) || []).length;

      expect(projectASnapshotsAfter).toBe(projectASnapshotsBefore + 1);
      expect(projectBSnapshotsAfter).toBe(projectBSnapshotsBefore);

      await readerA?.cancel();
      await readerB?.cancel();
    });
  });

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

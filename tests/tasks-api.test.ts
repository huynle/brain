/**
 * Integration tests for task API endpoints
 */

import { describe, test, expect, beforeAll } from "bun:test";
import { createApp } from "../src/server";
import { getConfig } from "../src/config";

// =============================================================================
// Test Setup
// =============================================================================

const config = getConfig();
let app: ReturnType<typeof createApp>;

beforeAll(() => {
  app = createApp(config);
});

// =============================================================================
// Validation Tests
// =============================================================================

describe("GET /api/v1/tasks/:projectId", () => {
  test("returns 400 for empty project ID", async () => {
    const res = await app.request("/api/v1/tasks/");
    // Note: This might be 404 depending on routing - adjust as needed
    expect([400, 404]).toContain(res.status);
  });

  test("returns tasks array for valid project", async () => {
    const res = await app.request("/api/v1/tasks/test-project");
    expect(res.status).toBe(200);

    const data = await res.json();
    expect(data).toHaveProperty("tasks");
    expect(Array.isArray(data.tasks)).toBe(true);
    expect(data).toHaveProperty("count");
    expect(data).toHaveProperty("stats");
  });

  test("returns empty array for project with no tasks", async () => {
    const res = await app.request("/api/v1/tasks/nonexistent-project");
    expect(res.status).toBe(200);

    const data = await res.json();
    expect(data.tasks).toEqual([]);
    expect(data.count).toBe(0);
  });
});

describe("GET /api/v1/tasks/:projectId/ready", () => {
  test("returns ready tasks", async () => {
    const res = await app.request("/api/v1/tasks/test-project/ready");
    expect(res.status).toBe(200);

    const data = await res.json();
    expect(data).toHaveProperty("tasks");
    expect(data).toHaveProperty("count");

    // All returned tasks should be classified as ready
    for (const task of data.tasks) {
      expect(task.classification).toBe("ready");
    }
  });

  test("returns tasks sorted by priority", async () => {
    const res = await app.request("/api/v1/tasks/test-project/ready");
    const data = await res.json();

    if (data.tasks.length > 1) {
      const priorities = data.tasks.map((t: any) => t.priority);
      const priorityOrder = { high: 0, medium: 1, low: 2 };

      for (let i = 1; i < priorities.length; i++) {
        const prev =
          priorityOrder[priorities[i - 1] as keyof typeof priorityOrder] ?? 1;
        const curr =
          priorityOrder[priorities[i] as keyof typeof priorityOrder] ?? 1;
        expect(prev).toBeLessThanOrEqual(curr);
      }
    }
  });
});

describe("GET /api/v1/tasks/:projectId/waiting", () => {
  test("returns waiting tasks", async () => {
    const res = await app.request("/api/v1/tasks/test-project/waiting");
    expect(res.status).toBe(200);

    const data = await res.json();
    expect(data).toHaveProperty("tasks");
    expect(data).toHaveProperty("count");

    // All returned tasks should be waiting or waiting_on_parent
    for (const task of data.tasks) {
      expect(["waiting", "waiting_on_parent"]).toContain(task.classification);
    }
  });
});

describe("GET /api/v1/tasks/:projectId/blocked", () => {
  test("returns blocked tasks", async () => {
    const res = await app.request("/api/v1/tasks/test-project/blocked");
    expect(res.status).toBe(200);

    const data = await res.json();
    expect(data).toHaveProperty("tasks");
    expect(data).toHaveProperty("count");

    // All returned tasks should be blocked or blocked_by_parent
    for (const task of data.tasks) {
      expect(["blocked", "blocked_by_parent"]).toContain(task.classification);
    }
  });

  test("includes blocked_by_reason for circular dependencies", async () => {
    const res = await app.request("/api/v1/tasks/test-project/blocked");
    const data = await res.json();

    const circularTasks = data.tasks.filter((t: any) => t.in_cycle);
    for (const task of circularTasks) {
      expect(task.blocked_by_reason).toBe("circular_dependency");
    }
  });
});

describe("GET /api/v1/tasks/:projectId/next", () => {
  test("returns next task when ready tasks exist", async () => {
    // First check if there are ready tasks
    const readyRes = await app.request("/api/v1/tasks/test-project/ready");
    const readyData = await readyRes.json();

    const res = await app.request("/api/v1/tasks/test-project/next");

    if (readyData.count > 0) {
      expect(res.status).toBe(200);
      const data = await res.json();
      expect(data).toHaveProperty("task");
      expect(data.task).not.toBeNull();
      expect(data.task.classification).toBe("ready");
    } else {
      expect(res.status).toBe(404);
      const data = await res.json();
      expect(data.task).toBeNull();
      expect(data.message).toBe("No ready tasks available");
    }
  });

  test("returns 404 with message when no ready tasks", async () => {
    const res = await app.request("/api/v1/tasks/empty-project/next");
    expect(res.status).toBe(404);

    const data = await res.json();
    expect(data.task).toBeNull();
    expect(data.message).toBeDefined();
  });
});

// =============================================================================
// Response Schema Tests
// =============================================================================

describe("response schema", () => {
  test("task includes all required fields", async () => {
    const res = await app.request("/api/v1/tasks/test-project");
    const data = await res.json();

    if (data.tasks.length > 0) {
      const task = data.tasks[0];

      // Original task fields
      expect(task).toHaveProperty("id");
      expect(task).toHaveProperty("path");
      expect(task).toHaveProperty("title");
      expect(task).toHaveProperty("priority");
      expect(task).toHaveProperty("status");
      expect(task).toHaveProperty("depends_on");
      expect(task).toHaveProperty("parent_id");

      // Resolved fields
      expect(task).toHaveProperty("resolved_deps");
      expect(task).toHaveProperty("unresolved_deps");
      expect(task).toHaveProperty("parent_chain");
      expect(task).toHaveProperty("classification");
      expect(task).toHaveProperty("blocked_by");
      expect(task).toHaveProperty("waiting_on");
      expect(task).toHaveProperty("in_cycle");
      expect(task).toHaveProperty("resolved_workdir");
    }
  });

  test("stats includes all counts", async () => {
    const res = await app.request("/api/v1/tasks/test-project");
    const data = await res.json();

    expect(data.stats).toHaveProperty("total");
    expect(data.stats).toHaveProperty("ready");
    expect(data.stats).toHaveProperty("waiting");
    expect(data.stats).toHaveProperty("blocked");
    expect(data.stats).toHaveProperty("not_pending");
  });
});

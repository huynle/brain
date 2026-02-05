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
// Project List Tests
// =============================================================================

describe("GET /api/v1/tasks", () => {
  test("returns list of projects", async () => {
    const res = await app.request("/api/v1/tasks");
    expect(res.status).toBe(200);

    const data = await res.json();
    expect(data).toHaveProperty("projects");
    expect(Array.isArray(data.projects)).toBe(true);
    expect(data).toHaveProperty("count");
    expect(typeof data.count).toBe("number");
  });
});

// =============================================================================
// Validation Tests
// =============================================================================

describe("GET /api/v1/tasks/:projectId", () => {

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

// =============================================================================
// Task Claiming Tests
// =============================================================================

describe("POST /api/v1/tasks/:projectId/:taskId/claim", () => {
  const projectId = "test-project";
  const taskId = "task123";

  test("claims unclaimed task successfully", async () => {
    // First release to ensure clean state
    await app.request(`/api/v1/tasks/${projectId}/${taskId}/release`, {
      method: "POST",
    });

    const res = await app.request(`/api/v1/tasks/${projectId}/${taskId}/claim`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ runnerId: "runner-1" }),
    });

    expect(res.status).toBe(200);
    const data = await res.json();
    expect(data.success).toBe(true);
    expect(data.taskId).toBe(taskId);
    expect(data.runnerId).toBe("runner-1");
    expect(data.claimedAt).toBeDefined();
  });

  test("returns 409 when task already claimed by different runner", async () => {
    // First claim the task
    await app.request(`/api/v1/tasks/${projectId}/${taskId}/release`, {
      method: "POST",
    });
    await app.request(`/api/v1/tasks/${projectId}/${taskId}/claim`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ runnerId: "runner-1" }),
    });

    // Try to claim with different runner
    const res = await app.request(`/api/v1/tasks/${projectId}/${taskId}/claim`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ runnerId: "runner-2" }),
    });

    expect(res.status).toBe(409);
    const data = await res.json();
    expect(data.success).toBe(false);
    expect(data.error).toBe("conflict");
    expect(data.claimedBy).toBe("runner-1");
    expect(data.isStale).toBe(false);
  });

  test("same runner can refresh claim", async () => {
    // First claim
    await app.request(`/api/v1/tasks/${projectId}/${taskId}/release`, {
      method: "POST",
    });
    await app.request(`/api/v1/tasks/${projectId}/${taskId}/claim`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ runnerId: "runner-1" }),
    });

    // Same runner claims again
    const res = await app.request(`/api/v1/tasks/${projectId}/${taskId}/claim`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ runnerId: "runner-1" }),
    });

    expect(res.status).toBe(200);
    const data = await res.json();
    expect(data.success).toBe(true);
    expect(data.runnerId).toBe("runner-1");
  });

  test("returns 400 for missing runnerId", async () => {
    const res = await app.request(`/api/v1/tasks/${projectId}/${taskId}/claim`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({}),
    });

    expect(res.status).toBe(400);
    const data = await res.json();
    expect(data.message).toContain("runnerId");
  });

  test("returns 400 for invalid project ID", async () => {
    const res = await app.request(`/api/v1/tasks/invalid@project/${taskId}/claim`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ runnerId: "runner-1" }),
    });

    expect(res.status).toBe(400);
  });
});

describe("POST /api/v1/tasks/:projectId/:taskId/release", () => {
  const projectId = "test-project";
  const taskId = "task456";

  test("releases existing claim", async () => {
    // First claim
    await app.request(`/api/v1/tasks/${projectId}/${taskId}/claim`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ runnerId: "runner-1" }),
    });

    // Release
    const res = await app.request(`/api/v1/tasks/${projectId}/${taskId}/release`, {
      method: "POST",
    });

    expect(res.status).toBe(200);
    const data = await res.json();
    expect(data.success).toBe(true);
    expect(data.message).toBe("Claim released");

    // Verify claim is gone
    const statusRes = await app.request(`/api/v1/tasks/${projectId}/${taskId}/claim-status`);
    const statusData = await statusRes.json();
    expect(statusData.claimed).toBe(false);
  });

  test("succeeds even if no claim exists", async () => {
    // Release without claiming first
    const res = await app.request(`/api/v1/tasks/${projectId}/nonexistent-task/release`, {
      method: "POST",
    });

    expect(res.status).toBe(200);
    const data = await res.json();
    expect(data.success).toBe(true);
    expect(data.message).toBe("No claim existed");
  });
});

describe("GET /api/v1/tasks/:projectId/:taskId/claim-status", () => {
  const projectId = "test-project";
  const taskId = "task789";

  test("returns claimed: false for unclaimed task", async () => {
    // Ensure unclaimed
    await app.request(`/api/v1/tasks/${projectId}/${taskId}/release`, {
      method: "POST",
    });

    const res = await app.request(`/api/v1/tasks/${projectId}/${taskId}/claim-status`);

    expect(res.status).toBe(200);
    const data = await res.json();
    expect(data.claimed).toBe(false);
    expect(data.claimedBy).toBeUndefined();
  });

  test("returns claim info for claimed task", async () => {
    // Claim first
    await app.request(`/api/v1/tasks/${projectId}/${taskId}/release`, {
      method: "POST",
    });
    await app.request(`/api/v1/tasks/${projectId}/${taskId}/claim`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ runnerId: "status-runner" }),
    });

    const res = await app.request(`/api/v1/tasks/${projectId}/${taskId}/claim-status`);

    expect(res.status).toBe(200);
    const data = await res.json();
    expect(data.claimed).toBe(true);
    expect(data.claimedBy).toBe("status-runner");
    expect(data.claimedAt).toBeDefined();
    expect(data.isStale).toBe(false);
  });

  test("reports isStale accurately", async () => {
    // This is harder to test without mocking time, but we verify the field exists
    await app.request(`/api/v1/tasks/${projectId}/${taskId}/release`, {
      method: "POST",
    });
    await app.request(`/api/v1/tasks/${projectId}/${taskId}/claim`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ runnerId: "fresh-runner" }),
    });

    const res = await app.request(`/api/v1/tasks/${projectId}/${taskId}/claim-status`);
    const data = await res.json();

    // Fresh claim should not be stale
    expect(data.isStale).toBe(false);
  });
});

describe("claims are per project+task", () => {
  test("same taskId in different projects are independent", async () => {
    const taskId = "shared-task-id";

    // Claim in project-a
    await app.request(`/api/v1/tasks/project-a/${taskId}/release`, { method: "POST" });
    await app.request(`/api/v1/tasks/project-a/${taskId}/claim`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ runnerId: "runner-a" }),
    });

    // Claim same taskId in project-b should succeed
    await app.request(`/api/v1/tasks/project-b/${taskId}/release`, { method: "POST" });
    const res = await app.request(`/api/v1/tasks/project-b/${taskId}/claim`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ runnerId: "runner-b" }),
    });

    expect(res.status).toBe(200);
    const data = await res.json();
    expect(data.success).toBe(true);
    expect(data.runnerId).toBe("runner-b");

    // Verify both claims exist independently
    const statusA = await app.request(`/api/v1/tasks/project-a/${taskId}/claim-status`);
    const dataA = await statusA.json();
    expect(dataA.claimedBy).toBe("runner-a");

    const statusB = await app.request(`/api/v1/tasks/project-b/${taskId}/claim-status`);
    const dataB = await statusB.json();
    expect(dataB.claimedBy).toBe("runner-b");
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

      // Resolved fields
      expect(task).toHaveProperty("resolved_deps");
      expect(task).toHaveProperty("unresolved_deps");
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

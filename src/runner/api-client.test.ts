/**
 * Tests for Brain API Client
 */

import {
  describe,
  it,
  expect,
  beforeEach,
  afterEach,
  mock,
} from "bun:test";
import {
  ApiClient,
  ApiError,
  getApiClient,
  resetApiClient,
} from "./api-client";
import { resetConfig } from "./config";
import type { RunnerConfig, ApiHealth } from "./types";
import type { ResolvedTask } from "../core/types";

// Mock config for testing
const mockConfig: RunnerConfig = {
  brainApiUrl: "http://localhost:3000",
  pollInterval: 30,
  taskPollInterval: 5,
  maxParallel: 3,
  stateDir: "/tmp/state",
  logDir: "/tmp/log",
  workDir: "/tmp/work",
  apiTimeout: 1000, // Short timeout for tests
  taskTimeout: 30000,
  opencode: {
    bin: "opencode",
    agent: "general",
    model: "anthropic/claude-sonnet-4-20250514",
  },
  excludeProjects: [],
};

// Helper to create a mock fetch function
function createMockFetch(handler: () => Promise<Response>) {
  const mockFn = mock(handler) as unknown as typeof fetch;
  return mockFn;
}

describe("ApiClient", () => {
  let client: ApiClient;
  let originalFetch: typeof fetch;

  beforeEach(() => {
    resetConfig();
    resetApiClient();
    client = new ApiClient(mockConfig);
    originalFetch = globalThis.fetch;
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    resetApiClient();
  });

  describe("checkHealth", () => {
    it("returns health status from API", async () => {
      const mockHealth: ApiHealth = {
        status: "healthy",
        zkAvailable: true,
        dbAvailable: true,
      };

      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(JSON.stringify(mockHealth), { status: 200 })
        )
      );

      const health = await client.checkHealth();

      expect(health).toEqual(mockHealth);
    });

    it("caches health status within TTL", async () => {
      const mockHealth: ApiHealth = {
        status: "healthy",
        zkAvailable: true,
        dbAvailable: true,
      };

      let callCount = 0;
      globalThis.fetch = createMockFetch(() => {
        callCount++;
        return Promise.resolve(
          new Response(JSON.stringify(mockHealth), { status: 200 })
        );
      });

      // First call
      await client.checkHealth();
      // Second call (should use cache)
      await client.checkHealth();

      expect(callCount).toBe(1);
    });

    it("returns unhealthy on fetch error", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.reject(new Error("Network error"))
      );

      const health = await client.checkHealth();

      expect(health.status).toBe("unhealthy");
      expect(health.zkAvailable).toBe(false);
      expect(health.dbAvailable).toBe(false);
    });
  });

  describe("isAvailable", () => {
    it("returns true when healthy", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              status: "healthy",
              zkAvailable: true,
              dbAvailable: true,
            }),
            { status: 200 }
          )
        )
      );

      const available = await client.isAvailable();

      expect(available).toBe(true);
    });

    it("returns true when degraded", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              status: "degraded",
              zkAvailable: true,
              dbAvailable: false,
            }),
            { status: 200 }
          )
        )
      );

      const available = await client.isAvailable();

      expect(available).toBe(true);
    });

    it("returns false when unhealthy", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.reject(new Error("Network error"))
      );

      const available = await client.isAvailable();

      expect(available).toBe(false);
    });
  });

  describe("getReadyTasks", () => {
    it("returns array of ready tasks", async () => {
      const mockTasks = [
        { id: "task1", title: "Task 1", classification: "ready" },
        { id: "task2", title: "Task 2", classification: "ready" },
      ];

      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(JSON.stringify({ tasks: mockTasks, count: 2 }), {
            status: 200,
          })
        )
      );

      const tasks = await client.getReadyTasks("myproject");

      expect(tasks.length).toBe(2);
      expect(tasks[0].id).toBe("task1");
    });

    it("throws ApiError on non-ok response", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(new Response("Not found", { status: 404 }))
      );

      await expect(client.getReadyTasks("myproject")).rejects.toThrow(ApiError);
    });
  });

  describe("getNextTask", () => {
    it("returns task when available", async () => {
      const mockTask = { id: "task1", title: "Task 1", classification: "ready" };

      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(JSON.stringify({ task: mockTask }), { status: 200 })
        )
      );

      const task = await client.getNextTask("myproject");

      expect(task).not.toBeNull();
      expect(task!.id).toBe("task1");
    });

    it("returns null on 404", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({ task: null, message: "No ready tasks" }),
            { status: 404 }
          )
        )
      );

      const task = await client.getNextTask("myproject");

      expect(task).toBeNull();
    });

    it("throws ApiError on other errors", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(new Response("Server error", { status: 500 }))
      );

      await expect(client.getNextTask("myproject")).rejects.toThrow(ApiError);
    });
  });

  describe("getAllTasks", () => {
    it("returns all tasks for project", async () => {
      const mockTasks = [
        { id: "task1", classification: "ready" },
        { id: "task2", classification: "waiting" },
        { id: "task3", classification: "blocked" },
      ];

      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(JSON.stringify({ tasks: mockTasks }), { status: 200 })
        )
      );

      const tasks = await client.getAllTasks("myproject");

      expect(tasks.length).toBe(3);
    });
  });

  describe("getWaitingTasks", () => {
    it("returns waiting tasks", async () => {
      const mockTasks = [{ id: "task1", classification: "waiting" }];

      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(JSON.stringify({ tasks: mockTasks, count: 1 }), {
            status: 200,
          })
        )
      );

      const tasks = await client.getWaitingTasks("myproject");

      expect(tasks.length).toBe(1);
    });
  });

  describe("getBlockedTasks", () => {
    it("returns blocked tasks", async () => {
      const mockTasks = [{ id: "task1", classification: "blocked" }];

      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(JSON.stringify({ tasks: mockTasks, count: 1 }), {
            status: 200,
          })
        )
      );

      const tasks = await client.getBlockedTasks("myproject");

      expect(tasks.length).toBe(1);
    });
  });

  describe("updateTaskStatus", () => {
    it("sends correct payload", async () => {
      let capturedBody: string | undefined;

      globalThis.fetch = createMockFetch(() => {
        return Promise.resolve(new Response("{}", { status: 200 }));
      });

      // Replace fetch with one that captures the body
      const origFetch = globalThis.fetch;
      globalThis.fetch = ((url: string, options: RequestInit) => {
        capturedBody = options.body as string;
        return origFetch(url, options);
      }) as typeof fetch;

      await client.updateTaskStatus("projects/test/task/abc.md", "completed");

      expect(capturedBody).toBe(JSON.stringify({ status: "completed" }));
    });

    it("encodes task path correctly", async () => {
      let capturedUrl: string | undefined;

      globalThis.fetch = ((url: string) => {
        capturedUrl = url;
        return Promise.resolve(new Response("{}", { status: 200 }));
      }) as typeof fetch;

      await client.updateTaskStatus("projects/test/task/abc.md", "completed");

      expect(capturedUrl).toContain(
        encodeURIComponent("projects/test/task/abc.md")
      );
    });

    it("throws ApiError on failure", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(new Response("Not found", { status: 404 }))
      );

      await expect(
        client.updateTaskStatus("nonexistent", "completed")
      ).rejects.toThrow(ApiError);
    });
  });

  describe("appendToTask", () => {
    it("sends correct payload", async () => {
      let capturedBody: string | undefined;

      globalThis.fetch = ((url: string, options: RequestInit) => {
        capturedBody = options.body as string;
        return Promise.resolve(new Response("{}", { status: 200 }));
      }) as typeof fetch;

      await client.appendToTask("projects/test/task/abc.md", "## Update\nDone");

      expect(capturedBody).toBe(
        JSON.stringify({ append: "## Update\nDone" })
      );
    });
  });

  describe("claimTask", () => {
    it("returns success on successful claim", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              success: true,
              taskId: "task1",
              runnerId: "runner1",
              claimedAt: new Date().toISOString(),
            }),
            { status: 200 }
          )
        )
      );

      const result = await client.claimTask("myproject", "task1", "runner1");

      expect(result.success).toBe(true);
      expect(result.taskId).toBe("task1");
    });

    it("returns conflict info on 409", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              success: false,
              error: "conflict",
              claimedBy: "other-runner",
            }),
            { status: 409 }
          )
        )
      );

      const result = await client.claimTask("myproject", "task1", "runner1");

      expect(result.success).toBe(false);
      expect(result.claimedBy).toBe("other-runner");
      expect(result.message).toBe("Task already claimed");
    });

    it("throws ApiError on other errors", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(new Response("Server error", { status: 500 }))
      );

      await expect(
        client.claimTask("myproject", "task1", "runner1")
      ).rejects.toThrow(ApiError);
    });
  });

  describe("releaseTask", () => {
    it("releases task successfully", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(JSON.stringify({ success: true }), { status: 200 })
        )
      );

      await expect(
        client.releaseTask("myproject", "task1")
      ).resolves.toBeUndefined();
    });

    it("throws ApiError on failure", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(new Response("Server error", { status: 500 }))
      );

      await expect(client.releaseTask("myproject", "task1")).rejects.toThrow(
        ApiError
      );
    });
  });

  describe("timeout handling", () => {
    it("throws ApiError with 408 on timeout", async () => {
      // Create client with very short timeout
      const shortTimeoutConfig = { ...mockConfig, apiTimeout: 10 };
      const timeoutClient = new ApiClient(shortTimeoutConfig);

      // Mock fetch that respects AbortSignal
      globalThis.fetch = ((_url: string, options?: RequestInit) => {
        return new Promise((resolve, reject) => {
          const signal = options?.signal;
          if (signal) {
            signal.addEventListener("abort", () => {
              const error = new Error("Aborted");
              error.name = "AbortError";
              reject(error);
            });
          }
          // Never resolve - let it time out
          setTimeout(() => resolve(new Response("{}")), 10000);
        });
      }) as typeof fetch;

      // Use getReadyTasks which throws errors (unlike checkHealth which catches them)
      try {
        await timeoutClient.getReadyTasks("myproject");
        expect(true).toBe(false); // Should not reach here
      } catch (error) {
        expect(error).toBeInstanceOf(ApiError);
        expect((error as ApiError).statusCode).toBe(408);
        expect((error as ApiError).message).toContain("timeout");
      }
    });
  });

  describe("ApiError", () => {
    it("has correct properties", () => {
      const error = new ApiError(404, "Not found");

      expect(error.name).toBe("ApiError");
      expect(error.statusCode).toBe(404);
      expect(error.message).toContain("404");
      expect(error.message).toContain("Not found");
    });
  });

  describe("getApiClient singleton", () => {
    it("returns singleton instance", () => {
      const client1 = getApiClient();
      const client2 = getApiClient();

      expect(client1).toBe(client2);
    });

    it("creates new instance after reset", () => {
      const client1 = getApiClient();
      resetApiClient();
      const client2 = getApiClient();

      expect(client1).not.toBe(client2);
    });
  });
});

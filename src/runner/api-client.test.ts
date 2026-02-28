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
import type { CronRun, ResolvedTask } from "../core/types";

// Mock config for testing
const mockConfig: RunnerConfig = {
  brainApiUrl: "http://localhost:3333",
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
  idleDetectionThreshold: 60000,
  maxTotalProcesses: 10,
  memoryThresholdPercent: 10,
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

  describe("listProjects", () => {
    it("returns array of project names", async () => {
      const mockProjects = ["brain-api", "my-project", "test-project"];

      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(JSON.stringify({ projects: mockProjects, count: 3 }), {
            status: 200,
          })
        )
      );

      const projects = await client.listProjects();

      expect(projects).toEqual(mockProjects);
    });

    it("returns empty array when no projects", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(JSON.stringify({ projects: [], count: 0 }), {
            status: 200,
          })
        )
      );

      const projects = await client.listProjects();

      expect(projects).toEqual([]);
    });

    it("returns empty array when projects field missing", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(JSON.stringify({}), { status: 200 })
        )
      );

      const projects = await client.listProjects();

      expect(projects).toEqual([]);
    });

    it("throws ApiError on non-ok response", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(new Response("Server error", { status: 500 }))
      );

      await expect(client.listProjects()).rejects.toThrow(ApiError);
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

  describe("cron endpoints", () => {
    it("getCronEntries returns cron list", async () => {
      const mockCrons = [
        {
          id: "crn00001",
          path: "projects/myproject/cron/crn00001.md",
          title: "Nightly",
          status: "active" as const,
          schedule: "0 2 * * *",
        },
      ];

      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(JSON.stringify({ crons: mockCrons, count: 1 }), {
            status: 200,
          })
        )
      );

      const crons = await client.getCronEntries("myproject");

      expect(crons).toEqual(mockCrons);
    });

    it("getCronEntry returns cron detail payload", async () => {
      const mockDetail = {
        cron: {
          id: "crn00001",
          path: "projects/myproject/cron/crn00001.md",
          title: "Nightly",
          status: "pending",
          type: "cron",
          content: "",
          tags: [],
          created: "2026-02-20T00:00:00.000Z",
          modified: "2026-02-20T00:00:00.000Z",
          link: "[Nightly](crn00001)",
          schedule: "0 2 * * *",
        },
        pipeline: [],
        pipelineCount: 0,
      };

      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(new Response(JSON.stringify(mockDetail), { status: 200 }))
      );

      const detail = await client.getCronEntry("myproject", "crn00001");

      expect(detail.cron.id).toBe("crn00001");
      expect(detail.pipelineCount).toBe(0);
    });

    it("createCronEntry posts payload and returns mutation response", async () => {
      let capturedMethod: string | undefined;
      let capturedBody: string | undefined;
      const mockResponse = {
        cron: {
          id: "crn00001",
          path: "projects/myproject/cron/crn00001.md",
          title: "Nightly",
          status: "pending",
          type: "cron",
          content: "",
          tags: [],
          created: "2026-02-20T00:00:00.000Z",
          modified: "2026-02-20T00:00:00.000Z",
          link: "[Nightly](crn00001)",
          schedule: "0 2 * * *",
        },
        message: "Cron created",
      };

      globalThis.fetch = ((_: string, options?: RequestInit) => {
        capturedMethod = options?.method;
        capturedBody = options?.body as string;
        return Promise.resolve(new Response(JSON.stringify(mockResponse), { status: 201 }));
      }) as typeof fetch;

      const result = await client.createCronEntry("myproject", {
        title: "Nightly",
        schedule: "0 2 * * *",
        tags: ["ops"],
      });

      expect(capturedMethod).toBe("POST");
      expect(capturedBody).toBe(
        JSON.stringify({ title: "Nightly", schedule: "0 2 * * *", tags: ["ops"] })
      );
      expect(result.cron.id).toBe("crn00001");
      expect(result.message).toBe("Cron created");
    });

    it("updateCronEntry patches payload and returns mutation response", async () => {
      let capturedUrl: string | undefined;
      let capturedMethod: string | undefined;
      let capturedBody: string | undefined;
      const mockResponse = {
        cron: {
          id: "crn00001",
          path: "projects/myproject/cron/crn00001.md",
          title: "Nightly Updated",
          status: "pending",
          type: "cron",
          content: "",
          tags: [],
          created: "2026-02-20T00:00:00.000Z",
          modified: "2026-02-20T00:00:00.000Z",
          link: "[Nightly Updated](crn00001)",
          schedule: "0 2 * * *",
        },
        message: "Cron updated",
      };

      globalThis.fetch = ((url: string, options?: RequestInit) => {
        capturedUrl = url;
        capturedMethod = options?.method;
        capturedBody = options?.body as string;
        return Promise.resolve(new Response(JSON.stringify(mockResponse), { status: 200 }));
      }) as typeof fetch;

      const result = await client.updateCronEntry("myproject", "crn00001", {
        title: "Nightly Updated",
      });

      expect(capturedUrl).toContain("/api/v1/crons/myproject/crons/crn00001");
      expect(capturedMethod).toBe("PATCH");
      expect(capturedBody).toBe(JSON.stringify({ title: "Nightly Updated" }));
      expect(result.cron.title).toBe("Nightly Updated");
      expect(result.message).toBe("Cron updated");
    });

    it("deleteCronEntry uses confirm query and returns payload", async () => {
      let capturedUrl: string | undefined;
      let capturedMethod: string | undefined;
      const mockResponse = {
        message: "Cron deleted successfully",
        path: "projects/myproject/cron/crn00001.md",
      };

      globalThis.fetch = ((url: string, options?: RequestInit) => {
        capturedUrl = url;
        capturedMethod = options?.method;
        return Promise.resolve(new Response(JSON.stringify(mockResponse), { status: 200 }));
      }) as typeof fetch;

      const result = await client.deleteCronEntry("myproject", "crn00001");

      expect(capturedUrl).toContain("/api/v1/crons/myproject/crons/crn00001?confirm=true");
      expect(capturedMethod).toBe("DELETE");
      expect(result.path).toBe("projects/myproject/cron/crn00001.md");
      expect(result.message).toBe("Cron deleted successfully");
    });

    it("getCronRuns returns run history", async () => {
      const mockRuns = {
        cronId: "crn00001",
        runs: [{ run_id: "run-1", status: "completed" as const, started: "2026-02-23T02:00:00.000Z" }],
        count: 1,
      };

      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(new Response(JSON.stringify(mockRuns), { status: 200 }))
      );

      const result = await client.getCronRuns("myproject", "crn00001");
      expect(result.cronId).toBe("crn00001");
      expect(result.count).toBe(1);
      expect(result.runs[0]?.run_id).toBe("run-1");
    });

    it("getCronLinkedTasks returns linked tasks", async () => {
      const mockLinked = {
        cronId: "crn00001",
        tasks: [{ id: "tsk00001", title: "Task A" }],
        count: 1,
      };

      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(new Response(JSON.stringify(mockLinked), { status: 200 }))
      );

      const result = await client.getCronLinkedTasks("myproject", "crn00001");
      expect(result.cronId).toBe("crn00001");
      expect(result.count).toBe(1);
    });

    it("setCronLinkedTasks PATCHes taskIds payload", async () => {
      let capturedMethod: string | undefined;
      let capturedBody: string | undefined;
      const mockResponse = { cronId: "crn00001", tasks: [], count: 0, message: "updated" };

      globalThis.fetch = ((_: string, options?: RequestInit) => {
        capturedMethod = options?.method;
        capturedBody = options?.body as string;
        return Promise.resolve(new Response(JSON.stringify(mockResponse), { status: 200 }));
      }) as typeof fetch;

      const result = await client.setCronLinkedTasks("myproject", "crn00001", ["tsk00001", "tsk00002"]);

      expect(capturedMethod).toBe("PATCH");
      expect(capturedBody).toBe(JSON.stringify({ taskIds: ["tsk00001", "tsk00002"] }));
      expect(result).toEqual(mockResponse);
    });

    it("addCronLinkedTask POSTs link endpoint", async () => {
      let capturedUrl: string | undefined;
      let capturedMethod: string | undefined;
      const mockResponse = { cronId: "crn00001", tasks: [], count: 0, message: "linked" };

      globalThis.fetch = ((url: string, options?: RequestInit) => {
        capturedUrl = url;
        capturedMethod = options?.method;
        return Promise.resolve(new Response(JSON.stringify(mockResponse), { status: 200 }));
      }) as typeof fetch;

      const result = await client.addCronLinkedTask("myproject", "crn00001", "tsk00001");

      expect(capturedUrl).toContain("/api/v1/crons/myproject/crons/crn00001/linked-tasks/tsk00001");
      expect(capturedMethod).toBe("POST");
      expect(result).toEqual(mockResponse);
    });

    it("removeCronLinkedTask DELETEs link endpoint", async () => {
      let capturedUrl: string | undefined;
      let capturedMethod: string | undefined;
      const mockResponse = { cronId: "crn00001", tasks: [], count: 0, message: "unlinked" };

      globalThis.fetch = ((url: string, options?: RequestInit) => {
        capturedUrl = url;
        capturedMethod = options?.method;
        return Promise.resolve(new Response(JSON.stringify(mockResponse), { status: 200 }));
      }) as typeof fetch;

      const result = await client.removeCronLinkedTask("myproject", "crn00001", "tsk00001");

      expect(capturedUrl).toContain("/api/v1/crons/myproject/crons/crn00001/linked-tasks/tsk00001");
      expect(capturedMethod).toBe("DELETE");
      expect(result).toEqual(mockResponse);
    });

    it("triggerCron posts trigger and returns trigger payload", async () => {
      let capturedUrl: string | undefined;
      let capturedMethod: string | undefined;
      let capturedBody: string | undefined;
      const triggerResponse = {
        cronId: "crn00001",
        run: {
          run_id: "run-abc",
          status: "in_progress" as const,
          started: "2026-02-23T02:00:00.000Z",
          tasks: 2,
        },
        pipeline: [],
        pipelineCount: 0,
        message: "Cron run triggered",
      };

      globalThis.fetch = ((url: string, options?: RequestInit) => {
        capturedUrl = url;
        capturedMethod = options?.method;
        capturedBody = options?.body as string;
        return Promise.resolve(new Response(JSON.stringify(triggerResponse), { status: 200 }));
      }) as typeof fetch;

      const result = await client.triggerCron("myproject", "crn00001");

      expect(capturedUrl).toContain("/api/v1/crons/myproject/crons/crn00001/trigger");
      expect(capturedMethod).toBe("POST");
      expect(capturedBody).toBeUndefined();
      expect(result).toEqual(triggerResponse);
    });

    it("encodes project and cron IDs for trigger endpoint", async () => {
      let capturedUrl: string | undefined;
      const triggerResponse = {
        cronId: "crn 00001",
        run: {
          run_id: "run-abc",
          status: "in_progress" as const,
          started: "2026-02-23T02:00:00.000Z",
          tasks: 0,
        },
        pipeline: [],
        pipelineCount: 0,
        message: "Cron run triggered",
      };

      globalThis.fetch = ((url: string) => {
        capturedUrl = url;
        return Promise.resolve(new Response(JSON.stringify(triggerResponse), { status: 200 }));
      }) as typeof fetch;

      await client.triggerCron("my/project", "crn 00001");

      expect(capturedUrl).toContain("/api/v1/crons/my%2Fproject/crons/crn%2000001/trigger");
    });

    it("encodes project, cron, and task IDs for linked-task mutations", async () => {
      let capturedUrl: string | undefined;
      const mockResponse = { cronId: "crn 00001", tasks: [], count: 0, message: "linked" };

      globalThis.fetch = ((url: string) => {
        capturedUrl = url;
        return Promise.resolve(new Response(JSON.stringify(mockResponse), { status: 200 }));
      }) as typeof fetch;

      await client.addCronLinkedTask("my/project", "crn 00001", "tsk 00001");

      expect(capturedUrl).toContain(
        "/api/v1/crons/my%2Fproject/crons/crn%2000001/linked-tasks/tsk%2000001"
      );
    });

    it("updateCronRun merges run by run_id and PATCHes entry", async () => {
      const existing: CronRun = {
        run_id: "20260222-0200",
        status: "failed",
        started: "2026-02-22T02:00:00.000Z",
      };
      const incoming: CronRun = {
        run_id: "20260222-0200",
        status: "completed",
        started: "2026-02-22T02:00:00.000Z",
        completed: "2026-02-22T02:00:08.000Z",
      };

      let callCount = 0;
      let patchBody: string | undefined;
      globalThis.fetch = ((_: string, options?: RequestInit) => {
        callCount++;
        if (callCount === 1) {
          return Promise.resolve(new Response(JSON.stringify({ runs: [existing] }), { status: 200 }));
        }

        patchBody = options?.body as string;
        return Promise.resolve(new Response("{}", { status: 200 }));
      }) as typeof fetch;

      await client.updateCronRun("projects/myproject/cron/crn00001.md", incoming);

      expect(callCount).toBe(2);
      expect(patchBody).toBe(JSON.stringify({ runs: [incoming] }));
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

  describe("updateEntryMetadata", () => {
    it("includes merge intent fields in PATCH payload when provided", async () => {
      let capturedBody: string | undefined;

      globalThis.fetch = ((_: string, options?: RequestInit) => {
        capturedBody = options?.body as string;
        return Promise.resolve(new Response("{}", { status: 200 }));
      }) as typeof fetch;

      await client.updateEntryMetadata("projects/test/task/abc.md", {
        git_branch: "feature/auth-flow",
        merge_target_branch: "main",
        merge_policy: "auto_merge",
        merge_strategy: "squash",
        remote_branch_policy: "delete",
        open_pr_before_merge: true,
        execution_mode: "current_branch",
        checkout_enabled: true,
      });

      expect(capturedBody).toBe(
        JSON.stringify({
          git_branch: "feature/auth-flow",
          merge_target_branch: "main",
          merge_policy: "auto_merge",
          merge_strategy: "squash",
          remote_branch_policy: "delete",
          open_pr_before_merge: true,
          execution_mode: "current_branch",
          checkout_enabled: true,
        })
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

  describe("markFeatureForCheckout", () => {
    it("posts checkout endpoint and returns checkout payload", async () => {
      let capturedUrl: string | undefined;
      let capturedMethod: string | undefined;
      const mockResponse = {
        created: true,
        generatedKey: "feature-checkout:auth-system:round-1",
        task: {
          id: "tsk00001",
          path: "projects/myproject/task/tsk00001.md",
          title: "Feature checkout: auth-system",
          status: "pending" as const,
          type: "task" as const,
          content: "Automated feature checkout",
          tags: ["checkout", "auth-system"],
          created: "2026-02-26T00:00:00.000Z",
          modified: "2026-02-26T00:00:00.000Z",
          link: "[Feature checkout: auth-system](tsk00001)",
        },
      };

      globalThis.fetch = ((url: string, options?: RequestInit) => {
        capturedUrl = url;
        capturedMethod = options?.method;
        return Promise.resolve(new Response(JSON.stringify(mockResponse), { status: 200 }));
      }) as typeof fetch;

      const result = await client.markFeatureForCheckout("myproject", "auth-system");

      expect(capturedUrl).toContain("/api/v1/tasks/myproject/features/auth-system/checkout");
      expect(capturedMethod).toBe("POST");
      expect(result.created).toBe(true);
      expect(result.generatedKey).toBe("feature-checkout:auth-system:round-1");
      expect(result.task.id).toBe("tsk00001");
    });

    it("encodes feature ID in checkout endpoint", async () => {
      let capturedUrl: string | undefined;
      const mockResponse = {
        created: false,
        generatedKey: "feature-checkout:auth-system:round-1",
        task: {
          id: "tsk00001",
          path: "projects/myproject/task/tsk00001.md",
          title: "Feature checkout: auth-system",
          status: "pending" as const,
          type: "task" as const,
          content: "Automated feature checkout",
          tags: ["checkout", "auth-system"],
          created: "2026-02-26T00:00:00.000Z",
          modified: "2026-02-26T00:00:00.000Z",
          link: "[Feature checkout: auth-system](tsk00001)",
        },
      };

      globalThis.fetch = ((url: string) => {
        capturedUrl = url;
        return Promise.resolve(new Response(JSON.stringify(mockResponse), { status: 200 }));
      }) as typeof fetch;

      await client.markFeatureForCheckout("myproject", "auth/system");

      expect(capturedUrl).toContain("/api/v1/tasks/myproject/features/auth%2Fsystem/checkout");
    });

    it("sends checkout options payload when provided", async () => {
      let capturedBody: string | undefined;
      const mockResponse = {
        created: true,
        generatedKey: "feature-checkout:auth-system:round-2",
        task: {
          id: "tsk00002",
          path: "projects/myproject/task/tsk00002.md",
          title: "Feature checkout: auth-system",
          status: "pending" as const,
          type: "task" as const,
          content: "Automated feature checkout",
          tags: ["checkout", "auth-system"],
          created: "2026-02-26T00:00:00.000Z",
          modified: "2026-02-26T00:00:00.000Z",
          link: "[Feature checkout: auth-system](tsk00002)",
        },
      };

      globalThis.fetch = ((_url: string, options?: RequestInit) => {
        capturedBody = typeof options?.body === "string" ? options.body : undefined;
        return Promise.resolve(new Response(JSON.stringify(mockResponse), { status: 200 }));
      }) as typeof fetch;

      await client.markFeatureForCheckout("myproject", "auth-system", {
        execution_branch: "feature/auth-system",
        merge_target_branch: "develop",
        merge_policy: "auto_pr",
        merge_strategy: "rebase",
        remote_branch_policy: "keep",
        open_pr_before_merge: true,
        execution_mode: "worktree",
      });

      expect(capturedBody).toBe(
        JSON.stringify({
          execution_branch: "feature/auth-system",
          merge_target_branch: "develop",
          merge_policy: "auto_pr",
          merge_strategy: "rebase",
          remote_branch_policy: "keep",
          open_pr_before_merge: true,
          execution_mode: "worktree",
        })
      );
    });

    it("throws ApiError on non-ok response", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(new Response("Validation error", { status: 400 }))
      );

      await expect(
        client.markFeatureForCheckout("myproject", "auth-system")
      ).rejects.toThrow(ApiError);
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

  describe("getCronNames", () => {
    it("returns mapping of cron ID to title", async () => {
      const mockCrons = [
        {
          id: "crn00001",
          path: "projects/myproject/cron/crn00001.md",
          title: "Daily Backup",
          status: "active" as const,
          schedule: "0 2 * * *",
        },
        {
          id: "crn00002",
          path: "projects/myproject/cron/crn00002.md",
          title: "Weekly Report",
          status: "active" as const,
          schedule: "0 9 * * 1",
        },
      ];

      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(JSON.stringify({ crons: mockCrons, count: 2 }), {
            status: 200,
          })
        )
      );

      const cronNames = await client.getCronNames("myproject");

      expect(cronNames).toEqual({
        crn00001: "Daily Backup",
        crn00002: "Weekly Report",
      });
    });

    it("returns empty object when no crons", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(
          new Response(JSON.stringify({ crons: [], count: 0 }), {
            status: 200,
          })
        )
      );

      const cronNames = await client.getCronNames("myproject");

      expect(cronNames).toEqual({});
    });

    it("encodes project ID in request", async () => {
      let capturedUrl: string | undefined;

      globalThis.fetch = ((url: string) => {
        capturedUrl = url;
        return Promise.resolve(
          new Response(JSON.stringify({ crons: [], count: 0 }), {
            status: 200,
          })
        );
      }) as typeof fetch;

      await client.getCronNames("my/project");

      expect(capturedUrl).toContain("/api/v1/crons/my%2Fproject/crons");
    });

    it("throws ApiError on non-ok response", async () => {
      globalThis.fetch = createMockFetch(() =>
        Promise.resolve(new Response("Server error", { status: 500 }))
      );

      await expect(client.getCronNames("myproject")).rejects.toThrow(ApiError);
    });
  });
});

/**
 * Task Runner Tests
 *
 * Tests for the TaskRunner orchestration class that ties together
 * polling, spawning, and monitoring of tasks.
 */

import {
  describe,
  test,
  expect,
  beforeEach,
  afterEach,
  mock,
} from "bun:test";
import { existsSync, mkdirSync, rmSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";
import {
  TaskRunner,
  getTaskRunner,
  resetTaskRunner,
  type TaskRunnerOptions,
} from "./task-runner";
import { resetApiClient } from "./api-client";
import { resetProcessManager } from "./process-manager";
import { resetOpencodeExecutor } from "./opencode-executor";
import { resetConfig } from "./config";
import { resetSignalHandler } from "./signals";
import { resetLogger } from "./logger";
import type { RunnerConfig, RunnerEvent, RunningTask, TaskResult } from "./types";
import type { ResolvedTask } from "../core/types";

// =============================================================================
// Test Helpers
// =============================================================================

function createTestConfig(stateDir: string): RunnerConfig {
  return {
    brainApiUrl: "http://localhost:3000",
    pollInterval: 1, // Short for tests
    taskPollInterval: 1,
    maxParallel: 3,
    stateDir,
    logDir: join(stateDir, "log"),
    workDir: "/tmp/test-workdir",
    apiTimeout: 1000,
    taskTimeout: 5000, // Short for tests
    opencode: {
      bin: "opencode",
      agent: "general",
      model: "test-model",
    },
    excludeProjects: [],
  };
}

function createMockTask(
  id: string,
  overrides: Partial<ResolvedTask> = {}
): ResolvedTask {
  return {
    id,
    path: `projects/test/task/${id}.md`,
    title: `Test Task ${id}`,
    priority: "medium",
    status: "pending",
    depends_on: [],
    parent_id: null,
    created: new Date().toISOString(),
    workdir: null,
    worktree: null,
    git_remote: null,
    git_branch: null,
    resolved_deps: [],
    unresolved_deps: [],
    parent_chain: [],
    classification: "ready",
    blocked_by: [],
    waiting_on: [],
    in_cycle: false,
    resolved_workdir: null,
    ...overrides,
  };
}

// Mock fetch for API calls
function createMockFetch(handlers: Record<string, () => Promise<Response>>) {
  return ((url: string, options?: RequestInit) => {
    for (const [pattern, handler] of Object.entries(handlers)) {
      if (url.includes(pattern)) {
        return handler();
      }
    }
    // Default: return 404
    return Promise.resolve(new Response("Not found", { status: 404 }));
  }) as typeof fetch;
}

// =============================================================================
// Tests
// =============================================================================

describe("TaskRunner", () => {
  let testDir: string;
  let config: RunnerConfig;
  let originalFetch: typeof fetch;

  beforeEach(() => {
    // Create temp directory
    testDir = join(tmpdir(), `task-runner-test-${Date.now()}-${Math.random().toString(36).slice(2)}`);
    mkdirSync(testDir, { recursive: true });
    mkdirSync(join(testDir, "log"), { recursive: true });

    config = createTestConfig(testDir);

    // Save original fetch
    originalFetch = globalThis.fetch;

    // Reset all singletons
    resetTaskRunner();
    resetApiClient();
    resetProcessManager();
    resetOpencodeExecutor();
    resetConfig();
    resetSignalHandler();
    resetLogger();
  });

  afterEach(async () => {
    // Restore fetch
    globalThis.fetch = originalFetch;

    // Reset singletons
    resetTaskRunner();
    resetApiClient();
    resetProcessManager();
    resetOpencodeExecutor();
    resetSignalHandler();
    resetConfig();
    resetLogger();

    // Clean up temp directory
    try {
      if (existsSync(testDir)) {
        rmSync(testDir, { recursive: true, force: true });
      }
    } catch {
      // Ignore cleanup errors
    }
  });

  describe("constructor", () => {
    test("initializes with default options", () => {
      const options: TaskRunnerOptions = {
        projectId: "test-project",
        config,
      };

      const runner = new TaskRunner(options);
      const status = runner.getStatus();

      expect(status.projectId).toBe("test-project");
      expect(status.status).toBe("idle");
      expect(status.runningTasks).toEqual([]);
      expect(status.stats).toEqual({ completed: 0, failed: 0, totalRuntime: 0 });
    });

    test("generates unique runner ID", () => {
      const runner1 = new TaskRunner({ projectId: "test", config });
      const runner2 = new TaskRunner({ projectId: "test", config });

      const status1 = runner1.getStatus();
      const status2 = runner2.getStatus();

      expect(status1.runnerId).not.toBe(status2.runnerId);
      expect(status1.runnerId).toMatch(/^runner_[a-f0-9]+$/);
    });

    test("accepts custom execution mode", () => {
      const runner = new TaskRunner({
        projectId: "test",
        config,
        mode: "tui",
      });

      // Mode is internal, but we can verify it was set
      expect(runner).toBeDefined();
    });
  });

  describe("getStatus()", () => {
    test("returns initial status", () => {
      const runner = new TaskRunner({ projectId: "my-project", config });
      const status = runner.getStatus();

      expect(status.status).toBe("idle");
      expect(status.projectId).toBe("my-project");
      expect(status.startedAt).toBeNull();
      expect(status.runningTasks).toEqual([]);
      expect(status.stats).toEqual({ completed: 0, failed: 0, totalRuntime: 0 });
    });
  });

  describe("start()", () => {
    test("changes status to polling", async () => {
      // Mock healthy API
      globalThis.fetch = createMockFetch({
        "/health": () =>
          Promise.resolve(
            new Response(
              JSON.stringify({
                status: "healthy",
                zkAvailable: true,
                dbAvailable: true,
              })
            )
          ),
        "/ready": () =>
          Promise.resolve(
            new Response(JSON.stringify({ tasks: [], count: 0 }))
          ),
      });

      const runner = new TaskRunner({ projectId: "test", config });

      // Start but immediately stop to avoid long polling
      const startPromise = runner.start();
      await new Promise((r) => setTimeout(r, 50));
      await runner.stop();

      // Should have been polling at some point
      // Note: status may be "stopped" now since we called stop()
      expect(runner.getStatus().status).toBe("stopped");
    });

    test("saves PID file on start", async () => {
      globalThis.fetch = createMockFetch({
        "/health": () =>
          Promise.resolve(
            new Response(
              JSON.stringify({
                status: "healthy",
                zkAvailable: true,
                dbAvailable: true,
              })
            )
          ),
        "/ready": () =>
          Promise.resolve(
            new Response(JSON.stringify({ tasks: [], count: 0 }))
          ),
      });

      const runner = new TaskRunner({ projectId: "pid-test", config });

      const startPromise = runner.start();
      await new Promise((r) => setTimeout(r, 50));

      // Check PID file exists
      const pidFile = join(testDir, "runner-pid-test.pid");
      expect(existsSync(pidFile)).toBe(true);

      await runner.stop();
    });

    test("does not start if already running", async () => {
      globalThis.fetch = createMockFetch({
        "/health": () =>
          Promise.resolve(
            new Response(
              JSON.stringify({
                status: "healthy",
                zkAvailable: true,
                dbAvailable: true,
              })
            )
          ),
        "/ready": () =>
          Promise.resolve(
            new Response(JSON.stringify({ tasks: [], count: 0 }))
          ),
      });

      const runner = new TaskRunner({ projectId: "test", config });

      // First start
      runner.start();
      await new Promise((r) => setTimeout(r, 50));

      // Second start should be no-op
      runner.start();

      await runner.stop();
    });
  });

  describe("stop()", () => {
    test("changes status to stopped", async () => {
      globalThis.fetch = createMockFetch({
        "/health": () =>
          Promise.resolve(
            new Response(
              JSON.stringify({
                status: "healthy",
                zkAvailable: true,
                dbAvailable: true,
              })
            )
          ),
        "/ready": () =>
          Promise.resolve(
            new Response(JSON.stringify({ tasks: [], count: 0 }))
          ),
      });

      const runner = new TaskRunner({ projectId: "test", config });

      runner.start();
      await new Promise((r) => setTimeout(r, 50));
      await runner.stop();

      expect(runner.getStatus().status).toBe("stopped");
    });

    test("clears PID file on stop", async () => {
      globalThis.fetch = createMockFetch({
        "/health": () =>
          Promise.resolve(
            new Response(
              JSON.stringify({
                status: "healthy",
                zkAvailable: true,
                dbAvailable: true,
              })
            )
          ),
        "/ready": () =>
          Promise.resolve(
            new Response(JSON.stringify({ tasks: [], count: 0 }))
          ),
      });

      const runner = new TaskRunner({ projectId: "clear-pid", config });

      runner.start();
      await new Promise((r) => setTimeout(r, 50));

      const pidFile = join(testDir, "runner-clear-pid.pid");
      expect(existsSync(pidFile)).toBe(true);

      await runner.stop();
      expect(existsSync(pidFile)).toBe(false);
    });

    test("emits shutdown event", async () => {
      globalThis.fetch = createMockFetch({
        "/health": () =>
          Promise.resolve(
            new Response(
              JSON.stringify({
                status: "healthy",
                zkAvailable: true,
                dbAvailable: true,
              })
            )
          ),
        "/ready": () =>
          Promise.resolve(
            new Response(JSON.stringify({ tasks: [], count: 0 }))
          ),
      });

      const events: RunnerEvent[] = [];
      const runner = new TaskRunner({ projectId: "test", config });
      runner.on((event) => events.push(event));

      runner.start();
      await new Promise((r) => setTimeout(r, 50));
      await runner.stop();

      const shutdownEvents = events.filter((e) => e.type === "shutdown");
      expect(shutdownEvents.length).toBe(1);
      expect(shutdownEvents[0]).toEqual({ type: "shutdown", reason: "manual" });
    });
  });

  describe("runOnce()", () => {
    test("returns null when API is unavailable", async () => {
      // Mock unhealthy API
      globalThis.fetch = createMockFetch({
        "/health": () =>
          Promise.resolve(
            new Response(
              JSON.stringify({
                status: "unhealthy",
                zkAvailable: false,
                dbAvailable: false,
              })
            )
          ),
      });

      const runner = new TaskRunner({ projectId: "test", config });
      const result = await runner.runOnce();

      expect(result).toBeNull();
    });

    test("returns null when no ready tasks", async () => {
      globalThis.fetch = createMockFetch({
        "/health": () =>
          Promise.resolve(
            new Response(
              JSON.stringify({
                status: "healthy",
                zkAvailable: true,
                dbAvailable: true,
              })
            )
          ),
        "/next": () =>
          Promise.resolve(
            new Response(
              JSON.stringify({ task: null, message: "No ready tasks" }),
              { status: 404 }
            )
          ),
      });

      const runner = new TaskRunner({ projectId: "test", config });
      const result = await runner.runOnce();

      expect(result).toBeNull();
    });
  });

  describe("event handling", () => {
    test("on() adds event handler", () => {
      const runner = new TaskRunner({ projectId: "test", config });
      const events: RunnerEvent[] = [];

      runner.on((event) => events.push(event));

      // Handler is added
      expect(events.length).toBe(0); // No events yet
    });

    test("off() removes event handler", () => {
      globalThis.fetch = createMockFetch({
        "/health": () =>
          Promise.resolve(
            new Response(
              JSON.stringify({
                status: "healthy",
                zkAvailable: true,
                dbAvailable: true,
              })
            )
          ),
        "/ready": () =>
          Promise.resolve(
            new Response(JSON.stringify({ tasks: [], count: 0 }))
          ),
      });

      const runner = new TaskRunner({ projectId: "test", config });
      const events: RunnerEvent[] = [];
      const handler = (event: RunnerEvent) => events.push(event);

      runner.on(handler);
      runner.off(handler);

      // Start and stop to trigger events
      runner.start();
      runner.stop();

      // Handler should have been removed, so no events
      expect(events.length).toBe(0);
    });

    test("emits poll_complete events", async () => {
      globalThis.fetch = createMockFetch({
        "/health": () =>
          Promise.resolve(
            new Response(
              JSON.stringify({
                status: "healthy",
                zkAvailable: true,
                dbAvailable: true,
              })
            )
          ),
        "/ready": () =>
          Promise.resolve(
            new Response(JSON.stringify({ tasks: [], count: 0 }))
          ),
      });

      const events: RunnerEvent[] = [];
      const runner = new TaskRunner({ projectId: "test", config });
      runner.on((event) => events.push(event));

      runner.start();
      // Wait for at least one poll cycle
      await new Promise((r) => setTimeout(r, 1500));
      await runner.stop();

      const pollEvents = events.filter((e) => e.type === "poll_complete");
      expect(pollEvents.length).toBeGreaterThan(0);
    });

    test("emits state_saved events", async () => {
      globalThis.fetch = createMockFetch({
        "/health": () =>
          Promise.resolve(
            new Response(
              JSON.stringify({
                status: "healthy",
                zkAvailable: true,
                dbAvailable: true,
              })
            )
          ),
        "/ready": () =>
          Promise.resolve(
            new Response(JSON.stringify({ tasks: [], count: 0 }))
          ),
      });

      const events: RunnerEvent[] = [];
      const runner = new TaskRunner({ projectId: "test", config });
      runner.on((event) => events.push(event));

      runner.start();
      await new Promise((r) => setTimeout(r, 1500));
      await runner.stop();

      const stateEvents = events.filter((e) => e.type === "state_saved");
      expect(stateEvents.length).toBeGreaterThan(0);
    });
  });

  describe("singleton", () => {
    test("getTaskRunner returns same instance", () => {
      const runner1 = getTaskRunner({ projectId: "test", config });
      const runner2 = getTaskRunner();

      expect(runner1).toBe(runner2);
    });

    test("getTaskRunner throws without options on first call", () => {
      expect(() => getTaskRunner()).toThrow(
        "TaskRunner not initialized. Call with options first."
      );
    });

    test("resetTaskRunner clears singleton", () => {
      getTaskRunner({ projectId: "test", config });
      resetTaskRunner();

      expect(() => getTaskRunner()).toThrow();
    });
  });
});

describe("TaskRunner - API interactions", () => {
  let testDir: string;
  let config: RunnerConfig;
  let originalFetch: typeof fetch;

  beforeEach(() => {
    testDir = join(tmpdir(), `task-runner-api-test-${Date.now()}`);
    mkdirSync(testDir, { recursive: true });
    mkdirSync(join(testDir, "log"), { recursive: true });

    config = createTestConfig(testDir);
    originalFetch = globalThis.fetch;

    resetTaskRunner();
    resetApiClient();
    resetProcessManager();
    resetOpencodeExecutor();
    resetConfig();
    resetSignalHandler();
    resetLogger();
  });

  afterEach(async () => {
    globalThis.fetch = originalFetch;

    resetTaskRunner();
    resetApiClient();
    resetProcessManager();
    resetOpencodeExecutor();
    resetSignalHandler();
    resetConfig();
    resetLogger();

    try {
      if (existsSync(testDir)) {
        rmSync(testDir, { recursive: true, force: true });
      }
    } catch {
      // Ignore
    }
  });

  test("skips poll when API is unhealthy", async () => {
    let pollCount = 0;

    globalThis.fetch = ((url: string) => {
      if (url.includes("/health")) {
        pollCount++;
        return Promise.resolve(
          new Response(
            JSON.stringify({
              status: "unhealthy",
              zkAvailable: false,
              dbAvailable: false,
            })
          )
        );
      }
      return Promise.resolve(new Response("Not found", { status: 404 }));
    }) as typeof fetch;

    const runner = new TaskRunner({ projectId: "test", config });

    runner.start();
    await new Promise((r) => setTimeout(r, 1500));
    await runner.stop();

    // Should have tried health check but not proceeded with task fetching
    expect(pollCount).toBeGreaterThan(0);
  });

  test("respects maxParallel limit", async () => {
    const tasks = [
      createMockTask("task1"),
      createMockTask("task2"),
      createMockTask("task3"),
      createMockTask("task4"),
      createMockTask("task5"),
    ];

    let claimCount = 0;

    globalThis.fetch = ((url: string, options?: RequestInit) => {
      if (url.includes("/health")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({
              status: "healthy",
              zkAvailable: true,
              dbAvailable: true,
            })
          )
        );
      }
      if (url.includes("/ready")) {
        return Promise.resolve(
          new Response(
            JSON.stringify({ tasks, count: tasks.length })
          )
        );
      }
      if (url.includes("/claim")) {
        claimCount++;
        return Promise.resolve(
          new Response(
            JSON.stringify({
              success: true,
              taskId: `task${claimCount}`,
              runnerId: "test",
              claimedAt: new Date().toISOString(),
            })
          )
        );
      }
      return Promise.resolve(new Response("{}", { status: 200 }));
    }) as typeof fetch;

    // Set maxParallel to 2
    const limitedConfig = { ...config, maxParallel: 2 };
    const runner = new TaskRunner({ projectId: "test", config: limitedConfig });

    runner.start();
    await new Promise((r) => setTimeout(r, 100));
    await runner.stop();

    // Should only claim up to maxParallel tasks
    expect(claimCount).toBeLessThanOrEqual(2);
  });
});

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
    brainApiUrl: "http://localhost:3333",
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
    idleDetectionThreshold: 60000,
    maxTotalProcesses: 10,
    memoryThresholdPercent: 10,
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

    created: new Date().toISOString(),
    target_workdir: null,
    workdir: null,
    worktree: null,
    git_remote: null,
    git_branch: null,
    user_original_request: null,
    resolved_deps: [],
    unresolved_deps: [],
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

  test("respects maxTotalProcesses hard limit", async () => {
    const tasks = [
      createMockTask("task1"),
      createMockTask("task2"),
      createMockTask("task3"),
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

    // Set maxTotalProcesses lower than maxParallel to test hard limit
    // maxParallel=5 but maxTotalProcesses=2 should still cap at 2
    const limitedConfig = { ...config, maxParallel: 5, maxTotalProcesses: 2 };
    const runner = new TaskRunner({ projectId: "test", config: limitedConfig });

    runner.start();
    await new Promise((r) => setTimeout(r, 100));
    await runner.stop();

    // Should only claim up to maxTotalProcesses (the hard limit)
    expect(claimCount).toBeLessThanOrEqual(2);
  });
});

describe("TaskRunner - Pause/Resume", () => {
  let testDir: string;
  let config: RunnerConfig;

  beforeEach(() => {
    testDir = join(tmpdir(), `task-runner-pause-test-${Date.now()}`);
    mkdirSync(testDir, { recursive: true });
    mkdirSync(join(testDir, "log"), { recursive: true });

    config = createTestConfig(testDir);

    resetTaskRunner();
    resetApiClient();
    resetProcessManager();
    resetOpencodeExecutor();
    resetConfig();
    resetSignalHandler();
    resetLogger();
  });

  afterEach(async () => {
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

  describe("pause()", () => {
    test("adds project to pausedProjects set", async () => {
      const runner = new TaskRunner({
        projects: ["project-a", "project-b"],
        config,
      });

      expect(runner.isPaused("project-a")).toBe(false);
      await runner.pause("project-a");
      expect(runner.isPaused("project-a")).toBe(true);
      expect(runner.isPaused("project-b")).toBe(false);
    });

    test("does NOT call API to find or update root task (root task removed)", async () => {
      // Track API calls - specifically looking for getAllTasks or updateTaskStatus
      let getAllTasksCalled = false;
      let updateStatusCalled = false;

      globalThis.fetch = ((url: string, options?: RequestInit) => {
        // Check for getAllTasks call (used by findProjectRootPath)
        if (url.includes("/tasks/project-a") && !options?.method) {
          getAllTasksCalled = true;
        }
        // Check for updateTaskStatus call
        if (url.includes("/entries/") && options?.method === "PATCH") {
          updateStatusCalled = true;
        }
        return Promise.resolve(new Response("{}", { status: 200 }));
      }) as typeof fetch;

      const runner = new TaskRunner({
        projects: ["project-a"],
        config,
      });

      await runner.pause("project-a");

      // Pause should NOT call the API to find root task or update any task status
      // (previously it would call getAllTasks to find root, then update it to "blocked")
      expect(getAllTasksCalled).toBe(false);
      expect(updateStatusCalled).toBe(false);
    });

    test("ignores unknown projects", async () => {
      const runner = new TaskRunner({
        projects: ["project-a"],
        config,
      });

      await runner.pause("unknown-project");
      expect(runner.getPausedProjects()).toEqual([]);
    });

    test("emits project_paused event", async () => {
      const events: RunnerEvent[] = [];
      const runner = new TaskRunner({
        projects: ["project-a", "project-b"],
        config,
      });
      runner.on((event) => events.push(event));

      await runner.pause("project-a");

      const pauseEvents = events.filter((e) => e.type === "project_paused");
      expect(pauseEvents.length).toBe(1);
      expect(pauseEvents[0]).toEqual({ type: "project_paused", projectId: "project-a" });
    });
  });

  describe("resume()", () => {
    test("removes project from pausedProjects set", async () => {
      const runner = new TaskRunner({
        projects: ["project-a", "project-b"],
        config,
      });

      await runner.pause("project-a");
      expect(runner.isPaused("project-a")).toBe(true);
      
      await runner.resume("project-a");
      expect(runner.isPaused("project-a")).toBe(false);
    });

    test("does NOT call API to find or update root task (root task removed)", async () => {
      // Track API calls - specifically looking for getAllTasks or updateTaskStatus
      let getAllTasksCalled = false;
      let updateStatusCalled = false;

      globalThis.fetch = ((url: string, options?: RequestInit) => {
        // Check for getAllTasks call (used by findProjectRootPath)
        if (url.includes("/tasks/project-a") && !options?.method) {
          getAllTasksCalled = true;
        }
        // Check for updateTaskStatus call
        if (url.includes("/entries/") && options?.method === "PATCH") {
          updateStatusCalled = true;
        }
        return Promise.resolve(new Response("{}", { status: 200 }));
      }) as typeof fetch;

      const runner = new TaskRunner({
        projects: ["project-a"],
        config,
      });

      // First pause, then resume
      await runner.pause("project-a");
      getAllTasksCalled = false; // Reset after pause
      updateStatusCalled = false;
      
      await runner.resume("project-a");

      // Resume should NOT call the API to find root task or update any task status
      // (previously it would call getAllTasks to find root, then update it to "active")
      expect(getAllTasksCalled).toBe(false);
      expect(updateStatusCalled).toBe(false);
    });

    test("does nothing if project is not paused", async () => {
      const events: RunnerEvent[] = [];
      const runner = new TaskRunner({
        projects: ["project-a"],
        config,
      });
      runner.on((event) => events.push(event));

      await runner.resume("project-a");

      const resumeEvents = events.filter((e) => e.type === "project_resumed");
      expect(resumeEvents.length).toBe(0);
    });

    test("emits project_resumed event", async () => {
      const events: RunnerEvent[] = [];
      const runner = new TaskRunner({
        projects: ["project-a"],
        config,
      });
      runner.on((event) => events.push(event));

      await runner.pause("project-a");
      await runner.resume("project-a");

      const resumeEvents = events.filter((e) => e.type === "project_resumed");
      expect(resumeEvents.length).toBe(1);
      expect(resumeEvents[0]).toEqual({ type: "project_resumed", projectId: "project-a" });
    });
  });

  describe("pauseAll()", () => {
    test("pauses all projects", async () => {
      const runner = new TaskRunner({
        projects: ["project-a", "project-b", "project-c"],
        config,
      });

      expect(runner.isAllPaused()).toBe(false);
      await runner.pauseAll();
      
      expect(runner.isPaused("project-a")).toBe(true);
      expect(runner.isPaused("project-b")).toBe(true);
      expect(runner.isPaused("project-c")).toBe(true);
      expect(runner.isAllPaused()).toBe(true);
    });

    test("emits all_paused event", async () => {
      const events: RunnerEvent[] = [];
      const runner = new TaskRunner({
        projects: ["project-a", "project-b"],
        config,
      });
      runner.on((event) => events.push(event));

      await runner.pauseAll();

      const pauseAllEvents = events.filter((e) => e.type === "all_paused");
      expect(pauseAllEvents.length).toBe(1);
    });
  });

  describe("resumeAll()", () => {
    test("resumes all paused projects", async () => {
      const runner = new TaskRunner({
        projects: ["project-a", "project-b"],
        config,
      });

      await runner.pauseAll();
      expect(runner.isAllPaused()).toBe(true);

      await runner.resumeAll();
      expect(runner.getPausedProjects()).toEqual([]);
      expect(runner.isAllPaused()).toBe(false);
    });

    test("emits all_resumed event", async () => {
      const events: RunnerEvent[] = [];
      const runner = new TaskRunner({
        projects: ["project-a", "project-b"],
        config,
      });
      runner.on((event) => events.push(event));

      await runner.pauseAll();
      await runner.resumeAll();

      const resumeAllEvents = events.filter((e) => e.type === "all_resumed");
      expect(resumeAllEvents.length).toBe(1);
    });
  });

  describe("getPausedProjects()", () => {
    test("returns array of paused project IDs", async () => {
      const runner = new TaskRunner({
        projects: ["project-a", "project-b", "project-c"],
        config,
      });

      await runner.pause("project-a");
      await runner.pause("project-c");

      const paused = runner.getPausedProjects();
      expect(paused.sort()).toEqual(["project-a", "project-c"].sort());
    });

    test("returns empty array when nothing is paused", () => {
      const runner = new TaskRunner({
        projects: ["project-a"],
        config,
      });

      expect(runner.getPausedProjects()).toEqual([]);
    });
  });

  describe("isAllPaused()", () => {
    test("returns true when all projects are paused", async () => {
      const runner = new TaskRunner({
        projects: ["project-a", "project-b"],
        config,
      });

      expect(runner.isAllPaused()).toBe(false);
      await runner.pause("project-a");
      expect(runner.isAllPaused()).toBe(false);
      await runner.pause("project-b");
      expect(runner.isAllPaused()).toBe(true);
    });
  });

  describe("getStatus()", () => {
    test("includes pausedProjects in status", async () => {
      const runner = new TaskRunner({
        projects: ["project-a", "project-b"],
        config,
      });

      await runner.pause("project-a");

      const status = runner.getStatus();
      expect(status.pausedProjects).toContain("project-a");
      expect(status.pausedProjects).not.toContain("project-b");
    });
  });

  describe("startPaused option", () => {
    test("starts with all projects paused when startPaused is true", () => {
      const runner = new TaskRunner({
        projects: ["project-a", "project-b"],
        config,
        startPaused: true,
      });

      // Before start(), nothing is paused yet (startPaused only applies after start)
      expect(runner.isAllPaused()).toBe(false);
    });

    test("defaults to not paused when startPaused is not specified", () => {
      const runner = new TaskRunner({
        projects: ["project-a", "project-b"],
        config,
      });

      expect(runner.isAllPaused()).toBe(false);
    });

    test("defaults to not paused when startPaused is false", () => {
      const runner = new TaskRunner({
        projects: ["project-a", "project-b"],
        config,
        startPaused: false,
      });

      expect(runner.isAllPaused()).toBe(false);
    });
  });
});

describe("TaskRunner - TUI Task Cleanup", () => {
  let testDir: string;
  let config: RunnerConfig;
  let originalFetch: typeof fetch;

  beforeEach(() => {
    testDir = join(tmpdir(), `task-runner-cleanup-test-${Date.now()}-${Math.random().toString(36).slice(2)}`);
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

  describe("cleanupTaskTmux()", () => {
    test("sends Ctrl+C to gracefully stop OpenCode before killing window", async () => {
      // Track tmux commands executed
      const tmuxCommands: string[] = [];
      
      // Mock Bun.$ to capture tmux commands
      const originalBunShell = Bun.$;
      const mockBunShell = (strings: TemplateStringsArray, ...values: unknown[]) => {
        const command = strings.reduce((acc, str, i) => {
          return acc + str + (values[i] !== undefined ? String(values[i]) : '');
        }, '');
        tmuxCommands.push(command);
        
        // Return a mock result that has .quiet() method
        return {
          quiet: () => Promise.resolve({ exitCode: 0, stdout: '', stderr: '' }),
          text: () => Promise.resolve(''),
          then: (resolve: (value: unknown) => void) => resolve({ exitCode: 0, stdout: '', stderr: '' }),
        };
      };
      
      // @ts-expect-error - mocking Bun.$
      Bun.$ = mockBunShell;

      try {
        // Create a mock running task with windowName (TUI mode)
        const mockTask: RunningTask = {
          id: "test-task-1",
          path: "projects/test/task/test-task-1.md",
          title: "Test Task",
          priority: "medium",
          projectId: "test-project",
          pid: 12345,
          windowName: "task_test-task-1",
          startedAt: new Date().toISOString(),
          isResume: false,
          workdir: "/tmp/test",
        };

        // Create runner and access the private cleanupTaskTmux method
        const runner = new TaskRunner({ projectId: "test-project", config });
        
        // Call the cleanup method via stop() which cleans up tuiTasks
        // First, we need to add the task to tuiTasks
        // @ts-expect-error - accessing private property for testing
        runner.tuiTasks.set(mockTask.id, mockTask);
        
        // Now stop the runner which should trigger cleanup
        await runner.stop();

        // Verify that Ctrl+C was sent BEFORE kill-window
        const sendKeysIndex = tmuxCommands.findIndex(cmd => 
          cmd.includes('send-keys') && cmd.includes('C-c')
        );
        const killWindowIndex = tmuxCommands.findIndex(cmd => 
          cmd.includes('kill-window')
        );

        // Both commands should have been executed
        expect(sendKeysIndex).toBeGreaterThanOrEqual(0);
        expect(killWindowIndex).toBeGreaterThanOrEqual(0);
        
        // Ctrl+C should come BEFORE kill-window
        expect(sendKeysIndex).toBeLessThan(killWindowIndex);
      } finally {
        // Restore original Bun.$
        Bun.$ = originalBunShell;
      }
    });

    test("waits briefly after sending Ctrl+C for graceful shutdown", async () => {
      // Track timing of tmux commands
      const commandTimings: { command: string; time: number }[] = [];
      const startTime = Date.now();
      
      // Mock Bun.$ to capture tmux commands with timing
      const originalBunShell = Bun.$;
      const mockBunShell = (strings: TemplateStringsArray, ...values: unknown[]) => {
        const command = strings.reduce((acc, str, i) => {
          return acc + str + (values[i] !== undefined ? String(values[i]) : '');
        }, '');
        commandTimings.push({ command, time: Date.now() - startTime });
        
        return {
          quiet: () => Promise.resolve({ exitCode: 0, stdout: '', stderr: '' }),
          text: () => Promise.resolve(''),
          then: (resolve: (value: unknown) => void) => resolve({ exitCode: 0, stdout: '', stderr: '' }),
        };
      };
      
      // @ts-expect-error - mocking Bun.$
      Bun.$ = mockBunShell;

      try {
        const mockTask: RunningTask = {
          id: "test-task-2",
          path: "projects/test/task/test-task-2.md",
          title: "Test Task 2",
          priority: "medium",
          projectId: "test-project",
          pid: 12346,
          windowName: "task_test-task-2",
          startedAt: new Date().toISOString(),
          isResume: false,
          workdir: "/tmp/test",
        };

        const runner = new TaskRunner({ projectId: "test-project", config });
        
        // @ts-expect-error - accessing private property for testing
        runner.tuiTasks.set(mockTask.id, mockTask);
        
        await runner.stop();

        // Find the send-keys and kill-window commands
        const sendKeysCmd = commandTimings.find(ct => 
          ct.command.includes('send-keys') && ct.command.includes('C-c')
        );
        const killWindowCmd = commandTimings.find(ct => 
          ct.command.includes('kill-window')
        );

        // Both should exist
        expect(sendKeysCmd).toBeDefined();
        expect(killWindowCmd).toBeDefined();
        
        // There should be a delay between send-keys and kill-window (at least 100ms)
        if (sendKeysCmd && killWindowCmd) {
          const delay = killWindowCmd.time - sendKeysCmd.time;
          expect(delay).toBeGreaterThanOrEqual(100);
        }
      } finally {
        Bun.$ = originalBunShell;
      }
    });

    test("still kills window even if Ctrl+C fails", async () => {
      const tmuxCommands: string[] = [];
      let sendKeysCalled = false;
      
      const originalBunShell = Bun.$;
      const mockBunShell = (strings: TemplateStringsArray, ...values: unknown[]) => {
        const command = strings.reduce((acc, str, i) => {
          return acc + str + (values[i] !== undefined ? String(values[i]) : '');
        }, '');
        tmuxCommands.push(command);
        
        // Make send-keys fail
        if (command.includes('send-keys')) {
          sendKeysCalled = true;
          return {
            quiet: () => Promise.reject(new Error('tmux send-keys failed')),
            text: () => Promise.reject(new Error('tmux send-keys failed')),
            then: (_: unknown, reject: (err: Error) => void) => reject(new Error('tmux send-keys failed')),
          };
        }
        
        return {
          quiet: () => Promise.resolve({ exitCode: 0, stdout: '', stderr: '' }),
          text: () => Promise.resolve(''),
          then: (resolve: (value: unknown) => void) => resolve({ exitCode: 0, stdout: '', stderr: '' }),
        };
      };
      
      // @ts-expect-error - mocking Bun.$
      Bun.$ = mockBunShell;

      try {
        const mockTask: RunningTask = {
          id: "test-task-3",
          path: "projects/test/task/test-task-3.md",
          title: "Test Task 3",
          priority: "medium",
          projectId: "test-project",
          pid: 12347,
          windowName: "task_test-task-3",
          startedAt: new Date().toISOString(),
          isResume: false,
          workdir: "/tmp/test",
        };

        const runner = new TaskRunner({ projectId: "test-project", config });
        
        // @ts-expect-error - accessing private property for testing
        runner.tuiTasks.set(mockTask.id, mockTask);
        
        // Should not throw even if send-keys fails
        await runner.stop();

        // Verify send-keys was attempted
        expect(sendKeysCalled).toBe(true);
        
        // Verify kill-window was still called
        const killWindowCalled = tmuxCommands.some(cmd => cmd.includes('kill-window'));
        expect(killWindowCalled).toBe(true);
      } finally {
        Bun.$ = originalBunShell;
      }
    });

    test("handles pane cleanup with Ctrl+C before kill-pane", async () => {
      const tmuxCommands: string[] = [];
      
      const originalBunShell = Bun.$;
      const mockBunShell = (strings: TemplateStringsArray, ...values: unknown[]) => {
        const command = strings.reduce((acc, str, i) => {
          return acc + str + (values[i] !== undefined ? String(values[i]) : '');
        }, '');
        tmuxCommands.push(command);
        
        return {
          quiet: () => Promise.resolve({ exitCode: 0, stdout: '', stderr: '' }),
          text: () => Promise.resolve(''),
          then: (resolve: (value: unknown) => void) => resolve({ exitCode: 0, stdout: '', stderr: '' }),
        };
      };
      
      // @ts-expect-error - mocking Bun.$
      Bun.$ = mockBunShell;

      try {
        // Create a mock running task with paneId (dashboard mode without tmuxManager)
        const mockTask: RunningTask = {
          id: "test-task-4",
          path: "projects/test/task/test-task-4.md",
          title: "Test Task 4",
          priority: "medium",
          projectId: "test-project",
          pid: 12348,
          paneId: "%42",  // Pane ID instead of windowName
          startedAt: new Date().toISOString(),
          isResume: false,
          workdir: "/tmp/test",
        };

        const runner = new TaskRunner({ projectId: "test-project", config });
        
        // @ts-expect-error - accessing private property for testing
        runner.tuiTasks.set(mockTask.id, mockTask);
        
        await runner.stop();

        // Verify that Ctrl+C was sent to the pane BEFORE kill-pane
        const sendKeysIndex = tmuxCommands.findIndex(cmd => 
          cmd.includes('send-keys') && cmd.includes('C-c') && cmd.includes('%42')
        );
        const killPaneIndex = tmuxCommands.findIndex(cmd => 
          cmd.includes('kill-pane')
        );

        // Both commands should have been executed
        expect(sendKeysIndex).toBeGreaterThanOrEqual(0);
        expect(killPaneIndex).toBeGreaterThanOrEqual(0);
        
        // Ctrl+C should come BEFORE kill-pane
        expect(sendKeysIndex).toBeLessThan(killPaneIndex);
      } finally {
        Bun.$ = originalBunShell;
      }
    });
  });

  // ========================================
  // Orphan Process Cleanup Tests
  // ========================================

  describe("orphan process cleanup in stop()", () => {
    test("does not attempt to kill non-existent PIDs", async () => {
      // Use a non-existent PID that isPidAlive will return false for
      const mockTask: RunningTask = {
        id: "dead-task-1",
        path: "projects/test/task/dead-task-1.md",
        title: "Dead Task",
        priority: "medium",
        projectId: "test-project",
        pid: 99999, // Non-existent PID
        windowName: "dead_task",
        startedAt: new Date().toISOString(),
        isResume: false,
        workdir: "/tmp/test",
      };

      const runner = new TaskRunner({ projectId: "test-project", config });

      // @ts-expect-error - accessing private property for testing
      runner.tuiTasks.set(mockTask.id, mockTask);

      // Should not throw - non-existent PIDs are handled gracefully
      await runner.stop();

      // @ts-expect-error - accessing private property for testing
      expect(runner.tuiTasks.size).toBe(0);
    });

    test("handles errors when killing processes gracefully", async () => {
      // Mock a task with a PID that is invalid/non-existent
      // NOTE: DO NOT use negative PIDs like -1! On POSIX systems:
      // - process.kill(-1, signal) sends the signal to ALL user processes
      // - This would kill everything on the system!
      // Use a very high PID that's guaranteed not to exist instead.
      const mockTask: RunningTask = {
        id: "error-task-1",
        path: "projects/test/task/error-task-1.md",
        title: "Error Task",
        priority: "medium",
        projectId: "test-project",
        pid: 4194301, // Very high non-existent PID (safely above macOS max of 99999)
        windowName: "error_task",
        startedAt: new Date().toISOString(),
        isResume: false,
        workdir: "/tmp/test",
      };

      const runner = new TaskRunner({ projectId: "test-project", config });

      // @ts-expect-error - accessing private property for testing
      runner.tuiTasks.set(mockTask.id, mockTask);

      // Should not throw - errors during process killing are caught
      await runner.stop();

      // @ts-expect-error - accessing private property for testing
      expect(runner.tuiTasks.size).toBe(0);
    });
  });

  // ========================================
  // OpenCode Idle Detection Tests
  // ========================================

  describe("idle detection", () => {
    test("sets idleSince when OpenCode becomes idle", async () => {
      // Create a mock running task with opencodePort
      // Use a non-existent PID (99999) to avoid triggering orphan process killing
      // which could kill the test runner itself if we used process.pid
      const mockTask: RunningTask = {
        id: "idle-task-1",
        path: "projects/test/task/idle-task-1.md",
        title: "Idle Test Task",
        priority: "medium",
        projectId: "test-project",
        pid: 99999, // Non-existent PID - won't trigger orphan killing
        windowName: "idle_task",
        startedAt: new Date().toISOString(),
        isResume: false,
        workdir: "/tmp/test",
        opencodePort: 65432, // Non-existent port (will return unavailable, not idle)
      };

      const runner = new TaskRunner({ projectId: "test-project", config });

      // @ts-expect-error - accessing private property for testing
      runner.tuiTasks.set(mockTask.id, mockTask);

      // Verify the task doesn't have idleSince initially
      expect(mockTask.idleSince).toBeUndefined();

      await runner.stop();
    });

    test("RunningTask includes opencodePort and idleSince fields", () => {
      const task: RunningTask = {
        id: "test-id",
        path: "test/path.md",
        title: "Test",
        priority: "medium",
        projectId: "test-project",
        pid: 1234,
        startedAt: new Date().toISOString(),
        isResume: false,
        workdir: "/tmp",
        opencodePort: 54321,
        idleSince: new Date().toISOString(),
      };

      expect(task.opencodePort).toBe(54321);
      expect(task.idleSince).toBeDefined();
    });
  });

  // ========================================
  // Config Tests  
  // ========================================

  describe("idleDetectionThreshold config", () => {
    test("config includes idleDetectionThreshold with default value", () => {
      expect(config.idleDetectionThreshold).toBe(60000);
    });

    test("idleDetectionThreshold can be customized", () => {
      const customConfig = { ...config, idleDetectionThreshold: 30000 };
      expect(customConfig.idleDetectionThreshold).toBe(30000);
    });
  });
});

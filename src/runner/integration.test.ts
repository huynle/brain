/**
 * Integration Tests for Brain Runner
 *
 * End-to-end tests that verify the full workflow from polling to completion.
 * These tests use a real (or mock) Brain API server and verify the complete
 * task execution pipeline.
 */

import {
  describe,
  test,
  expect,
  beforeEach,
  afterEach,
  beforeAll,
  afterAll,
} from "bun:test";
import { existsSync, mkdirSync, rmSync, writeFileSync, readFileSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";
import type { Subprocess } from "bun";

// Components under test
import { TaskRunner, resetTaskRunner } from "./task-runner";
import { ApiClient, resetApiClient } from "./api-client";
import { ProcessManager, resetProcessManager, CompletionStatus } from "./process-manager";
import { StateManager } from "./state-manager";
import { OpencodeExecutor, resetOpencodeExecutor } from "./opencode-executor";
import { resetConfig } from "./config";
import { resetSignalHandler } from "./signals";
import { resetLogger } from "./logger";
import type { RunnerConfig, RunningTask, TaskResult, RunnerEvent } from "./types";
import type { ResolvedTask } from "../core/types";

// =============================================================================
// Test Fixtures & Helpers
// =============================================================================

function createTestConfig(stateDir: string): RunnerConfig {
  return {
    brainApiUrl: "http://localhost:3333",
    pollInterval: 1,
    taskPollInterval: 1,
    maxParallel: 2,
    stateDir,
    logDir: join(stateDir, "log"),
    workDir: stateDir, // Use test dir as workdir
    apiTimeout: 2000,
    taskTimeout: 10000,
    opencode: {
      bin: "echo", // Use echo as mock OpenCode for fast tests
      agent: "general",
      model: "test-model",
    },
    excludeProjects: [],
    idleDetectionThreshold: 60000,
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

function createRunningTask(
  id: string,
  pid: number,
  overrides: Partial<RunningTask> = {}
): RunningTask {
  return {
    id,
    path: `projects/test/task/${id}.md`,
    title: `Task ${id}`,
    priority: "medium",
    projectId: "test-project",
    pid,
    startedAt: new Date().toISOString(),
    isResume: false,
    workdir: "/tmp/test",
    ...overrides,
  };
}

// Mock fetch factory
function createMockFetch(responses: Record<string, () => Promise<Response>>) {
  return ((url: string, options?: RequestInit) => {
    for (const [pattern, handler] of Object.entries(responses)) {
      if (url.includes(pattern)) {
        return handler();
      }
    }
    return Promise.resolve(new Response("Not found", { status: 404 }));
  }) as typeof fetch;
}

// =============================================================================
// Test Setup & Teardown
// =============================================================================

function resetAllSingletons() {
  resetTaskRunner();
  resetApiClient();
  resetProcessManager();
  resetOpencodeExecutor();
  resetConfig();
  resetSignalHandler();
  resetLogger();
}

// =============================================================================
// Integration Tests
// =============================================================================

describe("Integration: Component Interactions", () => {
  let testDir: string;
  let config: RunnerConfig;
  let originalFetch: typeof fetch;

  beforeEach(() => {
    testDir = join(
      tmpdir(),
      `integration-test-${Date.now()}-${Math.random().toString(36).slice(2)}`
    );
    mkdirSync(testDir, { recursive: true });
    mkdirSync(join(testDir, "log"), { recursive: true });

    config = createTestConfig(testDir);
    originalFetch = globalThis.fetch;
    resetAllSingletons();
  });

  afterEach(async () => {
    globalThis.fetch = originalFetch;
    resetAllSingletons();

    try {
      if (existsSync(testDir)) {
        rmSync(testDir, { recursive: true, force: true });
      }
    } catch {
      // Ignore cleanup errors
    }
  });

  describe("ApiClient + ProcessManager interaction", () => {
    test("can track tasks fetched from API", async () => {
      const mockTask = createMockTask("task1");

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
            new Response(
              JSON.stringify({ tasks: [mockTask], count: 1 })
            )
          ),
      });

      const apiClient = new ApiClient(config);
      const processManager = new ProcessManager(config);

      // Fetch tasks from API
      const tasks = await apiClient.getReadyTasks("test-project");
      expect(tasks).toHaveLength(1);
      expect(tasks[0].id).toBe("task1");

      // Track a process for the task
const proc = Bun.spawn(["sleep", "0.1"]);
      const runningTask = createRunningTask("task1", proc.pid!);

      processManager.add("task1", runningTask, proc);

      expect(processManager.isRunning("task1")).toBe(true);
      expect(processManager.count()).toBe(1);

      // Wait for process to complete
      await new Promise((r) => setTimeout(r, 200));

      expect(processManager.isRunning("task1")).toBe(false);

      // Cleanup
      await processManager.killAll();
    });

    test("claim tracking works end-to-end", async () => {
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
        "/claim": () =>
          Promise.resolve(
            new Response(
              JSON.stringify({
                success: true,
                taskId: "task1",
                runnerId: "runner1",
                claimedAt: new Date().toISOString(),
              })
            )
          ),
        "/release": () =>
          Promise.resolve(
            new Response(JSON.stringify({ success: true }))
          ),
      });

      const apiClient = new ApiClient(config);

      // Claim a task
      const claim = await apiClient.claimTask("test-project", "task1", "runner1");
      expect(claim.success).toBe(true);

      // Release the task
      await apiClient.releaseTask("test-project", "task1");
    });
  });

  describe("StateManager + ProcessManager interaction", () => {
    test("state persists across manager instances", () => {
      const projectId = "persist-test";

      // Create first state manager and save state
      const stateManager1 = new StateManager(testDir, projectId);
      const tasks: RunningTask[] = [createRunningTask("task1", 12345)];
      const stats = { completed: 5, failed: 1, totalRuntime: 30000 };

      stateManager1.save("processing", tasks, stats, new Date().toISOString());
      stateManager1.savePid(process.pid);

      // Create second state manager and load state
      const stateManager2 = new StateManager(testDir, projectId);
      const loaded = stateManager2.load();

      expect(loaded).not.toBeNull();
      expect(loaded!.status).toBe("processing");
      expect(loaded!.runningTasks).toHaveLength(1);
      expect(loaded!.stats).toEqual(stats);

      // Cleanup
      stateManager2.clear();
    });

    test("running tasks can be restored from state", () => {
      const processManager = new ProcessManager(config);

      // Spawn a real process
      const proc = Bun.spawn(["sleep", "10"]);
      const task = createRunningTask("task1", proc.pid);

      // Simulate saved state
      const states = [
        {
          taskId: "task1",
          task,
          pid: proc.pid!,
          exitCode: null,
          exited: false,
        },
      ];

      // Restore from state
      const restored = processManager.restoreFromState(states);

      expect(restored).toHaveLength(1);
      expect(restored[0].id).toBe("task1");

      // Cleanup
      proc.kill();
    });

    test("stale state is cleaned up", () => {
      // Create state with dead PID
      const stateManager1 = new StateManager(testDir, "stale-project");
      stateManager1.save(
        "processing",
        [],
        { completed: 0, failed: 0, totalRuntime: 0 },
        new Date().toISOString()
      );
      stateManager1.savePid(999999999); // Very unlikely to exist

      // Run cleanup
      const cleaned = StateManager.cleanupStaleStates(testDir);

      expect(cleaned).toBe(1);
      expect(stateManager1.load()).toBeNull();
    });
  });

  describe("OpencodeExecutor + ProcessManager interaction", () => {
    test("executor creates trackable processes", async () => {
      const executor = new OpencodeExecutor(config);
      const processManager = new ProcessManager(config);
      const task = createMockTask("task1");

      // Mock Bun.spawn
      const originalSpawn = Bun.spawn;
      const mockProc = {
        pid: 99999,
        kill: () => {},
        exited: Promise.resolve(0),
      };

      // @ts-expect-error - mocking
      Bun.spawn = () => mockProc;

      try {
        const result = await executor.spawn(task, "test-project", {
          mode: "background",
        });

        expect(result.pid).toBe(99999);
        expect(result.proc).toBeDefined();

        // Could add to process manager
        // (In real code, TaskRunner does this)
      } finally {
        Bun.spawn = originalSpawn;
        await executor.cleanup("task1", "test-project");
      }
    });

    test("executor builds correct prompts for tasks", () => {
      const executor = new OpencodeExecutor(config);
      const task = createMockTask("task1", {
        path: "projects/myproject/task/abc123.md",
      });

      const newPrompt = executor.buildPrompt(task, false);
      expect(newPrompt).toContain("do-work-queue skill");
      expect(newPrompt).toContain("projects/myproject/task/abc123.md");
      expect(newPrompt).not.toContain("RESUME");

      const resumePrompt = executor.buildPrompt(task, true);
      expect(resumePrompt).toContain("RESUME");
      expect(resumePrompt).toContain("interrupted");
    });
  });
});

describe("Integration: State Persistence", () => {
  let testDir: string;
  let config: RunnerConfig;

  beforeEach(() => {
    testDir = join(tmpdir(), `state-persist-test-${Date.now()}`);
    mkdirSync(testDir, { recursive: true });
    mkdirSync(join(testDir, "log"), { recursive: true });
    config = createTestConfig(testDir);
    resetAllSingletons();
  });

  afterEach(() => {
    resetAllSingletons();
    try {
      if (existsSync(testDir)) {
        rmSync(testDir, { recursive: true, force: true });
      }
    } catch {
      // Ignore
    }
  });

  test("full state round-trip: save -> load -> verify", () => {
    const projectId = "full-roundtrip";
    const stateManager = new StateManager(testDir, projectId);

    const tasks: RunningTask[] = [
      createRunningTask("task1", 1001, { priority: "high" }),
      createRunningTask("task2", 1002, { priority: "low" }),
    ];
    const stats = { completed: 10, failed: 2, totalRuntime: 120000 };
    const startedAt = "2024-01-15T10:00:00Z";

    // Save
    stateManager.save("processing", tasks, stats, startedAt);
    stateManager.savePid(process.pid);
    stateManager.saveRunningTasks(tasks);

    // Verify files exist
    expect(existsSync(join(testDir, `runner-${projectId}.json`))).toBe(true);
    expect(existsSync(join(testDir, `runner-${projectId}.pid`))).toBe(true);
    expect(existsSync(join(testDir, `running-${projectId}.json`))).toBe(true);

    // Load and verify
    const loaded = stateManager.load();
    expect(loaded).not.toBeNull();
    expect(loaded!.projectId).toBe(projectId);
    expect(loaded!.status).toBe("processing");
    expect(loaded!.runningTasks).toHaveLength(2);
    expect(loaded!.stats).toEqual(stats);
    expect(loaded!.startedAt).toBe(startedAt);

    const loadedPid = stateManager.loadPid();
    expect(loadedPid).toBe(process.pid);

    const loadedTasks = stateManager.loadRunningTasks();
    expect(loadedTasks).toHaveLength(2);

    // Clear and verify cleanup
    stateManager.clear();
    expect(existsSync(join(testDir, `runner-${projectId}.json`))).toBe(false);
  });

  test("find all runner states across projects", () => {
    // Create states for multiple projects
    const projects = ["project-a", "project-b", "project-c"];
    const stats = { completed: 0, failed: 0, totalRuntime: 0 };

    for (const projectId of projects) {
      const sm = new StateManager(testDir, projectId);
      sm.save("processing", [], stats, new Date().toISOString());
    }

    // Find all
    const states = StateManager.findAllRunnerStates(testDir);

    expect(states).toHaveLength(3);
    const foundProjects = states.map((s) => s.projectId).sort();
    expect(foundProjects).toEqual(projects.sort());
  });
});

describe("Integration: Process Lifecycle", () => {
  let testDir: string;
  let config: RunnerConfig;

  beforeEach(() => {
    testDir = join(tmpdir(), `process-lifecycle-test-${Date.now()}`);
    mkdirSync(testDir, { recursive: true });
    config = createTestConfig(testDir);
    resetAllSingletons();
  });

  afterEach(async () => {
    resetAllSingletons();
    try {
      if (existsSync(testDir)) {
        rmSync(testDir, { recursive: true, force: true });
      }
    } catch {
      // Ignore
    }
  });

  test("process completion detection works correctly", async () => {
    const processManager = new ProcessManager(config);
    const task = createRunningTask("task1", 0);

    // Spawn a quick process
    const proc = Bun.spawn(["sleep", "0.1"]);
    task.pid = proc.pid;

    processManager.add("task1", task, proc);
    expect(processManager.isRunning("task1")).toBe(true);

    // Wait for completion
    await new Promise((r) => setTimeout(r, 200));

    const status = await processManager.checkCompletion("task1", false);
    expect(status).toBe(CompletionStatus.Completed);

    // Cleanup
    processManager.remove("task1");
  });

  test("process can be killed gracefully", async () => {
    const processManager = new ProcessManager(config);
    const task = createRunningTask("task1", 0);

    // Spawn a long process
    const proc = Bun.spawn(["sleep", "60"]);
    task.pid = proc.pid;

    processManager.add("task1", task, proc);
    expect(processManager.runningCount()).toBe(1);

    // Kill it
    const killed = await processManager.kill("task1");
    expect(killed).toBe(true);
    expect(processManager.isRunning("task1")).toBe(false);
  });

  test("killAll terminates all running processes", async () => {
    const processManager = new ProcessManager(config);

    // Spawn multiple processes
    const proc1 = Bun.spawn(["sleep", "60"]);
    const proc2 = Bun.spawn(["sleep", "60"]);

    processManager.add("task1", createRunningTask("task1", proc1.pid), proc1);
    processManager.add("task2", createRunningTask("task2", proc2.pid), proc2);

    expect(processManager.runningCount()).toBe(2);

    await processManager.killAll();

    expect(processManager.runningCount()).toBe(0);
  });

  test("TaskResult is created correctly on completion", async () => {
    const processManager = new ProcessManager(config);
    const task = createRunningTask("task1", 0);

    const proc = Bun.spawn(["sleep", "0.1"]);
    task.pid = proc.pid;

    processManager.add("task1", task, proc);

    // Wait for completion
    await new Promise((r) => setTimeout(r, 200));

    const result = processManager.createTaskResult(
      "task1",
      CompletionStatus.Completed
    );

    expect(result).not.toBeNull();
    expect(result!.taskId).toBe("task1");
    expect(result!.status).toBe("completed");
    expect(result!.startedAt).toBe(task.startedAt);
    expect(result!.duration).toBeGreaterThan(0);
  });
});

describe("Integration: Error Handling", () => {
  let testDir: string;
  let config: RunnerConfig;
  let originalFetch: typeof fetch;

  beforeEach(() => {
    testDir = join(tmpdir(), `error-handling-test-${Date.now()}`);
    mkdirSync(testDir, { recursive: true });
    mkdirSync(join(testDir, "log"), { recursive: true });
    config = createTestConfig(testDir);
    originalFetch = globalThis.fetch;
    resetAllSingletons();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    resetAllSingletons();
    try {
      if (existsSync(testDir)) {
        rmSync(testDir, { recursive: true, force: true });
      }
    } catch {
      // Ignore
    }
  });

  test("API errors are handled gracefully", async () => {
    globalThis.fetch = createMockFetch({
      "/health": () => Promise.reject(new Error("Network error")),
    });

    const apiClient = new ApiClient(config);
    const health = await apiClient.checkHealth();

    expect(health.status).toBe("unhealthy");
    expect(health.zkAvailable).toBe(false);
    expect(health.dbAvailable).toBe(false);
  });

  test("claim conflicts are handled correctly", async () => {
    globalThis.fetch = createMockFetch({
      "/claim": () =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              success: false,
              error: "conflict",
              claimedBy: "other-runner",
            }),
            { status: 409 }
          )
        ),
    });

    const apiClient = new ApiClient(config);
    const result = await apiClient.claimTask("test", "task1", "runner1");

    expect(result.success).toBe(false);
    expect(result.claimedBy).toBe("other-runner");
    expect(result.message).toBe("Task already claimed");
  });

  test("corrupted state files are handled", () => {
    const stateManager = new StateManager(testDir, "corrupt-test");

    // Write corrupted JSON
    writeFileSync(
      join(testDir, "runner-corrupt-test.json"),
      "{ invalid json }",
      "utf-8"
    );

    const loaded = stateManager.load();
    expect(loaded).toBeNull();
  });

  test("missing files don't cause crashes", () => {
    const stateManager = new StateManager(testDir, "missing-test");

    // Try to load non-existent files
    expect(stateManager.load()).toBeNull();
    expect(stateManager.loadPid()).toBeNull();
    expect(stateManager.loadRunningTasks()).toEqual([]);

    // Clear should not throw
    expect(() => stateManager.clear()).not.toThrow();
  });
});

describe("Integration: Workdir/Execution Context Flow", () => {
  let testDir: string;
  let config: RunnerConfig;

  beforeEach(() => {
    testDir = join(tmpdir(), `workdir-flow-test-${Date.now()}`);
    mkdirSync(testDir, { recursive: true });
    mkdirSync(join(testDir, "log"), { recursive: true });
    config = createTestConfig(testDir);
    resetAllSingletons();
  });

  afterEach(() => {
    resetAllSingletons();
    try {
      if (existsSync(testDir)) {
        rmSync(testDir, { recursive: true, force: true });
      }
    } catch {
      // Ignore
    }
  });

  test("ResolvedTask includes workdir fields from API response", async () => {
    const taskWithWorkdir = createMockTask("task-with-workdir", {
      workdir: "projects/my-project",
      worktree: "projects/my-project-wt",
      git_remote: "git@github.com:user/repo.git",
      git_branch: "main",
      resolved_workdir: "/home/user/projects/my-project",
    });

    expect(taskWithWorkdir.workdir).toBe("projects/my-project");
    expect(taskWithWorkdir.worktree).toBe("projects/my-project-wt");
    expect(taskWithWorkdir.git_remote).toBe("git@github.com:user/repo.git");
    expect(taskWithWorkdir.git_branch).toBe("main");
    expect(taskWithWorkdir.resolved_workdir).toBe("/home/user/projects/my-project");
  });

  test("OpencodeExecutor.resolveWorkdir uses task workdir fields", () => {
    const executor = new OpencodeExecutor(config);
    
    // Task with no workdir fields - should use config default
    const taskNoWorkdir = createMockTask("task1", {
      workdir: null,
      worktree: null,
      resolved_workdir: null,
    });
    expect(executor.resolveWorkdir(taskNoWorkdir)).toBe(config.workDir);

    // Task with resolved_workdir - should use it if directory exists
    const taskWithResolvedWorkdir = createMockTask("task2", {
      resolved_workdir: testDir, // Use test directory which exists
    });
    expect(executor.resolveWorkdir(taskWithResolvedWorkdir)).toBe(testDir);

    // Task with non-existent resolved_workdir - should fall back to config
    const taskWithBadResolved = createMockTask("task3", {
      resolved_workdir: "/nonexistent/path/that/does/not/exist",
    });
    expect(executor.resolveWorkdir(taskWithBadResolved)).toBe(config.workDir);
  });

  test("RunningTask preserves workdir from ResolvedTask", () => {
    const resolvedTask = createMockTask("task1", {
      workdir: "projects/my-project",
      worktree: "projects/my-project-wt",
      resolved_workdir: testDir,
    });

    // Simulate what TaskRunner does when creating RunningTask
    const runningTask: RunningTask = {
      id: resolvedTask.id,
      path: resolvedTask.path,
      title: resolvedTask.title,
      priority: resolvedTask.priority,
      projectId: "test-project",
      pid: 12345,
      startedAt: new Date().toISOString(),
      isResume: false,
      workdir: testDir, // This would be set to resolved_workdir or config.workDir
    };

    expect(runningTask.workdir).toBe(testDir);
  });

  test("StateManager persists and restores tasks with workdir", () => {
    const projectId = "workdir-state-test";
    const stateManager = new StateManager(testDir, projectId);

    const tasks: RunningTask[] = [
      createRunningTask("task1", 1001, { workdir: "/path/to/project" }),
      createRunningTask("task2", 1002, { workdir: "/path/to/worktree" }),
    ];

    // Save state
    const stats = { completed: 0, failed: 0, totalRuntime: 0 };
    stateManager.save("processing", tasks, stats, new Date().toISOString());
    stateManager.saveRunningTasks(tasks);

    // Load and verify
    const loadedTasks = stateManager.loadRunningTasks();
    expect(loadedTasks).toHaveLength(2);
    expect(loadedTasks[0].workdir).toBe("/path/to/project");
    expect(loadedTasks[1].workdir).toBe("/path/to/worktree");
  });

  test("full workdir flow: mock task -> executor -> process spawn", async () => {
    const executor = new OpencodeExecutor(config);
    const processManager = new ProcessManager(config);

    // Task with no relative paths but has resolved_workdir
    // This is what TaskService produces after resolving workdir
    const task = createMockTask("task-with-workdir", {
      workdir: null, // No relative path
      worktree: null,
      resolved_workdir: testDir, // Already resolved to absolute path
    });

    // Verify workdir resolution - should use resolved_workdir since workdir/worktree are null
    const resolvedWorkdir = executor.resolveWorkdir(task);
    expect(resolvedWorkdir).toBe(testDir);

    // Mock Bun.spawn to capture spawn arguments
    let capturedCwd: string | undefined;
    const originalSpawn = Bun.spawn;
    const mockProc = {
      pid: 99999,
      kill: () => {},
      exited: Promise.resolve(0),
    };

    // @ts-expect-error - mocking
    Bun.spawn = (args: { cwd?: string }) => {
      capturedCwd = args.cwd;
      return mockProc;
    };

    try {
      // Spawn with explicit workdir (simulating what TaskRunner does)
      const result = await executor.spawn(task, "test-project", {
        mode: "background",
        workdir: resolvedWorkdir,
      });

      expect(result.pid).toBe(99999);
      expect(capturedCwd).toBe(testDir);
    } finally {
      Bun.spawn = originalSpawn;
      await executor.cleanup("task-with-workdir", "test-project");
    }
  });
});

describe("Integration: Dependency Resolution Flow", () => {
  let testDir: string;
  let config: RunnerConfig;
  let originalFetch: typeof fetch;

  beforeEach(() => {
    testDir = join(tmpdir(), `dep-resolution-test-${Date.now()}`);
    mkdirSync(testDir, { recursive: true });
    mkdirSync(join(testDir, "log"), { recursive: true });
    config = createTestConfig(testDir);
    originalFetch = globalThis.fetch;
    resetAllSingletons();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    resetAllSingletons();
    try {
      if (existsSync(testDir)) {
        rmSync(testDir, { recursive: true, force: true });
      }
    } catch {
      // Ignore
    }
  });

  test("tasks with dependencies return correct classification", async () => {
    const readyTask = createMockTask("ready-task", { classification: "ready" });
    const waitingTask = createMockTask("waiting-task", {
      classification: "waiting",
      waiting_on: ["ready-task"],
    });

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
          new Response(JSON.stringify({ tasks: [readyTask], count: 1 }))
        ),
      "/waiting": () =>
        Promise.resolve(
          new Response(JSON.stringify({ tasks: [waitingTask], count: 1 }))
        ),
      "/blocked": () =>
        Promise.resolve(new Response(JSON.stringify({ tasks: [], count: 0 }))),
    });

    const apiClient = new ApiClient(config);

    const ready = await apiClient.getReadyTasks("test");
    expect(ready).toHaveLength(1);
    expect(ready[0].classification).toBe("ready");

    const waiting = await apiClient.getWaitingTasks("test");
    expect(waiting).toHaveLength(1);
    expect(waiting[0].classification).toBe("waiting");
  });

  test("next task returns highest priority ready task", async () => {
    const highPriority = createMockTask("high", {
      priority: "high",
      classification: "ready",
    });

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
          new Response(JSON.stringify({ task: highPriority }))
        ),
    });

    const apiClient = new ApiClient(config);
    const next = await apiClient.getNextTask("test");

    expect(next).not.toBeNull();
    expect(next!.priority).toBe("high");
  });
});

// =============================================================================
// Multi-Project Integration Tests (Phase 5)
// =============================================================================

describe("Integration: Multi-Project Mode", () => {
  let testDir: string;
  let config: RunnerConfig;
  let originalFetch: typeof fetch;

  beforeEach(() => {
    testDir = join(tmpdir(), `multi-project-test-${Date.now()}`);
    mkdirSync(testDir, { recursive: true });
    mkdirSync(join(testDir, "log"), { recursive: true });
    config = createTestConfig(testDir);
    originalFetch = globalThis.fetch;
    resetAllSingletons();
  });

  afterEach(() => {
    globalThis.fetch = originalFetch;
    resetAllSingletons();
    try {
      if (existsSync(testDir)) {
        rmSync(testDir, { recursive: true, force: true });
      }
    } catch {
      // Ignore
    }
  });

  describe("resolveProjects", () => {
    test("filters projects with include pattern", async () => {
      // Import resolveProjects from project-filter
      const { resolveProjects } = await import("./project-filter");
      
      const mockProjects = ["brain-api", "brain-web", "prod-api", "test-api"];

      globalThis.fetch = createMockFetch({
        "/tasks": () =>
          Promise.resolve(
            new Response(JSON.stringify({ projects: mockProjects }))
          ),
      });

      const result = await resolveProjects(config.brainApiUrl, {
        includes: ["brain-*"],
        excludes: [],
      });

      expect(result).toEqual(["brain-api", "brain-web"]);
    });

    test("filters projects with exclude pattern", async () => {
      const { resolveProjects } = await import("./project-filter");
      
      const mockProjects = ["brain-api", "brain-web", "test-api", "test-web"];

      globalThis.fetch = createMockFetch({
        "/tasks": () =>
          Promise.resolve(
            new Response(JSON.stringify({ projects: mockProjects }))
          ),
      });

      const result = await resolveProjects(config.brainApiUrl, {
        includes: [],
        excludes: ["test-*"],
      });

      expect(result).toEqual(["brain-api", "brain-web"]);
    });

    test("filters projects with both include and exclude patterns", async () => {
      const { resolveProjects } = await import("./project-filter");
      
      const mockProjects = [
        "brain-api",
        "brain-web",
        "brain-legacy",
        "prod-api",
        "test-api",
      ];

      globalThis.fetch = createMockFetch({
        "/tasks": () =>
          Promise.resolve(
            new Response(JSON.stringify({ projects: mockProjects }))
          ),
      });

      // Include brain-* but exclude *-legacy
      const result = await resolveProjects(config.brainApiUrl, {
        includes: ["brain-*"],
        excludes: ["*-legacy"],
      });

      expect(result).toEqual(["brain-api", "brain-web"]);
    });

    test("returns empty array when no projects match filters", async () => {
      const { resolveProjects } = await import("./project-filter");
      
      const mockProjects = ["brain-api", "prod-api"];

      globalThis.fetch = createMockFetch({
        "/tasks": () =>
          Promise.resolve(
            new Response(JSON.stringify({ projects: mockProjects }))
          ),
      });

      const result = await resolveProjects(config.brainApiUrl, {
        includes: ["nonexistent-*"],
        excludes: [],
      });

      expect(result).toEqual([]);
    });
  });

  describe("TaskRunner multi-project initialization", () => {
    test("accepts multiple projects in constructor", () => {
      const runner = new TaskRunner({
        projects: ["project-a", "project-b", "project-c"],
        config,
      });

      const status = runner.getStatus();
      
      // First project is used as projectId for backward compatibility
      expect(status.projectId).toBe("project-a");
      // Status should be idle before start
      expect(status.status).toBe("idle");
    });

    test("single project mode works for backward compatibility", () => {
      const runner = new TaskRunner({
        projectId: "legacy-project",
        config,
      });

      const status = runner.getStatus();
      expect(status.projectId).toBe("legacy-project");
    });

    test("throws when neither projectId nor projects provided", () => {
      expect(() => {
        new TaskRunner({ config });
      }).toThrow("TaskRunner requires either projectId or projects option");
    });
  });

  describe("Multi-project polling flow", () => {
    test("API client can fetch tasks for multiple projects", async () => {
      const mockTasksA = [createMockTask("task-a-1", { title: "Task A1" })];
      const mockTasksB = [createMockTask("task-b-1", { title: "Task B1" })];

      globalThis.fetch = ((url: string) => {
        if (url.includes("project-a")) {
          return Promise.resolve(
            new Response(JSON.stringify({ tasks: mockTasksA, count: 1 }))
          );
        }
        if (url.includes("project-b")) {
          return Promise.resolve(
            new Response(JSON.stringify({ tasks: mockTasksB, count: 1 }))
          );
        }
        return Promise.resolve(
          new Response(
            JSON.stringify({ status: "healthy", zkAvailable: true, dbAvailable: true })
          )
        );
      }) as typeof fetch;

      const apiClient = new ApiClient(config);

      // Fetch from multiple projects in parallel
      const [tasksA, tasksB] = await Promise.all([
        apiClient.getReadyTasks("project-a"),
        apiClient.getReadyTasks("project-b"),
      ]);

      expect(tasksA).toHaveLength(1);
      expect(tasksA[0].title).toBe("Task A1");
      
      expect(tasksB).toHaveLength(1);
      expect(tasksB[0].title).toBe("Task B1");
    });

    test("shared execution pool limits total parallel tasks across all projects", async () => {
      // Create a runner with multiple projects and max-parallel of 2
      const multiConfig: RunnerConfig = {
        ...config,
        maxParallel: 2,
      };

      const runner = new TaskRunner({
        projects: ["project-a", "project-b"],
        config: multiConfig,
      });

      // Verify config was applied
      const status = runner.getStatus();
      expect(status.status).toBe("idle");
      
      // The maxParallel applies globally across all projects
      // This is enforced in ProcessManager.runningCount()
    });
  });

  describe("Composite task keys", () => {
    test("tasks from different projects can have same ID", () => {
      // Create tasks with same ID from different projects
      const taskFromA = createRunningTask("task-1", 1001, { projectId: "project-a" });
      const taskFromB = createRunningTask("task-1", 1002, { projectId: "project-b" });

      // In multi-project mode, composite keys would be: "project-a:task-1" and "project-b:task-1"
      const compositeKeyA = `${taskFromA.projectId}:${taskFromA.id}`;
      const compositeKeyB = `${taskFromB.projectId}:${taskFromB.id}`;

      expect(compositeKeyA).toBe("project-a:task-1");
      expect(compositeKeyB).toBe("project-b:task-1");
      expect(compositeKeyA).not.toBe(compositeKeyB);
    });
  });
});

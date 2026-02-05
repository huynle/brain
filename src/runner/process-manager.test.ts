/**
 * Process Manager Tests
 */

import { describe, test, expect, beforeEach, afterEach, mock } from "bun:test";
import {
  ProcessManager,
  CompletionStatus,
  resetProcessManager,
  type ProcessState,
  type BunSubprocess,
} from "./process-manager";
import type { RunningTask } from "./types";
import type { Subprocess } from "bun";

// =============================================================================
// Test Helpers
// =============================================================================

function createMockTask(id: string, overrides: Partial<RunningTask> = {}): RunningTask {
  return {
    id,
    path: `projects/test/task/${id}.md`,
    title: `Test Task ${id}`,
    priority: "medium",
    projectId: "test",
    pid: 0,
    startedAt: new Date().toISOString(),
    isResume: false,
    workdir: "/tmp/test",
    ...overrides,
  };
}

function createLongRunningProcess(): Subprocess {
  // Spawn a process that will stay alive until killed
  return Bun.spawn(["sleep", "60"]);
}

function createShortProcess(exitCode: number = 0): Subprocess {
  // Spawn a process that exits immediately with the given code
  if (exitCode === 0) {
    return Bun.spawn(["true"]);
  } else {
    return Bun.spawn(["false"]);
  }
}

// =============================================================================
// Tests
// =============================================================================

describe("ProcessManager", () => {
  let manager: ProcessManager;
  let spawnedProcesses: Subprocess[] = [];

  beforeEach(() => {
    resetProcessManager();
    manager = new ProcessManager();
    spawnedProcesses = [];
  });

  afterEach(async () => {
    // Clean up any spawned processes
    for (const proc of spawnedProcesses) {
      try {
        proc.kill();
      } catch {
        // Process may already be dead
      }
    }
    spawnedProcesses = [];

    // Kill all tracked processes
    await manager.killAll();
  });

  describe("add()", () => {
    test("tracks a new process", () => {
      const task = createMockTask("task1");
      const proc = createLongRunningProcess();
      spawnedProcesses.push(proc);

      manager.add("task1", task, proc);

      expect(manager.count()).toBe(1);
      expect(manager.get("task1")).toBeDefined();
      expect(manager.get("task1")?.task).toBe(task);
    });

    test("throws if task already tracked", () => {
      const task = createMockTask("task1");
      const proc1 = createLongRunningProcess();
      const proc2 = createLongRunningProcess();
      spawnedProcesses.push(proc1, proc2);

      manager.add("task1", task, proc1);

      expect(() => manager.add("task1", task, proc2)).toThrow(
        "Task task1 is already being tracked"
      );
    });

    test("sets up exit handler", async () => {
      const task = createMockTask("task1");
      const proc = createShortProcess(0);
      spawnedProcesses.push(proc);

      manager.add("task1", task, proc);

      // Wait for process to exit
      await new Promise<void>((resolve) => {
        const check = setInterval(() => {
          const info = manager.get("task1");
          if (info?.exited) {
            clearInterval(check);
            resolve();
          }
        }, 10);
      });

      const info = manager.get("task1");
      expect(info?.exited).toBe(true);
      expect(info?.exitCode).toBe(0);
      expect(info?.exitedAt).toBeDefined();
    });
  });

  describe("remove()", () => {
    test("removes and returns task info", () => {
      const task = createMockTask("task1");
      const proc = createLongRunningProcess();
      spawnedProcesses.push(proc);

      manager.add("task1", task, proc);
      const removed = manager.remove("task1");

      expect(removed).toBeDefined();
      expect(removed?.task).toBe(task);
      expect(manager.count()).toBe(0);
      expect(manager.get("task1")).toBeUndefined();
    });

    test("returns undefined for unknown task", () => {
      const removed = manager.remove("unknown");
      expect(removed).toBeUndefined();
    });
  });

  describe("isRunning()", () => {
    test("returns true for running process", () => {
      const task = createMockTask("task1");
      const proc = createLongRunningProcess();
      spawnedProcesses.push(proc);

      manager.add("task1", task, proc);

      expect(manager.isRunning("task1")).toBe(true);
    });

    test("returns false for exited process", async () => {
      const task = createMockTask("task1");
      const proc = createShortProcess(0);
      spawnedProcesses.push(proc);

      manager.add("task1", task, proc);

      // Wait for exit
      await new Promise((r) => setTimeout(r, 100));

      expect(manager.isRunning("task1")).toBe(false);
    });

    test("returns false for unknown task", () => {
      expect(manager.isRunning("unknown")).toBe(false);
    });
  });

  describe("getAll() and getAllRunning()", () => {
    test("returns all processes", () => {
      const proc1 = createLongRunningProcess();
      const proc2 = createLongRunningProcess();
      spawnedProcesses.push(proc1, proc2);

      manager.add("task1", createMockTask("task1"), proc1);
      manager.add("task2", createMockTask("task2"), proc2);

      expect(manager.getAll()).toHaveLength(2);
      expect(manager.count()).toBe(2);
    });

    test("getAllRunning filters exited processes", async () => {
      const proc1 = createLongRunningProcess();
      const proc2 = createShortProcess(0);
      spawnedProcesses.push(proc1, proc2);

      manager.add("task1", createMockTask("task1"), proc1);
      manager.add("task2", createMockTask("task2"), proc2);

      // Wait for proc2 to exit
      await new Promise((r) => setTimeout(r, 100));

      expect(manager.getAll()).toHaveLength(2);
      expect(manager.getAllRunning()).toHaveLength(1);
      expect(manager.runningCount()).toBe(1);
    });
  });

  describe("checkCompletion()", () => {
    test("returns Running for active process", async () => {
      const task = createMockTask("task1");
      const proc = createLongRunningProcess();
      spawnedProcesses.push(proc);

      manager.add("task1", task, proc);

      const status = await manager.checkCompletion("task1", false);
      expect(status).toBe(CompletionStatus.Running);
    });

    test("returns Crashed for unknown task", async () => {
      const status = await manager.checkCompletion("unknown", false);
      expect(status).toBe(CompletionStatus.Crashed);
    });

    test("returns Crashed for exited process without file check", async () => {
      const task = createMockTask("task1");
      const proc = createShortProcess(1); // Exit with error
      spawnedProcesses.push(proc);

      manager.add("task1", task, proc);

      // Wait for exit
      await new Promise((r) => setTimeout(r, 100));

      const status = await manager.checkCompletion("task1", false);
      expect(status).toBe(CompletionStatus.Crashed);
    });

    test("returns Completed for clean exit without file check", async () => {
      const task = createMockTask("task1");
      const proc = createShortProcess(0); // Exit cleanly
      spawnedProcesses.push(proc);

      manager.add("task1", task, proc);

      // Wait for exit
      await new Promise((r) => setTimeout(r, 100));

      const status = await manager.checkCompletion("task1", false);
      expect(status).toBe(CompletionStatus.Completed);
    });

    test("returns Timeout for process exceeding task timeout", async () => {
      // Create task that started long ago
      const task = createMockTask("task1", {
        startedAt: new Date(Date.now() - 10_000_000).toISOString(),
      });
      const proc = createLongRunningProcess();
      spawnedProcesses.push(proc);

      // Create manager with short timeout
      const customManager = new ProcessManager({
        brainApiUrl: "http://localhost:3333",
        pollInterval: 30,
        taskPollInterval: 5,
        maxParallel: 3,
        stateDir: "/tmp",
        logDir: "/tmp",
        workDir: "/tmp",
        apiTimeout: 5000,
        taskTimeout: 1000, // 1 second timeout
        opencode: { bin: "opencode", agent: "general", model: "test" },
        excludeProjects: [],
    idleDetectionThreshold: 60000,
      });

      customManager.add("task1", task, proc);

      const status = await customManager.checkCompletion("task1", false);
      expect(status).toBe(CompletionStatus.Timeout);

      await customManager.killAll();
    });
  });

  describe("kill()", () => {
    test("kills running process", async () => {
      const task = createMockTask("task1");
      const proc = createLongRunningProcess();
      spawnedProcesses.push(proc);

      manager.add("task1", task, proc);
      expect(manager.isRunning("task1")).toBe(true);

      const killed = await manager.kill("task1");

      expect(killed).toBe(true);
      expect(manager.isRunning("task1")).toBe(false);
    });

    test("returns false for unknown task", async () => {
      const killed = await manager.kill("unknown");
      expect(killed).toBe(false);
    });

    test("returns true for already exited process", async () => {
      const task = createMockTask("task1");
      const proc = createShortProcess(0);
      spawnedProcesses.push(proc);

      manager.add("task1", task, proc);

      // Wait for natural exit
      await new Promise((r) => setTimeout(r, 100));

      const killed = await manager.kill("task1");
      expect(killed).toBe(true);
    });
  });

  describe("killAll()", () => {
    test("kills all running processes", async () => {
      const proc1 = createLongRunningProcess();
      const proc2 = createLongRunningProcess();
      spawnedProcesses.push(proc1, proc2);

      manager.add("task1", createMockTask("task1"), proc1);
      manager.add("task2", createMockTask("task2"), proc2);

      expect(manager.runningCount()).toBe(2);

      await manager.killAll();

      expect(manager.runningCount()).toBe(0);
    });
  });

  describe("toJSON()", () => {
    test("serializes process state", () => {
      const task = createMockTask("task1");
      const proc = createLongRunningProcess();
      spawnedProcesses.push(proc);

      manager.add("task1", task, proc);

      const state = manager.toJSON();

      expect(state).toHaveLength(1);
      expect(state[0].taskId).toBe("task1");
      expect(state[0].task).toBe(task);
      expect(state[0].pid).toBe(proc.pid!);
      expect(state[0].exited).toBe(false);
    });

    test("includes exit info for exited processes", async () => {
      const task = createMockTask("task1");
      const proc = createShortProcess(42);
      spawnedProcesses.push(proc);

      manager.add("task1", task, proc);

      // Wait for exit
      await new Promise((r) => setTimeout(r, 100));

      const state = manager.toJSON();

      expect(state[0].exited).toBe(true);
      expect(state[0].exitedAt).toBeDefined();
    });
  });

  describe("restoreFromState()", () => {
    test("returns tasks for living PIDs", () => {
      const proc = createLongRunningProcess();
      spawnedProcesses.push(proc);

      const task = createMockTask("task1", { pid: proc.pid! });

      const states: ProcessState[] = [
        {
          taskId: "task1",
          task,
          pid: proc.pid!,
          exitCode: null,
          exited: false,
        },
      ];

      const restored = manager.restoreFromState(states);

      expect(restored).toHaveLength(1);
      expect(restored[0]).toBe(task);
    });

    test("skips tasks with dead PIDs", () => {
      const task = createMockTask("task1", { pid: 999999 }); // Very unlikely to be a real PID

      const states: ProcessState[] = [
        {
          taskId: "task1",
          task,
          pid: 999999,
          exitCode: null,
          exited: false,
        },
      ];

      const restored = manager.restoreFromState(states);

      expect(restored).toHaveLength(0);
    });

    test("skips already tracked tasks", () => {
      const proc = createLongRunningProcess();
      spawnedProcesses.push(proc);

      const task = createMockTask("task1");
      manager.add("task1", task, proc);

      const states: ProcessState[] = [
        {
          taskId: "task1",
          task,
          pid: proc.pid!,
          exitCode: null,
          exited: false,
        },
      ];

      const restored = manager.restoreFromState(states);

      expect(restored).toHaveLength(0);
    });
  });

  describe("createTaskResult()", () => {
    test("creates TaskResult for completed task", async () => {
      const task = createMockTask("task1");
      const proc = createShortProcess(0);
      spawnedProcesses.push(proc);

      manager.add("task1", task, proc);

      // Wait for exit
      await new Promise((r) => setTimeout(r, 100));

      const result = manager.createTaskResult("task1", CompletionStatus.Completed);

      expect(result).toBeDefined();
      expect(result?.taskId).toBe("task1");
      expect(result?.status).toBe("completed");
      expect(result?.startedAt).toBe(task.startedAt);
      expect(result?.completedAt).toBeDefined();
      expect(result?.duration).toBeGreaterThan(0);
      expect(result?.exitCode ?? -1).toBe(0);
    });

    test("maps completion statuses correctly", async () => {
      const task = createMockTask("task1");
      const proc = createLongRunningProcess();
      spawnedProcesses.push(proc);

      manager.add("task1", task, proc);

      // Test all status mappings
      const statusMappings: [CompletionStatus, "completed" | "failed" | "blocked" | "timeout" | "crashed"][] = [
        [CompletionStatus.Completed, "completed"],
        [CompletionStatus.Failed, "failed"],
        [CompletionStatus.Blocked, "blocked"],
        [CompletionStatus.Timeout, "timeout"],
        [CompletionStatus.Crashed, "crashed"],
        [CompletionStatus.Running, "crashed"], // Running maps to crashed
      ];

      for (const [completion, expected] of statusMappings) {
        const result = manager.createTaskResult("task1", completion);
        expect(result?.status).toBe(expected);
      }
    });

    test("returns null for unknown task", () => {
      const result = manager.createTaskResult("unknown", CompletionStatus.Completed);
      expect(result).toBeNull();
    });
  });
});

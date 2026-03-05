/**
 * Tests for Brain Runner State Manager
 */

import { describe, it, expect, beforeEach, afterEach } from "bun:test";
import { existsSync, mkdirSync, rmSync, writeFileSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";
import { StateManager } from "./state-manager";
import type { RunnerStats, RunnerStatus, RunningTask } from "./types";

describe("StateManager", () => {
  let testDir: string;
  let manager: StateManager;
  const projectId = "test-project";

  beforeEach(() => {
    // Create a unique temp directory for each test
    testDir = join(tmpdir(), `state-manager-test-${Date.now()}-${Math.random().toString(36).slice(2)}`);
    mkdirSync(testDir, { recursive: true });
    manager = new StateManager(testDir, projectId);
  });

  afterEach(() => {
    // Clean up test directory
    if (existsSync(testDir)) {
      rmSync(testDir, { recursive: true, force: true });
    }
  });

  // Helper to create sample data
  const createSampleTasks = (): RunningTask[] => [
    {
      id: "task-1",
      path: "projects/test/task/abc123.md",
      title: "Test Task 1",
      priority: "high",
      projectId: "test",
      pid: 12345,
      startedAt: "2024-01-15T10:00:00Z",
      isResume: false,
      workdir: "/tmp/work",
    },
    {
      id: "task-2",
      path: "projects/test/task/def456.md",
      title: "Test Task 2",
      priority: "medium",
      projectId: "test",
      pid: 12346,
      paneId: "%5",
      windowName: "test-window",
      startedAt: "2024-01-15T10:05:00Z",
      isResume: true,
      workdir: "/tmp/work2",
    },
  ];

  const createSampleStats = (): RunnerStats => ({
    completed: 5,
    failed: 1,
    totalRuntime: 300000,
  });

  describe("save and load", () => {
    it("saves and loads state round-trip", () => {
      const status: RunnerStatus = "processing";
      const tasks = createSampleTasks();
      const stats = createSampleStats();
      const startedAt = "2024-01-15T09:00:00Z";

      manager.save(status, tasks, stats, startedAt);
      const loaded = manager.load();

      expect(loaded).not.toBeNull();
      expect(loaded!.projectId).toBe(projectId);
      expect(loaded!.status).toBe(status);
      expect(loaded!.startedAt).toBe(startedAt);
      expect(loaded!.runningTasks).toEqual(tasks);
      expect(loaded!.stats).toEqual(stats);
      expect(loaded!.updatedAt).toBeDefined();
    });

    it("load returns null for missing file", () => {
      const result = manager.load();
      expect(result).toBeNull();
    });

    it("load returns null for corrupted JSON", () => {
      const stateFile = join(testDir, `runner-${projectId}.json`);
      writeFileSync(stateFile, "{ invalid json }", "utf-8");

      const result = manager.load();
      expect(result).toBeNull();
    });

    it("load returns null for empty file", () => {
      const stateFile = join(testDir, `runner-${projectId}.json`);
      writeFileSync(stateFile, "", "utf-8");

      const result = manager.load();
      expect(result).toBeNull();
    });
  });

  describe("clear", () => {
    it("removes all state files", () => {
      const status: RunnerStatus = "idle";
      const tasks = createSampleTasks();
      const stats = createSampleStats();

      manager.save(status, tasks, stats, "2024-01-15T09:00:00Z");
      manager.savePid(process.pid);
      manager.saveRunningTasks(tasks);

      const stateFile = join(testDir, `runner-${projectId}.json`);
      const pidFile = join(testDir, `runner-${projectId}.pid`);
      const runningFile = join(testDir, `running-${projectId}.json`);

      expect(existsSync(stateFile)).toBe(true);
      expect(existsSync(pidFile)).toBe(true);
      expect(existsSync(runningFile)).toBe(true);

      manager.clear();

      expect(existsSync(stateFile)).toBe(false);
      expect(existsSync(pidFile)).toBe(false);
      expect(existsSync(runningFile)).toBe(false);
    });

    it("handles missing files gracefully", () => {
      // Should not throw even if files don't exist
      expect(() => manager.clear()).not.toThrow();
    });
  });

  describe("PID file operations", () => {
    it("saves and loads PID", () => {
      const pid = 12345;
      manager.savePid(pid);

      const loaded = manager.loadPid();
      expect(loaded).toBe(pid);
    });

    it("loadPid returns null for missing file", () => {
      const result = manager.loadPid();
      expect(result).toBeNull();
    });

    it("loadPid returns null for invalid content", () => {
      const pidFile = join(testDir, `runner-${projectId}.pid`);
      writeFileSync(pidFile, "not-a-number", "utf-8");

      const result = manager.loadPid();
      expect(result).toBeNull();
    });

    it("clearPid removes the PID file", () => {
      manager.savePid(12345);
      const pidFile = join(testDir, `runner-${projectId}.pid`);
      expect(existsSync(pidFile)).toBe(true);

      manager.clearPid();
      expect(existsSync(pidFile)).toBe(false);
    });

    it("clearPid handles missing file gracefully", () => {
      expect(() => manager.clearPid()).not.toThrow();
    });
  });

  describe("isPidRunning", () => {
    it("returns true for live PID (current process)", () => {
      manager.savePid(process.pid);

      const result = manager.isPidRunning();
      expect(result).toBe(true);
    });

    it("returns false for dead PID", () => {
      // Use a very high PID that's unlikely to exist
      manager.savePid(999999999);

      const result = manager.isPidRunning();
      expect(result).toBe(false);
    });

    it("returns false when no PID file exists", () => {
      const result = manager.isPidRunning();
      expect(result).toBe(false);
    });
  });

  describe("running tasks", () => {
    it("saves and loads running tasks", () => {
      const tasks = createSampleTasks();
      manager.saveRunningTasks(tasks);

      const loaded = manager.loadRunningTasks();
      expect(loaded).toEqual(tasks);
    });

    it("loadRunningTasks returns empty array for missing file", () => {
      const result = manager.loadRunningTasks();
      expect(result).toEqual([]);
    });

    it("loadRunningTasks returns empty array for corrupted JSON", () => {
      const runningFile = join(testDir, `running-${projectId}.json`);
      writeFileSync(runningFile, "{ invalid json }", "utf-8");

      const result = manager.loadRunningTasks();
      expect(result).toEqual([]);
    });
  });

  describe("static utilities", () => {
    describe("findAllRunnerStates", () => {
      it("finds all runner state files", () => {
        // Create multiple runner states
        const manager1 = new StateManager(testDir, "project-a");
        const manager2 = new StateManager(testDir, "project-b");
        const manager3 = new StateManager(testDir, "project-c");

        const stats = createSampleStats();
        manager1.save("idle", [], stats, "2024-01-15T09:00:00Z");
        manager2.save("processing", [], stats, "2024-01-15T10:00:00Z");
        manager3.save("stopped", [], stats, "2024-01-15T11:00:00Z");

        const states = StateManager.findAllRunnerStates(testDir);

        expect(states).toHaveLength(3);
        const projectIds = states.map((s) => s.projectId).sort();
        expect(projectIds).toEqual(["project-a", "project-b", "project-c"]);
      });

      it("returns empty array for non-existent directory", () => {
        const nonExistent = join(testDir, "does-not-exist");
        const states = StateManager.findAllRunnerStates(nonExistent);
        expect(states).toEqual([]);
      });

      it("ignores non-runner files", () => {
        // Create a runner state and some other files
        manager.save("idle", [], createSampleStats(), "2024-01-15T09:00:00Z");
        writeFileSync(join(testDir, "other-file.json"), "{}", "utf-8");
        writeFileSync(join(testDir, "running-test.json"), "[]", "utf-8");
        writeFileSync(join(testDir, "random.txt"), "hello", "utf-8");

        const states = StateManager.findAllRunnerStates(testDir);

        expect(states).toHaveLength(1);
        expect(states[0].projectId).toBe(projectId);
      });
    });

    describe("cleanupStaleStates", () => {
      it("removes states with dead PIDs", () => {
        // Create a state with a dead PID
        const manager1 = new StateManager(testDir, "dead-project");
        manager1.save("processing", [], createSampleStats(), "2024-01-15T09:00:00Z");
        manager1.savePid(999999999); // Dead PID

        // Create a state with live PID
        const manager2 = new StateManager(testDir, "live-project");
        manager2.save("processing", [], createSampleStats(), "2024-01-15T10:00:00Z");
        manager2.savePid(process.pid); // Current process

        const cleaned = StateManager.cleanupStaleStates(testDir);

        expect(cleaned).toBe(1);

        // Dead project should be cleaned
        expect(manager1.load()).toBeNull();

        // Live project should remain
        expect(manager2.load()).not.toBeNull();
      });

      it("returns 0 when no stale states", () => {
        // Create only live states
        const manager1 = new StateManager(testDir, "live-project");
        manager1.save("processing", [], createSampleStats(), "2024-01-15T09:00:00Z");
        manager1.savePid(process.pid);

        const cleaned = StateManager.cleanupStaleStates(testDir);
        expect(cleaned).toBe(0);
      });

      it("handles empty directory", () => {
        const cleaned = StateManager.cleanupStaleStates(testDir);
        expect(cleaned).toBe(0);
      });
    });
  });
});

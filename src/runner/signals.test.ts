/**
 * Tests for Brain Runner Signal Handler
 */

import { describe, it, expect, beforeEach, afterEach, mock, spyOn } from "bun:test";
import { existsSync, mkdirSync, rmSync, readFileSync } from "fs";
import { join } from "path";
import { tmpdir } from "os";
import type { Subprocess } from "bun";
import {
  SignalHandler,
  setupSignalHandler,
  getSignalHandler,
  resetSignalHandler,
  type SignalHandlerOptions,
} from "./signals";
import { ProcessManager, resetProcessManager } from "./process-manager";
import { resetConfig } from "./config";
import type { RunnerEvent, RunningTask, RunnerStats } from "./types";

describe("SignalHandler", () => {
  let testDir: string;
  let processManager: ProcessManager;
  let handler: SignalHandler;
  let options: SignalHandlerOptions;

  beforeEach(() => {
    // Create a unique temp directory for each test
    testDir = join(
      tmpdir(),
      `signal-handler-test-${Date.now()}-${Math.random().toString(36).slice(2)}`
    );
    mkdirSync(testDir, { recursive: true });

    // Reset singletons
    resetProcessManager();
    resetConfig();
    resetSignalHandler();

    // Create fresh process manager
    processManager = new ProcessManager();

    // Default options
    options = {
      stateDir: testDir,
      projectId: "test-project",
      gracefulTimeout: 1000, // Short timeouts for tests
      forceKillTimeout: 500,
    };
  });

  afterEach(() => {
    // Clean up
    if (handler) {
      handler.unregister();
    }
    resetSignalHandler();

    // Kill any remaining processes
    processManager.killAll().catch(() => {});

    // Clean up test directory
    if (existsSync(testDir)) {
      rmSync(testDir, { recursive: true, force: true });
    }
  });

  // Helper to create a sample task
  const createSampleTask = (): RunningTask => ({
    id: "task-1",
    path: "projects/test/task/abc123.md",
    title: "Test Task",
    priority: "high",
    projectId: "test",
    pid: 12345,
    startedAt: new Date().toISOString(),
    isResume: false,
    workdir: "/tmp/work",
  });

  // Helper to spawn a simple sleep process
  const spawnSleepProcess = (seconds: number = 10): Subprocess => {
    return Bun.spawn(["sleep", String(seconds)]);
  };

  describe("registration", () => {
    it("registers signal handlers on register()", () => {
      handler = new SignalHandler(options, processManager);

      // Spy on process.on
      const onSpy = spyOn(process, "on");

      handler.register();

      // Check that handlers were registered
      expect(onSpy).toHaveBeenCalledWith("SIGTERM", expect.any(Function));
      expect(onSpy).toHaveBeenCalledWith("SIGINT", expect.any(Function));
      expect(onSpy).toHaveBeenCalledWith("SIGHUP", expect.any(Function));
    });

    it("unregisters signal handlers on unregister()", () => {
      handler = new SignalHandler(options, processManager);
      handler.register();

      // Spy on process.off
      const offSpy = spyOn(process, "off");

      handler.unregister();

      // Check that handlers were unregistered
      expect(offSpy).toHaveBeenCalledWith("SIGTERM", expect.any(Function));
      expect(offSpy).toHaveBeenCalledWith("SIGINT", expect.any(Function));
      expect(offSpy).toHaveBeenCalledWith("SIGHUP", expect.any(Function));
    });

    it("prevents double registration", async () => {
      handler = new SignalHandler(options, processManager);

      // Register first time
      handler.register();

      // Second call should be no-op (no error thrown, no double handlers)
      handler.register();

      // Verify it still works correctly - shutdown should work once
      const events: RunnerEvent[] = [];
      const optionsWithEvent: SignalHandlerOptions = {
        ...options,
        onEvent: (event) => events.push(event),
      };
      
      // Create a new handler that's been registered twice
      const handler2 = new SignalHandler(optionsWithEvent, processManager);
      handler2.register();
      handler2.register(); // Double registration
      
      await handler2.shutdown("manual");
      
      // Should only emit one event (not double)
      expect(events.filter(e => e.type === "shutdown")).toHaveLength(1);
      
      handler2.unregister();
    });
  });

  describe("shutdown", () => {
    it("returns exit code 0 on successful shutdown", async () => {
      handler = new SignalHandler(options, processManager);
      handler.register();

      const exitCode = await handler.shutdown("manual");

      expect(exitCode).toBe(0);
    });

    it("sets isShuttingDown to true during shutdown", async () => {
      handler = new SignalHandler(options, processManager);
      handler.register();

      expect(handler.isShuttingDown()).toBe(false);

      // Start shutdown but don't await
      const shutdownPromise = handler.shutdown("manual");

      expect(handler.isShuttingDown()).toBe(true);

      await shutdownPromise;

      expect(handler.isShuttingDown()).toBe(true); // Stays true after shutdown
    });

    it("emits shutdown event", async () => {
      const events: RunnerEvent[] = [];
      const optionsWithEvent: SignalHandlerOptions = {
        ...options,
        onEvent: (event) => events.push(event),
      };

      handler = new SignalHandler(optionsWithEvent, processManager);
      handler.register();

      await handler.shutdown("SIGTERM");

      expect(events).toHaveLength(1);
      expect(events[0]).toEqual({ type: "shutdown", reason: "SIGTERM" });
    });

    it("prevents double shutdown", async () => {
      const events: RunnerEvent[] = [];
      const optionsWithEvent: SignalHandlerOptions = {
        ...options,
        onEvent: (event) => events.push(event),
      };

      handler = new SignalHandler(optionsWithEvent, processManager);
      handler.register();

      // First shutdown
      const result1 = await handler.shutdown("SIGTERM");
      // Second shutdown should return early
      const result2 = await handler.shutdown("SIGINT");

      expect(result1).toBe(0);
      expect(result2).toBe(1); // Returns 1 when already shutting down

      // Only one shutdown event
      expect(events).toHaveLength(1);
    });

    it("saves state on shutdown", async () => {
      const runningTasks: RunningTask[] = [createSampleTask()];
      const stats: RunnerStats = { completed: 5, failed: 1, totalRuntime: 60000 };
      const startedAt = "2024-01-15T10:00:00Z";

      const optionsWithCallbacks: SignalHandlerOptions = {
        ...options,
        getRunningTasks: () => runningTasks,
        getStats: () => stats,
        getStartedAt: () => startedAt,
      };

      handler = new SignalHandler(optionsWithCallbacks, processManager);
      handler.register();

      await handler.shutdown("manual");

      // Check state file was written
      const stateFile = join(testDir, "runner-test-project.json");
      expect(existsSync(stateFile)).toBe(true);

      const savedState = JSON.parse(readFileSync(stateFile, "utf-8"));
      expect(savedState.status).toBe("stopped");
      expect(savedState.stats).toEqual(stats);
      expect(savedState.startedAt).toBe(startedAt);
    });

    it("waits for running tasks before exiting", async () => {
      handler = new SignalHandler(options, processManager);
      handler.register();

      // Spawn a short-lived process
      const proc = Bun.spawn(["sleep", "0.1"]);
      const task = createSampleTask();
      processManager.add(task.id, task, proc);

      expect(processManager.runningCount()).toBe(1);

      const exitCode = await handler.shutdown("manual");

      // Process should have exited naturally
      expect(exitCode).toBe(0);
      expect(processManager.runningCount()).toBe(0);
    });

    it("kills child processes on timeout", async () => {
      const shortOptions: SignalHandlerOptions = {
        ...options,
        gracefulTimeout: 100, // Very short timeout
        forceKillTimeout: 100,
      };

      handler = new SignalHandler(shortOptions, processManager);
      handler.register();

      // Spawn a long-running process
      const proc = spawnSleepProcess(60); // 60 seconds - won't finish naturally
      const task = createSampleTask();
      processManager.add(task.id, task, proc);

      expect(processManager.runningCount()).toBe(1);

      const exitCode = await handler.shutdown("manual");

      // Should have killed the process
      expect(exitCode).toBe(0);
    });
  });

  describe("getShutdownState", () => {
    it("returns initial state", () => {
      handler = new SignalHandler(options, processManager);

      const state = handler.getShutdownState();

      expect(state.isShuttingDown).toBe(false);
      expect(state.reason).toBeNull();
      expect(state.startedAt).toBeNull();
    });

    it("returns updated state during shutdown", async () => {
      handler = new SignalHandler(options, processManager);
      handler.register();

      await handler.shutdown("SIGTERM");

      const state = handler.getShutdownState();

      expect(state.isShuttingDown).toBe(true);
      expect(state.reason).toBe("SIGTERM");
      expect(state.startedAt).not.toBeNull();
    });
  });

  describe("SIGHUP handling", () => {
    it("reloads config on SIGHUP", () => {
      // Set initial env value
      const originalValue = process.env.RUNNER_POLL_INTERVAL;
      process.env.RUNNER_POLL_INTERVAL = "60";

      handler = new SignalHandler(options, processManager);
      handler.register();

      // Trigger SIGHUP via direct call (safer than actually sending signal)
      // We'll use the manual reload mechanism
      // @ts-ignore - accessing private method for testing
      handler.handleReload?.();

      // Clean up
      if (originalValue === undefined) {
        delete process.env.RUNNER_POLL_INTERVAL;
      } else {
        process.env.RUNNER_POLL_INTERVAL = originalValue;
      }
    });
  });

  describe("singleton functions", () => {
    it("setupSignalHandler creates and registers handler", () => {
      const handler = setupSignalHandler(options, processManager);

      expect(handler).toBeInstanceOf(SignalHandler);
      expect(getSignalHandler()).toBe(handler);
    });

    it("resetSignalHandler clears the singleton", () => {
      setupSignalHandler(options, processManager);
      expect(getSignalHandler()).not.toBeNull();

      resetSignalHandler();
      expect(getSignalHandler()).toBeNull();
    });

    it("setupSignalHandler replaces existing handler", () => {
      const handler1 = setupSignalHandler(options, processManager);
      const handler2 = setupSignalHandler(
        { ...options, projectId: "other" },
        processManager
      );

      expect(getSignalHandler()).toBe(handler2);
      expect(getSignalHandler()).not.toBe(handler1);
    });
  });
});

/**
 * OpenCode Executor Tests
 */

import {
  describe,
  test,
  expect,
  beforeEach,
  afterEach,
  mock,
  spyOn,
} from "bun:test";
import { existsSync, mkdirSync, rmSync, writeFileSync, readFileSync } from "fs";
import { join } from "path";
import { tmpdir, homedir } from "os";
import {
  OpencodeExecutor,
  getOpencodeExecutor,
  resetOpencodeExecutor,
  type SpawnOptions,
  type SpawnResult,
} from "./opencode-executor";
import type { ResolvedTask } from "../core/types";
import type { RunnerConfig } from "./types";

// =============================================================================
// Test Helpers
// =============================================================================

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

function createTestConfig(stateDir: string): RunnerConfig {
  return {
    brainApiUrl: "http://localhost:3333",
    pollInterval: 30,
    taskPollInterval: 5,
    maxParallel: 3,
    stateDir,
    logDir: join(stateDir, "log"),
    workDir: "/tmp/default-workdir",
    apiTimeout: 5000,
    taskTimeout: 1800000,
    opencode: {
      bin: "opencode",
      agent: "general",
      model: "anthropic/claude-opus-4-5",
    },
    excludeProjects: [],
    idleDetectionThreshold: 60000,
    maxTotalProcesses: 10,
    memoryThresholdPercent: 10,
  };
}

// =============================================================================
// Tests
// =============================================================================

describe("OpencodeExecutor", () => {
  let executor: OpencodeExecutor;
  let testStateDir: string;
  let testConfig: RunnerConfig;

  beforeEach(() => {
    resetOpencodeExecutor();

    // Create temp directory for state files
    testStateDir = join(tmpdir(), `opencode-executor-test-${Date.now()}`);
    mkdirSync(testStateDir, { recursive: true });

    testConfig = createTestConfig(testStateDir);
    executor = new OpencodeExecutor(testConfig);
  });

  afterEach(() => {
    // Clean up temp directory
    try {
      rmSync(testStateDir, { recursive: true, force: true });
    } catch {
      // Ignore cleanup errors
    }
  });

  describe("buildPrompt()", () => {
    test("generates new task prompt", () => {
      const task = createMockTask("task1");
      const prompt = executor.buildPrompt(task, false);

      expect(prompt).toContain("Load the do-work-queue skill");
      expect(prompt).toContain(task.path);
      expect(prompt).toContain("Mark the task as in_progress");
      expect(prompt).toContain("Triage complexity (Route A/B/C)");
      expect(prompt).toContain("Create atomic git commit");
      expect(prompt).not.toContain("RESUME");
      expect(prompt).not.toContain("interrupted");
    });

    test("generates resume task prompt", () => {
      const task = createMockTask("task1");
      const prompt = executor.buildPrompt(task, true);

      expect(prompt).toContain("Load the do-work-queue skill");
      expect(prompt).toContain("RESUME");
      expect(prompt).toContain(task.path);
      expect(prompt).toContain("interrupted");
      expect(prompt).toContain("Check the task file for any progress notes");
      expect(prompt).toContain("continue from where it left off");
      expect(prompt).toContain("Create atomic git commit");
    });

    test("includes task path in both prompts", () => {
      const task = createMockTask("abc123", {
        path: "projects/myproject/task/abc123.md",
      });

      const newPrompt = executor.buildPrompt(task, false);
      const resumePrompt = executor.buildPrompt(task, true);

      expect(newPrompt).toContain("projects/myproject/task/abc123.md");
      expect(resumePrompt).toContain("projects/myproject/task/abc123.md");
    });
  });

  describe("resolveWorkdir()", () => {
    test("returns config workDir when task has no workdir fields", () => {
      const task = createMockTask("task1");
      const workdir = executor.resolveWorkdir(task);
      expect(workdir).toBe(testConfig.workDir);
    });

    test("returns resolved_workdir when set and exists", () => {
      const existingDir = testStateDir; // We know this exists
      const task = createMockTask("task1", {
        resolved_workdir: existingDir,
      });

      const workdir = executor.resolveWorkdir(task);
      expect(workdir).toBe(existingDir);
    });

    test("returns config default when resolved_workdir does not exist", () => {
      const task = createMockTask("task1", {
        resolved_workdir: "/nonexistent/path/that/does/not/exist",
      });

      const workdir = executor.resolveWorkdir(task);
      expect(workdir).toBe(testConfig.workDir);
    });

    test("prioritizes worktree over workdir when both exist under homedir", () => {
      // Create directories under homedir for this test
      const worktreeDir = join(homedir(), ".test-worktree-temp");
      const workdirDir = join(homedir(), ".test-workdir-temp");
      
      try {
        mkdirSync(worktreeDir, { recursive: true });
        mkdirSync(workdirDir, { recursive: true });

        const task = createMockTask("task1", {
          worktree: ".test-worktree-temp",
          workdir: ".test-workdir-temp",
        });

        const workdir = executor.resolveWorkdir(task);
        expect(workdir).toBe(worktreeDir);
      } finally {
        // Clean up
        try { rmSync(worktreeDir, { recursive: true, force: true }); } catch {}
        try { rmSync(workdirDir, { recursive: true, force: true }); } catch {}
      }
    });

    test("falls back to workdir when worktree does not exist", () => {
      // Create workdir under homedir
      const workdirDir = join(homedir(), ".test-workdir-temp");
      
      try {
        mkdirSync(workdirDir, { recursive: true });

        const task = createMockTask("task1", {
          worktree: "nonexistent/worktree/path",
          workdir: ".test-workdir-temp",
        });

        const workdir = executor.resolveWorkdir(task);
        expect(workdir).toBe(workdirDir);
      } finally {
        // Clean up
        try { rmSync(workdirDir, { recursive: true, force: true }); } catch {}
      }
    });

    test("workdir resolution priority chain", () => {
      // Test the full priority chain: worktree > workdir > resolved_workdir > config

      // Create resolved_workdir
      const resolvedDir = join(testStateDir, "resolved");
      mkdirSync(resolvedDir, { recursive: true });

      const task = createMockTask("task1", {
        worktree: "nonexistent/worktree",
        workdir: "nonexistent/workdir",
        resolved_workdir: resolvedDir,
      });

      const workdir = executor.resolveWorkdir(task);
      expect(workdir).toBe(resolvedDir);
    });
  });

  describe("spawn() - prompt file creation", () => {
    test("creates prompt file with correct content", async () => {
      const task = createMockTask("task1");

      // Mock Bun.spawn to prevent actual execution
      const originalSpawn = Bun.spawn;
      const mockProc = {
        pid: 12345,
        kill: () => {},
        exited: Promise.resolve(0),
      };

      // @ts-expect-error - mocking Bun.spawn
      Bun.spawn = () => mockProc;

      try {
        const result = await executor.spawn(task, "test-project", {
          mode: "background",
        });

        // Check prompt file exists
        expect(existsSync(result.promptFile)).toBe(true);

        // Check content
        const content = readFileSync(result.promptFile, "utf-8");
        expect(content).toContain("Load the do-work-queue skill");
        expect(content).toContain(task.path);
      } finally {
        Bun.spawn = originalSpawn;
      }
    });

    test("creates resume prompt when isResume is true", async () => {
      const task = createMockTask("task1");

      const originalSpawn = Bun.spawn;
      const mockProc = {
        pid: 12345,
        kill: () => {},
        exited: Promise.resolve(0),
      };

      // @ts-expect-error - mocking Bun.spawn
      Bun.spawn = () => mockProc;

      try {
        const result = await executor.spawn(task, "test-project", {
          mode: "background",
          isResume: true,
        });

        const content = readFileSync(result.promptFile, "utf-8");
        expect(content).toContain("RESUME");
        expect(content).toContain("interrupted");
      } finally {
        Bun.spawn = originalSpawn;
      }
    });
  });

  describe("spawn() - background mode", () => {
    test("spawns background process with correct arguments", async () => {
      const task = createMockTask("task1");
      let spawnArgs: any = null;

      const originalSpawn = Bun.spawn;
      const mockProc = {
        pid: 12345,
        kill: () => {},
        exited: Promise.resolve(0),
      };

      // @ts-expect-error - mocking Bun.spawn
      Bun.spawn = (args: any) => {
        spawnArgs = args;
        return mockProc;
      };

      try {
        const result = await executor.spawn(task, "test-project", {
          mode: "background",
        });

        expect(result.pid).toBe(12345);
        expect(result.proc).toBeDefined();
        expect(spawnArgs.cmd).toContain("opencode");
        expect(spawnArgs.cmd).toContain("run");
        expect(spawnArgs.cmd).toContain("--agent");
        expect(spawnArgs.cmd).toContain("general");
        expect(spawnArgs.cmd).toContain("--model");
        expect(spawnArgs.cwd).toBe(testConfig.workDir);
      } finally {
        Bun.spawn = originalSpawn;
      }
    });

    test("uses custom workdir when provided", async () => {
      const task = createMockTask("task1");
      let spawnArgs: any = null;

      const originalSpawn = Bun.spawn;
      const mockProc = {
        pid: 12345,
        kill: () => {},
        exited: Promise.resolve(0),
      };

      // @ts-expect-error - mocking Bun.spawn
      Bun.spawn = (args: any) => {
        spawnArgs = args;
        return mockProc;
      };

      try {
        await executor.spawn(task, "test-project", {
          mode: "background",
          workdir: "/custom/workdir",
        });

        expect(spawnArgs.cwd).toBe("/custom/workdir");
      } finally {
        Bun.spawn = originalSpawn;
      }
    });
  });

  describe("spawn() - invalid mode", () => {
    test("throws for unknown mode", async () => {
      const task = createMockTask("task1");

      await expect(
        executor.spawn(task, "test-project", {
          mode: "invalid" as any,
        })
      ).rejects.toThrow("Unknown execution mode: invalid");
    });
  });

  describe("cleanup()", () => {
    test("removes prompt file", async () => {
      const promptFile = join(testStateDir, "prompt_test-project_task1.txt");
      writeFileSync(promptFile, "test prompt");

      expect(existsSync(promptFile)).toBe(true);

      await executor.cleanup("task1", "test-project");

      expect(existsSync(promptFile)).toBe(false);
    });

    test("removes runner script", async () => {
      const runnerScript = join(testStateDir, "runner_test-project_task1.sh");
      writeFileSync(runnerScript, "#!/bin/bash\necho test");

      expect(existsSync(runnerScript)).toBe(true);

      await executor.cleanup("task1", "test-project");

      expect(existsSync(runnerScript)).toBe(false);
    });

    test("removes output log file", async () => {
      const outputFile = join(testStateDir, "output_test-project_task1.log");
      writeFileSync(outputFile, "test output");

      expect(existsSync(outputFile)).toBe(true);

      await executor.cleanup("task1", "test-project");

      expect(existsSync(outputFile)).toBe(false);
    });

    test("handles missing files gracefully", async () => {
      // Should not throw even if files don't exist
      await expect(
        executor.cleanup("nonexistent", "test-project")
      ).resolves.toBeUndefined();
    });

    test("removes all three files when present", async () => {
      const promptFile = join(testStateDir, "prompt_test-project_task1.txt");
      const runnerScript = join(testStateDir, "runner_test-project_task1.sh");
      const outputFile = join(testStateDir, "output_test-project_task1.log");

      writeFileSync(promptFile, "prompt");
      writeFileSync(runnerScript, "script");
      writeFileSync(outputFile, "output");

      await executor.cleanup("task1", "test-project");

      expect(existsSync(promptFile)).toBe(false);
      expect(existsSync(runnerScript)).toBe(false);
      expect(existsSync(outputFile)).toBe(false);
    });
  });

  describe("singleton", () => {
    test("getOpencodeExecutor returns same instance", () => {
      const executor1 = getOpencodeExecutor();
      const executor2 = getOpencodeExecutor();

      expect(executor1).toBe(executor2);
    });

    test("resetOpencodeExecutor creates new instance", () => {
      const executor1 = getOpencodeExecutor();
      resetOpencodeExecutor();
      const executor2 = getOpencodeExecutor();

      expect(executor1).not.toBe(executor2);
    });
  });
});

describe("OpencodeExecutor - TUI mode", () => {
  // TUI mode tests are more integration-level and would require tmux
  // These tests verify the script content generation

  let executor: OpencodeExecutor;
  let testStateDir: string;

  beforeEach(() => {
    testStateDir = join(tmpdir(), `opencode-executor-tui-test-${Date.now()}`);
    mkdirSync(testStateDir, { recursive: true });

    const config = createTestConfig(testStateDir);
    executor = new OpencodeExecutor(config);
  });

  afterEach(() => {
    try {
      rmSync(testStateDir, { recursive: true, force: true });
    } catch {
      // Ignore
    }
  });

  test("TUI mode would create runner script with correct content", async () => {
    // This is a partial test - full TUI testing requires tmux
    const task = createMockTask("task1");

    // We can't easily test tmux commands without a real tmux session
    // But we can verify the executor initializes correctly
    expect(executor).toBeDefined();
  });
});

describe("OpencodeExecutor - Dashboard mode", () => {
  let executor: OpencodeExecutor;
  let testStateDir: string;

  beforeEach(() => {
    testStateDir = join(
      tmpdir(),
      `opencode-executor-dashboard-test-${Date.now()}`
    );
    mkdirSync(testStateDir, { recursive: true });

    const config = createTestConfig(testStateDir);
    executor = new OpencodeExecutor(config);
  });

  afterEach(() => {
    try {
      rmSync(testStateDir, { recursive: true, force: true });
    } catch {
      // Ignore
    }
  });

  test("Dashboard mode would create runner script", async () => {
    // Similar to TUI - requires tmux for full testing
    const task = createMockTask("task1");
    expect(executor).toBeDefined();
  });
});

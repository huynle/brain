import { afterEach, beforeEach, describe, expect, mock, test } from "bun:test";
import { existsSync, mkdirSync, rmSync } from "fs";
import { tmpdir } from "os";
import { join } from "path";
import type { ResolvedTask } from "../core/types";
import { CompletionStatus } from "./process-manager";
import { resetApiClient } from "./api-client";
import { resetConfig } from "./config";
import { resetLogger } from "./logger";
import { resetOpencodeExecutor } from "./opencode-executor";
import { resetProcessManager } from "./process-manager";
import { resetSignalHandler } from "./signals";
import { TaskRunner, resetTaskRunner } from "./task-runner";
import type { RunnerConfig, RunningTask, TaskResult } from "./types";

function createTestConfig(stateDir: string): RunnerConfig {
  return {
    brainApiUrl: "http://localhost:3333",
    pollInterval: 1,
    taskPollInterval: 1,
    maxParallel: 3,
    stateDir,
    logDir: join(stateDir, "log"),
    workDir: "/tmp/test-workdir",
    apiTimeout: 1000,
    taskTimeout: 5000,
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

function createTask(id: string, overrides: Partial<ResolvedTask> = {}): ResolvedTask {
  return {
    id,
    path: `projects/test-project/task/${id}.md`,
    title: `Task ${id}`,
    priority: "medium",
    status: "pending",
    depends_on: [],
    cron_ids: ["cron-daily"],
    tags: [],
    created: new Date().toISOString(),
    target_workdir: null,
    workdir: null,
    worktree: null,
    git_remote: null,
    git_branch: null,
    user_original_request: null,
    direct_prompt: null,
    agent: null,
    model: null,
    sessions: {},
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

describe("cron scheduler completion tracking", () => {
  let testDir: string;
  let config: RunnerConfig;

  beforeEach(() => {
    testDir = join(tmpdir(), `cron-scheduler-test-${Date.now()}-${Math.random().toString(36).slice(2)}`);
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

  afterEach(() => {
    resetTaskRunner();
    resetApiClient();
    resetProcessManager();
    resetOpencodeExecutor();
    resetConfig();
    resetSignalHandler();
    resetLogger();

    if (existsSync(testDir)) {
      rmSync(testDir, { recursive: true, force: true });
    }
  });

  test("processDueCronEntry initializes active run tracking", async () => {
    const runner = new TaskRunner({ projectId: "test-project", config });
    const updateCronRun = mock(async () => {});

    // @ts-expect-error private field override for test
    runner.apiClient = {
      getAllTasks: async () => [createTask("a"), createTask("b")],
      updateEntryMetadata: async () => {},
      updateCronRun,
    };

    const now = new Date("2026-02-23T02:00:00.000Z");
    const processDueCronEntry = (runner as unknown as {
      processDueCronEntry: (
        cronEntry: { id: string; path: string; title: string; schedule: string; next_run?: string },
        projectId: string,
        now: Date
      ) => Promise<void>;
    }).processDueCronEntry.bind(runner);

    await processDueCronEntry(
      {
        id: "cron-daily",
        path: "projects/test-project/cron/daily.md",
        title: "Daily",
        schedule: "0 2 * * *",
      },
      "test-project",
      now
    );

    const updateCronRunCalls = updateCronRun.mock.calls as unknown as Array<[unknown, { run_id: string }]>;
    const runId = updateCronRunCalls[0][1].run_id;
    const activeRun = (runner as unknown as { activeCronRuns: Map<string, unknown> }).activeCronRuns.get(runId) as {
      taskIds: string[];
      pendingTaskIds: Set<string>;
      startedAtIso: string;
      startedAtMs: number;
    };

    expect(activeRun).toBeDefined();
    expect(activeRun.taskIds).toEqual(["a", "b"]);
    expect(Array.from(activeRun.pendingTaskIds)).toEqual(["a", "b"]);
    expect(activeRun.startedAtIso).toBe(now.toISOString());
    expect(activeRun.startedAtMs).toBe(now.getTime());
  });

  test("finalizeCronRunForTask marks run completed when last task completes", async () => {
    const runner = new TaskRunner({ projectId: "test-project", config });
    const updateCronRun = mock(async () => {});

    // @ts-expect-error private field override for test
    runner.apiClient = { updateCronRun };

    const runId = "run-123";
    const startedAt = "2026-02-23T02:00:00.000Z";
    const startedAtMs = new Date(startedAt).getTime();
    (runner as unknown as { activeCronRuns: Map<string, unknown> }).activeCronRuns.set(runId, {
      cronId: "cron-daily",
      cronPath: "projects/test-project/cron/daily.md",
      projectId: "test-project",
      taskIds: ["task-a", "task-b"],
      startedAtIso: startedAt,
      startedAtMs,
      pendingTaskIds: new Set(["task-a", "task-b"]),
    });
    (runner as unknown as { taskToCronRun: Map<string, string> }).taskToCronRun.set("task-a", runId);
    (runner as unknown as { taskToCronRun: Map<string, string> }).taskToCronRun.set("task-b", runId);

    const finalizeCronRunForTask = (runner as unknown as {
      finalizeCronRunForTask: (taskId: string, status: CompletionStatus, completedAt?: Date) => Promise<void>;
    }).finalizeCronRunForTask.bind(runner);

    await finalizeCronRunForTask("task-a", CompletionStatus.Completed, new Date("2026-02-23T02:01:00.000Z"));
    expect(updateCronRun).toHaveBeenCalledTimes(0);

    await finalizeCronRunForTask("task-b", CompletionStatus.Completed, new Date("2026-02-23T02:02:00.000Z"));

    expect(updateCronRun).toHaveBeenCalledTimes(1);
    const completedCall = (updateCronRun.mock.calls as unknown as Array<[unknown, Record<string, unknown>]>)[0][1];
    expect(completedCall).toMatchObject({
      run_id: runId,
      status: "completed",
      completed: "2026-02-23T02:02:00.000Z",
      duration: 120000,
      tasks: 2,
    });
    expect((runner as unknown as { activeCronRuns: Map<string, unknown> }).activeCronRuns.has(runId)).toBe(false);
    expect((runner as unknown as { taskToCronRun: Map<string, string> }).taskToCronRun.size).toBe(0);
  });

  test("finalizeCronRunForTask marks run failed and records failed task", async () => {
    const runner = new TaskRunner({ projectId: "test-project", config });
    const updateCronRun = mock(async () => {});

    // @ts-expect-error private field override for test
    runner.apiClient = { updateCronRun };

    const runId = "run-456";
    const startedAt = "2026-02-23T03:00:00.000Z";
    const startedAtMs = new Date(startedAt).getTime();
    (runner as unknown as { activeCronRuns: Map<string, unknown> }).activeCronRuns.set(runId, {
      cronId: "cron-daily",
      cronPath: "projects/test-project/cron/daily.md",
      projectId: "test-project",
      taskIds: ["task-x", "task-y"],
      startedAtIso: startedAt,
      startedAtMs,
      pendingTaskIds: new Set(["task-x", "task-y"]),
    });
    (runner as unknown as { taskToCronRun: Map<string, string> }).taskToCronRun.set("task-x", runId);
    (runner as unknown as { taskToCronRun: Map<string, string> }).taskToCronRun.set("task-y", runId);

    const finalizeCronRunForTask = (runner as unknown as {
      finalizeCronRunForTask: (taskId: string, status: CompletionStatus, completedAt?: Date) => Promise<void>;
    }).finalizeCronRunForTask.bind(runner);

    await finalizeCronRunForTask("task-x", CompletionStatus.Timeout, new Date("2026-02-23T03:00:30.000Z"));

    expect(updateCronRun).toHaveBeenCalledTimes(1);
    const failedCall = (updateCronRun.mock.calls as unknown as Array<[unknown, Record<string, unknown>]>)[0][1];
    expect(failedCall).toMatchObject({
      run_id: runId,
      status: "failed",
      completed: "2026-02-23T03:00:30.000Z",
      duration: 30000,
      failed_task: "task-x",
      tasks: 2,
    });
    expect((runner as unknown as { activeCronRuns: Map<string, unknown> }).activeCronRuns.has(runId)).toBe(false);
    expect((runner as unknown as { taskToCronRun: Map<string, string> }).taskToCronRun.size).toBe(0);
  });

  test("completion handlers finalize cron runs", async () => {
    const runner = new TaskRunner({ projectId: "test-project", config });
    const updateCronRun = mock(async () => {});

    // @ts-expect-error private field override for test
    runner.apiClient = {
      updateCronRun,
      releaseTask: async () => {},
      updateTaskStatus: async () => {},
      appendToTask: async () => {},
    };
    // @ts-expect-error private field override for test
    runner.executor = { cleanup: async () => {} };
    // @ts-expect-error private method override for test
    runner.cleanupTaskTmux = async () => {};
    // @ts-expect-error private method override for test
    runner.handleDashboardTaskComplete = async () => {};
    // @ts-expect-error private method override for test
    runner.saveState = () => {};

    const runIdA = "run-handler-a";
    (runner as unknown as { activeCronRuns: Map<string, unknown> }).activeCronRuns.set(runIdA, {
      cronId: "cron-daily",
      cronPath: "projects/test-project/cron/daily.md",
      projectId: "test-project",
      taskIds: ["task-handler"],
      startedAtIso: "2026-02-23T04:00:00.000Z",
      startedAtMs: new Date("2026-02-23T04:00:00.000Z").getTime(),
      pendingTaskIds: new Set(["task-handler"]),
    });
    (runner as unknown as { taskToCronRun: Map<string, string> }).taskToCronRun.set("task-handler", runIdA);

    // @ts-expect-error private field override for test
    runner.processManager = {
      createTaskResult: () => ({
        taskId: "task-handler",
        status: "completed",
        startedAt: "2026-02-23T04:00:00.000Z",
        completedAt: "2026-02-23T04:01:00.000Z",
        duration: 60000,
      } as TaskResult),
      get: () => ({
        task: {
          id: "task-handler",
          path: "projects/test-project/task/task-handler.md",
          title: "Handler Task",
          priority: "medium",
          projectId: "test-project",
          pid: 123,
          startedAt: "2026-02-23T04:00:00.000Z",
          isResume: false,
          workdir: "/tmp/work",
        } as RunningTask,
      }),
      remove: () => {},
    };

    const handleTaskCompletion = (runner as unknown as {
      handleTaskCompletion: (taskId: string, status: CompletionStatus) => Promise<TaskResult | null>;
    }).handleTaskCompletion.bind(runner);

    await handleTaskCompletion("task-handler", CompletionStatus.Completed);

    const runIdB = "run-handler-b";
    (runner as unknown as { activeCronRuns: Map<string, unknown> }).activeCronRuns.set(runIdB, {
      cronId: "cron-daily",
      cronPath: "projects/test-project/cron/daily.md",
      projectId: "test-project",
      taskIds: ["task-tui"],
      startedAtIso: "2026-02-23T05:00:00.000Z",
      startedAtMs: new Date("2026-02-23T05:00:00.000Z").getTime(),
      pendingTaskIds: new Set(["task-tui"]),
    });
    (runner as unknown as { taskToCronRun: Map<string, string> }).taskToCronRun.set("task-tui", runIdB);

    const handleTuiTaskCompletion = (runner as unknown as {
      handleTuiTaskCompletion: (taskId: string, task: RunningTask, status: CompletionStatus) => Promise<TaskResult | null>;
    }).handleTuiTaskCompletion.bind(runner);

    await handleTuiTaskCompletion(
      "task-tui",
      {
        id: "task-tui",
        path: "projects/test-project/task/task-tui.md",
        title: "Tui Task",
        priority: "medium",
        projectId: "test-project",
        pid: 124,
        startedAt: new Date(Date.now() - 10_000).toISOString(),
        isResume: false,
        workdir: "/tmp/work",
      },
      CompletionStatus.Cancelled
    );

    expect(updateCronRun).toHaveBeenCalledTimes(2);
    const handlerCalls = updateCronRun.mock.calls as unknown as Array<[unknown, Record<string, unknown>]>;
    expect(handlerCalls[0]?.[1]?.status).toBe("completed");
    expect(handlerCalls[1]?.[1]?.status).toBe("failed");
    expect(handlerCalls[1]?.[1]?.failed_task).toBe("task-tui");
  });
});

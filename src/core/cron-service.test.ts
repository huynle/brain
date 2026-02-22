import { describe, expect, it } from "bun:test";
import type { Task } from "./types";
import {
  parseCronExpression,
  getNextRun,
  shouldTrigger,
  resolveCronPipeline,
  canTriggerPipeline,
  generateRunId,
} from "./cron-service";

function mockTask(overrides: Partial<Task> = {}): Task {
  return {
    id: "task1",
    path: "projects/brain-api/task/task1.md",
    title: "Task 1",
    priority: "medium",
    status: "pending",
    depends_on: [],
    tags: [],
    cron_ids: [],
    created: "2026-01-01T00:00:00.000Z",
    modified: "2026-01-01T00:00:00.000Z",
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
    ...overrides,
  };
}

describe("parseCronExpression", () => {
  it("parses wildcard, step, range, and list expressions", () => {
    const parsed = parseCronExpression("*/15 9-17 * * 1,3,5");

    expect(parsed.minute.values).toEqual([0, 15, 30, 45]);
    expect(parsed.hour.values).toEqual([9, 10, 11, 12, 13, 14, 15, 16, 17]);
    expect(parsed.dayOfMonth.any).toBe(true);
    expect(parsed.month.any).toBe(true);
    expect(parsed.dayOfWeek.values).toEqual([1, 3, 5]);
  });

  it("throws on invalid field count", () => {
    expect(() => parseCronExpression("0 2 * *")).toThrow();
  });

  it("throws on out-of-range values", () => {
    expect(() => parseCronExpression("61 * * * *")).toThrow();
    expect(() => parseCronExpression("* 24 * * *")).toThrow();
    expect(() => parseCronExpression("* * 0 * *")).toThrow();
    expect(() => parseCronExpression("* * * 13 *")).toThrow();
    expect(() => parseCronExpression("* * * * 7")).toThrow();
  });

  it("throws on invalid steps", () => {
    expect(() => parseCronExpression("*/0 * * * *")).toThrow();
    expect(() => parseCronExpression("*/-2 * * * *")).toThrow();
  });
});

describe("getNextRun", () => {
  it("calculates next interval minute", () => {
    const after = new Date("2026-03-01T10:07:12.000Z");
    const next = getNextRun("*/15 * * * *", after);

    expect(next.toISOString()).toBe("2026-03-01T10:15:00.000Z");
  });

  it("is strictly after the provided date", () => {
    const after = new Date("2026-03-01T00:00:00.000Z");
    const next = getNextRun("0 0 * * *", after);

    expect(next.toISOString()).toBe("2026-03-02T00:00:00.000Z");
  });

  it("handles weekday schedules", () => {
    const after = new Date("2026-03-06T14:30:00.000Z"); // Friday
    const next = getNextRun("30 14 * * 1-5", after);

    expect(next.toISOString()).toBe("2026-03-09T14:30:00.000Z"); // Monday
  });
});

describe("shouldTrigger", () => {
  it("returns true when next_run is due", () => {
    const cronEntry = {
      schedule: "*/15 * * * *",
      next_run: "2026-03-01T10:15:00.000Z",
    };

    const now = new Date("2026-03-01T10:15:02.000Z");
    expect(shouldTrigger(cronEntry, now)).toBe(true);
  });

  it("returns false when next_run is in the future", () => {
    const cronEntry = {
      schedule: "*/15 * * * *",
      next_run: "2026-03-01T10:30:00.000Z",
    };

    const now = new Date("2026-03-01T10:15:00.000Z");
    expect(shouldTrigger(cronEntry, now)).toBe(false);
  });

  it("falls back to schedule matching when next_run is missing", () => {
    const now = new Date("2026-03-01T10:15:30.000Z");

    expect(shouldTrigger({ schedule: "*/15 * * * *" }, now)).toBe(true);
    expect(shouldTrigger({ schedule: "*/15 * * * *" }, new Date("2026-03-01T10:16:00.000Z"))).toBe(false);
  });
});

describe("resolveCronPipeline", () => {
  it("resolves upstream cron-only pipeline for mixed dependencies", () => {
    const tasks = [
      mockTask({ id: "a", title: "A", cron_ids: ["cron-nightly"] }),
      mockTask({ id: "b", title: "B", depends_on: ["a"] }),
      mockTask({ id: "c", title: "C", depends_on: ["b"], cron_ids: ["cron-nightly"] }),
      mockTask({ id: "d", title: "D", depends_on: ["c"] }),
      mockTask({ id: "e", title: "E", depends_on: ["a"], cron_ids: ["other-cron"] }),
    ];

    const pipeline = resolveCronPipeline("cron-nightly", tasks);

    expect(pipeline.map((t) => t.id)).toEqual(["a", "c"]);
  });

  it("leaf cron can resolve full upstream cron chain", () => {
    const tasks = [
      mockTask({ id: "root", cron_ids: ["leaf-cron"] }),
      mockTask({ id: "mid", depends_on: ["root"], cron_ids: ["leaf-cron"] }),
      mockTask({ id: "leaf", depends_on: ["mid"], cron_ids: ["leaf-cron"] }),
    ];

    expect(resolveCronPipeline("leaf-cron", tasks).map((t) => t.id)).toEqual([
      "root",
      "mid",
      "leaf",
    ]);
  });

  it("root cron triggers only itself when descendants are not tagged", () => {
    const tasks = [
      mockTask({ id: "root", cron_ids: ["root-cron"] }),
      mockTask({ id: "child", depends_on: ["root"] }),
      mockTask({ id: "grandchild", depends_on: ["child"] }),
    ];

    expect(resolveCronPipeline("root-cron", tasks).map((t) => t.id)).toEqual(["root"]);
  });
});

describe("canTriggerPipeline", () => {
  it("prevents overlap when a pipeline task is already in progress", () => {
    const pipeline = [
      mockTask({ id: "a", status: "pending" }),
      mockTask({ id: "b", status: "in_progress" }),
    ];

    expect(canTriggerPipeline(pipeline)).toEqual({
      canTrigger: false,
      reason: "task b already in_progress",
    });
  });

  it("allows trigger when no task is in progress", () => {
    const pipeline = [
      mockTask({ id: "a", status: "pending" }),
      mockTask({ id: "b", status: "completed" }),
    ];

    expect(canTriggerPipeline(pipeline)).toEqual({ canTrigger: true });
  });
});

describe("generateRunId", () => {
  it("formats run ID as YYYYMMDD-HHmm in UTC", () => {
    const runId = generateRunId(new Date("2026-03-01T04:07:59.000Z"));
    expect(runId).toBe("20260301-0407");
  });
});

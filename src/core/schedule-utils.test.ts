import { describe, expect, it } from "bun:test";
import {
  parseCronExpression,
  getNextRun,
  shouldTrigger,
  canRunWithinBounds,
  generateRunId,
} from "./schedule-utils";
import type { CronRun } from "./types";

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

  it("returns true for one-shot entries with due next_run and no schedule", () => {
    const now = new Date("2026-03-01T10:15:30.000Z");
    expect(shouldTrigger({ next_run: "2026-03-01T10:15:00.000Z" }, now)).toBe(true);
  });

  it("returns false when both schedule and next_run are missing", () => {
    expect(shouldTrigger({}, new Date("2026-03-01T10:15:30.000Z"))).toBe(false);
  });
});

describe("canRunWithinBounds", () => {
  it("blocks when max_runs completed/failed limit is reached", () => {
    const result = canRunWithinBounds(
      {
        max_runs: 2,
        runs: [
          { run_id: "r1", status: "completed", started: "2026-03-01T00:00:00.000Z" },
          { run_id: "r2", status: "failed", started: "2026-03-02T00:00:00.000Z" },
          { run_id: "r3", status: "skipped", started: "2026-03-03T00:00:00.000Z" },
        ],
      },
      new Date("2026-03-04T00:00:00.000Z")
    );

    expect(result.canRun).toBe(false);
    expect(result.reason).toContain("max_runs");
  });

  it("default mode ignores skipped and in-progress attempts for max_runs", () => {
    const result = canRunWithinBounds(
      {
        max_runs: 1,
        runs: [
          { run_id: "r1", status: "skipped", started: "2026-03-01T00:00:00.000Z" },
          { run_id: "r2", status: "in_progress", started: "2026-03-02T00:00:00.000Z" },
        ],
      },
      new Date("2026-03-03T00:00:00.000Z")
    );

    expect(result).toEqual({ canRun: true });
  });

  it("attempt mode counts skipped and in-progress attempts toward max_runs", () => {
    const result = canRunWithinBounds(
      {
        max_runs: 2,
        runs: [
          { run_id: "r1", status: "completed", started: "2026-03-01T00:00:00.000Z" },
          { run_id: "r2", status: "skipped", started: "2026-03-02T00:00:00.000Z" },
        ],
      },
      new Date("2026-03-03T00:00:00.000Z"),
      { countAttemptsForMaxRuns: true }
    );

    expect(result.canRun).toBe(false);
    expect(result.reason).toContain("max_runs");
  });

  it("attempt mode counts legacy active run status toward max_runs", () => {
    const result = canRunWithinBounds(
      {
        max_runs: 1,
        runs: [
          {
            run_id: "r1",
            status: "active",
            started: "2026-03-01T00:00:00.000Z",
          },
        ] as unknown as CronRun[],
      },
      new Date("2026-03-03T00:00:00.000Z"),
      { countAttemptsForMaxRuns: true }
    );

    expect(result.canRun).toBe(false);
    expect(result.reason).toContain("max_runs");
  });

  it("blocks before starts_at and after expires_at", () => {
    const tooEarly = canRunWithinBounds(
      { starts_at: "2026-03-05T10:00:00.000Z" },
      new Date("2026-03-05T09:59:00.000Z")
    );
    expect(tooEarly.canRun).toBe(false);
    expect(tooEarly.reason).toContain("starts_at");

    const tooLate = canRunWithinBounds(
      { expires_at: "2026-03-05T10:00:00.000Z" },
      new Date("2026-03-05T10:01:00.000Z")
    );
    expect(tooLate.canRun).toBe(false);
    expect(tooLate.reason).toContain("expires_at");
  });

  it("allows runs when within bounds and below max_runs", () => {
    const result = canRunWithinBounds(
      {
        max_runs: 2,
        starts_at: "2026-03-01T00:00:00.000Z",
        expires_at: "2026-03-31T23:59:59.000Z",
        runs: [{ run_id: "r1", status: "completed", started: "2026-03-02T00:00:00.000Z" }],
      },
      new Date("2026-03-10T12:00:00.000Z")
    );

    expect(result).toEqual({ canRun: true });
  });
});

describe("generateRunId", () => {
  it("formats run ID with UTC minute prefix and uniqueness suffix", () => {
    const runId = generateRunId(new Date("2026-03-01T04:07:59.000Z"));

    expect(runId).toMatch(/^20260301-0407-[a-z0-9]{6}$/);
  });

  it("is collision-safe for repeated calls in the same minute", () => {
    const triggerTime = new Date("2026-03-01T04:07:12.000Z");

    const first = generateRunId(triggerTime);
    const second = generateRunId(triggerTime);

    expect(first).not.toBe(second);
  });
});

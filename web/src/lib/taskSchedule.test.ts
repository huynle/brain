/**
 * Tests for lib/taskSchedule.
 *
 * The precedence cases carry the weight here. The runner checks
 * schedule_enabled, then the time window, then max_runs, and a task that
 * has been auto-disabled trips SEVERAL of those at once — so a chip that
 * reported the first truthy condition instead of the governing one would
 * say "expired" about a task whose schedule someone turned off by hand,
 * and vice versa.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import { countScheduleRuns, taskScheduleChip } from "./taskSchedule";
import type { Task } from "./types";

/** A task carrying only what these tests read. */
const task = (over: Partial<Task>): Task =>
  ({
    id: "t1",
    path: "projects/p/task/t1.md",
    title: "T",
    priority: "medium",
    status: "active",
    ...over,
  }) as Task;

const NOW = new Date("2026-09-03T18:00:00Z");

test("a task with no schedule gets no chip", () => {
  assert.equal(taskScheduleChip(task({}), NOW), null);
  assert.equal(taskScheduleChip(task({ schedule: "" }), NOW), null);
});

test("a live recurring schedule reports cadence and next run", () => {
  const c = taskScheduleChip(
    task({ schedule: "0 * * * *", timezone: "UTC" }),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.code, "recurring");
  assert.equal(c.glyph, "⟳");
  assert.match(c.short, /^hourly · /);
  // The reset rule is the thing users get wrong; it belongs in the tooltip.
  assert.match(c.detail, /Completing it does not end the schedule/);
});

test("countScheduleRuns counts in_progress alongside settled runs", () => {
  // Mirrors countRuns in the runner: an in-flight run already spends budget.
  assert.equal(
    countScheduleRuns([
      { status: "completed" },
      { status: "failed" },
      { status: "skipped" },
      { status: "in_progress" },
    ]),
    4,
  );
  assert.equal(countScheduleRuns([{ status: "cancelled" }]), 0);
  assert.equal(countScheduleRuns(undefined), 0);
});

test("schedule_enabled=false reads as stopped, not as a cadence", () => {
  const c = taskScheduleChip(
    task({ schedule: "0 2 * * *", schedule_enabled: false }),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.code, "stopped");
  assert.equal(c.short, "sched off");
  assert.match(c.detail, /DISABLED/);
  assert.match(c.detail, /schedule_enabled back to true/);
});

test("an auto-disable after expiry names expiry as the cause", () => {
  const c = taskScheduleChip(
    task({
      schedule: "0 2 * * *",
      schedule_enabled: false,
      expires_at: "2026-08-01T00:00:00Z",
    }),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.code, "stopped");
  assert.match(c.detail, /expires_at .* has passed/);
});

test("an auto-disable after max_runs names the cap as the cause", () => {
  const c = taskScheduleChip(
    task({
      schedule: "0 2 * * *",
      schedule_enabled: false,
      max_runs: 3,
      runs: [
        { status: "completed" },
        { status: "completed" },
        { status: "failed" },
      ],
    }),
    NOW,
  );
  assert.ok(c);
  assert.match(c.detail, /max_runs cap/);
});

test("schedule_enabled=false wins over an unexpired window", () => {
  // Precedence: the master switch is checked before the window, so a task
  // turned off by hand must not advertise a future next-run.
  const c = taskScheduleChip(
    task({
      schedule: "0 * * * *",
      schedule_enabled: false,
      expires_at: "2027-01-01T00:00:00Z",
    }),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.code, "stopped");
  assert.match(c.detail, /disabled by hand|records the reason/);
});

test("a passed expires_at reads as expired while still enabled", () => {
  // The runner only flips schedule_enabled on its next poll, so this gap is
  // real and the row must not claim the task is still recurring.
  const c = taskScheduleChip(
    task({
      schedule: "0 * * * *",
      schedule_enabled: true,
      expires_at: "2026-08-01T00:00:00Z",
    }),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.code, "stopped");
  assert.equal(c.short, "expired");
  assert.match(c.detail, /disabled the next time a runner polls/);
});

test("a spent run budget reads as stopped and shows the count", () => {
  const c = taskScheduleChip(
    task({
      schedule: "0 * * * *",
      max_runs: 2,
      runs: [{ status: "completed" }, { status: "in_progress" }],
    }),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.code, "stopped");
  assert.equal(c.short, "2/2 runs");
});

test("a future starts_at reads as waiting, not stopped", () => {
  const c = taskScheduleChip(
    task({ schedule: "0 * * * *", starts_at: "2026-09-10T00:00:00Z" }),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.code, "waiting");
  assert.match(c.short, /^starts in /);
  assert.match(c.detail, /opens on\s+its own/);
});

test("expiry outranks a future starts_at", () => {
  // A window that closed before it opened is stopped, not waiting.
  const c = taskScheduleChip(
    task({
      schedule: "0 * * * *",
      starts_at: "2026-09-10T00:00:00Z",
      expires_at: "2026-08-01T00:00:00Z",
    }),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.code, "stopped");
});

test("a pending one-shot reports when it fires", () => {
  const c = taskScheduleChip(
    task({ run_once_at: "2026-09-04T18:00:00Z" }),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.code, "once");
  assert.equal(c.glyph, "⌖");
  assert.match(c.short, /^once in /);
});

test("a one-shot whose time has passed reads as due", () => {
  const c = taskScheduleChip(
    task({ run_once_at: "2026-09-03T17:00:00Z" }),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.short, "due now");
  assert.match(c.detail, /next runner poll/);
});

test("cron wins over run_once_at, and the tooltip says so", () => {
  // The runner's branch is `run_once_at != "" && schedule == ""`, so a task
  // with both is recurring and its run_once_at never fires.
  const c = taskScheduleChip(
    task({ schedule: "0 * * * *", run_once_at: "2026-09-04T18:00:00Z" }),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.code, "recurring");
  assert.match(c.detail, /run_once_at, which the runner ignores/);
});

test("an unparseable schedule is called out rather than shown as a cadence", () => {
  const c = taskScheduleChip(task({ schedule: "every hour plz" }), NOW);
  assert.ok(c);
  assert.equal(c.code, "stopped");
  assert.equal(c.short, "bad schedule");
});

test("a schedule matching no real date is called out", () => {
  const c = taskScheduleChip(task({ schedule: "0 0 30 2 *" }), NOW);
  assert.ok(c);
  assert.equal(c.code, "stopped");
  assert.equal(c.short, "bad schedule");
});

test("an unparseable run_once_at is called out", () => {
  const c = taskScheduleChip(task({ run_once_at: "not-a-date" }), NOW);
  assert.ok(c);
  assert.equal(c.code, "stopped");
  assert.equal(c.short, "bad run_once_at");
});

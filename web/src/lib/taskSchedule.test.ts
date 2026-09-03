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

test("the chip reports the stored next_run, not its own prediction", () => {
  // shouldTrigger compares against the STORED next_run when it is set, so a
  // chip predicting from the expression can contradict the detail pane — and
  // be wrong about what actually happens. Seen live: the header chip said
  // "in 8m" while the Next run row said "in 1h", on the same screen.
  const c = taskScheduleChip(
    task({
      schedule: "0 * * * *",
      timezone: "UTC",
      next_run: "2026-09-03T21:00:00Z", // 3h out, not the next :00
    }),
    NOW, // 2026-09-03T18:00:00Z
  );
  assert.ok(c);
  assert.equal(c.code, "recurring");
  assert.equal(c.short, "hourly · in 3h");
  assert.match(c.detail, /stored next_run/);
});

test("with no stored next_run the chip predicts and says so", () => {
  const c = taskScheduleChip(
    task({ schedule: "0 * * * *", timezone: "UTC" }),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.short, "hourly · in 1h");
  assert.match(c.detail, /predicted/);
});

test("a stored next_run in the past reads as due, not as elapsed", () => {
  // The runner will fire this on its next poll; "2h ago" would read as a
  // missed run rather than an imminent one.
  const c = taskScheduleChip(
    task({
      schedule: "0 * * * *",
      timezone: "UTC",
      next_run: "2026-09-03T16:00:00Z",
    }),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.short, "hourly · due now");
});

test("a stopped schedule ignores a stale stored next_run", () => {
  const c = taskScheduleChip(
    task({
      schedule: "0 * * * *",
      schedule_enabled: false,
      next_run: "2027-01-01T00:00:00Z",
    }),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.code, "stopped");
  assert.equal(c.short, "sched off");
});

// ── Status gate ────────────────────────────────────────────────────────────
// The runner's FIRST per-task gate after schedule_enabled is
// cronEligibleStatuses {active, completed, blocked}, and it sits ahead of the
// window, max_runs and shouldTrigger. The chip ignored task.status entirely,
// so a cancelled recurring task rendered as armed with a next-run time.

test("a status the runner will not trigger from reads as ineligible", () => {
  for (const status of [
    "cancelled",
    "archived",
    "superseded",
    "validated",
    "draft",
    "pending",
    "in_progress",
  ]) {
    const c = taskScheduleChip(
      task({ schedule: "0 * * * *", timezone: "UTC", status } as Partial<Task>),
      NOW,
    );
    assert.ok(c, `${status} should still render a chip`);
    assert.equal(c.code, "ineligible", `${status} must not read as armed`);
    assert.equal(c.short, `not while ${status}`);
  }
});

test("the three eligible statuses still read as armed", () => {
  for (const status of ["active", "completed", "blocked"]) {
    const c = taskScheduleChip(
      task({ schedule: "0 * * * *", timezone: "UTC", status } as Partial<Task>),
      NOW,
    );
    assert.ok(c);
    assert.equal(c.code, "recurring", `${status} is cron-eligible`);
  }
});

test("schedule_enabled=false outranks the status gate", () => {
  // The runner checks schedule_enabled first, so an explicitly disabled
  // schedule must report that rather than the status.
  const c = taskScheduleChip(
    task({
      schedule: "0 * * * *",
      schedule_enabled: false,
      status: "cancelled",
    } as Partial<Task>),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.code, "stopped");
});

// ── RFC3339 strictness ─────────────────────────────────────────────────────
// `new Date()` accepts strings Go's time.Parse(time.RFC3339) rejects, and the
// runner IGNORES an unparseable window bound (checkTimeWindow treats it as
// unset). Honouring it here reported the exact opposite of what runs.

test("an expires_at the runner cannot parse is ignored, not honoured", () => {
  for (const bad of ["2026-08-01", "2026-08-01 00:00:00", "01/08/2026"]) {
    const c = taskScheduleChip(
      task({ schedule: "0 * * * *", timezone: "UTC", expires_at: bad }),
      NOW,
    );
    assert.ok(c);
    assert.equal(
      c.code,
      "recurring",
      `${bad} is not RFC3339, so checkTimeWindow ignores it and the task is still live`,
    );
  }
});

test("a starts_at the runner cannot parse does not hold the task", () => {
  const c = taskScheduleChip(
    task({ schedule: "0 * * * *", timezone: "UTC", starts_at: "2026-12-01" }),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.code, "recurring");
});

test("a next_run the runner cannot parse falls back to cron matching", () => {
  // shouldTrigger only uses next_run when time.Parse succeeds; otherwise it
  // matches the live clock. Displaying the bad value as authoritative was
  // wrong on both counts.
  const c = taskScheduleChip(
    task({
      schedule: "0 * * * *",
      timezone: "UTC",
      next_run: "2026-09-03 19:00:00",
    }),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.short, "hourly · in 1h");
  assert.match(c.detail, /predicted/);
});

test("a stored next_run cannot mask an unparseable expression", () => {
  // The stored value used to short-circuit the cron parse, making the
  // "bad schedule" arm unreachable whenever next_run was set.
  const c = taskScheduleChip(
    task({ schedule: "every hour plz", next_run: "2026-09-03T19:00:00Z" }),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.code, "stopped");
  assert.equal(c.short, "bad schedule");
});

// ── One-shots and gates finish; they do not fail ───────────────────────────
// processRunOnceTask and processFeatureScheduleGate both write
// schedule_enabled:false directly, WITHOUT going through disableSchedule —
// so neither leaves the "## Schedule Disabled" body note that the generic
// stopped wording tells the user to go read.

test("a one-shot that fired reads as done, not as a fault", () => {
  const c = taskScheduleChip(
    task({
      run_once_at: "2026-09-02T15:00:00Z",
      schedule_enabled: false,
      status: "completed",
      runs: [{ status: "completed" }],
    }),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.code, "done");
  assert.equal(c.short, "fired");
  assert.match(c.detail, /normal end state/);
  // Must NOT send the user hunting for a note that was never written.
  assert.doesNotMatch(c.detail, /body records the reason/);
});

test("a fired feature_schedule gate reads as done", () => {
  const c = taskScheduleChip(
    task({
      schedule: "0 2 * * *",
      generated_kind: "feature_schedule",
      schedule_enabled: false,
      status: "completed",
    } as Partial<Task>),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.code, "done");
  assert.equal(c.short, "gate fired");
  assert.match(c.detail, /does not recur/);
});

test("an unfired gate is a one-time firing, not a cadence", () => {
  // The gate carries a cron expression, but processFeatureScheduleGate runs
  // it once and completes the task — so "daily 02:00 · in 8h" was describing
  // a recurrence that never happens.
  const c = taskScheduleChip(
    task({
      schedule: "0 2 * * *",
      timezone: "UTC",
      generated_kind: "feature_schedule",
      status: "active",
    } as Partial<Task>),
    NOW,
  );
  assert.ok(c);
  assert.equal(c.code, "once");
  assert.match(c.short, /^gate /);
  assert.match(c.detail, /does not recur/);
  // And it must never claim completing it keeps the schedule alive.
  assert.doesNotMatch(c.detail, /Completing it does not end the schedule/);
});

test("an ordinary disabled schedule still reports as stopped", () => {
  // The done branch must not swallow the genuine auto-disable case.
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
});

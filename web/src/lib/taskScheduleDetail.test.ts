/**
 * Tests for the data decisions behind TaskScheduleSection.
 *
 * The section itself is presentation, but two of its choices are load-bearing
 * and easy to regress into something confidently wrong:
 *
 *   1. Which "next run" to show. The runner's shouldTrigger compares against
 *      the STORED next_run when it is set, so displaying a locally computed
 *      time instead would describe a firing that will not happen. next_run is
 *      absent until a task first fires, which is precisely when a user wants
 *      reassurance, so the fallback has to exist — and has to be labelled.
 *   2. Whether a completed task re-arms. `completed` is the idle state for a
 *      recurring task (cronEligibleStatuses includes it), and the terminal
 *      state for a stopped one. Getting this backwards tells someone their
 *      nightly job is dead when it is healthy, or vice versa.
 *
 * Both are pure predicates over a Task, so they are pinned here rather than
 * through the DOM.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import { countScheduleRuns, taskScheduleChip } from "./taskSchedule";
import { nextCronRun } from "./cronSchedule";
import { showsRearmNotice } from "../components/Modal/TaskScheduleSection";
import type { Task } from "./types";

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

/** The section's own rule: server value wins, prediction is the fallback. */
function nextRunSource(t: Task): "server" | "predicted" | "none" {
  const chip = taskScheduleChip(t, NOW);
  if (!chip || chip.code === "stopped") return "none";
  if (t.next_run) return "server";
  return t.schedule && nextCronRun(t.schedule, t.timezone || "UTC", NOW)
    ? "predicted"
    : "none";
}

test("a stored next_run is preferred over a local prediction", () => {
  const t = task({
    schedule: "0 * * * *",
    timezone: "UTC",
    next_run: "2026-09-03T19:00:00Z",
  });
  assert.equal(nextRunSource(t), "server");
});

test("a schedule that has never fired falls back to a prediction", () => {
  // The runner writes next_run only when it first triggers the task, so this
  // is the state of every freshly created schedule.
  const t = task({ schedule: "0 * * * *", timezone: "UTC" });
  assert.equal(nextRunSource(t), "predicted");
});

test("a stopped schedule offers no next run at all", () => {
  for (const t of [
    task({ schedule: "0 2 * * *", schedule_enabled: false }),
    task({ schedule: "0 2 * * *", expires_at: "2026-08-01T00:00:00Z" }),
    task({
      schedule: "0 2 * * *",
      max_runs: 1,
      runs: [{ status: "completed" }],
    }),
  ]) {
    assert.equal(nextRunSource(t), "none");
  }
});

test("a stopped schedule shows no next run even when one is stored", () => {
  // A stale next_run outlives the disable, so trusting it here would
  // advertise a firing that cannot happen.
  const t = task({
    schedule: "0 2 * * *",
    schedule_enabled: false,
    next_run: "2027-01-01T00:00:00Z",
  });
  assert.equal(nextRunSource(t), "none");
});

test("re-arm mirrors cronEligibleStatuses, not \"looks finished\"", () => {
  assert.equal(
    showsRearmNotice(task({ schedule: "0 * * * *", status: "completed" }), NOW),
    true,
  );
  // `validated` is NOT in cronEligibleStatuses — the runner skips it, so the
  // banner would promise a firing that never comes. This assertion used to
  // say `true`, locking the defect in.
  assert.equal(
    showsRearmNotice(task({ schedule: "0 * * * *", status: "validated" }), NOW),
    false,
  );
  // `blocked` IS eligible and was missing entirely.
  assert.equal(
    showsRearmNotice(task({ schedule: "0 * * * *", status: "blocked" }), NOW),
    true,
  );
});

test("a completed task whose schedule is stopped is NOT reported as re-arming", () => {
  // The dangerous inverse: this one really is finished.
  assert.equal(
    showsRearmNotice(
      task({
        schedule: "0 * * * *",
        status: "completed",
        schedule_enabled: false,
      }), NOW),
    false,
  );
});

test("an active recurring task shows no re-arm notice", () => {
  // The notice answers "it says completed — is it coming back?". On an
  // active task there is no such confusion to resolve.
  assert.equal(
    showsRearmNotice(task({ schedule: "0 * * * *", status: "active" }), NOW),
    false,
  );
});

test("a completed one-shot shows no re-arm notice", () => {
  assert.equal(
    showsRearmNotice(
      task({ run_once_at: "2026-09-04T18:00:00Z", status: "completed" }), NOW),
    false,
  );
});

test("a task with no schedule renders no section at all", () => {
  assert.equal(taskScheduleChip(task({ status: "completed" }), NOW), null);
  assert.equal(nextRunSource(task({})), "none");
  assert.equal(showsRearmNotice(task({}), NOW), false);
});

test("the run counter matches what the runner spends against max_runs", () => {
  // Mirrors countRuns in internal/runner/schedule.go — an in-flight run has
  // already consumed budget, so "2 / 3" must include it.
  const t = task({
    schedule: "0 * * * *",
    max_runs: 3,
    runs: [{ status: "completed" }, { status: "in_progress" }],
  });
  assert.equal(countScheduleRuns(t.runs), 2);
});

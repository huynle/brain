/**
 * Tests for lib/actions/taskActions.
 *
 * The point of this module is the enabled/disabled matrix — every branch
 * that decides whether a verb can run, and what the user is told when it
 * cannot. These tests pin each branch, plus the effect each action routes
 * to, so a refactor cannot silently rewire "Delete" to run something else.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  abortBlockedReason,
  buildStatusActions,
  buildTaskActions,
  deleteBlockedReason,
  knownRunnerId,
  runBlockedReason,
  statusChangeBlockedReason,
  TERMINAL_STATUSES,
  type TaskActionContext,
} from "./taskActions";
import { isEnabled } from "./types";
import { ALL_STATUSES, type Task, type TaskStatus } from "../types";

function mkTask(over: Partial<Task> = {}): Task {
  return {
    id: "t1",
    path: "projects/p/task/t1.md",
    title: "A task",
    priority: "medium",
    status: "pending",
    ...over,
  };
}

/** Records what each action asked for, without performing anything. */
function recorder() {
  const calls: string[] = [];
  const ctx: TaskActionContext = {
    runTask: async (t) => void calls.push(`run:${t.id}`),
    setStatus: async (t, s) => void calls.push(`status:${t.id}:${s}`),
    deleteTask: async (t) => void calls.push(`delete:${t.id}`),
    abortTask: async (t) => void calls.push(`abort:${t.id}`),
    openResume: (t) => void calls.push(`resume:${t.id}`),
    openDetails: (t) => void calls.push(`details:${t.id}`),
    openLogs: (t) => void calls.push(`logs:${t.id}`),
    openMetadata: (t) => void calls.push(`metadata:${t.id}`),
    openStatusPicker: (t) => void calls.push(`picker:${t.id}`),
    openGoalCreate: (t) => void calls.push(`goal-create:${t.id}`),
  };
  return { calls, ctx };
}

function byId(task: Task, ctx: TaskActionContext) {
  const map = new Map(buildTaskActions(task, ctx).map((a) => [a.id, a]));
  return map;
}

// ─── presence ──────────────────────────────────────────────────────

test("every core verb is present for an ordinary pending task", () => {
  const { ctx } = recorder();
  const ids = buildTaskActions(mkTask(), ctx).map((a) => a.id);
  for (const expected of [
    "run",
    "status",
    "cancel",
    "metadata",
    "set-goal",
    "details",
    "logs",
    "delete",
  ]) {
    assert.ok(ids.includes(expected), `missing action: ${expected}`);
  }
});

test("set-goal is always enabled and routes to openGoalCreate", async () => {
  const { calls, ctx } = recorder();
  // Even a completed task can take a goal — the goal keeps validating it.
  const action = byId(mkTask({ status: "completed" }), ctx).get("set-goal")!;
  assert.equal(action.disabledReason ?? "", "");
  await action.run();
  assert.deepEqual(calls, ["goal-create:t1"]);
});

test("unavailable verbs are disabled, never dropped", () => {
  // A completed task can't be run — but Run must still appear, or the user
  // is left hunting for a control they saw yesterday.
  const { ctx } = recorder();
  const actions = byId(mkTask({ status: "completed" }), ctx);
  const run = actions.get("run");
  assert.ok(run, "Run disappeared for a completed task");
  assert.equal(isEnabled(run), false);
  assert.match(run.disabledReason ?? "", /completed/);
});

// ─── run ───────────────────────────────────────────────────────────

test("run is enabled for a pending task with no blockers", () => {
  assert.equal(runBlockedReason(mkTask({ status: "pending" })), "");
});

for (const status of [...TERMINAL_STATUSES]) {
  test(`run is blocked for terminal status ${status}`, () => {
    const reason = runBlockedReason(mkTask({ status: status as TaskStatus }));
    assert.notEqual(reason, "", `${status} should block run`);
    assert.match(reason, new RegExp(status));
  });
}

test("run is blocked while a task is genuinely running", () => {
  const reason = runBlockedReason(mkTask({ status: "in_progress" }));
  assert.match(reason, /already running/);
});

test("run is allowed on an abandoned in_progress task", () => {
  // Abandoned means nothing is actually executing it — that is the whole
  // point of the abandonment surface, so Run must not be blocked.
  const reason = runBlockedReason(
    mkTask({ status: "in_progress", is_abandoned: true }),
  );
  assert.equal(reason, "");
});

test("run is blocked by unmet dependencies and says how many", () => {
  const reason = runBlockedReason(
    mkTask({ status: "pending", blocked_by: ["a", "b"] }),
  );
  assert.match(reason, /2/);
});

test("run routes to runTask", async () => {
  const { calls, ctx } = recorder();
  await byId(mkTask(), ctx).get("run")!.run();
  assert.deepEqual(calls, ["run:t1"]);
});

// ─── resume ────────────────────────────────────────────────────────

test("resume appears for an abandoned task", () => {
  const { ctx } = recorder();
  const actions = byId(mkTask({ status: "in_progress", is_abandoned: true }), ctx);
  assert.ok(actions.has("resume"));
});

test("resume is absent for a completed task", () => {
  const { ctx } = recorder();
  assert.equal(byId(mkTask({ status: "completed" }), ctx).has("resume"), false);
});

test("resume labels itself as already-requested when it is a no-op", () => {
  const { ctx } = recorder();
  const actions = byId(
    mkTask({ status: "pending", resume_requested: true }),
    ctx,
  );
  assert.match(actions.get("resume")!.label, /already requested/i);
});

test("resume stays enabled even when force would be required", () => {
  // The Resume dialog is where force lives; disabling the entry here would
  // hide the only route to recovery.
  const { ctx } = recorder();
  const actions = byId(mkTask({ status: "blocked" }), ctx);
  const resume = actions.get("resume");
  assert.ok(resume);
  assert.equal(isEnabled(resume), true);
});

// ─── cancel ────────────────────────────────────────────────────────

test("cancel is enabled for an in-flight task and routes to cancelled", async () => {
  const { calls, ctx } = recorder();
  const cancel = byId(mkTask({ status: "pending" }), ctx).get("cancel")!;
  assert.equal(isEnabled(cancel), true);
  await cancel.run();
  assert.deepEqual(calls, ["status:t1:cancelled"]);
});

test("cancel is disabled for an already-terminal task", () => {
  const { ctx } = recorder();
  const cancel = byId(mkTask({ status: "cancelled" }), ctx).get("cancel")!;
  assert.equal(isEnabled(cancel), false);
});

test("cancelling a running task requires confirmation that explains the runner keeps going", () => {
  const { ctx } = recorder();
  const cancel = byId(mkTask({ status: "in_progress" }), ctx).get("cancel")!;
  assert.ok(cancel.confirm, "no confirm on cancelling a running task");
  assert.match(cancel.confirm.body, /runner/i);
});

test("cancelling a pending task needs no confirmation", () => {
  const { ctx } = recorder();
  const cancel = byId(mkTask({ status: "pending" }), ctx).get("cancel")!;
  assert.equal(cancel.confirm, undefined);
});

// ─── delete ────────────────────────────────────────────────────────

test("delete is blocked while a task is genuinely running", () => {
  assert.match(
    deleteBlockedReason(mkTask({ status: "in_progress" })),
    /running/,
  );
});

test("delete is allowed on an abandoned task", () => {
  // Cleaning up after a crashed runner is the main reason to delete.
  assert.equal(
    deleteBlockedReason(mkTask({ status: "in_progress", is_abandoned: true })),
    "",
  );
});

test("delete always demands confirmation and names the task", () => {
  const { ctx } = recorder();
  const del = byId(mkTask({ title: "Wire the thing" }), ctx).get("delete")!;
  assert.ok(del.confirm);
  assert.match(del.confirm.body, /Wire the thing/);
  assert.match(del.confirm.body, /cannot be undone/i);
});

test("delete is flagged danger", () => {
  const { ctx } = recorder();
  assert.equal(byId(mkTask(), ctx).get("delete")!.danger, true);
});

test("delete routes to deleteTask", async () => {
  const { calls, ctx } = recorder();
  await byId(mkTask(), ctx).get("delete")!.run();
  assert.deepEqual(calls, ["delete:t1"]);
});

// ─── abort ─────────────────────────────────────────────────────────

/** A running task with a live dispatch lease. */
function mkRunningTask(over: Partial<Task> = {}): Task {
  return mkTask({
    status: "in_progress",
    dispatch_lease: {
      leaseId: "l1",
      project_id: "p",
      task_id: "t1",
      assigned_runner_id: "amos-1",
      assigned_machine_id: "m1",
      state: "acked",
      pushed_at: 1,
      expires_at: 2,
    },
    ...over,
  });
}

test("abort is present and enabled for a running task with a known runner", () => {
  const { ctx } = recorder();
  const abort = byId(mkRunningTask(), ctx).get("abort");
  assert.ok(abort, "abort verb missing");
  assert.equal(isEnabled(abort), true);
});

test("abort is disabled — never hidden — for a non-running task", () => {
  const { ctx } = recorder();
  const abort = byId(mkTask({ status: "pending" }), ctx).get("abort");
  assert.ok(abort, "abort must render disabled, not disappear");
  assert.equal(isEnabled(abort), false);
  assert.match(abort.disabledReason ?? "", /only a running task/i);
});

test("abort on an abandoned task points at Resume instead", () => {
  const reason = abortBlockedReason(
    mkRunningTask({ is_abandoned: true }),
  );
  assert.match(reason, /abandoned/i);
  assert.match(reason, /resume/i);
});

test("abort is disabled when no runner is known", () => {
  const reason = abortBlockedReason(mkTask({ status: "in_progress" }));
  assert.match(reason, /no runner/i);
});

test("abort requires confirmation that says the status stays put", () => {
  const { ctx } = recorder();
  const abort = byId(mkRunningTask(), ctx).get("abort")!;
  assert.ok(abort.confirm, "abort must confirm");
  assert.match(abort.confirm.body, /keeps its current status/i);
  assert.equal(abort.danger, true);
});

test("abort routes to abortTask", async () => {
  const { calls, ctx } = recorder();
  await byId(mkRunningTask(), ctx).get("abort")!.run();
  assert.deepEqual(calls, ["abort:t1"]);
});

test("delete's blocked reason points at the abort verb, which exists", () => {
  // Regression: the copy used to reference a "Force delete" affordance
  // that was never built.
  const reason = deleteBlockedReason(mkRunningTask());
  assert.match(reason, /abort runner execution/i);
});

// ─── knownRunnerId ─────────────────────────────────────────────────

test("knownRunnerId prefers the dispatch lease", () => {
  const task = mkRunningTask({
    sessions: {
      s1: { timestamp: "2026-08-11T00:00:00Z", runner_id: "older-runner" },
    },
  });
  assert.equal(knownRunnerId(task), "amos-1");
});

test("knownRunnerId falls back to the most recent session's runner", () => {
  const task = mkTask({
    status: "in_progress",
    sessions: {
      s1: { timestamp: "2026-08-10T00:00:00Z", runner_id: "old-runner" },
      s2: { timestamp: "2026-08-11T00:00:00Z", runner_id: "new-runner" },
      s3: { timestamp: "2026-08-12T00:00:00Z" }, // no runner recorded
    },
  });
  assert.equal(knownRunnerId(task), "new-runner");
});

test("knownRunnerId is undefined when nothing recorded a runner", () => {
  assert.equal(knownRunnerId(mkTask()), undefined);
});

// ─── navigation + edit ─────────────────────────────────────────────

test("navigation and edit actions route to their openers", async () => {
  const { calls, ctx } = recorder();
  const actions = byId(mkTask(), ctx);
  await actions.get("details")!.run();
  await actions.get("logs")!.run();
  await actions.get("metadata")!.run();
  await actions.get("status")!.run();
  assert.deepEqual(calls, [
    "details:t1",
    "logs:t1",
    "metadata:t1",
    "picker:t1",
  ]);
});

// ─── accelerators ──────────────────────────────────────────────────

test("accelerator keys are unique across a task's actions", () => {
  // A duplicate key would make one of the two unreachable from the
  // keyboard, silently.
  const { ctx } = recorder();
  const keys = buildTaskActions(mkTask({ is_abandoned: true }), ctx)
    .map((a) => a.key)
    .filter(Boolean);
  assert.equal(new Set(keys).size, keys.length, `duplicate keys: ${keys}`);
});

test("delete is bound to d and run to x", () => {
  const { ctx } = recorder();
  const actions = byId(mkTask(), ctx);
  assert.equal(actions.get("delete")!.key, "d");
  assert.equal(actions.get("run")!.key, "x");
});

// ─── status picker ─────────────────────────────────────────────────

test("status picker offers every known status", () => {
  const { ctx } = recorder();
  const ids = buildStatusActions(mkTask(), ctx).map((a) => a.id);
  assert.equal(ids.length, ALL_STATUSES.length);
});

test("the current status is present but disabled", () => {
  // Present so the picker doubles as a display of where the task is.
  const { ctx } = recorder();
  const actions = buildStatusActions(mkTask({ status: "blocked" }), ctx);
  const current = actions.find((a) => a.id === "status:blocked")!;
  assert.equal(isEnabled(current), false);
  assert.match(current.disabledReason ?? "", /already/i);
});

test("status changes are blocked on a genuinely running task", () => {
  const reason = statusChangeBlockedReason(
    mkTask({ status: "in_progress" }),
    "pending",
  );
  assert.match(reason, /running/);
});

test("status changes are allowed on an abandoned running task", () => {
  assert.equal(
    statusChangeBlockedReason(
      mkTask({ status: "in_progress", is_abandoned: true }),
      "pending",
    ),
    "",
  );
});

test("picking a status routes to setStatus with that status", async () => {
  const { calls, ctx } = recorder();
  const actions = buildStatusActions(mkTask(), ctx);
  await actions.find((a) => a.id === "status:completed")!.run();
  assert.deepEqual(calls, ["status:t1:completed"]);
});

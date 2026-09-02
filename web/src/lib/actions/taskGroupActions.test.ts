import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  buildTaskGroupActions,
  countTaskGroup,
  runGroupBlockedReason,
  type TaskGroup,
  type TaskGroupActionContext,
} from "./taskGroupActions";
import type { Task, TaskStatus } from "../types";

const task = (id: string, status: TaskStatus): Task => ({
  id,
  path: `p/${id}.md`,
  title: `Task ${id}`,
  priority: "medium",
  status,
});

const group = (tasks: Task[], label = "No feature"): TaskGroup => ({
  projectId: "shop",
  key: "__nofeat__",
  label,
  tasks,
});

function recorder() {
  const calls: string[] = [];
  const ctx: TaskGroupActionContext = {
    toggleCollapsed: (g) => void calls.push(`fold:${g.key}`),
    selectAll: (g) => void calls.push(`select:${g.tasks.length}`),
    runGroup: async (g) => void calls.push(`run:${g.key}`),
    setStatusForAll: async (_g, s) => void calls.push(`status:${s}`),
    deleteGroup: async (g) => void calls.push(`delete:${g.key}`),
  };
  return { calls, ctx };
}

const byId = (g: TaskGroup, ctx: TaskGroupActionContext, collapsed = false) =>
  new Map(buildTaskGroupActions(g, ctx, { collapsed }).map((a) => [a.id, a]));

// ─── counts ──────────────────────────────────────────────────────

test("countTaskGroup: only pending tasks are runnable", () => {
  const c = countTaskGroup([
    task("a", "pending"),
    task("b", "in_progress"),
    // Blocked is waiting on a dependency a dispatch would not clear, so
    // counting it would make Run promise work it cannot do.
    task("c", "blocked"),
    task("d", "completed"),
  ]);
  assert.equal(c.total, 4);
  assert.equal(c.runnable, 1);
});

test("countTaskGroup: live excludes every terminal status", () => {
  const c = countTaskGroup([
    task("a", "pending"),
    task("b", "completed"),
    task("c", "validated"),
    task("d", "cancelled"),
    task("e", "archived"),
  ]);
  assert.equal(c.live, 1);
  assert.equal(c.done, 2);
  assert.equal(c.archived, 1);
});

// ─── gating ──────────────────────────────────────────────────────

test("runGroupBlockedReason: names the reason rather than vanishing", () => {
  assert.match(runGroupBlockedReason(countTaskGroup([])), /no tasks/);
  assert.match(
    runGroupBlockedReason(countTaskGroup([task("a", "blocked")])),
    /No pending tasks/,
  );
  assert.equal(runGroupBlockedReason(countTaskGroup([task("a", "pending")])), "");
});

test("archive is disabled once everything is already archived", () => {
  const { ctx } = recorder();
  const all = byId(group([task("a", "archived")]), ctx);
  assert.match(all.get("archive")!.disabledReason!, /already archived/);
  // …and unarchive becomes the live verb in that state, so the archive is
  // not a one-way door.
  assert.equal(all.get("unarchive")!.disabledReason, "");
});

test("unarchive is disabled when nothing here is archived", () => {
  const { ctx } = recorder();
  const all = byId(group([task("a", "pending")]), ctx);
  assert.match(all.get("unarchive")!.disabledReason!, /Nothing here is archived/);
  assert.equal(all.get("archive")!.disabledReason, "");
});

// Unarchive restores to `completed`, NOT `pending`, matching the
// single-task verb. `pending` is RUNNABLE — the scheduler dispatches every
// ready task on its next pass — so restoring to it would re-execute work
// that had already finished.
test("unarchive restores to a status that will not be re-run", () => {
  const { calls, ctx } = recorder();
  const all = byId(group([task("a", "archived")]), ctx);
  void all.get("unarchive")!.run();
  assert.deepEqual(calls, ["status:completed"]);
});

test("cancel is disabled when no task is still live", () => {
  const { ctx } = recorder();
  const all = byId(group([task("a", "completed"), task("b", "cancelled")]), ctx);
  assert.match(all.get("cancel")!.disabledReason!, /still live/);
});

// ─── labels + wiring ─────────────────────────────────────────────

test("the fold verb names the direction it will go", () => {
  const { ctx } = recorder();
  assert.equal(byId(group([task("a", "pending")]), ctx, false).get("collapse")!.label, "Collapse");
  assert.equal(byId(group([task("a", "pending")]), ctx, true).get("collapse")!.label, "Expand");
});

test("run says how many it will dispatch", () => {
  const { ctx } = recorder();
  const g = group([task("a", "pending"), task("b", "pending"), task("c", "completed")]);
  assert.equal(byId(g, ctx).get("run")!.label, "Run 2 pending");
});

// Delete is the one verb that cannot be undone, and the group label is
// what the header shows — so that is what has to be typed.
test("delete demands the group label typed back", () => {
  const { ctx } = recorder();
  const all = byId(group([task("a", "pending")]), ctx);
  const del = all.get("delete")!;
  assert.equal(del.danger, true);
  assert.equal(del.confirm?.typeToConfirm, "No feature");
  assert.match(del.confirm!.body, /cannot be undone/);
});

test("every verb reaches its context function", async () => {
  const { calls, ctx } = recorder();
  const g = group([task("a", "pending"), task("b", "archived")]);
  for (const a of buildTaskGroupActions(g, ctx)) await a.run();
  assert.deepEqual(calls, [
    "fold:__nofeat__",
    "select:2",
    "run:__nofeat__",
    "status:archived",
    "status:cancelled",
    "status:completed",
    "delete:__nofeat__",
  ]);
});

// A group with no tasks should offer nothing runnable — but the verbs must
// still BE there, disabled with a reason, rather than silently missing.
test("an empty group keeps its verbs, disabled with reasons", () => {
  const { ctx } = recorder();
  const all = byId(group([]), ctx);
  for (const id of ["select-all", "run", "archive", "cancel", "unarchive", "delete"]) {
    assert.match(all.get(id)!.disabledReason!, /no tasks/i, id);
  }
});

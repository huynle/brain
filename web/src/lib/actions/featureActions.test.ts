/**
 * Tests for lib/actions/featureActions.
 *
 * A feature is a fan-out over its tasks, so the things worth pinning are:
 * which verbs are available in which lifecycle, that cancel (reversible)
 * is offered ahead of delete (not), and that every destructive verb states
 * its real blast radius before it runs.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  affectedTaskCount,
  buildFeatureActions,
  buildFeatureStatusActions,
  cancelFeatureBlockedReason,
  checkoutBlockedReason,
  runFeatureBlockedReason,
  summarizeBulkResult,
  type FeatureActionContext,
} from "./featureActions";
import { isEnabled } from "./types";
import type { DerivedFeature } from "../features";
import type { TaskStatus } from "../types";

function mkFeature(over: Partial<DerivedFeature> = {}): DerivedFeature {
  return {
    id: "checkout-flow",
    projectId: "shop",
    name: "checkout-flow",
    progress: 0,
    lifecycle: "in-progress",
    taskCount: { total: 4, completed: 1, blocked: 0, active: 3 },
    ownerTaskIds: ["a", "b", "c", "d"],
    resumableCount: 0,
    dependsOn: [],
    ...over,
  };
}

function recorder() {
  const calls: string[] = [];
  const ctx: FeatureActionContext = {
    runFeature: async (f) => void calls.push(`run:${f.id}`),
    openStatusPicker: (f) => void calls.push(`picker:${f.id}`),
    setStatusForAll: async (f, s) => void calls.push(`status:${f.id}:${s}`),
    deleteFeature: async (f) => void calls.push(`delete:${f.id}`),
    openCheckout: (f) => void calls.push(`checkout:${f.id}`),
    openResume: (f) => void calls.push(`resume:${f.id}`),
    openPlan: (f) => void calls.push(`plan:${f.id}`),
    openDetails: (f) => void calls.push(`details:${f.id}`),
    openMetadata: (f) => void calls.push(`metadata:${f.id}`),
  };
  return { calls, ctx };
}

function byId(feature: DerivedFeature, ctx: FeatureActionContext) {
  return new Map(buildFeatureActions(feature, ctx).map((a) => [a.id, a]));
}

// ─── presence ──────────────────────────────────────────────────────

test("every core feature verb is present", () => {
  const { ctx } = recorder();
  const ids = buildFeatureActions(mkFeature(), ctx).map((a) => a.id);
  for (const expected of [
    "run",
    "checkout",
    "status",
    "cancel",
    "metadata",
    "plan",
    "details",
    "delete",
  ]) {
    assert.ok(ids.includes(expected), `missing action: ${expected}`);
  }
});

// ─── run ───────────────────────────────────────────────────────────

test("run is enabled for an in-progress feature with active tasks", () => {
  assert.equal(runFeatureBlockedReason(mkFeature()), "");
});

test("run is blocked for an empty feature", () => {
  const reason = runFeatureBlockedReason(
    mkFeature({ taskCount: { total: 0, completed: 0, blocked: 0, active: 0 } }),
  );
  assert.match(reason, /no tasks/i);
});

test("run is blocked for a merged feature", () => {
  const reason = runFeatureBlockedReason(mkFeature({ lifecycle: "merged" }));
  assert.match(reason, /merged/);
});

test("run is blocked when every task is blocked or done", () => {
  const reason = runFeatureBlockedReason(
    mkFeature({ taskCount: { total: 3, completed: 1, blocked: 2, active: 0 } }),
  );
  assert.match(reason, /blocked or done/i);
});

test("run routes to runFeature", async () => {
  const { calls, ctx } = recorder();
  await byId(mkFeature(), ctx).get("run")!.run();
  assert.deepEqual(calls, ["run:checkout-flow"]);
});

// ─── resume ────────────────────────────────────────────────────────

test("resume appears only when tasks are abandoned, and states the count", () => {
  const { ctx } = recorder();
  assert.equal(byId(mkFeature(), ctx).has("resume"), false);

  const withAbandoned = byId(mkFeature({ resumableCount: 3 }), ctx);
  assert.match(withAbandoned.get("resume")!.label, /3 abandoned tasks/);
});

test("resume label is singular for one abandoned task", () => {
  const { ctx } = recorder();
  const actions = byId(mkFeature({ resumableCount: 1 }), ctx);
  assert.match(actions.get("resume")!.label, /1 abandoned task\b/);
});

// ─── cancel vs delete ──────────────────────────────────────────────

test("cancel is enabled while work remains", () => {
  assert.equal(cancelFeatureBlockedReason(mkFeature()), "");
});

test("cancel is blocked once every task is done", () => {
  const reason = cancelFeatureBlockedReason(
    mkFeature({ taskCount: { total: 3, completed: 3, blocked: 0, active: 0 } }),
  );
  assert.match(reason, /already done/i);
});

test("cancel confirmation states the count and that it is reversible", () => {
  const { ctx } = recorder();
  const cancel = byId(mkFeature(), ctx).get("cancel")!;
  assert.ok(cancel.confirm);
  assert.match(cancel.confirm.body, /4 tasks/);
  assert.match(cancel.confirm.body, /reversible/i);
});

test("cancel does NOT require typing the feature name", () => {
  // Type-to-confirm is friction reserved for the irreversible. Applying it
  // to a reversible action trains people to type through the real one.
  const { ctx } = recorder();
  assert.equal(byId(mkFeature(), ctx).get("cancel")!.confirm?.typeToConfirm, undefined);
});

test("delete requires typing the feature name", () => {
  const { ctx } = recorder();
  const del = byId(mkFeature(), ctx).get("delete")!;
  assert.equal(del.confirm?.typeToConfirm, "checkout-flow");
});

test("delete confirmation states the blast radius and points at cancel", () => {
  const { ctx } = recorder();
  const del = byId(mkFeature(), ctx).get("delete")!;
  assert.match(del.confirm!.body, /all 4 tasks/i);
  assert.match(del.confirm!.body, /cannot be undone/i);
  assert.match(del.confirm!.body, /cancel/i, "delete should point at the reversible option");
});

test("delete is flagged danger and routes to deleteFeature", async () => {
  const { calls, ctx } = recorder();
  const del = byId(mkFeature(), ctx).get("delete")!;
  assert.equal(del.danger, true);
  await del.run();
  assert.deepEqual(calls, ["delete:checkout-flow"]);
});

test("delete is disabled for an empty feature", () => {
  const { ctx } = recorder();
  const del = byId(
    mkFeature({ taskCount: { total: 0, completed: 0, blocked: 0, active: 0 } }),
    ctx,
  ).get("delete")!;
  assert.equal(isEnabled(del), false);
});

// ─── checkout ──────────────────────────────────────────────────────

test("checkout is blocked for an empty feature", () => {
  assert.match(
    checkoutBlockedReason(
      mkFeature({ taskCount: { total: 0, completed: 0, blocked: 0, active: 0 } }),
    ),
    /no tasks/i,
  );
});

// ─── accelerators ──────────────────────────────────────────────────

test("accelerator keys are unique across a feature's actions", () => {
  const { ctx } = recorder();
  const keys = buildFeatureActions(mkFeature({ resumableCount: 2 }), ctx)
    .map((a) => a.key)
    .filter(Boolean);
  assert.equal(new Set(keys).size, keys.length, `duplicate keys: ${keys}`);
});

// ─── status picker ─────────────────────────────────────────────────

test("feature status entries confirm with the affected count", () => {
  const { ctx } = recorder();
  const statuses: TaskStatus[] = ["pending", "completed"];
  const actions = buildFeatureStatusActions(mkFeature(), ctx, statuses);
  assert.equal(actions.length, 2);
  assert.match(actions[0].confirm!.title, /4 tasks/);
});

test("every feature status entry is confirmed — none applies silently", () => {
  // A feature-wide status change touches everything under it; none of
  // these should fire straight from a menu click.
  const { ctx } = recorder();
  const actions = buildFeatureStatusActions(
    mkFeature(),
    ctx,
    ["pending", "completed", "blocked"] as TaskStatus[],
  );
  for (const a of actions) {
    assert.ok(a.confirm, `${a.id} applies with no confirmation`);
  }
});

test("picking a feature status routes to setStatusForAll", async () => {
  const { calls, ctx } = recorder();
  const actions = buildFeatureStatusActions(mkFeature(), ctx, ["completed"] as TaskStatus[]);
  await actions[0].run();
  assert.deepEqual(calls, ["status:checkout-flow:completed"]);
});

test("affectedTaskCount reports the feature's total", () => {
  assert.equal(affectedTaskCount(mkFeature()), 4);
});

// ─── result summaries ──────────────────────────────────────────────

test("summarizeBulkResult: clean success", () => {
  const r = summarizeBulkResult({ ok: 4, failed: 0, total: 4 });
  assert.equal(r.kind, "success");
  assert.match(r.message, /4 of 4/);
});

test("summarizeBulkResult: nothing matched is a warning, not a success", () => {
  const r = summarizeBulkResult({ ok: 0, failed: 0, total: 0 });
  assert.equal(r.kind, "warning");
  assert.match(r.message, /nothing matched/i);
});

test("summarizeBulkResult: partial failure names both counts", () => {
  const r = summarizeBulkResult({ ok: 7, failed: 2, total: 9 });
  assert.equal(r.kind, "warning");
  assert.match(r.message, /7 of 9/);
  assert.match(r.message, /2 failed/);
});

test("summarizeBulkResult: total failure is an error", () => {
  const r = summarizeBulkResult({ ok: 0, failed: 3, total: 3 });
  assert.equal(r.kind, "error");
});

test("summarizeBulkResult: truncation is surfaced, not swallowed", () => {
  // The whole point of the truncated flag: a "success" that only touched
  // the first 100 of 120 tasks must not read as done.
  const r = summarizeBulkResult({
    ok: 100,
    failed: 0,
    total: 100,
    truncated: true,
    matchedTotal: 120,
  });
  assert.equal(r.kind, "warning");
  assert.match(r.message, /20 more/);
  assert.match(r.message, /run again/i);
});

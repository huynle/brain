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
  archiveFeatureBlockedReason,
  buildFeatureActions,
  buildFeatureStatusActions,
  cancelFeatureBlockedReason,
  checkoutBlockedReason,
  resumeFeatureBlockedReason,
  runFeatureBlockedReason,
  summarizeBulkResult,
  summarizeResumeOutcome,
  type FeatureActionContext,
} from "./featureActions";
import { isEnabled } from "./types";
import type { DerivedFeature } from "../features";
import type { ResumeFeatureResult, TaskStatus } from "../types";

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

function recorder(over: Partial<FeatureActionContext> = {}) {
  const calls: string[] = [];
  const ctx: FeatureActionContext = {
    openDrawer: (f) => void calls.push(`open:${f.id}`),
    toggleSelect: (f) => void calls.push(`select:${f.id}`),
    isSelected: () => false,
    runFeature: async (f) => void calls.push(`run:${f.id}`),
    runFeatureWithDependents: async (f) => void calls.push(`run-deps:${f.id}`),
    cancelDependentChain: async (f) => void calls.push(`cancel-chain:${f.id}`),
    hasActiveChain: () => false,
    resumeFeature: async (f) => void calls.push(`resume-direct:${f.id}`),
    openStatusPicker: (f) => void calls.push(`picker:${f.id}`),
    setStatusForAll: async (f, s) => void calls.push(`status:${f.id}:${s}`),
    deleteFeature: async (f) => void calls.push(`delete:${f.id}`),
    openCheckout: (f) => void calls.push(`checkout:${f.id}`),
    openResume: (f) => void calls.push(`resume:${f.id}`),
    openPlan: (f) => void calls.push(`plan:${f.id}`),
    openDetails: (f) => void calls.push(`details:${f.id}`),
    watchInFocus: (f) => {
      calls.push(`watch-focus:${f.id}`);
      return 1;
    },
    openMetadata: (f) => void calls.push(`metadata:${f.id}`),
    openAssignRunner: (f) => void calls.push(`assign:${f.id}`),
    clearRunnerAssignment: async (f) => void calls.push(`unassign:${f.id}`),
    assignedRunner: () => undefined,
    openGoalCreate: (f) => void calls.push(`goal-create:${f.id}`),
    ...over,
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
    "resume",
    "checkout",
    "status",
    "cancel",
    "archive",
    "metadata",
    "assign",
    "unassign",
    "set-goal",
    "plan",
    "details",
    "delete",
  ]) {
    assert.ok(ids.includes(expected), `missing action: ${expected}`);
  }
});

test("set-goal is always enabled and routes to openGoalCreate", async () => {
  const { calls, ctx } = recorder();
  // Even a merged feature can take a goal (watch for regressions).
  const action = byId(mkFeature({ lifecycle: "merged" }), ctx).get("set-goal")!;
  assert.equal(action.disabledReason ?? "", "");
  await action.run();
  assert.deepEqual(calls, ["goal-create:checkout-flow"]);
});

// ─── open (context-menu Open verb) ──────────────────────────────────

test("open is the very first feature action, in the select group", () => {
  const { ctx } = recorder();
  const actions = buildFeatureActions(mkFeature(), ctx);
  assert.equal(actions[0].id, "open", "Open must render first");
  assert.equal(actions[0].label, "Open");
  assert.equal(actions[0].group, "select");
});

test("open is always enabled and routes to openDrawer", async () => {
  const { calls, ctx } = recorder();
  const open = byId(mkFeature({ lifecycle: "merged" }), ctx).get("open")!;
  assert.equal(open.disabledReason ?? "", "");
  await open.run();
  assert.deepEqual(calls, ["open:checkout-flow"]);
});

test("open sits ahead of select in the select group", () => {
  const { ctx } = recorder();
  const ids = buildFeatureActions(mkFeature(), ctx).map((a) => a.id);
  assert.ok(ids.indexOf("open") < ids.indexOf("select"), "Open before Select");
});

test("open does not replace Feature details or the plan drawer verb", () => {
  // The new Open verb is additive: the existing navigate-group verbs
  // (plan drawer + feature details) must survive.
  const { ctx } = recorder();
  const ids = buildFeatureActions(mkFeature(), ctx).map((a) => a.id);
  assert.ok(ids.includes("plan"), "plan drawer verb dropped");
  assert.ok(ids.includes("details"), "feature details verb dropped");
  assert.ok(ids.includes("open"), "open verb missing");
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

test("resume is always present — disabled with a reason at zero, never hidden", () => {
  // Disabled-never-hidden: a user who resumed tasks yesterday must find
  // the verb today, with the reason it cannot run.
  const { ctx } = recorder();
  const resume = byId(mkFeature(), ctx).get("resume");
  assert.ok(resume, "resume verb missing at resumableCount=0");
  assert.equal(isEnabled(resume), false);
  assert.match(resume.disabledReason ?? "", /no abandoned tasks/i);
});

test("resume is enabled and states the count when tasks are abandoned", () => {
  const { ctx } = recorder();
  const resume = byId(mkFeature({ resumableCount: 3 }), ctx).get("resume")!;
  assert.equal(isEnabled(resume), true);
  assert.match(resume.label, /3 abandoned tasks/);
});

test("resume label is singular for one abandoned task", () => {
  const { ctx } = recorder();
  const actions = byId(mkFeature({ resumableCount: 1 }), ctx);
  assert.match(actions.get("resume")!.label, /1 abandoned task\b/);
});

test("resume executes directly instead of opening the actions modal", async () => {
  const { calls, ctx } = recorder();
  await byId(mkFeature({ resumableCount: 2 }), ctx)
    .get("resume")!
    .run();
  assert.deepEqual(calls, ["resume-direct:checkout-flow"]);
});

test("resumeFeatureBlockedReason covers the empty feature too", () => {
  assert.match(
    resumeFeatureBlockedReason(
      mkFeature({
        taskCount: { total: 0, completed: 0, blocked: 0, active: 0 },
      }),
    ),
    /no tasks/i,
  );
});

// ─── runner assignment ─────────────────────────────────────────────

test("assign opens the runner picker", async () => {
  const { calls, ctx } = recorder();
  await byId(mkFeature(), ctx).get("assign")!.run();
  assert.deepEqual(calls, ["assign:checkout-flow"]);
});

test("assign names the current runner when one is assigned", () => {
  const { ctx } = recorder({ assignedRunner: () => "amos-1" });
  assert.match(byId(mkFeature(), ctx).get("assign")!.label, /amos-1/);
});

test("clear assignment is disabled with a reason when nothing is assigned", () => {
  const { ctx } = recorder();
  const unassign = byId(mkFeature(), ctx).get("unassign")!;
  assert.equal(isEnabled(unassign), false);
  assert.match(unassign.disabledReason ?? "", /no runner assigned/i);
});

test("clear assignment routes to clearRunnerAssignment when assigned", async () => {
  const { calls, ctx } = recorder({ assignedRunner: () => "amos-1" });
  const unassign = byId(mkFeature(), ctx).get("unassign")!;
  assert.equal(isEnabled(unassign), true);
  await unassign.run();
  assert.deepEqual(calls, ["unassign:checkout-flow"]);
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
  assert.equal(
    byId(mkFeature(), ctx).get("cancel")!.confirm?.typeToConfirm,
    undefined,
  );
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
  assert.match(
    del.confirm!.body,
    /cancel/i,
    "delete should point at the reversible option",
  );
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

// ─── archive ───────────────────────────────────────────────────────

for (const lifecycle of ["finished", "merged"] as const) {
  test(`archive is enabled for a ${lifecycle} feature`, () => {
    const { ctx } = recorder();
    const f = mkFeature({
      lifecycle,
      taskCount: { total: 4, completed: 4, blocked: 0, active: 0 },
    });
    assert.equal(archiveFeatureBlockedReason(f), "");
    assert.equal(isEnabled(byId(f, ctx).get("archive")!), true);
  });
}

for (const lifecycle of ["in-progress", "blocked", "mr-open"] as const) {
  test(`archive is disabled — never hidden — for a ${lifecycle} feature`, () => {
    const { ctx } = recorder();
    const archive = byId(mkFeature({ lifecycle }), ctx).get("archive");
    assert.ok(archive, "archive must render disabled, not disappear");
    assert.equal(isEnabled(archive), false);
    assert.equal(
      archive.disabledReason,
      "Feature has active work — archive is for settled features",
    );
  });
}

test("archive is disabled for an empty feature", () => {
  assert.match(
    archiveFeatureBlockedReason(
      mkFeature({
        lifecycle: "finished",
        taskCount: { total: 0, completed: 0, blocked: 0, active: 0 },
      }),
    ),
    /no tasks/i,
  );
});

test("archive confirms the blast radius at the reversible tier — no typing", () => {
  const { ctx } = recorder();
  const archive = byId(mkFeature({ lifecycle: "finished" }), ctx).get(
    "archive",
  )!;
  assert.ok(archive.confirm, "archive must confirm");
  assert.equal(archive.confirm.typeToConfirm, undefined);
  assert.match(archive.confirm.body, /All 4 tasks/);
  assert.match(archive.confirm.body, /reversible/i);
  assert.match(archive.confirm.body, /Archived filter/);
});

test("archive routes to setStatusForAll archived", async () => {
  const { calls, ctx } = recorder();
  await byId(mkFeature({ lifecycle: "merged" }), ctx)
    .get("archive")!
    .run();
  assert.deepEqual(calls, ["status:checkout-flow:archived"]);
});

test("archive sits in the state group directly after cancel", () => {
  const { ctx } = recorder();
  const actions = buildFeatureActions(mkFeature(), ctx);
  const ids = actions.map((a) => a.id);
  assert.equal(ids.indexOf("archive"), ids.indexOf("cancel") + 1);
  assert.equal(actions[ids.indexOf("archive")]!.group, "state");
});

// ─── checkout ──────────────────────────────────────────────────────

test("checkout is blocked for an empty feature", () => {
  assert.match(
    checkoutBlockedReason(
      mkFeature({
        taskCount: { total: 0, completed: 0, blocked: 0, active: 0 },
      }),
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
  const actions = buildFeatureStatusActions(mkFeature(), ctx, [
    "pending",
    "completed",
    "blocked",
  ] as TaskStatus[]);
  for (const a of actions) {
    assert.ok(a.confirm, `${a.id} applies with no confirmation`);
  }
});

test("picking a feature status routes to setStatusForAll", async () => {
  const { calls, ctx } = recorder();
  const actions = buildFeatureStatusActions(mkFeature(), ctx, [
    "completed",
  ] as TaskStatus[]);
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

test("summarizeResumeOutcome: clean resume is a success with the count", () => {
  const r = summarizeResumeOutcome({
    feature_id: "f",
    total_resumed: 3,
    total_skipped: 0,
    results: [],
  });
  assert.equal(r.kind, "success");
  assert.match(r.message, /Resumed 3 tasks/);
});

test("summarizeResumeOutcome: skips surface the top reasons, bucketed", () => {
  const result: ResumeFeatureResult = {
    feature_id: "f",
    total_resumed: 1,
    total_skipped: 3,
    results: [
      { task_id: "a", resumed: true },
      { task_id: "b", resumed: false, reason: "task is not abandoned" },
      { task_id: "c", resumed: false, reason: "task is not abandoned" },
      { task_id: "d", resumed: false, reason: "terminal status" },
    ],
  };
  const r = summarizeResumeOutcome(result);
  assert.equal(r.kind, "warning");
  assert.match(r.message, /Resumed 1 task\b/);
  assert.match(r.message, /3 skipped/);
  assert.match(r.message, /2× task is not abandoned/);
  assert.match(r.message, /terminal status/);
});

test("summarizeResumeOutcome: nothing resumed is informational, not silent", () => {
  const r = summarizeResumeOutcome({
    feature_id: "f",
    total_resumed: 0,
    total_skipped: 2,
    results: [
      { task_id: "a", resumed: false, reason: "already resumed" },
      { task_id: "b", resumed: false, reason: "already resumed" },
    ],
  });
  assert.equal(r.kind, "info");
  assert.match(r.message, /No tasks resumed/);
  assert.match(r.message, /2× already resumed/);
});

test("summarizeResumeOutcome: empty feature", () => {
  const r = summarizeResumeOutcome({
    feature_id: "f",
    total_resumed: 0,
    total_skipped: 0,
    results: [],
  });
  assert.equal(r.kind, "info");
  assert.match(r.message, /no tasks in feature/i);
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

// ─── run with dependents ─────────────────────────────────────────

test("run-with-dependents is a separate verb from run", () => {
  // A modifier on the existing verb would change what the default gesture
  // means. A chain has a far wider blast radius than one feature.
  const { ctx } = recorder();
  const ids = buildFeatureActions(mkFeature(), ctx).map((a) => a.id);
  assert.ok(ids.includes("run"), "plain run must stay");
  assert.ok(ids.includes("run-with-dependents"));
});

test("run-with-dependents routes to its own effect", async () => {
  const { ctx, calls } = recorder();
  const a = buildFeatureActions(mkFeature(), ctx).find(
    (x) => x.id === "run-with-dependents",
  )!;
  await a.run();
  assert.deepEqual(calls, ["run-deps:checkout-flow"]);
});

test("run-with-dependents carries no count in its label", () => {
  // The chain is derived server-side from the CURRENT graph at click time,
  // so a client-side count would be a guess that can disagree with what is
  // actually queued. The toast reports the real figure.
  const { ctx } = recorder();
  const a = buildFeatureActions(mkFeature(), ctx).find(
    (x) => x.id === "run-with-dependents",
  )!;
  assert.equal(a.label, "Run feature + dependents");
});

test("run-with-dependents is blocked for the same reasons as run", () => {
  const { ctx } = recorder();
  const settled = mkFeature({
    lifecycle: "merged",
    taskCount: { total: 4, completed: 4, blocked: 0, active: 0 },
  });
  const acts = buildFeatureActions(settled, ctx);
  const run = acts.find((a) => a.id === "run")!;
  const deps = acts.find((a) => a.id === "run-with-dependents")!;
  assert.equal(deps.disabledReason, run.disabledReason);
});

test("cancel appears only when a chain is actually queued", () => {
  // A permanently-dead verb in the list teaches users to ignore the list.
  const withoutChain = recorder();
  assert.ok(
    !buildFeatureActions(mkFeature(), withoutChain.ctx).some(
      (a) => a.id === "cancel-chain",
    ),
    "cancel must be absent with no chain",
  );

  const withChain = recorder({ hasActiveChain: () => true });
  const a = buildFeatureActions(mkFeature(), withChain.ctx).find(
    (x) => x.id === "cancel-chain",
  );
  assert.ok(a, "cancel must appear once a chain is queued");
});

test("cancel routes to its own effect", async () => {
  const { ctx, calls } = recorder({ hasActiveChain: () => true });
  const a = buildFeatureActions(mkFeature(), ctx).find(
    (x) => x.id === "cancel-chain",
  )!;
  await a.run();
  assert.deepEqual(calls, ["cancel-chain:checkout-flow"]);
});


// ─── watch-focus (fan out running tasks into Focus panes) ──────────

test("watch-focus: enabled while the feature has active tasks", async () => {
  const { calls, ctx } = recorder();
  const a = byId(mkFeature(), ctx).get("watch-focus")!;
  assert.equal(isEnabled(a), true);
  await a.run();
  assert.deepEqual(calls, [`watch-focus:${mkFeature().id}`]);
});

test("watch-focus: disabled when nothing in the feature is active", () => {
  const { ctx } = recorder();
  const a = byId(
    mkFeature({ taskCount: { total: 3, completed: 3, blocked: 0, active: 0 } }),
    ctx,
  ).get("watch-focus")!;
  assert.match(a.disabledReason ?? "", /nothing is running/i);
});

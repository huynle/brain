/**
 * Tests for lib/actions/runnerActions.
 *
 * The things worth pinning: every verb is present for every runner
 * status (disabled-never-hidden), clear-assignments counts through the
 * optimistic drag map rather than trusting the server list, shutdown
 * confirms plainly (no type-to-confirm — a restartable process, not
 * data loss), and an offline runner cannot be shut down because the
 * SSE command has nowhere to land.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  buildRunnerActions,
  clearAssignmentsBlockedReason,
  combineRunnerAssignments,
  shutdownRunnerBlockedReason,
  type RunnerActionContext,
} from "./runnerActions";
import { isEnabled } from "./types";
import type { RunnerInfo } from "../types";

function mkRunner(over: Partial<RunnerInfo> = {}): RunnerInfo {
  return {
    runner_id: "runner-1",
    hostname: "amos",
    max_parallel: 4,
    registered_at: "2026-08-21T10:00:00Z",
    last_heartbeat: "2026-08-21T10:05:00Z",
    status: "online",
    ...over,
  };
}

function recorder() {
  const calls: string[] = [];
  const ctx: RunnerActionContext = {
    openShell: (r) => void calls.push(`shell:${r.runner_id}`),
    openDetails: (r) => void calls.push(`details:${r.runner_id}`),
    openProcesses: (r) => void calls.push(`processes:${r.runner_id}`),
    clearAssignments: async (r) => void calls.push(`clear:${r.runner_id}`),
    shutdownRunner: async (r) => void calls.push(`shutdown:${r.runner_id}`),
  };
  return { calls, ctx };
}

function byId(
  runner: RunnerInfo,
  ctx: RunnerActionContext,
  opts: { assignmentCount?: number } = {},
) {
  return new Map(
    buildRunnerActions(runner, ctx, opts).map((a) => [a.id, a]),
  );
}

// ─── presence ──────────────────────────────────────────────────────

test("every runner verb is present regardless of status", () => {
  const { ctx } = recorder();
  for (const status of ["online", "stale", "offline"] as const) {
    const ids = buildRunnerActions(mkRunner({ status }), ctx).map(
      (a) => a.id,
    );
    for (const expected of [
      "shell",
      "details",
      "processes",
      "clear-assignments",
      "shutdown",
    ]) {
      assert.ok(
        ids.includes(expected),
        `status=${status}: missing action ${expected}`,
      );
    }
  }
});

// ─── clear-assignments gating ──────────────────────────────────────

test("clear disabled with reason at zero assignments", () => {
  const { ctx } = recorder();
  const clear = byId(mkRunner(), ctx, { assignmentCount: 0 }).get(
    "clear-assignments",
  )!;
  assert.ok(!isEnabled(clear));
  assert.match(clear.disabledReason!, /no features are assigned/i);
  assert.equal(clear.label, "Clear assignment");
});

test("clear label counts: singular at one, 'all N' above", () => {
  const { ctx } = recorder();
  assert.equal(
    byId(mkRunner(), ctx, { assignmentCount: 1 }).get("clear-assignments")!
      .label,
    "Clear assignment",
  );
  assert.equal(
    byId(mkRunner(), ctx, { assignmentCount: 3 }).get("clear-assignments")!
      .label,
    "Clear all 3 assignments",
  );
});

test("without an explicit count, server assignments drive the verb", () => {
  const { ctx } = recorder();
  const actions = byId(
    mkRunner({
      feature_assignments: [
        { feature_id: "auth" },
        { feature_id: "billing" },
      ],
    }),
    ctx,
  );
  const clear = actions.get("clear-assignments")!;
  assert.ok(isEnabled(clear));
  assert.equal(clear.label, "Clear all 2 assignments");
});

test("clear is destructive-toned but confirm-free (reversible bulk unpin)", () => {
  const { ctx } = recorder();
  const clear = byId(mkRunner(), ctx, { assignmentCount: 2 }).get(
    "clear-assignments",
  )!;
  assert.ok(clear.danger);
  assert.equal(clear.confirm, undefined);
});

// ─── shutdown gating ───────────────────────────────────────────────

test("shutdown enabled for online and stale, blocked for offline", () => {
  assert.equal(shutdownRunnerBlockedReason(mkRunner({ status: "online" })), "");
  assert.equal(shutdownRunnerBlockedReason(mkRunner({ status: "stale" })), "");
  assert.match(
    shutdownRunnerBlockedReason(mkRunner({ status: "offline" })),
    /offline/i,
  );
  const { ctx } = recorder();
  assert.ok(!isEnabled(byId(mkRunner({ status: "offline" }), ctx).get("shutdown")!));
});

test("shutdown confirms plainly — no type-to-confirm", () => {
  const { ctx } = recorder();
  const shutdown = byId(mkRunner(), ctx).get("shutdown")!;
  assert.ok(shutdown.danger);
  assert.ok(shutdown.confirm);
  assert.equal(shutdown.confirm?.typeToConfirm, undefined);
  assert.match(shutdown.confirm!.title, /runner-1/);
});

test("clearAssignmentsBlockedReason clears at any positive count", () => {
  assert.match(clearAssignmentsBlockedReason(0), /no features/i);
  assert.equal(clearAssignmentsBlockedReason(1), "");
  assert.equal(clearAssignmentsBlockedReason(5), "");
});

// ─── routing ───────────────────────────────────────────────────────

test("each verb routes to its context effect", async () => {
  const { calls, ctx } = recorder();
  const actions = byId(mkRunner(), ctx, { assignmentCount: 1 });
  await actions.get("shell")!.run();
  await actions.get("details")!.run();
  await actions.get("processes")!.run();
  await actions.get("clear-assignments")!.run();
  await actions.get("shutdown")!.run();
  assert.deepEqual(calls, [
    "shell:runner-1",
    "details:runner-1",
    "processes:runner-1",
    "clear:runner-1",
    "shutdown:runner-1",
  ]);
});

// ─── combineRunnerAssignments ──────────────────────────────────────

test("optimistic assignments to this runner come first, deduped", () => {
  const runner = mkRunner({
    feature_assignments: [
      { feature_id: "auth", project_id: "brain-api" },
      { feature_id: "billing", project_id: "brain-api" },
    ],
  });
  const out = combineRunnerAssignments(runner, {
    search: "runner-1", // freshly dragged here, server doesn't know yet
    auth: "runner-1", // also on the server list — must not duplicate
  });
  assert.deepEqual(out, [
    { featureId: "search" },
    { featureId: "auth" },
    { featureId: "billing", projectId: "brain-api" },
  ]);
});

test("a feature optimistically moved to another runner is excluded", () => {
  const runner = mkRunner({
    feature_assignments: [{ feature_id: "auth", project_id: "brain-api" }],
  });
  const out = combineRunnerAssignments(runner, { auth: "runner-2" });
  assert.deepEqual(out, []);
});

test("no optimistic state falls through to the server list", () => {
  const runner = mkRunner({
    feature_assignments: [{ feature_id: "auth", project_id: "brain-api" }],
  });
  assert.deepEqual(combineRunnerAssignments(runner, {}), [
    { featureId: "auth", projectId: "brain-api" },
  ]);
});

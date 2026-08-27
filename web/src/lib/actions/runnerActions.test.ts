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
  isRunnerPaused,
  pauseRunnerBlockedReason,
  resumeRunnerBlockedReason,
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
    pauseRunner: async (r) => void calls.push(`pause:${r.runner_id}`),
    resumeRunner: async (r) => void calls.push(`resume:${r.runner_id}`),
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
      "pause",
      "resume",
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

// ─── the runner-scoped pause dial ──────────────────────────────────
// A third dial, independent of the two project dials. Its state lives
// on the runner row (`paused` on GET /runners), NOT in
// /tasks/runner/status — reading the latter for runner state is the
// footgun this pins.

test("pause dial defaults to off when the API omits the field", () => {
  // The server omits `paused` when false (omitempty), so absence must
  // read as running, not as unknown.
  assert.equal(isRunnerPaused(mkRunner()), false);
  assert.equal(isRunnerPaused(mkRunner({ paused: true })), true);
});

test("exactly one of pause/resume is enabled", () => {
  const { ctx } = recorder();

  const running = byId(mkRunner({ paused: false }), ctx);
  assert.equal(isEnabled(running.get("pause")!), true);
  assert.equal(isEnabled(running.get("resume")!), false);
  assert.match(running.get("resume")!.disabledReason ?? "", /not paused/i);

  const paused = byId(mkRunner({ paused: true }), ctx);
  assert.equal(isEnabled(paused.get("pause")!), false);
  assert.equal(isEnabled(paused.get("resume")!), true);
  assert.match(paused.get("pause")!.disabledReason ?? "", /already paused/i);
});

test("pause and resume route to their effects", async () => {
  const { calls, ctx } = recorder();
  await byId(mkRunner(), ctx).get("pause")!.run();
  await byId(mkRunner({ paused: true }), ctx).get("resume")!.run();
  assert.deepEqual(calls, ["pause:runner-1", "resume:runner-1"]);
});

test("pause is a state verb, distinct from the shutdown danger verb", () => {
  // Pause leaves the process running and registered; shutdown stops it.
  // Grouping pause under danger — or letting it inherit shutdown's
  // confirm — would blur two very different consequences.
  const { ctx } = recorder();
  const m = byId(mkRunner(), ctx);
  assert.equal(m.get("pause")!.group, "state");
  assert.equal(m.get("resume")!.group, "state");
  assert.equal(m.get("pause")!.danger, undefined);
  assert.equal(m.get("pause")!.confirm, undefined);
  assert.equal(m.get("shutdown")!.group, "danger");
  assert.equal(m.get("shutdown")!.danger, true);
  assert.ok(m.get("shutdown")!.confirm);
});

test("pause stays available on a runner that cannot be shut down", () => {
  // Shutdown needs a live SSE stream; the pause dial is persisted
  // server-side, so it applies to an offline runner too and will be
  // honoured when it reconnects.
  const { ctx } = recorder();
  const m = byId(mkRunner({ status: "offline" }), ctx);
  assert.equal(isEnabled(m.get("shutdown")!), false);
  assert.equal(isEnabled(m.get("pause")!), true);
});

test("pause label promises only that NEW dispatch stops", () => {
  const { ctx } = recorder();
  assert.match(byId(mkRunner(), ctx).get("pause")!.label, /new dispatch/i);
});

test("blocked-reason helpers are exact", () => {
  assert.equal(pauseRunnerBlockedReason(mkRunner()), "");
  assert.match(
    pauseRunnerBlockedReason(mkRunner({ paused: true })),
    /already paused/i,
  );
  assert.equal(resumeRunnerBlockedReason(mkRunner({ paused: true })), "");
  assert.match(resumeRunnerBlockedReason(mkRunner()), /not paused/i);
});

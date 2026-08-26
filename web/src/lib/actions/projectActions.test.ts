/**
 * Tests for lib/actions/projectActions — small on purpose, but the card
 * header is now a registry surface, so pin the verb set, the routing, and
 * the disable rules.
 *
 * The pause dials get the most attention because their trap is in the
 * response shape, not the UI: `paused` / `automationsPaused` are the
 * server's any-project rollups, and reading either as this project's
 * state marks the whole board paused when one project is.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  buildProjectActions,
  isProjectAutomationsPaused,
  isProjectTasksPaused,
  pauseDialBlockedReason,
  type ProjectActionContext,
} from "./projectActions";
import { isEnabled } from "./types";
import type { RunnerStatusResponse } from "../types";

function recorder() {
  const calls: string[] = [];
  const ctx: ProjectActionContext = {
    runProject: async (pid) => void calls.push(`run:${pid}`),
    openTaskList: (pid) => void calls.push(`focus:${pid}`),
    hideProject: (pid) => void calls.push(`hide:${pid}`),
    pauseProject: async (pid) => void calls.push(`pause:${pid}`),
    resumeProject: async (pid) => void calls.push(`resume:${pid}`),
    pauseAutomations: async (pid) => void calls.push(`pause-auto:${pid}`),
    resumeAutomations: async (pid) => void calls.push(`resume-auto:${pid}`),
  };
  return { calls, ctx };
}

function mkStatus(over: Partial<RunnerStatusResponse> = {}): RunnerStatusResponse {
  return {
    running: true,
    paused: false,
    pausedProjects: null,
    automationsPaused: false,
    automationPausedProjects: null,
    ...over,
  };
}

function byId(
  ctx: ProjectActionContext,
  opts: Parameters<typeof buildProjectActions>[2] = {},
) {
  return new Map(
    buildProjectActions("shop", ctx, opts).map((a) => [a.id, a]),
  );
}

// ─── presence / routing ────────────────────────────────────────────

test("every project verb is present", () => {
  const { ctx } = recorder();
  const ids = buildProjectActions("shop", ctx).map((a) => a.id);
  assert.deepEqual(ids, [
    "run",
    "pause",
    "resume",
    "pause-automations",
    "resume-automations",
    "focus-tasks",
    "hide",
  ]);
});

test("verbs route to their effects with the project id", async () => {
  const { calls, ctx } = recorder();
  for (const a of buildProjectActions("shop", ctx)) await a.run();
  assert.deepEqual(calls, [
    "run:shop",
    "pause:shop",
    "resume:shop",
    "pause-auto:shop",
    "resume-auto:shop",
    "focus:shop",
    "hide:shop",
  ]);
});

test("run is disabled with a reason for an empty project", () => {
  const { ctx } = recorder();
  const run = buildProjectActions("shop", ctx, { taskCount: 0 })[0];
  assert.equal(isEnabled(run), false);
  assert.match(run.disabledReason ?? "", /no tasks/i);
});

test("run stays enabled when the task count is unknown", () => {
  // The caller may not have a snapshot yet; absence of data must not
  // disable the verb.
  const { ctx } = recorder();
  const run = buildProjectActions("shop", ctx)[0];
  assert.equal(isEnabled(run), true);
});

// ─── reading pause state out of the status response ────────────────

test("a project is paused only when it is in pausedProjects", () => {
  assert.equal(
    isProjectTasksPaused(mkStatus({ pausedProjects: ["shop"] }), "shop"),
    true,
  );
  assert.equal(
    isProjectTasksPaused(mkStatus({ pausedProjects: ["other"] }), "shop"),
    false,
  );
});

test("the top-level paused rollup does NOT mark this project paused", () => {
  // The server derives `paused` as len(pausedProjects) > 0 — "some
  // project is paused". Folding it in would paint every card paused
  // the moment one was.
  const status = mkStatus({ paused: true, pausedProjects: ["other"] });
  assert.equal(isProjectTasksPaused(status, "shop"), false);
  const auto = mkStatus({
    automationsPaused: true,
    automationPausedProjects: ["other"],
  });
  assert.equal(isProjectAutomationsPaused(auto, "shop"), false);
});

test("the two dials are read independently", () => {
  // Tasks paused, automations not — and vice versa. Collapsing them
  // into one toggle is the mistake this pins.
  const tasksOnly = mkStatus({ paused: true, pausedProjects: ["shop"] });
  assert.equal(isProjectTasksPaused(tasksOnly, "shop"), true);
  assert.equal(isProjectAutomationsPaused(tasksOnly, "shop"), false);

  const autoOnly = mkStatus({
    automationsPaused: true,
    automationPausedProjects: ["shop"],
  });
  assert.equal(isProjectTasksPaused(autoOnly, "shop"), false);
  assert.equal(isProjectAutomationsPaused(autoOnly, "shop"), true);
});

test("null slices (Go nil) are treated as empty, not a crash", () => {
  const status = mkStatus({
    pausedProjects: null,
    automationPausedProjects: null,
  });
  assert.equal(isProjectTasksPaused(status, "shop"), false);
  assert.equal(isProjectAutomationsPaused(status, "shop"), false);
});

test("an unresolved status reads as unknown, not as not-paused", () => {
  assert.equal(isProjectTasksPaused(undefined, "shop"), undefined);
  assert.equal(isProjectAutomationsPaused(undefined, "shop"), undefined);
});

// ─── the status-aware pairs ────────────────────────────────────────

test("exactly one of pause/resume is enabled when state is known", () => {
  const { ctx } = recorder();

  const running = byId(ctx, { tasksPaused: false });
  assert.equal(isEnabled(running.get("pause")!), true);
  assert.equal(isEnabled(running.get("resume")!), false);
  assert.match(running.get("resume")!.disabledReason ?? "", /not paused/i);

  const paused = byId(ctx, { tasksPaused: true });
  assert.equal(isEnabled(paused.get("pause")!), false);
  assert.equal(isEnabled(paused.get("resume")!), true);
  assert.match(paused.get("pause")!.disabledReason ?? "", /already paused/i);
});

test("the automations pair is status-aware on its own dial", () => {
  const { ctx } = recorder();
  // Tasks paused but automations running: the automations pair must
  // follow the automations dial, not the task one.
  const m = byId(ctx, { tasksPaused: true, automationsPaused: false });
  assert.equal(isEnabled(m.get("pause-automations")!), true);
  assert.equal(isEnabled(m.get("resume-automations")!), false);
  assert.equal(isEnabled(m.get("pause")!), false);
  assert.equal(isEnabled(m.get("resume")!), true);
});

test("both halves stay enabled while pause state is unknown", () => {
  // Status hasn't loaded. Both endpoints are idempotent, so an unknown
  // state must not disable the verb (same rule as an unknown taskCount).
  const { ctx } = recorder();
  const m = byId(ctx, {});
  for (const id of ["pause", "resume", "pause-automations", "resume-automations"]) {
    assert.equal(isEnabled(m.get(id)!), true, id);
  }
});

test("pauseDialBlockedReason names the dial it is talking about", () => {
  assert.equal(pauseDialBlockedReason(undefined, true, "Tasks"), "");
  assert.match(pauseDialBlockedReason(true, true, "Tasks"), /Tasks are already paused/);
  assert.match(
    pauseDialBlockedReason(false, false, "Automations"),
    /Automations are not paused/,
  );
  assert.equal(pauseDialBlockedReason(true, false, "Tasks"), "");
});

// ─── labels ────────────────────────────────────────────────────────

test("pause labels promise only that NEW dispatch stops", () => {
  // A running process runs to completion, and force-dispatch ("Run
  // now") deliberately bypasses pause. A bare "Pause project" would
  // claim more than the dial does.
  const { ctx } = recorder();
  const m = byId(ctx, {});
  assert.match(m.get("pause")!.label, /new dispatch/i);
});

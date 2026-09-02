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
  summarizeDeleteProjectResult,
  type ProjectActionContext,
} from "./projectActions";
import { isEnabled } from "./types";
import type { DeleteProjectResponse, RunnerStatusResponse } from "../types";
import { buildPauseState, EMPTY_PAUSE_STATE } from "../pause";

/** Adapt a raw status response into the PauseState the predicates now take.
 *  The predicates moved to lib/pause during the #35 + #37 merge so the
 *  controls and the badges read one model; these cases still pin the
 *  response-shape traps that motivated them. */
function pauseOf(status: RunnerStatusResponse) {
  return buildPauseState(status, []);
}

function recorder() {
  const calls: string[] = [];
  const ctx: ProjectActionContext = {
    runProject: async (pid) => void calls.push(`run:${pid}`),
    openProject: (pid) => void calls.push(`focus:${pid}`),
    hideProject: (pid) => void calls.push(`hide:${pid}`),
    pauseProject: async (pid) => void calls.push(`pause:${pid}`),
    resumeProject: async (pid) => void calls.push(`resume:${pid}`),
    pauseAutomations: async (pid) => void calls.push(`pause-auto:${pid}`),
    resumeAutomations: async (pid) => void calls.push(`resume-auto:${pid}`),
    deleteProject: async (pid) => void calls.push(`delete:${pid}`),
  };
  return { calls, ctx };
}

function mkStatus(
  over: Partial<RunnerStatusResponse> = {},
): RunnerStatusResponse {
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
  return new Map(buildProjectActions("shop", ctx, opts).map((a) => [a.id, a]));
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
    "focus-project",
    "hide",
    "delete",
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
    "delete:shop",
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
    isProjectTasksPaused(
      pauseOf(mkStatus({ pausedProjects: ["shop"] })),
      "shop",
    ),
    true,
  );
  assert.equal(
    isProjectTasksPaused(
      pauseOf(mkStatus({ pausedProjects: ["other"] })),
      "shop",
    ),
    false,
  );
});

test("the top-level paused rollup does NOT mark this project paused", () => {
  // The server derives `paused` as len(pausedProjects) > 0 — "some
  // project is paused". Folding it in would paint every card paused
  // the moment one was.
  const status = mkStatus({ paused: true, pausedProjects: ["other"] });
  assert.equal(isProjectTasksPaused(pauseOf(status), "shop"), false);
  const auto = mkStatus({
    automationsPaused: true,
    automationPausedProjects: ["other"],
  });
  assert.equal(isProjectAutomationsPaused(pauseOf(auto), "shop"), false);
});

test("the two dials are read independently", () => {
  // Tasks paused, automations not — and vice versa. Collapsing them
  // into one toggle is the mistake this pins.
  const tasksOnly = mkStatus({ paused: true, pausedProjects: ["shop"] });
  assert.equal(isProjectTasksPaused(pauseOf(tasksOnly), "shop"), true);
  assert.equal(isProjectAutomationsPaused(pauseOf(tasksOnly), "shop"), false);

  const autoOnly = mkStatus({
    automationsPaused: true,
    automationPausedProjects: ["shop"],
  });
  assert.equal(isProjectTasksPaused(pauseOf(autoOnly), "shop"), false);
  assert.equal(isProjectAutomationsPaused(pauseOf(autoOnly), "shop"), true);
});

test("null slices (Go nil) are treated as empty, not a crash", () => {
  const status = mkStatus({
    pausedProjects: null,
    automationPausedProjects: null,
  });
  assert.equal(isProjectTasksPaused(pauseOf(status), "shop"), false);
  assert.equal(isProjectAutomationsPaused(pauseOf(status), "shop"), false);
});

test("an empty pause state reads as not-paused", () => {
  // PauseState is total: there is no "unknown" inside it. Callers model
  // "not loaded yet" by passing `undefined` for the flags (see the
  // isLoading gate in ProjectCard/ProjectsSection), which keeps
  // pauseDialBlockedReason from disabling a verb before data lands.
  assert.equal(isProjectTasksPaused(EMPTY_PAUSE_STATE, "shop"), false);
  assert.equal(isProjectAutomationsPaused(EMPTY_PAUSE_STATE, "shop"), false);
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
  for (const id of [
    "pause",
    "resume",
    "pause-automations",
    "resume-automations",
  ]) {
    assert.equal(isEnabled(m.get(id)!), true, id);
  }
});

test("pauseDialBlockedReason names the dial it is talking about", () => {
  assert.equal(pauseDialBlockedReason(undefined, true, "Tasks"), "");
  assert.match(
    pauseDialBlockedReason(true, true, "Tasks"),
    /Tasks are already paused/,
  );
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

// ─── delete ────────────────────────────────────────────────────────
//
// The verb sits one right-click away from Hide, its reversible neighbour,
// so the guards around it are the point of these cases.

test("delete requires typing the project name", () => {
  const { ctx } = recorder();
  const del = byId(ctx).get("delete");
  assert.ok(del);
  assert.equal(del.confirm?.typeToConfirm, "shop");
  assert.equal(del.danger, true);
  assert.equal(del.group, "danger");
});

test("delete's type-to-confirm tracks the project it was built for", () => {
  // A dialog that accepts the wrong name is a dialog that deletes the
  // wrong project — pin that the string is per-project, not a constant.
  const { ctx } = recorder();
  const other = buildProjectActions("warehouse", ctx).find(
    (a) => a.id === "delete",
  );
  assert.equal(other?.confirm?.typeToConfirm, "warehouse");
});

test("delete carries no keyboard accelerator", () => {
  // Every other single-key verb on a row is recoverable. A bare letter
  // that starts erasing a project is not something to leave under a
  // finger resting on the list.
  const { ctx } = recorder();
  assert.equal(byId(ctx).get("delete")?.key, undefined);
});

test("delete stays enabled for an empty project", () => {
  // "Empty" here means no TASKS. The project still has a directory, pause
  // dials and possibly notes the sidebar count never showed — and deleting
  // the leftover name is exactly what the verb is for.
  const { ctx } = recorder();
  const del = byId(ctx, { taskCount: 0 }).get("delete");
  assert.ok(del);
  assert.equal(isEnabled(del), true);
});

test("delete's confirm body names hide as the reversible alternative", () => {
  const { ctx } = recorder();
  const body = byId(ctx).get("delete")?.confirm?.body ?? "";
  assert.match(body, /cannot be undone/i);
  assert.match(body, /hide it instead/i);
});

// ─── delete summaries ──────────────────────────────────────────────

function mkDeleteResult(
  over: Partial<DeleteProjectResponse> = {},
): DeleteProjectResponse {
  return {
    project: "shop",
    deleted: 12,
    failed: 0,
    directory_removed: true,
    ...over,
  };
}

test("summarizeDeleteProjectResult: clean wipe reports the count", () => {
  const msg = summarizeDeleteProjectResult(mkDeleteResult());
  assert.match(msg, /shop deleted/);
  assert.match(msg, /12 entries/);
});

test("summarizeDeleteProjectResult: a single entry is not pluralised", () => {
  const msg = summarizeDeleteProjectResult(mkDeleteResult({ deleted: 1 }));
  assert.match(msg, /1 entry/);
  assert.doesNotMatch(msg, /1 entries/);
});

test("summarizeDeleteProjectResult: an empty project is not an error", () => {
  // The sidebar counts only tasks, so a project can look empty and still
  // exist. Deleting the leftover name has to read as success.
  const msg = summarizeDeleteProjectResult(mkDeleteResult({ deleted: 0 }));
  assert.match(msg, /no entries/);
});

test("summarizeDeleteProjectResult: a partial wipe leads with the failure", () => {
  // "deleted 40 entries" is true and useless when 3 are still there — the
  // leftovers are the only part the user has to act on.
  const msg = summarizeDeleteProjectResult(
    mkDeleteResult({ deleted: 40, failed: 3 }),
  );
  assert.match(msg, /^shop: 3 failed/);
  assert.match(msg, /40 entries deleted/);
});

test("summarizeDeleteProjectResult: a partial wipe quotes the server's reason", () => {
  const msg = summarizeDeleteProjectResult(
    mkDeleteResult({
      deleted: 2,
      failed: 1,
      errors: ["projects/shop/task/x.md: permission denied"],
    }),
  );
  assert.match(msg, /permission denied/);
});

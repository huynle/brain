import test from "node:test";
import assert from "node:assert/strict";

import {
  EMPTY_PAUSE_STATE,
  isTaskExecuting,
  projectRunIndicator,
  allRunnersPaused,
  anyPaused,
  buildPauseState,
  forceDispatchNote,
  isAutomationTask,
  isReadyUndispatched,
  pauseSummary,
  projectPauseBadges,
  withForceNote,
  runnerDotState,
  schedulerHoldNote,
  taskHoldReason,
  featureGateReason,
  featureDepWarning,
} from "./pause";
import type {
  RunnerInfo,
  RunnerStatusResponse,
  SchedulerResult,
  Task,
} from "./types";

// ─── fixtures ────────────────────────────────────────────────────

function runner(over: Partial<RunnerInfo> = {}): RunnerInfo {
  return {
    runner_id: "runner-a",
    hostname: "host",
    max_parallel: 2,
    registered_at: "2026-08-26T00:00:00Z",
    last_heartbeat: "2026-08-26T00:00:00Z",
    status: "online",
    ...over,
  };
}

function task(over: Partial<Task> = {}): Task {
  return {
    id: "t1",
    path: "p/t1.md",
    title: "Task one",
    priority: "medium",
    status: "pending",
    classification: "ready",
    ...over,
  };
}

const status = (
  over: Partial<RunnerStatusResponse> = {},
): RunnerStatusResponse => ({
  running: true,
  paused: false,
  pausedProjects: null,
  automationsPaused: false,
  automationPausedProjects: null,
  ...over,
});

// ─── buildPauseState ─────────────────────────────────────────────

test("buildPauseState folds both endpoints into the three dials", () => {
  const s = buildPauseState(
    status({
      paused: true,
      pausedProjects: ["alpha"],
      automationsPaused: true,
      automationPausedProjects: ["beta"],
    }),
    [runner({ runner_id: "r1", paused: true }), runner({ runner_id: "r2" })],
  );
  assert.deepEqual([...s.projectTasks], ["alpha"]);
  assert.deepEqual([...s.projectAutomations], ["beta"]);
  assert.deepEqual([...s.runners], ["r1"]);
  assert.equal(s.runnerCount, 2);
  assert.equal(s.availableRunnerCount, 1);
});

test("buildPauseState survives Go's null-for-empty-slice marshalling", () => {
  const s = buildPauseState(status(), [runner()]);
  assert.equal(anyPaused(s), false);
  assert.equal(s.projectTasks.size, 0);
  assert.equal(s.projectAutomations.size, 0);
});

test("buildPauseState with no status yet reports nothing paused, not an error", () => {
  const s = buildPauseState(undefined, [runner()]);
  assert.equal(anyPaused(s), false);
  assert.equal(s.availableRunnerCount, 1);
});

test("availableRunnerCount excludes offline runners as well as paused ones", () => {
  const s = buildPauseState(undefined, [
    runner({ runner_id: "r1", status: "offline" }),
    runner({ runner_id: "r2", paused: true }),
    runner({ runner_id: "r3" }),
  ]);
  assert.equal(s.availableRunnerCount, 1);
  assert.equal(allRunnersPaused(s), false);
});

test("allRunnersPaused is false with zero runners — that is 'none', not 'paused'", () => {
  assert.equal(allRunnersPaused(EMPTY_PAUSE_STATE), false);
});

// ─── runnerDotState ──────────────────────────────────────────────

test("a paused-but-online runner does NOT get the same dot as a working one", () => {
  const paused = runner({ paused: true });
  const working = runner();
  assert.equal(runnerDotState(working), "on");
  assert.equal(runnerDotState(paused), "paused");
  assert.notEqual(runnerDotState(paused), runnerDotState(working));
});

test("runnerDotState keeps offline/stale distinct from paused", () => {
  assert.equal(runnerDotState(runner({ status: "stale" })), "err");
  assert.equal(runnerDotState(runner({ status: "offline" })), "");
  // A runner that is BOTH gone and paused reads as gone — that is the
  // operator's actual problem.
  assert.equal(runnerDotState(runner({ status: "offline", paused: true })), "");
});

// ─── pauseSummary ────────────────────────────────────────────────

test("pauseSummary is null when nothing is paused — statusbar stays quiet", () => {
  assert.equal(pauseSummary(buildPauseState(status(), [runner()])), null);
});

test("pauseSummary reports the verified two-dial case (project + runner)", () => {
  const s = buildPauseState(
    status({ paused: true, pausedProjects: ["sandbox-demo"] }),
    [runner({ runner_id: "r1", paused: true })],
  );
  const summary = pauseSummary(s, { projectCount: 1 });
  assert.ok(summary);
  // Both dials named, not collapsed into one "paused".
  assert.match(summary!.text, /project/);
  assert.match(summary!.text, /runner/);
  assert.equal(summary!.tone, "halt");
  assert.match(summary!.title, /sandbox-demo/);
  assert.match(summary!.title, /r1/);
  assert.match(summary!.title, /independent dials/);
});

test("pauseSummary tone is partial while some runner can still take work", () => {
  const s = buildPauseState(
    status({ paused: true, pausedProjects: ["alpha"] }),
    [runner({ runner_id: "r1" }), runner({ runner_id: "r2", paused: true })],
  );
  const summary = pauseSummary(s, { projectCount: 3 });
  assert.equal(summary!.tone, "partial");
  assert.match(summary!.text, /1 project/);
  assert.match(summary!.text, /1 runner/);
});

test("pauseSummary upgrades to 'all projects' only when the count proves it", () => {
  const s = buildPauseState(
    status({ paused: true, pausedProjects: ["a", "b"] }),
    [runner()],
  );
  assert.match(pauseSummary(s, { projectCount: 2 })!.text, /all projects/);
  assert.match(pauseSummary(s, { projectCount: 5 })!.text, /2 projects/);
  // Without a denominator it must not claim "all".
  assert.match(pauseSummary(s)!.text, /2 projects/);
});

test("pauseSummary names the automations dial separately from the task dial", () => {
  const s = buildPauseState(
    status({ automationsPaused: true, automationPausedProjects: ["alpha"] }),
    [runner()],
  );
  const summary = pauseSummary(s, { projectCount: 1 });
  assert.match(summary!.text, /automations/);
  assert.doesNotMatch(summary!.text, /\bproject\b/);
  assert.match(summary!.title, /Project automations paused: alpha/);
});

test("pauseSummary says force cannot help when every runner is paused", () => {
  const s = buildPauseState(status(), [runner({ paused: true })]);
  const summary = pauseSummary(s);
  assert.equal(summary!.tone, "halt");
  assert.match(summary!.title, /no override|cannot override/i);
  // Singular fleet reads as "the runner", not "all 1 runners".
  assert.match(summary!.text, /the runner/);
});

// ─── projectPauseBadges ──────────────────────────────────────────

test("projectPauseBadges separates the two project dials", () => {
  const s = buildPauseState(
    status({
      pausedProjects: ["alpha"],
      automationPausedProjects: ["beta"],
    }),
    [],
  );
  assert.deepEqual(
    [
      projectPauseBadges(s, "alpha").tasks,
      projectPauseBadges(s, "alpha").automations,
    ],
    [true, false],
  );
  assert.deepEqual(
    [
      projectPauseBadges(s, "beta").tasks,
      projectPauseBadges(s, "beta").automations,
    ],
    [false, true],
  );
  const none = projectPauseBadges(s, "gamma");
  assert.equal(none.tasks, false);
  assert.equal(none.automations, false);
});

// ─── isAutomationTask / isReadyUndispatched ──────────────────────

test("isAutomationTask matches the server's generated_by prefix test", () => {
  assert.equal(
    isAutomationTask(task({ generated_by: "automation:goal-x" })),
    true,
  );
  assert.equal(isAutomationTask(task({ generated_by: "brain-goal" })), false);
  assert.equal(isAutomationTask(task()), false);
});

test("isReadyUndispatched excludes waiting, blocked and terminal tasks", () => {
  assert.equal(isReadyUndispatched(task()), true);
  assert.equal(isReadyUndispatched(task({ classification: "waiting" })), false);
  assert.equal(isReadyUndispatched(task({ classification: "blocked" })), false);
  assert.equal(isReadyUndispatched(task({ status: "in_progress" })), false);
  assert.equal(isReadyUndispatched(task({ status: "completed" })), false);
});

test("isReadyUndispatched excludes a task already carrying a live lease", () => {
  const leased = task({
    dispatch_lease: {
      leaseId: "l1",
      project_id: "alpha",
      task_id: "t1",
      assigned_runner_id: "r1",
      assigned_machine_id: "m1",
      state: "pushed",
      pushed_at: 1,
      expires_at: 2,
    },
  });
  assert.equal(isReadyUndispatched(leased), false);
});

// ─── taskHoldReason ──────────────────────────────────────────────

const pausedProject = buildPauseState(
  status({ paused: true, pausedProjects: ["alpha"] }),
  [runner()],
);

test("taskHoldReason names the project task dial for a manual task", () => {
  const r = taskHoldReason(task(), {
    pause: pausedProject,
    projectId: "alpha",
  });
  assert.equal(r!.code, "project_paused");
  assert.match(r!.detail, /TASK dial/);
});

test("the project TASK dial does not hold an automation task", () => {
  // Mirrors shouldSkipTask: automation tasks answer to the autos dial only.
  const r = taskHoldReason(task({ generated_by: "automation:goal-x" }), {
    pause: pausedProject,
    projectId: "alpha",
  });
  assert.equal(r, null);
});

test("the project AUTOMATIONS dial holds only automation tasks", () => {
  const s = buildPauseState(status({ automationPausedProjects: ["alpha"] }), [
    runner(),
  ]);
  const auto = taskHoldReason(task({ generated_by: "automation:goal-x" }), {
    pause: s,
    projectId: "alpha",
  });
  assert.equal(auto!.code, "automations_paused");
  const manual = taskHoldReason(task(), { pause: s, projectId: "alpha" });
  assert.equal(manual, null);
});

test("a paused project does not hold tasks in a different project", () => {
  assert.equal(
    taskHoldReason(task(), { pause: pausedProject, projectId: "beta" }),
    null,
  );
});

test("fleet-wide runner pause outranks a per-task placement reason", () => {
  const s = buildPauseState(status(), [runner({ paused: true })]);
  const r = taskHoldReason(
    task({
      last_placement_reason: { decision: "no_candidate", reason: "stale" },
    }),
    { pause: s, projectId: "alpha" },
  );
  assert.equal(r!.code, "runners_paused");
  assert.match(r!.detail, /no force override/i);
});

test("taskHoldReason falls back to the task's own placement reason", () => {
  const s = buildPauseState(status(), [runner()]);
  const r = taskHoldReason(
    task({
      last_placement_reason: {
        decision: "no_candidate",
        reason: "runner-a: project not allowed",
      },
    }),
    { pause: s, projectId: "alpha" },
  );
  assert.equal(r!.code, "no_candidate");
  assert.match(r!.detail, /project not allowed/);
});

test("taskHoldReason reports an empty fleet distinctly from a paused one", () => {
  const r = taskHoldReason(task(), {
    pause: buildPauseState(status(), []),
    projectId: "alpha",
  });
  assert.equal(r!.code, "no_runners");
});

test("taskHoldReason is null for a healthy, unheld ready task", () => {
  assert.equal(
    taskHoldReason(task(), {
      pause: buildPauseState(status(), [runner()]),
      projectId: "alpha",
    }),
    null,
  );
});

test("taskHoldReason never annotates a task that is not ready", () => {
  assert.equal(
    taskHoldReason(task({ classification: "waiting" }), {
      pause: pausedProject,
      projectId: "alpha",
    }),
    null,
  );
});

// ─── schedulerHoldNote ───────────────────────────────────────────

const result = (over: Partial<SchedulerResult> = {}): SchedulerResult => ({
  project_id: "sandbox-demo",
  considered: 1,
  dispatched: 0,
  skipped: 1,
  ...over,
});

test("schedulerHoldNote reads the verified sandbox payload", () => {
  const note = schedulerHoldNote(result({ skipped_tasks_paused: 1 }));
  assert.match(note!.short, /1 held/);
  assert.match(note!.short, /project paused/);
  assert.match(note!.detail, /TASK dial/);
});

test("schedulerHoldNote is null when nothing was actually held", () => {
  assert.equal(schedulerHoldNote(undefined), null);
  assert.equal(
    schedulerHoldNote(result({ considered: 2, dispatched: 2, skipped: 0 })),
    null,
  );
});

test("already-leased work is in flight and must not count as held", () => {
  assert.equal(
    schedulerHoldNote(result({ skipped: 1, skipped_already_leased: 1 })),
    null,
  );
});

test("schedulerHoldNote breaks a mixed pass down by cause", () => {
  const note = schedulerHoldNote(
    result({
      considered: 4,
      dispatched: 1,
      skipped: 3,
      skipped_tasks_paused: 1,
      skipped_automations_paused: 1,
      skipped_no_candidate: 1,
    }),
  );
  assert.match(note!.short, /3 held/);
  assert.match(note!.detail, /AUTOMATIONS dial/);
  assert.match(note!.detail, /no eligible runner/);
});

// ─── forceDispatchNote ───────────────────────────────────────────

test("force run against a paused project says the dial was bypassed", () => {
  const note = forceDispatchNote(pausedProject, { projectId: "alpha" });
  assert.match(note!, /bypassed/);
});

test("force run against an all-paused fleet says the override cannot apply", () => {
  const s = buildPauseState(status(), [runner({ paused: true })]);
  const note = forceDispatchNote(s, { projectId: "alpha" });
  assert.match(note!, /no override/);
  assert.match(note!, /nothing could be placed/);
});

test("force notes carry no em-dash of their own", () => {
  // The server's reason strings already use em-dashes; a second dashed
  // clause appended to them produced unreadable pileups in the toast.
  const cases = [
    forceDispatchNote(pausedProject, { projectId: "alpha" }),
    forceDispatchNote(buildPauseState(status(), [runner({ paused: true })]), {
      projectId: "alpha",
    }),
    forceDispatchNote(
      buildPauseState(status({ automationPausedProjects: ["alpha"] }), [
        runner(),
      ]),
      { projectId: "alpha", automation: true },
    ),
  ];
  for (const note of cases) {
    assert.ok(note);
    assert.doesNotMatch(note!, /—/);
  }
});

test("withForceNote appends only when there is a note", () => {
  assert.equal(withForceNote("Dispatched 1 task", null), "Dispatched 1 task");
  assert.equal(
    withForceNote("Dispatched 1 task", "pause bypassed"),
    "Dispatched 1 task · pause bypassed",
  );
});

test("force run on a healthy system adds no note", () => {
  assert.equal(
    forceDispatchNote(buildPauseState(status(), [runner()]), {
      projectId: "alpha",
    }),
    null,
  );
});

test("force run notes the automations dial for an automation task", () => {
  const s = buildPauseState(status({ automationPausedProjects: ["alpha"] }), [
    runner(),
  ]);
  assert.match(
    forceDispatchNote(s, { projectId: "alpha", automation: true })!,
    /automations dial/,
  );
  // The manual dial is a different question and stays quiet.
  assert.equal(forceDispatchNote(s, { projectId: "alpha" }), null);
});

test("both a project dial AND the whole fleet paused names both switches", () => {
  // The trap this guards: resuming only the project leaves the task held by
  // the runner dial, with the UI no longer explaining why.
  const s = buildPauseState(status({ pausedProjects: ["alpha"] }), [
    runner({ paused: true }),
  ]);
  const r = taskHoldReason(task(), { pause: s, projectId: "alpha" });
  assert.equal(r!.code, "project_paused");
  assert.match(r!.detail, /TASK dial/);
  assert.match(r!.detail, /every registered runner/i);
  assert.match(r!.detail, /resume a runner too/i);
});

test("the fleet note is absent when runners can still take work", () => {
  const r = taskHoldReason(task(), {
    pause: pausedProject,
    projectId: "alpha",
  });
  assert.doesNotMatch(r!.detail, /resume a runner too/i);
});

test("deliberate holds get ⏸; unchosen ones get ⚠", () => {
  // A user seeing ⏸ goes looking for a switch to flip. "No eligible runner"
  // has no switch, so it must not wear the pause glyph.
  const noRunner = taskHoldReason(
    task({ last_placement_reason: { decision: "no_candidate", reason: "x" } }),
    { pause: buildPauseState(status(), [runner()]), projectId: "alpha" },
  );
  assert.equal(noRunner!.glyph, "⚠");
  assert.equal(
    taskHoldReason(task(), { pause: pausedProject, projectId: "alpha" })!.glyph,
    "⏸",
  );
  assert.equal(
    taskHoldReason(task(), {
      pause: buildPauseState(status(), []),
      projectId: "alpha",
    })!.glyph,
    "⚠",
  );
});

test("schedulerHoldNote marks pause-caused holds apart from no-runner ones", () => {
  const paused = schedulerHoldNote(result({ skipped_tasks_paused: 1 }));
  assert.equal(paused!.glyph, "⏸");
  assert.equal(paused!.tone, "paused");

  const noRunner = schedulerHoldNote(result({ skipped_no_candidate: 1 }));
  assert.equal(noRunner!.glyph, "⚠");
  assert.equal(noRunner!.tone, "warn");

  // Mixed causes: a real pause dial IS in play, so it reads as deliberate.
  const mixed = schedulerHoldNote(
    result({ skipped: 2, skipped_tasks_paused: 1, skipped_no_candidate: 1 }),
  );
  assert.equal(mixed!.glyph, "⏸");
});

// ─── feature-level gating ────────────────────────────────────────
//
// applyFeatureGating (internal/service/taskdeps.go) only ever downgrades a
// "ready" task, so when these fields are set they are the WHOLE reason the
// task is not running. The blocking party is a feature, which has no row in
// the task tree — so without a chip the task just sits at "waiting".

const gated = (over: Partial<Task> = {}) =>
  task({ classification: "waiting", ...over });

test("a feature-gated task explains itself even though it is not 'ready'", () => {
  // The bug this pins: isReadyUndispatched bails on classification !== "ready",
  // which would silence every feature-gated task.
  const t = gated({ waiting_on_features: ["data-pipeline"] });
  assert.equal(isReadyUndispatched(t), false);

  const hold = taskHoldReason(t, {
    pause: EMPTY_PAUSE_STATE,
    projectId: "sandbox-demo",
  });
  assert.equal(hold?.code, "waiting_on_features");
  assert.equal(hold?.short, "waits on data-pipeline");
});

test("a self-clearing feature wait is neither a dial nor a fault", () => {
  // ⏸ would send the user hunting for a switch; ⚠ would imply something is
  // wrong. Upstream work is simply still running.
  const hold = featureGateReason(gated({ waiting_on_features: ["a"] }));
  assert.equal(hold?.glyph, "⇢");
});

test("multiple waited-on features collapse in the chip, not the tooltip", () => {
  const hold = featureGateReason(
    gated({ waiting_on_features: ["a", "b", "c"] }),
  );
  assert.equal(hold?.short, "waits on 3 features");
  assert.match(hold!.detail, /"a", "b", and "c"/);
});

test("a blocked upstream feature reads as a fault, not a wait", () => {
  const hold = featureGateReason(
    gated({ classification: "blocked", blocked_by_features: ["upstream"] }),
  );
  assert.equal(hold?.code, "feature_blocked");
  assert.equal(hold?.glyph, "⚠");
});

test("a dependency cycle is called out as a cycle", () => {
  // Distinct from a plain block: a cycle never resolves without an edit.
  const hold = featureGateReason(
    gated({
      classification: "blocked",
      blocked_by_features: ["b"],
      blocked_by_reason: "feature_circular_dependency",
    }),
  );
  assert.equal(hold?.code, "feature_cycle");
  assert.match(hold!.detail, /never resolves on its own/);
});

test("a cycle is detected with an EMPTY blocked_by_features list", () => {
  // This is the shape the server actually sends. classifyFeature signals a
  // cycle through InCycle and leaves BlockedByFeatures empty — there is no
  // single upstream feature to name when members block each other.
  // Verified live: an A<->B feature cycle produces
  //   classification=blocked, blocked_by_features=null,
  //   blocked_by_reason="feature_circular_dependency"
  // Keying the branch on the list left every task in a cycle with NO chip.
  const hold = featureGateReason(
    gated({
      classification: "blocked",
      blocked_by_reason: "feature_circular_dependency",
    }),
  );
  assert.equal(hold?.code, "feature_cycle");
  assert.match(hold!.detail, /leads? back to itself|cycle/);
});

test("a blocked feature is detected from the reason alone too", () => {
  const hold = featureGateReason(
    gated({
      classification: "blocked",
      blocked_by_reason: "feature_dependency_blocked",
    }),
  );
  assert.equal(hold?.code, "feature_blocked");
});

test("blocked outranks waiting when both are set", () => {
  const hold = featureGateReason(
    gated({
      classification: "blocked",
      blocked_by_features: ["b"],
      waiting_on_features: ["w"],
    }),
  );
  assert.equal(hold?.code, "feature_blocked");
});

test("the feature gate is named ahead of a pause dial", () => {
  // Both are true, but only one is the binding constraint: resuming the
  // project would not release a feature-gated task.
  const pause = buildPauseState(
    {
      running: true,
      paused: true,
      pausedProjects: ["p"],
      automationsPaused: false,
      automationPausedProjects: null,
    },
    [],
  );
  const hold = taskHoldReason(gated({ waiting_on_features: ["upstream"] }), {
    pause,
    projectId: "p",
  });
  assert.equal(hold?.code, "waiting_on_features");
});

test("an ungated task is unaffected", () => {
  assert.equal(featureGateReason(task()), null);
  // A healthy fleet, so none of the non-feature hold branches fire either.
  // (EMPTY_PAUSE_STATE would report "no runners" — correctly, but that is a
  // different hold and would mask what this case is pinning.)
  const healthy = buildPauseState(
    {
      running: true,
      paused: false,
      pausedProjects: null,
      automationsPaused: false,
      automationPausedProjects: null,
    },
    [runner()],
  );
  assert.equal(
    taskHoldReason(task(), { pause: healthy, projectId: "p" }),
    null,
  );
});

test("an unresolved feature dep warns even on a healthy task", () => {
  // These gate NOTHING, so the task may be running fine — which is exactly
  // why it needs saying: the ordering the author wrote is not in effect.
  const t = task({
    status: "in_progress",
    unresolved_feature_deps: ["typo-feat"],
  });
  const warn = featureDepWarning(t);
  assert.equal(warn?.short, "unknown feature dep");
  // Not "feature_blocked": nothing is blocked, the ordering is simply absent.
  assert.equal(warn?.code, "feature_dep_unresolved");
  assert.match(warn!.detail, /gate NOTHING/);
  // and it is NOT reported as a hold, because nothing is held
  assert.equal(featureGateReason(t), null);
});

test("no unresolved deps means no warning", () => {
  assert.equal(featureDepWarning(task()), null);
  assert.equal(featureDepWarning(task({ unresolved_feature_deps: [] })), null);
});

// ─── projectRunIndicator (the pause/play control) ────────────────

const RUNNING = { status: "in_progress" } as const;

test("projectRunIndicator: an unpaused project shows the play glyph", () => {
  const i = projectRunIndicator([task()], {
    paused: false,
    projectId: "shop",
  });
  assert.equal(i.paused, false);
  assert.equal(i.state, "on");
  assert.equal(i.actionLabel, "Pause shop");
});

test("projectRunIndicator: work in flight paints it busy", () => {
  const i = projectRunIndicator([task(), task({ id: "t2", ...RUNNING })], {
    paused: false,
    projectId: "shop",
  });
  assert.equal(i.state, "busy");
  assert.equal(i.liveCount, 1);
});

test("projectRunIndicator: blocked outranks ready, running outranks blocked", () => {
  const blocked = task({ id: "b", status: "blocked" });
  assert.equal(
    projectRunIndicator([task(), blocked], { paused: false, projectId: "p" })
      .state,
    "err",
  );
  assert.equal(
    projectRunIndicator([blocked, task({ id: "r", ...RUNNING })], {
      paused: false,
      projectId: "p",
    }).state,
    "busy",
  );
});

test("projectRunIndicator: an empty project has no colour to report", () => {
  const i = projectRunIndicator([], { paused: false, projectId: "p" });
  assert.equal(i.state, "");
  assert.equal(i.liveCount, 0);
});

test("projectRunIndicator: a quiet paused project reads as held", () => {
  const i = projectRunIndicator([task(), task({ id: "b", status: "blocked" })], {
    paused: true,
    projectId: "shop",
  });
  assert.equal(i.state, "paused");
  assert.equal(i.paused, true);
  assert.equal(i.actionLabel, "Resume shop");
  // The dial outranks the task mix here: with dispatch off, "some tasks
  // are blocked" is not the headline — nothing is going to move at all.
  assert.match(i.title, /PAUSED/);
});

// The whole point of the override state: "Run now" force-dispatches past
// the dial on purpose, so paused + running is a deliberate workflow, not a
// contradiction. The old dot ranked busy above paused and erased it.
test("projectRunIndicator: paused WITH work running is its own state", () => {
  const i = projectRunIndicator([task(), task({ id: "t2", ...RUNNING })], {
    paused: true,
    projectId: "shop",
  });
  assert.equal(i.state, "override");
  assert.equal(i.paused, true);
  assert.equal(i.liveCount, 1);
  assert.match(i.title, /still running/);
});

// Automation tasks answer to the SEPARATE automations dial, so one running
// under a paused task dial is ordinary scheduling. Counting it would flag
// an override on every project with a cron.
test("projectRunIndicator: an automation task is not an override", () => {
  const i = projectRunIndicator(
    [task({ id: "a", ...RUNNING, generated_by: "automation:nightly" })],
    { paused: true, projectId: "shop" },
  );
  assert.equal(i.state, "paused");
  assert.equal(i.liveCount, 0);
});

// `in_progress` sticks when a runner dies mid-task — that is exactly what
// the server's is_abandoned enrichment exists to flag. Counting it would
// paint a dead project amber forever and claim an override that is not
// happening.
test("projectRunIndicator: an abandoned task is not running", () => {
  const i = projectRunIndicator(
    [task({ id: "z", ...RUNNING, is_abandoned: true })],
    { paused: true, projectId: "shop" },
  );
  assert.equal(i.state, "paused");
  assert.equal(i.liveCount, 0);
  assert.equal(isTaskExecuting(task({ ...RUNNING, is_abandoned: true })), false);
  assert.equal(isTaskExecuting(task({ ...RUNNING })), true);
});

// The automation carve-out belongs to the OVERRIDE question only. Folding
// it into the colour too made an unpaused project with a running cron
// report "ready" in static green while the same card header said "1 active".
test("projectRunIndicator: a running automation still paints an unpaused project busy", () => {
  const i = projectRunIndicator(
    [task({ id: "a", ...RUNNING, generated_by: "automation:nightly" })],
    { paused: false, projectId: "shop" },
  );
  assert.equal(i.state, "busy");
  assert.match(i.title, /1 task running/);
  // …but it is not work THIS dial governs, so the override count stays 0.
  assert.equal(i.liveCount, 0);
});

test("projectRunIndicator: the busy count includes automations, the override count does not", () => {
  const i = projectRunIndicator(
    [
      task({ id: "a", ...RUNNING, generated_by: "automation:nightly" }),
      task({ id: "m", ...RUNNING }),
    ],
    { paused: false, projectId: "shop" },
  );
  assert.equal(i.state, "busy");
  assert.match(i.title, /2 tasks running/);
  assert.equal(i.liveCount, 1);
});

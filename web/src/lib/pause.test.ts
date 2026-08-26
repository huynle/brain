import test from "node:test";
import assert from "node:assert/strict";

import {
  EMPTY_PAUSE_STATE,
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

const status = (over: Partial<RunnerStatusResponse> = {}): RunnerStatusResponse => ({
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
    [projectPauseBadges(s, "alpha").tasks, projectPauseBadges(s, "alpha").automations],
    [true, false],
  );
  assert.deepEqual(
    [projectPauseBadges(s, "beta").tasks, projectPauseBadges(s, "beta").automations],
    [false, true],
  );
  const none = projectPauseBadges(s, "gamma");
  assert.equal(none.tasks, false);
  assert.equal(none.automations, false);
});

// ─── isAutomationTask / isReadyUndispatched ──────────────────────

test("isAutomationTask matches the server's generated_by prefix test", () => {
  assert.equal(isAutomationTask(task({ generated_by: "automation:goal-x" })), true);
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
  const r = taskHoldReason(task(), { pause: pausedProject, projectId: "alpha" });
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
  const s = buildPauseState(
    status({ automationPausedProjects: ["alpha"] }),
    [runner()],
  );
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
    task({ last_placement_reason: { decision: "no_candidate", reason: "stale" } }),
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
      buildPauseState(status({ automationPausedProjects: ["alpha"] }), [runner()]),
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
  const s = buildPauseState(
    status({ automationPausedProjects: ["alpha"] }),
    [runner()],
  );
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
  const s = buildPauseState(
    status({ pausedProjects: ["alpha"] }),
    [runner({ paused: true })],
  );
  const r = taskHoldReason(task(), { pause: s, projectId: "alpha" });
  assert.equal(r!.code, "project_paused");
  assert.match(r!.detail, /TASK dial/);
  assert.match(r!.detail, /every registered runner/i);
  assert.match(r!.detail, /resume a runner too/i);
});

test("the fleet note is absent when runners can still take work", () => {
  const r = taskHoldReason(task(), { pause: pausedProject, projectId: "alpha" });
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

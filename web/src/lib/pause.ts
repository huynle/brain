/**
 * Pause visibility — the three independent dials, and why a ready task sits.
 *
 * Brain has THREE separate pause switches. They compose; any subset can be on
 * at once, and resuming one does not resume the others:
 *
 *   1. project-tasks   POST /tasks/runner/pause/{project}
 *                      Holds NON-automation tasks in that project.
 *   2. project-autos   POST /tasks/runner/automations/pause/{project}
 *                      Holds ONLY automation-generated tasks in that project.
 *   3. runner          PUT  /runners/{runnerId}/pause
 *                      Removes that runner from placement entirely.
 *
 * The two project dials are a strict carve-out of each other, not a hierarchy
 * — see service.SchedulerService.shouldSkipTask, which routes a task to
 * exactly one of them based on `generated_by: automation:*`. Pausing manual
 * work does not stop automations, and vice versa. Any UI that collapses these
 * into one "paused" light is lying about which switch to flip.
 *
 * The dials also differ in how force interacts with them:
 *   - The project dials are DELIBERATELY bypassed by "Run now" / "Run feature
 *     now" (SchedulerService.RunTaskNow skips shouldSkipTask outright) — a
 *     manual override is the point of a manual override.
 *   - Runner pause has NO override. runnerEligibleForTask rejects a paused
 *     runner before force is ever consulted, so a force-run against an
 *     all-paused fleet places nothing.
 *
 * Everything here is pure so it can be unit-tested without fetch or React.
 * Data comes from two endpoints the PWA already has clients for:
 * getRunnerStatus() (project scope) and getRunners() (runner scope).
 */
import type {
  PlacementReason,
  RunnerInfo,
  RunnerStatusResponse,
  SchedulerResult,
  Task,
} from "./types";

// ─── State ───────────────────────────────────────────────────────

/** Normalized snapshot of all three dials. */
export interface PauseState {
  /** Projects whose task dial is off (holds non-automation tasks). */
  projectTasks: ReadonlySet<string>;
  /** Projects whose automations dial is off (holds automation tasks). */
  projectAutomations: ReadonlySet<string>;
  /** Features whose dial is off, keyed `${projectId}/${featureId}`. The
   *  fourth dial: it holds ONE feature while the rest of its project keeps
   *  running, which is the only shape that can stop a feature someone
   *  started by hand — "Run feature now" force-dispatches past the project
   *  dial on purpose. */
  features: ReadonlySet<string>;
  /** Runner ids whose runner-scoped dial is off. */
  runners: ReadonlySet<string>;
  /** Total runners known, so "2 paused" can be read against a denominator. */
  runnerCount: number;
  /** Online runners that are NOT paused — the fleet that can actually take
   *  a dispatch right now. Zero means nothing places, force or not. */
  availableRunnerCount: number;
}

const EMPTY_SET: ReadonlySet<string> = new Set();

export const EMPTY_PAUSE_STATE: PauseState = {
  projectTasks: EMPTY_SET,
  projectAutomations: EMPTY_SET,
  features: EMPTY_SET,
  runners: EMPTY_SET,
  runnerCount: 0,
  availableRunnerCount: 0,
};

/**
 * Fold the two API responses into one PauseState.
 *
 * `status` may be undefined while its query is in flight — treat that as "no
 * project dial known", never as "not paused", so callers can distinguish an
 * unloaded state from a verified-running one if they care.
 */
export function buildPauseState(
  status: RunnerStatusResponse | undefined,
  runners: readonly RunnerInfo[],
): PauseState {
  // Go marshals nil slices as JSON null, not [] — hence the ?? [].
  const projectTasks = new Set(status?.pausedProjects ?? []);
  const projectAutomations = new Set(status?.automationPausedProjects ?? []);
  const features = new Set(status?.pausedFeatures ?? []);
  const pausedRunners = new Set(
    runners.filter((r) => r.paused).map((r) => r.runner_id),
  );
  const available = runners.filter(
    (r) => r.status === "online" && !r.paused,
  ).length;
  return {
    projectTasks,
    projectAutomations,
    features,
    runners: pausedRunners,
    runnerCount: runners.length,
    availableRunnerCount: available,
  };
}

export const isProjectTasksPaused = (s: PauseState, projectId: string) =>
  s.projectTasks.has(projectId);

export const isProjectAutomationsPaused = (s: PauseState, projectId: string) =>
  s.projectAutomations.has(projectId);

/** Key for the feature dial. Project-qualified because feature ids are only
 *  unique within a project. */
export const featurePauseKey = (projectId: string, featureId: string) =>
  `${projectId}/${featureId}`;

export const isFeaturePaused = (
  s: PauseState,
  projectId: string,
  featureId: string,
) => s.features.has(featurePauseKey(projectId, featureId));

export const isRunnerPaused = (s: PauseState, runnerId: string) =>
  s.runners.has(runnerId);

/** True when at least one runner exists and every one of them is paused. */
export const allRunnersPaused = (s: PauseState) =>
  s.runnerCount > 0 && s.runners.size === s.runnerCount;

/** True when any dial anywhere is off. */
export const anyPaused = (s: PauseState) =>
  s.projectTasks.size > 0 ||
  s.projectAutomations.size > 0 ||
  s.features.size > 0 ||
  s.runners.size > 0;

// ─── Runner row ──────────────────────────────────────────────────

/** Dot state for a runner row. `paused` is its own state precisely because a
 *  paused runner keeps reporting status "online" — deriving the dot from
 *  status alone paints it the same green as a runner doing work. */
export type RunnerDotState = "on" | "busy" | "paused" | "err" | "";

export function runnerDotState(runner: RunnerInfo): RunnerDotState {
  // An offline or stale runner is a bigger problem than a paused one, and
  // pause on a runner that is already gone is not the operator's next move.
  if (runner.status === "stale") return "err";
  if (runner.status !== "online") return "";
  if (runner.paused) return "paused";
  return "on";
}

/** Tooltip for a runner row's dot. */
export function runnerDotTitle(runner: RunnerInfo): string {
  if (runner.status === "stale") return "Runner stale — heartbeat overdue";
  if (runner.status !== "online") return "Runner offline";
  if (runner.paused) {
    return (
      "Runner PAUSED — still online and heartbeating, but the scheduler " +
      "will not place any dispatch on it. Runner pause has no force " +
      "override: Run now routes around it rather than through it."
    );
  }
  return "Runner online";
}

// ─── Global summary (statusbar) ──────────────────────────────────

export interface PauseSummary {
  /** Compact statusbar text. */
  text: string;
  /** `halt` = nothing can dispatch anywhere; `partial` = some work flows. */
  tone: "halt" | "partial";
  /** Long-form tooltip naming every dial that is off. */
  title: string;
}

const plural = (n: number, word: string) => `${n} ${word}${n === 1 ? "" : "s"}`;

/**
 * One-line "something is paused" indicator for the global statusbar.
 * Returns null when every dial is on — the statusbar stays quiet in the
 * normal case, and any text at all means work is being held.
 *
 * `projectCount` (total projects known) is optional; when given it upgrades
 * "3 projects" to "all projects", which is a materially different fact.
 */
export function pauseSummary(
  state: PauseState,
  opts: { projectCount?: number } = {},
): PauseSummary | null {
  if (!anyPaused(state)) return null;

  const { projectCount } = opts;
  const parts: string[] = [];
  const lines: string[] = [];

  if (state.projectTasks.size > 0) {
    const all =
      projectCount !== undefined &&
      projectCount > 0 &&
      state.projectTasks.size >= projectCount;
    parts.push(
      all ? "all projects" : plural(state.projectTasks.size, "project"),
    );
    lines.push(
      `Project tasks paused: ${[...state.projectTasks].sort().join(", ")}`,
    );
  }

  if (state.projectAutomations.size > 0) {
    parts.push(
      state.projectAutomations.size === 1
        ? "automations"
        : `automations ×${state.projectAutomations.size}`,
    );
    lines.push(
      `Project automations paused: ${[...state.projectAutomations].sort().join(", ")}`,
    );
  }

  if (state.runners.size > 0) {
    const all = allRunnersPaused(state);
    parts.push(
      all
        ? state.runnerCount === 1
          ? "the runner"
          : `all ${state.runnerCount} runners`
        : plural(state.runners.size, "runner"),
    );
    lines.push(`Runners paused: ${[...state.runners].sort().join(", ")}`);
  }

  // The one unambiguous global halt: no runner will accept anything, so no
  // dial on any project can produce a dispatch, and force cannot help.
  const halt = allRunnersPaused(state);
  if (halt) {
    lines.push(
      "",
      "Every runner is paused — nothing will dispatch anywhere. " +
        "Run now cannot override runner pause.",
    );
  } else if (state.availableRunnerCount === 0 && state.runnerCount > 0) {
    lines.push("", "No online, unpaused runner is available right now.");
  }

  lines.push(
    "",
    "These are independent dials — resuming one does not resume the others.",
  );

  return {
    text: `paused: ${parts.join(" · ")}`,
    tone: halt ? "halt" : "partial",
    title: lines.join("\n"),
  };
}

// ─── Per-project badge ───────────────────────────────────────────

export interface ProjectPauseBadges {
  /** The project task dial is off. */
  tasks: boolean;
  /** The project automations dial is off. */
  automations: boolean;
  /** Tooltip for the tasks badge. */
  tasksTitle: string;
  /** Tooltip for the automations badge. */
  automationsTitle: string;
}

export function projectPauseBadges(
  state: PauseState,
  projectId: string,
): ProjectPauseBadges {
  return {
    tasks: isProjectTasksPaused(state, projectId),
    automations: isProjectAutomationsPaused(state, projectId),
    tasksTitle:
      "Task dispatch PAUSED for this project. Ready tasks will not be " +
      "picked up. Automation-generated tasks are governed by the separate " +
      "automations dial and are NOT held by this one.",
    automationsTitle:
      "Automations PAUSED for this project. Automation-generated tasks will " +
      "not be picked up. Manually authored tasks are governed by the " +
      "separate task dial and are NOT held by this one.",
  };
}

// ─── Project run indicator (the pause/play control) ──────────────

/**
 * State of the one control that both shows whether a project's task dial
 * is on and flips it.
 *
 * It replaces a plain status dot, so it has to keep saying everything the
 * dot said — busy / blocked / idle / empty — while adding the dial. The
 * two are carried on different channels: the GLYPH is the dial (⏸ vs ▶),
 * the COLOUR is the work. That is why `paused` is its own field rather
 * than just another `state` value.
 */
export type ProjectRunState =
  // Reuses the existing dot palette so the CSS keeps working.
  | "on" // tasks present, none blocked, nothing executing
  | "busy" // a runner is executing something
  | "err" // blocked tasks and nothing executing
  | "" // no tasks at all
  | "paused" // dial off, nothing executing
  | "override"; // dial off, yet work is executing anyway

export interface ProjectRunIndicator {
  state: ProjectRunState;
  /** The task dial is off. Drives the glyph, not the colour. */
  paused: boolean;
  /** Tasks a runner is executing right now (see {@link isTaskExecuting}),
   *  EXCLUDING automations — the count the override state is about. Use
   *  it for "running despite the dial", not for "is anything running":
   *  {@link ProjectRunIndicator.state} `busy` answers that one. */
  liveCount: number;
  /** Describes the CURRENT state — what the old dot's tooltip said, plus
   *  the dial and the override case. */
  title: string;
  /** What a click does. Used as the aria-label so a screen reader is told
   *  the action, while `title` carries the state. */
  actionLabel: string;
}

/**
 * Is a runner actually executing this task right now?
 *
 * `status === "in_progress"` alone over-reports: the status stays stuck
 * there when a runner dies mid-task, which is exactly what the server's
 * `is_abandoned` enrichment exists to flag (see the Abandonment + Resume
 * model in CLAUDE.md). An abandoned task is not work in flight — counting
 * it would paint a dead project amber forever.
 */
export const isTaskExecuting = (task: Task): boolean =>
  task.status === "in_progress" && !task.is_abandoned;

/**
 * Fold a project's tasks and its task dial into one indicator.
 *
 * ─── Why `override` exists ───────────────────────────────────────
 *
 * The task dial is DELIBERATELY bypassed by "Run now" / "Run feature now"
 * (SchedulerService.RunTaskNow skips shouldSkipTask outright), so "paused"
 * and "something is running" are not contradictory — pausing the project
 * and then hand-running one feature is a normal, deliberate workflow. The
 * old dot collapsed that: `busy` outranked `paused`, so a project the user
 * had deliberately isolated looked identical to one running freely.
 *
 * AUTOMATION tasks are excluded from the live count on purpose. They answer
 * to the SEPARATE automations dial (`shouldSkipTask` routes a task to
 * exactly one of the two on the `automation:` prefix), so an automation
 * running while the task dial is off is ordinary scheduling, not an
 * override, and flagging it would cry wolf on every project with a cron.
 *
 * The title stops short of claiming the run was manual. A task dispatched
 * just before the dial was flipped, and a server-side dependent-chain
 * drain, both land here too — what is certain is that the dial is off and
 * work is moving anyway, so that is what it says.
 */
export function projectRunIndicator(
  tasks: readonly Task[],
  opts: { paused: boolean; projectId: string },
): ProjectRunIndicator {
  const { paused, projectId } = opts;

  // TWO counters, because the automation carve-out applies to exactly one
  // of the two questions this function answers.
  //
  //   executing  is anything running at all → the COLOUR
  //   liveCount  is anything running that THIS dial governs → the override
  //
  // Folding them into one counter made an unpaused project with a running
  // cron report "ready" in static green while the very same card header
  // said "1 active" and healthFor said "active".
  let executing = 0;
  let liveCount = 0;
  let hasBlocked = false;
  for (const t of tasks) {
    if (isTaskExecuting(t)) {
      executing++;
      if (!isAutomationTask(t)) liveCount++;
    } else if (t.status === "blocked") {
      hasBlocked = true;
    }
  }

  const actionLabel = paused ? `Resume ${projectId}` : `Pause ${projectId}`;

  if (paused) {
    if (liveCount > 0) {
      return {
        state: "override",
        paused: true,
        liveCount,
        title:
          `${projectId} — PAUSED, but ${liveCount} task` +
          `${liveCount === 1 ? " is" : "s are"} still running. Pause holds ` +
          `NEW dispatch only; "Run now" / "Run feature now" force past the ` +
          `dial on purpose, and work already in flight runs to completion. ` +
          `Click to resume normal dispatch.`,
        actionLabel,
      };
    }
    return {
      state: "paused",
      paused: true,
      liveCount: 0,
      title:
        `${projectId} — PAUSED. Ready tasks will not be picked up. ` +
        `Automation-generated tasks answer to the separate automations ` +
        `dial and are NOT held by this one. Click to resume.`,
      actionLabel,
    };
  }

  const state: ProjectRunState =
    executing > 0
      ? "busy"
      : tasks.length === 0
        ? ""
        : hasBlocked
          ? "err"
          : "on";

  const what =
    executing > 0
      ? `${executing} task${executing === 1 ? "" : "s"} running`
      : tasks.length === 0
        ? "no tasks"
        : hasBlocked
          ? "blocked tasks, nothing running"
          : "ready";

  return {
    state,
    paused: false,
    liveCount,
    title: `${projectId} — ${what}. Click to pause new task dispatch.`,
    actionLabel,
  };
}

// ─── Per-task hold reason ────────────────────────────────────────

export type HoldCode =
  | "project_paused"
  | "automations_paused"
  | "runners_paused"
  | "no_candidate"
  | "no_runners"
  | "waiting_on_features"
  | "feature_blocked"
  | "feature_cycle"
  | "feature_dep_unresolved";

export interface HoldReason {
  code: HoldCode;
  /** Leading marker. ⏸ means a dial someone deliberately flipped; ⚠ means
   *  nobody chose this and it will not clear on its own. Using ⏸ for both
   *  would send a user looking for a switch that does not exist.
   *
   *  ⇢ is the third case: an ordering constraint that IS clearing itself —
   *  upstream work is in flight. There is no switch to flip and nothing is
   *  wrong, so it must read as neither a dial nor a fault. */
  glyph: "⏸" | "⚠" | "⇢";
  /** Compact chip text for a dense task row. */
  short: string;
  /** Full sentence for a tooltip or the task modal. */
  detail: string;
}

/** Automation-generated tasks answer to the automations dial only. Mirrors
 *  the `strings.HasPrefix(task.GeneratedBy, "automation:")` test the
 *  scheduler uses to route a task to one dial or the other. */
export const isAutomationTask = (task: Task): boolean =>
  (task.generated_by ?? "").startsWith("automation:");

/** True when a task is eligible to dispatch right now and simply isn't —
 *  the only case where "why is this held?" has a non-obvious answer.
 *  Dependency waits, blocks and terminal statuses explain themselves. */
export function isReadyUndispatched(task: Task): boolean {
  if (task.status !== "pending") return false;
  // The server classifies every pending task; anything but "ready" is a
  // dependency story the tree already tells.
  if (task.classification !== undefined && task.classification !== "ready")
    return false;
  // A live lease means it IS on its way to a runner, not held.
  const leaseState = task.dispatch_lease?.state;
  if (leaseState === "pushed" || leaseState === "acked") return false;
  return true;
}

/**
 * Why this ready task has not been dispatched, or null when there is no
 * evidence it is being held.
 *
 * Precedence deliberately mirrors the server's own order of checks, so the
 * answer names the switch that is actually holding this task:
 *
 *   1. The one project dial that governs THIS task (shouldSkipTask routes to
 *      exactly one based on generated_by).
 *   2. Fleet-wide runner pause, which no project dial and no force can undo.
 *   3. The task's own recorded placement reason from the last pass.
 *   4. No runners registered at all.
 *
 * Project-level scheduler counts are intentionally NOT used here: a
 * `skipped_no_candidate: 1` on a five-task project cannot be attributed to
 * any particular row. Those belong on the project card — see
 * schedulerHoldNote.
 */
export function taskHoldReason(
  task: Task,
  ctx: { pause: PauseState; projectId: string },
): HoldReason | null {
  // Feature gating is answered first, and deliberately ahead of the
  // ready-only guard below.
  //
  // isReadyUndispatched skips anything not classified "ready" on the grounds
  // that "the tree already tells that story". That holds for task-level
  // depends_on, where the blocking task is a visible row. It does NOT hold
  // for feature_depends_on: the gate names a FEATURE, the task tree has no
  // feature row to point at, and the task simply sits at "waiting" forever
  // with nothing on screen explaining why. That is the same invisible-hold
  // failure this module exists to remove, arriving through a different door.
  const featureHold = featureGateReason(task);
  if (featureHold) return featureHold;

  if (!isReadyUndispatched(task)) return null;
  const { pause, projectId } = ctx;

  // Dials compose. When a project dial AND the whole fleet are both off,
  // naming only one of them sends the user to flip a switch that will not
  // release the task — the "I resumed it and nothing happened" trap. The
  // second dial is appended to the detail rather than replacing it.
  const fleetPaused = allRunnersPaused(pause);
  const alsoFleet = fleetPaused
    ? `\nAlso: every registered runner (${pause.runnerCount}) is paused, so ` +
      "resuming the project alone will not release this — resume a runner too."
    : "";

  if (isAutomationTask(task)) {
    if (isProjectAutomationsPaused(pause, projectId)) {
      return {
        code: "automations_paused",
        glyph: "⏸",
        short: "automations paused",
        detail:
          "Held by the project AUTOMATIONS dial. This task is " +
          "automation-generated, so the separate project task dial does not " +
          "govern it — resume automations for this project to release it." +
          alsoFleet,
      };
    }
  } else if (isProjectTasksPaused(pause, projectId)) {
    return {
      code: "project_paused",
      glyph: "⏸",
      short: "project paused",
      detail:
        "Held by the project TASK dial. Resume the project to release it. " +
        "Run now dispatches it anyway — that override is deliberate." +
        alsoFleet,
    };
  }

  if (fleetPaused) {
    return {
      code: "runners_paused",
      glyph: "⏸",
      short: "runners paused",
      detail:
        `Every registered runner (${pause.runnerCount}) is paused, so there ` +
        "is no placement candidate. Runner pause has no force override — " +
        "Run now will not dispatch this either. Resume a runner.",
    };
  }

  const placement = task.last_placement_reason;
  if (placement && placement.decision && placement.decision !== "dispatched") {
    return {
      code: "no_candidate",
      glyph: "⚠",
      short: placementShort(placement),
      detail: placementDetail(placement),
    };
  }

  if (pause.runnerCount === 0) {
    return {
      code: "no_runners",
      glyph: "⚠",
      short: "no runners",
      detail:
        "No runner is registered with the API, so nothing can be placed. " +
        "Start a runner (brain run start <project>).",
    };
  }

  return null;
}

/**
 * Why a task is held by its FEATURE's dependencies, or null.
 *
 * Set by applyFeatureGating (internal/service/taskdeps.go), which only ever
 * downgrades a "ready" task — so when these fields are present they are the
 * whole reason the task is not running, and no task-level story competes.
 */
export function featureGateReason(task: Task): HoldReason | null {
  const blocked = task.blocked_by_features ?? [];
  const reason = task.blocked_by_reason;

  // Cycle is keyed on the REASON, not on blocked_by_features.
  //
  // classifyFeature reports a cycle through InCycle and leaves
  // BlockedByFeatures EMPTY — there is no single upstream feature to name
  // when the members block each other. Requiring a non-empty list here left
  // a genuine A<->B cycle rendering no chip at all: every task stuck
  // forever with nothing on screen saying why. Verified against the server,
  // not assumed.
  if (reason === "feature_circular_dependency") {
    return {
      code: "feature_cycle",
      glyph: "⚠",
      short: "feature cycle",
      detail:
        (blocked.length > 0
          ? `This task's feature is in a dependency cycle with ${listFeatures(blocked)}. `
          : "This task's feature is in a feature_depends_on cycle — its " +
            "dependencies lead back to itself. ") +
        "A cycle never resolves on its own, and Run now will not override " +
        "it: break the loop by editing feature_depends_on on one of the " +
        "features involved.",
    };
  }

  if (blocked.length > 0 || reason === "feature_dependency_blocked") {
    const who =
      blocked.length > 0 ? listFeatures(blocked) : "an upstream feature";
    return {
      code: "feature_blocked",
      glyph: "⚠",
      short: "feature blocked",
      detail:
        `Held because ${who} ${blocked.length === 1 || blocked.length === 0 ? "is" : "are"} blocked. ` +
        "This will not clear until that is unblocked — Run now will not " +
        "override it either, because the gate is part of readiness, not a " +
        "pause dial.",
    };
  }

  const waiting = task.waiting_on_features ?? [];
  if (waiting.length > 0) {
    return {
      code: "waiting_on_features",
      glyph: "⇢",
      short: `waits on ${waiting.length === 1 ? waiting[0] : `${waiting.length} features`}`,
      detail:
        `Waiting for ${listFeatures(waiting)} to finish. Declared via ` +
        "feature_depends_on; it releases automatically once that feature " +
        "completes. Run now does NOT override it — the gate is part of " +
        'readiness, so forcing returns "task not ready".',
    };
  }

  return null;
}

/**
 * feature_depends_on entries naming a feature that does not exist.
 *
 * Orthogonal to holds: these gate NOTHING (applyFeatureGating reports them
 * but never acts on them), so the task may be running perfectly well. That is
 * exactly why it needs surfacing — a typo'd feature dep silently orders
 * nothing, and the only symptom is work running earlier than intended.
 */
export function featureDepWarning(task: Task): HoldReason | null {
  const unresolved = task.unresolved_feature_deps ?? [];
  if (unresolved.length === 0) return null;
  return {
    // Deliberately NOT feature_blocked: nothing is blocked. The task may be
    // running. What is wrong is the ordering the author thought they wrote.
    code: "feature_dep_unresolved",
    glyph: "⚠",
    short: "unknown feature dep",
    detail:
      `feature_depends_on names ${listFeatures(unresolved)}, which ` +
      `${unresolved.length === 1 ? "matches" : "match"} no feature in this ` +
      "project. Unresolved entries gate NOTHING — this task is ordered as " +
      "if the dependency were not declared. Check for a typo.",
  };
}

function listFeatures(ids: string[]): string {
  if (ids.length === 1) return `"${ids[0]}"`;
  if (ids.length === 2) return `"${ids[0]}" and "${ids[1]}"`;
  return (
    ids
      .map((f) => `"${f}"`)
      .slice(0, -1)
      .join(", ") + `, and "${ids[ids.length - 1]}"`
  );
}

function placementShort(reason: PlacementReason): string {
  return reason.decision === "no_candidate" ? "no runner" : reason.decision!;
}

function placementDetail(reason: PlacementReason): string {
  const why = reason.reason?.trim();
  const head =
    reason.decision === "no_candidate"
      ? "No eligible runner accepted this task on the last scheduler pass."
      : `Scheduler decision: ${reason.decision}.`;
  return why ? `${head}\n${why}` : head;
}

// ─── Per-project scheduler note ──────────────────────────────────

export interface SchedulerHoldNote {
  /** ⏸ when at least one task was held by a pause dial; ⚠ when the only
   *  cause was "no eligible runner", which nobody chose and which no
   *  resume will fix. */
  glyph: "⏸" | "⚠";
  /** Matching tone for styling: `paused` (deliberate) vs `warn`. */
  tone: "paused" | "warn";
  /** Compact text, e.g. "1 held: project paused". */
  short: string;
  /** Long-form breakdown for a tooltip. */
  detail: string;
}

/**
 * Turn the last scheduler pass for a project into a plain-language note.
 * Returns null when the pass held nothing back — a project where everything
 * dispatched needs no explanation.
 *
 * `skipped_already_leased` is excluded from the "held" count on purpose: that
 * work is in flight, not stuck, and counting it would make a perfectly
 * healthy project look throttled.
 */
export function schedulerHoldNote(
  result: SchedulerResult | undefined,
): SchedulerHoldNote | null {
  if (!result) return null;
  const tasksPaused = result.skipped_tasks_paused ?? 0;
  const autosPaused = result.skipped_automations_paused ?? 0;
  const noCandidate = result.skipped_no_candidate ?? 0;
  const leased = result.skipped_already_leased ?? 0;
  const held = tasksPaused + autosPaused + noCandidate;
  if (held === 0) return null;

  const causes: string[] = [];
  if (tasksPaused > 0) causes.push(`${tasksPaused} project paused`);
  if (autosPaused > 0) causes.push(`${autosPaused} automations paused`);
  if (noCandidate > 0) causes.push(`${noCandidate} no runner`);

  const lines = [
    `Last scheduler pass: ${result.considered} considered, ` +
      `${result.dispatched} dispatched, ${result.skipped} skipped.`,
  ];
  if (tasksPaused > 0)
    lines.push(
      `· ${tasksPaused} held by the project TASK dial (non-automation tasks).`,
    );
  if (autosPaused > 0)
    lines.push(
      `· ${autosPaused} held by the project AUTOMATIONS dial (automation tasks).`,
    );
  if (noCandidate > 0) lines.push(`· ${noCandidate} had no eligible runner.`);
  if (leased > 0)
    lines.push(`· ${leased} already leased and in flight (not held).`);

  const deliberate = tasksPaused > 0 || autosPaused > 0;
  return {
    glyph: deliberate ? "⏸" : "⚠",
    tone: deliberate ? "paused" : "warn",
    short: `${held} held · ${causes.join(" · ")}`,
    detail: lines.join("\n"),
  };
}

// ─── Force-dispatch cue ──────────────────────────────────────────

/**
 * Note to append to a manual run's toast so a force-dispatch against a paused
 * system does not read as "pause was silently ignored".
 *
 * The two halves are genuinely different guarantees, so they get different
 * copy: bypassing a project dial is intended behavior and worth naming, while
 * runner pause is a wall the override cannot cross.
 *
 * Kept free of em-dashes: the server's own reason strings already use them,
 * and appending a second dashed clause produced unreadable pileups like
 * "no eligible runner (r1: runner paused) — every runner is paused — runner
 * pause has no override". Callers join with FORCE_NOTE_SEP.
 */
export const FORCE_NOTE_SEP = " · ";

export function forceDispatchNote(
  state: PauseState,
  opts: { projectId: string; automation?: boolean },
): string | null {
  if (allRunnersPaused(state)) {
    return "runner pause has no override, so nothing could be placed";
  }
  const dialPaused = opts.automation
    ? isProjectAutomationsPaused(state, opts.projectId)
    : isProjectTasksPaused(state, opts.projectId);
  if (dialPaused) {
    return opts.automation
      ? "pause bypassed: this project's automations dial is off"
      : "pause bypassed: this project's task dial is off";
  }
  if (state.runners.size > 0 && state.availableRunnerCount === 0) {
    return "no online, unpaused runner is available";
  }
  return null;
}

/** Join a run result summary with its pause note, when there is one. */
export function withForceNote(message: string, note: string | null): string {
  return note ? `${message}${FORCE_NOTE_SEP}${note}` : message;
}

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
  const pausedRunners = new Set(
    runners.filter((r) => r.paused).map((r) => r.runner_id),
  );
  const available = runners.filter(
    (r) => r.status === "online" && !r.paused,
  ).length;
  return {
    projectTasks,
    projectAutomations,
    runners: pausedRunners,
    runnerCount: runners.length,
    availableRunnerCount: available,
  };
}

export const isProjectTasksPaused = (s: PauseState, projectId: string) =>
  s.projectTasks.has(projectId);

export const isProjectAutomationsPaused = (s: PauseState, projectId: string) =>
  s.projectAutomations.has(projectId);

export const isRunnerPaused = (s: PauseState, runnerId: string) =>
  s.runners.has(runnerId);

/** True when at least one runner exists and every one of them is paused. */
export const allRunnersPaused = (s: PauseState) =>
  s.runnerCount > 0 && s.runners.size === s.runnerCount;

/** True when any dial anywhere is off. */
export const anyPaused = (s: PauseState) =>
  s.projectTasks.size > 0 ||
  s.projectAutomations.size > 0 ||
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

// ─── Per-task hold reason ────────────────────────────────────────

export type HoldCode =
  | "project_paused"
  | "automations_paused"
  | "runners_paused"
  | "no_candidate"
  | "no_runners";

export interface HoldReason {
  code: HoldCode;
  /** Leading marker. ⏸ means a dial someone deliberately flipped; ⚠ means
   *  nobody chose this and it will not clear on its own. Using ⏸ for both
   *  would send a user looking for a switch that does not exist. */
  glyph: "⏸" | "⚠";
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
  if (noCandidate > 0)
    lines.push(`· ${noCandidate} had no eligible runner.`);
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

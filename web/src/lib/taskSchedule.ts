/**
 * The task row's answer to "does this recur, and is it still going?"
 *
 * A recurring task and a one-shot task render identically in the PWA today:
 * `schedule`, `expires_at`, `max_runs` and `next_run` all round-trip through
 * the API and none of them reach a pixel. The costly half of that is not the
 * missing cadence — it is that the runner can turn a schedule OFF on its own
 * and say so nowhere a user looks.
 *
 * `disableSchedule` (internal/runner/schedule.go) fires when `expires_at`
 * passes or `max_runs` is reached. It sets `schedule_enabled: false`, appends
 * a "## Schedule Disabled" note to the markdown body, and emits an event the
 * TUI consumes. In the PWA the task simply stops recurring, keeping whatever
 * status it last held — so a nightly job that quietly retired three weeks ago
 * is indistinguishable from one that is still running nightly. That is the
 * state this module exists to make visible, which is why every stopped
 * variant is styled as a fault rather than as information.
 *
 * Precedence below follows the runner's own order of checks in
 * `checkProjectScheduledTasks`, so the chip names the gate that is actually
 * in force rather than the first one that happens to be truthy.
 */
import { describeCron, nextCronRun } from "./cronSchedule";
import { relativeTime } from "./format";
import type { Task, TaskRun } from "./types";

export type ScheduleCode =
  /** Firing on a cron cadence. */
  | "recurring"
  /** A one-shot `run_once_at` that has not fired yet. */
  | "once"
  /** Valid, but `starts_at` has not arrived — it will begin on its own. */
  | "waiting"
  /** Not firing any more, and it will not resume without an edit. */
  | "stopped";

export interface ScheduleChip {
  code: ScheduleCode;
  /** ⟳ recurring cadence, ⌖ a single point in time. */
  glyph: "⟳" | "⌖";
  /** Compact chip text for a dense task row. */
  short: string;
  /** Full sentence for the tooltip. */
  detail: string;
}

/**
 * Runs that count against `max_runs`.
 *
 * Mirrors `countRuns` in internal/runner/schedule.go, which counts
 * in_progress alongside the settled outcomes. Leaving in_progress out here
 * would show "19/20" for a task the runner has already stopped.
 */
export function countScheduleRuns(runs?: TaskRun[]): number {
  if (!runs) return 0;
  return runs.filter(
    (r) =>
      r.status === "completed" ||
      r.status === "failed" ||
      r.status === "skipped" ||
      r.status === "in_progress",
  ).length;
}

/** Parses an RFC3339 stamp, returning null for anything unusable. */
function at(iso?: string): Date | null {
  if (!iso) return null;
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? null : d;
}

/**
 * The schedule chip for a task, or null when the task has no schedule at
 * all — which is the overwhelming majority of rows, and they must stay
 * exactly as they are.
 */
export function taskScheduleChip(
  task: Task,
  now: Date = new Date(),
): ScheduleChip | null {
  const cron = task.schedule || "";
  const once = task.run_once_at || "";
  // isScheduledTask in the runner: a schedule OR a run_once_at.
  if (!cron && !once) return null;

  const tz = task.timezone || "UTC";
  const cadence = cron ? describeCron(cron) : "";
  // The runner branches on `run_once_at != "" && schedule == ""`, so a task
  // carrying both is treated as recurring and its run_once_at is ignored.
  // Say so rather than describing a one-shot that will never happen.
  const bothNote =
    cron && once
      ? ` This task also sets run_once_at, which the runner ignores while a cron schedule is present.`
      : "";

  const runs = countScheduleRuns(task.runs);
  const cap = task.max_runs && task.max_runs > 0 ? task.max_runs : null;
  const runNote = cap ? ` ${runs}/${cap} runs used.` : "";

  // 1. The master switch. Checked first because the runner skips on it
  //    before evaluating any window, and because it is what the auto-disable
  //    actually writes — so this is the branch that catches a retired job.
  if (task.schedule_enabled === false) {
    const expired = at(task.expires_at);
    const exhausted = cap !== null && runs >= cap;
    // Name the likely cause. The runner records the real reason in a
    // "## Schedule Disabled" note in the body; these are the two conditions
    // that write it automatically, and either one alone is enough to say
    // more than "off".
    const cause =
      expired && expired <= now
        ? ` Its expires_at (${task.expires_at}) has passed, so the runner disabled it automatically.`
        : exhausted
          ? ` It reached its max_runs cap, so the runner disabled it automatically.`
          : ` It was disabled by hand or by the runner; the task body records the reason.`;
    return {
      code: "stopped",
      glyph: cron ? "⟳" : "⌖",
      short: "sched off",
      detail:
        `Schedule ${cadence || once} is DISABLED — this task is not recurring.${cause}` +
        `${runNote} Re-enable by setting schedule_enabled back to true.`,
    };
  }

  // 2. Expired window. schedule_enabled is still true here, because the
  //    runner only flips it the next time it polls — so between expiry and
  //    that poll the task looks live while being finished.
  const expires = at(task.expires_at);
  if (expires && expires <= now) {
    return {
      code: "stopped",
      glyph: cron ? "⟳" : "⌖",
      short: "expired",
      detail:
        `Schedule ${cadence || once} expired ${relativeTime(task.expires_at)} ` +
        `(expires_at ${task.expires_at}). It will be disabled the next time a ` +
        `runner polls this project.${runNote}`,
    };
  }

  // 3. Run budget spent — same "true until the next poll" caveat.
  if (cap !== null && runs >= cap) {
    return {
      code: "stopped",
      glyph: cron ? "⟳" : "⌖",
      short: `${runs}/${cap} runs`,
      detail:
        `Schedule ${cadence || once} has used its full run budget (${runs} of ${cap}). ` +
        `It will be disabled the next time a runner polls this project.`,
    };
  }

  // 4. Not open yet. Distinct from stopped: nobody needs to do anything,
  //    it starts on its own.
  const starts = at(task.starts_at);
  if (starts && starts > now) {
    return {
      code: "waiting",
      glyph: cron ? "⟳" : "⌖",
      short: `starts ${relativeTime(task.starts_at)}`,
      detail:
        `Schedule ${cadence || once} in ${tz} does not begin until ` +
        `${starts.toLocaleString()} (starts_at). Nothing to do — it opens on ` +
        `its own.${runNote}${bothNote}`,
    };
  }

  // 5. One-shot. Only when there is no cron, matching the runner's branch.
  if (!cron && once) {
    const when = at(once);
    if (!when) {
      return {
        code: "stopped",
        glyph: "⌖",
        short: "bad run_once_at",
        detail: `run_once_at (${once}) is not a valid RFC3339 timestamp, so this task will never fire.`,
      };
    }
    const due = when <= now;
    return {
      code: due ? "waiting" : "once",
      glyph: "⌖",
      short: due ? "due now" : `once ${relativeTime(once)}`,
      detail: due
        ? `One-shot task was due ${relativeTime(once)} (${when.toLocaleString()}) and fires on the next runner poll.`
        : `One-shot task, fires once at ${when.toLocaleString()}.`,
    };
  }

  // 6. Live recurring schedule.
  const next = nextCronRun(cron, tz, now);
  if (!next) {
    return {
      code: "stopped",
      glyph: "⟳",
      short: "bad schedule",
      detail:
        `Schedule "${cron}" is not a valid 5-field cron expression, or matches ` +
        `no date within the next year, so this task will never fire.`,
    };
  }
  // relativeTime floors to whole minutes, so the last seconds read "in 0m".
  const soon = next.getTime() - now.getTime() < 60_000;
  const rel = soon ? "soon" : relativeTime(next.toISOString());
  return {
    code: "recurring",
    glyph: "⟳",
    short: `${cadence} · ${rel}`,
    detail:
      `Recurring ${cadence} ("${cron}") in ${tz}. Next run ${next.toLocaleString()}.` +
      `${runNote}` +
      (task.expires_at
        ? ` Expires ${relativeTime(task.expires_at)} (${task.expires_at}).`
        : "") +
      ` Completing it does not end the schedule — the runner resets it to ` +
      `pending on the next match.${bothNote}`,
  };
}

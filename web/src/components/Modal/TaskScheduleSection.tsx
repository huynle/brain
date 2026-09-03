/**
 * TaskScheduleSection — the recurrence half of a task, and its run history.
 *
 * The task row grew a schedule chip so a recurring task stops looking like a
 * one-shot, but opening the task showed LESS than the row did: no cadence, no
 * next run, no window, no run history — while `runs[]` had been accumulating
 * a full execution log the whole time. This closes that inversion.
 *
 * The question it exists to answer is "this says completed — is it coming
 * back?". For a recurring task `completed` is not a terminal state, it is the
 * IDLE state: cronEligibleStatuses in internal/runner/schedule.go is
 * {active, completed, blocked}, and the runner flips a due task straight to
 * pending on its next poll. Nothing in the UI said so, which made a healthy
 * nightly job and a finished one indistinguishable.
 *
 * Two details drive the design:
 *
 *   1. "Next run" prefers the SERVER's next_run when it is set, because that
 *      is the value shouldTrigger actually compares against — not our
 *      prediction. But next_run is only written when a task first fires, so a
 *      freshly created schedule has none, and that is exactly when a user
 *      most wants to know it is armed. There we fall back to computing the
 *      occurrence from the expression and label it as predicted. Showing the
 *      computed value while the server holds a different stored one would be
 *      confidently wrong about what happens next.
 *
 *   2. Everything about state (armed / stopped / waiting) comes from
 *      taskScheduleChip, the same function the row uses. Two surfaces
 *      disagreeing about whether a job is retired is worse than either being
 *      absent.
 */
import {
  countScheduleRuns,
  isRunnerParseableTime,
  taskScheduleChip,
} from "../../lib/taskSchedule";
import { describeCron, nextCronRun } from "../../lib/cronSchedule";
import { relativeTime } from "../../lib/format";
import type { Task, TaskRun } from "../../lib/types";

/** Most recent runs to draw. runs[] is unbounded — see the render note. */
const RUN_HISTORY_LIMIT = 20;

/** Absolute local stamp, matching the Sessions/Dispatch row format. */
function fmtWhen(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(
    d.getHours(),
  )}:${pad(d.getMinutes())}`;
}

/** Absolute time plus its relative gloss, when both are meaningful. */
function whenWithRelative(iso?: string): string {
  const abs = fmtWhen(iso);
  if (!abs) return "";
  const rel = relativeTime(iso);
  return rel ? `${abs} (${rel})` : abs;
}

/** Status -> tone class. Colours live in CSS so both themes get their own. */
function runTone(status?: string): string {
  switch (status) {
    case "completed":
      return "ok";
    case "failed":
      return "err";
    case "in_progress":
      return "busy";
    default:
      return "muted";
  }
}

/**
 * Whether to tell the user this task will come back.
 *
 * Mirrors cronEligibleStatuses {active, completed, blocked}. `validated` was
 * once here and is NOT eligible — the runner skips it, so the banner promised
 * a firing that never comes. `blocked` IS eligible and was missing, so a
 * blocked recurring task looked stuck when the runner will re-arm it.
 *
 * Exported so tests pin THIS predicate rather than a copy of it: a test that
 * re-declares the rule can only confirm what the test itself wrote, which is
 * how the wrong `validated` behaviour got certified in the first place.
 */
export function showsRearmNotice(task: Task, now?: Date): boolean {
  // A feature_schedule gate completes by design and never resets to pending,
  // so it must never be told it is coming back. taskScheduleChip already
  // classifies gates as "once"/"done" rather than "recurring"; this is the
  // explicit belt to that braces.
  if (task.generated_kind === "feature_schedule") return false;
  const chip = taskScheduleChip(task, now);
  return (
    !!chip &&
    chip.code === "recurring" &&
    (task.status === "completed" || task.status === "blocked")
  );
}

export function TaskScheduleSection({ task }: { task: Task }): JSX.Element | null {
  const chip = taskScheduleChip(task);
  // No schedule and no run_once_at — the overwhelming majority of tasks.
  if (!chip) return null;

  const cron = task.schedule || "";
  const tz = task.timezone || "UTC";
  const runs = task.runs ?? [];
  const used = countScheduleRuns(runs);
  const cap = task.max_runs && task.max_runs > 0 ? task.max_runs : null;

  // Server value first — see the header note. Only predict when it is absent.
  const serverNext = task.next_run || "";
  const predicted = cron && !serverNext ? nextCronRun(cron, tz) : null;

  const rows: { k: string; v: React.ReactNode; title?: string }[] = [];

  rows.push({
    k: "Cadence",
    v: cron ? (
      // describeCron returns the expression verbatim for shapes it does not
      // model, so printing both unconditionally echoed it beside itself.
      describeCron(cron) === cron ? (
        <code>{cron}</code>
      ) : (
        <>
          {describeCron(cron)} <code style={{ opacity: 0.7 }}>{cron}</code>
        </>
      )
    ) : (
      "one-shot"
    ),
    title: cron ? `Cron expression evaluated in ${tz}` : undefined,
  });

  if (cron) rows.push({ k: "Timezone", v: tz });

  if (task.run_once_at) {
    // The runner's branch is `run_once_at != "" && schedule == ""`, so with a
    // cron present this value never fires. Showing it unqualified read as a
    // second scheduled firing.
    rows.push({
      k: "Run once at",
      v: (
        <>
          {isRunnerParseableTime(task.run_once_at) ? (
            whenWithRelative(task.run_once_at)
          ) : (
            <code>{task.run_once_at}</code>
          )}{" "}
          {cron ? (
            <span className="sched-run__when">
              · ignored (cron takes priority)
            </span>
          ) : isRunnerParseableTime(task.run_once_at) ? null : (
            <span className="sched-run__when">· unparseable, never fires</span>
          )}
        </>
      ),
    });
  }

  if (
    chip.code === "stopped" ||
    chip.code === "ineligible" ||
    chip.code === "done"
  ) {
    rows.push({
      k: "Next run",
      v: (
        <span className="sched-stopped-note">not scheduled — {chip.short}</span>
      ),
      title: chip.detail,
    });
  } else if (chip.code === "waiting" && task.starts_at) {
    // checkTimeWindow returns windowNotYet until starts_at passes, so any
    // cron occurrence before then is not a firing. Printing one contradicted
    // both the heading chip and the Starts row on this same grid.
    rows.push({
      k: "Next run",
      v: <>not before {whenWithRelative(task.starts_at)}</>,
      title:
        "The runner refuses to trigger this task until starts_at passes, so the cron expression's earlier occurrences do not fire.",
    });
  } else if (serverNext) {
    rows.push({
      k: "Next run",
      v: whenWithRelative(serverNext) || serverNext,
      title:
        "The runner's stored next_run — this is the value shouldTrigger compares against.",
    });
  } else if (predicted) {
    rows.push({
      k: "Next run",
      v: (
        <>
          {whenWithRelative(predicted.toISOString())}{" "}
          <span className="sched-run__when">· predicted</span>
        </>
      ),
      title:
        "Computed from the cron expression. The runner writes its own next_run only after the task first fires, so until then it matches on the live clock.",
    });
  }

  // checkTimeWindow tolerates an unparseable bound and treats it as unset, so
  // a malformed value is neither a window nor an error — but rendering the
  // empty string whenWithRelative returns left a labelled blank cell that
  // said nothing about either fact.
  const windowRow = (k: string, iso: string) => {
    // Ask the RUNNER'S parser, not `new Date()`. "2026-08-01" renders as a
    // perfectly good date here and is rejected by Go, so formatting-succeeded
    // is the wrong test — it showed a bound that has no effect as if it were
    // in force, contradicting the chip on the same screen.
    const pretty = isRunnerParseableTime(iso) ? whenWithRelative(iso) : "";
    rows.push({
      k,
      v: pretty ? (
        pretty
      ) : (
        <>
          <code>{iso}</code>{" "}
          <span className="sched-run__when">· unparseable, ignored</span>
        </>
      ),
      title: pretty
        ? undefined
        : "The runner cannot parse this as RFC3339, so checkTimeWindow treats it as unset and the bound has no effect.",
    });
  };
  if (task.starts_at) windowRow("Starts", task.starts_at);
  if (task.expires_at) windowRow("Expires", task.expires_at);
  if (cap) {
    rows.push({
      k: "Runs",
      v: `${used} / ${cap}`,
      title:
        "Counts completed, failed, skipped and in-progress runs — an in-flight run already spends its budget. The runner disables the schedule when the cap is reached.",
    });
  }

  // The point of the whole section: `completed` is the idle state for a
  // recurring task, not a terminal one.
  const rearms = showsRearmNotice(task);

  return (
    <>
      <h4 className="modal-content-heading">
        Schedule{" "}
        <span className={`sched-chip ${chip.code}`} title={chip.detail}>
          {chip.glyph} {chip.short}
        </span>
      </h4>

      {rearms && (
        <div className="sched-rearm">
          {task.status === "blocked" ? "Blocked" : "Completed"}, but still
          scheduled — the runner resets it to <code>pending</code> at the next
          occurrence.
        </div>
      )}

      <div className="kv-grid">
        {rows.map((r) => (
          <div key={r.k} style={{ display: "contents" }}>
            <div className="k" title={r.title}>
              {r.k}
            </div>
            <div className="v">{r.v}</div>
          </div>
        ))}
      </div>

      {runs.length > 0 && (
        <>
          <div className="sched-note">
            Run history ({runs.length})
            {runs.length > RUN_HISTORY_LIMIT
              ? ` · showing the newest ${RUN_HISTORY_LIMIT}`
              : ""}
          </div>
          <div className="kv-grid">
            {/* Newest first — runs[] is append-only in chronological order.
                Nothing in the runner trims it (buildRunsArray always appends),
                so a minutely task accumulates without bound; cap the render
                and SAY the cap rather than silently drawing thousands of rows. */}
            {[...runs]
              .reverse()
              .slice(0, RUN_HISTORY_LIMIT)
              .map((r, i) => (
                <ScheduleRunRow key={`${r.run_id ?? ""}:${i}`} run={r} />
              ))}
          </div>
        </>
      )}
    </>
  );
}

function ScheduleRunRow({ run }: { run: TaskRun }): JSX.Element {
  return (
    <div className="sched-run">
      <span className={`sched-run__status ${runTone(run.status)}`}>
        {run.status || "—"}
      </span>
      <span className="sched-run__id">
        {run.skip_reason || run.run_id || ""}
      </span>
      <span className="sched-run__when">
        {fmtWhen(run.started)}
        {run.completed ? ` → ${fmtWhen(run.completed)}` : ""}
      </span>
    </div>
  );
}

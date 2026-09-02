/**
 * Automation run history rows — shared by the automation modal's Runs
 * tab and the dockable Runs pane, so the two surfaces cannot drift.
 *
 * The point of this list is the LAST column: an automation that fired at
 * 03:00 is only interesting once you can read what its agent actually
 * did. The chain the row walks is
 *
 *   run audit → generated task id → the task in the live snapshot
 *             → its session (live instance, else newest recorded)
 *
 * and every hop can legitimately be missing — a skipped run generates no
 * task, a script-executor task opens no session, an old task may have
 * aged out of the snapshot. Each of those is stated in place (a disabled
 * button that says why) rather than rendered as a button that does
 * nothing: "no session" and "session you can't reach" are different
 * facts, and only the first is normal.
 *
 * WHERE things open is the host's decision, not this component's — the
 * modal is dismissed by any navigation and so goes full-page, while the
 * pane docks its drill-downs in the side panel so the run list survives
 * being used (opening a log into the pane's own slot replaced the list
 * you were working through). Same pattern as SessionsSection's `onView`.
 */
import { useMemo } from "react";

import { useLive } from "../../lib/sse";
import { useSessions } from "../../hooks/useSessions";
import { relativeTime } from "../../lib/format";
import { resolveRunTarget } from "../../lib/automationRunTarget";
import {
  durationLabel,
  outcomeGlyph,
  outcomeLabel,
  outcomeTone,
  runOutcome,
  runTime,
  triggerLabel,
  type AutomationRun,
} from "../../lib/automationRuns";
import type { SessionRef, Task } from "../../lib/types";

export interface AutomationRunRowsProps {
  runs: readonly AutomationRun[];
  projectId: string;
  /** Open a session for a run's task. Host decides where it lands. */
  onOpenSession: (task: Task, ref: SessionRef) => void;
  /** Open the run's task log. */
  onOpenLog: (task: Task) => void;
  /** Open the run's task itself. */
  onOpenTask: (task: Task) => void;
  /** Name for an automation id — shown only when supplied (the pane
   *  lists several automations; the modal is already about one). */
  automationName?: (automationId: string) => string | undefined;
  /** Row selection, for hosts that show a detail panel beside the list. */
  selectedRunId?: string;
  onSelect?: (run: AutomationRun) => void;
}

export function AutomationRunRows({
  runs,
  projectId,
  onOpenSession,
  onOpenLog,
  onOpenTask,
  automationName,
  selectedRunId,
  onSelect,
}: AutomationRunRowsProps): JSX.Element {
  const tasks = useLive((s) => s.projects[projectId]?.tasks);
  const { allInstances } = useSessions();

  // One index for the whole page: a project snapshot runs to thousands of
  // tasks and the list can hold hundreds of runs, so a find() per row is
  // a quadratic scan on every poll.
  const byId = useMemo(() => {
    const m = new Map<string, Task>();
    for (const t of tasks ?? []) m.set(t.id, t);
    return m;
  }, [tasks]);

  // Resolution lives in lib/automationRunTarget so the four outcomes —
  // no task, task gone, task without a session, session — are unit
  // tested; they depend on runner state that cannot be produced on
  // demand from a live store.
  const targetFor = (run: AutomationRun) =>
    resolveRunTarget(run, byId, allInstances, tasks !== undefined);

  return (
    <div className="arun-scope">
      <div className="arun-list">
        {runs.map((run) => {
          const outcome = runOutcome(run);
          const target = targetFor(run);
          const when = runTime(run);
          const dur = durationLabel(run);
          const name = automationName?.(run.automationId);

          return (
            <div
              key={run.id}
              className={`arun-row${name !== undefined ? " named" : ""}${
                selectedRunId === run.id ? " sel" : ""
              }`}
              onClick={() => onSelect?.(run)}
              title={outcomeLabel(run)}
            >
              <span className={`glyph ${outcomeTone(outcome)}`}>
                {outcomeGlyph(outcome)}
              </span>
              <span className="arun-when" title={when}>
                {relativeTime(when) || "—"}
              </span>
              {name !== undefined && (
                <span className="arun-name" title={name}>
                  {name}
                </span>
              )}
              <span className="arun-trigger" title={triggerLabel(run)}>
                {triggerLabel(run)}
              </span>
              <span className="arun-dur">{dur}</span>
              <span className="arun-task">
                {target.task ? (
                  <>
                    <span className={`arun-task-status ${target.task.status}`}>
                      {target.task.status}
                    </span>
                    <code>{target.task.id.slice(0, 8)}</code>
                  </>
                ) : (
                  <span className="arun-muted">{outcomeLabel(run)}</span>
                )}
              </span>
              <span className="arun-actions">
                <button
                  type="button"
                  disabled={!target.sref || !target.task}
                  title={
                    target.sref
                      ? `Open the session this run's agent ran in (${target.sref.mode})`
                      : target.reason
                  }
                  onClick={(e) => {
                    e.stopPropagation();
                    if (target.task && target.sref) {
                      onOpenSession(target.task, target.sref);
                    }
                  }}
                >
                  session
                </button>
                <button
                  type="button"
                  disabled={!target.task}
                  title={
                    target.task ? "Open this run's task log" : target.reason
                  }
                  onClick={(e) => {
                    e.stopPropagation();
                    if (target.task) onOpenLog(target.task);
                  }}
                >
                  log
                </button>
                <button
                  type="button"
                  disabled={!target.task}
                  title={target.task ? "Open the task" : target.reason}
                  onClick={(e) => {
                    e.stopPropagation();
                    if (target.task) onOpenTask(target.task);
                  }}
                >
                  task
                </button>
              </span>
            </div>
          );
        })}
      </div>
    </div>
  );
}

/**
 * The audit fields a row has no room for: the dedup key that explains a
 * skip, the source event, the trigger payload, the full error text.
 * Rendered by hosts that have space for a detail area.
 */
export function AutomationRunDetail({
  run,
}: {
  run: AutomationRun;
}): JSX.Element {
  const rows: Array<[string, string]> = [
    ["Run id", run.id],
    ["Outcome", outcomeLabel(run)],
    ["Trigger", triggerLabel(run)],
    ["Started", run.startedAt || run.created],
  ];
  if (run.completedAt && run.completedAt !== run.startedAt) {
    rows.push(["Completed", run.completedAt]);
  }
  if (durationLabel(run)) rows.push(["Duration", durationLabel(run)]);
  if (run.sourceEventId) rows.push(["Source event", run.sourceEventId]);
  // The dedup key is the whole explanation of a cron run that "did
  // nothing": it names the minute bucket the run was collapsed into.
  if (run.dedupKey) rows.push(["Dedup key", run.dedupKey]);
  if (run.entryStatus) rows.push(["Entry status", run.entryStatus]);
  if (run.taskIds.length > 0) rows.push(["Tasks", run.taskIds.join(", ")]);

  return (
    <div className="arun-detail">
      <div className="kv-grid">
        {rows.map(([k, v]) => (
          <div key={k} style={{ display: "contents" }}>
            <div className="k">{k}</div>
            <div className="v">{v}</div>
          </div>
        ))}
      </div>
      {run.error && <div className="arun-error">{run.error}</div>}
      {run.payload.length > 0 && (
        <>
          <h4 className="modal-content-heading">Trigger payload</h4>
          <div className="kv-grid">
            {run.payload.map((p) => (
              <div key={p.key} style={{ display: "contents" }}>
                <div className="k">{p.key}</div>
                <div className="v">{p.value}</div>
              </div>
            ))}
          </div>
        </>
      )}
    </div>
  );
}

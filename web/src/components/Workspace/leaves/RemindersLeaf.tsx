/**
 * RemindersLeaf — the reminder centre: everything, not just what is waiting.
 *
 * The topbar bell answers "what needs me right now" and deliberately shows
 * only unacknowledged firings. This pane answers the other three questions:
 * what is scheduled, what has already happened, and what agents were sent off
 * to do. Without it a fired reminder that someone dismissed leaves no visible
 * trace, and a reminder whose action created a task gives no way to find that
 * task later.
 *
 * Grouped by state rather than filtered to one, because the useful reading is
 * comparative — "three waiting, nine scheduled, and here is the month of
 * history behind them".
 */
import { useMemo, useState } from "react";

import { useReminders, snoozeUntil } from "../../../hooks/useReminders";
import { useWorkspace } from "../../../store/workspace";
import { useUI } from "../../../store/ui";
import { Loading } from "../../common/Loading";
import { ErrorState } from "../../common/ErrorState";
import type { ReminderSummary } from "../../../lib/types";

type Group = "waiting" | "scheduled" | "agent" | "undated" | "history";

const GROUP_LABEL: Record<Group, string> = {
  waiting: "Waiting on you",
  scheduled: "Scheduled",
  agent: "Agent runs",
  undated: "Someday",
  history: "History",
};

const GROUP_HINT: Record<Group, string> = {
  waiting: "Fired and not yet acknowledged.",
  scheduled: "Armed and waiting for their time.",
  agent: "Reminders that dispatched a task for an agent to work.",
  undated: "No date — they never fire on their own.",
  history: "Acknowledged, or a series that has ended.",
};

function fmtWhen(iso?: string): string {
  if (!iso) return "";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  // The viewer's own zone: a reminder is about when it reaches THEM, and
  // remind_at carries an explicit offset so this conversion is exact.
  return d.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

export function RemindersLeaf({
  target,
}: {
  target: Record<string, unknown>;
}): JSX.Element {
  const projectId =
    typeof target.projectId === "string" ? target.projectId : undefined;
  const { reminders, isLoading, error, refetch, ack, snooze } =
    useReminders(projectId);
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const toast = useUI((s) => s.toast);
  const [collapsed, setCollapsed] = useState<Partial<Record<Group, boolean>>>(
    // History is the one group that grows without bound, so it starts folded.
    { history: true },
  );

  const groups = useMemo(() => {
    const g: Record<Group, ReminderSummary[]> = {
      waiting: [],
      scheduled: [],
      agent: [],
      undated: [],
      history: [],
    };
    for (const r of reminders) {
      // "Agent runs" is a VIEW, not a state — a task-action reminder that has
      // fired belongs in both, and the one people look for is the agent list.
      if (r.action === "task" && r.generated_task_id) g.agent.push(r);
      if (r.state === "fired") g.waiting.push(r);
      else if (r.state === "armed") g.scheduled.push(r);
      else if (r.state === "undated") g.undated.push(r);
      else g.history.push(r);
    }
    return g;
  }, [reminders]);

  if (isLoading) return <Loading size="sm" label="Loading reminders…" />;
  if (error) return <ErrorState error={error} onRetry={refetch} />;

  const run = async (label: string, fn: () => Promise<void>) => {
    try {
      await fn();
    } catch (err) {
      toast(
        `${label} failed: ${err instanceof Error ? err.message : String(err)}`,
        "error",
      );
    }
  };

  const order: Group[] = [
    "waiting",
    "scheduled",
    "agent",
    "undated",
    "history",
  ];
  const total = reminders.length;

  return (
    <div className="reminders-leaf">
      {total === 0 && (
        <div className="reminders-leaf__empty">
          No reminders yet. Agents create them with the{" "}
          <code>reminder_create</code> tool; a dated one fires here, an undated
          one is just something to come back to.
        </div>
      )}
      {order.map((key) => {
        const rows = groups[key];
        if (rows.length === 0) return null;
        const folded = collapsed[key] ?? false;
        return (
          <section key={key} className="reminders-group">
            <button
              className="reminders-group__head"
              aria-expanded={!folded}
              onClick={() =>
                setCollapsed((c) => ({ ...c, [key]: !(c[key] ?? false) }))
              }
              title={GROUP_HINT[key]}
            >
              <span className="caret">{folded ? "▸" : "▾"}</span>
              {GROUP_LABEL[key]}
              <span className="reminders-group__count">{rows.length}</span>
            </button>
            {!folded && (
              <div className="reminders-group__rows">
                {rows.map((r) => (
                  <div
                    key={`${key}-${r.reminder_id}`}
                    className="reminder-item"
                  >
                    <div className="reminder-item__main">
                      <div className="reminder-item__title">{r.title}</div>
                      <div className="reminder-item__meta">
                        {r.remind_at ? fmtWhen(r.remind_at) : "no date"}
                        {r.repeat ? ` · repeats ${r.repeat}` : ""}
                        {r.project ? ` · ${r.project}` : ""}
                        {r.fire_count ? ` · fired ${r.fire_count}×` : ""}
                      </div>
                      {r.generated_task_id && (
                        <button
                          className="reminder-item__task"
                          title="Open the task this reminder created"
                          onClick={() =>
                            openInFocus(
                              "task-detail",
                              {
                                projectId: r.project,
                                taskId: r.generated_task_id,
                              },
                              r.generated_task_id ?? "task",
                            )
                          }
                        >
                          → task {r.generated_task_id}
                        </button>
                      )}
                    </div>
                    <div className="reminder-item__acts">
                      {r.state === "fired" && (
                        <>
                          <button
                            title="Remind me again in an hour"
                            onClick={() =>
                              void run("Snooze", () =>
                                snooze(r.reminder_id, snoozeUntil(60)),
                              )
                            }
                          >
                            1h
                          </button>
                          <button
                            className="primary"
                            title="Acknowledge"
                            onClick={() =>
                              void run("Acknowledge", () => ack(r.reminder_id))
                            }
                          >
                            Done
                          </button>
                        </>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </section>
        );
      })}
    </div>
  );
}

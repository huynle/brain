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

import { useReminders } from "../../../hooks/useReminders";
import { useReminderRowActions } from "../../../hooks/useReminderRowActions";
import { Loading } from "../../common/Loading";
import { ErrorState } from "../../common/ErrorState";
import { ReminderRows } from "../../Reminders/ReminderRows";
import { groupReminders } from "../../../lib/reminders";

type Group = "waiting" | "scheduled" | "stopped" | "someday" | "executed" | "agent";

const GROUP_LABEL: Record<Group, string> = {
  waiting: "Waiting on you",
  scheduled: "Scheduled",
  stopped: "Stopped",
  someday: "Someday",
  executed: "Executed",
  agent: "Agent runs",
};

const GROUP_HINT: Record<Group, string> = {
  waiting: "Fired and not yet acknowledged.",
  scheduled: "Armed and waiting for their time.",
  stopped: "Blocked, cancelled, or archived reminders.",
  someday: "No date — they never fire on their own.",
  executed: "Acknowledged reminders and completed series.",
  agent: "Reminders that dispatched a task for an agent to work.",
};

export function RemindersLeaf({
  target,
}: {
  target: Record<string, unknown>;
}): JSX.Element {
  const projectId =
    typeof target.projectId === "string" ? target.projectId : undefined;
  const remindersQuery = useReminders(projectId);
  const { reminders, isLoading, error, refetch } = remindersQuery;
  const actions = useReminderRowActions(remindersQuery);
  const [collapsed, setCollapsed] = useState<Partial<Record<Group, boolean>>>(
    // Completed history is the one group that grows without bound.
    { executed: true },
  );

  const groups = useMemo(() => groupReminders(reminders), [reminders]);

  if (isLoading) return <Loading size="sm" label="Loading reminders…" />;
  if (error) return <ErrorState error={error} onRetry={refetch} />;

  const order: Group[] = [
    "waiting",
    "scheduled",
    "stopped",
    "someday",
    "executed",
    "agent",
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
                <ReminderRows reminders={rows} actions={actions} />
              </div>
            )}
          </section>
        );
      })}
    </div>
  );
}

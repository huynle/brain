/**
 * ReminderStatusPopups — fired reminders surfaced as persistent status
 * cards in the lower-left background-operation tray.
 *
 * This is a second, more proactive surface for the SAME fired reminders
 * the ReminderBell counts (see hooks/useReminders). The bell is a
 * pull surface — you have to open it. These cards push: the moment a
 * reminder fires it appears down in the corner alongside background
 * operations, the way a "Deleted entry" status does, and stays there
 * until the user acts on it (Done / Snooze) or dismisses the card.
 *
 * State model, deliberately minimal:
 *   • `fired` from useReminders is the source of truth (server-backed,
 *     survives reload). No client store mirrors it.
 *   • Done (ack) and Snooze mutate the server, so the reminder leaves
 *     `fired` on the next poll/invalidate and its card disappears on its
 *     own — nothing to reconcile locally.
 *   • The one purely-local bit is "I've seen this, hide the card" without
 *     acknowledging it (the reminder stays fired/visible under the bell).
 *     That lives in a component-local set of reminder ids so a dismissed
 *     card does not re-pop on every 30s poll. It resets on reload, which
 *     is fine: an unacknowledged reminder SHOULD greet you again.
 */
import { useEffect, useState } from "react";

import { useReminders, snoozeUntil } from "../../hooks/useReminders";
import { useWorkspace } from "../../store/workspace";
import { useUI } from "../../store/ui";

/** How overdue, in words. Mirrors ReminderBell's `firedAgo`. */
function firedAgo(firedAt?: string): string {
  if (!firedAt) return "";
  const ms = Date.now() - new Date(firedAt).getTime();
  if (!Number.isFinite(ms) || ms < 0) return "just now";
  const mins = Math.floor(ms / 60_000);
  if (mins < 1) return "just now";
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

export function ReminderStatusPopups(): JSX.Element | null {
  const { fired, ack, snooze } = useReminders();
  const toast = useUI((s) => s.toast);
  const openInFocus = useWorkspace((s) => s.openInFocus);

  // Locally hidden cards (dismissed without acknowledging). Keyed by
  // reminder_id. Not persisted — see the module docstring.
  const [hidden, setHidden] = useState<Set<string>>(new Set());

  // Keep the hidden set from growing unbounded and, more importantly,
  // let a reminder re-pop if it fires again after being snoozed: prune
  // ids that are no longer in the fired list.
  useEffect(() => {
    setHidden((prev) => {
      if (prev.size === 0) return prev;
      const live = new Set(fired.map((r) => r.reminder_id));
      let changed = false;
      const next = new Set<string>();
      for (const id of prev) {
        if (live.has(id)) next.add(id);
        else changed = true;
      }
      return changed ? next : prev;
    });
  }, [fired]);

  const visible = fired.filter((r) => !hidden.has(r.reminder_id));
  if (visible.length === 0) return null;

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

  const hide = (id: string) =>
    setHidden((prev) => {
      const next = new Set(prev);
      next.add(id);
      return next;
    });

  return (
    <aside className="reminder-status-list" aria-label="Fired reminders">
      {visible.map((r) => (
        <section
          key={r.reminder_id}
          className="background-operation reminder-status"
        >
          <div className="background-operation-heading">
            <strong>🔔 {r.title}</strong>
            <button
              onClick={() => hide(r.reminder_id)}
              aria-label={`Dismiss reminder ${r.title}`}
              title="Hide this card (stays under the bell)"
            >
              ×
            </button>
          </div>
          <div role="status" className="reminder-status__meta">
            {firedAgo(r.fired_at)}
            {r.project ? ` · ${r.project}` : ""}
            {r.generated_task_id ? ` · created task ${r.generated_task_id}` : ""}
          </div>
          <div className="reminder-status__acts">
            <button
              title="Remind me again in an hour"
              onClick={() =>
                void run("Snooze", () => snooze(r.reminder_id, snoozeUntil(60)))
              }
            >
              Snooze 1h
            </button>
            <button
              title="Open the Reminders pane"
              onClick={() => openInFocus("reminders", {}, "Reminders")}
            >
              Open
            </button>
            <button
              className="primary"
              title="Acknowledge and clear"
              onClick={() => void run("Acknowledge", () => ack(r.reminder_id))}
            >
              Done
            </button>
          </div>
        </section>
      ))}
    </aside>
  );
}

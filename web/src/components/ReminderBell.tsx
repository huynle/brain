/**
 * ReminderBell — the in-app notification surface for fired reminders.
 *
 * "A notification in the brain app" is exactly this: a reminder whose time
 * arrived, sitting in the fired state until someone acknowledges it. Nothing
 * is stored client-side and nothing expires on its own — unlike a toast,
 * which is gone in four seconds and cannot survive the reload that a reminder
 * has to.
 *
 * The bell only appears when something is waiting. Chrome that is always
 * present but almost always empty trains people to stop looking at it.
 */
import { useEffect, useRef, useState } from "react";

import { useReminders, snoozeUntil } from "../hooks/useReminders";
import { useUI } from "../store/ui";

/** How overdue, in words. Reminders are inherently about time. */
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

export function ReminderBell(): JSX.Element | null {
  const { fired, ack, snooze } = useReminders();
  const [open, setOpen] = useState(false);
  const wrapRef = useRef<HTMLDivElement | null>(null);
  const toast = useUI((s) => s.toast);

  // Close on an outside click or Escape, like every other transient popover.
  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  // A panel left open after the last reminder is acknowledged is an empty box
  // the user has to dismiss by hand.
  useEffect(() => {
    if (open && fired.length === 0) setOpen(false);
  }, [open, fired.length]);

  if (fired.length === 0) return null;

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

  return (
    <div className="reminder-bell" ref={wrapRef}>
      <button
        className={"icon-btn" + (open ? " active" : "")}
        title={`${fired.length} reminder${fired.length === 1 ? "" : "s"} waiting`}
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        🔔<span className="dock-count">{fired.length}</span>
      </button>

      {open && (
        <div className="reminder-panel" role="dialog" aria-label="Reminders">
          <div className="reminder-panel__head">
            {fired.length} reminder{fired.length === 1 ? "" : "s"}
          </div>
          <div className="reminder-panel__list">
            {fired.map((r) => (
              <div key={r.reminder_id} className="reminder-row">
                <div className="reminder-row__main">
                  <div className="reminder-row__title">{r.title}</div>
                  <div className="reminder-row__meta">
                    {firedAgo(r.fired_at)}
                    {r.project ? ` · ${r.project}` : ""}
                    {/* Say what it DID, not just that it fired — a task
                        action already created work, and hiding that makes
                        the notification look like it did nothing. */}
                    {r.generated_task_id
                      ? ` · created task ${r.generated_task_id}`
                      : ""}
                  </div>
                </div>
                <div className="reminder-row__acts">
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
                    title="Remind me again tomorrow"
                    onClick={() =>
                      void run("Snooze", () =>
                        snooze(r.reminder_id, snoozeUntil(60 * 24)),
                      )
                    }
                  >
                    1d
                  </button>
                  <button
                    className="primary"
                    title="Acknowledge and clear"
                    onClick={() =>
                      void run("Acknowledge", () => ack(r.reminder_id))
                    }
                  >
                    Done
                  </button>
                </div>
              </div>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

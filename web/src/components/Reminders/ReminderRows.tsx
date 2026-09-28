import type { ReminderSummary } from "../../lib/types";

export function formatReminderTime(iso?: string): string {
  if (!iso) return "";
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return date.toLocaleString(undefined, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
  });
}

export interface ReminderRowActions {
  onAck: (reminder: ReminderSummary) => void;
  onSnooze: (reminder: ReminderSummary) => void;
  onOpenReminder: (reminder: ReminderSummary) => void;
  onOpenTask: (reminder: ReminderSummary) => void;
}

export function ReminderRows({
  reminders,
  actions,
}: {
  reminders: ReminderSummary[];
  actions: ReminderRowActions;
}): JSX.Element {
  return (
    <div className="reminder-items">
      {reminders.map((reminder) => (
        <ReminderRow
          key={reminder.reminder_id}
          reminder={reminder}
          actions={actions}
        />
      ))}
    </div>
  );
}

function ReminderRow({
  reminder,
  actions,
}: {
  reminder: ReminderSummary;
  actions: ReminderRowActions;
}): JSX.Element {
  return (
        <article className="reminder-item reminder-item--openable">
          <div className="reminder-item__row">
          <button
            type="button"
            className="reminder-item__open"
            aria-label={`Open ${reminder.title} in the sidebar`}
            onClick={() => actions.onOpenReminder(reminder)}
          >
            <div className="reminder-item__title">{reminder.title}</div>
            <div className="reminder-item__meta">
              {reminder.remind_at
                ? formatReminderTime(reminder.remind_at)
                : "no date"}
              {reminder.repeat ? ` · repeats ${reminder.repeat}` : ""}
              {` · ${reminder.project || "global"}`}
              {reminder.fire_count ? ` · fired ${reminder.fire_count}×` : ""}
            </div>
          </button>
          <div className="reminder-item__acts">
            {reminder.generated_task_id && (
              <button
                className="reminder-item__task"
                title="Open the task this reminder created"
                onClick={() => actions.onOpenTask(reminder)}
              >
                → task {reminder.generated_task_id}
              </button>
            )}
            {reminder.state === "fired" && (
              <>
                <button
                  title="Remind me again in an hour"
                  aria-label={`Snooze ${reminder.title} for one hour`}
                  onClick={() => actions.onSnooze(reminder)}
                >
                  1h
                </button>
                <button
                  className="primary"
                  title="Acknowledge"
                  aria-label={`Mark ${reminder.title} done`}
                  onClick={() => actions.onAck(reminder)}
                >
                  Done
                </button>
              </>
            )}
          </div>
          </div>
        </article>
  );
}

import { useCallback } from "react";

import { snoozeUntil, type UseRemindersResult } from "./useReminders";
import type { ReminderSummary } from "../lib/types";
import { useWorkspace } from "../store/workspace";
import { useUI } from "../store/ui";
import type { ReminderRowActions } from "../components/Reminders/ReminderRows";

export function useReminderRowActions(
  reminders: Pick<UseRemindersResult, "ack" | "snooze">,
): ReminderRowActions {
  const openInFocus = useWorkspace((state) => state.openInFocus);
  const openOrReuseInSidebar = useWorkspace(
    (state) => state.openOrReuseInSidebar,
  );
  const toast = useUI((state) => state.toast);

  const run = useCallback(
    async (label: string, action: () => Promise<void>) => {
      try {
        await action();
      } catch (error) {
        toast(
          `${label} failed: ${error instanceof Error ? error.message : String(error)}`,
          "error",
        );
      }
    },
    [toast],
  );

  return {
    onAck: (reminder: ReminderSummary) => {
      void run("Acknowledge", () => reminders.ack(reminder.reminder_id));
    },
    onSnooze: (reminder: ReminderSummary) => {
      void run("Snooze", () =>
        reminders.snooze(reminder.reminder_id, snoozeUntil(60)),
      );
    },
    onOpenReminder: (reminder: ReminderSummary) => {
      openOrReuseInSidebar(
        "entry",
        { path: reminder.path || reminder.entry_id },
        reminder.title,
      );
    },
    onOpenTask: (reminder: ReminderSummary) => {
      if (!reminder.generated_task_id) return;
      openInFocus(
        "task-detail",
        {
          projectId: reminder.project,
          taskId: reminder.generated_task_id,
        },
        reminder.generated_task_id,
      );
    },
  };
}

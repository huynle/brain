/**
 * useReminders — the reminder list, and the fired ones that act as the app's
 * notifications.
 *
 * There is no separate notification store, deliberately. A reminder that has
 * fired and not been acknowledged IS the notification: the server records
 * that as the entry's own status, which reaches the markdown file, so it
 * survives a reload, a restart and a closed tab. A parallel notification
 * table would be a second source of truth to drift.
 */
import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { ackReminder, listReminders, snoozeReminder } from "../lib/api";
import type { ReminderSummary } from "../lib/types";

const REMINDERS_KEY = ["v2", "reminders"] as const;

export interface UseRemindersResult {
  reminders: ReminderSummary[];
  /** Fired and unacknowledged — what the bell counts. */
  fired: ReminderSummary[];
  isLoading: boolean;
  error: unknown;
  refetch: () => void;
  ack: (id: string) => Promise<void>;
  snooze: (id: string, remindAt: string) => Promise<void>;
}

export function useReminders(project?: string): UseRemindersResult {
  const qc = useQueryClient();
  const q = useQuery({
    queryKey: [...REMINDERS_KEY, project ?? ""],
    queryFn: () => listReminders(project ? { project } : undefined),
    // 30s: a reminder is already up to a minute imprecise because the server
    // sweeps on a 1m tick, so polling faster buys nothing real.
    refetchInterval: 30_000,
    staleTime: 20_000,
  });

  const reminders = q.data ?? [];
  const fired = reminders.filter((r) => r.state === "fired");

  const invalidate = useCallback(() => {
    void qc.invalidateQueries({ queryKey: REMINDERS_KEY });
  }, [qc]);

  const ack = useCallback(
    async (id: string) => {
      await ackReminder(id);
      invalidate();
    },
    [invalidate],
  );

  const snooze = useCallback(
    async (id: string, remindAt: string) => {
      await snoozeReminder(id, remindAt);
      invalidate();
    },
    [invalidate],
  );

  return {
    reminders,
    fired,
    isLoading: q.isLoading,
    error: q.error,
    refetch: () => void q.refetch(),
    ack,
    snooze,
  };
}

/** Minutes from now as an RFC3339 instant, for snooze presets. */
export function snoozeUntil(minutesFromNow: number): string {
  return new Date(Date.now() + minutesFromNow * 60_000).toISOString();
}

/**
 * useAttention — the attention inbox, and the unread items that act as the
 * app's notifications.
 *
 * Mirrors useReminders: an unread attention item IS the notification, so
 * there is no separate client-side notification store to drift. The server
 * records state on the item itself, so it survives reload/restart/closed tab.
 *
 * The list polls on a 30s interval (mirroring reminders) rather than
 * subscribing to the `attention.*` SSE topic: the app's only live stream is
 * the per-project task stream, and there is no generic events/stream
 * subscription hook to hang an invalidation off. Polling keeps the surface
 * consistent with reminders and avoids a second long-lived socket.
 */
import { useCallback } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { useAuth } from "../lib/auth";
import {
  getAttentionCounts,
  listAttention,
  setAttentionState,
  type AttentionFilter,
} from "../lib/api";
import type { Attention, AttentionCounts } from "../lib/types";

const ATTENTION_KEY = ["v2", "attention"] as const;
const ATTENTION_COUNTS_KEY = ["v2", "attention", "counts"] as const;

export interface UseAttentionResult {
  attention: Attention[];
  /** Unread — what the bell counts. */
  unread: Attention[];
  isLoading: boolean;
  error: unknown;
  refetch: () => void;
  markRead: (id: string) => Promise<void>;
  markUnread: (id: string) => Promise<void>;
  resolve: (id: string) => Promise<void>;
  dismiss: (id: string) => Promise<void>;
  snooze: (id: string, snoozedUntil: string) => Promise<void>;
}

export function useAttention(filter?: AttentionFilter): UseAttentionResult {
  const qc = useQueryClient();
  const auth = useAuth((s) => s.status);
  const q = useQuery({
    queryKey: [...ATTENTION_KEY, filter ?? {}],
    enabled: auth === "authenticated" || auth === "anonymous",
    queryFn: () => listAttention(filter),
    refetchInterval: 30_000,
    staleTime: 20_000,
  });

  const attention = q.data ?? [];
  const unread = attention.filter((a) => a.state === "unread");

  const invalidate = useCallback(() => {
    void qc.invalidateQueries({ queryKey: ATTENTION_KEY });
  }, [qc]);

  const markRead = useCallback(
    async (id: string) => {
      await setAttentionState(id, "read");
      invalidate();
    },
    [invalidate],
  );

  const unreadAction = useCallback(
    async (id: string) => {
      await setAttentionState(id, "unread");
      invalidate();
    },
    [invalidate],
  );

  const resolve = useCallback(
    async (id: string) => {
      await setAttentionState(id, "resolve");
      invalidate();
    },
    [invalidate],
  );

  const dismiss = useCallback(
    async (id: string) => {
      await setAttentionState(id, "dismiss");
      invalidate();
    },
    [invalidate],
  );

  const snooze = useCallback(
    async (id: string, snoozedUntil: string) => {
      await setAttentionState(id, "snooze", { snoozed_until: snoozedUntil });
      invalidate();
    },
    [invalidate],
  );

  return {
    attention,
    unread,
    isLoading: q.isLoading,
    error: q.error,
    refetch: () => void q.refetch(),
    markRead,
    markUnread: unreadAction,
    resolve,
    dismiss,
    snooze,
  };
}

export function useAttentionCounts(): {
  counts: AttentionCounts | undefined;
  isLoading: boolean;
  error: unknown;
} {
  const auth = useAuth((s) => s.status);
  const q = useQuery({
    queryKey: ATTENTION_COUNTS_KEY,
    enabled: auth === "authenticated" || auth === "anonymous",
    queryFn: () => getAttentionCounts(),
    refetchInterval: 30_000,
    staleTime: 20_000,
  });
  return { counts: q.data, isLoading: q.isLoading, error: q.error };
}

/** Minutes from now as an RFC3339 instant, for snooze presets. */
export function snoozeUntil(minutesFromNow: number): string {
  return new Date(Date.now() + minutesFromNow * 60_000).toISOString();
}

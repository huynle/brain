/**
 * panes-v2 sessions hook.
 *
 * Wraps `listInstances()` with react-query polling at 8s. The v2 UI
 * treats "sessions" as OpenCode instances of `kind === "task"` that
 * are still running (status "starting" or "busy"). Idle/exited task
 * instances are hidden — Phase 6 will wire the "history" drilldown
 * that surfaces them.
 *
 * Callers use `sessions` for the pre-filtered running list and
 * `allInstances` when they need the raw data (e.g. per-project card
 * that wants to show "no sessions" vs "sessions exist but idle").
 *
 * Polling cadence mirrors the RunnersView instance query — matches
 * the pattern the task description points at.
 */
import { useQuery } from "@tanstack/react-query";
import { listInstances } from "../lib/api";
import type { OpencodeInstance } from "../lib/types";

export interface UseSessionsResult {
  /** Task instances currently running (starting|busy). */
  sessions: OpencodeInstance[];
  /** Raw list from the server, unfiltered. */
  allInstances: OpencodeInstance[];
  isLoading: boolean;
  error: unknown;
  refetch: () => void;
}

/**
 * A session is a runnable OpenCode instance attached to a task —
 * kind "task" and status starting|busy. Idle instances are hidden
 * because they typically represent recently-completed work; the
 * sidebar section is meant to show "what's live right now."
 */
export function isLiveTaskSession(inst: OpencodeInstance): boolean {
  if (inst.kind !== "task") return false;
  return inst.status === "starting" || inst.status === "busy";
}

export function useSessions(): UseSessionsResult {
  const q = useQuery({
    queryKey: ["v2", "sessions"],
    queryFn: listInstances,
    refetchInterval: 8_000,
    staleTime: 6_000,
  });
  const all = q.data ?? [];
  const sessions = all.filter(isLiveTaskSession);

  return {
    sessions,
    allInstances: all,
    isLoading: q.isLoading,
    error: q.error,
    refetch: () => void q.refetch(),
  };
}

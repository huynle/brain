/**
 * Automation run history.
 *
 * Two shapes over the same endpoint, because the two questions have
 * different costs:
 *
 *   useAutomationRuns(project)              — the project's recent runs,
 *     one cheap query, used for the "last run" cell on every automation
 *     row and for the project-wide Runs pane.
 *   useAutomationRuns(project, automationId) — one automation's history,
 *     which the server can only answer with a bounded over-fetch (the
 *     automation id is in the run body, not a column).
 *
 * `useAutomations` deliberately drops the run list it fetches — this is
 * the hook its docstring says to add rather than overloading it.
 *
 * Runs are append-only history: a 30s refetch is plenty, and the parsed
 * result is memoized so a poll that returns identical data doesn't churn
 * every row.
 */
import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";

import { listAutomationRuns } from "../lib/api";
import { parseAutomationRuns, type AutomationRun } from "../lib/automationRuns";

/** Rows fetched for a project-wide view. */
export const PROJECT_RUNS_LIMIT = 200;
/** Rows fetched for one automation's own history. */
export const AUTOMATION_RUNS_LIMIT = 100;

export interface UseAutomationRunsResult {
  runs: AutomationRun[];
  isLoading: boolean;
  error: unknown;
  /** The server exhausted its scan window before filling the page — the
   *  history is longer than what is shown, not shorter. */
  truncated: boolean;
  /**
   * The page came back FULL, so absence proves nothing.
   *
   * This is the difference between "this automation has never run" and
   * "nothing of this automation is in the newest 200 runs" — and on a
   * project with a minutely cron, the second is the normal case for
   * every other automation. A surface that renders the second as the
   * first states a falsehood about a job that may be firing nightly.
   */
  windowFull: boolean;
  /** The window size that produced this result. */
  limit: number;
  refetch: () => void;
}

export function useAutomationRuns(
  projectId: string,
  automationId?: string,
  limit?: number,
): UseAutomationRunsResult {
  const rows =
    limit ?? (automationId ? AUTOMATION_RUNS_LIMIT : PROJECT_RUNS_LIMIT);
  const q = useQuery({
    queryKey: ["v2", "automation-runs", projectId, automationId ?? "*", rows],
    queryFn: () =>
      listAutomationRuns({
        project: projectId,
        ...(automationId ? { automation_id: automationId } : {}),
        limit: rows,
      }),
    enabled: !!projectId,
    refetchInterval: 30_000,
    staleTime: 20_000,
    retry: 2,
  });

  const runs = useMemo(
    () => parseAutomationRuns(q.data?.entries ?? []),
    [q.data],
  );

  return {
    runs,
    windowFull: (q.data?.entries?.length ?? 0) >= rows,
    limit: rows,
    // `isPending` alone stays true forever for a disabled query; pair it
    // with fetchStatus so an empty projectId renders the empty state.
    isLoading: q.isPending && q.fetchStatus !== "idle",
    error: q.error,
    truncated: q.data?.truncated ?? false,
    refetch: () => void q.refetch(),
  };
}

/**
 * useSchedulerStatus — per-project "why did the last pass skip this?".
 *
 * The scheduler already publishes the answer; nothing in the PWA was reading
 * it. `last_project_results[project]` carries the skip breakdown from the most
 * recent pass, e.g.
 *
 *   {"project_id":"sandbox-demo","considered":1,"dispatched":0,
 *    "skipped":1,"skipped_tasks_paused":1}
 *
 * which is the difference between "held by a dial someone flipped" and "no
 * runner will ever take this".
 *
 * The endpoint is global — one call covers every project — so this is a
 * single shared query rather than one per project card. Like usePauseState
 * it is a CACHE READER with no timer of its own: `useSchedulerSync()` owns
 * the poll and is mounted once by Dashboard. Without that split, a dashboard
 * showing ten projects would have had ten observers each polling on their
 * own schedule against one shared cache entry.
 */
import { useQuery } from "@tanstack/react-query";

import { ApiError, getSchedulerStatus } from "../lib/api";
import type { SchedulerResult, SchedulerStatus } from "../lib/types";

const SCHEDULER_KEY = ["v2", "scheduler-status"] as const;
const SCHEDULER_POLL_MS = 12_000;

function schedulerQuery(poll: boolean) {
  return {
    queryKey: SCHEDULER_KEY,
    queryFn: getSchedulerStatus,
    refetchInterval: poll ? SCHEDULER_POLL_MS : (false as const),
    staleTime: 10_000,
    // /scheduler/status is `notImplemented` when the handler has no scheduler
    // wired (see internal/api/router.go). Do not retry a permanent 501.
    retry: (count: number, err: unknown) =>
      err instanceof ApiError && (err.status === 501 || err.status === 404)
        ? false
        : count < 2,
    placeholderData: (prev: SchedulerStatus | undefined) => prev,
  };
}

/** Mount ONCE, in the app shell. Owns the polling for every reader. */
export function useSchedulerSync(): void {
  useQuery(schedulerQuery(true));
}

export interface UseSchedulerStatusResult {
  /** Last pass for one project, or undefined when the scheduler has not run
   *  for it yet (or the endpoint is unavailable). */
  resultFor: (projectId: string) => SchedulerResult | undefined;
}

export function useSchedulerStatus(): UseSchedulerStatusResult {
  const { data } = useQuery(schedulerQuery(false));
  return {
    resultFor: (projectId: string) =>
      data?.last_project_results?.[projectId],
  };
}

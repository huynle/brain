/**
 * panes-v2 automations hook.
 *
 * Wraps `listAutomationData(project)` with react-query. Refetches
 * every 20s and on window focus, which is a good balance — the
 * automation catalog is generally static within a session, and
 * the "last-run" timestamps on the CardAutomations UI aren't
 * time-critical (a 20s lag is fine).
 *
 * Returns only the automation entries (the auxiliary `tasks` and
 * `runs` lists are intentionally dropped here — CardAutomations
 * only shows the automation list). If a future card needs the run
 * history, add a `useAutomationRuns` hook instead of overloading
 * this one.
 *
 * We surface `isPending && !data` as "loading" instead of the older
 * `isLoading` shortcut so that a request that never resolves (e.g.
 * the auxiliary automation_run endpoint hanging) still shows an
 * empty state after react-query gives up rather than an eternal
 * spinner. The parallel Promise.all in listAutomationData wraps
 * automation_run errors already; this hook just guards against
 * disabled-query edge cases.
 */
import { useQuery } from "@tanstack/react-query";
import { listAutomationData } from "../lib/api";
import type { BrainEntry } from "../lib/types";

export interface UseAutomationsResult {
  automations: BrainEntry[];
  isLoading: boolean;
  error: unknown;
  refetch: () => void;
}

export function useAutomations(projectId: string): UseAutomationsResult {
  const q = useQuery({
    queryKey: ["v2", "automations", projectId],
    queryFn: () => listAutomationData(projectId),
    refetchInterval: 20_000,
    staleTime: 15_000,
    enabled: !!projectId,
    // Cap retries so a persistent 500 doesn't leave the card spinning
    // forever. Two attempts with exponential backoff is enough to ride
    // out a transient blip.
    retry: 2,
  });

  return {
    // `isPending` is true whenever there's no data yet AND the query
    // isn't disabled. If it's disabled (empty projectId), we treat
    // that as "not loading, no data" so the empty state renders.
    isLoading: q.isPending && q.fetchStatus !== "idle",
    error: q.error,
    automations: q.data?.automations ?? [],
    refetch: () => void q.refetch(),
  };
}

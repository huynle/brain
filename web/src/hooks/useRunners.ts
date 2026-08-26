/**
 * panes-v2 runners hook.
 *
 * Preferred data source is `useLive().runners`, which is populated
 * live from the runner SSE stream (see `lib/sse.ts` — the
 * `runners_update` event handler). When SSE hasn't produced any
 * runners yet (fresh reload, connection re-negotiating, or SSE
 * disabled in a smoke environment), we fall back to the REST
 * snapshot from `getRunners()` — the same pattern
 * `views/RunnersView.tsx` uses.
 *
 * We surface a react-query `isLoading` / `error` so the sidebar can
 * render `Loading` / `ErrorState` on first paint, but as soon as
 * SSE lands live runners the row list swaps to the live copy on
 * subsequent renders (React re-runs the selector when the store
 * changes).
 *
 * The query polls every 12s to catch runners that come online while
 * SSE is dropped; that mirrors the existing dashboard's cadence.
 *
 * NOTE on `poll`: react-query schedules `refetchInterval` PER OBSERVER, and
 * every observer shares one cache entry. Six mounted components polling at
 * 12s therefore hit the network every ~2s, not every 12s. Callers that only
 * need to read the shared snapshot should pass `{ poll: false }`; they still
 * re-render on every update, they just do not add a timer of their own.
 */
import { useQuery } from "@tanstack/react-query";
import { getRunners } from "../lib/api";
import { useLive } from "../lib/sse";
import type { RunnerInfo } from "../lib/types";

export interface UseRunnersResult {
  runners: RunnerInfo[];
  isLoading: boolean;
  error: unknown;
  refetch: () => void;
}

export interface UseRunnersOptions {
  /** Own a 12s refetch timer. Default true (existing behavior). Pass false
   *  from hooks that merely read the shared snapshot. */
  poll?: boolean;
}

export function useRunners(opts: UseRunnersOptions = {}): UseRunnersResult {
  const { poll = true } = opts;
  const liveRunners = useLive((s) => s.runners);
  const q = useQuery({
    queryKey: ["v2", "runners"],
    queryFn: getRunners,
    refetchInterval: poll ? 12_000 : false,
    staleTime: 10_000,
  });

  // Prefer live snapshot; fall back to REST when live is empty.
  const runners =
    liveRunners.length > 0 ? liveRunners : (q.data ?? []);

  return {
    runners,
    // Only surface "loading" when neither SSE nor REST has produced
    // rows yet. If SSE has already sent runners we've got data.
    isLoading: liveRunners.length === 0 && q.isLoading,
    error: liveRunners.length === 0 ? q.error : null,
    refetch: () => void q.refetch(),
  };
}

/**
 * usePauseState — the three pause dials, in one object.
 *
 * Before this hook existed the PWA never asked whether anything was paused:
 * `getRunnerStatus()` shipped in lib/api with zero callers, and
 * `RunnerInfo.paused` was typed and documented but never rendered. A fully
 * paused system was pixel-identical to a healthy one — green project dot,
 * "online" runner, tasks sitting at ready forever with no explanation.
 *
 * Two sources, because the dials live in two different places:
 *   - GET /tasks/runner/status → the two PROJECT dials. Note the top-level
 *     `paused` field there is project scope despite the path; see the
 *     FOOTGUN note on RunnerStatusResponse.
 *   - GET /runners → the RUNNER dial, as `paused` on each runner. Reused
 *     from useRunners so the sidebar and this hook can never disagree.
 *
 * ─── Who polls ───────────────────────────────────────────────────
 *
 * `usePauseState` is a CACHE READER. It owns no refetch timer, because
 * react-query schedules `refetchInterval` per observer rather than per
 * query: this hook is mounted by the statusbar, the sidebar, and once per
 * project card AND once per card body, so a 12s interval on each observer
 * would have meant a request every ~12/N seconds — measured at 3s with a
 * single project, and getting worse with every project added.
 *
 * `usePauseSync()` owns the one timer. Dashboard mounts it exactly once.
 * Every reader still re-renders the instant that poll lands, because they
 * all share the same cache entry.
 */
import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";

import { ApiError, getRunnerStatus } from "../lib/api";
import { buildPauseState, type PauseState } from "../lib/pause";
import { useRunners } from "./useRunners";
import type { RunnerStatusResponse } from "../lib/types";

const RUNNER_STATUS_KEY = ["v2", "runner-status"] as const;

/** Poll cadence for the project dials. Matches useRunners so the two halves
 *  of a PauseState never drift far apart. Pause flips arrive as runner
 *  commands over SSE rather than a stream event the PWA subscribes to, so
 *  polling is the only way to stay fresh; 12s bounds how long a stale
 *  "everything is running" can persist. */
const PAUSE_POLL_MS = 12_000;

/** A 501 means the deployment has no runner service wired at all (see
 *  internal/api/router.go's notImplemented fallbacks) — permanent, so
 *  retrying it three times per poll is pure noise. */
const retryUnlessUnsupported = (count: number, err: unknown) =>
  err instanceof ApiError && (err.status === 501 || err.status === 404)
    ? false
    : count < 2;

function runnerStatusQuery(poll: boolean) {
  return {
    queryKey: RUNNER_STATUS_KEY,
    queryFn: getRunnerStatus,
    refetchInterval: poll ? PAUSE_POLL_MS : (false as const),
    staleTime: 10_000,
    retry: retryUnlessUnsupported,
    // One failed poll must not blank a PAUSED badge back to green — that is
    // exactly the false "everything is fine" this whole surface exists to
    // remove. Keep showing the last known dial state.
    placeholderData: (prev: RunnerStatusResponse | undefined) => prev,
  };
}

/**
 * Mount ONCE, in the app shell. Owns the polling for every pause reader.
 * Returns nothing: callers read through usePauseState.
 */
export function usePauseSync(): void {
  useQuery(runnerStatusQuery(true));
  useRunners({ poll: true });
}

export interface UsePauseStateResult {
  pause: PauseState;
  /** True until the project-scope dials have been read at least once. A
   *  caller that must not show a reassuring "running" badge prematurely can
   *  wait on this; the badges themselves simply render nothing. */
  isLoading: boolean;
}

export function usePauseState(): UsePauseStateResult {
  // poll:false — usePauseSync owns the timer for both queries.
  const { runners } = useRunners({ poll: false });
  const { data, isLoading } = useQuery(runnerStatusQuery(false));

  const pause = useMemo(
    () => buildPauseState(data, runners),
    [data, runners],
  );

  return { pause, isLoading: isLoading && data === undefined };
}

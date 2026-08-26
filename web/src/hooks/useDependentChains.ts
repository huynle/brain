/**
 * useDependentChains — standing "run feature + dependents" requests.
 *
 * A chain is a queue the server advances on its own, so the only way the UI
 * can show one is to ask. Without this the feature would be invisible after
 * the click: the toast would say "queued 2 features" and then nothing on
 * screen would ever mention them again — the same unexplained-state problem
 * the pause indicators exist to remove.
 *
 * ─── Who polls ───────────────────────────────────────────────────
 *
 * Same split as usePauseState/usePauseSync, for the same reason: react-query
 * schedules `refetchInterval` per OBSERVER, and this hook is mounted once per
 * project card plus once per feature row. An interval on each would multiply
 * into a request every few seconds. `useDependentChainsSync` owns the one
 * timer; every reader shares the cache entry and re-renders when it lands.
 */
import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";

import { ApiError, listDependentChains } from "../lib/api";
import type { DependentChain } from "../lib/api";

/** Exported so effects that mutate a chain can invalidate the same entry
 *  every reader shares. A second key string would leave the badge stale. */
export const dependentChainsKey = (projectId: string) =>
  ["v2", "dependent-chains", projectId] as const;

/** Chains change on a server-side sweep the PWA does not subscribe to, so
 *  polling is the only way to stay fresh. 15s bounds how long a stale
 *  "queued" badge can outlive a chain that already finished. */
const CHAIN_POLL_MS = 15_000;

/** A 404/501 means the deployment has no chain support wired (see the
 *  notImplemented fallbacks in internal/api/router.go). That is permanent —
 *  retrying it every poll is pure noise on an older server. */
const retryUnlessUnsupported = (count: number, err: unknown) =>
  err instanceof ApiError && (err.status === 501 || err.status === 404)
    ? false
    : count < 2;

function chainsQuery(projectId: string, poll: boolean) {
  return {
    queryKey: dependentChainsKey(projectId),
    queryFn: () => listDependentChains(projectId),
    enabled: projectId !== "",
    refetchInterval: poll ? CHAIN_POLL_MS : (false as const),
    staleTime: 10_000,
    retry: retryUnlessUnsupported,
    // A failed poll must not blank a "queued" badge: the chain is still
    // running on the server, and hiding it would tell the user the opposite
    // of the truth. Keep the last known state.
    placeholderData: (prev: { chains: DependentChain[] } | undefined) => prev,
  };
}

/** Mount ONCE per project card. Owns the polling for every chain reader. */
export function useDependentChainsSync(projectId: string): void {
  useQuery(chainsQuery(projectId, true));
}

export interface UseDependentChainsResult {
  /** Every standing chain in the project, keyed by its root feature. */
  byRoot: Map<string, DependentChain>;
  /** Feature ids that are QUEUED as a member of some chain (roots excluded —
   *  a root is running, not waiting its turn). */
  queuedMembers: Set<string>;
  isLoading: boolean;
}

export function useDependentChains(
  projectId: string,
): UseDependentChainsResult {
  // poll:false — useDependentChainsSync owns the timer.
  const { data, isLoading } = useQuery(chainsQuery(projectId, false));

  return useMemo(() => {
    const byRoot = new Map<string, DependentChain>();
    const queuedMembers = new Set<string>();
    for (const c of data?.chains ?? []) {
      byRoot.set(c.rootFeatureId, c);
      for (const id of c.queued ?? []) queuedMembers.add(id);
    }
    return {
      byRoot,
      queuedMembers,
      isLoading: isLoading && data === undefined,
    };
  }, [data, isLoading]);
}

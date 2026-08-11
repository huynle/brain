/**
 * useMergeRequests — open Brain-native merge requests, grouped by project.
 *
 * One query for every project rather than one per project: OverviewGrid
 * derives features for all projects inside a single memo, so a per-project
 * hook cannot be called there (hooks in loops). All consumers share this
 * one React Query cache entry.
 *
 * merge_request entries do not ride the tasks SSE stream (`tasks_snapshot`
 * carries tasks only), so this polls. 30s matches the cadence of the other
 * REST fallbacks; an MR appearing a few seconds late is acceptable — the
 * alternative (widening the SSE snapshot payload) is a server change with
 * bigger blast radius.
 */
import { useQuery } from "@tanstack/react-query";

import { listEntries } from "../lib/api";
import { mrFeatureId, isOpenMergeRequest } from "../lib/mergeRequests";

const EMPTY: ReadonlyMap<string, ReadonlySet<string>> = new Map();

export interface UseMergeRequestsResult {
  /** projectId → feature ids with an open merge request. */
  openByProject: ReadonlyMap<string, ReadonlySet<string>>;
  /** Convenience accessor; returns undefined when a project has none. */
  openFor: (projectId: string) => ReadonlySet<string> | undefined;
}

export function useMergeRequests(): UseMergeRequestsResult {
  const { data } = useQuery({
    queryKey: ["merge-requests", "open"],
    queryFn: async () => {
      const resp = await listEntries({ type: "merge_request", limit: 200 });
      const byProject = new Map<string, Set<string>>();
      for (const e of resp.entries ?? []) {
        if (!isOpenMergeRequest(e)) continue;
        const fid = mrFeatureId(e);
        const pid = e.project_id;
        if (!fid || !pid) continue;
        let set = byProject.get(pid);
        if (!set) {
          set = new Set();
          byProject.set(pid, set);
        }
        set.add(fid);
      }
      return byProject as ReadonlyMap<string, ReadonlySet<string>>;
    },
    refetchInterval: 30_000,
    // A failed poll should not blank the badge on the next render; keep
    // showing the last known MR state.
    placeholderData: (prev) => prev,
  });

  const openByProject = data ?? EMPTY;
  return {
    openByProject,
    openFor: (projectId: string) => openByProject.get(projectId),
  };
}

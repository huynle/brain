/**
 * The sidebar's visible-project set — the one definition of "the projects
 * I'm currently working with".
 *
 * A project is visible when the user hasn't hidden it (the × / Hidden fold
 * in the sidebar's Projects section) AND it matches the sidebar's
 * task-status chip. Both narrowings are the user pointing at a subset of
 * their 40-odd projects, so every view that claims to follow the sidebar
 * has to read the same set — the Projects list itself, and the Entries
 * browser's default scope.
 */
import { useMemo } from "react";
import { useWorkspace } from "../store/workspace";
import { useProjects } from "./useProjects";
import { useLive } from "../lib/sse";
import { projectMatchesStatusFilter } from "../lib/statusFilter";

export interface VisibleProjects {
  /** Project ids passing both narrowings, in API order. */
  visible: string[];
  /** Explicitly hidden ids, in API order. */
  hidden: string[];
  /** Every known project id. */
  all: string[];
  /** True when nothing is narrowed — visible === all. Callers use this to
   *  skip sending a scope the server would treat as "everything". */
  unfiltered: boolean;
  loading: boolean;
}

export function useVisibleProjects(): VisibleProjects {
  const { data: projects, isLoading } = useProjects();
  const hiddenProjects = useWorkspace((s) => s.hiddenProjects);
  const statusFilter = useWorkspace((s) => s.statusFilter);
  const liveProjects = useLive((s) => s.projects);

  return useMemo(() => {
    const all = projects ?? [];
    const hiddenSet = new Set(hiddenProjects);
    const visible = all.filter(
      (p) =>
        !hiddenSet.has(p) &&
        projectMatchesStatusFilter(liveProjects[p]?.tasks ?? [], statusFilter),
    );
    return {
      visible,
      hidden: all.filter((p) => hiddenSet.has(p)),
      all,
      unfiltered: visible.length === all.length,
      loading: isLoading,
    };
  }, [projects, hiddenProjects, statusFilter, liveProjects, isLoading]);
}

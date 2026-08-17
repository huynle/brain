/**
 * useGoals — goal automations for every project, one shared query.
 *
 * Goals do NOT ride the SSE stream (`tasks_snapshot` carries tasks only),
 * so this polls, mirroring useMergeRequests: one query for all projects,
 * 30s cadence, `placeholderData` so a failed poll never blanks the UI.
 * Mutations must call `invalidate()` — there is no push path to catch a
 * missed one.
 *
 * The default query returns the server's default set (active + blocked +
 * completed; archived hidden). Pass `{ archived: true }` for the archived
 * list — a separate cache entry, fetched only where a surface asks for it.
 */
import { useMemo } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { goalAudit, goalProgress, listGoals } from "../lib/api";
import type {
  GoalAuditResponse,
  GoalProgressResponse,
  GoalSummary,
} from "../lib/types";

const EMPTY_GOALS: readonly GoalSummary[] = Object.freeze([]);
const EMPTY_LIST: GoalSummary[] = [];

// ─── pure selectors (exported for tests) ─────────────────────────────

/** Group goals by project id. Goals without a project are dropped — every
 *  consumer addresses goals through a project surface. */
export function indexGoalsByProject(
  goals: readonly GoalSummary[],
): ReadonlyMap<string, GoalSummary[]> {
  const byProject = new Map<string, GoalSummary[]>();
  for (const g of goals) {
    if (!g.project) continue;
    let list = byProject.get(g.project);
    if (!list) {
      list = [];
      byProject.set(g.project, list);
    }
    list.push(g);
  }
  return byProject;
}

/** Goals scoped to a feature (task-scoped goals under the feature included —
 *  they still belong to it, the task filter just narrows their trigger). */
export function goalsForFeature(
  goals: readonly GoalSummary[],
  projectId: string,
  featureId: string,
): GoalSummary[] {
  if (!projectId || !featureId) return EMPTY_LIST;
  return goals.filter(
    (g) => g.project === projectId && g.feature_id === featureId,
  );
}

/** Goals whose config pins them to exactly this task. */
export function goalsForTask(
  goals: readonly GoalSummary[],
  projectId: string,
  taskId: string,
): GoalSummary[] {
  if (!projectId || !taskId) return EMPTY_LIST;
  return goals.filter(
    (g) => g.project === projectId && g.config?.task_id === taskId,
  );
}

// ─── hook ────────────────────────────────────────────────────────────

export interface UseGoalsResult {
  goals: readonly GoalSummary[];
  /** projectId → goals, insertion order preserved. */
  byProject: ReadonlyMap<string, GoalSummary[]>;
  forFeature: (projectId: string, featureId: string) => GoalSummary[];
  forTask: (projectId: string, taskId: string) => GoalSummary[];
  /** Call after every goal mutation — goals do not ride SSE. */
  invalidate: () => void;
  isLoading: boolean;
  error: unknown;
}

export interface UseGoalsOptions {
  /** Fetch the archived list instead of the default set. */
  archived?: boolean;
  /** Gate the query off entirely (e.g. a fallback lookup). */
  enabled?: boolean;
}

export function useGoals(opts?: UseGoalsOptions): UseGoalsResult {
  const queryClient = useQueryClient();
  const archived = opts?.archived ?? false;
  const q = useQuery({
    queryKey: ["goals", archived ? "archived" : "all"],
    queryFn: () => listGoals(archived ? { status: "archived" } : undefined),
    refetchInterval: 30_000,
    // A failed poll should not blank every goal surface on the next
    // render; keep showing the last known list.
    placeholderData: (prev) => prev,
    enabled: opts?.enabled ?? true,
  });

  const goals = q.data ?? EMPTY_GOALS;
  const byProject = useMemo(() => indexGoalsByProject(goals), [goals]);

  return useMemo(
    () => ({
      goals,
      byProject,
      forFeature: (projectId: string, featureId: string) =>
        goalsForFeature(goals, projectId, featureId),
      forTask: (projectId: string, taskId: string) =>
        goalsForTask(goals, projectId, taskId),
      // Prefix match: also drops per-goal progress/audit entries, which
      // key as ["goals", "progress"|"audit", id].
      invalidate: () =>
        void queryClient.invalidateQueries({ queryKey: ["goals"] }),
      isLoading: q.isPending && q.fetchStatus !== "idle",
      error: q.error,
    }),
    [goals, byProject, queryClient, q.isPending, q.fetchStatus, q.error],
  );
}

/** Linked-task progress for one goal. 30s poll matches the list. */
export function useGoalProgress(goalId: string): {
  progress: GoalProgressResponse | undefined;
  isLoading: boolean;
  error: unknown;
} {
  const q = useQuery({
    queryKey: ["goals", "progress", goalId],
    queryFn: () => goalProgress(goalId),
    refetchInterval: 30_000,
    placeholderData: (prev) => prev,
    enabled: !!goalId,
  });
  return {
    progress: q.data,
    isLoading: q.isPending && q.fetchStatus !== "idle",
    error: q.error,
  };
}

/** Recent reconcile audits for one goal (newest first, server-ordered). */
export function useGoalAudit(
  goalId: string,
  limit = 15,
): {
  audit: GoalAuditResponse | undefined;
  isLoading: boolean;
  error: unknown;
} {
  const q = useQuery({
    queryKey: ["goals", "audit", goalId, limit],
    queryFn: () => goalAudit(goalId, limit),
    refetchInterval: 30_000,
    placeholderData: (prev) => prev,
    enabled: !!goalId,
  });
  return {
    audit: q.data,
    isLoading: q.isPending && q.fetchStatus !== "idle",
    error: q.error,
  };
}

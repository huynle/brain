/**
 * lib/selection — pure logic for multi-select of tasks and features.
 *
 * Selection is scoped to ONE project at a time: marking a row in a
 * different project restarts the selection there. That matches how the
 * bulk verbs work (every bulk endpoint is project-scoped) and keeps the
 * mental model simple — the bar always describes one project's pending
 * operation.
 *
 * Pure by the same rule as the action builders: no react, no store, no
 * fetch. The zustand wrapper lives in `store/selection.ts`; the delete
 * orchestration in `hooks/useSelectionActions`.
 */
import type { Task } from "./types";

export interface SelectionSnapshot {
  projectId: string | null;
  taskIds: ReadonlySet<string>;
  featureIds: ReadonlySet<string>;
}

export const EMPTY_SELECTION: SelectionSnapshot = {
  projectId: null,
  taskIds: new Set(),
  featureIds: new Set(),
};

function toggled(set: ReadonlySet<string>, id: string): Set<string> {
  const next = new Set(set);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  return next;
}

/** Toggle one task. Marking in a different project restarts the scope. */
export function toggleTask(
  s: SelectionSnapshot,
  projectId: string,
  taskId: string,
): SelectionSnapshot {
  if (s.projectId !== projectId) {
    return { projectId, taskIds: new Set([taskId]), featureIds: new Set() };
  }
  return { ...s, taskIds: toggled(s.taskIds, taskId) };
}

/** Toggle one feature. Marking in a different project restarts the scope. */
export function toggleFeature(
  s: SelectionSnapshot,
  projectId: string,
  featureId: string,
): SelectionSnapshot {
  if (s.projectId !== projectId) {
    return { projectId, taskIds: new Set(), featureIds: new Set([featureId]) };
  }
  return { ...s, featureIds: toggled(s.featureIds, featureId) };
}

export function selectionCount(s: SelectionSnapshot): number {
  return s.taskIds.size + s.featureIds.size;
}

export function isEmptySelection(s: SelectionSnapshot): boolean {
  return selectionCount(s) === 0;
}

/**
 * Resolve a selection against the project's live task list into the
 * concrete delete plan:
 *
 * - `taskPaths`/`taskTitles`: entry paths for explicitly selected tasks
 *   that are NOT covered by a selected feature. A task under a selected
 *   feature is deleted by that feature's filter pass; sending its path
 *   too would double-count it in previews and summaries.
 * - `featureIds`: passed through for the per-feature filter deletes.
 * - `staleTaskIds`: selected ids with no matching live task (deleted by
 *   someone else since selection) — dropped from the plan, surfaced so
 *   the caller can mention them rather than silently shrinking counts.
 */
export interface DeletePlan {
  taskPaths: string[];
  taskTitles: string[];
  featureIds: string[];
  staleTaskIds: string[];
}

export function buildDeletePlan(
  s: SelectionSnapshot,
  liveTasks: readonly Task[],
): DeletePlan {
  const byId = new Map(liveTasks.map((t) => [t.id, t]));
  const taskPaths: string[] = [];
  const taskTitles: string[] = [];
  const staleTaskIds: string[] = [];
  for (const id of s.taskIds) {
    const t = byId.get(id);
    if (!t) {
      staleTaskIds.push(id);
      continue;
    }
    if (t.feature_id && s.featureIds.has(t.feature_id)) continue;
    taskPaths.push(t.path);
    taskTitles.push(t.title || t.id);
  }
  return {
    taskPaths,
    taskTitles,
    featureIds: [...s.featureIds],
    staleTaskIds,
  };
}

/** The server caps bulk operations at 100 entries per call. */
export const BULK_PATHS_CHUNK = 100;

export function chunkPaths(
  paths: readonly string[],
  size = BULK_PATHS_CHUNK,
): string[][] {
  const out: string[][] = [];
  for (let i = 0; i < paths.length; i += size) {
    out.push(paths.slice(i, i + size));
  }
  return out;
}

/** One-line description of a selection, for the bar and confirm copy. */
export function describeSelection(taskCount: number, featureCount: number): string {
  const parts: string[] = [];
  if (taskCount > 0) parts.push(`${taskCount} task${taskCount === 1 ? "" : "s"}`);
  if (featureCount > 0)
    parts.push(`${featureCount} feature${featureCount === 1 ? "" : "s"}`);
  return parts.join(" and ") || "nothing";
}

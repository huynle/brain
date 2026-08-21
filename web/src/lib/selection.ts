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

/**
 * The row a shift-click ranges from: the last row the user marked
 * individually (checkbox, tap, or the `v` key). Kind-scoped — a task
 * anchor only ranges over task rows, a feature anchor over feature
 * rows — because a mixed span has no meaningful visual order.
 */
export interface SelectionAnchor {
  kind: "task" | "feature";
  id: string;
}

/** Ids between anchor and target in `orderedIds`, inclusive, in either
 *  direction. Null when either endpoint is not in the list (stale
 *  anchor, filtered-out row) — the caller falls back to a plain toggle. */
function spanIds(
  orderedIds: readonly string[],
  anchorId: string,
  targetId: string,
): string[] | null {
  const a = orderedIds.indexOf(anchorId);
  const b = orderedIds.indexOf(targetId);
  if (a === -1 || b === -1) return null;
  const [lo, hi] = a <= b ? [a, b] : [b, a];
  return orderedIds.slice(lo, hi + 1);
}

/**
 * Shift-click on a task: select every task row between the anchor and
 * the target, in the card's visual order. Monotonic — rows already
 * marked stay marked and the span is always added, never toggled off,
 * matching file-manager convention. With no usable anchor (missing,
 * stale, wrong kind) it degrades to selecting just the target; a
 * shift-click can therefore never shrink the selection. Crossing into
 * a different project restarts the scope, same as a toggle.
 */
export function selectTaskRange(
  s: SelectionSnapshot,
  projectId: string,
  orderedIds: readonly string[],
  anchorId: string | null,
  targetId: string,
): SelectionSnapshot {
  if (s.projectId !== projectId) {
    return { projectId, taskIds: new Set([targetId]), featureIds: new Set() };
  }
  const span =
    anchorId !== null ? spanIds(orderedIds, anchorId, targetId) : null;
  const next = new Set(s.taskIds);
  for (const id of span ?? [targetId]) next.add(id);
  return { ...s, taskIds: next };
}

/** Shift-click on a feature header. Same rules as selectTaskRange. */
export function selectFeatureRange(
  s: SelectionSnapshot,
  projectId: string,
  orderedIds: readonly string[],
  anchorId: string | null,
  targetId: string,
): SelectionSnapshot {
  if (s.projectId !== projectId) {
    return { projectId, taskIds: new Set(), featureIds: new Set([targetId]) };
  }
  const span =
    anchorId !== null ? spanIds(orderedIds, anchorId, targetId) : null;
  const next = new Set(s.featureIds);
  for (const id of span ?? [targetId]) next.add(id);
  return { ...s, featureIds: next };
}

/**
 * True for the Shift+V chord — the keyboard range gesture on a focused
 * row. Plain `v` toggles via the row's Select verb; with shift held the
 * browser reports the key uppercased, so the two never collide.
 */
export function isRangeKey(e: {
  key: string;
  shiftKey: boolean;
  metaKey: boolean;
  ctrlKey: boolean;
  altKey: boolean;
}): boolean {
  return e.key === "V" && e.shiftKey && !e.metaKey && !e.ctrlKey && !e.altKey;
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

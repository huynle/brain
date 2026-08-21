/**
 * store/selection — the multi-select scope for bulk operations.
 *
 * Thin zustand wrapper around the pure reducers in `lib/selection`; the
 * store holds exactly one SelectionSnapshot so every consumer (row
 * checkboxes, the "Select" verbs, SelectionBar) reads and mutates the
 * same scope. See lib/selection for the one-project-at-a-time rule.
 *
 * On top of the snapshot the store tracks the shift-click anchor: every
 * mark — checkbox, tap, `v` key, or a range's own target — becomes the
 * next range's starting row, so consecutive shift-clicks chain.
 */
import { create } from "zustand";

import {
  EMPTY_SELECTION,
  selectFeatureRange,
  selectTaskRange,
  toggleFeature,
  toggleTask,
  type SelectionAnchor,
  type SelectionSnapshot,
} from "../lib/selection";

/** Bulk verbs the selection context menu can request. */
export type SelectionVerb = "archive" | "delete";

interface SelectionStore extends SelectionSnapshot {
  anchor: SelectionAnchor | null;
  /**
   * A bulk verb requested from a row's selection context menu.
   * SelectionBar owns the preview/confirm ladders for these verbs, so
   * the menu posts a request here and the bar consumes it — one dialog
   * owner, no duplicated delete/archive plumbing.
   */
  verbRequest: SelectionVerb | null;
  requestVerb: (verb: SelectionVerb) => void;
  consumeVerbRequest: () => void;
  toggleTask: (projectId: string, taskId: string) => void;
  toggleFeature: (projectId: string, featureId: string) => void;
  /** Shift-click: mark every row between the anchor and `taskId`, in
   *  `orderedIds` (the card's visual order). Falls back to a plain
   *  toggle when the anchor is missing, stale, or the wrong kind. */
  rangeTask: (
    projectId: string,
    orderedIds: readonly string[],
    taskId: string,
  ) => void;
  rangeFeature: (
    projectId: string,
    orderedIds: readonly string[],
    featureId: string,
  ) => void;
  clear: () => void;
}

export const useSelection = create<SelectionStore>((set) => ({
  ...EMPTY_SELECTION,
  anchor: null,
  verbRequest: null,

  requestVerb: (verb) => set({ verbRequest: verb }),
  consumeVerbRequest: () => set({ verbRequest: null }),

  toggleTask: (projectId, taskId) =>
    set((s) => ({
      ...toggleTask(s, projectId, taskId),
      anchor: { kind: "task", id: taskId },
    })),
  toggleFeature: (projectId, featureId) =>
    set((s) => ({
      ...toggleFeature(s, projectId, featureId),
      anchor: { kind: "feature", id: featureId },
    })),
  rangeTask: (projectId, orderedIds, taskId) =>
    set((s) => ({
      ...selectTaskRange(
        s,
        projectId,
        orderedIds,
        s.anchor?.kind === "task" ? s.anchor.id : null,
        taskId,
      ),
      anchor: { kind: "task", id: taskId },
    })),
  rangeFeature: (projectId, orderedIds, featureId) =>
    set((s) => ({
      ...selectFeatureRange(
        s,
        projectId,
        orderedIds,
        s.anchor?.kind === "feature" ? s.anchor.id : null,
        featureId,
      ),
      anchor: { kind: "feature", id: featureId },
    })),
  clear: () => set({ ...EMPTY_SELECTION, anchor: null, verbRequest: null }),
}));

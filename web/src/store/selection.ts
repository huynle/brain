/**
 * store/selection — the multi-select scope for bulk operations.
 *
 * Thin zustand wrapper around the pure reducers in `lib/selection`; the
 * store holds exactly one SelectionSnapshot so every consumer (row
 * checkboxes, the "Select" verbs, SelectionBar) reads and mutates the
 * same scope. See lib/selection for the one-project-at-a-time rule.
 */
import { create } from "zustand";

import {
  EMPTY_SELECTION,
  toggleFeature,
  toggleTask,
  type SelectionSnapshot,
} from "../lib/selection";

interface SelectionStore extends SelectionSnapshot {
  toggleTask: (projectId: string, taskId: string) => void;
  toggleFeature: (projectId: string, featureId: string) => void;
  clear: () => void;
}

export const useSelection = create<SelectionStore>((set) => ({
  ...EMPTY_SELECTION,

  toggleTask: (projectId, taskId) =>
    set((s) => toggleTask(s, projectId, taskId)),
  toggleFeature: (projectId, featureId) =>
    set((s) => toggleFeature(s, projectId, featureId)),
  clear: () => set(EMPTY_SELECTION),
}));

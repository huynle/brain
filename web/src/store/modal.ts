/**
 * panes-v2 modal store.
 *
 * Single-modal state machine. At most one modal is open at a time —
 * `kind` names which one, `target` carries opaque payload data
 * (usually just an id lookup), and `tab` names the active inner tab
 * for modals that have tab strips (Task, Feature).
 *
 * Not persisted — modals are ephemeral and closing the tab should
 * fully reset them.
 *
 * Actions:
 *   • open(kind, target?, tab?) — replaces any currently-open modal
 *   • close()                    — clears kind/target/tab back to null
 *   • switchTab(tab)             — edits only the tab field
 */
import { create } from "zustand";

export type ModalKind =
  | "runner"
  | "task"
  | "task-actions"
  | "task-status"
  | "task-metadata"
  | "feature"
  | "feature-actions"
  | "feature-status"
  | "feature-metadata"
  | "feature-assign"
  | "goal"
  | "goal-create"
  | "settings"
  | null;

export interface ModalState {
  kind: ModalKind;
  target: Record<string, unknown> | null;
  tab: string | null;

  open(
    kind: Exclude<ModalKind, null>,
    target?: Record<string, unknown>,
    tab?: string,
  ): void;
  close(): void;
  switchTab(tab: string): void;
}

export const useModal = create<ModalState>((set) => ({
  kind: null,
  target: null,
  tab: null,

  open: (kind, target, tab) =>
    set({
      kind,
      target: target ?? null,
      tab: tab ?? null,
    }),

  close: () => set({ kind: null, target: null, tab: null }),

  switchTab: (tab) => set({ tab }),
}));

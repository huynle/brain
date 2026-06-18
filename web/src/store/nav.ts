// Keyboard navigation state: a per-view cursor (the keyboard-focused row),
// a multi-select set (composite task keys), and the help overlay flag.
// Mirrors the TUI's cursor + selection model.

import { create } from "zustand";

interface NavState {
  cursor: Record<string, number>; // scope -> index
  selected: Record<string, true>; // composite key -> selected
  helpOpen: boolean;
  commandOpen: boolean;

  getCursor: (scope: string) => number;
  setCursor: (scope: string, n: number) => void;
  moveCursor: (scope: string, delta: number, count: number) => void;
  top: (scope: string) => void;
  bottom: (scope: string, count: number) => void;

  toggleSelect: (key: string) => void;
  selectMany: (keys: string[]) => void;
  clearSelect: () => void;
  selectedCount: () => number;

  setHelpOpen: (open: boolean) => void;
  setCommandOpen: (open: boolean) => void;
}

function clamp(n: number, max: number) {
  if (max <= 0) return 0;
  return Math.max(0, Math.min(n, max - 1));
}

export const useNav = create<NavState>((set, get) => ({
  cursor: {},
  selected: {},
  helpOpen: false,
  commandOpen: false,

  getCursor: (scope) => get().cursor[scope] ?? 0,
  setCursor: (scope, n) =>
    set((s) => ({ cursor: { ...s.cursor, [scope]: Math.max(0, n) } })),
  moveCursor: (scope, delta, count) =>
    set((s) => ({
      cursor: { ...s.cursor, [scope]: clamp((s.cursor[scope] ?? 0) + delta, count) },
    })),
  top: (scope) => set((s) => ({ cursor: { ...s.cursor, [scope]: 0 } })),
  bottom: (scope, count) =>
    set((s) => ({ cursor: { ...s.cursor, [scope]: clamp(count - 1, count) } })),

  toggleSelect: (key) =>
    set((s) => {
      const next = { ...s.selected };
      if (next[key]) delete next[key];
      else next[key] = true;
      return { selected: next };
    }),
  selectMany: (keys) =>
    set((s) => {
      const next = { ...s.selected };
      for (const k of keys) next[k] = true;
      return { selected: next };
    }),
  clearSelect: () => set({ selected: {} }),
  selectedCount: () => Object.keys(get().selected).length,

  setHelpOpen: (open) => set({ helpOpen: open }),
  setCommandOpen: (open) => set({ commandOpen: open }),
}));

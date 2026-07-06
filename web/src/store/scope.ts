// Navigation-context store: the k9s-style drill-down stack plus per-view
// filter / sort / counts, all surfaced by the ContextBar and unwound by the
// unified Escape chain.
//
// Keys for filter/sort/counts are view scopes ("tasks", "brain", ...). The
// drill stack is global — you are only ever inside one drill path at a time;
// switching projects resets it.

import { create } from "zustand";

export interface ScopeFrame {
  kind: "feature" | "task" | "session";
  id: string;
  /** Breadcrumb text. */
  label: string;
  /** The view that interprets this frame. */
  view: string;
}

export interface SortState {
  field: string;
  dir: "asc" | "desc";
}

interface ScopeState {
  stack: ScopeFrame[];
  filter: Record<string, string>;
  sort: Record<string, SortState>;
  counts: Record<string, { shown: number; total: number }>;

  push: (f: ScopeFrame) => void;
  /** Pop the top frame; returns false when the stack was already empty. */
  pop: () => boolean;
  /** Pop back TO a frame (it becomes the top); index -1 clears the stack. */
  popTo: (index: number) => void;
  reset: () => void;

  setFilter: (scope: string, raw: string) => void;
  /** Clear a scope's filter; returns true when there was one to clear. */
  clearFilter: (scope: string) => boolean;

  cycleSortField: (scope: string, fields: readonly string[], defaultField?: string) => void;
  toggleSortDir: (scope: string, defaultField?: string) => void;
  setSort: (scope: string, sort: SortState) => void;

  setCounts: (scope: string, shown: number, total: number) => void;
}

export const useScope = create<ScopeState>((set, get) => ({
  stack: [],
  filter: {},
  sort: {},
  counts: {},

  push: (f) => set((s) => ({ stack: [...s.stack, f] })),
  pop: () => {
    const { stack } = get();
    if (stack.length === 0) return false;
    set({ stack: stack.slice(0, -1) });
    return true;
  },
  popTo: (index) => set((s) => ({ stack: index < 0 ? [] : s.stack.slice(0, index + 1) })),
  reset: () => set({ stack: [] }),

  setFilter: (scope, raw) =>
    set((s) => ({ filter: { ...s.filter, [scope]: raw } })),
  clearFilter: (scope) => {
    const { filter } = get();
    if (!filter[scope]) return false;
    const next = { ...filter };
    delete next[scope];
    set({ filter: next });
    return true;
  },

  cycleSortField: (scope, fields, defaultField) => {
    if (fields.length === 0) return;
    set((s) => {
      const cur = s.sort[scope]?.field ?? defaultField ?? fields[0];
      const idx = fields.indexOf(cur);
      const field = fields[(idx + 1) % fields.length];
      return { sort: { ...s.sort, [scope]: { field, dir: s.sort[scope]?.dir ?? "desc" } } };
    });
  },
  toggleSortDir: (scope, defaultField) =>
    set((s) => {
      const cur = s.sort[scope] ?? { field: defaultField ?? "", dir: "desc" as const };
      return { sort: { ...s.sort, [scope]: { ...cur, dir: cur.dir === "desc" ? "asc" : "desc" } } };
    }),
  setSort: (scope, sort) => set((s) => ({ sort: { ...s.sort, [scope]: sort } })),

  setCounts: (scope, shown, total) =>
    set((s) => {
      const cur = s.counts[scope];
      if (cur && cur.shown === shown && cur.total === total) return s;
      return { counts: { ...s.counts, [scope]: { shown, total } } };
    }),
}));

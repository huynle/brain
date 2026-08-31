/**
 * Entries browser store — filters, selection, and compare pins for the
 * top-level Entries view (see `components/Workspace/EntriesBrowser.tsx`).
 *
 * Persisted (key `panes-v2:entries:v1`): filters, sort, search strategy,
 * the selected entry, and the compare pins — so a reload lands back on
 * the same entry. Ephemeral: the live search query and the compare/diff
 * open flags.
 */
import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import type { SearchStrategy } from "../lib/types";
import type {
  EntrySortBy,
  EntrySortOrder,
  EntryTypeFilter,
} from "../lib/entries";

export const ENTRIES_STORAGE_KEY = "panes-v2:entries:v1";

export interface EntriesState {
  /** "knowledge" (default), "all", or one concrete entry type. */
  typeFilter: EntryTypeFilter;
  /** Project picker value. "" (the default) follows the sidebar's visible
   *  projects, "*" is every project explicitly, "global" is global entries
   *  only, anything else is one project id. See lib/entries
   *  `resolveProjectScope`, which turns this into the actual API scope. */
  projectFilter: string;
  /** "" = any status. */
  statusFilter: string;
  sortBy: EntrySortBy;
  sortOrder: EntrySortOrder;
  /** Live search text (debounced by the browser component). Empty = browse. */
  query: string;
  strategy: SearchStrategy;
  /** Entry open in the reader pane (path). */
  selectedPath: string | null;
  /** Up to two entry paths pinned for compare. */
  comparePins: string[];
  /** Compare surface open (needs two pins). */
  compareOpen: boolean;
  /** Compare surface mode: side-by-side (false) or unified diff (true). */
  diffMode: boolean;

  setTypeFilter(t: EntryTypeFilter): void;
  setProjectFilter(p: string): void;
  setStatusFilter(s: string): void;
  setSort(by: EntrySortBy, order: EntrySortOrder): void;
  setQuery(q: string): void;
  setStrategy(s: SearchStrategy): void;
  selectEntry(path: string | null): void;
  /**
   * Toggle a compare pin. Unpins if already pinned; fills a free slot if
   * one exists; otherwise replaces the second pin (the first pin is the
   * "base" the user is comparing against).
   */
  togglePin(path: string): void;
  clearPins(): void;
  /**
   * Rewrite a ref (entry-link short id, stale path) to the canonical
   * path the API resolved it to. Fixes selection highlight, pin
   * identity, and duplicate pins pointing at the same entry.
   */
  canonicalizeRef(oldRef: string, path: string): void;
  setCompareOpen(open: boolean): void;
  setDiffMode(on: boolean): void;
}

function safeStorage() {
  if (typeof window === "undefined") return undefined;
  try {
    const probe = "__entries_probe__";
    window.localStorage.setItem(probe, "1");
    window.localStorage.removeItem(probe);
    return window.localStorage;
  } catch {
    return undefined;
  }
}

const noopStorage = {
  getItem: () => null,
  setItem: () => undefined,
  removeItem: () => undefined,
};

export const useEntriesStore = create<EntriesState>()(
  persist(
    (set) => ({
      typeFilter: "knowledge",
      projectFilter: "",
      statusFilter: "",
      sortBy: "modified",
      sortOrder: "desc",
      query: "",
      strategy: "fts",
      selectedPath: null,
      comparePins: [],
      compareOpen: false,
      diffMode: false,

      setTypeFilter: (typeFilter) => set({ typeFilter }),
      setProjectFilter: (projectFilter) => set({ projectFilter }),
      setStatusFilter: (statusFilter) => set({ statusFilter }),
      setSort: (sortBy, sortOrder) => set({ sortBy, sortOrder }),
      setQuery: (query) => set({ query }),
      setStrategy: (strategy) => set({ strategy }),
      selectEntry: (selectedPath) => set({ selectedPath }),

      togglePin: (path) =>
        set((s) => {
          if (s.comparePins.includes(path)) {
            const comparePins = s.comparePins.filter((p) => p !== path);
            // Compare can't stay open with fewer than two pins.
            return {
              comparePins,
              compareOpen: comparePins.length === 2 ? s.compareOpen : false,
            };
          }
          if (s.comparePins.length < 2) {
            return { comparePins: [...s.comparePins, path] };
          }
          return { comparePins: [s.comparePins[0], path] };
        }),

      clearPins: () => set({ comparePins: [], compareOpen: false }),

      canonicalizeRef: (oldRef, path) =>
        set((s) => {
          if (oldRef === path) return s;
          const next: Partial<EntriesState> = {};
          if (s.selectedPath === oldRef) next.selectedPath = path;
          if (s.comparePins.includes(oldRef)) {
            const rewritten = s.comparePins.map((p) =>
              p === oldRef ? path : p,
            );
            // Both slots may now name the same entry — collapse.
            const comparePins = [...new Set(rewritten)];
            next.comparePins = comparePins;
            if (comparePins.length < 2) next.compareOpen = false;
          }
          return Object.keys(next).length ? next : s;
        }),

      setCompareOpen: (compareOpen) => set({ compareOpen }),
      setDiffMode: (diffMode) => set({ diffMode }),
    }),
    {
      name: ENTRIES_STORAGE_KEY,
      partialize: (s) => ({
        typeFilter: s.typeFilter,
        projectFilter: s.projectFilter,
        statusFilter: s.statusFilter,
        sortBy: s.sortBy,
        sortOrder: s.sortOrder,
        strategy: s.strategy,
        selectedPath: s.selectedPath,
        comparePins: s.comparePins,
      }),
      storage: createJSONStorage(() => safeStorage() ?? noopStorage),
      version: 1,
      merge: (persistedState, currentState) => {
        const p = (persistedState ?? {}) as Partial<EntriesState>;
        const pins = Array.isArray(p.comparePins)
          ? p.comparePins.filter((x): x is string => typeof x === "string")
          : [];
        return {
          ...currentState,
          ...p,
          comparePins: pins.slice(0, 2),
          // Ephemeral fields never come from the cache.
          query: "",
          compareOpen: false,
          diffMode: false,
        };
      },
    },
  ),
);

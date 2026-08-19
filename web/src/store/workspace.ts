/**
 * panes-v2 workspace store.
 *
 * Owns the shell's view state: which top-level view is active
 * (overview / focus / session), the sidebar section collapse map,
 * mobile-viewport flag, live-streaming flag, and the local
 * feature→runner assignment map (a client-side optimistic mirror of
 * the assignment API landed in Phase 6).
 *
 * The `dockTree` field is the Focus workspace's docking tree. It is
 * a `DockNode | null` (see `lib/dock.ts`). Phase 7 introduces real
 * mutations — `openInFocus`, `closeLeaf`, `moveLeaf`, etc. — and
 * persists the tree to localStorage.
 *
 * ─── persistence ─────────────────────────────────────────────────
 * Persisted key: `panes-v2:workspace:v1` (unchanged from Phase 3).
 * The persisted shape now includes `dockTree`; if a browser loads a
 * cache that predates Phase 7, `dockTree` is simply absent, defaults
 * back to `null`, and the user's Focus layout starts empty. That's
 * an acceptable one-time loss, not a corruption.
 *
 * If the persisted `dockTree` shape can't be parsed as a valid
 * DockNode, `merge` defensively coerces it to `null` and logs a
 * warning. We do NOT bump the storage key — that would strand
 * pre-Phase-7 view/section/assignment state that is fine.
 */
import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import type { SessionRef } from "../lib/types";
import {
  addLeafAtEdge,
  newLeafNode,
  removeNode as removeDockNode,
  moveLeaf as moveDockLeaf,
  setActiveTab as setDockActiveTab,
  updateSplitRatio as updateDockSplitRatio,
  walkLeaves,
  firstLeaf,
  type DockLeaf,
  type DockNode,
  type Edge,
} from "../lib/dock";

export type { DockNode, DockLeaf, Edge } from "../lib/dock";

/** Versioned localStorage key. Bump the suffix on breaking schema changes. */
export const WORKSPACE_STORAGE_KEY = "panes-v2:workspace:v1";

export type WorkspaceView = "overview" | "focus" | "session" | "entries";

export type SidebarSectionKey = "projects" | "sessions" | "runners";

/**
 * Task-status filter driven by the sidebar chips (All / Active / Ready /
 * Blocked / Done / Archived). Applied to both the sidebar Projects list AND
 * the workspace overview grid — a project is included iff it has at least
 * one task with a matching status. `all` disables the filter entirely.
 */
export type StatusFilter =
  | "all"
  | "active"
  | "ready"
  | "blocked"
  | "done"
  | "archived";

export interface WorkspaceState {
  view: WorkspaceView;
  focusSessionId?: string;
  /** Session-view target when addressed by SessionRef (history mode or
   *  a fully-resolved live ref). Mutually exclusive with
   *  `focusSessionId`, which remains the instance-id fast path used by
   *  the sidebar/MobileNav live rows. */
  focusSessionRef?: SessionRef | null;
  dockTree: DockNode | null;
  /** Last leaf a user interacted with — used as the default drop
   *  target when they invoke `openInFocus` from a non-drag path
   *  (e.g. TaskModal → "Open logs in focus pane"). */
  lastFocusLeafId: string | null;
  sidebarSection: Record<SidebarSectionKey, boolean>;
  /** Whole sidebar collapsed to a slim rail. Independent of the
   *  per-section collapse map. Driven by user toggle in the topbar. */
  sidebarCollapsed: boolean;
  /** Assistant right-rail panel open/closed. */
  assistantOpen: boolean;
  /** Command palette open (⌘K). */
  commandOpen: boolean;
  /** Feature drawer open + which feature it's showing. */
  featureDrawer: { projectId: string; featureId: string } | null;
  /** User theme preference. `system` follows `prefers-color-scheme`. */
  theme: "dark" | "light" | "system";
  mobile: boolean;
  streaming: boolean;
  featureAssignments: Record<string, string>;
  /** Per-project expansion state for the "N merged features" fold
   *  in CardFeatures. Missing key = collapsed (default). */
  mergedExpanded: Record<string, boolean>;
  /** Per-project expansion state for the "N archived tasks" fold
   *  in CardTasks. Missing key = collapsed (default). */
  archivedExpanded: Record<string, boolean>;
  /** Explicitly-hidden project ids. Anything NOT in this set is
   *  visible in the overview grid + sidebar Projects section. */
  hiddenProjects: string[];
  /** Sidebar-chip task-status filter. Narrows both the sidebar Projects
   *  list and the workspace grid to projects with at least one task of
   *  the selected status. `all` disables the filter. */
  statusFilter: StatusFilter;

  // ─── actions: view ────────────────────────────────────────────
  setView(v: WorkspaceView): void;
  setFocusSession(id: string | undefined): void;
  /** Open the session view addressed by a SessionRef (history refs and
   *  task-verb entry points; live rows keep using setFocusSession). */
  openSessionRef(ref: SessionRef): void;
  /** One-shot "focus the composer" intent, set by the Steer verb and
   *  consumed (cleared) by the composer on mount. Ephemeral. */
  steerIntent: boolean;
  setSteerIntent(v: boolean): void;
  toggleSidebarSection(k: SidebarSectionKey): void;
  toggleSidebarCollapsed(): void;
  setAssistantOpen(open: boolean): void;
  toggleAssistant(): void;
  setCommandOpen(open: boolean): void;
  toggleCommand(): void;
  openFeatureDrawer(projectId: string, featureId: string): void;
  closeFeatureDrawer(): void;
  setTheme(t: "dark" | "light" | "system"): void;
  cycleTheme(): void;
  setMobile(m: boolean): void;
  setStreaming(s: boolean): void;
  assignFeature(featureId: string, runnerId: string): void;
  unassignFeature(featureId: string): void;
  toggleMergedExpanded(projectId: string): void;
  toggleArchivedExpanded(projectId: string): void;
  hideProject(projectId: string): void;
  showProject(projectId: string): void;
  toggleProjectVisibility(projectId: string): void;
  hideAllEmpty(projectIds: string[], nonEmpty: string[]): void;
  setStatusFilter(f: StatusFilter): void;

  // ─── actions: dock tree ───────────────────────────────────────
  openInFocus(
    kind: DockLeaf["kind"],
    target: Record<string, unknown>,
    title?: string,
  ): void;
  closeLeaf(leafId: string): void;
  moveLeaf(sourceLeafId: string, targetId: string, edge: Edge): void;
  setSplitRatio(splitId: string, ratio: number): void;
  setActiveTab(tabsId: string, idx: number): void;
  setLastFocusLeaf(leafId: string | null): void;
}

/**
 * Detect whether localStorage is usable (jsdom, node --test, SSR bail).
 */
function safeStorage() {
  if (typeof window === "undefined") return undefined;
  try {
    const probe = "__p2_probe__";
    window.localStorage.setItem(probe, "1");
    window.localStorage.removeItem(probe);
    return window.localStorage;
  } catch {
    return undefined;
  }
}

/**
 * Best-effort validation for a persisted DockNode. Runs on rehydrate.
 * Returns the input if it looks like a valid tree, or `null` if the
 * shape is unrecognized (which lets the user start fresh instead of
 * crashing the whole shell on a stale cache).
 */
function coerceDockTree(raw: unknown): DockNode | null {
  if (raw == null) return null;
  if (typeof raw !== "object") return null;
  const node = raw as Partial<DockNode>;
  if (typeof node.id !== "string") return null;
  if (node.type === "leaf") {
    const leaf = (node as { leaf?: unknown }).leaf;
    if (!leaf || typeof leaf !== "object") return null;
    const l = leaf as Partial<DockLeaf>;
    if (
      l.kind !== "task-detail" &&
      l.kind !== "logs" &&
      l.kind !== "session" &&
      l.kind !== "runners" &&
      l.kind !== "browser" &&
      l.kind !== "entry"
    )
      return null;
    if (typeof l.title !== "string") return null;
    return raw as DockNode;
  }
  if (node.type === "split" || node.type === "tabs") {
    const kids = (node as { children?: unknown }).children;
    if (!Array.isArray(kids)) return null;
    for (const kid of kids) {
      if (coerceDockTree(kid) === null) return null;
    }
    return raw as DockNode;
  }
  return null;
}

export const useWorkspace = create<WorkspaceState>()(
  persist(
    (set, get) => ({
      view: "overview",
      focusSessionId: undefined,
      focusSessionRef: null,
      dockTree: null,
      lastFocusLeafId: null,
      sidebarSection: { projects: true, sessions: true, runners: true },
      sidebarCollapsed: false,
      assistantOpen: false,
      commandOpen: false,
      featureDrawer: null,
      theme: "dark",
      mobile: false,
      streaming: false,
      featureAssignments: {},
      mergedExpanded: {},
      archivedExpanded: {},
      hiddenProjects: [],
      statusFilter: "all" as StatusFilter,

      setView: (v) => set({ view: v }),

      setFocusSession: (id) =>
        set((s) => ({
          focusSessionId: id,
          focusSessionRef: null,
          view: id ? "session" : s.view,
        })),

      openSessionRef: (ref) =>
        set({
          focusSessionRef: ref,
          focusSessionId: undefined,
          view: "session",
        }),

      steerIntent: false,
      setSteerIntent: (v) => set({ steerIntent: v }),

      toggleSidebarSection: (k) =>
        set((s) => ({
          sidebarSection: { ...s.sidebarSection, [k]: !s.sidebarSection[k] },
        })),

      toggleSidebarCollapsed: () =>
        set((s) => ({ sidebarCollapsed: !s.sidebarCollapsed })),

      setAssistantOpen: (open) => set({ assistantOpen: open }),
      toggleAssistant: () => set((s) => ({ assistantOpen: !s.assistantOpen })),

      setCommandOpen: (open) => set({ commandOpen: open }),
      toggleCommand: () => set((s) => ({ commandOpen: !s.commandOpen })),

      openFeatureDrawer: (projectId, featureId) =>
        set({ featureDrawer: { projectId, featureId } }),
      closeFeatureDrawer: () => set({ featureDrawer: null }),

      setTheme: (theme) => set({ theme }),
      cycleTheme: () =>
        set((s) => ({
          theme:
            s.theme === "dark"
              ? "light"
              : s.theme === "light"
                ? "system"
                : "dark",
        })),

      setMobile: (m) => set({ mobile: m }),
      setStreaming: (streaming) => set({ streaming }),

      assignFeature: (featureId, runnerId) =>
        set((s) => ({
          featureAssignments: {
            ...s.featureAssignments,
            [featureId]: runnerId,
          },
        })),

      unassignFeature: (featureId) =>
        set((s) => {
          if (!(featureId in s.featureAssignments)) return s;
          const next = { ...s.featureAssignments };
          delete next[featureId];
          return { featureAssignments: next };
        }),

      toggleMergedExpanded: (projectId) =>
        set((s) => ({
          mergedExpanded: {
            ...s.mergedExpanded,
            [projectId]: !s.mergedExpanded[projectId],
          },
        })),

      toggleArchivedExpanded: (projectId) =>
        set((s) => ({
          archivedExpanded: {
            ...s.archivedExpanded,
            [projectId]: !s.archivedExpanded[projectId],
          },
        })),

      hideProject: (projectId) =>
        set((s) =>
          s.hiddenProjects.includes(projectId)
            ? s
            : { hiddenProjects: [...s.hiddenProjects, projectId] },
        ),

      showProject: (projectId) =>
        set((s) => ({
          hiddenProjects: s.hiddenProjects.filter((p) => p !== projectId),
        })),

      toggleProjectVisibility: (projectId) =>
        set((s) =>
          s.hiddenProjects.includes(projectId)
            ? { hiddenProjects: s.hiddenProjects.filter((p) => p !== projectId) }
            : { hiddenProjects: [...s.hiddenProjects, projectId] },
        ),

      hideAllEmpty: (allProjectIds, nonEmpty) =>
        set(() => {
          const nonEmptySet = new Set(nonEmpty);
          return {
            hiddenProjects: allProjectIds.filter((p) => !nonEmptySet.has(p)),
          };
        }),

      setStatusFilter: (f) => set({ statusFilter: f }),

      // ─── dock tree actions ──────────────────────────────────

      openInFocus: (kind, target, title) => {
        const state = get();
        const leaf: DockLeaf = {
          kind,
          target,
          title: title ?? defaultLeafTitle(kind, target),
        };

        if (state.dockTree === null) {
          const root = newLeafNode(leaf);
          set({
            dockTree: root,
            view: "focus",
            lastFocusLeafId: root.type === "leaf" ? root.id : null,
          });
          return;
        }

        const dropTargetId = pickDropTarget(
          state.dockTree,
          state.lastFocusLeafId,
        );
        if (!dropTargetId) {
          const root = newLeafNode(leaf);
          set({
            dockTree: root,
            view: "focus",
            lastFocusLeafId: root.type === "leaf" ? root.id : null,
          });
          return;
        }

        // "center" means merge into tabs; matches user expectation
        // for e.g. TaskModal → "Open logs in focus".
        const nextTree = addLeafAtEdge(
          state.dockTree,
          dropTargetId,
          "center",
          leaf,
        );
        set({ dockTree: nextTree, view: "focus" });
      },

      closeLeaf: (leafId) => {
        const state = get();
        if (!state.dockTree) return;
        const next = removeDockNode(state.dockTree, leafId);
        set({
          dockTree: next,
          lastFocusLeafId:
            state.lastFocusLeafId === leafId ? null : state.lastFocusLeafId,
        });
      },

      moveLeaf: (sourceLeafId, targetId, edge) => {
        const state = get();
        if (!state.dockTree) return;
        const next = moveDockLeaf(
          state.dockTree,
          sourceLeafId,
          targetId,
          edge,
        );
        set({ dockTree: next });
      },

      setSplitRatio: (splitId, ratio) => {
        const state = get();
        if (!state.dockTree) return;
        set({
          dockTree: updateDockSplitRatio(state.dockTree, splitId, ratio),
        });
      },

      setActiveTab: (tabsId, idx) => {
        const state = get();
        if (!state.dockTree) return;
        set({ dockTree: setDockActiveTab(state.dockTree, tabsId, idx) });
      },

      setLastFocusLeaf: (leafId) => set({ lastFocusLeafId: leafId }),
    }),
    {
      name: WORKSPACE_STORAGE_KEY,
      partialize: (s) => ({
        view: s.view,
        focusSessionId: s.focusSessionId,
        focusSessionRef: s.focusSessionRef,
        sidebarSection: s.sidebarSection,
        sidebarCollapsed: s.sidebarCollapsed,
        theme: s.theme,
        featureAssignments: s.featureAssignments,
        mergedExpanded: s.mergedExpanded,
        archivedExpanded: s.archivedExpanded,
        hiddenProjects: s.hiddenProjects,
        statusFilter: s.statusFilter,
        dockTree: s.dockTree,
        lastFocusLeafId: s.lastFocusLeafId,
      }),
      storage: createJSONStorage(() => safeStorage() ?? noopStorage),
      version: 1,
      merge: (persistedState, currentState) => {
        // Defensive merge: keep known fields, coerce the docktree so
        // a bad cache doesn't crash the shell.
        const p = (persistedState ?? {}) as Partial<WorkspaceState>;
        const tree = coerceDockTree(p.dockTree ?? null);
        if (tree === null && p.dockTree != null) {
          // eslint-disable-next-line no-console
          console.warn(
            "[panes-v2] discarded incompatible persisted dockTree; starting fresh.",
          );
        }
        return {
          ...currentState,
          ...p,
          dockTree: tree,
          lastFocusLeafId:
            tree && p.lastFocusLeafId && leafIdExists(tree, p.lastFocusLeafId)
              ? p.lastFocusLeafId
              : null,
        };
      },
    },
  ),
);

// ─── helpers ─────────────────────────────────────────────────────────

function leafIdExists(tree: DockNode, id: string): boolean {
  let found = false;
  walkLeaves(tree, (_leaf, leafId) => {
    if (leafId === id) found = true;
  });
  return found;
}

/** Return the id of the leaf we should drop a new leaf on top of.
 *  Falls back to the first leaf if the "last touched" id no longer
 *  exists in the tree. */
function pickDropTarget(tree: DockNode, hintId: string | null): string | null {
  if (hintId && leafIdExists(tree, hintId)) return hintId;
  let firstId: string | null = null;
  walkLeaves(tree, (_leaf, id) => {
    if (firstId === null) firstId = id;
  });
  return firstId;
}

function defaultLeafTitle(
  kind: DockLeaf["kind"],
  target: Record<string, unknown>,
): string {
  switch (kind) {
    case "task-detail": {
      const id = (target.taskId ?? target.id) as string | undefined;
      return id ? `Task ${id.slice(0, 8)}` : "Task";
    }
    case "logs": {
      const id = target.taskId as string | undefined;
      return id ? `Logs ${id.slice(0, 8)}` : "Logs";
    }
    case "session":
      return "Session";
    case "runners":
      return "Runners";
    case "browser": {
      const url = target.url as string | undefined;
      if (!url) return "Browser";
      try {
        return new URL(url).host || "Browser";
      } catch {
        return "Browser";
      }
    }
    case "entry": {
      const path = target.path as string | undefined;
      if (!path) return "Entry";
      const base = path.split("/").pop() || path;
      return base.replace(/\.md$/, "");
    }
    default:
      return "Pane";
  }
}

/** Fallback storage used when window.localStorage is unavailable. */
const noopStorage = {
  getItem: () => null,
  setItem: () => undefined,
  removeItem: () => undefined,
};

// firstLeaf is re-exported here for consumers that need it without
// pulling from lib/dock directly (keeps the surface narrow).
export { firstLeaf };

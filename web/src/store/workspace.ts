/**
 * panes-v2 workspace store.
 *
 * Owns the shell's view state: which top-level view is active
 * (overview / focus / session), the sidebar section collapse map,
 * mobile-viewport flag, live-streaming flag, and the local
 * feature→runner assignment map (a client-side optimistic mirror of
 * the assignment API landed in Phase 6).
 *
 * ─── two docks, one engine ─────────────────────────────────────────
 * `docks.focus` and `docks.sidebar` are independent `DockNode | null`
 * trees (see `lib/dock.ts`) driving the Focus tab and the right-side
 * sidebar panel respectively. Both are rendered by the exact same
 * `PaneNode`/`PaneTabs`/`PaneLeaf`/`Splitter` chain — split/tabs/
 * drag-to-rearrange come for free instead of being reimplemented per
 * dock. Every mutating action exists in a Focus/sidebar pair
 * (`openInFocus`/`openInSidebar`, `closeLeaf`/`closeSidebarLeaf`,
 * `moveLeaf`/`moveSidebarLeaf`, `setSplitRatio`/`setSidebarSplitRatio`,
 * `setActiveTab`/`setSidebarActiveTab`) sharing one dockId-generic
 * implementation defined below — see the `make*` factories.
 *
 * The right-side panel replaced an earlier single-item `DrawerState`
 * (a `{kind, ...}` union rendered by `FeatureDrawer.tsx`) that could
 * only show one feature/task/entry/session at a time. That machinery
 * is gone; `FeatureDrawer.tsx` was renamed `SidebarDock.tsx` and now
 * renders `docks.sidebar` the same way `FocusPanes.tsx` renders
 * `docks.focus`. `sidebarDockOpen` is the panel's own visibility gate
 * (mirrors the left `Sidebar`'s `sidebarCollapsed`): opening a leaf
 * into the sidebar flips it true automatically, and the user can also
 * pin it open (while empty) or close it (while populated) manually —
 * closing never discards `docks.sidebar`, it just hides the column.
 *
 * ─── persistence ─────────────────────────────────────────────────
 * Persisted key: `panes-v2:workspace:v1` (unchanged from Phase 3).
 * The persisted shape includes `docks`; a browser loading a cache from
 * before the two-dock generalization has a bare `dockTree` instead —
 * `merge` migrates that into `{ focus: dockTree, sidebar: null }` so a
 * user's saved Focus layout survives the upgrade. If a persisted dock
 * tree can't be parsed as a valid DockNode, `merge` defensively coerces
 * it to `null` and logs a warning rather than crashing the shell.
 */
import { create } from "zustand";
import { persist, createJSONStorage } from "zustand/middleware";
import type { SessionRef } from "../lib/types";
import {
  addNodeAtEdge,
  addSubtreeAtEdge,
  enclosingTabsId,
  evenSplitTree,
  findLeafOfKind,
  findNodeInfo,
  retargetLeaf,
  newLeafNode,
  removeNode as removeDockNode,
  moveLeaf as moveDockLeaf,
  setActiveTab as setDockActiveTab,
  updateSplitRatio as updateDockSplitRatio,
  walkLeaves,
  firstLeaf,
  isDockLeafKind,
  type DockLeaf,
  type DockNode,
  type Edge,
} from "../lib/dock";

export type { DockNode, DockLeaf, Edge } from "../lib/dock";

/** Versioned localStorage key. Bump the suffix on breaking schema changes. */
export const WORKSPACE_STORAGE_KEY = "panes-v2:workspace:v1";

export type WorkspaceView = "overview" | "focus" | "session" | "entries";

export type SidebarSectionKey = "projects" | "sessions" | "runners";

/** Which dock a tree-mutating action targets. Not part of the public
 *  store surface — every action is exposed as a Focus/sidebar pair
 *  instead of taking this as a parameter, so existing call sites never
 *  have to pass it. */
type DockId = "focus" | "sidebar";

/**
 * Task-status filter driven by the sidebar chips (All / Active / Ready /
 * Blocked / Done / Archived). Applied to both the sidebar Projects list AND
 * the workspace overview grid — a project is included iff it has at least
 * one task with a matching status. `all` disables the filter entirely.
 */
export type StatusFilter =
  "all" | "active" | "ready" | "blocked" | "done" | "archived";

/**
 * Clamp a requested drawer width (px) into a usable range. Pure so the
 * store's `setDrawerWidth` and the resize handler both funnel through
 * one testable rule. Defaults to [300, 900].
 */
export function clampDrawerWidth(px: number, min = 300, max = 900): number {
  if (!Number.isFinite(px)) return min;
  return Math.min(max, Math.max(min, px));
}

/**
 * Clamp a requested sidebar width (px) into a usable range. Mirrors
 * `clampDrawerWidth` — same shape, same reasoning. Defaults to
 * [180, 480], centered on the historical fixed 250px sidebar.
 */
export function clampSidebarWidth(px: number, min = 180, max = 480): number {
  if (!Number.isFinite(px)) return min;
  return Math.min(max, Math.max(min, px));
}

/** A copy of `map` without `key`. Returns `map` untouched when absent, so a
 *  no-op never invalidates a zustand selector. */
function omitKey<T>(map: Record<string, T>, key: string): Record<string, T> {
  if (!(key in map)) return map;
  const next = { ...map };
  delete next[key];
  return next;
}

export interface WorkspaceState {
  view: WorkspaceView;
  focusSessionId?: string;
  /** Session-view target when addressed by SessionRef (history mode or
   *  a fully-resolved live ref). Mutually exclusive with
   *  `focusSessionId`, which remains the instance-id fast path used by
   *  the sidebar/MobileNav live rows. */
  focusSessionRef?: SessionRef | null;
  /** The two independent dock trees. `focus` backs the Focus tab;
   *  `sidebar` backs the right-side panel (see module docstring). */
  docks: { focus: DockNode | null; sidebar: DockNode | null };
  /** Last leaf a user interacted with in the Focus dock — used as the
   *  default drop target when they invoke `openInFocus` from a non-drag
   *  path (e.g. TaskModal → "Open logs in focus pane"). Written by
   *  mousing down on a pane AND by `openInFocus`/`openInFocusAt`, which
   *  point it at the leaf they just created so consecutive opens chain
   *  onto the newest pane. It is only ever a hint: nothing outside this
   *  store may treat it as "the id of the leaf that was just opened". */
  lastFocusLeafId: string | null;
  /** Same idea as `lastFocusLeafId`, scoped to the sidebar dock. */
  lastSidebarLeafId: string | null;
  /** Which dock the user touched most recently.
   *
   *  Deliberately NOT persisted: it answers "what am I looking at right
   *  now", and a value restored from a previous session would aim a close
   *  shortcut at a pane the user has not looked at since. */
  lastActiveDock: DockId | null;
  /** Close the pane the user is currently working in. */
  closeCurrentLeaf(): void;
  sidebarSection: Record<SidebarSectionKey, boolean>;
  /** Whole sidebar collapsed to a slim rail. Independent of the
   *  per-section collapse map. Driven by user toggle in the topbar. */
  sidebarCollapsed: boolean;
  /** Assistant right-rail panel open/closed. */
  assistantOpen: boolean;
  /** Command palette open (⌘K). */
  commandOpen: boolean;
  /** Right-side sidebar-dock panel open/closed. This is the panel's own
   *  visibility gate — independent of whether `docks.sidebar` has any
   *  content. Opening a leaf into the sidebar (`openInSidebar`, or a
   *  drop) flips this true automatically; the user can also pin it open
   *  manually (e.g. to see the empty state before dragging something
   *  in) or close it manually. Closing never clears `docks.sidebar` —
   *  the layout is preserved and reappears on reopen, same as the left
   *  `Sidebar`'s `sidebarCollapsed`. */
  sidebarDockOpen: boolean;
  /** Persisted drawer width in px, clamped via `clampDrawerWidth`. */
  drawerWidth: number;
  /** Persisted sidebar width in px, clamped via `clampSidebarWidth`. */
  sidebarWidth: number;
  /** User theme preference. `system` follows `prefers-color-scheme`. */
  theme: "dark" | "light" | "system";
  mobile: boolean;
  streaming: boolean;
  featureAssignments: Record<string, string>;
  /** Per-feature collapse state for a feature's task rows in CardTasks,
   *  nested projectId → featureId → collapsed.
   *
   *  TRI-STATE on purpose: a missing key means "no opinion", and the
   *  view falls back to `isFeatureDone(f)` so finished and merged
   *  features start folded without anyone having to write an entry for
   *  every feature in the project. Only an explicit click stores a
   *  boolean, and it then outranks the default in both directions.
   *
   *  Nested rather than a `${projectId}:${featureId}` composite key so
   *  `forgetProject` can drop a whole project with the same `omitKey`
   *  the other per-project maps use — a composite key would survive it. */
  featureCollapsed: Record<string, Record<string, boolean>>;
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
  setSidebarDockOpen(open: boolean): void;
  toggleSidebarDockOpen(): void;
  setDrawerWidth(px: number): void;
  setSidebarWidth(px: number): void;
  setTheme(t: "dark" | "light" | "system"): void;
  cycleTheme(): void;
  setMobile(m: boolean): void;
  setStreaming(s: boolean): void;
  assignFeature(featureId: string, runnerId: string): void;
  unassignFeature(featureId: string): void;
  /** Flip one feature's task rows. `defaultCollapsed` is the view's
   *  derived state, so the FIRST click always does the visible opposite
   *  of what is on screen rather than of `!undefined`. */
  toggleFeatureCollapsed(
    projectId: string,
    featureId: string,
    defaultCollapsed: boolean,
  ): void;
  hideProject(projectId: string): void;
  showProject(projectId: string): void;
  forgetProject(projectId: string): void;
  toggleProjectVisibility(projectId: string): void;
  hideAllEmpty(projectIds: string[], nonEmpty: string[]): void;
  setStatusFilter(f: StatusFilter): void;

  // ─── actions: focus dock tree ───────────────────────────────────
  openInFocus(
    kind: DockLeaf["kind"],
    target: Record<string, unknown>,
    title?: string,
  ): void;
  /** Target-aware `openInFocus`: place the new leaf at an explicit
   *  node + edge instead of merging into the last-touched pane. This is
   *  the drop path — see `makeOpenInAt` for why it's a second action
   *  rather than optional parameters on `openInFocus`. */
  openInFocusAt(
    kind: DockLeaf["kind"],
    target: Record<string, unknown>,
    title: string | undefined,
    targetNodeId: string,
    edge: Edge,
  ): void;
  /**
   * Open several leaves at once as ONE evenly-split layout, and switch
   * to Focus.
   *
   * This is the action behind the "Watch in Focus" verbs. Chaining
   * `openInFocus` would not do: each call merges into the last-touched
   * pane as a TAB, so a session and its log would stack on top of each
   * other — the opposite of the point. The group is built as a balanced
   * split and docked beside whatever the user already had, so an
   * existing layout is never replaced.
   */
  openInFocusGroup(
    items: Array<{
      kind: DockLeaf["kind"];
      target: Record<string, unknown>;
      title?: string;
    }>,
    dir?: "row" | "col",
  ): void;
  closeLeaf(leafId: string): void;
  moveLeaf(sourceLeafId: string, targetId: string, edge: Edge): void;
  setSplitRatio(splitId: string, ratio: number): void;
  setActiveTab(tabsId: string, idx: number): void;
  setLastFocusLeaf(leafId: string | null): void;

  // ─── actions: sidebar dock tree ─────────────────────────────────
  /** Same shape as `openInFocus`; also flips `sidebarDockOpen` true. */
  openInSidebar(
    kind: DockLeaf["kind"],
    target: Record<string, unknown>,
    title?: string,
  ): void;
  /** Sidebar twin of `openInFocusAt`. */
  openInSidebarAt(
    kind: DockLeaf["kind"],
    target: Record<string, unknown>,
    title: string | undefined,
    targetNodeId: string,
    edge: Edge,
  ): void;
  /**
   * Open a leaf in the sidebar, REUSING the pane of the same kind if one
   * is already docked there.
   *
   * For a viewer pane — one automation at a time — stacking is wrong:
   * clicking through a project's automations would leave a tab per row
   * for the user to clean up, and every one of them a stale snapshot of
   * a row they have moved on from. Panes there is a real reason to have
   * several of (sessions, logs) keep using `openInSidebar`.
   */
  openOrReuseInSidebar(
    kind: DockLeaf["kind"],
    target: Record<string, unknown>,
    title?: string,
  ): void;
  closeSidebarLeaf(leafId: string): void;
  moveSidebarLeaf(sourceLeafId: string, targetId: string, edge: Edge): void;
  setSidebarSplitRatio(splitId: string, ratio: number): void;
  setSidebarActiveTab(tabsId: string, idx: number): void;
  setLastSidebarLeaf(leafId: string | null): void;

  // ─── actions: across the two docks ──────────────────────────────
  /**
   * Move one pane to the other dock, keeping its content and title.
   *
   * Dragging a pane header between the docks already did this; this is
   * the same operation as a click, because the drag is undiscoverable
   * and impossible on a touch screen. `fromDockId` says where the pane
   * lives now — the destination is the other one.
   */
  sendLeafToOtherDock(leafId: string, fromDockId: "focus" | "sidebar"): void;
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
    // Delegate to `isDockLeafKind` rather than repeating the union here.
    // The literal chain this replaces had already drifted: it never
    // learned "automation-runs" or "automation-detail", and an unknown
    // kind returns null, which the split/tabs branch below propagates all
    // the way up — so `merge` threw away the user's ENTIRE persisted dock
    // layout on the next reload, silently, for anyone who left an
    // automation pane docked. One list, one source of truth.
    if (typeof l.kind !== "string" || !isDockLeafKind(l.kind)) return null;
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
    (set, get) => {
      // ─── dockId-generic tree-mutation factories ──────────────────
      // Each pair of public actions (openInFocus/openInSidebar, etc.)
      // shares one of these implementations so the Focus and sidebar
      // reducers cannot drift apart. `dockId` picks which tree in
      // `docks` to read/write and which `lastXLeafId` field tracks the
      // "last touched" drop-target hint.

      const lastLeafField = (
        dockId: DockId,
      ): "lastFocusLeafId" | "lastSidebarLeafId" =>
        dockId === "focus" ? "lastFocusLeafId" : "lastSidebarLeafId";

      const makeOpenIn =
        (dockId: DockId) =>
        (
          kind: DockLeaf["kind"],
          target: Record<string, unknown>,
          title?: string,
        ) => {
          const state = get();
          const leaf: DockLeaf = {
            kind,
            target,
            title: title ?? defaultLeafTitle(kind, target),
          };
          const tree = state.docks[dockId];
          const lastField = lastLeafField(dockId);
          const gateOpen: Partial<WorkspaceState> =
            dockId === "focus" ? { view: "focus" } : { sidebarDockOpen: true };

          const node = newLeafNode(leaf);

          if (tree === null) {
            set({
              docks: { ...state.docks, [dockId]: node },
              [lastField]: node.id,
              lastActiveDock: dockId,
              ...gateOpen,
            } as Partial<WorkspaceState>);
            return;
          }

          const dropTargetId = pickDropTarget(tree, state[lastField]);
          if (!dropTargetId) {
            set({
              docks: { ...state.docks, [dockId]: node },
              [lastField]: node.id,
              lastActiveDock: dockId,
              ...gateOpen,
            } as Partial<WorkspaceState>);
            return;
          }

          // "center" means merge into tabs; matches user expectation
          // for e.g. TaskModal → "Open logs in focus".
          const nextTree = addNodeAtEdge(tree, dropTargetId, "center", node);
          set({
            docks: { ...state.docks, [dockId]: nextTree },
            // Record the leaf we just created as the drop-target hint.
            // Only the two degenerate branches above used to do this,
            // so on a populated dock the hint stayed pinned to whatever
            // pane was last moused down: consecutive opens didn't chain
            // onto the pane just opened, and PaneLeaf read the hint as
            // "the id of the leaf openIn* created" and moved the wrong
            // pane. If the insert somehow no-opped, keep the old hint
            // rather than pointing it at a leaf that isn't in the tree.
            [lastField]: nextTree === tree ? state[lastField] : node.id,
            lastActiveDock: dockId,
            ...gateOpen,
          } as Partial<WorkspaceState>);
        };

      /**
       * Target-aware sibling of `makeOpenIn`: place a NEW leaf at an
       * explicit node + edge in ONE tree write.
       *
       * `openIn*`'s "merge as a tab into the last-touched pane" rule is
       * right for the ~16 menu / command-palette / double-click call
       * sites that have no particular pane in mind, and wrong for a
       * drop, where the user picked both the pane and the edge by hand.
       * Two rules, so two actions — rather than threading a nullable
       * target through every existing caller.
       *
       * This replaces the round-trip `PaneLeaf` used to do (openIn*,
       * then `moveLeaf(lastXLeafId, ...)` to drag the result to the
       * chosen edge). That never worked: `lastXLeafId` was not the new
       * leaf's id, so it shuffled a pre-existing pane instead — and
       * because `moveLeaf` is remove-then-add, a no-op in its add step
       * destroyed a pane outright. A single `addNodeAtEdge` cannot
       * misplace or lose anything: worst case the tree comes back
       * unchanged and the drag simply didn't take.
       *
       * `targetNodeId` is any node id, not only a leaf's — the tab
       * strip drops onto its own tabs container.
       */
      const makeOpenInAt =
        (dockId: DockId) =>
        (
          kind: DockLeaf["kind"],
          target: Record<string, unknown>,
          title: string | undefined,
          targetNodeId: string,
          edge: Edge,
        ) => {
          const state = get();
          const leaf: DockLeaf = {
            kind,
            target,
            title: title ?? defaultLeafTitle(kind, target),
          };
          const tree = state.docks[dockId];
          const lastField = lastLeafField(dockId);
          const gateOpen: Partial<WorkspaceState> =
            dockId === "focus" ? { view: "focus" } : { sidebarDockOpen: true };
          const node = newLeafNode(leaf);

          if (tree === null) {
            set({
              docks: { ...state.docks, [dockId]: node },
              [lastField]: node.id,
              lastActiveDock: dockId,
              ...gateOpen,
            } as Partial<WorkspaceState>);
            return;
          }

          // The pane the user aimed at should still be there, but a
          // close can race a drop. Fall back to the target-less rule
          // (merge as a tab at the last-touched pane) rather than
          // dropping the item on the floor or clobbering the tree.
          const hit = findNodeInfo(tree, targetNodeId) !== null;
          const anchorId = hit
            ? targetNodeId
            : pickDropTarget(tree, state[lastField]);
          if (!anchorId) {
            set({
              docks: { ...state.docks, [dockId]: node },
              [lastField]: node.id,
              lastActiveDock: dockId,
              ...gateOpen,
            } as Partial<WorkspaceState>);
            return;
          }

          const nextTree = addNodeAtEdge(
            tree,
            anchorId,
            hit ? edge : "center",
            node,
          );
          set({
            docks: { ...state.docks, [dockId]: nextTree },
            [lastField]: nextTree === tree ? state[lastField] : node.id,
            lastActiveDock: dockId,
            ...gateOpen,
          } as Partial<WorkspaceState>);
        };

      const makeCloseLeaf = (dockId: DockId) => (leafId: string) => {
        const state = get();
        const tree = state.docks[dockId];
        if (!tree) return;
        const next = removeDockNode(tree, leafId);
        const lastField = lastLeafField(dockId);
        set({
          docks: { ...state.docks, [dockId]: next },
          [lastField]: state[lastField] === leafId ? null : state[lastField],
        } as Partial<WorkspaceState>);
      };

      const makeMoveLeaf =
        (dockId: DockId) =>
        (sourceLeafId: string, targetId: string, edge: Edge) => {
          const state = get();
          const tree = state.docks[dockId];
          if (!tree) return;
          const next = moveDockLeaf(tree, sourceLeafId, targetId, edge);
          set({ docks: { ...state.docks, [dockId]: next } });
        };

      const makeSetSplitRatio =
        (dockId: DockId) => (splitId: string, ratio: number) => {
          const state = get();
          const tree = state.docks[dockId];
          if (!tree) return;
          set({
            docks: {
              ...state.docks,
              [dockId]: updateDockSplitRatio(tree, splitId, ratio),
            },
          });
        };

      const makeSetActiveTab =
        (dockId: DockId) => (tabsId: string, idx: number) => {
          const state = get();
          const tree = state.docks[dockId];
          if (!tree) return;
          const nextTree = setDockActiveTab(tree, tabsId, idx);
          // Clicking a tab is the clearest possible statement of "this is
          // the pane I am working in", and it updated NOTHING before: the
          // tab strip is a sibling of the pane body, so PaneLeaf's
          // onMouseDown never fires for it. The close shortcut would have
          // closed the tab you just clicked away from — and the same gap
          // was already misdirecting openInFocus's merge target.
          const patch: Partial<WorkspaceState> = {
            docks: { ...state.docks, [dockId]: nextTree },
            lastActiveDock: dockId,
          };
          const found = findNodeInfo(nextTree, tabsId);
          const tabs = found?.node;
          if (tabs && tabs.type === "tabs" && tabs.children[idx]) {
            (patch as Record<string, unknown>)[lastLeafField(dockId)] =
              tabs.children[idx].id;
          }
          set(patch);
        };

      const makeSetLastLeaf = (dockId: DockId) => (leafId: string | null) =>
        set({
          [lastLeafField(dockId)]: leafId,
          lastActiveDock: dockId,
        } as Partial<WorkspaceState>);

      return {
        view: "overview",
        focusSessionId: undefined,
        focusSessionRef: null,
        docks: { focus: null, sidebar: null },
        lastFocusLeafId: null,
        lastSidebarLeafId: null,
        lastActiveDock: null,
        sidebarSection: { projects: true, sessions: true, runners: true },
        sidebarCollapsed: false,
        assistantOpen: false,
        commandOpen: false,
        sidebarDockOpen: false,
        drawerWidth: 430,
        sidebarWidth: 250,
        theme: "dark",
        mobile: false,
        streaming: false,
        featureAssignments: {},
        featureCollapsed: {},
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
        toggleAssistant: () =>
          set((s) => ({ assistantOpen: !s.assistantOpen })),

        setCommandOpen: (open) => set({ commandOpen: open }),
        toggleCommand: () => set((s) => ({ commandOpen: !s.commandOpen })),

        setSidebarDockOpen: (open) => set({ sidebarDockOpen: open }),
        toggleSidebarDockOpen: () =>
          set((s) => ({ sidebarDockOpen: !s.sidebarDockOpen })),

        setDrawerWidth: (px) => set({ drawerWidth: clampDrawerWidth(px) }),
        setSidebarWidth: (px) => set({ sidebarWidth: clampSidebarWidth(px) }),

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

        // Writes an explicit boolean even when it equals the default —
        // "the user decided this" has to outlive the feature moving to
        // another lifecycle, or reopening a finished feature's rows
        // would silently re-fold them the moment it merged.
        toggleFeatureCollapsed: (projectId, featureId, defaultCollapsed) =>
          set((s) => {
            const forProject = s.featureCollapsed[projectId] ?? {};
            const current = forProject[featureId] ?? defaultCollapsed;
            return {
              featureCollapsed: {
                ...s.featureCollapsed,
                [projectId]: { ...forProject, [featureId]: !current },
              },
            };
          }),

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

        // forgetProject drops every trace of a project from the persisted
        // shell state. Called after the project is DELETED, not hidden —
        // hide is a view preference about something that still exists.
        //
        // Panes have to close, not merely go stale: a task-detail leaf
        // pointed at a deleted project renders an error state forever, and
        // it survives a reload because the dock tree is persisted. Both
        // docks are swept because a leaf can be dragged between them.
        //
        // The hiddenProjects entry goes too — otherwise a project recreated
        // under the same name comes back invisible, which reads as the
        // delete having half-failed.
        forgetProject: (projectId) =>
          set((s) => {
            const sweep = (tree: DockNode | null): DockNode | null => {
              if (!tree) return null;
              const doomed: string[] = [];
              walkLeaves(tree, (leaf, id) => {
                if (leaf.target?.projectId === projectId) doomed.push(id);
              });
              let next: DockNode | null = tree;
              for (const id of doomed) {
                if (!next) break;
                next = removeDockNode(next, id);
              }
              return next;
            };
            const focus = sweep(s.docks.focus);
            const sidebar = sweep(s.docks.sidebar);
            return {
              hiddenProjects: s.hiddenProjects.filter((p) => p !== projectId),
              featureCollapsed: omitKey(s.featureCollapsed, projectId),
              docks: { focus, sidebar },
              // A leaf id recorded as "last focused" may have just been
              // removed; the dock no longer contains it either way.
              lastFocusLeafId: focus ? s.lastFocusLeafId : null,
              lastSidebarLeafId: sidebar ? s.lastSidebarLeafId : null,
            };
          }),

        toggleProjectVisibility: (projectId) =>
          set((s) =>
            s.hiddenProjects.includes(projectId)
              ? {
                  hiddenProjects: s.hiddenProjects.filter(
                    (p) => p !== projectId,
                  ),
                }
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

        // ─── focus dock tree actions ────────────────────────────
        openInFocus: makeOpenIn("focus"),
        openInFocusAt: makeOpenInAt("focus"),

        openInFocusGroup: (items, dir = "row") => {
          if (items.length === 0) return;
          const state = get();
          const nodes = items.map((it) =>
            newLeafNode({
              kind: it.kind,
              target: it.target,
              title: it.title ?? defaultLeafTitle(it.kind, it.target),
            }),
          );
          // evenSplitTree returns null only for an empty list, which the
          // guard above already excluded.
          const group = evenSplitTree(nodes, dir) as DockNode;
          const tree = state.docks.focus;

          // Nothing docked yet: the group IS the layout.
          if (tree === null) {
            set({
              docks: { ...state.docks, focus: group },
              lastFocusLeafId: nodes[0].id,
              view: "focus",
            });
            return;
          }

          // Otherwise dock it BESIDE the existing layout. Replacing what
          // the user had arranged would be the more obvious
          // implementation and the wrong one — a watch layout is
          // something you add, not a mode you enter.
          const anchor = pickDropTarget(tree, state.lastFocusLeafId);
          const nextTree = anchor
            ? addSubtreeAtEdge(
                tree,
                anchor,
                dir === "row" ? "right" : "bottom",
                group,
              )
            : group;
          set({
            docks: { ...state.docks, focus: nextTree },
            // Point the hint at the first pane of the group when the
            // insert took, so a follow-up open chains onto what was
            // just created rather than the pane it displaced.
            lastFocusLeafId:
              nextTree === tree ? state.lastFocusLeafId : nodes[0].id,
            view: "focus",
          });
        },

        closeLeaf: makeCloseLeaf("focus"),
        moveLeaf: makeMoveLeaf("focus"),
        setSplitRatio: makeSetSplitRatio("focus"),
        setActiveTab: makeSetActiveTab("focus"),
        setLastFocusLeaf: makeSetLastLeaf("focus"),

        // ─── sidebar dock tree actions ──────────────────────────
        openInSidebar: makeOpenIn("sidebar"),
        openInSidebarAt: makeOpenInAt("sidebar"),
        openOrReuseInSidebar: (kind, target, title) => {
          const state = get();
          const tree = state.docks.sidebar;
          const existing = tree ? findLeafOfKind(tree, kind) : null;
          if (!tree || !existing) {
            makeOpenIn("sidebar")(kind, target, title);
            return;
          }
          const leaf: DockLeaf = {
            kind,
            target,
            title: title ?? defaultLeafTitle(kind, target),
          };
          let next = retargetLeaf(tree, existing.id, leaf);
          // Bring it to the front of its strip: a pane updated behind
          // another tab looks like the click did nothing.
          const tabsId = enclosingTabsId(next, existing.id);
          if (tabsId) {
            const info = findNodeInfo(next, tabsId);
            if (info && info.node.type === "tabs") {
              const idx = info.node.children.findIndex(
                (c) => c.id === existing.id,
              );
              if (idx >= 0) next = setDockActiveTab(next, tabsId, idx);
            }
          }
          set({
            docks: { ...state.docks, sidebar: next },
            lastSidebarLeafId: existing.id,
            sidebarDockOpen: true,
          });
        },
        closeSidebarLeaf: makeCloseLeaf("sidebar"),

        // Close the pane the user is working in.
        //
        // The close primitive already existed; what was missing was the
        // REFERENT. Three rules make that safe:
        //
        //  1. Only a dock that is actually ON SCREEN may be closed. The
        //     Focus dock is invisible in Overview/Entries and the sidebar
        //     is invisible when collapsed — and `docks` is persisted, so a
        //     chord pressed in the wrong view would delete a pane the user
        //     cannot see and it would still be gone after a reload.
        //  2. The leaf hint is a HINT (the store's own docstring says so
        //     and it is nullable), so it is resolved through the same
        //     `pickDropTarget` rule the drop path uses: a stale id closes
        //     the frontmost pane rather than silently nothing.
        //  3. Emptying the sidebar also collapses the column, so it cannot
        //     linger as an empty strip the shortcut can no longer clear.
        //     Deliberately NOT folded into makeCloseLeaf: the pane × keeps
        //     its documented layout-preserving behaviour.
        closeCurrentLeaf: () => {
          const state = get();
          const onScreen = (dockId: DockId): boolean =>
            dockId === "focus" ? state.view === "focus" : state.sidebarDockOpen;

          const order: DockId[] =
            state.lastActiveDock === "sidebar"
              ? ["sidebar", "focus"]
              : ["focus", "sidebar"];
          const dockId = order.find(
            (d) => onScreen(d) && state.docks[d] !== null,
          );
          if (!dockId) return;

          const tree = state.docks[dockId];
          if (!tree) return;
          const leafId = pickDropTarget(tree, state[lastLeafField(dockId)]);
          if (!leafId) return;

          makeCloseLeaf(dockId)(leafId);

          if (dockId === "sidebar" && get().docks.sidebar === null) {
            set({ sidebarDockOpen: false });
          }
        },
        moveSidebarLeaf: makeMoveLeaf("sidebar"),
        setSidebarSplitRatio: makeSetSplitRatio("sidebar"),
        setSidebarActiveTab: makeSetActiveTab("sidebar"),
        setLastSidebarLeaf: makeSetLastLeaf("sidebar"),

        // ─── across the two docks ───────────────────────────────
        sendLeafToOtherDock: (leafId, fromDockId) => {
          const state = get();
          const fromTree = state.docks[fromDockId];
          if (!fromTree) return;
          const info = findNodeInfo(fromTree, leafId);
          if (!info || info.node.type !== "leaf") return;
          const leaf = info.node.leaf;
          const toDockId: DockId = fromDockId === "focus" ? "sidebar" : "focus";

          // Remove first, then place — the two trees are independent, so
          // there is no ordering hazard, and doing both in ONE set keeps
          // a pane from flickering out of existence between renders.
          const prunedFrom = removeDockNode(fromTree, leafId);
          const toTree = state.docks[toDockId];
          const node = newLeafNode(leaf);
          const anchor = toTree
            ? pickDropTarget(toTree, state[lastLeafField(toDockId)])
            : null;
          // "center" — arriving as a tab in the destination's current
          // pane, the same rule openIn* uses for a menu-driven open.
          const nextTo =
            toTree && anchor
              ? addNodeAtEdge(toTree, anchor, "center", node)
              : node;

          set({
            docks: {
              ...state.docks,
              [fromDockId]: prunedFrom,
              [toDockId]: nextTo,
            },
            [lastLeafField(fromDockId)]:
              prunedFrom === null ? null : state[lastLeafField(fromDockId)],
            [lastLeafField(toDockId)]: node.id,
            // Take the user where the pane went, so the move is never
            // silent: to Focus by switching view, to the sidebar by
            // making sure the column is open.
            ...(toDockId === "focus"
              ? { view: "focus" }
              : { sidebarDockOpen: true }),
          } as Partial<WorkspaceState>);
        },
      };
    },
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
        featureCollapsed: s.featureCollapsed,
        hiddenProjects: s.hiddenProjects,
        statusFilter: s.statusFilter,
        docks: s.docks,
        lastFocusLeafId: s.lastFocusLeafId,
        lastSidebarLeafId: s.lastSidebarLeafId,
        sidebarDockOpen: s.sidebarDockOpen,
        drawerWidth: s.drawerWidth,
        sidebarWidth: s.sidebarWidth,
      }),
      storage: createJSONStorage(() => safeStorage() ?? noopStorage),
      version: 1,
      merge: (persistedState, currentState) => {
        // Defensive merge: keep known fields, coerce the dock trees so
        // a bad cache doesn't crash the shell.
        const p = (persistedState ?? {}) as Partial<WorkspaceState> & {
          dockTree?: unknown;
        };

        let rawFocus: unknown = null;
        let rawSidebar: unknown = null;
        if (p.docks && typeof p.docks === "object") {
          const d = p.docks as { focus?: unknown; sidebar?: unknown };
          rawFocus = d.focus ?? null;
          rawSidebar = d.sidebar ?? null;
        } else if (Object.prototype.hasOwnProperty.call(p, "dockTree")) {
          // Pre-generalization persisted shape: the one tree there was
          // is the Focus tab's. Migrate it in place; the sidebar starts
          // empty (it didn't exist yet).
          rawFocus = p.dockTree ?? null;
        }

        const focusTree = coerceDockTree(rawFocus);
        if (focusTree === null && rawFocus != null) {
          // eslint-disable-next-line no-console
          console.warn(
            "[panes-v2] discarded incompatible persisted Focus dock tree; starting fresh.",
          );
        }
        const sidebarTree = coerceDockTree(rawSidebar);
        if (sidebarTree === null && rawSidebar != null) {
          // eslint-disable-next-line no-console
          console.warn(
            "[panes-v2] discarded incompatible persisted sidebar dock tree; starting fresh.",
          );
        }

        return {
          ...currentState,
          ...p,
          docks: { focus: focusTree, sidebar: sidebarTree },
          lastFocusLeafId:
            focusTree &&
            p.lastFocusLeafId &&
            leafIdExists(focusTree, p.lastFocusLeafId)
              ? p.lastFocusLeafId
              : null,
          lastSidebarLeafId:
            sidebarTree &&
            p.lastSidebarLeafId &&
            leafIdExists(sidebarTree, p.lastSidebarLeafId)
              ? p.lastSidebarLeafId
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
    case "feature-detail": {
      const id = (target.featureId ?? target.id) as string | undefined;
      return id ? `Feature ${id}` : "Feature";
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
    case "project": {
      const project = target.projectId as string | undefined;
      return project ?? "Project";
    }
    case "automation-runs": {
      const project = target.projectId as string | undefined;
      return project ? `${project} runs` : "Automation runs";
    }
    case "automation-detail": {
      const id = target.automationId as string | undefined;
      return id ? `Automation ${id}` : "Automation";
    }
    case "reminders":
      return "Reminders";
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

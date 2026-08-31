/**
 * SidebarDock — right-side panel hosting the sidebar's own dock tree
 * (`docks.sidebar` in the workspace store), a second independent
 * instance of the exact same dock engine that backs the Focus tab
 * (`lib/dock.ts` + `PaneNode`/`PaneTabs`/`PaneLeaf`/`Splitter`).
 *
 * This replaces the earlier single-item `FeatureDrawer` (a `drawer:
 * DrawerState | null` union that could show only one feature/task/
 * entry/session at a time — see git history on this file's old name,
 * `FeatureDrawer.tsx`, for that version). Multiple items — a live
 * session, a recorded session, a task, a feature — now dock here
 * simultaneously as tabs and/or splits, with the same drag-to-rearrange
 * gestures the Focus tab offers, because it IS the same engine
 * (`dockId="sidebar"` vs `dockId="focus"`).
 *
 * Visibility: driven by `sidebarDockOpen`, a manual pin/collapse toggle
 * (mirrored after the left `Sidebar`'s `sidebarCollapsed`; see the
 * topbar toggle button). Opening a leaf into the sidebar
 * (`openInSidebar`, or a drop) flips `sidebarDockOpen` true
 * automatically. Closing it manually only hides the column — it does
 * NOT clear `docks.sidebar`, so reopening restores the exact same
 * layout, same as the left sidebar's collapse.
 *
 * The aside is drag-resizable from its LEFT edge; the chosen width is
 * persisted (`drawerWidth` in the store, unchanged from the prior
 * round) and applied via a CSS var. CSS classes are kept as
 * `.feature-drawer` / `.drawer-*` (unrenamed) to avoid unrelated CSS
 * churn — they are purely presentational hooks at this point, no
 * longer tied to the retired `FeatureDrawer` concept.
 *
 * Mount strategy: on mobile the panel stays a fixed-position overlay
 * portaled to `document.body` (unchanged, `.feature-drawer` position:
 * fixed override under `body.mobile`). On desktop it is NOT portaled —
 * Dashboard.tsx mounts `<SidebarDock/>` as a direct child of `#app`,
 * and `grid-area: drawer` slots it in as a real, third grid column
 * that pushes the workspace column aside instead of overlaying it.
 */
import { useCallback, useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { useWorkspace } from "../store/workspace";
import { useIsMobile } from "../hooks/useIsMobile";
import { useEdgeResize } from "../hooks/useEdgeResize";
import { PaneNode } from "./Workspace/PaneNode";
import { useDockDrop } from "./Workspace/useDockDrop";

export function SidebarDock(): JSX.Element | null {
  const open = useWorkspace((s) => s.sidebarDockOpen);
  const setOpen = useWorkspace((s) => s.setSidebarDockOpen);
  const sidebarTree = useWorkspace((s) => s.docks.sidebar);
  const drawerWidth = useWorkspace((s) => s.drawerWidth);
  const setDrawerWidth = useWorkspace((s) => s.setDrawerWidth);
  const { dragActive, drop } = useDockDrop("sidebar");
  const isMobile = useIsMobile();

  const [dragover, setDragover] = useState(false);

  // ─── left-edge drag-resize ────────────────────────────────────────
  // The panel is anchored to the right edge (fixed on mobile, the
  // rightmost grid column on desktop), so its width is the distance
  // from the pointer to the viewport's right edge.
  const startResize = useEdgeResize({
    computeWidth: (clientX) => window.innerWidth - clientX,
    onResize: setDrawerWidth,
    bodyClass: "drawer-resizing",
  });

  // ─── catch-all drop target ──────────────────────────────────────────
  // Mirrors FocusPanes.tsx exactly, just bound to dockId "sidebar".
  // Mounted on the whole `<aside>` in BOTH branches, not only the empty
  // state: the panel's header strip, the 12px padding, the pane gutters
  // and the splitters between panes used to reject every drop with no
  // fallback behind them, so a near-miss anywhere in a ~25% inert band
  // simply evaporated. The pane zones `stopPropagation`, so this only
  // fires for genuine misses.
  const handleDragOver = useCallback(
    (e: React.DragEvent) => {
      if (!dragActive) return;
      e.preventDefault();
      e.dataTransfer.dropEffect = "move";
      setDragover(true);
    },
    [dragActive],
  );

  const handleDragLeave = useCallback((e: React.DragEvent) => {
    if (e.currentTarget === e.target) setDragover(false);
  }, []);

  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      setDragover(false);
      drop(null, "center", e);
    },
    [drop],
  );

  // A drag abandoned over the panel (Escape, or a drop on a pane zone,
  // which stops propagation before `dragleave` reaches the aside) never
  // sends the falling-edge event that would clear this. Same reset
  // PaneLeaf/PaneTabs use for their own hover state.
  useEffect(() => {
    if (!dragActive) setDragover(false);
  }, [dragActive]);

  if (!open) return null;
  if (typeof document === "undefined") return null;

  const asideStyle = {
    ["--drawer-w" as never]: `${drawerWidth}px`,
  } as React.CSSProperties;
  const Resizer = <div className="drawer-resizer" onPointerDown={startResize} />;
  // Mobile: portal to document.body (fixed overlay, unchanged from
  // before). Desktop: render in place — Dashboard mounts <SidebarDock/>
  // as a direct child of #app so `grid-area: drawer` applies.
  const wrap = (node: JSX.Element) =>
    isMobile ? createPortal(node, document.body) : node;

  const CloseButton = (
    <button
      className="drawer-close"
      onClick={() => setOpen(false)}
      title="Close panel (layout is preserved)"
      aria-label="Close side panel"
    >
      ×
    </button>
  );

  if (sidebarTree === null) {
    return wrap(
      <aside
        className="feature-drawer"
        style={asideStyle}
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
        onDrop={handleDrop}
      >
        {Resizer}
        <div className="drawer-head">
          <div>
            <div className="drawer-kicker">Side panel</div>
          </div>
          {CloseButton}
        </div>
        <div
          className={"p2-dock-empty" + (dragover ? " dragover" : "")}
          aria-label="Side panel empty state"
          style={{ margin: 0 }}
        >
          <div className="p2-dock-empty__icon" aria-hidden="true">
            ⌗
          </div>
          <div>Side panel is empty.</div>
          <div style={{ fontSize: 11, maxWidth: 260 }}>
            Double-click a task or feature, or drag a task here, to open
            it. Multiple items can dock side by side.
          </div>
        </div>
      </aside>,
    );
  }

  return wrap(
    <aside
      className={"feature-drawer" + (dragover ? " dragover" : "")}
      style={asideStyle}
      onDragOver={handleDragOver}
      onDragLeave={handleDragLeave}
      onDrop={handleDrop}
    >
      {Resizer}
      <div className="drawer-head">
        <div>
          <div className="drawer-kicker">Side panel</div>
        </div>
        {CloseButton}
      </div>
      <div
        style={{
          flex: 1,
          minHeight: 0,
          display: "flex",
          flexDirection: "column",
        }}
      >
        <div className="p2-dock" style={{ margin: 0 }}>
          <PaneNode dockId="sidebar" node={sidebarTree} />
        </div>
      </div>
    </aside>,
  );
}

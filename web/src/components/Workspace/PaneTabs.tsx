/**
 * panes-v2 tabs container.
 *
 * Renders a tab strip on top of the active leaf. Left-click switches
 * tabs, middle-click closes a tab, and dragging the tab acts like
 * dragging a leaf title (same DnD payload).
 *
 * The strip is itself a drop target, equivalent to the active leaf's
 * `center` zone: releasing on it adds the payload to THIS group. It
 * used to be deliberately inert ("dropping onto the strip creates
 * duplication and confusion"), but that reasoning only held while a
 * center drop was unreliable about which pane it merged into — the
 * store ignored the drop target entirely and merged at the last-touched
 * pane. Now that a drop lands where it is aimed, "drop here to add a
 * tab" is unambiguous, and the strip is the obvious place to express
 * it: 26px of chrome sitting directly between the tab the user grabbed
 * and the zones below.
 *
 * Right-click on a tab opens the standard tab-strip menu (close,
 * close others, split right/down). These are dock-store verbs, not
 * entity actions, so the menu is built directly with useContextMenu
 * rather than the lib/actions registry.
 */
import React, { useCallback, useEffect, useState } from "react";
import type { DockNode } from "../../lib/dock";
import { useWorkspace } from "../../store/workspace";
import { useContextMenu } from "../common/ContextMenu";
import {
  beginDrag,
  endDrag,
  type DragPayload,
} from "../../hooks/useDragDrop";
import { PaneLeaf } from "./PaneLeaf";
import { useDockDrop } from "./useDockDrop";

type TabsNode = Extract<DockNode, { type: "tabs" }>;

export function PaneTabs({
  dockId,
  node,
}: {
  dockId: "focus" | "sidebar";
  node: TabsNode;
}): JSX.Element {
  const setFocusActiveTab = useWorkspace((s) => s.setActiveTab);
  const setSidebarActiveTab = useWorkspace((s) => s.setSidebarActiveTab);
  const closeFocusLeaf = useWorkspace((s) => s.closeLeaf);
  const closeSidebarLeaf = useWorkspace((s) => s.closeSidebarLeaf);
  const moveFocusLeaf = useWorkspace((s) => s.moveLeaf);
  const moveSidebarLeaf = useWorkspace((s) => s.moveSidebarLeaf);
  const setActiveTab = dockId === "focus" ? setFocusActiveTab : setSidebarActiveTab;
  const closeLeaf = dockId === "focus" ? closeFocusLeaf : closeSidebarLeaf;
  const moveLeaf = dockId === "focus" ? moveFocusLeaf : moveSidebarLeaf;
  const ctx = useContextMenu();
  const { dragActive, drop } = useDockDrop(dockId);
  const [stripOver, setStripOver] = useState(false);

  // Same falling-edge reset PaneLeaf uses: a drag that ends by Escape
  // or on another target never sends this strip a `dragleave`.
  useEffect(() => {
    if (!dragActive) setStripOver(false);
  }, [dragActive]);

  const active = node.children[node.activeIdx] ?? node.children[0];
  const soloTab = node.children.length < 2;

  const openTabMenu = (e: React.MouseEvent, leafId: string, title: string) => {
    e.preventDefault();
    ctx.open(e.clientX, e.clientY, [
      {
        id: "close",
        label: `Close ${title}`,
        onClick: () => closeLeaf(leafId),
      },
      {
        id: "close-others",
        label: "Close other tabs",
        disabled: soloTab,
        tooltip: soloTab ? "No other tabs in this group" : undefined,
        onClick: () => {
          for (const child of node.children) {
            if (child.id !== leafId) closeLeaf(child.id);
          }
        },
      },
      { id: "sep", separator: true, label: "" },
      // Splitting targets the tabs group itself: the leaf leaves the
      // strip and lands beside it. Meaningless for a lone tab.
      {
        id: "split-right",
        label: "Split right",
        disabled: soloTab,
        tooltip: soloTab ? "Already its own pane" : undefined,
        onClick: () => moveLeaf(leafId, node.id, "right"),
      },
      {
        id: "split-down",
        label: "Split down",
        disabled: soloTab,
        tooltip: soloTab ? "Already its own pane" : undefined,
        onClick: () => moveLeaf(leafId, node.id, "bottom"),
      },
    ]);
  };

  return (
    <div className="p2-pane-tabs">
      <div
        className={
          "p2-pane-tabs__strip" +
          (dragActive && stripOver ? " dragover" : "")
        }
        role="tablist"
        onDragEnter={() => setStripOver(true)}
        onDragOver={(e) => {
          if (!dragActive) return;
          e.preventDefault();
          e.stopPropagation();
          e.dataTransfer.dropEffect = "move";
          // No setState here: `dragenter` already armed the highlight,
          // and `dragover` repeats continuously.
        }}
        onDragLeave={(e) => {
          // Tabs are children of the strip, so a plain `dragleave`
          // fires every time the pointer crosses one. Only clear when
          // the pointer left the strip itself.
          if (e.currentTarget === e.target) setStripOver(false);
        }}
        onDrop={(e) => {
          setStripOver(false);
          // Target the tabs CONTAINER, not the active leaf: "center on
          // this group" appends a tab, which is what the strip means.
          drop(node.id, "center", e);
        }}
      >
        {node.children.map((child, i) => (
          <TabButton
            key={child.id}
            active={i === node.activeIdx}
            title={child.leaf.title}
            onSelect={() => setActiveTab(node.id, i)}
            onClose={() => closeLeaf(child.id)}
            onMenu={(e) => openTabMenu(e, child.id, child.leaf.title)}
            leafId={child.id}
            leafKind={child.leaf.kind}
            leafTarget={child.leaf.target}
            dockId={dockId}
          />
        ))}
      </div>
      <div className="p2-pane-tabs__body">
        {active && (
          <PaneLeaf
            dockId={dockId}
            id={active.id}
            leaf={active.leaf}
            inTabGroup
          />
        )}
      </div>
      {ctx.menu}
    </div>
  );
}

function TabButton({
  active,
  title,
  onSelect,
  onClose,
  onMenu,
  leafId,
  leafKind,
  leafTarget,
  dockId,
}: {
  active: boolean;
  title: string;
  onSelect: () => void;
  onClose: () => void;
  onMenu: (e: React.MouseEvent) => void;
  leafId: string;
  leafKind: TabsNode["children"][number]["leaf"]["kind"];
  leafTarget: Record<string, unknown>;
  dockId: "focus" | "sidebar";
}): JSX.Element {
  const handleDragStart = useCallback(
    (e: React.DragEvent) => {
      const payload: DragPayload = {
        source: "pane-leaf",
        kind: leafKind,
        target: leafTarget,
        title,
        sourceLeafId: leafId,
        sourceDockId: dockId,
      };
      beginDrag(e, payload);
    },
    [leafId, leafKind, leafTarget, title, dockId],
  );

  const handleAuxClick = useCallback(
    (e: React.MouseEvent) => {
      // Middle-click closes the tab. Some browsers still open a link
      // for auxclick even on non-anchors, so guard on button === 1.
      if (e.button === 1) {
        e.preventDefault();
        onClose();
      }
    },
    [onClose],
  );

  return (
    <button
      type="button"
      role="tab"
      aria-selected={active}
      className={"p2-pane-tab" + (active ? " active" : "")}
      draggable
      onDragStart={handleDragStart}
      onDragEnd={endDrag}
      onClick={onSelect}
      onAuxClick={handleAuxClick}
      onContextMenu={onMenu}
    >
      <span title={title}>{title}</span>
      <span
        role="button"
        aria-label={`Close ${title}`}
        className="p2-pane-tab__close"
        onClick={(e) => {
          e.stopPropagation();
          onClose();
        }}
        tabIndex={-1}
      >
        ×
      </span>
    </button>
  );
}

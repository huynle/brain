/**
 * panes-v2 tabs container.
 *
 * Renders a tab strip on top of the active leaf. Left-click switches
 * tabs, middle-click closes a tab, and dragging the tab acts like
 * dragging a leaf title (same DnD payload). The tab strip itself is
 * NOT droppable — dropping onto the strip creates duplication and
 * confusion. Users drop into edges of the active leaf below.
 *
 * Right-click on a tab opens the standard tab-strip menu (close,
 * close others, split right/down). These are dock-store verbs, not
 * entity actions, so the menu is built directly with useContextMenu
 * rather than the lib/actions registry.
 */
import React, { useCallback } from "react";
import type { DockNode } from "../../lib/dock";
import { useWorkspace } from "../../store/workspace";
import { useContextMenu } from "../common/ContextMenu";
import {
  beginDrag,
  endDrag,
  type DragPayload,
} from "../../hooks/useDragDrop";
import { PaneLeaf } from "./PaneLeaf";

type TabsNode = Extract<DockNode, { type: "tabs" }>;

export function PaneTabs({ node }: { node: TabsNode }): JSX.Element {
  const setActiveTab = useWorkspace((s) => s.setActiveTab);
  const closeLeaf = useWorkspace((s) => s.closeLeaf);
  const moveLeaf = useWorkspace((s) => s.moveLeaf);
  const ctx = useContextMenu();

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
      <div className="p2-pane-tabs__strip" role="tablist">
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
          />
        ))}
      </div>
      <div className="p2-pane-tabs__body">
        {active && <PaneLeaf id={active.id} leaf={active.leaf} />}
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
}: {
  active: boolean;
  title: string;
  onSelect: () => void;
  onClose: () => void;
  onMenu: (e: React.MouseEvent) => void;
  leafId: string;
  leafKind: TabsNode["children"][number]["leaf"]["kind"];
  leafTarget: Record<string, unknown>;
}): JSX.Element {
  const handleDragStart = useCallback(
    (e: React.DragEvent) => {
      const payload: DragPayload = {
        source: "pane-leaf",
        kind: leafKind,
        target: leafTarget,
        title,
        sourceLeafId: leafId,
      };
      beginDrag(e, payload);
    },
    [leafId, leafKind, leafTarget, title],
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

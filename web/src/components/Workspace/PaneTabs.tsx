/**
 * panes-v2 tabs container.
 *
 * Renders a tab strip on top of the active leaf. Left-click switches
 * tabs, middle-click closes a tab, and dragging the tab acts like
 * dragging a leaf title (same DnD payload). The tab strip itself is
 * NOT droppable — dropping onto the strip creates duplication and
 * confusion. Users drop into edges of the active leaf below.
 */
import React, { useCallback } from "react";
import type { DockNode } from "../../lib/dock";
import { useWorkspace } from "../../store/workspace";
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

  const active = node.children[node.activeIdx] ?? node.children[0];

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
            leafId={child.id}
            leafKind={child.leaf.kind}
            leafTarget={child.leaf.target}
          />
        ))}
      </div>
      <div className="p2-pane-tabs__body">
        {active && <PaneLeaf id={active.id} leaf={active.leaf} />}
      </div>
    </div>
  );
}

function TabButton({
  active,
  title,
  onSelect,
  onClose,
  leafId,
  leafKind,
  leafTarget,
}: {
  active: boolean;
  title: string;
  onSelect: () => void;
  onClose: () => void;
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

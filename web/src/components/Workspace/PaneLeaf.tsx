/**
 * panes-v2 leaf pane — chrome + content + drop zones.
 *
 * A leaf renders:
 *   • Title bar with pane title + close button. The title bar is the
 *     drag handle (draggable=true on the header).
 *   • Body — the leaf's rendered content, delegated by `kind` to a
 *     concrete leaf component under `./leaves/`.
 *   • 5 drop zones (top/bottom/left/right/center) overlaid on the
 *     WHOLE PANE, active only while a drag this dock accepts is in
 *     progress. Dropping on an edge splits; the center merges into tabs.
 *
 * The overlay is a sibling of the body, not a child of it. `.p2-pane-
 * body` is `overflow: auto`, and an absolutely-positioned child of a
 * scroll container is laid out against the scrolled padding box — so
 * inside the body the overlay slid up by `scrollTop`, off the top of
 * the pane entirely once the content was scrolled by more than one
 * pane-height, leaving the bottom of the visible pane with no drop
 * target at all. Anchored to the leaf it stays put, spans the scrollbar
 * gutter, and makes the title bar droppable too.
 *
 * The leaf never mutates the tree directly — it delegates every
 * operation to the workspace store, which owns the tree.
 */
import React, { useCallback, useEffect, useState } from "react";
import type { DockLeaf, Edge } from "../../lib/dock";
import { useWorkspace } from "../../store/workspace";
import { beginDrag, endDrag, type DragPayload } from "../../hooks/useDragDrop";
import { useDockDrop } from "./useDockDrop";

import { TaskDetailLeaf } from "./leaves/TaskDetailLeaf";
import { FeatureDetailLeaf } from "./leaves/FeatureDetailLeaf";
import { LogsLeaf } from "./leaves/LogsLeaf";
import { SessionLeaf } from "./leaves/SessionLeaf";
import { RunnersLeaf } from "./leaves/RunnersLeaf";
import { BrowserLeaf } from "./leaves/BrowserLeaf";
import { EntryLeaf } from "./leaves/EntryLeaf";
import { AutomationRunsLeaf } from "./leaves/AutomationRunsLeaf";
import { AutomationDetailLeaf } from "./leaves/AutomationDetailLeaf";

export function PaneLeaf({
  dockId,
  id,
  leaf,
  inTabGroup = false,
}: {
  dockId: "focus" | "sidebar";
  id: string;
  leaf: DockLeaf;
  /**
   * True when this pane is the active tab of a strip. An edge drop on a
   * tab retargets to the whole strip (see `addNodeAtEdge`), so the
   * affordance has to say "this tab group", not the tab's own title —
   * otherwise the label promises an operation the dock will not perform.
   */
  inTabGroup?: boolean;
}): JSX.Element {
  const sendToOtherDock = useWorkspace((s) => s.sendLeafToOtherDock);
  const closeFocusLeaf = useWorkspace((s) => s.closeLeaf);
  const closeSidebarLeaf = useWorkspace((s) => s.closeSidebarLeaf);
  const setLastFocusLeaf = useWorkspace((s) => s.setLastFocusLeaf);
  const setLastSidebarLeaf = useWorkspace((s) => s.setLastSidebarLeaf);
  const { dragActive, dragSourceLeafId, drop } = useDockDrop(dockId);

  const closeLeaf = dockId === "focus" ? closeFocusLeaf : closeSidebarLeaf;
  const setLastLeaf = dockId === "focus" ? setLastFocusLeaf : setLastSidebarLeaf;

  // A pane is not a drop target for itself: `moveLeaf` self-guards, so
  // arming these zones would offer full acceptance feedback for a move
  // that is a guaranteed no-op.
  const armed = dragActive && dragSourceLeafId !== id;

  const [hoverEdge, setHoverEdge] = useState<Edge | null>(null);

  // Clear the hover highlight on the falling edge of the drag rather
  // than from the source's `onDragEnd`. `dragend` fires only on the
  // element the drag STARTED from, and unmounting the zones doesn't
  // fire `dragleave` — so a drag ended by Escape, or dropped on another
  // pane, used to leave this pane's `hoverEdge` pinned forever: a
  // permanent orange border, and a zone pre-lit as "selected" on the
  // next drag that the pointer had never entered.
  useEffect(() => {
    if (!armed) setHoverEdge(null);
  }, [armed]);

  const handleDragStart = useCallback(
    (e: React.DragEvent) => {
      const payload: DragPayload = {
        source: "pane-leaf",
        kind: leaf.kind,
        target: leaf.target,
        title: leaf.title,
        sourceLeafId: id,
        sourceDockId: dockId,
      };
      beginDrag(e, payload);
    },
    [id, leaf, dockId],
  );

  const handleDragEnd = useCallback(() => {
    endDrag();
    setHoverEdge(null);
  }, []);

  const dropAtEdge = useCallback(
    (edge: Edge, e: React.DragEvent) => {
      setHoverEdge(null);
      drop(id, edge, e);
    },
    [drop, id],
  );

  const handleClose = useCallback(() => {
    closeLeaf(id);
  }, [closeLeaf, id]);

  const handleSend = useCallback(() => {
    sendToOtherDock(id, dockId);
  }, [sendToOtherDock, id, dockId]);

  const handleFocus = useCallback(() => {
    setLastLeaf(id);
  }, [id, setLastLeaf]);

  return (
    <div
      className={
        "p2-pane-leaf" +
        (hoverEdge === "center" ? " dragover-center" : "")
      }
      onMouseDown={handleFocus}
    >
      <div
        className="p2-pane-header"
        draggable
        onDragStart={handleDragStart}
        onDragEnd={handleDragEnd}
      >
        <span className="p2-pane-header__title" title={leaf.title}>
          {leaf.title}
        </span>
        {/*
          Dragging a pane header from one dock to the other already
          moved it. That is undiscoverable, and impossible on a touch
          screen — so the same operation gets a button. Focus panes send
          to the narrow side panel; side-panel panes get promoted to
          Focus, which is the move you want the moment a transcript
          stops fitting in 430px.
        */}
        <button
          type="button"
          className="p2-pane-header__send"
          onClick={handleSend}
          aria-label={
            dockId === "focus"
              ? `Send ${leaf.title} to the side panel`
              : `Send ${leaf.title} to Focus`
          }
          title={
            dockId === "focus" ? "Send to side panel" : "Send to Focus"
          }
        >
          {dockId === "focus" ? "⇥" : "⤢"}
        </button>
        <button
          type="button"
          className="p2-pane-header__close"
          onClick={handleClose}
          aria-label={`Close ${leaf.title}`}
          title="Close"
        >
          ×
        </button>
      </div>
      <div className="p2-pane-body">
        <LeafContent leaf={leaf} />
      </div>

      {armed && (
        <div className="p2-pane-dropzones active">
          {(["top", "bottom", "left", "right", "center"] as const).map(
            (edge) => (
              <div
                key={edge}
                className={
                  "p2-pane-dropzone " +
                  edge +
                  (hoverEdge === edge ? " over" : "")
                }
                aria-label={dropZoneLabel(edge, leaf.title, inTabGroup)}
                // Set the highlight from `dragenter`, which fires on the
                // new target BEFORE `dragleave` on the old one. Doing it
                // from `dragover` alone made the state go old → null →
                // new across two events, and Chrome throttles `dragover`
                // to ~3Hz when the pointer is still, so the highlight
                // visibly blinked off when crossing a zone boundary.
                onDragEnter={() => setHoverEdge(edge)}
                onDragOver={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  e.dataTransfer.dropEffect = "move";
                  // Only write when it actually changed: `dragover`
                  // repeats continuously, and each call schedules a
                  // render of this leaf's whole subtree.
                  setHoverEdge((cur) => (cur === edge ? cur : edge));
                }}
                onDragLeave={() => {
                  // Debounce: only clear if we're still on this edge.
                  setHoverEdge((cur) => (cur === edge ? null : cur));
                }}
                onDrop={(e) => dropAtEdge(edge, e)}
              >
                {/* Position is otherwise the only thing distinguishing
                    "split" from "merge as a tab". Only the hovered zone
                    reveals its caption, so exactly one is on screen. */}
                <span className="p2-pane-dropzone__hint">
                  {edge === "center"
                    ? "Add as tab"
                    : inTabGroup
                      ? `Split group ${edge}`
                      : `Split ${edge}`}
                </span>
              </div>
            ),
          )}
        </div>
      )}
    </div>
  );
}

/**
 * Accessible name for a drop zone — the zones were unlabelled.
 *
 * Center always merges into the pane dropped on. The four edges split
 * the pane — except when it is a tab, where `addNodeAtEdge` retargets to
 * the strip and the split wraps every tab in it, so the name says so.
 */
function dropZoneLabel(
  edge: Edge,
  paneTitle: string,
  inTabGroup: boolean,
): string {
  if (edge === "center") return `Add as a tab in ${paneTitle}`;
  return inTabGroup
    ? `Split this tab group ${edge}`
    : `Split ${paneTitle} ${edge}`;
}

/** Delegate content rendering by kind. */
function LeafContent({ leaf }: { leaf: DockLeaf }): JSX.Element {
  switch (leaf.kind) {
    case "task-detail":
      return <TaskDetailLeaf target={leaf.target} />;
    case "feature-detail":
      return <FeatureDetailLeaf target={leaf.target} />;
    case "logs":
      return <LogsLeaf target={leaf.target} />;
    case "session":
      return <SessionLeaf target={leaf.target} />;
    case "runners":
      return <RunnersLeaf target={leaf.target} />;
    case "browser":
      return <BrowserLeaf target={leaf.target} />;
    case "entry":
      return <EntryLeaf target={leaf.target} />;
    case "automation-runs":
      return <AutomationRunsLeaf target={leaf.target} />;
    case "automation-detail":
      return <AutomationDetailLeaf target={leaf.target} />;
    default:
      return (
        <div style={{ color: "var(--p2-fg-faint)" }}>
          Unknown leaf kind.
        </div>
      );
  }
}

/**
 * panes-v2 leaf pane — chrome + content + drop zones.
 *
 * A leaf renders:
 *   • Title bar with pane title + close button. The title bar is the
 *     drag handle (draggable=true on the header).
 *   • Body — the leaf's rendered content, delegated by `kind` to a
 *     concrete leaf component under `./leaves/`.
 *   • 5 drop zones (top/bottom/left/right/center) overlaid on the
 *     body, active only while a drag is in progress. Dropping on
 *     an edge splits; dropping in the center merges into tabs.
 *
 * The leaf never mutates the tree directly — it delegates every
 * operation to the workspace store, which owns the tree.
 */
import React, { useCallback, useState } from "react";
import type { DockLeaf, Edge } from "../../lib/dock";
import { useWorkspace } from "../../store/workspace";
import {
  beginDrag,
  endDrag,
  readDragPayload,
  useDragDrop,
  type DragPayload,
} from "../../hooks/useDragDrop";

import { TaskDetailLeaf } from "./leaves/TaskDetailLeaf";
import { LogsLeaf } from "./leaves/LogsLeaf";
import { SessionLeaf } from "./leaves/SessionLeaf";
import { RunnersLeaf } from "./leaves/RunnersLeaf";
import { BrowserLeaf } from "./leaves/BrowserLeaf";

export function PaneLeaf({
  id,
  leaf,
}: {
  id: string;
  leaf: DockLeaf;
}): JSX.Element {
  const closeLeaf = useWorkspace((s) => s.closeLeaf);
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const moveLeaf = useWorkspace((s) => s.moveLeaf);
  const setLastFocusLeaf = useWorkspace((s) => s.setLastFocusLeaf);
  const dragActive = useDragDrop((s) => s.payload !== null);

  const [hoverEdge, setHoverEdge] = useState<Edge | null>(null);

  const handleDragStart = useCallback(
    (e: React.DragEvent) => {
      const payload: DragPayload = {
        source: "pane-leaf",
        kind: leaf.kind,
        target: leaf.target,
        title: leaf.title,
        sourceLeafId: id,
      };
      beginDrag(e, payload);
    },
    [id, leaf],
  );

  const handleDragEnd = useCallback(() => {
    endDrag();
    setHoverEdge(null);
  }, []);

  const dropAtEdge = useCallback(
    (edge: Edge, e: React.DragEvent) => {
      e.preventDefault();
      e.stopPropagation();
      const payload = readDragPayload(e);
      endDrag();
      setHoverEdge(null);
      if (!payload) return;
      if (!isLeafKind(payload.kind)) return;

      // Pane-leaf source → move within tree.
      if (payload.source === "pane-leaf" && payload.sourceLeafId) {
        if (payload.sourceLeafId === id && edge === "center") return;
        moveLeaf(payload.sourceLeafId, id, edge);
      } else {
        // Sidebar row → open + immediately move to the correct edge.
        // Cleanest way: openInFocus (adds at last-focus center), then
        // if the requested edge isn't center, move it to the right
        // spot. To avoid the round-trip we could reach into dock ops
        // directly, but that risks state divergence. This call site
        // is rare (drag from sidebar → edge of a leaf).
        openInFocus(payload.kind, payload.target, payload.title);
        // Moving the just-opened leaf is tricky because we don't have
        // its id here. Accept the fallback: it lands next to the last-
        // focus leaf (which may be this one). The user can drag it
        // to the exact edge from the pane title. This is documented
        // in the phase notes.
        if (edge !== "center") {
          // Try to find the freshly-added leaf and move it.
          // We know it's the "last touched" leaf after openInFocus.
          const nextTree = useWorkspace.getState().dockTree;
          const lastId = useWorkspace.getState().lastFocusLeafId;
          if (nextTree && lastId && lastId !== id) {
            moveLeaf(lastId, id, edge);
          }
        }
      }
    },
    [id, moveLeaf, openInFocus],
  );

  const handleClose = useCallback(() => {
    closeLeaf(id);
  }, [closeLeaf, id]);

  const handleFocus = useCallback(() => {
    setLastFocusLeaf(id);
  }, [id, setLastFocusLeaf]);

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

        {dragActive && (
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
                  onDragOver={(e) => {
                    e.preventDefault();
                    e.stopPropagation();
                    e.dataTransfer.dropEffect = "move";
                    setHoverEdge(edge);
                  }}
                  onDragLeave={() => {
                    // Debounce: only clear if we're still on this edge.
                    setHoverEdge((cur) => (cur === edge ? null : cur));
                  }}
                  onDrop={(e) => dropAtEdge(edge, e)}
                />
              ),
            )}
          </div>
        )}
      </div>
    </div>
  );
}

/** Delegate content rendering by kind. */
function LeafContent({ leaf }: { leaf: DockLeaf }): JSX.Element {
  switch (leaf.kind) {
    case "task-detail":
      return <TaskDetailLeaf target={leaf.target} />;
    case "logs":
      return <LogsLeaf target={leaf.target} />;
    case "session":
      return <SessionLeaf target={leaf.target} />;
    case "runners":
      return <RunnersLeaf target={leaf.target} />;
    case "browser":
      return <BrowserLeaf target={leaf.target} />;
    default:
      return (
        <div style={{ color: "var(--p2-fg-faint)" }}>
          Unknown leaf kind.
        </div>
      );
  }
}

function isLeafKind(kind: DragPayload["kind"]): kind is DockLeaf["kind"] {
  return (
    kind === "task-detail" ||
    kind === "logs" ||
    kind === "session" ||
    kind === "runners" ||
    kind === "browser"
  );
}

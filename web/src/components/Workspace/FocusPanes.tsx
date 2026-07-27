/**
 * panes-v2 Focus workspace root.
 *
 * Renders the current `dockTree` as a recursive `<PaneNode/>` tree.
 * When the tree is null, shows a friendly empty state with a drop
 * zone so users can drag their first pane in from the sidebar.
 *
 * The workspace-level drop handling here is the single place a "drop
 * on empty" turns into `openInFocus`. Nested drops on existing leaves
 * are handled inside each `<PaneLeaf/>`.
 */
import React, { useCallback } from "react";
import { useWorkspace } from "../../store/workspace";
import { PaneNode } from "./PaneNode";
import { readDragPayload, endDrag, type DragPayload } from "../../hooks/useDragDrop";
import type { DockLeaf } from "../../lib/dock";

export function FocusPanes(): JSX.Element {
  const dockTree = useWorkspace((s) => s.dockTree);
  const openInFocus = useWorkspace((s) => s.openInFocus);

  const [dragover, setDragover] = React.useState(false);

  const handleDragOver = useCallback((e: React.DragEvent) => {
    // Preventing default is REQUIRED to enable drop targets.
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    setDragover(true);
  }, []);

  const handleDragLeave = useCallback((e: React.DragEvent) => {
    // Only clear when leaving the element itself, not a bubbling child.
    if (e.currentTarget === e.target) setDragover(false);
  }, []);

  const handleDrop = useCallback(
    (e: React.DragEvent) => {
      e.preventDefault();
      setDragover(false);
      const payload = readDragPayload(e);
      endDrag();
      if (!payload) return;
      if (!isLeafKind(payload.kind)) return;
      // Skip if the drag came from an existing leaf — moving into an
      // empty state doesn't make sense (there's no "other" leaf to
      // move relative to). We just re-open it.
      openInFocus(
        payload.kind,
        payload.target,
        payload.title,
      );
    },
    [openInFocus],
  );

  if (dockTree === null) {
    return (
      <div
        className={"p2-dock-empty" + (dragover ? " dragover" : "")}
        onDragOver={handleDragOver}
        onDragLeave={handleDragLeave}
        onDrop={handleDrop}
        aria-label="Focus workspace empty state"
      >
        <div className="p2-dock-empty__icon" aria-hidden="true">
          ⌗
        </div>
        <div>Focus workspace is empty.</div>
        <div style={{ fontSize: 11, maxWidth: 340 }}>
          Drag a task, session, or runner from the sidebar to open it
          here, or click a task/feature/runner to open its detail in a
          pane.
        </div>
      </div>
    );
  }

  return (
    <div className="p2-dock">
      <PaneNode node={dockTree} />
    </div>
  );
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

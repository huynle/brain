/**
 * panes-v2 — one drop reducer for every dock surface.
 *
 * A leaf's five drop zones, a tabs strip, the dock gutter and the empty
 * state all answer the same three questions — can this payload become a
 * pane, did it come from a pane in THIS dock, and where did the user
 * aim it — so they share one implementation instead of four that drift.
 *
 * `drop(targetNodeId, edge, e)`:
 *   • `targetNodeId` is the node the user aimed at (a leaf id from a
 *     drop zone, a tabs id from a strip). Pass `null` for "no pane in
 *     particular" — the gutter and the empty state — and the store's
 *     last-touched rule picks the spot.
 *   • An external payload (a task row, a command) becomes a new leaf at
 *     exactly that node + edge, via `openIn*At`.
 *   • A `pane-leaf` payload from THIS dock is a pure tree move.
 *   • A `pane-leaf` payload from the OTHER dock is a move too, but it
 *     takes two calls: `moveLeaf` is bound to a single tree and returns
 *     it unchanged when the source id isn't in it, which is why
 *     dragging a pane between the Focus tab and the side panel used to
 *     do nothing at all — or, onto an empty dock, silently duplicate it.
 */
import { useCallback } from "react";
import { isDockLeafKind, type Edge } from "../../lib/dock";
import { useWorkspace } from "../../store/workspace";
import { endDrag, readDragPayload, useDragDrop } from "../../hooks/useDragDrop";

export type DockId = "focus" | "sidebar";

export function useDockDrop(dockId: DockId): {
  /** True only while a drag this dock can actually accept is in flight. */
  dragActive: boolean;
  /**
   * The pane being dragged, when the in-flight drag is a pane from THIS
   * dock — otherwise null. A pane cannot be dropped onto itself
   * (`moveLeaf` self-guards), so surfaces belonging to that pane use
   * this to stay unarmed rather than offering feedback for a move that
   * will not happen. Extracting a tab from its strip is still reachable
   * from the strip's own right-click menu, which targets the strip.
   */
  dragSourceLeafId: string | null;
  drop: (
    targetNodeId: string | null,
    edge: Edge,
    e: React.DragEvent,
  ) => void;
} {
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const openInSidebar = useWorkspace((s) => s.openInSidebar);
  const openInFocusAt = useWorkspace((s) => s.openInFocusAt);
  const openInSidebarAt = useWorkspace((s) => s.openInSidebarAt);
  const moveFocusLeaf = useWorkspace((s) => s.moveLeaf);
  const moveSidebarLeaf = useWorkspace((s) => s.moveSidebarLeaf);
  const closeFocusLeaf = useWorkspace((s) => s.closeLeaf);
  const closeSidebarLeaf = useWorkspace((s) => s.closeSidebarLeaf);
  const payload = useDragDrop((s) => s.payload);

  // Gate the drop-zone affordance on ACCEPTANCE, not on "some drag is
  // happening". The old `payload !== null` test lit up all five dashed
  // rectangles for a feature→runner "assign" drag, which every dock
  // surface then rejected — the user saw full acceptance feedback and
  // got nothing on release.
  const dragActive = payload !== null && isDockLeafKind(payload.kind);

  const dragSourceLeafId =
    dragActive &&
    payload.source === "pane-leaf" &&
    payload.sourceLeafId &&
    // Same "assume local when unstamped" rule `drop` applies below.
    (!payload.sourceDockId || payload.sourceDockId === dockId)
      ? payload.sourceLeafId
      : null;

  const drop = useCallback(
    (targetNodeId: string | null, edge: Edge, e: React.DragEvent) => {
      e.preventDefault();
      e.stopPropagation();
      const p = readDragPayload(e);
      endDrag();
      if (!p) return;
      if (!isDockLeafKind(p.kind)) return;

      const openIn = dockId === "focus" ? openInFocus : openInSidebar;
      const openInAt = dockId === "focus" ? openInFocusAt : openInSidebarAt;
      const moveLeaf = dockId === "focus" ? moveFocusLeaf : moveSidebarLeaf;

      if (p.source === "pane-leaf" && p.sourceLeafId) {
        // A payload minted before `sourceDockId` existed (or by a
        // future source that forgets it) is assumed local — that was
        // the old behaviour and it is the common case.
        const sameDock = !p.sourceDockId || p.sourceDockId === dockId;
        if (sameDock) {
          // Nothing to move relative to on the gutter/empty state, so
          // leave the pane where it is rather than teleporting it.
          if (targetNodeId) moveLeaf(p.sourceLeafId, targetNodeId, edge);
          return;
        }
        // Cross-dock: drop it from the dock it came from first, then
        // fall through to re-open it here. Order matters only in that
        // the two docks are independent trees, so `targetNodeId` is
        // still valid after the close.
        const closeInSource =
          p.sourceDockId === "focus" ? closeFocusLeaf : closeSidebarLeaf;
        closeInSource(p.sourceLeafId);
      }

      if (targetNodeId) openInAt(p.kind, p.target, p.title, targetNodeId, edge);
      else openIn(p.kind, p.target, p.title);
    },
    [
      dockId,
      openInFocus,
      openInSidebar,
      openInFocusAt,
      openInSidebarAt,
      moveFocusLeaf,
      moveSidebarLeaf,
      closeFocusLeaf,
      closeSidebarLeaf,
    ],
  );

  return { dragActive, dragSourceLeafId, drop };
}

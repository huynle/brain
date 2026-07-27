/**
 * panes-v2 drag/drop coordinator hook.
 *
 * We use the HTML5 native drag-and-drop API (draggable + dragstart /
 * dragover / drop) for cross-widget drags, not a library. A tiny
 * zustand store carries the current payload during a drag so any
 * component in the tree can inspect what's being dragged — even
 * across iframe or portal boundaries where `dataTransfer` is unreliable.
 *
 * dataTransfer.setData("application/x-p2-drag", <json>) is still the
 * canonical channel (it survives across browser-security boundaries),
 * but reading the payload back on `dragover` is intentionally
 * unreliable across browsers — hence the shadow store.
 *
 * Payload sources:
 *   - "task-row" / "feature-row" / "session-row" / "runner-row" — sidebar drags
 *   - "feature-header" — Phase 8: dragging a project-card feature row
 *                        onto a sidebar runner assigns it. Kept distinct
 *                        from "feature-row" so drop targets can filter:
 *                        runners only accept feature-header drops.
 *   - "pane-leaf" — dragging an existing focus pane title
 *
 * Payload targets:
 *   - The Focus workspace empty state  → openInFocus(kind, target)
 *   - A leaf's edge drop-zone           → openInFocus + edge split /
 *                                        moveLeaf(sourceLeafId, ...)
 *   - A sidebar runner row (Phase 8)   → assignFeatureToRunner
 *
 * The store is intentionally NOT persisted; drags are ephemeral.
 */
import { create } from "zustand";
import type { DockLeaf } from "../lib/dock";

/** All drag sources the panes-v2 workspace understands. */
export type DragSource =
  | "task-row"
  | "feature-row"
  | "feature-header"
  | "session-row"
  | "runner-row"
  | "pane-leaf";

/**
 * The payload carried during a drag. For sidebar rows and TaskModal
 * links `kind` is one of the leaf kinds; when the drag is a runner-
 * assignment operation (Phase 8) `kind` is "assign" and `source` is
 * "feature-header".
 *
 * For "feature-header" drags `target` carries:
 *   { projectId: string, featureId: string, currentRunnerId?: string }
 * so the drop handler knows which project/feature to assign and can
 * short-circuit no-op drops (dropping onto the runner already assigned).
 */
export interface DragPayload {
  source: DragSource;
  /** Either a leaf kind (open a pane) or "assign" (drag feature→runner). */
  kind: DockLeaf["kind"] | "assign";
  target: Record<string, unknown>;
  title: string;
  /** When source === "pane-leaf", this is the leaf's id in the dock tree.
   *  Consumers use this to distinguish a move from an open. */
  sourceLeafId?: string;
}

/** The MIME type we register so cross-frame drags work. */
export const DRAG_MIME = "application/x-p2-drag";

interface DragState {
  payload: DragPayload | null;
  start(p: DragPayload): void;
  end(): void;
}

export const useDragDrop = create<DragState>((set) => ({
  payload: null,
  start: (p) => set({ payload: p }),
  end: () => set({ payload: null }),
}));

/**
 * Helper: attach payload to a native drag event AND publish to the store.
 * Call this from `onDragStart` on the source element.
 */
export function beginDrag(
  ev: React.DragEvent,
  payload: DragPayload,
): void {
  try {
    ev.dataTransfer.effectAllowed = "move";
    ev.dataTransfer.setData(DRAG_MIME, JSON.stringify(payload));
    // Fallback for browsers that don't preserve custom MIME on cross-
    // frame drops.
    ev.dataTransfer.setData("text/plain", payload.title);
  } catch {
    // ignore — the shadow store still has the payload.
  }
  useDragDrop.getState().start(payload);
}

/**
 * Helper: read the payload during dragover/drop. Prefers dataTransfer
 * (works across iframes) but falls back to the shadow store when the
 * MIME wasn't preserved by the browser.
 */
export function readDragPayload(ev: React.DragEvent): DragPayload | null {
  try {
    const raw = ev.dataTransfer.getData(DRAG_MIME);
    if (raw) return JSON.parse(raw) as DragPayload;
  } catch {
    // fall through to store
  }
  return useDragDrop.getState().payload;
}

/**
 * Helper: end the drag (clear the store). Call from `onDragEnd` on the
 * source element.
 */
export function endDrag(): void {
  useDragDrop.getState().end();
}

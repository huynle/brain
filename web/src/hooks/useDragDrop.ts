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
 * Payload targets — every dock surface routes through one reducer,
 * `Workspace/useDockDrop.ts`; only the runner row is separate:
 *   - A leaf's edge drop-zone, a tabs strip, the dock gutter, or the
 *     empty state → openIn*At(kind, target, title, targetNodeId, edge)
 *     for an external payload, moveLeaf for a "pane-leaf" one
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
  /** When source === "pane-leaf", which dock the leaf currently lives
   *  in. There are two independent trees (`docks.focus` and
   *  `docks.sidebar`) and every tree op is bound to one of them, so a
   *  drop target has no other way to tell a rearrange-in-place from a
   *  move across docks — it would just call `moveLeaf` with an id the
   *  destination tree has never heard of and silently no-op. */
  sourceDockId?: "focus" | "sidebar";
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
 * Backstop: clear the payload when the pointer's drag ends, whatever it
 * ended on. Every drop target calls `endDrag()` itself, but a drag can
 * also end by Escape or by releasing over something that isn't a target
 * at all, and then only the SOURCE element's `onDragEnd` runs — so a
 * source that unmounted mid-drag (an SSE-driven list re-sort, say)
 * leaves the payload set forever. A stuck payload keeps `.p2-pane-
 * dropzones.active` armed with `pointer-events: auto` on every pane in
 * both docks, which swallows clicks until the next complete drag.
 *
 * `dragend` is capture-phase (it carries nothing anyone reads).
 * `drop` is bubble-phase deliberately: `readDragPayload` falls back to
 * this store when the browser didn't preserve our custom MIME type, so
 * clearing before a target's own handler ran would break that fallback.
 * Targets that call `stopPropagation()` never reach this listener —
 * they clear the payload themselves.
 */
if (typeof window !== "undefined") {
  const clear = () => useDragDrop.getState().end();
  window.addEventListener("dragend", clear, true);
  window.addEventListener("drop", clear, false);
}

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

// Drag-resize handles for the pane layout. Two variants:
//
//   PaneSplitterRow      — horizontal bar between the top list and the
//                          bottom row; drags vertically to change the
//                          bottom row's height.
//   PaneSplitterColumn   — vertical bar between Detail and Logs; drags
//                          horizontally to change the detail/logs ratio.
//
// Pattern matches AssistantSplitter: pointer events with setPointerCapture,
// document-level listeners while dragging so the gesture survives the
// cursor leaving the 4px hit area. Double-click resets to default.

import { useEffect, useRef } from "react";
import { useUI } from "../../store/ui";
import { BOTTOM_HEIGHT_BOUNDS, DETAIL_LOGS_RATIO_BOUNDS } from "../../lib/paneNav";

// ─── Row splitter (drag to change bottom-row HEIGHT) ────────────────────────

export function PaneSplitterRow() {
  const setBottomHeight = useUI((s) => s.setBottomHeight);
  const dragging = useRef(false);

  useEffect(() => {
    function onMove(e: PointerEvent) {
      if (!dragging.current) return;
      // The bottom row is anchored to the bottom of the viewport. Its
      // height equals (innerHeight - cursor.y). The store clamps to
      // [min, viewportH * maxRatio] so we don't have to do it here.
      const next = window.innerHeight - e.clientY;
      setBottomHeight(next);
    }
    function onUp() {
      if (!dragging.current) return;
      dragging.current = false;
      document.body.classList.remove("resizing-pane");
    }
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onUp);
    return () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onUp);
    };
  }, [setBottomHeight]);

  return (
    <div
      role="separator"
      aria-orientation="horizontal"
      aria-label="Resize detail/logs row height (drag, or Alt+J/K)"
      className="pane-splitter pane-splitter-row"
      onPointerDown={(e) => {
        (e.target as HTMLElement).setPointerCapture(e.pointerId);
        dragging.current = true;
        document.body.classList.add("resizing-pane");
      }}
      onDoubleClick={() => setBottomHeight(BOTTOM_HEIGHT_BOUNDS.default)}
    />
  );
}

// ─── Column splitter (drag to change detail/logs RATIO) ─────────────────────

// The column splitter is rendered as a sibling between the Detail and Logs
// Panels inside .tui-bottom. It needs the bounding rect of that container
// to translate cursor X into a ratio, so we accept a ref to the container.
//
// Why not measure off the splitter element itself? Because the detail
// pane's width changes during drag, which would move the splitter too,
// breaking incremental drag math. The container rect is stable.
export function PaneSplitterColumn({
  containerRef,
}: {
  containerRef: React.RefObject<HTMLDivElement>;
}) {
  const setRatio = useUI((s) => s.setDetailLogsRatio);
  const dragging = useRef(false);

  useEffect(() => {
    function onMove(e: PointerEvent) {
      if (!dragging.current) return;
      const el = containerRef.current;
      if (!el) return;
      const rect = el.getBoundingClientRect();
      if (rect.width <= 0) return;
      // Translate the cursor's x position within the container into a
      // 0..1 fraction. Store clamps to [0.2, 0.8].
      const ratio = (e.clientX - rect.left) / rect.width;
      setRatio(ratio);
    }
    function onUp() {
      if (!dragging.current) return;
      dragging.current = false;
      document.body.classList.remove("resizing-pane-col");
    }
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onUp);
    return () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onUp);
    };
  }, [setRatio, containerRef]);

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize detail/logs split (drag, or Alt+H/L)"
      className="pane-splitter pane-splitter-col"
      onPointerDown={(e) => {
        (e.target as HTMLElement).setPointerCapture(e.pointerId);
        dragging.current = true;
        document.body.classList.add("resizing-pane-col");
      }}
      onDoubleClick={() => setRatio(DETAIL_LOGS_RATIO_BOUNDS.default)}
    />
  );
}

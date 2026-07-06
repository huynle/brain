import { useEffect, useRef } from "react";
import { ASSISTANT_WIDTH_BOUNDS, useUI } from "../../store/ui";

// Draggable vertical separator between the main content area and the
// assistant sidebar. Uses pointer events so a single handler covers mouse,
// pen, and touch. While dragging, attaches listeners to the document so the
// gesture survives the cursor leaving the 4px-wide handle.
export function AssistantSplitter() {
  const setWidth = useUI((s) => s.setAssistantWidth);
  const dragging = useRef(false);

  useEffect(() => {
    function onMove(e: PointerEvent) {
      if (!dragging.current) return;
      // The sidebar is anchored to the right edge of the viewport, so the
      // width corresponds to (window.innerWidth - cursor.x).
      const next = window.innerWidth - e.clientX;
      const { min, max } = ASSISTANT_WIDTH_BOUNDS;
      if (next < min) {
        setWidth(min);
        return;
      }
      if (next > max) {
        setWidth(max);
        return;
      }
      setWidth(next);
    }
    function onUp() {
      if (!dragging.current) return;
      dragging.current = false;
      document.body.classList.remove("resizing-assistant");
    }
    window.addEventListener("pointermove", onMove);
    window.addEventListener("pointerup", onUp);
    window.addEventListener("pointercancel", onUp);
    return () => {
      window.removeEventListener("pointermove", onMove);
      window.removeEventListener("pointerup", onUp);
      window.removeEventListener("pointercancel", onUp);
    };
  }, [setWidth]);

  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize assistant sidebar"
      className="assistant-splitter"
      onPointerDown={(e) => {
        // Capture so the splitter keeps receiving the move/up events even
        // when the cursor sweeps onto a child or sibling element.
        (e.target as HTMLElement).setPointerCapture(e.pointerId);
        dragging.current = true;
        document.body.classList.add("resizing-assistant");
      }}
      onDoubleClick={() => setWidth(ASSISTANT_WIDTH_BOUNDS.default)}
    />
  );
}

import { useEffect, useRef, useState } from "react";
import { useUI } from "../store/ui";
import { useViewport } from "../hooks/useViewport";
import { AssistantPanel } from "./AssistantPanel";

// Threshold (px) for the bottom-sheet drag-to-dismiss gesture. The user must
// drag the handle down at least this far for the sheet to close on release.
const SHEET_DISMISS_PX = 90;

// Overlay host for the Brain Assistant. Rendered as a slide-up bottom sheet on
// mobile and as a right-side drawer on narrow desktop viewports (where the
// persistent sidebar would crowd the main content). On wide viewports this
// component still renders if the user explicitly opens the overlay (e.g. with
// Cmd/Ctrl+. while the sidebar is collapsed) so there's always a way to reach
// the assistant. The persistent sidebar is the AssistantSidebar component.
export function AssistantDrawer() {
  const tier = useViewport();
  const open = useUI((s) => s.assistantOpen);
  const setOpen = useUI((s) => s.setAssistantOpen);
  const isMobile = tier === "mobile";

  const [dragY, setDragY] = useState(0);
  const dragStartY = useRef<number | null>(null);

  function onClose() {
    setOpen(false);
  }

  // Reset the drag offset whenever the sheet (re-)opens.
  useEffect(() => {
    if (open) setDragY(0);
  }, [open]);

  // Android/Chrome back-button integration: while open, push a history entry so
  // the user's natural "back" gesture closes the assistant instead of leaving
  // the page. We mark the entry so popstate only acts on our own state.
  useEffect(() => {
    if (!open) return;
    const marker = { __brainAssistant: true } as const;
    try {
      window.history.pushState(marker, "");
    } catch {
      /* private mode / sandboxed iframe — back-button integration just no-ops */
    }
    function onPop() {
      // Any popstate while we're open means the user navigated back: close.
      setOpen(false);
    }
    window.addEventListener("popstate", onPop);
    return () => {
      window.removeEventListener("popstate", onPop);
      // If we're unmounting while still "open" (e.g. parent forced close) and
      // our marker is still on top of the history stack, pop it so we don't
      // leave dead entries that swallow a future back press.
      if (
        window.history.state &&
        (window.history.state as { __brainAssistant?: boolean }).__brainAssistant
      ) {
        try {
          window.history.back();
        } catch {
          /* ignore */
        }
      }
    };
  }, [open, setOpen]);

  if (!open) return null;

  // Drag-to-dismiss handlers for the mobile bottom sheet's drag handle.
  // Only downward drag is honored — upward drag clamps to 0 so the sheet can't
  // be pushed past its anchored top edge. On release, if the user dragged past
  // SHEET_DISMISS_PX we close; otherwise we snap back to 0.
  function onHandleTouchStart(e: React.TouchEvent) {
    if (e.touches.length !== 1) {
      dragStartY.current = null;
      return;
    }
    dragStartY.current = e.touches[0].clientY;
  }
  function onHandleTouchMove(e: React.TouchEvent) {
    const start = dragStartY.current;
    if (start == null) return;
    const dy = e.touches[0].clientY - start;
    setDragY(dy > 0 ? dy : 0);
  }
  function onHandleTouchEnd() {
    const dy = dragY;
    dragStartY.current = null;
    if (dy >= SHEET_DISMISS_PX) {
      setOpen(false);
      return;
    }
    setDragY(0);
  }

  const surfaceClass = isMobile ? "assistant-sheet" : "assistant-drawer";
  const surfaceStyle =
    isMobile && dragY > 0 ? { transform: `translateY(${dragY}px)` } : undefined;

  return (
    <div
      className={`assistant-shell ${isMobile ? "mobile" : ""}`}
      role="dialog"
      aria-label="Brain Assistant"
      aria-modal="true"
    >
      <div className="assistant-backdrop" onClick={onClose} />
      <aside className={surfaceClass} style={surfaceStyle}>
        {isMobile && (
          <div
            className="assistant-grabber"
            role="button"
            aria-label="Drag down to close assistant"
            onTouchStart={onHandleTouchStart}
            onTouchMove={onHandleTouchMove}
            onTouchEnd={onHandleTouchEnd}
            onTouchCancel={onHandleTouchEnd}
          >
            <span className="assistant-grabber-bar" />
          </div>
        )}
        <AssistantPanel
          active
          className="assistant-panel-overlay"
          headerActions={
            <button
              className="icon-btn"
              onClick={onClose}
              title="Close assistant"
              aria-label="Close"
            >
              ×
            </button>
          }
        />
      </aside>
    </div>
  );
}

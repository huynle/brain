/**
 * useEdgeResize — shared pointer-capture drag-resize dance.
 *
 * Both the drawer (left-edge handle, anchored to the viewport's right
 * edge) and the sidebar (right-edge handle, anchored to the left edge)
 * need the same mechanics: capture the pointer on down, translate
 * pointermove into a width via a caller-supplied projection, forward it
 * to a setter, toggle a body class for the global `cursor: col-resize` +
 * `user-select: none` treatment while dragging, and clean everything up
 * on pointerup (or on unmount, if the drag is still in flight).
 *
 * Callers differ only in `computeWidth` (the pointer→width projection)
 * and `bodyClass` (which CSS hook drives the drag visuals).
 */
import { useCallback, useEffect } from "react";

export interface UseEdgeResizeOptions {
  /** Project a pointermove's clientX into the new width, in px. */
  computeWidth: (clientX: number) => number;
  /** Store setter (already clamps internally). */
  onResize: (px: number) => void;
  /** Body class toggled for the duration of the drag, e.g.
   *  "drawer-resizing" / "sidebar-resizing". */
  bodyClass: string;
}

export function useEdgeResize({
  computeWidth,
  onResize,
  bodyClass,
}: UseEdgeResizeOptions): (e: React.PointerEvent<HTMLDivElement>) => void {
  const onPointerDown = useCallback(
    (e: React.PointerEvent<HTMLDivElement>) => {
      e.preventDefault();
      const handle = e.currentTarget;
      try {
        handle.setPointerCapture(e.pointerId);
      } catch {
        /* jsdom / non-capturing envs — safe to ignore */
      }
      document.body.classList.add(bodyClass);
      handle.classList.add("dragging");

      const onMove = (ev: PointerEvent) => {
        onResize(computeWidth(ev.clientX));
      };
      const onUp = (ev: PointerEvent) => {
        try {
          handle.releasePointerCapture(ev.pointerId);
        } catch {
          /* ignore */
        }
        document.body.classList.remove(bodyClass);
        handle.classList.remove("dragging");
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onUp);
      };
      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onUp);
    },
    [computeWidth, onResize, bodyClass],
  );

  // Belt-and-braces cleanup: if the owning component unmounts mid-drag,
  // drop the body class so the page doesn't get stuck with col-resize /
  // no-select.
  useEffect(() => {
    return () => {
      if (typeof document !== "undefined") {
        document.body.classList.remove(bodyClass);
      }
    };
  }, [bodyClass]);

  return onPointerDown;
}

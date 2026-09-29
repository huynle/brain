import { useEffect, useRef, useState, type CSSProperties, type PointerEvent, type MouseEvent } from "react";

type Position = { x: number; y: number };
const key = "brain.assistant-shortcut-position";
const margin = 12;
function readPosition(): Position | null {
  try {
    const p = JSON.parse(localStorage.getItem(key) || "null");
    return p && Number.isFinite(p.x) && Number.isFinite(p.y)
      ? { x: Math.max(0, Math.min(1, p.x)), y: Math.max(0, Math.min(1, p.y)) }
      : null;
  } catch { return null; }
}
function bounds() {
  const v = window.visualViewport;
  return { left: (v?.offsetLeft || 0) + margin, top: (v?.offsetTop || 0) + margin,
    width: Math.max(0, (v?.width || window.innerWidth) - 56 - margin * 2),
    height: Math.max(0, (v?.height || window.innerHeight) - 56 - margin * 2) };
}

/** Keep the user's preferred position while temporarily clamping for the keyboard. */
export function useDraggableShortcut(open: () => void) {
  const [position, setPosition] = useState(readPosition);
  const [, resize] = useState(0);
  const drag = useRef<{ id: number; x: number; y: number; left: number; top: number; moved: boolean; before: Position | null; next: Position | null } | null>(null);
  const suppressClick = useRef(false);
  useEffect(() => {
    const update = () => resize(n => n + 1);
    window.addEventListener("resize", update);
    window.visualViewport?.addEventListener("resize", update);
    window.visualViewport?.addEventListener("scroll", update);
    return () => {
      window.removeEventListener("resize", update);
      window.visualViewport?.removeEventListener("resize", update);
      window.visualViewport?.removeEventListener("scroll", update);
    };
  }, []);
  const b = bounds();
  const style: CSSProperties | undefined = position ? {
    left: `clamp(calc(env(safe-area-inset-left) + 12px), ${b.left + position.x * b.width}px, calc(100vw - env(safe-area-inset-right) - 68px))`,
    top: `clamp(calc(env(safe-area-inset-top) + 12px), ${b.top + position.y * b.height}px, calc(100dvh - env(safe-area-inset-bottom) - 68px))`,
    bottom: "auto", transform: "none",
  } : undefined;
  return {
    style,
    onPointerDown(e: PointerEvent<HTMLButtonElement>) {
      if (!e.isPrimary || e.button !== 0) return;
      const rect = e.currentTarget.getBoundingClientRect();
      suppressClick.current = false;
      drag.current = { id: e.pointerId, x: e.clientX, y: e.clientY, left: rect.left, top: rect.top, moved: false, before: position, next: position };
      e.currentTarget.setPointerCapture(e.pointerId);
    },
    onPointerMove(e: PointerEvent<HTMLButtonElement>) {
      const d = drag.current;
      if (!d || d.id !== e.pointerId) return;
      const dx = e.clientX - d.x, dy = e.clientY - d.y;
      if (!d.moved && Math.hypot(dx, dy) < 6) return;
      d.moved = true;
      suppressClick.current = true;
      const v = bounds();
      d.next = {
        x: Math.max(0, Math.min(1, (d.left + dx - v.left) / (v.width || 1))),
        y: Math.max(0, Math.min(1, (d.top + dy - v.top) / (v.height || 1))),
      };
      setPosition(d.next);
    },
    onPointerUp(e: PointerEvent<HTMLButtonElement>) {
      const d = drag.current;
      if (!d || d.id !== e.pointerId) return;
      if (d.moved) {
        try { localStorage.setItem(key, JSON.stringify(d.next)); } catch { /* Storage may be unavailable in private browsing. */ }
      }
      drag.current = null;
      if (e.currentTarget.hasPointerCapture(e.pointerId)) e.currentTarget.releasePointerCapture(e.pointerId);
    },
    onPointerCancel() {
      if (drag.current) setPosition(drag.current.before);
      drag.current = null;
      suppressClick.current = true;
    },
    onLostPointerCapture() {
      if (drag.current) {
        setPosition(drag.current.before);
        drag.current = null;
        suppressClick.current = true;
      }
    },
    onClick(e: MouseEvent<HTMLButtonElement>) {
      // Keyboard activation remains available after a drag or cancellation.
      if (e.detail !== 0 && suppressClick.current) { suppressClick.current = false; return; }
      open();
    },
  };
}

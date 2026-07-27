/**
 * panes-v2 splitter between two children of a split node.
 *
 * Pointer-events based (no external DnD lib). On pointer-down we
 * capture the pointer, then translate subsequent `pointermove`
 * deltas into a new split ratio relative to the parent flex box.
 *
 * The splitter looks up its parent flex container on pointer-down
 * (walks up to the nearest `.p2-dock-split` ancestor), which gives us
 * the total width/height needed to normalize pixel deltas to a ratio.
 */
import React, { useCallback, useEffect, useRef, useState } from "react";
import { useWorkspace } from "../../store/workspace";

export function Splitter({
  dir,
  splitId,
}: {
  dir: "row" | "col";
  splitId: string;
}): JSX.Element {
  const setSplitRatio = useWorkspace((s) => s.setSplitRatio);
  const ref = useRef<HTMLDivElement | null>(null);
  const [active, setActive] = useState(false);
  const draggingRef = useRef(false);

  const handlePointerDown = useCallback(
    (ev: React.PointerEvent<HTMLDivElement>) => {
      // Only respond to primary button.
      if (ev.button !== 0) return;
      ev.preventDefault();
      const el = ref.current;
      if (!el) return;
      el.setPointerCapture(ev.pointerId);
      draggingRef.current = true;
      setActive(true);
    },
    [],
  );

  const handlePointerMove = useCallback(
    (ev: React.PointerEvent<HTMLDivElement>) => {
      if (!draggingRef.current) return;
      const el = ref.current;
      if (!el) return;
      // Parent flex container.
      const parent = el.parentElement as HTMLElement | null;
      if (!parent) return;
      const rect = parent.getBoundingClientRect();
      const total = dir === "row" ? rect.width : rect.height;
      if (total <= 0) return;
      const pos = dir === "row" ? ev.clientX - rect.left : ev.clientY - rect.top;
      const ratio = pos / total;
      setSplitRatio(splitId, ratio); // clamp happens in the reducer
    },
    [dir, setSplitRatio, splitId],
  );

  const handlePointerUp = useCallback(
    (ev: React.PointerEvent<HTMLDivElement>) => {
      const el = ref.current;
      if (el && el.hasPointerCapture(ev.pointerId)) {
        el.releasePointerCapture(ev.pointerId);
      }
      draggingRef.current = false;
      setActive(false);
    },
    [],
  );

  // Global escape hatch: if the user releases outside the element or
  // hits Escape mid-drag, clear state.
  useEffect(() => {
    if (!active) return;
    const cancel = () => {
      draggingRef.current = false;
      setActive(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") cancel();
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
    };
  }, [active]);

  return (
    <div
      ref={ref}
      role="separator"
      aria-orientation={dir === "row" ? "vertical" : "horizontal"}
      className={"p2-dock-splitter" + (active ? " active" : "")}
      onPointerDown={handlePointerDown}
      onPointerMove={handlePointerMove}
      onPointerUp={handlePointerUp}
      onPointerCancel={handlePointerUp}
    />
  );
}

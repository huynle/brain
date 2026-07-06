import { useRef } from "react";

interface SwipeOpts {
  onLeft?: () => void;
  onRight?: () => void;
  // Minimum horizontal travel (px) to count as a swipe.
  threshold?: number;
  // Require the gesture to start within this many px of the left edge (for an
  // edge-swipe "back" affordance). 0 = anywhere.
  edgeOnly?: number;
}

interface SwipeHandlers {
  onTouchStart: (e: React.TouchEvent) => void;
  onTouchEnd: (e: React.TouchEvent) => void;
}

// useSwipe returns touch handlers that fire onLeft/onRight for a predominantly
// horizontal swipe, ignoring vertical scrolls and multi-touch (pinch). It never
// calls preventDefault, so native vertical scrolling is untouched.
export function useSwipe({ onLeft, onRight, threshold = 55, edgeOnly = 0 }: SwipeOpts): SwipeHandlers {
  const start = useRef<{ x: number; y: number } | null>(null);

  return {
    onTouchStart: (e: React.TouchEvent) => {
      if (e.touches.length !== 1) {
        start.current = null;
        return;
      }
      const t = e.touches[0];
      if (edgeOnly > 0 && t.clientX > edgeOnly) {
        start.current = null;
        return;
      }
      start.current = { x: t.clientX, y: t.clientY };
    },
    onTouchEnd: (e: React.TouchEvent) => {
      const s = start.current;
      start.current = null;
      if (!s) return;
      const t = e.changedTouches[0];
      const dx = t.clientX - s.x;
      const dy = t.clientY - s.y;
      // Must be far enough and clearly more horizontal than vertical.
      if (Math.abs(dx) < threshold) return;
      if (Math.abs(dx) < Math.abs(dy) * 1.7) return;
      if (dx < 0) onLeft?.();
      else onRight?.();
    },
  };
}

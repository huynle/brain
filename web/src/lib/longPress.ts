/**
 * lib/longPress — touch equivalent of right-click.
 *
 * Every row action in this app used to sit behind `onContextMenu`, which
 * simply does not exist on a phone. That made Run task, Run feature,
 * Checkout and Resume unreachable on the surface the PWA was built for.
 *
 * A plain factory rather than a hook: row counts vary per render, so the
 * handlers have to be built inside a per-row callback where hooks are
 * illegal. There is no react state involved — only timer bookkeeping — so
 * a closure is the honest shape, and it makes the gesture unit-testable
 * without a DOM. Keep one instance per list across React renders.
 *
 * Behaviour chosen to match platform expectations:
 *
 * - **650 ms** requires a deliberate hold before marking a row.
 * - **Movement cancels.** A press that drifts more than 8 px is a scroll,
 *   not a long press. Without this the sheet pops open mid-scroll, which is
 *   the single most irritating way to get this wrong.
 * - **The synthetic click is suppressed.** After the sheet opens, touchend
 *   would otherwise also fire the row's normal onClick and open a detail
 *   modal behind the sheet.
 */

export const LONG_PRESS_MS = 650;
/** Movement beyond this many px cancels — the gesture was a scroll. */
export const LONG_PRESS_MOVE_TOLERANCE_PX = 8;

/** The subset of a TouchEvent this module needs. Keeps it testable. */
export interface MinimalTouchEvent {
  touches: ArrayLike<{ clientX: number; clientY: number }>;
}

/** The subset of a MouseEvent this module needs. */
export interface MinimalMouseEvent {
  detail?: number;
  preventDefault: () => void;
  stopPropagation: () => void;
}

export interface LongPressHandlers {
  onTouchStart: (e: MinimalTouchEvent) => void;
  onTouchMove: (e: MinimalTouchEvent) => void;
  onTouchEnd: () => void;
  onTouchCancel: () => void;
  onClickCapture: (e: MinimalMouseEvent) => void;
  onScroll: () => void;
  dispose: () => void;
  isTouchInteraction: () => boolean;
}

export interface LongPressOptions {
  /** Override the hold duration. Tests use this to avoid real waits. */
  durationMs?: number;
  now?: () => number;
  /** Injectable timers, for deterministic tests. */
  setTimeoutFn?: (fn: () => void, ms: number) => unknown;
  clearTimeoutFn?: (handle: unknown) => void;
}

/**
 * Build the touch handlers for one row.
 *
 * @param onLongPress fired once the hold completes without moving.
 */
export function createLongPressHandlers(
  onLongPress: () => void,
  opts: LongPressOptions = {},
): LongPressHandlers {
  const duration = opts.durationMs ?? LONG_PRESS_MS;
  const setT = opts.setTimeoutFn ?? ((fn, ms) => setTimeout(fn, ms));
  const clearT = opts.clearTimeoutFn ?? ((h) => clearTimeout(h as never));

  const now = opts.now ?? Date.now;
  let lastScroll = -Infinity;
  let lastTouch = -Infinity;
  let timer: unknown = null;
  let origin: { x: number; y: number } | null = null;
  // Long presses and cancelled scroll gestures must not also activate a row.
  let swallowClick = false;

  const clear = () => {
    if (timer !== null) {
      clearT(timer);
      timer = null;
    }
    origin = null;
  };

  return {
    onTouchStart: (e) => {
      clear();
      lastTouch = now();
      swallowClick = false;
      // A touch that stops momentum scrolling is not a selection gesture.
      if (e.touches.length !== 1 || now() - lastScroll < 150) {
        swallowClick = true;
        return;
      }
      const t = e.touches[0];
      origin = { x: t.clientX, y: t.clientY };
      timer = setT(() => {
        timer = null;
        swallowClick = true;
        onLongPress();
      }, duration);
    },

    onTouchMove: (e) => {
      if (!origin || timer === null) return;
      if (e.touches.length !== 1) { swallowClick = true; clear(); return; }
      const t = e.touches[0];
      if (!t) return;
      if (
        Math.hypot(t.clientX - origin.x, t.clientY - origin.y) > LONG_PRESS_MOVE_TOLERANCE_PX
      ) {
        swallowClick = true;
        clear();
      }
    },

    onTouchEnd: clear,
    onTouchCancel: () => { swallowClick = true; clear(); },
    onScroll: () => {
      lastScroll = now();
      if (origin) { swallowClick = true; clear(); }
    },
    dispose: clear,
    isTouchInteraction: () => origin !== null || now() - lastTouch < 1000,

    onClickCapture: (e) => {
      if (swallowClick && e.detail !== 0) {
        swallowClick = false;
        e.preventDefault();
        e.stopPropagation();
      }
    },
  };
}

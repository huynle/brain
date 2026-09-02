/**
 * useDeferredPreview — hold a single-click action long enough for a
 * double-click to cancel it.
 *
 * Every row in the app follows one contract: single click previews in the
 * side panel, double click pins into Focus. Implemented naively that
 * contract fights itself, because a double-click BEGINS with a single
 * click. The browser dispatches click → click → dblclick, so the preview
 * pane had already opened before dblclick arrived — the user saw the panel
 * flash open on its way to Focus, and on a slow double-click never got the
 * Focus pane at all.
 *
 * So the preview is scheduled, not performed, and a double-click cancels
 * the pending one.
 *
 * What is NOT deferred: the row's selection highlight. That is the click's
 * acknowledgement, and delaying it makes every row feel broken. Callers
 * set the highlight synchronously and schedule only the pane.
 */
import { useCallback, useEffect, useRef } from "react";

/**
 * How long to wait for a second click.
 *
 * macOS's own double-click interval tops out around 500ms and is user
 * configurable, so no single number is right for everyone. 260ms covers the
 * default setting on both macOS and Windows with margin, and is still below
 * the ~300ms mark where a delay stops reading as "instant".
 *
 * The cost of being too short is the flash this hook exists to remove; the
 * cost of being too long is a preview that feels sluggish. Erring slightly
 * long is the better trade because the highlight already lands immediately.
 */
export const DOUBLE_CLICK_WINDOW_MS = 260;

export interface DeferredPreview {
  /** Run `fn` once the double-click window closes, replacing any pending one. */
  schedule: (fn: () => void) => void;
  /** Drop a pending preview. Call this first in a double-click handler. */
  cancel: () => void;
}

export function useDeferredPreview(
  delayMs: number = DOUBLE_CLICK_WINDOW_MS,
): DeferredPreview {
  const timer = useRef<number | null>(null);

  const cancel = useCallback(() => {
    if (timer.current !== null) {
      window.clearTimeout(timer.current);
      timer.current = null;
    }
  }, []);

  // A row can unmount while a preview is pending — a task completing and
  // leaving the list, a project being hidden. Firing then would open a pane
  // for a row that no longer exists.
  useEffect(() => cancel, [cancel]);

  const schedule = useCallback(
    (fn: () => void) => {
      cancel();
      timer.current = window.setTimeout(() => {
        timer.current = null;
        fn();
      }, delayMs);
    },
    [cancel, delayMs],
  );

  return { schedule, cancel };
}

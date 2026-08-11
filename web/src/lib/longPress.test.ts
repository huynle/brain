/**
 * Tests for lib/longPress — the touch gesture that replaces right-click.
 *
 * Timers are injected so these run instantly and deterministically; the
 * cases that matter are the cancellations, since a long press that fires
 * during a scroll is worse than no long press at all.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  createLongPressHandlers,
  LONG_PRESS_MOVE_TOLERANCE_PX,
  type MinimalMouseEvent,
} from "./longPress";

/** Manual clock: collect pending callbacks and fire them on demand. */
function fakeTimers() {
  let next = 1;
  const pending = new Map<number, () => void>();
  return {
    setTimeoutFn: (fn: () => void) => {
      const id = next++;
      pending.set(id, fn);
      return id;
    },
    clearTimeoutFn: (h: unknown) => {
      pending.delete(h as number);
    },
    /** Fire every pending timer, as the real clock would after the hold. */
    advance: () => {
      const fns = [...pending.values()];
      pending.clear();
      for (const fn of fns) fn();
    },
    pendingCount: () => pending.size,
  };
}

function touch(x: number, y: number) {
  return { touches: [{ clientX: x, clientY: y }] };
}

function mouseEvent(): MinimalMouseEvent & {
  prevented: boolean;
  stopped: boolean;
} {
  const e = {
    prevented: false,
    stopped: false,
    preventDefault() {
      e.prevented = true;
    },
    stopPropagation() {
      e.stopped = true;
    },
  };
  return e;
}

test("a stationary hold fires the long press", () => {
  const timers = fakeTimers();
  let fired = 0;
  const h = createLongPressHandlers(() => fired++, timers);

  h.onTouchStart(touch(10, 10));
  timers.advance();

  assert.equal(fired, 1);
});

test("releasing before the hold completes does not fire", () => {
  const timers = fakeTimers();
  let fired = 0;
  const h = createLongPressHandlers(() => fired++, timers);

  h.onTouchStart(touch(10, 10));
  h.onTouchEnd();
  timers.advance();

  assert.equal(fired, 0);
});

test("moving beyond the tolerance cancels — a scroll is not a long press", () => {
  const timers = fakeTimers();
  let fired = 0;
  const h = createLongPressHandlers(() => fired++, timers);

  h.onTouchStart(touch(10, 10));
  h.onTouchMove(touch(10, 10 + LONG_PRESS_MOVE_TOLERANCE_PX + 1));
  timers.advance();

  assert.equal(fired, 0, "long press fired during a scroll");
});

test("small drift within the tolerance still fires", () => {
  // Fingers are not styluses; a couple of pixels of wobble is normal.
  const timers = fakeTimers();
  let fired = 0;
  const h = createLongPressHandlers(() => fired++, timers);

  h.onTouchStart(touch(10, 10));
  h.onTouchMove(touch(10 + LONG_PRESS_MOVE_TOLERANCE_PX - 1, 10));
  timers.advance();

  assert.equal(fired, 1);
});

test("horizontal drift cancels too, not just vertical", () => {
  const timers = fakeTimers();
  let fired = 0;
  const h = createLongPressHandlers(() => fired++, timers);

  h.onTouchStart(touch(10, 10));
  h.onTouchMove(touch(10 + LONG_PRESS_MOVE_TOLERANCE_PX + 1, 10));
  timers.advance();

  assert.equal(fired, 0);
});

test("multi-touch never fires — that is a pinch", () => {
  const timers = fakeTimers();
  let fired = 0;
  const h = createLongPressHandlers(() => fired++, timers);

  h.onTouchStart({
    touches: [
      { clientX: 10, clientY: 10 },
      { clientX: 50, clientY: 50 },
    ],
  });
  timers.advance();

  assert.equal(fired, 0);
});

test("touchcancel clears the pending timer", () => {
  const timers = fakeTimers();
  let fired = 0;
  const h = createLongPressHandlers(() => fired++, timers);

  h.onTouchStart(touch(10, 10));
  assert.equal(timers.pendingCount(), 1);
  h.onTouchCancel();
  assert.equal(timers.pendingCount(), 0);

  timers.advance();
  assert.equal(fired, 0);
});

test("the click following a long press is swallowed", () => {
  // Otherwise the row's own onClick opens a detail modal behind the sheet.
  const timers = fakeTimers();
  const h = createLongPressHandlers(() => {}, timers);

  h.onTouchStart(touch(10, 10));
  timers.advance();

  const e = mouseEvent();
  h.onClickCapture(e);
  assert.equal(e.prevented, true);
  assert.equal(e.stopped, true);
});

test("only ONE click is swallowed — later taps work normally", () => {
  const timers = fakeTimers();
  const h = createLongPressHandlers(() => {}, timers);

  h.onTouchStart(touch(10, 10));
  timers.advance();

  h.onClickCapture(mouseEvent());
  const second = mouseEvent();
  h.onClickCapture(second);

  assert.equal(second.prevented, false, "a normal tap was swallowed");
});

test("a plain tap is never swallowed", () => {
  const timers = fakeTimers();
  const h = createLongPressHandlers(() => {}, timers);

  h.onTouchStart(touch(10, 10));
  h.onTouchEnd();

  const e = mouseEvent();
  h.onClickCapture(e);
  assert.equal(e.prevented, false);
});

test("onTouchMove after the press fired is inert", () => {
  const timers = fakeTimers();
  let fired = 0;
  const h = createLongPressHandlers(() => fired++, timers);

  h.onTouchStart(touch(10, 10));
  timers.advance();
  h.onTouchMove(touch(500, 500)); // finger lifts away afterwards

  assert.equal(fired, 1, "already-fired press should not re-fire or throw");
});

test("consecutive presses each fire", () => {
  const timers = fakeTimers();
  let fired = 0;
  const h = createLongPressHandlers(() => fired++, timers);

  h.onTouchStart(touch(10, 10));
  timers.advance();
  h.onTouchEnd();

  h.onTouchStart(touch(20, 20));
  timers.advance();

  assert.equal(fired, 2);
});

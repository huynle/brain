import { strict as assert } from "node:assert";
import { test } from "node:test";
import {
  nextFocus,
  scrollStep,
  makeGgSequence,
  clampBottomHeight,
  clampDetailLogsRatio,
  BOTTOM_HEIGHT_BOUNDS,
  DETAIL_LOGS_RATIO_BOUNDS,
  type Panel,
} from "./paneNav";

// ─── nextFocus: pure pane-cycling helper ────────────────────────────────────

test("nextFocus forward: tasks → detail → logs → tasks", () => {
  assert.equal(nextFocus("tasks", { detailVisible: true, logsVisible: true }, 1), "detail");
  assert.equal(nextFocus("detail", { detailVisible: true, logsVisible: true }, 1), "logs");
  assert.equal(nextFocus("logs", { detailVisible: true, logsVisible: true }, 1), "tasks");
});

test("nextFocus backward: tasks → logs → detail → tasks", () => {
  assert.equal(nextFocus("tasks", { detailVisible: true, logsVisible: true }, -1), "logs");
  assert.equal(nextFocus("logs", { detailVisible: true, logsVisible: true }, -1), "detail");
  assert.equal(nextFocus("detail", { detailVisible: true, logsVisible: true }, -1), "tasks");
});

test("nextFocus skips hidden panes forward (detail hidden)", () => {
  const v = { detailVisible: false, logsVisible: true };
  assert.equal(nextFocus("tasks", v, 1), "logs");
  assert.equal(nextFocus("logs", v, 1), "tasks");
});

test("nextFocus skips hidden panes backward (logs hidden)", () => {
  const v = { detailVisible: true, logsVisible: false };
  assert.equal(nextFocus("tasks", v, -1), "detail");
  assert.equal(nextFocus("detail", v, -1), "tasks");
});

test("nextFocus: when both bottom panes hidden, tasks stays focused", () => {
  const v = { detailVisible: false, logsVisible: false };
  assert.equal(nextFocus("tasks", v, 1), "tasks");
  assert.equal(nextFocus("tasks", v, -1), "tasks");
});

test("nextFocus: current focus on a hidden pane snaps to tasks", () => {
  // If the detail pane was just toggled off while focused there, cycling
  // should treat the user's effective position as tasks.
  const v = { detailVisible: false, logsVisible: true };
  assert.equal(nextFocus("detail", v, 1), "logs");
  assert.equal(nextFocus("detail", v, -1), "logs");
});

// ─── scrollStep: pure scroll calculator ─────────────────────────────────────

test("scrollStep j scrolls down by line height", () => {
  const el = mkScroll({ top: 100, height: 500, scrollHeight: 2000 });
  scrollStep(el, "j");
  assert.equal(el.scrollTop, 140); // 100 + 40
});

test("scrollStep k scrolls up by line height", () => {
  const el = mkScroll({ top: 100, height: 500, scrollHeight: 2000 });
  scrollStep(el, "k");
  assert.equal(el.scrollTop, 60); // 100 - 40
});

test("scrollStep k clamps at 0", () => {
  const el = mkScroll({ top: 10, height: 500, scrollHeight: 2000 });
  scrollStep(el, "k");
  assert.equal(el.scrollTop, 0);
});

test("scrollStep G jumps to bottom", () => {
  const el = mkScroll({ top: 100, height: 500, scrollHeight: 2000 });
  scrollStep(el, "G");
  // Impl writes el.scrollTop = scrollHeight (2000), browser clamps to
  // scrollHeight - clientHeight = 1500. The fixture mirrors that clamp.
  assert.equal(el.scrollTop, 1500);
});

test("scrollStep gg jumps to top", () => {
  const el = mkScroll({ top: 1000, height: 500, scrollHeight: 2000 });
  scrollStep(el, "gg");
  assert.equal(el.scrollTop, 0);
});

test("scrollStep ctrl-d half-page down (half of clientHeight)", () => {
  const el = mkScroll({ top: 100, height: 600, scrollHeight: 3000 });
  scrollStep(el, "ctrl-d");
  assert.equal(el.scrollTop, 400); // 100 + 600/2
});

test("scrollStep ctrl-u half-page up", () => {
  const el = mkScroll({ top: 1000, height: 600, scrollHeight: 3000 });
  scrollStep(el, "ctrl-u");
  assert.equal(el.scrollTop, 700); // 1000 - 600/2
});

test("scrollStep ctrl-d does not overshoot the bottom", () => {
  // scrollTop's natural cap is (scrollHeight - clientHeight). We don't enforce
  // it ourselves — the DOM clamps. The test fixture's setter clamps the same.
  const el = mkScroll({ top: 2900, height: 600, scrollHeight: 3000 });
  scrollStep(el, "ctrl-d");
  // 2900 + 300 = 3200, clamped to scrollHeight - clientHeight = 2400.
  assert.equal(el.scrollTop, 2400);
});

// ─── gg two-key sequence ────────────────────────────────────────────────────

test("gg sequence: first 'g' arms, second 'g' fires", () => {
  let firedTop = false;
  const fakeClock = makeFakeClock();
  const seq = makeGgSequence({
    timeoutMs: 500,
    onTop: () => {
      firedTop = true;
    },
    setTimeout: fakeClock.setTimeout,
    clearTimeout: fakeClock.clearTimeout,
  });

  assert.equal(seq.handle("g"), "armed");
  assert.equal(firedTop, false);
  assert.equal(seq.handle("g"), "fired");
  assert.equal(firedTop, true);
});

test("gg sequence: 'g' then non-'g' cancels (returns 'cancelled')", () => {
  let firedTop = false;
  const fakeClock = makeFakeClock();
  const seq = makeGgSequence({
    timeoutMs: 500,
    onTop: () => {
      firedTop = true;
    },
    setTimeout: fakeClock.setTimeout,
    clearTimeout: fakeClock.clearTimeout,
  });

  seq.handle("g");
  const result = seq.handle("j");
  assert.equal(result, "cancelled");
  assert.equal(firedTop, false);
});

test("gg sequence: timeout cancels armed state", () => {
  let firedTop = false;
  const fakeClock = makeFakeClock();
  const seq = makeGgSequence({
    timeoutMs: 500,
    onTop: () => {
      firedTop = true;
    },
    setTimeout: fakeClock.setTimeout,
    clearTimeout: fakeClock.clearTimeout,
  });

  seq.handle("g");
  fakeClock.tick(600);
  // After timeout, a second 'g' should re-arm, not fire.
  assert.equal(seq.handle("g"), "armed");
  assert.equal(firedTop, false);
});

test("gg sequence: handle('g') after 'fired' returns to armed", () => {
  let topCount = 0;
  const fakeClock = makeFakeClock();
  const seq = makeGgSequence({
    timeoutMs: 500,
    onTop: () => {
      topCount += 1;
    },
    setTimeout: fakeClock.setTimeout,
    clearTimeout: fakeClock.clearTimeout,
  });

  seq.handle("g");
  seq.handle("g");
  seq.handle("g");
  // 'g' alone after a complete gg should re-arm (next 'g' would fire again).
  assert.equal(topCount, 1);
  assert.equal(seq.handle("g"), "fired");
  assert.equal(topCount, 2);
});

test("gg sequence: handle(non-g) when idle returns 'none'", () => {
  const fakeClock = makeFakeClock();
  const seq = makeGgSequence({
    timeoutMs: 500,
    onTop: () => {},
    setTimeout: fakeClock.setTimeout,
    clearTimeout: fakeClock.clearTimeout,
  });
  assert.equal(seq.handle("j"), "none");
});

// ─── test fixtures ──────────────────────────────────────────────────────────

interface ScrollFixture extends Pick<HTMLElement, "clientHeight" | "scrollHeight"> {
  scrollTop: number;
}

function mkScroll(p: { top: number; height: number; scrollHeight: number }): ScrollFixture {
  let scrollTop = p.top;
  return {
    get scrollTop() {
      return scrollTop;
    },
    set scrollTop(v: number) {
      // Mirror browser clamping behavior:
      // scrollTop is clamped to [0, scrollHeight - clientHeight].
      const maxTop = Math.max(0, p.scrollHeight - p.height);
      scrollTop = Math.max(0, Math.min(v, maxTop));
    },
    clientHeight: p.height,
    scrollHeight: p.scrollHeight,
  };
}

function makeFakeClock() {
  let now = 0;
  let nextId = 1;
  const pending = new Map<number, { fireAt: number; cb: () => void }>();
  return {
    setTimeout: (cb: () => void, ms: number): number => {
      const id = nextId++;
      pending.set(id, { fireAt: now + ms, cb });
      return id as unknown as number;
    },
    clearTimeout: (id: number) => {
      pending.delete(id);
    },
    tick: (ms: number) => {
      now += ms;
      for (const [id, { fireAt, cb }] of [...pending.entries()]) {
        if (fireAt <= now) {
          pending.delete(id);
          cb();
        }
      }
    },
  };
}

// Force the Panel type to be exported correctly (compile-time assertion)
const _p: Panel = "tasks";
void _p;

// ─── pane-size clamp helpers ────────────────────────────────────────────────

test("clampBottomHeight: in-bounds value returned as-is", () => {
  assert.equal(clampBottomHeight(400), 400);
});

test("clampBottomHeight: below min snaps to min", () => {
  assert.equal(clampBottomHeight(10), BOTTOM_HEIGHT_BOUNDS.min);
  assert.equal(clampBottomHeight(BOTTOM_HEIGHT_BOUNDS.min - 1), BOTTOM_HEIGHT_BOUNDS.min);
});

test("clampBottomHeight: above max snaps to max-derived-from-viewport", () => {
  // Max is 0.8 * viewport height. The function takes optional viewportH for
  // testability; default uses window.innerHeight in the browser.
  const viewport = 1000;
  const maxAt1000 = Math.floor(viewport * BOTTOM_HEIGHT_BOUNDS.maxRatio);
  assert.equal(clampBottomHeight(99999, viewport), maxAt1000);
});

test("clampBottomHeight: rounds non-integer input", () => {
  assert.equal(clampBottomHeight(320.7), 321);
});

test("clampBottomHeight: handles NaN/non-finite by falling back to default", () => {
  assert.equal(clampBottomHeight(Number.NaN), BOTTOM_HEIGHT_BOUNDS.default);
  assert.equal(
    clampBottomHeight(Infinity, 1000),
    Math.floor(1000 * BOTTOM_HEIGHT_BOUNDS.maxRatio),
  );
  assert.equal(clampBottomHeight(-Infinity), BOTTOM_HEIGHT_BOUNDS.min);
});

test("clampDetailLogsRatio: in-bounds value returned as-is", () => {
  assert.equal(clampDetailLogsRatio(0.5), 0.5);
  assert.equal(clampDetailLogsRatio(0.33), 0.33);
});

test("clampDetailLogsRatio: below min snaps to min (20%)", () => {
  assert.equal(clampDetailLogsRatio(0.05), DETAIL_LOGS_RATIO_BOUNDS.min);
  assert.equal(clampDetailLogsRatio(0), DETAIL_LOGS_RATIO_BOUNDS.min);
});

test("clampDetailLogsRatio: above max snaps to max (80%)", () => {
  assert.equal(clampDetailLogsRatio(0.95), DETAIL_LOGS_RATIO_BOUNDS.max);
  assert.equal(clampDetailLogsRatio(1.5), DETAIL_LOGS_RATIO_BOUNDS.max);
});

test("clampDetailLogsRatio: handles NaN by returning default 0.5", () => {
  assert.equal(clampDetailLogsRatio(Number.NaN), DETAIL_LOGS_RATIO_BOUNDS.default);
});

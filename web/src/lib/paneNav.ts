// Pure helpers for keyboard pane navigation and vim-style content scrolling.
// Side-effect-free where possible; the only effect is writing scrollTop on a
// DOM element via scrollStep. The two-key 'gg' sequence is a tiny state
// machine that takes setTimeout/clearTimeout via DI so it's testable without
// real timers.

export type Panel = "tasks" | "detail" | "logs";

export interface PaneVisibility {
  detailVisible: boolean;
  logsVisible: boolean;
}

// nextFocus returns the panel that should receive focus when cycling from
// `current` by `dir` (+1 forward / -1 backward). Hidden panes are skipped.
// If `current` is on a hidden pane the function treats the effective
// starting point as "tasks" — this matches the user's intuition that
// toggling a pane off should snap focus away from it.
//
// Direction is +1 (Tab) or -1 (Shift-Tab). Panel order: tasks → detail → logs.
export function nextFocus(
  current: Panel,
  vis: PaneVisibility,
  dir: 1 | -1,
): Panel {
  const panels: Panel[] = ["tasks"];
  if (vis.detailVisible) panels.push("detail");
  if (vis.logsVisible) panels.push("logs");

  // Effective start: if current is hidden, treat it as tasks.
  const effective: Panel = panels.includes(current) ? current : "tasks";

  const i = panels.indexOf(effective);
  const next = (i + dir + panels.length) % panels.length;
  return panels[next];
}

// ─── scroll step ────────────────────────────────────────────────────────────

export type ScrollAction =
  | "j"        // line down
  | "k"        // line up
  | "G"        // jump to bottom
  | "gg"       // jump to top
  | "ctrl-d"   // half-page down
  | "ctrl-u";  // half-page up

// Line height for j/k. Matches what TasksView used before this hook existed.
const LINE_STEP_PX = 40;

// scrollStep mutates the element's scrollTop. It writes a value that
// represents user intent; the browser (or test fixture) is responsible for
// clamping to [0, scrollHeight - clientHeight].
export function scrollStep(
  el: Pick<HTMLElement, "scrollTop" | "scrollHeight" | "clientHeight">,
  action: ScrollAction,
): void {
  switch (action) {
    case "j":
      el.scrollTop = el.scrollTop + LINE_STEP_PX;
      return;
    case "k":
      el.scrollTop = el.scrollTop - LINE_STEP_PX;
      return;
    case "G":
      el.scrollTop = el.scrollHeight;
      return;
    case "gg":
      el.scrollTop = 0;
      return;
    case "ctrl-d":
      el.scrollTop = el.scrollTop + Math.floor(el.clientHeight / 2);
      return;
    case "ctrl-u":
      el.scrollTop = el.scrollTop - Math.floor(el.clientHeight / 2);
      return;
  }
}

// ─── gg two-key sequence ────────────────────────────────────────────────────

export type GgResult =
  | "armed"      // first 'g' received; awaiting second
  | "fired"      // second 'g' completed the gg; onTop was called
  | "cancelled"  // non-'g' key arrived while armed; sequence cleared
  | "none";      // non-'g' key, idle state — caller can ignore

export interface GgSequence {
  // Feed a key. Returns the state transition the caller should respond to.
  // The caller is responsible for consuming the event (preventing fall-
  // through) on "armed", "fired", and "cancelled" returns when the key
  // is "g".
  handle(key: string): GgResult;
  // For React-component cleanup — clears any pending timeout.
  dispose(): void;
}

export interface GgSequenceOpts {
  timeoutMs: number;
  onTop: () => void;
  // Injectable timer functions so tests can use a fake clock. Defaults to
  // the global setTimeout/clearTimeout for production callers.
  setTimeout?: (cb: () => void, ms: number) => number;
  clearTimeout?: (id: number) => void;
}

export function makeGgSequence(opts: GgSequenceOpts): GgSequence {
  const setT = opts.setTimeout ?? ((cb, ms) => globalThis.setTimeout(cb, ms) as unknown as number);
  const clearT = opts.clearTimeout ?? ((id) => globalThis.clearTimeout(id));

  let armed = false;
  let pending: number | null = null;

  function disarm() {
    armed = false;
    if (pending !== null) {
      clearT(pending);
      pending = null;
    }
  }

  return {
    handle(key: string): GgResult {
      if (key !== "g") {
        if (armed) {
          disarm();
          return "cancelled";
        }
        return "none";
      }
      // key is "g"
      if (armed) {
        // Second 'g' completes the sequence.
        disarm();
        opts.onTop();
        return "fired";
      }
      // First 'g' — arm and set the cancellation timer.
      armed = true;
      pending = setT(() => {
        armed = false;
        pending = null;
      }, opts.timeoutMs);
      return "armed";
    },
    dispose() {
      disarm();
    },
  };
}

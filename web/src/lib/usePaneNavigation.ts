// React hook bundling pane-focus + vim-scroll behavior so views don't all
// re-implement it. The pure helpers live in lib/paneNav.ts; this is the
// stateful adapter.
//
// Returns wiring objects that the view applies to each <Panel>: focused
// boolean, onFocus handler. For detail/logs panes the hook also owns the
// body ref so it can scroll those bodies in response to keystrokes.
//
// The hook also returns `handleKey(e)` that the view should call from its
// useViewKeyboard handler. It returns true when it consumes the key. This
// lets each view stay in charge of priority — j/k on the tasks pane go to
// the view's list-nav, not here.

import { useEffect, useMemo, useRef } from "react";
import { useUI } from "../store/ui";
import {
  makeGgSequence,
  scrollStep,
  RESIZE_STEP,
  type ScrollAction,
} from "./paneNav";
import { useActions } from "./keymap/useActions";
import type { ActionSpec } from "./keymap/types";

// Pane-tier action table: registered while a view with detail/logs panes is
// mounted, dispatching BEFORE the view scope so pane scroll wins while a
// content pane has focus. The help modal derives its "panes" group from
// these — shown exactly when a paned view is active.
const paneFocus = { focus: ["detail" as const, "logs" as const] };

export const PANE_SPECS: ActionSpec[] = [
  { id: "panes.cycle", keys: ["Tab"], desc: "Cycle pane focus (list → detail → logs)", group: "panes" },
  { id: "panes.cycleBack", keys: ["S-Tab"], desc: "Cycle pane focus backward", group: "panes" },
  { id: "panes.down", keys: ["j", "ArrowDown"], desc: "Scroll line down (in focused pane)", group: "panes", when: paneFocus, countable: true },
  { id: "panes.up", keys: ["k", "ArrowUp"], desc: "Scroll line up (in focused pane)", group: "panes", when: paneFocus, countable: true },
  { id: "panes.top", keys: ["g g"], desc: "Jump to top (vim gg)", group: "panes", when: paneFocus },
  { id: "panes.bottom", keys: ["G"], desc: "Jump to bottom", group: "panes", when: paneFocus },
  { id: "panes.halfDown", keys: ["C-d"], desc: "Half-page down", group: "panes", when: paneFocus },
  { id: "panes.halfUp", keys: ["C-u"], desc: "Half-page up", group: "panes", when: paneFocus },
  { id: "panes.rowGrow", keys: ["A-j"], desc: "Grow bottom-row height", group: "panes" },
  { id: "panes.rowShrink", keys: ["A-k"], desc: "Shrink bottom-row height", group: "panes" },
  { id: "panes.splitGrow", keys: ["A-l"], desc: "Grow detail vs logs width", group: "panes" },
  { id: "panes.splitShrink", keys: ["A-h"], desc: "Shrink detail vs logs width", group: "panes" },
  { id: "panes.resetSplit", keys: ["dbl-click"], desc: "Double-click a separator to reset that split", group: "panes" },
];

export interface PaneNavigation {
  // Wiring for the tasks pane (the top list). Spread onto a <Panel>.
  // The view supplies its own bodyRef for the list (so list-nav can
  // scroll the cursor into view) — this hook does not own that ref.
  tasksPaneProps: {
    focused: boolean;
    onFocus: () => void;
  };
  // Wiring for the detail pane. Spread onto a <Panel>; the hook owns
  // the bodyRef so it can scroll on j/k/gg/G/Ctrl-D/Ctrl-U.
  detailPaneProps: {
    focused: boolean;
    onFocus: () => void;
    bodyRef: React.RefObject<HTMLDivElement>;
  };
  // Wiring for the logs pane. Same shape as detail.
  logsPaneProps: {
    focused: boolean;
    onFocus: () => void;
    bodyRef: React.RefObject<HTMLDivElement>;
  };
  // Feed every keydown the view sees. Returns true if consumed.
  handleKey: (e: KeyboardEvent) => boolean;
}

/**
 * usePaneNavigation wires keyboard pane focus + vim-style scroll into a view.
 *
 * Behavior:
 *   Tab            → cycle focus forward (tasks → detail → logs)
 *   Shift+Tab      → cycle focus backward
 *   j/k            → line down/up inside focused detail/logs pane
 *   gg             → top of pane (two-key, ~500ms timeout)
 *   G              → bottom of pane
 *   Ctrl-D/Ctrl-U  → half-page down/up
 *
 * When focus is on `tasks` (the list), this hook does NOT consume j/k/g/G,
 * letting the view's own list navigation (handleListNavKey) handle them.
 * Tab/Shift-Tab always consume.
 */
export function usePaneNavigation(): PaneNavigation {
  const focus = useUI((s) => s.focus);
  const setFocus = useUI((s) => s.setFocus);
  const cycleFocus = useUI((s) => s.cycleFocus);
  // Pane-size mutators for Alt+H/J/K/L keyboard resize. Reading the latest
  // values here is fine because the setters clamp internally; we only need
  // to add/subtract a step. Using getState() inside handleKey avoids stale
  // closure issues without re-creating the handler on every render.
  const setBottomHeight = useUI((s) => s.setBottomHeight);
  const setDetailLogsRatio = useUI((s) => s.setDetailLogsRatio);

  const detailBodyRef = useRef<HTMLDivElement>(null);
  const logsBodyRef = useRef<HTMLDivElement>(null);

  function bodyForFocus(): HTMLDivElement | null {
    if (focusRef.current === "detail") return detailBodyRef.current;
    if (focusRef.current === "logs") return logsBodyRef.current;
    return null;
  }

  // gg's onTop fires asynchronously (next 'g' or — for cancellation — via
  // timeout). Mirror focus in a ref so the callback sees the current value
  // rather than the value captured at hook-mount time.
  const focusRef = useRef(focus);
  focusRef.current = focus;

  const gg = useMemo(
    () =>
      makeGgSequence({
        timeoutMs: 500,
        onTop: () => {
          const el = bodyForFocus();
          if (el) scrollStep(el, "gg");
        },
      }),
    // Intentional: never recreate. Refs stay valid across renders.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [],
  );

  // Cleanup any pending timer when the consuming component unmounts.
  useEffect(() => {
    return () => gg.dispose();
  }, [gg]);

  // Registry pane scope: dispatches before view scopes. Migrated views rely
  // on this alone; unmigrated views still call handleKey() below, which the
  // dispatcher only reaches for chords this scope declined.
  useActions(
    "panes",
    "pane",
    PANE_SPECS,
    {
      "panes.cycle": () => cycleFocus(1),
      "panes.cycleBack": () => cycleFocus(-1),
      "panes.down": ({ count }) => {
        const el = bodyForFocus();
        if (!el) return false;
        for (let i = 0; i < count; i++) scrollStep(el, "j");
      },
      "panes.up": ({ count }) => {
        const el = bodyForFocus();
        if (!el) return false;
        for (let i = 0; i < count; i++) scrollStep(el, "k");
      },
      "panes.top": () => {
        const el = bodyForFocus();
        if (!el) return false;
        scrollStep(el, "gg");
      },
      "panes.bottom": () => {
        const el = bodyForFocus();
        if (!el) return false;
        scrollStep(el, "G");
      },
      "panes.halfDown": () => {
        const el = bodyForFocus();
        if (!el) return false;
        scrollStep(el, "ctrl-d");
      },
      "panes.halfUp": () => {
        const el = bodyForFocus();
        if (!el) return false;
        scrollStep(el, "ctrl-u");
      },
      "panes.rowGrow": () => setBottomHeight(useUI.getState().bottomHeight + RESIZE_STEP.heightPx),
      "panes.rowShrink": () => setBottomHeight(useUI.getState().bottomHeight - RESIZE_STEP.heightPx),
      "panes.splitGrow": () => setDetailLogsRatio(useUI.getState().detailLogsRatio + RESIZE_STEP.ratio),
      "panes.splitShrink": () => setDetailLogsRatio(useUI.getState().detailLogsRatio - RESIZE_STEP.ratio),
    },
    [focus],
  );

  function handleKey(e: KeyboardEvent): boolean {
    // Tab / Shift-Tab: always consumes, regardless of which pane is focused.
    if (e.key === "Tab") {
      cycleFocus(e.shiftKey ? -1 : 1);
      return true;
    }

    // Alt+H/J/K/L: keyboard pane resize. Available regardless of focus,
    // because resizing the bottom row is a layout action, not a content
    // action. We read live store values via getState() to compute the
    // next size, then let the setter clamp.
    //
    // Match on e.code (physical key), not e.key: with Option held, macOS
    // produces composed characters ("∆", "˚", "¬", "˙") in e.key, so a
    // key-based match never fires on Mac.
    if (e.altKey && !e.ctrlKey && !e.metaKey) {
      const ui = useUI.getState();
      if (e.code === "KeyJ") {
        setBottomHeight(ui.bottomHeight + RESIZE_STEP.heightPx);
        return true;
      }
      if (e.code === "KeyK") {
        setBottomHeight(ui.bottomHeight - RESIZE_STEP.heightPx);
        return true;
      }
      if (e.code === "KeyL") {
        // Right means Detail gets more, Logs less.
        setDetailLogsRatio(ui.detailLogsRatio + RESIZE_STEP.ratio);
        return true;
      }
      if (e.code === "KeyH") {
        setDetailLogsRatio(ui.detailLogsRatio - RESIZE_STEP.ratio);
        return true;
      }
      // Other Alt-modified keys are not ours — let them fall through.
      return false;
    }

    // Scroll keys only apply when a content pane is focused.
    if (focus !== "detail" && focus !== "logs") {
      // Still feed the gg sequence so an in-flight 'g' gets cancelled when
      // the user moves focus off detail/logs mid-sequence.
      if (e.key !== "g") gg.handle(e.key);
      return false;
    }

    const el = focus === "detail" ? detailBodyRef.current : logsBodyRef.current;
    if (!el) return false;

    // Ctrl-D / Ctrl-U half page. (We accept Meta as well for Mac users who
    // expect Cmd to work the same, though Ctrl is canonical for vim.)
    if (e.ctrlKey || e.metaKey) {
      let action: ScrollAction | null = null;
      if (e.key === "d" || e.key === "D") action = "ctrl-d";
      else if (e.key === "u" || e.key === "U") action = "ctrl-u";
      if (action) {
        scrollStep(el, action);
        return true;
      }
      return false;
    }

    // Single-key shortcuts (no modifiers).
    if (e.key === "j" || e.key === "ArrowDown") {
      scrollStep(el, "j");
      return true;
    }
    if (e.key === "k" || e.key === "ArrowUp") {
      scrollStep(el, "k");
      return true;
    }
    if (e.key === "G") {
      scrollStep(el, "G");
      return true;
    }

    // 'g' is a two-key sequence (gg = top). Delegate to the sequence
    // machine and consume the event if it took any state-changing action.
    if (e.key === "g") {
      const r = gg.handle("g");
      // "armed" and "fired" both consume; "cancelled" / "none" don't.
      return r === "armed" || r === "fired";
    }

    // Any other key cancels an in-flight gg arming.
    gg.handle(e.key);
    return false;
  }

  return {
    tasksPaneProps: {
      focused: focus === "tasks",
      onFocus: () => setFocus("tasks"),
    },
    detailPaneProps: {
      focused: focus === "detail",
      onFocus: () => setFocus("detail"),
      bodyRef: detailBodyRef,
    },
    logsPaneProps: {
      focused: focus === "logs",
      onFocus: () => setFocus("logs"),
      bodyRef: logsBodyRef,
    },
    handleKey,
  };
}

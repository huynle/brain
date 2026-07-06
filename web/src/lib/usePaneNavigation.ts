// React hook bundling pane-focus + vim-scroll behavior so views don't all
// re-implement it. The pure helpers live in lib/paneNav.ts; this is the
// stateful adapter.
//
// Returns wiring objects that the view applies to each <Panel>: focused
// boolean, onFocus handler. For detail/logs panes the hook also owns the
// body ref so it can scroll those bodies in response to keystrokes.
//
// Key handling registers as a pane-tier keymap scope (see PANE_SPECS below):
// the dispatcher runs it before the view scope, so pane scroll wins while a
// content pane has focus and the view's list-nav wins otherwise.

import { useRef } from "react";
import { useUI } from "../store/ui";
import { scrollStep, RESIZE_STEP } from "./paneNav";
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
 * When focus is on `tasks` (the list), the pane scope does not claim
 * j/k/g/G (when-clause), letting the view's list navigation handle them.
 * Tab/Shift-Tab always consume.
 */
export function usePaneNavigation(): PaneNavigation {
  const focus = useUI((s) => s.focus);
  const setFocus = useUI((s) => s.setFocus);
  const cycleFocus = useUI((s) => s.cycleFocus);
  // Pane-size mutators for Alt+H/J/K/L keyboard resize. Reading live store
  // values via getState() inside the handlers avoids stale closures.
  const setBottomHeight = useUI((s) => s.setBottomHeight);
  const setDetailLogsRatio = useUI((s) => s.setDetailLogsRatio);

  const detailBodyRef = useRef<HTMLDivElement>(null);
  const logsBodyRef = useRef<HTMLDivElement>(null);

  function bodyForFocus(): HTMLDivElement | null {
    if (focusRef.current === "detail") return detailBodyRef.current;
    if (focusRef.current === "logs") return logsBodyRef.current;
    return null;
  }

  // Handlers read focus through a ref so the registry closures always see
  // the current pane. (The "g g" sequence is handled by the registry's
  // sequence buffer — no local gg machine needed.)
  const focusRef = useRef(focus);
  focusRef.current = focus;

  // Registry pane scope: dispatches before view scopes.
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
  };
}

// Keyboard system providing TUI-parity shortcuts on desktop.
//
// One global listener (useGlobalKeyboard, mounted in Dashboard) normalizes
// each keydown into a Chord and dispatches through the keymap registry in
// tier order: pane/view scopes first, then the legacy per-view handler (for
// views not yet migrated to ActionSpec tables), then the global scope.
// Global keys are declared in keymap/global.ts; migrated views register
// spec tables via useActions. The useViewKeyboard shim below is deleted once
// the last view migrates.

import { useEffect, useRef } from "react";
import { useNav } from "../store/nav";
import { makeCountMachine } from "./keymap/count";
import {
  buildGlobalHandlers,
  GLOBAL_SPECS,
  jumpToProject,
  type GlobalKeyboardOpts,
} from "./keymap/global";
import { dispatchChord, isCountable, registerScope } from "./keymap/registry";
import { chordOf, type WhenEnv } from "./keymap/types";

export type { GlobalKeyboardOpts } from "./keymap/global";

export type ViewKeyHandler = (e: KeyboardEvent) => boolean;

let activeHandler: ViewKeyHandler | null = null;

/**
 * Legacy registration shim for views that haven't migrated to useActions.
 * The dispatcher tries registered scopes first, then falls back to this.
 */
export function useViewKeyboard(handler: ViewKeyHandler, deps: unknown[]) {
  useEffect(() => {
    activeHandler = handler;
    return () => {
      if (activeHandler === handler) activeHandler = null;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
}

export function isEditableTarget(t: EventTarget | null): boolean {
  const el = t as HTMLElement | null;
  if (!el) return false;
  const tag = el.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
  if (el.isContentEditable) return true;
  // CodeMirror
  if (el.closest?.(".cm-editor")) return true;
  return false;
}

export function anyModalOpen(): boolean {
  // Bottom sheets (.sheet-backdrop) count as modal too — global shortcuts
  // must not fire underneath an open sheet.
  return !!document.querySelector(".modal-backdrop, .sheet-backdrop");
}

/**
 * Shared list navigation for views. Handles j/k/↑/↓/g/G against `count`,
 * updating the cursor for `scope`. Returns true if the key was consumed.
 */
export function handleListNavKey(
  e: KeyboardEvent,
  scope: string,
  count: number,
): boolean {
  const nav = useNav.getState();
  switch (e.key) {
    case "j":
    case "ArrowDown":
      nav.moveCursor(scope, 1, count);
      return true;
    case "k":
    case "ArrowUp":
      nav.moveCursor(scope, -1, count);
      return true;
    case "g":
      nav.top(scope);
      return true;
    case "G":
      nav.bottom(scope, count);
      return true;
    default:
      return false;
  }
}

// Chords the legacy per-view handlers treat as repeatable cursor motion.
// Migrated views declare `countable` on their specs instead; this set keeps
// vim counts working for unmigrated views (the handler is replayed).
const LEGACY_COUNTABLE = new Set(["j", "k", "ArrowDown", "ArrowUp"]);
const MAX_COUNT_REPLAY = 99;

export function useGlobalKeyboard(opts: GlobalKeyboardOpts) {
  const optsRef = useRef(opts);
  optsRef.current = opts;

  // The listener is installed once; it reads the latest opts via the ref.
  useEffect(() => {
    const unregisterGlobal = registerScope({
      scopeId: "global",
      tier: "global",
      specs: GLOBAL_SPECS,
      handlers: buildGlobalHandlers(() => optsRef.current),
    });
    const counts = makeCountMachine({
      onReplayDigit: (n) => jumpToProject(n, optsRef.current),
    });

    function buildEnv(): WhenEnv {
      const ui = uiApi();
      return {
        focus: ui.focus,
        mode: undefined,
        hasSelection: useNav.getState().selectedCount() > 0,
        isMobile: false,
      };
    }

    function onKeyDown(e: KeyboardEvent) {
      const nav = useNav.getState();

      // Help overlay swallows everything.
      if (nav.helpOpen) {
        if (e.key === "?" || e.key === "Escape" || e.key === "q") {
          e.preventDefault();
          nav.setHelpOpen(false);
        }
        return;
      }

      // Quick project switcher: Cmd/Ctrl+; opens the searchable picker from
      // anywhere (even while typing in a field). Handled before the modifier
      // and editable-target guards below.
      if ((e.metaKey || e.ctrlKey) && e.key === ";") {
        e.preventDefault();
        uiApi().setProjectSheetOpen(true);
        return;
      }

      // Cmd/Ctrl+. toggles the Brain Assistant from anywhere — including while
      // typing in another field — so it functions as a global command center.
      // Behavior is viewport-aware: on a wide desktop it toggles the persistent
      // sidebar; on narrow desktop / mobile it toggles the overlay drawer.
      if ((e.metaKey || e.ctrlKey) && (e.key === ".")) {
        e.preventDefault();
        const ui = uiApi();
        // Window width check rather than the hook — this listener runs outside
        // React's render cycle. Threshold mirrors the "wide" tier in useViewport.
        const wide = window.innerWidth >= 1100;
        if (wide) {
          ui.setAssistantSidebar(!ui.assistantSidebar);
          if (!ui.assistantSidebar) window.setTimeout(ui.focusAssistantPrompt, 0);
        } else {
          ui.setAssistantOpen(!ui.assistantOpen);
          if (!ui.assistantOpen) window.setTimeout(ui.focusAssistantPrompt, 0);
        }
        return;
      }

      // Don't hijack typing or modal interactions.
      if (isEditableTarget(e.target)) return;
      if (anyModalOpen()) return;

      const env = buildEnv();

      // Modified chords: registered scopes may claim them (vim Ctrl-D/U,
      // Alt+HJKL resize), then the legacy view handler; otherwise leave them
      // to the browser (Ctrl-F, Ctrl-W, Alt+Left, ...). Global-tier bindings
      // deliberately don't participate — no global chord uses modifiers
      // beyond the pre-guard Cmd+;/Cmd+. handled above.
      if (e.ctrlKey || e.metaKey || e.altKey) {
        const chord = chordOf(e);
        if (chord && dispatchChord(chord, { event: e, count: 1 }, env, ["pane", "view"])) {
          e.preventDefault();
          return;
        }
        if (activeHandler && activeHandler(e)) {
          e.preventDefault();
          return;
        }
        return;
      }

      // Digits buffer as vim counts; a lone 1-9 replays as a project jump on
      // timeout or when the next chord isn't countable (see keymap/count.ts).
      if (/^[0-9]$/.test(e.key) && counts.feedDigit(e.key)) {
        e.preventDefault();
        return;
      }

      const chord = chordOf(e);
      if (!chord) return;
      const countable = isCountable(chord, env) || LEGACY_COUNTABLE.has(chord);
      const count = counts.resolveForChord(countable);
      const ctx = { event: e, count };

      // 1. Registered pane/view scopes (migrated views).
      if (dispatchChord(chord, ctx, env, ["pane", "view"])) {
        e.preventDefault();
        return;
      }

      // 2. Legacy view handler (unmigrated views). Counts replay the handler.
      if (activeHandler) {
        const repeats = LEGACY_COUNTABLE.has(chord) ? Math.min(count, MAX_COUNT_REPLAY) : 1;
        let consumed = false;
        for (let i = 0; i < repeats; i++) {
          if (!activeHandler(e)) break;
          consumed = true;
        }
        if (consumed) {
          e.preventDefault();
          return;
        }
      }

      // 3. Global scope.
      if (dispatchChord(chord, ctx, env, ["global"])) {
        e.preventDefault();
        return;
      }
    }

    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      unregisterGlobal();
      counts.dispose();
    };
  }, []);
}

// Late-bound accessor for the UI store to avoid an import cycle
// (keyboard ← views ← ui, and here ui ← keyboard would loop).
import { useUI } from "../store/ui";
function uiApi() {
  return useUI.getState();
}

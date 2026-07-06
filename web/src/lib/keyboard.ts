// Keyboard system providing TUI-parity shortcuts on desktop.
//
// One global listener (useGlobalKeyboard, mounted in Dashboard) normalizes
// each keydown into a Chord and dispatches through the keymap registry in
// tier order: pane scope, then the active view scope, then the global scope.
// Global keys are declared in keymap/global.ts; every view registers its
// ActionSpec table via useActions (views/*/keymap.ts).

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
      // Alt+HJKL resize); otherwise leave them to the browser (Ctrl-F,
      // Ctrl-W, Alt+Left, ...). Global-tier bindings deliberately don't
      // participate — no global chord uses modifiers beyond the pre-guard
      // Cmd+;/Cmd+. handled above.
      if (e.ctrlKey || e.metaKey || e.altKey) {
        const chord = chordOf(e);
        if (chord && dispatchChord(chord, { event: e, count: 1 }, env, ["pane", "view"])) {
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
      const count = counts.resolveForChord(isCountable(chord, env));
      const ctx = { event: e, count };

      // Pane and view scopes first, then the global scope.
      if (dispatchChord(chord, ctx, env, ["pane", "view"])) {
        e.preventDefault();
        return;
      }
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

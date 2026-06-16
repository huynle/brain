// Keyboard system providing TUI-parity shortcuts on desktop.
//
// One global listener (useGlobalKeyboard, mounted in Dashboard) handles the
// cross-view keys — content-tab cycling (h/l/[/]), project tabs (H/L/1-9),
// help (?), settings (S), wrap (w), refresh (r/R) — and otherwise delegates to
// the active view's handler, registered via useViewKeyboard. Views build their
// handler with handleListNavKey() for j/k/g/G + a switch for their action keys.

import { useEffect, useRef } from "react";
import { useNav } from "../store/nav";

export type ViewKeyHandler = (e: KeyboardEvent) => boolean;

let activeHandler: ViewKeyHandler | null = null;

/** Register the active view's key handler for the lifetime of the component. */
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
  return !!document.querySelector(".modal-backdrop");
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

interface GlobalKeyboardOpts {
  projects: string[];
  allLabel: string; // the ALL_PROJECTS sentinel
  onRefresh: () => void;
  onPauseToggle: () => void; // p — pause/resume active project (or all on the All tab)
  onPauseAll: () => void; // P — pause/resume active project (all only on All tab)
}

export function useGlobalKeyboard(opts: GlobalKeyboardOpts) {
  const optsRef = useRef(opts);
  optsRef.current = opts;

  // The listener is installed once; it reads the latest opts via the ref.
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      const opts = optsRef.current;
      const nav = useNav.getState();

      // Help overlay swallows everything.
      if (nav.helpOpen) {
        if (e.key === "?" || e.key === "Escape" || e.key === "q") {
          e.preventDefault();
          nav.setHelpOpen(false);
        }
        return;
      }

      // Quick project switcher: Cmd/Ctrl+K opens the searchable picker from
      // anywhere (even while typing in a field). Handled before the modifier and
      // editable-target guards below.
      if ((e.metaKey || e.ctrlKey) && (e.key === "k" || e.key === "K")) {
        e.preventDefault();
        uiApi().setProjectSheetOpen(true);
        return;
      }

      // Don't hijack typing or modal interactions.
      if (isEditableTarget(e.target)) return;
      if (anyModalOpen()) return;
      if (e.metaKey || e.ctrlKey || e.altKey) return;

      // Delegate to the active view first (its action + list-nav keys).
      if (activeHandler && activeHandler(e)) {
        e.preventDefault();
        return;
      }

      // ── Cross-view global keys ────────────────────────────────
      // Import here to read the latest store APIs without stale closures.
      const ui = uiApi();
      const tabs = [opts.allLabel, ...opts.projects];

      switch (e.key) {
        case "?":
          nav.setHelpOpen(true);
          break;
        case "S":
          ui.setSettingsOpen(true);
          break;
        case "w":
          ui.toggleWrap();
          break;
        // h/l (and [/]) cycle content tabs (Tasks / Brain / Automations / …).
        case "l":
        case "]":
          ui.cycleView(1);
          break;
        case "h":
        case "[":
          ui.cycleView(-1);
          break;
        case "R":
          ui.setView("runners");
          break;
        // H/L (shift) switch between project tabs in multi-project mode.
        case "L": {
          const i = tabs.indexOf(ui.activeProject);
          ui.setActiveProject(tabs[Math.min(i + 1, tabs.length - 1)]);
          break;
        }
        case "H": {
          const i = tabs.indexOf(ui.activeProject);
          ui.setActiveProject(tabs[Math.max(i - 1, 0)]);
          break;
        }
        case "r":
          opts.onRefresh();
          break;
        case "p":
          opts.onPauseToggle();
          break;
        case "P":
          opts.onPauseAll();
          break;
        case "Escape":
          if (nav.selectedCount() > 0) nav.clearSelect();
          else return;
          break;
        default: {
          if (/^[1-9]$/.test(e.key)) {
            const idx = Number(e.key) - 1;
            if (idx < tabs.length) ui.setActiveProject(tabs[idx]);
            else return;
            break;
          }
          return; // not handled
        }
      }
      e.preventDefault();
    }

    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);
}

// Late-bound accessor for the UI store to avoid an import cycle
// (keyboard ← views ← ui, and here ui ← keyboard would loop).
import { useUI } from "../store/ui";
function uiApi() {
  return useUI.getState();
}

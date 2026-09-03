/**
 * Global keyboard shortcut hook.
 *
 * ⌘K / Ctrl+K — toggle command palette
 * ⌘/ / Ctrl+/ — toggle assistant panel
 * ⌘B / Ctrl+B — toggle sidebar
 * ⌘1 / Ctrl+1 — go to Overview
 * ⌘2 / Ctrl+2 — go to Focus
 * ⌘3 / Ctrl+3 — go to Entries
 * ⇧⌘X / Ctrl+⇧+X — close the current pane (portable)
 * Ctrl+W — close the current pane (macOS only, see isMacLike)
 * Esc — close open drawer/palette/modal (handled by respective components)
 *
 * Shortcuts are suppressed when focus is inside an <input>, <textarea>,
 * or a contenteditable element to avoid stealing keystrokes.
 */
import { useEffect } from "react";
import { useWorkspace } from "../store/workspace";

/**
 * Whether Ctrl+W is ours to claim.
 *
 * macOS binds ⌘W to close-tab and leaves Ctrl+W free, so on a Mac the
 * keydown reaches the page and we can use it — which is the binding the
 * app's author actually asked for.
 *
 * Everywhere else Ctrl+W IS close-tab. Claiming it there is worse than
 * useless: on the engines that still deliver the keydown our handler
 * closes the pane, `docks` is persisted, and then the browser closes the
 * tab anyway — so the pane is silently gone on next launch, for a
 * keystroke the user pressed meaning "close this tab". Better to not
 * claim it and let the browser do exactly what they expect.
 *
 * ⌘W is never claimed on any platform: it is reserved everywhere, and in
 * the installed PWA (display: standalone) it closes the app window.
 */
function isMacLike(): boolean {
  if (typeof navigator === "undefined") return false;
  const p =
    (navigator as { userAgentData?: { platform?: string } }).userAgentData
      ?.platform ||
    navigator.platform ||
    "";
  return /mac|iphone|ipad|ipod/i.test(p);
}

function isTypingContext(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return true;
  if (target.isContentEditable) return true;
  return false;
}

export function useGlobalKeyboard(): void {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const mod = e.metaKey || e.ctrlKey;
      if (!mod) return;
      if (e.defaultPrevented) return;
      if (isTypingContext(e.target)) return;

      // Ctrl+W — the requested binding, macOS only, and never with ⌘.
      if (
        e.key.toLowerCase() === "w" &&
        e.ctrlKey &&
        !e.metaKey &&
        !e.altKey &&
        !e.shiftKey &&
        isMacLike()
      ) {
        e.preventDefault();
        useWorkspace.getState().closeCurrentLeaf();
        return;
      }

      switch (e.key.toLowerCase()) {
        case "k":
          e.preventDefault();
          useWorkspace.getState().toggleCommand();
          break;
        case "/":
          e.preventDefault();
          useWorkspace.getState().toggleAssistant();
          break;
        case "b":
          e.preventDefault();
          useWorkspace.getState().toggleSidebarCollapsed();
          break;
        case "1":
          e.preventDefault();
          useWorkspace.getState().setView("overview");
          break;
        case "2":
          e.preventDefault();
          useWorkspace.getState().setView("focus");
          break;
        case "3":
          e.preventDefault();
          useWorkspace.getState().setView("entries");
          break;
        // The portable close binding. Shift is checked EXPLICITLY: this
        // switch only gates on meta-or-ctrl, so a bare `case "x"` would
        // also swallow ⌘X (cut).
        case "x":
          if (!e.shiftKey || e.altKey) break;
          e.preventDefault();
          useWorkspace.getState().closeCurrentLeaf();
          break;
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
}

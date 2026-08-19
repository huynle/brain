/**
 * Global keyboard shortcut hook.
 *
 * ⌘K / Ctrl+K — toggle command palette
 * ⌘/ / Ctrl+/ — toggle assistant panel
 * ⌘B / Ctrl+B — toggle sidebar
 * ⌘1 / Ctrl+1 — go to Overview
 * ⌘2 / Ctrl+2 — go to Focus
 * ⌘3 / Ctrl+3 — go to Entries
 * Esc — close open drawer/palette/modal (handled by respective components)
 *
 * Shortcuts are suppressed when focus is inside an <input>, <textarea>,
 * or a contenteditable element to avoid stealing keystrokes.
 */
import { useEffect } from "react";
import { useWorkspace } from "../store/workspace";

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
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);
}

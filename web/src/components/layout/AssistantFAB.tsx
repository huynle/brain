import { useUI } from "../../store/ui";
import { useIsMobile } from "../../hooks/useIsMobile";

// Floating action button that opens the Brain Assistant. Mobile-only — desktop
// uses the StatusBar "assistant" button and the cmd/ctrl+. shortcut. Hidden
// while the assistant is already open so it doesn't collide with the sheet.
export function AssistantFAB() {
  const isMobile = useIsMobile();
  const open = useUI((s) => s.assistantOpen);
  const setOpen = useUI((s) => s.setAssistantOpen);
  const focusAssistantPrompt = useUI((s) => s.focusAssistantPrompt);

  if (!isMobile || open) return null;

  return (
    <button
      type="button"
      className="assistant-fab"
      aria-label="Open Brain Assistant"
      title="Brain Assistant"
      onClick={() => { setOpen(true); window.setTimeout(focusAssistantPrompt, 0); }}
    >
      <span className="assistant-fab-glyph" aria-hidden>✦</span>
    </button>
  );
}

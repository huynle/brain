/**
 * MobileNav — wireframe-parity port of `renderMobileNav`.
 *
 * Horizontal scrollable pill nav. Rendered under the topbar on
 * mobile only (via CSS `body.mobile`). Overview / Focus + one pill
 * per live session.
 */
import { useWorkspace } from "../store/workspace";
import { useDraggableShortcut } from "../hooks/useDraggableShortcut";
import { useSessions } from "../hooks/useSessions";

export function MobileNav(): JSX.Element {
  const assistantOpen = useWorkspace((s) => s.assistantOpen);
  const openAssistant = () => useWorkspace.getState().setAssistantOpen(true);
  const shortcut = useDraggableShortcut(openAssistant);
  const view = useWorkspace((s) => s.view);
  const setView = useWorkspace((s) => s.setView);
  const setFocusSession = useWorkspace((s) => s.setFocusSession);
  const focusSessionId = useWorkspace((s) => s.focusSessionId);
  const { sessions } = useSessions();

  return (
    <nav className="mobile-nav" aria-label="Main navigation">
      <button
        type="button"
        className="assistant-shortcut"
        aria-label="Assistant"
        title="Open Assistant (drag to move)"
        aria-pressed={assistantOpen}
        {...shortcut}
      >
        <svg
          width="28"
          height="28"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="1.8"
          aria-hidden="true"
        >
          <path d="M21 11.5a8.5 8.5 0 0 1-8.5 8.5H4l-2 2V11.5a9.5 9.5 0 0 1 19 0Z" />
          <path d="M7 10h10M7 14h6" />
        </svg>
      </button>
      <button
        type="button"
        aria-pressed={view === "overview"}
        className={`pill ${view === "overview" ? "active" : ""}`}
        onClick={() => setView("overview")}
      >
        Overview
      </button>
      <button
        type="button"
        aria-pressed={view === "focus"}
        className={`pill ${view === "focus" ? "active" : ""}`}
        onClick={() => setView("focus")}
      >
        Focus
      </button>
      <button
        type="button"
        aria-pressed={view === "entries"}
        className={`pill ${view === "entries" ? "active" : ""}`}
        onClick={() => setView("entries")}
      >
        Entries
      </button>
      {sessions
        .filter((s) => s.status === "busy" || s.status === "starting")
        .map((s) => (
          <button
            type="button"
            aria-pressed={
              view === "session" && focusSessionId === s.instance_id
            }
            key={s.instance_id}
            className={`pill ${
              view === "session" && focusSessionId === s.instance_id
                ? "active"
                : ""
            }`}
            onClick={() => setFocusSession(s.instance_id)}
          >
            <span
              className="live-dot"
              style={{
                display: "inline-block",
                verticalAlign: "middle",
                marginRight: 4,
              }}
            />
            {s.title || s.task_id || s.instance_id.slice(0, 8)}
          </button>
        ))}
    </nav>
  );
}

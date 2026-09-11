/**
 * MobileNav — wireframe-parity port of `renderMobileNav`.
 *
 * Horizontal scrollable pill nav. Rendered under the topbar on
 * mobile only (via CSS `body.mobile`). Overview / Focus + one pill
 * per live session.
 */
import { useWorkspace } from "../store/workspace";
import { useSessions } from "../hooks/useSessions";

export function MobileNav(): JSX.Element {
  const assistantOpen = useWorkspace((s) => s.assistantOpen);
  const openAssistant = () => useWorkspace.getState().setAssistantOpen(true);
  const view = useWorkspace((s) => s.view);
  const setView = useWorkspace((s) => s.setView);
  const setFocusSession = useWorkspace((s) => s.setFocusSession);
  const focusSessionId = useWorkspace((s) => s.focusSessionId);
  const { sessions } = useSessions();

  return (
    <nav className="mobile-nav" aria-label="Main navigation">
      <button
        type="button"
        className="pill assistant-shortcut"
        aria-pressed={assistantOpen}
        onClick={openAssistant}
      >
        Assistant
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

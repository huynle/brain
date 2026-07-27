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
  const view = useWorkspace((s) => s.view);
  const setView = useWorkspace((s) => s.setView);
  const setFocusSession = useWorkspace((s) => s.setFocusSession);
  const focusSessionId = useWorkspace((s) => s.focusSessionId);
  const { sessions } = useSessions();

  return (
    <div className="mobile-nav">
      <span
        className={`pill ${view === "overview" ? "active" : ""}`}
        onClick={() => setView("overview")}
      >
        Overview
      </span>
      <span
        className={`pill ${view === "focus" ? "active" : ""}`}
        onClick={() => setView("focus")}
      >
        Focus
      </span>
      {sessions
        .filter((s) => s.status === "busy" || s.status === "starting")
        .map((s) => (
          <span
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
              style={{ display: "inline-block", verticalAlign: "middle", marginRight: 4 }}
            />
            {s.title || s.task_id || s.instance_id.slice(0, 8)}
          </span>
        ))}
    </div>
  );
}

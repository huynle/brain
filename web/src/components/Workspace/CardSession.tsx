/**
 * CardSession — wireframe-parity port.
 *
 * Shows a live session summary for the project. If a session is
 * running, render a `.sess-mini` bubble; otherwise show a hint.
 */
import { useMemo } from "react";
import { useSessions } from "../../hooks/useSessions";
import { useWorkspace } from "../../store/workspace";

export interface CardSessionProps {
  projectId: string;
}

export function CardSession({ projectId }: CardSessionProps): JSX.Element {
  const { sessions } = useSessions();
  const setFocusSession = useWorkspace((s) => s.setFocusSession);

  const projectSessions = useMemo(
    () => sessions.filter((s) => s.project_id === projectId),
    [sessions, projectId],
  );

  if (projectSessions.length === 0) {
    return (
      <div style={{ color: "#6b757e", fontSize: 11, padding: "6px 0" }}>
        No active session for {projectId}.
      </div>
    );
  }

  return (
    <div>
      {projectSessions.map((s) => {
        const isLive = s.status === "busy" || s.status === "starting";
        return (
          <div
            key={s.instance_id}
            className="sess-mini"
            onClick={() => setFocusSession(s.instance_id)}
            style={{ cursor: "pointer" }}
          >
            <span className="who">{isLive ? "live" : s.status}</span>
            <span className="txt">
              {s.title || s.task_id || s.instance_id}
            </span>
          </div>
        );
      })}
    </div>
  );
}

/**
 * CardSession — wireframe-parity port.
 *
 * Shows a live session summary for the project. If a session is
 * running, render a `.sess-mini` bubble; otherwise show a hint.
 *
 * Verbs come from `lib/actions/sessionActions` via `useRowActions`, so
 * the bubble carries the same right-click/long-press/keyboard set as
 * the sidebar rows and the runner Processes list. Plain click keeps
 * its original meaning: focus the session view.
 */
import { useMemo } from "react";
import { useSessions } from "../../hooks/useSessions";
import { useSessionActionContext } from "../../hooks/useSessionActionContext";
import { useRowActions } from "../../hooks/useRowActions";
import { buildSessionActions } from "../../lib/actions/sessionActions";
import { useWorkspace } from "../../store/workspace";

export interface CardSessionProps {
  projectId: string;
}

export function CardSession({ projectId }: CardSessionProps): JSX.Element {
  const { sessions } = useSessions();
  const setFocusSession = useWorkspace((s) => s.setFocusSession);
  const actionCtx = useSessionActionContext();
  const { rowProps, overlays } = useRowActions();

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
        const label = s.title || s.task_id || s.instance_id;
        return (
          <div
            key={s.instance_id}
            className="sess-mini"
            {...rowProps(buildSessionActions(s, actionCtx), label, () =>
              setFocusSession(s.instance_id),
            )}
            onClick={() => setFocusSession(s.instance_id)}
            style={{ cursor: "pointer" }}
          >
            <span className="who">{isLive ? "live" : s.status}</span>
            <span className="txt">{label}</span>
          </div>
        );
      })}
      {overlays}
    </div>
  );
}

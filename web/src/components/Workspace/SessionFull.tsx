/**
 * SessionFull — wireframe-parity port of `renderSessionFull`.
 *
 * Renders an active OpenCode instance's stream + right-rail metadata
 * + composer. Uses the existing `useSessions` hook for the instance
 * summary and pulls messages via the assistant/opencode control APIs
 * where available.
 */
import { useMemo } from "react";
import { useWorkspace } from "../../store/workspace";
import { useSessions } from "../../hooks/useSessions";
import { useLive } from "../../lib/sse";

export interface SessionFullProps {
  instanceId: string;
}

export function SessionFull({ instanceId }: SessionFullProps): JSX.Element {
  const setView = useWorkspace((s) => s.setView);
  const setFocusSession = useWorkspace((s) => s.setFocusSession);
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const { sessions } = useSessions();
  const inst = useMemo(
    () => sessions.find((s) => s.instance_id === instanceId),
    [sessions, instanceId],
  );

  const logs = useLive((s) => s.logs);
  const relatedLogs = useMemo(
    () =>
      logs
        .filter(
          (r) =>
            (inst && r.projectId === inst.project_id) ||
            (inst && r.taskId === inst.task_id),
        )
        .slice(-40)
        .reverse(),
    [logs, inst],
  );

  if (!inst) {
    return (
      <div style={{ padding: 40, color: "#6b757e" }}>
        Session not found or already exited.
        <div style={{ marginTop: 10 }}>
          <button
            onClick={() => {
              setFocusSession(undefined);
              setView("overview");
            }}
          >
            ◀ Overview
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="session-view">
      <div className="hdr">
        <span style={{ color: "#f4b23a" }}>{inst.project_id}</span>
        <span style={{ color: "#6b757e" }}>›</span>
        <span>{inst.title || inst.task_id || inst.instance_id}</span>
        <span style={{ color: "#6b757e" }}>· {inst.status}</span>
        {inst.status === "busy" && (
          <span
            style={{
              display: "inline-flex",
              alignItems: "center",
              gap: 4,
              marginLeft: 8,
              color: "#6fca7d",
              fontSize: 10,
            }}
          >
            <span className="live-dot" /> streaming
          </span>
        )}
        <span className="spacer" style={{ flex: 1 }} />
        <button
          onClick={() => {
            setFocusSession(undefined);
            setView("overview");
          }}
        >
          ◀ Overview
        </button>
        <button
          onClick={() =>
            openInFocus(
              "session",
              {
                instance_id: inst.instance_id,
                runner_id: inst.runner_id,
                project_id: inst.project_id,
              },
              inst.title || inst.task_id || inst.instance_id,
            )
          }
        >
          Focus split ⤢
        </button>
      </div>

      <div className="stream">
        {/* We don't yet stream OpenCode messages inline here — the
         *  message stream requires attaching to the runner control
         *  socket. For now render the live logs as a proxy. */}
        {relatedLogs.length === 0 && (
          <div style={{ color: "#6b757e", padding: 12 }}>
            No log lines yet. Streaming will populate as the runner
            emits output.
          </div>
        )}
        {relatedLogs.map((r) => (
          <div key={r.seq} className="msg tool">
            <div className="role">
              <span>runner</span>
              <span style={{ color: "#6b757e" }}>
                · {r.line.timestamp ?? ""}
              </span>
            </div>
            <pre>{r.line.content}</pre>
          </div>
        ))}
      </div>

      <div className="sidebar-r">
        <h5>Session</h5>
        <div className="kv">
          <b>Instance</b>
          <span>{inst.instance_id}</span>
          <b>Runner</b>
          <span>{inst.runner_id}</span>
          <b>Project</b>
          <span>{inst.project_id ?? "—"}</span>
          <b>Task</b>
          <span>{inst.task_id ?? "—"}</span>
          <b>Status</b>
          <span>{inst.status}</span>
        </div>
      </div>

      <div className="composer">
        <input
          type="text"
          placeholder={`Message the agent (mock — not yet wired)`}
        />
        <button>Send</button>
      </div>
    </div>
  );
}

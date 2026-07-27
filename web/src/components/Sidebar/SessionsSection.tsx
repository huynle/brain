/**
 * Sidebar — Sessions section (wireframe-parity).
 *
 * DOM:
 *   .sb-section
 *     .sb-head (▾ Live sessions · N)
 *     .sb-list
 *       .sess-row × N (glyph + name + project + live-dot)
 */
import { useWorkspace } from "../../store/workspace";
import { useSessions } from "../../hooks/useSessions";
import { Loading } from "../common/Loading";
import { ErrorState } from "../common/ErrorState";
import type { OpencodeInstance } from "../../lib/types";

function sessionLabel(inst: OpencodeInstance): string {
  if (inst.title && inst.title.trim()) return inst.title;
  if (inst.project_id && inst.task_id)
    return `${inst.project_id} · ${inst.task_id}`;
  if (inst.project_id) return inst.project_id;
  return inst.instance_id;
}

export function SessionsSection(): JSX.Element {
  const expanded = useWorkspace((s) => s.sidebarSection.sessions);
  const toggle = useWorkspace((s) => s.toggleSidebarSection);
  const setFocusSession = useWorkspace((s) => s.setFocusSession);
  const focusSessionId = useWorkspace((s) => s.focusSessionId);
  const { sessions, isLoading, error, refetch } = useSessions();

  const rows = (() => {
    if (isLoading) return <Loading size="sm" label="Loading…" />;
    if (error) return <ErrorState error={error} onRetry={refetch} />;
    if (sessions.length === 0) {
      return (
        <div style={{ padding: "6px 10px", color: "#6b757e", fontSize: 11 }}>
          No live sessions.
        </div>
      );
    }
    return sessions.map((s) => {
      const active = focusSessionId === s.instance_id;
      const label = sessionLabel(s);
      const isLive = s.status === "busy" || s.status === "starting";
      return (
        <div
          key={s.instance_id}
          className={`sess-row ${active ? "active" : ""}`}
          onClick={() => setFocusSession(s.instance_id)}
          title={label}
        >
          <span className="glyph">{isLive ? "▸" : "○"}</span>
          <span className="name">{label}</span>
          {s.project_id && <span className="proj">{s.project_id}</span>}
          {isLive && <span className="live-dot" />}
        </div>
      );
    });
  })();

  const liveCount = sessions.filter(
    (s) => s.status === "busy" || s.status === "starting",
  ).length;

  return (
    <div className="sb-section" style={{ flex: 1, minHeight: 0 }}>
      <div
        className={`sb-head ${!expanded ? "collapsed" : ""}`}
        onClick={() => toggle("sessions")}
      >
        <span className="caret">▾</span>
        Live sessions
        <span className="count">{liveCount}</span>
      </div>
      {expanded && <div className="sb-list">{rows}</div>}
    </div>
  );
}

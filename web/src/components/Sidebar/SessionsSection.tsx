/**
 * Sidebar — Sessions section (wireframe-parity).
 *
 * DOM:
 *   .sb-section
 *     .sb-head (▾ Live sessions · N)
 *     .sb-list
 *       .sess-row × N (glyph + name + project + live-dot)
 *
 * Verbs come from `lib/actions/sessionActions` via `useRowActions`, so
 * right-click, long-press and keyboard offer the identical set as the
 * session card and the runner Processes rows. Plain click opens the
 * session view.
 *
 * The click builds a live SessionRef from the instance row we already
 * hold (openSessionRef(instanceSessionRef(s))) instead of only handing
 * SessionFull an instance-id string. That makes SessionFull's live flag
 * and header correct immediately — without waiting for the global
 * instances poll to re-resolve the row — so the "· transcript" false
 * state and the missing steer box are gone.
 */
import { useWorkspace } from "../../store/workspace";
import { useSessions } from "../../hooks/useSessions";
import { useSessionActionContext } from "../../hooks/useSessionActionContext";
import { useRowActions } from "../../hooks/useRowActions";
import { buildSessionActions } from "../../lib/actions/sessionActions";
import { instanceSessionRef } from "../../lib/sessionRef";
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
  const openSessionRef = useWorkspace((s) => s.openSessionRef);
  const focusSessionId = useWorkspace((s) => s.focusSessionId);
  const focusSessionRef = useWorkspace((s) => s.focusSessionRef);
  const { sessions, isLoading, error, refetch } = useSessions();
  const actionCtx = useSessionActionContext();
  const { rowProps, overlays } = useRowActions();

  const open = (s: OpencodeInstance) => openSessionRef(instanceSessionRef(s));

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
      // A row is active whether it was opened by the legacy instance-id
      // fast path or by the new live-ref path (openSessionRef sets
      // focusSessionRef and clears focusSessionId).
      const active =
        focusSessionId === s.instance_id ||
        (focusSessionRef?.mode === "live" &&
          focusSessionRef.instance_id === s.instance_id);
      const label = sessionLabel(s);
      const isLive = s.status === "busy" || s.status === "starting";
      return (
        <div
          key={s.instance_id}
          className={`sess-row ${active ? "active" : ""}`}
          {...rowProps(buildSessionActions(s, actionCtx), label, () => open(s))}
          onClick={() => open(s)}
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
      {overlays}
    </div>
  );
}

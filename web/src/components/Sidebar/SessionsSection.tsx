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
 * session card and the runner Processes rows.
 *
 * Click previews in the side panel, double click pins into Focus — the
 * same contract as a task or feature row. Both dock a `session` leaf,
 * whose body is `SessionPane`: the same component the full-page view
 * renders, so a docked live session streams and can be steered rather
 * than being a read-only transcript.
 *
 * Either way the target is a live SessionRef built from the instance row
 * already in hand (`instanceSessionRef(s)`), not a bare instance-id
 * string — that is what makes the pane's live flag and header correct
 * immediately instead of waiting for the global instances poll to
 * re-resolve the row.
 */
import { useWorkspace } from "../../store/workspace";
import { useSessions } from "../../hooks/useSessions";
import { useSessionActionContext } from "../../hooks/useSessionActionContext";
import { useDeferredPreview } from "../../hooks/useDeferredPreview";
import { useRowActions } from "../../hooks/useRowActions";
import { buildSessionActions } from "../../lib/actions/sessionActions";
import { instanceSessionRef } from "../../lib/sessionRef";
import { Loading } from "../common/Loading";
import { ErrorState } from "../common/ErrorState";
import { walkLeaves } from "../../lib/dock";
import type { OpencodeInstance, SessionRef } from "../../lib/types";

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
  // Same click contract as a task or feature row: single click previews
  // in the side panel, double click pins it into Focus.
  // `openOrReuseInSidebar` retargets the one session pane rather than
  // adding a tab, so clicking down a list of live sessions to see what
  // each is doing costs one pane, not one per session.
  const previewInSidebar = useWorkspace((s) => s.openOrReuseInSidebar);
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const docks = useWorkspace((s) => s.docks);
  const { sessions, isLoading, error, refetch } = useSessions();
  const actionCtx = useSessionActionContext();
  const { rowProps, overlays } = useRowActions();

  // The preview waits out the double-click window. A live session pane
  // subscribes to a stream on mount, so opening one on the way to Focus is
  // not just a visual flash — it connects and tears down a transcript
  // nobody asked for.
  const deferred = useDeferredPreview();
  const preview = (s: OpencodeInstance) =>
    deferred.schedule(() =>
      previewInSidebar(
        "session",
        { ref: instanceSessionRef(s) },
        sessionLabel(s),
      ),
    );
  const pin = (s: OpencodeInstance) => {
    deferred.cancel();
    openInFocus("session", { ref: instanceSessionRef(s) }, sessionLabel(s));
  };

  // Which instances are on screen right now, in either dock. This used to
  // be read off `focusSessionId`/`focusSessionRef`, which only the
  // full-screen session view sets — with the row opening a docked pane
  // instead, that marker would never move and every row would read
  // inactive. Asking the docks is also simply the truer question: the
  // highlight means "this is the one you are looking at".
  const openInstanceIds = new Set<string>();
  for (const tree of [docks.sidebar, docks.focus]) {
    if (!tree) continue;
    walkLeaves(tree, (leaf) => {
      if (leaf.kind !== "session") return;
      const ref = (leaf.target as { ref?: SessionRef }).ref;
      if (ref?.mode === "live" && ref.instance_id) {
        openInstanceIds.add(ref.instance_id);
      }
      const legacy = (leaf.target as { instance_id?: string }).instance_id;
      if (legacy) openInstanceIds.add(legacy);
    });
  }

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
      const active = openInstanceIds.has(s.instance_id);
      const label = sessionLabel(s);
      const isLive = s.status === "busy" || s.status === "starting";
      return (
        <div
          key={s.instance_id}
          className={`sess-row ${active ? "active" : ""}`}
          // Enter matches a single click, as on every other row.
          {...rowProps(buildSessionActions(s, actionCtx), label, () =>
            preview(s),
          )}
          onClick={() => preview(s)}
          onDoubleClick={() => pin(s)}
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

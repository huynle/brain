/**
 * panes-v2 SessionLeaf (placeholder).
 *
 * A minimal session view for Phase 7 — shows the session (instance)
 * id and, when available, a link to open the RunnerModal (which is
 * the closest full-detail view we currently ship).
 *
 * The "full" SessionFull view — with live PTY, permissions, prompt
 * composer — is deferred to a follow-up. This placeholder keeps the
 * drag-drop workflow functional so users can already dock a session
 * into a pane.
 */
import { useModal } from "../../../store/modal";
import { useSessions } from "../../../hooks/useSessions";

export function SessionLeaf({
  target,
}: {
  target: Record<string, unknown>;
}): JSX.Element {
  const instanceId = target.instance_id as string | undefined;
  const runnerId = target.runner_id as string | undefined;
  const open = useModal((s) => s.open);
  const { sessions } = useSessions();
  const instance = instanceId
    ? sessions.find((s) => s.instance_id === instanceId)
    : undefined;

  if (!instanceId) {
    return (
      <div style={{ color: "var(--p2-fg-faint)", fontSize: 12 }}>
        No session selected. Drag a session row from the sidebar to
        dock it here.
      </div>
    );
  }

  return (
    <div style={{ fontSize: 12, color: "var(--p2-fg-dim)" }}>
      <div
        style={{
          display: "flex",
          gap: 8,
          alignItems: "center",
          marginBottom: "var(--p2-space-2)",
        }}
      >
        <strong style={{ color: "var(--p2-fg)" }}>Session</strong>
        <code style={{ fontSize: 11 }}>{instanceId}</code>
      </div>
      {instance ? (
        <>
          <div>Status: {instance.status}</div>
          {instance.title && <div>Title: {instance.title}</div>}
          {instance.workdir && (
            <div style={{ marginTop: 4 }}>
              Workdir: <code>{instance.workdir}</code>
            </div>
          )}
        </>
      ) : (
        <div style={{ color: "var(--p2-fg-faint)" }}>
          Instance not currently reported by the runner.
        </div>
      )}
      {runnerId && (
        <button
          type="button"
          className="p2-btn"
          style={{
            marginTop: "var(--p2-space-3)",
            padding: "4px 10px",
            background: "var(--p2-bg-2)",
            color: "var(--p2-fg)",
            border: "1px solid var(--p2-border)",
            borderRadius: "var(--p2-radius-xs)",
            fontSize: 12,
            cursor: "pointer",
          }}
          onClick={() => open("runner", { id: runnerId })}
        >
          Open runner modal
        </button>
      )}
      <div
        style={{
          marginTop: "var(--p2-space-4)",
          padding: "var(--p2-space-2)",
          color: "var(--p2-fg-faint)",
          fontSize: 11,
          borderTop: "1px dashed var(--p2-border)",
        }}
      >
        Session view — full implementation deferred.
      </div>
    </div>
  );
}

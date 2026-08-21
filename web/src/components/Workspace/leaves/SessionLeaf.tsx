/**
 * panes-v2 SessionLeaf — a session transcript docked in a Focus pane.
 *
 * Target shapes (leaf targets are persisted, so both remain readable):
 *   • legacy live rows: { instance_id, runner_id, project_id, session_id? }
 *   • ref-addressed:    { ref: SessionRef }
 *
 * A rehydrated live leaf whose instance has exited falls back to
 * history mode when it knows its session id, instead of rotting into
 * "not found".
 */
import { useMemo } from "react";
import { useSessions } from "../../../hooks/useSessions";
import { useSessionTranscript } from "../../../hooks/useSessionTranscript";
import { Transcript } from "../../Session/Transcript";
import { instanceSessionRef } from "../../../lib/sessionRef";
import type { SessionRef } from "../../../lib/types";

function targetRef(target: Record<string, unknown>): SessionRef | undefined {
  const ref = target.ref as SessionRef | undefined;
  if (ref && (ref.mode === "live" || ref.mode === "history")) return ref;
  const instanceId = target.instance_id as string | undefined;
  const runnerId = target.runner_id as string | undefined;
  if (instanceId && runnerId) {
    return {
      mode: "live",
      runner_id: runnerId,
      instance_id: instanceId,
      session_id: target.session_id as string | undefined,
    };
  }
  return undefined;
}

export function SessionLeaf({
  target,
}: {
  target: Record<string, unknown>;
}): JSX.Element {
  const base = targetRef(target);
  const { sessions } = useSessions();

  const effective: SessionRef | undefined = useMemo(() => {
    if (!base || base.mode === "history") return base;
    const inst = sessions.find((s) => s.instance_id === base.instance_id);
    if (inst) {
      // A persisted leaf target names the session the user docked; it
      // stays pinned, and only a target without one follows the
      // instance's newest. Shared rule — see lib/sessionRef.
      return instanceSessionRef(inst, base.session_id);
    }
    if (base.session_id) {
      return { mode: "history", runner_id: base.runner_id, session_id: base.session_id };
    }
    return undefined;
  }, [base, sessions]);

  const transcript = useSessionTranscript(effective);

  if (!base) {
    return (
      <div style={{ color: "var(--p2-fg-faint)", fontSize: 12 }}>
        No session selected. Drag a session row from the sidebar to
        dock it here.
      </div>
    );
  }

  if (!effective) {
    return (
      <div style={{ color: "var(--p2-fg-faint)", fontSize: 12 }}>
        Session exited before a transcript was discovered.
      </div>
    );
  }

  const sessionId = effective.session_id;

  return (
    <div
      className="session-view"
      style={{ gridTemplateColumns: "1fr", height: "100%" }}
    >
      <div className="hdr" style={{ fontSize: 11 }}>
        <span>{effective.mode === "live" ? "live" : "transcript"}</span>
        <code style={{ fontSize: 10, color: "var(--p2-fg-faint)" }}>
          {sessionId ?? "discovering…"}
        </code>
        <span className="spacer" style={{ flex: 1 }} />
        <span style={{ color: "var(--p2-fg-faint)" }}>{effective.runner_id}</span>
      </div>
      {transcript.error ? (
        <div className="stream" style={{ borderRight: "none" }}>
          <div style={{ color: "#e06c5f", padding: 8, fontSize: 11 }}>
            Transcript unavailable — the recorded runner may be offline.
          </div>
        </div>
      ) : (
        <Transcript
          className="stream"
          messages={transcript.messages}
          emptyText={
            transcript.starting
              ? "Session starting — the runner is still discovering it."
              : transcript.isLoading
                ? "Loading transcript…"
                : "No messages yet."
          }
        />
      )}
    </div>
  );
}

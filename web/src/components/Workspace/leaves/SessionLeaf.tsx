/**
 * panes-v2 SessionLeaf — a session docked in a Focus pane or the
 * sidebar dock.
 *
 * Target shapes (leaf targets are persisted, so both remain readable):
 *   • legacy live rows: { instance_id, runner_id, project_id, session_id? }
 *   • ref-addressed:    { ref: SessionRef }
 *
 * A rehydrated live leaf whose instance has exited falls back to
 * history mode when it knows its session id, instead of rotting into
 * "not found".
 *
 * The body is SessionPane — the same component the runner Processes tab
 * and the full-page view render — so a docked live session streams and
 * can be steered exactly like the one in the modal. This leaf used to
 * render a bare Transcript, which is why a session read from the dock
 * looked frozen and had nowhere to type.
 */
import { useMemo } from "react";
import { useSessions } from "../../../hooks/useSessions";
import { SessionPane } from "../../Session/SessionPane";
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

  const inst = useMemo(
    () =>
      base?.mode === "live"
        ? sessions.find((s) => s.instance_id === base.instance_id)
        : undefined,
    [base, sessions],
  );

  const effective: SessionRef | undefined = useMemo(() => {
    if (!base || base.mode === "history") return base;
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
  }, [base, inst]);

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

  return (
    <SessionPane
      className="session-leaf"
      sref={effective}
      liveLabel={inst?.status === "busy" ? "working" : "live"}
      checkinSeed={
        inst?.title
          ? { title: inst.title }
          : inst?.task_id
            ? { title: inst.task_id }
            : undefined
      }
      headerExtra={
        <span style={{ color: "var(--p2-fg-faint)", fontSize: 10 }}>
          {effective.runner_id}
        </span>
      }
    />
  );
}

/**
 * SessionFull — the full-page session view.
 *
 * Addressed either by a live instance id (sidebar/MobileNav rows) or a
 * SessionRef (history mode for completed tasks, task-verb entry
 * points). Live mode renders the real OpenCode transcript polled at 5s
 * (Phase 2 upgrades to the control SSE stream); history mode renders
 * the instance-independent transcript from the recorded runner. A live
 * ref whose instance has exited falls back to history when it carries a
 * session id.
 */
import { useMemo, useState } from "react";
import { useWorkspace } from "../../store/workspace";
import { useSessions } from "../../hooks/useSessions";
import { useSessionTranscript } from "../../hooks/useSessionTranscript";
import { useLive } from "../../lib/sse";
import { useUI } from "../../store/ui";
import { controlKillInstance } from "../../lib/api";
import { Transcript } from "../Session/Transcript";
import { Composer } from "../Session/Composer";
import { PermissionBanner } from "../Session/PermissionBanner";
import { instanceSessionRef } from "../../lib/sessionRef";
import type { SessionRef, Task } from "../../lib/types";

const EMPTY_TASKS: readonly Task[] = Object.freeze([]);

export interface SessionFullProps {
  instanceId?: string;
  sref?: SessionRef;
}

/**
 * Kill an ad-hoc (continuation) instance. Two-step inline confirm —
 * the session's transcript survives in storage; only the instance dies.
 */
function CloseSessionButton({
  runnerId,
  instanceId,
  onClosed,
}: {
  runnerId: string;
  instanceId: string;
  onClosed: () => void;
}): JSX.Element {
  const toast = useUI((s) => s.toast);
  const [armed, setArmed] = useState(false);
  return (
    <button
      style={{ color: armed ? "#e06c5f" : undefined }}
      title="Kill this continuation instance (the transcript stays readable)"
      onClick={async () => {
        if (!armed) {
          setArmed(true);
          setTimeout(() => setArmed(false), 3000);
          return;
        }
        try {
          await controlKillInstance(runnerId, instanceId);
          toast("Session closed", "success");
          onClosed();
        } catch (err) {
          toast(`Close failed: ${(err as Error)?.message ?? err}`, "error");
        }
      }}
    >
      {armed ? "Confirm close?" : "Close session"}
    </button>
  );
}

export function SessionFull({ instanceId, sref }: SessionFullProps): JSX.Element {
  const setView = useWorkspace((s) => s.setView);
  const setFocusSession = useWorkspace((s) => s.setFocusSession);
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const { sessions } = useSessions();

  const targetInstanceId =
    sref?.mode === "live" ? sref.instance_id : sref ? undefined : instanceId;
  const inst = useMemo(
    () => sessions.find((s) => s.instance_id === targetInstanceId),
    [sessions, targetInstanceId],
  );

  // Resolve the effective ref: an explicit history ref wins; a live
  // instance builds its ref from the registry row; a live ref whose
  // instance is gone degrades to history if it knows its session id.
  const effective: SessionRef | undefined = useMemo(() => {
    if (sref?.mode === "history") return sref;
    if (inst) {
      // An explicitly addressed session stays pinned; otherwise the
      // instance's newest discovered one. Both rules live in
      // instanceSessionRef so this view cannot drift from the others.
      return instanceSessionRef(
        inst,
        sref?.mode === "live" ? sref.session_id : undefined,
      );
    }
    if (sref?.mode === "live" && sref.session_id) {
      return { mode: "history", runner_id: sref.runner_id, session_id: sref.session_id };
    }
    return undefined;
  }, [sref, inst]);

  const transcript = useSessionTranscript(effective);

  // The linked task, for the composer's check-in preset seed.
  const projectTasks =
    useLive((s) => (inst?.project_id ? s.projects[inst.project_id]?.tasks : undefined)) ??
    EMPTY_TASKS;
  const linkedTask = useMemo(
    () => (inst?.task_id ? projectTasks.find((t) => t.id === inst.task_id) : undefined),
    [projectTasks, inst?.task_id],
  );

  const back = () => {
    setFocusSession(undefined);
    setView("overview");
  };

  if (!effective) {
    return (
      <div style={{ padding: 40, color: "#6b757e" }}>
        Session not found or already exited.
        <div style={{ marginTop: 10 }}>
          <button onClick={back}>◀ Overview</button>
        </div>
      </div>
    );
  }

  const live = effective.mode === "live";
  const sessionId = effective.mode === "live" ? effective.session_id : effective.session_id;
  const title = inst
    ? inst.title || inst.task_id || inst.instance_id
    : sessionId || "session";
  const projectLabel =
    inst?.project_id ?? (sref?.mode === "history" ? sref.project_id : undefined);

  return (
    <div className="session-view">
      <div className="hdr">
        {projectLabel && <span style={{ color: "#f4b23a" }}>{projectLabel}</span>}
        {projectLabel && <span style={{ color: "#6b757e" }}>›</span>}
        <span>{title}</span>
        <span style={{ color: "#6b757e" }}>
          · {live ? inst?.status ?? "live" : "transcript"}
        </span>
        {live && transcript.starting && (
          <span style={{ marginLeft: 8, color: "#6b757e", fontSize: 10 }}>
            session starting…
          </span>
        )}
        {live && !transcript.starting && (
          <span
            style={{
              display: "inline-flex",
              alignItems: "center",
              gap: 4,
              marginLeft: 8,
              color: transcript.delivery === "streaming" ? "#6fca7d" : "#6b757e",
              fontSize: 10,
            }}
          >
            {transcript.delivery === "streaming" && (
              <>
                <span className="live-dot" /> streaming
              </>
            )}
            {transcript.delivery === "polling" && "updating · 10s"}
            {transcript.delivery === "ended" && "session ended"}
          </span>
        )}
        <span className="spacer" style={{ flex: 1 }} />
        {inst?.kind === "adhoc" && (
          <CloseSessionButton
            runnerId={effective.runner_id}
            instanceId={inst.instance_id}
            onClosed={back}
          />
        )}
        <button onClick={back}>◀ Overview</button>
        <button
          onClick={() =>
            openInFocus(
              "session",
              inst
                ? {
                    instance_id: inst.instance_id,
                    runner_id: inst.runner_id,
                    project_id: inst.project_id,
                    session_id: sessionId,
                  }
                : { ref: effective },
              typeof title === "string" ? title : undefined,
            )
          }
        >
          Focus split ⤢
        </button>
      </div>

      <div
        className="stream"
        style={{ padding: 0, display: "flex", flexDirection: "column", overflow: "hidden" }}
      >
        {live && inst && (
          <PermissionBanner
            runnerId={effective.runner_id}
            instanceId={inst.instance_id}
            sessionId={sessionId}
          />
        )}
        {transcript.error ? (
          <div style={{ color: "#e06c5f", padding: 12 }}>
            Transcript unavailable — the recorded runner may be offline.
            <div style={{ color: "#6b757e", marginTop: 6, fontSize: 11 }}>
              {String((transcript.error as Error)?.message ?? transcript.error)}
            </div>
          </div>
        ) : (
          <Transcript
            style={{ flex: 1, overflowY: "auto", padding: "8px 12px" }}
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

      <div className="sidebar-r">
        <h5>Session</h5>
        <div className="kv">
          {inst && (
            <>
              <b>Instance</b>
              <span>{inst.instance_id}</span>
            </>
          )}
          <b>Runner</b>
          <span>{effective.runner_id}</span>
          {projectLabel && (
            <>
              <b>Project</b>
              <span>{projectLabel}</span>
            </>
          )}
          {(inst?.task_id || (sref?.mode === "history" && sref.task_id)) && (
            <>
              <b>Task</b>
              <span>{inst?.task_id ?? (sref?.mode === "history" ? sref.task_id : "")}</span>
            </>
          )}
          <b>Session</b>
          <span>{sessionId ?? "discovering…"}</span>
          {sref?.mode === "history" && sref.workdir && (
            <>
              <b>Workdir</b>
              <span>{sref.workdir}</span>
            </>
          )}
          <b>Mode</b>
          <span>{live ? "live" : "history"}</span>
        </div>
      </div>

      {live && inst && sessionId && (
        <Composer
          target={{
            runner_id: effective.runner_id,
            instance_id: inst.instance_id,
            session_id: sessionId,
          }}
          checkinSeed={
            linkedTask
              ? {
                  title: linkedTask.title || linkedTask.id,
                  request: linkedTask.content,
                }
              : inst.title
                ? { title: inst.title }
                : undefined
          }
        />
      )}
    </div>
  );
}

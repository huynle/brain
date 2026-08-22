/**
 * SessionFull — the full-page session view.
 *
 * Addressed either by a live instance id (sidebar/MobileNav rows) or a
 * SessionRef (history mode for completed tasks, task-verb entry
 * points).
 *
 * Detail area (full parity with the runner Processes tab):
 *   • When a live/known instance is in hand, the middle column hosts the
 *     runner's own ProcessChat / ProcessRawLog panes, chosen by a
 *     Chat / Raw-log toggle. ProcessChat brings its own header (mode +
 *     delivery pill + session id + toggle), the transcript, and the
 *     steer Composer — including the explicit read-only note when
 *     steering isn't yet possible. Reusing those components means the
 *     sidebar-opened view cannot drift from the Processes tab.
 *   • When there is no live instance but a history ref exists (a
 *     completed-task transcript opened via a task verb), the middle
 *     column renders the read-only transcript. No composer — the
 *     process is gone.
 *
 * SessionFull keeps its own outer chrome: the breadcrumb header
 * (project label, title, Overview / Focus split / Close for adhoc) and
 * the right-hand metadata panel.
 */
import { useMemo, useState } from "react";
import { useWorkspace } from "../../store/workspace";
import { useSessions } from "../../hooks/useSessions";
import { useSessionTranscript } from "../../hooks/useSessionTranscript";
import { useUI } from "../../store/ui";
import { controlKillInstance } from "../../lib/api";
import { Transcript } from "../Session/Transcript";
import { ProcessChat, ProcessRawLog, ViewToggle } from "../RunnerProcesses";
import { instanceSessionRef } from "../../lib/sessionRef";
import {
  chatCapability,
  resolveProcessView,
  sessionFullDetailMode,
  type ProcessView,
} from "../../lib/processes";
import type { SessionRef } from "../../lib/types";

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

  // Chat / Raw-log override for the runner panes (instance mode only).
  const [viewOverride, setViewOverride] = useState<ProcessView | undefined>(
    undefined,
  );

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

  // Read-only transcript for the history-only branch (no live instance).
  // ProcessChat resolves its own transcript in the instance branch, so
  // this is only consumed when `mode === "history"`.
  const historyTranscript = useSessionTranscript(inst ? undefined : effective);

  const back = () => {
    setFocusSession(undefined);
    setView("overview");
  };

  const mode = sessionFullDetailMode({
    hasInstance: !!inst,
    hasEffectiveRef: !!effective,
  });

  if (mode === "not-found" || !effective) {
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
  const sessionId = effective.session_id;
  const title = inst
    ? inst.title || inst.task_id || inst.instance_id
    : sessionId || "session";
  const projectLabel =
    inst?.project_id ?? (sref?.mode === "history" ? sref.project_id : undefined);

  // Instance mode: the runner's Chat / Raw-log panes carry the header,
  // transcript, and steer composer. History mode: a plain header word.
  const view = inst ? resolveProcessView(inst, viewOverride) : "chat";
  const chatEnabled = inst ? chatCapability(inst) !== "none" : false;
  const toggle = inst ? (
    <ViewToggle
      view={view}
      chatEnabled={chatEnabled}
      onView={(v) => setViewOverride(v)}
    />
  ) : null;

  return (
    <div className="session-view session-full">
      <div className="hdr">
        {projectLabel && <span style={{ color: "#f4b23a" }}>{projectLabel}</span>}
        {projectLabel && <span style={{ color: "#6b757e" }}>›</span>}
        <span>{title}</span>
        <span style={{ color: "#6b757e" }}>
          · {live ? inst?.status ?? "live" : "transcript"}
        </span>
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

      <div className="session-full-main">
        {mode === "instance" && inst && toggle ? (
          view === "chat" ? (
            <ProcessChat inst={inst} toggle={toggle} />
          ) : (
            <ProcessRawLog inst={inst} toggle={toggle} />
          )
        ) : (
          // History-only: read-only transcript, no composer.
          <div
            className="stream"
            style={{
              padding: 0,
              display: "flex",
              flexDirection: "column",
              overflow: "hidden",
              borderRight: "none",
            }}
          >
            {historyTranscript.error ? (
              <div style={{ color: "#e06c5f", padding: 12 }}>
                Transcript unavailable — the recorded runner may be offline.
                <div style={{ color: "#6b757e", marginTop: 6, fontSize: 11 }}>
                  {String(
                    (historyTranscript.error as Error)?.message ??
                      historyTranscript.error,
                  )}
                </div>
              </div>
            ) : (
              <Transcript
                style={{ flex: 1, overflowY: "auto", padding: "8px 12px" }}
                messages={historyTranscript.messages}
                emptyText={
                  historyTranscript.isLoading
                    ? "Loading transcript…"
                    : "No messages yet."
                }
              />
            )}
          </div>
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
    </div>
  );
}

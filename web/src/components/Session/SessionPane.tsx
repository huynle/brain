/**
 * SessionPane — THE session body. One implementation of "show me this
 * session", rendered by every surface that shows one.
 *
 * Before this component the three surfaces had drifted: the runner
 * Processes tab streamed and could steer, the docked Focus/sidebar leaf
 * was a read-only transcript with no delivery state and no composer,
 * and the full-page view had a fourth hand-rolled variant for history.
 * Where a session is being looked at is a layout question; whether it
 * can be steered is a property of the SESSION, so it is decided here
 * and only here.
 *
 * What it renders, top to bottom:
 *   • header — mode word, delivery pill (streaming / updating / ended),
 *     session id, plus whatever chrome the host adds (`headerExtra`).
 *   • PermissionBanner — live sessions only; a blocked permission
 *     request is the one thing that stops a transcript dead.
 *   • Transcript — follows the tail, detaches when the reader scrolls up,
 *     offers "Jump to latest" while detached.
 *   • an always-present input — one box on EVERY session, live or finished.
 *     A live session steers (Composer → prompt_async). A finished session
 *     resumes (ResumeComposer → resume-with-context relaunches the task with
 *     the typed text as injected context). A session with no owning task, or
 *     one still discovering its id, shows the input disabled with the reason,
 *     never hidden — so a read-only-looking view is never a dead end. The
 *     route is a pure decision (sessionSubmitRoute in lib/sessionRef).
 *
 * Steerability is deliberately derived from the REF plus the live
 * delivery state, not from an instance row: a `live` ref means the
 * caller resolved an instance that is up (see lib/sessionRef —
 * instanceTranscriptRef degrades an exited instance to history), and
 * `delivery === "ended"` is the server telling us that instance just
 * went away underneath us.
 */
import { useSessionTranscript } from "../../hooks/useSessionTranscript";
import { useSessions } from "../../hooks/useSessions";
import { Composer } from "./Composer";
import { ResumeComposer } from "./ResumeComposer";
import { PermissionBanner } from "./PermissionBanner";
import { Transcript } from "./Transcript";
import { sessionSubmitRoute } from "../../lib/sessionRef";
import { useUI } from "../../store/ui";
import type { SessionRef } from "../../lib/types";

export interface SessionPaneProps {
  /** The session to show. Undefined renders the "nothing to show" note. */
  sref: SessionRef | undefined;
  /** Seeds the composer's check-in preset (task title / original request). */
  checkinSeed?: { title?: string; request?: string };
  /** Host chrome for the header's right edge (e.g. a Chat/Raw-log toggle). */
  headerExtra?: React.ReactNode;
  /** Word shown in the header for a live session — hosts that know the
   *  process status pass "working" while it is busy. */
  liveLabel?: string;
  /** Extra class on the pane wrapper, for host-specific sizing. */
  className?: string;
  /** Note shown instead of the composer when the host already knows
   *  steering is impossible (e.g. the process has exited). */
  readOnlyNote?: string;
}

/** Live / polling / ended indicator. */
function DeliveryPill({
  delivery,
}: {
  delivery: "streaming" | "polling" | "ended" | "none";
}): JSX.Element | null {
  if (delivery === "none") return null;
  if (delivery === "streaming") {
    return (
      <span className="proc-chat-delivery live">
        <span className="live-dot" /> streaming
      </span>
    );
  }
  return (
    <span className="proc-chat-delivery">
      {delivery === "polling" ? "updating · 10s" : "session ended"}
    </span>
  );
}

export function SessionPane({
  sref,
  checkinSeed,
  headerExtra,
  liveLabel,
  className,
  readOnlyNote,
}: SessionPaneProps): JSX.Element {
  const transcript = useSessionTranscript(sref);

  // The agent and model this session is configured with. Read off the
  // instance rather than the ref, which carries only routing.
  //
  // The transcript already stamps a model on each turn, and that is the
  // better answer for "what produced THIS message". This answers the other
  // question — what the session is set to right now — which a turn cannot,
  // because the newest turn may be scrolled off, or the session may be idle
  // with no assistant turn yet at all.
  // SessionRef is a discriminated union and `instance_id` lives only on the
  // live arm, so it has to be narrowed before it is read — a history ref has
  // no such property at all.
  const { allInstances } = useSessions();
  const instanceId = sref?.mode === "live" ? sref.instance_id : undefined;
  const inst = instanceId
    ? allInstances.find((i) => i.instance_id === instanceId)
    : undefined;
  const sessionModel = inst?.model
    ? (inst.model.split("/").pop() ?? inst.model)
    : "";

  const live = sref?.mode === "live";
  const sessionId = sref?.session_id;
  // The header shows the id CSS-truncated, so the full value must be
  // copyable in one action — a wrong/truncated session id sent to the
  // control API silently no-ops (incident report jc9ky1jn).
  const toast = useUI((s) => s.toast);
  const copySessionId = () => {
    if (!sessionId) return;
    navigator.clipboard
      ?.writeText(sessionId)
      .then(() => toast("Session ID copied", "info"))
      .catch(() => toast("Copy failed", "error"));
  };
  // Route the always-present input: live → steer, finished → resume, else
  // disabled-with-reason. The rule is pure and lives in lib/sessionRef.
  //
  // The owning {project,task} the resume route needs comes from the ref for a
  // history session (it carries task_id/project_id) and from the resolved
  // instance row for a live-ended session (`inst`, resolved above). A session
  // with neither is a task-less adhoc/continuation session — the route falls
  // to disabled by design.
  const owner: { project_id?: string; task_id?: string } | undefined =
    sref?.mode === "history"
      ? { project_id: sref.project_id, task_id: sref.task_id }
      : inst
        ? { project_id: inst.project_id, task_id: inst.task_id }
        : undefined;
  const route = sessionSubmitRoute(sref, transcript.delivery, owner);
  // A host that knows more (e.g. "this process has exited") can still colour
  // the disabled reason; it no longer suppresses the input.
  const disabledNote =
    route.kind === "disabled"
      ? sref?.mode === "history" && readOnlyNote
        ? readOnlyNote
        : route.reason
      : "";

  return (
    <div
      className={`session-view session-pane${className ? ` ${className}` : ""}`}
      style={{ gridTemplateColumns: "1fr" }}
    >
      <div className="hdr">
        <span className="proc-chat-mode">
          {live ? (liveLabel ?? "live") : "transcript"}
        </span>
        {live && transcript.starting ? (
          <span className="proc-chat-delivery">session starting…</span>
        ) : (
          <DeliveryPill delivery={transcript.delivery} />
        )}
        <code
          className={`proc-chat-sid${sessionId ? " copyable" : ""}`}
          title={sessionId ? `Click to copy: ${sessionId}` : undefined}
          onClick={sessionId ? copySessionId : undefined}
        >
          {sessionId ? `${sessionId} ⧉` : "discovering…"}
        </code>
        {inst?.agent && <span className="proc-chat-agent">{inst.agent}</span>}
        {sessionModel && (
          <span className="msg-model" title={inst?.model}>
            {sessionModel}
          </span>
        )}
        <span className="spacer" style={{ flex: 1 }} />
        {headerExtra}
      </div>

      <div className="stream proc-chat-stream">
        {live && sref?.mode === "live" && (
          <PermissionBanner
            runnerId={sref.runner_id}
            instanceId={sref.instance_id}
            sessionId={sessionId}
          />
        )}
        {transcript.error ? (
          <div className="proc-log-empty">
            Transcript unavailable — the runner hosting this session may be
            offline.
            <div style={{ marginTop: 4 }}>
              {String((transcript.error as Error)?.message ?? transcript.error)}
            </div>
          </div>
        ) : (
          <Transcript
            style={{ flex: 1, overflowY: "auto", padding: "8px 10px" }}
            messages={transcript.messages}
            sessionRef={sref}
            resetKey={sessionId}
            follow={live}
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

      {route.kind === "steer" ? (
        <Composer target={route.target} checkinSeed={checkinSeed} />
      ) : route.kind === "resume" ? (
        <ResumeComposer
          target={{ project_id: route.project_id, task_id: route.task_id }}
        />
      ) : (
        <div className="composer proc-chat-note">{disabledNote}</div>
      )}
    </div>
  );
}

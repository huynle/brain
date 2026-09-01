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
 *   • Composer — for a live, addressable session. Otherwise a note
 *     saying why not, so read-only never reads as broken.
 *
 * Steerability is deliberately derived from the REF plus the live
 * delivery state, not from an instance row: a `live` ref means the
 * caller resolved an instance that is up (see lib/sessionRef —
 * instanceTranscriptRef degrades an exited instance to history), and
 * `delivery === "ended"` is the server telling us that instance just
 * went away underneath us.
 */
import { useSessionTranscript } from "../../hooks/useSessionTranscript";
import { Composer } from "./Composer";
import { PermissionBanner } from "./PermissionBanner";
import { Transcript } from "./Transcript";
import { sessionSteerState } from "../../lib/sessionRef";
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

  const live = sref?.mode === "live";
  const sessionId = sref?.session_id;
  // Live, addressable, and the stream has not been closed under us —
  // the rule itself is pure and lives in lib/sessionRef.
  const { canSteer, note } = sessionSteerState(
    sref,
    transcript.delivery,
    readOnlyNote,
  );

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
        <code className="proc-chat-sid" title={sessionId ?? undefined}>
          {sessionId ?? "discovering…"}
        </code>
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

      {canSteer && sref?.mode === "live" ? (
        <Composer
          target={{
            runner_id: sref.runner_id,
            instance_id: sref.instance_id,
            session_id: sessionId as string,
          }}
          checkinSeed={checkinSeed}
        />
      ) : (
        <div className="composer proc-chat-note">{note}</div>
      )}
    </div>
  );
}

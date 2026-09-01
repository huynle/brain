/**
 * useSessionTranscript — mode-aware transcript data for a SessionRef.
 *
 * live    → control SSE stream (message deltas folded by the pure
 *           transcript reducer) over a fetched backlog. The backlog is
 *           refetched on every stream (re)connect to close the gap
 *           between snapshot and first delta; when the stream is down
 *           a 10s poll covers the outage.
 * history → one-shot controlSessionHistory: served by the recorded
 *           runner from a live instance if one still hosts the session,
 *           else from OpenCode's storage.
 *
 * A live ref without a discovered session id yet ("starting") keeps
 * everything disabled and reports `starting: true`.
 */
import { useEffect, useRef, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { controlListMessages, controlSessionHistory } from "../lib/api";
import { openInstanceStream, type InstanceStreamState } from "../lib/instanceStream";
import { applyEvent, mergeBacklog } from "../lib/transcript";
import type { OcMessage, SessionRef } from "../lib/types";

/** Stable empty list, so a stale session tag does not churn consumers. */
const EMPTY_MESSAGES: OcMessage[] = [];

export interface SessionTranscript {
  messages: OcMessage[];
  isLoading: boolean;
  error: unknown;
  /** Live instance exists but session discovery hasn't finished. */
  starting: boolean;
  /** Live-mode delivery: streaming (SSE live), polling (stream down,
   *  10s poll), or ended (server closed the stream — instance exited).
   *  History mode is always "none". */
  delivery: "streaming" | "polling" | "ended" | "none";
  refetch: () => void;
}

export function useSessionTranscript(
  sref: SessionRef | null | undefined,
): SessionTranscript {
  const starting = sref?.mode === "live" && !sref.session_id;
  const live = sref?.mode === "live" && !starting;
  const enabled = !!sref && !starting;

  const [streamState, setStreamState] = useState<InstanceStreamState | null>(null);
  const [ended, setEnded] = useState(false);

  const runnerId = sref?.runner_id ?? "";
  const instanceId = sref?.mode === "live" ? sref.instance_id : "";
  const sessionId = sref?.session_id ?? "";

  /*
   * Delta-fed state for live mode, TAGGED with the session it belongs to.
   *
   * The tag is what makes "switch to another session" safe without an
   * effect that clears the list. Clearing was the obvious implementation
   * and it was wrong in a way that only shows up on the second mount:
   * the reset ran in the stream effect, AFTER the backlog effect had
   * already folded react-query's cached snapshot in, wiping it — and
   * because react-query's structural sharing hands back the SAME array
   * reference when a refetch is deeply equal, the backlog effect never
   * fired again to restore it. A second surface opened on a session that
   * was already loaded showed an empty transcript until the next delta.
   *
   * With the tag, a stale key simply reads as empty and the backlog
   * effect re-seeds it; no ordering between the two effects matters.
   */
  const streamKey = `${runnerId}|${instanceId}|${sessionId}`;
  const [liveState, setLiveState] = useState<{
    key: string;
    messages: OcMessage[];
  }>({ key: "", messages: [] });
  const liveMessages =
    liveState.key === streamKey ? liveState.messages : EMPTY_MESSAGES;

  const query = useQuery<OcMessage[]>({
    queryKey: ["v2", "transcript", sref?.mode ?? "none", runnerId, instanceId, sessionId],
    queryFn: () => {
      if (!sref) return Promise.resolve([]);
      if (sref.mode === "live") {
        return controlListMessages(sref.runner_id, sref.instance_id, sref.session_id!, 200);
      }
      return controlSessionHistory(sref.runner_id, sref.session_id);
    },
    enabled,
    // Streaming replaces polling; the poll only covers stream outages.
    refetchInterval: live && streamState !== "open" && !ended ? 10_000 : false,
    staleTime: sref?.mode === "history" ? 60_000 : 0,
  });

  const refetchRef = useRef(query.refetch);
  refetchRef.current = query.refetch;

  // Fold the backlog snapshot into the delta state. Deltas that arrived
  // after the snapshot was taken are kept; anything under a different
  // session's tag is dropped by starting from [].
  const backlog = query.data;
  useEffect(() => {
    if (!live || !backlog) return;
    setLiveState((prev) => {
      const base = prev.key === streamKey ? prev.messages : EMPTY_MESSAGES;
      const merged = mergeBacklog(backlog, base);
      return merged === base && prev.key === streamKey
        ? prev
        : { key: streamKey, messages: merged };
    });
  }, [live, backlog, streamKey]);

  // The stream itself. Keyed on the instance/session identity; session
  // filtering happens in the reducer (events are per-instance).
  useEffect(() => {
    if (!live || !runnerId || !instanceId || !sessionId) return;
    setEnded(false);
    const dispose = openInstanceStream(runnerId, instanceId, {
      onEvent: (evt) =>
        // applyEvent returns the SAME array for an event this session
        // does not care about (wrong session, non-transcript type), and
        // most events on a per-instance stream are exactly that — so
        // pass that bail through instead of re-wrapping it in a fresh
        // object and re-rendering every consumer.
        setLiveState((prev) => {
          const base = prev.key === streamKey ? prev.messages : EMPTY_MESSAGES;
          const next = applyEvent(base, evt, sessionId);
          return next === base && prev.key === streamKey
            ? prev
            : { key: streamKey, messages: next };
        }),
      onState: (s) => setStreamState(s),
      onConnected: () => void refetchRef.current(),
      onStreamClosed: () => setEnded(true),
    });
    return dispose;
  }, [live, runnerId, instanceId, sessionId, streamKey]);

  const delivery: SessionTranscript["delivery"] = !live
    ? "none"
    : ended
      ? "ended"
      : streamState === "open"
        ? "streaming"
        : "polling";

  return {
    messages: live ? liveMessages : (query.data ?? []),
    isLoading: enabled && query.isLoading && (!live || liveMessages.length === 0),
    error: query.error,
    starting: !!starting,
    delivery,
    refetch: query.refetch,
  };
}

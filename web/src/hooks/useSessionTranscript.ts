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
  // Delta-fed state for live mode; the backlog query below reconciles
  // into it via mergeBacklog whenever a fresh snapshot lands.
  const [liveMessages, setLiveMessages] = useState<OcMessage[]>([]);

  const runnerId = sref?.runner_id ?? "";
  const instanceId = sref?.mode === "live" ? sref.instance_id : "";
  const sessionId = sref?.session_id ?? "";

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

  // Fold the backlog snapshot into the delta state.
  const backlog = query.data;
  useEffect(() => {
    if (!live || !backlog) return;
    setLiveMessages((prev) => mergeBacklog(backlog, prev));
  }, [live, backlog]);

  // The stream itself. Keyed on the instance/session identity; session
  // filtering happens in the reducer (events are per-instance).
  useEffect(() => {
    if (!live || !runnerId || !instanceId || !sessionId) return;
    setEnded(false);
    setLiveMessages([]);
    const dispose = openInstanceStream(runnerId, instanceId, {
      onEvent: (evt) => setLiveMessages((prev) => applyEvent(prev, evt, sessionId)),
      onState: (s) => setStreamState(s),
      onConnected: () => void refetchRef.current(),
      onStreamClosed: () => setEnded(true),
    });
    return dispose;
  }, [live, runnerId, instanceId, sessionId]);

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

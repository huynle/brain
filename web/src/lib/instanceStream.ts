// Per-instance control event stream.
//
// Connects to GET /api/v1/control/runners/{r}/instances/{i}/events —
// the OpenCode /event firehose tunneled over the runner bridge. The
// server collapses every upstream event to the single SSE event name
// "instance_event"; consumers switch on the INNER OpenCode `type`.
//
// fetch-based (not EventSource) so the request carries an
// Authorization header and a stream 401 can run the auth store's
// refresh before reconnecting — EventSource + ?token= would silently
// die when the token expires mid-stream.
//
// One stream per open session surface, by design (the endpoint is
// per-instance). Reconnects with capped exponential backoff.

import { API_BASE } from "./config";
import { useAuth } from "./auth";
import { parseSSEFrame } from "./sse";
import type { OcEvent } from "./types";

export type InstanceStreamState =
  | "connecting"
  | "open"
  | "closed" // server said stream_closed (e.g. instance_exited) — terminal
  | "stopped"; // disposed locally

export interface InstanceStreamHandlers {
  /** An OpenCode event from the instance (already JSON-parsed). */
  onEvent: (evt: OcEvent) => void;
  /** Connection lifecycle, for header chrome. */
  onState?: (state: InstanceStreamState) => void;
  /** Server closed the stream ({reason: "instance_exited" | ...}). */
  onStreamClosed?: (reason: string) => void;
  /** Fired on every (re)connect AFTER the stream is live — callers
   *  refetch the message backlog here to close the fetch/stream gap. */
  onConnected?: () => void;
}

/**
 * Route one parsed SSE frame. Pure — unit-tested separately from the
 * network loop.
 */
export function routeFrame(frame: {
  event: string;
  data: string;
}):
  | { kind: "event"; evt: OcEvent }
  | { kind: "closed"; reason: string }
  | { kind: "connected" }
  | { kind: "ignore" } {
  switch (frame.event) {
    case "instance_event": {
      try {
        const evt = JSON.parse(frame.data) as OcEvent;
        if (evt && typeof evt.type === "string") return { kind: "event", evt };
      } catch {
        // fall through to ignore
      }
      return { kind: "ignore" };
    }
    case "stream_closed": {
      let reason = "closed";
      try {
        const body = JSON.parse(frame.data) as { reason?: string };
        if (body && typeof body.reason === "string") reason = body.reason;
      } catch {
        // keep default
      }
      return { kind: "closed", reason };
    }
    case "connected":
      return { kind: "connected" };
    default:
      // heartbeat and anything unknown
      return { kind: "ignore" };
  }
}

const BACKOFF_START_MS = 1000;
const BACKOFF_CAP_MS = 15_000;

/**
 * Open the stream. Returns a dispose function; safe to call twice.
 */
export function openInstanceStream(
  runnerId: string,
  instanceId: string,
  handlers: InstanceStreamHandlers,
): () => void {
  const url =
    `${API_BASE}/api/v1/control/runners/${encodeURIComponent(runnerId)}` +
    `/instances/${encodeURIComponent(instanceId)}/events`;

  let disposed = false;
  let controller: AbortController | null = null;
  let backoff = BACKOFF_START_MS;
  let timer: ReturnType<typeof setTimeout> | undefined;

  const setState = (s: InstanceStreamState) => {
    if (!disposed) handlers.onState?.(s);
  };

  const scheduleReconnect = () => {
    if (disposed) return;
    timer = setTimeout(run, backoff);
    backoff = Math.min(backoff * 2, BACKOFF_CAP_MS);
  };

  async function run(): Promise<void> {
    if (disposed) return;
    setState("connecting");
    controller = new AbortController();
    try {
      const token = useAuth.getState().token;
      const res = await fetch(url, {
        headers: {
          Accept: "text/event-stream",
          ...(token ? { Authorization: `Bearer ${token}` } : {}),
        },
        signal: controller.signal,
      });

      if (res.status === 401 || res.status === 403) {
        // The api() wrapper's once-refresh doesn't cover hand-rolled
        // streams — run it explicitly, then retry immediately.
        const refreshed = await useAuth.getState().onUnauthorized();
        if (refreshed && !disposed) {
          backoff = BACKOFF_START_MS;
          void run();
        }
        return;
      }
      if (!res.ok || !res.body) {
        scheduleReconnect();
        return;
      }

      setState("open");
      backoff = BACKOFF_START_MS;
      handlers.onConnected?.();

      const reader = res.body.getReader();
      const decoder = new TextDecoder();
      let buf = "";
      for (;;) {
        const { done, value } = await reader.read();
        if (done) break;
        buf += decoder.decode(value, { stream: true });
        for (;;) {
          const sep = buf.indexOf("\n\n");
          if (sep === -1) break;
          const block = buf.slice(0, sep);
          buf = buf.slice(sep + 2);
          const frame = parseSSEFrame(block);
          if (!frame) continue;
          const routed = routeFrame(frame);
          if (disposed) return;
          if (routed.kind === "event") {
            handlers.onEvent(routed.evt);
          } else if (routed.kind === "closed") {
            handlers.onStreamClosed?.(routed.reason);
            setState("closed");
            return; // terminal — instance is gone, don't reconnect
          }
        }
      }
      // Stream ended without stream_closed: transient drop — reconnect.
      scheduleReconnect();
    } catch {
      if (!disposed) scheduleReconnect();
    }
  }

  void run();

  return () => {
    disposed = true;
    if (timer !== undefined) clearTimeout(timer);
    controller?.abort();
    handlers.onState?.("stopped");
  };
}

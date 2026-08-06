// Real-time layer. One long-lived multiplexed fetch stream consumes
// server-sent-events frames for every subscribed project and pushes task
// snapshots, runner updates, and runner logs into a zustand store the UI
// reads from.
//
// Why one stream, not N?
//   Chrome caps HTTP/1.1 at 6 concurrent connections per origin. With one
//   stream per project on a dashboard listing 30+ projects, the browser
//   stalled all subsequent /api/* fetches (automations, entries, task
//   metadata) forever, queued behind long-lived streams that never close.
//   The multiplexed stream (backed by GET /tasks/stream?projects=a,b,c on
//   the server) collapses that fanout to a single socket.
//
// Why fetch-stream instead of EventSource?
//   1. EventSource cannot set an Authorization header, forcing us to pass
//      the access token as ?token= (worse for auth, worse for logs).
//      With fetch we send `Authorization: Bearer <token>` cleanly.
//   2. AbortController gives deterministic teardown without waiting on
//      EventSource's internal buffering.
//
// NOTE — the browser tab favicon spins while the fetch stream is
// in-flight. This is Chrome's correct behavior: it reports "network
// activity in progress" as long as pending requests exist. Both EventSource
// and fetch-stream trip it; only WebSocket (post-upgrade) is exempt. We
// intentionally accept the spinner as the honest indicator that the
// dashboard is streaming live data — the alternatives (polling for tasks
// / feature status / runner logs) lose the real-time feel that's the
// whole point of the panes-v2 shell. With one multiplexed stream instead
// of 30, at least the spinner represents ONE request, not 30.
//
// If we ever migrate to WebSocket (would need matching backend upgrade
// handler + framing), the spinner goes away because post-upgrade WS
// connections aren't counted toward page loading state.
//
// The wire format is standard SSE:
//   event: <name>\n
//   data: <json>\n
//   \n
//
// Every project-scoped event carries a `projectId` field in its data
// payload (see internal/types/types.go SSEEventData). We stream bytes
// through TextDecoderStream, parse frames by splitting on the blank-line
// separator, and demux to the right project slice by that field.

import { create } from "zustand";
import { useAuth } from "./auth";
import type {
  LogLine,
  RunnerInfo,
  SSERunnerLog,
  SSERunnersUpdate,
  SSETasksSnapshot,
  Task,
  TaskStats,
} from "./types";

export interface ProjectLive {
  tasks: Task[];
  stats?: TaskStats;
  cycles?: string[][];
  connected: boolean;
  error: string | null;
}

export interface LogRecord {
  seq: number;
  projectId: string;
  taskId: string;
  runnerId: string;
  line: LogLine;
}

const MAX_LOGS = 3000;

interface LiveState {
  projects: Record<string, ProjectLive>;
  runners: RunnerInfo[];
  logs: LogRecord[];
  _seq: number;
  setProject: (id: string, patch: Partial<ProjectLive>) => void;
  setRunners: (r: RunnerInfo[]) => void;
  appendLogs: (
    projectId: string,
    taskId: string,
    runnerId: string,
    lines: LogLine[],
  ) => void;
  reset: (id: string) => void;
}

export const useLive = create<LiveState>((set) => ({
  projects: {},
  runners: [],
  logs: [],
  _seq: 0,
  setProject: (id, patch) =>
    set((s) => {
      const base: ProjectLive = s.projects[id] ?? {
        tasks: [],
        connected: false,
        error: null,
      };
      return { projects: { ...s.projects, [id]: { ...base, ...patch } } };
    }),
  setRunners: (r) => set({ runners: r }),
  appendLogs: (projectId, taskId, runnerId, lines) =>
    set((s) => {
      let seq = s._seq;
      const recs = lines.map((line) => ({
        seq: ++seq,
        projectId,
        taskId,
        runnerId,
        line,
      }));
      const merged = [...s.logs, ...recs];
      return {
        logs: merged.length > MAX_LOGS ? merged.slice(-MAX_LOGS) : merged,
        _seq: seq,
      };
    }),
  reset: (id) =>
    set((s) => {
      const next = { ...s.projects };
      delete next[id];
      return { projects: next };
    }),
}));

// ─── SSE frame parser ────────────────────────────────────────────

interface SSEFrame {
  event: string;
  data: string;
}

/**
 * Parse a single SSE frame block (text between blank lines) into
 * {event, data}. Handles multi-line data (concatenated with \n per the
 * SSE spec). Returns null if the frame is a comment or has no data.
 *
 * Exported for unit testing.
 */
export function parseSSEFrame(block: string): SSEFrame | null {
  const lines = block.split("\n");
  let event = "message";
  const dataParts: string[] = [];
  for (const line of lines) {
    if (!line || line.startsWith(":")) continue; // blank or comment
    const colon = line.indexOf(":");
    const field = colon === -1 ? line : line.slice(0, colon);
    const value =
      colon === -1
        ? ""
        : line[colon + 1] === " "
          ? line.slice(colon + 2)
          : line.slice(colon + 1);
    if (field === "event") event = value;
    else if (field === "data") dataParts.push(value);
    // id / retry ignored — we don't use them
  }
  if (dataParts.length === 0) return null;
  return { event, data: dataParts.join("\n") };
}

// ─── Stream manager ──────────────────────────────────────────────
//
// Design note — Aug 2026 rewrite. The original manager opened one
// long-lived fetch stream PER project. On dashboards with 30+ projects
// that saturated Chrome's HTTP/1.1 per-origin socket pool (6/origin) and
// stalled every subsequent /api/* fetch (automations, entries, task
// metadata) indefinitely — the automation pane sat "Loading…" forever
// because its GET was queued behind streams that never close.
//
// Now the manager opens ONE multi-project stream against
// /api/v1/tasks/stream?projects=a,b,c and demuxes frames by the
// projectId field the backend stamps on every SSEEventData payload.
// Runner-lifecycle events carry no projectId; we route them to the
// global runners slice via useLive.setRunners.

// Extract projectId from an SSE frame's parsed data payload. The backend
// stamps every project-scoped event with SSEEventData.projectId (the
// JSON field is "projectId" — see internal/types/types.go). Returns null
// for runner-lifecycle events that legitimately carry no project scope.
function projectIdOf(data: unknown): string | null {
  if (data && typeof data === "object" && "projectId" in data) {
    const v = (data as { projectId?: unknown }).projectId;
    if (typeof v === "string" && v) return v;
  }
  return null;
}

class MultiStream {
  private controller: AbortController | null = null;
  private closed = false;
  private retry = 0;
  private timer: number | null = null;
  private projectIds: string[];

  constructor(projectIds: string[]) {
    // Sort so a stable set of projects produces a stable URL (matters
    // for HTTP caching / debug clarity, not correctness).
    this.projectIds = [...projectIds].sort();
  }

  start(): void {
    this.closed = false;
    void this.open();
  }

  private url(): string {
    const qs = this.projectIds.map(encodeURIComponent).join(",");
    return `/api/v1/tasks/stream?projects=${qs}`;
  }

  /**
   * Route a parsed frame to useLive. The stream carries events for many
   * projects on one connection, so we read the projectId out of the
   * frame's data payload instead of using a hardcoded per-stream field
   * (as the old per-project Stream did).
   */
  private handleFrame(frame: SSEFrame): void {
    const live = useLive.getState();

    let payload: unknown = null;
    if (frame.data) {
      try {
        payload = JSON.parse(frame.data);
      } catch {
        return; // malformed frame — drop
      }
    }

    switch (frame.event) {
      case "connected": {
        // Fired once per project on stream open. Each project flips its
        // own ProjectLive.connected flag; we also reset retry on any
        // successful connected frame (the first one on a fresh
        // connection is enough).
        this.retry = 0;
        const pid = projectIdOf(payload);
        if (pid) {
          live.setProject(pid, { connected: true, error: null });
        }
        break;
      }
      case "heartbeat":
        // No-op; connection liveness is implicit in the reader loop.
        break;
      case "tasks_snapshot": {
        const d = payload as SSETasksSnapshot | null;
        if (!d) break;
        const pid = projectIdOf(d);
        if (!pid) break;
        live.setProject(pid, {
          tasks: d.tasks || [],
          stats: d.stats,
          cycles: d.cycles,
          connected: true,
          error: null,
        });
        break;
      }
      case "runners_update": {
        const d = payload as SSERunnersUpdate | null;
        if (d?.runners) live.setRunners(d.runners);
        break;
      }
      case "runner_registered":
      case "runner_offline":
        // Delta events on the runner-lifecycle topic. The backend still
        // emits full runners_update snapshots too, so we don't need to
        // reconcile deltas here — a snapshot will follow. Kept as an
        // explicit no-op so an unknown-event log line doesn't imply
        // these are dropped by accident.
        break;
      case "runner_log": {
        const d = payload as SSERunnerLog | null;
        if (!d?.lines?.length) break;
        // runner_log carries projectId in its own field, not in
        // SSEEventData. Fall back to the payload's projectId if the
        // envelope one is missing.
        const pid =
          projectIdOf(d) ??
          (typeof (d as { projectId?: unknown }).projectId === "string"
            ? (d as { projectId: string }).projectId
            : null);
        if (!pid) break;
        live.appendLogs(pid, d.taskId, d.runnerId, d.lines);
        break;
      }
    }
  }

  private async open(): Promise<void> {
    if (this.closed) return;
    if (this.projectIds.length === 0) return;

    const controller = new AbortController();
    this.controller = controller;

    const token = useAuth.getState().token;
    const headers: Record<string, string> = {
      Accept: "text/event-stream",
      "Cache-Control": "no-cache",
    };
    if (token) headers.Authorization = `Bearer ${token}`;

    try {
      const res = await fetch(this.url(), {
        method: "GET",
        headers,
        credentials: "same-origin",
        signal: controller.signal,
      });

      if (!res.ok || !res.body) {
        throw new Error(`stream: HTTP ${res.status}`);
      }

      const reader = res.body
        .pipeThrough(new TextDecoderStream())
        .getReader();
      let buffer = "";

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += value;
        while (true) {
          const lf = buffer.indexOf("\n\n");
          const crlf = buffer.indexOf("\r\n\r\n");
          let sep: number;
          let skip: number;
          if (lf === -1 && crlf === -1) break;
          if (crlf !== -1 && (lf === -1 || crlf < lf)) {
            sep = crlf;
            skip = 4;
          } else {
            sep = lf;
            skip = 2;
          }
          const block = buffer.slice(0, sep);
          buffer = buffer.slice(sep + skip);
          const frame = parseSSEFrame(block);
          if (frame) this.handleFrame(frame);
        }
      }
    } catch (err) {
      if ((err as { name?: string })?.name === "AbortError") return;
      // On error, mark ALL subscribed projects as disconnected — the
      // failure isn't scoped to any one of them.
      const live = useLive.getState();
      for (const pid of this.projectIds) {
        live.setProject(pid, {
          connected: false,
          error: (err as Error).message ?? String(err),
        });
      }
    }

    this.controller = null;
    if (this.closed) return;
    // Mark all as disconnected on normal end-of-stream too, then retry.
    const live = useLive.getState();
    for (const pid of this.projectIds) {
      live.setProject(pid, { connected: false });
    }
    this.retry = Math.min(this.retry + 1, 5);
    const delay = Math.min(1000 * 2 ** this.retry, 15000);
    this.timer = window.setTimeout(() => void this.open(), delay);
  }

  stop(): void {
    this.closed = true;
    if (this.timer) {
      window.clearTimeout(this.timer);
      this.timer = null;
    }
    this.controller?.abort();
    this.controller = null;
  }
}

class StreamManager {
  private stream: MultiStream | null = null;
  private currentIds: string[] = [];

  /** Open a single multiplexed stream for exactly these projects. */
  sync(projectIds: string[]): void {
    const wanted = [...projectIds].sort();
    if (
      this.stream &&
      wanted.length === this.currentIds.length &&
      wanted.every((id, i) => id === this.currentIds[i])
    ) {
      // No change; keep existing stream alive.
      return;
    }
    // Tear down projects that are leaving so their ProjectLive slots
    // don't linger with stale connected=true.
    const dropping = this.currentIds.filter((id) => !wanted.includes(id));
    for (const id of dropping) useLive.getState().reset(id);

    this.stream?.stop();
    this.currentIds = wanted;
    if (wanted.length === 0) {
      this.stream = null;
      return;
    }
    this.stream = new MultiStream(wanted);
    this.stream.start();
  }

  /** Tear down and reopen (e.g. after token change or manual refresh). */
  restartAll(): void {
    const ids = this.currentIds;
    this.stream?.stop();
    this.stream = null;
    this.currentIds = [];
    this.sync(ids);
  }

  stopAll(): void {
    this.stream?.stop();
    this.stream = null;
    this.currentIds = [];
  }
}

export const streams = new StreamManager();

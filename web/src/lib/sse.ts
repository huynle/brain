// Real-time layer. One long-lived fetch stream per active project consumes
// server-sent-events frames and pushes task snapshots, runner updates, and
// runner logs into a zustand store the UI reads from.
//
// Why fetch-stream instead of EventSource?
//   1. EventSource cannot set an Authorization header, forcing us to pass
//      the access token as ?token= (worse for auth, worse for logs).
//      With fetch we send `Authorization: Bearer <token>` cleanly.
//   2. AbortController gives deterministic teardown without waiting on
//      EventSource's internal buffering.
//
// NOTE — the browser tab favicon spins while any of these fetch streams
// are in-flight. This is Chrome's correct behavior: it reports "network
// activity in progress" as long as pending requests exist. Both EventSource
// and fetch-stream trip it; only WebSocket (post-upgrade) is exempt. We
// intentionally accept the spinner as the honest indicator that the
// dashboard is streaming live data — the alternatives (polling for tasks
// / feature status / runner logs) lose the real-time feel that's the
// whole point of the panes-v2 shell. Users learn to ignore it, and
// Firefox/Safari handle the indicator more gracefully than Chrome does.
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
// We stream bytes through TextDecoderStream and parse frames by splitting
// on the blank-line separator.

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

class Stream {
  private controller: AbortController | null = null;
  private closed = false;
  private retry = 0;
  private timer: number | null = null;
  private projectId: string;

  constructor(projectId: string) {
    this.projectId = projectId;
  }

  start(): void {
    this.closed = false;
    void this.open();
  }

  private url(): string {
    return `/api/v1/tasks/${encodeURIComponent(this.projectId)}/stream`;
  }

  private handleFrame(frame: SSEFrame): void {
    const live = useLive.getState();
    switch (frame.event) {
      case "connected":
        this.retry = 0;
        live.setProject(this.projectId, { connected: true, error: null });
        break;
      case "tasks_snapshot":
        try {
          const d = JSON.parse(frame.data) as SSETasksSnapshot;
          live.setProject(this.projectId, {
            tasks: d.tasks || [],
            stats: d.stats,
            cycles: d.cycles,
            connected: true,
            error: null,
          });
        } catch {
          /* ignore malformed */
        }
        break;
      case "runners_update":
        try {
          const d = JSON.parse(frame.data) as SSERunnersUpdate;
          live.setRunners(d.runners || []);
        } catch {
          /* ignore */
        }
        break;
      case "runner_log":
        try {
          const d = JSON.parse(frame.data) as SSERunnerLog;
          if (d.lines?.length) {
            live.appendLogs(
              this.projectId,
              d.taskId,
              d.runnerId,
              d.lines,
            );
          }
        } catch {
          /* ignore */
        }
        break;
    }
  }

  private async open(): Promise<void> {
    if (this.closed) return;

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
        throw new Error(`stream ${this.projectId}: HTTP ${res.status}`);
      }

      const reader = res.body
        .pipeThrough(new TextDecoderStream())
        .getReader();
      let buffer = "";

      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        buffer += value;
        // SSE frames are separated by \n\n (or \r\n\r\n). Pull as
        // many complete frames as possible out of the buffer on each
        // read.
        // eslint-disable-next-line no-constant-condition
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
      useLive
        .getState()
        .setProject(this.projectId, {
          connected: false,
          error: (err as Error).message ?? String(err),
        });
    }

    // Normal end-of-stream OR error → schedule reconnect if not closed.
    this.controller = null;
    if (this.closed) return;
    useLive.getState().setProject(this.projectId, { connected: false });
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
  private streams = new Map<string, Stream>();

  /** Open streams for exactly these projects; close any others. */
  sync(projectIds: string[]): void {
    const wanted = new Set(projectIds);
    for (const [id, s] of this.streams) {
      if (!wanted.has(id)) {
        s.stop();
        this.streams.delete(id);
        useLive.getState().reset(id);
      }
    }
    for (const id of wanted) {
      if (!this.streams.has(id)) {
        const s = new Stream(id);
        this.streams.set(id, s);
        s.start();
      }
    }
  }

  /** Tear down and reopen all (e.g. after token change or manual refresh). */
  restartAll(): void {
    const ids = [...this.streams.keys()];
    for (const [, s] of this.streams) s.stop();
    this.streams.clear();
    this.sync(ids);
  }

  stopAll(): void {
    for (const [, s] of this.streams) s.stop();
    this.streams.clear();
  }
}

export const streams = new StreamManager();

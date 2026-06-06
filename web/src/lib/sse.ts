// Real-time layer. One EventSource per active project streams task snapshots,
// runner updates, and runner logs into a zustand store the UI reads from.
//
// EventSource cannot set Authorization headers, so the access token is passed
// as the ?token= query param (the server's auth middleware accepts both).

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

// ─── Stream manager ──────────────────────────────────────────────

class Stream {
  private es: EventSource | null = null;
  private closed = false;
  private retry = 0;
  private timer: number | null = null;
  private projectId: string;

  constructor(projectId: string) {
    this.projectId = projectId;
  }

  start() {
    this.closed = false;
    this.open();
  }

  private url(): string {
    const token = useAuth.getState().token;
    const base = `/api/v1/tasks/${encodeURIComponent(this.projectId)}/stream`;
    return token ? `${base}?token=${encodeURIComponent(token)}` : base;
  }

  private open() {
    if (this.closed) return;
    const live = useLive.getState();
    const es = new EventSource(this.url());
    this.es = es;

    es.addEventListener("connected", () => {
      this.retry = 0;
      live.setProject(this.projectId, { connected: true, error: null });
    });

    es.addEventListener("tasks_snapshot", (ev) => {
      try {
        const d = JSON.parse((ev as MessageEvent).data) as SSETasksSnapshot;
        live.setProject(this.projectId, {
          tasks: d.tasks || [],
          stats: d.stats,
          cycles: d.cycles,
          connected: true,
          error: null,
        });
      } catch {
        /* ignore malformed frame */
      }
    });

    es.addEventListener("runners_update", (ev) => {
      try {
        const d = JSON.parse((ev as MessageEvent).data) as SSERunnersUpdate;
        useLive.getState().setRunners(d.runners || []);
      } catch {
        /* ignore */
      }
    });

    es.addEventListener("runner_log", (ev) => {
      try {
        const d = JSON.parse((ev as MessageEvent).data) as SSERunnerLog;
        if (d.lines?.length) {
          useLive
            .getState()
            .appendLogs(this.projectId, d.taskId, d.runnerId, d.lines);
        }
      } catch {
        /* ignore */
      }
    });

    es.onerror = () => {
      live.setProject(this.projectId, { connected: false });
      es.close();
      this.es = null;
      if (this.closed) return;
      // Exponential backoff capped at 15s.
      this.retry = Math.min(this.retry + 1, 5);
      const delay = Math.min(1000 * 2 ** this.retry, 15000);
      this.timer = window.setTimeout(() => this.open(), delay);
    };
  }

  stop() {
    this.closed = true;
    if (this.timer) window.clearTimeout(this.timer);
    this.es?.close();
    this.es = null;
  }
}

class StreamManager {
  private streams = new Map<string, Stream>();

  /** Open streams for exactly these projects; close any others. */
  sync(projectIds: string[]) {
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
  restartAll() {
    const ids = [...this.streams.keys()];
    for (const [, s] of this.streams) s.stop();
    this.streams.clear();
    this.sync(ids);
  }

  stopAll() {
    for (const [, s] of this.streams) s.stop();
    this.streams.clear();
  }
}

export const streams = new StreamManager();

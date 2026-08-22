/**
 * Runner-process helpers.
 *
 * A "process" in the UI is an executor instance reported by a runner —
 * one `OpencodeInstance` registry row per in-flight task process (any
 * executor: opencode, pi, script) plus ad-hoc control sessions. The
 * RunnerModal "Processes" tab and the RunnersLeaf drill-down both
 * render these rows; the pure sorting/merging logic lives here so it
 * can be unit-tested without React.
 */
import type { InstanceStatus, LogLine, OpencodeInstance } from "./types";

/** Map an instance status onto the wireframe dot classes. */
export function instanceDot(
  status: InstanceStatus,
): "on" | "busy" | "err" | "" {
  if (status === "busy") return "busy";
  if (status === "idle") return "on";
  if (status === "exited") return "err";
  return ""; // starting — grey
}

/** Compact uptime like "45s", "3m", "2h 14m", "1d 3h". */
export function formatUptime(startedAtMs: number, nowMs: number): string {
  if (!startedAtMs || startedAtMs > nowMs) return "";
  const s = Math.floor((nowMs - startedAtMs) / 1000);
  if (s < 60) return `${s}s`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ${m % 60}m`;
  const d = Math.floor(h / 24);
  return `${d}d ${h % 24}h`;
}

const STATUS_ORDER: Record<string, number> = {
  busy: 0,
  starting: 1,
  idle: 2,
  exited: 3,
};

/**
 * Sort processes for display: active work first (busy, starting),
 * then idle, exited last; newest-started first within a group.
 */
export function sortProcesses(
  instances: OpencodeInstance[],
): OpencodeInstance[] {
  return [...instances].sort((a, b) => {
    const sa = STATUS_ORDER[a.status] ?? 9;
    const sb = STATUS_ORDER[b.status] ?? 9;
    if (sa !== sb) return sa - sb;
    return (b.started_at ?? 0) - (a.started_at ?? 0);
  });
}

/** Group instances by runner_id, each group display-sorted. */
export function groupInstancesByRunner(
  instances: OpencodeInstance[],
): Record<string, OpencodeInstance[]> {
  const out: Record<string, OpencodeInstance[]> = {};
  for (const inst of instances) {
    (out[inst.runner_id] ??= []).push(inst);
  }
  for (const id of Object.keys(out)) out[id] = sortProcesses(out[id]);
  return out;
}

/**
 * Merge a historical REST log snapshot with live SSE tail lines.
 *
 * Both sources ultimately come from the same runner-side ingest, so
 * overlap is common right after mount (the REST snapshot contains
 * lines the live buffer also holds). Dedupe on timestamp+content —
 * cheap and collision-safe enough for display purposes.
 */
export function mergeTaskLogs(
  historical: LogLine[],
  live: LogLine[],
): LogLine[] {
  // Both inputs are individually oldest-to-newest, so this is a stable
  // two-pointer merge, NOT an append. Appending was correct only while
  // the REST window was the OLDEST n lines; it now returns the newest n
  // (the tail), so a live line older than that window would land after
  // the newest ones and render 301..500 followed by 1..200. Merging also
  // keeps those earlier lines instead of discarding them: the client
  // legitimately holds scrollback the server ring has already evicted.
  //
  // Ties keep the historical line first. The runner stamps every line in
  // one write batch with the same timestamp, so equal timestamps are the
  // common case and the merge must not reorder within them.
  const seen = new Set<string>();
  const merged: LogLine[] = [];

  const push = (l: LogLine) => {
    const key = `${l.timestamp}\u0000${l.content}`;
    if (seen.has(key)) return;
    seen.add(key);
    merged.push(l);
  };

  let h = 0;
  let v = 0;
  while (h < historical.length && v < live.length) {
    if (live[v].timestamp < historical[h].timestamp) push(live[v++]);
    else push(historical[h++]);
  }
  while (h < historical.length) push(historical[h++]);
  while (v < live.length) push(live[v++]);

  return merged;
}

// ─── detail-pane mode ────────────────────────────────────────────

/**
 * Which detail pane a selected process gets.
 *
 *   chat — the session transcript (role bubbles, tool calls, a composer
 *          that injects a prompt into the running agent)
 *   log  — the raw stdout stream from the task-log buffer
 *
 * Chat is the default wherever a session is addressable; raw log is the
 * fallback for executors that have no session (pi, script) and stays
 * reachable from chat by explicit toggle, because debugging genuinely
 * wants stdout sometimes.
 */
export type ProcessView = "chat" | "log";

/**
 * Whether a transcript is reachable for this process.
 *
 *   ready    — a session id is known; the transcript renders now
 *   starting — an OpenCode-family process whose session discovery is
 *              still in flight; the transcript pane says so and fills in
 *   none     — no session will ever exist (pi/script), or the process
 *              exited before one was discovered
 */
export type ChatCapability = "ready" | "starting" | "none";

export function chatCapability(inst: OpencodeInstance): ChatCapability {
  if ((inst.session_ids?.length ?? 0) > 0) return "ready";
  // An exited process will never report a session it never had.
  if (inst.status === "exited") return "none";
  const executor = (inst.executor || "").toLowerCase();
  if (executor === "opencode") return "starting";
  // Ad-hoc control sessions are OpenCode instances; the registry row
  // does not always carry an executor for them.
  if (executor === "" && inst.kind === "adhoc") return "starting";
  return "none";
}

/** Whether the raw stdout pane has a task-log stream to show. */
export function hasTaskLog(inst: OpencodeInstance): boolean {
  return inst.kind === "task" && !!inst.project_id && !!inst.task_id;
}

/**
 * Whether that log stream is still being APPENDED to.
 *
 * `hasTaskLog` is a shape test — an exited process keeps its buffer and
 * still answers true. Only this may drive a "live" affordance; the
 * pulsing dot over an exited process's frozen stdout is a lie about
 * what the user is watching.
 */
export function isLogStreaming(inst: OpencodeInstance): boolean {
  return hasTaskLog(inst) && inst.status !== "exited";
}

/** The pane shown when the user has not chosen one. */
export function defaultProcessView(inst: OpencodeInstance): ProcessView {
  return chatCapability(inst) === "none" ? "log" : "chat";
}

/**
 * Fold the user's per-process toggle onto the default.
 *
 * An explicit "log" always wins (the raw pane renders an explanatory
 * empty state even with nothing to stream, so it is never a dead end).
 * An explicit "chat" is ignored when no session is addressable, which
 * happens when a selection is carried onto a pi/script process.
 */
export function resolveProcessView(
  inst: OpencodeInstance,
  override?: ProcessView,
): ProcessView {
  if (override === "log") return "log";
  if (override === "chat") {
    return chatCapability(inst) === "none" ? defaultProcessView(inst) : "chat";
  }
  return defaultProcessView(inst);
}

/**
 * Which detail layout the full-page session view should render.
 *
 * SessionFull is addressed either by a live instance id (sidebar/mobile
 * rows) or a SessionRef (history entry points). Its detail area has
 * three shapes, and the choice is a pure function of two booleans so it
 * can be unit-tested without React:
 *
 *   instance  — an OpencodeInstance row is in hand; render the runner's
 *               ProcessChat/ProcessRawLog panes (toggle + transcript +
 *               steer composer), exactly like the Processes tab.
 *   history   — no live instance, but an effective (history) ref exists;
 *               render the read-only transcript. No composer — the
 *               process is gone.
 *   not-found — nothing addressable; show the "session not found" guard.
 *
 * An instance always wins: ProcessChat needs the full instance object,
 * and having one means the session is live/known.
 */
export type SessionFullDetailMode = "instance" | "history" | "not-found";

export function sessionFullDetailMode(input: {
  hasInstance: boolean;
  hasEffectiveRef: boolean;
}): SessionFullDetailMode {
  if (input.hasInstance) return "instance";
  if (input.hasEffectiveRef) return "history";
  return "not-found";
}

/** Log-level CSS class from a log line (mirrors CardLogs heuristics). */
export function logLevelClass(line: LogLine): "err" | "wrn" | "ok" | "" {
  const lvl = (line.level || "").toUpperCase();
  if (lvl === "ERROR" || lvl === "FATAL") return "err";
  if (lvl === "WARN" || lvl === "WARNING") return "wrn";
  const upper = line.content.toUpperCase();
  if (upper.includes("ERROR") || upper.includes("FATAL")) return "err";
  if (upper.includes("WARN")) return "wrn";
  if (upper.includes("SUCCESS") || upper.includes(" OK ")) return "ok";
  return "";
}

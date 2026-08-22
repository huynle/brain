/**
 * Runner-process helper tests (pure logic, node --test, no DOM).
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  chatCapability,
  defaultProcessView,
  formatUptime,
  groupInstancesByRunner,
  hasTaskLog,
  instanceDot,
  isLogStreaming,
  logLevelClass,
  mergeTaskLogs,
  resolveProcessView,
  sessionFullDetailMode,
  sortProcesses,
} from "./processes";
import type { LogLine, OpencodeInstance } from "./types";

function inst(over: Partial<OpencodeInstance>): OpencodeInstance {
  return {
    instance_id: "inst_x",
    runner_id: "runner-1",
    kind: "task",
    status: "busy",
    ...over,
  } as OpencodeInstance;
}

test("instanceDot maps statuses to wireframe dot classes", () => {
  assert.equal(instanceDot("busy"), "busy");
  assert.equal(instanceDot("idle"), "on");
  assert.equal(instanceDot("exited"), "err");
  assert.equal(instanceDot("starting"), "");
});

test("formatUptime renders compact durations", () => {
  const now = 1_000_000_000;
  assert.equal(formatUptime(now - 45_000, now), "45s");
  assert.equal(formatUptime(now - 3 * 60_000, now), "3m");
  assert.equal(formatUptime(now - (2 * 3600 + 14 * 60) * 1000, now), "2h 14m");
  assert.equal(formatUptime(now - 27 * 3600_000, now), "1d 3h");
  assert.equal(formatUptime(0, now), "");
  assert.equal(formatUptime(now + 5000, now), "");
});

test("sortProcesses puts active work first, newest first within group", () => {
  const rows = sortProcesses([
    inst({ instance_id: "a", status: "exited", started_at: 50 }),
    inst({ instance_id: "b", status: "idle", started_at: 40 }),
    inst({ instance_id: "c", status: "busy", started_at: 10 }),
    inst({ instance_id: "d", status: "busy", started_at: 30 }),
    inst({ instance_id: "e", status: "starting", started_at: 99 }),
  ]);
  assert.deepEqual(
    rows.map((r) => r.instance_id),
    ["d", "c", "e", "b", "a"],
  );
});

test("groupInstancesByRunner buckets and sorts per runner", () => {
  const grouped = groupInstancesByRunner([
    inst({ instance_id: "a", runner_id: "r1", status: "idle" }),
    inst({ instance_id: "b", runner_id: "r2", status: "busy" }),
    inst({ instance_id: "c", runner_id: "r1", status: "busy" }),
  ]);
  assert.deepEqual(Object.keys(grouped).sort(), ["r1", "r2"]);
  assert.deepEqual(
    grouped["r1"].map((r) => r.instance_id),
    ["c", "a"],
  );
  assert.deepEqual(
    grouped["r2"].map((r) => r.instance_id),
    ["b"],
  );
});

test("mergeTaskLogs dedupes overlap between snapshot and live tail", () => {
  const l = (timestamp: string, content: string): LogLine => ({
    timestamp,
    level: "INFO",
    content,
  });
  const historical = [l("t1", "one"), l("t2", "two")];
  const live = [l("t2", "two"), l("t3", "three"), l("t3", "three")];
  const merged = mergeTaskLogs(historical, live);
  assert.deepEqual(
    merged.map((m) => m.content),
    ["one", "two", "three"],
  );
});

test("logLevelClass prefers explicit level, falls back to content", () => {
  const l = (level: string, content: string): LogLine => ({
    timestamp: "t",
    level,
    content,
  });
  assert.equal(logLevelClass(l("ERROR", "x")), "err");
  assert.equal(logLevelClass(l("warning", "x")), "wrn");
  assert.equal(logLevelClass(l("", "fatal ERROR: boom")), "err");
  assert.equal(logLevelClass(l("INFO", "all good")), "");
});

// ─── detail-pane mode ────────────────────────────────────────────

test("chatCapability: a known session id is ready, whatever the executor", () => {
  assert.equal(
    chatCapability(inst({ executor: "opencode", session_ids: ["s1"] })),
    "ready",
  );
  // Even an exited process keeps a readable transcript.
  assert.equal(
    chatCapability(inst({ status: "exited", session_ids: ["s1"] })),
    "ready",
  );
});

test("chatCapability: opencode without a session yet is starting", () => {
  assert.equal(chatCapability(inst({ executor: "opencode" })), "starting");
  assert.equal(chatCapability(inst({ executor: "OpenCode" })), "starting");
  assert.equal(
    chatCapability(inst({ executor: "opencode", session_ids: [] })),
    "starting",
  );
  // Ad-hoc control sessions are opencode even when the row omits it.
  assert.equal(
    chatCapability(inst({ kind: "adhoc", executor: "" })),
    "starting",
  );
});

test("chatCapability: sessionless executors and dead processes have none", () => {
  assert.equal(chatCapability(inst({ executor: "pi" })), "none");
  assert.equal(chatCapability(inst({ executor: "script" })), "none");
  assert.equal(chatCapability(inst({ executor: "" })), "none");
  assert.equal(
    chatCapability(inst({ executor: "opencode", status: "exited" })),
    "none",
  );
});

test("hasTaskLog: needs a task-kind row with both ids", () => {
  assert.equal(hasTaskLog(inst({ project_id: "p", task_id: "t" })), true);
  assert.equal(hasTaskLog(inst({ project_id: "p" })), false);
  assert.equal(hasTaskLog(inst({ task_id: "t" })), false);
  assert.equal(
    hasTaskLog(inst({ kind: "adhoc", project_id: "p", task_id: "t" })),
    false,
  );
});

/*
 * The pulsing "live" dot was gated on hasTaskLog — a SHAPE test that an
 * exited process still passes, so frozen stdout advertised itself as
 * live. Liveness is a separate question and gets its own predicate.
 */
test("isLogStreaming: a log buffer that is still being appended to", () => {
  const running = { project_id: "p", task_id: "t" };
  assert.equal(isLogStreaming(inst({ ...running, status: "busy" })), true);
  assert.equal(isLogStreaming(inst({ ...running, status: "idle" })), true);
  assert.equal(isLogStreaming(inst({ ...running, status: "starting" })), true);
  assert.equal(isLogStreaming(inst({ ...running, status: "exited" })), false);
  // Still false wherever there was no stream to begin with.
  assert.equal(isLogStreaming(inst({ status: "busy" })), false);
  assert.equal(
    isLogStreaming(inst({ ...running, kind: "adhoc", status: "busy" })),
    false,
  );
});

test("defaultProcessView: chat wherever a session is addressable", () => {
  assert.equal(defaultProcessView(inst({ session_ids: ["s1"] })), "chat");
  assert.equal(defaultProcessView(inst({ executor: "opencode" })), "chat");
  assert.equal(defaultProcessView(inst({ executor: "pi" })), "log");
  assert.equal(
    defaultProcessView(inst({ executor: "opencode", status: "exited" })),
    "log",
  );
});

test("resolveProcessView: an explicit raw-log choice always wins", () => {
  const chatty = inst({ session_ids: ["s1"] });
  assert.equal(resolveProcessView(chatty, "log"), "log");
  assert.equal(resolveProcessView(inst({ executor: "pi" }), "log"), "log");
});

test("resolveProcessView: an explicit chat choice needs a session", () => {
  assert.equal(
    resolveProcessView(inst({ session_ids: ["s1"] }), "chat"),
    "chat",
  );
  assert.equal(
    resolveProcessView(inst({ executor: "opencode" }), "chat"),
    "chat",
  );
  // Carrying a chat preference onto a pi process must not blank the pane.
  assert.equal(resolveProcessView(inst({ executor: "pi" }), "chat"), "log");
});

test("resolveProcessView: no override falls back to the default", () => {
  assert.equal(resolveProcessView(inst({ session_ids: ["s1"] })), "chat");
  assert.equal(resolveProcessView(inst({ executor: "pi" }), undefined), "log");
});

// The REST window is the NEWEST n lines (server-side tail). A live line
// older than that window must not be appended after the newest ones — that
// rendered "301..500, 1..200" on screen. Regression test for the ordering
// contract between logbuffer.Tail and this merge.
test("mergeTaskLogs: keeps chronological order when live predates the tail", () => {
  const line = (n: number): LogLine => ({
    timestamp: new Date(Date.UTC(2026, 0, 1, 0, 0, n)).toISOString(),
    level: "info",
    content: `line ${n}`,
  });
  // Server retained only the newest 3; the client saw all 6 live.
  const historical = [line(4), line(5), line(6)];
  const live = [line(1), line(2), line(3), line(4), line(5), line(6)];

  const merged = mergeTaskLogs(historical, live);

  assert.deepEqual(
    merged.map((l) => l.content),
    ["line 1", "line 2", "line 3", "line 4", "line 5", "line 6"],
    "merge must be chronological and must not duplicate the overlap",
  );
});

test("mergeTaskLogs: equal timestamps keep historical first and do not reorder", () => {
  const at = "2026-01-01T00:00:00.000Z";
  const mk = (content: string): LogLine => ({
    timestamp: at,
    level: "info",
    content,
  });
  const merged = mergeTaskLogs([mk("h1"), mk("h2")], [mk("h1"), mk("v1")]);
  assert.deepEqual(
    merged.map((l) => l.content),
    ["h1", "h2", "v1"],
  );
});

// ─── SessionFull detail-mode decision ────────────────────────────

test("sessionFullDetailMode: an instance in hand renders the runner panes", () => {
  assert.equal(
    sessionFullDetailMode({ hasInstance: true, hasEffectiveRef: true }),
    "instance",
  );
  // An instance always wins even if the effective ref were somehow absent.
  assert.equal(
    sessionFullDetailMode({ hasInstance: true, hasEffectiveRef: false }),
    "instance",
  );
});

test("sessionFullDetailMode: no instance but a ref is history-only (read-only)", () => {
  assert.equal(
    sessionFullDetailMode({ hasInstance: false, hasEffectiveRef: true }),
    "history",
  );
});

test("sessionFullDetailMode: nothing addressable is not-found", () => {
  assert.equal(
    sessionFullDetailMode({ hasInstance: false, hasEffectiveRef: false }),
    "not-found",
  );
});

/**
 * Runner-process helper tests (pure logic, node --test, no DOM).
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  formatUptime,
  groupInstancesByRunner,
  instanceDot,
  logLevelClass,
  mergeTaskLogs,
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

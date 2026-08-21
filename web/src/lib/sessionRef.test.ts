import test from "node:test";
import assert from "node:assert/strict";
import {
  findTaskInstance,
  historySessionRefs,
  instanceSessionRef,
  instanceTranscriptRef,
  liveSessionRef,
  resolveSessionRef,
} from "./sessionRef";
import type { OpencodeInstance } from "./types";

function inst(over: Partial<OpencodeInstance>): OpencodeInstance {
  return {
    instance_id: "i1",
    runner_id: "r1",
    kind: "task",
    status: "busy",
    ...over,
  } as OpencodeInstance;
}

const TASK = { id: "t1", projectId: "p1" };

test("findTaskInstance: matches task-kind, same task id, not exited", () => {
  const match = inst({ task_id: "t1", project_id: "p1" });
  assert.equal(findTaskInstance(TASK, [match]), match);
  assert.equal(
    findTaskInstance(TASK, [inst({ task_id: "t1", kind: "adhoc" })]),
    undefined,
  );
  assert.equal(
    findTaskInstance(TASK, [inst({ task_id: "t1", status: "exited" })]),
    undefined,
  );
  assert.equal(findTaskInstance(TASK, [inst({ task_id: "t2" })]), undefined);
});

test("findTaskInstance: project match only enforced when both sides carry it", () => {
  const noProject = inst({ task_id: "t1" });
  assert.equal(findTaskInstance(TASK, [noProject]), noProject);
  assert.equal(
    findTaskInstance(TASK, [inst({ task_id: "t1", project_id: "other" })]),
    undefined,
  );
});

test("liveSessionRef: last discovered session wins; absent early in life", () => {
  const ref = liveSessionRef(TASK, [
    inst({ task_id: "t1", session_ids: ["s_old", "s_new"] }),
  ]);
  assert.deepEqual(ref, {
    mode: "live",
    runner_id: "r1",
    instance_id: "i1",
    session_id: "s_new",
  });

  const starting = liveSessionRef(TASK, [inst({ task_id: "t1" })]);
  assert.equal(starting?.mode, "live");
  assert.equal(starting?.session_id, undefined);
});

test("historySessionRefs: newest first, unaddressable entries dropped", () => {
  const refs = historySessionRefs({
    ...TASK,
    sessions: {
      s_a: { timestamp: "2026-08-01T00:00:00Z", runner_id: "r1", workdir: "/w1" },
      s_b: { timestamp: "2026-08-10T00:00:00Z", runner_id: "r2", workdir: "/w2" },
      s_c: { timestamp: "2026-08-05T00:00:00Z" }, // no runner_id — dropped
    },
  });
  assert.deepEqual(
    refs.map((r) => (r.mode === "history" ? r.session_id : "?")),
    ["s_b", "s_a"],
  );
  assert.equal(refs[0].runner_id, "r2");
  assert.equal(refs[0].mode === "history" && refs[0].workdir, "/w2");
  assert.equal(refs[0].mode === "history" && refs[0].task_id, "t1");
});

test("resolveSessionRef: live beats history; history beats nothing", () => {
  const sessions = {
    s_a: { timestamp: "2026-08-01T00:00:00Z", runner_id: "r1" },
  };
  const live = resolveSessionRef({ ...TASK, sessions }, [
    inst({ task_id: "t1", session_ids: ["s_live"] }),
  ]);
  assert.equal(live?.mode, "live");

  const history = resolveSessionRef({ ...TASK, sessions }, []);
  assert.equal(history?.mode, "history");
  assert.equal(history?.mode === "history" && history.session_id, "s_a");

  assert.equal(resolveSessionRef(TASK, []), undefined);
});

// ─── instance-addressed refs ─────────────────────────────────────

test("instanceSessionRef: builds the one canonical live ref shape", () => {
  assert.deepEqual(
    instanceSessionRef(inst({ session_ids: ["s_old", "s_new"] })),
    { mode: "live", runner_id: "r1", instance_id: "i1", session_id: "s_new" },
  );
  assert.deepEqual(instanceSessionRef(inst({})), {
    mode: "live",
    runner_id: "r1",
    instance_id: "i1",
    session_id: undefined,
  });
});

/*
 * SessionFull and SessionLeaf used to build live refs inline, with their
 * own precedence rule: a session the caller was already addressing wins
 * over the instance's newest. That rule now lives here as the optional
 * pin, so the "single place" claim in the docstring is true.
 */
test("instanceSessionRef: a pinned session wins over the newest discovered", () => {
  const row = inst({ session_ids: ["s_old", "s_new"] });
  assert.equal(instanceSessionRef(row, "s_old").session_id, "s_old");
  // A pin the instance has not reported is still honoured — the caller
  // is addressing a real session, the registry row is just behind.
  assert.equal(instanceSessionRef(row, "s_other").session_id, "s_other");
  // No pin, or an empty one, falls back to last-discovered.
  assert.equal(instanceSessionRef(row).session_id, "s_new");
  assert.equal(instanceSessionRef(row, undefined).session_id, "s_new");
  assert.equal(instanceSessionRef(row, "").session_id, "s_new");
  // Pinning does not invent a session on a row that has none.
  assert.equal(instanceSessionRef(inst({}), undefined).session_id, undefined);
});

test("instanceSessionRef: liveSessionRef delegates to it (one rule, one copy)", () => {
  const row = inst({ task_id: "t1", project_id: "p1", session_ids: ["s1"] });
  assert.deepEqual(liveSessionRef(TASK, [row]), instanceSessionRef(row));
});

test("instanceTranscriptRef: live while the instance is up", () => {
  const ref = instanceTranscriptRef(inst({ session_ids: ["s1"] }));
  assert.equal(ref?.mode, "live");
  assert.equal(ref?.session_id, "s1");
  // Starting: addressable, session not discovered yet.
  const starting = instanceTranscriptRef(inst({ status: "starting" }));
  assert.equal(starting?.mode, "live");
  assert.equal(starting?.session_id, undefined);
});

test("instanceTranscriptRef: an exited instance degrades to history", () => {
  const ref = instanceTranscriptRef(
    inst({
      status: "exited",
      session_ids: ["s1", "s2"],
      task_id: "t1",
      project_id: "p1",
      workdir: "/w",
    }),
  );
  assert.equal(ref?.mode, "history");
  assert.equal(ref?.session_id, "s2");
  assert.equal(ref?.mode === "history" && ref.task_id, "t1");
  assert.equal(ref?.mode === "history" && ref.project_id, "p1");
  assert.equal(ref?.mode === "history" && ref.workdir, "/w");
});

test("instanceTranscriptRef: exited with no session is unaddressable", () => {
  assert.equal(instanceTranscriptRef(inst({ status: "exited" })), undefined);
  assert.equal(
    instanceTranscriptRef(inst({ status: "exited", session_ids: [] })),
    undefined,
  );
});

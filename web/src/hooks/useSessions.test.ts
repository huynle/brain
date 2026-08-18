import test from "node:test";
import assert from "node:assert/strict";
import { isLiveTaskSession } from "./useSessions";
import type { OpencodeInstance } from "../lib/types";

function inst(over: Partial<OpencodeInstance>): OpencodeInstance {
  return {
    instance_id: "i1",
    runner_id: "r1",
    kind: "task",
    status: "busy",
    ...over,
  } as OpencodeInstance;
}

test("task instances are live only while starting or busy", () => {
  assert.equal(isLiveTaskSession(inst({ status: "starting" })), true);
  assert.equal(isLiveTaskSession(inst({ status: "busy" })), true);
  assert.equal(isLiveTaskSession(inst({ status: "idle" })), false);
  assert.equal(isLiveTaskSession(inst({ status: "exited" })), false);
});

test("adhoc instances (continuations) stay listed while idle", () => {
  assert.equal(isLiveTaskSession(inst({ kind: "adhoc", status: "idle" })), true);
  assert.equal(isLiveTaskSession(inst({ kind: "adhoc", status: "busy" })), true);
  assert.equal(isLiveTaskSession(inst({ kind: "adhoc", status: "starting" })), true);
  assert.equal(isLiveTaskSession(inst({ kind: "adhoc", status: "exited" })), false);
});

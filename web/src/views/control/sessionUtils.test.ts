import { strict as assert } from "node:assert";
import { test } from "node:test";
import type { OcSession, OpencodeInstance } from "../../lib/types";
import { latestInstanceSessionId, sortSessionsByExecutedTime } from "./sessionUtils";

function session(id: string, created: number, updated?: number): OcSession {
  return { id, time: { created, updated } };
}

test("sortSessionsByExecutedTime orders newest updated or created first", () => {
  const sorted = sortSessionsByExecutedTime([
    session("older", 10, 20),
    session("new-created", 30),
    session("new-updated", 5, 40),
  ]);

  assert.deepEqual(sorted.map((s) => s.id), ["new-updated", "new-created", "older"]);
});

test("latestInstanceSessionId prefers newest session details then instance fallback", () => {
  const instance = {
    instance_id: "i1",
    runner_id: "r1",
    kind: "task",
    status: "busy",
    session_ids: ["fallback"],
  } satisfies OpencodeInstance;

  assert.equal(latestInstanceSessionId(instance, [session("latest", 100), session("old", 1)]), "latest");
  assert.equal(latestInstanceSessionId(instance, []), "fallback");
});

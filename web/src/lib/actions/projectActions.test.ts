/**
 * Tests for lib/actions/projectActions — small on purpose, but the card
 * header is now a registry surface, so pin the verb set, the routing, and
 * the one disable rule.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import { buildProjectActions, type ProjectActionContext } from "./projectActions";
import { isEnabled } from "./types";

function recorder() {
  const calls: string[] = [];
  const ctx: ProjectActionContext = {
    runProject: async (pid) => void calls.push(`run:${pid}`),
    openTaskList: (pid) => void calls.push(`focus:${pid}`),
    hideProject: (pid) => void calls.push(`hide:${pid}`),
  };
  return { calls, ctx };
}

test("every project verb is present", () => {
  const { ctx } = recorder();
  const ids = buildProjectActions("shop", ctx).map((a) => a.id);
  assert.deepEqual(ids, ["run", "focus-tasks", "hide"]);
});

test("verbs route to their effects with the project id", async () => {
  const { calls, ctx } = recorder();
  for (const a of buildProjectActions("shop", ctx)) await a.run();
  assert.deepEqual(calls, ["run:shop", "focus:shop", "hide:shop"]);
});

test("run is disabled with a reason for an empty project", () => {
  const { ctx } = recorder();
  const run = buildProjectActions("shop", ctx, { taskCount: 0 })[0];
  assert.equal(isEnabled(run), false);
  assert.match(run.disabledReason ?? "", /no tasks/i);
});

test("run stays enabled when the task count is unknown", () => {
  // The caller may not have a snapshot yet; absence of data must not
  // disable the verb.
  const { ctx } = recorder();
  const run = buildProjectActions("shop", ctx)[0];
  assert.equal(isEnabled(run), true);
});

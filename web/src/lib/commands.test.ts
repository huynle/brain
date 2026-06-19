import { strict as assert } from "node:assert";
import { test } from "node:test";
import { resolveCommand } from "./commands";

test("resolveCommand maps k9s-style resource commands to Brain views", () => {
  assert.deepEqual(resolveCommand(":tasks"), { type: "view", view: "tasks" });
  assert.deepEqual(resolveCommand("task"), { type: "view", view: "tasks" });
  assert.deepEqual(resolveCommand("ta"), { type: "view", view: "tasks" });
  assert.deepEqual(resolveCommand("entries"), { type: "view", view: "brain" });
  assert.deepEqual(resolveCommand("auto"), { type: "view", view: "automations" });
  assert.deepEqual(resolveCommand("po"), { type: "projectPicker" });
});

test("resolveCommand returns suggestions for partial or unknown commands", () => {
  assert.deepEqual(resolveCommand("ru"), { type: "view", view: "runners" });
  assert.deepEqual(resolveCommand("run"), { type: "suggest", suggestions: ["runners", "runner", "instances"] });
  assert.deepEqual(resolveCommand("nope"), { type: "suggest", suggestions: [] });
});

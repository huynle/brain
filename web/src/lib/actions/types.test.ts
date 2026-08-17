/**
 * Tests for lib/actions/types — the shared descriptor helpers every
 * surface relies on for ordering, grouping, and key dispatch.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  ACTION_GROUP_ORDER,
  findByKey,
  groupActions,
  isEnabled,
  sortActions,
  type ActionDescriptor,
  type ActionGroup,
} from "./types";

function mk(
  id: string,
  group: ActionGroup,
  over: Partial<ActionDescriptor> = {},
): ActionDescriptor {
  return {
    id,
    label: id,
    group,
    run: async () => {},
    ...over,
  };
}

test("isEnabled is false exactly when a disabledReason is present", () => {
  assert.equal(isEnabled(mk("a", "run")), true);
  assert.equal(isEnabled(mk("a", "run", { disabledReason: "nope" })), false);
  // Empty string is the "no reason" sentinel builders emit.
  assert.equal(isEnabled(mk("a", "run", { disabledReason: "" })), true);
});

test("sortActions puts groups in canonical order", () => {
  const sorted = sortActions([
    mk("del", "danger"),
    mk("go", "navigate"),
    mk("run", "run"),
    mk("sel", "select"),
    mk("edit", "edit"),
    mk("state", "state"),
  ]);
  assert.deepEqual(
    sorted.map((a) => a.group),
    ACTION_GROUP_ORDER,
  );
});

test("sortActions is stable within a group", () => {
  // Builders order deliberately inside a group (Run before Resume); the
  // sort must not scramble that.
  const sorted = sortActions([
    mk("first", "run"),
    mk("second", "run"),
    mk("third", "run"),
  ]);
  assert.deepEqual(
    sorted.map((a) => a.id),
    ["first", "second", "third"],
  );
});

test("sortActions does not mutate its input", () => {
  const input = [mk("del", "danger"), mk("run", "run")];
  const before = input.map((a) => a.id);
  sortActions(input);
  assert.deepEqual(
    input.map((a) => a.id),
    before,
  );
});

test("groupActions splits into contiguous same-group runs", () => {
  const groups = groupActions([
    mk("del", "danger"),
    mk("run1", "run"),
    mk("run2", "run"),
    mk("state1", "state"),
  ]);
  assert.deepEqual(
    groups.map((g) => g.map((a) => a.id)),
    [["run1", "run2"], ["state1"], ["del"]],
  );
});

test("groupActions on an empty list yields no groups", () => {
  assert.deepEqual(groupActions([]), []);
});

test("findByKey returns the matching enabled action", () => {
  const actions = [mk("run", "run", { key: "x" }), mk("del", "danger", { key: "d" })];
  assert.equal(findByKey(actions, "x")?.id, "run");
  assert.equal(findByKey(actions, "d")?.id, "del");
});

test("findByKey skips a disabled action rather than firing it", () => {
  // A key press must never invoke something the menu shows as unavailable.
  const actions = [mk("run", "run", { key: "x", disabledReason: "running" })];
  assert.equal(findByKey(actions, "x"), undefined);
});

test("findByKey falls through a disabled action to an enabled one", () => {
  const actions = [
    mk("a", "run", { key: "x", disabledReason: "no" }),
    mk("b", "state", { key: "x" }),
  ];
  assert.equal(findByKey(actions, "x")?.id, "b");
});

test("findByKey returns undefined for an unbound key", () => {
  assert.equal(findByKey([mk("run", "run", { key: "x" })], "q"), undefined);
});

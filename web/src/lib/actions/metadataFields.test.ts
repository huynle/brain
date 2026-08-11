/**
 * Tests for lib/actions/metadataFields.
 *
 * The risky part of a metadata editor is not rendering it — it is the
 * diff. Sending unchanged fields rewrites values a runner may have just
 * updated, and in feature mode a blind save can flatten deliberate
 * per-task differences across every task at once. Those two hazards are
 * what these tests pin.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  allFields,
  buildPatch,
  fieldsForTab,
  initialFeatureValues,
  initialTaskValues,
  splitList,
  tabsForMode,
  type FormValues,
} from "./metadataFields";
import type { DerivedFeature } from "../features";
import type { Task } from "../types";

function mkTask(over: Partial<Task> = {}): Task {
  return {
    id: "t1",
    path: "projects/p/task/t1.md",
    title: "A task",
    priority: "medium",
    status: "pending",
    ...over,
  };
}

function mkFeature(over: Partial<DerivedFeature> = {}): DerivedFeature {
  return {
    id: "feat",
    projectId: "p",
    name: "feat",
    progress: 0,
    lifecycle: "in-progress",
    taskCount: { total: 2, completed: 0, blocked: 0, active: 2 },
    ownerTaskIds: [],
    resumableCount: 0,
    dependsOn: [],
    ...over,
  };
}

// ─── schema ────────────────────────────────────────────────────────

test("task mode exposes the fields the TUI exposes", () => {
  const keys = allFields("task").map((f) => f.key);
  for (const expected of [
    "status",
    "priority",
    "feature_id",
    "agent",
    "model",
    "executor",
    "execution_mode",
    "merge_policy",
    "merge_strategy",
    "merge_target_branch",
  ]) {
    assert.ok(keys.includes(expected), `missing field: ${expected}`);
  }
});

test("feature mode leads with the Feature tab", () => {
  assert.equal(tabsForMode("feature")[0], "feature");
});

test("feature mode excludes per-task identity fields from the Task tab", () => {
  // Applying feature_id or depends_on in bulk would move the whole
  // feature elsewhere, or collapse every task onto one dependency.
  const keys = fieldsForTab("task", "feature").map((f) => f.key);
  assert.ok(!keys.includes("feature_id"), "feature_id is bulk-editable");
  assert.ok(!keys.includes("depends_on"), "depends_on is bulk-editable");
  assert.ok(keys.includes("status"));
  assert.ok(keys.includes("priority"));
});

test("task mode keeps feature_id and depends_on editable", () => {
  const keys = fieldsForTab("task", "task").map((f) => f.key);
  assert.ok(keys.includes("feature_id"));
  assert.ok(keys.includes("depends_on"));
});

test("every select field offers options", () => {
  for (const f of allFields("task")) {
    if (f.kind === "select") {
      assert.ok((f.options?.length ?? 0) > 0, `${f.key} has no options`);
    }
  }
});

// ─── initial values ────────────────────────────────────────────────

test("initialTaskValues reads through from the task", () => {
  const v = initialTaskValues(
    mkTask({ status: "blocked", priority: "high", agent: "tdd-dev" }),
  );
  assert.equal(v.status, "blocked");
  assert.equal(v.priority, "high");
  assert.equal(v.agent, "tdd-dev");
});

test("initialTaskValues renders an unset field as empty, not 'undefined'", () => {
  const v = initialTaskValues(mkTask());
  assert.equal(v.model, "");
});

test("initialTaskValues joins list fields for editing", () => {
  const v = initialTaskValues(mkTask({ depends_on: ["a", "b"] }));
  assert.equal(v.depends_on, "a, b");
});

test("initialTaskValues coerces booleans", () => {
  assert.equal(initialTaskValues(mkTask({ complete_on_idle: true })).complete_on_idle, true);
  assert.equal(initialTaskValues(mkTask()).complete_on_idle, false);
});

// ─── feature mode: mixed values ────────────────────────────────────

test("a field all tasks agree on gets that value", () => {
  const tasks = [
    mkTask({ id: "a", feature_id: "feat", priority: "high" }),
    mkTask({ id: "b", feature_id: "feat", priority: "high" }),
  ];
  const { values, mixed } = initialFeatureValues(mkFeature(), tasks);
  assert.equal(values.priority, "high");
  assert.equal(mixed.has("priority"), false);
});

test("a field tasks disagree on starts blank and is flagged mixed", () => {
  // Crucial: a blind save must not flatten a deliberate difference.
  const tasks = [
    mkTask({ id: "a", feature_id: "feat", priority: "high" }),
    mkTask({ id: "b", feature_id: "feat", priority: "low" }),
  ];
  const { values, mixed } = initialFeatureValues(mkFeature(), tasks);
  assert.equal(values.priority, "");
  assert.ok(mixed.has("priority"), "mixed field not flagged");
});

test("tasks outside the feature are ignored when computing agreement", () => {
  const tasks = [
    mkTask({ id: "a", feature_id: "feat", priority: "high" }),
    mkTask({ id: "b", feature_id: "other", priority: "low" }),
  ];
  const { values, mixed } = initialFeatureValues(mkFeature(), tasks);
  assert.equal(values.priority, "high");
  assert.equal(mixed.has("priority"), false);
});

// ─── diffing ───────────────────────────────────────────────────────

test("an untouched form produces an empty patch", () => {
  // Sending unchanged fields would rewrite values a runner may have just
  // updated, and generate pointless entry.updated churn.
  const initial = initialTaskValues(mkTask({ status: "pending" }));
  assert.deepEqual(buildPatch(initial, { ...initial }, "task"), {});
});

test("only changed keys appear in the patch", () => {
  const initial = initialTaskValues(mkTask({ status: "pending", agent: "a" }));
  const current: FormValues = { ...initial, status: "blocked" };
  const patch = buildPatch(initial, current, "task");
  assert.deepEqual(patch, { status: "blocked" });
});

test("list fields are split back into arrays", () => {
  const initial = initialTaskValues(mkTask({ depends_on: ["a"] }));
  const patch = buildPatch(initial, { ...initial, depends_on: "a, b, c" }, "task");
  assert.deepEqual(patch.depends_on, ["a", "b", "c"]);
});

test("clearing a list field sends an empty array, not an empty string", () => {
  const initial = initialTaskValues(mkTask({ depends_on: ["a"] }));
  const patch = buildPatch(initial, { ...initial, depends_on: "" }, "task");
  assert.deepEqual(patch.depends_on, []);
});

test("booleans are sent as booleans", () => {
  const initial = initialTaskValues(mkTask({ complete_on_idle: false }));
  const patch = buildPatch(initial, { ...initial, complete_on_idle: true }, "task");
  assert.equal(patch.complete_on_idle, true);
});

test("clearing a text field sends an empty string, so it can be unset", () => {
  const initial = initialTaskValues(mkTask({ agent: "tdd-dev" }));
  const patch = buildPatch(initial, { ...initial, agent: "" }, "task");
  assert.equal(patch.agent, "");
});

test("skipped fields never enter the patch even if they differ", () => {
  // Feature mode passes the mixed set here: a field that started blank
  // because tasks disagreed must not be written unless touched.
  const initial: FormValues = { priority: "", status: "pending" };
  const current: FormValues = { priority: "", status: "blocked" };
  const patch = buildPatch(initial, current, "feature", new Set(["priority"]));
  assert.equal("priority" in patch, false);
  assert.equal(patch.status, "blocked");
});

test("a mixed field that WAS touched does enter the patch", () => {
  const initial: FormValues = { priority: "", status: "pending" };
  const current: FormValues = { priority: "high", status: "pending" };
  // The modal drops a field from `skip` once the user edits it.
  const patch = buildPatch(initial, current, "feature", new Set());
  assert.equal(patch.priority, "high");
});

// ─── list parsing ──────────────────────────────────────────────────

test("splitList handles commas, newlines, and stray whitespace", () => {
  assert.deepEqual(splitList("a, b\nc ,  d"), ["a", "b", "c", "d"]);
});

test("splitList drops empties rather than emitting blank entries", () => {
  assert.deepEqual(splitList("a,,  ,b"), ["a", "b"]);
});

test("splitList on an empty string yields an empty array", () => {
  assert.deepEqual(splitList(""), []);
});

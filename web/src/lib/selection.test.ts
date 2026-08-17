/**
 * Tests for lib/selection — the multi-select scope and delete planning.
 *
 * The invariants worth pinning: one-project-at-a-time scoping, the
 * feature/task dedupe in the delete plan (a task under a selected
 * feature must not also be deleted by path, or previews double-count),
 * stale-selection handling, and the 100-entry chunking the server's
 * silent paths clamp makes mandatory.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  buildDeletePlan,
  chunkPaths,
  describeSelection,
  EMPTY_SELECTION,
  isEmptySelection,
  selectionCount,
  toggleFeature,
  toggleTask,
} from "./selection";
import type { Task } from "./types";

function mkTask(id: string, featureId?: string): Task {
  return {
    id,
    path: `projects/p/task/${id}.md`,
    title: `Task ${id}`,
    status: "pending",
    feature_id: featureId,
  } as Task;
}

// ─── toggling + scope ──────────────────────────────────────────────

test("toggle adds then removes the same task", () => {
  let s = toggleTask(EMPTY_SELECTION, "p1", "t1");
  assert.equal(s.taskIds.has("t1"), true);
  assert.equal(s.projectId, "p1");
  s = toggleTask(s, "p1", "t1");
  assert.equal(s.taskIds.has("t1"), false);
});

test("marking in a different project restarts the scope", () => {
  let s = toggleTask(EMPTY_SELECTION, "p1", "t1");
  s = toggleFeature(s, "p1", "f1");
  s = toggleTask(s, "p2", "t9");
  assert.equal(s.projectId, "p2");
  assert.deepEqual([...s.taskIds], ["t9"]);
  assert.equal(s.featureIds.size, 0);
});

test("feature toggle in a new project also restarts", () => {
  let s = toggleTask(EMPTY_SELECTION, "p1", "t1");
  s = toggleFeature(s, "p2", "f2");
  assert.equal(s.projectId, "p2");
  assert.equal(s.taskIds.size, 0);
  assert.deepEqual([...s.featureIds], ["f2"]);
});

test("counts and emptiness", () => {
  assert.equal(isEmptySelection(EMPTY_SELECTION), true);
  let s = toggleTask(EMPTY_SELECTION, "p", "a");
  s = toggleFeature(s, "p", "f");
  assert.equal(selectionCount(s), 2);
  assert.equal(isEmptySelection(s), false);
});

// ─── delete planning ───────────────────────────────────────────────

test("plan excludes tasks covered by a selected feature", () => {
  let s = toggleTask(EMPTY_SELECTION, "p", "t1");
  s = toggleTask(s, "p", "t2");
  s = toggleFeature(s, "p", "checkout");
  const live = [mkTask("t1", "checkout"), mkTask("t2", "search"), mkTask("t3")];
  const plan = buildDeletePlan(s, live);
  // t1 is under the selected feature — its filter pass deletes it.
  assert.deepEqual(plan.taskPaths, ["projects/p/task/t2.md"]);
  assert.deepEqual(plan.featureIds, ["checkout"]);
  assert.deepEqual(plan.staleTaskIds, []);
});

test("plan reports stale selections instead of silently dropping them", () => {
  const s = toggleTask(EMPTY_SELECTION, "p", "gone");
  const plan = buildDeletePlan(s, [mkTask("t1")]);
  assert.deepEqual(plan.taskPaths, []);
  assert.deepEqual(plan.staleTaskIds, ["gone"]);
});

test("plan keeps titles aligned with paths for confirm copy", () => {
  let s = toggleTask(EMPTY_SELECTION, "p", "t2");
  const plan = buildDeletePlan(s, [mkTask("t2", "search")]);
  assert.deepEqual(plan.taskTitles, ["Task t2"]);
});

// ─── chunking ──────────────────────────────────────────────────────

test("chunkPaths splits at the server cap", () => {
  const paths = Array.from({ length: 250 }, (_, i) => `p/${i}`);
  const chunks = chunkPaths(paths);
  assert.deepEqual(
    chunks.map((c) => c.length),
    [100, 100, 50],
  );
  // Order preserved end to end.
  assert.equal(chunks[2][49], "p/249");
});

test("chunkPaths of an empty list is no chunks (no empty API calls)", () => {
  assert.deepEqual(chunkPaths([]), []);
});

// ─── copy ──────────────────────────────────────────────────────────

test("describeSelection grammar", () => {
  assert.equal(describeSelection(1, 0), "1 task");
  assert.equal(describeSelection(2, 1), "2 tasks and 1 feature");
  assert.equal(describeSelection(0, 3), "3 features");
  assert.equal(describeSelection(0, 0), "nothing");
});

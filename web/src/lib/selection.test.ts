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
  isRangeKey,
  selectFeatureRange,
  selectTaskRange,
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

// ─── shift-click ranges ────────────────────────────────────────────

const ORDER = ["t1", "t2", "t3", "t4", "t5"];

test("task range selects the inclusive span in visual order", () => {
  let s = toggleTask(EMPTY_SELECTION, "p", "t2");
  s = selectTaskRange(s, "p", ORDER, "t2", "t4");
  assert.deepEqual([...s.taskIds].sort(), ["t2", "t3", "t4"]);
});

test("task range works upward — target before the anchor", () => {
  let s = toggleTask(EMPTY_SELECTION, "p", "t4");
  s = selectTaskRange(s, "p", ORDER, "t4", "t1");
  assert.deepEqual([...s.taskIds].sort(), ["t1", "t2", "t3", "t4"]);
});

test("marks outside the span survive a range select", () => {
  let s = toggleTask(EMPTY_SELECTION, "p", "t5");
  s = toggleTask(s, "p", "t1");
  s = selectTaskRange(s, "p", ORDER, "t1", "t2");
  assert.deepEqual([...s.taskIds].sort(), ["t1", "t2", "t5"]);
});

test("shift-click on a SELECTED target deselects the span", () => {
  // Select everything, then shift-click back up the list: the range
  // gesture is symmetric — deselecting works the same way selecting
  // does, decided by the target row's current state.
  let s = toggleTask(EMPTY_SELECTION, "p", "t1");
  s = selectTaskRange(s, "p", ORDER, "t1", "t5"); // all selected
  s = selectTaskRange(s, "p", ORDER, "t5", "t2"); // t2..t5 deselect
  assert.deepEqual([...s.taskIds], ["t1"]);
});

test("deselect span tolerates unselected rows inside it", () => {
  let s = toggleTask(EMPTY_SELECTION, "p", "t1");
  s = toggleTask(s, "p", "t4"); // t1, t4 selected; t2, t3 not
  s = selectTaskRange(s, "p", ORDER, "t1", "t4"); // target t4 selected → deselect t1..t4
  assert.deepEqual([...s.taskIds], []);
});

test("missing or stale anchor falls back to a plain toggle of the target", () => {
  let s = selectTaskRange(EMPTY_SELECTION, "p", ORDER, null, "t3");
  assert.deepEqual([...s.taskIds], ["t3"]);
  // Anchor id no longer in the visible order (filtered out, deleted).
  s = selectTaskRange(s, "p", ORDER, "gone", "t5");
  assert.deepEqual([...s.taskIds].sort(), ["t3", "t5"]);
  // Selected target + no anchor: the toggle deselects it, matching
  // what a one-row range would do.
  s = selectTaskRange(s, "p", ORDER, null, "t3");
  assert.deepEqual([...s.taskIds], ["t5"]);
});

test("range into a different project restarts the scope at the target", () => {
  let s = toggleTask(EMPTY_SELECTION, "p1", "t1");
  s = selectTaskRange(s, "p2", ORDER, "t1", "t4");
  assert.equal(s.projectId, "p2");
  assert.deepEqual([...s.taskIds], ["t4"]);
});

test("feature range mirrors task range and leaves tasks alone", () => {
  let s = toggleTask(EMPTY_SELECTION, "p", "t1");
  s = toggleFeature(s, "p", "f1");
  s = selectFeatureRange(s, "p", ["f1", "f2", "f3"], "f1", "f3");
  assert.deepEqual([...s.featureIds].sort(), ["f1", "f2", "f3"]);
  assert.deepEqual([...s.taskIds], ["t1"]);
  // And the symmetric deselect: target f1 is selected → span clears.
  s = selectFeatureRange(s, "p", ["f1", "f2", "f3"], "f3", "f1");
  assert.deepEqual([...s.featureIds], []);
  assert.deepEqual([...s.taskIds], ["t1"]);
});

test("isRangeKey matches shift+V and nothing else", () => {
  const ev = (over: Partial<Parameters<typeof isRangeKey>[0]>) => ({
    key: "V",
    shiftKey: true,
    metaKey: false,
    ctrlKey: false,
    altKey: false,
    ...over,
  });
  assert.equal(isRangeKey(ev({})), true);
  assert.equal(isRangeKey(ev({ key: "v", shiftKey: false })), false);
  assert.equal(isRangeKey(ev({ metaKey: true })), false);
  assert.equal(isRangeKey(ev({ ctrlKey: true })), false);
  assert.equal(isRangeKey(ev({ key: "A" })), false);
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

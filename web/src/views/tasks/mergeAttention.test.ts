import { strict as assert } from "node:assert";
import { test } from "node:test";
import { MERGE_ATTENTION_WINDOW_MS, mergeReadyFeatures } from "./mergeAttention";
import type { Task } from "../../lib/types";

const NOW = Date.parse("2026-07-06T12:00:00Z");
const RECENT = "2026-07-06T10:00:00Z";

const task = (over: Partial<Task>): Task =>
  ({
    id: Math.random().toString(36).slice(2),
    path: "p",
    title: "t",
    priority: "medium",
    status: "completed",
    completed_at: RECENT,
    feature_id: "feat-a",
    merge_target_branch: "main",
    ...over,
  }) as Task;

test("fully-completed feature with merge config is merge-ready, newest first", () => {
  const out = mergeReadyFeatures(
    [
      task({ feature_id: "feat-a", completed_at: "2026-07-06T09:00:00Z" }),
      task({ feature_id: "feat-a", status: "validated", completed_at: RECENT }),
      task({ feature_id: "feat-b", completed_at: "2026-07-05T09:00:00Z" }),
    ],
    NOW,
  );
  assert.deepEqual(out.map((m) => m.feature), ["feat-a", "feat-b"]);
  assert.equal(out[0].taskCount, 2);
});

test("an unfinished task blocks the feature", () => {
  const out = mergeReadyFeatures(
    [task({}), task({ status: "in_progress" })],
    NOW,
  );
  assert.deepEqual(out, []);
});

test("automation-generated tasks are ignored for completeness", () => {
  const out = mergeReadyFeatures(
    [task({}), task({ status: "pending", generated_by: "automation:xyz" })],
    NOW,
  );
  assert.equal(out.length, 1);
});

test("merge config required; auto_merge excluded", () => {
  assert.deepEqual(mergeReadyFeatures([task({ merge_target_branch: undefined })], NOW), []);
  assert.deepEqual(
    mergeReadyFeatures([task({ merge_target_branch: "main", merge_policy: "auto_merge" })], NOW),
    [],
  );
  assert.equal(
    mergeReadyFeatures(
      [task({ merge_target_branch: undefined, merge_policy: "auto_pr", git_branch: "feat/x" })],
      NOW,
    ).length,
    1,
  );
});

test("stale completions age out of the attention window", () => {
  const old = new Date(NOW - MERGE_ATTENTION_WINDOW_MS - 1000).toISOString();
  assert.deepEqual(mergeReadyFeatures([task({ completed_at: old })], NOW), []);
});

test("featureless tasks never contribute", () => {
  assert.deepEqual(mergeReadyFeatures([task({ feature_id: undefined })], NOW), []);
});

test("completed_at falls back to modified for pre-stamp entries", () => {
  const out = mergeReadyFeatures([task({ completed_at: undefined, modified: RECENT })], NOW);
  assert.equal(out.length, 1);
});

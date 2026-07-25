import { strict as assert } from "node:assert";
import { test } from "node:test";
import type { Task, TaskStatus } from "../../lib/types";
import { computeFeatureState, deriveCheckoutDefaults } from "./featureActions";

function task(
  id: string,
  status: TaskStatus,
  extra: Partial<Task> = {},
): Task {
  return {
    id,
    path: `${id}.md`,
    title: id,
    priority: "medium",
    status,
    ...extra,
  };
}

test("computeFeatureState: all-completed set marks allCompleted true", () => {
  const s = computeFeatureState([
    task("a", "completed"),
    task("b", "validated"),
    task("c", "completed"),
  ]);
  assert.equal(s.allCompleted, true);
  assert.equal(s.anyBlockedOrWaiting, false);
  assert.equal(s.anyInProgress, false);
  assert.equal(s.taskCount, 3);
  assert.equal(s.incompleteCount, 0);
});

test("computeFeatureState: any blocked task flips anyBlockedOrWaiting", () => {
  const s = computeFeatureState([
    task("a", "completed"),
    task("b", "blocked"),
    task("c", "pending"),
  ]);
  assert.equal(s.allCompleted, false);
  assert.equal(s.anyBlockedOrWaiting, true);
  assert.equal(s.incompleteCount, 2);
});

test("computeFeatureState: pending task with waiting_on flips anyBlockedOrWaiting", () => {
  const s = computeFeatureState([
    task("a", "completed"),
    task("b", "pending", { waiting_on: ["a"] }),
  ]);
  assert.equal(s.anyBlockedOrWaiting, true);
});

test("computeFeatureState: pending task with blocked_by flips anyBlockedOrWaiting", () => {
  const s = computeFeatureState([
    task("a", "pending", { blocked_by: ["x"] }),
  ]);
  assert.equal(s.anyBlockedOrWaiting, true);
});

test("computeFeatureState: readyCount counts pending/active with no deps", () => {
  const s = computeFeatureState([
    task("a", "pending"),
    task("b", "active"),
    task("c", "pending", { waiting_on: ["a"] }),
    task("d", "completed"),
  ]);
  assert.equal(s.readyCount, 2);
});

test("computeFeatureState: in_progress task flips anyInProgress", () => {
  const s = computeFeatureState([
    task("a", "in_progress"),
    task("b", "completed"),
  ]);
  assert.equal(s.anyInProgress, true);
  assert.equal(s.allCompleted, false);
});

test("computeFeatureState: cancelled counts as incomplete but not as blocked", () => {
  const s = computeFeatureState([
    task("a", "cancelled"),
    task("b", "completed"),
  ]);
  assert.equal(s.allCompleted, false);
  assert.equal(s.anyBlockedOrWaiting, false);
  assert.equal(s.incompleteCount, 1);
});

test("computeFeatureState: empty task list is not allCompleted", () => {
  const s = computeFeatureState([]);
  assert.equal(s.allCompleted, false);
  assert.equal(s.taskCount, 0);
});

test("deriveCheckoutDefaults: inherits merge_target_branch from first task with a value", () => {
  const d = deriveCheckoutDefaults([
    task("a", "pending"),
    task("b", "pending", { merge_target_branch: "main" }),
    task("c", "pending", { merge_target_branch: "develop" }),
  ]);
  assert.equal(d.merge_target_branch, "main");
});

test("deriveCheckoutDefaults: falls back to sane server-parity defaults", () => {
  const d = deriveCheckoutDefaults([task("a", "pending")]);
  assert.equal(d.checkout_mode, "ai");
  assert.equal(d.merge_policy, "prompt_only");
  assert.equal(d.merge_strategy, "squash");
  assert.equal(d.remote_branch_policy, "keep");
  assert.equal(d.open_pr_before_merge, false);
  assert.equal(d.execution_mode, "worktree");
});

test("deriveCheckoutDefaults: task's merge_policy overrides the built-in default", () => {
  const d = deriveCheckoutDefaults([
    task("a", "pending", { merge_policy: "auto_pr" }),
  ]);
  assert.equal(d.merge_policy, "auto_pr");
});

test("deriveCheckoutDefaults: open_pr_before_merge=true is honored", () => {
  const d = deriveCheckoutDefaults([
    task("a", "pending", { open_pr_before_merge: true }),
  ]);
  assert.equal(d.open_pr_before_merge, true);
});

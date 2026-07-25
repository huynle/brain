// Pure helpers backing the Feature Actions modal. Given the list of tasks in
// a feature, compute which actions apply and derive sensible defaults for the
// feature-checkout form.

import type { FeatureCheckoutOptions, Task } from "../../lib/types";

// Statuses that count as "done" for the purpose of gating Review & Merge.
// Mirrors the server's completion check (completed, validated).
const COMPLETED_STATUSES: ReadonlySet<string> = new Set(["completed", "validated"]);

// Statuses that count as terminal-not-done (a feature with these tasks won't
// naturally complete on its own).
const TERMINAL_NOT_DONE: ReadonlySet<string> = new Set(["cancelled", "superseded", "archived"]);

export interface FeatureState {
  /** Every task in the feature is completed or validated. */
  allCompleted: boolean;
  /** At least one task is blocked, or has a non-empty waiting_on list. */
  anyBlockedOrWaiting: boolean;
  /** At least one task is currently in flight. */
  anyInProgress: boolean;
  /** Total task count in the feature (excludes filtered UI states). */
  taskCount: number;
  /** Number of tasks in a "ready" runnable state. */
  readyCount: number;
  /** Number of tasks not in a completed/validated status. */
  incompleteCount: number;
}

// isReadyTask duplicates the definition from grouping.ts to avoid a circular
// import — the file is tiny and stable so duplication is cheap. Ready means
// runnable (pending/active) with nothing to wait on.
function isReadyTask(t: Task): boolean {
  const runnable = t.status === "pending" || t.status === "active";
  const waiting = (t.waiting_on?.length ?? 0) > 0 || (t.blocked_by?.length ?? 0) > 0;
  return runnable && !waiting;
}

export function computeFeatureState(tasks: Task[]): FeatureState {
  let allCompleted = tasks.length > 0;
  let anyBlockedOrWaiting = false;
  let anyInProgress = false;
  let readyCount = 0;
  let incompleteCount = 0;

  for (const t of tasks) {
    const done = COMPLETED_STATUSES.has(t.status);
    if (!done) {
      incompleteCount++;
      // Terminal-not-done tasks (cancelled/superseded/archived) also stop
      // "all completed" from being true.
      if (!TERMINAL_NOT_DONE.has(t.status)) allCompleted = false;
      else allCompleted = false;
    }
    if (t.status === "blocked") anyBlockedOrWaiting = true;
    if ((t.waiting_on?.length ?? 0) > 0 || (t.blocked_by?.length ?? 0) > 0) {
      anyBlockedOrWaiting = true;
    }
    if (t.status === "in_progress") anyInProgress = true;
    if (isReadyTask(t)) readyCount++;
  }

  return {
    allCompleted,
    anyBlockedOrWaiting,
    anyInProgress,
    taskCount: tasks.length,
    readyCount,
    incompleteCount,
  };
}

// deriveCheckoutDefaults reads sensible defaults for the checkout form off the
// task set. Prefers the first non-empty value seen across tasks so multi-task
// features inherit their existing merge configuration.
export function deriveCheckoutDefaults(tasks: Task[]): FeatureCheckoutOptions {
  const defaults: FeatureCheckoutOptions = {
    checkout_mode: "ai",
    merge_policy: "prompt_only",
    merge_strategy: "squash",
    remote_branch_policy: "keep",
    open_pr_before_merge: false,
    execution_mode: "worktree",
  };

  for (const t of tasks) {
    if (!defaults.merge_target_branch && t.merge_target_branch) {
      defaults.merge_target_branch = t.merge_target_branch;
    }
    // t.merge_policy on the Task type is optional; only override the built-in
    // default (prompt_only) if a task actually sets one.
    if (t.merge_policy) defaults.merge_policy = t.merge_policy;
    if (t.merge_strategy) defaults.merge_strategy = t.merge_strategy;
    if (typeof t.open_pr_before_merge === "boolean") {
      defaults.open_pr_before_merge = t.open_pr_before_merge;
    }
    if (t.execution_mode) defaults.execution_mode = t.execution_mode;
  }

  return defaults;
}

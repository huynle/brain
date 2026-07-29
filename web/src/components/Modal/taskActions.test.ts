/**
 * Test coverage for computeTaskResumeState + abandonReasonCopy.
 *
 * Covers every branch of the state helper so a future refactor doesn't
 * silently break the TaskModal Actions-button gating (which now consumes
 * this helper directly per commit c103f10).
 */
import { describe, test } from "node:test";
import assert from "node:assert/strict";
import {
  computeTaskResumeState,
  abandonReasonCopy,
} from "./taskActions";
import type { Task } from "../../lib/types";

/** Minimal Task factory — only the fields computeTaskResumeState reads. */
function mkTask(overrides: Partial<Task>): Task {
  return {
    id: "t1",
    path: "projects/p/task/t1.md",
    title: "test",
    priority: "medium",
    status: "pending",
    ...overrides,
  } as Task;
}

describe("computeTaskResumeState", () => {
  test("null / undefined task → empty state", () => {
    const empty = {
      showResume: false,
      canResumeCleanly: false,
      alreadyResumed: false,
      reasonHint: "",
      forceHint: "",
    };
    assert.deepEqual(computeTaskResumeState(null), empty);
    assert.deepEqual(computeTaskResumeState(undefined), empty);
  });

  test("pending + resume_requested → alreadyResumed no-op branch", () => {
    const state = computeTaskResumeState(
      mkTask({ status: "pending", resume_requested: true }),
    );
    assert.equal(state.showResume, true);
    assert.equal(state.alreadyResumed, true);
    assert.equal(state.canResumeCleanly, true);
    assert.match(state.reasonHint, /already requested/i);
  });

  test("is_abandoned → clean resume path with reason hint", () => {
    const state = computeTaskResumeState(
      mkTask({
        status: "in_progress",
        is_abandoned: true,
        abandon_reason: "runner_offline",
      }),
    );
    assert.equal(state.showResume, true);
    assert.equal(state.canResumeCleanly, true);
    assert.equal(state.alreadyResumed, false);
    // Reason should mention the runner going offline.
    assert.match(state.reasonHint, /offline/i);
  });

  test("pending + no resume_requested → stuck-pending force path", () => {
    const state = computeTaskResumeState(
      mkTask({ status: "pending" }),
    );
    assert.equal(state.showResume, true);
    assert.equal(state.canResumeCleanly, false);
    assert.equal(state.alreadyResumed, false);
    assert.match(state.forceHint, /force/i);
  });

  test("terminal statuses → empty state (use Trigger instead)", () => {
    for (const status of [
      "completed",
      "validated",
      "cancelled",
      "superseded",
      "archived",
    ] as const) {
      const state = computeTaskResumeState(mkTask({ status }));
      assert.equal(
        state.showResume,
        false,
        `status ${status} should NOT show Resume`,
      );
    }
  });

  test("blocked task (no reaper marker) → force path with hint", () => {
    const state = computeTaskResumeState(mkTask({ status: "blocked" }));
    assert.equal(state.showResume, true);
    assert.equal(state.canResumeCleanly, false);
    assert.match(state.forceHint, /force/i);
  });

  test("in_progress (live) → force path", () => {
    // No is_abandoned, no resume_requested — the server will refuse via
    // live-claim safety, but the UI still surfaces the affordance so the
    // user learns why.
    const state = computeTaskResumeState(mkTask({ status: "in_progress" }));
    assert.equal(state.showResume, true);
    assert.equal(state.canResumeCleanly, false);
  });
});

describe("abandonReasonCopy", () => {
  test("known reasons produce distinct user-facing copy", () => {
    const copies = new Set([
      abandonReasonCopy("no_claim"),
      abandonReasonCopy("claim_expired"),
      abandonReasonCopy("runner_offline"),
      abandonReasonCopy("orphan_reaped"),
    ]);
    assert.equal(copies.size, 4, "each reason should have distinct copy");
  });

  test("unknown / empty reason falls back to generic string", () => {
    assert.match(abandonReasonCopy(""), /abandon/i);
    assert.match(abandonReasonCopy(undefined), /abandon/i);
    assert.match(abandonReasonCopy(null), /abandon/i);
    assert.match(abandonReasonCopy("weird_unseen_reason"), /abandon/i);
  });
});

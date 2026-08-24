/**
 * Toast-copy tests for the feature-run summarizer.
 *
 * The bug these pin: a feature run where the server queues every ready task
 * and dispatches none used to summarize as "Not triggered: nothing to
 * dispatch" — indistinguishable, to the user, from the menu item doing
 * nothing at all. The cause was always present in the payload, either as the
 * promoted top-level `reason`/`detail` or as a per-task entry in `results`.
 * Both routes are covered here, because a redeployed PWA has to stay honest
 * against a server that has not been redeployed yet.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import { summarizeRunFeatureResult, type RunFeatureResponse } from "./api";

const base: RunFeatureResponse = {
  dispatched: false,
  projectId: "supernote",
  featureId: "supernote-brain-sync",
  dispatchedCount: 0,
  skippedCount: 0,
};

const mk = (over: Partial<RunFeatureResponse>): RunFeatureResponse => ({
  ...base,
  ...over,
});

test("clean dispatch reports the count", () => {
  const { message, kind } = summarizeRunFeatureResult(
    mk({ dispatched: true, dispatchedCount: 3 }),
  );
  assert.equal(message, "Dispatched 3 tasks");
  assert.equal(kind, "success");
});

test("single dispatch is not pluralized", () => {
  const { message } = summarizeRunFeatureResult(
    mk({ dispatched: true, dispatchedCount: 1 }),
  );
  assert.equal(message, "Dispatched 1 task");
});

test("partial dispatch names the queued remainder", () => {
  const { message, kind } = summarizeRunFeatureResult(
    mk({ dispatched: true, dispatchedCount: 1, queued: ["b", "c"] }),
  );
  assert.equal(
    message,
    "Dispatched 1, queued 2 (auto-dispatch as slots free)",
  );
  assert.equal(kind, "success");
});

test("queued-with-nothing-dispatched says so and names the cause", () => {
  const { message, kind } = summarizeRunFeatureResult(
    mk({
      queued: ["5to7av6m"],
      skippedCount: 1,
      reason: "no_eligible_runner",
      detail: "runner_352e9fd5: project not allowed",
    }),
  );
  assert.equal(
    message,
    "Queued 1 task, nothing dispatched: no eligible runner (runner_352e9fd5: project not allowed)",
  );
  assert.equal(kind, "info");
});

test("queued plural", () => {
  const { message } = summarizeRunFeatureResult(
    mk({
      queued: ["a", "b"],
      skippedCount: 2,
      reason: "no_eligible_runner",
      detail: "runner-a: at capacity",
    }),
  );
  assert.match(message, /^Queued 2 tasks, nothing dispatched: /);
});

test("a server that leaves reason empty is read out of results", () => {
  // Verbatim shape of the production payload that started this: no
  // top-level reason, the whole story in results[0].
  const { message, kind } = summarizeRunFeatureResult(
    mk({
      queued: ["5to7av6m"],
      skippedCount: 1,
      results: [
        {
          dispatched: false,
          taskId: "5to7av6m",
          projectId: "supernote",
          reason: "no_eligible_runner",
          detail: "runner_352e9fd5: project not allowed",
        },
      ],
    }),
  );
  assert.equal(
    message,
    "Queued 1 task, nothing dispatched: no eligible runner (runner_352e9fd5: project not allowed)",
  );
  assert.equal(kind, "info");
});

test("dispatched entries in results are not mistaken for the cause", () => {
  const { message } = summarizeRunFeatureResult(
    mk({
      queued: ["b"],
      skippedCount: 1,
      results: [
        { dispatched: true, taskId: "a", projectId: "supernote" },
        {
          dispatched: false,
          taskId: "b",
          projectId: "supernote",
          reason: "no_online_runner",
        },
      ],
    }),
  );
  assert.equal(
    message,
    "Queued 1 task, nothing dispatched: no runners are online",
  );
});

test("no runners online reads plainly, without echoing the detail", () => {
  const { message } = summarizeRunFeatureResult(
    mk({
      skippedCount: 1,
      reason: "no_online_runner",
      detail: "no runners are registered",
    }),
  );
  assert.equal(message, "Not triggered: no runners are online");
});

test("known feature-level reasons keep their copy", () => {
  assert.equal(
    summarizeRunFeatureResult(mk({ reason: "no_ready_tasks" })).message,
    "Not triggered: no ready tasks in this feature (check dependencies)",
  );
  assert.equal(
    summarizeRunFeatureResult(mk({ reason: "feature_in_progress" })).message,
    "Not triggered: every ready task is already in flight",
  );
  assert.equal(
    summarizeRunFeatureResult(mk({ reason: "feature_not_found" })).message,
    "Not triggered: feature not found",
  );
  assert.equal(
    summarizeRunFeatureResult(mk({ reason: "scheduler_not_configured" }))
      .message,
    "Not triggered: scheduler not configured on server",
  );
});

test("an unknown reason still surfaces its detail", () => {
  const { message } = summarizeRunFeatureResult(
    mk({ reason: "brand_new_reason", detail: "something specific" }),
  );
  assert.equal(message, "Not triggered: brand_new_reason: something specific");
});

test("a genuinely empty response keeps the old generic copy", () => {
  const { message, kind } = summarizeRunFeatureResult(mk({}));
  assert.equal(message, "Not triggered: nothing to dispatch");
  assert.equal(kind, "info");
});

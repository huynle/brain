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

/**
 * The lie this file now guards against: a dispatch lease stuck in "pushed"
 * — pushed to a runner that never acknowledged it — used to be summarized as
 * "every ready task is already in flight". Nothing was running. The user was
 * told to go find a process that did not exist, with no hint that the hold
 * clears itself.
 */
test("an unacknowledged dispatch is not reported as in flight", () => {
  const { message } = summarizeRunFeatureResult(
    mk({
      skippedCount: 1,
      queued: ["8f5qhpj5"],
      reason: "feature_dispatch_pending",
      results: [
        {
          dispatched: false,
          taskId: "8f5qhpj5",
          projectId: "sandbox-demo",
          runnerId: "runner-a",
          leaseState: "pushed",
          reason: "already_leased",
        },
      ],
    }),
  );
  assert.ok(
    !message.includes("in flight"),
    `must not claim work is in flight: ${message}`,
  );
  assert.ok(
    message.includes("nothing is running"),
    `must say nothing is running: ${message}`,
  );
});

test("a pending dispatch says when the hold clears", () => {
  const expiresAt = new Date(Date.now() + 90_000).toISOString();
  const { message } = summarizeRunFeatureResult(
    mk({
      skippedCount: 1,
      reason: "feature_dispatch_pending",
      results: [
        {
          dispatched: false,
          taskId: "8f5qhpj5",
          projectId: "sandbox-demo",
          runnerId: "runner-a",
          leaseState: "pushed",
          expiresAt,
          reason: "already_leased",
        },
      ],
    }),
  );
  assert.ok(
    /clears in \d+[smhd]/.test(message),
    `want a relative clear time: ${message}`,
  );
});

test("an acked lease keeps the in-flight wording", () => {
  const { message } = summarizeRunFeatureResult(
    mk({ skippedCount: 1, reason: "feature_in_progress" }),
  );
  assert.equal(message, "Not triggered: every ready task is already in flight");
});

test("an undelivered dispatch names the delivery failure", () => {
  const { message } = summarizeRunFeatureResult(
    mk({
      skippedCount: 1,
      reason: "runner_unreachable",
      detail:
        "runner-a is registered but its command stream is not connected; the dispatch was not delivered",
    }),
  );
  assert.ok(
    message.includes("not delivered"),
    `want the delivery failure named: ${message}`,
  );
  assert.ok(message.includes("runner-a"), `want the runner named: ${message}`);
});

test("no ready tasks names the feature being waited on", () => {
  const { message } = summarizeRunFeatureResult(
    mk({ reason: "no_ready_tasks", waitingOnFeatures: ["data-pipeline"] }),
  );
  assert.equal(message, "Not triggered: no ready tasks — waiting on data-pipeline");
});

test("no ready tasks distinguishes blocked from waiting", () => {
  const { message } = summarizeRunFeatureResult(
    mk({ reason: "no_ready_tasks", blockedByFeatures: ["data-pipeline"] }),
  );
  assert.equal(message, "Not triggered: no ready tasks — blocked by data-pipeline");
});

test("no ready tasks without a feature hold keeps the generic copy", () => {
  const { message } = summarizeRunFeatureResult(mk({ reason: "no_ready_tasks" }));
  assert.equal(
    message,
    "Not triggered: no ready tasks in this feature (check dependencies)",
  );
});

test("a per-task pushed lease reads as not-running in the legacy path", () => {
  // Older server: no top-level reason, so the PWA reads `results`.
  const { message } = summarizeRunFeatureResult(
    mk({
      skippedCount: 1,
      results: [
        {
          dispatched: false,
          taskId: "8f5qhpj5",
          projectId: "sandbox-demo",
          runnerId: "runner-a",
          leaseState: "pushed",
          reason: "already_leased",
        },
      ],
    }),
  );
  assert.ok(
    message.includes("not acknowledged yet"),
    `want the unacked state surfaced: ${message}`,
  );
  assert.ok(
    message.includes("nothing running"),
    `want the absence of a process stated: ${message}`,
  );
});

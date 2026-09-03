/**
 * Tests for lib/featureAssignments.
 *
 * These pin the three things that were actually wrong, not the happy path:
 * an AUTO assignment must be visible (no click ever writes it locally), a
 * released row must not read as live, and the optimistic overlay must win
 * in BOTH directions while a mutation is in flight but not outlive it.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  CLEARED,
  resolveFeatureAssignments,
  settledAssignments,
} from "./featureAssignments";
import type { RunnerInfo } from "./types";

const runner = (id: string, assignments: unknown[] = []): RunnerInfo =>
  ({
    runner_id: id,
    status: "online",
    feature_assignments: assignments,
  }) as unknown as RunnerInfo;

const a = (over: Record<string, unknown>) => ({
  feature_id: "feat-1",
  project_id: "proj",
  runner_id: "runner-a",
  source: "manual",
  status: "active",
  ...over,
});

test("an assignment the server reports is visible with no local state", () => {
  // The whole bug: the UI only ever read its own optimistic map, so a
  // feature assigned on another device — or by any means other than a click
  // in THIS browser — showed as unassigned.
  const got = resolveFeatureAssignments([runner("runner-a", [a({})])], {});
  assert.deepEqual(got, { "feat-1": "runner-a" });
});

test("an AUTO assignment is visible", () => {
  // task.go auto-claims a feature for the first runner to pick up one of its
  // tasks. Nothing in the UI writes that, so under the old code an auto
  // assignment was invisible everywhere, always. Live production had one.
  const got = resolveFeatureAssignments(
    [runner("runner-a", [a({ source: "auto" })])],
    {},
  );
  assert.equal(got["feat-1"], "runner-a");
});

test("a released row does not read as a live assignment", () => {
  // ClearFeatureAssignmentsByRunner MARKS rows rather than deleting them, so
  // treating any row as live would pin a feature to a runner that let it go.
  for (const status of ["released", "cleared", "inactive"]) {
    const got = resolveFeatureAssignments(
      [runner("runner-a", [a({ status })])],
      {},
    );
    assert.deepEqual(got, {}, `status=${status} must not count`);
  }
});

test("assignments from several runners all resolve", () => {
  const got = resolveFeatureAssignments(
    [
      runner("runner-a", [a({ feature_id: "feat-1", runner_id: "runner-a" })]),
      runner("runner-b", [a({ feature_id: "feat-2", runner_id: "runner-b" })]),
    ],
    {},
  );
  assert.deepEqual(got, { "feat-1": "runner-a", "feat-2": "runner-b" });
});

test("a row missing runner_id falls back to the runner carrying it", () => {
  const got = resolveFeatureAssignments(
    [runner("runner-a", [a({ runner_id: undefined })])],
    {},
  );
  assert.equal(got["feat-1"], "runner-a");
});

test("the optimistic overlay wins while an assign is in flight", () => {
  const got = resolveFeatureAssignments([runner("runner-a", [a({})])], {
    "feat-1": "runner-b",
  });
  assert.equal(got["feat-1"], "runner-b");
});

test("an optimistic clear hides a server row that still exists", () => {
  // Without a tombstone this is impossible: absence means "no opinion", so
  // the row being deleted would win the merge straight back and the UI would
  // appear to ignore the click.
  const got = resolveFeatureAssignments([runner("runner-a", [a({})])], {
    "feat-1": CLEARED,
  });
  assert.equal("feat-1" in got, false);
});

test("an optimistic assign with no server row yet still shows", () => {
  const got = resolveFeatureAssignments([runner("runner-a", [])], {
    "feat-9": "runner-a",
  });
  assert.equal(got["feat-9"], "runner-a");
});

test("settledAssignments retires an overlay the server has caught up with", () => {
  const runners = [runner("runner-a", [a({})])];
  // Server now agrees with the optimistic assign — drop it.
  assert.deepEqual(settledAssignments(runners, { "feat-1": "runner-a" }), [
    "feat-1",
  ]);
  // Server still disagrees — the mutation is in flight, keep it.
  assert.deepEqual(settledAssignments(runners, { "feat-1": "runner-b" }), []);
});

test("settledAssignments retires a tombstone once the row is gone", () => {
  const runners = [runner("runner-a", [])];
  assert.deepEqual(settledAssignments(runners, { "feat-1": CLEARED }), [
    "feat-1",
  ]);
  // But not while the server still reports the row.
  assert.deepEqual(
    settledAssignments([runner("runner-a", [a({})])], { "feat-1": CLEARED }),
    [],
  );
});

test("no runners means no assignments, not a crash", () => {
  assert.deepEqual(resolveFeatureAssignments([], {}), {});
  assert.deepEqual(resolveFeatureAssignments([runner("r")], {}), {});
  // A runner payload with the field absent entirely.
  assert.deepEqual(
    resolveFeatureAssignments([{ runner_id: "r" } as unknown as RunnerInfo], {}),
    {},
  );
});

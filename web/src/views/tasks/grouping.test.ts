import { strict as assert } from "node:assert";
import { test } from "node:test";
import type { Task } from "../../lib/types";
import { filterTasks, groupByFeature, UNGROUPED } from "./grouping";

function task(id: string, feature_id: string | undefined, status: Task["status"], created: string): Task {
  return {
    id,
    path: `${id}.md`,
    title: id,
    priority: "medium",
    status,
    created,
    feature_id,
  };
}

test("groupByFeature sorts features by most recent completed date", () => {
  const groups = groupByFeature([
    task("old-open", "alpha", "pending", "2026-06-17T09:00:00Z"),
    task("old-done", "alpha", "completed", "2026-06-17T10:00:00Z"),
    task("new-done", "bravo", "completed", "2026-06-17T12:00:00Z"),
    task("middle-done", "charlie", "validated", "2026-06-17T11:00:00Z"),
  ]);

  assert.deepEqual(groups.map((g) => g.feature), ["bravo", "charlie", "alpha"]);
});

test("groupByFeature supports alternate feature sort modes", () => {
  const tasks = [
    task("a", "alpha", "pending", "2026-06-17T09:00:00Z"),
    task("b", "bravo", "completed", "2026-06-17T12:00:00Z"),
  ];

  assert.deepEqual(groupByFeature(tasks, "name").map((g) => g.feature), ["alpha", "bravo"]);
  assert.deepEqual(groupByFeature(tasks, "created").map((g) => g.feature), ["bravo", "alpha"]);
});

test("ready tasks sort above waiting tasks within the same status", () => {
  const groups = groupByFeature([
    { id: "w", title: "waiting", status: "pending", priority: "high", waiting_on: ["x"] } as never,
    { id: "r", title: "ready", status: "pending", priority: "low" } as never,
  ]);
  assert.deepEqual(groups[0].tasks.map((t) => t.id), ["r", "w"]);
});

test("sort direction reverses feature order but keeps UNGROUPED pinned last", () => {
  const mk = (feature: string | undefined, created: string) =>
    ({ id: feature ?? "u", title: feature ?? "u", status: "pending", priority: "low", feature_id: feature, created }) as never;
  const tasks = [mk("old-feat", "2026-01-01T00:00:00Z"), mk("new-feat", "2026-06-01T00:00:00Z"), mk(undefined, "2026-07-01T00:00:00Z")];
  const desc = groupByFeature(tasks, "created", "desc").map((g) => g.feature);
  const asc = groupByFeature(tasks, "created", "asc").map((g) => g.feature);
  assert.deepEqual(desc, ["new-feat", "old-feat", UNGROUPED]);
  assert.deepEqual(asc, ["old-feat", "new-feat", UNGROUPED]);
});

test("filterTasks accepts field queries", () => {
  const tasks = [
    { id: "a", title: "A", status: "blocked", priority: "low", feature_id: "auth" } as never,
    { id: "b", title: "B", status: "pending", priority: "low", feature_id: "auth" } as never,
  ];
  assert.deepEqual(filterTasks(tasks, "status:blocked").map((t) => t.id), ["a"]);
  assert.deepEqual(filterTasks(tasks, "feature:auth status:pending").map((t) => t.id), ["b"]);
});

test("completed feature sort prefers completed_at over modified", () => {
  // A task's completed_at is the source of truth for "when did this feature
  // finish?". Falling back to modified conflates completion with later edits
  // — e.g. a feature completed last week that got a note appended today
  // would sort as if it finished today. This test locks in the correct
  // precedence: completed_at wins when present, modified is a fallback.
  const feat = (id: string, completed_at: string | undefined, modified: string): Task => ({
    id,
    path: `${id}.md`,
    title: id,
    priority: "medium",
    status: "completed",
    created: "2026-01-01T00:00:00Z",
    modified,
    completed_at,
    feature_id: id, // one task per feature — feature label == id
  });

  const groups = groupByFeature([
    // "old-actually" was completed last Monday but got a note edit today.
    // Under completed_at semantics it should sort BELOW "new-actually",
    // which was completed today. Under (buggy) modified-only semantics,
    // "old-actually" would win because its modified is today.
    feat("old-actually", "2026-07-01T09:00:00Z", "2026-07-07T14:00:00Z"),
    feat("new-actually", "2026-07-07T10:00:00Z", "2026-07-07T10:00:00Z"),
  ]);

  assert.deepEqual(
    groups.map((g) => g.feature),
    ["new-actually", "old-actually"],
    "features should sort by completed_at, not modified",
  );
});

test("completed feature sort falls back to modified when completed_at missing", () => {
  // Legacy entries created before the completed_at column existed still
  // need to sort sanely. Fall back chain: completed_at → modified → created.
  const feat = (id: string, modified: string): Task => ({
    id,
    path: `${id}.md`,
    title: id,
    priority: "medium",
    status: "completed",
    created: "2026-01-01T00:00:00Z",
    modified,
    feature_id: id,
  });

  const groups = groupByFeature([
    feat("older", "2026-06-01T00:00:00Z"),
    feat("newer", "2026-07-01T00:00:00Z"),
  ]);

  assert.deepEqual(groups.map((g) => g.feature), ["newer", "older"]);
});

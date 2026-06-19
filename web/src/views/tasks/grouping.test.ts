import { strict as assert } from "node:assert";
import { test } from "node:test";
import type { Task } from "../../lib/types";
import { groupByFeature } from "./grouping";

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

import { strict as assert } from "node:assert";
import { test } from "node:test";
import type { Task } from "../../lib/types";
import { filterByHiddenStatuses } from "./statusFilters";

function task(id: string, status: Task["status"]): Task {
  return {
    id,
    path: `${id}.md`,
    title: id,
    priority: "medium",
    status,
    created: "2026-01-01T00:00:00Z",
  };
}

test("filterByHiddenStatuses hides only selected statuses", () => {
  const tasks = [
    task("t1", "pending"),
    task("t2", "in_progress"),
    task("t3", "completed"),
    task("t4", "draft"),
  ];

  const visible = filterByHiddenStatuses(tasks, new Set(["completed", "draft"]));

  assert.deepEqual(visible.map((t) => t.id), ["t1", "t2"]);
});

test("filterByHiddenStatuses returns input unchanged when hidden set is empty", () => {
  const tasks = [task("t1", "pending"), task("t2", "completed")];
  // Same reference identity when no filtering happens keeps the memoization in
  // TasksView from re-computing downstream state on every render.
  assert.equal(filterByHiddenStatuses(tasks, new Set()), tasks);
});

test("filterByHiddenStatuses preserves task ordering", () => {
  const tasks = [
    task("first", "pending"),
    task("middle", "blocked"),
    task("last", "pending"),
  ];

  const visible = filterByHiddenStatuses(tasks, new Set(["blocked"]));

  assert.deepEqual(visible.map((t) => t.id), ["first", "last"]);
});

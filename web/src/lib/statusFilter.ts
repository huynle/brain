// Task-status filter helpers.
//
// The sidebar chip row (All / Active / Ready / Blocked / Done) drives a
// `StatusFilter` value in the workspace store. Both the sidebar Projects
// list and the workspace overview grid narrow their project rows/cards to
// projects matching the current filter — a project matches iff at least
// one of its tasks has the corresponding status.
//
// `all` disables the filter (every project passes).
//
// This module is pure so it stays unit-testable without pulling in
// zustand or react.

import type { StatusFilter } from "../store/workspace";

export interface StatusFilterTask {
  status?: string;
}

/** Does a single task match the filter? `all` matches everything. */
export function taskMatchesStatusFilter(
  task: StatusFilterTask,
  filter: StatusFilter,
): boolean {
  switch (filter) {
    case "all":
      return true;
    case "active":
      return task.status === "in_progress";
    case "ready":
      return task.status === "pending";
    case "blocked":
      return task.status === "blocked";
    case "done":
      return task.status === "completed" || task.status === "validated";
  }
}

/**
 * Does a project (represented by its live task list) match the filter?
 * Empty task lists never match a non-`all` filter — a project with no
 * live tasks contributes nothing to any specific status bucket.
 */
export function projectMatchesStatusFilter(
  tasks: readonly StatusFilterTask[],
  filter: StatusFilter,
): boolean {
  if (filter === "all") return true;
  return tasks.some((t) => taskMatchesStatusFilter(t, filter));
}

import type { Task } from "../../lib/types";

/**
 * Filter tasks by hiding those whose status is in the hidden set.
 *
 * When the hidden set is empty, returns the input array *by reference* so
 * memoization consumers in the caller don't re-run for a no-op change.
 */
export function filterByHiddenStatuses(
  tasks: Task[],
  hidden: ReadonlySet<string>,
): Task[] {
  if (hidden.size === 0) return tasks;
  return tasks.filter((t) => !hidden.has(t.status));
}

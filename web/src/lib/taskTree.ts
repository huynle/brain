/**
 * lib/taskTree — task dependency forests for the project cards.
 *
 * Thin domain layer over `lib/depTree`: it picks the right id/dep/parent
 * accessors for a `Task` and supplies the sibling ordering used across
 * surfaces (priority, then status). Everything structural lives in depTree.
 *
 * Why `resolved_deps` and not `depends_on`: `depends_on` is authored by
 * humans and agents, so it holds a mix of task ids and free-text titles.
 * The server resolves those refs against the project (see
 * `internal/service/taskdeps.go ResolveDependencies`) and hands back
 * `resolved_deps` — actual ids — plus `unresolved_deps` for the refs it
 * could not match. Building edges from ids is the only way the tree
 * agrees with the server's own blocked/ready classification.
 */
import { buildDepForest, type DepNode } from "./depTree";
import type { Task } from "./types";

// Sibling ordering: priority first, then status.
const PRIORITY_ORDER: Record<string, number> = {
  high: 0,
  medium: 1,
  low: 2,
};

const STATUS_ORDER: Record<string, number> = {
  in_progress: 0,
  pending: 1,
  blocked: 2,
  cancelled: 3,
  completed: 4,
  draft: 5,
  active: 6,
  validated: 7,
  superseded: 8,
  archived: 9,
};

/**
 * Sort weight for a task: priority band first, status within the band.
 * Unknown values sort last within their dimension rather than first, so
 * a typo'd priority never jumps a genuine high-priority task.
 */
export function taskRank(task: Task): number {
  const p = PRIORITY_ORDER[task.priority] ?? 3;
  const s = STATUS_ORDER[task.status] ?? 10;
  return p * 100 + s;
}

/**
 * Build the dependency forest for one bucket of tasks (typically the
 * tasks of a single feature). Edges come from `resolved_deps`, with
 * `parent_id` taking precedence for placement.
 */
export function buildTaskForest(tasks: readonly Task[]): DepNode<Task>[] {
  return buildDepForest(tasks, {
    id: (t) => t.id,
    deps: (t) => t.resolved_deps,
    parent: (t) => t.parent_id,
    rank: (t) => taskRank(t),
  });
}

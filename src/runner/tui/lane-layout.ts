/**
 * Lane Layout - Git-graph style lane rendering for task dependencies
 *
 * Pure functions for computing topological sort, lane assignments,
 * and merge point detection. No React/Ink dependency.
 */

import type { TaskDisplay } from './types';

/**
 * Lane assignment for a single task in the graph visualization
 */
export interface LaneAssignment {
  taskId: string;
  lane: number;           // column position (0 = leftmost)
  activeLanes: number[];  // which lanes have vertical lines at this row
  isMerge: boolean;       // true if task has 2+ in-tree dependencies
  mergeFromLanes: number[]; // lanes that converge at this merge point
}

// ---------------------------------------------------------------------------
// topoSort
// ---------------------------------------------------------------------------

/**
 * Topological sort using Kahn's algorithm.
 * Dependencies appear before dependents in the output.
 * If cycles exist, cycled tasks are appended at the end (never throws).
 */
export function topoSort(tasks: TaskDisplay[]): TaskDisplay[] {
  if (tasks.length === 0) return [];

  // Build a set of known task IDs for filtering out-of-tree references
  const taskIds = new Set(tasks.map((t) => t.id));
  const taskById = new Map(tasks.map((t) => [t.id, t]));

  // In-degree: count of in-tree dependencies for each task
  const inDegree = new Map<string, number>();
  // Adjacency: task -> list of tasks that depend on it (its dependents that are in-tree)
  const dependentsOf = new Map<string, string[]>();

  for (const task of tasks) {
    inDegree.set(task.id, 0);
    dependentsOf.set(task.id, []);
  }

  // Build graph edges from dependencies field
  for (const task of tasks) {
    for (const depId of task.dependencies) {
      if (!taskIds.has(depId)) continue; // skip out-of-tree refs
      // depId -> task.id  (task depends on depId, so depId is a predecessor)
      inDegree.set(task.id, (inDegree.get(task.id) || 0) + 1);
      dependentsOf.get(depId)!.push(task.id);
    }
  }

  // Kahn's: start with nodes that have in-degree 0
  const queue: string[] = [];
  for (const [id, deg] of inDegree) {
    if (deg === 0) queue.push(id);
  }

  const sorted: TaskDisplay[] = [];

  while (queue.length > 0) {
    const id = queue.shift()!;
    sorted.push(taskById.get(id)!);

    for (const depId of dependentsOf.get(id) || []) {
      const newDeg = (inDegree.get(depId) || 1) - 1;
      inDegree.set(depId, newDeg);
      if (newDeg === 0) {
        queue.push(depId);
      }
    }
  }

  // Any tasks not in sorted are part of cycles — append them at the end
  if (sorted.length < tasks.length) {
    const sortedIds = new Set(sorted.map((t) => t.id));
    for (const task of tasks) {
      if (!sortedIds.has(task.id)) {
        sorted.push(task);
      }
    }
  }

  return sorted;
}

// ---------------------------------------------------------------------------
// detectMergePoints
// ---------------------------------------------------------------------------

/**
 * Detect tasks with 2+ in-tree dependencies (merge nodes).
 * Only counts dependencies whose IDs exist in the input task list.
 */
export function detectMergePoints(tasks: TaskDisplay[]): Set<string> {
  const taskIds = new Set(tasks.map((t) => t.id));
  const merges = new Set<string>();

  for (const task of tasks) {
    const inTreeDeps = task.dependencies.filter((d) => taskIds.has(d));
    if (inTreeDeps.length >= 2) {
      merges.add(task.id);
    }
  }

  return merges;
}

// ---------------------------------------------------------------------------
// assignLanes
// ---------------------------------------------------------------------------

/**
 * Assign each topo-sorted task a lane (column index) for graph rendering.
 *
 * Design:
 * - Lane 0 = leftmost "main" lane (feature trunk)
 * - Fork: when a task has multiple in-tree dependents, first child inherits
 *   the parent's lane, additional children get new lanes (or reuse freed ones)
 * - Merge: when a task has multiple in-tree dependencies, it takes the
 *   lowest-numbered dependency lane and frees the others
 * - Freed lanes are reused via a free list (min-heap style: always pick lowest)
 */
export function assignLanes(sortedTasks: TaskDisplay[]): LaneAssignment[] {
  if (sortedTasks.length === 0) return [];

  const taskIds = new Set(sortedTasks.map((t) => t.id));
  const mergePoints = detectMergePoints(sortedTasks);

  // Track which lane each task occupies
  const taskLane = new Map<string, number>();

  // Active lanes: set of lane indices currently in use
  const activeLaneSet = new Set<number>();

  // Free list of lanes available for reuse (sorted ascending)
  const freeLanes: number[] = [];
  let nextNewLane = 0;

  // Track how many in-tree dependents still need to be processed for each task
  // When this reaches 0, the lane can be freed
  const remainingDependents = new Map<string, number>();

  // Pre-compute in-tree dependents count for each task
  for (const task of sortedTasks) {
    const inTreeDependents = task.dependents.filter((d) => taskIds.has(d));
    remainingDependents.set(task.id, inTreeDependents.length);
  }

  function allocateLane(): number {
    if (freeLanes.length > 0) {
      // Sort to always pick lowest available
      freeLanes.sort((a, b) => a - b);
      return freeLanes.shift()!;
    }
    return nextNewLane++;
  }

  function freeLane(lane: number): void {
    activeLaneSet.delete(lane);
    freeLanes.push(lane);
  }

  // Track which parent lanes have already been "claimed" by a child.
  // When a parent forks (has multiple dependents), the first child inherits
  // the parent's lane; subsequent children must get new lanes.
  const claimedLanes = new Set<string>(); // key: "parentId:lane"

  function tryClaimParentLane(parentId: string): number | null {
    const parentLane = taskLane.get(parentId);
    if (parentLane === undefined) return null;
    const key = `${parentId}:${parentLane}`;
    if (claimedLanes.has(key)) return null; // already claimed by another child
    claimedLanes.add(key);
    return parentLane;
  }

  const results: LaneAssignment[] = [];

  for (const task of sortedTasks) {
    const inTreeDeps = task.dependencies.filter((d) => taskIds.has(d));
    const isMerge = mergePoints.has(task.id);
    let lane: number;
    const mergeFromLanes: number[] = [];

    if (inTreeDeps.length === 0) {
      // Root task or independent: allocate a new lane
      lane = allocateLane();
    } else if (inTreeDeps.length === 1) {
      // Single dependency: try to inherit its lane (first child wins)
      const claimed = tryClaimParentLane(inTreeDeps[0]);
      lane = claimed !== null ? claimed : allocateLane();
    } else {
      // Merge: take the lowest lane from dependencies, free the rest
      const depLanes: number[] = [];
      for (const depId of inTreeDeps) {
        const claimed = tryClaimParentLane(depId);
        if (claimed !== null) {
          depLanes.push(claimed);
        } else {
          // Parent lane already claimed; use whatever lane the dep is on
          const dl = taskLane.get(depId);
          if (dl !== undefined) depLanes.push(dl);
        }
      }

      if (depLanes.length === 0) {
        lane = allocateLane();
      } else {
        // Deduplicate and sort
        const uniqueLanes = [...new Set(depLanes)].sort((a, b) => a - b);
        lane = uniqueLanes[0]; // take lowest

        // Record merge-from lanes (all dep lanes except the one we're taking)
        for (const dl of uniqueLanes) {
          if (dl !== lane) {
            mergeFromLanes.push(dl);
          }
        }
      }
    }

    // Register this task's lane
    taskLane.set(task.id, lane);
    activeLaneSet.add(lane);

    // Decrement remaining dependents for each in-tree dependency
    // If a dependency has no more remaining dependents and its lane
    // is different from the current task's lane, free it
    for (const depId of inTreeDeps) {
      const remaining = (remainingDependents.get(depId) || 1) - 1;
      remainingDependents.set(depId, remaining);
      if (remaining <= 0) {
        const depLane = taskLane.get(depId);
        if (depLane !== undefined && depLane !== lane) {
          freeLane(depLane);
        }
      }
    }

    // Free merge-from lanes (they converge into this task's lane)
    for (const ml of mergeFromLanes) {
      if (activeLaneSet.has(ml)) {
        freeLane(ml);
      }
    }

    // Snapshot active lanes at this row
    const activeLanes = Array.from(activeLaneSet).sort((a, b) => a - b);

    results.push({
      taskId: task.id,
      lane,
      activeLanes,
      isMerge,
      mergeFromLanes,
    });
  }

  return results;
}

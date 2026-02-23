/**
 * Lane Layout - Git-graph style lane rendering for task dependencies
 *
 * Pure functions for computing topological sort, lane assignments,
 * and merge point detection. No React/Ink dependency.
 */

import type { TaskDisplay } from './types';

/**
 * Maximum number of parallel lanes before we cap and compress.
 * Prevents excessively wide layouts (e.g., 10 independent roots all forking).
 * Lanes beyond this limit wrap back to the highest allowed lane.
 */
export const MAX_LANES = 8;

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

export type LanePrefixSegmentRole =
  | 'vertical'
  | 'branch'
  | 'last-branch'
  | 'merge-start'
  | 'merge-join'
  | 'connector'
  | 'empty';

export type LanePrefixSegmentKind = 'neutral' | 'upstream' | 'downstream';

export interface LanePrefixSegment {
  text: string;
  lane: number;
  role: LanePrefixSegmentRole;
  kind: LanePrefixSegmentKind;
}

export interface LanePrefixSegmentContext {
  upstreamLanes?: number[] | Set<number>;
  downstreamLanes?: number[] | Set<number>;
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
    // Guard: skip tasks with missing/malformed dependencies
    if (!Array.isArray(task.dependencies)) continue;
    for (const depId of task.dependencies) {
      if (!depId || !taskIds.has(depId)) continue; // skip out-of-tree or falsy refs
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
    // Guard: skip tasks with missing/malformed dependencies
    if (!Array.isArray(task.dependencies)) continue;
    const inTreeDeps = task.dependencies.filter((d) => d && taskIds.has(d));
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
    // Guard: handle missing/malformed dependents array
    const deps = Array.isArray(task.dependents) ? task.dependents : [];
    const inTreeDependents = deps.filter((d) => d && taskIds.has(d));
    remainingDependents.set(task.id, inTreeDependents.length);
  }

  function allocateLane(): number {
    if (freeLanes.length > 0) {
      // Sort to always pick lowest available
      freeLanes.sort((a, b) => a - b);
      return freeLanes.shift()!;
    }
    // Cap at MAX_LANES - 1 to prevent excessively wide layouts
    const lane = nextNewLane++;
    if (lane >= MAX_LANES) {
      return MAX_LANES - 1;
    }
    return lane;
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
    // Guard: handle missing/malformed dependencies
    const safeDeps = Array.isArray(task.dependencies) ? task.dependencies : [];
    const inTreeDeps = safeDeps.filter((d) => d && taskIds.has(d));
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

// ---------------------------------------------------------------------------
// generatePrefix
// ---------------------------------------------------------------------------

// Box-drawing characters for git-graph rendering
const CHAR = {
  VERTICAL: '│',
  BRANCH: '├─',
  LAST_BRANCH: '└─',
  MERGE_START: '╰─',
  MERGE_JOIN: '┴─',
  EMPTY: '  ',      // 2 spaces to match lane width
  CONNECTOR: '─',
} as const;

/**
 * Check whether a given lane continues below the current row.
 * A lane "continues" if any subsequent assignment has it in activeLanes
 * or is itself on that lane.
 */
function laneActiveBelow(
  lane: number,
  currentIndex: number,
  allAssignments: LaneAssignment[],
): boolean {
  for (let i = currentIndex + 1; i < allAssignments.length; i++) {
    const a = allAssignments[i];
    if (a.activeLanes.includes(lane) || a.lane === lane) return true;
  }
  return false;
}

/**
 * Generate the box-drawing prefix string for a single row in the git-graph.
 *
 * Converts a LaneAssignment into the visual prefix that precedes the task
 * marker (○) and task name. Pure string function, no React dependency.
 *
 * Examples:
 *   Normal child:     │ ├─
 *   Last child:       │ └─
 *   Merge (2 lanes):  ╰─┴─
 *   Merge (3 lanes):  ╰─┴─┴─
 *   Active lanes:     │ │ ├─  (lane 0 + lane 1 active, branch at lane 2)
 */
export function generatePrefix(
  assignment: LaneAssignment,
  index: number,
  allAssignments: LaneAssignment[],
  context?: LanePrefixSegmentContext,
): string {
  return generatePrefixSegments(assignment, index, allAssignments, context)
    .map((segment) => segment.text)
    .join('');
}

export function generatePrefixSegments(
  assignment: LaneAssignment,
  index: number,
  allAssignments: LaneAssignment[],
  context: LanePrefixSegmentContext = {},
): LanePrefixSegment[] {
  const { isMerge, mergeFromLanes } = assignment;

  if (isMerge && mergeFromLanes.length > 0) {
    return buildMergePrefixSegments(assignment, index, allAssignments, context);
  }

  return buildBranchPrefixSegments(assignment, index, allAssignments, context);
}

function hasLane(setOrArray: number[] | Set<number> | undefined, lane: number): boolean {
  if (!setOrArray) return false;
  if (setOrArray instanceof Set) return setOrArray.has(lane);
  return setOrArray.includes(lane);
}

function laneKind(lane: number, context: LanePrefixSegmentContext): LanePrefixSegmentKind {
  if (hasLane(context.upstreamLanes, lane)) return 'upstream';
  if (hasLane(context.downstreamLanes, lane)) return 'downstream';
  return 'neutral';
}

function createSegment(
  lane: number,
  role: LanePrefixSegmentRole,
  text: string,
  context: LanePrefixSegmentContext,
): LanePrefixSegment {
  return {
    text,
    lane,
    role,
    kind: laneKind(lane, context),
  };
}

/**
 * Build prefix for a non-merge row (root, single-dep, or fork child).
 */
function buildBranchPrefixSegments(
  assignment: LaneAssignment,
  index: number,
  allAssignments: LaneAssignment[],
  context: LanePrefixSegmentContext,
): LanePrefixSegment[] {
  const { lane, activeLanes } = assignment;

  // Determine the max lane we need to render up to (inclusive of this task's lane)
  const maxLane = lane;

  const parts: LanePrefixSegment[] = [];

  for (let l = 0; l < maxLane; l++) {
    if (activeLanes.includes(l)) {
      parts.push(createSegment(l, 'vertical', CHAR.VERTICAL + ' ', context));
    } else {
      parts.push(createSegment(l, 'empty', CHAR.EMPTY, context));
    }
  }

  // At the task's own lane, determine branch vs last-branch
  const continues = laneActiveBelow(lane, index, allAssignments);
  if (continues) {
    parts.push(createSegment(lane, 'branch', CHAR.BRANCH, context));
  } else {
    parts.push(createSegment(lane, 'last-branch', CHAR.LAST_BRANCH, context));
  }

  return parts;
}

/**
 * Build prefix for a merge row (task has 2+ in-tree dependencies converging).
 *
 * Merge rendering strategy:
 * - All lanes from the leftmost merge lane to the rightmost are joined
 * - The leftmost merge source starts with ╰─
 * - Each additional merge lane adds ┴─
 * - Non-merge lanes between them get ──
 * - The task's own lane ends the sequence
 */
function buildMergePrefixSegments(
  assignment: LaneAssignment,
  index: number,
  allAssignments: LaneAssignment[],
  context: LanePrefixSegmentContext,
): LanePrefixSegment[] {
  const { lane, activeLanes, mergeFromLanes } = assignment;

  // All lanes involved in the merge (the task's own lane + merge-from lanes)
  const allMergeLanes = [lane, ...mergeFromLanes].sort((a, b) => a - b);
  const minMergeLane = allMergeLanes[0];
  const maxMergeLane = allMergeLanes[allMergeLanes.length - 1];
  const mergeLaneSet = new Set(allMergeLanes);

  const parts: LanePrefixSegment[] = [];

  // Lanes before the merge region
  for (let l = 0; l < minMergeLane; l++) {
    if (activeLanes.includes(l)) {
      parts.push(createSegment(l, 'vertical', CHAR.VERTICAL + ' ', context));
    } else {
      parts.push(createSegment(l, 'empty', CHAR.EMPTY, context));
    }
  }

  // The merge region: from minMergeLane to maxMergeLane
  //
  // Strategy:
  // - The task's own lane (which is always the lowest merge lane from
  //   assignLanes) starts with ╰─ to indicate the merge origin
  // - Each additional merge lane adds ┴─
  // - Non-merge lanes between merge lanes get ──
  // - After the last merge lane, we're done (the task node marker follows)
  let started = false;
  for (let l = minMergeLane; l <= maxMergeLane; l++) {
    if (mergeLaneSet.has(l) || l === lane) {
      if (!started) {
        parts.push(createSegment(l, 'merge-start', CHAR.MERGE_START, context));
        started = true;
      } else {
        parts.push(createSegment(l, 'merge-join', CHAR.MERGE_JOIN, context));
      }
    } else {
      // Non-merge lane between merge lanes
      if (started) {
        parts.push(createSegment(l, 'connector', CHAR.CONNECTOR + CHAR.CONNECTOR, context));
      } else if (activeLanes.includes(l)) {
        parts.push(createSegment(l, 'vertical', CHAR.VERTICAL + ' ', context));
      } else {
        parts.push(createSegment(l, 'empty', CHAR.EMPTY, context));
      }
    }
  }

  // If the task's lane is beyond maxMergeLane (shouldn't happen with our
  // assignLanes logic since task gets the lowest lane, but handle defensively)
  if (lane > maxMergeLane) {
    for (let l = maxMergeLane + 1; l < lane; l++) {
      if (started) {
        parts.push(createSegment(l, 'connector', CHAR.CONNECTOR + CHAR.CONNECTOR, context));
      } else if (activeLanes.includes(l)) {
        parts.push(createSegment(l, 'vertical', CHAR.VERTICAL + ' ', context));
      } else {
        parts.push(createSegment(l, 'empty', CHAR.EMPTY, context));
      }
    }
    parts.push(createSegment(lane, 'merge-start', CHAR.MERGE_START, context));
  }

  return parts;
}

/**
 * Task Dependency Resolution
 *
 * Ported from do-work bash script jq logic.
 * Handles: dependency resolution, cycle detection, parent hierarchy, task classification.
 */

import type {
  Task,
  ResolvedTask,
  TaskClassification,
  DependencyResult,
} from "./types";

// =============================================================================
// Lookup Map Builders
// =============================================================================

export interface TaskLookupMaps {
  byId: Map<string, Task>;
  titleToId: Map<string, string>;
}

/**
 * Build lookup maps for fast dependency resolution
 */
export function buildLookupMaps(tasks: Task[]): TaskLookupMaps {
  const byId = new Map<string, Task>();
  const titleToId = new Map<string, string>();

  for (const task of tasks) {
    byId.set(task.id, task);
    titleToId.set(task.title, task.id);
  }

  return { byId, titleToId };
}

/**
 * Resolve a dependency reference to a task ID
 * Supports both ID and title references
 */
export function resolveDep(ref: string, maps: TaskLookupMaps): string | null {
  if (maps.byId.has(ref)) return ref;
  if (maps.titleToId.has(ref)) return maps.titleToId.get(ref)!;
  return null;
}

// =============================================================================
// Parent Hierarchy
// =============================================================================

/**
 * Get the chain of parent IDs for a task (for hierarchy checks)
 * Returns array from immediate parent to root ancestor
 */
export function getParentChain(
  taskId: string,
  maps: TaskLookupMaps,
  visited: Set<string> = new Set()
): string[] {
  if (visited.has(taskId)) return []; // Prevent infinite loop

  const task = maps.byId.get(taskId);
  if (!task?.parent_id) return [];

  visited.add(taskId);
  return [task.parent_id, ...getParentChain(task.parent_id, maps, visited)];
}

// =============================================================================
// Cycle Detection
// =============================================================================

/**
 * Build adjacency list from tasks (task -> its dependencies)
 */
export function buildAdjacencyList(
  tasks: Task[],
  maps: TaskLookupMaps
): Map<string, string[]> {
  const adj = new Map<string, string[]>();

  for (const task of tasks) {
    const resolvedDeps = (task.depends_on || [])
      .map((ref) => resolveDep(ref, maps))
      .filter((id): id is string => id !== null);
    adj.set(task.id, resolvedDeps);
  }

  return adj;
}

/**
 * Find all tasks that are part of a dependency cycle
 * Uses iterative BFS to detect if a task can reach itself
 */
export function findCycles(adjacency: Map<string, string[]>): Set<string> {
  const inCycle = new Set<string>();

  for (const start of adjacency.keys()) {
    const initialDeps = adjacency.get(start) || [];
    if (initialDeps.length === 0) continue;

    const frontier = [...initialDeps];
    const seen = new Set<string>();
    let iterations = 0;
    const maxIterations = 100; // Safety limit

    while (frontier.length > 0 && iterations < maxIterations) {
      iterations++;
      const current = frontier.shift()!;

      if (current === start) {
        inCycle.add(start);
        break;
      }

      if (seen.has(current)) continue;
      seen.add(current);

      const deps = adjacency.get(current) || [];
      frontier.push(...deps);
    }
  }

  return inCycle;
}

// =============================================================================
// Task Classification
// =============================================================================

/**
 * Classify a single task based on its dependencies and parent hierarchy
 */
export function classifyTask(
  task: Task,
  resolvedDeps: string[],
  parentChain: string[],
  effectiveStatus: Map<string, string>,
  inCycle: Set<string>
): {
  classification: TaskClassification;
  blockedBy: string[];
  waitingOn: string[];
  reason?: string;
} {
  // Check if task is in a cycle
  if (inCycle.has(task.id)) {
    return {
      classification: "blocked",
      blockedBy: [],
      waitingOn: [],
      reason: "circular_dependency",
    };
  }

  // Task not pending - skip classification
  if (task.status !== "pending") {
    return { classification: "not_pending", blockedBy: [], waitingOn: [] };
  }

  // Check parent hierarchy for blocked/cancelled parents
  const blockedParents = parentChain.filter((pid) => {
    const status = effectiveStatus.get(pid) || "unknown";
    return ["blocked", "cancelled"].includes(status) || inCycle.has(pid);
  });

  if (blockedParents.length > 0) {
    return {
      classification: "blocked_by_parent",
      blockedBy: blockedParents,
      waitingOn: [],
      reason: "parent_blocked",
    };
  }

  // Check if direct parent is not active/in_progress/completed
  if (task.parent_id) {
    const parentStatus = effectiveStatus.get(task.parent_id) || "unknown";
    if (!["active", "in_progress", "completed"].includes(parentStatus)) {
      return {
        classification: "waiting_on_parent",
        blockedBy: [],
        waitingOn: [task.parent_id],
      };
    }
  }

  // Check for blocked dependencies
  const blockedDeps = resolvedDeps.filter((depId) => {
    const status = effectiveStatus.get(depId) || "unknown";
    return ["blocked", "cancelled"].includes(status) || inCycle.has(depId);
  });

  if (blockedDeps.length > 0) {
    return {
      classification: "blocked",
      blockedBy: blockedDeps,
      waitingOn: [],
      reason: "dependency_blocked",
    };
  }

  // Check for waiting dependencies (pending or in_progress)
  const waitingDeps = resolvedDeps.filter((depId) => {
    const status = effectiveStatus.get(depId) || "unknown";
    return ["pending", "in_progress"].includes(status);
  });

  if (waitingDeps.length > 0) {
    return { classification: "waiting", blockedBy: [], waitingOn: waitingDeps };
  }

  // All dependencies satisfied
  return { classification: "ready", blockedBy: [], waitingOn: [] };
}

// =============================================================================
// Main Resolution Function
// =============================================================================

/**
 * Resolve all dependencies and classify all tasks
 * This is the main entry point - equivalent to resolve_dependencies() in bash
 */
export function resolveDependencies(tasks: Task[]): DependencyResult {
  if (tasks.length === 0) {
    return {
      tasks: [],
      cycles: [],
      stats: { total: 0, ready: 0, waiting: 0, blocked: 0, not_pending: 0 },
    };
  }

  // Step 1: Build lookup maps
  const maps = buildLookupMaps(tasks);

  // Step 2: Build adjacency list and detect cycles
  const adjacency = buildAdjacencyList(tasks, maps);
  const inCycle = findCycles(adjacency);

  // Step 3: Build effective status map (with cycle override)
  const effectiveStatus = new Map<string, string>();
  for (const task of tasks) {
    effectiveStatus.set(
      task.id,
      inCycle.has(task.id) ? "circular" : task.status
    );
  }

  // Step 4: Resolve and classify each task
  const resolvedTasks: ResolvedTask[] = tasks.map((task) => {
    // Resolve dependencies
    const resolvedDeps: string[] = [];
    const unresolvedDeps: string[] = [];

    for (const ref of task.depends_on || []) {
      const resolved = resolveDep(ref, maps);
      if (resolved) {
        resolvedDeps.push(resolved);
      } else {
        unresolvedDeps.push(ref);
      }
    }

    // Get parent chain
    const parentChain = getParentChain(task.id, maps);

    // Classify
    const { classification, blockedBy, waitingOn, reason } = classifyTask(
      task,
      resolvedDeps,
      parentChain,
      effectiveStatus,
      inCycle
    );

    return {
      ...task,
      resolved_deps: resolvedDeps,
      unresolved_deps: unresolvedDeps,
      parent_chain: parentChain,
      classification,
      blocked_by: blockedBy,
      blocked_by_reason: reason,
      waiting_on: waitingOn,
      in_cycle: inCycle.has(task.id),
      resolved_workdir: null, // Resolved separately by TaskService
    };
  });

  // Step 5: Compute stats
  const stats = {
    total: resolvedTasks.length,
    ready: resolvedTasks.filter((t) => t.classification === "ready").length,
    waiting: resolvedTasks.filter(
      (t) =>
        t.classification === "waiting" ||
        t.classification === "waiting_on_parent"
    ).length,
    blocked: resolvedTasks.filter(
      (t) =>
        t.classification === "blocked" ||
        t.classification === "blocked_by_parent"
    ).length,
    not_pending: resolvedTasks.filter((t) => t.classification === "not_pending")
      .length,
  };

  // Step 6: Extract cycle groups
  const cycles = inCycle.size > 0 ? [Array.from(inCycle)] : [];

  return { tasks: resolvedTasks, cycles, stats };
}

// =============================================================================
// Utility Functions
// =============================================================================

/**
 * Sort tasks by priority (high first, then medium, then low)
 */
export function sortByPriority(tasks: ResolvedTask[]): ResolvedTask[] {
  const priorityOrder: Record<string, number> = { high: 0, medium: 1, low: 2 };
  return [...tasks].sort((a, b) => {
    const aOrder = priorityOrder[a.priority] ?? 1;
    const bOrder = priorityOrder[b.priority] ?? 1;
    if (aOrder !== bOrder) return aOrder - bOrder;
    // Secondary sort by created date
    return (a.created || "").localeCompare(b.created || "");
  });
}

/**
 * Filter to ready tasks, sorted by priority
 */
export function getReadyTasks(result: DependencyResult): ResolvedTask[] {
  const ready = result.tasks.filter((t) => t.classification === "ready");
  return sortByPriority(ready);
}

/**
 * Filter to waiting tasks (including waiting_on_parent)
 */
export function getWaitingTasks(result: DependencyResult): ResolvedTask[] {
  return result.tasks.filter(
    (t) =>
      t.classification === "waiting" ||
      t.classification === "waiting_on_parent"
  );
}

/**
 * Filter to blocked tasks (including blocked_by_parent)
 */
export function getBlockedTasks(result: DependencyResult): ResolvedTask[] {
  return result.tasks.filter(
    (t) =>
      t.classification === "blocked" ||
      t.classification === "blocked_by_parent"
  );
}

/**
 * Get the next task to execute (highest priority ready task)
 */
export function getNextTask(result: DependencyResult): ResolvedTask | null {
  const ready = getReadyTasks(result);
  return ready.length > 0 ? ready[0] : null;
}

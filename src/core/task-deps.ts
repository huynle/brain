/**
 * Task Hierarchy Resolution
 *
 * Tasks use a parent_id field to form a tree hierarchy.
 * - Tasks point UP to their parent via parent_id
 * - Tasks without parent_id are root-level project deliverables
 * - Leaf tasks (no children) are ready to execute
 * - Parent tasks wait until all children complete
 */

import type {
  Task,
  ResolvedTask,
  TaskClassification,
  TaskResolutionResult,
} from "./types";

// =============================================================================
// Lookup Map Builders
// =============================================================================

export interface TaskLookupMaps {
  byId: Map<string, Task>;
  titleToId: Map<string, string>;
}

/**
 * Build lookup maps for fast task resolution
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

// =============================================================================
// Parent-Child Map Builder
// =============================================================================

/**
 * Build a map from parent_id to list of child tasks
 * Key null represents root-level tasks (no parent)
 */
export function buildParentChildMap(
  tasks: Task[]
): Map<string | null, Task[]> {
  const childrenMap = new Map<string | null, Task[]>();

  for (const task of tasks) {
    const parentId = task.parent_id || null;
    if (!childrenMap.has(parentId)) {
      childrenMap.set(parentId, []);
    }
    childrenMap.get(parentId)!.push(task);
  }

  return childrenMap;
}

/**
 * Get children of a specific task
 */
export function getChildren(taskId: string, tasks: Task[]): Task[] {
  return tasks.filter((t) => t.parent_id === taskId);
}

/**
 * Get the parent of a specific task
 */
export function getParent(
  taskId: string,
  maps: TaskLookupMaps
): Task | null {
  const task = maps.byId.get(taskId);
  if (!task?.parent_id) return null;
  return maps.byId.get(task.parent_id) || null;
}

// =============================================================================
// Task Classification
// =============================================================================

/**
 * Classify a single task based on its parent/child relationships
 *
 * Rules:
 * - Leaf tasks (no children) with status=pending are "ready"
 * - Parent tasks (have children) are "waiting" until all children complete
 * - Tasks with blocked/cancelled parent are "blocked"
 * - Non-pending tasks are "not_pending"
 */
export function classifyTask(
  task: Task,
  childrenIds: string[],
  maps: TaskLookupMaps
): {
  classification: TaskClassification;
  blockedBy?: string;
  reason?: string;
} {
  // Task not pending - skip classification
  if (task.status !== "pending") {
    return { classification: "not_pending" };
  }

  // Check if parent is blocked or cancelled
  if (task.parent_id) {
    const parent = maps.byId.get(task.parent_id);
    if (parent) {
      if (parent.status === "blocked") {
        return {
          classification: "blocked",
          blockedBy: task.parent_id,
          reason: "parent_blocked",
        };
      }
      if (parent.status === "cancelled") {
        return {
          classification: "blocked",
          blockedBy: task.parent_id,
          reason: "parent_cancelled",
        };
      }
    }
  }

  // Leaf task (no children) - ready to work on
  if (childrenIds.length === 0) {
    return { classification: "ready" };
  }

  // Has children - check if all are complete
  const allChildrenComplete = childrenIds.every((childId) => {
    const child = maps.byId.get(childId);
    if (!child) return true; // Treat missing children as complete
    return child.status === "completed" || child.status === "validated";
  });

  if (allChildrenComplete) {
    // All children done - parent is ready for review/completion
    return { classification: "ready" };
  }

  // Has incomplete children - waiting
  return { classification: "waiting" };
}

// =============================================================================
// Main Resolution Function
// =============================================================================

/**
 * Resolve all parent/child relationships and classify all tasks
 * This is the main entry point
 */
export function resolveTasks(tasks: Task[]): TaskResolutionResult {
  if (tasks.length === 0) {
    return {
      tasks: [],
      stats: { total: 0, ready: 0, waiting: 0, blocked: 0, not_pending: 0 },
    };
  }

  // Step 1: Build lookup maps
  const maps = buildLookupMaps(tasks);

  // Step 2: Build parent-child map
  const parentChildMap = buildParentChildMap(tasks);

  // Step 3: Resolve and classify each task
  const resolvedTasks: ResolvedTask[] = tasks.map((task) => {
    // Get children IDs
    const children = parentChildMap.get(task.id) || [];
    const childrenIds = children.map((c) => c.id);

    // Classify
    const { classification, blockedBy, reason } = classifyTask(
      task,
      childrenIds,
      maps
    );

    return {
      ...task,
      children_ids: childrenIds,
      classification,
      blocked_by: blockedBy,
      blocked_by_reason: reason,
      resolved_workdir: null, // Resolved separately by TaskService
    };
  });

  // Step 4: Compute stats
  const stats = {
    total: resolvedTasks.length,
    ready: resolvedTasks.filter((t) => t.classification === "ready").length,
    waiting: resolvedTasks.filter((t) => t.classification === "waiting").length,
    blocked: resolvedTasks.filter((t) => t.classification === "blocked").length,
    not_pending: resolvedTasks.filter((t) => t.classification === "not_pending")
      .length,
  };

  return { tasks: resolvedTasks, stats };
}

// Backwards compatibility alias
export const resolveDependencies = resolveTasks;

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
export function getReadyTasks(result: TaskResolutionResult): ResolvedTask[] {
  const ready = result.tasks.filter((t) => t.classification === "ready");
  return sortByPriority(ready);
}

/**
 * Filter to waiting tasks
 */
export function getWaitingTasks(result: TaskResolutionResult): ResolvedTask[] {
  return result.tasks.filter((t) => t.classification === "waiting");
}

/**
 * Filter to blocked tasks
 */
export function getBlockedTasks(result: TaskResolutionResult): ResolvedTask[] {
  return result.tasks.filter((t) => t.classification === "blocked");
}

/**
 * Get the next task to execute (highest priority ready task)
 */
export function getNextTask(result: TaskResolutionResult): ResolvedTask | null {
  const ready = getReadyTasks(result);
  return ready.length > 0 ? ready[0] : null;
}

/**
 * Build a tree structure from resolved tasks
 */
export interface TaskTreeNode {
  task: ResolvedTask;
  children: TaskTreeNode[];
}

export function buildTaskTree(tasks: ResolvedTask[]): TaskTreeNode[] {
  const taskMap = new Map(tasks.map((t) => [t.id, t]));
  const childrenMap = new Map<string | null, ResolvedTask[]>();

  // Group by parent_id
  for (const task of tasks) {
    const parentId = task.parent_id || null;
    if (!childrenMap.has(parentId)) {
      childrenMap.set(parentId, []);
    }
    childrenMap.get(parentId)!.push(task);
  }

  // Root tasks = those with no parent_id
  const rootTasks = childrenMap.get(null) || [];

  function buildNode(task: ResolvedTask): TaskTreeNode {
    const children = childrenMap.get(task.id) || [];
    return {
      task,
      children: children.map(buildNode),
    };
  }

  return rootTasks.map(buildNode);
}

/**
 * Flatten a task tree to a list with depth info
 */
export interface FlattenedTask {
  task: ResolvedTask;
  depth: number;
}

export function flattenTaskTree(tree: TaskTreeNode[]): FlattenedTask[] {
  const result: FlattenedTask[] = [];

  function visit(node: TaskTreeNode, depth: number) {
    result.push({ task: node.task, depth });
    for (const child of node.children) {
      visit(child, depth + 1);
    }
  }

  for (const root of tree) {
    visit(root, 0);
  }

  return result;
}

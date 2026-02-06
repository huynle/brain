/**
 * Feature Computation Service
 *
 * Aggregates tasks into computed features and resolves feature dependencies.
 * Follows patterns from task-deps.ts: pure functions, lookup maps, cycle detection.
 */

import type { ResolvedTask, Priority, TaskClassification } from "./types";

// =============================================================================
// Types
// =============================================================================

export type FeatureStatus = "pending" | "in_progress" | "completed" | "blocked";
export type FeatureClassification = "ready" | "waiting" | "blocked";

export interface FeatureTaskStats {
  total: number;
  pending: number;
  in_progress: number;
  completed: number;
  blocked: number;
}

export interface ComputedFeature {
  id: string;
  project: string;
  priority: Priority;
  depends_on_features: string[];
  tasks: ResolvedTask[];
  status: FeatureStatus;
  classification: FeatureClassification;
  task_stats: FeatureTaskStats;
  blocked_by_features: string[];
  waiting_on_features: string[];
  in_cycle: boolean;
}

export interface FeatureLookupMaps {
  byId: Map<string, ComputedFeature>;
}

export interface FeatureDependencyResult {
  features: ComputedFeature[];
  cycles: string[][];
  stats: {
    total: number;
    ready: number;
    waiting: number;
    blocked: number;
  };
}

// =============================================================================
// Feature Computation
// =============================================================================

/**
 * Group tasks by feature_id and compute initial feature metadata.
 * Tasks without feature_id are skipped.
 */
export function computeFeatures(tasks: ResolvedTask[]): ComputedFeature[] {
  const featureMap = new Map<string, ResolvedTask[]>();

  // Group tasks by feature_id
  for (const task of tasks) {
    if (!task.feature_id) continue;

    const existing = featureMap.get(task.feature_id) || [];
    existing.push(task);
    featureMap.set(task.feature_id, existing);
  }

  // Build computed features
  const features: ComputedFeature[] = [];

  for (const [featureId, featureTasks] of Array.from(featureMap.entries())) {
    // Compute task stats
    const taskStats = computeTaskStats(featureTasks);

    // Compute feature status from task statuses
    const status = computeFeatureStatus(featureTasks);

    // Get highest priority from any task in feature
    const priority = computeHighestPriority(featureTasks);

    // Collect feature dependencies (union of all task feature_depends_on)
    const dependsOnFeatures = collectFeatureDependencies(featureTasks);

    // Get project from first task (all tasks in a feature should have same project)
    const project = featureTasks[0]?.path.split("/")[1] || "default";

    features.push({
      id: featureId,
      project,
      priority,
      depends_on_features: dependsOnFeatures,
      tasks: featureTasks,
      status,
      classification: "waiting", // Will be resolved in resolveFeatureDependencies
      task_stats: taskStats,
      blocked_by_features: [],
      waiting_on_features: [],
      in_cycle: false,
    });
  }

  return features;
}

/**
 * Compute task statistics for a feature
 */
function computeTaskStats(tasks: ResolvedTask[]): FeatureTaskStats {
  const stats: FeatureTaskStats = {
    total: tasks.length,
    pending: 0,
    in_progress: 0,
    completed: 0,
    blocked: 0,
  };

  for (const task of tasks) {
    switch (task.status) {
      case "pending":
        stats.pending++;
        break;
      case "in_progress":
        stats.in_progress++;
        break;
      case "completed":
      case "validated":
        stats.completed++;
        break;
      case "blocked":
      case "cancelled":
        stats.blocked++;
        break;
    }
  }

  return stats;
}

/**
 * Compute feature status from constituent task statuses
 *
 * Rules:
 * - All completed -> completed
 * - Any blocked (and no in_progress) -> blocked
 * - Any in_progress -> in_progress
 * - Otherwise -> pending
 */
export function computeFeatureStatus(tasks: ResolvedTask[]): FeatureStatus {
  if (tasks.length === 0) return "pending";

  const stats = computeTaskStats(tasks);

  // All tasks completed
  if (stats.completed === stats.total) {
    return "completed";
  }

  // Any task in_progress
  if (stats.in_progress > 0) {
    return "in_progress";
  }

  // Any task blocked (and none in_progress)
  if (stats.blocked > 0 && stats.in_progress === 0) {
    return "blocked";
  }

  return "pending";
}

/**
 * Get the highest priority from any task in the feature
 */
function computeHighestPriority(tasks: ResolvedTask[]): Priority {
  const priorityOrder: Record<Priority, number> = { high: 0, medium: 1, low: 2 };
  let highest: Priority = "low";

  for (const task of tasks) {
    const taskPriority = task.feature_priority || task.priority;
    if (priorityOrder[taskPriority] < priorityOrder[highest]) {
      highest = taskPriority;
    }
  }

  return highest;
}

/**
 * Collect all unique feature dependencies from tasks
 */
function collectFeatureDependencies(tasks: ResolvedTask[]): string[] {
  const deps = new Set<string>();

  for (const task of tasks) {
    if (task.feature_depends_on) {
      for (const dep of task.feature_depends_on) {
        deps.add(dep);
      }
    }
  }

  return Array.from(deps);
}

// =============================================================================
// Feature Dependency Resolution
// =============================================================================

/**
 * Build lookup maps for fast feature resolution
 */
export function buildFeatureLookupMaps(
  features: ComputedFeature[]
): FeatureLookupMaps {
  const byId = new Map<string, ComputedFeature>();

  for (const feature of features) {
    byId.set(feature.id, feature);
  }

  return { byId };
}

/**
 * Build adjacency list from features (feature -> its dependencies)
 */
export function buildFeatureAdjacencyList(
  features: ComputedFeature[],
  maps: FeatureLookupMaps
): Map<string, string[]> {
  const adj = new Map<string, string[]>();

  for (const feature of features) {
    // Only include dependencies that actually exist
    const resolvedDeps = feature.depends_on_features.filter((depId) =>
      maps.byId.has(depId)
    );
    adj.set(feature.id, resolvedDeps);
  }

  return adj;
}

/**
 * Find all features that are part of a dependency cycle
 * Uses iterative BFS to detect if a feature can reach itself
 */
export function findFeatureCycles(
  adjacency: Map<string, string[]>
): Set<string> {
  const inCycle = new Set<string>();

  for (const start of Array.from(adjacency.keys())) {
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

/**
 * Classify a feature based on its dependencies
 */
function classifyFeature(
  feature: ComputedFeature,
  resolvedDeps: string[],
  effectiveStatus: Map<string, FeatureStatus>,
  inCycle: Set<string>
): {
  classification: FeatureClassification;
  blockedBy: string[];
  waitingOn: string[];
} {
  // Check if feature is in a cycle
  if (inCycle.has(feature.id)) {
    return {
      classification: "blocked",
      blockedBy: [],
      waitingOn: [],
    };
  }

  // Feature already completed - no classification needed
  if (feature.status === "completed") {
    return { classification: "ready", blockedBy: [], waitingOn: [] };
  }

  // Check for blocked dependencies
  const blockedDeps = resolvedDeps.filter((depId) => {
    const status = effectiveStatus.get(depId) || "pending";
    return status === "blocked" || inCycle.has(depId);
  });

  if (blockedDeps.length > 0) {
    return {
      classification: "blocked",
      blockedBy: blockedDeps,
      waitingOn: [],
    };
  }

  // Check for waiting dependencies (pending or in_progress)
  const waitingDeps = resolvedDeps.filter((depId) => {
    const status = effectiveStatus.get(depId) || "pending";
    return status === "pending" || status === "in_progress";
  });

  if (waitingDeps.length > 0) {
    return { classification: "waiting", blockedBy: [], waitingOn: waitingDeps };
  }

  // All dependencies satisfied
  return { classification: "ready", blockedBy: [], waitingOn: [] };
}

/**
 * Resolve all feature dependencies and classify all features
 */
export function resolveFeatureDependencies(
  features: ComputedFeature[]
): ComputedFeature[] {
  if (features.length === 0) return [];

  // Step 1: Build lookup maps
  const maps = buildFeatureLookupMaps(features);

  // Step 2: Build adjacency list and detect cycles
  const adjacency = buildFeatureAdjacencyList(features, maps);
  const inCycle = findFeatureCycles(adjacency);

  // Step 3: Build effective status map (with cycle override)
  const effectiveStatus = new Map<string, FeatureStatus>();
  for (const feature of features) {
    effectiveStatus.set(
      feature.id,
      inCycle.has(feature.id) ? "blocked" : feature.status
    );
  }

  // Step 4: Classify each feature
  return features.map((feature) => {
    // Get resolved dependencies (only those that exist)
    const resolvedDeps = feature.depends_on_features.filter((depId) =>
      maps.byId.has(depId)
    );

    const { classification, blockedBy, waitingOn } = classifyFeature(
      feature,
      resolvedDeps,
      effectiveStatus,
      inCycle
    );

    return {
      ...feature,
      classification,
      blocked_by_features: blockedBy,
      waiting_on_features: waitingOn,
      in_cycle: inCycle.has(feature.id),
    };
  });
}

// =============================================================================
// Utility Functions
// =============================================================================

/**
 * Sort features by priority (high first, then medium, then low)
 */
export function sortFeaturesByPriority(
  features: ComputedFeature[]
): ComputedFeature[] {
  const priorityOrder: Record<Priority, number> = { high: 0, medium: 1, low: 2 };
  return [...features].sort((a, b) => {
    const aOrder = priorityOrder[a.priority] ?? 1;
    const bOrder = priorityOrder[b.priority] ?? 1;
    return aOrder - bOrder;
  });
}

/**
 * Get features that are ready to execute (all dependencies satisfied)
 */
export function getReadyFeatures(
  features: ComputedFeature[]
): ComputedFeature[] {
  const ready = features.filter(
    (f) => f.classification === "ready" && f.status !== "completed"
  );
  return sortFeaturesByPriority(ready);
}

/**
 * Get features that are waiting on other features
 */
export function getWaitingFeatures(
  features: ComputedFeature[]
): ComputedFeature[] {
  return features.filter((f) => f.classification === "waiting");
}

/**
 * Get features that are blocked
 */
export function getBlockedFeatures(
  features: ComputedFeature[]
): ComputedFeature[] {
  return features.filter((f) => f.classification === "blocked");
}

/**
 * Main entry point: compute features from tasks and resolve dependencies
 */
export function computeAndResolveFeatures(
  tasks: ResolvedTask[]
): FeatureDependencyResult {
  // Compute initial features
  const features = computeFeatures(tasks);

  // Resolve dependencies
  const resolved = resolveFeatureDependencies(features);

  // Detect cycles for reporting
  const maps = buildFeatureLookupMaps(resolved);
  const adjacency = buildFeatureAdjacencyList(resolved, maps);
  const inCycle = findFeatureCycles(adjacency);
  const cycles = inCycle.size > 0 ? [Array.from(inCycle)] : [];

  // Compute stats
  const stats = {
    total: resolved.length,
    ready: resolved.filter((f) => f.classification === "ready").length,
    waiting: resolved.filter((f) => f.classification === "waiting").length,
    blocked: resolved.filter((f) => f.classification === "blocked").length,
  };

  return { features: resolved, cycles, stats };
}

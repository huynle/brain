/**
 * Hook for polling the Brain API for task updates
 */

import { useReducer, useEffect, useCallback, useRef } from 'react';
import type { TaskDisplay } from '../types';

/**
 * Simple hash function for change detection.
 * Uses FNV-1a algorithm for fast, reasonably collision-resistant hashing.
 */
function simpleHash(str: string): number {
  let hash = 2166136261; // FNV offset basis
  for (let i = 0; i < str.length; i++) {
    hash ^= str.charCodeAt(i);
    hash = (hash * 16777619) >>> 0; // FNV prime, keep as uint32
  }
  return hash;
}

/**
 * Stats for tasks returned from the API
 */
export interface TaskStats {
  total: number;
  ready: number;
  waiting: number;
  blocked: number;
  inProgress: number;
  completed: number;
}

export interface UseTaskPollerOptions {
  projectId: string;
  apiUrl: string;
  pollInterval?: number; // Default 2000ms
  enabled?: boolean; // Default true
}

export interface UseTaskPollerResult {
  tasks: TaskDisplay[];
  stats: TaskStats;
  isLoading: boolean;
  isConnected: boolean;
  error: Error | null;
  refetch: () => Promise<void>;
}

const DEFAULT_POLL_INTERVAL = 1000;

/**
 * Build a lookup map from task ID to task title
 */
function buildIdToTitleMap(tasks: any[]): Map<string, string> {
  const map = new Map<string, string>();
  for (const task of tasks) {
    if (task.id && task.title) {
      map.set(task.id, task.title);
    }
  }
  return map;
}

/**
 * Compute reverse dependencies (dependents) for each task.
 * Returns a map from task ID to array of dependent task IDs.
 */
function computeDependents(tasks: any[]): Map<string, string[]> {
  const dependentsMap = new Map<string, string[]>();
  
  // Initialize empty arrays for all tasks
  for (const task of tasks) {
    if (task.id) {
      dependentsMap.set(task.id, []);
    }
  }
  
  // For each task, add it as a dependent of its dependencies
  for (const task of tasks) {
    const deps = task.resolved_deps || task.dependencies || [];
    for (const depId of deps) {
      const existingDependents = dependentsMap.get(depId);
      if (existingDependents) {
        existingDependents.push(task.id);
      }
    }
  }
  
  return dependentsMap;
}

/**
 * Resolve an array of task IDs to their titles.
 * Falls back to ID if title not found.
 */
function resolveIdsToTitles(ids: string[] | undefined, idToTitle: Map<string, string>): string[] {
  if (!ids || ids.length === 0) return [];
  return ids.map(id => idToTitle.get(id) || id);
}

/**
 * Build a map from task ID to its direct dependency IDs.
 */
function buildDependencyMap(tasks: any[]): Map<string, string[]> {
  const map = new Map<string, string[]>();
  for (const task of tasks) {
    if (task.id) {
      const deps = task.resolved_deps || task.dependencies || [];
      map.set(task.id, deps);
    }
  }
  return map;
}

/**
 * Compute all transitive ancestors (dependencies) for a task.
 * Returns all task IDs that must complete before this task can run.
 * Uses BFS to traverse the dependency graph upward.
 * Handles cycles gracefully via visited set.
 */
function computeTransitiveAncestors(
  taskId: string,
  dependencyMap: Map<string, string[]>
): { directAncestors: string[]; indirectAncestors: string[] } {
  const directAncestors = dependencyMap.get(taskId) || [];
  const allAncestors = new Set<string>();
  const visited = new Set<string>();
  const queue = [...directAncestors];
  
  // Mark direct ancestors
  const directSet = new Set(directAncestors);
  
  while (queue.length > 0) {
    const currentId = queue.shift()!;
    if (visited.has(currentId)) continue;
    visited.add(currentId);
    allAncestors.add(currentId);
    
    // Add this task's dependencies to the queue
    const deps = dependencyMap.get(currentId) || [];
    for (const depId of deps) {
      if (!visited.has(depId)) {
        queue.push(depId);
      }
    }
  }
  
  // Separate direct from indirect ancestors
  const indirectAncestors = [...allAncestors].filter(id => !directSet.has(id));
  
  return {
    directAncestors,
    indirectAncestors,
  };
}

const EMPTY_STATS: TaskStats = {
  total: 0,
  ready: 0,
  waiting: 0,
  blocked: 0,
  inProgress: 0,
  completed: 0,
};

/**
 * Consolidated state for the task poller.
 * Using a single reducer instead of multiple useState calls ensures
 * exactly ONE re-render per poll cycle instead of 4-5.
 */
export interface PollerState {
  tasks: TaskDisplay[];
  stats: TaskStats;
  isLoading: boolean;
  isConnected: boolean;
  error: Error | null;
}

export type PollerAction =
  | { type: 'FETCH_START' }
  | { type: 'FETCH_SUCCESS'; tasks: TaskDisplay[]; stats: TaskStats }
  | { type: 'FETCH_ERROR'; error: Error };

export function pollerReducer(state: PollerState, action: PollerAction): PollerState {
  switch (action.type) {
    case 'FETCH_START':
      return { ...state, isLoading: true };
    case 'FETCH_SUCCESS':
      return {
        tasks: action.tasks,
        stats: action.stats,
        isLoading: false,
        isConnected: true,
        error: null,
      };
    case 'FETCH_ERROR':
      return {
        ...state,
        isLoading: false,
        isConnected: false,
        error: action.error,
      };
  }
}

const INITIAL_POLLER_STATE: PollerState = {
  tasks: [],
  stats: EMPTY_STATS,
  isLoading: true,
  isConnected: false,
  error: null,
};

/**
 * Poll the Brain API for tasks at a regular interval
 */
export function useTaskPoller(options: UseTaskPollerOptions): UseTaskPollerResult {
  const {
    projectId,
    apiUrl,
    pollInterval = DEFAULT_POLL_INTERVAL,
    enabled = true,
  } = options;

  const [state, dispatch] = useReducer(pollerReducer, INITIAL_POLLER_STATE);

  // Track if this is the initial fetch vs subsequent polls
  const isInitialFetchRef = useRef(true);
  
  // Track previous data hash to avoid unnecessary re-renders
  const prevDataHashRef = useRef<number | null>(null);

  const fetchTasks = useCallback(async () => {
    // Only set loading on initial fetch
    if (isInitialFetchRef.current) {
      dispatch({ type: 'FETCH_START' });
    }

    try {
      const response = await fetch(
        `${apiUrl}/api/v1/tasks/${encodeURIComponent(projectId)}`
      );

      if (!response.ok) {
        throw new Error(`API error: ${response.status} ${response.statusText}`);
      }

      const data = await response.json();
      const rawTasks = data.tasks || [];

      // Build lookup map and compute reverse dependencies
      const idToTitle = buildIdToTitleMap(rawTasks);
      const dependentsMap = computeDependents(rawTasks);
      const dependencyMap = buildDependencyMap(rawTasks);

      // Transform API response to TaskDisplay format
      // Include all frontmatter fields from the brain entry
      // Resolve IDs to titles for better readability
      const taskDisplays: TaskDisplay[] = rawTasks.map((task: any) => {
        // Keep dependencies as IDs for tree building
        const depIds = task.resolved_deps || task.dependencies || [];
        const dependentIds = dependentsMap.get(task.id) || [];
        
        // Compute transitive ancestors (all tasks that must complete before this one)
        const { indirectAncestors } = computeTransitiveAncestors(task.id, dependencyMap);
        
        return {
          id: task.id,
          path: task.path,
          title: task.title,
          status: task.status,
          priority: task.priority || 'medium',
          // Keep IDs for tree building (TaskTree.tsx needs these)
          dependencies: depIds,
          dependents: dependentIds,
          // Resolve to titles for display (TaskDetail.tsx uses these)
          dependencyTitles: resolveIdsToTitles(depIds, idToTitle),
          dependentTitles: resolveIdsToTitles(dependentIds, idToTitle),
          // Transitive (indirect) ancestors for full dependency chain display
          indirectAncestorTitles: resolveIdsToTitles(indirectAncestors, idToTitle),
          progress: task.progress,
          error: task.error,
          parent_id: task.parent_id,
          // Frontmatter fields
          created: task.created,
          modified: task.modified,
          workdir: task.workdir,
          gitRemote: task.git_remote,
          gitBranch: task.git_branch,
          userOriginalRequest: task.user_original_request,
          // Dependency resolution fields - resolve to titles for display
          resolvedDeps: resolveIdsToTitles(task.resolved_deps, idToTitle),
          unresolvedDeps: task.unresolved_deps,
          classification: task.classification,
          blockedBy: resolveIdsToTitles(task.blocked_by, idToTitle),
          blockedByReason: task.blocked_by_reason,
          waitingOn: resolveIdsToTitles(task.waiting_on, idToTitle),
          inCycle: task.in_cycle,
          resolvedWorkdir: task.resolved_workdir,
          // Feature grouping fields
          feature_id: task.feature_id,
          feature_priority: task.feature_priority,
          feature_depends_on: task.feature_depends_on,
        };
      });

      // Calculate stats from tasks
      // API may provide base stats, we enhance with calculated fields
      const apiStats = data.stats || {};
      const calculatedStats: TaskStats = {
        total: apiStats.total ?? taskDisplays.length,
        ready: apiStats.ready ?? taskDisplays.filter((t) => t.status === 'pending').length,
        waiting: apiStats.waiting ?? 0,
        blocked: apiStats.blocked ?? taskDisplays.filter((t) => t.status === 'blocked').length,
        inProgress: taskDisplays.filter((t) => t.status === 'in_progress').length,
        completed: taskDisplays.filter((t) => t.status === 'completed' || t.status === 'validated').length,
      };

      // Compute hash of the data to detect changes
      // Only include fields that affect display to avoid spurious updates
      const dataForHash = taskDisplays.map(t => ({
        id: t.id,
        title: t.title,
        status: t.status,
        priority: t.priority,
        dependencies: t.dependencies,
        classification: t.classification,
        inCycle: t.inCycle,
      }));
      const newHash = simpleHash(JSON.stringify({ tasks: dataForHash, stats: calculatedStats }));
      
      // Only dispatch if data actually changed (prevents flickering)
      if (newHash !== prevDataHashRef.current) {
        prevDataHashRef.current = newHash;
        dispatch({ type: 'FETCH_SUCCESS', tasks: taskDisplays, stats: calculatedStats });
      }
    } catch (err) {
      const errorObj = err instanceof Error ? err : new Error('Unknown error');
      // Single dispatch replaces 2 separate setState calls
      // Don't clear tasks/stats on error - reducer preserves stale data via ...state
      dispatch({ type: 'FETCH_ERROR', error: errorObj });
    } finally {
      isInitialFetchRef.current = false;
    }
  }, [apiUrl, projectId]);

  // Manual refetch function
  const refetch = useCallback(async () => {
    await fetchTasks();
  }, [fetchTasks]);

  // Initial fetch and polling interval
  useEffect(() => {
    if (!enabled) {
      return;
    }

    // Reset initial fetch flag when dependencies change
    isInitialFetchRef.current = true;

    // Initial fetch
    fetchTasks();

    // Set up polling interval
    const intervalId = setInterval(fetchTasks, pollInterval);

    return () => {
      clearInterval(intervalId);
    };
  }, [fetchTasks, pollInterval, enabled]);

  return {
    tasks: state.tasks,
    stats: state.stats,
    isLoading: state.isLoading,
    isConnected: state.isConnected,
    error: state.error,
    refetch,
  };
}

export default useTaskPoller;

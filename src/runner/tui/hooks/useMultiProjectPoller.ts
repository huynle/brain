/**
 * Hook for polling multiple projects from the Brain API in parallel
 * 
 * This extends useTaskPoller to support multi-project mode where "all" was specified.
 * It fetches tasks from all projects concurrently and merges them with projectId tags.
 */

import { useReducer, useEffect, useCallback, useRef, useMemo } from 'react';
import type { TaskDisplay } from '../types';
import type { TaskStats } from './useTaskPoller';

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
 * Create a hashable representation of the multi-project state.
 * Only includes fields that affect display to avoid spurious updates.
 */
function createStateHash(
  tasksByProject: Map<string, TaskDisplay[]>,
  statsByProject: Map<string, TaskStats>,
  connectionByProject: Map<string, boolean>,
  errorsByProject: Map<string, Error>
): number {
  const tasksData: Record<string, Array<{ id: string; status: string; priority: string; classification?: string }>> = {};
  for (const [projectId, tasks] of tasksByProject) {
    tasksData[projectId] = tasks.map(t => ({
      id: t.id,
      status: t.status,
      priority: t.priority,
      classification: t.classification,
    }));
  }
  
  const statsData: Record<string, TaskStats> = {};
  for (const [projectId, stats] of statsByProject) {
    statsData[projectId] = stats;
  }
  
  const connectionData: Record<string, boolean> = {};
  for (const [projectId, connected] of connectionByProject) {
    connectionData[projectId] = connected;
  }
  
  const errorData: Record<string, string> = {};
  for (const [projectId, error] of errorsByProject) {
    errorData[projectId] = error.message;
  }
  
  return simpleHash(JSON.stringify({ tasks: tasksData, stats: statsData, conn: connectionData, err: errorData }));
}

/**
 * Options for multi-project polling
 */
export interface UseMultiProjectPollerOptions {
  projects: string[];
  apiUrl: string;
  pollInterval?: number; // Default 2000ms
  enabled?: boolean; // Default true
}

/**
 * Result from multi-project polling
 */
export interface MultiProjectPollerResult {
  /** Tasks grouped by project ID */
  tasksByProject: Map<string, TaskDisplay[]>;
  /** Stats grouped by project ID */
  statsByProject: Map<string, TaskStats>;
  /** Combined stats across all projects */
  aggregateStats: TaskStats;
  /** All tasks from all projects (with projectId attached) */
  allTasks: TaskDisplay[];
  /** Loading state for initial fetch */
  isLoading: boolean;
  /** Whether at least one project is connected */
  isConnected: boolean;
  /** Error from the most recent failed fetch (null if all succeeded) */
  error: Error | null;
  /** Manual refetch function */
  refetch: () => Promise<void>;
  /** Map of project ID to connection status */
  connectionByProject: Map<string, boolean>;
  /** Map of project ID to error (if any) */
  errorsByProject: Map<string, Error>;
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

const EMPTY_STATS: TaskStats = {
  total: 0,
  ready: 0,
  waiting: 0,
  blocked: 0,
  inProgress: 0,
  completed: 0,
};

/**
 * Fetch tasks for a single project
 */
async function fetchProjectTasks(
  projectId: string,
  apiUrl: string
): Promise<{ tasks: TaskDisplay[]; stats: TaskStats }> {
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

  // Transform API response to TaskDisplay format with projectId attached
  // Keep IDs for tree building, resolve to titles for display
  const tasks: TaskDisplay[] = rawTasks.map((task: any) => {
    // Keep dependencies as IDs for tree building
    const depIds = task.resolved_deps || task.dependencies || [];
    const dependentIds = dependentsMap.get(task.id) || [];
    
    return {
      id: task.id,
      path: task.path,
      title: task.title,
      status: task.status,
      priority: task.priority || 'medium',
      tags: task.tags || [],
      // Keep IDs for tree building (TaskTree.tsx needs these)
      dependencies: depIds,
      dependents: dependentIds,
      // Resolve to titles for display (TaskDetail.tsx uses these)
      dependencyTitles: resolveIdsToTitles(depIds, idToTitle),
      dependentTitles: resolveIdsToTitles(dependentIds, idToTitle),
      progress: task.progress,
      error: task.error,
      projectId, // Tag with project ID
      parent_id: task.parent_id,
      // Frontmatter fields
      created: task.created,
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
      // Execution override fields
      agent: task.agent,
      model: task.model,
      direct_prompt: task.direct_prompt,
    };
  });

  // Calculate stats from tasks
  const apiStats = data.stats || {};
  const stats: TaskStats = {
    total: apiStats.total ?? tasks.length,
    ready: apiStats.ready ?? tasks.filter((t) => t.status === 'pending').length,
    waiting: apiStats.waiting ?? 0,
    blocked: apiStats.blocked ?? tasks.filter((t) => t.status === 'blocked').length,
    inProgress: tasks.filter((t) => t.status === 'in_progress').length,
    completed: tasks.filter((t) => t.status === 'completed' || t.status === 'validated').length,
  };

  return { tasks, stats };
}

/**
 * Aggregate stats from multiple projects
 */
export function aggregateProjectStats(statsByProject: Map<string, TaskStats>): TaskStats {
  const aggregate: TaskStats = { ...EMPTY_STATS };
  
  for (const stats of statsByProject.values()) {
    aggregate.total += stats.total;
    aggregate.ready += stats.ready;
    aggregate.waiting += stats.waiting;
    aggregate.blocked += stats.blocked;
    aggregate.inProgress += stats.inProgress;
    aggregate.completed += stats.completed;
  }

  return aggregate;
}

/**
 * Merge all tasks from all projects into a single flat array.
 * Extracted as a named function for use with useMemo.
 */
export function mergeAllTasks(tasksByProject: Map<string, TaskDisplay[]>): TaskDisplay[] {
  return Array.from(tasksByProject.values()).flat();
}

/**
 * Check if at least one project is connected.
 * Extracted as a named function for use with useMemo.
 */
export function checkAnyConnected(connectionByProject: Map<string, boolean>): boolean {
  return Array.from(connectionByProject.values()).some(Boolean);
}

/**
 * Get the first error from the errors map, or null if none.
 * Extracted as a named function for use with useMemo.
 */
export function getFirstError(errorsByProject: Map<string, Error>): Error | null {
  return Array.from(errorsByProject.values())[0] ?? null;
}

/**
 * Consolidated state for the multi-project poller.
 * Using a single reducer instead of multiple useState calls ensures
 * exactly ONE re-render per poll cycle instead of 5.
 */
export interface MultiProjectPollerState {
  tasksByProject: Map<string, TaskDisplay[]>;
  statsByProject: Map<string, TaskStats>;
  connectionByProject: Map<string, boolean>;
  errorsByProject: Map<string, Error>;
  isLoading: boolean;
}

export type MultiProjectPollerAction =
  | { type: 'FETCH_START' }
  | {
      type: 'FETCH_SUCCESS';
      tasksByProject: Map<string, TaskDisplay[]>;
      statsByProject: Map<string, TaskStats>;
      connectionByProject: Map<string, boolean>;
      errorsByProject: Map<string, Error>;
    };

export function multiProjectReducer(
  state: MultiProjectPollerState,
  action: MultiProjectPollerAction
): MultiProjectPollerState {
  switch (action.type) {
    case 'FETCH_START':
      return { ...state, isLoading: true };
    case 'FETCH_SUCCESS':
      return {
        tasksByProject: action.tasksByProject,
        statsByProject: action.statsByProject,
        connectionByProject: action.connectionByProject,
        errorsByProject: action.errorsByProject,
        isLoading: false,
      };
  }
}

const INITIAL_MULTI_PROJECT_STATE: MultiProjectPollerState = {
  tasksByProject: new Map(),
  statsByProject: new Map(),
  connectionByProject: new Map(),
  errorsByProject: new Map(),
  isLoading: true,
};

/**
 * Poll multiple projects in parallel for task updates
 */
export function useMultiProjectPoller(
  options: UseMultiProjectPollerOptions
): MultiProjectPollerResult {
  const {
    projects,
    apiUrl,
    pollInterval = DEFAULT_POLL_INTERVAL,
    enabled = true,
  } = options;

  // Consolidated state via reducer — single dispatch per poll cycle
  const [state, dispatch] = useReducer(multiProjectReducer, INITIAL_MULTI_PROJECT_STATE);

  // Track if this is the initial fetch
  const isInitialFetchRef = useRef(true);
  
  // Stable key for projects array to avoid effect re-running on array reference change
  const projectsKey = projects.join(',');

  // Derived state — memoized to avoid new references on every render
  const aggregateStats = useMemo(
    () => aggregateProjectStats(state.statsByProject),
    [state.statsByProject]
  );
  const allTasks = useMemo(
    () => mergeAllTasks(state.tasksByProject),
    [state.tasksByProject]
  );
  const isConnected = useMemo(
    () => checkAnyConnected(state.connectionByProject),
    [state.connectionByProject]
  );
  const error = useMemo(
    () => getFirstError(state.errorsByProject),
    [state.errorsByProject]
  );

  // Refs to hold current state for use in callback without re-creating it
  const tasksByProjectRef = useRef(state.tasksByProject);
  const statsByProjectRef = useRef(state.statsByProject);
  tasksByProjectRef.current = state.tasksByProject;
  statsByProjectRef.current = state.statsByProject;
  
  // Track previous data hash to avoid unnecessary re-renders
  const prevDataHashRef = useRef<number | null>(null);

  // Fetch all projects in parallel
  const fetchAllProjects = useCallback(async () => {
    if (projects.length === 0) {
      return;
    }

    // Only set loading on initial fetch
    if (isInitialFetchRef.current) {
      dispatch({ type: 'FETCH_START' });
    }

    // Fetch all projects in parallel
    const results = await Promise.allSettled(
      projects.map(async (projectId) => {
        const result = await fetchProjectTasks(projectId, apiUrl);
        return { projectId, ...result };
      })
    );

    // Process results
    const newTasksByProject = new Map<string, TaskDisplay[]>();
    const newStatsByProject = new Map<string, TaskStats>();
    const newConnectionByProject = new Map<string, boolean>();
    const newErrorsByProject = new Map<string, Error>();

    for (let i = 0; i < results.length; i++) {
      const projectId = projects[i];
      const result = results[i];

      if (result.status === 'fulfilled') {
        newTasksByProject.set(projectId, result.value.tasks);
        newStatsByProject.set(projectId, result.value.stats);
        newConnectionByProject.set(projectId, true);
      } else {
        // Keep old data on error (use refs to avoid dependency)
        const existingTasks = tasksByProjectRef.current.get(projectId);
        const existingStats = statsByProjectRef.current.get(projectId);
        
        if (existingTasks) {
          newTasksByProject.set(projectId, existingTasks);
        }
        if (existingStats) {
          newStatsByProject.set(projectId, existingStats);
        }
        
        newConnectionByProject.set(projectId, false);
        newErrorsByProject.set(projectId, result.reason);
      }
    }

    // Compute hash of the data to detect changes
    const newHash = createStateHash(
      newTasksByProject,
      newStatsByProject,
      newConnectionByProject,
      newErrorsByProject
    );
    
    // Only dispatch if data actually changed (prevents flickering)
    if (newHash !== prevDataHashRef.current) {
      prevDataHashRef.current = newHash;
      dispatch({
        type: 'FETCH_SUCCESS',
        tasksByProject: newTasksByProject,
        statsByProject: newStatsByProject,
        connectionByProject: newConnectionByProject,
        errorsByProject: newErrorsByProject,
      });
    }
    isInitialFetchRef.current = false;
  }, [apiUrl, projectsKey]); // Use projectsKey for stable comparison; removed tasksByProject, statsByProject - using refs

  // Manual refetch function
  const refetch = useCallback(async () => {
    await fetchAllProjects();
  }, [fetchAllProjects]);

  // Initial fetch and polling interval
  useEffect(() => {
    if (!enabled || projects.length === 0) {
      return;
    }

    // Reset initial fetch flag when projects change
    isInitialFetchRef.current = true;

    // Initial fetch
    fetchAllProjects();

    // Set up polling interval
    const intervalId = setInterval(fetchAllProjects, pollInterval);

    return () => {
      clearInterval(intervalId);
    };
    // Use projectsKey instead of projects array to avoid reference comparison issues
  }, [fetchAllProjects, pollInterval, enabled, projectsKey]);

  return {
    tasksByProject: state.tasksByProject,
    statsByProject: state.statsByProject,
    aggregateStats,
    allTasks,
    isLoading: state.isLoading,
    isConnected,
    error,
    refetch,
    connectionByProject: state.connectionByProject,
    errorsByProject: state.errorsByProject,
  };
}

export default useMultiProjectPoller;

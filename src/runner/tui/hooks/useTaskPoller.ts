/**
 * Hook for polling the Brain API for task updates
 */

import { useReducer, useEffect, useCallback, useRef } from 'react';
import type { TaskDisplay } from '../types';

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

const DEFAULT_POLL_INTERVAL = 2000;

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

      // Transform API response to TaskDisplay format
      // Include all frontmatter fields from the brain entry
      const taskDisplays: TaskDisplay[] = (data.tasks || []).map((task: any) => ({
        id: task.id,
        path: task.path,
        title: task.title,
        status: task.status,
        priority: task.priority || 'medium',
        dependencies: task.resolved_deps || task.dependencies || [],
        dependents: task.dependents || [],
        progress: task.progress,
        error: task.error,
        parent_id: task.parent_id,
        // Frontmatter fields
        created: task.created,
        workdir: task.workdir,
        worktree: task.worktree,
        gitRemote: task.git_remote,
        gitBranch: task.git_branch,
        userOriginalRequest: task.user_original_request,
        // Dependency resolution fields
        resolvedDeps: task.resolved_deps,
        unresolvedDeps: task.unresolved_deps,
        classification: task.classification,
        blockedBy: task.blocked_by,
        blockedByReason: task.blocked_by_reason,
        waitingOn: task.waiting_on,
        inCycle: task.in_cycle,
        resolvedWorkdir: task.resolved_workdir,
      }));

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

      // Single dispatch replaces 4 separate setState calls
      dispatch({ type: 'FETCH_SUCCESS', tasks: taskDisplays, stats: calculatedStats });
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

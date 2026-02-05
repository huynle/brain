/**
 * Hook for polling the Brain API for task updates
 */

import { useState, useEffect, useCallback, useRef } from 'react';
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
 * Poll the Brain API for tasks at a regular interval
 */
export function useTaskPoller(options: UseTaskPollerOptions): UseTaskPollerResult {
  const {
    projectId,
    apiUrl,
    pollInterval = DEFAULT_POLL_INTERVAL,
    enabled = true,
  } = options;

  const [tasks, setTasks] = useState<TaskDisplay[]>([]);
  const [stats, setStats] = useState<TaskStats>(EMPTY_STATS);
  const [isLoading, setIsLoading] = useState(true);
  const [isConnected, setIsConnected] = useState(false);
  const [error, setError] = useState<Error | null>(null);

  // Track if this is the initial fetch vs subsequent polls
  const isInitialFetchRef = useRef(true);

  const fetchTasks = useCallback(async () => {
    // Only set loading on initial fetch
    if (isInitialFetchRef.current) {
      setIsLoading(true);
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

      setTasks(taskDisplays);
      setStats(calculatedStats);
      setIsConnected(true);
      setError(null);
    } catch (err) {
      const errorObj = err instanceof Error ? err : new Error('Unknown error');
      setError(errorObj);
      setIsConnected(false);
      // Don't clear tasks/stats on error - keep showing stale data
    } finally {
      setIsLoading(false);
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
    tasks,
    stats,
    isLoading,
    isConnected,
    error,
    refetch,
  };
}

export default useTaskPoller;

/**
 * Hook for polling multiple projects from the Brain API in parallel
 * 
 * This extends useTaskPoller to support multi-project mode where "all" was specified.
 * It fetches tasks from all projects concurrently and merges them with projectId tags.
 */

import { useState, useEffect, useCallback, useRef } from 'react';
import type { TaskDisplay } from '../types';
import type { TaskStats } from './useTaskPoller';

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

  // Transform API response to TaskDisplay format with projectId attached
  const tasks: TaskDisplay[] = (data.tasks || []).map((task: any) => ({
    id: task.id,
    path: task.path,
    title: task.title,
    status: task.status,
    priority: task.priority || 'medium',
    dependencies: task.resolved_deps || task.dependencies || [],
    dependents: task.dependents || [],
    progress: task.progress,
    error: task.error,
    projectId, // Tag with project ID
  }));

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
function aggregateProjectStats(statsByProject: Map<string, TaskStats>): TaskStats {
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

  // State
  const [tasksByProject, setTasksByProject] = useState<Map<string, TaskDisplay[]>>(new Map());
  const [statsByProject, setStatsByProject] = useState<Map<string, TaskStats>>(new Map());
  const [connectionByProject, setConnectionByProject] = useState<Map<string, boolean>>(new Map());
  const [errorsByProject, setErrorsByProject] = useState<Map<string, Error>>(new Map());
  const [isLoading, setIsLoading] = useState(true);

  // Track if this is the initial fetch
  const isInitialFetchRef = useRef(true);
  
  // Stable key for projects array to avoid effect re-running on array reference change
  const projectsKey = projects.join(',');

  // Derived state
  const aggregateStats = aggregateProjectStats(statsByProject);
  const allTasks: TaskDisplay[] = Array.from(tasksByProject.values()).flat();
  const isConnected = Array.from(connectionByProject.values()).some(Boolean);
  const error = Array.from(errorsByProject.values())[0] ?? null;

  // Refs to hold current state for use in callback without re-creating it
  const tasksByProjectRef = useRef(tasksByProject);
  const statsByProjectRef = useRef(statsByProject);
  tasksByProjectRef.current = tasksByProject;
  statsByProjectRef.current = statsByProject;

  // Fetch all projects in parallel
  const fetchAllProjects = useCallback(async () => {
    if (projects.length === 0) {
      return;
    }

    // Only set loading on initial fetch
    if (isInitialFetchRef.current) {
      setIsLoading(true);
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

    setTasksByProject(newTasksByProject);
    setStatsByProject(newStatsByProject);
    setConnectionByProject(newConnectionByProject);
    setErrorsByProject(newErrorsByProject);
    setIsLoading(false);
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
    tasksByProject,
    statsByProject,
    aggregateStats,
    allTasks,
    isLoading,
    isConnected,
    error,
    refetch,
    connectionByProject,
    errorsByProject,
  };
}

export default useMultiProjectPoller;

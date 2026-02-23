/**
 * Shared utilities for multi-project hooks (SSE and polling)
 * Extracted from deleted useMultiProjectPoller to avoid duplication.
 */

import type { TaskDisplay } from '../types';
import type { TaskStats } from './taskTypes';

export interface UseMultiProjectOptions {
  projects: string[];
  apiUrl: string;
  pollInterval?: number;
  enabled?: boolean;
}

export interface MultiProjectResult {
  tasksByProject: Map<string, TaskDisplay[]>;
  statsByProject: Map<string, TaskStats>;
  aggregateStats: TaskStats;
  allTasks: TaskDisplay[];
  isLoading: boolean;
  isConnected: boolean;
  error: Error | null;
  refetch: () => Promise<void>;
  connectionByProject: Map<string, boolean>;
  errorsByProject: Map<string, Error>;
}

const EMPTY_STATS: TaskStats = {
  total: 0,
  ready: 0,
  waiting: 0,
  blocked: 0,
  inProgress: 0,
  completed: 0,
};

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

export function mergeAllTasks(tasksByProject: Map<string, TaskDisplay[]>): TaskDisplay[] {
  const allTasks: TaskDisplay[] = [];
  for (const tasks of tasksByProject.values()) {
    allTasks.push(...tasks);
  }
  return allTasks;
}

export function checkAnyConnected(connectionByProject: Map<string, boolean>): boolean {
  for (const connected of connectionByProject.values()) {
    if (connected) {
      return true;
    }
  }
  return false;
}

export function getFirstError(errorsByProject: Map<string, Error>): Error | null {
  for (const error of errorsByProject.values()) {
    return error;
  }
  return null;
}

/**
 * Shared types for task hooks (SSE and polling)
 * Extracted to avoid circular dependencies after removing polling hooks.
 */

import type { TaskDisplay } from '../types';

export interface TaskStats {
  total: number;
  ready: number;
  waiting: number;
  blocked: number;
  inProgress: number;
  completed: number;
}

export interface UseTaskResult {
  tasks: TaskDisplay[];
  stats: TaskStats;
  isLoading: boolean;
  isConnected: boolean;
  error: Error | null;
  refetch: () => Promise<void>;
}

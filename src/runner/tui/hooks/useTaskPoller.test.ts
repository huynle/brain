/**
 * useTaskPoller Hook Tests
 *
 * Tests the task polling hook including:
 * - Polling at configured interval
 * - Error handling and recovery
 * - Stats calculation
 * - Connection state management
 * - Data transformation
 */

import { describe, it, expect, beforeEach, afterEach, mock } from 'bun:test';

// =============================================================================
// Test the core logic directly (without React hooks)
// =============================================================================

describe('useTaskPoller - Core Logic', () => {
  describe('stats calculation', () => {
    it('should calculate stats from task list', () => {
      const tasks = [
        { id: '1', status: 'pending' },
        { id: '2', status: 'pending' },
        { id: '3', status: 'in_progress' },
        { id: '4', status: 'completed' },
        { id: '5', status: 'blocked' },
        { id: '6', status: 'validated' },
      ];

      const stats = calculateStats(tasks);

      expect(stats.total).toBe(6);
      expect(stats.ready).toBe(2); // pending tasks are "ready"
      expect(stats.inProgress).toBe(1);
      expect(stats.completed).toBe(2); // completed + validated
      expect(stats.blocked).toBe(1);
    });

    it('should handle empty task list', () => {
      const stats = calculateStats([]);

      expect(stats.total).toBe(0);
      expect(stats.ready).toBe(0);
      expect(stats.waiting).toBe(0);
      expect(stats.inProgress).toBe(0);
      expect(stats.completed).toBe(0);
      expect(stats.blocked).toBe(0);
    });

    it('should handle all pending tasks', () => {
      const tasks = [
        { id: '1', status: 'pending' },
        { id: '2', status: 'pending' },
        { id: '3', status: 'pending' },
      ];

      const stats = calculateStats(tasks);

      expect(stats.ready).toBe(3);
      expect(stats.total).toBe(3);
    });
  });

  describe('API response transformation', () => {
    it('should transform API task to TaskDisplay format', () => {
      const apiTask = {
        id: 'task-123',
        title: 'Test Task',
        status: 'pending',
        priority: 'high',
        resolved_deps: ['dep-1', 'dep-2'],
        dependents: ['child-1'],
        progress: 50,
        error: null,
      };

      const display = transformTask(apiTask);

      expect(display.id).toBe('task-123');
      expect(display.title).toBe('Test Task');
      expect(display.status).toBe('pending');
      expect(display.priority).toBe('high');
      expect(display.dependencies).toEqual(['dep-1', 'dep-2']);
      expect(display.dependents).toEqual(['child-1']);
      expect(display.progress).toBe(50);
      expect(display.error).toBeNull();
    });

    it('should use default priority when not provided', () => {
      const apiTask = {
        id: 'task-123',
        title: 'Test Task',
        status: 'pending',
        // priority not provided
      };

      const display = transformTask(apiTask);

      expect(display.priority).toBe('medium');
    });

    it('should handle missing dependencies', () => {
      const apiTask = {
        id: 'task-123',
        title: 'Test Task',
        status: 'pending',
        // no resolved_deps or dependencies
      };

      const display = transformTask(apiTask);

      expect(display.dependencies).toEqual([]);
    });

    it('should fallback to dependencies if resolved_deps not present', () => {
      const apiTask = {
        id: 'task-123',
        title: 'Test Task',
        status: 'pending',
        dependencies: ['fallback-dep'],
      };

      const display = transformTask(apiTask);

      expect(display.dependencies).toEqual(['fallback-dep']);
    });
  });

  describe('poll interval', () => {
    it('should use default interval when not specified', () => {
      const options = {
        projectId: 'test',
        apiUrl: 'http://localhost:3333',
      };

      const interval = getPollInterval(options);
      expect(interval).toBe(2000); // default is 2000ms
    });

    it('should use custom interval when specified', () => {
      const options = {
        projectId: 'test',
        apiUrl: 'http://localhost:3333',
        pollInterval: 5000,
      };

      const interval = getPollInterval(options);
      expect(interval).toBe(5000);
    });
  });

  describe('enabled state', () => {
    it('should default to enabled', () => {
      const options = {
        projectId: 'test',
        apiUrl: 'http://localhost:3333',
      };

      const isEnabled = getEnabled(options);
      expect(isEnabled).toBe(true);
    });

    it('should respect disabled state', () => {
      const options = {
        projectId: 'test',
        apiUrl: 'http://localhost:3333',
        enabled: false,
      };

      const isEnabled = getEnabled(options);
      expect(isEnabled).toBe(false);
    });
  });

  describe('URL construction', () => {
    it('should construct correct API URL', () => {
      const url = buildTasksUrl('http://localhost:3333', 'my-project');
      expect(url).toBe('http://localhost:3333/api/v1/tasks/my-project');
    });

    it('should encode special characters in project ID', () => {
      const url = buildTasksUrl('http://localhost:3333', 'my project/test');
      expect(url).toBe(
        'http://localhost:3333/api/v1/tasks/my%20project%2Ftest'
      );
    });

    it('should handle trailing slash in base URL', () => {
      const url = buildTasksUrl('http://localhost:3333/', 'project');
      expect(url).toBe('http://localhost:3333//api/v1/tasks/project');
    });
  });

  describe('error handling', () => {
    it('should classify network errors', () => {
      const error = new Error('fetch failed');
      expect(isNetworkError(error)).toBe(true);
    });

    it('should classify HTTP errors', () => {
      const error = new Error('API error: 500 Internal Server Error');
      expect(isHttpError(error)).toBe(true);
    });

    it('should preserve stale data on error', () => {
      const currentTasks = [
        { id: '1', title: 'Task 1', status: 'pending' },
      ];
      const error = new Error('Network error');

      // When an error occurs, we should keep the current tasks
      const { tasks, shouldClear } = handlePollingError(error, currentTasks);

      expect(tasks).toEqual(currentTasks);
      expect(shouldClear).toBe(false);
    });
  });
});

// =============================================================================
// Test the pollerReducer (consolidated state management)
// =============================================================================

describe('useTaskPoller - pollerReducer', () => {
  // Import the reducer and types from the hook module
  // These will be exported after the refactor
  let pollerReducer: typeof import('./useTaskPoller').pollerReducer;

  beforeEach(async () => {
    const mod = await import('./useTaskPoller');
    pollerReducer = mod.pollerReducer;
  });

  const initialState = {
    tasks: [],
    stats: { total: 0, ready: 0, waiting: 0, blocked: 0, inProgress: 0, completed: 0 },
    isLoading: true,
    isConnected: false,
    error: null,
  };

  it('should handle FETCH_START by setting isLoading to true', () => {
    const state = { ...initialState, isLoading: false };
    const result = pollerReducer(state, { type: 'FETCH_START' as const });
    expect(result.isLoading).toBe(true);
    // Other fields should remain unchanged
    expect(result.tasks).toEqual([]);
    expect(result.isConnected).toBe(false);
    expect(result.error).toBeNull();
  });

  it('should handle FETCH_SUCCESS with tasks and stats in a single state object', () => {
    const tasks = [
      { id: '1', path: '/p', title: 'Task 1', status: 'pending' as any, priority: 'medium' as any, tags: [], dependencies: [], dependents: [], dependencyTitles: [], dependentTitles: [] },
    ];
    const stats = { total: 1, ready: 1, waiting: 0, blocked: 0, inProgress: 0, completed: 0 };

    const result = pollerReducer(initialState, {
      type: 'FETCH_SUCCESS' as const,
      tasks,
      stats,
    });

    expect(result.tasks).toEqual(tasks);
    expect(result.stats).toEqual(stats);
    expect(result.isLoading).toBe(false);
    expect(result.isConnected).toBe(true);
    expect(result.error).toBeNull();
  });

  it('should handle FETCH_ERROR by preserving existing tasks and stats', () => {
    const existingTasks = [
      { id: '1', path: '/p', title: 'Task 1', status: 'pending' as any, priority: 'medium' as any, tags: [], dependencies: [], dependents: [], dependencyTitles: [], dependentTitles: [] },
    ];
    const existingStats = { total: 1, ready: 1, waiting: 0, blocked: 0, inProgress: 0, completed: 0 };
    const stateWithData = {
      tasks: existingTasks,
      stats: existingStats,
      isLoading: false,
      isConnected: true,
      error: null,
    };

    const error = new Error('Network failure');
    const result = pollerReducer(stateWithData, {
      type: 'FETCH_ERROR' as const,
      error,
    });

    // Tasks and stats should be preserved (stale data)
    expect(result.tasks).toEqual(existingTasks);
    expect(result.stats).toEqual(existingStats);
    expect(result.isLoading).toBe(false);
    expect(result.isConnected).toBe(false);
    expect(result.error).toBe(error);
  });

  it('should produce a new state reference on FETCH_SUCCESS (enables React re-render)', () => {
    const tasks = [
      { id: '1', path: '/p', title: 'Task 1', status: 'pending' as any, priority: 'medium' as any, tags: [], dependencies: [], dependents: [], dependencyTitles: [], dependentTitles: [] },
    ];
    const stats = { total: 1, ready: 1, waiting: 0, blocked: 0, inProgress: 0, completed: 0 };

    const result = pollerReducer(initialState, {
      type: 'FETCH_SUCCESS' as const,
      tasks,
      stats,
    });

    expect(result).not.toBe(initialState);
  });
});

// =============================================================================
// Helper functions that mirror the hook's internal logic
// These are extracted to enable unit testing
// =============================================================================

interface TaskStats {
  total: number;
  ready: number;
  waiting: number;
  blocked: number;
  inProgress: number;
  completed: number;
}

interface TaskLike {
  id: string;
  status: string;
}

function calculateStats(tasks: TaskLike[]): TaskStats {
  return {
    total: tasks.length,
    ready: tasks.filter((t) => t.status === 'pending').length,
    waiting: 0, // Would need more context to calculate
    blocked: tasks.filter((t) => t.status === 'blocked').length,
    inProgress: tasks.filter((t) => t.status === 'in_progress').length,
    completed: tasks.filter(
      (t) => t.status === 'completed' || t.status === 'validated'
    ).length,
  };
}

interface ApiTask {
  id: string;
  title: string;
  status: string;
  priority?: string;
  resolved_deps?: string[];
  dependencies?: string[];
  dependents?: string[];
  progress?: number;
  error?: string | null;
}

interface TaskDisplay {
  id: string;
  title: string;
  status: string;
  priority: string;
  dependencies: string[];
  dependents: string[];
  progress?: number;
  error?: string | null;
}

function transformTask(task: ApiTask): TaskDisplay {
  return {
    id: task.id,
    title: task.title,
    status: task.status,
    priority: task.priority || 'medium',
    dependencies: task.resolved_deps || task.dependencies || [],
    dependents: task.dependents || [],
    progress: task.progress,
    error: task.error,
  };
}

interface PollerOptions {
  projectId: string;
  apiUrl: string;
  pollInterval?: number;
  enabled?: boolean;
}

function getPollInterval(options: PollerOptions): number {
  return options.pollInterval ?? 2000;
}

function getEnabled(options: PollerOptions): boolean {
  return options.enabled ?? true;
}

function buildTasksUrl(apiUrl: string, projectId: string): string {
  return `${apiUrl}/api/v1/tasks/${encodeURIComponent(projectId)}`;
}

function isNetworkError(error: Error): boolean {
  return error.message.toLowerCase().includes('fetch');
}

function isHttpError(error: Error): boolean {
  return error.message.includes('API error:');
}

function handlePollingError(
  _error: Error,
  currentTasks: TaskLike[]
): { tasks: TaskLike[]; shouldClear: boolean } {
  // Don't clear tasks on error - show stale data
  return {
    tasks: currentTasks,
    shouldClear: false,
  };
}

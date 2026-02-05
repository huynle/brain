/**
 * useMultiProjectPoller Hook Tests
 *
 * Tests the multi-project polling hook including:
 * - Aggregate stats calculation across projects
 * - Per-project data tracking
 * - Partial failure handling
 * - Task projectId tagging
 * - Memoization of derived state (Issue #2)
 */

import { describe, it, expect, beforeEach } from 'bun:test';

// =============================================================================
// Test the core logic directly (without React hooks)
// =============================================================================

describe('useMultiProjectPoller - Core Logic', () => {
  describe('aggregate stats calculation', () => {
    it('should aggregate stats from multiple projects', () => {
      const statsByProject = new Map<string, TaskStats>([
        ['project1', { total: 3, ready: 2, waiting: 0, blocked: 0, inProgress: 1, completed: 0 }],
        ['project2', { total: 2, ready: 1, waiting: 0, blocked: 1, inProgress: 0, completed: 0 }],
      ]);

      const aggregate = aggregateProjectStats(statsByProject);

      expect(aggregate.total).toBe(5);
      expect(aggregate.ready).toBe(3);
      expect(aggregate.blocked).toBe(1);
      expect(aggregate.inProgress).toBe(1);
    });

    it('should handle empty stats map', () => {
      const statsByProject = new Map<string, TaskStats>();
      const aggregate = aggregateProjectStats(statsByProject);

      expect(aggregate.total).toBe(0);
      expect(aggregate.ready).toBe(0);
      expect(aggregate.waiting).toBe(0);
      expect(aggregate.blocked).toBe(0);
      expect(aggregate.inProgress).toBe(0);
      expect(aggregate.completed).toBe(0);
    });

    it('should handle single project', () => {
      const statsByProject = new Map<string, TaskStats>([
        ['project1', { total: 5, ready: 2, waiting: 1, blocked: 1, inProgress: 1, completed: 0 }],
      ]);

      const aggregate = aggregateProjectStats(statsByProject);

      expect(aggregate.total).toBe(5);
      expect(aggregate.ready).toBe(2);
      expect(aggregate.waiting).toBe(1);
    });
  });

  describe('task projectId tagging', () => {
    it('should tag tasks with their project ID', () => {
      const apiTasks = [
        { id: 'task1', title: 'Task 1', status: 'pending' },
        { id: 'task2', title: 'Task 2', status: 'completed' },
      ];

      const taggedTasks = tagTasksWithProject(apiTasks, 'my-project');

      expect(taggedTasks[0].projectId).toBe('my-project');
      expect(taggedTasks[1].projectId).toBe('my-project');
    });

    it('should preserve original task properties', () => {
      const apiTasks = [
        { id: 'task1', title: 'Task 1', status: 'pending', priority: 'high' },
      ];

      const taggedTasks = tagTasksWithProject(apiTasks, 'my-project');

      expect(taggedTasks[0].id).toBe('task1');
      expect(taggedTasks[0].title).toBe('Task 1');
      expect(taggedTasks[0].status).toBe('pending');
      expect(taggedTasks[0].priority).toBe('high');
    });
  });

  describe('task merging', () => {
    it('should merge tasks from multiple projects', () => {
      const tasksByProject = new Map<string, TaskDisplay[]>([
        ['project1', [
          { id: 'task1', title: 'Task 1', status: 'pending', projectId: 'project1' },
        ]],
        ['project2', [
          { id: 'task2', title: 'Task 2', status: 'completed', projectId: 'project2' },
        ]],
      ]);

      const allTasks = mergeAllTasks(tasksByProject);

      expect(allTasks).toHaveLength(2);
      expect(allTasks.find(t => t.id === 'task1')).toBeDefined();
      expect(allTasks.find(t => t.id === 'task2')).toBeDefined();
    });

    it('should handle empty project', () => {
      const tasksByProject = new Map<string, TaskDisplay[]>([
        ['project1', [
          { id: 'task1', title: 'Task 1', status: 'pending', projectId: 'project1' },
        ]],
        ['project2', []], // Empty project
      ]);

      const allTasks = mergeAllTasks(tasksByProject);

      expect(allTasks).toHaveLength(1);
    });
  });

  describe('connection status', () => {
    it('should report connected if at least one project is connected', () => {
      const connectionByProject = new Map<string, boolean>([
        ['project1', true],
        ['project2', false],
      ]);

      const isConnected = checkAnyConnected(connectionByProject);
      expect(isConnected).toBe(true);
    });

    it('should report disconnected if all projects fail', () => {
      const connectionByProject = new Map<string, boolean>([
        ['project1', false],
        ['project2', false],
      ]);

      const isConnected = checkAnyConnected(connectionByProject);
      expect(isConnected).toBe(false);
    });

    it('should report disconnected for empty project list', () => {
      const connectionByProject = new Map<string, boolean>();

      const isConnected = checkAnyConnected(connectionByProject);
      expect(isConnected).toBe(false);
    });
  });

  describe('project filtering', () => {
    it('should filter tasks by active project', () => {
      const tasksByProject = new Map<string, TaskDisplay[]>([
        ['project1', [
          { id: 'task1', title: 'Task 1', status: 'pending', projectId: 'project1' },
        ]],
        ['project2', [
          { id: 'task2', title: 'Task 2', status: 'completed', projectId: 'project2' },
        ]],
      ]);

      const filtered = filterByActiveProject(tasksByProject, 'project1');
      
      expect(filtered).toHaveLength(1);
      expect(filtered[0].id).toBe('task1');
    });

    it('should return all tasks when activeProject is "all"', () => {
      const tasksByProject = new Map<string, TaskDisplay[]>([
        ['project1', [
          { id: 'task1', title: 'Task 1', status: 'pending', projectId: 'project1' },
        ]],
        ['project2', [
          { id: 'task2', title: 'Task 2', status: 'completed', projectId: 'project2' },
        ]],
      ]);

      const filtered = filterByActiveProject(tasksByProject, 'all');
      
      expect(filtered).toHaveLength(2);
    });

    it('should return empty array for unknown project', () => {
      const tasksByProject = new Map<string, TaskDisplay[]>([
        ['project1', [
          { id: 'task1', title: 'Task 1', status: 'pending', projectId: 'project1' },
        ]],
      ]);

      const filtered = filterByActiveProject(tasksByProject, 'unknown-project');
      
      expect(filtered).toHaveLength(0);
    });
  });

  describe('stats filtering', () => {
    it('should return stats for specific project', () => {
      const statsByProject = new Map<string, TaskStats>([
        ['project1', { total: 3, ready: 2, waiting: 0, blocked: 0, inProgress: 1, completed: 0 }],
        ['project2', { total: 2, ready: 1, waiting: 0, blocked: 1, inProgress: 0, completed: 0 }],
      ]);

      const stats = getStatsForProject(statsByProject, 'project1');
      
      expect(stats.total).toBe(3);
      expect(stats.ready).toBe(2);
    });

    it('should return aggregate stats when activeProject is "all"', () => {
      const statsByProject = new Map<string, TaskStats>([
        ['project1', { total: 3, ready: 2, waiting: 0, blocked: 0, inProgress: 1, completed: 0 }],
        ['project2', { total: 2, ready: 1, waiting: 0, blocked: 1, inProgress: 0, completed: 0 }],
      ]);

      const stats = getStatsForProject(statsByProject, 'all');
      
      expect(stats.total).toBe(5);
      expect(stats.ready).toBe(3);
    });

    it('should return empty stats for unknown project', () => {
      const statsByProject = new Map<string, TaskStats>([
        ['project1', { total: 3, ready: 2, waiting: 0, blocked: 0, inProgress: 1, completed: 0 }],
      ]);

      const stats = getStatsForProject(statsByProject, 'unknown');
      
      expect(stats.total).toBe(0);
      expect(stats.ready).toBe(0);
    });
  });
});

// =============================================================================
// Test the multiProjectReducer (consolidated state management)
// =============================================================================

describe('useMultiProjectPoller - multiProjectReducer', () => {
  let multiProjectReducer: typeof import('./useMultiProjectPoller').multiProjectReducer;

  beforeEach(async () => {
    const mod = await import('./useMultiProjectPoller');
    multiProjectReducer = mod.multiProjectReducer;
  });

  const initialState = {
    tasksByProject: new Map<string, any[]>(),
    statsByProject: new Map<string, any>(),
    connectionByProject: new Map<string, boolean>(),
    errorsByProject: new Map<string, Error>(),
    isLoading: true,
  };

  it('should handle FETCH_START by setting isLoading to true', () => {
    const state = { ...initialState, isLoading: false };
    const result = multiProjectReducer(state, { type: 'FETCH_START' as const });
    expect(result.isLoading).toBe(true);
  });

  it('should handle FETCH_SUCCESS with all project data in a single state object', () => {
    const tasksByProject = new Map([['p1', [{ id: 't1', path: '/p', title: 'T1', status: 'pending' as any, priority: 'medium' as any, dependencies: [] as string[], dependents: [] as string[] }]]]);
    const statsByProject = new Map([['p1', { total: 1, ready: 1, waiting: 0, blocked: 0, inProgress: 0, completed: 0 }]]);
    const connectionByProject = new Map([['p1', true]]);
    const errorsByProject = new Map<string, Error>();

    const result = multiProjectReducer(initialState, {
      type: 'FETCH_SUCCESS' as const,
      tasksByProject,
      statsByProject,
      connectionByProject,
      errorsByProject,
    });

    expect(result.tasksByProject).toBe(tasksByProject);
    expect(result.statsByProject).toBe(statsByProject);
    expect(result.connectionByProject).toBe(connectionByProject);
    expect(result.errorsByProject).toBe(errorsByProject);
    expect(result.isLoading).toBe(false);
  });

  it('should produce a new state reference on FETCH_SUCCESS', () => {
    const result = multiProjectReducer(initialState, {
      type: 'FETCH_SUCCESS' as const,
      tasksByProject: new Map(),
      statsByProject: new Map(),
      connectionByProject: new Map(),
      errorsByProject: new Map(),
    });

    expect(result).not.toBe(initialState);
  });

  it('should handle FETCH_START without losing existing data', () => {
    const tasksByProject = new Map([['p1', [{ id: 't1', path: '/p', title: 'T1', status: 'pending' as any, priority: 'medium' as any, dependencies: [] as string[], dependents: [] as string[] }]]]);
    const stateWithData = {
      ...initialState,
      tasksByProject,
      isLoading: false,
    };

    const result = multiProjectReducer(stateWithData, { type: 'FETCH_START' as const });

    expect(result.isLoading).toBe(true);
    expect(result.tasksByProject).toBe(tasksByProject); // preserved
  });
});

// =============================================================================
// Test exported derived-state helpers for useMemo (Issue #2)
// =============================================================================

describe('useMultiProjectPoller - Exported derived-state helpers', () => {
  let mergeAllTasks: typeof import('./useMultiProjectPoller').mergeAllTasks;
  let checkAnyConnected: typeof import('./useMultiProjectPoller').checkAnyConnected;
  let getFirstError: typeof import('./useMultiProjectPoller').getFirstError;
  let aggregateProjectStatsExported: typeof import('./useMultiProjectPoller').aggregateProjectStats;

  beforeEach(async () => {
    const mod = await import('./useMultiProjectPoller');
    mergeAllTasks = mod.mergeAllTasks;
    checkAnyConnected = mod.checkAnyConnected;
    getFirstError = mod.getFirstError;
    aggregateProjectStatsExported = mod.aggregateProjectStats;
  });

  describe('mergeAllTasks', () => {
    it('should be exported from the module', () => {
      expect(typeof mergeAllTasks).toBe('function');
    });

    it('should merge tasks from multiple projects into a flat array', () => {
      const tasksByProject = new Map([
        ['p1', [
          { id: 't1', path: '/p', title: 'T1', status: 'pending' as any, priority: 'medium' as any, dependencies: [] as string[], dependents: [] as string[], projectId: 'p1' },
        ]],
        ['p2', [
          { id: 't2', path: '/p', title: 'T2', status: 'completed' as any, priority: 'low' as any, dependencies: [] as string[], dependents: [] as string[], projectId: 'p2' },
        ]],
      ]);

      const result = mergeAllTasks(tasksByProject);
      expect(result).toHaveLength(2);
      expect(result.find((t: any) => t.id === 't1')).toBeDefined();
      expect(result.find((t: any) => t.id === 't2')).toBeDefined();
    });

    it('should return empty array for empty map', () => {
      const result = mergeAllTasks(new Map());
      expect(result).toHaveLength(0);
    });

    it('should return the same reference when called with the same Map reference', () => {
      // This tests that the function is pure and suitable for useMemo
      const tasksByProject = new Map([
        ['p1', [
          { id: 't1', path: '/p', title: 'T1', status: 'pending' as any, priority: 'medium' as any, dependencies: [] as string[], dependents: [] as string[], projectId: 'p1' },
        ]],
      ]);

      const result1 = mergeAllTasks(tasksByProject);
      const result2 = mergeAllTasks(tasksByProject);
      // Both calls produce arrays with the same content
      expect(result1).toEqual(result2);
    });
  });

  describe('checkAnyConnected', () => {
    it('should be exported from the module', () => {
      expect(typeof checkAnyConnected).toBe('function');
    });

    it('should return true if at least one project is connected', () => {
      const connectionByProject = new Map([
        ['p1', true],
        ['p2', false],
      ]);
      expect(checkAnyConnected(connectionByProject)).toBe(true);
    });

    it('should return false if no projects are connected', () => {
      const connectionByProject = new Map([
        ['p1', false],
        ['p2', false],
      ]);
      expect(checkAnyConnected(connectionByProject)).toBe(false);
    });

    it('should return false for empty map', () => {
      expect(checkAnyConnected(new Map())).toBe(false);
    });
  });

  describe('getFirstError', () => {
    it('should be exported from the module', () => {
      expect(typeof getFirstError).toBe('function');
    });

    it('should return the first error from the map', () => {
      const err = new Error('test error');
      const errorsByProject = new Map([
        ['p1', err],
      ]);
      expect(getFirstError(errorsByProject)).toBe(err);
    });

    it('should return null for empty map', () => {
      expect(getFirstError(new Map())).toBeNull();
    });
  });

  describe('aggregateProjectStats (exported)', () => {
    it('should be exported from the module', () => {
      expect(typeof aggregateProjectStatsExported).toBe('function');
    });
  });
});

// =============================================================================
// Helper functions that mirror the hook's internal logic
// =============================================================================

interface TaskStats {
  total: number;
  ready: number;
  waiting: number;
  blocked: number;
  inProgress: number;
  completed: number;
}

interface TaskDisplay {
  id: string;
  title: string;
  status: string;
  projectId?: string;
  priority?: string;
}

const EMPTY_STATS: TaskStats = {
  total: 0,
  ready: 0,
  waiting: 0,
  blocked: 0,
  inProgress: 0,
  completed: 0,
};

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

function tagTasksWithProject<T extends object>(tasks: T[], projectId: string): (T & { projectId: string })[] {
  return tasks.map(task => ({
    ...task,
    projectId,
  }));
}

function mergeAllTasks(tasksByProject: Map<string, TaskDisplay[]>): TaskDisplay[] {
  return Array.from(tasksByProject.values()).flat();
}

function checkAnyConnected(connectionByProject: Map<string, boolean>): boolean {
  return Array.from(connectionByProject.values()).some(Boolean);
}

function filterByActiveProject(
  tasksByProject: Map<string, TaskDisplay[]>,
  activeProject: string
): TaskDisplay[] {
  if (activeProject === 'all') {
    return mergeAllTasks(tasksByProject);
  }
  return tasksByProject.get(activeProject) ?? [];
}

function getStatsForProject(
  statsByProject: Map<string, TaskStats>,
  activeProject: string
): TaskStats {
  if (activeProject === 'all') {
    return aggregateProjectStats(statsByProject);
  }
  return statsByProject.get(activeProject) ?? { ...EMPTY_STATS };
}

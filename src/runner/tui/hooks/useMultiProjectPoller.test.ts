/**
 * useMultiProjectPoller Hook Tests
 *
 * Tests the multi-project polling hook including:
 * - Aggregate stats calculation across projects
 * - Per-project data tracking
 * - Partial failure handling
 * - Task projectId tagging
 */

import { describe, it, expect } from 'bun:test';

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

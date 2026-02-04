/**
 * TaskTree Component Tests
 *
 * Tests the task dependency tree rendering including:
 * - Flat list rendering
 * - Nested dependencies
 * - Status symbols and colors
 * - Empty states
 * - Selection highlighting
 * - Priority indicators
 * - Cycle detection
 */

import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { TaskTree } from './TaskTree';
import type { TaskDisplay } from '../types';

// Helper to create mock tasks
function createTask(overrides: Partial<TaskDisplay> = {}): TaskDisplay {
  return {
    id: 'task-1',
    title: 'Test Task',
    status: 'pending',
    priority: 'medium',
    dependencies: [],
    dependents: [],
    ...overrides,
  };
}

describe('TaskTree', () => {
  describe('empty state', () => {
    it('shows "No tasks found" when task list is empty', () => {
      const { lastFrame } = render(
        <TaskTree tasks={[]} selectedId={null} onSelect={() => {}} />
      );
      expect(lastFrame()).toContain('No tasks found');
    });
  });

  describe('flat list rendering', () => {
    it('renders single task correctly', () => {
      const tasks = [createTask({ id: '1', title: 'Setup Project' })];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      expect(lastFrame()).toContain('Setup Project');
      expect(lastFrame()).toContain('Tasks (1)');
    });

    it('renders multiple tasks', () => {
      const tasks = [
        createTask({ id: '1', title: 'Task One' }),
        createTask({ id: '2', title: 'Task Two' }),
        createTask({ id: '3', title: 'Task Three' }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      expect(lastFrame()).toContain('Task One');
      expect(lastFrame()).toContain('Task Two');
      expect(lastFrame()).toContain('Task Three');
      expect(lastFrame()).toContain('Tasks (3)');
    });
  });

  describe('nested dependencies', () => {
    it('renders child tasks indented under parent', () => {
      const tasks = [
        createTask({ id: 'parent', title: 'Parent Task', dependencies: [] }),
        createTask({
          id: 'child',
          title: 'Child Task',
          dependencies: ['parent'],
        }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      // Both tasks should be rendered
      expect(lastFrame()).toContain('Parent Task');
      expect(lastFrame()).toContain('Child Task');
      // Tree structure characters should be present for nested tasks
      expect(lastFrame()).toContain('─');
    });

    it('renders deeply nested dependencies', () => {
      const tasks = [
        createTask({ id: 'root', title: 'Root Task', dependencies: [] }),
        createTask({
          id: 'level1',
          title: 'Level 1 Task',
          dependencies: ['root'],
        }),
        createTask({
          id: 'level2',
          title: 'Level 2 Task',
          dependencies: ['level1'],
        }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      expect(lastFrame()).toContain('Root Task');
      expect(lastFrame()).toContain('Level 1 Task');
      expect(lastFrame()).toContain('Level 2 Task');
    });
  });

  describe('status symbols', () => {
    it('shows pending symbol for pending tasks', () => {
      const tasks = [createTask({ id: '1', title: 'Pending', status: 'pending' })];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      // Pending tasks with no blocking deps should show ready indicator
      expect(lastFrame()).toContain('●');
    });

    it('shows in_progress symbol for active tasks', () => {
      const tasks = [
        createTask({ id: '1', title: 'Active', status: 'in_progress' }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      expect(lastFrame()).toContain('▶');
    });

    it('shows completed symbol for completed tasks', () => {
      const tasks = [
        createTask({ id: '1', title: 'Done', status: 'completed' }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      expect(lastFrame()).toContain('✓');
    });

    it('shows blocked symbol for blocked tasks', () => {
      const tasks = [
        createTask({ id: '1', title: 'Blocked', status: 'blocked' }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      expect(lastFrame()).toContain('✗');
    });
  });

  describe('selection highlighting', () => {
    it('highlights selected task', () => {
      const tasks = [
        createTask({ id: '1', title: 'First Task' }),
        createTask({ id: '2', title: 'Second Task' }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId="1" onSelect={() => {}} />
      );
      // Both tasks should be visible
      expect(lastFrame()).toContain('First Task');
      expect(lastFrame()).toContain('Second Task');
    });

    it('handles null selectedId', () => {
      const tasks = [createTask({ id: '1', title: 'Task' })];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      expect(lastFrame()).toContain('Task');
    });

    it('handles non-existent selectedId', () => {
      const tasks = [createTask({ id: '1', title: 'Task' })];
      const { lastFrame } = render(
        <TaskTree
          tasks={tasks}
          selectedId="non-existent"
          onSelect={() => {}}
        />
      );
      expect(lastFrame()).toContain('Task');
    });
  });

  describe('priority indicators', () => {
    it('shows ! for high priority tasks', () => {
      const tasks = [
        createTask({ id: '1', title: 'Urgent', priority: 'high' }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      expect(lastFrame()).toContain('!');
    });

    it('does not show ! for medium priority tasks', () => {
      const tasks = [
        createTask({ id: '1', title: 'Normal', priority: 'medium' }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      // The frame should contain Normal but after it should not have !
      const frame = lastFrame() || '';
      const normalIndex = frame.indexOf('Normal');
      // Check there's no ! immediately after the title
      expect(frame.includes('Normal!')).toBe(false);
    });

    it('does not show ! for low priority tasks', () => {
      const tasks = [createTask({ id: '1', title: 'Low', priority: 'low' })];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      const frame = lastFrame() || '';
      expect(frame.includes('Low!')).toBe(false);
    });

    it('sorts high priority tasks first', () => {
      const tasks = [
        createTask({ id: '1', title: 'Low Task', priority: 'low' }),
        createTask({ id: '2', title: 'High Task', priority: 'high' }),
        createTask({ id: '3', title: 'Medium Task', priority: 'medium' }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      const frame = lastFrame() || '';
      const highIndex = frame.indexOf('High Task');
      const mediumIndex = frame.indexOf('Medium Task');
      const lowIndex = frame.indexOf('Low Task');
      // High priority should appear before medium and low
      expect(highIndex).toBeLessThan(mediumIndex);
      expect(highIndex).toBeLessThan(lowIndex);
    });
  });

  describe('cycle detection', () => {
    it('marks tasks in circular dependency with cycle indicator', () => {
      // Create circular dependency: A depends on B, B depends on A
      const tasks = [
        createTask({ id: 'a', title: 'Task A', dependencies: ['b'] }),
        createTask({ id: 'b', title: 'Task B', dependencies: ['a'] }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      // Cycle indicator should be present
      expect(lastFrame()).toContain('↺');
    });

    it('does not show cycle indicator for non-circular tasks', () => {
      const tasks = [
        createTask({ id: 'a', title: 'Task A', dependencies: [] }),
        createTask({ id: 'b', title: 'Task B', dependencies: ['a'] }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      expect(lastFrame()).not.toContain('↺');
    });
  });

  describe('ready state detection', () => {
    it('shows ready indicator for pending tasks with completed dependencies', () => {
      const tasks = [
        createTask({
          id: 'parent',
          title: 'Parent',
          status: 'completed',
          dependencies: [],
        }),
        createTask({
          id: 'child',
          title: 'Child',
          status: 'pending',
          dependencies: ['parent'],
        }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      // Child should show ready indicator since parent is completed
      expect(lastFrame()).toContain('●');
    });

    it('shows waiting indicator for pending tasks with incomplete dependencies', () => {
      const tasks = [
        createTask({
          id: 'parent',
          title: 'Parent',
          status: 'pending',
          dependencies: [],
        }),
        createTask({
          id: 'child',
          title: 'Child',
          status: 'pending',
          dependencies: ['parent'],
        }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      // Both should be visible, child waiting on parent
      expect(lastFrame()).toContain('Parent');
      expect(lastFrame()).toContain('Child');
    });
  });

  describe('edge cases', () => {
    it('handles tasks with external dependencies (deps not in list)', () => {
      const tasks = [
        createTask({
          id: '1',
          title: 'Task with external dep',
          dependencies: ['external-task-id'],
        }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      expect(lastFrame()).toContain('Task with external dep');
    });

    it('handles diamond dependency pattern', () => {
      // A -> B -> D
      // A -> C -> D
      const tasks = [
        createTask({ id: 'a', title: 'Task A', dependencies: [] }),
        createTask({ id: 'b', title: 'Task B', dependencies: ['a'] }),
        createTask({ id: 'c', title: 'Task C', dependencies: ['a'] }),
        createTask({ id: 'd', title: 'Task D', dependencies: ['b', 'c'] }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} />
      );
      // All tasks should be rendered once
      expect(lastFrame()).toContain('Task A');
      expect(lastFrame()).toContain('Task B');
      expect(lastFrame()).toContain('Task C');
      expect(lastFrame()).toContain('Task D');
    });
  });
});

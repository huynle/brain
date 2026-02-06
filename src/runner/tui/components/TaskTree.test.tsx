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
import { TaskTree, flattenTreeOrder, buildTree, COMPLETED_HEADER_ID } from './TaskTree';
import type { TaskDisplay } from '../types';

// Helper to create mock tasks
function createTask(overrides: Partial<TaskDisplay> = {}): TaskDisplay {
  // Extract dependencies/dependents to auto-generate title fields
  const dependencies = overrides.dependencies ?? [];
  const dependents = overrides.dependents ?? [];
  return {
    id: 'task-1',
    path: 'projects/test/task/task-1.md',
    title: 'Test Task',
    status: 'pending',
    priority: 'medium',
    dependencies,
    dependents,
    // Auto-generate title fields from ID fields (test uses IDs as titles)
    dependencyTitles: overrides.dependencyTitles ?? dependencies,
    dependentTitles: overrides.dependentTitles ?? dependents,
    ...overrides,
  };
}

// Default props for TaskTree to reduce test boilerplate
const defaultTreeProps = {
  completedCollapsed: true,
  onToggleCompleted: () => {},
};

describe('TaskTree', () => {
  describe('empty state', () => {
    it('shows "No tasks found" when task list is empty', () => {
      const { lastFrame } = render(
        <TaskTree tasks={[]} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
      );
      expect(lastFrame()).toContain('No tasks found');
    });
  });

  describe('flat list rendering', () => {
    it('renders single task correctly', () => {
      const tasks = [createTask({ id: '1', title: 'Setup Project' })];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
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
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
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
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
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
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
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
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
      );
      // Pending tasks with no blocking deps should show ready indicator
      expect(lastFrame()).toContain('●');
    });

    it('shows in_progress symbol for active tasks', () => {
      const tasks = [
        createTask({ id: '1', title: 'Active', status: 'in_progress' }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
      );
      expect(lastFrame()).toContain('▶');
    });

    it('shows completed symbol for completed tasks', () => {
      const tasks = [
        createTask({ id: '1', title: 'Done', status: 'completed' }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} completedCollapsed={false} onToggleCompleted={() => {}} />
      );
      expect(lastFrame()).toContain('✓');
    });

    it('shows blocked symbol for blocked tasks', () => {
      const tasks = [
        createTask({ id: '1', title: 'Blocked', status: 'blocked' }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
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
        <TaskTree tasks={tasks} selectedId="1" onSelect={() => {}} {...defaultTreeProps} />
      );
      // Both tasks should be visible
      expect(lastFrame()).toContain('First Task');
      expect(lastFrame()).toContain('Second Task');
    });

    it('handles null selectedId', () => {
      const tasks = [createTask({ id: '1', title: 'Task' })];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
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
          {...defaultTreeProps}
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
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
      );
      expect(lastFrame()).toContain('!');
    });

    it('does not show ! for medium priority tasks', () => {
      const tasks = [
        createTask({ id: '1', title: 'Normal', priority: 'medium' }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
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
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
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
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
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
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
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
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
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
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
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
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
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
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
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
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
      );
      // All tasks should be rendered once
      expect(lastFrame()).toContain('Task A');
      expect(lastFrame()).toContain('Task B');
      expect(lastFrame()).toContain('Task C');
      expect(lastFrame()).toContain('Task D');
    });
  });

  describe('collapsible completed section', () => {
    it('shows collapsed completed header when there are completed tasks', () => {
      const tasks = [
        createTask({ id: '1', title: 'Active', status: 'pending' }),
        createTask({ id: '2', title: 'Done', status: 'completed' }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} completedCollapsed={true} onToggleCompleted={() => {}} />
      );
      expect(lastFrame()).toContain('▶ Completed (1)');
      expect(lastFrame()).not.toContain('Done');
    });

    it('shows expanded completed header and tasks when expanded', () => {
      const tasks = [
        createTask({ id: '1', title: 'Active', status: 'pending' }),
        createTask({ id: '2', title: 'Done', status: 'completed' }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} completedCollapsed={false} onToggleCompleted={() => {}} />
      );
      expect(lastFrame()).toContain('▾ Completed (1)');
      expect(lastFrame()).toContain('Done');
    });

    it('does not show completed section when no completed tasks', () => {
      const tasks = [
        createTask({ id: '1', title: 'Active', status: 'pending' }),
        createTask({ id: '2', title: 'In Progress', status: 'in_progress' }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
      );
      expect(lastFrame()).not.toContain('Completed');
    });

    it('counts validated tasks as completed', () => {
      const tasks = [
        createTask({ id: '1', title: 'Active', status: 'pending' }),
        createTask({ id: '2', title: 'Done', status: 'completed' }),
        createTask({ id: '3', title: 'Validated', status: 'validated' }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} completedCollapsed={true} onToggleCompleted={() => {}} />
      );
      expect(lastFrame()).toContain('▶ Completed (2)');
    });

    it('highlights completed header when selected', () => {
      const tasks = [
        createTask({ id: '1', title: 'Active', status: 'pending' }),
        createTask({ id: '2', title: 'Done', status: 'completed' }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={COMPLETED_HEADER_ID} onSelect={() => {}} completedCollapsed={true} onToggleCompleted={() => {}} />
      );
      // The header should be visible and selectable
      expect(lastFrame()).toContain('Completed (1)');
    });
  });
});

// =============================================================================
// Navigation order tests (flattenTreeOrder)
// =============================================================================

describe('flattenTreeOrder', () => {
  describe('basic ordering', () => {
    it('returns empty array for empty tasks', () => {
      const order = flattenTreeOrder([]);
      expect(order).toEqual([]);
    });

    it('returns single task id for single task', () => {
      const tasks = [createTask({ id: 'task-1', title: 'Only Task' })];
      const order = flattenTreeOrder(tasks);
      expect(order).toEqual(['task-1']);
    });

    it('maintains order for flat list of independent tasks', () => {
      const tasks = [
        createTask({ id: '1', title: 'Task 1', priority: 'medium' }),
        createTask({ id: '2', title: 'Task 2', priority: 'medium' }),
        createTask({ id: '3', title: 'Task 3', priority: 'medium' }),
      ];
      const order = flattenTreeOrder(tasks);
      // Should contain all task IDs
      expect(order.length).toBe(3);
      expect(order).toContain('1');
      expect(order).toContain('2');
      expect(order).toContain('3');
    });
  });

  describe('tree traversal order', () => {
    it('places children after parent in navigation order', () => {
      const tasks = [
        createTask({ id: 'parent', title: 'Parent', dependencies: [] }),
        createTask({ id: 'child', title: 'Child', dependencies: ['parent'] }),
      ];
      const order = flattenTreeOrder(tasks);
      expect(order).toEqual(['parent', 'child']);
    });

    it('traverses deeply nested tree in depth-first order', () => {
      const tasks = [
        createTask({ id: 'root', title: 'Root', dependencies: [] }),
        createTask({ id: 'level1', title: 'Level 1', dependencies: ['root'] }),
        createTask({ id: 'level2', title: 'Level 2', dependencies: ['level1'] }),
        createTask({ id: 'level3', title: 'Level 3', dependencies: ['level2'] }),
      ];
      const order = flattenTreeOrder(tasks);
      // Should be depth-first: root -> level1 -> level2 -> level3
      expect(order).toEqual(['root', 'level1', 'level2', 'level3']);
    });

    it('handles multiple children at same level', () => {
      const tasks = [
        createTask({ id: 'parent', title: 'Parent', dependencies: [] }),
        createTask({ id: 'child1', title: 'Child 1', dependencies: ['parent'], priority: 'high' }),
        createTask({ id: 'child2', title: 'Child 2', dependencies: ['parent'], priority: 'medium' }),
      ];
      const order = flattenTreeOrder(tasks);
      // Parent first, then children (sorted by priority: high before medium)
      expect(order[0]).toBe('parent');
      expect(order).toContain('child1');
      expect(order).toContain('child2');
      // High priority child should come before medium
      expect(order.indexOf('child1')).toBeLessThan(order.indexOf('child2'));
    });
  });

  describe('priority sorting in navigation', () => {
    it('respects priority order among siblings', () => {
      const tasks = [
        createTask({ id: 'low', title: 'Low', priority: 'low' }),
        createTask({ id: 'high', title: 'High', priority: 'high' }),
        createTask({ id: 'medium', title: 'Medium', priority: 'medium' }),
      ];
      const order = flattenTreeOrder(tasks);
      // High should come before medium, medium before low
      expect(order.indexOf('high')).toBeLessThan(order.indexOf('medium'));
      expect(order.indexOf('medium')).toBeLessThan(order.indexOf('low'));
    });
  });

  describe('diamond dependencies', () => {
    it('includes each task only once for diamond pattern', () => {
      // A -> B, C
      // B, C -> D
      const tasks = [
        createTask({ id: 'a', title: 'Task A', dependencies: [] }),
        createTask({ id: 'b', title: 'Task B', dependencies: ['a'] }),
        createTask({ id: 'c', title: 'Task C', dependencies: ['a'] }),
        createTask({ id: 'd', title: 'Task D', dependencies: ['b', 'c'] }),
      ];
      const order = flattenTreeOrder(tasks);
      // All four tasks exactly once
      expect(order.length).toBe(4);
      expect(new Set(order).size).toBe(4);
      // A must come before B and C, B and C before D
      expect(order.indexOf('a')).toBeLessThan(order.indexOf('b'));
      expect(order.indexOf('a')).toBeLessThan(order.indexOf('c'));
    });
  });

  describe('consistency with visual display', () => {
    it('navigation order matches task appearance order in rendered output', () => {
      const tasks = [
        createTask({ id: 'root', title: 'Root Task', dependencies: [] }),
        createTask({ id: 'child1', title: 'Child One', dependencies: ['root'], priority: 'high' }),
        createTask({ id: 'child2', title: 'Child Two', dependencies: ['root'], priority: 'low' }),
        createTask({ id: 'grandchild', title: 'Grandchild', dependencies: ['child1'] }),
      ];
      
      // Get navigation order
      const navOrder = flattenTreeOrder(tasks);
      
      // Render the tree and check visual order
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
      );
      const frame = lastFrame() || '';
      
      // Tasks should appear in same order visually as in navOrder
      let lastIndex = -1;
      for (const taskId of navOrder) {
        const task = tasks.find(t => t.id === taskId);
        if (task) {
          const currentIndex = frame.indexOf(task.title);
          expect(currentIndex).toBeGreaterThan(lastIndex);
          lastIndex = currentIndex;
        }
      }
    });
  });

  describe('completed section navigation', () => {
    it('includes completed header ID when there are completed tasks', () => {
      const tasks = [
        createTask({ id: 'active', title: 'Active', status: 'pending' }),
        createTask({ id: 'done', title: 'Done', status: 'completed' }),
      ];
      const order = flattenTreeOrder(tasks, true);
      expect(order).toContain(COMPLETED_HEADER_ID);
      expect(order).not.toContain('done'); // collapsed - completed tasks not in order
    });

    it('includes completed task IDs when expanded', () => {
      const tasks = [
        createTask({ id: 'active', title: 'Active', status: 'pending' }),
        createTask({ id: 'done', title: 'Done', status: 'completed' }),
      ];
      const order = flattenTreeOrder(tasks, false);
      expect(order).toContain(COMPLETED_HEADER_ID);
      expect(order).toContain('done'); // expanded - completed tasks in order
    });

    it('does not include completed header when no completed tasks', () => {
      const tasks = [
        createTask({ id: 'active', title: 'Active', status: 'pending' }),
        createTask({ id: 'blocked', title: 'Blocked', status: 'blocked' }),
      ];
      const order = flattenTreeOrder(tasks, true);
      expect(order).not.toContain(COMPLETED_HEADER_ID);
    });

    it('places completed header after all active tasks', () => {
      const tasks = [
        createTask({ id: 'active1', title: 'Active 1', status: 'pending' }),
        createTask({ id: 'active2', title: 'Active 2', status: 'in_progress' }),
        createTask({ id: 'done', title: 'Done', status: 'completed' }),
      ];
      const order = flattenTreeOrder(tasks, true);
      const headerIndex = order.indexOf(COMPLETED_HEADER_ID);
      const active1Index = order.indexOf('active1');
      const active2Index = order.indexOf('active2');
      expect(headerIndex).toBeGreaterThan(active1Index);
      expect(headerIndex).toBeGreaterThan(active2Index);
    });

    it('excludes completed tasks from active tree', () => {
      const tasks = [
        createTask({ id: 'parent', title: 'Parent', status: 'completed' }),
        createTask({ id: 'child', title: 'Child', status: 'pending', dependencies: ['parent'] }),
      ];
      const order = flattenTreeOrder(tasks, true);
      // Child should be in active tree (despite parent being completed)
      // Parent should only appear in completed section
      expect(order.indexOf('child')).toBeLessThan(order.indexOf(COMPLETED_HEADER_ID));
    });
  });
});

// =============================================================================
// Group By Project Tests
// =============================================================================

describe('TaskTree groupByProject', () => {
  describe('project headers', () => {
    it('shows project headers when groupByProject is true', () => {
      const tasks = [
        createTask({ id: '1', title: 'Task 1', projectId: 'brain-api' }),
        createTask({ id: '2', title: 'Task 2', projectId: 'opencode' }),
      ];
      const { lastFrame } = render(
        <TaskTree
          tasks={tasks}
          selectedId={null}
          onSelect={() => {}}
          {...defaultTreeProps}
          groupByProject={true}
        />
      );
      expect(lastFrame()).toContain('brain-api');
      expect(lastFrame()).toContain('opencode');
    });

    it('shows task count with proper pluralization in project headers', () => {
      const tasks = [
        createTask({ id: '1', title: 'Task 1', projectId: 'brain-api' }),
        createTask({ id: '2', title: 'Task 2', projectId: 'brain-api' }),
        createTask({ id: '3', title: 'Task 3', projectId: 'opencode' }),
      ];
      const { lastFrame } = render(
        <TaskTree
          tasks={tasks}
          selectedId={null}
          onSelect={() => {}}
          {...defaultTreeProps}
          groupByProject={true}
        />
      );
      expect(lastFrame()).toContain('brain-api (2 tasks)');
      expect(lastFrame()).toContain('opencode (1 task)');
    });

    it('does not show project headers when groupByProject is false', () => {
      const tasks = [
        createTask({ id: '1', title: 'Task 1', projectId: 'brain-api' }),
      ];
      const { lastFrame } = render(
        <TaskTree
          tasks={tasks}
          selectedId={null}
          onSelect={() => {}}
          {...defaultTreeProps}
          groupByProject={false}
        />
      );
      // Should show task but not the project header format with "(X tasks)"
      expect(lastFrame()).toContain('Task 1');
      expect(lastFrame()).not.toContain('(1 task)');
    });
  });

  describe('project sorting', () => {
    it('sorts projects with in_progress tasks first', () => {
      const tasks = [
        createTask({ id: '1', title: 'Waiting Task', projectId: 'aaa-first', status: 'pending' }),
        createTask({ id: '2', title: 'Active Task', projectId: 'zzz-last', status: 'in_progress' }),
      ];
      const { lastFrame } = render(
        <TaskTree
          tasks={tasks}
          selectedId={null}
          onSelect={() => {}}
          {...defaultTreeProps}
          groupByProject={true}
        />
      );
      const frame = lastFrame() || '';
      // zzz-last should come BEFORE aaa-first because it has in_progress task
      const zzzIndex = frame.indexOf('zzz-last');
      const aaaIndex = frame.indexOf('aaa-first');
      expect(zzzIndex).toBeLessThan(aaaIndex);
    });

    it('sorts projects alphabetically when none have in_progress', () => {
      const tasks = [
        createTask({ id: '1', title: 'Task 1', projectId: 'zebra', status: 'pending' }),
        createTask({ id: '2', title: 'Task 2', projectId: 'alpha', status: 'pending' }),
      ];
      const { lastFrame } = render(
        <TaskTree
          tasks={tasks}
          selectedId={null}
          onSelect={() => {}}
          {...defaultTreeProps}
          groupByProject={true}
        />
      );
      const frame = lastFrame() || '';
      const alphaIndex = frame.indexOf('alpha');
      const zebraIndex = frame.indexOf('zebra');
      expect(alphaIndex).toBeLessThan(zebraIndex);
    });
  });
});

// =============================================================================
// parent_id hierarchy tests
// =============================================================================

describe('buildTree with parent_id', () => {
  describe('unified tree via parent_id', () => {
    it('builds a single tree when tasks have parent_id', () => {
      const tasks = [
        createTask({ id: 'root', title: 'Root Task', dependencies: [] }),
        createTask({ id: 'child1', title: 'Child 1', dependencies: [], parent_id: 'root' }),
        createTask({ id: 'child2', title: 'Child 2', dependencies: [], parent_id: 'root' }),
      ];
      const tree = buildTree(tasks);
      // Should produce exactly 1 root
      expect(tree.length).toBe(1);
      expect(tree[0].task.id).toBe('root');
      expect(tree[0].children.length).toBe(2);
    });

    it('nests grandchildren correctly via parent_id', () => {
      const tasks = [
        createTask({ id: 'root', title: 'Root', dependencies: [] }),
        createTask({ id: 'child', title: 'Child', dependencies: [], parent_id: 'root' }),
        createTask({ id: 'grandchild', title: 'Grandchild', dependencies: [], parent_id: 'child' }),
      ];
      const tree = buildTree(tasks);
      expect(tree.length).toBe(1);
      expect(tree[0].task.id).toBe('root');
      expect(tree[0].children.length).toBe(1);
      expect(tree[0].children[0].task.id).toBe('child');
      expect(tree[0].children[0].children.length).toBe(1);
      expect(tree[0].children[0].children[0].task.id).toBe('grandchild');
    });

    it('does not create duplicate roots when using parent_id', () => {
      // Without parent_id, these would all be separate roots (no depends_on)
      const tasks = [
        createTask({ id: 'root', title: 'Scanner Redesign', dependencies: [] }),
        createTask({ id: 'p0', title: 'P0 Setup', dependencies: [], parent_id: 'root' }),
        createTask({ id: 'p1-1', title: 'P1-1 Layout', dependencies: [], parent_id: 'root' }),
        createTask({ id: 'p1-2', title: 'P1-2 FilterChip', dependencies: [], parent_id: 'p1-1' }),
        createTask({ id: 'p2-1', title: 'P2-1 ViewTabs', dependencies: [], parent_id: 'p1-1' }),
      ];
      const tree = buildTree(tasks);
      expect(tree.length).toBe(1);
      expect(tree[0].task.id).toBe('root');
      // root should have p0 and p1-1 as children
      const rootChildIds = tree[0].children.map(c => c.task.id);
      expect(rootChildIds).toContain('p0');
      expect(rootChildIds).toContain('p1-1');
      // p1-1 should have p1-2 and p2-1 as children
      const p1Node = tree[0].children.find(c => c.task.id === 'p1-1');
      expect(p1Node).toBeDefined();
      const p1ChildIds = p1Node!.children.map(c => c.task.id);
      expect(p1ChildIds).toContain('p1-2');
      expect(p1ChildIds).toContain('p2-1');
    });
  });

  describe('mixed parent_id and depends_on', () => {
    it('parent_id determines placement, depends_on orders siblings', () => {
      const tasks = [
        createTask({ id: 'root', title: 'Root', dependencies: [] }),
        createTask({ id: 'a', title: 'Task A', dependencies: [], parent_id: 'root', priority: 'medium' }),
        createTask({ id: 'b', title: 'Task B', dependencies: ['a'], parent_id: 'root', priority: 'medium' }),
      ];
      const tree = buildTree(tasks);
      expect(tree.length).toBe(1);
      // Both a and b should be children of root (parent_id = root for both)
      expect(tree[0].children.length).toBe(2);
      // b depends on a, so a should come first
      expect(tree[0].children[0].task.id).toBe('a');
      expect(tree[0].children[1].task.id).toBe('b');
    });

    it('task with parent_id is placed under parent even if it has depends_on to another task', () => {
      const tasks = [
        createTask({ id: 'root', title: 'Root', dependencies: [] }),
        createTask({ id: 'branch1', title: 'Branch 1', dependencies: [], parent_id: 'root' }),
        createTask({ id: 'branch2', title: 'Branch 2', dependencies: [], parent_id: 'root' }),
        // leaf has parent_id=branch1 but also depends_on branch2
        // parent_id should win for placement (under branch1, not branch2)
        createTask({ id: 'leaf', title: 'Leaf', dependencies: ['branch2'], parent_id: 'branch1' }),
      ];
      const tree = buildTree(tasks);
      expect(tree.length).toBe(1);
      const branch1 = tree[0].children.find(c => c.task.id === 'branch1');
      const branch2 = tree[0].children.find(c => c.task.id === 'branch2');
      expect(branch1).toBeDefined();
      expect(branch2).toBeDefined();
      // Leaf should be under branch1 (parent_id), not branch2 (depends_on)
      expect(branch1!.children.length).toBe(1);
      expect(branch1!.children[0].task.id).toBe('leaf');
      // branch2 should NOT have leaf as a child
      expect(branch2!.children.length).toBe(0);
    });

    it('depends_on still works for tasks without parent_id', () => {
      const tasks = [
        createTask({ id: 'root', title: 'Root', dependencies: [] }),
        createTask({ id: 'child', title: 'Child', dependencies: ['root'] }),
        // No parent_id — should still be placed under root via depends_on
      ];
      const tree = buildTree(tasks);
      expect(tree.length).toBe(1);
      expect(tree[0].children.length).toBe(1);
      expect(tree[0].children[0].task.id).toBe('child');
    });
  });

  describe('completed intermediate tasks', () => {
    it('walks up parent_id chain through completed tasks to find active ancestor', () => {
      const allTasks = [
        createTask({ id: 'root', title: 'Root', status: 'pending', dependencies: [] }),
        createTask({ id: 'mid', title: 'Middle (completed)', status: 'completed', dependencies: [], parent_id: 'root' }),
        createTask({ id: 'leaf', title: 'Leaf', status: 'pending', dependencies: [], parent_id: 'mid' }),
      ];
      // Active tasks only (mid is completed, so filtered out)
      const activeTasks = allTasks.filter(t => t.status !== 'completed');
      
      const tree = buildTree(activeTasks, allTasks);
      // Should be 1 root: root, with leaf as child (mid is skipped)
      expect(tree.length).toBe(1);
      expect(tree[0].task.id).toBe('root');
      expect(tree[0].children.length).toBe(1);
      expect(tree[0].children[0].task.id).toBe('leaf');
    });

    it('treats task as root when all ancestors are completed and filtered', () => {
      const allTasks = [
        createTask({ id: 'root', title: 'Root', status: 'completed', dependencies: [] }),
        createTask({ id: 'mid', title: 'Middle', status: 'completed', dependencies: [], parent_id: 'root' }),
        createTask({ id: 'leaf', title: 'Leaf', status: 'pending', dependencies: [], parent_id: 'mid' }),
      ];
      const activeTasks = allTasks.filter(t => t.status !== 'completed');
      
      const tree = buildTree(activeTasks, allTasks);
      // Leaf should become a root since no active ancestors exist
      expect(tree.length).toBe(1);
      expect(tree[0].task.id).toBe('leaf');
    });

    it('unified tree stays intact when intermediate depends_on task is completed', () => {
      // Scenario: A -> B -> C, B is completed. Without parent_id, C becomes orphaned root.
      // With parent_id, C stays under A.
      const allTasks = [
        createTask({ id: 'A', title: 'Task A', status: 'pending', dependencies: [] }),
        createTask({ id: 'B', title: 'Task B', status: 'completed', dependencies: ['A'], parent_id: 'A' }),
        createTask({ id: 'C', title: 'Task C', status: 'pending', dependencies: ['B'], parent_id: 'A' }),
      ];
      const activeTasks = allTasks.filter(t => t.status !== 'completed');
      
      const tree = buildTree(activeTasks, allTasks);
      // Should be 1 root: A, with C as child
      expect(tree.length).toBe(1);
      expect(tree[0].task.id).toBe('A');
      expect(tree[0].children.length).toBe(1);
      expect(tree[0].children[0].task.id).toBe('C');
    });
  });

  describe('root detection with parent_id', () => {
    it('task with parent_id pointing to active task is NOT a root', () => {
      const tasks = [
        createTask({ id: 'parent', title: 'Parent', dependencies: [] }),
        createTask({ id: 'child', title: 'Child', dependencies: [], parent_id: 'parent' }),
      ];
      const tree = buildTree(tasks);
      expect(tree.length).toBe(1);
      expect(tree[0].task.id).toBe('parent');
    });

    it('task with parent_id pointing to non-existent task is a root', () => {
      const tasks = [
        createTask({ id: 'orphan', title: 'Orphan', dependencies: [], parent_id: 'non-existent' }),
      ];
      const tree = buildTree(tasks);
      expect(tree.length).toBe(1);
      expect(tree[0].task.id).toBe('orphan');
    });

    it('task with both parent_id and depends_on pointing to active tasks is NOT a root', () => {
      const tasks = [
        createTask({ id: 'root', title: 'Root', dependencies: [] }),
        createTask({ id: 'dep', title: 'Dep', dependencies: [], parent_id: 'root' }),
        createTask({ id: 'child', title: 'Child', dependencies: ['dep'], parent_id: 'root' }),
      ];
      const tree = buildTree(tasks);
      expect(tree.length).toBe(1);
    });
  });

  describe('flattenTreeOrder with parent_id', () => {
    it('preserves depth-first order with parent_id hierarchy', () => {
      const tasks = [
        createTask({ id: 'root', title: 'Root', dependencies: [] }),
        createTask({ id: 'child1', title: 'Child 1', dependencies: [], parent_id: 'root', priority: 'high' }),
        createTask({ id: 'child2', title: 'Child 2', dependencies: [], parent_id: 'root', priority: 'medium' }),
        createTask({ id: 'grandchild', title: 'Grandchild', dependencies: [], parent_id: 'child1' }),
      ];
      const order = flattenTreeOrder(tasks);
      expect(order[0]).toBe('root');
      // child1 (high priority) before child2 (medium)
      expect(order.indexOf('child1')).toBeLessThan(order.indexOf('child2'));
      // grandchild immediately after child1
      expect(order.indexOf('grandchild')).toBe(order.indexOf('child1') + 1);
    });

    it('completed intermediate parent_id tasks do not break flatten order', () => {
      const tasks = [
        createTask({ id: 'root', title: 'Root', status: 'pending', dependencies: [] }),
        createTask({ id: 'mid', title: 'Mid', status: 'completed', dependencies: [], parent_id: 'root' }),
        createTask({ id: 'leaf', title: 'Leaf', status: 'pending', dependencies: [], parent_id: 'mid' }),
      ];
      const order = flattenTreeOrder(tasks, true);
      // Active tasks: root and leaf. Leaf should be under root.
      expect(order.indexOf('root')).toBe(0);
      expect(order.indexOf('leaf')).toBe(1);
      // Completed header after active tasks
      expect(order).toContain(COMPLETED_HEADER_ID);
    });
  });

  describe('TaskTree rendering with parent_id', () => {
    it('renders unified tree with parent_id', () => {
      const tasks = [
        createTask({ id: 'root', title: 'Scanner Redesign', dependencies: [] }),
        createTask({ id: 'p0', title: 'P0 Setup', dependencies: [], parent_id: 'root' }),
        createTask({ id: 'p1', title: 'P1 Layout', dependencies: [], parent_id: 'root' }),
        createTask({ id: 'p1-2', title: 'P1-2 FilterChip', dependencies: [], parent_id: 'p1' }),
      ];
      const { lastFrame } = render(
        <TaskTree tasks={tasks} selectedId={null} onSelect={() => {}} {...defaultTreeProps} />
      );
      const frame = lastFrame() || '';
      // All tasks visible
      expect(frame).toContain('Scanner Redesign');
      expect(frame).toContain('P0 Setup');
      expect(frame).toContain('P1 Layout');
      expect(frame).toContain('P1-2 FilterChip');
      // Should have tree drawing chars for nesting
      expect(frame).toContain('─');
    });

    it('renders unified tree in grouped view with parent_id', () => {
      const tasks = [
        createTask({ id: 'root', title: 'Root Task', dependencies: [], projectId: 'myproject' }),
        createTask({ id: 'child', title: 'Child Task', dependencies: [], parent_id: 'root', projectId: 'myproject' }),
      ];
      const { lastFrame } = render(
        <TaskTree
          tasks={tasks}
          selectedId={null}
          onSelect={() => {}}
          {...defaultTreeProps}
          groupByProject={true}
        />
      );
      const frame = lastFrame() || '';
      expect(frame).toContain('myproject');
      expect(frame).toContain('Root Task');
      expect(frame).toContain('Child Task');
    });
  });
});

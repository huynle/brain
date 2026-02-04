/**
 * Task dependency tree component
 * 
 * Renders tasks as a tree with proper hierarchy, box-drawing characters,
 * and visual indicators for status, priority, and selection.
 */

import React, { useMemo } from 'react';
import { Box, Text } from 'ink';
import type { TaskDisplay } from '../types';
import type { EntryStatus, Priority } from '../../../core/types';

interface TaskTreeProps {
  tasks: TaskDisplay[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  completedCollapsed: boolean;
  onToggleCompleted: () => void;
}

// Special ID for the completed section header (used for navigation)
export const COMPLETED_HEADER_ID = '__completed_header__';

// Status symbols per spec
const STATUS_ICONS: Record<string, string> = {
  pending: '○',       // Waiting (yellow when ready)
  in_progress: '▶',   // In Progress (blue)
  completed: '✓',     // Completed (green dim)
  blocked: '✗',       // Blocked (red)
  cancelled: '⊘',     // Cancelled (yellow)
  draft: '○',
  active: '●',        // Ready (green)
  validated: '✓',
  superseded: '○',
  archived: '○',
};

const STATUS_COLORS: Record<string, string> = {
  pending: 'yellow',  // Ready tasks are yellow
  in_progress: 'blue',
  completed: 'green',
  blocked: 'red',
  cancelled: 'yellow',
  draft: 'gray',
  active: 'green',
  validated: 'green',
  superseded: 'gray',
  archived: 'gray',
};

const PRIORITY_ORDER: Record<Priority, number> = {
  high: 0,
  medium: 1,
  low: 2,
};

const STATUS_ORDER: Record<EntryStatus, number> = {
  in_progress: 0,
  pending: 1,
  blocked: 2,
  cancelled: 3,
  completed: 4,
  draft: 5,
  active: 6,
  validated: 7,
  superseded: 8,
  archived: 9,
};

// Tree node representation
export interface TreeNode {
  task: TaskDisplay;
  children: TreeNode[];
  inCycle: boolean;
}

// Box drawing characters
const BRANCH = '├─';
const LAST_BRANCH = '└─';
const VERTICAL = '│ ';
const EMPTY = '  ';

/**
 * Build tree structure from flat task list
 */
export function buildTree(tasks: TaskDisplay[]): TreeNode[] {
  // Create lookup map
  const taskMap = new Map<string, TaskDisplay>();
  tasks.forEach(t => taskMap.set(t.id, t));

  // Track which tasks are children (have a parent that exists in our list)
  const childIds = new Set<string>();
  
  // Build reverse dependency map (who depends on each task)
  const reverseDeps = new Map<string, string[]>();
  tasks.forEach(task => {
    task.dependencies.forEach(depId => {
      if (taskMap.has(depId)) {
        childIds.add(task.id);
        if (!reverseDeps.has(depId)) {
          reverseDeps.set(depId, []);
        }
        reverseDeps.get(depId)!.push(task.id);
      }
    });
  });

  // Detect cycles using DFS
  const inCycle = new Set<string>();
  const visited = new Set<string>();
  const recursionStack = new Set<string>();

  function detectCycles(taskId: string): boolean {
    visited.add(taskId);
    recursionStack.add(taskId);

    const children = reverseDeps.get(taskId) || [];
    for (const childId of children) {
      if (!visited.has(childId)) {
        if (detectCycles(childId)) {
          inCycle.add(taskId);
          return true;
        }
      } else if (recursionStack.has(childId)) {
        // Found cycle
        inCycle.add(taskId);
        inCycle.add(childId);
        return true;
      }
    }

    recursionStack.delete(taskId);
    return false;
  }

  // Run cycle detection from all nodes
  tasks.forEach(task => {
    if (!visited.has(task.id)) {
      detectCycles(task.id);
    }
  });

  // Track rendered tasks to handle diamond dependencies
  const rendered = new Set<string>();

  // Build tree recursively
  function buildNode(taskId: string): TreeNode | null {
    const task = taskMap.get(taskId);
    if (!task) return null;

    // Handle diamond dependencies - only render once
    if (rendered.has(taskId)) {
      return null;
    }
    rendered.add(taskId);

    const childIds = reverseDeps.get(taskId) || [];
    const children: TreeNode[] = [];

    // Sort children by priority then status
    const sortedChildIds = [...childIds].sort((a, b) => {
      const taskA = taskMap.get(a);
      const taskB = taskMap.get(b);
      if (!taskA || !taskB) return 0;

      const priorityDiff = 
        (PRIORITY_ORDER[taskA.priority] ?? 1) - (PRIORITY_ORDER[taskB.priority] ?? 1);
      if (priorityDiff !== 0) return priorityDiff;
      
      return (STATUS_ORDER[taskA.status] ?? 1) - (STATUS_ORDER[taskB.status] ?? 1);
    });

    for (const childId of sortedChildIds) {
      const childNode = buildNode(childId);
      if (childNode) {
        children.push(childNode);
      }
    }

    return {
      task,
      children,
      inCycle: inCycle.has(taskId),
    };
  }

  // Find root nodes (tasks with no dependencies, or deps outside our list)
  const roots: TreeNode[] = [];
  
  // Sort tasks for consistent ordering
  const sortedTasks = [...tasks].sort((a, b) => {
    const priorityDiff = 
      (PRIORITY_ORDER[a.priority] ?? 1) - (PRIORITY_ORDER[b.priority] ?? 1);
    if (priorityDiff !== 0) return priorityDiff;
    return (STATUS_ORDER[a.status] ?? 1) - (STATUS_ORDER[b.status] ?? 1);
  });

  for (const task of sortedTasks) {
    // Root if no dependencies or all dependencies are outside our task list
    const hasInternalDeps = task.dependencies.some(depId => taskMap.has(depId));
    if (!hasInternalDeps) {
      const node = buildNode(task.id);
      if (node) {
        roots.push(node);
      }
    }
  }

  // Handle orphan tasks (those with unresolved dependencies)
  for (const task of sortedTasks) {
    if (!rendered.has(task.id)) {
      const node = buildNode(task.id);
      if (node) {
        roots.push(node);
      }
    }
  }

  return roots;
}

// Helper to check if a task is completed
const isCompleted = (t: TaskDisplay): boolean => t.status === 'completed' || t.status === 'validated';

/**
 * Flatten tree into an array of task IDs in visual/navigation order.
 * This matches the order tasks appear on screen for j/k navigation.
 * 
 * @param tasks - All tasks
 * @param completedCollapsed - Whether the completed section is collapsed
 */
export function flattenTreeOrder(tasks: TaskDisplay[], completedCollapsed: boolean = true): string[] {
  // Separate active and completed tasks
  const activeTasks = tasks.filter(t => !isCompleted(t));
  const completedTasks = tasks.filter(isCompleted);
  
  // Build tree only from active tasks
  const tree = buildTree(activeTasks);
  const result: string[] = [];

  function traverse(nodes: TreeNode[]): void {
    for (const node of nodes) {
      result.push(node.task.id);
      if (node.children.length > 0) {
        traverse(node.children);
      }
    }
  }

  traverse(tree);
  
  // Add completed header and tasks if there are any completed tasks
  if (completedTasks.length > 0) {
    result.push(COMPLETED_HEADER_ID);
    
    // If expanded, add completed task IDs
    if (!completedCollapsed) {
      completedTasks.forEach(t => result.push(t.id));
    }
  }
  
  return result;
}

/**
 * Completed section header component
 */
function CompletedHeader({
  count,
  collapsed,
  isSelected,
}: {
  count: number;
  collapsed: boolean;
  isSelected: boolean;
}): React.ReactElement {
  const icon = collapsed ? '▶' : '▾';
  return (
    <Box>
      <Text
        color={isSelected ? 'white' : 'green'}
        backgroundColor={isSelected ? 'blue' : undefined}
        bold={isSelected}
        dimColor
      >
        {icon} Completed ({count})
      </Text>
    </Box>
  );
}

/**
 * Task row component
 */
function TaskRow({
  task,
  prefix,
  isSelected,
  inCycle,
  isReady,
}: {
  task: TaskDisplay;
  prefix: string;
  isSelected: boolean;
  inCycle: boolean;
  isReady: boolean;
}): React.ReactElement {
  // Determine icon and color based on status and readiness
  let icon = STATUS_ICONS[task.status] || '?';
  let color = STATUS_COLORS[task.status] || 'white';
  
  // Override for ready tasks (pending with no blocking deps)
  if (task.status === 'pending' && isReady) {
    icon = '●';  // Ready indicator
    color = 'green';
  }
  
  // Dim completed tasks
  const isDim = task.status === 'completed' || task.status === 'validated';
  
  // High priority indicator
  const prioritySuffix = task.priority === 'high' ? '!' : '';
  
  // Cycle indicator
  const cycleSuffix = inCycle ? ' ↺' : '';

  return (
    <Box>
      <Text dimColor={isDim}>{prefix}</Text>
      <Text
        color={color}
        dimColor={isDim}
        backgroundColor={isSelected ? 'blue' : undefined}
      >
        {icon}
      </Text>
      <Text
        color={isSelected ? 'white' : undefined}
        backgroundColor={isSelected ? 'blue' : undefined}
        bold={isSelected}
        dimColor={isDim}
      >
        {' '}{task.title}
      </Text>
      {prioritySuffix && (
        <Text color="red" bold>
          {prioritySuffix}
        </Text>
      )}
      {cycleSuffix && (
        <Text color="magenta">
          {cycleSuffix}
        </Text>
      )}
    </Box>
  );
}

/**
 * Render tree recursively
 */
function renderTree(
  nodes: TreeNode[],
  selectedId: string | null,
  prefix: string = '',
  readyIds: Set<string>,
): React.ReactElement[] {
  const elements: React.ReactElement[] = [];

  nodes.forEach((node, index) => {
    const isLast = index === nodes.length - 1;
    const branchChar = isLast ? LAST_BRANCH : BRANCH;
    const isSelected = node.task.id === selectedId;
    const isReady = readyIds.has(node.task.id);

    elements.push(
      <TaskRow
        key={node.task.id}
        task={node.task}
        prefix={prefix + branchChar}
        isSelected={isSelected}
        inCycle={node.inCycle}
        isReady={isReady}
      />
    );

    // Render children with appropriate prefix
    if (node.children.length > 0) {
      const childPrefix = prefix + (isLast ? EMPTY : VERTICAL);
      elements.push(
        ...renderTree(node.children, selectedId, childPrefix, readyIds)
      );
    }
  });

  return elements;
}

export function TaskTree({
  tasks,
  selectedId,
  completedCollapsed,
}: TaskTreeProps): React.ReactElement {
  // Separate active and completed tasks
  const activeTasks = useMemo(() => tasks.filter(t => !isCompleted(t)), [tasks]);
  const completedTasks = useMemo(() => tasks.filter(isCompleted), [tasks]);
  
  // Build tree structure from active tasks only
  const tree = useMemo(() => buildTree(activeTasks), [activeTasks]);

  // Compute ready task IDs (pending with all deps completed)
  const readyIds = useMemo(() => {
    const taskMap = new Map<string, TaskDisplay>();
    tasks.forEach(t => taskMap.set(t.id, t));

    const ready = new Set<string>();
    tasks.forEach(task => {
      if (task.status === 'pending') {
        const allDepsCompleted = task.dependencies.every(depId => {
          const dep = taskMap.get(depId);
          return !dep || dep.status === 'completed' || dep.status === 'validated';
        });
        if (allDepsCompleted) {
          ready.add(task.id);
        }
      }
    });
    return ready;
  }, [tasks]);

  if (tasks.length === 0) {
    return (
      <Box padding={1}>
        <Text dimColor>No tasks found</Text>
      </Box>
    );
  }

  // Render root nodes at top level (no prefix for roots)
  const elements: React.ReactElement[] = [];
  
  tree.forEach((rootNode, rootIndex) => {
    const isSelected = rootNode.task.id === selectedId;
    const isReady = readyIds.has(rootNode.task.id);

    // Root task (no tree prefix)
    elements.push(
      <TaskRow
        key={rootNode.task.id}
        task={rootNode.task}
        prefix=""
        isSelected={isSelected}
        inCycle={rootNode.inCycle}
        isReady={isReady}
      />
    );

    // Children of root
    if (rootNode.children.length > 0) {
      elements.push(
        ...renderTree(rootNode.children, selectedId, '', readyIds)
      );
    }

    // Add blank line between root chains (except after last)
    if (rootIndex < tree.length - 1) {
      elements.push(
        <Box key={`spacer-${rootIndex}`}>
          <Text> </Text>
        </Box>
      );
    }
  });

  // Add completed section if there are completed tasks
  const completedElements: React.ReactElement[] = [];
  if (completedTasks.length > 0) {
    // Add spacing before completed section
    completedElements.push(
      <Box key="completed-spacer">
        <Text> </Text>
      </Box>
    );
    
    // Completed header
    completedElements.push(
      <CompletedHeader
        key={COMPLETED_HEADER_ID}
        count={completedTasks.length}
        collapsed={completedCollapsed}
        isSelected={selectedId === COMPLETED_HEADER_ID}
      />
    );
    
    // Render completed tasks as flat list when expanded
    if (!completedCollapsed) {
      completedTasks.forEach(task => {
        completedElements.push(
          <TaskRow
            key={task.id}
            task={task}
            prefix="  "
            isSelected={task.id === selectedId}
            inCycle={false}
            isReady={false}
          />
        );
      });
    }
  }

  return (
    <Box flexDirection="column" padding={1}>
      <Text bold underline>
        Tasks ({tasks.length})
      </Text>
      <Box flexDirection="column" marginTop={1}>
        {elements}
        {completedElements}
      </Box>
    </Box>
  );
}

export default TaskTree;

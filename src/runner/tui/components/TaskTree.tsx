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
  /** When true, tasks are grouped by project ID (for multi-project "All" view) */
  groupByProject?: boolean;
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
 * Build tree structure from flat task list.
 * 
 * Uses BOTH parent_id (primary hierarchy) and depends_on (secondary ordering):
 * - parent_id determines WHERE a task appears in the tree (keeps tree unified)
 * - depends_on orders siblings and adds additional edges
 * - When a task has both, parent_id wins for placement
 * 
 * @param tasks - Active tasks to build tree from
 * @param allTasks - Optional full task list (including completed) for parent_id chain walking
 */
export function buildTree(tasks: TaskDisplay[], allTasks?: TaskDisplay[]): TreeNode[] {
  // Step 1: Create lookup map for active tasks
  const taskMap = new Map<string, TaskDisplay>();
  tasks.forEach(t => taskMap.set(t.id, t));

  // Also create a lookup for all tasks (for parent_id chain walking through completed tasks)
  const allTaskMap = new Map<string, TaskDisplay>();
  if (allTasks) {
    allTasks.forEach(t => allTaskMap.set(t.id, t));
  } else {
    tasks.forEach(t => allTaskMap.set(t.id, t));
  }

  // Step 2: Build TWO relationship maps
  
  // 2a: parentChildren - from parent_id field (primary hierarchy)
  const parentChildren = new Map<string, Set<string>>();
  // Track tasks that have an active parent via parent_id
  const hasParentInTree = new Set<string>();
  
  tasks.forEach(task => {
    if (task.parent_id) {
      // Walk up the parent_id chain to find the nearest active ancestor
      const activeAncestorId = findActiveAncestor(task.parent_id, taskMap, allTaskMap);
      if (activeAncestorId) {
        hasParentInTree.add(task.id);
        if (!parentChildren.has(activeAncestorId)) {
          parentChildren.set(activeAncestorId, new Set());
        }
        parentChildren.get(activeAncestorId)!.add(task.id);
      }
    }
  });

  // 2b: reverseDeps - from depends_on field (secondary ordering + additional edges)
  const reverseDeps = new Map<string, string[]>();
  // Track tasks that have active depends_on targets
  const hasDepsInTree = new Set<string>();
  
  tasks.forEach(task => {
    task.dependencies.forEach(depId => {
      if (taskMap.has(depId)) {
        hasDepsInTree.add(task.id);
        if (!reverseDeps.has(depId)) {
          reverseDeps.set(depId, []);
        }
        reverseDeps.get(depId)!.push(task.id);
      }
    });
  });

  // Step 3: Merge children - UNION of parent_id children and depends_on children
  // But: if a task has a parent_id pointing to an active task, parent_id wins for placement
  const mergedChildren = new Map<string, Set<string>>();
  
  // Start with parent_id children
  for (const [parentId, children] of parentChildren) {
    if (!mergedChildren.has(parentId)) {
      mergedChildren.set(parentId, new Set());
    }
    for (const childId of children) {
      mergedChildren.get(parentId)!.add(childId);
    }
  }
  
  // Add depends_on children, but only if the child doesn't already have a parent_id placement
  for (const [depId, dependents] of reverseDeps) {
    if (!mergedChildren.has(depId)) {
      mergedChildren.set(depId, new Set());
    }
    for (const dependentId of dependents) {
      // Only add via depends_on if this task doesn't have an active parent_id elsewhere
      if (!hasParentInTree.has(dependentId)) {
        mergedChildren.get(depId)!.add(dependentId);
      }
    }
  }

  // Detect cycles using DFS (on the merged children graph)
  const inCycle = new Set<string>();
  const visited = new Set<string>();
  const recursionStack = new Set<string>();

  function detectCycles(taskId: string): boolean {
    visited.add(taskId);
    recursionStack.add(taskId);

    const children = mergedChildren.get(taskId) || new Set();
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

  // Step 5: Sort children - use depends_on to order siblings within a parent
  function sortChildIds(childIdSet: Set<string>): string[] {
    const childIds = [...childIdSet];
    
    // Build a local dependency order among siblings:
    // If sibling A is in sibling B's depends_on, A should come before B
    return childIds.sort((a, b) => {
      const taskA = taskMap.get(a);
      const taskB = taskMap.get(b);
      if (!taskA || !taskB) return 0;

      // Check if B depends on A (A should come first)
      if (taskB.dependencies.includes(a)) return -1;
      // Check if A depends on B (B should come first)
      if (taskA.dependencies.includes(b)) return 1;

      // Fall back to priority then status ordering
      const priorityDiff = 
        (PRIORITY_ORDER[taskA.priority] ?? 1) - (PRIORITY_ORDER[taskB.priority] ?? 1);
      if (priorityDiff !== 0) return priorityDiff;
      
      return (STATUS_ORDER[taskA.status] ?? 1) - (STATUS_ORDER[taskB.status] ?? 1);
    });
  }

  // Build tree recursively
  function buildNode(taskId: string): TreeNode | null {
    const task = taskMap.get(taskId);
    if (!task) return null;

    // Handle diamond dependencies - only render once
    if (rendered.has(taskId)) {
      return null;
    }
    rendered.add(taskId);

    const childIdSet = mergedChildren.get(taskId) || new Set();
    const children: TreeNode[] = [];

    const sortedChildIds = sortChildIds(childIdSet);

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

  // Step 4: Root detection - a task is a root ONLY if:
  // - It has no parent_id pointing to an active task, AND
  // - It has no depends_on pointing to an active task
  const roots: TreeNode[] = [];
  
  // Sort tasks for consistent ordering
  const sortedTasks = [...tasks].sort((a, b) => {
    const priorityDiff = 
      (PRIORITY_ORDER[a.priority] ?? 1) - (PRIORITY_ORDER[b.priority] ?? 1);
    if (priorityDiff !== 0) return priorityDiff;
    return (STATUS_ORDER[a.status] ?? 1) - (STATUS_ORDER[b.status] ?? 1);
  });

  for (const task of sortedTasks) {
    const isChildViaParentId = hasParentInTree.has(task.id);
    const isChildViaDepsOn = task.dependencies.some(depId => taskMap.has(depId));
    
    // Root only if NOT a child via either mechanism
    if (!isChildViaParentId && !isChildViaDepsOn) {
      const node = buildNode(task.id);
      if (node) {
        roots.push(node);
      }
    }
  }

  // Handle orphan tasks (those with unresolved dependencies or cycles)
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

/**
 * Walk up the parent_id chain to find the nearest active ancestor.
 * Handles the case where a task's parent_id points to a completed (filtered-out) task.
 */
function findActiveAncestor(
  parentId: string,
  activeTaskMap: Map<string, TaskDisplay>,
  allTaskMap: Map<string, TaskDisplay>,
): string | null {
  const visited = new Set<string>();
  let currentId: string | null = parentId;
  
  while (currentId) {
    // Prevent infinite loops
    if (visited.has(currentId)) return null;
    visited.add(currentId);
    
    // If this ancestor is in the active task list, we found it
    if (activeTaskMap.has(currentId)) {
      return currentId;
    }
    
    // Otherwise, look up this task in the full list and walk to its parent
    const task = allTaskMap.get(currentId);
    if (!task || !task.parent_id) {
      return null; // No more parents to walk up to
    }
    currentId = task.parent_id;
  }
  
  return null;
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
  
  // Build tree from active tasks, passing all tasks for parent_id chain walking
  const tree = buildTree(activeTasks, tasks);
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
 * Completed section header component (memoized)
 */
const CompletedHeader = React.memo(function CompletedHeader({
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
});

/**
 * Task row component (memoized to prevent unnecessary re-renders)
 */
const TaskRow = React.memo(function TaskRow({
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
});

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

/**
 * Project header component for grouped view (memoized)
 */
const ProjectHeader = React.memo(function ProjectHeader({
  projectId,
  taskCount,
}: {
  projectId: string;
  taskCount: number;
}): React.ReactElement {
  return (
    <Box marginTop={1}>
      <Text bold color="cyan">
        {projectId} ({taskCount} {taskCount === 1 ? 'task' : 'tasks'})
      </Text>
    </Box>
  );
});

/**
 * Render a single project's tasks as a tree
 */
function renderProjectTasks(
  projectTasks: TaskDisplay[],
  selectedId: string | null,
  readyIds: Set<string>,
): React.ReactElement[] {
  const elements: React.ReactElement[] = [];
  
  // Separate active and completed tasks
  const activeTasks = projectTasks.filter(t => !isCompleted(t));
  
  // Build tree structure from active tasks, passing all project tasks for parent_id chain walking
  const tree = buildTree(activeTasks, projectTasks);
  
  tree.forEach((rootNode, rootIndex) => {
    const isSelected = rootNode.task.id === selectedId;
    const isReady = readyIds.has(rootNode.task.id);

    // Root task with indent for project grouping
    elements.push(
      <TaskRow
        key={rootNode.task.id}
        task={rootNode.task}
        prefix="  "
        isSelected={isSelected}
        inCycle={rootNode.inCycle}
        isReady={isReady}
      />
    );

    // Children of root
    if (rootNode.children.length > 0) {
      elements.push(
        ...renderTree(rootNode.children, selectedId, '  ', readyIds)
      );
    }
  });
  
  return elements;
}

export const TaskTree = React.memo(function TaskTree({
  tasks,
  selectedId,
  completedCollapsed,
  groupByProject = false,
}: TaskTreeProps): React.ReactElement {
  // Separate active and completed tasks
  const activeTasks = useMemo(() => tasks.filter(t => !isCompleted(t)), [tasks]);
  const completedTasks = useMemo(() => tasks.filter(isCompleted), [tasks]);
  
  // Build tree structure from active tasks, passing all tasks for parent_id chain walking
  const tree = useMemo(() => buildTree(activeTasks, tasks), [activeTasks, tasks]);

  // Group tasks by project (for grouped view)
  const tasksByProject = useMemo(() => {
    if (!groupByProject) return null;
    const grouped = new Map<string, TaskDisplay[]>();
    for (const task of tasks) {
      const projectId = task.projectId || 'unknown';
      if (!grouped.has(projectId)) {
        grouped.set(projectId, []);
      }
      grouped.get(projectId)!.push(task);
    }
    return grouped;
  }, [tasks, groupByProject]);

  // Compute ready task IDs (pending with all deps completed)
  const readyIds = useMemo(() => {
    const taskMap = new Map<string, TaskDisplay>();
    tasks.forEach(t => taskMap.set(t.id, t));

    const ready = new Set<string>();
    tasks.forEach(task => {
      if (task.status === 'pending') {
        const allDepsCompleted = task.dependencies.every(depId => {
          const dep = taskMap.get(depId);
          // active = non-blocking container (project root), completed/validated = done
          return !dep || dep.status === 'completed' || dep.status === 'validated' || dep.status === 'active';
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

  // Grouped view by project
  if (groupByProject && tasksByProject) {
    const projectElements: React.ReactElement[] = [];
    
    // Sort projects: those with in_progress tasks first, then alphabetically
    const projectIds = Array.from(tasksByProject.keys()).sort((a, b) => {
      const tasksA = tasksByProject.get(a) || [];
      const tasksB = tasksByProject.get(b) || [];
      const hasInProgressA = tasksA.some(t => t.status === 'in_progress');
      const hasInProgressB = tasksB.some(t => t.status === 'in_progress');
      
      if (hasInProgressA && !hasInProgressB) return -1;
      if (!hasInProgressA && hasInProgressB) return 1;
      return a.localeCompare(b);
    });
    
    projectIds.forEach((projectId, projectIndex) => {
      const projectTasks = tasksByProject.get(projectId) || [];
      const projectActiveTasks = projectTasks.filter(t => !isCompleted(t));
      
      // Skip projects with no active tasks in grouped view
      if (projectActiveTasks.length === 0) return;
      
      // Project header
      projectElements.push(
        <ProjectHeader
          key={`project-header-${projectId}`}
          projectId={projectId}
          taskCount={projectActiveTasks.length}
        />
      );
      
      // Project's tasks
      projectElements.push(
        ...renderProjectTasks(projectTasks, selectedId, readyIds)
      );
      
      // Add spacing between projects (except last)
      if (projectIndex < projectIds.length - 1) {
        projectElements.push(
          <Box key={`project-spacer-${projectId}`}>
            <Text> </Text>
          </Box>
        );
      }
    });

    // Add completed section if there are completed tasks
    const completedElements: React.ReactElement[] = [];
    if (completedTasks.length > 0) {
      completedElements.push(
        <Box key="completed-spacer">
          <Text> </Text>
        </Box>
      );
      
      completedElements.push(
        <CompletedHeader
          key={COMPLETED_HEADER_ID}
          count={completedTasks.length}
          collapsed={completedCollapsed}
          isSelected={selectedId === COMPLETED_HEADER_ID}
        />
      );
      
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
          {projectElements}
          {completedElements}
        </Box>
      </Box>
    );
  }

  // Standard view (non-grouped)
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
});

export default TaskTree;

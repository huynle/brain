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
import { getStatusIcon, getStatusColor, READY_ICON } from '../status-display';

interface TaskTreeProps {
  tasks: TaskDisplay[];
  selectedId: string | null;
  onSelect: (id: string) => void;
  completedCollapsed: boolean;
  onToggleCompleted: () => void;
  /** When true, draft section is collapsed */
  draftCollapsed?: boolean;
  /** Callback to toggle draft section collapsed state */
  onToggleDraft?: () => void;
  /** When true, tasks are grouped by project ID (for multi-project "All" view) */
  groupByProject?: boolean;
  /** When true, tasks are grouped by feature_id */
  groupByFeature?: boolean;
  /** Scroll offset for viewport (0 = top of list visible) */
  scrollOffset?: number;
  /** Number of visible rows in the viewport (for virtual scrolling) */
  viewportHeight?: number;
  /** Set of feature IDs that are collapsed */
  collapsedFeatures?: Set<string>;
  /** Set of feature IDs that are paused */
  pausedFeatures?: Set<string>;
}

// Special ID for the completed section header (used for navigation)
export const COMPLETED_HEADER_ID = '__completed_header__';

// Special ID for the draft section header (used for navigation)
export const DRAFT_HEADER_ID = '__draft_header__';

// Special ID prefix for feature headers (used for navigation)
export const FEATURE_HEADER_PREFIX = '__feature_header__';

// Alias for backwards compatibility
export const GROUP_HEADER_PREFIX = FEATURE_HEADER_PREFIX;

// Special ID prefix for spacer elements (non-navigable, for visual index alignment)
export const SPACER_PREFIX = '__spacer__';

// Status icons and colors are now imported from shared status-display.ts

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

// Helper to check if a task is a draft
const isDraft = (t: TaskDisplay): boolean => t.status === 'draft';

/**
 * Flatten tree into an array of task IDs in visual/navigation order.
 * This matches the order tasks appear on screen for j/k navigation.
 * 
 * @param tasks - All tasks
 * @param completedCollapsed - Whether the completed section is collapsed
 * @param draftCollapsed - Whether the draft section is collapsed
 */
export function flattenTreeOrder(tasks: TaskDisplay[], completedCollapsed: boolean = true, draftCollapsed: boolean = true): string[] {
  // Separate active, completed, and draft tasks
  const activeTasks = tasks.filter(t => !isCompleted(t) && !isDraft(t));
  const completedTasks = tasks.filter(isCompleted).sort((a, b) => {
    // Sort by modified time descending (most recent first)
    const aTime = a.modified ? new Date(a.modified).getTime() : 0;
    const bTime = b.modified ? new Date(b.modified).getTime() : 0;
    return bTime - aTime;
  });
  const draftTasks = tasks.filter(isDraft);
  
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
  
  // Add draft header and tasks if there are any draft tasks
  if (draftTasks.length > 0) {
    result.push(DRAFT_HEADER_ID);
    
    // If expanded, add draft task IDs
    if (!draftCollapsed) {
      draftTasks.forEach(t => result.push(t.id));
    }
  }
  
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
 * Flatten feature-grouped tree into an array of IDs in visual/navigation order.
 * Includes feature header IDs for navigation.
 * 
 * @param tasks - All tasks
 * @param completedCollapsed - Whether the completed section is collapsed
 * @param draftCollapsed - Whether the draft section is collapsed
 * @param collapsedFeatures - Set of feature IDs that are collapsed
 */
export function flattenFeatureOrder(tasks: TaskDisplay[], completedCollapsed: boolean = true, draftCollapsed: boolean = true, collapsedFeatures: Set<string> = new Set()): string[] {
  const result: string[] = [];
  
  // Separate active, completed, and draft tasks
  const activeTasks = tasks.filter(t => !isCompleted(t) && !isDraft(t));
  const completedTasks = tasks.filter(isCompleted).sort((a, b) => {
    // Sort by modified time descending (most recent first)
    const aTime = a.modified ? new Date(a.modified).getTime() : 0;
    const bTime = b.modified ? new Date(b.modified).getTime() : 0;
    return bTime - aTime;
  });
  const draftTasks = tasks.filter(isDraft);
  
  // Group tasks by feature
  const tasksByFeature = new Map<string, TaskDisplay[]>();
  const ungrouped: TaskDisplay[] = [];
  
  for (const task of tasks) {
    if (task.feature_id) {
      if (!tasksByFeature.has(task.feature_id)) {
        tasksByFeature.set(task.feature_id, []);
      }
      tasksByFeature.get(task.feature_id)!.push(task);
    } else {
      ungrouped.push(task);
    }
  }
  
  // Compute feature priorities for sorting
  const featurePriorities = new Map<string, number>();
  for (const [featureId, featureTasks] of tasksByFeature) {
    const firstTaskWithPriority = featureTasks.find(t => t.feature_priority);
    if (firstTaskWithPriority?.feature_priority) {
      featurePriorities.set(featureId, PRIORITY_ORDER[firstTaskWithPriority.feature_priority] ?? 1);
    } else {
      const minPriority = Math.min(...featureTasks.map(t => PRIORITY_ORDER[t.priority] ?? 1));
      featurePriorities.set(featureId, minPriority);
    }
  }
  
  // Collect feature dependencies for ordering
  const featureDeps = new Map<string, string[]>();
  for (const [featureId, featureTasks] of tasksByFeature) {
    const deps = new Set<string>();
    for (const task of featureTasks) {
      if (task.feature_depends_on) {
        task.feature_depends_on.forEach(dep => deps.add(dep));
      }
    }
    featureDeps.set(featureId, [...deps]);
  }
  
  // Sort features: by priority, then dependency order, then alphabetically
  const featureIds = Array.from(tasksByFeature.keys()).sort((a, b) => {
    const priorityA = featurePriorities.get(a) ?? 1;
    const priorityB = featurePriorities.get(b) ?? 1;
    
    if (priorityA !== priorityB) return priorityA - priorityB;
    
    const depsA = featureDeps.get(a) || [];
    const depsB = featureDeps.get(b) || [];
    
    if (depsB.includes(a)) return -1;
    if (depsA.includes(b)) return 1;
    
    return a.localeCompare(b);
  });
  
  // Track which features have active tasks (to match render spacer logic)
  const activeFeatureIds: string[] = [];
  for (const featureId of featureIds) {
    const featureTasks = tasksByFeature.get(featureId) || [];
    const activeFeatureTasks = featureTasks.filter(t => !isCompleted(t));
    if (activeFeatureTasks.length > 0) {
      activeFeatureIds.push(featureId);
    }
  }
  
  let spacerIndex = 0;
  
  // Add feature headers and tasks
  activeFeatureIds.forEach((featureId, featureIndex) => {
    const featureTasks = tasksByFeature.get(featureId) || [];
    const activeFeatureTasks = featureTasks.filter(t => !isCompleted(t));
    const isFeatureCollapsed = collapsedFeatures.has(featureId);
    
    // Add feature header
    result.push(`${FEATURE_HEADER_PREFIX}${featureId}`);
    
    // Only add tasks if feature is not collapsed
    if (!isFeatureCollapsed) {
      // Build tree for this feature's tasks
      const tree = buildTree(activeFeatureTasks, featureTasks);
      
      function traverse(nodes: TreeNode[]): void {
        for (const node of nodes) {
          result.push(node.task.id);
          if (node.children.length > 0) {
            traverse(node.children);
          }
        }
      }
      
      traverse(tree);
    }
    
    // Add spacer after feature (except last) - matches render logic
    if (featureIndex < activeFeatureIds.length - 1) {
      result.push(`${SPACER_PREFIX}${spacerIndex++}`);
    }
  });
  
  // Add ungrouped tasks
  // Include drafts in ungrouped navigation - they're rendered in the ungrouped section
  // (Draft section at bottom shows ALL drafts, but ungrouped section also displays drafts inline)
  const ungroupedActive = ungrouped.filter(t => !isCompleted(t));
  if (ungroupedActive.length > 0) {
    // Add spacer before ungrouped section if there are features
    if (activeFeatureIds.length > 0) {
      result.push(`${SPACER_PREFIX}${spacerIndex++}`);
    }
    
    // Add ungrouped header placeholder (matches render's <Box key="ungrouped-header">)
    result.push(`${SPACER_PREFIX}ungrouped_header`);
    
    const tree = buildTree(ungroupedActive, ungrouped);
    
    function traverse(nodes: TreeNode[]): void {
      for (const node of nodes) {
        result.push(node.task.id);
        if (node.children.length > 0) {
          traverse(node.children);
        }
      }
    }
    
    traverse(tree);
  }
  
  // Add draft section
  if (draftTasks.length > 0) {
    // Spacer before draft section
    result.push(`${SPACER_PREFIX}${spacerIndex++}`);
    result.push(DRAFT_HEADER_ID);
    
    if (!draftCollapsed) {
      draftTasks.forEach(t => result.push(t.id));
    }
  }
  
  // Add completed section
  if (completedTasks.length > 0) {
    // Spacer before completed section
    result.push(`${SPACER_PREFIX}${spacerIndex++}`);
    result.push(COMPLETED_HEADER_ID);
    
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
        dimColor={!isSelected}
      >
        {icon} Completed ({count})
      </Text>
    </Box>
  );
});

/**
 * Draft section header component (memoized)
 */
const DraftHeader = React.memo(function DraftHeader({
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
        color={isSelected ? 'white' : 'gray'}
        backgroundColor={isSelected ? 'blue' : undefined}
        bold={isSelected}
        dimColor={!isSelected}
      >
        {icon} Draft ({count})
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
  const icon = getStatusIcon(task.status, isReady);
  const color = getStatusColor(task.status, isReady);
  
  // Dim completed tasks
  const isDim = task.status === 'completed' || task.status === 'validated';
  
  // High priority indicator
  const prioritySuffix = task.priority === 'high' ? '!' : '';
  
  // Cycle indicator
  const cycleSuffix = inCycle ? ' ↺' : '';

  return (
    <Box flexDirection="row">
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
        <Text
          color="red"
          bold
          backgroundColor={isSelected ? 'blue' : undefined}
        >
          {prioritySuffix}
        </Text>
      )}
      {cycleSuffix && (
        <Text
          color="magenta"
          backgroundColor={isSelected ? 'blue' : undefined}
        >
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
 * Feature header component for feature grouping (memoized)
 * Shows: feature name, completion stats, status indicator, collapse indicator, pause indicator
 */
const FeatureHeader = React.memo(function FeatureHeader({
  featureId,
  completed,
  total,
  status,
  blockedBy,
  isSelected,
  isCollapsed,
  isPaused,
}: {
  featureId: string;
  completed: number;
  total: number;
  status: 'ready' | 'waiting' | 'blocked' | 'in_progress' | 'completed';
  blockedBy?: string[];
  isSelected?: boolean;
  isCollapsed?: boolean;
  isPaused?: boolean;
}): React.ReactElement {
  // Collapse indicator
  const collapseIcon = isCollapsed ? '▶' : '▾';
  
  // Status indicator and color
  let icon: string;
  let color: string;
  
  switch (status) {
    case 'completed':
      icon = '✓';
      color = 'green';
      break;
    case 'in_progress':
      icon = '▶';
      color = 'blue';
      break;
    case 'blocked':
      icon = '✗';
      color = 'red';
      break;
    case 'waiting':
      icon = '◌';
      color = 'yellow';
      break;
    case 'ready':
    default:
      icon = '●';
      color = 'green';
      break;
  }
  
  // Build the header text
  const statsText = `[${completed}/${total} complete]`;
  const blockedText = blockedBy && blockedBy.length > 0 
    ? ` [waiting on: ${blockedBy.join(', ')}]`
    : '';

  return (
    <Box marginTop={1} flexDirection="row">
      <Text
        color={isSelected ? 'white' : 'gray'}
        backgroundColor={isSelected ? 'blue' : undefined}
      >
        {collapseIcon}
      </Text>
      <Text
        color={isSelected ? 'white' : color}
        backgroundColor={isSelected ? 'blue' : undefined}
        bold
      >
        {icon}
      </Text>
      <Text
        color={isSelected ? 'white' : 'cyan'}
        backgroundColor={isSelected ? 'blue' : undefined}
        bold
      >
        {' '}Feature: {featureId}
      </Text>
      {isPaused && (
        <Text
          color={isSelected ? 'white' : 'yellow'}
          backgroundColor={isSelected ? 'blue' : undefined}
        >
          {' '}⏸
        </Text>
      )}
      <Text
        color={isSelected ? 'white' : 'gray'}
        backgroundColor={isSelected ? 'blue' : undefined}
      >
        {' '}{statsText}
      </Text>
      {blockedText && (
        <Text
          color={isSelected ? 'white' : 'yellow'}
          backgroundColor={isSelected ? 'blue' : undefined}
          dimColor
        >
          {blockedText}
        </Text>
      )}
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

/**
 * Render a single feature's tasks as a tree
 */
function renderFeatureTasks(
  featureTasks: TaskDisplay[],
  selectedId: string | null,
  readyIds: Set<string>,
): React.ReactElement[] {
  const elements: React.ReactElement[] = [];
  
  // Separate active and completed tasks
  const activeTasks = featureTasks.filter(t => !isCompleted(t));
  
  // Build tree structure from active tasks, passing all feature tasks for parent_id chain walking
  const tree = buildTree(activeTasks, featureTasks);
  
  tree.forEach((rootNode) => {
    const isSelected = rootNode.task.id === selectedId;
    const isReady = readyIds.has(rootNode.task.id);

    // Root task with indent for feature grouping
    elements.push(
      <TaskRow
        key={rootNode.task.id}
        task={rootNode.task}
        prefix="├─"
        isSelected={isSelected}
        inCycle={rootNode.inCycle}
        isReady={isReady}
      />
    );

    // Children of root
    if (rootNode.children.length > 0) {
      elements.push(
        ...renderTree(rootNode.children, selectedId, '│ ', readyIds)
      );
    }
  });
  
  // Update last task prefix to use └─ instead of ├─
  if (elements.length > 0) {
    const lastElement = elements[elements.length - 1];
    if (lastElement && React.isValidElement(lastElement)) {
      const props = lastElement.props as { prefix?: string };
      if (props.prefix === '├─') {
        const task = (lastElement.props as { task: TaskDisplay }).task;
        const isSelected = (lastElement.props as { isSelected: boolean }).isSelected;
        const inCycle = (lastElement.props as { inCycle: boolean }).inCycle;
        const isReady = (lastElement.props as { isReady: boolean }).isReady;
        elements[elements.length - 1] = (
          <TaskRow
            key={task.id}
            task={task}
            prefix="└─"
            isSelected={isSelected}
            inCycle={inCycle}
            isReady={isReady}
          />
        );
      }
    }
  }
  
  return elements;
}

export const TaskTree = React.memo(function TaskTree({
  tasks,
  selectedId,
  completedCollapsed,
  draftCollapsed = true,
  groupByProject = false,
  groupByFeature = false,
  scrollOffset = 0,
  viewportHeight,
  collapsedFeatures = new Set(),
  pausedFeatures = new Set(),
}: TaskTreeProps): React.ReactElement {
  // Separate active, completed, and draft tasks
  const activeTasks = useMemo(() => tasks.filter(t => !isCompleted(t) && !isDraft(t)), [tasks]);
  const completedTasks = useMemo(() => 
    tasks.filter(isCompleted).sort((a, b) => {
      // Sort by modified time descending (most recent first)
      const aTime = a.modified ? new Date(a.modified).getTime() : 0;
      const bTime = b.modified ? new Date(b.modified).getTime() : 0;
      return bTime - aTime;
    }), [tasks]);
  const draftTasks = useMemo(() => tasks.filter(isDraft), [tasks]);
  
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

  // Group tasks by feature (for feature grouping view)
  const tasksByFeature = useMemo(() => {
    if (!groupByFeature) return null;
    const grouped = new Map<string, TaskDisplay[]>();
    const ungrouped: TaskDisplay[] = [];
    
    for (const task of tasks) {
      if (task.feature_id) {
        if (!grouped.has(task.feature_id)) {
          grouped.set(task.feature_id, []);
        }
        grouped.get(task.feature_id)!.push(task);
      } else {
        ungrouped.push(task);
      }
    }
    
    // Add ungrouped as a special key
    if (ungrouped.length > 0) {
      grouped.set('__ungrouped__', ungrouped);
    }
    
    return grouped;
  }, [tasks, groupByFeature]);

  // Compute feature metadata for sorting and display
  const featureMetadata = useMemo(() => {
    if (!tasksByFeature) return null;
    
    const metadata = new Map<string, {
      priority: number;
      status: 'ready' | 'waiting' | 'blocked' | 'in_progress' | 'completed';
      completed: number;
      total: number;
      blockedBy: string[];
      dependsOn: string[];
    }>();
    
    for (const [featureId, featureTasks] of tasksByFeature) {
      if (featureId === '__ungrouped__') continue;
      
      const activeFeatTasks = featureTasks.filter(t => !isCompleted(t));
      const completedCount = featureTasks.filter(isCompleted).length;
      
      // Determine feature priority (from feature_priority or highest task priority)
      let priorityNum = 1; // medium default
      const firstTaskWithFeaturePriority = featureTasks.find(t => t.feature_priority);
      if (firstTaskWithFeaturePriority?.feature_priority) {
        priorityNum = PRIORITY_ORDER[firstTaskWithFeaturePriority.feature_priority] ?? 1;
      } else {
        // Use highest task priority
        priorityNum = Math.min(...featureTasks.map(t => PRIORITY_ORDER[t.priority] ?? 1));
      }
      
      // Collect feature dependencies
      const dependsOn = new Set<string>();
      for (const task of featureTasks) {
        if (task.feature_depends_on) {
          task.feature_depends_on.forEach(dep => dependsOn.add(dep));
        }
      }
      
      // Determine feature status
      let status: 'ready' | 'waiting' | 'blocked' | 'in_progress' | 'completed';
      const hasInProgress = featureTasks.some(t => t.status === 'in_progress');
      const hasBlocked = featureTasks.some(t => t.status === 'blocked');
      const allCompleted = activeFeatTasks.length === 0 && completedCount > 0;
      
      // Check if waiting on other features
      const blockedByFeatures: string[] = [];
      for (const depFeatureId of dependsOn) {
        const depFeatureTasks = tasksByFeature.get(depFeatureId);
        if (depFeatureTasks) {
          const depActiveCount = depFeatureTasks.filter(t => !isCompleted(t)).length;
          if (depActiveCount > 0) {
            blockedByFeatures.push(depFeatureId);
          }
        }
      }
      
      if (allCompleted) {
        status = 'completed';
      } else if (hasBlocked) {
        status = 'blocked';
      } else if (hasInProgress) {
        status = 'in_progress';
      } else if (blockedByFeatures.length > 0) {
        status = 'waiting';
      } else {
        status = 'ready';
      }
      
      metadata.set(featureId, {
        priority: priorityNum,
        status,
        completed: completedCount,
        total: featureTasks.length,
        blockedBy: blockedByFeatures,
        dependsOn: [...dependsOn],
      });
    }
    
    return metadata;
  }, [tasksByFeature]);

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

    // Add draft section if there are draft tasks
    const draftElements: React.ReactElement[] = [];
    if (draftTasks.length > 0) {
      draftElements.push(
        <Box key="draft-spacer">
          <Text> </Text>
        </Box>
      );
      
      draftElements.push(
        <DraftHeader
          key={DRAFT_HEADER_ID}
          count={draftTasks.length}
          collapsed={draftCollapsed}
          isSelected={selectedId === DRAFT_HEADER_ID}
        />
      );
      
      if (!draftCollapsed) {
        draftTasks.forEach(task => {
          draftElements.push(
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
          {draftElements}
          {completedElements}
        </Box>
      </Box>
    );
  }

  // Grouped view by feature
  if (groupByFeature && tasksByFeature && featureMetadata) {
    const featureElements: React.ReactElement[] = [];
    
    // Sort features: by priority first, then by dependency order, then alphabetically
    // Features that are dependencies of others come first
    const featureIds = Array.from(tasksByFeature.keys())
      .filter(id => id !== '__ungrouped__')
      .sort((a, b) => {
        const metaA = featureMetadata.get(a);
        const metaB = featureMetadata.get(b);
        if (!metaA || !metaB) return 0;
        
        // Priority first (lower number = higher priority)
        if (metaA.priority !== metaB.priority) {
          return metaA.priority - metaB.priority;
        }
        
        // Then by dependency order: if B depends on A, A comes first
        if (metaB.dependsOn.includes(a)) return -1;
        if (metaA.dependsOn.includes(b)) return 1;
        
        // Then alphabetically
        return a.localeCompare(b);
      });
    
    // Pre-filter to features with active tasks (matches flattenFeatureOrder for navigation)
    const activeFeatureIds = featureIds.filter(featureId => {
      const featureTasks = tasksByFeature.get(featureId) || [];
      return featureTasks.some(t => !isCompleted(t));
    });
    
    activeFeatureIds.forEach((featureId, featureIndex) => {
      const featureTasks = tasksByFeature.get(featureId) || [];
      const meta = featureMetadata.get(featureId);
      const isFeatureCollapsed = collapsedFeatures.has(featureId);
      
      const headerId = `${FEATURE_HEADER_PREFIX}${featureId}`;
      
      // Feature header
      featureElements.push(
        <FeatureHeader
          key={headerId}
          featureId={featureId}
          completed={meta?.completed ?? 0}
          total={meta?.total ?? featureTasks.length}
          status={meta?.status ?? 'ready'}
          blockedBy={meta?.blockedBy}
          isSelected={selectedId === headerId}
          isCollapsed={isFeatureCollapsed}
          isPaused={pausedFeatures.has(featureId)}
        />
      );
      
      // Feature's tasks (only render if not collapsed)
      if (!isFeatureCollapsed) {
        featureElements.push(
          ...renderFeatureTasks(featureTasks, selectedId, readyIds)
        );
      }
      
      // Add spacing between features (except last)
      if (featureIndex < activeFeatureIds.length - 1) {
        featureElements.push(
          <Box key={`feature-spacer-${featureId}`}>
            <Text> </Text>
          </Box>
        );
      }
    });
    
    // Handle ungrouped tasks
    const ungroupedTasks = tasksByFeature.get('__ungrouped__');
    if (ungroupedTasks && ungroupedTasks.length > 0) {
      const ungroupedActive = ungroupedTasks.filter(t => !isCompleted(t) && !isDraft(t));
      
      if (ungroupedActive.length > 0) {
        // Add spacing before ungrouped section
        if (featureElements.length > 0) {
          featureElements.push(
            <Box key="ungrouped-spacer">
              <Text> </Text>
            </Box>
          );
        }
        
        // Ungrouped header
        featureElements.push(
          <Box key="ungrouped-header" marginTop={1}>
            <Text bold color="gray">
              Ungrouped ({ungroupedActive.length})
            </Text>
          </Box>
        );
        
        // Ungrouped tasks
        featureElements.push(
          ...renderFeatureTasks(ungroupedTasks, selectedId, readyIds)
        );
      }
    }

    // Add draft section if there are draft tasks
    const draftElements: React.ReactElement[] = [];
    if (draftTasks.length > 0) {
      draftElements.push(
        <Box key="draft-spacer">
          <Text> </Text>
        </Box>
      );
      
      draftElements.push(
        <DraftHeader
          key={DRAFT_HEADER_ID}
          count={draftTasks.length}
          collapsed={draftCollapsed}
          isSelected={selectedId === DRAFT_HEADER_ID}
        />
      );
      
      if (!draftCollapsed) {
        draftTasks.forEach(task => {
          draftElements.push(
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

    // Combine all elements for viewport slicing
    const allElements = [...featureElements, ...draftElements, ...completedElements];
    
    // Apply viewport slicing if viewportHeight is provided
    let visibleElements: React.ReactElement[];
    let hasMoreAbove = false;
    let hasMoreBelow = false;
    
    if (viewportHeight && viewportHeight > 0) {
      const startIndex = scrollOffset;
      const endIndex = scrollOffset + viewportHeight;
      visibleElements = allElements.slice(startIndex, endIndex);
      hasMoreAbove = startIndex > 0;
      hasMoreBelow = endIndex < allElements.length;
    } else {
      visibleElements = allElements;
    }

    return (
      <Box flexDirection="column" padding={1}>
        <Box>
          <Text bold underline>
            Tasks ({tasks.length})
          </Text>
          {hasMoreAbove && (
            <Text dimColor> ↑{scrollOffset} more</Text>
          )}
        </Box>
        <Box flexDirection="column" marginTop={1}>
          {visibleElements}
        </Box>
        {hasMoreBelow && (
          <Box>
            <Text dimColor>↓{allElements.length - scrollOffset - (viewportHeight || 0)} more</Text>
          </Box>
        )}
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

  // Add draft section if there are draft tasks
  const draftElements: React.ReactElement[] = [];
  if (draftTasks.length > 0) {
    // Add spacing before draft section
    draftElements.push(
      <Box key="draft-spacer">
        <Text> </Text>
      </Box>
    );
    
    // Draft header
    draftElements.push(
      <DraftHeader
        key={DRAFT_HEADER_ID}
        count={draftTasks.length}
        collapsed={draftCollapsed}
        isSelected={selectedId === DRAFT_HEADER_ID}
      />
    );
    
    // Render draft tasks as flat list when expanded
    if (!draftCollapsed) {
      draftTasks.forEach(task => {
        draftElements.push(
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

  // Combine all elements for viewport slicing
  const allElements = [...elements, ...draftElements, ...completedElements];
  
  // Apply viewport slicing if viewportHeight is provided
  let visibleElements: React.ReactElement[];
  let hasMoreAbove = false;
  let hasMoreBelow = false;
  
  if (viewportHeight && viewportHeight > 0) {
    const startIndex = scrollOffset;
    const endIndex = scrollOffset + viewportHeight;
    visibleElements = allElements.slice(startIndex, endIndex);
    hasMoreAbove = startIndex > 0;
    hasMoreBelow = endIndex < allElements.length;
  } else {
    visibleElements = allElements;
  }

  return (
    <Box flexDirection="column" padding={1}>
      <Box>
        <Text bold underline>
          Tasks ({tasks.length})
        </Text>
        {hasMoreAbove && (
          <Text dimColor> ↑{scrollOffset} more</Text>
        )}
      </Box>
      <Box flexDirection="column" marginTop={1}>
        {visibleElements}
      </Box>
      {hasMoreBelow && (
        <Box>
          <Text dimColor>↓{allElements.length - scrollOffset - (viewportHeight || 0)} more</Text>
        </Box>
      )}
    </Box>
  );
});

export default TaskTree;

/**
 * Task detail panel component showing all frontmatter fields
 */

import React from 'react';
import { Box, Text } from 'ink';
import type { TaskDisplay } from '../types';

interface TaskDetailProps {
  task: TaskDisplay | null;
  /** Whether this panel is currently focused */
  isFocused?: boolean;
  /** Scroll offset for viewport slicing (0 = top) */
  scrollOffset?: number;
  /** Total viewport height for calculating visible content */
  viewportHeight?: number;
}

const STATUS_COLORS: Record<string, string> = {
  pending: 'gray',
  in_progress: 'yellow',
  completed: 'green',
  blocked: 'red',
  failed: 'red',
};

const CLASSIFICATION_COLORS: Record<string, string> = {
  ready: 'green',
  waiting: 'yellow',
  blocked: 'red',
  not_pending: 'gray',
};

/**
 * Format an ISO date string to a more readable format
 */
function formatDate(isoString: string | undefined): string {
  if (!isoString) return '';
  try {
    const date = new Date(isoString);
    return date.toLocaleString();
  } catch {
    return isoString;
  }
}

/**
 * Truncate a string with ellipsis if too long
 */
function truncate(str: string | null | undefined, maxLen: number): string {
  if (!str) return '';
  if (str.length <= maxLen) return str;
  return str.slice(0, maxLen - 3) + '...';
}

export const TaskDetail = React.memo(function TaskDetail({ 
  task, 
  isFocused = false,
  scrollOffset = 0,
  viewportHeight = 20,
}: TaskDetailProps): React.ReactElement {
  if (!task) {
    return (
      <Box
        borderStyle="single"
        borderColor="gray"
        padding={1}
        flexDirection="column"
        flexGrow={1}
      >
        <Text dimColor>Select a task to view details</Text>
      </Box>
    );
  }

  const statusColor = STATUS_COLORS[task.status] || 'white';
  const classificationColor = task.classification ? CLASSIFICATION_COLORS[task.classification] || 'white' : 'white';

  // Build all content lines as an array of React elements for viewport slicing
  const contentLines: React.ReactNode[] = [];
  
  // Title
  contentLines.push(<Text key="title" bold>{task.title}</Text>);
  
  // Core fields
  contentLines.push(
    <Box key="status" marginTop={1}>
      <Text>Status: </Text>
      <Text color={statusColor}>{task.status}</Text>
      {task.classification && (
        <>
          <Text dimColor> (</Text>
          <Text color={classificationColor}>{task.classification}</Text>
          <Text dimColor>)</Text>
        </>
      )}
    </Box>
  );
  contentLines.push(
    <Box key="priority">
      <Text>Priority: </Text>
      <Text color={task.priority === 'high' ? 'red' : task.priority === 'low' ? 'gray' : 'white'}>
        {task.priority}
      </Text>
    </Box>
  );
  contentLines.push(
    <Box key="id">
      <Text>ID: </Text>
      <Text dimColor>{task.id}</Text>
    </Box>
  );
  contentLines.push(
    <Box key="path">
      <Text>Path: </Text>
      <Text dimColor>{task.path}</Text>
    </Box>
  );

  // Parent task
  if (task.parent_id) {
    contentLines.push(
      <Box key="parent">
        <Text>Parent: </Text>
        <Text color="cyan">{task.parent_id}</Text>
      </Box>
    );
  }

  // Created date
  if (task.created) {
    contentLines.push(
      <Box key="created">
        <Text>Created: </Text>
        <Text dimColor>{formatDate(task.created)}</Text>
      </Box>
    );
  }

  // Git context
  if (task.gitBranch || task.gitRemote) {
    contentLines.push(
      <Box key="git-header" flexDirection="column" marginTop={1}>
        <Text underline>Git Context:</Text>
      </Box>
    );
    if (task.gitBranch) {
      contentLines.push(
        <Box key="git-branch">
          <Text dimColor>  Branch: </Text>
          <Text color="cyan">{task.gitBranch}</Text>
        </Box>
      );
    }
    if (task.gitRemote) {
      contentLines.push(
        <Box key="git-remote">
          <Text dimColor>  Remote: </Text>
          <Text>{truncate(task.gitRemote, 50)}</Text>
        </Box>
      );
    }
  }

  // Working directory
  if (task.workdir || task.gitBranch || task.resolvedWorkdir) {
    contentLines.push(
      <Box key="workdir-header" flexDirection="column" marginTop={1}>
        <Text underline>Working Directory:</Text>
      </Box>
    );
    if (task.workdir) {
      contentLines.push(
        <Box key="workdir">
          <Text dimColor>  workdir: </Text>
          <Text>{task.workdir}</Text>
        </Box>
      );
    }
    if (task.gitBranch) {
      contentLines.push(
        <Box key="git-branch">
          <Text dimColor>  branch: </Text>
          <Text>{task.gitBranch}</Text>
        </Box>
      );
    }
    if (task.resolvedWorkdir) {
      contentLines.push(
        <Box key="resolved-workdir">
          <Text dimColor>  resolved: </Text>
          <Text color="green">{task.resolvedWorkdir}</Text>
        </Box>
      );
    }
  }

  // Dependencies section
  const hasDirectDeps = task.dependencyTitles.length > 0;
  const hasIndirectDeps = (task.indirectAncestorTitles?.length ?? 0) > 0;
  const hasWaitingOn = (task.waitingOn?.length ?? 0) > 0;
  const hasBlockedBy = (task.blockedBy?.length ?? 0) > 0;
  
  if (hasDirectDeps || hasIndirectDeps || hasWaitingOn || hasBlockedBy) {
    contentLines.push(
      <Box key="deps-header" flexDirection="column" marginTop={1}>
        <Text underline>Dependencies:</Text>
      </Box>
    );
    // Direct dependencies
    task.dependencyTitles.forEach((dep, i) => {
      contentLines.push(
        <Text key={`dep-${i}`} dimColor>
          {'  '}- {dep}
        </Text>
      );
    });
    // Indirect (transitive) dependencies - shown with different styling
    if (hasIndirectDeps) {
      contentLines.push(
        <Text key="indirect-header" dimColor italic>
          {'  '}(transitive):
        </Text>
      );
      task.indirectAncestorTitles!.forEach((dep, i) => {
        contentLines.push(
          <Text key={`indirect-${i}`} dimColor>
            {'    '}- {dep}
          </Text>
        );
      });
    }
    if (task.waitingOn && task.waitingOn.length > 0) {
      contentLines.push(
        <Box key="waiting-on" marginTop={0}>
          <Text color="yellow">  Waiting on: </Text>
          <Text dimColor>{task.waitingOn.join(', ')}</Text>
        </Box>
      );
    }
    if (task.blockedBy && task.blockedBy.length > 0) {
      contentLines.push(
        <Box key="blocked-by">
          <Text color="red">  Blocked by: </Text>
          <Text dimColor>{task.blockedBy.join(', ')}</Text>
        </Box>
      );
    }
    if (task.blockedByReason) {
      contentLines.push(
        <Box key="blocked-reason">
          <Text color="red">  Reason: </Text>
          <Text>{task.blockedByReason}</Text>
        </Box>
      );
    }
  }

  // Unresolved deps warning
  if (task.unresolvedDeps && task.unresolvedDeps.length > 0) {
    contentLines.push(
      <Box key="unresolved-header" flexDirection="column" marginTop={1}>
        <Text color="yellow">Unresolved Dependencies:</Text>
      </Box>
    );
    task.unresolvedDeps.forEach((dep, i) => {
      contentLines.push(
        <Text key={`unresolved-${i}`} color="yellow">
          {'  '}- {dep}
        </Text>
      );
    });
  }

  // Cycle warning
  if (task.inCycle) {
    contentLines.push(
      <Box key="cycle" marginTop={1}>
        <Text color="red" bold>⚠ Task is part of a dependency cycle</Text>
      </Box>
    );
  }

  // Dependents
  if (task.dependentTitles.length > 0) {
    contentLines.push(
      <Box key="dependents-header" flexDirection="column" marginTop={1}>
        <Text underline>Dependents (blocked by this):</Text>
      </Box>
    );
    task.dependentTitles.forEach((dep, i) => {
      contentLines.push(
        <Text key={`dependent-${i}`} dimColor>
          {'  '}- {dep}
        </Text>
      );
    });
  }

  // User original request
  if (task.userOriginalRequest) {
    contentLines.push(
      <Box key="request-header" flexDirection="column" marginTop={1}>
        <Text underline>Original Request:</Text>
      </Box>
    );
    contentLines.push(
      <Text key="request-content" dimColor wrap="wrap">{'  '}{truncate(task.userOriginalRequest, 200)}</Text>
    );
  }

  // Error
  if (task.error) {
    contentLines.push(
      <Box key="error" marginTop={1}>
        <Text color="red">Error: {task.error}</Text>
      </Box>
    );
  }

  // Progress
  if (task.progress !== undefined) {
    contentLines.push(
      <Box key="progress" marginTop={1}>
        <Text>Progress: {task.progress}%</Text>
      </Box>
    );
  }

  // Apply viewport slicing based on scrollOffset
  const totalLines = contentLines.length;
  const visibleLines = contentLines.slice(scrollOffset, scrollOffset + viewportHeight);
  const hasMoreAbove = scrollOffset > 0;
  const hasMoreBelow = scrollOffset + viewportHeight < totalLines;

  // Build header with scroll indicators
  const headerParts: string[] = ['Details'];
  if (hasMoreAbove || hasMoreBelow) {
    const position = `${scrollOffset + 1}-${Math.min(scrollOffset + viewportHeight, totalLines)}/${totalLines}`;
    headerParts.push(`(${position})`);
  }
  const headerText = headerParts.join(' ');

  return (
    <Box
      borderStyle="single"
      borderColor={isFocused ? 'cyan' : 'gray'}
      padding={1}
      flexDirection="column"
      flexGrow={1}
    >
      {/* Header with scroll position indicator */}
      <Box justifyContent="space-between">
        <Text bold dimColor={!isFocused} color={isFocused ? 'cyan' : undefined}>
          {headerText}
        </Text>
        {hasMoreAbove && <Text dimColor>▲</Text>}
      </Box>
      
      {/* Visible content lines */}
      {visibleLines}
      
      {/* Bottom scroll indicator */}
      {hasMoreBelow && (
        <Box justifyContent="flex-end">
          <Text dimColor>▼</Text>
        </Box>
      )}
    </Box>
  );
});

export default TaskDetail;

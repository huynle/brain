/**
 * Task detail panel component showing all frontmatter fields
 */

import React from 'react';
import { Box, Text } from 'ink';
import type { TaskDisplay } from '../types';

interface TaskDetailProps {
  task: TaskDisplay | null;
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

export const TaskDetail = React.memo(function TaskDetail({ task }: TaskDetailProps): React.ReactElement {
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

  return (
    <Box
      borderStyle="single"
      borderColor="cyan"
      padding={1}
      flexDirection="column"
      flexGrow={1}
    >
      {/* Title */}
      <Text bold>{task.title}</Text>
      
      {/* Core fields */}
      <Box marginTop={1}>
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
      <Box>
        <Text>Priority: </Text>
        <Text color={task.priority === 'high' ? 'red' : task.priority === 'low' ? 'gray' : 'white'}>
          {task.priority}
        </Text>
      </Box>
      <Box>
        <Text>ID: </Text>
        <Text dimColor>{task.id}</Text>
      </Box>
      
      {/* Path */}
      <Box>
        <Text>Path: </Text>
        <Text dimColor>{task.path}</Text>
      </Box>

      {/* Parent task */}
      {task.parent_id && (
        <Box>
          <Text>Parent: </Text>
          <Text color="cyan">{task.parent_id}</Text>
        </Box>
      )}

      {/* Created date */}
      {task.created && (
        <Box>
          <Text>Created: </Text>
          <Text dimColor>{formatDate(task.created)}</Text>
        </Box>
      )}

      {/* Git context */}
      {(task.gitBranch || task.gitRemote) && (
        <Box flexDirection="column" marginTop={1}>
          <Text underline>Git Context:</Text>
          {task.gitBranch && (
            <Box>
              <Text dimColor>  Branch: </Text>
              <Text color="cyan">{task.gitBranch}</Text>
            </Box>
          )}
          {task.gitRemote && (
            <Box>
              <Text dimColor>  Remote: </Text>
              <Text>{truncate(task.gitRemote, 50)}</Text>
            </Box>
          )}
        </Box>
      )}

      {/* Working directory */}
      {(task.workdir || task.worktree || task.resolvedWorkdir) && (
        <Box flexDirection="column" marginTop={1}>
          <Text underline>Working Directory:</Text>
          {task.workdir && (
            <Box>
              <Text dimColor>  workdir: </Text>
              <Text>{task.workdir}</Text>
            </Box>
          )}
          {task.worktree && (
            <Box>
              <Text dimColor>  worktree: </Text>
              <Text>{task.worktree}</Text>
            </Box>
          )}
          {task.resolvedWorkdir && (
            <Box>
              <Text dimColor>  resolved: </Text>
              <Text color="green">{task.resolvedWorkdir}</Text>
            </Box>
          )}
        </Box>
      )}

      {/* Dependencies section */}
      {(task.dependencyTitles.length > 0 || (task.waitingOn?.length ?? 0) > 0 || (task.blockedBy?.length ?? 0) > 0) && (
        <Box flexDirection="column" marginTop={1}>
          <Text underline>Dependencies:</Text>
          {task.dependencyTitles.map((dep, i) => (
            <Text key={i} dimColor>
              {'  '}- {dep}
            </Text>
          ))}
          {task.waitingOn && task.waitingOn.length > 0 && (
            <Box marginTop={0}>
              <Text color="yellow">  Waiting on: </Text>
              <Text dimColor>{task.waitingOn.join(', ')}</Text>
            </Box>
          )}
          {task.blockedBy && task.blockedBy.length > 0 && (
            <Box>
              <Text color="red">  Blocked by: </Text>
              <Text dimColor>{task.blockedBy.join(', ')}</Text>
            </Box>
          )}
          {task.blockedByReason && (
            <Box>
              <Text color="red">  Reason: </Text>
              <Text>{task.blockedByReason}</Text>
            </Box>
          )}
        </Box>
      )}

      {/* Unresolved deps warning */}
      {task.unresolvedDeps && task.unresolvedDeps.length > 0 && (
        <Box flexDirection="column" marginTop={1}>
          <Text color="yellow">Unresolved Dependencies:</Text>
          {task.unresolvedDeps.map((dep, i) => (
            <Text key={i} color="yellow">
              {'  '}- {dep}
            </Text>
          ))}
        </Box>
      )}

      {/* Cycle warning */}
      {task.inCycle && (
        <Box marginTop={1}>
          <Text color="red" bold>⚠ Task is part of a dependency cycle</Text>
        </Box>
      )}

      {/* Dependents */}
      {task.dependentTitles.length > 0 && (
        <Box flexDirection="column" marginTop={1}>
          <Text underline>Dependents (blocked by this):</Text>
          {task.dependentTitles.map((dep, i) => (
            <Text key={i} dimColor>
              {'  '}- {dep}
            </Text>
          ))}
        </Box>
      )}

      {/* User original request */}
      {task.userOriginalRequest && (
        <Box flexDirection="column" marginTop={1}>
          <Text underline>Original Request:</Text>
          <Text dimColor wrap="wrap">{'  '}{truncate(task.userOriginalRequest, 200)}</Text>
        </Box>
      )}

      {/* Error */}
      {task.error && (
        <Box marginTop={1}>
          <Text color="red">Error: {task.error}</Text>
        </Box>
      )}

      {/* Progress */}
      {task.progress !== undefined && (
        <Box marginTop={1}>
          <Text>Progress: {task.progress}%</Text>
        </Box>
      )}
    </Box>
  );
});

export default TaskDetail;

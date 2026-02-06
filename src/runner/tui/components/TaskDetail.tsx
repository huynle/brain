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

      {/* Children tasks */}
      {task.children_ids.length > 0 && (
        <Box flexDirection="column" marginTop={1}>
          <Text underline>Children ({task.children_ids.length}):</Text>
          {task.children_ids.map((childId, i) => (
            <Text key={i} dimColor>
              {'  '}- {childId}
            </Text>
          ))}
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

      {/* Blocked status */}
      {task.blockedBy && (
        <Box flexDirection="column" marginTop={1}>
          <Text color="red" underline>Blocked:</Text>
          <Box>
            <Text color="red">  By parent: </Text>
            <Text dimColor>{task.blockedBy}</Text>
          </Box>
          {task.blockedByReason && (
            <Box>
              <Text color="red">  Reason: </Text>
              <Text>{task.blockedByReason}</Text>
            </Box>
          )}
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

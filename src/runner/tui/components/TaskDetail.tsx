/**
 * Task detail panel component (optional, for expanded task view)
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

export function TaskDetail({ task }: TaskDetailProps): React.ReactElement {
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

  return (
    <Box
      borderStyle="single"
      borderColor="cyan"
      padding={1}
      flexDirection="column"
    >
      <Text bold>{task.title}</Text>
      <Box marginTop={1}>
        <Text>Status: </Text>
        <Text color={statusColor}>{task.status}</Text>
      </Box>
      <Box>
        <Text>Priority: </Text>
        <Text>{task.priority}</Text>
      </Box>
      <Box>
        <Text>ID: </Text>
        <Text dimColor>{task.id}</Text>
      </Box>

      {task.dependencies.length > 0 && (
        <Box flexDirection="column" marginTop={1}>
          <Text underline>Dependencies:</Text>
          {task.dependencies.map((dep, i) => (
            <Text key={i} dimColor>
              {' '}- {dep}
            </Text>
          ))}
        </Box>
      )}

      {task.error && (
        <Box marginTop={1}>
          <Text color="red">Error: {task.error}</Text>
        </Box>
      )}

      {task.progress !== undefined && (
        <Box marginTop={1}>
          <Text>Progress: {task.progress}%</Text>
        </Box>
      )}
    </Box>
  );
}

export default TaskDetail;

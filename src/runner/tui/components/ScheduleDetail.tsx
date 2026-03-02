import React from 'react';
import { Box, Text } from 'ink';
import type { TaskDisplay } from '../types';
import { getStatusColor, getStatusIcon, getStatusLabel } from '../status-display';

interface ScheduleDetailProps {
  task: TaskDisplay | null;
  isFocused?: boolean;
}

export const ScheduleDetail = React.memo(function ScheduleDetail({
  task,
  isFocused = false,
}: ScheduleDetailProps): React.ReactElement {
  if (!task) {
    return (
      <Box borderStyle="single" borderColor="gray" padding={1} flexDirection="column" flexGrow={1}>
        <Text dimColor>Select a scheduled task to view details</Text>
      </Box>
    );
  }

  const isReady = task.classification === 'ready';
  const statusIcon = getStatusIcon(task.status, isReady);
  const statusColor = getStatusColor(task.status, isReady);

  return (
    <Box borderStyle="single" borderColor={isFocused ? 'cyan' : 'gray'} padding={1} flexDirection="column" flexGrow={1}>
      <Text bold dimColor={!isFocused} color={isFocused ? 'cyan' : undefined}>Schedule Details</Text>
      <Text bold>{task.title}</Text>
      <Text>ID: <Text dimColor>{task.id}</Text></Text>
      <Text>Schedule: <Text dimColor>{task.schedule || '(none)'}</Text></Text>
      <Text>Enabled: <Text color={task.scheduleEnabled === false ? 'yellow' : 'green'}>{task.scheduleEnabled === false ? 'no' : 'yes'}</Text></Text>
      <Text>Status: <Text color={statusColor}>{statusIcon} {getStatusLabel(task.status)}</Text></Text>
      <Text>Priority: <Text dimColor>{task.priority}</Text></Text>
      {task.tags.length > 0 && (
        <Text>Tags: <Text dimColor>{task.tags.join(', ')}</Text></Text>
      )}
      {task.projectId && (
        <Text>Project: <Text dimColor>{task.projectId}</Text></Text>
      )}
      <Text>Path: <Text dimColor>{task.path}</Text></Text>
      {task.created && (
        <Text>Created: <Text dimColor>{task.created}</Text></Text>
      )}
      {task.modified && (
        <Text>Modified: <Text dimColor>{task.modified}</Text></Text>
      )}
    </Box>
  );
});

export default ScheduleDetail;

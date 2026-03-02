import React from 'react';
import { Box, Text } from 'ink';
import type { TaskDisplay } from '../types';
import { getStatusColor, getStatusIcon } from '../status-display';

interface ScheduleListProps {
  tasks: TaskDisplay[];
  selectedId: string | null;
  isFocused?: boolean;
  scrollOffset?: number;
  viewportHeight?: number;
  showProjectPrefix?: boolean;
}

export const ScheduleList = React.memo(function ScheduleList({
  tasks,
  selectedId,
  isFocused = false,
  scrollOffset = 0,
  viewportHeight = 10,
  showProjectPrefix = false,
}: ScheduleListProps): React.ReactElement {
  if (tasks.length === 0) {
    return (
      <Box paddingX={1} flexDirection="column">
        <Text dimColor>No scheduled tasks found</Text>
      </Box>
    );
  }

  const safeScrollOffset = Math.max(0, Math.min(scrollOffset, Math.max(0, tasks.length - viewportHeight)));
  const visibleTasks = tasks.slice(safeScrollOffset, safeScrollOffset + viewportHeight);

  return (
    <Box paddingX={1} flexDirection="column">
      <Box marginBottom={1}>
        <Text bold dimColor={!isFocused} color={isFocused ? 'cyan' : undefined}>Scheduled</Text>
      </Box>
      {visibleTasks.map((task) => {
        const isSelected = task.id === selectedId;
        const isReady = task.classification === 'ready';
        const statusIcon = getStatusIcon(task.status, isReady);
        const statusColor = getStatusColor(task.status, isReady);
        const projectPrefix = showProjectPrefix && task.projectId ? `[${task.projectId}] ` : '';

        const isDisabled = task.scheduleEnabled === false;

        return (
          <Box key={task.id}>
            <Text color={isSelected ? 'cyan' : undefined}>{isSelected ? '> ' : '  '}</Text>
            <Text color={statusColor}>{statusIcon}</Text>
            <Text> </Text>
            <Text color={isSelected ? 'cyan' : undefined} dimColor={isDisabled}>
              {projectPrefix}
              {task.title}
            </Text>
            {isDisabled ? (
              <Text color="yellow">  [disabled]</Text>
            ) : (
              <Text color="magenta">  [scheduled]</Text>
            )}
            <Text dimColor>  {task.schedule || '(no schedule)'}</Text>
            {task.priority !== 'medium' && (
              <Text dimColor>  pri:{task.priority}</Text>
            )}
          </Box>
        );
      })}
    </Box>
  );
});

export default ScheduleList;

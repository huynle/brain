import React from 'react';
import { Box, Text } from 'ink';
import type { CronDisplay, TaskDisplay } from '../types';

interface CronLinkEditorProps {
  cron: CronDisplay;
  projectId: string;
  tasks: TaskDisplay[];
  linkedTaskIds: Set<string>;
  selectedIndex: number;
}

function truncate(value: string, maxLength: number): string {
  if (value.length <= maxLength) return value;
  return `${value.slice(0, Math.max(0, maxLength - 3))}...`;
}

export function CronLinkEditor({
  cron,
  projectId,
  tasks,
  linkedTaskIds,
  selectedIndex,
}: CronLinkEditorProps): React.ReactElement {
  const linkedCount = tasks.filter((task) => linkedTaskIds.has(task.id)).length;
  const visibleTasks = tasks.slice(0, 12);

  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor="cyan"
      paddingX={2}
      paddingY={1}
      width="90%"
    >
      <Text bold color="cyan">Edit Cron Linked Tasks</Text>
      <Text>Cron: <Text bold>{cron.title}</Text> <Text dimColor>({cron.id})</Text></Text>
      <Text>Project: <Text dimColor>{projectId}</Text></Text>
      <Text>
        Linked: <Text bold>{linkedCount}</Text>
        {'  '}
        Available: <Text bold>{tasks.length - linkedCount}</Text>
      </Text>

      <Box marginTop={1} flexDirection="column">
        <Text underline>Tasks</Text>
        {visibleTasks.length === 0 ? (
          <Text dimColor>(no tasks available in this project)</Text>
        ) : (
          visibleTasks.map((task, index) => {
            const isSelected = index === selectedIndex;
            const isLinked = linkedTaskIds.has(task.id);
            return (
              <Text key={task.id} color={isSelected ? 'cyan' : undefined}>
                {isSelected ? '>' : ' '}
                {' '}
                [{isLinked ? 'x' : ' '}]
                {' '}
                {truncate(task.title, 44)}
                {' '}
                <Text dimColor>({task.id})</Text>
              </Text>
            );
          })
        )}
        {tasks.length > visibleTasks.length && (
          <Text dimColor>...and {tasks.length - visibleTasks.length} more</Text>
        )}
      </Box>

      <Box marginTop={1}>
        <Text dimColor>j/k move, Space toggle, Enter apply, Esc cancel</Text>
      </Box>
    </Box>
  );
}

export default CronLinkEditor;

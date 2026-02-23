import React from 'react';
import { Box, Text } from 'ink';
import type { CronDisplay } from '../types';
import { getStatusColor, getStatusIcon } from '../status-display';

interface CronListProps {
  crons: CronDisplay[];
  selectedId: string | null;
  isFocused?: boolean;
  scrollOffset?: number;
  viewportHeight?: number;
  showProjectPrefix?: boolean;
}

function formatDateShort(value?: string): string {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

export const CronList = React.memo(function CronList({
  crons,
  selectedId,
  isFocused = false,
  scrollOffset = 0,
  viewportHeight = 10,
  showProjectPrefix = false,
}: CronListProps): React.ReactElement {
  if (crons.length === 0) {
    return (
      <Box paddingX={1} flexDirection="column">
        <Text dimColor>No cron entries found</Text>
      </Box>
    );
  }

  const safeScrollOffset = Math.max(0, Math.min(scrollOffset, Math.max(0, crons.length - viewportHeight)));
  const visibleCrons = crons.slice(safeScrollOffset, safeScrollOffset + viewportHeight);

  return (
    <Box paddingX={1} flexDirection="column">
      <Box marginBottom={1}>
        <Text bold dimColor={!isFocused} color={isFocused ? 'cyan' : undefined}>Crons</Text>
      </Box>
      {visibleCrons.map((cron) => {
        const isSelected = cron.id === selectedId;
        const statusIcon = getStatusIcon(cron.status, false);
        const statusColor = getStatusColor(cron.status, false);
        const projectPrefix = showProjectPrefix && cron.projectId ? `[${cron.projectId}] ` : '';
        const runCount = cron.runs?.length ?? 0;

        return (
          <Box key={cron.id}>
            <Text color={isSelected ? 'cyan' : undefined}>{isSelected ? '> ' : '  '}</Text>
            <Text color={statusColor}>{statusIcon}</Text>
            <Text> </Text>
            <Text color={isSelected ? 'cyan' : undefined}>
              {projectPrefix}
              {cron.title}
            </Text>
            <Text dimColor>  {cron.schedule || '(no schedule)'}</Text>
            <Text dimColor>  next: {formatDateShort(cron.next_run)}</Text>
            <Text dimColor>  runs: {runCount}</Text>
          </Box>
        );
      })}
    </Box>
  );
});

export default CronList;

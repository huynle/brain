import React from 'react';
import { Box, Text } from 'ink';
import type { CronDisplay } from '../types';

interface CronDetailProps {
  cron: CronDisplay | null;
  isFocused?: boolean;
}

function formatDate(value?: string): string {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

function formatDuration(value?: number): string {
  if (value === undefined || value === null) return '-';
  if (value < 1000) return `${value}ms`;
  return `${(value / 1000).toFixed(2)}s`;
}

export const CronDetail = React.memo(function CronDetail({
  cron,
  isFocused = false,
}: CronDetailProps): React.ReactElement {
  if (!cron) {
    return (
      <Box borderStyle="single" borderColor="gray" padding={1} flexDirection="column" flexGrow={1}>
        <Text dimColor>Select a cron entry to view run history</Text>
      </Box>
    );
  }

  const runs = cron.runs ?? [];

  return (
    <Box borderStyle="single" borderColor={isFocused ? 'cyan' : 'gray'} padding={1} flexDirection="column" flexGrow={1}>
      <Text bold dimColor={!isFocused} color={isFocused ? 'cyan' : undefined}>Cron Details</Text>
      <Text bold>{cron.title}</Text>
      <Text>ID: <Text dimColor>{cron.id}</Text></Text>
      <Text>Schedule: <Text dimColor>{cron.schedule || '(none)'}</Text></Text>
      <Text>Next run: <Text dimColor>{formatDate(cron.next_run)}</Text></Text>
      {cron.projectId && (
        <Text>Project: <Text dimColor>{cron.projectId}</Text></Text>
      )}

      <Box marginTop={1} flexDirection="column">
        <Text underline>Run History</Text>
        {runs.length === 0 ? (
          <Text dimColor>No runs recorded</Text>
        ) : (
          runs.slice(0, 8).map((run) => (
            <Box key={run.run_id} flexDirection="column" marginTop={1}>
              <Text>
                <Text bold>{run.status}</Text>
                <Text dimColor>  {run.run_id}</Text>
              </Text>
              <Text dimColor>
                start: {formatDate(run.started)}  end: {formatDate(run.completed)}  dur: {formatDuration(run.duration)}
              </Text>
              {run.skip_reason && <Text color="yellow">skip: {run.skip_reason}</Text>}
              {run.failed_task && <Text color="red">failed task: {run.failed_task}</Text>}
            </Box>
          ))
        )}
      </Box>
    </Box>
  );
});

export default CronDetail;

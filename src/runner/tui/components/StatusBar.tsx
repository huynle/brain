/**
 * Top status bar component showing task counts and runner status
 */

import React from 'react';
import { Box, Text } from 'ink';

interface StatusBarProps {
  projectId: string;
  stats: {
    ready: number;
    waiting: number;
    inProgress: number;
    completed: number;
    blocked: number;
  };
  isConnected: boolean;
}

export function StatusBar({
  projectId,
  stats,
  isConnected,
}: StatusBarProps): React.ReactElement {
  return (
    <Box
      borderStyle="single"
      borderColor="cyan"
      paddingX={1}
      justifyContent="space-between"
    >
      <Box>
        <Text bold color="cyan">
          {projectId}
        </Text>
      </Box>

      <Box>
        <Text color="green">● {stats.ready} ready</Text>
        <Text>   </Text>
        <Text color="yellow">○ {stats.waiting} waiting</Text>
        <Text>   </Text>
        <Text color="blue">▶ {stats.inProgress} active</Text>
        <Text>   </Text>
        <Text color="green" dimColor>✓ {stats.completed} done</Text>
        {stats.blocked > 0 && (
          <>
            <Text>   </Text>
            <Text color="red">✗ {stats.blocked} blocked</Text>
          </>
        )}
      </Box>

      <Box>
        {isConnected ? (
          <Text color="green">● online</Text>
        ) : (
          <Text color="red">○ offline</Text>
        )}
      </Box>
    </Box>
  );
}

export default StatusBar;

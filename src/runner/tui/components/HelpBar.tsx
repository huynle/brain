/**
 * Bottom help bar showing keyboard shortcuts
 */

import React from 'react';
import { Box, Text } from 'ink';

interface HelpBarProps {
  focusedPanel?: 'tasks' | 'logs';
}

export function HelpBar({ focusedPanel }: HelpBarProps): React.ReactElement {
  return (
    <Box paddingX={1} justifyContent="space-between">
      <Box>
        <Text dimColor>
          <Text bold>Arrow/j/k</Text> Navigate
          {'  '}
          <Text bold>Enter</Text> Details/Toggle
          {'  '}
          <Text bold>x</Text> Cancel
          {'  '}
          <Text bold>Tab</Text> Switch Panel
          {'  '}
          <Text bold>r</Text> Refresh
          {'  '}
          <Text bold>q</Text> Quit
        </Text>
      </Box>
      {focusedPanel && (
        <Box>
          <Text dimColor>
            Focus: <Text color="cyan">{focusedPanel}</Text>
          </Text>
        </Box>
      )}
    </Box>
  );
}

export default HelpBar;

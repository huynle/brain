/**
 * Bottom help bar showing keyboard shortcuts
 */

import React from 'react';
import { Box, Text } from 'ink';

interface HelpBarProps {
  focusedPanel?: 'tasks' | 'logs';
  /** Whether in multi-project mode (shows tab shortcuts) */
  isMultiProject?: boolean;
}

export const HelpBar = React.memo(function HelpBar({ focusedPanel, isMultiProject }: HelpBarProps): React.ReactElement {
  return (
    <Box paddingX={1} justifyContent="space-between">
      <Box>
        <Text dimColor>
          {isMultiProject && (
            <>
              <Text bold>h/l/[/]/1-9</Text> Tabs
              {'  '}
            </>
          )}
          <Text bold>j/k</Text> Navigate
          {'  '}
          <Text bold>Enter</Text> Toggle
          {'  '}
          <Text bold>x</Text> Cancel
          {'  '}
          {isMultiProject ? (
            <>
              <Text bold>p/P</Text> Pause
              {'  '}
            </>
          ) : (
            <>
              <Text bold>p</Text> Pause
              {'  '}
            </>
          )}
          <Text bold>Tab</Text> Panel
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
});

export default HelpBar;

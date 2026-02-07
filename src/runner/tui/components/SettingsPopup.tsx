/**
 * SettingsPopup - A modal popup for adjusting per-project concurrent task limits
 *
 * Displays a list of projects with their current limits and allows adjustment with +/- keys.
 * j/k navigates, +/- adjusts limits, Escape closes.
 *
 * ┌── Settings ─────────────────────────┐
 * │  Per-project task limits            │
 * │                                     │
 * │ → brain-api         [3]             │
 * │   opencode          [2]             │
 * │   my-project        [no limit]      │
 * │                                     │
 * │  j/k: Navigate  +/-: Adjust  Esc: Close  │
 * └─────────────────────────────────────┘
 */

import React from 'react';
import { Box, Text } from 'ink';
import type { ProjectLimitEntry } from '../types';

export interface SettingsPopupProps {
  /** List of projects with their current limits */
  projects: ProjectLimitEntry[];
  /** Currently selected project index */
  selectedIndex: number;
}

export function SettingsPopup({
  projects,
  selectedIndex,
}: SettingsPopupProps): React.ReactElement {
  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor="cyan"
      paddingX={2}
      paddingY={1}
    >
      {/* Header */}
      <Box marginBottom={1} flexDirection="column">
        <Text bold color="cyan">Settings</Text>
        <Text dimColor>Per-project task limits</Text>
      </Box>

      {/* Project list */}
      <Box flexDirection="column">
        {projects.length === 0 ? (
          <Text dimColor>No projects available</Text>
        ) : (
          projects.map((entry, index) => {
            const isSelected = index === selectedIndex;
            const limitDisplay = entry.limit === undefined 
              ? 'no limit' 
              : String(entry.limit);

            return (
              <Box key={entry.projectId}>
                {/* Selection indicator */}
                <Text color={isSelected ? 'cyan' : undefined}>
                  {isSelected ? '→ ' : '  '}
                </Text>
                
                {/* Project name (fixed width for alignment) */}
                <Box width={20}>
                  <Text
                    color={isSelected ? 'cyan' : undefined}
                    bold={isSelected}
                  >
                    {entry.projectId.length > 18 
                      ? entry.projectId.slice(0, 15) + '...' 
                      : entry.projectId}
                  </Text>
                </Box>
                
                {/* Limit value */}
                <Text
                  color={entry.limit === undefined ? 'gray' : 'green'}
                  bold={isSelected}
                >
                  [{limitDisplay}]
                </Text>
                
                {/* Running count context */}
                <Text dimColor>
                  {' '}({entry.running} running)
                </Text>
              </Box>
            );
          })
        )}
      </Box>

      {/* Footer with shortcuts */}
      <Box marginTop={1} borderStyle="single" borderTop borderBottom={false} borderLeft={false} borderRight={false} borderColor="gray">
        <Text dimColor>
          j/k: Navigate  +/-: Adjust  0: No limit  Esc: Close
        </Text>
      </Box>
    </Box>
  );
}

export default SettingsPopup;

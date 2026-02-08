/**
 * SettingsPopup - A modal popup for adjusting settings
 *
 * Supports two sections:
 * 1. Per-project task limits - Adjust concurrent task limits per project
 * 2. Visible groups - Toggle which task status groups to display
 *
 * Tab switches between sections. j/k navigates, Space toggles, +/- adjusts limits.
 *
 * ┌── Settings ─────────────────────────┐
 * │  [Limits]  [Groups]                 │
 * │                                     │
 * │  Per-project task limits            │
 * │ → brain-api         [3]             │
 * │   opencode          [2]             │
 * │                                     │
 * │  Tab: Section  j/k: Navigate  Esc: Close  │
 * └─────────────────────────────────────┘
 */

import React from 'react';
import { Box, Text } from 'ink';
import type { ProjectLimitEntry, GroupVisibilityEntry, SettingsSection } from '../types';

export interface SettingsPopupProps {
  /** List of projects with their current limits */
  projects: ProjectLimitEntry[];
  /** Currently selected index (in current section) */
  selectedIndex: number;
  /** Current settings section */
  section?: SettingsSection;
  /** Group visibility entries */
  groups?: GroupVisibilityEntry[];
}

export function SettingsPopup({
  projects,
  selectedIndex,
  section = 'limits',
  groups = [],
}: SettingsPopupProps): React.ReactElement {
  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor="cyan"
      paddingX={2}
      paddingY={1}
      minWidth={45}
    >
      {/* Header with section tabs */}
      <Box marginBottom={1} flexDirection="column">
        <Text bold color="cyan">Settings</Text>
        <Box marginTop={1}>
          <Text
            color={section === 'limits' ? 'cyan' : 'gray'}
            bold={section === 'limits'}
            backgroundColor={section === 'limits' ? undefined : undefined}
          >
            {section === 'limits' ? '[Limits]' : ' Limits '}
          </Text>
          <Text> </Text>
          <Text
            color={section === 'groups' ? 'cyan' : 'gray'}
            bold={section === 'groups'}
          >
            {section === 'groups' ? '[Groups]' : ' Groups '}
          </Text>
        </Box>
      </Box>

      {/* Section content */}
      {section === 'limits' ? (
        <>
          <Text dimColor>Per-project task limits</Text>
          <Box flexDirection="column" marginTop={1}>
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
        </>
      ) : (
        <>
          <Text dimColor>Toggle visibility and collapse state</Text>
          <Box flexDirection="column" marginTop={1}>
            {groups.length === 0 ? (
              <Text dimColor>No groups available</Text>
            ) : (
              groups.map((entry, index) => {
                const isSelected = index === selectedIndex;
                const visibleIcon = entry.visible ? '✓' : '○';
                const collapseIcon = entry.collapsed ? '▶' : '▾';

                return (
                  <Box key={entry.status}>
                    {/* Selection indicator */}
                    <Text color={isSelected ? 'cyan' : undefined}>
                      {isSelected ? '→ ' : '  '}
                    </Text>
                    
                    {/* Visibility checkbox */}
                    <Text
                      color={entry.visible ? 'green' : 'gray'}
                      bold={isSelected}
                    >
                      {visibleIcon}
                    </Text>
                    <Text> </Text>
                    
                    {/* Collapse indicator (only shown if visible) */}
                    <Text
                      color={entry.visible ? 'white' : 'gray'}
                      dimColor={!entry.visible}
                    >
                      {entry.visible ? collapseIcon : ' '}
                    </Text>
                    <Text> </Text>
                    
                    {/* Status label (fixed width for alignment) */}
                    <Box width={14}>
                      <Text
                        color={isSelected ? 'cyan' : (entry.visible ? undefined : 'gray')}
                        bold={isSelected}
                        dimColor={!entry.visible}
                      >
                        {entry.label}
                      </Text>
                    </Box>
                    
                    {/* Task count */}
                    <Text
                      color={entry.taskCount > 0 ? 'yellow' : 'gray'}
                      dimColor={!entry.visible}
                    >
                      ({entry.taskCount})
                    </Text>
                  </Box>
                );
              })
            )}
          </Box>
        </>
      )}

      {/* Footer with shortcuts */}
      <Box marginTop={1} borderStyle="single" borderTop borderBottom={false} borderLeft={false} borderRight={false} borderColor="gray">
        <Text dimColor>
          {section === 'limits' 
            ? 'Tab: Section  j/k: Nav  +/-: Adjust  0: No limit  Esc: Close'
            : 'Tab: Section  j/k: Nav  Space: Toggle  c: Collapse  Esc: Close'
          }
        </Text>
      </Box>
    </Box>
  );
}

export default SettingsPopup;

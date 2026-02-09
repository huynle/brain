/**
 * SettingsPopup - A modal popup for adjusting per-project concurrent task limits and group visibility
 *
 * Tab switches between Limits and Groups sections.
 * 
 * Limits section:
 * - j/k navigates projects, +/- adjusts limits, 0 removes limit
 *
 * Groups section:
 * - j/k navigates status groups, Space toggles visibility, c toggles collapse
 *
 * ┌── Settings ─────────────────────────┐
 * │  [Limits]  Groups                   │
 * │                                     │
 * │ → brain-api         [3]             │
 * │   opencode          [2]             │
 * │   my-project        [no limit]      │
 * │                                     │
 * │  Tab: Section  j/k: Nav  +/-: Adjust  Esc: Close  │
 * └─────────────────────────────────────┘
 */

import React from 'react';
import { Box, Text } from 'ink';
import type { ProjectLimitEntry, GroupVisibilityEntry, SettingsSection } from '../types';

export interface SettingsPopupProps {
  /** List of projects with their current limits */
  projects: ProjectLimitEntry[];
  /** Currently selected index (project or group depending on section) */
  selectedIndex: number;
  /** Current active section */
  section?: SettingsSection;
  /** List of status groups with visibility settings */
  groups?: GroupVisibilityEntry[];
}

export function SettingsPopup({
  projects,
  selectedIndex,
  section = 'limits',
  groups = [],
}: SettingsPopupProps): React.ReactElement {
  const isLimitsSection = section === 'limits';
  const isGroupsSection = section === 'groups';

  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor="cyan"
      paddingX={2}
      paddingY={1}
    >
      {/* Header with tab bar */}
      <Box marginBottom={1} flexDirection="column">
        <Text bold color="cyan">Settings</Text>
        <Box>
          {/* Limits tab */}
          <Text 
            color={isLimitsSection ? 'cyan' : 'gray'}
            bold={isLimitsSection}
          >
            {isLimitsSection ? '[Limits]' : ' Limits '}
          </Text>
          <Text> </Text>
          {/* Groups tab */}
          <Text 
            color={isGroupsSection ? 'cyan' : 'gray'}
            bold={isGroupsSection}
          >
            {isGroupsSection ? '[Groups]' : ' Groups '}
          </Text>
        </Box>
      </Box>

      {/* Limits section content */}
      {isLimitsSection && (
        <Box flexDirection="column">
          <Text dimColor>Per-project task limits</Text>
          <Box marginTop={1} flexDirection="column">
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
        </Box>
      )}

      {/* Groups section content */}
      {isGroupsSection && (
        <Box flexDirection="column">
          <Text dimColor>Status group visibility</Text>
          <Box marginTop={1} flexDirection="column">
            {groups.length === 0 ? (
              <Text dimColor>No groups available</Text>
            ) : (
              groups.map((entry, index) => {
                const isSelected = index === selectedIndex;
                // Visibility indicator: checkmark for visible, empty circle for hidden
                const visibilityIcon = entry.visible ? '✓' : '○';
                // Collapse indicator: arrow right for collapsed, arrow down for expanded
                const collapseIcon = entry.collapsed ? '▸' : '▾';

                return (
                  <Box key={entry.status}>
                    {/* Selection indicator */}
                    <Text color={isSelected ? 'cyan' : undefined}>
                      {isSelected ? '→ ' : '  '}
                    </Text>
                    
                    {/* Visibility checkbox */}
                    <Text
                      color={entry.visible ? 'green' : 'gray'}
                    >
                      {visibilityIcon}
                    </Text>
                    <Text> </Text>
                    
                    {/* Collapse indicator (only shown if visible) */}
                    <Text
                      color={entry.visible ? undefined : 'gray'}
                      dimColor={!entry.visible}
                    >
                      {entry.visible ? collapseIcon : ' '}
                    </Text>
                    <Text> </Text>
                    
                    {/* Status label (fixed width for alignment) */}
                    <Box width={14}>
                      <Text
                        color={isSelected ? 'cyan' : undefined}
                        bold={isSelected}
                        dimColor={!entry.visible}
                      >
                        {entry.label}
                      </Text>
                    </Box>
                    
                    {/* Task count */}
                    <Text
                      dimColor={!entry.visible}
                    >
                      ({entry.taskCount})
                    </Text>
                  </Box>
                );
              })
            )}
          </Box>
        </Box>
      )}

      {/* Footer with section-specific shortcuts */}
      <Box marginTop={1} borderStyle="single" borderTop borderBottom={false} borderLeft={false} borderRight={false} borderColor="gray">
        <Text dimColor>
          {isLimitsSection 
            ? 'Tab: Section  j/k: Nav  +/-: Adjust  0: No limit  Esc: Close'
            : 'Tab: Section  j/k: Nav  Space: Toggle  c: Collapse  Esc: Close'}
        </Text>
      </Box>
    </Box>
  );
}

export default SettingsPopup;

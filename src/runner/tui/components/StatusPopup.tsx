/**
 * StatusPopup - A modal popup for changing task status
 *
 * Displays a list of available statuses and allows navigation with j/k keys.
 * Enter confirms selection, Escape cancels.
 *
 * ┌── Change Status ──────────────────┐
 * │                                    │
 * │   ○ draft                          │
 * │   ○ pending                        │
 * │   ○ active                         │
 * │   ○ in_progress                    │
 * │   ○ blocked                        │
 * │   ○ cancelled                      │
 * │ → ● completed                      │
 * │   ○ validated                      │
 * │   ○ superseded                     │
 * │   ○ archived                       │
 * │                                    │
 * │  j/k: Navigate  Enter: Select  Esc: Cancel  │
 * └────────────────────────────────────┘
 */

import React from 'react';
import { Box, Text } from 'ink';
import type { EntryStatus } from '../../../core/types';
import { ENTRY_STATUSES } from '../../../core/types';
import { getStatusIcon, getStatusColor, getStatusLabel } from '../status-display';

export interface StatusPopupProps {
  /** Current status of the task (highlighted as current) */
  currentStatus: EntryStatus;
  /** Currently selected status in the list */
  selectedStatus: EntryStatus;
  /** Task title to show in header */
  taskTitle: string;
  /** When true, shows bulk mode with limited status options */
  bulkMode?: boolean;
  /** Feature ID for bulk mode (shown in header) */
  bulkFeatureId?: string;
  /** Number of tasks to be updated in bulk mode */
  bulkTaskCount?: number;
  /** Custom list of allowed statuses (for bulk mode) */
  allowedStatuses?: EntryStatus[];
}

// Status colors, icons, and labels are now imported from shared status-display.ts

export function StatusPopup({
  currentStatus,
  selectedStatus,
  taskTitle,
  bulkMode = false,
  bulkFeatureId,
  bulkTaskCount,
  allowedStatuses,
}: StatusPopupProps): React.ReactElement {
  // Truncate title if too long
  const maxTitleLen = 30;
  const displayTitle = taskTitle.length > maxTitleLen 
    ? taskTitle.slice(0, maxTitleLen - 3) + '...'
    : taskTitle;

  // Use allowed statuses if provided, otherwise all statuses
  const statusList = allowedStatuses ?? ENTRY_STATUSES;

  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor={bulkMode ? 'yellow' : 'cyan'}
      paddingX={2}
      paddingY={1}
    >
      {/* Header */}
      <Box marginBottom={1} flexDirection="column">
        {bulkMode ? (
          <>
            <Text bold color="yellow">Bulk Change Status</Text>
            <Text dimColor>Feature: {bulkFeatureId} ({bulkTaskCount} tasks)</Text>
          </>
        ) : (
          <>
            <Text bold color="cyan">Change Status</Text>
            <Text dimColor> - {displayTitle}</Text>
          </>
        )}
      </Box>

      {/* Status list */}
      <Box flexDirection="column">
        {statusList.map((status) => {
          const isSelected = status === selectedStatus;
          const isCurrent = status === currentStatus;
          const color = getStatusColor(status);

          return (
            <Box key={status}>
              {/* Selection indicator */}
              <Text color={isSelected ? 'cyan' : undefined}>
                {isSelected ? '→ ' : '  '}
              </Text>
              
              {/* Status icon - use the same icons as TaskTree */}
              <Text color={color}>
                {getStatusIcon(status)}
              </Text>
              
              {/* Status label */}
              <Text
                color={isSelected ? color : undefined}
                bold={isSelected}
                dimColor={!isSelected && !isCurrent}
              >
                {' '}{getStatusLabel(status)}
              </Text>
              
              {/* Current indicator */}
              {isCurrent && !bulkMode && (
                <Text dimColor> (current)</Text>
              )}
            </Box>
          );
        })}
      </Box>

      {/* Footer with shortcuts */}
      <Box marginTop={1} borderStyle="single" borderTop borderBottom={false} borderLeft={false} borderRight={false} borderColor="gray">
        <Text dimColor>
          j/k: Navigate  Enter: Select  Esc: Cancel
        </Text>
      </Box>
    </Box>
  );
}

export default StatusPopup;

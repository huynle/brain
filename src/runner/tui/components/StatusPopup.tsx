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

export interface StatusPopupProps {
  /** Current status of the task (highlighted as current) */
  currentStatus: EntryStatus;
  /** Currently selected status in the list */
  selectedStatus: EntryStatus;
  /** Task title to show in header */
  taskTitle: string;
}

/** Get a color for each status */
function getStatusColor(status: EntryStatus): string {
  switch (status) {
    case 'draft':
      return 'gray';
    case 'pending':
      return 'yellow';
    case 'active':
      return 'blue';
    case 'in_progress':
      return 'cyan';
    case 'blocked':
      return 'red';
    case 'cancelled':
      return 'magenta';
    case 'completed':
      return 'green';
    case 'validated':
      return 'greenBright';
    case 'superseded':
      return 'gray';
    case 'archived':
      return 'gray';
    default:
      return 'white';
  }
}

/** Get display label for status */
function getStatusLabel(status: EntryStatus): string {
  return status.replace('_', ' ');
}

export function StatusPopup({
  currentStatus,
  selectedStatus,
  taskTitle,
}: StatusPopupProps): React.ReactElement {
  // Truncate title if too long
  const maxTitleLen = 30;
  const displayTitle = taskTitle.length > maxTitleLen 
    ? taskTitle.slice(0, maxTitleLen - 3) + '...'
    : taskTitle;

  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor="cyan"
      paddingX={2}
      paddingY={1}
    >
      {/* Header */}
      <Box marginBottom={1}>
        <Text bold color="cyan">Change Status</Text>
        <Text dimColor> - {displayTitle}</Text>
      </Box>

      {/* Status list */}
      <Box flexDirection="column">
        {ENTRY_STATUSES.map((status) => {
          const isSelected = status === selectedStatus;
          const isCurrent = status === currentStatus;
          const color = getStatusColor(status);

          return (
            <Box key={status}>
              {/* Selection indicator */}
              <Text color={isSelected ? 'cyan' : undefined}>
                {isSelected ? '→ ' : '  '}
              </Text>
              
              {/* Radio-style indicator */}
              <Text color={color}>
                {isCurrent ? '●' : '○'}
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
              {isCurrent && (
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

/**
 * DeleteConfirmPopup - A confirmation popup for deleting tasks
 *
 * Displays a destructive action confirmation with task count and titles.
 * Uses red border to indicate destructive action.
 *
 * ┌─── Delete Tasks ────────────────────┐
 * │                                     │
 * │  Delete 3 task(s)?                  │
 * │                                     │
 * │  • Fix login bug                    │
 * │  • Add dark mode                    │
 * │  • Update docs                      │
 * │                                     │
 * │  This action cannot be undone.      │
 * │                                     │
 * │  Enter: Confirm  Esc: Cancel        │
 * └─────────────────────────────────────┘
 */

import React from 'react';
import { Box, Text } from 'ink';

export interface DeleteConfirmPopupProps {
  /** List of task titles to be deleted */
  taskTitles: string[];
  /** Optional: feature ID if deleting a feature group */
  featureId?: string;
}

/** Maximum number of task titles to display before truncating */
const MAX_VISIBLE_TASKS = 5;
/** Maximum length for task title display */
const MAX_TITLE_LENGTH = 40;

export function DeleteConfirmPopup({
  taskTitles,
  featureId,
}: DeleteConfirmPopupProps): React.ReactElement {
  const taskCount = taskTitles.length;
  const visibleTitles = taskTitles.slice(0, MAX_VISIBLE_TASKS);
  const hiddenCount = taskCount - visibleTitles.length;

  // Truncate long titles
  const truncateTitle = (title: string): string => {
    if (title.length <= MAX_TITLE_LENGTH) return title;
    return title.slice(0, MAX_TITLE_LENGTH - 3) + '...';
  };

  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor="red"
      paddingX={2}
      paddingY={1}
    >
      {/* Header */}
      <Box marginBottom={1}>
        <Text bold color="red">Delete Tasks</Text>
      </Box>

      {/* Confirmation message */}
      <Box marginBottom={1}>
        <Text>Delete </Text>
        <Text bold color="red">{taskCount}</Text>
        <Text> task{taskCount !== 1 ? 's' : ''}?</Text>
        {featureId && (
          <Text dimColor> (feature: {featureId})</Text>
        )}
      </Box>

      {/* Task list */}
      <Box flexDirection="column" marginBottom={1}>
        {visibleTitles.map((title, index) => (
          <Box key={index}>
            <Text dimColor>  • </Text>
            <Text>{truncateTitle(title)}</Text>
          </Box>
        ))}
        {hiddenCount > 0 && (
          <Box>
            <Text dimColor>  ... and {hiddenCount} more</Text>
          </Box>
        )}
      </Box>

      {/* Warning */}
      <Box marginBottom={1}>
        <Text color="yellow">This action cannot be undone.</Text>
      </Box>

      {/* Footer with shortcuts */}
      <Box borderStyle="single" borderTop borderBottom={false} borderLeft={false} borderRight={false} borderColor="gray">
        <Text dimColor>Enter: Confirm  Esc: Cancel</Text>
      </Box>
    </Box>
  );
}

export default DeleteConfirmPopup;

/**
 * PausePopup - A modal popup for pausing/resuming project execution
 *
 * Displays the project pause state with toggle option.
 * Enter toggles pause state, Escape cancels.
 *
 * When paused with focus mode:
 * ┌── Pause/Resume ────────────────────┐
 * │                                    │
 * │ → ⏸ Project (paused)               │
 * │                                    │
 * │   Focus: auth-system               │
 * │                                    │
 * │  Enter: Toggle  Esc: Cancel        │
 * └────────────────────────────────────┘
 *
 * When running:
 * ┌── Pause/Resume ────────────────────┐
 * │                                    │
 * │ → ▶ Project (running)              │
 * │                                    │
 * │  Enter: Toggle  Esc: Cancel        │
 * └────────────────────────────────────┘
 */

import React from 'react';
import { Box, Text } from 'ink';

export interface PausePopupProps {
  /** Project ID being targeted */
  projectId: string;
  /** Whether the project is currently paused */
  isProjectPaused?: boolean;
  /** Currently focused feature (for display only) */
  focusedFeature?: string | null;
  /** Callback to pause the project */
  onPauseProject?: () => void;
  /** Callback to unpause the project */
  onUnpauseProject?: () => void;
  /** Callback to close the popup */
  onClose?: () => void;
}

export function PausePopup({
  projectId,
  isProjectPaused = false,
  focusedFeature,
}: PausePopupProps): React.ReactElement {
  // Truncate IDs if too long
  const maxIdLen = 20;
  const displayProjectId = projectId.length > maxIdLen
    ? projectId.slice(0, maxIdLen - 3) + '...'
    : projectId;

  // Special display for ungrouped feature ID
  const UNGROUPED_FEATURE_ID = '__ungrouped__';
  const displayFocusedFeature = focusedFeature === UNGROUPED_FEATURE_ID 
    ? 'Ungrouped Tasks' 
    : focusedFeature;

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
        <Text bold color="cyan">Pause/Resume</Text>
      </Box>

      {/* Project status */}
      <Box>
        {/* Selection indicator */}
        <Text color="cyan">→ </Text>
        
        {/* Pause status icon */}
        <Text color={isProjectPaused ? 'yellow' : 'green'}>
          {isProjectPaused ? '⏸' : '▶'}
        </Text>
        
        {/* Project label */}
        <Text color="cyan" bold>
          {' '}Project: {displayProjectId}
        </Text>
        
        {/* Current state indicator */}
        <Text color={isProjectPaused ? 'yellow' : 'green'}>
          {' '}({isProjectPaused ? 'paused' : 'running'})
        </Text>
      </Box>

      {/* Show focused feature if active */}
      {focusedFeature && (
        <Box marginTop={1}>
          <Text dimColor>  Focus: </Text>
          <Text color="green">{displayFocusedFeature}</Text>
        </Box>
      )}

      {/* Footer with shortcuts */}
      <Box marginTop={1} borderStyle="single" borderTop borderBottom={false} borderLeft={false} borderRight={false} borderColor="gray">
        <Text dimColor>
          Enter: Toggle  Esc: Cancel
        </Text>
      </Box>
    </Box>
  );
}

export default PausePopup;

/**
 * PausePopup - A modal popup for pausing project or feature execution
 *
 * Displays options to pause either the project or a specific feature.
 * j/k navigates options, Enter confirms selection, Escape cancels.
 *
 * When feature is available:
 * ┌── Pause/Resume ────────────────────┐
 * │                                    │
 * │ → ○ Project                        │
 * │   ○ Feature: auth-system           │
 * │                                    │
 * │  j/k: Navigate  Enter: Select  Esc: Cancel  │
 * └────────────────────────────────────┘
 *
 * When no feature:
 * ┌── Pause/Resume ────────────────────┐
 * │                                    │
 * │ → ○ Project                        │
 * │                                    │
 * │  Enter: Confirm  Esc: Cancel       │
 * └────────────────────────────────────┘
 */

import React from 'react';
import { Box, Text } from 'ink';

export type PauseTarget = 'project' | 'feature';

export interface PausePopupProps {
  /** Project ID being targeted */
  projectId: string;
  /** Feature ID if available (optional) */
  featureId?: string;
  /** Currently selected option */
  selectedTarget: PauseTarget;
  /** Whether the project is currently paused */
  isProjectPaused?: boolean;
  /** Whether the feature is currently paused */
  isFeaturePaused?: boolean;
}

export function PausePopup({
  projectId,
  featureId,
  selectedTarget,
  isProjectPaused = false,
  isFeaturePaused = false,
}: PausePopupProps): React.ReactElement {
  // Truncate IDs if too long
  const maxIdLen = 20;
  const displayProjectId = projectId.length > maxIdLen
    ? projectId.slice(0, maxIdLen - 3) + '...'
    : projectId;
  const displayFeatureId = featureId && featureId.length > maxIdLen
    ? featureId.slice(0, maxIdLen - 3) + '...'
    : featureId;

  const hasFeature = !!featureId;

  // Options to show
  const options: Array<{ target: PauseTarget; label: string; isPaused: boolean }> = [
    { 
      target: 'project', 
      label: `Project: ${displayProjectId}`,
      isPaused: isProjectPaused,
    },
  ];

  if (hasFeature) {
    options.push({
      target: 'feature',
      label: `Feature: ${displayFeatureId}`,
      isPaused: isFeaturePaused,
    });
  }

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

      {/* Options list */}
      <Box flexDirection="column">
        {options.map((option) => {
          const isSelected = option.target === selectedTarget;
          
          return (
            <Box key={option.target}>
              {/* Selection indicator */}
              <Text color={isSelected ? 'cyan' : undefined}>
                {isSelected ? '→ ' : '  '}
              </Text>
              
              {/* Pause status icon */}
              <Text color={option.isPaused ? 'yellow' : 'green'}>
                {option.isPaused ? '⏸' : '▶'}
              </Text>
              
              {/* Option label */}
              <Text
                color={isSelected ? 'cyan' : undefined}
                bold={isSelected}
              >
                {' '}{option.label}
              </Text>
              
              {/* Current state indicator */}
              <Text dimColor>
                {' '}({option.isPaused ? 'paused' : 'running'})
              </Text>
            </Box>
          );
        })}
      </Box>

      {/* Footer with shortcuts */}
      <Box marginTop={1} borderStyle="single" borderTop borderBottom={false} borderLeft={false} borderRight={false} borderColor="gray">
        <Text dimColor>
          {hasFeature
            ? 'j/k: Navigate  Enter: Toggle  Esc: Cancel'
            : 'Enter: Toggle  Esc: Cancel'}
        </Text>
      </Box>
    </Box>
  );
}

export default PausePopup;

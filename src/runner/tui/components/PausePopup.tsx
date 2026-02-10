/**
 * PausePopup - A modal popup for pausing project or feature execution
 *
 * Displays options to pause either the project or a specific feature.
 * j/k navigates options, Enter confirms selection, Escape cancels.
 *
 * Feature state display logic:
 * - Project paused + feature not enabled: "(paused - project)" in yellow
 * - Project paused + feature enabled: "(enabled)" in green  
 * - Project running + feature paused: "(paused)" in yellow
 * - Project running + feature not paused: "(running)" in green
 *
 * When project is paused AND feature is available, shows enable/disable option:
 * ┌── Pause/Resume ────────────────────┐
 * │ 1 feature enabled                  │
 * │                                    │
 * │ → ⏸ Project (paused)               │
 * │   ⏸ Feature: auth-system (paused - project) │
 * │                                    │
 * │  j/k: Navigate  Enter: Toggle  e: Enable Feature  Esc: Cancel  │
 * └────────────────────────────────────┘
 *
 * When feature is enabled (whitelisted to run while project paused):
 * ┌── Pause/Resume ────────────────────┐
 * │ 1 feature enabled                  │
 * │                                    │
 * │ → ⏸ Project (paused)               │
 * │   ▶ Feature: auth-system (enabled) │
 * │                                    │
 * │  j/k: Navigate  Enter: Toggle  e: Disable Feature  Esc: Cancel │
 * └────────────────────────────────────┘
 *
 * When feature is available (project running):
 * ┌── Pause/Resume ────────────────────┐
 * │                                    │
 * │ → ▶ Project (running)              │
 * │   ▶ Feature: auth-system (running) │
 * │                                    │
 * │  j/k: Navigate  Enter: Toggle  Esc: Cancel  │
 * └────────────────────────────────────┘
 *
 * When no feature:
 * ┌── Pause/Resume ────────────────────┐
 * │                                    │
 * │ → ▶ Project (running)              │
 * │                                    │
 * │  Enter: Toggle  Esc: Cancel        │
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
  /** Set of enabled feature IDs (can run when project is paused) */
  enabledFeatures?: Set<string>;
}

export function PausePopup({
  projectId,
  featureId,
  selectedTarget,
  isProjectPaused = false,
  isFeaturePaused = false,
  enabledFeatures,
}: PausePopupProps): React.ReactElement {
  // Truncate IDs if too long
  const maxIdLen = 20;
  const displayProjectId = projectId.length > maxIdLen
    ? projectId.slice(0, maxIdLen - 3) + '...'
    : projectId;
  
  // Special display for ungrouped feature ID
  const UNGROUPED_FEATURE_ID = '__ungrouped__';
  const isUngrouped = featureId === UNGROUPED_FEATURE_ID;
  const rawDisplayFeatureId = isUngrouped 
    ? 'Ungrouped Tasks' 
    : featureId;
  const displayFeatureId = rawDisplayFeatureId && rawDisplayFeatureId.length > maxIdLen
    ? rawDisplayFeatureId.slice(0, maxIdLen - 3) + '...'
    : rawDisplayFeatureId;

  const hasFeature = !!featureId;
  const isFeatureEnabled = featureId && enabledFeatures?.has(featureId);
  const enabledCount = enabledFeatures?.size ?? 0;

  // Calculate effective feature state based on project pause + feature enable status
  const getFeatureState = (): { isPaused: boolean; stateText: string; stateColor: string } => {
    if (isProjectPaused) {
      // Project is paused - check if feature is whitelisted/enabled
      if (isFeatureEnabled) {
        return { isPaused: false, stateText: 'enabled', stateColor: 'green' };
      }
      return { isPaused: true, stateText: 'paused - project', stateColor: 'yellow' };
    }
    
    // Project is running - check feature's own pause state
    if (isFeaturePaused) {
      return { isPaused: true, stateText: 'paused', stateColor: 'yellow' };
    }
    
    return { isPaused: false, stateText: 'running', stateColor: 'green' };
  };

  const featureState = hasFeature ? getFeatureState() : null;

  // Options to show
  const options: Array<{ target: PauseTarget; label: string; isPaused: boolean; stateText?: string; stateColor?: string }> = [
    { 
      target: 'project', 
      label: `Project: ${displayProjectId}`,
      isPaused: isProjectPaused,
      stateText: isProjectPaused ? 'paused' : 'running',
      stateColor: isProjectPaused ? 'yellow' : 'green',
    },
  ];

  if (hasFeature && featureState) {
    options.push({
      target: 'feature',
      label: `Feature: ${displayFeatureId}`,
      isPaused: featureState.isPaused,
      stateText: featureState.stateText,
      stateColor: featureState.stateColor,
    });
  }

  // Determine if enable/disable feature option should be shown
  // Only show when: project is paused AND feature is selected
  const showEnableOption = isProjectPaused && hasFeature;

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
        <Text bold color="cyan">Pause/Resume</Text>
        {/* Show enabled features count when project is paused */}
        {isProjectPaused && enabledCount > 0 && (
          <Text color="green">
            {enabledCount} feature{enabledCount > 1 ? 's' : ''} enabled
          </Text>
        )}
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
              
              {/* Current state indicator - using computed state text and color */}
              <Text color={option.stateColor} dimColor={!option.stateColor}>
                {' '}({option.stateText})
              </Text>
            </Box>
          );
        })}
      </Box>

      {/* Footer with shortcuts */}
      <Box marginTop={1} borderStyle="single" borderTop borderBottom={false} borderLeft={false} borderRight={false} borderColor="gray">
        <Text dimColor>
          {showEnableOption
            ? `j/k: Navigate  Enter: Toggle  e: ${isFeatureEnabled ? 'Disable' : 'Enable'} Feature  Esc: Cancel`
            : hasFeature
              ? 'j/k: Navigate  Enter: Toggle  Esc: Cancel'
              : 'Enter: Toggle  Esc: Cancel'}
        </Text>
      </Box>
    </Box>
  );
}

export default PausePopup;

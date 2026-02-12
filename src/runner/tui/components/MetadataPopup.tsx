/**
 * MetadataPopup - A modal popup for batch updating task metadata
 *
 * Displays editable fields for status, feature_id, git_branch, and target_workdir.
 * Supports single task, batch mode, and feature mode.
 *
 * Uses a 3-mode state machine for keyboard navigation:
 * - NAVIGATE: j/k moves between fields, Enter enters edit mode, Esc closes popup
 * - EDIT_TEXT: typing updates buffer, Enter saves field immediately, Esc discards
 * - EDIT_STATUS: j/k cycles status options, Enter saves immediately, Esc discards
 *
 * ┌─ Update Metadata ──────────────────────┐
 * │                                        │
 * │  3 tasks selected                      │
 * │                                        │
 * │  Status:      ● pending                │
 * │  Feature ID:  dark-mode                │
 * │  Branch:      feature/dark-mode        │
 * │  Workdir:     /path/to/project         │
 * │                                        │
 * │  j/k: navigate  Enter: edit  Esc: close│
 * └────────────────────────────────────────┘
 */

import React from 'react';
import { Box, Text } from 'ink';
import type { EntryStatus } from '../../../core/types';
import { getStatusIcon, getStatusColor, getStatusLabel } from '../status-display';

/** Fields that can be focused in the metadata popup */
export type MetadataField = 'status' | 'feature_id' | 'git_branch' | 'target_workdir';

/** Mode for the metadata popup */
export type MetadataPopupMode = 'single' | 'batch' | 'feature';

/** Interaction mode for the 3-mode state machine */
export type MetadataInteractionMode = 'navigate' | 'edit_text' | 'edit_status';

export interface MetadataPopupProps {
  /** Mode: single task, batch of tasks, or feature group */
  mode: MetadataPopupMode;
  /** Task title (for single mode header) */
  taskTitle?: string;
  /** Number of tasks selected (for batch mode) */
  batchCount?: number;
  /** Feature ID (for feature mode header) */
  featureId?: string;
  /** Which field is currently focused */
  focusedField: MetadataField;
  /** Current status value */
  statusValue: EntryStatus;
  /** Current feature_id value */
  featureIdValue: string;
  /** Current git_branch value */
  branchValue: string;
  /** Current target_workdir value */
  workdirValue: string;
  /** Index of selected status in dropdown (for status field navigation) */
  selectedStatusIndex: number;
  /** Allowed statuses for the dropdown */
  allowedStatuses: readonly EntryStatus[];
  /** Current interaction mode (3-mode state machine) */
  interactionMode: MetadataInteractionMode;
  /** Current edit buffer for text fields or status selection */
  editBuffer?: string;
}

const FIELD_ORDER: MetadataField[] = ['status', 'feature_id', 'git_branch', 'target_workdir'];
const FIELD_LABELS: Record<MetadataField, string> = {
  status: 'Status',
  feature_id: 'Feature ID',
  git_branch: 'Branch',
  target_workdir: 'Workdir',
};

export function MetadataPopup({
  mode,
  taskTitle,
  batchCount,
  featureId,
  focusedField,
  statusValue,
  featureIdValue,
  branchValue,
  workdirValue,
  selectedStatusIndex,
  allowedStatuses,
  interactionMode,
  editBuffer = '',
}: MetadataPopupProps): React.ReactElement {
  // Truncate title if too long
  const maxTitleLen = 30;
  const displayTitle = taskTitle && taskTitle.length > maxTitleLen
    ? taskTitle.slice(0, maxTitleLen - 3) + '...'
    : taskTitle;

  // Get value for a field
  const getFieldValue = (field: MetadataField): string => {
    switch (field) {
      case 'status':
        return getStatusLabel(statusValue);
      case 'feature_id':
        return featureIdValue || '(none)';
      case 'git_branch':
        return branchValue || '(none)';
      case 'target_workdir':
        return workdirValue || '(none)';
    }
  };

  // Determine if a field is in edit mode based on interaction mode and focus
  const isFieldEditing = (field: MetadataField): boolean => {
    if (field === 'status') {
      return interactionMode === 'edit_status' && focusedField === 'status';
    }
    return interactionMode === 'edit_text' && focusedField === field;
  };

  // Determine border color based on mode
  const borderColor = mode === 'batch' ? 'yellow' : mode === 'feature' ? 'magenta' : 'cyan';
  const headerColor = borderColor;

  // Help text based on interaction mode
  const getHelpText = (): string => {
    switch (interactionMode) {
      case 'edit_status':
        return 'j/k: select status  Enter: save  Esc: cancel';
      case 'edit_text':
        return 'Type to edit  Enter: save  Esc: cancel';
      case 'navigate':
      default:
        return 'j/k: navigate  Enter: edit  Esc: close';
    }
  };

  return (
    <Box
      flexDirection="column"
      borderStyle="round"
      borderColor={borderColor}
      paddingX={2}
      paddingY={1}
    >
      {/* Header */}
      <Box marginBottom={1} flexDirection="column">
        <Text bold color={headerColor}>Update Metadata</Text>
        {mode === 'single' && displayTitle && (
          <Text dimColor> - {displayTitle}</Text>
        )}
        {mode === 'batch' && batchCount !== undefined && (
          <Text dimColor>{batchCount} tasks selected</Text>
        )}
        {mode === 'feature' && featureId && (
          <Text dimColor>Feature: {featureId}{batchCount !== undefined ? ` (${batchCount} tasks)` : ''}</Text>
        )}
      </Box>

      {/* Fields */}
      <Box flexDirection="column">
        {FIELD_ORDER.map((field) => {
          const isFocused = field === focusedField;
          const isEditing = isFieldEditing(field);
          const label = FIELD_LABELS[field];
          const value = getFieldValue(field);

          return (
            <Box key={field}>
              {/* Selection indicator */}
              <Text color={isFocused ? 'cyan' : undefined}>
                {isFocused ? '→ ' : '  '}
              </Text>

              {/* Field label (fixed width for alignment) */}
              <Box width={14}>
                <Text
                  color={isFocused ? 'cyan' : undefined}
                  bold={isFocused}
                >
                  {label}:
                </Text>
              </Box>

              {/* Field value */}
              {field === 'status' ? (
                // Status field: show icon and label
                <Box flexDirection="column">
                  <Box>
                    <Text color={getStatusColor(allowedStatuses[selectedStatusIndex] || statusValue)}>
                      {getStatusIcon(allowedStatuses[selectedStatusIndex] || statusValue)}
                    </Text>
                    <Text
                      color={isFocused ? getStatusColor(allowedStatuses[selectedStatusIndex] || statusValue) : undefined}
                      bold={isFocused}
                    >
                      {' '}{getStatusLabel(allowedStatuses[selectedStatusIndex] || statusValue)}
                    </Text>
                    {isFocused && !isEditing && (
                      <Text dimColor> (Enter to select)</Text>
                    )}
                  </Box>
                  {/* Status sub-popup when in edit_status mode */}
                  {isEditing && (
                    <Box
                      flexDirection="column"
                      borderStyle="round"
                      borderColor="cyan"
                      marginTop={1}
                      marginLeft={2}
                      paddingX={1}
                    >
                      <Text bold color="cyan">Select Status</Text>
                      {allowedStatuses.map((status, idx) => {
                        const isSelected = idx === selectedStatusIndex;
                        return (
                          <Box key={status}>
                            <Text color={isSelected ? 'cyan' : undefined}>
                              {isSelected ? '→ ' : '  '}
                            </Text>
                            <Text color={getStatusColor(status)}>
                              {isSelected ? '●' : '○'}
                            </Text>
                            <Text
                              color={isSelected ? getStatusColor(status) : undefined}
                              bold={isSelected}
                            >
                              {' '}{getStatusLabel(status)}
                            </Text>
                          </Box>
                        );
                      })}
                    </Box>
                  )}
                </Box>
              ) : isEditing ? (
                // Text field in edit mode: show edit buffer with cursor
                <Box>
                  <Text color="cyan" bold>
                    {editBuffer}
                  </Text>
                  <Text color="cyan" bold inverse>
                    {' '}
                  </Text>
                </Box>
              ) : (
                // Text field: show value
                <Box>
                  <Text
                    color={isFocused ? 'cyan' : undefined}
                    bold={isFocused}
                    dimColor={!isFocused && (value === '(none)')}
                  >
                    {value}
                  </Text>
                  {isFocused && (
                    <Text dimColor> (Enter to edit)</Text>
                  )}
                </Box>
              )}
            </Box>
          );
        })}
      </Box>

      {/* Footer with shortcuts */}
      <Box marginTop={1} borderStyle="single" borderTop borderBottom={false} borderLeft={false} borderRight={false} borderColor="gray">
        <Text dimColor>
          {getHelpText()}
        </Text>
      </Box>
    </Box>
  );
}

export default MetadataPopup;

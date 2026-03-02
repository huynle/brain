/**
 * MetadataPopup - A modal popup for batch updating task metadata
 *
 * Displays editable fields for task metadata and feature execution settings.
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
import type {
  EntryStatus,
  MergePolicy,
  MergeStrategy,
  RemoteBranchPolicy,
  ExecutionMode,
} from '../../../core/types';
import { getStatusIcon, getStatusColor, getStatusLabel } from '../status-display';

/** Fields that can be focused in the metadata popup */
export type MetadataField =
  | 'status'
  | 'feature_id'
  | 'git_branch'
  | 'merge_target_branch'
  | 'execution_mode'
  | 'checkout_enabled'
  | 'complete_on_idle'
  | 'merge_policy'
  | 'merge_strategy'
  | 'remote_branch_policy'
  | 'open_pr_before_merge'
  | 'target_workdir'
  | 'schedule'
  | 'schedule_enabled'
  | 'project'
  | 'agent'
  | 'model'
  | 'direct_prompt';

/** Mode for the metadata popup */
export type MetadataPopupMode = 'single' | 'batch' | 'feature';

/** Interaction mode for the 3-mode state machine */
export type MetadataInteractionMode = 'navigate' | 'edit_text' | 'edit_status' | 'edit_project';

export interface MetadataPopupProps {
  /** Width constraint for the popup (in columns). Prevents text overflow. */
  width?: number;
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
  /** Current merge_target_branch value */
  mergeTargetBranchValue?: string;
  /** Current execution_mode value */
  executionModeValue?: ExecutionMode;
  /** Current checkout_enabled value */
  checkoutEnabledValue?: boolean;
  /** Current complete_on_idle value */
  completeOnIdleValue?: boolean;
  /** Current merge_policy value */
  mergePolicyValue?: MergePolicy;
  /** Current merge_strategy value */
  mergeStrategyValue?: MergeStrategy;
  /** Current remote_branch_policy value */
  remoteBranchPolicyValue?: RemoteBranchPolicy;
  /** Current open_pr_before_merge value */
  openPrBeforeMergeValue?: boolean;
  /** Current target_workdir value */
  workdirValue: string;
  /** Current schedule value */
  scheduleValue: string;
  /** Current schedule_enabled value */
  scheduleEnabledValue?: boolean;
  /** Current project value */
  projectValue: string;
  /** Current agent value (OpenCode agent override) */
  agentValue: string;
  /** Current model value (LLM model override) */
  modelValue: string;
  /** Current direct_prompt value (bypasses do-work skill) */
  directPromptValue: string;
  /** Available projects for the dropdown */
  availableProjects: string[];
  /** Index of selected project in dropdown */
  selectedProjectIndex: number;
  /** Index of selected status in dropdown (for status field navigation) */
  selectedStatusIndex: number;
  /** Allowed statuses for the dropdown */
  allowedStatuses: readonly EntryStatus[];
  /** Current interaction mode (3-mode state machine) */
  interactionMode: MetadataInteractionMode;
  /** Current edit buffer for text fields or status selection */
  editBuffer?: string;
  /** Fields with mixed values across selected tasks (batch/feature mode only) */
  mixedFields?: ReadonlySet<MetadataField>;
}

export const METADATA_FIELDS_DEFAULT: MetadataField[] = [
  'status',
  'feature_id',
  'git_branch',
  'target_workdir',
  'schedule',
  'schedule_enabled',
  'project',
  'agent',
  'model',
  'direct_prompt',
];

/** Field groups for feature-mode popup (visual separators between groups) */
export const FEATURE_FIELD_GROUPS: Array<{ label: string; fields: MetadataField[] }> = [
  { label: 'Task', fields: ['status', 'feature_id', 'project'] },
  { label: 'Execution', fields: ['execution_mode', 'agent', 'model', 'direct_prompt', 'schedule', 'schedule_enabled', 'complete_on_idle'] },
  { label: 'Git / Branch', fields: ['git_branch', 'target_workdir', 'checkout_enabled'] },
  { label: 'Merge / PR', fields: ['merge_target_branch', 'merge_policy', 'merge_strategy', 'remote_branch_policy', 'open_pr_before_merge'] },
];

/** Flat merged array of all feature fields (for navigation) */
export const METADATA_FIELDS_FEATURE_SETTINGS: MetadataField[] =
  FEATURE_FIELD_GROUPS.flatMap(g => g.fields);

/** Lookup: field -> group label (for rendering separators) */
const FEATURE_FIELD_TO_GROUP = new Map<MetadataField, string>();
FEATURE_FIELD_GROUPS.forEach(g => g.fields.forEach(f => FEATURE_FIELD_TO_GROUP.set(f, g.label)));

const FIELD_LABELS: Record<MetadataField, string> = {
  status: 'Status',
  feature_id: 'Feature ID',
  git_branch: 'Branch',
  merge_target_branch: 'Merge Target',
  execution_mode: 'Execution Mode',
  checkout_enabled: 'Checkout Enabled',
  complete_on_idle: 'Complete on Idle',
  merge_policy: 'Merge Policy',
  merge_strategy: 'Merge Strategy',
  remote_branch_policy: 'Remote Branch Policy',
  open_pr_before_merge: 'Open PR Before Merge',
  target_workdir: 'Workdir',
  schedule: 'Schedule',
  schedule_enabled: 'Schedule Enabled',
  project: 'Project',
  agent: 'Agent',
  model: 'Model',
  direct_prompt: 'Prompt',
};

const FIELD_HINTS: Partial<Record<MetadataField, string>> = {
  git_branch: '(source branch for feature execution)',
  execution_mode: '(worktree|current_branch)',
  target_workdir: '(worktree/current-branch root path)',
  merge_target_branch: '(target branch for merge)',
  checkout_enabled: '(true|false)',
  complete_on_idle: '(true|false)',
  merge_policy: '(prompt_only|auto_pr|auto_merge)',
  merge_strategy: '(squash|merge|rebase)',
  remote_branch_policy: '(keep|delete)',
  open_pr_before_merge: '(true|false)',
  schedule_enabled: '(true|false)',
};

export function MetadataPopup({
  width: popupWidth,
  mode,
  taskTitle,
  batchCount,
  featureId,
  focusedField,
  statusValue,
  featureIdValue,
  branchValue,
  mergeTargetBranchValue,
  executionModeValue,
  checkoutEnabledValue,
  completeOnIdleValue,
  mergePolicyValue,
  mergeStrategyValue,
  remoteBranchPolicyValue,
  openPrBeforeMergeValue,
  workdirValue,
  scheduleValue,
  scheduleEnabledValue,
  projectValue,
  agentValue,
  modelValue,
  directPromptValue,
  availableProjects,
  selectedProjectIndex,
  selectedStatusIndex,
  allowedStatuses,
  interactionMode,
  editBuffer = '',
  mixedFields,
}: MetadataPopupProps): React.ReactElement {
  // Truncate title if too long
  const maxTitleLen = 30;
  const displayTitle = taskTitle && taskTitle.length > maxTitleLen
    ? taskTitle.slice(0, maxTitleLen - 3) + '...'
    : taskTitle;

  const fieldOrder = mode === 'feature' ? METADATA_FIELDS_FEATURE_SETTINGS : METADATA_FIELDS_DEFAULT;

  // Check if a field has mixed values across selected tasks (batch/feature only)
  const isFieldMixed = (field: MetadataField): boolean => {
    if (mode === 'single') return false;
    return mixedFields?.has(field) ?? false;
  };

  // Get value for a field
  const getFieldValue = (field: MetadataField): string => {
    // Show "(mixed)" for fields with differing values across tasks (not in edit mode)
    if (isFieldMixed(field) && !isFieldEditing(field)) {
      return '(mixed)';
    }
    switch (field) {
      case 'status':
        return getStatusLabel(statusValue);
      case 'feature_id':
        return featureIdValue || '(none)';
      case 'git_branch':
        return branchValue || '(none)';
      case 'merge_target_branch':
        return mergeTargetBranchValue || '(none)';
      case 'execution_mode':
        return executionModeValue || 'worktree';
      case 'checkout_enabled':
        return checkoutEnabledValue ? 'true' : 'false';
      case 'complete_on_idle':
        return completeOnIdleValue ? 'true' : 'false';
      case 'merge_policy':
        return mergePolicyValue || 'prompt_only';
      case 'merge_strategy':
        return mergeStrategyValue || 'squash';
      case 'remote_branch_policy':
        return remoteBranchPolicyValue || 'delete';
      case 'open_pr_before_merge':
        return openPrBeforeMergeValue ? 'true' : 'false';
      case 'target_workdir':
        return workdirValue || '(none)';
      case 'schedule':
        return scheduleValue || '(none)';
      case 'schedule_enabled':
        return scheduleEnabledValue ? 'true' : 'false';
      case 'project':
        return projectValue || '(none)';
      case 'agent':
        return agentValue || '(default)';
      case 'model':
        return modelValue || '(default)';
      case 'direct_prompt':
        return directPromptValue || '(none)';
    }
  };

  // Determine if a field is in edit mode based on interaction mode and focus
  const isFieldEditing = (field: MetadataField): boolean => {
    if (field === 'status') {
      return interactionMode === 'edit_status' && focusedField === 'status';
    }
    if (field === 'project') {
      return interactionMode === 'edit_project' && focusedField === 'project';
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
      case 'edit_project':
        return 'j/k: select project  Enter: move  Esc: cancel';
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
      width={popupWidth}
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
        {fieldOrder.map((field, idx) => {
          const isFocused = field === focusedField;
          const isEditing = isFieldEditing(field);
          const label = FIELD_LABELS[field];
          const value = getFieldValue(field);

          // Group separator for feature mode
          let separator: React.ReactElement | null = null;
          if (mode === 'feature') {
            const currentGroup = FEATURE_FIELD_TO_GROUP.get(field);
            const prevGroup = idx > 0 ? FEATURE_FIELD_TO_GROUP.get(fieldOrder[idx - 1]) : undefined;
            if (currentGroup && currentGroup !== prevGroup) {
              separator = (
                <Box marginTop={idx > 0 ? 1 : 0} key={`sep-${currentGroup}`}>
                  <Text dimColor bold>{`── ${currentGroup} ──`}</Text>
                </Box>
              );
            }
          }

          return (
            <React.Fragment key={field}>
            {separator}
            <Box>
              {/* Selection indicator */}
              <Text color={isFocused ? 'cyan' : undefined}>
                {isFocused ? '→ ' : '  '}
              </Text>

              {/* Field label (fixed width for alignment) */}
              <Box width={22} flexShrink={0}>
                <Text
                  color={isFocused ? 'cyan' : undefined}
                  bold={isFocused}
                >
                  {label}:
                </Text>
              </Box>

              {/* Field value */}
              {field === 'status' && isFieldMixed('status') && !isEditing ? (
                // Status field with mixed values: show "(mixed)" instead of icon/label
                <Box flexShrink={1}>
                  <Text color="yellow" dimColor>
                    {'(mixed)'}
                  </Text>
                  {isFocused && (
                    <Text dimColor> (Enter to select)</Text>
                  )}
                </Box>
              ) : field === 'status' ? (
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
              ) : field === 'project' ? (
                // Project field: show dropdown (similar to status)
                <Box flexDirection="column">
                  <Box>
                    <Text color="green">
                      {'📁'}
                    </Text>
                    <Text
                      color={isFocused ? 'green' : undefined}
                      bold={isFocused}
                    >
                      {' '}{availableProjects[selectedProjectIndex] || projectValue || '(none)'}
                    </Text>
                    {isFocused && !isEditing && (
                      <Text dimColor> (Enter to select)</Text>
                    )}
                  </Box>
                  {/* Project sub-popup when in edit_project mode */}
                  {isEditing && (
                    <Box
                      flexDirection="column"
                      borderStyle="round"
                      borderColor="green"
                      marginTop={1}
                      marginLeft={2}
                      paddingX={1}
                    >
                      <Text bold color="green">Select Project</Text>
                      {availableProjects.map((project, idx) => {
                        const isSelected = idx === selectedProjectIndex;
                        const isCurrent = project === projectValue;
                        return (
                          <Box key={project}>
                            <Text color={isSelected ? 'green' : undefined}>
                              {isSelected ? '→ ' : '  '}
                            </Text>
                            <Text color={isSelected ? 'green' : undefined}>
                              {isSelected ? '●' : '○'}
                            </Text>
                            <Text
                              color={isSelected ? 'green' : undefined}
                              bold={isSelected}
                            >
                              {' '}{project}
                            </Text>
                            {isCurrent && (
                              <Text dimColor> (current)</Text>
                            )}
                          </Box>
                        );
                      })}
                    </Box>
                  )}
                </Box>
              ) : isEditing ? (
                // Text field in edit mode: show edit buffer with cursor
                <Box flexShrink={1}>
                  <Text color="cyan" bold wrap="wrap">
                    {editBuffer}
                  </Text>
                  <Text color="cyan" bold inverse>
                    {' '}
                  </Text>
                </Box>
              ) : (
                // Text field: show value
                <Box flexShrink={1}>
                  <Text
                    color={value === '(mixed)' ? 'yellow' : isFocused ? 'cyan' : undefined}
                    bold={isFocused}
                    dimColor={!isFocused && (value === '(none)' || value === '(mixed)')}
                  >
                    {value}
                  </Text>
                  {isFocused && !isEditing && FIELD_HINTS[field] && (
                    <Text dimColor> {FIELD_HINTS[field]}</Text>
                  )}
                  {isFocused && (
                    <Text dimColor> (Enter to edit)</Text>
                  )}
                </Box>
              )}
            </Box>
            </React.Fragment>
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

/**
 * Root TUI component for the Brain Task Runner dashboard
 *
 * Layout:
 * ┌─ project-name ──────────────────────────────────────────────────────────┐
 * │  ● 2 ready   ○ 3 waiting   ▶ 1 active   ✓ 2 done                        │
 * ├──────────────────────────────────────────────────────────────────────────┤
 * │ Tasks (full width)                                                       │
 * │ ──────────────────────────────────────────────────────────────────────── │
 * │ ● Setup base config                                                      │
 * │ └─○ Create utils module                                                  │
 * │   └─○ Create main entry                                                  │
 * ├──────────────────────────────────────────────────────────────────────────┤
 * │ Task Details (hidden by default, toggle with T)                          │
 * │ ───────────────────────────────────────────────────────────────────────  │
 * │ Title: Setup base config   Status: pending   Priority: high              │
 * ├──────────────────────────────────────────────────────────────────────────┤
 * │ Logs (hidden by default, toggle with L)                                  │
 * │ ───────────────────────────────────────────────────────────────────────  │
 * │ 17:30:45 INFO  Runner started                                            │
 * ├──────────────────────────────────────────────────────────────────────────┤
 * │ ↑↓/j/k Navigate  Enter: Details  Tab: Switch  r: Refresh  q: Quit       │
 * └──────────────────────────────────────────────────────────────────────────┘
 *
 * Bottom area stacking (when both visible):
 *   Task Detail takes 70% of bottom area, Logs takes 30%
 *   When only one visible, it takes 100% of bottom area
 */

import React, { useState, useEffect, useMemo, useCallback, useRef } from 'react';
import { join } from 'path';
import { spawnSync } from 'child_process';
import { writeFileSync, readFileSync, unlinkSync, mkdtempSync } from 'fs';
import { tmpdir } from 'os';
import { copyToClipboard } from '../system-utils';
import { getApiClient } from '../api-client';

/** Compare two Sets for value equality (same size and same elements) */
export function setsEqual<T>(a: Set<T>, b: Set<T>): boolean {
  if (a.size !== b.size) return false;
  for (const item of a) {
    if (!b.has(item)) return false;
  }
  return true;
}
import { Box, Text, useInput, useApp, useStdin } from 'ink';
import { StatusBar } from './components/StatusBar';
import { TaskTree, flattenFeatureOrder, parseTaskTreeRowTarget, COMPLETED_HEADER_ID, DRAFT_HEADER_ID, CANCELLED_HEADER_ID, SUPERSEDED_HEADER_ID, ARCHIVED_HEADER_ID, GROUP_HEADER_PREFIX, SPACER_PREFIX, FEATURE_HEADER_PREFIX, COMPLETED_FEATURE_PREFIX, DRAFT_FEATURE_PREFIX, CANCELLED_FEATURE_PREFIX, SUPERSEDED_FEATURE_PREFIX, ARCHIVED_FEATURE_PREFIX, UNGROUPED_HEADER_ID, UNGROUPED_FEATURE_ID, GROUP_STATUSES, PROJECT_HEADER_PREFIX } from './components/TaskTree';
import { LogViewer } from './components/LogViewer';
import { TaskDetail } from './components/TaskDetail';
import { CronDetail } from './components/CronDetail';
import { CronLinkEditor } from './components/CronLinkEditor';
import { HelpBar } from './components/HelpBar';
import {
  MetadataPopup,
  METADATA_FIELDS_DEFAULT,
  METADATA_FIELDS_FEATURE_SETTINGS,
  type MetadataField,
  type MetadataPopupMode,
  type MetadataInteractionMode,
} from './components/MetadataPopup';
import { SettingsPopup } from './components/SettingsPopup';
import { DeleteConfirmPopup } from './components/DeleteConfirmPopup';
import { SessionSelectPopup } from './components/SessionSelectPopup';

import {
  ENTRY_STATUSES,
  EXECUTION_MODES,
  MERGE_POLICIES,
  REMOTE_BRANCH_POLICIES,
  MERGE_STRATEGIES,
  type EntryStatus,
  type MergePolicy,
  type MergeStrategy,
  type RemoteBranchPolicy,
  type ExecutionMode,
} from '../../core/types';
import { useTaskSse } from './hooks/useTaskSse';
import { useMultiProjectSse } from './hooks/useMultiProjectSse';
import { useLogStream } from './hooks/useLogStream';
import { useTerminalSize } from './hooks/useTerminalSize';
import { useResourceMetrics } from './hooks/useResourceMetrics';
import { useSettingsStorage } from './hooks/useSettingsStorage';
import { useTaskFilter } from './hooks/useTaskFilter';
import { useMouseInput } from './hooks/useMouseInput';
import { FilterBar } from './components/FilterBar';
import { CronList } from './components/CronList';
import { useCronPoller } from './hooks/useCronPoller';
import { useMultiProjectCronPoller } from './hooks/useMultiProjectCronPoller';
import type { AppProps, TaskDisplay, ProjectLimitEntry, GroupVisibilityEntry, SettingsSection, OpenSessionTaskContext, CronDisplay, TaskTreeRowTarget, TUIMouseButton, TUIMouseEvent, TaskTreeVisibleRow } from './types';
import type { TaskStats } from './hooks/taskTypes';

type FocusedPanel = 'tasks' | 'details' | 'logs';
type ViewMode = 'tasks' | 'crons';
type CronActionMode = 'create' | 'edit' | 'add-link' | 'remove-link' | 'replace-links';





/** Cycle to the next panel (for Tab navigation) */
function nextPanel(current: FocusedPanel, logsVisible: boolean, detailVisible: boolean): FocusedPanel {
  // Build available panels list based on visibility
  const panels: FocusedPanel[] = ['tasks'];
  if (detailVisible) panels.push('details');
  if (logsVisible) panels.push('logs');
  
  // Find current index and cycle to next
  const currentIndex = panels.indexOf(current);
  if (currentIndex === -1) return 'tasks'; // Fallback if current panel is hidden
  const nextIndex = (currentIndex + 1) % panels.length;
  return panels[nextIndex];
}

/** Display labels for each entry status */
const STATUS_LABELS: Record<string, string> = {
  draft: 'Draft',
  pending: 'Pending',
  active: 'Active',
  in_progress: 'In Progress',
  blocked: 'Blocked',
  cancelled: 'Cancelled',
  completed: 'Completed',
  validated: 'Validated',
  superseded: 'Superseded',
  archived: 'Archived',
};

const BOOLEAN_METADATA_FIELDS: ReadonlySet<MetadataField> = new Set([
  'checkout_enabled',
  'open_pr_before_merge',
]);

const LOWERCASE_ENUM_METADATA_FIELDS: ReadonlySet<MetadataField> = new Set([
  'execution_mode',
  'merge_policy',
  'merge_strategy',
  'remote_branch_policy',
]);

const TRIMMED_METADATA_FIELDS: ReadonlySet<MetadataField> = new Set([
  'feature_id',
  'git_branch',
  'merge_target_branch',
  'target_workdir',
  'schedule',
  'agent',
  'model',
]);

/**
 * Normalize user-entered metadata text before local state updates/save.
 */
export function normalizeMetadataFieldValue(field: MetadataField, rawValue: string): string {
  if (LOWERCASE_ENUM_METADATA_FIELDS.has(field) || BOOLEAN_METADATA_FIELDS.has(field)) {
    return rawValue.trim().toLowerCase();
  }

  if (TRIMMED_METADATA_FIELDS.has(field)) {
    return rawValue.trim();
  }

  return rawValue;
}

/**
 * Validate metadata field input and return user-facing error, or null if valid.
 */
export function validateMetadataFieldValue(field: MetadataField, value: string): string | null {
  if (field === 'execution_mode' && value.length > 0 && !EXECUTION_MODES.includes(value as ExecutionMode)) {
    return `Invalid execution mode: ${value}. Use one of: ${EXECUTION_MODES.join(', ')}`;
  }

  if (field === 'merge_policy' && value.length > 0 && !MERGE_POLICIES.includes(value as MergePolicy)) {
    return `Invalid merge policy: ${value}. Use one of: ${MERGE_POLICIES.join(', ')}`;
  }

  if (field === 'merge_strategy' && value.length > 0 && !MERGE_STRATEGIES.includes(value as MergeStrategy)) {
    return `Invalid merge strategy: ${value}. Use one of: ${MERGE_STRATEGIES.join(', ')}`;
  }

  if (field === 'remote_branch_policy' && value.length > 0 && !REMOTE_BRANCH_POLICIES.includes(value as RemoteBranchPolicy)) {
    return `Invalid remote branch policy: ${value}. Use one of: ${REMOTE_BRANCH_POLICIES.join(', ')}`;
  }

  if (BOOLEAN_METADATA_FIELDS.has(field) && value.length > 0 && value !== 'true' && value !== 'false') {
    return `Invalid ${field}: use true or false`;
  }

  if (field === 'schedule' && value.length > 0) {
    const fields = value.split(/\s+/);
    if (fields.length !== 5) {
      return `Invalid cron expression: expected 5 fields (minute hour day month weekday), got ${fields.length}`;
    }
  }

  return null;
}

const TERMINAL_METADATA_SKIP_STATUSES: ReadonlySet<EntryStatus> = new Set([
  'completed',
  'validated',
  'cancelled',
  'superseded',
  'archived',
]);

type MetadataPrefillValues = {
  status: EntryStatus;
  feature_id: string;
  git_branch: string;
  merge_target_branch: string;
  execution_mode: ExecutionMode;
  checkout_enabled: boolean;
  merge_policy: MergePolicy;
  merge_strategy: MergeStrategy;
  remote_branch_policy: RemoteBranchPolicy;
  open_pr_before_merge: boolean;
  target_workdir: string;
  schedule: string;
  project: string;
  agent: string;
  model: string;
  direct_prompt: string;
};

const DEFAULT_METADATA_PREFILL: MetadataPrefillValues = {
  status: 'pending',
  feature_id: '',
  git_branch: '',
  merge_target_branch: '',
  execution_mode: 'worktree',
  checkout_enabled: true,
  merge_policy: 'auto_merge',
  merge_strategy: 'squash',
  remote_branch_policy: 'delete',
  open_pr_before_merge: false,
  target_workdir: '',
  schedule: '',
  project: '',
  agent: '',
  model: '',
  direct_prompt: '',
};

function allEqual<T>(values: readonly T[]): boolean {
  if (values.length <= 1) return true;
  const first = values[0];
  return values.every((value) => value === first);
}

function sharedString(
  tasks: TaskDisplay[],
  selector: (task: TaskDisplay) => string | null | undefined,
): string {
  if (tasks.length === 0) return '';
  const values = tasks.map((task) => selector(task) ?? '');
  return allEqual(values) ? values[0] : '';
}

function sharedEnum<T extends string>(
  tasks: TaskDisplay[],
  selector: (task: TaskDisplay) => T | null | undefined,
  fallback: T,
): T {
  if (tasks.length === 0) return fallback;
  const values = tasks.map((task) => selector(task) ?? fallback);
  return allEqual(values) ? values[0] : fallback;
}

function sharedBoolean(
  tasks: TaskDisplay[],
  selector: (task: TaskDisplay) => boolean | null | undefined,
  fallback: boolean,
): boolean {
  if (tasks.length === 0) return fallback;
  const values = tasks.map((task) => selector(task) ?? fallback);
  return allEqual(values) ? values[0] : fallback;
}

export function buildMetadataPrefillFromTasks(
  tasks: TaskDisplay[],
  mode: MetadataPopupMode,
  featureIdOverride?: string,
): MetadataPrefillValues {
  if (tasks.length === 0) return { ...DEFAULT_METADATA_PREFILL, feature_id: featureIdOverride ?? '' };

  const first = tasks[0];

  return {
    status: mode === 'single'
      ? first.status
      : sharedEnum(tasks, (task) => task.status, 'pending'),
    feature_id: featureIdOverride ?? sharedString(tasks, (task) => task.feature_id),
    git_branch: sharedString(tasks, (task) => task.gitBranch),
    merge_target_branch: sharedString(tasks, (task) => task.mergeTargetBranch),
    execution_mode: sharedEnum(tasks, (task) => task.executionMode, 'worktree'),
    checkout_enabled: sharedBoolean(tasks, (task) => task.checkoutEnabled, true),
    merge_policy: sharedEnum(tasks, (task) => task.mergePolicy, 'auto_merge'),
    merge_strategy: sharedEnum(tasks, (task) => task.mergeStrategy, 'squash'),
    remote_branch_policy: sharedEnum(tasks, (task) => task.remoteBranchPolicy, 'delete'),
    open_pr_before_merge: sharedBoolean(tasks, (task) => task.openPrBeforeMerge, false),
    target_workdir: sharedString(tasks, (task) => task.resolvedWorkdir || task.workdir),
    schedule: sharedString(tasks, (task) => task.schedule),
    project: sharedString(tasks, (task) => task.projectId),
    agent: sharedString(tasks, (task) => task.agent),
    model: sharedString(tasks, (task) => task.model),
    direct_prompt: sharedString(tasks, (task) => task.direct_prompt),
  };
}

export function isFeatureCheckoutTask(task: TaskDisplay): boolean {
  if (task.tags.includes('checkout')) return true;

  const generatedBy = typeof task.frontmatter?.generated_by === 'string'
    ? task.frontmatter.generated_by
    : null;
  if (generatedBy === 'feature-checkout') return true;

  const generatedKind = typeof task.frontmatter?.generated_kind === 'string'
    ? task.frontmatter.generated_kind
    : null;
  if (generatedKind === 'feature_checkout') return true;

  const generatedKey = typeof task.frontmatter?.generated_key === 'string'
    ? task.frontmatter.generated_key
    : null;
  if (generatedKey?.startsWith('feature-checkout:')) return true;

  return false;
}

export type MetadataApplyPlan = {
  updatable: TaskDisplay[];
  skippedTerminal: TaskDisplay[];
  skippedCheckout: TaskDisplay[];
};

export function buildMetadataApplyPlan(
  tasks: TaskDisplay[],
  mode: MetadataPopupMode,
): MetadataApplyPlan {
  const orderedTasks = [...tasks].sort((a, b) => {
    const byPath = a.path.localeCompare(b.path);
    if (byPath !== 0) return byPath;
    return a.id.localeCompare(b.id);
  });

  if (mode !== 'feature') {
    return {
      updatable: orderedTasks,
      skippedTerminal: [],
      skippedCheckout: [],
    };
  }

  const updatable: TaskDisplay[] = [];
  const skippedTerminal: TaskDisplay[] = [];
  const skippedCheckout: TaskDisplay[] = [];

  for (const task of orderedTasks) {
    if (TERMINAL_METADATA_SKIP_STATUSES.has(task.status)) {
      skippedTerminal.push(task);
      continue;
    }

    if (isFeatureCheckoutTask(task)) {
      skippedCheckout.push(task);
      continue;
    }

    updatable.push(task);
  }

  return { updatable, skippedTerminal, skippedCheckout };
}

/**
 * Get visible (active) tasks for a feature, excluding group-status tasks.
 * Used when pressing 's' on a feature header to bulk update metadata.
 * 
 * GROUP_STATUSES (draft, cancelled, completed, validated, superseded, archived)
 * are rendered in collapsed sections at the bottom of the TUI, not under feature headers.
 * This function ensures bulk updates only affect the visible active tasks.
 */
export function getVisibleTasksForFeature(tasks: TaskDisplay[], featureId: string): TaskDisplay[] {
  return tasks.filter(t => t.feature_id === featureId && !GROUP_STATUSES.includes(t.status));
}

/**
 * Get visible (active) ungrouped tasks, excluding group-status tasks.
 * Used when pressing 's' on the ungrouped header to bulk update metadata.
 * 
 * GROUP_STATUSES (draft, cancelled, completed, validated, superseded, archived)
 * are rendered in collapsed sections at the bottom of the TUI, not under the ungrouped header.
 * This function ensures bulk updates only affect the visible active tasks.
 */
export function getVisibleTasksForUngrouped(tasks: TaskDisplay[]): TaskDisplay[] {
  return tasks.filter(t => !t.feature_id && !GROUP_STATUSES.includes(t.status));
}

/**
 * Map from status-group feature prefix to the statuses that belong in that group.
 * Used to determine which tasks to target when pressing 's' on a status-group feature header.
 */
export const STATUS_GROUP_MAP: Record<string, EntryStatus[]> = {
  [COMPLETED_FEATURE_PREFIX]: ['completed', 'validated'],
  [DRAFT_FEATURE_PREFIX]: ['draft'],
  [CANCELLED_FEATURE_PREFIX]: ['cancelled'],
  [SUPERSEDED_FEATURE_PREFIX]: ['superseded'],
  [ARCHIVED_FEATURE_PREFIX]: ['archived'],
};

export type TaskTreeClickAction = 'open_editor' | 'open_metadata' | 'toggle_collapsed' | 'noop';

/**
 * Header targets that toggle collapse/expand when selected.
 */
export function isTaskTreeCollapseToggleTarget(target: TaskTreeRowTarget): boolean {
  return (
    target.kind === 'feature_header' ||
    target.kind === 'project_header' ||
    target.kind === 'status_header' ||
    target.kind === 'status_feature_header' ||
    target.kind === 'ungrouped_header'
  );
}

/**
 * Route pointer clicks to high-level TaskTree actions.
 */
export function resolveTaskTreeClickAction(target: TaskTreeRowTarget, button: TUIMouseButton): TaskTreeClickAction {
  if (target.kind === 'task') {
    if (button === 'right') return 'open_metadata';
    if (button === 'left') return 'noop'; // Just highlight, don't open editor
    return 'noop';
  }

  if (button === 'left') {
    if (isTaskTreeCollapseToggleTarget(target)) {
      return 'toggle_collapsed';
    }
  }

  return 'noop';
}

/**
 * Resolve Set key used by collapsedFeatures for a row target.
 */
export function getTaskTreeCollapseKey(target: TaskTreeRowTarget): string | null {
  if (target.kind === 'ungrouped_header') {
    return UNGROUPED_FEATURE_ID;
  }

  if (target.kind === 'feature_header' && target.featureId) {
    return target.featureId;
  }

  if (target.kind === 'status_feature_header' && target.featureId && target.statusGroup) {
    return `${target.statusGroup}:${target.featureId}`;
  }

  if (target.kind === 'project_header' && target.projectId) {
    return `project:${target.projectId}`;
  }

  return null;
}

const SINGLE_PROJECT_STATUS_BAR_HEIGHT = 3;
const MULTI_PROJECT_STATUS_BAR_HEIGHT = 4;
const TASK_PANEL_BORDER_TOP_ROWS = 1;
const TASK_TREE_PADDING_TOP_ROWS = 1;
const TASK_TREE_HEADER_ROWS = 1;
const TASK_TREE_HEADER_MARGIN_ROWS = 1;

/**
 * Resolve first visible task-tree row as an absolute terminal row (1-based).
 */
export function getTaskTreeViewportStartRow(
  isMultiProject: boolean,
  filterMode: 'off' | 'typing' | 'locked',
): number {
  const statusBarHeight = isMultiProject ? MULTI_PROJECT_STATUS_BAR_HEIGHT : SINGLE_PROJECT_STATUS_BAR_HEIGHT;
  const filterBarRows = filterMode === 'off' ? 0 : 1;

  return statusBarHeight
    + TASK_PANEL_BORDER_TOP_ROWS
    + filterBarRows
    + TASK_TREE_PADDING_TOP_ROWS
    + TASK_TREE_HEADER_ROWS
    + TASK_TREE_HEADER_MARGIN_ROWS
    + 1;
}

/**
 * Resolve semantic row target from an absolute mouse row.
 */
export function findTaskTreeTargetFromMouseRow(
  visibleRows: TaskTreeVisibleRow[],
  mouseRow: number,
  viewportStartRow: number,
): TaskTreeRowTarget | null {
  const relativeRow = mouseRow - viewportStartRow;
  if (relativeRow < 0) {
    return null;
  }

  const hit = visibleRows.find((row) => row.row === relativeRow);
  return hit?.target ?? null;
}

type TaskTreeMouseGuardState = {
  viewMode: ViewMode;
  showMetadataPopup: boolean;
  showSettingsPopup: boolean;
  deletePopupOpen: boolean;
  sessionPopupOpen: boolean;
  cronActionOpen: boolean;
  cronDeleteConfirmOpen: boolean;
  cronLinkEditorOpen: boolean;
  showHelp: boolean;
  isEditing: boolean;
};

export function shouldHandleTaskTreeMouseEvent(state: TaskTreeMouseGuardState): boolean {
  if (state.viewMode !== 'tasks') {
    return false;
  }

  return !(
    state.showMetadataPopup ||
    state.showSettingsPopup ||
    state.deletePopupOpen ||
    state.sessionPopupOpen ||
    state.cronActionOpen ||
    state.cronDeleteConfirmOpen ||
    state.cronLinkEditorOpen ||
    state.showHelp ||
    state.isEditing
  );
}

/**
 * Resolve which task ID should drive preview rendering.
 * Only the persistent selection drives preview — hover was removed.
 */
export function resolvePreviewTaskId(
  selectedTaskId: string | null,
  availableTaskIds?: ReadonlySet<string>,
): string | null {
  if (!selectedTaskId) return null;
  if (!availableTaskIds) return selectedTaskId;
  return availableTaskIds.has(selectedTaskId) ? selectedTaskId : null;
}

/**
 * Get tasks for a feature within a specific status group section.
 * Used when pressing 's' on a feature header inside completed/draft/etc sections.
 */
export function getTasksForStatusGroupFeature(
  tasks: TaskDisplay[],
  featureId: string,
  groupStatuses: EntryStatus[]
): TaskDisplay[] {
  return tasks.filter(t => t.feature_id === featureId && groupStatuses.includes(t.status));
}

export type FeatureCheckoutResult = {
  created: boolean;
  taskId: string;
  taskTitle: string;
};

export type FeatureCheckoutOptions = {
  execution_branch?: string;
  merge_target_branch?: string;
  merge_policy?: MergePolicy;
  merge_strategy?: MergeStrategy;
  remote_branch_policy?: RemoteBranchPolicy;
  open_pr_before_merge?: boolean;
  execution_mode?: ExecutionMode;
};

export const DEFAULT_FEATURE_CHECKOUT_OPTIONS: FeatureCheckoutOptions = {
  execution_mode: 'worktree',
  merge_policy: 'auto_merge',
  merge_strategy: 'squash',
  remote_branch_policy: 'delete',
  open_pr_before_merge: false,
};

type ResolveFeatureCheckoutOptionsParams = {
  selectedTaskId: string | null;
  isMultiProject: boolean;
  activeProject: string;
  project: string;
  tasks: TaskDisplay[];
};

export function resolveFeatureCheckoutOptionsForSelection(
  params: ResolveFeatureCheckoutOptionsParams
): FeatureCheckoutOptions {
  const selectedRowTarget = params.selectedTaskId
    ? parseTaskTreeRowTarget(params.selectedTaskId)
    : null;

  if (!selectedRowTarget || selectedRowTarget.kind !== 'feature_header' || !selectedRowTarget.featureId) {
    return DEFAULT_FEATURE_CHECKOUT_OPTIONS;
  }

  const targetProjectId = params.isMultiProject ? params.activeProject : params.project;
  if (!targetProjectId || targetProjectId === 'all') {
    return DEFAULT_FEATURE_CHECKOUT_OPTIONS;
  }

  const matchingTask = params.tasks.find(task => (
    task.feature_id === selectedRowTarget.featureId
    && task.projectId === targetProjectId
    && !GROUP_STATUSES.includes(task.status)
  ));

  if (!matchingTask) {
    return DEFAULT_FEATURE_CHECKOUT_OPTIONS;
  }

  const options: FeatureCheckoutOptions = {
    execution_mode: matchingTask.executionMode || DEFAULT_FEATURE_CHECKOUT_OPTIONS.execution_mode,
    merge_policy: matchingTask.mergePolicy || DEFAULT_FEATURE_CHECKOUT_OPTIONS.merge_policy,
    merge_strategy: matchingTask.mergeStrategy || DEFAULT_FEATURE_CHECKOUT_OPTIONS.merge_strategy,
    remote_branch_policy: matchingTask.remoteBranchPolicy || DEFAULT_FEATURE_CHECKOUT_OPTIONS.remote_branch_policy,
    open_pr_before_merge: matchingTask.openPrBeforeMerge ?? DEFAULT_FEATURE_CHECKOUT_OPTIONS.open_pr_before_merge,
  };

  const executionBranch = matchingTask.gitBranch?.trim();
  if (executionBranch) {
    options.execution_branch = executionBranch;
  }

  const mergeTargetBranch = matchingTask.mergeTargetBranch?.trim();
  if (mergeTargetBranch) {
    options.merge_target_branch = mergeTargetBranch;
  }

  return options;
}

type FeatureCheckoutLogEntry = {
  level: 'info' | 'warn' | 'error';
  message: string;
};

type TriggerFeatureCheckoutParams = {
  selectedTaskId: string | null;
  isMultiProject: boolean;
  activeProject: string;
  project: string;
  onMarkFeatureForCheckout?: (
    projectId: string,
    featureId: string,
    options: FeatureCheckoutOptions
  ) => Promise<FeatureCheckoutResult>;
  checkoutOptions?: FeatureCheckoutOptions;
  addLog: (entry: FeatureCheckoutLogEntry) => void;
};

function formatCheckoutFailureReason(error: unknown): string {
  const raw = String(error);
  const jsonStart = raw.indexOf('{');
  if (jsonStart >= 0) {
    const candidate = raw.slice(jsonStart);
    try {
      const parsed = JSON.parse(candidate) as { message?: unknown };
      if (typeof parsed.message === 'string' && parsed.message.length > 0) {
        return parsed.message;
      }
    } catch {
      // Fall back to raw error text
    }
  }

  return raw;
}

export function triggerFeatureCheckoutFromSelection(params: TriggerFeatureCheckoutParams): boolean {
  const selectedRowTarget = params.selectedTaskId
    ? parseTaskTreeRowTarget(params.selectedTaskId)
    : null;
  if (!selectedRowTarget || selectedRowTarget.kind !== 'feature_header' || !selectedRowTarget.featureId) {
    return false;
  }

  if (!params.onMarkFeatureForCheckout) {
    params.addLog({
      level: 'warn',
      message: 'Feature checkout action unavailable',
    });
    return true;
  }

  const featureId = selectedRowTarget.featureId;
  const targetProjectId = params.isMultiProject ? params.activeProject : params.project;
  if (!targetProjectId || targetProjectId === 'all') {
    params.addLog({
      level: 'warn',
      message: 'Select a specific project tab before marking feature checkout',
    });
    return true;
  }

  params.addLog({
    level: 'info',
    message: `Marking feature for checkout: ${featureId}`,
  });

  const checkoutOptions = params.checkoutOptions ?? DEFAULT_FEATURE_CHECKOUT_OPTIONS;

  params.onMarkFeatureForCheckout(targetProjectId, featureId, checkoutOptions)
    .then((result) => {
      params.addLog({
        level: 'info',
        message: `${result.created ? 'Created' : 'Reused'} checkout task: ${result.taskId} - ${result.taskTitle}`,
      });
    })
    .catch((err) => {
      params.addLog({
        level: 'error',
        message: `Failed to mark feature checkout: ${formatCheckoutFailureReason(err)}`,
      });
    });

  return true;
}

export function App({ 
  config, 
  onLogCallback, 
  onCancelTask,
  onPause,
  onResume,
  onPauseAll,
  onResumeAll,
  getPausedProjects,
  onUpdateStatus,
  onEditTask,
  onExecuteTask,
  onExecuteFeature,
  onMarkFeatureForCheckout,
  getRunningProcessCount,
  getResourceMetrics,
  getProjectLimits,
  setProjectLimit,
  getRuntimeDefaultModel,
  setRuntimeDefaultModel,
  onEnableFeature,
  onDisableFeature,
  getEnabledFeatures,
  onUpdateMetadata,
  onMoveTask,
  onListProjects,
  onDeleteTasks,
  onOpenSession,
  onOpenSessionTmux,
  onCreateCron,
  onUpdateCron,
  onDeleteCron,
  onSetCronLinkedTasks,
  onAddCronLinkedTask,
  onRemoveCronLinkedTask,
  onTriggerCron,
}: AppProps): React.ReactElement {
  const { exit } = useApp();

  // Determine if multi-project mode - memoize to avoid re-creating array on every render
  const projects = useMemo(
    () => config.projects ?? [config.project],
    [config.projects, config.project]
  );
  const isMultiProject = projects.length > 1;
  // SSE is now the only transport mode (Phase 2)


  // State
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);

  const [selectedCronId, setSelectedCronId] = useState<string | null>(null);
  const [focusedPanel, setFocusedPanel] = useState<FocusedPanel>('tasks');
  const [viewMode, setViewMode] = useState<ViewMode>('tasks');
  const [showHelp, setShowHelp] = useState(false);
  // MetadataPopup state
  const [showMetadataPopup, setShowMetadataPopup] = useState(false);
  const [metadataPopupMode, setMetadataPopupMode] = useState<MetadataPopupMode>('single');
  const [metadataFocusedField, setMetadataFocusedField] = useState<MetadataField>('status');
  const [metadataStatusValue, setMetadataStatusValue] = useState<EntryStatus>('pending');
  const [metadataFeatureIdValue, setMetadataFeatureIdValue] = useState('');
  const [metadataBranchValue, setMetadataBranchValue] = useState('');
  const [metadataMergeTargetBranchValue, setMetadataMergeTargetBranchValue] = useState('');
  const [metadataExecutionModeValue, setMetadataExecutionModeValue] = useState<ExecutionMode>('worktree');
  const [metadataCheckoutEnabledValue, setMetadataCheckoutEnabledValue] = useState<boolean>(true);
  const [metadataMergePolicyValue, setMetadataMergePolicyValue] = useState<MergePolicy>('auto_merge');
  const [metadataMergeStrategyValue, setMetadataMergeStrategyValue] = useState<MergeStrategy>('squash');
  const [metadataRemoteBranchPolicyValue, setMetadataRemoteBranchPolicyValue] = useState<RemoteBranchPolicy>('delete');
  const [metadataOpenPrBeforeMergeValue, setMetadataOpenPrBeforeMergeValue] = useState<boolean>(false);
  const [metadataWorkdirValue, setMetadataWorkdirValue] = useState('');
  const [metadataScheduleValue, setMetadataScheduleValue] = useState('');
  const [metadataStatusIndex, setMetadataStatusIndex] = useState(0);
  const [metadataProjectValue, setMetadataProjectValue] = useState('');
  const [metadataProjectIndex, setMetadataProjectIndex] = useState(0);
  const [metadataAgentValue, setMetadataAgentValue] = useState('');
  const [metadataModelValue, setMetadataModelValue] = useState('');
  const [metadataDirectPromptValue, setMetadataDirectPromptValue] = useState('');
  // 3-mode state machine: navigate (j/k fields), edit_text (typing), edit_status (j/k status), edit_project (j/k project)
  const [metadataInteractionMode, setMetadataInteractionMode] = useState<MetadataInteractionMode>('navigate');
  const [metadataEditBuffer, setMetadataEditBuffer] = useState('');
  const [metadataTargetTasks, setMetadataTargetTasks] = useState<TaskDisplay[]>([]);
  // Original values for comparison (only send changed fields)
  const [metadataOriginalValues, setMetadataOriginalValues] = useState<{
    status: EntryStatus;
    feature_id: string;
    git_branch: string;
    merge_target_branch: string;
    execution_mode: ExecutionMode;
    checkout_enabled: boolean;
    merge_policy: MergePolicy;
    merge_strategy: MergeStrategy;
    remote_branch_policy: RemoteBranchPolicy;
    open_pr_before_merge: boolean;
    target_workdir: string;
    schedule: string;
    project: string;
    agent: string;
    model: string;
    direct_prompt: string;
  }>({
    status: 'pending',
    feature_id: '',
    git_branch: '',
    merge_target_branch: '',
    execution_mode: 'worktree',
    checkout_enabled: true,
    merge_policy: 'auto_merge',
    merge_strategy: 'squash',
    remote_branch_policy: 'delete',
    open_pr_before_merge: false,
    target_workdir: '',
    schedule: '',
    project: '',
    agent: '',
    model: '',
    direct_prompt: '',
  });
  // Track whether any metadata changes were saved while popup is open
  const [metadataDirty, setMetadataDirty] = useState(false);
  // All projects from API (for project picker in metadata popup)
  const [allProjects, setAllProjects] = useState<string[]>([]);
  // Computed: the effective project list used by the project picker (same as availableProjects prop)
  const effectiveProjects = allProjects.length > 0 ? allProjects : projects;
  // Cron names for metadata popup (maps cron ID -> cron title)
  const [cronNames, setCronNames] = useState<Record<string, string>>({});
  const [showSettingsPopup, setShowSettingsPopup] = useState(false);
  const [settingsSelectedIndex, setSettingsSelectedIndex] = useState(0);
  const [projectLimitsState, setProjectLimitsState] = useState<ProjectLimitEntry[]>([]);
  const [completedCollapsed, setCompletedCollapsed] = useState(true);
  const [draftCollapsed, setDraftCollapsed] = useState(true);
  const [cancelledCollapsed, setCancelledCollapsed] = useState(true);
  const [supersededCollapsed, setSupersededCollapsed] = useState(true);
  const [archivedCollapsed, setArchivedCollapsed] = useState(true);
  const [collapsedFeatures, setCollapsedFeatures] = useState<Set<string>>(new Set());
  const [collapsedProjects, setCollapsedProjects] = useState<Set<string>>(new Set());
  
  // Group visibility settings - persisted to ~/.brain/tui-settings.json
  // Default: show draft, pending, active, in_progress, blocked, completed
  const {
    visibleGroups,
    groupCollapsed,
    textWrap,
    setVisibleGroups,
    setGroupCollapsed,
    setTextWrap,
  } = useSettingsStorage();
  // Settings popup section
  const [settingsSection, setSettingsSection] = useState<SettingsSection>('limits');
  // Group visibility state for settings popup
  const [groupVisibilityState, setGroupVisibilityState] = useState<GroupVisibilityEntry[]>([]);
  const [runtimeDefaultModelState, setRuntimeDefaultModelState] = useState<string>('');
  const [runtimeModelEditMode, setRuntimeModelEditMode] = useState(false);
  const [runtimeModelEditBuffer, setRuntimeModelEditBuffer] = useState('');
  const [activeProject, setActiveProject] = useState<string>(config.activeProject ?? (isMultiProject ? 'all' : projects[0]));
  const [pausedProjects, setPausedProjects] = useState<Set<string>>(new Set());
  const [enabledFeatures, setEnabledFeatures] = useState<Set<string>>(new Set());
  const [logScrollOffset, setLogScrollOffset] = useState(0);
  const [detailsScrollOffset, setDetailsScrollOffset] = useState(0);
  const [filterLogsByTask, setFilterLogsByTask] = useState(false);
  const [isEditing, setIsEditing] = useState(false);

  const [taskScrollOffset, setTaskScrollOffset] = useState(0);
  const [cronScrollOffset, setCronScrollOffset] = useState(0);
  const [logsVisible, setLogsVisible] = useState(false);
  const [detailVisible, setDetailVisible] = useState(false);
  // Active features: features queued for execution (controlled by x key on feature headers)
  const [activeFeatures, setActiveFeatures] = useState<Set<string>>(new Set());
  // Multi-select state: tasks selected via Space key for batch operations
  const [selectedTaskIds, setSelectedTaskIds] = useState<Set<string>>(new Set());
  const [taskTreeVisibleRows, setTaskTreeVisibleRows] = useState<TaskTreeVisibleRow[]>([]);
  // Delete confirmation popup state
  const [deletePopupOpen, setDeletePopupOpen] = useState(false);
  const [tasksToDelete, setTasksToDelete] = useState<Array<{id: string, title: string, path: string}>>([]);
  
  // Session select popup state (for 'o'/'O' key to open sessions)
  const [sessionPopupOpen, setSessionPopupOpen] = useState(false);
  const [sessionPopupIds, setSessionPopupIds] = useState<string[]>([]);
  const [sessionPopupSelectedIndex, setSessionPopupSelectedIndex] = useState(0);
  const [sessionPopupTmuxMode, setSessionPopupTmuxMode] = useState(false);
  const [cronActionOpen, setCronActionOpen] = useState(false);
  const [cronActionMode, setCronActionMode] = useState<CronActionMode>('create');
  const [cronActionInput, setCronActionInput] = useState('');
  const [cronDeleteConfirmOpen, setCronDeleteConfirmOpen] = useState(false);
  const [cronLinkEditorOpen, setCronLinkEditorOpen] = useState(false);
  const [cronLinkEditorProjectId, setCronLinkEditorProjectId] = useState<string | null>(null);
  const [cronLinkEditorTaskIds, setCronLinkEditorTaskIds] = useState<Set<string>>(new Set());
  const [cronLinkEditorCursor, setCronLinkEditorCursor] = useState(0);
  const cronLinkEditorTaskIdsRef = useRef<Set<string>>(new Set());
  const cronLinkEditorCursorRef = useRef(0);
  const cronActionOpenRef = useRef(false);
  const cronActionModeRef = useRef<CronActionMode>('create');
  const cronActionInputRef = useRef('');

  // Get stdin control for suspending during editor session
  const { setRawMode } = useStdin();



  // Single-project SSE task transport
  const singleProjectSse = useTaskSse({
    projectId: config.project,
    apiUrl: config.apiUrl,
    enabled: !isMultiProject,
  });



  // Multi-project SSE transport
  const multiProjectSse = useMultiProjectSse({
    projects,
    apiUrl: config.apiUrl,
    enabled: isMultiProject,
  });

  // Single-project cron poller (used when not in multi-project mode)
  const singleCronPoller = useCronPoller({
    projectId: config.project,
    apiUrl: config.apiUrl,
    pollInterval: config.pollInterval,
    enabled: !isMultiProject,
  });

  // Multi-project cron poller (used when in multi-project mode)
  const multiProjectCronPoller = useMultiProjectCronPoller({
    projects,
    apiUrl: config.apiUrl,
    pollInterval: config.pollInterval,
    enabled: isMultiProject,
  });

  // Select appropriate data based on mode
  let tasks: TaskDisplay[];
  let stats: TaskStats;
  let isLoading: boolean;
  let isConnected: boolean;
  let error: Error | null;
  let refetch: () => Promise<void>;
  let crons: CronDisplay[];
  let cronError: Error | null;
  let refetchCrons: () => Promise<void>;

  if (isMultiProject) {
    const taskTransport = multiProjectSse;

    // Multi-project mode: filter tasks by activeProject
    if (activeProject === 'all') {
      tasks = taskTransport.allTasks;
    } else {
      tasks = taskTransport.tasksByProject.get(activeProject) ?? [];
    }
    stats = activeProject === 'all' 
      ? taskTransport.aggregateStats 
      : (taskTransport.statsByProject.get(activeProject) ?? {
          total: 0, ready: 0, waiting: 0, blocked: 0, inProgress: 0, completed: 0,
        });
    isLoading = taskTransport.isLoading;
    isConnected = taskTransport.isConnected;
    error = taskTransport.error;
    refetch = taskTransport.refetch;
    if (activeProject === 'all') {
      crons = multiProjectCronPoller.allCrons;
    } else {
      crons = multiProjectCronPoller.cronsByProject.get(activeProject) ?? [];
    }
    cronError = multiProjectCronPoller.error;
    refetchCrons = multiProjectCronPoller.refetch;
  } else {
    const taskTransport = singleProjectSse;

    // Single-project mode
    tasks = taskTransport.tasks;
    stats = taskTransport.stats;
    isLoading = taskTransport.isLoading;
    isConnected = taskTransport.isConnected;
    error = taskTransport.error;
    refetch = taskTransport.refetch;
    crons = singleCronPoller.crons;
    cronError = singleCronPoller.error;
    refetchCrons = singleCronPoller.refetch;
  }

  // Compute log file path if logDir is configured
  const logFile = useMemo(() => {
    if (!config.logDir) return undefined;
    // Use first project for single-project mode, 'multi' for multi-project mode
    const projectName = isMultiProject ? 'multi' : (config.project || 'default');
    return join(config.logDir, 'brain-runner', projectName, 'tui-logs.jsonl');
  }, [config.logDir, config.project, isMultiProject]);

  const { logs, addLog } = useLogStream({ maxEntries: config.maxLogs, logFile });
  const { rows: terminalRows, columns: terminalColumns } = useTerminalSize();
  const { metrics: resourceMetrics } = useResourceMetrics({ getResourceMetrics });

  // Calculate dynamic layout heights
  // Account for: StatusBar (3 lines) + HelpBar (1 line) + borders/chrome (2 lines)
  const availableHeight = terminalRows - 6;
  const anyBottomVisible = logsVisible || detailVisible;
  
  // Top row (TaskTree) height: 60% when bottom area visible, full otherwise
  const topRowHeight = anyBottomVisible
    ? Math.floor(availableHeight * 0.6)
    : availableHeight;
  
  // Bottom area height: remaining space after top row
  const bottomAreaHeight = availableHeight - topRowHeight;
  
  // When both bottom panels visible: TaskDetail 70%, Logs 30%
  // When only one visible: it gets 100% of bottom area
  const bothBottomVisible = logsVisible && detailVisible;
  const detailHeight = bothBottomVisible
    ? Math.floor(bottomAreaHeight * 0.7)
    : bottomAreaHeight;
  const logHeight = bothBottomVisible
    ? bottomAreaHeight - detailHeight
    : bottomAreaHeight;
  
  const logMaxLines = Math.max(5, logHeight - 2); // minus log panel chrome
  
  // Calculate task viewport height for scrolling
  // Account for: TaskTree header (2 lines: title + margin) + border (2 lines) + padding (2 lines)
  const taskViewportHeight = Math.max(3, topRowHeight - 6);
  const cronViewportHeight = Math.max(3, topRowHeight - 4);
  
  // Calculate details viewport height for scrolling (now in bottom area)
  // Account for: header (1 line) + border (2 lines) + padding (2 lines) + scroll indicators (2 lines)
  const detailsViewportHeight = Math.max(3, detailHeight - 7);
  
  // Task tree is always full width now (detail moved to bottom)
  const taskPanelWidth = terminalColumns - 2;
  
  // Reset scroll offset when new logs arrive and we're at the bottom
  useEffect(() => {
    if (logScrollOffset === 0) {
      // Already at bottom, no action needed - new logs will appear automatically
    }
  }, [logs.length, logScrollOffset]);

  // Expose addLog to parent for external log integration
  useEffect(() => {
    if (onLogCallback) {
      onLogCallback(addLog);
    }
  }, [onLogCallback, addLog]);

  // Derive pause state from tasks: project root task with status === 'blocked' means paused
  // Also fall back to getPausedProjects() for backward compatibility
  useEffect(() => {
    // Derive from task list: find root tasks (title = projectId, no deps) that are blocked
    const derivedPaused = new Set<string>();
    const allTasksForPause = isMultiProject ? multiProjectSse.allTasks : tasks;
    for (const task of allTasksForPause) {
      if (task.dependencies.length === 0 && task.status === 'blocked') {
        // This could be a project root task — use projectId or title
        const pid = task.projectId || task.title;
        if (pid && projects.includes(pid)) {
          derivedPaused.add(pid);
        }
      }
    }

    // Fall back to getPausedProjects() if no root tasks found (transition period)
    if (derivedPaused.size === 0 && getPausedProjects) {
      const paused = getPausedProjects();
      for (const p of paused) {
        derivedPaused.add(p);
      }
    }

    setPausedProjects(prev => {
      // Only update if the set actually changed — returning the same reference
      // causes React to skip the re-render (Object.is equality check)
      if (setsEqual(prev, derivedPaused)) {
        return prev;
      }
      return derivedPaused;
    });
  }, [tasks, isMultiProject, multiProjectSse.allTasks, projects, getPausedProjects]);

  // Sync enabledFeatures from TaskRunner
  useEffect(() => {
    if (!getEnabledFeatures) return;
    
    const enabled = getEnabledFeatures();
    const enabledSet = new Set(enabled);
    
    setEnabledFeatures(prev => {
      if (setsEqual(prev, enabledSet)) {
        return prev;
      }
      return enabledSet;
    });
  }, [getEnabledFeatures, tasks]); // Re-sync when tasks change (poll interval)

  // Auto-clear activeFeatures when ALL tasks for a feature reach terminal state
  // Terminal states: completed, validated, cancelled, superseded, archived
  useEffect(() => {
    if (activeFeatures.size === 0) return;

    const TERMINAL_STATUSES: Set<string> = new Set([
      'completed', 'validated', 'cancelled', 'superseded', 'archived',
    ]);

    const featuresToClear: string[] = [];
    for (const featureId of activeFeatures) {
      // Get all tasks for this feature (or ungrouped tasks)
      const featureTasks = featureId === UNGROUPED_FEATURE_ID
        ? tasks.filter(t => !t.feature_id)
        : tasks.filter(t => t.feature_id === featureId);

      // If there are tasks and ALL are in terminal state, auto-clear
      if (featureTasks.length > 0 && featureTasks.every(t => TERMINAL_STATUSES.has(t.status))) {
        featuresToClear.push(featureId);
      }
    }

    if (featuresToClear.length > 0) {
      setActiveFeatures(prev => {
        const next = new Set(prev);
        for (const fid of featuresToClear) {
          next.delete(fid);
        }
        return next;
      });
      for (const fid of featuresToClear) {
        if (onDisableFeature) {
          onDisableFeature(fid);
        }
        addLog({
          level: 'info',
          message: `Auto-deactivated feature (all tasks complete): ${fid}`,
        });
      }
    }
  }, [tasks, activeFeatures, onDisableFeature, addLog]);

  // Stable callbacks for toggling section collapsed states (avoids new ref on every render)
  const handleToggleCompleted = useCallback(() => {
    setCompletedCollapsed(prev => !prev);
  }, []);

  const handleToggleDraft = useCallback(() => {
    setDraftCollapsed(prev => !prev);
  }, []);

  const availableTaskIds = useMemo(
    () => new Set(tasks.map((task) => task.id)),
    [tasks],
  );

  // Find selected task
  const selectedTask = tasks.find((t) => t.id === selectedTaskId) || null;
  const previewTaskId = resolvePreviewTaskId(selectedTaskId, availableTaskIds);
  const previewTask = tasks.find((t) => t.id === previewTaskId) || null;
  const selectedCron = crons.find((c) => c.id === selectedCronId) || null;

  const closeCronLinkEditor = useCallback(() => {
    setCronLinkEditorOpen(false);
    setCronLinkEditorProjectId(null);
    setCronLinkEditorTaskIds(new Set());
    setCronLinkEditorCursor(0);
    cronLinkEditorTaskIdsRef.current = new Set();
    cronLinkEditorCursorRef.current = 0;
  }, []);

  const taskBelongsToProject = useCallback((task: TaskDisplay, projectId: string): boolean => {
    const resolvedProjectId = task.projectId ?? (!isMultiProject ? config.project : undefined);
    return resolvedProjectId === projectId;
  }, [isMultiProject, config.project]);

  const cronLinkEditorTasks = useMemo(() => {
    if (!cronLinkEditorProjectId) return [];
    return tasks.filter((task) => taskBelongsToProject(task, cronLinkEditorProjectId));
  }, [tasks, cronLinkEditorProjectId, taskBelongsToProject]);

  const getSelectedCronProjectId = useCallback((cron: CronDisplay | null): string | null => {
    if (cron?.projectId) return cron.projectId;
    if (!isMultiProject) return config.project;
    if (activeProject !== 'all') return activeProject;
    return null;
  }, [isMultiProject, config.project, activeProject]);

  const parseCronTitleSchedule = useCallback((value: string): { title: string; schedule: string } | null => {
    const parts = value.split('|');
    if (parts.length !== 2) return null;
    const [rawTitle, rawSchedule] = parts;
    const title = rawTitle?.trim();
    const schedule = rawSchedule?.trim();
    if (!title || !schedule) return null;
    return { title, schedule };
  }, []);

  const parseTaskIdList = useCallback((value: string): string[] => {
    return value
      .split(',')
      .map((taskId) => taskId.trim())
      .filter((taskId) => taskId.length > 0);
  }, []);

  // Task filter hook - provides filtered tasks and navigation order
  // Replaces direct flattenFeatureOrder call with filter-aware version
  const {
    filterText,
    filterMode,
    filteredTasks,
    navigationOrder,
    matchCount,
    totalCount,
    activate: activateFilter,
    deactivate: deactivateFilter,
    lockIn: lockInFilter,
    handleChar: handleFilterChar,
    handleBackspace: handleFilterBackspace,
  } = useTaskFilter({
    tasks,
    completedCollapsed,
    draftCollapsed,
    collapsedFeatures,
    collapsedProjects,
    visibleGroups,
    cancelledCollapsed,
    supersededCollapsed,
    archivedCollapsed,
    groupByProject: isMultiProject && activeProject === 'all',
  });

  // Auto-scroll task list to keep selected task in view
  useEffect(() => {
    if (!selectedTaskId || taskViewportHeight <= 0) return;
    
    const selectedIndex = navigationOrder.indexOf(selectedTaskId);
    if (selectedIndex === -1) return;
    
    // Ensure selected task is visible in viewport
    // Use consistent boundary checks: trigger scroll when selection reaches the edge
    if (selectedIndex < taskScrollOffset) {
      // Selected is above viewport - scroll up immediately
      setTaskScrollOffset(selectedIndex);
    } else if (selectedIndex > taskScrollOffset + taskViewportHeight - 1) {
      // Selected is below viewport - scroll down immediately when hitting bottom edge
      // Note: using > instead of >= and subtracting 1 to match up-scroll behavior
      setTaskScrollOffset(selectedIndex - taskViewportHeight + 1);
    }
  }, [selectedTaskId, navigationOrder, taskScrollOffset, taskViewportHeight]);

  // Keep selected cron valid when switching projects/views or after data refresh
  useEffect(() => {
    if (crons.length === 0) {
      if (selectedCronId !== null) {
        setSelectedCronId(null);
      }
      setCronScrollOffset(0);
      return;
    }

    if (!selectedCronId || !crons.some((cron) => cron.id === selectedCronId)) {
      setSelectedCronId(crons[0]?.id ?? null);
    }
  }, [crons, selectedCronId]);

  useEffect(() => {
    if (!cronLinkEditorOpen) return;
    if (!selectedCron || !cronLinkEditorProjectId) {
      closeCronLinkEditor();
      return;
    }
    const cronStillVisible = crons.some((cron) => cron.id === selectedCron.id);
    if (!cronStillVisible) {
      closeCronLinkEditor();
    }
  }, [cronLinkEditorOpen, selectedCron, cronLinkEditorProjectId, crons, closeCronLinkEditor]);

  useEffect(() => {
    if (!cronLinkEditorOpen) return;
    if (cronLinkEditorTasks.length === 0) {
      if (cronLinkEditorCursorRef.current !== 0) {
        cronLinkEditorCursorRef.current = 0;
        setCronLinkEditorCursor(0);
      }
      return;
    }

    const bounded = Math.max(0, Math.min(cronLinkEditorCursorRef.current, cronLinkEditorTasks.length - 1));
    if (bounded !== cronLinkEditorCursorRef.current) {
      cronLinkEditorCursorRef.current = bounded;
      setCronLinkEditorCursor(bounded);
    }
  }, [cronLinkEditorOpen, cronLinkEditorTasks]);

  // Auto-scroll cron list to keep selected cron in view
  useEffect(() => {
    if (!selectedCronId || cronViewportHeight <= 0) return;
    const selectedIndex = crons.findIndex((cron) => cron.id === selectedCronId);
    if (selectedIndex === -1) return;

    if (selectedIndex < cronScrollOffset) {
      setCronScrollOffset(selectedIndex);
    } else if (selectedIndex > cronScrollOffset + cronViewportHeight - 1) {
      setCronScrollOffset(selectedIndex - cronViewportHeight + 1);
    }
  }, [selectedCronId, crons, cronScrollOffset, cronViewportHeight]);

  // Reset details scroll offset when previewed task changes
  useEffect(() => {
    setDetailsScrollOffset(0);
  }, [previewTaskId]);

  // All project tabs including 'all' at the front
  const allProjectTabs = ['all', ...projects];

  /**
   * Edit a task in an external editor ($EDITOR or $VISUAL, fallback to vim).
   * Suspends the TUI, launches editor synchronously, then resumes TUI and syncs changes.
   */
  const editTaskInEditor = useCallback(async (taskId: string, taskPath: string) => {
    if (!onEditTask) {
      addLog({ level: 'warn', message: 'Editor feature not available (no onEditTask callback)' });
      return;
    }

    addLog({ level: 'info', message: `Opening editor for: ${taskPath}`, taskId });
    setIsEditing(true);

    try {
      // Call the external editor handler (provided by TaskRunner)
      const result = await onEditTask(taskId, taskPath);
      
      if (result !== null) {
        addLog({ level: 'info', message: 'Task content updated from editor', taskId });
        refetch(); // Refresh to show updated content
      } else {
        addLog({ level: 'info', message: 'Editor cancelled or no changes made', taskId });
      }
    } catch (err) {
      addLog({ 
        level: 'error', 
        message: `Editor failed: ${err instanceof Error ? err.message : String(err)}`, 
        taskId 
      });
    } finally {
      setIsEditing(false);
    }
  }, [onEditTask, addLog, refetch]);

  /**
   * Apply metadata changes to all target tasks.
   * Only sends fields that differ from their original values.
   * @param overrides - Optional override values to use instead of state (for immediate Esc commit)
   */
  const applyMetadataChanges = useCallback((overrides?: {
    feature_id?: string;
    git_branch?: string;
    target_workdir?: string;
  }) => {
    if (!onUpdateMetadata || metadataTargetTasks.length === 0) {
      setShowMetadataPopup(false);
      return;
    }

    // Use overrides if provided, otherwise use state values
    const effectiveFeatureId = overrides?.feature_id ?? metadataFeatureIdValue;
    const effectiveBranch = overrides?.git_branch ?? metadataBranchValue;
    const effectiveWorkdir = overrides?.target_workdir ?? metadataWorkdirValue;

    // Build changed fields object (only include fields that differ from original)
    const changedFields: {
      status?: EntryStatus;
      feature_id?: string;
      git_branch?: string;
      merge_target_branch?: string;
      merge_policy?: MergePolicy;
      merge_strategy?: MergeStrategy;
      remote_branch_policy?: RemoteBranchPolicy;
      open_pr_before_merge?: boolean;
      execution_mode?: ExecutionMode;
      checkout_enabled?: boolean;
      target_workdir?: string;
      schedule?: string;
      agent?: string;
      model?: string;
      direct_prompt?: string;
    } = {};

    if (metadataStatusValue !== metadataOriginalValues.status) {
      changedFields.status = metadataStatusValue;
    }
    if (effectiveFeatureId !== metadataOriginalValues.feature_id) {
      changedFields.feature_id = effectiveFeatureId;
    }
    if (effectiveBranch !== metadataOriginalValues.git_branch) {
      changedFields.git_branch = effectiveBranch;
    }
    if (metadataMergeTargetBranchValue !== metadataOriginalValues.merge_target_branch) {
      changedFields.merge_target_branch = metadataMergeTargetBranchValue;
    }
    if (metadataExecutionModeValue !== metadataOriginalValues.execution_mode) {
      changedFields.execution_mode = metadataExecutionModeValue;
    }
    if (metadataCheckoutEnabledValue !== metadataOriginalValues.checkout_enabled) {
      changedFields.checkout_enabled = metadataCheckoutEnabledValue;
    }
    if (metadataMergePolicyValue !== metadataOriginalValues.merge_policy) {
      changedFields.merge_policy = metadataMergePolicyValue;
    }
    if (metadataMergeStrategyValue !== metadataOriginalValues.merge_strategy) {
      changedFields.merge_strategy = metadataMergeStrategyValue;
    }
    if (metadataRemoteBranchPolicyValue !== metadataOriginalValues.remote_branch_policy) {
      changedFields.remote_branch_policy = metadataRemoteBranchPolicyValue;
    }
    if (metadataOpenPrBeforeMergeValue !== metadataOriginalValues.open_pr_before_merge) {
      changedFields.open_pr_before_merge = metadataOpenPrBeforeMergeValue;
    }
    if (effectiveWorkdir !== metadataOriginalValues.target_workdir) {
      changedFields.target_workdir = effectiveWorkdir;
    }
    if (metadataScheduleValue !== metadataOriginalValues.schedule) {
      changedFields.schedule = metadataScheduleValue;
    }
    if (metadataAgentValue !== metadataOriginalValues.agent) {
      changedFields.agent = metadataAgentValue;
    }
    if (metadataModelValue !== metadataOriginalValues.model) {
      changedFields.model = metadataModelValue;
    }
    if (metadataDirectPromptValue !== metadataOriginalValues.direct_prompt) {
      changedFields.direct_prompt = metadataDirectPromptValue;
    }

    // Skip if no changes
    if (Object.keys(changedFields).length === 0) {
      addLog({ level: 'info', message: 'No metadata changes to apply' });
      setShowMetadataPopup(false);
      setSelectedTaskIds(new Set());
      return;
    }

    const changeDescription = Object.entries(changedFields)
      .map(([key, value]) => `${key}=${value}`)
      .join(', ');

    addLog({
      level: 'info',
      message: `Updating ${metadataTargetTasks.length} task(s): ${changeDescription}`,
    });

    // Update all tasks in parallel
    Promise.all(
      metadataTargetTasks.map(task => onUpdateMetadata(task.path, changedFields))
    )
      .then(() => {
        addLog({
          level: 'info',
          message: `Updated ${metadataTargetTasks.length} task(s) successfully`,
        });
        refetch();
      })
      .catch((err) => {
        addLog({
          level: 'error',
          message: `Failed to update metadata: ${err}`,
        });
      });

    // Close popup and clear multi-select
    setShowMetadataPopup(false);
    setSelectedTaskIds(new Set());
  }, [
    onUpdateMetadata,
    metadataTargetTasks,
    metadataStatusValue,
    metadataFeatureIdValue,
    metadataBranchValue,
    metadataMergeTargetBranchValue,
    metadataExecutionModeValue,
    metadataCheckoutEnabledValue,
    metadataMergePolicyValue,
    metadataMergeStrategyValue,
    metadataRemoteBranchPolicyValue,
    metadataOpenPrBeforeMergeValue,
    metadataWorkdirValue,
    metadataScheduleValue,
    metadataOriginalValues,
    addLog,
    refetch,
  ]);

  const openSingleTaskMetadataPopup = useCallback(async (task: TaskDisplay) => {
    let fetchedProjects = projects;
    if (onListProjects) {
      try {
        fetchedProjects = await onListProjects();
      } catch {
        fetchedProjects = projects;
      }
    }

    // Fetch cron names if task has cron_ids
    let fetchedCronNames: Record<string, string> = {};
    if (task.cron_ids && task.cron_ids.length > 0 && task.projectId) {
      try {
        const apiClient = getApiClient();
        fetchedCronNames = await apiClient.getCronNames(task.projectId);
      } catch {
        // Graceful fallback - continue with empty cron names on error
        fetchedCronNames = {};
      }
    }

    setAllProjects(fetchedProjects);
    setCronNames(fetchedCronNames);
    setMetadataTargetTasks([task]);
    setMetadataPopupMode('single');
    setMetadataFocusedField('status');
    setMetadataStatusValue(task.status);
    setMetadataStatusIndex(ENTRY_STATUSES.indexOf(task.status));
    setMetadataFeatureIdValue(task.feature_id || '');
    setMetadataBranchValue(task.gitBranch || '');
    setMetadataMergeTargetBranchValue(task.mergeTargetBranch || '');
    setMetadataExecutionModeValue(task.executionMode || 'worktree');
    setMetadataCheckoutEnabledValue(task.checkoutEnabled ?? true);
    setMetadataMergePolicyValue(task.mergePolicy || 'auto_merge');
    setMetadataMergeStrategyValue(task.mergeStrategy || 'squash');
    setMetadataRemoteBranchPolicyValue(task.remoteBranchPolicy || 'delete');
    setMetadataOpenPrBeforeMergeValue(task.openPrBeforeMerge ?? false);
    setMetadataWorkdirValue(task.resolvedWorkdir || task.workdir || '');
    setMetadataScheduleValue(task.schedule || '');
    setMetadataProjectValue(task.projectId || '');
    setMetadataProjectIndex(fetchedProjects.indexOf(task.projectId || '') >= 0 ? fetchedProjects.indexOf(task.projectId || '') : 0);
    setMetadataAgentValue(task.agent || '');
    setMetadataModelValue(task.model || '');
    setMetadataDirectPromptValue(task.direct_prompt || '');
    setMetadataOriginalValues({
      status: task.status,
      feature_id: task.feature_id || '',
      git_branch: task.gitBranch || '',
      merge_target_branch: task.mergeTargetBranch || '',
      execution_mode: task.executionMode || 'worktree',
      checkout_enabled: task.checkoutEnabled ?? true,
      merge_policy: task.mergePolicy || 'auto_merge',
      merge_strategy: task.mergeStrategy || 'squash',
      remote_branch_policy: task.remoteBranchPolicy || 'delete',
      open_pr_before_merge: task.openPrBeforeMerge ?? false,
      target_workdir: task.resolvedWorkdir || task.workdir || '',
      schedule: task.schedule || '',
      project: task.projectId || '',
      agent: task.agent || '',
      model: task.model || '',
      direct_prompt: task.direct_prompt || '',
    });
    setMetadataInteractionMode('navigate');
    setMetadataEditBuffer('');
    setMetadataDirty(false);
    setShowMetadataPopup(true);
  }, [onListProjects, projects]);

  const toggleCollapsedForTarget = useCallback((target: TaskTreeRowTarget): boolean => {
    if (target.kind === 'status_header') {
      if (target.statusGroup === 'completed') {
        setCompletedCollapsed(prev => !prev);
        return true;
      }
      if (target.statusGroup === 'draft') {
        setDraftCollapsed(prev => !prev);
        return true;
      }
      if (target.statusGroup === 'cancelled') {
        setCancelledCollapsed(prev => !prev);
        return true;
      }
      if (target.statusGroup === 'superseded') {
        setSupersededCollapsed(prev => !prev);
        return true;
      }
      if (target.statusGroup === 'archived') {
        setArchivedCollapsed(prev => !prev);
        return true;
      }
      return false;
    }

    const collapseKey = getTaskTreeCollapseKey(target);
    if (collapseKey) {
      if (collapseKey.startsWith('project:')) {
        const projectId = collapseKey.slice('project:'.length);
        setCollapsedProjects(prev => {
          const next = new Set(prev);
          if (next.has(projectId)) {
            next.delete(projectId);
          } else {
            next.add(projectId);
          }
          return next;
        });
      } else {
        setCollapsedFeatures(prev => {
          const next = new Set(prev);
          if (next.has(collapseKey)) {
            next.delete(collapseKey);
          } else {
            next.add(collapseKey);
          }
          return next;
        });
      }
      return true;
    }

    return false;
  }, []);

  const onMouseEvent = useCallback((event: TUIMouseEvent) => {
    if (!shouldHandleTaskTreeMouseEvent({
      viewMode,
      showMetadataPopup,
      showSettingsPopup,
      deletePopupOpen,
      sessionPopupOpen,
      cronActionOpen,
      cronDeleteConfirmOpen,
      cronLinkEditorOpen,
      showHelp,
      isEditing,
    })) {
      return;
    }

    // Ignore mouse movement — only clicks change selection
    if (event.kind === 'move') {
      return;
    }

    const viewportStartRow = getTaskTreeViewportStartRow(isMultiProject, filterMode);
    const rowTarget = findTaskTreeTargetFromMouseRow(taskTreeVisibleRows, event.row, viewportStartRow);

    if (!rowTarget) return;

    const action = resolveTaskTreeClickAction(rowTarget, event.button);
    
    // Always set focus and selection for task clicks, even if action is 'noop'
    if (rowTarget.kind === 'task') {
      setFocusedPanel('tasks');
      setSelectedTaskId(rowTarget.id);
    }
    
    if (action === 'noop') return;

    if (action === 'toggle_collapsed') {
      toggleCollapsedForTarget(rowTarget);
      return;
    }

    if (rowTarget.kind !== 'task' || !rowTarget.taskId) {
      return;
    }

    const clickedTask = tasks.find((task) => task.id === rowTarget.taskId);
    if (!clickedTask) return;

    if (action === 'open_editor') {
      editTaskInEditor(clickedTask.id, clickedTask.path);
      return;
    }

    if (action === 'open_metadata') {
      openSingleTaskMetadataPopup(clickedTask);
    }
  }, [
    viewMode,
    showMetadataPopup,
    showSettingsPopup,
    deletePopupOpen,
    sessionPopupOpen,
    cronActionOpen,
    cronDeleteConfirmOpen,
    cronLinkEditorOpen,
    showHelp,
    isEditing,
    isMultiProject,
    filterMode,
    taskTreeVisibleRows,
    toggleCollapsedForTarget,
    tasks,
    editTaskInEditor,
    openSingleTaskMetadataPopup,
  ]);

  useMouseInput(onMouseEvent);

  // Handle keyboard input
  useInput((input, key) => {
    // === Metadata Popup Mode (3-mode state machine) ===
    // NAVIGATE: j/k moves between fields, Enter enters edit mode, Esc closes popup
    // EDIT_TEXT: typing updates buffer, Enter saves field immediately, Esc discards
    // EDIT_STATUS: j/k cycles status options, Enter saves immediately, Esc discards
    // EDIT_PROJECT: j/k cycles project options, Enter moves task, Esc discards
    if (showMetadataPopup) {
      const METADATA_FIELDS: MetadataField[] = metadataPopupMode === 'feature'
        ? METADATA_FIELDS_FEATURE_SETTINGS
        : METADATA_FIELDS_DEFAULT;
      
      // Helper: save a single field immediately to API
      // Helper: save a single field immediately to API
      const saveField = (field: MetadataField, value: string | EntryStatus) => {
        if (!onUpdateMetadata || metadataTargetTasks.length === 0) return;

        const normalizedValue = typeof value === 'string'
          ? normalizeMetadataFieldValue(field, value)
          : value;

        if (typeof normalizedValue === 'string') {
          const validationError = validateMetadataFieldValue(field, normalizedValue);
          if (validationError) {
            addLog({ level: 'error', message: validationError });
            return;
          }
        }

        const updates: {
          [key: string]: string | EntryStatus | MergePolicy | MergeStrategy | RemoteBranchPolicy | ExecutionMode | boolean;
        } = {};

        if (field === 'execution_mode') {
          if (typeof normalizedValue !== 'string' || !EXECUTION_MODES.includes(normalizedValue as ExecutionMode)) {
            return;
          }
          updates.execution_mode = normalizedValue as ExecutionMode;
        } else if (field === 'merge_policy') {
          if (typeof normalizedValue !== 'string' || !MERGE_POLICIES.includes(normalizedValue as MergePolicy)) {
            return;
          }
          updates.merge_policy = normalizedValue as MergePolicy;
        } else if (field === 'merge_strategy') {
          if (typeof normalizedValue !== 'string' || !MERGE_STRATEGIES.includes(normalizedValue as MergeStrategy)) {
            return;
          }
          updates.merge_strategy = normalizedValue as MergeStrategy;
        } else if (field === 'remote_branch_policy') {
          if (typeof normalizedValue !== 'string' || !REMOTE_BRANCH_POLICIES.includes(normalizedValue as RemoteBranchPolicy)) {
            return;
          }
          updates.remote_branch_policy = normalizedValue as RemoteBranchPolicy;
        } else if (field === 'checkout_enabled' || field === 'open_pr_before_merge') {
          if (typeof normalizedValue !== 'string') {
            return;
          }

          updates[field] = normalizedValue === 'true';
        } else {
          updates[field] = normalizedValue;
        }
        
        // Mark dirty synchronously so Escape handler sees it immediately
        setMetadataDirty(true);
        
        const applyPlan = buildMetadataApplyPlan(metadataTargetTasks, metadataPopupMode);
        const totalSkipped = applyPlan.skippedTerminal.length + applyPlan.skippedCheckout.length;

        if (metadataPopupMode === 'feature' && totalSkipped > 0) {
          addLog({
            level: 'info',
            message: `Feature-scope apply (${field}): updating ${applyPlan.updatable.length}, skipped ${applyPlan.skippedTerminal.length} terminal, ${applyPlan.skippedCheckout.length} checkout`,
          });
        }

        if (applyPlan.updatable.length === 0) {
          addLog({
            level: 'warn',
            message: `Skipped metadata update (${field}): no eligible tasks`,
          });
          return;
        }

        Promise.all(
          applyPlan.updatable.map(task => onUpdateMetadata(task.path, updates))
        ).then(() => {
          addLog({
            level: 'info',
            message: `Updated ${field} for ${applyPlan.updatable.length} task(s): ${Object.values(updates)[0]}`,
          });
          // Update original values to reflect the saved state
          setMetadataOriginalValues(prev => ({
            ...prev,
            ...(field === 'execution_mode' ? { execution_mode: updates.execution_mode as ExecutionMode } : {}),
            ...(field === 'merge_policy' ? { merge_policy: updates.merge_policy as MergePolicy } : {}),
            ...(field === 'merge_strategy' ? { merge_strategy: updates.merge_strategy as MergeStrategy } : {}),
            ...(field === 'remote_branch_policy' ? { remote_branch_policy: updates.remote_branch_policy as RemoteBranchPolicy } : {}),
            ...(field === 'checkout_enabled' ? { checkout_enabled: updates.checkout_enabled as boolean } : {}),
            ...(field === 'open_pr_before_merge' ? { open_pr_before_merge: updates.open_pr_before_merge as boolean } : {}),
              ...(field !== 'execution_mode' &&
            field !== 'merge_policy' &&
            field !== 'merge_strategy' &&
            field !== 'remote_branch_policy' &&
            field !== 'checkout_enabled' &&
            field !== 'open_pr_before_merge'
              ? { [field]: normalizedValue }
              : {}),
          }));
          refetch();
        }).catch((err) => {
          addLog({ level: 'error', message: `Failed to update ${field}: ${err}` });
        });
      };
      
      // Helper: move task(s) to a different project
      const moveTaskToProject = (newProjectId: string) => {
        if (!onMoveTask || metadataTargetTasks.length === 0) return;
        
        // Skip if no change
        if (newProjectId === metadataOriginalValues.project) {
          addLog({ level: 'info', message: 'No project change' });
          return;
        }
        
        addLog({ level: 'info', message: `Moving ${metadataTargetTasks.length} task(s) to project: ${newProjectId}` });
        
        // Mark dirty synchronously so Escape handler sees it immediately
        setMetadataDirty(true);
        
        Promise.all(
          metadataTargetTasks.map(task => onMoveTask(task.path, newProjectId))
        ).then((results) => {
          addLog({ level: 'info', message: `Moved ${results.length} task(s) to project: ${newProjectId}` });
          // Update original values to reflect the saved state
          setMetadataOriginalValues(prev => ({ ...prev, project: newProjectId }));
          setMetadataProjectValue(newProjectId);
          refetch();
        }).catch((err) => {
          addLog({ level: 'error', message: `Failed to move task(s): ${err}` });
        });
      };

      // === ESCAPE KEY ===
      if (key.escape) {
        if (metadataInteractionMode === 'edit_text') {
          // In text edit mode: discard buffer, back to NAVIGATE (no save)
          setMetadataEditBuffer('');
          setMetadataInteractionMode('navigate');
        } else if (metadataInteractionMode === 'edit_status') {
          // In status edit mode: discard selection, restore original, back to NAVIGATE
          const originalIndex = ENTRY_STATUSES.indexOf(metadataOriginalValues.status);
          setMetadataStatusIndex(Math.max(0, originalIndex));
          setMetadataStatusValue(metadataOriginalValues.status);
          setMetadataInteractionMode('navigate');
        } else if (metadataInteractionMode === 'edit_project') {
          // In project edit mode: discard selection, restore original, back to NAVIGATE
          const originalIndex = effectiveProjects.indexOf(metadataOriginalValues.project);
          setMetadataProjectIndex(Math.max(0, originalIndex));
          setMetadataProjectValue(metadataOriginalValues.project);
          setMetadataInteractionMode('navigate');
        } else {
          // In navigate mode: close popup entirely
          setShowMetadataPopup(false);
          setMetadataInteractionMode('navigate');
          setMetadataEditBuffer('');
          // Exit multi-select mode if changes were saved during this popup session
          if (metadataDirty) {
            setSelectedTaskIds(new Set());
            setMetadataDirty(false);
          }
        }
        return;
      }

      // === NAVIGATE MODE ===
      if (metadataInteractionMode === 'navigate') {
        // j/k or Down/Up: navigate between fields
        if (input === 'j' || key.downArrow) {
          const currentIndex = METADATA_FIELDS.indexOf(metadataFocusedField);
          const nextIndex = Math.min(currentIndex + 1, METADATA_FIELDS.length - 1);
          setMetadataFocusedField(METADATA_FIELDS[nextIndex]);
          return;
        }
        if (input === 'k' || key.upArrow) {
          const currentIndex = METADATA_FIELDS.indexOf(metadataFocusedField);
          const prevIndex = Math.max(currentIndex - 1, 0);
          setMetadataFocusedField(METADATA_FIELDS[prevIndex]);
          return;
        }

        // Enter: enter edit mode for focused field
        if (key.return) {
          if (metadataFocusedField === 'status') {
            // Status field: transition to EDIT_STATUS mode
            setMetadataInteractionMode('edit_status');
          } else if (metadataFocusedField === 'project') {
            // Project field: transition to EDIT_PROJECT mode
            setMetadataInteractionMode('edit_project');
          } else {
            // Text field: transition to EDIT_TEXT mode with pre-filled buffer
            let currentValue = '';
            switch (metadataFocusedField) {
              case 'feature_id':
                currentValue = metadataFeatureIdValue;
                break;
              case 'git_branch':
                currentValue = metadataBranchValue;
                break;
              case 'merge_target_branch':
                currentValue = metadataMergeTargetBranchValue;
                break;
              case 'execution_mode':
                currentValue = metadataExecutionModeValue;
                break;
              case 'checkout_enabled':
                currentValue = metadataCheckoutEnabledValue ? 'true' : 'false';
                break;
              case 'merge_policy':
                currentValue = metadataMergePolicyValue;
                break;
              case 'merge_strategy':
                currentValue = metadataMergeStrategyValue;
                break;
              case 'remote_branch_policy':
                currentValue = metadataRemoteBranchPolicyValue;
                break;
              case 'open_pr_before_merge':
                currentValue = metadataOpenPrBeforeMergeValue ? 'true' : 'false';
                break;
              case 'target_workdir':
                currentValue = metadataWorkdirValue;
                break;
              case 'schedule':
                currentValue = metadataScheduleValue;
                break;
              case 'agent':
                currentValue = metadataAgentValue;
                break;
              case 'model':
                currentValue = metadataModelValue;
                break;
              case 'direct_prompt':
                currentValue = metadataDirectPromptValue;
                break;
            }
            setMetadataEditBuffer(currentValue);
            setMetadataInteractionMode('edit_text');
          }
          return;
        }

        // Block other input in navigate mode
        return;
      }

      // === EDIT_TEXT MODE ===
      if (metadataInteractionMode === 'edit_text') {
        // Enter: save field immediately to API and return to NAVIGATE
        if (key.return) {
          const value = normalizeMetadataFieldValue(metadataFocusedField, metadataEditBuffer);
          // Update local state
          switch (metadataFocusedField) {
            case 'feature_id':
              setMetadataFeatureIdValue(value);
              break;
            case 'git_branch':
              setMetadataBranchValue(value);
              break;
            case 'merge_target_branch':
              setMetadataMergeTargetBranchValue(value);
              break;
            case 'execution_mode':
              if (EXECUTION_MODES.includes(value as ExecutionMode)) {
                setMetadataExecutionModeValue(value as ExecutionMode);
              }
              break;
            case 'checkout_enabled':
              if (value === 'true' || value === 'false') {
                setMetadataCheckoutEnabledValue(value === 'true');
              }
              break;
            case 'merge_policy':
              if (MERGE_POLICIES.includes(value as MergePolicy)) {
                setMetadataMergePolicyValue(value as MergePolicy);
              }
              break;
            case 'merge_strategy':
              if (MERGE_STRATEGIES.includes(value as MergeStrategy)) {
                setMetadataMergeStrategyValue(value as MergeStrategy);
              }
              break;
            case 'remote_branch_policy':
              if (REMOTE_BRANCH_POLICIES.includes(value as RemoteBranchPolicy)) {
                setMetadataRemoteBranchPolicyValue(value as RemoteBranchPolicy);
              }
              break;
            case 'open_pr_before_merge':
              if (value === 'true' || value === 'false') {
                setMetadataOpenPrBeforeMergeValue(value === 'true');
              }
              break;
            case 'target_workdir':
              setMetadataWorkdirValue(value);
              break;
            case 'schedule':
              setMetadataScheduleValue(value);
              break;
            case 'agent':
              setMetadataAgentValue(value);
              break;
            case 'model':
              setMetadataModelValue(value);
              break;
            case 'direct_prompt':
              setMetadataDirectPromptValue(value);
              break;
          }
          // Save to API immediately
          saveField(metadataFocusedField, value);
          setMetadataEditBuffer('');
          setMetadataInteractionMode('navigate');
          return;
        }

        // Backspace: remove last character
        if (key.backspace || key.delete) {
          setMetadataEditBuffer(prev => prev.slice(0, -1));
          return;
        }

        // Printable characters: append to buffer
        if (input && input.length === 1 && !key.ctrl && !key.meta) {
          setMetadataEditBuffer(prev => prev + input);
          return;
        }

        // Block other input in edit_text mode
        return;
      }

      // === EDIT_STATUS MODE ===
      if (metadataInteractionMode === 'edit_status') {
        // j/k or Down/Up: cycle through status options
        if (input === 'j' || key.downArrow) {
          const nextIndex = Math.min(metadataStatusIndex + 1, ENTRY_STATUSES.length - 1);
          setMetadataStatusIndex(nextIndex);
          setMetadataStatusValue(ENTRY_STATUSES[nextIndex]);
          return;
        }
        if (input === 'k' || key.upArrow) {
          const prevIndex = Math.max(metadataStatusIndex - 1, 0);
          setMetadataStatusIndex(prevIndex);
          setMetadataStatusValue(ENTRY_STATUSES[prevIndex]);
          return;
        }

        // Enter: save status immediately to API and return to NAVIGATE
        if (key.return) {
          const value = ENTRY_STATUSES[metadataStatusIndex];
          saveField('status', value);
          setMetadataInteractionMode('navigate');
          return;
        }

        // Block other input in edit_status mode
        return;
      }

      // === EDIT_PROJECT MODE ===
      if (metadataInteractionMode === 'edit_project') {
        // j/k or Down/Up: cycle through project options
        if (input === 'j' || key.downArrow) {
          const nextIndex = Math.min(metadataProjectIndex + 1, effectiveProjects.length - 1);
          setMetadataProjectIndex(nextIndex);
          setMetadataProjectValue(effectiveProjects[nextIndex]);
          return;
        }
        if (input === 'k' || key.upArrow) {
          const prevIndex = Math.max(metadataProjectIndex - 1, 0);
          setMetadataProjectIndex(prevIndex);
          setMetadataProjectValue(effectiveProjects[prevIndex]);
          return;
        }

        // Enter: move task(s) to selected project and return to NAVIGATE
        if (key.return) {
          const newProjectId = effectiveProjects[metadataProjectIndex];
          moveTaskToProject(newProjectId);
          setMetadataInteractionMode('navigate');
          return;
        }

        // Block other input in edit_project mode
        return;
      }

      // Fallback: block all input when popup is open
      return;
    }

    // === Delete Confirmation Popup Mode ===
    if (deletePopupOpen) {
      // Escape: cancel and close popup
      if (key.escape) {
        setDeletePopupOpen(false);
        setTasksToDelete([]);
        return;
      }
      
      // Enter: confirm delete
      if (key.return) {
        if (onDeleteTasks && tasksToDelete.length > 0) {
          const paths = tasksToDelete.map(t => t.path);
          addLog({
            level: 'info',
            message: `Deleting ${tasksToDelete.length} task(s)...`,
          });
          
          onDeleteTasks(paths)
            .then(() => {
              addLog({
                level: 'info',
                message: `Deleted ${tasksToDelete.length} task(s) successfully`,
              });
              refetch();
            })
            .catch((err: unknown) => {
              addLog({
                level: 'error',
                message: `Failed to delete tasks: ${err}`,
              });
            });
        }
        
        // Clear selection and close popup
        setSelectedTaskIds(new Set());
        setDeletePopupOpen(false);
        setTasksToDelete([]);
        return;
      }
      
      // Block all other input when popup is open
      return;
    }

    // === Session Select Popup Mode ===
    if (sessionPopupOpen) {
      // Escape: cancel and close popup
      if (key.escape) {
        setSessionPopupOpen(false);
        setSessionPopupIds([]);
        setSessionPopupSelectedIndex(0);
        setSessionPopupTmuxMode(false);
        return;
      }
      
      // j/k or Down/Up: navigate sessions
      if (input === 'j' || key.downArrow) {
        setSessionPopupSelectedIndex(prev => Math.min(prev + 1, sessionPopupIds.length - 1));
        return;
      }
      if (input === 'k' || key.upArrow) {
        setSessionPopupSelectedIndex(prev => Math.max(prev - 1, 0));
        return;
      }
      
      // Enter: open selected session (fullscreen or tmux depending on mode)
      if (key.return) {
        const sessionId = sessionPopupIds[sessionPopupSelectedIndex];
        const modeLabel = sessionPopupTmuxMode ? 'tmux window' : 'fullscreen';
        if (sessionId) {
          addLog({
            level: 'info',
            message: `Opening session in ${modeLabel}: ${sessionId}`,
          });
          if (sessionPopupTmuxMode && onOpenSessionTmux) {
            // Build task context for idle monitoring tracking
            const taskCtx: OpenSessionTaskContext | undefined = selectedTask ? {
              taskId: selectedTask.id,
              path: selectedTask.path,
              title: selectedTask.title,
              priority: selectedTask.priority,
              projectId: selectedTask.projectId || 'unknown',
              workdir: selectedTask.resolvedWorkdir || selectedTask.workdir || process.cwd(),
            } : undefined;
            onOpenSessionTmux(sessionId, taskCtx).catch((err: unknown) => {
              addLog({
                level: 'error',
                message: `Failed to open session: ${err}`,
              });
            });
          } else if (!sessionPopupTmuxMode && onOpenSession) {
            onOpenSession(sessionId).catch((err: unknown) => {
              addLog({
                level: 'error',
                message: `Failed to open session: ${err}`,
              });
            });
          }
        }
        setSessionPopupOpen(false);
        setSessionPopupIds([]);
        setSessionPopupSelectedIndex(0);
        setSessionPopupTmuxMode(false);
        return;
      }
      
      // Block all other input when popup is open
      return;
    }

    // === Cron Action Popup Mode ===
    if (cronActionOpen || cronActionOpenRef.current) {
      const currentMode = cronActionModeRef.current;
      if (key.escape) {
        setCronActionOpen(false);
        setCronActionInput('');
        cronActionOpenRef.current = false;
        cronActionInputRef.current = '';
        return;
      }

      if (key.backspace || key.delete) {
        cronActionInputRef.current = cronActionInputRef.current.slice(0, -1);
        setCronActionInput(cronActionInputRef.current);
        return;
      }

      if (key.return) {
        const selectedProjectId = getSelectedCronProjectId(selectedCron);
        const inputValue = cronActionInputRef.current;
        const closePopup = () => {
          setCronActionOpen(false);
          setCronActionInput('');
          cronActionOpenRef.current = false;
          cronActionInputRef.current = '';
        };

        const failInput = (message: string) => {
          addLog({ level: 'warn', message });
          closePopup();
        };

        if (currentMode === 'create') {
          if (!selectedProjectId) {
            failInput('Cannot create cron: no active project selected');
            return;
          }
          if (!onCreateCron) {
            failInput('Create cron action unavailable');
            return;
          }
          const parsed = parseCronTitleSchedule(inputValue);
          if (!parsed) {
            failInput('Invalid cron input. Use: title|schedule');
            return;
          }

          onCreateCron(selectedProjectId, parsed)
            .then(() => {
              addLog({ level: 'info', message: `Created cron: ${parsed.title}` });
              refetchCrons();
            })
            .catch((err: unknown) => {
              addLog({ level: 'error', message: `Failed to create cron: ${err}` });
            });
          closePopup();
          return;
        }

        if (!selectedCron || !selectedProjectId) {
          failInput('No cron selected');
          return;
        }

        if (currentMode === 'edit') {
          if (!onUpdateCron) {
            failInput('Update cron action unavailable');
            return;
          }
          const parsed = parseCronTitleSchedule(inputValue);
          if (!parsed) {
            failInput('Invalid cron input. Use: title|schedule');
            return;
          }

          onUpdateCron(selectedProjectId, selectedCron.id, parsed)
            .then(() => {
              addLog({ level: 'info', message: `Updated cron: ${selectedCron.id}` });
              refetchCrons();
            })
            .catch((err: unknown) => {
              addLog({ level: 'error', message: `Failed to update cron: ${err}` });
            });
          closePopup();
          return;
        }

        if (currentMode === 'replace-links') {
          if (!onSetCronLinkedTasks) {
            failInput('Replace linked tasks action unavailable');
            return;
          }
          const taskIds = parseTaskIdList(inputValue);
          onSetCronLinkedTasks(selectedProjectId, selectedCron.id, taskIds)
            .then((result: { count: number }) => {
              addLog({
                level: 'info',
                message: `Replaced linked tasks for ${selectedCron.id}: ${result.count} linked`,
              });
              refetch();
            })
            .catch((err: unknown) => {
              addLog({ level: 'error', message: `Failed to replace linked tasks: ${err}` });
            });
          closePopup();
          return;
        }

        const taskId = inputValue.trim();
        if (!taskId) {
          failInput('Task ID is required');
          return;
        }

        if (currentMode === 'add-link') {
          if (!onAddCronLinkedTask) {
            failInput('Add linked task action unavailable');
            return;
          }
          onAddCronLinkedTask(selectedProjectId, selectedCron.id, taskId)
            .then(() => {
              addLog({ level: 'info', message: `Linked task ${taskId} to cron ${selectedCron.id}` });
              refetch();
            })
            .catch((err: unknown) => {
              addLog({ level: 'error', message: `Failed to link task to cron: ${err}` });
            });
          closePopup();
          return;
        }

        if (!onRemoveCronLinkedTask) {
          failInput('Remove linked task action unavailable');
          return;
        }
        onRemoveCronLinkedTask(selectedProjectId, selectedCron.id, taskId)
          .then(() => {
            addLog({ level: 'info', message: `Unlinked task ${taskId} from cron ${selectedCron.id}` });
            refetch();
          })
          .catch((err: unknown) => {
            addLog({ level: 'error', message: `Failed to unlink task from cron: ${err}` });
          });
        closePopup();
        return;
      }

      if (input && !key.ctrl && !key.meta) {
        cronActionInputRef.current += input;
        setCronActionInput(cronActionInputRef.current);
        return;
      }

      return;
    }

    // === Cron Delete Confirmation Popup ===
    if (cronDeleteConfirmOpen) {
      if (key.escape) {
        setCronDeleteConfirmOpen(false);
        return;
      }

      if (key.return) {
        const selectedProjectId = getSelectedCronProjectId(selectedCron);
        if (!selectedCron || !selectedProjectId) {
          addLog({ level: 'warn', message: 'No cron selected' });
          setCronDeleteConfirmOpen(false);
          return;
        }
        if (!onDeleteCron) {
          addLog({ level: 'warn', message: 'Delete cron action unavailable' });
          setCronDeleteConfirmOpen(false);
          return;
        }

        onDeleteCron(selectedProjectId, selectedCron.id)
          .then(() => {
            addLog({ level: 'info', message: `Deleted cron: ${selectedCron.id}` });
            refetchCrons();
          })
          .catch((err: unknown) => {
            addLog({ level: 'error', message: `Failed to delete cron: ${err}` });
          });
        setCronDeleteConfirmOpen(false);
        return;
      }

      return;
    }

    if (cronLinkEditorOpen) {
      if (key.escape) {
        closeCronLinkEditor();
        return;
      }

      if (input === 'j' || key.downArrow) {
        const maxIndex = Math.max(0, cronLinkEditorTasks.length - 1);
        const nextIndex = Math.min(cronLinkEditorCursorRef.current + 1, maxIndex);
        cronLinkEditorCursorRef.current = nextIndex;
        setCronLinkEditorCursor(nextIndex);
        return;
      }

      if (input === 'k' || key.upArrow) {
        const prevIndex = Math.max(cronLinkEditorCursorRef.current - 1, 0);
        cronLinkEditorCursorRef.current = prevIndex;
        setCronLinkEditorCursor(prevIndex);
        return;
      }

      if (input === ' ') {
        const task = cronLinkEditorTasks[cronLinkEditorCursorRef.current];
        if (!task) return;
        const next = new Set(cronLinkEditorTaskIdsRef.current);
        if (next.has(task.id)) {
          next.delete(task.id);
        } else {
          next.add(task.id);
        }
        cronLinkEditorTaskIdsRef.current = next;
        setCronLinkEditorTaskIds(next);
        return;
      }

      if (key.return) {
        if (!selectedCron || !cronLinkEditorProjectId) {
          addLog({ level: 'warn', message: 'No cron selected' });
          closeCronLinkEditor();
          return;
        }

        if (!onSetCronLinkedTasks) {
          addLog({ level: 'warn', message: 'Replace linked tasks action unavailable' });
          closeCronLinkEditor();
          return;
        }

        const taskIds = cronLinkEditorTasks
          .filter((task) => cronLinkEditorTaskIdsRef.current.has(task.id))
          .map((task) => task.id);

        onSetCronLinkedTasks(cronLinkEditorProjectId, selectedCron.id, taskIds)
          .then((result: { count: number }) => {
            addLog({
              level: 'info',
              message: `Replaced linked tasks for ${selectedCron.id}: ${result.count} linked`,
            });
            refetch();
          })
          .catch((err: unknown) => {
            addLog({ level: 'error', message: `Failed to replace linked tasks: ${err}` });
          });

        closeCronLinkEditor();
        return;
      }

      return;
    }

    // === Settings Popup Mode ===
    if (showSettingsPopup) {
      // Escape to close popup
      if (key.escape) {
        if (settingsSection === 'runtime' && runtimeModelEditMode) {
          setRuntimeModelEditMode(false);
          setRuntimeModelEditBuffer(runtimeDefaultModelState);
          return;
        }
        setShowSettingsPopup(false);
        setRuntimeModelEditMode(false);
        return;
      }

      // Tab to switch between sections
      if (key.tab) {
        setSettingsSection(prev => {
          if (prev === 'limits') return 'groups';
          if (prev === 'groups') return 'runtime';
          return 'limits';
        });
        setSettingsSelectedIndex(0);
        setRuntimeModelEditMode(false);
        return;
      }

      // Section-specific handling
      if (settingsSection === 'limits') {
        // Use React state for navigation bounds (synced when popup opened)
        const currentLimits = projectLimitsState;

        // Navigate project list with j/k or arrows
        if (key.upArrow || input === 'k') {
          setSettingsSelectedIndex((prev) => Math.max(0, prev - 1));
          return;
        }

        if (key.downArrow || input === 'j') {
          setSettingsSelectedIndex((prev) => Math.min(currentLimits.length - 1, prev + 1));
          return;
        }

        // + or = to increase limit
        if ((input === '+' || input === '=') && setProjectLimit && currentLimits.length > 0) {
          const entry = currentLimits[settingsSelectedIndex];
          if (entry) {
            const newLimit = (entry.limit ?? 0) + 1;
            setProjectLimit(entry.projectId, newLimit);
            // Update React state to trigger re-render
            setProjectLimitsState(prev => prev.map((e, i) => 
              i === settingsSelectedIndex ? { ...e, limit: newLimit } : e
            ));
            addLog({
              level: 'info',
              message: `Set ${entry.projectId} limit: ${newLimit}`,
            });
          }
          return;
        }

        // - to decrease limit (min 1, or remove limit if at 1)
        if (input === '-' && setProjectLimit && currentLimits.length > 0) {
          const entry = currentLimits[settingsSelectedIndex];
          if (entry && entry.limit !== undefined) {
            if (entry.limit <= 1) {
              setProjectLimit(entry.projectId, undefined);
              // Update React state to trigger re-render
              setProjectLimitsState(prev => prev.map((e, i) => 
                i === settingsSelectedIndex ? { ...e, limit: undefined } : e
              ));
              addLog({
                level: 'info',
                message: `Removed ${entry.projectId} limit`,
              });
            } else {
              const newLimit = entry.limit - 1;
              setProjectLimit(entry.projectId, newLimit);
              // Update React state to trigger re-render
              setProjectLimitsState(prev => prev.map((e, i) => 
                i === settingsSelectedIndex ? { ...e, limit: newLimit } : e
              ));
              addLog({
                level: 'info',
                message: `Set ${entry.projectId} limit: ${newLimit}`,
              });
            }
          }
          return;
        }

        // 0 to remove limit entirely
        if (input === '0' && setProjectLimit && currentLimits.length > 0) {
          const entry = currentLimits[settingsSelectedIndex];
          if (entry) {
            setProjectLimit(entry.projectId, undefined);
            // Update React state to trigger re-render
            setProjectLimitsState(prev => prev.map((e, i) => 
              i === settingsSelectedIndex ? { ...e, limit: undefined } : e
            ));
            addLog({
              level: 'info',
              message: `Removed ${entry.projectId} limit`,
            });
          }
          return;
        }
      } else if (settingsSection === 'groups') {
        // Groups section
        const currentGroups = groupVisibilityState;

        // Navigate group list with j/k or arrows
        if (key.upArrow || input === 'k') {
          setSettingsSelectedIndex((prev) => Math.max(0, prev - 1));
          return;
        }

        if (key.downArrow || input === 'j') {
          setSettingsSelectedIndex((prev) => Math.min(currentGroups.length - 1, prev + 1));
          return;
        }

        // Space to toggle visibility
        if (input === ' ' && currentGroups.length > 0) {
          const entry = currentGroups[settingsSelectedIndex];
          if (entry) {
            const newVisible = !entry.visible;
            // Update React state for popup
            setGroupVisibilityState(prev => prev.map((e, i) => 
              i === settingsSelectedIndex ? { ...e, visible: newVisible } : e
            ));
            // Update actual visible groups state
            setVisibleGroups(prev => {
              const next = new Set(prev);
              if (newVisible) {
                next.add(entry.status);
              } else {
                next.delete(entry.status);
              }
              return next;
            });
            addLog({
              level: 'info',
              message: `${entry.label} group: ${newVisible ? 'visible' : 'hidden'}`,
            });
          }
          return;
        }

        // 'c' to toggle collapse state
        if (input === 'c' && currentGroups.length > 0) {
          const entry = currentGroups[settingsSelectedIndex];
          if (entry && entry.visible) {
            const newCollapsed = !entry.collapsed;
            // Update React state for popup
            setGroupVisibilityState(prev => prev.map((e, i) => 
              i === settingsSelectedIndex ? { ...e, collapsed: newCollapsed } : e
            ));
            // Update actual collapsed state
            setGroupCollapsed(prev => ({
              ...prev,
              [entry.status]: newCollapsed,
            }));
            // Also update legacy collapsed states for Draft and Completed
            if (entry.status === 'draft') {
              setDraftCollapsed(newCollapsed);
            } else if (entry.status === 'completed') {
              setCompletedCollapsed(newCollapsed);
            }
            addLog({
              level: 'info',
              message: `${entry.label} group: ${newCollapsed ? 'collapsed' : 'expanded'}`,
            });
          }
          return;
        }
      } else {
        // Runtime section (default model override)
        if (runtimeModelEditMode) {
          // Save edited model
          if (key.return) {
            const trimmed = runtimeModelEditBuffer.trim();
            const nextModel = trimmed.length > 0 ? trimmed : undefined;
            if (setRuntimeDefaultModel) {
              setRuntimeDefaultModel(nextModel);
            }
            setRuntimeDefaultModelState(trimmed);
            setRuntimeModelEditMode(false);
            addLog({
              level: 'info',
              message: `Runtime default model: ${trimmed.length > 0 ? trimmed : 'config default'}`,
            });
            return;
          }

          // Cancel edit but keep popup open
          if (key.escape) {
            setRuntimeModelEditMode(false);
            setRuntimeModelEditBuffer(runtimeDefaultModelState);
            return;
          }

          // Backspace/delete
          if (key.backspace || key.delete) {
            setRuntimeModelEditBuffer(prev => prev.slice(0, -1));
            return;
          }

          // Append printable characters
          if (input && input.length === 1 && !key.ctrl && !key.meta) {
            setRuntimeModelEditBuffer(prev => prev + input);
            return;
          }

          // Ignore all other keys while editing
          return;
        }

        // Enter edit mode
        if (input === 'e' || key.return) {
          setRuntimeModelEditMode(true);
          setRuntimeModelEditBuffer(runtimeDefaultModelState);
          return;
        }

        // Clear runtime override (fallback to config)
        if (input === '0') {
          if (setRuntimeDefaultModel) {
            setRuntimeDefaultModel(undefined);
          }
          setRuntimeDefaultModelState('');
          addLog({
            level: 'info',
            message: 'Runtime default model reset to config default',
          });
          return;
        }
      }

      // Block all other input when popup is open
      return;
    }

    // === Filter Mode Handling (intercepts before normal-mode shortcuts) ===
    // Must come BEFORE normal shortcuts to properly capture keys when typing in filter
    if (viewMode === 'tasks' && focusedPanel === 'tasks') {
      // Typing mode: route all input to filter, block normal navigation
      if (filterMode === 'typing') {
        // Escape: deactivate filter and clear
        if (key.escape) {
          deactivateFilter();
          return;
        }
        // Enter: lock in filter
        if (key.return) {
          lockInFilter();
          return;
        }
        // Backspace: delete last character
        if (key.backspace || key.delete) {
          handleFilterBackspace();
          return;
        }
        // Printable characters: add to filter
        if (input && input.length === 1 && !key.ctrl && !key.meta) {
          handleFilterChar(input);
          return;
        }
        // Block all other input in typing mode
        return;
      }

      // Locked mode: Esc clears filter, "/" re-enters typing, other keys pass through
      if (filterMode === 'locked') {
        if (key.escape) {
          deactivateFilter();
          return;
        }
        if (input === '/') {
          activateFilter();
          return;
        }
        // Other keys fall through to normal navigation on filtered list
      }

      // Off mode (normal): "/" activates filter
      if (filterMode === 'off' && input === '/') {
        activateFilter();
        return;
      }
    }

    // === Normal Mode ===
    // Note: Quit via Ctrl-C is handled by SIGINT handler (lines 487-500)

    // Refresh
    if (input === 'r') {
      addLog({ level: 'info', message: 'Manual refresh triggered' });
      refetch();
      refetchCrons();
      return;
    }

    // Toggle between task and cron views
    if (input === 'C') {
      setViewMode((prev) => {
        const next: ViewMode = prev === 'tasks' ? 'crons' : 'tasks';
        if (next === 'crons') {
          setDetailVisible(true);
        }
        setFocusedPanel('tasks');
        setSelectedTaskIds(new Set());
        deactivateFilter();
        return next;
      });
      return;
    }

    if (viewMode === 'crons' && focusedPanel === 'tasks') {
      const selectedProjectId = getSelectedCronProjectId(selectedCron);

      if (input === 'n') {
        if (!selectedProjectId) {
          addLog({ level: 'warn', message: 'Cannot create cron: no active project selected' });
          return;
        }
        if (!onCreateCron) {
          addLog({ level: 'warn', message: 'Create cron action unavailable' });
          return;
        }
        setCronActionMode('create');
        setCronActionInput('');
        setCronActionOpen(true);
        cronActionModeRef.current = 'create';
        cronActionInputRef.current = '';
        cronActionOpenRef.current = true;
        return;
      }

      if (!selectedCron) {
        if (input === 'e' || input === 'x' || input === 'p' || input === 'D' || input === 'a' || input === 'u' || input === 'R') {
          addLog({ level: 'warn', message: 'No cron selected' });
          return;
        }
      } else if (!selectedProjectId) {
        if (input === 'e' || input === 'x' || input === 'p' || input === 'D' || input === 'a' || input === 'u' || input === 'R') {
          addLog({ level: 'warn', message: 'No project context for selected cron' });
          return;
        }
      } else {
        if (input === 'e') {
          if (!onUpdateCron) {
            addLog({ level: 'warn', message: 'Update cron action unavailable' });
            return;
          }
          setCronActionMode('edit');
          setCronActionInput('');
          setCronActionOpen(true);
          cronActionModeRef.current = 'edit';
          cronActionInputRef.current = '';
          cronActionOpenRef.current = true;
          return;
        }

        if (input === 'x') {
          if (!onTriggerCron) {
            addLog({ level: 'warn', message: 'Trigger cron action unavailable' });
            return;
          }
          onTriggerCron(selectedProjectId, selectedCron.id)
            .then((result) => {
              addLog({ level: 'info', message: `Triggered cron ${selectedCron.id}: ${result.run.run_id}` });
              refetchCrons();
            })
            .catch((err: unknown) => {
              addLog({ level: 'error', message: `Failed to trigger cron: ${err}` });
            });
          return;
        }

        if (input === 'p') {
          if (!onUpdateCron) {
            addLog({ level: 'warn', message: 'Update cron action unavailable' });
            return;
          }

          const nextStatus = selectedCron.status === 'active' ? 'blocked' : 'active';
          onUpdateCron(selectedProjectId, selectedCron.id, { status: nextStatus })
            .then(() => {
              addLog({
                level: 'info',
                message: `${nextStatus === 'active' ? 'Enabled' : 'Paused'} cron ${selectedCron.id}`,
              });
              refetchCrons();
            })
            .catch((err: unknown) => {
              addLog({ level: 'error', message: `Failed to toggle cron status: ${err}` });
            });
          return;
        }

        if (input === 'a' || input === 'u' || input === 'R') {
          const linkedIds = tasks
            .filter((task) => taskBelongsToProject(task, selectedProjectId))
            .filter((task) => task.cron_ids?.includes(selectedCron.id))
            .map((task) => task.id);

          const initialLinked = new Set(linkedIds);
          setCronLinkEditorProjectId(selectedProjectId);
          setCronLinkEditorTaskIds(initialLinked);
          setCronLinkEditorCursor(0);
          cronLinkEditorTaskIdsRef.current = initialLinked;
          cronLinkEditorCursorRef.current = 0;
          setCronLinkEditorOpen(true);
          return;
        }

        if (input === 'D') {
          if (!onDeleteCron) {
            addLog({ level: 'warn', message: 'Delete cron action unavailable' });
            return;
          }
          setCronDeleteConfirmOpen(true);
          return;
        }
      }
    }

    // f key: Mark selected feature header for checkout task generation
    if (input === 'f' && viewMode === 'tasks' && focusedPanel === 'tasks') {
      const checkoutOptions = resolveFeatureCheckoutOptionsForSelection({
        selectedTaskId,
        isMultiProject,
        activeProject,
        project: config.project,
        tasks,
      });

      const handled = triggerFeatureCheckoutFromSelection({
        selectedTaskId,
        isMultiProject,
        activeProject,
        project: config.project,
        onMarkFeatureForCheckout,
        checkoutOptions,
        addLog,
      });
      if (handled) {
        return;
      }
    }

    // x key: Execute feature immediately or execute single task
    // - On feature header: enable feature AND immediately execute ready tasks
    // - On ungrouped header: enable ungrouped AND immediately execute ready tasks
    // - On task row: execute the task immediately
    if (input === 'x' && viewMode === 'tasks') {
      // Case 1: Feature header selected - toggle enable/disable
      if (selectedTaskId?.startsWith(FEATURE_HEADER_PREFIX)) {
        const featureId = selectedTaskId.replace(FEATURE_HEADER_PREFIX, '');
        
        // Toggle: if already active, disable; if not active, enable and execute
        if (activeFeatures.has(featureId)) {
          // Disable the feature (remove from whitelist)
          setActiveFeatures(prev => {
            const next = new Set(prev);
            next.delete(featureId);
            return next;
          });
          if (onDisableFeature) {
            onDisableFeature(featureId);
          }
          addLog({
            level: 'info',
            message: `Deactivated feature: ${featureId}`,
          });
        } else {
          // Enable the feature (add to whitelist so tasks can run even when paused)
          setActiveFeatures(prev => {
            const next = new Set(prev);
            next.add(featureId);
            return next;
          });
          if (onEnableFeature) {
            onEnableFeature(featureId);
          }
          
          // Execute ready tasks for this feature immediately
          if (onExecuteFeature) {
            addLog({
              level: 'info',
              message: `Executing feature: ${featureId}`,
            });
            onExecuteFeature(featureId).then((tasksStarted) => {
              if (tasksStarted > 0) {
                addLog({
                  level: 'info',
                  message: `Started ${tasksStarted} task(s) for feature: ${featureId}`,
                });
              } else {
                addLog({
                  level: 'warn',
                  message: `No ready tasks to execute for feature: ${featureId}`,
                });
              }
              refetch(); // Refresh to show updated status
            }).catch((err) => {
              addLog({
                level: 'error',
                message: `Failed to execute feature: ${err}`,
              });
            });
          } else {
            addLog({
              level: 'info',
              message: `Activated feature: ${featureId}`,
            });
          }
        }
        return;
      }
      
      // Case 2: Ungrouped header selected - toggle enable/disable
      if (selectedTaskId === UNGROUPED_HEADER_ID) {
        // Toggle: if already active, disable; if not active, enable and execute
        if (activeFeatures.has(UNGROUPED_FEATURE_ID)) {
          // Disable ungrouped (remove from whitelist)
          setActiveFeatures(prev => {
            const next = new Set(prev);
            next.delete(UNGROUPED_FEATURE_ID);
            return next;
          });
          if (onDisableFeature) {
            onDisableFeature(UNGROUPED_FEATURE_ID);
          }
          addLog({
            level: 'info',
            message: 'Deactivated ungrouped tasks',
          });
        } else {
          // Enable ungrouped (add to whitelist so tasks can run even when paused)
          setActiveFeatures(prev => {
            const next = new Set(prev);
            next.add(UNGROUPED_FEATURE_ID);
            return next;
          });
          if (onEnableFeature) {
            onEnableFeature(UNGROUPED_FEATURE_ID);
          }
          
          // Execute ready tasks for ungrouped immediately
          if (onExecuteFeature) {
            addLog({
              level: 'info',
              message: 'Executing ungrouped tasks',
            });
            onExecuteFeature(UNGROUPED_FEATURE_ID).then((tasksStarted) => {
              if (tasksStarted > 0) {
                addLog({
                  level: 'info',
                  message: `Started ${tasksStarted} ungrouped task(s)`,
                });
              } else {
                addLog({
                  level: 'warn',
                  message: 'No ready ungrouped tasks to execute',
                });
              }
              refetch(); // Refresh to show updated status
            }).catch((err) => {
              addLog({
                level: 'error',
                message: `Failed to execute ungrouped tasks: ${err}`,
              });
            });
          } else {
            addLog({
              level: 'info',
              message: 'Activated ungrouped tasks',
            });
          }
        }
        return;
      }
      
      // Case 2b: Status-group feature header (e.g. completed/draft/cancelled section)
      // Allow toggling off activeFeatures from these sections
      const statusGroupPrefixes = [
        COMPLETED_FEATURE_PREFIX,
        DRAFT_FEATURE_PREFIX,
        CANCELLED_FEATURE_PREFIX,
        SUPERSEDED_FEATURE_PREFIX,
        ARCHIVED_FEATURE_PREFIX,
      ];
      for (const prefix of statusGroupPrefixes) {
        if (selectedTaskId?.startsWith(prefix)) {
          const featureId = selectedTaskId.replace(prefix, '');
          
          if (activeFeatures.has(featureId)) {
            // Disable the feature (remove from whitelist)
            setActiveFeatures(prev => {
              const next = new Set(prev);
              next.delete(featureId);
              return next;
            });
            if (onDisableFeature) {
              onDisableFeature(featureId);
            }
            addLog({
              level: 'info',
              message: `Deactivated feature: ${featureId}`,
            });
          } else {
            addLog({
              level: 'info',
              message: `Feature not active: ${featureId} (nothing to toggle)`,
            });
          }
          return;
        }
      }
      
      // Case 3: Task selected - execute or resume
      if (selectedTask && onExecuteTask) {
        const isResume = selectedTask.status === 'in_progress';
        addLog({
          level: 'info',
          message: `${isResume ? 'Resuming' : 'Executing'} task: ${selectedTask.title}`,
          taskId: selectedTask.id,
        });
        onExecuteTask(selectedTask.id, selectedTask.path).then((success) => {
          if (success) {
            addLog({
              level: 'info',
              message: `Task ${isResume ? 'resume' : 'execution'} started: ${selectedTask.title}`,
              taskId: selectedTask.id,
            });
          } else {
            addLog({
              level: 'warn',
              message: `Failed to ${isResume ? 'resume' : 'execute'} task: ${selectedTask.title} (at capacity or not found)`,
              taskId: selectedTask.id,
            });
          }
          refetch(); // Refresh to show updated status
        }).catch((err) => {
          addLog({
            level: 'error',
            message: `Failed to execute task: ${err}`,
            taskId: selectedTask.id,
          });
        });
        return;
      }
      return;
    }

    // Cancel selected task (X uppercase) - only works on in_progress tasks
    if (input === 'X' && viewMode === 'tasks' && selectedTask && onCancelTask) {
      // Guard: only cancel tasks that are currently running (in_progress)
      if (selectedTask.status !== 'in_progress') {
        addLog({
          level: 'warn',
          message: `Cannot cancel task: ${selectedTask.title} (status: ${selectedTask.status}, must be in_progress)`,
          taskId: selectedTask.id,
        });
        return;
      }
      addLog({
        level: 'warn',
        message: `Cancelling task: ${selectedTask.title}`,
        taskId: selectedTask.id,
      });
      onCancelTask(selectedTask.id, selectedTask.path).then(() => {
        refetch(); // Refresh to show updated status
      }).catch((err) => {
        addLog({
          level: 'error',
          message: `Failed to cancel task: ${err}`,
          taskId: selectedTask.id,
        });
      });
      return;
    }

    // Edit selected task in external editor
    if (input === 'e' && viewMode === 'tasks' && selectedTask && focusedPanel === 'tasks') {
      editTaskInEditor(selectedTask.id, selectedTask.path);
      return;
    }

    // Open session for selected task
    if (input === 'o' && viewMode === 'tasks' && selectedTask && focusedPanel === 'tasks') {
      const sessionIds = Object.keys(selectedTask.sessions || {});
      if (sessionIds.length === 0) {
        addLog({
          level: 'warn',
          message: `No sessions available for: ${selectedTask.title}`,
          taskId: selectedTask.id,
        });
        return;
      }
      if (sessionIds.length === 1) {
        // Single session - open directly
        if (onOpenSession) {
          addLog({
            level: 'info',
            message: `Opening session: ${sessionIds[0]}`,
            taskId: selectedTask.id,
          });
          onOpenSession(sessionIds[0]).catch((err: unknown) => {
            addLog({
              level: 'error',
              message: `Failed to open session: ${err}`,
              taskId: selectedTask.id,
            });
          });
        }
      } else {
        // Multiple sessions - show popup (latest first, so index 0 is selected by default)
        setSessionPopupIds(sessionIds);
        setSessionPopupSelectedIndex(0);
        setSessionPopupOpen(true);
      }
      return;
    }

    // Open session in tmux window for selected task
    if (input === 'O' && viewMode === 'tasks' && selectedTask && focusedPanel === 'tasks') {
      const sessionIds = Object.keys(selectedTask.sessions || {});
      if (sessionIds.length === 0) {
        addLog({
          level: 'warn',
          message: `No sessions available for: ${selectedTask.title}`,
          taskId: selectedTask.id,
        });
        return;
      }
      if (sessionIds.length === 1) {
        // Single session - open directly in tmux window
        if (onOpenSessionTmux) {
          const taskCtx: OpenSessionTaskContext = {
            taskId: selectedTask.id,
            path: selectedTask.path,
            title: selectedTask.title,
            priority: selectedTask.priority,
            projectId: selectedTask.projectId || 'unknown',
            workdir: selectedTask.resolvedWorkdir || selectedTask.workdir || process.cwd(),
          };
          addLog({
            level: 'info',
            message: `Opening session in tmux window: ${sessionIds[0]}`,
            taskId: selectedTask.id,
          });
          onOpenSessionTmux(sessionIds[0], taskCtx).catch((err: unknown) => {
            addLog({
              level: 'error',
              message: `Failed to open session in tmux window: ${err}`,
              taskId: selectedTask.id,
            });
          });
        }
      } else {
        // Multiple sessions - show popup in tmux mode (latest first, so index 0 is selected by default)
        setSessionPopupTmuxMode(true);
        setSessionPopupIds(sessionIds);
        setSessionPopupSelectedIndex(0);
        setSessionPopupOpen(true);
      }
      return;
    }

    // Yank (copy) selected task name to clipboard
    if (input === 'y' && viewMode === 'tasks' && selectedTask && focusedPanel === 'tasks') {
      const success = copyToClipboard(selectedTask.title);
      if (success) {
        addLog({
          level: 'info',
          message: `Copied to clipboard: ${selectedTask.title}`,
          taskId: selectedTask.id,
        });
      } else {
        addLog({
          level: 'warn',
          message: 'Failed to copy to clipboard (clipboard tool not available)',
          taskId: selectedTask.id,
        });
      }
      return;
    }

    // Toggle help
    if (input === '?') {
      setShowHelp(!showHelp);
      return;
    }

    // Tab to switch focus between panels: tasks → details → logs → tasks (skip hidden panels)
    if (key.tab) {
      setFocusedPanel((prev) => nextPanel(prev, logsVisible, detailVisible));
      return;
    }

    // Toggle logs panel visibility
    if (input === 'L') {
      setLogsVisible(prev => {
        const newVisible = !prev;
        // If hiding logs and logs are focused, switch focus to tasks
        if (!newVisible && focusedPanel === 'logs') {
          setFocusedPanel('tasks');
        }
        return newVisible;
      });
      return;
    }

    // Toggle detail panel visibility
    if (input === 'T') {
      setDetailVisible(prev => {
        const newVisible = !prev;
        // If hiding detail and detail is focused, switch focus to tasks
        if (!newVisible && focusedPanel === 'details') {
          setFocusedPanel('tasks');
        }
        return newVisible;
      });
      return;
    }

    // Toggle text wrap/truncate mode for task titles
    if (input === 'w') {
      setTextWrap(prev => !prev);
      return;
    }

    // Open metadata popup for selected task, feature header, or batch (s key) - only when focused on tasks panel
    if (input === 's' && viewMode === 'tasks' && focusedPanel === 'tasks') {
      // Helper to open popup with common setup
      const openMetadataPopup = async (
        mode: MetadataPopupMode,
        targetTasks: TaskDisplay[],
        prefill: MetadataPrefillValues,
      ) => {
        // Fetch all projects from API for the project picker
        let fetchedProjects = projects; // fallback to monitored projects
        if (onListProjects) {
          try {
            fetchedProjects = await onListProjects();
          } catch {
            // Fallback to monitored projects on error
            fetchedProjects = projects;
          }
        }
        setAllProjects(fetchedProjects);
        
        setMetadataTargetTasks(targetTasks);
        setMetadataPopupMode(mode);
        setMetadataFocusedField(mode === 'feature' ? 'execution_mode' : 'status');
        setMetadataStatusValue(prefill.status);
        setMetadataStatusIndex(ENTRY_STATUSES.indexOf(prefill.status));
        setMetadataFeatureIdValue(prefill.feature_id);
        setMetadataBranchValue(prefill.git_branch);
        setMetadataMergeTargetBranchValue(prefill.merge_target_branch);
        setMetadataExecutionModeValue(prefill.execution_mode);
        setMetadataCheckoutEnabledValue(prefill.checkout_enabled);
        setMetadataMergePolicyValue(prefill.merge_policy);
        setMetadataMergeStrategyValue(prefill.merge_strategy);
        setMetadataRemoteBranchPolicyValue(prefill.remote_branch_policy);
        setMetadataOpenPrBeforeMergeValue(prefill.open_pr_before_merge);
        setMetadataWorkdirValue(prefill.target_workdir);
        setMetadataScheduleValue(prefill.schedule);
        setMetadataProjectValue(prefill.project);
        setMetadataProjectIndex(fetchedProjects.indexOf(prefill.project) >= 0 ? fetchedProjects.indexOf(prefill.project) : 0);
        setMetadataAgentValue(prefill.agent);
        setMetadataModelValue(prefill.model);
        setMetadataDirectPromptValue(prefill.direct_prompt);
        setMetadataOriginalValues({
          status: prefill.status,
          feature_id: prefill.feature_id,
          git_branch: prefill.git_branch,
          merge_target_branch: prefill.merge_target_branch,
          execution_mode: prefill.execution_mode,
           checkout_enabled: prefill.checkout_enabled,
           merge_policy: prefill.merge_policy,
           merge_strategy: prefill.merge_strategy,
           remote_branch_policy: prefill.remote_branch_policy,
           open_pr_before_merge: prefill.open_pr_before_merge,
          target_workdir: prefill.target_workdir,
          schedule: prefill.schedule,
          project: prefill.project,
          agent: prefill.agent,
          model: prefill.model,
          direct_prompt: prefill.direct_prompt,
        });
        setMetadataInteractionMode('navigate');
        setMetadataEditBuffer('');
        setMetadataDirty(false);
        setShowMetadataPopup(true);
      };

      // Case 1: Multi-select active - batch mode
      if (selectedTaskIds.size > 0) {
        // Resolve all selected task IDs to TaskDisplay objects
        const batchTasks = tasks.filter(t => selectedTaskIds.has(t.id));
        if (batchTasks.length > 0) {
          const prefill = buildMetadataPrefillFromTasks(batchTasks, 'batch');
          openMetadataPopup(
            'batch',
            batchTasks,
            prefill,
          );
        }
        return;
      }
      
      // Case 2: Ungrouped header selected - feature mode for ungrouped tasks
      // Only include active tasks (exclude group statuses: draft, completed, etc.)
      if (selectedTaskId === UNGROUPED_HEADER_ID) {
        const ungroupedTasks = getVisibleTasksForUngrouped(tasks);
        if (ungroupedTasks.length > 0) {
          const prefill = buildMetadataPrefillFromTasks(ungroupedTasks, 'feature', '');
          openMetadataPopup(
            'feature',
            ungroupedTasks,
            prefill,
          );
        }
        return;
      }
      
      // Case 3: Feature header selected - feature mode
      // Only include active tasks (exclude group statuses: draft, completed, etc.)
      if (selectedTaskId?.startsWith(FEATURE_HEADER_PREFIX)) {
        const featureId = selectedTaskId.replace(FEATURE_HEADER_PREFIX, '');
        const featureTasks = getVisibleTasksForFeature(tasks, featureId);
        if (featureTasks.length > 0) {
          const prefill = buildMetadataPrefillFromTasks(featureTasks, 'feature', featureId);
          openMetadataPopup(
            'feature',
            featureTasks,
            prefill,
          );
        }
        return;
      }
      
      // Case 3b: Status-group feature header selected (e.g. __completed_feature__auth-system)
      // Open metadata popup targeting only tasks in that feature within that status group
      for (const [prefix, groupStatuses] of Object.entries(STATUS_GROUP_MAP)) {
        if (selectedTaskId?.startsWith(prefix)) {
          const featureId = selectedTaskId.replace(prefix, '');
          const featureTasks = getTasksForStatusGroupFeature(tasks, featureId, groupStatuses);
          if (featureTasks.length > 0) {
            const prefill = buildMetadataPrefillFromTasks(featureTasks, 'feature', featureId);
            openMetadataPopup(
              'feature',
              featureTasks,
              prefill,
            );
          }
          return;
        }
      }
      
      // Case 4: Single task selected - single mode
      if (selectedTask) {
        const prefill = buildMetadataPrefillFromTasks([selectedTask], 'single', selectedTask.feature_id || '');
        openMetadataPopup(
          'single',
          [selectedTask],
          prefill,
        );
      }
      return;
    }

    // Open settings popup (S key - shift+s)
    if (input === 'S') {
      // Clear multi-select when opening popup
      setSelectedTaskIds(new Set());
      
      setSettingsSelectedIndex(0);
      setSettingsSection('limits');
      setRuntimeModelEditMode(false);
      // Sync React state with external limits when opening popup
      if (getProjectLimits) {
        setProjectLimitsState(getProjectLimits());
      }
      const runtimeModel = getRuntimeDefaultModel?.() ?? '';
      setRuntimeDefaultModelState(runtimeModel);
      setRuntimeModelEditBuffer(runtimeModel);
      // Compute group visibility entries from current tasks
      const statusCounts: Record<string, number> = {};
      for (const task of tasks) {
        statusCounts[task.status] = (statusCounts[task.status] || 0) + 1;
      }
      const groupEntries: GroupVisibilityEntry[] = ENTRY_STATUSES.map(status => ({
        status,
        label: STATUS_LABELS[status] || status,
        visible: visibleGroups.has(status),
        collapsed: groupCollapsed[status] ?? true,
        taskCount: statusCounts[status] || 0,
      }));
      setGroupVisibilityState(groupEntries);
      setShowSettingsPopup(true);
      return;
    }

    // Project tab navigation (only in multi-project mode)
    if (isMultiProject) {
      // Previous tab: [ or h
      if (input === '[' || input === 'h') {
        const currentIndex = allProjectTabs.indexOf(activeProject);
        if (currentIndex > 0) {
          setActiveProject(allProjectTabs[currentIndex - 1]);
          setSelectedTaskIds(new Set()); // Clear multi-select on tab switch
          addLog({ level: 'info', message: `Switched to: ${allProjectTabs[currentIndex - 1]}` });
        }
        return;
      }

      // Next tab: ] or l
      if (input === ']' || input === 'l') {
        const currentIndex = allProjectTabs.indexOf(activeProject);
        if (currentIndex < allProjectTabs.length - 1) {
          setActiveProject(allProjectTabs[currentIndex + 1]);
          setSelectedTaskIds(new Set()); // Clear multi-select on tab switch
          addLog({ level: 'info', message: `Switched to: ${allProjectTabs[currentIndex + 1]}` });
        }
        return;
      }

      // Number keys 1-9 for direct tab access
      if (input >= '1' && input <= '9') {
        const index = parseInt(input, 10) - 1;
        if (index < allProjectTabs.length) {
          setActiveProject(allProjectTabs[index]);
          setSelectedTaskIds(new Set()); // Clear multi-select on tab switch
          addLog({ level: 'info', message: `Switched to: ${allProjectTabs[index]}` });
        }
        return;
      }
    }

    // Pause/Resume handling - toggle pause for selected project header or active project
    // Skip in cron view where 'p' toggles cron status
    if (input === 'p' && viewMode !== 'crons') {
      // Check if a project header is selected
      let targetProject: string;
      if (selectedTaskId?.startsWith(PROJECT_HEADER_PREFIX)) {
        // Extract project ID from the selected project header
        targetProject = selectedTaskId.replace(PROJECT_HEADER_PREFIX, '');
      } else {
        // Default to active project (current tab)
        targetProject = activeProject === 'all' 
          ? (isMultiProject ? projects[0] : projects[0]) 
          : activeProject;
      }

      // Toggle project pause
      const isPaused = pausedProjects.has(targetProject);
      if (isPaused && onResume) {
        addLog({ level: 'info', message: `Resuming ${targetProject}...` });
        setPausedProjects(prev => {
          const next = new Set(prev);
          next.delete(targetProject);
          return next;
        });
        Promise.resolve(onResume(targetProject)).catch((err: unknown) => {
          addLog({ level: 'error', message: `Failed to resume ${targetProject}: ${err}` });
        });
      } else if (!isPaused && onPause) {
        addLog({ level: 'info', message: `Pausing ${targetProject}...` });
        setPausedProjects(prev => new Set([...prev, targetProject]));
        Promise.resolve(onPause(targetProject)).catch((err: unknown) => {
          addLog({ level: 'error', message: `Failed to pause ${targetProject}: ${err}` });
        });
      }
      return;
    }

    // 'P' (shift+p) - toggle pause for ALL projects (only in multi-project mode)
    if (input === 'P' && isMultiProject) {
      const allPaused = pausedProjects.size === projects.length;
      if (allPaused && onResumeAll) {
        addLog({ level: 'info', message: 'Resuming all projects...' });
        // Optimistic update: clear all paused projects
        setPausedProjects(new Set());
        Promise.resolve(onResumeAll()).catch((err) => {
          addLog({ level: 'error', message: `Failed to resume all: ${err}` });
        });
      } else if (!allPaused && onPauseAll) {
        addLog({ level: 'info', message: 'Pausing all projects...' });
        // Optimistic update: mark all projects as paused
        setPausedProjects(new Set(projects));
        Promise.resolve(onPauseAll()).catch((err) => {
          addLog({ level: 'error', message: `Failed to pause all: ${err}` });
        });
      }
      return;
    }

    // Navigation (only when focused on tasks panel)
    if (focusedPanel === 'tasks') {
      if (viewMode === 'crons') {
        const currentIndex = crons.findIndex((cron) => cron.id === selectedCronId);

        if (key.upArrow || input === 'k') {
          if (currentIndex > 0) {
            setSelectedCronId(crons[currentIndex - 1]?.id ?? selectedCronId);
          } else if (currentIndex === -1 && crons.length > 0) {
            setSelectedCronId(crons[crons.length - 1]?.id ?? null);
          }
          return;
        }

        if (key.downArrow || input === 'j') {
          if (currentIndex === -1 && crons.length > 0) {
            setSelectedCronId(crons[0]?.id ?? null);
          } else if (currentIndex < crons.length - 1) {
            setSelectedCronId(crons[currentIndex + 1]?.id ?? selectedCronId);
          }
          return;
        }

        if ((input === 'g' || key.home) && crons.length > 0) {
          setSelectedCronId(crons[0]?.id ?? null);
          setCronScrollOffset(0);
          return;
        }

        if ((input === 'G' || key.end) && crons.length > 0) {
          const lastIndex = crons.length - 1;
          setSelectedCronId(crons[lastIndex]?.id ?? null);
          setCronScrollOffset(Math.max(0, crons.length - cronViewportHeight));
          return;
        }

        if (key.return) {
          setDetailVisible(true);
          return;
        }

        return;
      }

      // Use navigationOrder (flattened tree) instead of raw tasks array
      // This ensures j/k navigation matches the visual tree order
      const currentIndex = navigationOrder.indexOf(selectedTaskId || '');
      const selectedRowTarget = selectedTaskId ? parseTaskTreeRowTarget(selectedTaskId) : null;
      
      // Helper to find next navigable item (skip spacers)
      const findNextNavigable = (startIndex: number, direction: 1 | -1): string | null => {
        let idx = startIndex + direction;
        while (idx >= 0 && idx < navigationOrder.length) {
          const id = navigationOrder[idx];
          // Skip spacer elements - they're for visual alignment only
          if (!id.startsWith(SPACER_PREFIX)) {
            return id;
          }
          idx += direction;
        }
        return null;
      };

      // Up arrow or k (vim-style)
      if (key.upArrow || input === 'k') {
        if (currentIndex > 0) {
          const nextId = findNextNavigable(currentIndex, -1);
          if (nextId) setSelectedTaskId(nextId);
        } else if (currentIndex === -1 && navigationOrder.length > 0) {
          // No selection - go to last navigable item
          const lastId = findNextNavigable(navigationOrder.length, -1);
          if (lastId) setSelectedTaskId(lastId);
        }
        return;
      }

      // Down arrow or j (vim-style)
      if (key.downArrow || input === 'j') {
        if (currentIndex === -1 && navigationOrder.length > 0) {
          // No selection - go to first navigable item
          const firstId = findNextNavigable(-1, 1);
          if (firstId) setSelectedTaskId(firstId);
        } else if (currentIndex < navigationOrder.length - 1) {
          const nextId = findNextNavigable(currentIndex, 1);
          if (nextId) setSelectedTaskId(nextId);
        }
        return;
      }

      // g - Jump to top of task list
      if (input === 'g' && navigationOrder.length > 0) {
        // Find first navigable item (skip spacers)
        const firstId = findNextNavigable(-1, 1);
        if (firstId) {
          setSelectedTaskId(firstId);
          setTaskScrollOffset(0);
        }
        return;
      }

      // G - Jump to bottom of task list
      if (input === 'G' && navigationOrder.length > 0) {
        // Find last navigable item (skip spacers)
        const lastId = findNextNavigable(navigationOrder.length, -1);
        if (lastId) {
          setSelectedTaskId(lastId);
          // Scroll to show last item
          const maxOffset = Math.max(0, navigationOrder.length - taskViewportHeight);
          setTaskScrollOffset(maxOffset);
        }
        return;
      }

      // Enter to toggle group section when header is selected
      if (key.return) {
        if (selectedRowTarget && resolveTaskTreeClickAction(selectedRowTarget, 'left') === 'toggle_collapsed') {
          if (toggleCollapsedForTarget(selectedRowTarget)) {
            return;
          }
        }

        // Toggle dynamic group section if group header is selected
        if (selectedTaskId?.startsWith(GROUP_HEADER_PREFIX)) {
          const status = selectedTaskId.replace(GROUP_HEADER_PREFIX, '');
          setGroupCollapsed(prev => ({
            ...prev,
            [status]: !prev[status],
          }));
          // Also sync legacy collapsed states for Draft and Completed
          if (status === 'draft') {
            setDraftCollapsed(prev => !prev);
          } else if (status === 'completed') {
            setCompletedCollapsed(prev => !prev);
          }
          return;
        }
        // Enter on a regular task: open in editor (same as 'e')
        if (
          selectedTask &&
          selectedRowTarget &&
          resolveTaskTreeClickAction(selectedRowTarget, 'left') === 'open_editor' &&
          focusedPanel === 'tasks'
        ) {
          editTaskInEditor(selectedTask.id, selectedTask.path);
          return;
        }
        return;
      }

      // Space key: Toggle multi-select for the currently cursor'd task
      if (input === ' ') {
        // Only toggle selection for actual tasks, not headers or spacers
        if (
          selectedTaskId &&
          !selectedTaskId.startsWith(FEATURE_HEADER_PREFIX) &&
          !selectedTaskId.startsWith(COMPLETED_FEATURE_PREFIX) &&
          !selectedTaskId.startsWith(DRAFT_FEATURE_PREFIX) &&
          !selectedTaskId.startsWith(CANCELLED_FEATURE_PREFIX) &&
          !selectedTaskId.startsWith(SUPERSEDED_FEATURE_PREFIX) &&
          !selectedTaskId.startsWith(ARCHIVED_FEATURE_PREFIX) &&
          !selectedTaskId.startsWith(GROUP_HEADER_PREFIX) &&
          !selectedTaskId.startsWith(SPACER_PREFIX) &&
          selectedTaskId !== COMPLETED_HEADER_ID &&
          selectedTaskId !== DRAFT_HEADER_ID &&
          selectedTaskId !== CANCELLED_HEADER_ID &&
          selectedTaskId !== SUPERSEDED_HEADER_ID &&
          selectedTaskId !== ARCHIVED_HEADER_ID &&
          selectedTaskId !== UNGROUPED_HEADER_ID &&
          navigationOrder.includes(selectedTaskId)
        ) {
          setSelectedTaskIds(prev => {
            const next = new Set(prev);
            if (next.has(selectedTaskId)) {
              next.delete(selectedTaskId);
            } else {
              next.add(selectedTaskId);
            }
            return next;
          });
        }
        return;
      }

      // Backspace key: Delete selected tasks or tasks in selected feature
      if (key.backspace || key.delete) {
        // Case 1: Multi-select active - delete all selected tasks
        if (selectedTaskIds.size > 0) {
          const tasksInfo = tasks
            .filter(t => selectedTaskIds.has(t.id))
            .map(t => ({ id: t.id, title: t.title, path: t.path }));
          
          if (tasksInfo.length > 0) {
            setTasksToDelete(tasksInfo);
            setDeletePopupOpen(true);
          }
          return;
        }
        
        // Case 2: Feature header selected - delete all tasks in feature
        if (selectedTaskId?.startsWith(FEATURE_HEADER_PREFIX)) {
          const featureId = selectedTaskId.replace(FEATURE_HEADER_PREFIX, '');
          const featureTasks = tasks
            .filter(t => t.feature_id === featureId)
            .map(t => ({ id: t.id, title: t.title, path: t.path }));
          
          if (featureTasks.length > 0) {
            setTasksToDelete(featureTasks);
            setDeletePopupOpen(true);
          }
          return;
        }
        
        // Case 3: Ungrouped header selected - delete all ungrouped tasks
        if (selectedTaskId === UNGROUPED_HEADER_ID) {
          const ungroupedTasks = tasks
            .filter(t => !t.feature_id)
            .map(t => ({ id: t.id, title: t.title, path: t.path }));
          
          if (ungroupedTasks.length > 0) {
            setTasksToDelete(ungroupedTasks);
            setDeletePopupOpen(true);
          }
          return;
        }
        
        // Case 4: Single task selected (cursor on task row) - delete that task
        if (selectedTask) {
          setTasksToDelete([{ id: selectedTask.id, title: selectedTask.title, path: selectedTask.path }]);
          setDeletePopupOpen(true);
          return;
        }
        
        return;
      }
    }
    
    // Log scrolling (when focused on logs panel)
    if (focusedPanel === 'logs') {
      // k or up arrow: scroll up (increase offset to see older logs)
      if (key.upArrow || input === 'k') {
        setLogScrollOffset(prev => Math.min(prev + 1, Math.max(0, logs.length - logMaxLines)));
        return;
      }
      
      // j or down arrow: scroll down (decrease offset to see newer logs)
      if (key.downArrow || input === 'j') {
        setLogScrollOffset(prev => Math.max(0, prev - 1));
        return;
      }
      
      // G or End: jump to bottom (latest logs)
      if (input === 'G' || key.end) {
        setLogScrollOffset(0);
        return;
      }
      
      // g or Home: jump to top (oldest logs)
      if (input === 'g' || key.home) {
        setLogScrollOffset(Math.max(0, logs.length - logMaxLines));
        return;
      }
      
      // f: toggle log filtering by selected task
      if (input === 'f') {
        setFilterLogsByTask(prev => !prev);
        setLogScrollOffset(0); // Reset scroll when toggling filter
        return;
      }
    }
    
    // Details scrolling (when focused on details panel)
    if (focusedPanel === 'details') {
      // k or up arrow: scroll up (decrease offset to see content above)
      if (key.upArrow || input === 'k') {
        setDetailsScrollOffset(prev => Math.max(0, prev - 1));
        return;
      }
      
      // j or down arrow: scroll down (increase offset to see content below)
      if (key.downArrow || input === 'j') {
        // Note: max offset will be enforced by TaskDetail component
        setDetailsScrollOffset(prev => prev + 1);
        return;
      }
      
      // g or Home: jump to top
      if (input === 'g' || key.home) {
        setDetailsScrollOffset(0);
        return;
      }
      
      // G or End: jump to bottom (let TaskDetail handle max offset)
      if (input === 'G' || key.end) {
        setDetailsScrollOffset(999); // Large number, TaskDetail will clamp
        return;
      }
    }
  });

  // Log errors when they occur
  useEffect(() => {
    if (error) {
      addLog({ level: 'error', message: error.message });
    }
  }, [error, addLog]);

  useEffect(() => {
    if (cronError) {
      addLog({ level: 'error', message: cronError.message });
    }
  }, [cronError, addLog]);

  // Handle SIGINT/SIGTERM for graceful exit
  useEffect(() => {
    const handleExit = () => {
      exit();
    };

    process.on('SIGINT', handleExit);
    process.on('SIGTERM', handleExit);

    return () => {
      process.off('SIGINT', handleExit);
      process.off('SIGTERM', handleExit);
    };
  }, [exit]);

  // Help overlay
  if (showHelp) {
    return (
      <Box flexDirection="column" padding={1}>
        <Text bold>Keyboard Shortcuts</Text>
        <Text />
        <Text bold dimColor>Global shortcuts:</Text>
        <Text>  <Text bold>Ctrl-C</Text>    - Quit</Text>
        <Text>  <Text bold>r</Text>         - Refresh tasks and crons</Text>
        <Text>  <Text bold>p</Text>         - Toggle pause/resume</Text>
        {isMultiProject && (
          <Text>  <Text bold>P</Text>         - Pause/Resume ALL projects</Text>
        )}
        <Text>  <Text bold>?</Text>         - Toggle help</Text>
        <Text>  <Text bold>Tab</Text>       - Switch focus (list/details/logs)</Text>
        <Text>  <Text bold>C</Text>         - Toggle task/cron view</Text>
        <Text>  <Text bold>L</Text>         - Toggle logs panel visibility</Text>
        <Text>  <Text bold>Up/k</Text>      - Navigate up</Text>
        <Text>  <Text bold>Down/j</Text>    - Navigate down</Text>
        <Text>  <Text bold>S</Text>         - Open settings</Text>
        <Text />
        <Text bold dimColor>Task panel shortcuts (task view):</Text>
        <Text>  <Text bold>e</Text>         - Edit selected task in $EDITOR</Text>
        <Text>  <Text bold>f</Text>         - Mark selected feature header for checkout</Text>
        <Text>  <Text bold>x</Text>         - Execute selected task/feature</Text>
        <Text>  <Text bold>X</Text>         - Cancel running selected task</Text>
        <Text>  <Text bold>s</Text>         - Edit task metadata / feature settings</Text>
        <Text />
        <Text bold dimColor>Cron panel shortcuts (cron view):</Text>
        <Text>  <Text bold>Enter</Text>     - Show cron details panel</Text>
        <Text>  <Text bold>n/e</Text>       - New/Edit selected cron</Text>
        <Text>  <Text bold>x</Text>         - Trigger selected cron now</Text>
        <Text>  <Text bold>p</Text>         - Pause/enable selected cron</Text>
        <Text>  <Text bold>a/u/R</Text>     - Edit linked tasks in editor</Text>
        <Text>  <Text bold>D</Text>         - Delete selected cron (confirm)</Text>
        <Text />
        <Text bold dimColor>Logs Panel (when focused):</Text>
        <Text>  <Text bold>f</Text>         - Filter logs by selected task</Text>
        {isMultiProject && (
          <>
            <Text />
            <Text bold dimColor>Multi-project Navigation:</Text>
            <Text>  <Text bold>[</Text>         - Previous project tab</Text>
            <Text>  <Text bold>]</Text>         - Next project tab</Text>
            <Text>  <Text bold>1-9</Text>       - Jump to project tab 1-9</Text>
          </>
        )}
        <Text />
        <Text dimColor>Press ? to close</Text>
      </Box>
    );
  }

  // Metadata popup overlay
  if (showMetadataPopup && metadataTargetTasks.length > 0) {
    return (
      <Box flexDirection="column" width="100%" height={terminalRows} alignItems="center" justifyContent="center">
        <MetadataPopup
          width={Math.min(80, terminalColumns - 4)}
          mode={metadataPopupMode}
          taskTitle={metadataPopupMode === 'single' ? metadataTargetTasks[0]?.title : undefined}
          batchCount={metadataTargetTasks.length}
          featureId={metadataPopupMode === 'feature' ? metadataFeatureIdValue || 'Ungrouped' : undefined}
          focusedField={metadataFocusedField}
          statusValue={metadataStatusValue}
          featureIdValue={metadataFeatureIdValue}
          branchValue={metadataBranchValue}
          mergeTargetBranchValue={metadataMergeTargetBranchValue}
          executionModeValue={metadataExecutionModeValue}
          checkoutEnabledValue={metadataCheckoutEnabledValue}
          mergePolicyValue={metadataMergePolicyValue}
          mergeStrategyValue={metadataMergeStrategyValue}
          remoteBranchPolicyValue={metadataRemoteBranchPolicyValue}
          openPrBeforeMergeValue={metadataOpenPrBeforeMergeValue}
          workdirValue={metadataWorkdirValue}
          scheduleValue={metadataScheduleValue}
          projectValue={metadataProjectValue}
          agentValue={metadataAgentValue}
          modelValue={metadataModelValue}
          directPromptValue={metadataDirectPromptValue}
          availableProjects={effectiveProjects}
          selectedProjectIndex={metadataProjectIndex}
          selectedStatusIndex={metadataStatusIndex}
          allowedStatuses={ENTRY_STATUSES}
          interactionMode={metadataInteractionMode}
          editBuffer={metadataEditBuffer}
          cronIds={metadataTargetTasks[0]?.cron_ids}
          cronNames={cronNames}
        />
      </Box>
    );
  }

  // Settings popup overlay
  if (showSettingsPopup) {
    return (
      <Box flexDirection="column" width="100%" height={terminalRows} alignItems="center" justifyContent="center">
        <SettingsPopup
          projects={projectLimitsState}
          selectedIndex={settingsSelectedIndex}
          section={settingsSection}
          groups={groupVisibilityState}
          runtimeDefaultModel={runtimeDefaultModelState}
          runtimeEditMode={runtimeModelEditMode}
          runtimeEditBuffer={runtimeModelEditBuffer}
        />
      </Box>
    );
  }

  if (cronActionOpen) {
    const title = cronActionMode === 'create'
      ? 'Create Cron'
      : cronActionMode === 'edit'
        ? 'Edit Cron'
        : cronActionMode === 'add-link'
          ? 'Add Linked Task'
          : cronActionMode === 'remove-link'
            ? 'Remove Linked Task'
            : 'Replace Linked Tasks';

    const hint = cronActionMode === 'create' || cronActionMode === 'edit'
      ? 'Use format: title|schedule'
      : cronActionMode === 'replace-links'
        ? 'Use comma-separated task IDs'
        : 'Enter task ID';

    return (
      <Box flexDirection="column" width="100%" height={terminalRows} alignItems="center" justifyContent="center">
        <Box borderStyle="single" borderColor="cyan" padding={1} flexDirection="column" width={Math.min(90, terminalColumns - 4)}>
          <Text bold>{title}</Text>
          <Text dimColor>{hint}</Text>
          <Text>{cronActionInput || '_'}</Text>
          <Text dimColor>Enter to submit, Esc to cancel</Text>
        </Box>
      </Box>
    );
  }

  if (cronDeleteConfirmOpen) {
    return (
      <Box flexDirection="column" width="100%" height={terminalRows} alignItems="center" justifyContent="center">
        <Box borderStyle="single" borderColor="red" padding={1} flexDirection="column" width={Math.min(90, terminalColumns - 4)}>
          <Text bold color="red">Delete Cron</Text>
          <Text>Delete selected cron <Text bold>{selectedCron?.title ?? '(none)'}</Text>?</Text>
          <Text dimColor>Enter to confirm, Esc to cancel</Text>
        </Box>
      </Box>
    );
  }

  if (cronLinkEditorOpen && selectedCron && cronLinkEditorProjectId) {
    return (
      <Box flexDirection="column" width="100%" height={terminalRows} alignItems="center" justifyContent="center">
        <CronLinkEditor
          cron={selectedCron}
          projectId={cronLinkEditorProjectId}
          tasks={cronLinkEditorTasks}
          linkedTaskIds={cronLinkEditorTaskIds}
          selectedIndex={cronLinkEditorCursor}
        />
      </Box>
    );
  }

  // Delete confirmation popup overlay
  if (deletePopupOpen && tasksToDelete.length > 0) {
    // Determine featureId if all tasks share the same feature
    const featureIds = new Set(tasksToDelete.map(t => {
      const task = tasks.find(task => task.id === t.id);
      return task?.feature_id;
    }));
    const sharedFeatureId = featureIds.size === 1 ? [...featureIds][0] : undefined;

    return (
      <Box flexDirection="column" width="100%" height={terminalRows} alignItems="center" justifyContent="center">
        <DeleteConfirmPopup
          taskTitles={tasksToDelete.map(t => t.title)}
          featureId={sharedFeatureId}
        />
      </Box>
    );
  }

  // Session select popup overlay
  if (sessionPopupOpen && sessionPopupIds.length > 0) {
    return (
      <Box flexDirection="column" width="100%" height={terminalRows} alignItems="center" justifyContent="center">
        <SessionSelectPopup
          sessionIds={sessionPopupIds}
          selectedIndex={sessionPopupSelectedIndex}
        />
      </Box>
    );
  }

  return (
    <Box flexDirection="column" width="100%" height={terminalRows}>
      {/* Status bar at top */}
      <StatusBar
        projectId={isMultiProject && activeProject === 'all' ? `${projects.length} projects` : (activeProject || config.project || 'brain-runner')}
        projects={isMultiProject ? projects : undefined}
        activeProject={isMultiProject ? (activeProject || 'all') : undefined}
        onSelectProject={isMultiProject ? setActiveProject : undefined}
        stats={stats}
        statsByProject={isMultiProject ? multiProjectSse.statsByProject : undefined}
        isConnected={isConnected}
        pausedProjects={pausedProjects}
        enabledFeatures={enabledFeatures}
        getRunningProcessCount={getRunningProcessCount}
        resourceMetrics={resourceMetrics}
        activeFeatures={activeFeatures}
      />

      {/* Main content area: Top row (List) | Bottom area (Details + Logs) */}
      <Box flexGrow={1} flexDirection="column">
        {/* Top row: Task tree or cron list (always full width) */}
        <Box height={topRowHeight} flexDirection="row">
          <Box
            width="100%"
            borderStyle="single"
            borderColor={focusedPanel === 'tasks' ? 'cyan' : 'gray'}
            flexDirection="column"
          >
            {viewMode === 'tasks' ? (
              <>
                <FilterBar
                  filterText={filterText}
                  filterMode={filterMode}
                  matchCount={matchCount}
                  totalCount={totalCount}
                />
                <TaskTree
                  tasks={filteredTasks}
                  selectedId={selectedTaskId}
                  hoveredId={null}
                  onSelect={setSelectedTaskId}
                  completedCollapsed={completedCollapsed}
                  onToggleCompleted={handleToggleCompleted}
                  draftCollapsed={draftCollapsed}
                  onToggleDraft={handleToggleDraft}
                  cancelledCollapsed={cancelledCollapsed}
                  supersededCollapsed={supersededCollapsed}
                  archivedCollapsed={archivedCollapsed}
                  groupByProject={isMultiProject && activeProject === 'all'}
                  groupByFeature={true}
                  scrollOffset={taskScrollOffset}
                  viewportHeight={taskViewportHeight}
                  collapsedFeatures={collapsedFeatures}
                  collapsedProjects={collapsedProjects}
                  activeFeatures={activeFeatures}
                  selectedTaskIds={selectedTaskIds}
                  visibleGroups={visibleGroups}
                  textWrap={textWrap}
                  panelWidth={taskPanelWidth}
                  onVisibleRows={setTaskTreeVisibleRows}
                />
              </>
            ) : (
              <CronList
                crons={crons}
                selectedId={selectedCronId}
                isFocused={focusedPanel === 'tasks'}
                scrollOffset={cronScrollOffset}
                viewportHeight={cronViewportHeight}
                showProjectPrefix={isMultiProject && activeProject === 'all'}
              />
            )}
          </Box>
        </Box>

        {/* Bottom area: Detail + Logs (stacked vertically, hidden by default) */}
        {anyBottomVisible && (
          <Box flexDirection="column" height={bottomAreaHeight}>
            {/* Detail panel (toggle with T) */}
            {detailVisible && (
              <Box height={detailHeight} flexDirection="column">
                {viewMode === 'tasks' ? (
                  <TaskDetail
                    task={previewTask}
                    isFocused={focusedPanel === 'details'}
                    scrollOffset={detailsScrollOffset}
                    viewportHeight={detailsViewportHeight}
                  />
                ) : (
                  <CronDetail
                    cron={selectedCron}
                    isFocused={focusedPanel === 'details'}
                  />
                )}
              </Box>
            )}

            {/* Logs (toggle with L) */}
            {logsVisible && (
              <Box flexGrow={1}>
                <LogViewer 
                  logs={logs} 
                  maxLines={logMaxLines} 
                  showProjectPrefix={isMultiProject}
                  isFocused={focusedPanel === 'logs'}
                  scrollOffset={logScrollOffset}
                  filterByTaskId={viewMode === 'tasks' ? selectedTaskId : null}
                  isFiltering={filterLogsByTask}
                />
              </Box>
            )}
          </Box>
        )}
      </Box>

      {/* Help bar at bottom */}
      <HelpBar 
        focusedPanel={focusedPanel} 
        viewMode={viewMode}
        isMultiProject={isMultiProject}
        isFilterActive={viewMode === 'tasks' && filterMode === 'locked'}
        hasSelectedTasks={selectedTaskIds.size > 0}
        hasTaskSessions={viewMode === 'tasks' && !!selectedTask?.sessions && Object.keys(selectedTask.sessions).length > 0}
        textWrap={textWrap}
      />
    </Box>
  );
}

export default App;

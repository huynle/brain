/**
 * Root TUI component for the Brain Task Runner dashboard
 *
 * Layout:
 * ┌─ project-name ──────────────────────────────────────────────────────────┐
 * │  ● 2 ready   ○ 3 waiting   ▶ 1 active   ✓ 2 done                        │
 * ├──────────────────────────┬───────────────────────────────────────────────┤
 * │ Tasks                    │ Task Details                                  │
 * │ ──────────────────────── │ ───────────────────────────────────────────── │
 * │ ● Setup base config      │ Title: Setup base config                      │
 * │ └─○ Create utils module  │ Status: pending (ready)                       │
 * │   └─○ Create main entry  │ Priority: high                                │
 * ├──────────────────────────┴───────────────────────────────────────────────┤
 * │ Logs                                                                     │
 * │ ───────────────────────────────────────────────────────────────────────  │
 * │ 17:30:45 INFO  Runner started                                            │
 * │ 17:30:46 INFO  Task started...                                           │
 * │ 17:30:47 DEBUG Polling...                                                │
 * ├──────────────────────────────────────────────────────────────────────────┤
 * │ ↑↓/j/k Navigate  Enter: Details  Tab: Switch  r: Refresh  q: Quit       │
 * └──────────────────────────────────────────────────────────────────────────┘
 */

import React, { useState, useEffect, useMemo, useCallback, useRef } from 'react';
import { join } from 'path';
import { spawnSync } from 'child_process';
import { writeFileSync, readFileSync, unlinkSync, mkdtempSync } from 'fs';
import { tmpdir } from 'os';
import { copyToClipboard } from '../system-utils';

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
import { TaskTree, flattenFeatureOrder, COMPLETED_HEADER_ID, DRAFT_HEADER_ID, GROUP_HEADER_PREFIX, SPACER_PREFIX, FEATURE_HEADER_PREFIX, COMPLETED_FEATURE_PREFIX, DRAFT_FEATURE_PREFIX, UNGROUPED_HEADER_ID, UNGROUPED_FEATURE_ID } from './components/TaskTree';
import { LogViewer } from './components/LogViewer';
import { TaskDetail } from './components/TaskDetail';
import { HelpBar } from './components/HelpBar';
import { MetadataPopup, type MetadataField, type MetadataPopupMode, type MetadataInteractionMode } from './components/MetadataPopup';
import { SettingsPopup } from './components/SettingsPopup';

import { ENTRY_STATUSES, type EntryStatus } from '../../core/types';
import { useTaskPoller } from './hooks/useTaskPoller';
import { useMultiProjectPoller } from './hooks/useMultiProjectPoller';
import { useLogStream } from './hooks/useLogStream';
import { useTerminalSize } from './hooks/useTerminalSize';
import { useResourceMetrics } from './hooks/useResourceMetrics';
import { useSettingsStorage } from './hooks/useSettingsStorage';
import { useTaskFilter } from './hooks/useTaskFilter';
import { FilterBar } from './components/FilterBar';
import type { AppProps, TaskDisplay, ProjectLimitEntry, GroupVisibilityEntry, SettingsSection } from './types';
import type { TaskStats } from './hooks/useTaskPoller';

type FocusedPanel = 'tasks' | 'details' | 'logs';

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
  getRunningProcessCount,
  getResourceMetrics,
  getProjectLimits,
  setProjectLimit,
  onEnableFeature,
  onDisableFeature,
  getEnabledFeatures,
  onUpdateMetadata,
  onMoveTask,
  onListProjects,
}: AppProps): React.ReactElement {
  const { exit } = useApp();

  // Determine if multi-project mode - memoize to avoid re-creating array on every render
  const projects = useMemo(
    () => config.projects ?? [config.project],
    [config.projects, config.project]
  );
  const isMultiProject = projects.length > 1;

  // State
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);
  const [focusedPanel, setFocusedPanel] = useState<FocusedPanel>('tasks');
  const [showHelp, setShowHelp] = useState(false);
  // MetadataPopup state
  const [showMetadataPopup, setShowMetadataPopup] = useState(false);
  const [metadataPopupMode, setMetadataPopupMode] = useState<MetadataPopupMode>('single');
  const [metadataFocusedField, setMetadataFocusedField] = useState<MetadataField>('status');
  const [metadataStatusValue, setMetadataStatusValue] = useState<EntryStatus>('pending');
  const [metadataFeatureIdValue, setMetadataFeatureIdValue] = useState('');
  const [metadataBranchValue, setMetadataBranchValue] = useState('');
  const [metadataWorkdirValue, setMetadataWorkdirValue] = useState('');
  const [metadataStatusIndex, setMetadataStatusIndex] = useState(0);
  const [metadataProjectValue, setMetadataProjectValue] = useState('');
  const [metadataProjectIndex, setMetadataProjectIndex] = useState(0);
  // 3-mode state machine: navigate (j/k fields), edit_text (typing), edit_status (j/k status), edit_project (j/k project)
  const [metadataInteractionMode, setMetadataInteractionMode] = useState<MetadataInteractionMode>('navigate');
  const [metadataEditBuffer, setMetadataEditBuffer] = useState('');
  const [metadataTargetTasks, setMetadataTargetTasks] = useState<TaskDisplay[]>([]);
  // Original values for comparison (only send changed fields)
  const [metadataOriginalValues, setMetadataOriginalValues] = useState<{
    status: EntryStatus;
    feature_id: string;
    git_branch: string;
    target_workdir: string;
    project: string;
  }>({ status: 'pending', feature_id: '', git_branch: '', target_workdir: '', project: '' });
  // All projects from API (for project picker in metadata popup)
  const [allProjects, setAllProjects] = useState<string[]>([]);
  // Computed: the effective project list used by the project picker (same as availableProjects prop)
  const effectiveProjects = allProjects.length > 0 ? allProjects : projects;
  const [showSettingsPopup, setShowSettingsPopup] = useState(false);
  const [settingsSelectedIndex, setSettingsSelectedIndex] = useState(0);
  const [projectLimitsState, setProjectLimitsState] = useState<ProjectLimitEntry[]>([]);
  const [completedCollapsed, setCompletedCollapsed] = useState(true);
  const [draftCollapsed, setDraftCollapsed] = useState(true);
  const [collapsedFeatures, setCollapsedFeatures] = useState<Set<string>>(new Set());
  
  // Group visibility settings - persisted to ~/.brain/tui-settings.json
  // Default: show draft, pending, active, in_progress, blocked, completed
  const {
    visibleGroups,
    groupCollapsed,
    setVisibleGroups,
    setGroupCollapsed,
  } = useSettingsStorage();
  // Settings popup section
  const [settingsSection, setSettingsSection] = useState<SettingsSection>('limits');
  // Group visibility state for settings popup
  const [groupVisibilityState, setGroupVisibilityState] = useState<GroupVisibilityEntry[]>([]);
  const [activeProject, setActiveProject] = useState<string>(config.activeProject ?? (isMultiProject ? 'all' : projects[0]));
  const [pausedProjects, setPausedProjects] = useState<Set<string>>(new Set());
  const [enabledFeatures, setEnabledFeatures] = useState<Set<string>>(new Set());
  const [logScrollOffset, setLogScrollOffset] = useState(0);
  const [detailsScrollOffset, setDetailsScrollOffset] = useState(0);
  const [filterLogsByTask, setFilterLogsByTask] = useState(false);
  const [isEditing, setIsEditing] = useState(false);

  const [taskScrollOffset, setTaskScrollOffset] = useState(0);
  const [logsVisible, setLogsVisible] = useState(false);
  const [detailVisible, setDetailVisible] = useState(false);
  // Active features: features queued for execution (controlled by x key on feature headers)
  const [activeFeatures, setActiveFeatures] = useState<Set<string>>(new Set());
  // Multi-select state: tasks selected via Space key for batch operations
  const [selectedTaskIds, setSelectedTaskIds] = useState<Set<string>>(new Set());

  // Get stdin control for suspending during editor session
  const { setRawMode } = useStdin();

  // Single-project poller (used when not in multi-project mode)
  const singleProjectPoller = useTaskPoller({
    projectId: config.project,
    apiUrl: config.apiUrl,
    pollInterval: config.pollInterval,
    enabled: !isMultiProject,
  });

  // Multi-project poller (used when in multi-project mode)
  const multiProjectPoller = useMultiProjectPoller({
    projects: projects,
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

  if (isMultiProject) {
    // Multi-project mode: filter tasks by activeProject
    if (activeProject === 'all') {
      tasks = multiProjectPoller.allTasks;
    } else {
      tasks = multiProjectPoller.tasksByProject.get(activeProject) ?? [];
    }
    stats = activeProject === 'all' 
      ? multiProjectPoller.aggregateStats 
      : (multiProjectPoller.statsByProject.get(activeProject) ?? {
          total: 0, ready: 0, waiting: 0, blocked: 0, inProgress: 0, completed: 0,
        });
    isLoading = multiProjectPoller.isLoading;
    isConnected = multiProjectPoller.isConnected;
    error = multiProjectPoller.error;
    refetch = multiProjectPoller.refetch;
  } else {
    // Single-project mode
    tasks = singleProjectPoller.tasks;
    stats = singleProjectPoller.stats;
    isLoading = singleProjectPoller.isLoading;
    isConnected = singleProjectPoller.isConnected;
    error = singleProjectPoller.error;
    refetch = singleProjectPoller.refetch;
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

  // Calculate dynamic maxLines for log viewer based on terminal height
  // New layout: logs at bottom take ~40% of available height (when visible)
  // Account for: StatusBar (3 lines) + HelpBar (1 line) + borders (4 lines for top row, 2 for bottom)
  const topRowHeight = logsVisible 
    ? Math.floor((terminalRows - 6) * 0.6) // ~60% for top row when logs visible
    : terminalRows - 6; // Full height when logs hidden
  const logMaxLines = Math.max(5, terminalRows - 6 - topRowHeight - 2); // remaining height minus log panel chrome
  
  // Calculate task viewport height for scrolling
  // Account for: TaskTree header (2 lines: title + margin) + border (2 lines) + padding (2 lines)
  const taskViewportHeight = Math.max(3, topRowHeight - 6);
  
  // Calculate details viewport height for scrolling
  // Account for: header (1 line) + border (2 lines) + padding (2 lines) + scroll indicators (2 lines)
  const detailsViewportHeight = Math.max(3, topRowHeight - 7);
  
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
    const allTasksForPause = isMultiProject ? multiProjectPoller.allTasks : tasks;
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
  }, [tasks, isMultiProject, multiProjectPoller.allTasks, projects, getPausedProjects]);

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

  // Stable callbacks for toggling section collapsed states (avoids new ref on every render)
  const handleToggleCompleted = useCallback(() => {
    setCompletedCollapsed(prev => !prev);
  }, []);

  const handleToggleDraft = useCallback(() => {
    setDraftCollapsed(prev => !prev);
  }, []);

  // Find selected task
  const selectedTask = tasks.find((t) => t.id === selectedTaskId) || null;

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

  // Reset details scroll offset when selected task changes
  useEffect(() => {
    setDetailsScrollOffset(0);
  }, [selectedTaskId]);

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
      target_workdir?: string;
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
    if (effectiveWorkdir !== metadataOriginalValues.target_workdir) {
      changedFields.target_workdir = effectiveWorkdir;
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
    metadataWorkdirValue,
    metadataOriginalValues,
    addLog,
    refetch,
  ]);

  // Handle keyboard input
  useInput((input, key) => {
    // === Metadata Popup Mode (3-mode state machine) ===
    // NAVIGATE: j/k moves between fields, Enter enters edit mode, Esc closes popup
    // EDIT_TEXT: typing updates buffer, Enter saves field immediately, Esc discards
    // EDIT_STATUS: j/k cycles status options, Enter saves immediately, Esc discards
    // EDIT_PROJECT: j/k cycles project options, Enter moves task, Esc discards
    if (showMetadataPopup) {
      const METADATA_FIELDS: MetadataField[] = ['status', 'feature_id', 'git_branch', 'target_workdir', 'project'];
      
      // Helper: save a single field immediately to API
      const saveField = (field: MetadataField, value: string | EntryStatus) => {
        if (!onUpdateMetadata || metadataTargetTasks.length === 0) return;
        
        const updates: { [key: string]: string | EntryStatus } = { [field]: value };
        
        Promise.all(
          metadataTargetTasks.map(task => onUpdateMetadata(task.path, updates))
        ).then(() => {
          addLog({ level: 'info', message: `Updated ${field}: ${value}` });
          // Update original values to reflect the saved state
          setMetadataOriginalValues(prev => ({ ...prev, [field]: value }));
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
          // In navigate mode: close popup entirely (no save)
          setShowMetadataPopup(false);
          setMetadataInteractionMode('navigate');
          setMetadataEditBuffer('');
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
              case 'target_workdir':
                currentValue = metadataWorkdirValue;
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
          const value = metadataEditBuffer;
          // Update local state
          switch (metadataFocusedField) {
            case 'feature_id':
              setMetadataFeatureIdValue(value);
              break;
            case 'git_branch':
              setMetadataBranchValue(value);
              break;
            case 'target_workdir':
              setMetadataWorkdirValue(value);
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

    // === Settings Popup Mode ===
    if (showSettingsPopup) {
      // Escape to close popup
      if (key.escape) {
        setShowSettingsPopup(false);
        return;
      }

      // Tab to switch between sections
      if (key.tab) {
        setSettingsSection(prev => prev === 'limits' ? 'groups' : 'limits');
        setSettingsSelectedIndex(0);
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
      } else {
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
      }

      // Block all other input when popup is open
      return;
    }

    // === Filter Mode Handling (intercepts before normal-mode shortcuts) ===
    // Must come BEFORE normal shortcuts to properly capture keys when typing in filter
    if (focusedPanel === 'tasks') {
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
      return;
    }

    // x key: Execute feature immediately or execute single task
    // - On feature header: enable feature AND immediately execute ready tasks
    // - On ungrouped header: enable ungrouped AND immediately execute ready tasks
    // - On task row: execute the task immediately
    if (input === 'x') {
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
      
      // Case 3: Task selected - execute immediately
      if (selectedTask && onExecuteTask) {
        addLog({
          level: 'info',
          message: `Executing task: ${selectedTask.title}`,
          taskId: selectedTask.id,
        });
        onExecuteTask(selectedTask.id, selectedTask.path).then((success) => {
          if (success) {
            addLog({
              level: 'info',
              message: `Task execution started: ${selectedTask.title}`,
              taskId: selectedTask.id,
            });
          } else {
            addLog({
              level: 'warn',
              message: `Failed to execute task: ${selectedTask.title} (at capacity or not found)`,
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
    if (input === 'X' && selectedTask && onCancelTask) {
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
    if (input === 'e' && selectedTask && focusedPanel === 'tasks') {
      editTaskInEditor(selectedTask.id, selectedTask.path);
      return;
    }

    // Yank (copy) selected task name to clipboard
    if (input === 'y' && selectedTask && focusedPanel === 'tasks') {
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

    // Open metadata popup for selected task, feature header, or batch (s key) - only when focused on tasks panel
    if (input === 's' && focusedPanel === 'tasks') {
      // Helper to open popup with common setup
      const openMetadataPopup = async (
        mode: MetadataPopupMode,
        targetTasks: TaskDisplay[],
        prefillStatus: EntryStatus = 'pending',
        prefillFeatureId: string = '',
        prefillBranch: string = '',
        prefillWorkdir: string = '',
        prefillProject: string = ''
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
        setMetadataFocusedField('status');
        setMetadataStatusValue(prefillStatus);
        setMetadataStatusIndex(ENTRY_STATUSES.indexOf(prefillStatus));
        setMetadataFeatureIdValue(prefillFeatureId);
        setMetadataBranchValue(prefillBranch);
        setMetadataWorkdirValue(prefillWorkdir);
        setMetadataProjectValue(prefillProject);
        setMetadataProjectIndex(fetchedProjects.indexOf(prefillProject) >= 0 ? fetchedProjects.indexOf(prefillProject) : 0);
        setMetadataOriginalValues({
          status: prefillStatus,
          feature_id: prefillFeatureId,
          git_branch: prefillBranch,
          target_workdir: prefillWorkdir,
          project: prefillProject,
        });
        setMetadataInteractionMode('navigate');
        setMetadataEditBuffer('');
        setShowMetadataPopup(true);
      };

      // Case 1: Multi-select active - batch mode
      if (selectedTaskIds.size > 0) {
        // Resolve all selected task IDs to TaskDisplay objects
        const batchTasks = tasks.filter(t => selectedTaskIds.has(t.id));
        if (batchTasks.length > 0) {
          // Use first task's values as defaults (or empty for truly mixed)
          const first = batchTasks[0];
          openMetadataPopup(
            'batch',
            batchTasks,
            first.status,
            first.feature_id || '',
            first.gitBranch || '',
            first.resolvedWorkdir || first.workdir || '',
            first.projectId || ''
          );
        }
        return;
      }
      
      // Case 2: Ungrouped header selected - feature mode for ungrouped tasks
      if (selectedTaskId === UNGROUPED_HEADER_ID) {
        const ungroupedTasks = tasks.filter(t => !t.feature_id);
        if (ungroupedTasks.length > 0) {
          const firstUngrouped = ungroupedTasks[0];
          openMetadataPopup('feature', ungroupedTasks, 'pending', '', '', '', firstUngrouped.projectId || '');
        }
        return;
      }
      
      // Case 3: Feature header selected - feature mode
      if (selectedTaskId?.startsWith(FEATURE_HEADER_PREFIX)) {
        const featureId = selectedTaskId.replace(FEATURE_HEADER_PREFIX, '');
        const featureTasks = tasks.filter(t => t.feature_id === featureId);
        if (featureTasks.length > 0) {
          const firstFeatureTask = featureTasks[0];
          openMetadataPopup('feature', featureTasks, 'pending', featureId, '', '', firstFeatureTask.projectId || '');
        }
        return;
      }
      
      // Case 4: Single task selected - single mode
      if (selectedTask) {
        openMetadataPopup(
          'single',
          [selectedTask],
          selectedTask.status,
          selectedTask.feature_id || '',
          selectedTask.gitBranch || '',
          selectedTask.resolvedWorkdir || selectedTask.workdir || '',
          selectedTask.projectId || ''
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
      // Sync React state with external limits when opening popup
      if (getProjectLimits) {
        setProjectLimitsState(getProjectLimits());
      }
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

    // Pause/Resume handling - directly toggle pause for active project
    if (input === 'p') {
      const targetProject = activeProject === 'all' 
        ? (isMultiProject ? projects[0] : projects[0]) 
        : activeProject;

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
      // Use navigationOrder (flattened tree) instead of raw tasks array
      // This ensures j/k navigation matches the visual tree order
      const currentIndex = navigationOrder.indexOf(selectedTaskId || '');
      
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
        // Toggle completed section if header is selected (legacy)
        if (selectedTaskId === COMPLETED_HEADER_ID) {
          setCompletedCollapsed(prev => !prev);
          return;
        }
        // Toggle draft section if header is selected (legacy)
        if (selectedTaskId === DRAFT_HEADER_ID) {
          setDraftCollapsed(prev => !prev);
          return;
        }
        // Toggle ungrouped section if ungrouped header is selected
        if (selectedTaskId === UNGROUPED_HEADER_ID) {
          setCollapsedFeatures(prev => {
            const next = new Set(prev);
            if (next.has(UNGROUPED_FEATURE_ID)) {
              next.delete(UNGROUPED_FEATURE_ID);
            } else {
              next.add(UNGROUPED_FEATURE_ID);
            }
            return next;
          });
          return;
        }
        // Toggle feature section if feature header is selected
        if (selectedTaskId?.startsWith(FEATURE_HEADER_PREFIX)) {
          const featureId = selectedTaskId.replace(FEATURE_HEADER_PREFIX, '');
          setCollapsedFeatures(prev => {
            const next = new Set(prev);
            if (next.has(featureId)) {
              next.delete(featureId);
            } else {
              next.add(featureId);
            }
            return next;
          });
          return;
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
        if (selectedTask && focusedPanel === 'tasks') {
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
          !selectedTaskId.startsWith(GROUP_HEADER_PREFIX) &&
          !selectedTaskId.startsWith(SPACER_PREFIX) &&
          selectedTaskId !== COMPLETED_HEADER_ID &&
          selectedTaskId !== DRAFT_HEADER_ID &&
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
        <Text>  <Text bold>Ctrl-C</Text>    - Quit</Text>
        <Text>  <Text bold>r</Text>         - Refresh tasks</Text>
        <Text>  <Text bold>e</Text>         - Edit selected task in $EDITOR</Text>
        <Text>  <Text bold>f</Text>         - Focus on feature (task panel)</Text>
        <Text>  <Text bold>x</Text>         - Execute highlighted task immediately</Text>
        <Text>  <Text bold>X</Text>         - Cancel running task (kill PID)</Text>
        <Text>  <Text bold>p</Text>         - Toggle pause/resume</Text>
        {isMultiProject && (
          <Text>  <Text bold>P</Text>         - Pause/Resume ALL projects</Text>
        )}
        <Text>  <Text bold>?</Text>         - Toggle help</Text>
        <Text>  <Text bold>Tab</Text>       - Switch focus (tasks/logs)</Text>
        <Text>  <Text bold>L</Text>         - Toggle logs panel visibility</Text>
        <Text>  <Text bold>Up/k</Text>      - Navigate up</Text>
        <Text>  <Text bold>Down/j</Text>    - Navigate down</Text>
        <Text>  <Text bold>s</Text>         - Change task status</Text>
        <Text>  <Text bold>S</Text>         - Open settings</Text>
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
          mode={metadataPopupMode}
          taskTitle={metadataPopupMode === 'single' ? metadataTargetTasks[0]?.title : undefined}
          batchCount={metadataTargetTasks.length}
          featureId={metadataPopupMode === 'feature' ? metadataFeatureIdValue || 'Ungrouped' : undefined}
          focusedField={metadataFocusedField}
          statusValue={metadataStatusValue}
          featureIdValue={metadataFeatureIdValue}
          branchValue={metadataBranchValue}
          workdirValue={metadataWorkdirValue}
          projectValue={metadataProjectValue}
          availableProjects={effectiveProjects}
          selectedProjectIndex={metadataProjectIndex}
          selectedStatusIndex={metadataStatusIndex}
          allowedStatuses={ENTRY_STATUSES}
          interactionMode={metadataInteractionMode}
          editBuffer={metadataEditBuffer}
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
        statsByProject={isMultiProject ? multiProjectPoller.statsByProject : undefined}
        isConnected={isConnected}
        pausedProjects={pausedProjects}
        enabledFeatures={enabledFeatures}
        getRunningProcessCount={getRunningProcessCount}
        resourceMetrics={resourceMetrics}
        activeFeatures={activeFeatures}
      />

      {/* Main content area: Top row (Tasks + Details) | Bottom row (Logs) */}
      <Box flexGrow={1} flexDirection="column">
        {/* Top row: Task Tree (left) + Task Detail (right, when visible) */}
        <Box height={topRowHeight} flexDirection="row">
          {/* Left panel: Task Tree */}
          <Box
            width={detailVisible ? "50%" : "100%"}
            borderStyle="single"
            borderColor={focusedPanel === 'tasks' ? 'cyan' : 'gray'}
            flexDirection="column"
          >
            <FilterBar
              filterText={filterText}
              filterMode={filterMode}
              matchCount={matchCount}
              totalCount={totalCount}
            />
            <TaskTree
              tasks={filteredTasks}
              selectedId={selectedTaskId}
              onSelect={setSelectedTaskId}
              completedCollapsed={completedCollapsed}
              onToggleCompleted={handleToggleCompleted}
              draftCollapsed={draftCollapsed}
              onToggleDraft={handleToggleDraft}
              groupByProject={isMultiProject && activeProject === 'all'}
              groupByFeature={true}
              scrollOffset={taskScrollOffset}
              viewportHeight={taskViewportHeight}
              collapsedFeatures={collapsedFeatures}
              activeFeatures={activeFeatures}
              selectedTaskIds={selectedTaskIds}
            />
          </Box>

          {/* Right panel: Task Detail (conditionally rendered) */}
          {detailVisible && (
            <Box
              width="50%"
              flexDirection="column"
            >
              <TaskDetail 
                task={selectedTask} 
                isFocused={focusedPanel === 'details'}
                scrollOffset={detailsScrollOffset}
                viewportHeight={detailsViewportHeight}
              />
            </Box>
          )}
        </Box>

        {/* Bottom row: Logs (full width) - conditionally rendered */}
        {logsVisible && (
          <Box flexGrow={1}>
            <LogViewer 
              logs={logs} 
              maxLines={logMaxLines} 
              showProjectPrefix={isMultiProject}
              isFocused={focusedPanel === 'logs'}
              scrollOffset={logScrollOffset}
              filterByTaskId={selectedTaskId}
              isFiltering={filterLogsByTask}
            />
          </Box>
        )}
      </Box>

      {/* Help bar at bottom */}
      <HelpBar 
        focusedPanel={focusedPanel} 
        isMultiProject={isMultiProject}
        isFilterActive={filterMode === 'locked'}
      />
    </Box>
  );
}

export default App;

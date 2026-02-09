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
import { TaskTree, flattenFeatureOrder, COMPLETED_HEADER_ID, DRAFT_HEADER_ID, GROUP_HEADER_PREFIX, SPACER_PREFIX } from './components/TaskTree';
import { LogViewer } from './components/LogViewer';
import { TaskDetail } from './components/TaskDetail';
import { HelpBar } from './components/HelpBar';
import { StatusPopup } from './components/StatusPopup';
import { SettingsPopup } from './components/SettingsPopup';
import { ENTRY_STATUSES, type EntryStatus } from '../../core/types';
import { useTaskPoller } from './hooks/useTaskPoller';
import { useMultiProjectPoller } from './hooks/useMultiProjectPoller';
import { useLogStream } from './hooks/useLogStream';
import { useTerminalSize } from './hooks/useTerminalSize';
import { useResourceMetrics } from './hooks/useResourceMetrics';
import type { AppProps, TaskDisplay, ProjectLimitEntry, GroupVisibilityEntry, SettingsSection } from './types';
import type { TaskStats } from './hooks/useTaskPoller';

type FocusedPanel = 'tasks' | 'details' | 'logs';

/** Cycle to the next panel (for Tab navigation) */
function nextPanel(current: FocusedPanel, logsVisible: boolean): FocusedPanel {
  if (logsVisible) {
    // Full cycle: tasks -> details -> logs -> tasks
    if (current === 'tasks') return 'details';
    if (current === 'details') return 'logs';
    return 'tasks';
  } else {
    // No logs: tasks -> details -> tasks
    if (current === 'tasks') return 'details';
    return 'tasks';
  }
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
  getRunningProcessCount,
  getResourceMetrics,
  getProjectLimits,
  setProjectLimit,
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
  const [showStatusPopup, setShowStatusPopup] = useState(false);
  const [popupSelectedStatus, setPopupSelectedStatus] = useState<EntryStatus>('pending');
  const [showSettingsPopup, setShowSettingsPopup] = useState(false);
  const [settingsSelectedIndex, setSettingsSelectedIndex] = useState(0);
  const [projectLimitsState, setProjectLimitsState] = useState<ProjectLimitEntry[]>([]);
  const [completedCollapsed, setCompletedCollapsed] = useState(true);
  const [draftCollapsed, setDraftCollapsed] = useState(true);
  
  // Group visibility settings - which status groups to display
  // Default: show draft, pending, active, in_progress, blocked, completed
  const [visibleGroups, setVisibleGroups] = useState<Set<string>>(() => 
    new Set(['draft', 'pending', 'active', 'in_progress', 'blocked', 'completed'])
  );
  // Collapsed state for each group (by status)
  const [groupCollapsed, setGroupCollapsed] = useState<Record<string, boolean>>(() => ({
    draft: true,
    pending: false,
    active: false,
    in_progress: false,
    blocked: false,
    cancelled: true,
    completed: true,
    validated: true,
    superseded: true,
    archived: true,
  }));
  // Settings popup section
  const [settingsSection, setSettingsSection] = useState<SettingsSection>('limits');
  // Group visibility state for settings popup
  const [groupVisibilityState, setGroupVisibilityState] = useState<GroupVisibilityEntry[]>([]);
  const [activeProject, setActiveProject] = useState<string>(config.activeProject ?? (isMultiProject ? 'all' : projects[0]));
  const [pausedProjects, setPausedProjects] = useState<Set<string>>(new Set());
  const [logScrollOffset, setLogScrollOffset] = useState(0);
  const [detailsScrollOffset, setDetailsScrollOffset] = useState(0);
  const [filterLogsByTask, setFilterLogsByTask] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [taskScrollOffset, setTaskScrollOffset] = useState(0);
  const [logsVisible, setLogsVisible] = useState(true);

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

  // Stable callbacks for toggling section collapsed states (avoids new ref on every render)
  const handleToggleCompleted = useCallback(() => {
    setCompletedCollapsed(prev => !prev);
  }, []);

  const handleToggleDraft = useCallback(() => {
    setDraftCollapsed(prev => !prev);
  }, []);

  // Find selected task
  const selectedTask = tasks.find((t) => t.id === selectedTaskId) || null;

  // Get task IDs in visual tree order for navigation (j/k keys)
  // This ensures navigation follows the same order tasks appear on screen
  const navigationOrder = useMemo(
    () => flattenFeatureOrder(tasks, completedCollapsed, draftCollapsed), 
    [tasks, completedCollapsed, draftCollapsed]
  );

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

  // Handle keyboard input
  useInput((input, key) => {
    // === Status Popup Mode ===
    if (showStatusPopup) {
      // Escape to close popup
      if (key.escape) {
        setShowStatusPopup(false);
        return;
      }

      // Navigate status list with j/k or arrows
      if (key.upArrow || input === 'k') {
        setPopupSelectedStatus((prev) => {
          const currentIndex = ENTRY_STATUSES.indexOf(prev);
          if (currentIndex > 0) {
            return ENTRY_STATUSES[currentIndex - 1];
          }
          return prev;
        });
        return;
      }

      if (key.downArrow || input === 'j') {
        setPopupSelectedStatus((prev) => {
          const currentIndex = ENTRY_STATUSES.indexOf(prev);
          if (currentIndex < ENTRY_STATUSES.length - 1) {
            return ENTRY_STATUSES[currentIndex + 1];
          }
          return prev;
        });
        return;
      }

      // Enter to confirm status change
      if (key.return && selectedTask && onUpdateStatus) {
        const newStatus = popupSelectedStatus;
        setShowStatusPopup(false);
        
        addLog({
          level: 'info',
          message: `Updating status: ${selectedTask.title} → ${newStatus}`,
          taskId: selectedTask.id,
        });
        
        onUpdateStatus(selectedTask.id, selectedTask.path, newStatus)
          .then(() => {
            addLog({
              level: 'info',
              message: `Status updated: ${selectedTask.title} is now ${newStatus}`,
              taskId: selectedTask.id,
            });
            refetch(); // Refresh to show updated status
          })
          .catch((err) => {
            addLog({
              level: 'error',
              message: `Failed to update status: ${err}`,
              taskId: selectedTask.id,
            });
          });
        return;
      }

      // Block all other input when popup is open
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

    // === Normal Mode ===
    // Note: Quit via Ctrl-C is handled by SIGINT handler (lines 487-500)

    // Refresh
    if (input === 'r') {
      addLog({ level: 'info', message: 'Manual refresh triggered' });
      refetch();
      return;
    }

    // Execute selected task manually (x lowercase)
    if (input === 'x' && selectedTask && onExecuteTask) {
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

    // Cancel selected task (X uppercase)
    if (input === 'X' && selectedTask && onCancelTask) {
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

    // Tab to switch focus between panels: tasks → details → logs → tasks (skip logs if hidden)
    if (key.tab) {
      setFocusedPanel((prev) => {
        if (prev === 'tasks') return 'details';
        if (prev === 'details') return logsVisible ? 'logs' : 'tasks';
        return 'tasks'; // logs → tasks
      });
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

    // Open settings popup (s key)
    if (input === 's') {
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
          addLog({ level: 'info', message: `Switched to: ${allProjectTabs[currentIndex - 1]}` });
        }
        return;
      }

      // Next tab: ] or l
      if (input === ']' || input === 'l') {
        const currentIndex = allProjectTabs.indexOf(activeProject);
        if (currentIndex < allProjectTabs.length - 1) {
          setActiveProject(allProjectTabs[currentIndex + 1]);
          addLog({ level: 'info', message: `Switched to: ${allProjectTabs[currentIndex + 1]}` });
        }
        return;
      }

      // Number keys 1-9 for direct tab access
      if (input >= '1' && input <= '9') {
        const index = parseInt(input, 10) - 1;
        if (index < allProjectTabs.length) {
          setActiveProject(allProjectTabs[index]);
          addLog({ level: 'info', message: `Switched to: ${allProjectTabs[index]}` });
        }
        return;
      }
    }

    // Pause/Resume handling (async — fire-and-forget with error logging)
    // 'p' - toggle pause for current project (or all if viewing "all")
    if (input === 'p') {
      const targetProject = activeProject === 'all' 
        ? (isMultiProject ? 'all' : projects[0]) 
        : activeProject;
      
      if (targetProject === 'all') {
        // Toggle all projects
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
      } else {
        // Toggle single project
        const isPaused = pausedProjects.has(targetProject);
        if (isPaused && onResume) {
          addLog({ level: 'info', message: `Resuming ${targetProject}...` });
          // Optimistic update: remove project from paused set
          setPausedProjects(prev => {
            const next = new Set(prev);
            next.delete(targetProject);
            return next;
          });
          Promise.resolve(onResume(targetProject)).catch((err) => {
            addLog({ level: 'error', message: `Failed to resume ${targetProject}: ${err}` });
          });
        } else if (!isPaused && onPause) {
          addLog({ level: 'info', message: `Pausing ${targetProject}...` });
          // Optimistic update: add project to paused set
          setPausedProjects(prev => new Set([...prev, targetProject]));
          Promise.resolve(onPause(targetProject)).catch((err) => {
            addLog({ level: 'error', message: `Failed to pause ${targetProject}: ${err}` });
          });
        }
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

      // Enter to toggle group section when header is selected, or open status popup
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
        // Open status popup for selected task
        if (selectedTask) {
          setPopupSelectedStatus(selectedTask.status);
          setShowStatusPopup(true);
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
        <Text>  <Text bold>x</Text>         - Cancel selected task</Text>
        <Text>  <Text bold>p</Text>         - Pause/Resume current project</Text>
        {isMultiProject && (
          <Text>  <Text bold>P</Text>         - Pause/Resume ALL projects</Text>
        )}
        <Text>  <Text bold>?</Text>         - Toggle help</Text>
        <Text>  <Text bold>Tab</Text>       - Switch focus (tasks/logs)</Text>
        <Text>  <Text bold>L</Text>         - Toggle logs panel visibility</Text>
        <Text>  <Text bold>Up/k</Text>      - Navigate up</Text>
        <Text>  <Text bold>Down/j</Text>    - Navigate down</Text>
        <Text>  <Text bold>s</Text>         - Change status / Toggle completed</Text>
        <Text>  <Text bold>f</Text>         - Filter logs by selected task (in logs panel)</Text>
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

  // Status popup overlay
  if (showStatusPopup && selectedTask) {
    return (
      <Box flexDirection="column" width="100%" height={terminalRows} alignItems="center" justifyContent="center">
        <StatusPopup
          currentStatus={selectedTask.status}
          selectedStatus={popupSelectedStatus}
          taskTitle={selectedTask.title}
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
        getRunningProcessCount={getRunningProcessCount}
        resourceMetrics={resourceMetrics}
      />

      {/* Main content area: Top row (Tasks + Details) | Bottom row (Logs) */}
      <Box flexGrow={1} flexDirection="column">
        {/* Top row: Task Tree (left) + Task Detail (right) */}
        <Box height={topRowHeight} flexDirection="row">
          {/* Left panel: Task Tree */}
          <Box
            width="50%"
            borderStyle="single"
            borderColor={focusedPanel === 'tasks' ? 'cyan' : 'gray'}
            flexDirection="column"
          >
            <TaskTree
              tasks={tasks}
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
            />
          </Box>

          {/* Right panel: Task Detail */}
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
      <HelpBar focusedPanel={focusedPanel} isMultiProject={isMultiProject} />
    </Box>
  );
}

export default App;

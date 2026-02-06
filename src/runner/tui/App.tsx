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
import { TaskTree, flattenTreeOrder, COMPLETED_HEADER_ID, DRAFT_HEADER_ID } from './components/TaskTree';
import { LogViewer } from './components/LogViewer';
import { TaskDetail } from './components/TaskDetail';
import { HelpBar } from './components/HelpBar';
import { StatusPopup } from './components/StatusPopup';
import { ENTRY_STATUSES, type EntryStatus } from '../../core/types';
import { useTaskPoller } from './hooks/useTaskPoller';
import { useMultiProjectPoller } from './hooks/useMultiProjectPoller';
import { useLogStream } from './hooks/useLogStream';
import { useTerminalSize } from './hooks/useTerminalSize';
import type { AppProps, TaskDisplay } from './types';
import type { TaskStats } from './hooks/useTaskPoller';

type FocusedPanel = 'tasks' | 'logs';

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
  const [completedCollapsed, setCompletedCollapsed] = useState(true);
  const [draftCollapsed, setDraftCollapsed] = useState(true);
  const [activeProject, setActiveProject] = useState<string>(config.activeProject ?? (isMultiProject ? 'all' : projects[0]));
  const [pausedProjects, setPausedProjects] = useState<Set<string>>(new Set());
  const [logScrollOffset, setLogScrollOffset] = useState(0);
  const [filterLogsByTask, setFilterLogsByTask] = useState(false);
  const [isEditing, setIsEditing] = useState(false);
  const [taskScrollOffset, setTaskScrollOffset] = useState(0);

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

  // Calculate dynamic maxLines for log viewer based on terminal height
  // New layout: logs at bottom take ~40% of available height
  // Account for: StatusBar (3 lines) + HelpBar (1 line) + borders (4 lines for top row, 2 for bottom)
  const topRowHeight = Math.floor((terminalRows - 6) * 0.6); // ~60% for top row
  const logMaxLines = Math.max(5, terminalRows - 6 - topRowHeight - 2); // remaining height minus log panel chrome
  
  // Calculate task viewport height for scrolling
  // Account for: TaskTree header (2 lines: title + margin) + border (2 lines) + padding (2 lines)
  const taskViewportHeight = Math.max(3, topRowHeight - 6);
  
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
  const navigationOrder = useMemo(() => flattenTreeOrder(tasks, completedCollapsed, draftCollapsed), [tasks, completedCollapsed, draftCollapsed]);

  // Auto-scroll task list to keep selected task in view
  useEffect(() => {
    if (!selectedTaskId || taskViewportHeight <= 0) return;
    
    const selectedIndex = navigationOrder.indexOf(selectedTaskId);
    if (selectedIndex === -1) return;
    
    // Ensure selected task is visible in viewport
    if (selectedIndex < taskScrollOffset) {
      // Selected is above viewport - scroll up
      setTaskScrollOffset(selectedIndex);
    } else if (selectedIndex >= taskScrollOffset + taskViewportHeight) {
      // Selected is below viewport - scroll down
      setTaskScrollOffset(selectedIndex - taskViewportHeight + 1);
    }
  }, [selectedTaskId, navigationOrder, taskScrollOffset, taskViewportHeight]);

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

    // === Normal Mode ===
    // Note: Quit via Ctrl-C is handled by SIGINT handler (lines 487-500)

    // Refresh
    if (input === 'r') {
      addLog({ level: 'info', message: 'Manual refresh triggered' });
      refetch();
      return;
    }

    // Cancel selected task
    if (input === 'x' && selectedTask && onCancelTask) {
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

    // Toggle help
    if (input === '?') {
      setShowHelp(!showHelp);
      return;
    }

    // Tab to switch focus between panels
    if (key.tab) {
      setFocusedPanel((prev) => (prev === 'tasks' ? 'logs' : 'tasks'));
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
          Promise.resolve(onResumeAll()).catch((err) => {
            addLog({ level: 'error', message: `Failed to resume all: ${err}` });
          });
          addLog({ level: 'info', message: 'All projects resumed' });
        } else if (!allPaused && onPauseAll) {
          Promise.resolve(onPauseAll()).catch((err) => {
            addLog({ level: 'error', message: `Failed to pause all: ${err}` });
          });
          addLog({ level: 'warn', message: 'All projects paused' });
        }
      } else {
        // Toggle single project
        const isPaused = pausedProjects.has(targetProject);
        if (isPaused && onResume) {
          Promise.resolve(onResume(targetProject)).catch((err) => {
            addLog({ level: 'error', message: `Failed to resume ${targetProject}: ${err}` });
          });
          addLog({ level: 'info', message: `Project resumed: ${targetProject}` });
        } else if (!isPaused && onPause) {
          Promise.resolve(onPause(targetProject)).catch((err) => {
            addLog({ level: 'error', message: `Failed to pause ${targetProject}: ${err}` });
          });
          addLog({ level: 'warn', message: `Project paused: ${targetProject}` });
        }
      }
      return;
    }

    // 'P' (shift+p) - toggle pause for ALL projects (only in multi-project mode)
    if (input === 'P' && isMultiProject) {
      const allPaused = pausedProjects.size === projects.length;
      if (allPaused && onResumeAll) {
        Promise.resolve(onResumeAll()).catch((err) => {
          addLog({ level: 'error', message: `Failed to resume all: ${err}` });
        });
        addLog({ level: 'info', message: 'All projects resumed' });
      } else if (!allPaused && onPauseAll) {
        Promise.resolve(onPauseAll()).catch((err) => {
          addLog({ level: 'error', message: `Failed to pause all: ${err}` });
        });
        addLog({ level: 'warn', message: 'All projects paused' });
      }
      return;
    }

    // Navigation (only when focused on tasks panel)
    if (focusedPanel === 'tasks') {
      // Use navigationOrder (flattened tree) instead of raw tasks array
      // This ensures j/k navigation matches the visual tree order
      const currentIndex = navigationOrder.indexOf(selectedTaskId || '');

      // Up arrow or k (vim-style)
      if (key.upArrow || input === 'k') {
        if (currentIndex > 0) {
          setSelectedTaskId(navigationOrder[currentIndex - 1]);
        } else if (currentIndex === -1 && navigationOrder.length > 0) {
          // No selection - go to last item
          setSelectedTaskId(navigationOrder[navigationOrder.length - 1]);
        }
        return;
      }

      // Down arrow or j (vim-style)
      if (key.downArrow || input === 'j') {
        if (currentIndex === -1 && navigationOrder.length > 0) {
          // No selection - go to first item
          setSelectedTaskId(navigationOrder[0]);
        } else if (currentIndex < navigationOrder.length - 1) {
          setSelectedTaskId(navigationOrder[currentIndex + 1]);
        }
        return;
      }

      // g - Jump to top of task list
      if (input === 'g' && navigationOrder.length > 0) {
        setSelectedTaskId(navigationOrder[0]);
        setTaskScrollOffset(0);
        return;
      }

      // G - Jump to bottom of task list
      if (input === 'G' && navigationOrder.length > 0) {
        setSelectedTaskId(navigationOrder[navigationOrder.length - 1]);
        // Scroll to show last item
        const maxOffset = Math.max(0, navigationOrder.length - taskViewportHeight);
        setTaskScrollOffset(maxOffset);
        return;
      }

      // Enter to toggle completed/draft section when header is selected
      if (key.return && selectedTaskId === COMPLETED_HEADER_ID) {
        setCompletedCollapsed(prev => !prev);
        return;
      }
      if (key.return && selectedTaskId === DRAFT_HEADER_ID) {
        setDraftCollapsed(prev => !prev);
        return;
      }

      // 's' to toggle completed/draft section or open status popup
      if (input === 's') {
        // Toggle completed section if header is selected
        if (selectedTaskId === COMPLETED_HEADER_ID) {
          setCompletedCollapsed(prev => !prev);
          return;
        }
        // Toggle draft section if header is selected
        if (selectedTaskId === DRAFT_HEADER_ID) {
          setDraftCollapsed(prev => !prev);
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
              scrollOffset={taskScrollOffset}
              viewportHeight={taskViewportHeight}
            />
          </Box>

          {/* Right panel: Task Detail */}
          <Box
            width="50%"
            flexDirection="column"
          >
            <TaskDetail task={selectedTask} />
          </Box>
        </Box>

        {/* Bottom row: Logs (full width) */}
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
      </Box>

      {/* Help bar at bottom */}
      <HelpBar focusedPanel={focusedPanel} isMultiProject={isMultiProject} />
    </Box>
  );
}

export default App;

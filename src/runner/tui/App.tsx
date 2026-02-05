/**
 * Root TUI component for the Brain Task Runner dashboard
 *
 * Layout:
 * ┌─ project-name ──────────────────────────────────────────────────────────┐
 * │  ● 2 ready   ○ 3 waiting   ▶ 1 active   ✓ 2 done                        │
 * ├──────────────────────────────────────────────────────────────────────────┤
 * │ Tasks                              │ Logs                                │
 * │ ────────────────────────────────── │ ─────────────────────────────────── │
 * │ ● Setup base config                │ 17:30:45 INFO  Runner started       │
 * │ └─○ Create utils module            │ 17:30:46 INFO  Task started...      │
 * │   └─○ Create main entry            │ 17:30:47 DEBUG Polling...           │
 * │                                    │ 17:31:22 INFO  Task completed       │
 * ├──────────────────────────────────────────────────────────────────────────┤
 * │ ↑↓/j/k Navigate  Enter: Details  Tab: Switch  r: Refresh  q: Quit       │
 * └──────────────────────────────────────────────────────────────────────────┘
 */

import React, { useState, useEffect, useMemo, useCallback } from 'react';

/** Compare two Sets for value equality (same size and same elements) */
export function setsEqual<T>(a: Set<T>, b: Set<T>): boolean {
  if (a.size !== b.size) return false;
  for (const item of a) {
    if (!b.has(item)) return false;
  }
  return true;
}
import { Box, Text, useInput, useApp } from 'ink';
import { StatusBar } from './components/StatusBar';
import { TaskTree, flattenTreeOrder, COMPLETED_HEADER_ID } from './components/TaskTree';
import { LogViewer } from './components/LogViewer';
import { TaskDetail } from './components/TaskDetail';
import { HelpBar } from './components/HelpBar';
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
  const [completedCollapsed, setCompletedCollapsed] = useState(true);
  const [activeProject, setActiveProject] = useState<string>(config.activeProject ?? (isMultiProject ? 'all' : projects[0]));
  const [pausedProjects, setPausedProjects] = useState<Set<string>>(new Set());

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

  const { logs, addLog } = useLogStream({ maxEntries: config.maxLogs });
  const { rows: terminalRows } = useTerminalSize();

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

  // Stable callback for toggling completed section (avoids new ref on every render)
  const handleToggleCompleted = useCallback(() => {
    setCompletedCollapsed(prev => !prev);
  }, []);

  // Find selected task
  const selectedTask = tasks.find((t) => t.id === selectedTaskId) || null;

  // Get task IDs in visual tree order for navigation (j/k keys)
  // This ensures navigation follows the same order tasks appear on screen
  const navigationOrder = useMemo(() => flattenTreeOrder(tasks, completedCollapsed), [tasks, completedCollapsed]);

  // All project tabs including 'all' at the front
  const allProjectTabs = ['all', ...projects];

  // Handle keyboard input
  useInput((input, key) => {
    // Quit
    if (input === 'q') {
      exit();
      return;
    }

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

      // Enter to toggle completed section or show details
      if (key.return) {
        // Toggle completed section if header is selected
        if (selectedTaskId === COMPLETED_HEADER_ID) {
          setCompletedCollapsed(prev => !prev);
          return;
        }
        // Currently just logs - TaskDetail is always visible
        if (selectedTask) {
          addLog({
            level: 'info',
            message: `Selected: ${selectedTask.title}`,
            taskId: selectedTask.id,
          });
        }
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
        <Text>  <Text bold>q</Text>         - Quit</Text>
        <Text>  <Text bold>r</Text>         - Refresh tasks</Text>
        <Text>  <Text bold>x</Text>         - Cancel selected task</Text>
        <Text>  <Text bold>p</Text>         - Pause/Resume current project</Text>
        {isMultiProject && (
          <Text>  <Text bold>P</Text>         - Pause/Resume ALL projects</Text>
        )}
        <Text>  <Text bold>?</Text>         - Toggle help</Text>
        <Text>  <Text bold>Tab</Text>       - Switch focus (tasks/logs)</Text>
        <Text>  <Text bold>Up/k</Text>      - Navigate up</Text>
        <Text>  <Text bold>Down/j</Text>    - Navigate down</Text>
        <Text>  <Text bold>Enter</Text>     - Select task / Toggle completed</Text>
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

  return (
    <Box flexDirection="column" width="100%" height={terminalRows}>
      {/* Status bar at top */}
      <StatusBar
        projectId={isMultiProject && activeProject === 'all' ? `${projects.length} projects` : (activeProject || config.project || 'brain-runner')}
        projects={isMultiProject ? projects : undefined}
        activeProject={isMultiProject ? activeProject : undefined}
        onSelectProject={isMultiProject ? setActiveProject : undefined}
        stats={stats}
        statsByProject={isMultiProject ? multiProjectPoller.statsByProject : undefined}
        isConnected={isConnected}
        pausedProjects={pausedProjects}
      />

      {/* Main content area: Tasks (left) + Logs/Details (right) */}
      <Box flexGrow={1} flexDirection="row">
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
            groupByProject={isMultiProject && activeProject === 'all'}
          />
        </Box>

        {/* Right panel: Logs (top) + Task Detail (bottom) */}
        <Box
          width="50%"
          borderStyle="single"
          borderColor={focusedPanel === 'logs' ? 'cyan' : 'gray'}
          flexDirection="column"
        >
          {/* Log viewer takes most of the space */}
          <Box flexGrow={1}>
            <LogViewer 
              logs={logs} 
              maxLines={10} 
              showProjectPrefix={isMultiProject}
            />
          </Box>

          {/* Task detail at bottom of right panel */}
          <TaskDetail task={selectedTask} />
        </Box>
      </Box>

      {/* Help bar at bottom */}
      <HelpBar focusedPanel={focusedPanel} isMultiProject={isMultiProject} />
    </Box>
  );
}

export default App;

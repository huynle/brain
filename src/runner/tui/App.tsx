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

import React, { useState, useEffect, useMemo } from 'react';
import { Box, Text, useInput, useApp } from 'ink';
import { StatusBar } from './components/StatusBar';
import { TaskTree, flattenTreeOrder } from './components/TaskTree';
import { LogViewer } from './components/LogViewer';
import { TaskDetail } from './components/TaskDetail';
import { HelpBar } from './components/HelpBar';
import { useTaskPoller } from './hooks/useTaskPoller';
import { useLogStream } from './hooks/useLogStream';
import { useTerminalSize } from './hooks/useTerminalSize';
import type { AppProps } from './types';

type FocusedPanel = 'tasks' | 'logs';

export function App({ config, onLogCallback, onCancelTask }: AppProps): React.ReactElement {
  const { exit } = useApp();

  // State
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);
  const [focusedPanel, setFocusedPanel] = useState<FocusedPanel>('tasks');
  const [showHelp, setShowHelp] = useState(false);

  // Hooks
  const { tasks, stats, isLoading, isConnected, error, refetch } = useTaskPoller({
    projectId: config.project,
    apiUrl: config.apiUrl,
    pollInterval: config.pollInterval,
    enabled: true,
  });
  const { logs, addLog } = useLogStream({ maxEntries: config.maxLogs });
  const { rows: terminalRows } = useTerminalSize();

  // Expose addLog to parent for external log integration
  useEffect(() => {
    if (onLogCallback) {
      onLogCallback(addLog);
    }
  }, [onLogCallback, addLog]);

  // Find selected task
  const selectedTask = tasks.find((t) => t.id === selectedTaskId) || null;

  // Get task IDs in visual tree order for navigation (j/k keys)
  // This ensures navigation follows the same order tasks appear on screen
  const navigationOrder = useMemo(() => flattenTreeOrder(tasks), [tasks]);

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

      // Enter to show details (future enhancement)
      if (key.return) {
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
        <Text>  <Text bold>?</Text>         - Toggle help</Text>
        <Text>  <Text bold>Tab</Text>       - Switch focus (tasks/logs)</Text>
        <Text>  <Text bold>Up/k</Text>      - Navigate up</Text>
        <Text>  <Text bold>Down/j</Text>    - Navigate down</Text>
        <Text>  <Text bold>Enter</Text>     - Select task (show details)</Text>
        <Text />
        <Text dimColor>Press ? to close</Text>
      </Box>
    );
  }

  return (
    <Box flexDirection="column" width="100%" height={terminalRows}>
      {/* Status bar at top */}
      <StatusBar
        projectId={config.project || 'brain-runner'}
        stats={stats}
        isConnected={isConnected}
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
            <LogViewer logs={logs} maxLines={10} />
          </Box>

          {/* Task detail at bottom of right panel */}
          <TaskDetail task={selectedTask} />
        </Box>
      </Box>

      {/* Help bar at bottom */}
      <HelpBar focusedPanel={focusedPanel} />
    </Box>
  );
}

export default App;

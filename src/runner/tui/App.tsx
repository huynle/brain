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

import React, { useState, useEffect } from 'react';
import { Box, Text, useInput, useApp } from 'ink';
import { StatusBar } from './components/StatusBar';
import { TaskTree } from './components/TaskTree';
import { LogViewer } from './components/LogViewer';
import { TaskDetail } from './components/TaskDetail';
import { HelpBar } from './components/HelpBar';
import { useTaskPoller } from './hooks/useTaskPoller';
import { useLogStream } from './hooks/useLogStream';
import type { AppProps } from './types';

type FocusedPanel = 'tasks' | 'logs';

export function App({ config }: AppProps): React.ReactElement {
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

  // Find selected task
  const selectedTask = tasks.find((t) => t.id === selectedTaskId) || null;

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
      const currentIndex = tasks.findIndex((t) => t.id === selectedTaskId);

      // Up arrow or k (vim-style)
      if (key.upArrow || input === 'k') {
        if (currentIndex > 0) {
          setSelectedTaskId(tasks[currentIndex - 1].id);
        } else if (currentIndex === -1 && tasks.length > 0) {
          setSelectedTaskId(tasks[tasks.length - 1].id);
        }
        return;
      }

      // Down arrow or j (vim-style)
      if (key.downArrow || input === 'j') {
        if (currentIndex === -1 && tasks.length > 0) {
          setSelectedTaskId(tasks[0].id);
        } else if (currentIndex < tasks.length - 1) {
          setSelectedTaskId(tasks[currentIndex + 1].id);
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
    <Box flexDirection="column" width="100%" height="100%">
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

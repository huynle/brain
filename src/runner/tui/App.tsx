/**
 * Root TUI component for the Brain Task Runner dashboard
 */

import React, { useState } from 'react';
import { Box, Text, useInput } from 'ink';
import { StatusBar } from './components/StatusBar';
import { TaskTree } from './components/TaskTree';
import { LogViewer } from './components/LogViewer';
import { TaskDetail } from './components/TaskDetail';
import { useTaskPoller } from './hooks/useTaskPoller';
import { useLogStream } from './hooks/useLogStream';
import type { AppProps } from './types';

export function App({ config }: AppProps): React.ReactElement {
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null);
  const [showHelp, setShowHelp] = useState(false);

  const { tasks, isPolling, error, lastUpdate, refresh } = useTaskPoller(config);
  const { logs, addLog } = useLogStream(config.maxLogs);

  // Helper to check if all dependencies are completed
  const areDepsCompleted = (depIds: string[]): boolean => {
    if (depIds.length === 0) return true;
    return depIds.every(depId => {
      const depTask = tasks.find(t => t.id === depId);
      return depTask?.status === 'completed';
    });
  };

  // Calculate stats for StatusBar
  const stats = {
    ready: tasks.filter((t) => t.status === 'pending' && areDepsCompleted(t.dependencies)).length,
    waiting: tasks.filter((t) => t.status === 'pending' && !areDepsCompleted(t.dependencies)).length,
    inProgress: tasks.filter((t) => t.status === 'in_progress').length,
    completed: tasks.filter((t) => t.status === 'completed').length,
    blocked: tasks.filter((t) => t.status === 'blocked').length,
  };

  // Find selected task
  const selectedTask = tasks.find((t) => t.id === selectedTaskId) || null;

  // Handle keyboard input
  useInput((input, key) => {
    if (input === 'q') {
      process.exit(0);
    }
    if (input === 'r') {
      addLog({ level: 'info', message: 'Manual refresh triggered' });
      refresh();
    }
    if (input === '?') {
      setShowHelp(!showHelp);
    }
    if (key.upArrow || key.downArrow) {
      const currentIndex = tasks.findIndex((t) => t.id === selectedTaskId);
      if (key.upArrow && currentIndex > 0) {
        setSelectedTaskId(tasks[currentIndex - 1].id);
      } else if (key.downArrow && currentIndex < tasks.length - 1) {
        setSelectedTaskId(tasks[currentIndex + 1].id);
      } else if (currentIndex === -1 && tasks.length > 0) {
        setSelectedTaskId(tasks[0].id);
      }
    }
  });

  // Log errors when they occur
  React.useEffect(() => {
    if (error) {
      addLog({ level: 'error', message: error });
    }
  }, [error, addLog]);

  if (showHelp) {
    return (
      <Box flexDirection="column" padding={1}>
        <Text bold>Keyboard Shortcuts</Text>
        <Text>  q - Quit</Text>
        <Text>  r - Refresh tasks</Text>
        <Text>  ? - Toggle help</Text>
        <Text>  Up/Down - Navigate tasks</Text>
        <Text />
        <Text dimColor>Press ? to close</Text>
      </Box>
    );
  }

  return (
    <Box flexDirection="column">
      <StatusBar
        projectId={config.project || 'brain-runner'}
        stats={stats}
        isConnected={!error}
      />

      <Box flexDirection="row" marginTop={1}>
        <Box width="60%">
          <TaskTree
            tasks={tasks}
            selectedId={selectedTaskId}
            onSelect={setSelectedTaskId}
          />
        </Box>
        <Box width="40%">
          <TaskDetail task={selectedTask} />
        </Box>
      </Box>

      <LogViewer logs={logs} maxLines={8} />

      <Box paddingX={1}>
        <Text dimColor>
          Press 'q' to quit | 'r' to refresh | '?' for help
        </Text>
      </Box>
    </Box>
  );
}

export default App;

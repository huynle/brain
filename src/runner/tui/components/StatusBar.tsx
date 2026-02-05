/**
 * Top status bar component showing task counts and runner status
 * 
 * In multi-project mode, shows project tabs on a separate row above stats.
 */

import React from 'react';
import { Box, Text } from 'ink';
import { ProjectTabs } from './ProjectTabs';
import type { TaskStats } from '../hooks/useTaskPoller';

interface StatusBarProps {
  projectId: string;
  projects?: string[];        // All projects (for multi-project mode)
  activeProject?: string;     // Currently selected project or 'all'
  onSelectProject?: (id: string) => void;  // Callback when tab is selected
  stats: {
    ready: number;
    waiting: number;
    inProgress: number;
    completed: number;
    blocked: number;
  };
  statsByProject?: Map<string, TaskStats>;  // Per-project stats for tab indicators
  isConnected: boolean;
  pausedProjects?: Set<string>;  // Set of paused project IDs
}

export const StatusBar = React.memo(function StatusBar({
  projectId,
  projects,
  activeProject,
  onSelectProject,
  stats,
  statsByProject,
  isConnected,
  pausedProjects,
}: StatusBarProps): React.ReactElement {
  // Display title based on multi-project mode
  const isMultiProject = projects && projects.length > 1;
  const displayTitle = isMultiProject && activeProject === 'all'
    ? `[${projects.length} projects]`
    : projectId;

  // Calculate if current view is paused
  const isPaused = pausedProjects 
    ? (activeProject === 'all' 
        ? (pausedProjects.size === (projects?.length ?? 0) && (projects?.length ?? 0) > 0)
        : pausedProjects.has(activeProject ?? projectId))
    : false;

  // If multi-project mode, render tabs on separate row above stats
  if (isMultiProject && activeProject && onSelectProject) {
    return (
      <Box flexDirection="column">
        {/* Tabs row */}
        <Box
          borderStyle="single"
          borderColor="cyan"
          borderBottom={false}
          paddingX={1}
        >
          <ProjectTabs
            projects={projects}
            activeProject={activeProject}
            onSelectProject={onSelectProject}
            statsByProject={statsByProject}
            pausedProjects={pausedProjects}
          />
        </Box>
        
        {/* Stats row */}
        <Box
          borderStyle="single"
          borderColor="cyan"
          borderTop={false}
          paddingX={1}
          justifyContent="space-between"
        >
          <Box>
            {isPaused && (
              <>
                <Text color="yellow" bold>⏸ PAUSED</Text>
                <Text>   </Text>
              </>
            )}
            <Text color="green">● {stats.ready} ready</Text>
            <Text>   </Text>
            <Text color="yellow">○ {stats.waiting} waiting</Text>
            <Text>   </Text>
            <Text color="blue">▶ {stats.inProgress} active</Text>
            <Text>   </Text>
            <Text color="green" dimColor>✓ {stats.completed} done</Text>
            {stats.blocked > 0 && (
              <>
                <Text>   </Text>
                <Text color="red">✗ {stats.blocked} blocked</Text>
              </>
            )}
          </Box>

          <Box>
            {isConnected ? (
              <Text color="green">● online</Text>
            ) : (
              <Text color="red">○ offline</Text>
            )}
          </Box>
        </Box>
      </Box>
    );
  }

  // Single project mode: original layout
  return (
    <Box
      borderStyle="single"
      borderColor="cyan"
      paddingX={1}
      justifyContent="space-between"
    >
      <Box>
        <Text bold color="cyan">
          {displayTitle}
        </Text>
        {isMultiProject && activeProject !== 'all' && (
          <Text dimColor> ({projects.length} total)</Text>
        )}
      </Box>

      <Box>
        {isPaused && (
          <>
            <Text color="yellow" bold>⏸ PAUSED</Text>
            <Text>   </Text>
          </>
        )}
        <Text color="green">● {stats.ready} ready</Text>
        <Text>   </Text>
        <Text color="yellow">○ {stats.waiting} waiting</Text>
        <Text>   </Text>
        <Text color="blue">▶ {stats.inProgress} active</Text>
        <Text>   </Text>
        <Text color="green" dimColor>✓ {stats.completed} done</Text>
        {stats.blocked > 0 && (
          <>
            <Text>   </Text>
            <Text color="red">✗ {stats.blocked} blocked</Text>
          </>
        )}
      </Box>

      <Box>
        {isConnected ? (
          <Text color="green">● online</Text>
        ) : (
          <Text color="red">○ offline</Text>
        )}
      </Box>
    </Box>
  );
});

export default StatusBar;

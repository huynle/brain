/**
 * Top status bar component showing task counts and runner status
 * 
 * In multi-project mode, shows project tabs on a separate row above stats.
 */

import React from 'react';
import { Box, Text } from 'ink';
import { ProjectTabs } from './ProjectTabs';
import type { TaskStats } from '../hooks/useTaskPoller';
import type { FeatureStats, ResourceMetrics } from '../types';

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
  featureStats?: FeatureStats;   // Feature-level statistics
  activeFeatureName?: string;    // Name of active feature (when tasks running)
  /** Callback to get actual running OpenCode process count (overrides stats.inProgress) */
  getRunningProcessCount?: () => number;
  /** Resource metrics for running OpenCode processes */
  resourceMetrics?: ResourceMetrics | null;
}

/**
 * Render resource metrics inline
 * Format: "CPU: 12% | Mem: 1.2GB | 3 procs"
 */
function ResourceMetricsDisplay({
  metrics,
}: {
  metrics: ResourceMetrics;
}): React.ReactElement {
  // Format memory: convert MB to GB if >= 1000MB
  const memoryValue = parseFloat(metrics.memoryMB);
  const memoryDisplay = memoryValue >= 1000
    ? `${(memoryValue / 1024).toFixed(1)}GB`
    : `${metrics.memoryMB}MB`;

  return (
    <>
      <Text color="green">CPU: {metrics.cpuPercent}%</Text>
      <Text>   </Text>
      <Text color="green">Mem: {memoryDisplay}</Text>
      <Text>   </Text>
      <Text color="green">{metrics.processCount} procs</Text>
    </>
  );
}

/**
 * Render feature stats inline
 * Format: "Features: N/M ready" or "Features: N/M ready [active-name]"
 */
function FeatureStatsDisplay({ 
  featureStats, 
  activeFeatureName 
}: { 
  featureStats: FeatureStats; 
  activeFeatureName?: string;
}): React.ReactElement | null {
  // Calculate ready features: total - pending - inProgress - blocked
  const ready = featureStats.total - featureStats.pending - featureStats.inProgress - featureStats.blocked;
  
  return (
    <>
      <Text dimColor>|</Text>
      <Text>   </Text>
      <Text color="magenta">
        Features: {ready}/{featureStats.total} ready
      </Text>
      {activeFeatureName && (
        <>
          <Text> </Text>
          <Text color="blue" dimColor>[{activeFeatureName}]</Text>
        </>
      )}
    </>
  );
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
  featureStats,
  activeFeatureName,
  getRunningProcessCount,
  resourceMetrics,
}: StatusBarProps): React.ReactElement {
  // Check if we have feature stats to display (total > 0 means features exist)
  const hasFeatures = featureStats && featureStats.total > 0;
  
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

  // Use actual running process count if callback provided, otherwise fall back to stats.inProgress
  const activeCount = getRunningProcessCount ? getRunningProcessCount() : stats.inProgress;

  // If multi-project mode, render tabs on separate row above stats
  // Only check isMultiProject - activeProject defaults to 'all' if undefined
  if (isMultiProject && onSelectProject) {
    const effectiveActiveProject = activeProject || 'all';
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
            activeProject={effectiveActiveProject}
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
            <Text color="blue">▶ {activeCount} active</Text>
            <Text>   </Text>
            <Text color="green" dimColor>✓ {stats.completed} done</Text>
            {stats.blocked > 0 && (
              <>
                <Text>   </Text>
                <Text color="red">✗ {stats.blocked} blocked</Text>
              </>
            )}
            {hasFeatures && featureStats && (
              <FeatureStatsDisplay 
                featureStats={featureStats} 
                activeFeatureName={activeFeatureName} 
              />
            )}
          </Box>

          <Box>
            {resourceMetrics && resourceMetrics.processCount > 0 && (
              <>
                <ResourceMetricsDisplay metrics={resourceMetrics} />
                <Text>   </Text>
                <Text dimColor>|</Text>
                <Text>   </Text>
              </>
            )}
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
        <Text color="blue">▶ {activeCount} active</Text>
        <Text>   </Text>
        <Text color="green" dimColor>✓ {stats.completed} done</Text>
        {stats.blocked > 0 && (
          <>
            <Text>   </Text>
            <Text color="red">✗ {stats.blocked} blocked</Text>
          </>
        )}
        {hasFeatures && featureStats && (
          <FeatureStatsDisplay 
            featureStats={featureStats} 
            activeFeatureName={activeFeatureName} 
          />
        )}
      </Box>

      <Box>
        {resourceMetrics && resourceMetrics.processCount > 0 && (
          <>
            <ResourceMetricsDisplay metrics={resourceMetrics} />
            <Text>   </Text>
            <Text dimColor>|</Text>
            <Text>   </Text>
          </>
        )}
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

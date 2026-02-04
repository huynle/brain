/**
 * Tab-based project selector for multi-project mode
 * 
 * Displays horizontal tabs for switching between projects:
 * [All] [brain-api] [opencode] [my-proj]
 * 
 * Features:
 * - "All" tab shows unified view across all projects
 * - Per-project tabs filter to just that project
 * - Active tab is highlighted
 * - Indicator dots show projects with active/blocked tasks
 * - Long project names are truncated
 */

import React from 'react';
import { Box, Text } from 'ink';
import type { TaskStats } from '../hooks/useTaskPoller';

export interface ProjectTabsProps {
  /** All project IDs */
  projects: string[];
  /** Currently selected project ('all' or specific project ID) */
  activeProject: string;
  /** Callback when a project tab is selected */
  onSelectProject: (id: string) => void;
  /** Optional per-project stats for showing activity indicators */
  statsByProject?: Map<string, TaskStats>;
}

/** Maximum characters for a project name before truncation */
const MAX_PROJECT_NAME_LENGTH = 15;

/**
 * Truncate a project name if too long
 */
function truncateName(name: string, maxLength: number = MAX_PROJECT_NAME_LENGTH): string {
  if (name.length <= maxLength) return name;
  return name.slice(0, maxLength - 2) + '..';
}

/**
 * Get activity indicator and color for a project
 * Priority: in_progress (▶ blue) > blocked (✗ red) > ready (● green)
 */
function getProjectIndicator(stats: TaskStats | undefined): { icon: string; color: string } | null {
  if (!stats) return null;
  if (stats.inProgress > 0) return { icon: '▶', color: 'blue' };   // ▶ for in_progress
  if (stats.blocked > 0) return { icon: '✗', color: 'red' };       // ✗ for blocked
  if (stats.ready > 0) return { icon: '●', color: 'green' };       // ● for ready
  return null;
}

/**
 * Single tab component
 */
function Tab({
  label,
  isActive,
  indicator,
  indicatorColor,
  isLast,
}: {
  label: string;
  isActive: boolean;
  indicator?: string;
  indicatorColor?: string;
  isLast: boolean;
}): React.ReactElement {
  const suffix = isLast ? '' : '  ';
  return (
    <>
      {indicator && <Text color={indicatorColor}>{indicator}</Text>}
      <Text
        backgroundColor={isActive ? 'cyan' : undefined}
        color={isActive ? 'black' : undefined}
        bold={isActive}
      >[{label}]</Text>
      {suffix && <Text>{suffix}</Text>}
    </>
  );
}

export function ProjectTabs({
  projects,
  activeProject,
  onSelectProject,
  statsByProject,
}: ProjectTabsProps): React.ReactElement {
  // Don't render tabs if single project
  if (projects.length <= 1) {
    return <></>;
  }

  // Calculate aggregate stats for "All" tab activity indicator
  const aggregateStats: TaskStats | undefined = statsByProject
    ? {
        total: 0,
        ready: 0,
        waiting: 0,
        blocked: 0,
        inProgress: 0,
        completed: 0,
      }
    : undefined;

  if (statsByProject && aggregateStats) {
    for (const stats of statsByProject.values()) {
      aggregateStats.total += stats.total;
      aggregateStats.ready += stats.ready;
      aggregateStats.waiting += stats.waiting;
      aggregateStats.blocked += stats.blocked;
      aggregateStats.inProgress += stats.inProgress;
      aggregateStats.completed += stats.completed;
    }
  }

  // Get indicator for aggregate stats ("All" tab)
  const aggregateIndicator = getProjectIndicator(aggregateStats);

  return (
    <Box flexDirection="row">
      {/* All tab */}
      <Tab
        label="All"
        isActive={activeProject === 'all'}
        indicator={aggregateIndicator?.icon}
        indicatorColor={aggregateIndicator?.color}
        isLast={projects.length === 0}
      />

      {/* Project tabs */}
      {projects.map((projectId, index) => {
        const stats = statsByProject?.get(projectId);
        const indicator = getProjectIndicator(stats);
        return (
          <Tab
            key={projectId}
            label={truncateName(projectId)}
            isActive={activeProject === projectId}
            indicator={indicator?.icon}
            indicatorColor={indicator?.color}
            isLast={index === projects.length - 1}
          />
        );
      })}

      {/* Fill remaining space */}
      <Box flexGrow={1} />
    </Box>
  );
}

export default ProjectTabs;

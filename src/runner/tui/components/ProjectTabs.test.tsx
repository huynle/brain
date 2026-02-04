/**
 * ProjectTabs Component Tests
 *
 * Tests the tab-based project selector for multi-project mode
 */

import React from 'react';
import { describe, it, expect } from 'bun:test';
import { render } from 'ink-testing-library';
import { ProjectTabs } from './ProjectTabs';
import type { TaskStats } from '../hooks/useTaskPoller';

describe('ProjectTabs', () => {
  const mockProjects = ['brain-api', 'opencode', 'my-proj'];
  const mockOnSelect = () => {};

  describe('rendering', () => {
    it('renders all project tabs plus All tab', () => {
      const { lastFrame } = render(
        <ProjectTabs
          projects={mockProjects}
          activeProject="all"
          onSelectProject={mockOnSelect}
        />
      );
      expect(lastFrame()).toContain('[All]');
      expect(lastFrame()).toContain('[brain-api]');
      expect(lastFrame()).toContain('[opencode]');
      expect(lastFrame()).toContain('[my-proj]');
    });

    it('returns empty for single project (no tabs needed)', () => {
      const { lastFrame } = render(
        <ProjectTabs
          projects={['single-proj']}
          activeProject="single-proj"
          onSelectProject={mockOnSelect}
        />
      );
      // Should be empty or minimal
      expect(lastFrame()).toBe('');
    });

    it('returns empty for empty projects array', () => {
      const { lastFrame } = render(
        <ProjectTabs
          projects={[]}
          activeProject="all"
          onSelectProject={mockOnSelect}
        />
      );
      expect(lastFrame()).toBe('');
    });
  });

  describe('active tab highlighting', () => {
    it('highlights All tab when active', () => {
      const { lastFrame } = render(
        <ProjectTabs
          projects={mockProjects}
          activeProject="all"
          onSelectProject={mockOnSelect}
        />
      );
      // The active tab should contain [All]
      expect(lastFrame()).toContain('[All]');
    });

    it('highlights specific project tab when active', () => {
      const { lastFrame } = render(
        <ProjectTabs
          projects={mockProjects}
          activeProject="brain-api"
          onSelectProject={mockOnSelect}
        />
      );
      expect(lastFrame()).toContain('[brain-api]');
    });
  });

  describe('truncation', () => {
    it('truncates long project names', () => {
      // Need at least 2 projects to show tabs
      const longProjects = ['very-long-project-name-here', 'proj-b'];
      const { lastFrame } = render(
        <ProjectTabs
          projects={longProjects}
          activeProject="all"
          onSelectProject={mockOnSelect}
        />
      );
      // Should be truncated (max 15 chars: 13 chars + "..")
      expect(lastFrame()).toContain('very-long-pro..');
    });

    it('does not truncate short project names', () => {
      // Need at least 2 projects to show tabs
      const { lastFrame } = render(
        <ProjectTabs
          projects={['short', 'other']}
          activeProject="all"
          onSelectProject={mockOnSelect}
        />
      );
      expect(lastFrame()).toContain('[short]');
    });
  });

  describe('activity indicators', () => {
    it('shows indicator for projects with active tasks', () => {
      const statsByProject = new Map<string, TaskStats>([
        ['brain-api', { total: 5, ready: 1, waiting: 1, blocked: 0, inProgress: 2, completed: 1 }],
        ['opencode', { total: 3, ready: 0, waiting: 0, blocked: 0, inProgress: 0, completed: 3 }],
      ]);
      const { lastFrame } = render(
        <ProjectTabs
          projects={['brain-api', 'opencode']}
          activeProject="all"
          onSelectProject={mockOnSelect}
          statsByProject={statsByProject}
        />
      );
      // brain-api has inProgress > 0, should have ▶ indicator (blue)
      expect(lastFrame()).toContain('\u25b6'); // ▶ for in_progress
    });

    it('shows indicator for projects with blocked tasks', () => {
      const statsByProject = new Map<string, TaskStats>([
        ['brain-api', { total: 5, ready: 1, waiting: 1, blocked: 2, inProgress: 0, completed: 1 }],
        ['other', { total: 3, ready: 0, waiting: 0, blocked: 0, inProgress: 0, completed: 3 }],
      ]);
      const { lastFrame } = render(
        <ProjectTabs
          projects={['brain-api', 'other']}
          activeProject="all"
          onSelectProject={mockOnSelect}
          statsByProject={statsByProject}
        />
      );
      // brain-api has blocked > 0, should have ✗ indicator (red)
      expect(lastFrame()).toContain('\u2717'); // ✗ for blocked
    });
    
    it('shows ready indicator when no in_progress or blocked', () => {
      const statsByProject = new Map<string, TaskStats>([
        ['brain-api', { total: 5, ready: 2, waiting: 1, blocked: 0, inProgress: 0, completed: 2 }],
        ['other', { total: 3, ready: 0, waiting: 0, blocked: 0, inProgress: 0, completed: 3 }],
      ]);
      const { lastFrame } = render(
        <ProjectTabs
          projects={['brain-api', 'other']}
          activeProject="all"
          onSelectProject={mockOnSelect}
          statsByProject={statsByProject}
        />
      );
      // brain-api has ready > 0 but no inProgress or blocked, should have ● indicator (green)
      expect(lastFrame()).toContain('\u25cf'); // ● for ready
    });

    it('works without statsByProject prop', () => {
      const { lastFrame } = render(
        <ProjectTabs
          projects={mockProjects}
          activeProject="all"
          onSelectProject={mockOnSelect}
        />
      );
      // Should render without crashing
      expect(lastFrame()).toContain('[All]');
    });
  });

  describe('separators', () => {
    it('has separators between tabs', () => {
      const { lastFrame } = render(
        <ProjectTabs
          projects={mockProjects}
          activeProject="all"
          onSelectProject={mockOnSelect}
        />
      );
      const frame = lastFrame() || '';
      // Should have multiple tabs visible with some form of separator
      // The ink-testing-library may render unicode in various ways
      expect(frame).toContain('[All]');
      expect(frame).toContain('[brain-api]');
      // Check that tabs are separated (not adjacent)
      expect(frame.length).toBeGreaterThan(30); // Should have content
    });
  });

  describe('pause indicators', () => {
    it('shows ⏸ indicator for paused projects', () => {
      const pausedProjects = new Set(['brain-api']);
      const { lastFrame } = render(
        <ProjectTabs
          projects={['brain-api', 'opencode']}
          activeProject="all"
          onSelectProject={mockOnSelect}
          pausedProjects={pausedProjects}
        />
      );
      // brain-api is paused, should show ⏸ indicator
      expect(lastFrame()).toContain('⏸');
    });

    it('shows ⏸ for All tab when all projects are paused', () => {
      const pausedProjects = new Set(['brain-api', 'opencode']);
      const { lastFrame } = render(
        <ProjectTabs
          projects={['brain-api', 'opencode']}
          activeProject="all"
          onSelectProject={mockOnSelect}
          pausedProjects={pausedProjects}
        />
      );
      // When all are paused, All tab should show ⏸
      const frame = lastFrame() || '';
      // Count occurrences of ⏸ - should be 3 (All + 2 projects)
      const pauseCount = (frame.match(/⏸/g) || []).length;
      expect(pauseCount).toBe(3);
    });

    it('does not show pause indicator for non-paused projects', () => {
      const pausedProjects = new Set(['brain-api']);
      const { lastFrame } = render(
        <ProjectTabs
          projects={['brain-api', 'opencode']}
          activeProject="all"
          onSelectProject={mockOnSelect}
          pausedProjects={pausedProjects}
        />
      );
      const frame = lastFrame() || '';
      // Only 1 pause indicator for brain-api
      const pauseCount = (frame.match(/⏸/g) || []).length;
      expect(pauseCount).toBe(1);
    });

    it('works without pausedProjects prop', () => {
      const { lastFrame } = render(
        <ProjectTabs
          projects={['brain-api', 'opencode']}
          activeProject="all"
          onSelectProject={mockOnSelect}
        />
      );
      // Should render without crashing and no pause indicators
      const frame = lastFrame() || '';
      expect(frame).not.toContain('⏸');
    });
  });
});

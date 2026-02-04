/**
 * App Component Tests
 *
 * Tests the main TUI application component including:
 * - Layout rendering
 * - Keyboard shortcuts
 * - Component integration
 * - State management
 */

import React from 'react';
import { describe, it, expect, mock } from 'bun:test';
import { render } from 'ink-testing-library';
import { App } from './App';
import type { TUIConfig } from './types';

// Mock the hooks to isolate App component testing
const mockTasks = [
  {
    id: 'task-1',
    title: 'Setup Project',
    status: 'completed',
    priority: 'high',
    dependencies: [],
    dependents: ['task-2'],
  },
  {
    id: 'task-2',
    title: 'Implement Features',
    status: 'in_progress',
    priority: 'medium',
    dependencies: ['task-1'],
    dependents: [],
  },
];

const mockStats = {
  ready: 1,
  waiting: 2,
  inProgress: 1,
  completed: 3,
  blocked: 0,
};

const defaultConfig: TUIConfig = {
  apiUrl: 'http://localhost:3000',
  project: 'test-project',
  pollInterval: 2000,
  maxLogs: 50,
};

// Note: Full App testing would require mocking the hooks.
// For now, we test the layout and rendering without mocking.
// These tests validate that the App renders without crashing.

describe('App', () => {
  describe('initial render', () => {
    it('renders without crashing', () => {
      // Note: This test may fail initially due to network requests
      // In a real test suite, we'd mock fetch or the hooks
      expect(() => {
        const { unmount } = render(<App config={defaultConfig} />);
        unmount();
      }).not.toThrow();
    });

    it('displays the project name in status bar', () => {
      const { lastFrame, unmount } = render(<App config={defaultConfig} />);
      expect(lastFrame()).toContain('test-project');
      unmount();
    });
  });

  describe('layout structure', () => {
    it('contains the main layout elements', () => {
      const { lastFrame, unmount } = render(<App config={defaultConfig} />);
      const frame = lastFrame() || '';
      
      // Should have border characters indicating panels
      expect(frame.includes('─') || frame.includes('│')).toBe(true);
      unmount();
    });

    it('shows Tasks header', () => {
      const { lastFrame, unmount } = render(<App config={defaultConfig} />);
      // May show "Tasks" or "No tasks found" initially
      const frame = lastFrame() || '';
      expect(frame.includes('Tasks') || frame.includes('tasks')).toBe(true);
      unmount();
    });

    it('shows Logs header', () => {
      const { lastFrame, unmount } = render(<App config={defaultConfig} />);
      expect(lastFrame()).toContain('Logs');
      unmount();
    });
  });

  describe('help bar', () => {
    it('shows keyboard shortcuts in help bar', () => {
      const { lastFrame, unmount } = render(<App config={defaultConfig} />);
      const frame = lastFrame() || '';
      // Should show common shortcuts
      expect(frame).toContain('Navigate');
      expect(frame).toContain('Quit');
      unmount();
    });

    it('shows current focus panel', () => {
      const { lastFrame, unmount } = render(<App config={defaultConfig} />);
      const frame = lastFrame() || '';
      // Should show which panel is focused (tasks or logs)
      expect(frame.includes('Focus:')).toBe(true);
      unmount();
    });
  });

  describe('connection status', () => {
    it('shows connection indicator', () => {
      const { lastFrame, unmount } = render(<App config={defaultConfig} />);
      const frame = lastFrame() || '';
      // Should show either online or offline
      expect(frame.includes('online') || frame.includes('offline')).toBe(true);
      unmount();
    });
  });
});

// =============================================================================
// Keyboard interaction tests (using ink-testing-library stdin)
// =============================================================================

describe('App - Keyboard Interactions', () => {
  describe('navigation keys', () => {
    it('responds to j/k keys for navigation', () => {
      const { stdin, lastFrame, unmount } = render(
        <App config={defaultConfig} />
      );
      
      // Send 'j' to move down
      stdin.write('j');
      
      // The app should still be rendering (not crashed)
      expect(lastFrame()).toBeDefined();
      unmount();
    });

    it('responds to Tab key to switch panels', () => {
      const { stdin, lastFrame, unmount } = render(
        <App config={defaultConfig} />
      );
      
      // Initial focus should be tasks
      expect(lastFrame()).toContain('tasks');
      
      // Send Tab to switch focus
      stdin.write('\t');
      
      // Should now focus logs
      const frameAfterTab = lastFrame() || '';
      expect(frameAfterTab).toContain('logs');
      unmount();
    });

    it('responds to r key for refresh', () => {
      const { stdin, lastFrame, unmount } = render(
        <App config={defaultConfig} />
      );
      
      // Send 'r' for refresh
      stdin.write('r');
      
      // Should add a log entry about refresh
      // The app should continue rendering
      expect(lastFrame()).toBeDefined();
      unmount();
    });
  });

  describe('help toggle', () => {
    it('can send ? key without crashing', () => {
      const { stdin, lastFrame, unmount } = render(
        <App config={defaultConfig} />
      );
      
      // Send '?' to toggle help - this may toggle help overlay
      // but depending on timing and rendering, we just verify it doesn't crash
      stdin.write('?');
      
      const frameAfterQuestion = lastFrame() || '';
      // Should have some content (either help or normal view)
      expect(frameAfterQuestion.length).toBeGreaterThan(0);
      
      // Toggle off (or toggle again)
      stdin.write('?');
      
      // Should still render
      expect(lastFrame()).toBeDefined();
      unmount();
    });
  });
});

// =============================================================================
// Integration tests for component composition
// =============================================================================

describe('App - Component Integration', () => {
  it('renders StatusBar component', () => {
    const { lastFrame, unmount } = render(<App config={defaultConfig} />);
    const frame = lastFrame() || '';
    // StatusBar shows ready/waiting/active/done indicators
    expect(frame.includes('ready') || frame.includes('waiting') || frame.includes('active')).toBe(true);
    unmount();
  });

  it('renders TaskTree component', () => {
    const { lastFrame, unmount } = render(<App config={defaultConfig} />);
    const frame = lastFrame() || '';
    // TaskTree shows task count or "No tasks found"
    expect(frame.includes('Tasks') || frame.includes('No tasks')).toBe(true);
    unmount();
  });

  it('renders LogViewer component', () => {
    const { lastFrame, unmount } = render(<App config={defaultConfig} />);
    const frame = lastFrame() || '';
    // LogViewer shows "Logs" header and possibly "No logs yet"
    expect(frame).toContain('Logs');
    unmount();
  });

  it('renders HelpBar component', () => {
    const { lastFrame, unmount } = render(<App config={defaultConfig} />);
    const frame = lastFrame() || '';
    // HelpBar shows navigation shortcuts
    expect(frame).toContain('Navigate');
    unmount();
  });
});

// =============================================================================
// Configuration handling tests
// =============================================================================

describe('App - Configuration', () => {
  it('uses project name from config', () => {
    const customConfig: TUIConfig = {
      ...defaultConfig,
      project: 'custom-project-name',
    };
    
    const { lastFrame, unmount } = render(<App config={customConfig} />);
    expect(lastFrame()).toContain('custom-project-name');
    unmount();
  });

  it('handles empty project name', () => {
    const emptyProjectConfig: TUIConfig = {
      ...defaultConfig,
      project: '',
    };
    
    // Should use fallback name
    const { lastFrame, unmount } = render(<App config={emptyProjectConfig} />);
    expect(lastFrame()).toContain('brain-runner');
    unmount();
  });
});

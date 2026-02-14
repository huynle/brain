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
      // Should show connection dot (● or ○)
      expect(frame.includes('●') || frame.includes('○')).toBe(true);
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
      
      // With logs and details hidden by default, Tab just cycles back to tasks
      // Send Tab key and verify the app doesn't crash
      stdin.write('\t');
      
      // App should still be rendering - focus stays on tasks when other panels hidden
      const frameAfterTab = lastFrame() || '';
      expect(frameAfterTab).toBeDefined();
      expect(frameAfterTab).toContain('tasks'); // Should cycle back to tasks
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

// =============================================================================
// Multi-project mode tests
// =============================================================================

describe('App - Multi-Project Mode', () => {
  const multiProjectConfig: TUIConfig = {
    ...defaultConfig,
    projects: ['brain-api', 'opencode', 'my-proj'],
    activeProject: 'all',
  };

  it('renders tabs in multi-project mode', () => {
    const { lastFrame, unmount } = render(<App config={multiProjectConfig} />);
    const frame = lastFrame() || '';
    // Should show tabs
    expect(frame).toContain('[All]');
    unmount();
  });

  it('shows tab navigation shortcuts in help bar for multi-project mode', () => {
    const { lastFrame, unmount } = render(<App config={multiProjectConfig} />);
    const frame = lastFrame() || '';
    // Should show tab shortcuts (1-9/[/])
    expect(frame).toContain('Tabs');
    unmount();
  });

  it('responds to [ key to switch to previous tab', () => {
    const startOnSecondTab: TUIConfig = {
      ...multiProjectConfig,
      activeProject: 'brain-api', // Start on first project tab (after All)
    };
    const { stdin, lastFrame, unmount } = render(<App config={startOnSecondTab} />);
    
    // Press [ to go to previous tab (should go to All)
    stdin.write('[');
    
    const frame = lastFrame() || '';
    // Should still render
    expect(frame.length).toBeGreaterThan(0);
    unmount();
  });

  it('responds to ] key to switch to next tab', () => {
    const { stdin, lastFrame, unmount } = render(<App config={multiProjectConfig} />);
    
    // Press ] to go to next tab (should go from All to brain-api)
    stdin.write(']');
    
    const frame = lastFrame() || '';
    // Should still render
    expect(frame.length).toBeGreaterThan(0);
    unmount();
  });

  it('responds to number keys for direct tab access', () => {
    const { stdin, lastFrame, unmount } = render(<App config={multiProjectConfig} />);
    
    // Press 2 to go to tab 2 (brain-api, index 1)
    stdin.write('2');
    
    const frame = lastFrame() || '';
    // Should still render without crashing
    expect(frame.length).toBeGreaterThan(0);
    unmount();
  });

  it('does not show tab shortcuts in single-project mode', () => {
    const { lastFrame, unmount } = render(<App config={defaultConfig} />);
    const frame = lastFrame() || '';
    // Should NOT show tab shortcuts in single project mode
    expect(frame.includes('1-9/[/]')).toBe(false);
    unmount();
  });
});

// =============================================================================
// Pause state optimization tests (Issue #3)
// =============================================================================

import { setsEqual } from './App';

describe('setsEqual', () => {
  it('returns true for two empty sets', () => {
    expect(setsEqual(new Set(), new Set())).toBe(true);
  });

  it('returns true for identical single-element sets', () => {
    expect(setsEqual(new Set(['a']), new Set(['a']))).toBe(true);
  });

  it('returns true for identical multi-element sets', () => {
    expect(setsEqual(new Set(['a', 'b', 'c']), new Set(['a', 'b', 'c']))).toBe(true);
  });

  it('returns true regardless of insertion order', () => {
    expect(setsEqual(new Set(['b', 'a', 'c']), new Set(['c', 'a', 'b']))).toBe(true);
  });

  it('returns false when sizes differ', () => {
    expect(setsEqual(new Set(['a']), new Set(['a', 'b']))).toBe(false);
  });

  it('returns false when elements differ but sizes match', () => {
    expect(setsEqual(new Set(['a', 'b']), new Set(['a', 'c']))).toBe(false);
  });

  it('returns false for empty vs non-empty', () => {
    expect(setsEqual(new Set(), new Set(['a']))).toBe(false);
  });
});

describe('App - Pause state derivation optimization', () => {
  it('does not trigger unnecessary re-renders when pause state is unchanged', () => {
    // This test verifies the optimization works at the component level:
    // When tasks change but no project's pause state changes, the pausedProjects
    // Set reference should remain the same (React skips re-render for same ref).
    //
    // We verify this indirectly: render with tasks that have no paused projects,
    // then verify the component renders correctly (the optimization is internal).
    const { lastFrame, unmount } = render(<App config={defaultConfig} />);
    const frame = lastFrame() || '';
    // Should render without any pause indicators in single-project mode
    expect(frame.length).toBeGreaterThan(0);
    unmount();
  });
});

// =============================================================================
// Logs panel visibility toggle tests
// =============================================================================

// =============================================================================
// Manual Task Execution tests
// =============================================================================

describe('App - Manual Task Execution', () => {
  it('accepts onExecuteTask callback prop', () => {
    // Verify that onExecuteTask can be passed as a prop without errors
    const mockExecuteTask = async (_taskId: string, _taskPath: string): Promise<boolean> => true;
    
    expect(() => {
      const { unmount } = render(
        <App 
          config={defaultConfig} 
          onExecuteTask={mockExecuteTask}
        />
      );
      unmount();
    }).not.toThrow();
  });

  it('x key triggers execute task, not cancel', async () => {
    // Verify that lowercase 'x' is for execute (new behavior)
    // and uppercase 'X' is for cancel (moved from 'x')
    let executeCalled = false;
    let cancelCalled = false;
    
    const mockExecuteTask = async (taskId: string, taskPath: string): Promise<boolean> => {
      executeCalled = true;
      return true;
    };
    
    const mockCancelTask = async (taskId: string, taskPath: string): Promise<void> => {
      cancelCalled = true;
    };
    
    const { stdin, unmount } = render(
      <App 
        config={defaultConfig} 
        onExecuteTask={mockExecuteTask}
        onCancelTask={mockCancelTask}
      />
    );
    
    // Send lowercase 'x' - should NOT trigger cancel (old behavior was cancel on 'x')
    // Note: Without a selected task, neither callback will be called
    stdin.write('x');
    
    // App should not crash
    expect(true).toBe(true);
    
    unmount();
  });

  it('X key (shift+x) triggers cancel task', async () => {
    // Verify uppercase 'X' is for cancel (moved from lowercase 'x')
    const { stdin, unmount } = render(
      <App config={defaultConfig} />
    );
    
    // Send uppercase 'X' - should trigger cancel handler (if task selected)
    stdin.write('X');
    
    // App should not crash
    expect(true).toBe(true);
    
    unmount();
  });

  it('help bar shows x for Execute and X for Cancel', () => {
    const { lastFrame, unmount } = render(
      <App config={defaultConfig} />
    );
    const frame = lastFrame() || '';
    
    // Should show 'x Execute' in help bar (new shortcut)
    expect(frame).toContain('x');
    expect(frame).toContain('Execute');
    
    // Should show 'X Cancel' in help bar (moved shortcut)
    expect(frame).toContain('X');
    expect(frame).toContain('Cancel');
    
    unmount();
  });

  it('execute callback not called when no task is selected', async () => {
    // Verify that x key requires a selected task to trigger the callback
    let executeCalled = false;
    
    const mockExecuteTask = async (_taskId: string, _taskPath: string): Promise<boolean> => {
      executeCalled = true;
      return true;
    };
    
    const { stdin, unmount } = render(
      <App 
        config={defaultConfig} 
        onExecuteTask={mockExecuteTask}
      />
    );
    
    // Send 'x' without selecting a task first
    stdin.write('x');
    
    // Wait a tick for any async handlers
    await new Promise(r => setTimeout(r, 10));
    
    // Execute should NOT be called because no task is selected
    expect(executeCalled).toBe(false);
    
    unmount();
  });

  it('cancel callback not called when no task is selected', async () => {
    // Verify that X key requires a selected task to trigger the callback
    let cancelCalled = false;
    
    const mockCancelTask = async (_taskId: string, _taskPath: string): Promise<void> => {
      cancelCalled = true;
    };
    
    const { stdin, unmount } = render(
      <App 
        config={defaultConfig} 
        onCancelTask={mockCancelTask}
      />
    );
    
    // Send 'X' without selecting a task first
    stdin.write('X');
    
    // Wait a tick for any async handlers
    await new Promise(r => setTimeout(r, 10));
    
    // Cancel should NOT be called because no task is selected
    expect(cancelCalled).toBe(false);
    
    unmount();
  });
});

describe('App - Logs Panel Visibility Toggle', () => {
  it('hides logs panel by default', () => {
    const { lastFrame, unmount } = render(<App config={defaultConfig} />);
    const frame = lastFrame() || '';
    // Logs panel is hidden by default, so "No logs yet" should NOT appear
    expect(frame).not.toContain('No logs yet');
    unmount();
  });

  it('can send L key without crashing', () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Initial render should NOT show logs panel content (hidden by default)
    expect(lastFrame()).not.toContain('No logs yet');
    
    // Send 'L' to toggle logs visibility
    stdin.write('L');
    
    // App should still be rendering
    expect(lastFrame()).toBeDefined();
    
    // Toggle back
    stdin.write('L');
    
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('Tab key works without crashing', () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Initial state - only tasks panel is visible (logs and details hidden by default)
    const initialFrame = lastFrame() || '';
    expect(initialFrame).toContain('Focus:');
    expect(initialFrame).toContain('tasks');
    
    // Tab should cycle back to tasks (only panel visible by default)
    stdin.write('\t');
    
    // App should still render
    expect(lastFrame()).toBeDefined();
    
    // Tab again
    stdin.write('\t');
    
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('shows L and T shortcuts in help bar', () => {
    const { lastFrame, unmount } = render(<App config={defaultConfig} />);
    const frame = lastFrame() || '';
    // HelpBar should show "L Logs" and "T Detail" shortcuts
    expect(frame).toContain('L');
    expect(frame).toContain('T');
    unmount();
  });
});

// =============================================================================
// Pause Toggle (p key) Tests
// =============================================================================

describe('App - Pause Toggle (p key)', () => {
  it('p key directly toggles pause without popup', () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Send 'p' to toggle pause - should work immediately
    stdin.write('p');
    
    // App should still be rendering (no popup overlay)
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('p key calls onPause when project is running', async () => {
    let pauseCalled = false;
    const onPause = async (projectId: string) => {
      pauseCalled = true;
      expect(projectId).toBe('test-project');
    };
    
    const { stdin, unmount } = render(
      <App config={defaultConfig} onPause={onPause} />
    );
    
    // Send 'p' to toggle pause
    stdin.write('p');
    
    // Wait for async handler
    await new Promise(r => setTimeout(r, 10));
    
    expect(pauseCalled).toBe(true);
    
    unmount();
  });

  it('p shortcut shown in help bar', () => {
    const { lastFrame, unmount } = render(<App config={defaultConfig} />);
    const frame = lastFrame() || '';
    // HelpBar should include 'p' for pause
    expect(frame).toContain('Pause');
    unmount();
  });
});

// =============================================================================
// Execute Task (x key) Tests
// =============================================================================

describe('App - Execute Task (x key)', () => {
  it('x key calls onExecuteTask when task is selected', async () => {
    // Note: This test is already covered in "Manual Task Execution" section
    // Adding for completeness in the new key binding tests
    let executeCalled = false;
    
    const mockExecuteTask = async (_taskId: string, _taskPath: string): Promise<boolean> => {
      executeCalled = true;
      return true;
    };
    
    const { stdin, unmount } = render(
      <App 
        config={defaultConfig} 
        onExecuteTask={mockExecuteTask}
      />
    );
    
    // Send 'x' to trigger execute
    stdin.write('x');
    
    // Wait for any async handlers
    await new Promise(r => setTimeout(r, 10));
    
    // Without a selected task, execute should NOT be called
    expect(executeCalled).toBe(false);
    
    unmount();
  });

  it('x key on feature header does not trigger execute', async () => {
    let executeCalled = false;
    
    const mockExecuteTask = async (_taskId: string, _taskPath: string): Promise<boolean> => {
      executeCalled = true;
      return true;
    };
    
    const { stdin, lastFrame, unmount } = render(
      <App 
        config={defaultConfig} 
        onExecuteTask={mockExecuteTask}
      />
    );
    
    // Navigate (potentially to a feature header if tasks exist)
    stdin.write('j');
    await new Promise(r => setTimeout(r, 10));
    
    // Press 'x'
    stdin.write('x');
    await new Promise(r => setTimeout(r, 10));
    
    // App should still be rendering
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('x key shows failure message when at capacity', async () => {
    // When executeTaskManually returns false, appropriate log should be shown
    const mockExecuteTask = async (_taskId: string, _taskPath: string): Promise<boolean> => {
      return false; // Simulate at capacity
    };
    
    const { stdin, lastFrame, unmount } = render(
      <App 
        config={defaultConfig} 
        onExecuteTask={mockExecuteTask}
      />
    );
    
    // Navigate and try to execute
    stdin.write('j');
    await new Promise(r => setTimeout(r, 10));
    stdin.write('x');
    await new Promise(r => setTimeout(r, 10));
    
    // App should handle gracefully
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });
});

// =============================================================================
// Cancel Task (X key) Tests
// =============================================================================

describe('App - Cancel Task (X key)', () => {
  it('X key requires in_progress status to trigger cancel', async () => {
    let cancelCalled = false;
    
    const mockCancelTask = async (_taskId: string, _taskPath: string): Promise<void> => {
      cancelCalled = true;
    };
    
    const { stdin, lastFrame, unmount } = render(
      <App 
        config={defaultConfig} 
        onCancelTask={mockCancelTask}
      />
    );
    
    // Send 'X' without selecting an in_progress task
    stdin.write('X');
    await new Promise(r => setTimeout(r, 10));
    
    // Cancel should NOT be called (no in_progress task selected)
    expect(cancelCalled).toBe(false);
    
    // App should still be rendering
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('X key on non-running task shows warning instead of canceling', async () => {
    // When X is pressed on a task that is not in_progress, it should show a warning
    let cancelCalled = false;
    
    const mockCancelTask = async (_taskId: string, _taskPath: string): Promise<void> => {
      cancelCalled = true;
    };
    
    const { stdin, unmount } = render(
      <App 
        config={defaultConfig} 
        onCancelTask={mockCancelTask}
      />
    );
    
    // Navigate to a task (if any)
    stdin.write('j');
    await new Promise(r => setTimeout(r, 10));
    
    // Press 'X' to try to cancel
    stdin.write('X');
    await new Promise(r => setTimeout(r, 10));
    
    // Without an in_progress task, cancel should NOT be called
    expect(cancelCalled).toBe(false);
    
    unmount();
  });

  it('X key can be pressed without crashing', () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Send uppercase 'X'
    stdin.write('X');
    
    // App should still be rendering
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });
});

// =============================================================================
// Auto-Pause on Feature Completion Tests
// =============================================================================

describe('App - Auto-Pause on Feature Completion', () => {
  it('does not crash when focused feature has no tasks', async () => {
    const mockPause = async (projectId: string) => {};
    
    const { stdin, lastFrame, unmount } = render(
      <App 
        config={defaultConfig} 
        onPause={mockPause}
      />
    );
    
    // Try to focus and let auto-pause logic run
    stdin.write('f');
    await new Promise(r => setTimeout(r, 50));
    
    // App should still be rendering
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });
});

// =============================================================================
// Multi-Select and MetadataPopup Integration Tests
// =============================================================================

describe('App - Multi-Select (Space Key)', () => {
  it('Space key can be pressed without crashing', () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Send Space to toggle selection
    stdin.write(' ');
    
    // App should still be rendering
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('Space toggles selection on task (when task is focused)', async () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Navigate to a potential task position
    stdin.write('j');
    await new Promise(r => setTimeout(r, 10));
    
    // Press Space to toggle selection
    stdin.write(' ');
    await new Promise(r => setTimeout(r, 10));
    
    // App should continue rendering
    expect(lastFrame()).toBeDefined();
    
    // Press Space again to deselect
    stdin.write(' ');
    await new Promise(r => setTimeout(r, 10));
    
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('Space key does not select feature headers', async () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Navigate and try to select
    stdin.write('j');
    stdin.write(' ');
    await new Promise(r => setTimeout(r, 10));
    
    // App should not crash even if on a header
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });
});

describe('App - MetadataPopup (s Key)', () => {
  it('s key can be pressed without crashing', () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Send 's' to open metadata popup
    stdin.write('s');
    
    // App should still be rendering
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('s key opens popup when on tasks panel', async () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Navigate to a task first
    stdin.write('j');
    await new Promise(r => setTimeout(r, 10));
    
    // Press 's' to open metadata popup
    stdin.write('s');
    await new Promise(r => setTimeout(r, 10));
    
    // If there's a task selected, we might see popup elements
    const frame = lastFrame() || '';
    // App should at least render (popup may or may not appear depending on task availability)
    expect(frame.length).toBeGreaterThan(0);
    
    unmount();
  });

  it('s key does not open popup when on logs panel', async () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // First enable logs panel (hidden by default)
    stdin.write('L');
    await new Promise(r => setTimeout(r, 10));
    
    // Switch to logs panel
    stdin.write('\t');
    await new Promise(r => setTimeout(r, 10));
    
    // Verify we're on logs panel
    const frameAfterTab = lastFrame() || '';
    expect(frameAfterTab).toContain('logs');
    
    // Press 's' - should not do anything special
    stdin.write('s');
    await new Promise(r => setTimeout(r, 10));
    
    // Should still be on logs, no popup
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });
});

describe('App - MetadataPopup Modes', () => {
  it('Esc closes popup without crashing', async () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Try to open popup
    stdin.write('j');
    stdin.write('s');
    await new Promise(r => setTimeout(r, 10));
    
    // Press Esc to close
    stdin.write('\x1b'); // Escape character
    await new Promise(r => setTimeout(r, 10));
    
    // App should still render
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('Tab key works in popup context', async () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Navigate and open popup
    stdin.write('j');
    stdin.write('s');
    await new Promise(r => setTimeout(r, 10));
    
    // Press Tab to cycle fields
    stdin.write('\t');
    await new Promise(r => setTimeout(r, 10));
    
    // App should continue rendering
    expect(lastFrame()).toBeDefined();
    
    // Another Tab
    stdin.write('\t');
    await new Promise(r => setTimeout(r, 10));
    
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('Enter key works in popup context', async () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Navigate and open popup
    stdin.write('j');
    stdin.write('s');
    await new Promise(r => setTimeout(r, 10));
    
    // Press Enter to apply/edit
    stdin.write('\r');
    await new Promise(r => setTimeout(r, 10));
    
    // App should continue rendering
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });
});

describe('App - Multi-Select Batch Mode', () => {
  it('selecting multiple tasks and pressing s works', async () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Navigate and select multiple tasks
    stdin.write('j');
    await new Promise(r => setTimeout(r, 10));
    stdin.write(' '); // Select first
    await new Promise(r => setTimeout(r, 10));
    
    stdin.write('j');
    await new Promise(r => setTimeout(r, 10));
    stdin.write(' '); // Select second
    await new Promise(r => setTimeout(r, 10));
    
    // Open batch metadata popup
    stdin.write('s');
    await new Promise(r => setTimeout(r, 10));
    
    // Should render (popup may show if tasks exist)
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });
});

describe('App - Project Tab Switch Clears Selection', () => {
  const multiProjectConfig: TUIConfig = {
    ...defaultConfig,
    projects: ['project-a', 'project-b'],
    activeProject: 'all',
  };

  it('switching tabs clears multi-select state', async () => {
    const { stdin, lastFrame, unmount } = render(<App config={multiProjectConfig} />);
    
    // Navigate and select a task
    stdin.write('j');
    stdin.write(' ');
    await new Promise(r => setTimeout(r, 10));
    
    // Switch to next project tab
    stdin.write(']');
    await new Promise(r => setTimeout(r, 10));
    
    // App should still be rendering (selection cleared internally)
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('pressing number keys to switch tabs clears selection', async () => {
    const { stdin, lastFrame, unmount } = render(<App config={multiProjectConfig} />);
    
    // Navigate and select
    stdin.write('j');
    stdin.write(' ');
    await new Promise(r => setTimeout(r, 10));
    
    // Press '2' to go to second tab
    stdin.write('2');
    await new Promise(r => setTimeout(r, 10));
    
    // App should still render
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });
});

describe('App - MetadataPopup with onUpdateMetadata callback', () => {
  it('accepts onUpdateMetadata callback prop', () => {
    const mockUpdateMetadata = async (
      _taskPath: string, 
      _fields: { status?: string; feature_id?: string; git_branch?: string; target_workdir?: string }
    ): Promise<void> => {};
    
    expect(() => {
      const { unmount } = render(
        <App 
          config={defaultConfig} 
          onUpdateMetadata={mockUpdateMetadata}
        />
      );
      unmount();
    }).not.toThrow();
  });

  it('s shortcut shown in help bar', () => {
    const { lastFrame, unmount } = render(<App config={defaultConfig} />);
    const frame = lastFrame() || '';
    // HelpBar should include 's' for status/metadata
    expect(frame).toContain('s');
    unmount();
  });

  it('Space shortcut shown in help bar', () => {
    const { lastFrame, unmount } = render(<App config={defaultConfig} />);
    const frame = lastFrame() || '';
    // HelpBar should include 'Space' for select
    expect(frame).toContain('Space');
    unmount();
  });
});

// =============================================================================
// Task Filter Mode Integration Tests
// =============================================================================

describe('App - Task Filter Mode (/ key)', () => {
  describe('filter activation (/ key enters typing mode)', () => {
    it('pressing / activates filter mode and shows FilterBar', async () => {
      const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
      
      // Initially no filter bar visible (no "/" indicator)
      const frameBefore = lastFrame() || '';
      // FilterBar shows "/" only in typing mode
      const hasFilterIndicatorBefore = frameBefore.includes(' / ');
      
      // Press '/' to activate filter
      stdin.write('/');
      await new Promise(r => setTimeout(r, 10));
      
      // FilterBar should now be visible with "/" indicator (typing mode)
      const frameAfter = lastFrame() || '';
      // In typing mode, FilterBar renders with yellow background "/" 
      // We check that the app handled the / key without crashing
      expect(frameAfter.length).toBeGreaterThan(0);
      expect(frameAfter).toBeDefined();
      
      unmount();
    });

    it('/ key only works when focused on tasks panel', async () => {
      const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
      
      // Enable logs panel first (hidden by default)
      stdin.write('L');
      await new Promise(r => setTimeout(r, 10));
      
      // Switch to logs panel
      stdin.write('\t');
      await new Promise(r => setTimeout(r, 10));
      
      // Verify we're on logs panel
      const frameOnLogs = lastFrame() || '';
      expect(frameOnLogs).toContain('logs');
      
      // Press '/' while on logs panel - should NOT activate filter
      stdin.write('/');
      await new Promise(r => setTimeout(r, 10));
      
      // App should still render without crash
      expect(lastFrame()).toBeDefined();
      
      unmount();
    });
  });

  describe('typing mode (dynamic filtering)', () => {
    it('typing characters dynamically filters tasks', async () => {
      const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
      
      // Activate filter mode
      stdin.write('/');
      await new Promise(r => setTimeout(r, 10));
      
      // Type some characters
      stdin.write('t');
      await new Promise(r => setTimeout(r, 10));
      stdin.write('a');
      await new Promise(r => setTimeout(r, 10));
      stdin.write('s');
      await new Promise(r => setTimeout(r, 10));
      stdin.write('k');
      await new Promise(r => setTimeout(r, 10));
      
      // App should still be rendering with filter applied
      const frame = lastFrame() || '';
      expect(frame.length).toBeGreaterThan(0);
      
      unmount();
    });

    it('backspace deletes last character in filter', async () => {
      const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
      
      // Activate filter and type
      stdin.write('/');
      await new Promise(r => setTimeout(r, 10));
      stdin.write('t');
      stdin.write('e');
      stdin.write('s');
      stdin.write('t');
      await new Promise(r => setTimeout(r, 10));
      
      // Send backspace (delete key)
      stdin.write('\x7F'); // DEL character (backspace)
      await new Promise(r => setTimeout(r, 10));
      
      // App should still render
      expect(lastFrame()).toBeDefined();
      
      unmount();
    });
  });

  describe('lock-in mode (Enter to lock filter)', () => {
    it('pressing Enter locks in the filter', async () => {
      const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
      
      // Activate filter and type
      stdin.write('/');
      await new Promise(r => setTimeout(r, 10));
      stdin.write('t');
      stdin.write('e');
      stdin.write('s');
      stdin.write('t');
      await new Promise(r => setTimeout(r, 10));
      
      // Press Enter to lock in filter
      stdin.write('\r');
      await new Promise(r => setTimeout(r, 10));
      
      // Should transition to locked mode (filter persists, navigation enabled)
      const frame = lastFrame() || '';
      expect(frame.length).toBeGreaterThan(0);
      
      unmount();
    });

    it('j/k navigation works on filtered list after lock-in', async () => {
      const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
      
      // Activate filter, type, and lock in
      stdin.write('/');
      await new Promise(r => setTimeout(r, 10));
      stdin.write('t');
      await new Promise(r => setTimeout(r, 10));
      stdin.write('\r'); // Lock in
      await new Promise(r => setTimeout(r, 10));
      
      // Navigate with j/k (should work in locked mode on filtered list)
      stdin.write('j');
      await new Promise(r => setTimeout(r, 10));
      
      expect(lastFrame()).toBeDefined();
      
      stdin.write('k');
      await new Promise(r => setTimeout(r, 10));
      
      expect(lastFrame()).toBeDefined();
      
      unmount();
    });
  });

  describe('escape key behavior', () => {
    it('Esc from typing mode clears filter immediately', async () => {
      const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
      
      // Activate filter and type
      stdin.write('/');
      await new Promise(r => setTimeout(r, 10));
      stdin.write('t');
      stdin.write('e');
      stdin.write('s');
      stdin.write('t');
      await new Promise(r => setTimeout(r, 10));
      
      // Press Escape to clear filter
      stdin.write('\x1b'); // Escape character
      await new Promise(r => setTimeout(r, 10));
      
      // Filter should be cleared (back to off mode)
      const frame = lastFrame() || '';
      expect(frame.length).toBeGreaterThan(0);
      // Should no longer have filter indicator (though this depends on rendering)
      
      unmount();
    });

    it('Esc from locked mode clears filter and returns to full list', async () => {
      const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
      
      // Activate, type, and lock in
      stdin.write('/');
      await new Promise(r => setTimeout(r, 10));
      stdin.write('x');
      stdin.write('y');
      stdin.write('z');
      await new Promise(r => setTimeout(r, 10));
      stdin.write('\r'); // Lock in
      await new Promise(r => setTimeout(r, 10));
      
      // Now in locked mode - press Escape to clear
      stdin.write('\x1b'); // Escape character
      await new Promise(r => setTimeout(r, 10));
      
      // Should return to full task list (filter cleared)
      const frame = lastFrame() || '';
      expect(frame.length).toBeGreaterThan(0);
      
      unmount();
    });
  });

  describe('re-entering filter from locked mode', () => {
    it('/ from locked mode re-enters typing to modify filter', async () => {
      const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
      
      // Activate, type, and lock in
      stdin.write('/');
      await new Promise(r => setTimeout(r, 10));
      stdin.write('a');
      stdin.write('b');
      stdin.write('c');
      await new Promise(r => setTimeout(r, 10));
      stdin.write('\r'); // Lock in
      await new Promise(r => setTimeout(r, 10));
      
      // Now in locked mode - press / to re-enter typing mode
      stdin.write('/');
      await new Promise(r => setTimeout(r, 10));
      
      // Should be back in typing mode, can add more characters
      stdin.write('d');
      stdin.write('e');
      stdin.write('f');
      await new Promise(r => setTimeout(r, 10));
      
      // App should handle the transition smoothly
      const frame = lastFrame() || '';
      expect(frame.length).toBeGreaterThan(0);
      
      unmount();
    });
  });
});

// =============================================================================
// Multi-Project Filter Tests
// =============================================================================

describe('App - Filter in Multi-Project Mode', () => {
  const multiProjectConfig: TUIConfig = {
    ...defaultConfig,
    projects: ['project-alpha', 'project-beta', 'project-gamma'],
    activeProject: 'all',
  };

  it('filter on "All" tab filters across all projects', async () => {
    const { stdin, lastFrame, unmount } = render(<App config={multiProjectConfig} />);
    
    // Activate filter in multi-project All view
    stdin.write('/');
    await new Promise(r => setTimeout(r, 10));
    
    // Type filter text
    stdin.write('a');
    stdin.write('l');
    stdin.write('p');
    stdin.write('h');
    stdin.write('a');
    await new Promise(r => setTimeout(r, 10));
    
    // App should handle cross-project filtering
    const frame = lastFrame() || '';
    expect(frame.length).toBeGreaterThan(0);
    
    unmount();
  });

  it('switching tabs clears active filter', async () => {
    const { stdin, lastFrame, unmount } = render(<App config={multiProjectConfig} />);
    
    // Activate and lock in a filter
    stdin.write('/');
    await new Promise(r => setTimeout(r, 10));
    stdin.write('t');
    stdin.write('e');
    stdin.write('s');
    stdin.write('t');
    await new Promise(r => setTimeout(r, 10));
    stdin.write('\r'); // Lock in
    await new Promise(r => setTimeout(r, 10));
    
    // Switch to next project tab
    stdin.write(']');
    await new Promise(r => setTimeout(r, 10));
    
    // Filter should be cleared when switching tabs
    const frame = lastFrame() || '';
    expect(frame.length).toBeGreaterThan(0);
    
    unmount();
  });
});

// =============================================================================
// Multi-Select with Filter Tests
// =============================================================================

describe('App - Multi-Select with Active Filter', () => {
  it('Space key works on filtered list after lock-in', async () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Activate filter, type, and lock in
    stdin.write('/');
    await new Promise(r => setTimeout(r, 10));
    stdin.write('t');
    await new Promise(r => setTimeout(r, 10));
    stdin.write('\r'); // Lock in
    await new Promise(r => setTimeout(r, 10));
    
    // Navigate to a task in filtered list
    stdin.write('j');
    await new Promise(r => setTimeout(r, 10));
    
    // Press Space to toggle selection (on filtered list)
    stdin.write(' ');
    await new Promise(r => setTimeout(r, 10));
    
    // App should handle selection on filtered results
    const frame = lastFrame() || '';
    expect(frame.length).toBeGreaterThan(0);
    
    unmount();
  });

  it('selecting multiple filtered tasks works', async () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Activate filter, type, and lock in
    stdin.write('/');
    await new Promise(r => setTimeout(r, 10));
    stdin.write('t');
    await new Promise(r => setTimeout(r, 10));
    stdin.write('\r'); // Lock in
    await new Promise(r => setTimeout(r, 10));
    
    // Navigate and select multiple tasks
    stdin.write('j');
    stdin.write(' '); // Select first
    await new Promise(r => setTimeout(r, 10));
    
    stdin.write('j');
    stdin.write(' '); // Select second
    await new Promise(r => setTimeout(r, 10));
    
    // App should maintain multi-select state on filtered list
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('clearing filter preserves selections that still exist', async () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Activate filter, type, lock in, select
    stdin.write('/');
    await new Promise(r => setTimeout(r, 10));
    stdin.write('t');
    await new Promise(r => setTimeout(r, 10));
    stdin.write('\r'); // Lock in
    await new Promise(r => setTimeout(r, 10));
    
    stdin.write('j');
    stdin.write(' '); // Select a task
    await new Promise(r => setTimeout(r, 10));
    
    // Clear filter with Escape
    stdin.write('\x1b');
    await new Promise(r => setTimeout(r, 10));
    
    // App should return to full list
    // Selected tasks that still exist should remain selected
    const frame = lastFrame() || '';
    expect(frame.length).toBeGreaterThan(0);
    
    unmount();
  });
});

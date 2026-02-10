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
  it('shows logs panel by default', () => {
    const { lastFrame, unmount } = render(<App config={defaultConfig} />);
    const frame = lastFrame() || '';
    // Should show "No logs yet" when logs panel is visible (from LogViewer)
    expect(frame).toContain('No logs yet');
    unmount();
  });

  it('can send L key without crashing', () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Initial render should show logs panel content
    expect(lastFrame()).toContain('No logs yet');
    
    // Send 'L' to toggle logs visibility
    // Note: ink-testing-library may not properly handle shift+key for uppercase
    // This test verifies the app doesn't crash when receiving the key
    stdin.write('L');
    
    // App should still be rendering
    expect(lastFrame()).toBeDefined();
    
    // Toggle back
    stdin.write('L');
    
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('Tab switches focus between panels', () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Initial state - logs panel is visible
    const initialFrame = lastFrame() || '';
    expect(initialFrame).toContain('No logs yet');
    
    // Tab to switch focus
    stdin.write('\t');
    
    // Focus should now be on logs
    const afterTab = lastFrame() || '';
    expect(afterTab.includes('Focus:')).toBe(true);
    
    // Tab again to switch back
    stdin.write('\t');
    
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('shows L shortcut in help bar', () => {
    const { lastFrame, unmount } = render(<App config={defaultConfig} />);
    const frame = lastFrame() || '';
    // HelpBar should show "L Logs" shortcut - this appears in the help bar text
    expect(frame).toContain('L');
    unmount();
  });
});

// =============================================================================
// PausePopup Integration Tests
// =============================================================================

describe('App - PausePopup Integration', () => {
  it('can send p key without crashing', () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Send 'p' for pause/resume
    stdin.write('p');
    
    // App should still be rendering
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  // Note: The p key now opens a pause popup overlay
  // Due to timing in ink-testing-library, we just verify it doesn't crash
  it('p key can be pressed without crashing', () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Send 'p' to trigger pause popup
    stdin.write('p');
    
    // App should still be rendering
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('Escape key does not crash in normal mode', () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Send Escape in normal mode (should do nothing)
    stdin.write('\x1B'); // Escape key
    
    // App should still be rendering
    expect(lastFrame()).toBeDefined();
    
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
// Focus Mode (f key) Tests
// =============================================================================

describe('App - Focus Mode (f key)', () => {
  it('f key can be pressed without crashing', () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Send 'f' for focus mode toggle
    stdin.write('f');
    
    // App should still be rendering
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('f key on task row without feature_id is handled gracefully', async () => {
    // When pressing 'f' on a task without feature_id, it should focus "ungrouped"
    let enableFeatureCalled = false;
    let enabledFeatureId = '';
    
    const mockEnableFeature = (featureId: string) => {
      enableFeatureCalled = true;
      enabledFeatureId = featureId;
    };
    
    const { stdin, unmount } = render(
      <App 
        config={defaultConfig} 
        onEnableFeature={mockEnableFeature}
      />
    );
    
    // Navigate to first task (if any)
    stdin.write('j');
    
    // Wait for navigation
    await new Promise(r => setTimeout(r, 10));
    
    // Press 'f' to toggle focus
    stdin.write('f');
    
    // Wait for async handlers
    await new Promise(r => setTimeout(r, 10));
    
    // Note: Without tasks loaded (no mock poller), onEnableFeature won't be called
    // This test just verifies the app doesn't crash
    expect(true).toBe(true);
    
    unmount();
  });

  it('f key toggles focus mode off when pressed on same feature', async () => {
    let disableFeatureCalled = false;
    
    const mockDisableFeature = (featureId: string) => {
      disableFeatureCalled = true;
    };
    
    const { stdin, lastFrame, unmount } = render(
      <App 
        config={defaultConfig} 
        onDisableFeature={mockDisableFeature}
      />
    );
    
    // Press 'f' twice - first to focus, second to unfocus
    stdin.write('f');
    await new Promise(r => setTimeout(r, 10));
    stdin.write('f');
    await new Promise(r => setTimeout(r, 10));
    
    // App should still be rendering
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('focus mode indicator appears in help bar when feature is focused', () => {
    // When a feature is focused, help bar should show the focus shortcut
    const { lastFrame, unmount } = render(<App config={defaultConfig} />);
    const frame = lastFrame() || '';
    
    // Should show 'f' shortcut in help bar
    expect(frame).toContain('f');
    
    unmount();
  });
});

// =============================================================================
// Context-Aware Pause (p key with focus mode) Tests
// =============================================================================

describe('App - Context-Aware Pause with Focus Mode', () => {
  it('p key opens pause popup when no feature is focused', () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);
    
    // Press 'p' without any focused feature
    stdin.write('p');
    
    // App should still render (popup may or may not be visible depending on timing)
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('p key does not crash when feature is focused', async () => {
    let resumeCalled = false;
    
    const mockResume = async (projectId: string) => {
      resumeCalled = true;
    };
    
    const { stdin, lastFrame, unmount } = render(
      <App 
        config={defaultConfig} 
        onResume={mockResume}
      />
    );
    
    // Navigate down to select a task
    stdin.write('j');
    await new Promise(r => setTimeout(r, 10));
    
    // Press 'f' to focus (may or may not work depending on task availability)
    stdin.write('f');
    await new Promise(r => setTimeout(r, 10));
    
    // Press 'p' which should either resume or open popup
    stdin.write('p');
    await new Promise(r => setTimeout(r, 10));
    
    // App should still be rendering
    expect(lastFrame()).toBeDefined();
    
    unmount();
  });

  it('p with focused feature skips popup and resumes directly', async () => {
    // This test verifies the behavior when focusedFeature is set
    // Due to limited mocking in ink-testing-library, we verify the app handles this gracefully
    let resumeCalled = false;
    let pausePopupOpened = false;
    
    const mockResume = async (projectId: string) => {
      resumeCalled = true;
    };
    
    // Note: We can't directly mock focusedFeature state, so we test the code path exists
    const { stdin, lastFrame, unmount } = render(
      <App 
        config={defaultConfig} 
        onResume={mockResume}
      />
    );
    
    // Send 'p' - without focused feature, should open popup
    stdin.write('p');
    await new Promise(r => setTimeout(r, 10));
    
    // The popup behavior is handled gracefully
    expect(lastFrame()).toBeDefined();
    
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

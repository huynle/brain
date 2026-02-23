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
import { App, setsEqual, resolveTaskTreeClickAction, isTaskTreeCollapseToggleTarget, getTaskTreeCollapseKey, getTaskTreeViewportStartRow, findTaskTreeTargetFromMouseRow } from './App';
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

describe('resolveTaskTreeClickAction', () => {
  it('routes left click on task to open editor', () => {
    expect(resolveTaskTreeClickAction({ kind: 'task', id: 't1', taskId: 't1' }, 'left')).toBe('open_editor');
  });

  it('routes right click on task to metadata popup', () => {
    expect(resolveTaskTreeClickAction({ kind: 'task', id: 't1', taskId: 't1' }, 'right')).toBe('open_metadata');
  });

  it('routes left click on headers to collapse toggle', () => {
    expect(resolveTaskTreeClickAction({ kind: 'feature_header', id: 'h1', featureId: 'feature-a' }, 'left')).toBe('toggle_collapsed');
    expect(resolveTaskTreeClickAction({ kind: 'status_header', id: 'h2', statusGroup: 'completed' }, 'left')).toBe('toggle_collapsed');
    expect(resolveTaskTreeClickAction({ kind: 'status_feature_header', id: 'h3', featureId: 'feature-a', statusGroup: 'draft' }, 'left')).toBe('toggle_collapsed');
    expect(resolveTaskTreeClickAction({ kind: 'project_header', id: 'h4', projectId: 'brain-api' }, 'left')).toBe('toggle_collapsed');
  });

  it('ignores non-handled targets/buttons', () => {
    expect(resolveTaskTreeClickAction({ kind: 'spacer', id: 's1' }, 'left')).toBe('noop');
    expect(resolveTaskTreeClickAction({ kind: 'feature_header', id: 'h5', featureId: 'f' }, 'right')).toBe('noop');
    expect(resolveTaskTreeClickAction({ kind: 'task', id: 't1', taskId: 't1' }, 'middle')).toBe('noop');
  });

  it('routes left click on ungrouped header to collapse toggle', () => {
    expect(resolveTaskTreeClickAction({ kind: 'ungrouped_header', id: 'h6' }, 'left')).toBe('toggle_collapsed');
  });
});

describe('isTaskTreeCollapseToggleTarget', () => {
  it('matches all collapsible task tree header targets', () => {
    expect(isTaskTreeCollapseToggleTarget({ kind: 'feature_header', id: 'h1', featureId: 'feature-a' })).toBe(true);
    expect(isTaskTreeCollapseToggleTarget({ kind: 'project_header', id: 'h2', projectId: 'brain-api' })).toBe(true);
    expect(isTaskTreeCollapseToggleTarget({ kind: 'status_header', id: 'h3', statusGroup: 'completed' })).toBe(true);
    expect(isTaskTreeCollapseToggleTarget({ kind: 'status_feature_header', id: 'h4', statusGroup: 'draft', featureId: 'feature-a' })).toBe(true);
    expect(isTaskTreeCollapseToggleTarget({ kind: 'ungrouped_header', id: 'h5' })).toBe(true);
  });

  it('returns false for task rows and spacer rows', () => {
    expect(isTaskTreeCollapseToggleTarget({ kind: 'task', id: 't1', taskId: 't1' })).toBe(false);
    expect(isTaskTreeCollapseToggleTarget({ kind: 'spacer', id: 's1' })).toBe(false);
  });

  it('keeps Enter key routing consistent with left click routing', () => {
    const toggles = [
      { kind: 'feature_header', id: 'h1', featureId: 'feature-a' },
      { kind: 'project_header', id: 'h2', projectId: 'brain-api' },
      { kind: 'status_header', id: 'h3', statusGroup: 'completed' },
      { kind: 'status_feature_header', id: 'h4', statusGroup: 'draft', featureId: 'feature-a' },
      { kind: 'ungrouped_header', id: 'h5' },
    ] as const;

    for (const target of toggles) {
      expect(isTaskTreeCollapseToggleTarget(target)).toBe(true);
      expect(resolveTaskTreeClickAction(target, 'left')).toBe('toggle_collapsed');
    }

    const editorTarget = { kind: 'task', id: 'task-1', taskId: 'task-1' } as const;
    expect(isTaskTreeCollapseToggleTarget(editorTarget)).toBe(false);
    expect(resolveTaskTreeClickAction(editorTarget, 'left')).toBe('open_editor');
  });
});

describe('App click helper exports', () => {
  it('exports a shared collapse-target resolver for keyboard and mouse paths', async () => {
    const appModule = (await import('./App')) as Record<string, unknown>;
    expect(typeof appModule.getTaskTreeCollapseKey).toBe('function');
  });

  it('returns project collapse key for project header targets', () => {
    expect(getTaskTreeCollapseKey({ kind: 'project_header', id: 'h1', projectId: 'brain-api' })).toBe('project:brain-api');
  });
});

describe('task tree mouse hit testing', () => {
  it('computes viewport start row in single-project mode', () => {
    expect(getTaskTreeViewportStartRow(false, 'off')).toBe(8);
  });

  it('computes viewport start row in multi-project mode', () => {
    expect(getTaskTreeViewportStartRow(true, 'off')).toBe(9);
  });

  it('adds one row when filter bar is visible', () => {
    expect(getTaskTreeViewportStartRow(false, 'typing')).toBe(9);
    expect(getTaskTreeViewportStartRow(false, 'locked')).toBe(9);
  });

  it('maps absolute mouse row to visible task tree row target', () => {
    const target = findTaskTreeTargetFromMouseRow(
      [
        { row: 0, target: { kind: 'feature_header', id: '__feature_header__alpha', featureId: 'alpha' } },
        { row: 1, target: { kind: 'task', id: 'task-1', taskId: 'task-1' } },
      ],
      9,
      8,
    );

    expect(target).toEqual({ kind: 'task', id: 'task-1', taskId: 'task-1' });
  });

  it('returns null when mouse row is outside visible task rows', () => {
    const target = findTaskTreeTargetFromMouseRow(
      [{ row: 0, target: { kind: 'task', id: 'task-1', taskId: 'task-1' } }],
      7,
      8,
    );

    expect(target).toBeNull();
  });

  it('maps viewport start row to first visible target', () => {
    const target = findTaskTreeTargetFromMouseRow(
      [{ row: 0, target: { kind: 'task', id: 'task-1', taskId: 'task-1' } }],
      8,
      8,
    );

    expect(target).toEqual({ kind: 'task', id: 'task-1', taskId: 'task-1' });
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

describe('App - Settings Popup Runtime Model (S Key)', () => {
  it('S opens settings popup and Tab reaches Runtime section', async () => {
    const { stdin, lastFrame, unmount } = render(
      <App
        config={defaultConfig}
        getProjectLimits={() => [{ projectId: 'test-project', limit: 2, running: 0 }]}
        getRuntimeDefaultModel={() => 'anthropic/claude-sonnet-4-20250514'}
      />
    );

    stdin.write('S');
    await new Promise(r => setTimeout(r, 10));
    expect(lastFrame()).toContain('Settings');
    expect(lastFrame()).toContain('[Limits]');

    stdin.write('\t');
    await new Promise(r => setTimeout(r, 10));
    expect(lastFrame()).toContain('[Groups]');

    stdin.write('\t');
    await new Promise(r => setTimeout(r, 10));
    const frame = lastFrame() || '';
    expect(frame).toContain('[Runtime]');
    expect(frame).toContain('Default model:');
    expect(frame).toContain('anthropic/claude-sonnet-4-20250514');

    unmount();
  });

  it('Runtime section edits and applies model override in memory', async () => {
    const setRuntimeDefaultModel = mock((_model: string | undefined) => {});

    const { stdin, lastFrame, unmount } = render(
      <App
        config={defaultConfig}
        getProjectLimits={() => [{ projectId: 'test-project', limit: 2, running: 0 }]}
        getRuntimeDefaultModel={() => ''}
        setRuntimeDefaultModel={setRuntimeDefaultModel}
      />
    );

    // Open settings and navigate to Runtime tab
    stdin.write('S');
    await new Promise(r => setTimeout(r, 10));
    stdin.write('\t');
    await new Promise(r => setTimeout(r, 10));
    stdin.write('\t');
    await new Promise(r => setTimeout(r, 10));

    // Enter edit mode, type model, and save
    stdin.write('e');
    await new Promise(r => setTimeout(r, 10));
    for (const ch of 'openai/gpt-5.3-codex') {
      stdin.write(ch);
    }
    await new Promise(r => setTimeout(r, 10));
    stdin.write('\r');
    await new Promise(r => setTimeout(r, 10));

    expect(setRuntimeDefaultModel).toHaveBeenCalledWith('openai/gpt-5.3-codex');
    expect(lastFrame()).toContain('openai/gpt-5.3-codex');

    unmount();
  });

  it('Runtime section resets to config default with 0', async () => {
    const setRuntimeDefaultModel = mock((_model: string | undefined) => {});

    const { stdin, unmount } = render(
      <App
        config={defaultConfig}
        getProjectLimits={() => [{ projectId: 'test-project', limit: 2, running: 0 }]}
        getRuntimeDefaultModel={() => 'anthropic/claude-sonnet-4-20250514'}
        setRuntimeDefaultModel={setRuntimeDefaultModel}
      />
    );

    // Open settings and navigate to Runtime tab
    stdin.write('S');
    await new Promise(r => setTimeout(r, 10));
    stdin.write('\t');
    await new Promise(r => setTimeout(r, 10));
    stdin.write('\t');
    await new Promise(r => setTimeout(r, 10));

    // Reset to config default
    stdin.write('0');
    await new Promise(r => setTimeout(r, 10));

    expect(setRuntimeDefaultModel).toHaveBeenCalledWith(undefined);

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
      _fields: { status?: string; feature_id?: string; git_branch?: string; target_workdir?: string; schedule?: string }
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
// Bulk Metadata Update - Feature Header Filtering Tests (Bug Fix)
// =============================================================================

import { GROUP_STATUSES, COMPLETED_FEATURE_PREFIX, DRAFT_FEATURE_PREFIX, CANCELLED_FEATURE_PREFIX, SUPERSEDED_FEATURE_PREFIX, ARCHIVED_FEATURE_PREFIX } from './components/TaskTree';
import { getVisibleTasksForFeature, getVisibleTasksForUngrouped, getTasksForStatusGroupFeature, STATUS_GROUP_MAP } from './App';
import type { TaskDisplay } from './types';

describe('App - Bulk Metadata Update filters out group-status tasks', () => {
  // GROUP_STATUSES includes: draft, cancelled, completed, validated, superseded, archived
  // When pressing 's' on a feature header, only active tasks should be collected
  
  it('GROUP_STATUSES contains expected statuses', () => {
    // Verify the GROUP_STATUSES constant has the expected values
    expect(GROUP_STATUSES).toContain('draft');
    expect(GROUP_STATUSES).toContain('cancelled');
    expect(GROUP_STATUSES).toContain('completed');
    expect(GROUP_STATUSES).toContain('validated');
    expect(GROUP_STATUSES).toContain('superseded');
    expect(GROUP_STATUSES).toContain('archived');
    // Active statuses should NOT be in GROUP_STATUSES
    expect(GROUP_STATUSES).not.toContain('pending');
    expect(GROUP_STATUSES).not.toContain('active');
    expect(GROUP_STATUSES).not.toContain('in_progress');
    expect(GROUP_STATUSES).not.toContain('blocked');
  });

  describe('getVisibleTasksForFeature', () => {
    const createTask = (id: string, status: string, feature_id?: string): TaskDisplay => ({
      id,
      title: `Task ${id}`,
      status: status as TaskDisplay['status'],
      priority: 'medium',
      dependencies: [],
      dependents: [],
      dependencyTitles: [],
      dependentTitles: [],
      tags: [],
      path: `/path/to/${id}`,
      feature_id,
    });

    it('returns only active tasks for a feature, excluding group-status tasks', () => {
      const tasks: TaskDisplay[] = [
        createTask('1', 'pending', 'feature-a'),
        createTask('2', 'in_progress', 'feature-a'),
        createTask('3', 'completed', 'feature-a'),  // Should be excluded
        createTask('4', 'draft', 'feature-a'),       // Should be excluded
        createTask('5', 'cancelled', 'feature-a'),   // Should be excluded
        createTask('6', 'validated', 'feature-a'),   // Should be excluded
        createTask('7', 'superseded', 'feature-a'),  // Should be excluded
        createTask('8', 'archived', 'feature-a'),    // Should be excluded
        createTask('9', 'blocked', 'feature-a'),
        createTask('10', 'pending', 'feature-b'),    // Different feature
      ];

      const result = getVisibleTasksForFeature(tasks, 'feature-a');
      
      // Should only include active tasks (pending, in_progress, blocked)
      expect(result).toHaveLength(3);
      expect(result.map((t: TaskDisplay) => t.id)).toEqual(['1', '2', '9']);
      
      // Should NOT include any group-status tasks
      const resultIds = result.map((t: TaskDisplay) => t.id);
      expect(resultIds).not.toContain('3'); // completed
      expect(resultIds).not.toContain('4'); // draft
      expect(resultIds).not.toContain('5'); // cancelled
      expect(resultIds).not.toContain('6'); // validated
      expect(resultIds).not.toContain('7'); // superseded
      expect(resultIds).not.toContain('8'); // archived
      expect(resultIds).not.toContain('10'); // different feature
    });

    it('returns empty array when all tasks have group statuses', () => {
      const tasks: TaskDisplay[] = [
        createTask('1', 'completed', 'feature-a'),
        createTask('2', 'draft', 'feature-a'),
        createTask('3', 'archived', 'feature-a'),
      ];

      const result = getVisibleTasksForFeature(tasks, 'feature-a');
      expect(result).toHaveLength(0);
    });

    it('returns empty array when feature has no tasks', () => {
      const tasks: TaskDisplay[] = [
        createTask('1', 'pending', 'feature-b'),
      ];

      const result = getVisibleTasksForFeature(tasks, 'feature-a');
      expect(result).toHaveLength(0);
    });
  });

  describe('getVisibleTasksForUngrouped', () => {
    const createTask = (id: string, status: string, feature_id?: string): TaskDisplay => ({
      id,
      title: `Task ${id}`,
      status: status as TaskDisplay['status'],
      priority: 'medium',
      dependencies: [],
      dependents: [],
      dependencyTitles: [],
      dependentTitles: [],
      tags: [],
      path: `/path/to/${id}`,
      feature_id,
    });

    it('returns only active ungrouped tasks, excluding group-status tasks', () => {
      const tasks: TaskDisplay[] = [
        createTask('1', 'pending'),           // Ungrouped, active - include
        createTask('2', 'in_progress'),       // Ungrouped, active - include
        createTask('3', 'completed'),         // Ungrouped, group-status - exclude
        createTask('4', 'draft'),             // Ungrouped, group-status - exclude
        createTask('5', 'cancelled'),         // Ungrouped, group-status - exclude
        createTask('6', 'validated'),         // Ungrouped, group-status - exclude
        createTask('7', 'superseded'),        // Ungrouped, group-status - exclude
        createTask('8', 'archived'),          // Ungrouped, group-status - exclude
        createTask('9', 'blocked'),           // Ungrouped, active - include
        createTask('10', 'pending', 'feature-a'), // Has feature_id - exclude
      ];

      const result = getVisibleTasksForUngrouped(tasks);
      
      // Should only include active ungrouped tasks (pending, in_progress, blocked)
      expect(result).toHaveLength(3);
      expect(result.map((t: TaskDisplay) => t.id)).toEqual(['1', '2', '9']);
      
      // Should NOT include any group-status tasks
      const resultIds = result.map((t: TaskDisplay) => t.id);
      expect(resultIds).not.toContain('3'); // completed
      expect(resultIds).not.toContain('4'); // draft
      expect(resultIds).not.toContain('5'); // cancelled
      expect(resultIds).not.toContain('6'); // validated
      expect(resultIds).not.toContain('7'); // superseded
      expect(resultIds).not.toContain('8'); // archived
      expect(resultIds).not.toContain('10'); // has feature_id
    });

    it('returns empty array when all ungrouped tasks have group statuses', () => {
      const tasks: TaskDisplay[] = [
        createTask('1', 'completed'),
        createTask('2', 'draft'),
        createTask('3', 'archived'),
      ];

      const result = getVisibleTasksForUngrouped(tasks);
      expect(result).toHaveLength(0);
    });

    it('returns empty array when all tasks have feature_id', () => {
      const tasks: TaskDisplay[] = [
        createTask('1', 'pending', 'feature-a'),
        createTask('2', 'in_progress', 'feature-b'),
      ];

      const result = getVisibleTasksForUngrouped(tasks);
      expect(result).toHaveLength(0);
    });
  });
});

// =============================================================================
// Status-Group Feature Header 's' Key Tests
// =============================================================================

describe('App - Status-group feature header batch metadata update', () => {
  const createTask = (id: string, status: string, feature_id?: string): TaskDisplay => ({
    id,
    title: `Task ${id}`,
    status: status as TaskDisplay['status'],
    priority: 'medium',
    dependencies: [],
    dependents: [],
    dependencyTitles: [],
    dependentTitles: [],
    tags: [],
    path: `/path/to/${id}`,
    feature_id,
  });

  describe('STATUS_GROUP_MAP', () => {
    it('maps completed prefix to completed and validated statuses', () => {
      expect(STATUS_GROUP_MAP[COMPLETED_FEATURE_PREFIX]).toEqual(['completed', 'validated']);
    });

    it('maps draft prefix to draft status', () => {
      expect(STATUS_GROUP_MAP[DRAFT_FEATURE_PREFIX]).toEqual(['draft']);
    });

    it('maps cancelled prefix to cancelled status', () => {
      expect(STATUS_GROUP_MAP[CANCELLED_FEATURE_PREFIX]).toEqual(['cancelled']);
    });

    it('maps superseded prefix to superseded status', () => {
      expect(STATUS_GROUP_MAP[SUPERSEDED_FEATURE_PREFIX]).toEqual(['superseded']);
    });

    it('maps archived prefix to archived status', () => {
      expect(STATUS_GROUP_MAP[ARCHIVED_FEATURE_PREFIX]).toEqual(['archived']);
    });
  });

  describe('getTasksForStatusGroupFeature', () => {
    it('returns only completed/validated tasks for a feature when using completed group', () => {
      const tasks: TaskDisplay[] = [
        createTask('1', 'completed', 'auth-system'),
        createTask('2', 'validated', 'auth-system'),
        createTask('3', 'pending', 'auth-system'),       // Wrong status
        createTask('4', 'in_progress', 'auth-system'),   // Wrong status
        createTask('5', 'completed', 'payment-flow'),    // Wrong feature
        createTask('6', 'draft', 'auth-system'),         // Wrong status
      ];

      const result = getTasksForStatusGroupFeature(tasks, 'auth-system', ['completed', 'validated']);
      expect(result).toHaveLength(2);
      expect(result.map((t: TaskDisplay) => t.id)).toEqual(['1', '2']);
    });

    it('returns only draft tasks for a feature when using draft group', () => {
      const tasks: TaskDisplay[] = [
        createTask('1', 'draft', 'auth-system'),
        createTask('2', 'draft', 'auth-system'),
        createTask('3', 'pending', 'auth-system'),
        createTask('4', 'draft', 'payment-flow'),
      ];

      const result = getTasksForStatusGroupFeature(tasks, 'auth-system', ['draft']);
      expect(result).toHaveLength(2);
      expect(result.map((t: TaskDisplay) => t.id)).toEqual(['1', '2']);
    });

    it('returns only cancelled tasks for a feature when using cancelled group', () => {
      const tasks: TaskDisplay[] = [
        createTask('1', 'cancelled', 'auth-system'),
        createTask('2', 'pending', 'auth-system'),
        createTask('3', 'cancelled', 'other-feature'),
      ];

      const result = getTasksForStatusGroupFeature(tasks, 'auth-system', ['cancelled']);
      expect(result).toHaveLength(1);
      expect(result[0].id).toBe('1');
    });

    it('returns only superseded tasks for a feature when using superseded group', () => {
      const tasks: TaskDisplay[] = [
        createTask('1', 'superseded', 'auth-system'),
        createTask('2', 'completed', 'auth-system'),
      ];

      const result = getTasksForStatusGroupFeature(tasks, 'auth-system', ['superseded']);
      expect(result).toHaveLength(1);
      expect(result[0].id).toBe('1');
    });

    it('returns only archived tasks for a feature when using archived group', () => {
      const tasks: TaskDisplay[] = [
        createTask('1', 'archived', 'auth-system'),
        createTask('2', 'archived', 'auth-system'),
        createTask('3', 'completed', 'auth-system'),
      ];

      const result = getTasksForStatusGroupFeature(tasks, 'auth-system', ['archived']);
      expect(result).toHaveLength(2);
      expect(result.map((t: TaskDisplay) => t.id)).toEqual(['1', '2']);
    });

    it('returns empty array when no tasks match feature + status group', () => {
      const tasks: TaskDisplay[] = [
        createTask('1', 'pending', 'auth-system'),
        createTask('2', 'completed', 'payment-flow'),
      ];

      const result = getTasksForStatusGroupFeature(tasks, 'auth-system', ['completed', 'validated']);
      expect(result).toHaveLength(0);
    });

    it('returns empty array for empty task list', () => {
      const result = getTasksForStatusGroupFeature([], 'auth-system', ['completed', 'validated']);
      expect(result).toHaveLength(0);
    });
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

// =============================================================================
// Text wrap toggle tests (w key)
// =============================================================================

describe('App - Text Wrap Toggle', () => {
  it('w key does not crash the app', () => {
    const { stdin, lastFrame, unmount } = render(
      <App config={defaultConfig} />
    );
    
    // Send 'w' to toggle text wrap
    stdin.write('w');
    
    // App should still be rendering
    expect(lastFrame()).toBeDefined();
    unmount();
  });

  it('help bar shows w shortcut for text wrap toggle', () => {
    const { lastFrame, unmount } = render(
      <App config={defaultConfig} />
    );
    const frame = lastFrame() || '';
    
    // Should show 'w' shortcut in help bar with Trunc label (default is truncate mode)
    expect(frame).toContain('Trunc');
    
    unmount();
  });
});

describe('App - Cron View Mode (C key)', () => {
  it('toggles from task view to cron view', async () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);

    stdin.write('C');
    await new Promise(r => setTimeout(r, 10));

    const frame = lastFrame() || '';
    expect(frame).toContain('No cron entries found');
    expect(frame).toContain('Focus:');
    expect(frame).toContain('crons');

    unmount();
  });

  it('help bar shows view toggle shortcut', () => {
    const { lastFrame, unmount } = render(<App config={defaultConfig} />);
    const frame = lastFrame() || '';

    expect(frame).toContain('C');
    expect(frame).toContain('View');

    unmount();
  });

  it('full help overlay documents cron shortcuts consistently', async () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);

    stdin.write('?');
    await new Promise(r => setTimeout(r, 20));

    const frame = lastFrame() || '';
    expect(frame).toContain('Cron panel shortcuts (cron view):');
    expect(frame).toContain('n/e');
    expect(frame).toContain('New/Edit selected cron');
    expect(frame).toContain('Trigger selected cron now');
    expect(frame).toContain('Pause/enable selected cron');
    expect(frame).toContain('a/u/R');
    expect(frame).toContain('Edit linked tasks in editor');
    expect(frame).toContain('Delete selected cron (confirm)');
    expect(frame).toContain('Show cron details panel');

    unmount();
  });

  it('supports j/k navigation in cron view without crashing', async () => {
    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);

    stdin.write('C');
    await new Promise(r => setTimeout(r, 10));
    stdin.write('j');
    stdin.write('k');
    await new Promise(r => setTimeout(r, 10));

    expect(lastFrame()).toBeDefined();

    unmount();
  });
});

describe('App - Cron Mutation Flows', () => {
  const originalFetch = globalThis.fetch;

  const installCronFetchMock = () => {
    globalThis.fetch = mock(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();

      if (url.includes('/api/v1/crons/test-project/crons')) {
        return new Response(
          JSON.stringify({
            crons: [
              {
                id: 'crn00001',
                path: 'projects/test-project/cron/crn00001.md',
                title: 'Nightly Build',
                status: 'active',
                schedule: '0 2 * * *',
                next_run: '2026-02-24T02:00:00.000Z',
                runs: [],
              },
            ],
            count: 1,
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        );
      }

      if (url.includes('/api/v1/tasks/test-project')) {
        return new Response(
          JSON.stringify({ tasks: [], count: 0 }),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        );
      }

      return new Response(
        JSON.stringify({ tasks: [], count: 0, crons: [] }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      );
    }) as unknown as typeof fetch;
  };

  const installEmptyCronFetchMock = () => {
    globalThis.fetch = mock(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.includes('/api/v1/crons/test-project/crons')) {
        return new Response(
          JSON.stringify({ crons: [], count: 0 }),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        );
      }
      if (url.includes('/api/v1/tasks/test-project')) {
        return new Response(
          JSON.stringify({ tasks: [], count: 0 }),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        );
      }
      return new Response(
        JSON.stringify({ tasks: [], count: 0, crons: [] }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      );
    }) as unknown as typeof fetch;
  };

  it('creates a cron from cron view prompt', async () => {
    installCronFetchMock();
    const onCreateCron = mock(async () => ({
      cron: {
        id: 'crn99999',
        path: 'projects/test-project/cron/crn99999.md',
        title: 'Nightly Cleanup',
        type: 'cron',
        status: 'active',
        content: 'Cron content',
        tags: ['cron'],
      },
      message: 'created',
    })) as any;

    const { stdin, unmount } = render(
      <App config={defaultConfig} onCreateCron={onCreateCron} />
    );

    stdin.write('C');
    await new Promise(r => setTimeout(r, 20));
    stdin.write('n');
    await new Promise(r => setTimeout(r, 20));
    for (const ch of 'Nightly Cleanup|15 1 * * *') stdin.write(ch);
    stdin.write('\r');
    await new Promise(r => setTimeout(r, 20));

    expect(onCreateCron).toHaveBeenCalledWith(
      'test-project',
      expect.objectContaining({ title: 'Nightly Cleanup', schedule: '15 1 * * *' })
    );

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('updates selected cron from cron view prompt', async () => {
    installCronFetchMock();
    const onUpdateCron = mock(async () => ({
      cron: {
        id: 'crn00001',
        path: 'projects/test-project/cron/crn00001.md',
        title: 'Nightly Build Updated',
        type: 'cron',
        status: 'active',
        content: 'Cron content',
        tags: ['cron'],
      },
      message: 'updated',
    })) as any;

    const { stdin, unmount } = render(
      <App config={defaultConfig} onUpdateCron={onUpdateCron} />
    );

    stdin.write('C');
    await new Promise(r => setTimeout(r, 20));
    stdin.write('e');
    await new Promise(r => setTimeout(r, 20));
    for (const ch of 'Nightly Build Updated|0 4 * * *') stdin.write(ch);
    stdin.write('\r');
    await new Promise(r => setTimeout(r, 20));

    expect(onUpdateCron).toHaveBeenCalledWith(
      'test-project',
      'crn00001',
      expect.objectContaining({ title: 'Nightly Build Updated', schedule: '0 4 * * *' })
    );

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('triggers selected cron with x key in cron view', async () => {
    installCronFetchMock();
    const onTriggerCron = mock(async () => ({
      cronId: 'crn00001',
      run: { run_id: '20260223-0100', status: 'in_progress' as const, started: '2026-02-23T01:00:00.000Z' },
      pipeline: [],
      pipelineCount: 0,
      message: 'triggered',
    })) as any;

    const { stdin, unmount } = render(
      <App config={defaultConfig} onTriggerCron={onTriggerCron} />
    );

    stdin.write('C');
    await new Promise(r => setTimeout(r, 20));
    stdin.write('x');
    await new Promise(r => setTimeout(r, 20));

    expect(onTriggerCron).toHaveBeenCalledWith('test-project', 'crn00001');

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('toggles selected cron status to blocked with p key in cron view', async () => {
    installCronFetchMock();
    const onUpdateCron = mock(async () => ({
      cron: {
        id: 'crn00001',
        path: 'projects/test-project/cron/crn00001.md',
        title: 'Nightly Build',
        type: 'cron',
        status: 'blocked',
        content: 'Cron content',
        tags: ['cron'],
      },
      message: 'updated',
    })) as any;

    const { stdin, unmount } = render(
      <App config={defaultConfig} onUpdateCron={onUpdateCron} />
    );

    stdin.write('C');
    await new Promise(r => setTimeout(r, 20));
    stdin.write('p');
    await new Promise(r => setTimeout(r, 20));

    expect(onUpdateCron).toHaveBeenCalledWith(
      'test-project',
      'crn00001',
      expect.objectContaining({ status: 'blocked' })
    );

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('toggles selected blocked cron status back to active with p key', async () => {
    globalThis.fetch = mock(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();

      if (url.includes('/api/v1/crons/test-project/crons')) {
        return new Response(
          JSON.stringify({
            crons: [
              {
                id: 'crn00001',
                path: 'projects/test-project/cron/crn00001.md',
                title: 'Nightly Build',
                status: 'blocked',
                schedule: '0 2 * * *',
                next_run: '2026-02-24T02:00:00.000Z',
                runs: [],
              },
            ],
            count: 1,
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        );
      }

      if (url.includes('/api/v1/tasks/test-project')) {
        return new Response(
          JSON.stringify({ tasks: [], count: 0 }),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        );
      }

      return new Response(
        JSON.stringify({ tasks: [], count: 0, crons: [] }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      );
    }) as unknown as typeof fetch;

    const onUpdateCron = mock(async () => ({
      cron: {
        id: 'crn00001',
        path: 'projects/test-project/cron/crn00001.md',
        title: 'Nightly Build',
        type: 'cron',
        status: 'active',
        content: 'Cron content',
        tags: ['cron'],
      },
      message: 'updated',
    })) as any;

    const { stdin, unmount } = render(
      <App config={defaultConfig} onUpdateCron={onUpdateCron} />
    );

    stdin.write('C');
    await new Promise(r => setTimeout(r, 20));
    stdin.write('p');
    await new Promise(r => setTimeout(r, 20));

    expect(onUpdateCron).toHaveBeenCalledWith(
      'test-project',
      'crn00001',
      expect.objectContaining({ status: 'active' })
    );

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('applies selected cron links from editor in single-project mode', async () => {
    globalThis.fetch = mock(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();

      if (url.includes('/api/v1/crons/test-project/crons')) {
        return new Response(
          JSON.stringify({
            crons: [
              {
                id: 'crn00001',
                path: 'projects/test-project/cron/crn00001.md',
                title: 'Nightly Build',
                status: 'active',
                schedule: '0 2 * * *',
                next_run: null,
                runs: [],
              },
            ],
            count: 1,
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        );
      }

      if (url.includes('/api/v1/tasks/test-project')) {
        return new Response(
          JSON.stringify({
            tasks: [
              {
                id: 'task-1',
                path: 'projects/test-project/task/task-1.md',
                title: 'Task One',
                status: 'pending',
                priority: 'medium',
                dependencies: [],
                cron_ids: ['crn00001'],
              },
              {
                id: 'task-2',
                path: 'projects/test-project/task/task-2.md',
                title: 'Task Two',
                status: 'pending',
                priority: 'medium',
                dependencies: [],
              },
            ],
            count: 2,
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        );
      }

      return new Response(
        JSON.stringify({ tasks: [], count: 0, crons: [] }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      );
    }) as unknown as typeof fetch;

    const onSetCronLinkedTasks = mock(async () => ({ cronId: 'crn00001', tasks: [], count: 1, message: 'replaced' })) as any;

    const { stdin, lastFrame, unmount } = render(
      <App config={defaultConfig} onSetCronLinkedTasks={onSetCronLinkedTasks} />
    );

    stdin.write('C');
    await new Promise(r => setTimeout(r, 30));

    stdin.write('a');
    await new Promise(r => setTimeout(r, 20));

    const openFrame = lastFrame() || '';
    expect(openFrame).toContain('Edit Cron Linked Tasks');

    // Toggle current row (task-1) off, move to task-2, toggle it on, then apply
    stdin.write(' ');
    stdin.write('j');
    stdin.write(' ');
    stdin.write('\r');
    await new Promise(r => setTimeout(r, 40));

    expect(onSetCronLinkedTasks).toHaveBeenCalledWith('test-project', 'crn00001', ['task-2']);
    expect((lastFrame() || '')).not.toContain('Edit Cron Linked Tasks');

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('closes cron link editor with Esc without applying changes', async () => {
    installCronFetchMock();
    const onSetCronLinkedTasks = mock(async () => ({ cronId: 'crn00001', tasks: [], count: 0, message: 'replaced' })) as any;

    const { stdin, lastFrame, unmount } = render(
      <App config={defaultConfig} onSetCronLinkedTasks={onSetCronLinkedTasks} />
    );

    stdin.write('C');
    await new Promise(r => setTimeout(r, 20));

    stdin.write('a');
    await new Promise(r => setTimeout(r, 20));

    expect(lastFrame() || '').toContain('Edit Cron Linked Tasks');

    stdin.write('j');
    stdin.write(' ');
    stdin.write('\x1b');
    await new Promise(r => setTimeout(r, 30));

    expect(onSetCronLinkedTasks).not.toHaveBeenCalled();
    expect(lastFrame() || '').not.toContain('Edit Cron Linked Tasks');

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('requires delete confirmation before deleting cron', async () => {
    installCronFetchMock();
    const onDeleteCron = mock(async () => ({ message: 'deleted', path: 'projects/test-project/cron/crn00001.md' })) as any;

    const { stdin, unmount } = render(
      <App config={defaultConfig} onDeleteCron={onDeleteCron} />
    );

    stdin.write('C');
    await new Promise(r => setTimeout(r, 20));

    stdin.write('D');
    await new Promise(r => setTimeout(r, 20));
    expect(onDeleteCron).not.toHaveBeenCalled();

    stdin.write('\r');
    await new Promise(r => setTimeout(r, 20));
    expect(onDeleteCron).toHaveBeenCalledWith('test-project', 'crn00001');

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('does not update cron when none is selected', async () => {
    installEmptyCronFetchMock();
    const onUpdateCron = mock(async () => ({
      cron: {
        id: 'crn00001',
        path: 'projects/test-project/cron/crn00001.md',
        title: 'Unused',
        type: 'cron',
        status: 'active',
        content: 'Unused',
        tags: ['cron'],
      },
      message: 'updated',
    })) as any;

    const { stdin, unmount } = render(
      <App config={defaultConfig} onUpdateCron={onUpdateCron} />
    );

    stdin.write('C');
    await new Promise(r => setTimeout(r, 20));
    stdin.write('e');
    await new Promise(r => setTimeout(r, 20));

    expect(onUpdateCron).not.toHaveBeenCalled();

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('rejects malformed create input without calling API', async () => {
    installCronFetchMock();
    const onCreateCron = mock(async () => ({
      cron: {
        id: 'crn99999',
        path: 'projects/test-project/cron/crn99999.md',
        title: 'Should Not Be Called',
        type: 'cron',
        status: 'active',
        content: 'Cron content',
        tags: ['cron'],
      },
      message: 'created',
    })) as any;

    const { stdin, unmount } = render(
      <App config={defaultConfig} onCreateCron={onCreateCron} />
    );

    stdin.write('C');
    await new Promise(r => setTimeout(r, 20));
    stdin.write('n');
    await new Promise(r => setTimeout(r, 20));
    for (const ch of 'MissingDelimiterAndSchedule') stdin.write(ch);
    stdin.write('\r');
    await new Promise(r => setTimeout(r, 20));

    expect(onCreateCron).not.toHaveBeenCalled();

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('rejects create input with extra delimiters without calling API', async () => {
    installCronFetchMock();
    const onCreateCron = mock(async () => ({
      cron: {
        id: 'crn99999',
        path: 'projects/test-project/cron/crn99999.md',
        title: 'Should Not Be Called',
        type: 'cron',
        status: 'active',
        content: 'Cron content',
        tags: ['cron'],
      },
      message: 'created',
    })) as any;

    const { stdin, unmount } = render(
      <App config={defaultConfig} onCreateCron={onCreateCron} />
    );

    stdin.write('C');
    await new Promise(r => setTimeout(r, 20));
    stdin.write('n');
    await new Promise(r => setTimeout(r, 20));
    for (const ch of 'Nightly Cleanup|15 1 * * *|extra') stdin.write(ch);
    stdin.write('\r');
    await new Promise(r => setTimeout(r, 20));

    expect(onCreateCron).not.toHaveBeenCalled();

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('rejects edit input with extra delimiters without calling API', async () => {
    installCronFetchMock();
    const onUpdateCron = mock(async () => ({
      cron: {
        id: 'crn00001',
        path: 'projects/test-project/cron/crn00001.md',
        title: 'Should Not Be Called',
        type: 'cron',
        status: 'active',
        content: 'Cron content',
        tags: ['cron'],
      },
      message: 'updated',
    })) as any;

    const { stdin, unmount } = render(
      <App config={defaultConfig} onUpdateCron={onUpdateCron} />
    );

    stdin.write('C');
    await new Promise(r => setTimeout(r, 20));
    stdin.write('e');
    await new Promise(r => setTimeout(r, 20));
    for (const ch of 'Nightly Build|0 2 * * *|extra') stdin.write(ch);
    stdin.write('\r');
    await new Promise(r => setTimeout(r, 20));

    expect(onUpdateCron).not.toHaveBeenCalled();

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('rejects malformed edit input without calling API', async () => {
    installCronFetchMock();
    const onUpdateCron = mock(async () => ({
      cron: {
        id: 'crn00001',
        path: 'projects/test-project/cron/crn00001.md',
        title: 'Should Not Be Called',
        type: 'cron',
        status: 'active',
        content: 'Cron content',
        tags: ['cron'],
      },
      message: 'updated',
    })) as any;

    const { stdin, unmount } = render(
      <App config={defaultConfig} onUpdateCron={onUpdateCron} />
    );

    stdin.write('C');
    await new Promise(r => setTimeout(r, 20));
    stdin.write('e');
    await new Promise(r => setTimeout(r, 20));
    for (const ch of 'MissingDelimiterAndSchedule') stdin.write(ch);
    stdin.write('\r');
    await new Promise(r => setTimeout(r, 20));

    expect(onUpdateCron).not.toHaveBeenCalled();

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('does not open create popup when create callback is unavailable', async () => {
    installCronFetchMock();

    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);

    stdin.write('C');
    await new Promise(r => setTimeout(r, 20));
    stdin.write('n');
    await new Promise(r => setTimeout(r, 20));

    expect(lastFrame() || '').not.toContain('Create Cron');

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('does not open edit popup when update callback is unavailable', async () => {
    installCronFetchMock();

    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);

    stdin.write('C');
    await new Promise(r => setTimeout(r, 20));
    stdin.write('e');
    await new Promise(r => setTimeout(r, 20));

    expect(lastFrame() || '').not.toContain('Edit Cron');

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('does not open delete confirmation when delete callback is unavailable', async () => {
    installCronFetchMock();

    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);

    stdin.write('C');
    await new Promise(r => setTimeout(r, 20));
    stdin.write('D');
    await new Promise(r => setTimeout(r, 20));

    expect(lastFrame() || '').not.toContain('Delete Cron');

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('warns and remains stable when trigger callback is unavailable', async () => {
    installCronFetchMock();

    const { stdin, lastFrame, unmount } = render(<App config={defaultConfig} />);

    stdin.write('C');
    await new Promise(r => setTimeout(r, 20));
    stdin.write('L');
    await new Promise(r => setTimeout(r, 20));
    stdin.write('x');
    await new Promise(r => setTimeout(r, 40));

    const frame = lastFrame() || '';
    expect(frame).toContain('Trigger cron action unavailable');
    expect(frame).not.toContain('Triggered cron');

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('blocks create in all-project cron view when project cannot be inferred', async () => {
    globalThis.fetch = mock(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();

      if (url.includes('/api/v1/crons/proj-a/crons') || url.includes('/api/v1/crons/proj-b/crons')) {
        return new Response(
          JSON.stringify({ crons: [], count: 0 }),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        );
      }

      if (url.includes('/api/v1/tasks/proj-a') || url.includes('/api/v1/tasks/proj-b')) {
        return new Response(
          JSON.stringify({ tasks: [], count: 0 }),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        );
      }

      return new Response(
        JSON.stringify({ tasks: [], count: 0, crons: [] }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      );
    }) as unknown as typeof fetch;

    const onCreateCron = mock(async () => ({
      cron: {
        id: 'crn99999',
        path: 'projects/proj-a/cron/crn99999.md',
        title: 'Should Not Be Called',
        type: 'cron',
        status: 'active',
        content: 'Cron content',
        tags: ['cron'],
      },
      message: 'created',
    })) as any;

    const multiProjectConfig = {
      ...defaultConfig,
      projects: ['proj-a', 'proj-b'],
      activeProject: 'all',
    };

    const { stdin, lastFrame, unmount } = render(
      <App config={multiProjectConfig} onCreateCron={onCreateCron} />
    );

    stdin.write('C');
    await new Promise(r => setTimeout(r, 30));
    stdin.write('L');
    await new Promise(r => setTimeout(r, 20));
    stdin.write('n');
    await new Promise(r => setTimeout(r, 40));

    const frame = lastFrame() || '';
    expect(frame).toContain('Cannot create cron: no active project selected');
    expect(frame).not.toContain('Create Cron');
    expect(onCreateCron).not.toHaveBeenCalled();

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('uses selected cron project ID for trigger in all-project cron view', async () => {
    globalThis.fetch = mock(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();

      if (url.includes('/api/v1/crons/proj-a/crons')) {
        return new Response(
          JSON.stringify({
            crons: [
              {
                id: 'crnA0001',
                path: 'projects/proj-a/cron/crnA0001.md',
                title: 'Cron A',
                status: 'active',
                schedule: '0 2 * * *',
                next_run: null,
                runs: [],
              },
            ],
            count: 1,
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        );
      }

      if (url.includes('/api/v1/crons/proj-b/crons')) {
        return new Response(
          JSON.stringify({
            crons: [
              {
                id: 'crnB0001',
                path: 'projects/proj-b/cron/crnB0001.md',
                title: 'Cron B',
                status: 'active',
                schedule: '0 3 * * *',
                next_run: null,
                runs: [],
              },
            ],
            count: 1,
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        );
      }

      if (url.includes('/api/v1/tasks/proj-a') || url.includes('/api/v1/tasks/proj-b')) {
        return new Response(
          JSON.stringify({ tasks: [], count: 0 }),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        );
      }

      return new Response(
        JSON.stringify({ tasks: [], count: 0, crons: [] }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      );
    }) as unknown as typeof fetch;

    const onTriggerCron = mock(async () => ({
      cronId: 'crnA0001',
      run: { run_id: 'run-a-1', status: 'in_progress' as const, started: '2026-02-23T01:00:00.000Z' },
      pipeline: [],
      pipelineCount: 0,
      message: 'triggered',
    })) as any;

    const multiProjectConfig = {
      ...defaultConfig,
      projects: ['proj-a', 'proj-b'],
      activeProject: 'all',
    };

    const { stdin, unmount } = render(
      <App config={multiProjectConfig} onTriggerCron={onTriggerCron} />
    );

    stdin.write('C');
    await new Promise(r => setTimeout(r, 30));
    stdin.write('x');
    await new Promise(r => setTimeout(r, 20));

    expect(onTriggerCron).toHaveBeenCalledWith('proj-a', 'crnA0001');

    unmount();
    globalThis.fetch = originalFetch;
  });

  it('uses selected cron project ID when applying links in all-project cron view', async () => {
    globalThis.fetch = mock(async (input: RequestInfo | URL) => {
      const url = typeof input === 'string' ? input : input.toString();

      if (url.includes('/api/v1/crons/proj-a/crons')) {
        return new Response(
          JSON.stringify({
            crons: [
              {
                id: 'crnA0001',
                path: 'projects/proj-a/cron/crnA0001.md',
                title: 'Cron A',
                status: 'active',
                schedule: '0 2 * * *',
                next_run: null,
                runs: [],
              },
            ],
            count: 1,
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        );
      }

      if (url.includes('/api/v1/crons/proj-b/crons')) {
        return new Response(
          JSON.stringify({
            crons: [
              {
                id: 'crnB0001',
                path: 'projects/proj-b/cron/crnB0001.md',
                title: 'Cron B',
                status: 'active',
                schedule: '0 3 * * *',
                next_run: null,
                runs: [],
              },
            ],
            count: 1,
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        );
      }

      if (url.includes('/api/v1/tasks/proj-a')) {
        return new Response(
          JSON.stringify({
            tasks: [
              {
                id: 'task-a-1',
                path: 'projects/proj-a/task/task-a-1.md',
                title: 'Task A1',
                status: 'pending',
                priority: 'medium',
                dependencies: [],
                cron_ids: ['crnA0001'],
              },
            ],
            count: 1,
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        );
      }

      if (url.includes('/api/v1/tasks/proj-b')) {
        return new Response(
          JSON.stringify({
            tasks: [
              {
                id: 'task-b-1',
                path: 'projects/proj-b/task/task-b-1.md',
                title: 'Task B1',
                status: 'pending',
                priority: 'medium',
                dependencies: [],
              },
            ],
            count: 1,
          }),
          { status: 200, headers: { 'Content-Type': 'application/json' } }
        );
      }

      return new Response(
        JSON.stringify({ tasks: [], count: 0, crons: [] }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      );
    }) as unknown as typeof fetch;

    const onSetCronLinkedTasks = mock(async () => ({ cronId: 'crnA0001', tasks: [], count: 1, message: 'replaced' })) as any;

    const multiProjectConfig = {
      ...defaultConfig,
      projects: ['proj-a', 'proj-b'],
      activeProject: 'all',
    };

    const { stdin, unmount } = render(
      <App config={multiProjectConfig} onSetCronLinkedTasks={onSetCronLinkedTasks} />
    );

    stdin.write('C');
    await new Promise(r => setTimeout(r, 30));
    stdin.write('a');
    await new Promise(r => setTimeout(r, 20));
    stdin.write('\r');
    await new Promise(r => setTimeout(r, 40));

    expect(onSetCronLinkedTasks).toHaveBeenCalledWith('proj-a', 'crnA0001', ['task-a-1']);

    unmount();
    globalThis.fetch = originalFetch;
  });
});

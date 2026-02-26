import { describe, expect, it, mock } from 'bun:test';
import {
  COMPLETED_FEATURE_PREFIX,
  FEATURE_HEADER_PREFIX,
  parseTaskTreeRowTarget,
} from './components/TaskTree';
import {
  DEFAULT_FEATURE_CHECKOUT_OPTIONS,
  resolveFeatureCheckoutOptionsForSelection,
  triggerFeatureCheckoutFromSelection,
  type FeatureCheckoutResult,
} from './App';
import type { TaskDisplay } from './types';

type LogEntry = { level: string; message: string };

function isCheckoutEligibleRow(rowId: string): boolean {
  const target = parseTaskTreeRowTarget(rowId);
  return target.kind === 'feature_header' && Boolean(target.featureId);
}

function formatCheckoutSuccessMessage(result: FeatureCheckoutResult): string {
  return `${result.created ? 'Created' : 'Reused'} checkout task: ${result.taskId} - ${result.taskTitle}`;
}

async function flushPromises(): Promise<void> {
  await Promise.resolve();
  await Promise.resolve();
}

function createTaskDisplay(overrides: Partial<TaskDisplay>): TaskDisplay {
  return {
    id: 'task-1',
    path: 'projects/project-a/task/task-1.md',
    title: 'Task 1',
    status: 'pending',
    priority: 'medium',
    created: '2026-01-01T00:00:00.000Z',
    modified: '2026-01-01T00:00:00.000Z',
    type: 'task',
    ...overrides,
  } as TaskDisplay;
}

describe('App feature checkout row gating', () => {
  it('allows action for feature header rows', () => {
    expect(isCheckoutEligibleRow(`${FEATURE_HEADER_PREFIX}feature-alpha`)).toBe(true);
  });

  it('no-ops for non-feature rows (task + status-group feature)', () => {
    expect(isCheckoutEligibleRow('task-1')).toBe(false);
    expect(isCheckoutEligibleRow(`${COMPLETED_FEATURE_PREFIX}feature-alpha`)).toBe(false);
  });
});

describe('App feature checkout success logging', () => {
  it('formats created task success message with task id/title', () => {
    const message = formatCheckoutSuccessMessage({
      created: true,
      taskId: 'abc12def',
      taskTitle: 'Run feature checkout',
    });

    expect(message).toBe('Created checkout task: abc12def - Run feature checkout');
  });

  it('treats created=false as success and formats reused message', () => {
    const message = formatCheckoutSuccessMessage({
      created: false,
      taskId: 'xyz98abc',
      taskTitle: 'Run feature checkout',
    });

    expect(message).toBe('Reused checkout task: xyz98abc - Run feature checkout');
    expect(message.includes('Failed to mark feature checkout')).toBe(false);
  });
});

describe('App feature checkout trigger behavior', () => {
  it('uses feature task metadata when resolving checkout options', () => {
    const options = resolveFeatureCheckoutOptionsForSelection({
      selectedTaskId: `${FEATURE_HEADER_PREFIX}feature-alpha`,
      isMultiProject: false,
      activeProject: 'ignored-in-single-project',
      project: 'project-a',
      tasks: [
        createTaskDisplay({
          feature_id: 'feature-alpha',
          projectId: 'project-a',
          gitBranch: 'feature/alpha',
          mergeTargetBranch: 'develop',
          executionMode: 'current_branch',
          mergePolicy: 'auto_pr',
          mergeStrategy: 'rebase',
          openPrBeforeMerge: true,
        }),
      ],
    });

    expect(options).toEqual({
      execution_branch: 'feature/alpha',
      merge_target_branch: 'develop',
      execution_mode: 'current_branch',
      merge_policy: 'auto_pr',
      merge_strategy: 'rebase',
      open_pr_before_merge: true,
    });
  });

  it('falls back to canonical defaults when feature has no active task metadata', () => {
    const options = resolveFeatureCheckoutOptionsForSelection({
      selectedTaskId: `${FEATURE_HEADER_PREFIX}feature-alpha`,
      isMultiProject: false,
      activeProject: 'ignored-in-single-project',
      project: 'project-a',
      tasks: [
        createTaskDisplay({
          feature_id: 'feature-beta',
          projectId: 'project-a',
        }),
      ],
    });

    expect(options).toEqual(DEFAULT_FEATURE_CHECKOUT_OPTIONS);
  });

  it('returns false for non-feature rows and does not call callback', () => {
    const onMarkFeatureForCheckout = mock(async () => ({
      created: true,
      taskId: 'abc12def',
      taskTitle: 'Run feature checkout',
    }));
    const logs: LogEntry[] = [];

    const handled = triggerFeatureCheckoutFromSelection({
      selectedTaskId: 'task-1',
      isMultiProject: false,
      activeProject: 'test-project',
      project: 'test-project',
      onMarkFeatureForCheckout,
      addLog: (entry: LogEntry) => logs.push(entry),
    });

    expect(handled).toBe(false);
    expect(onMarkFeatureForCheckout).toHaveBeenCalledTimes(0);
    expect(logs).toHaveLength(0);
  });

  it('logs warning when callback is unavailable', () => {
    const logs: LogEntry[] = [];

    const handled = triggerFeatureCheckoutFromSelection({
      selectedTaskId: `${FEATURE_HEADER_PREFIX}feature-alpha`,
      isMultiProject: false,
      activeProject: 'test-project',
      project: 'test-project',
      addLog: (entry: LogEntry) => logs.push(entry),
    });

    expect(handled).toBe(true);
    expect(logs).toEqual([
      { level: 'warn', message: 'Feature checkout action unavailable' },
    ]);
  });

  it('logs warning and skips callback when multi-project tab is all', () => {
    const onMarkFeatureForCheckout = mock(async () => ({
      created: true,
      taskId: 'abc12def',
      taskTitle: 'Run feature checkout',
    }));
    const logs: LogEntry[] = [];

    const handled = triggerFeatureCheckoutFromSelection({
      selectedTaskId: `${FEATURE_HEADER_PREFIX}feature-alpha`,
      isMultiProject: true,
      activeProject: 'all',
      project: 'test-project',
      onMarkFeatureForCheckout,
      addLog: (entry: LogEntry) => logs.push(entry),
    });

    expect(handled).toBe(true);
    expect(onMarkFeatureForCheckout).toHaveBeenCalledTimes(0);
    expect(logs).toEqual([
      {
        level: 'warn',
        message: 'Select a specific project tab before marking feature checkout',
      },
    ]);
  });

  it('invokes callback with project/feature and logs created success', async () => {
    const onMarkFeatureForCheckout = mock(async () => ({
      created: true,
      taskId: 'abc12def',
      taskTitle: 'Run feature checkout',
    }));
    const logs: LogEntry[] = [];

    const handled = triggerFeatureCheckoutFromSelection({
      selectedTaskId: `${FEATURE_HEADER_PREFIX}feature-alpha`,
      isMultiProject: false,
      activeProject: 'ignored-in-single-project',
      project: 'project-a',
      onMarkFeatureForCheckout,
      addLog: (entry: LogEntry) => logs.push(entry),
    });

    await flushPromises();

    expect(handled).toBe(true);
    expect(onMarkFeatureForCheckout).toHaveBeenCalledTimes(1);
    expect(onMarkFeatureForCheckout).toHaveBeenCalledWith(
      'project-a',
      'feature-alpha',
      DEFAULT_FEATURE_CHECKOUT_OPTIONS
    );
    expect(logs).toEqual([
      { level: 'info', message: 'Marking feature for checkout: feature-alpha' },
      {
        level: 'info',
        message: 'Created checkout task: abc12def - Run feature checkout',
      },
    ]);
  });

  it('logs reused success when callback returns created=false', async () => {
    const onMarkFeatureForCheckout = mock(async () => ({
      created: false,
      taskId: 'xyz98abc',
      taskTitle: 'Existing feature checkout',
    }));
    const logs: LogEntry[] = [];

    triggerFeatureCheckoutFromSelection({
      selectedTaskId: `${FEATURE_HEADER_PREFIX}feature-alpha`,
      isMultiProject: false,
      activeProject: 'ignored-in-single-project',
      project: 'project-a',
      onMarkFeatureForCheckout,
      addLog: (entry: LogEntry) => logs.push(entry),
    });

    await flushPromises();

    expect(logs[1]).toEqual({
      level: 'info',
      message: 'Reused checkout task: xyz98abc - Existing feature checkout',
    });
  });

  it('logs error when callback rejects', async () => {
    const onMarkFeatureForCheckout = mock(async () => {
      throw new Error('boom');
    });
    const logs: LogEntry[] = [];

    triggerFeatureCheckoutFromSelection({
      selectedTaskId: `${FEATURE_HEADER_PREFIX}feature-alpha`,
      isMultiProject: false,
      activeProject: 'ignored-in-single-project',
      project: 'project-a',
      onMarkFeatureForCheckout,
      addLog: (entry: LogEntry) => logs.push(entry),
    });

    await flushPromises();

    expect(logs[0]).toEqual({
      level: 'info',
      message: 'Marking feature for checkout: feature-alpha',
    });
    expect(logs[1]).toEqual({
      level: 'error',
      message: 'Failed to mark feature checkout: Error: boom',
    });
  });

  it('passes explicit checkout options to callback when provided', async () => {
    const onMarkFeatureForCheckout = mock(async () => ({
      created: true,
      taskId: 'abc12def',
      taskTitle: 'Run feature checkout',
    }));
    const logs: LogEntry[] = [];

    const checkoutOptions = {
      execution_branch: 'feature/alpha',
      merge_target_branch: 'develop',
      execution_mode: 'worktree' as const,
      merge_policy: 'auto_pr' as const,
      merge_strategy: 'rebase' as const,
      open_pr_before_merge: true,
    };

    triggerFeatureCheckoutFromSelection({
      selectedTaskId: `${FEATURE_HEADER_PREFIX}feature-alpha`,
      isMultiProject: false,
      activeProject: 'ignored-in-single-project',
      project: 'project-a',
      onMarkFeatureForCheckout,
      checkoutOptions,
      addLog: (entry: LogEntry) => logs.push(entry),
    });

    await flushPromises();

    expect(onMarkFeatureForCheckout).toHaveBeenCalledWith(
      'project-a',
      'feature-alpha',
      checkoutOptions
    );
  });

  it('surfaces checkout validation error messages from API payload', async () => {
    const onMarkFeatureForCheckout = mock(async () => {
      throw new Error(
        'API Error (400): {"error":"Validation Error","message":"execution_branch must be different from merge_target_branch"}'
      );
    });
    const logs: LogEntry[] = [];

    triggerFeatureCheckoutFromSelection({
      selectedTaskId: `${FEATURE_HEADER_PREFIX}feature-alpha`,
      isMultiProject: false,
      activeProject: 'ignored-in-single-project',
      project: 'project-a',
      onMarkFeatureForCheckout,
      addLog: (entry: LogEntry) => logs.push(entry),
    });

    await flushPromises();

    expect(logs[1]).toEqual({
      level: 'error',
      message:
        'Failed to mark feature checkout: execution_branch must be different from merge_target_branch',
    });
  });
});

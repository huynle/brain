import { describe, expect, it } from 'bun:test';
import { isSseDebugLoggingEnabled, normalizeTaskSSEEvent } from './taskSseEvents';

describe('taskSseEvents', () => {
  it('normalizes connected events', () => {
    const event = normalizeTaskSSEEvent({
      event: 'connected',
      data: JSON.stringify({
        type: 'connected',
        transport: 'sse',
        timestamp: '2026-02-23T10:00:00.000Z',
        projectId: 'brain-api',
      }),
    });

    expect(event).toEqual({
      type: 'connected',
      transport: 'sse',
      timestamp: '2026-02-23T10:00:00.000Z',
      projectId: 'brain-api',
    });
  });

  it('normalizes tasks_snapshot payloads to TUISSEEvent shape', () => {
    const event = normalizeTaskSSEEvent({
      event: 'tasks_snapshot',
      data: JSON.stringify({
        type: 'tasks_snapshot',
        transport: 'sse',
        timestamp: '2026-02-23T10:00:00.000Z',
        projectId: 'brain-api',
        stats: { ready: 2, waiting: 1, blocked: 1, total: 7, not_pending: 3 },
        tasks: [
          {
            id: 'task-a',
            path: 'projects/brain-api/task/task-a.md',
            title: 'Task A',
            status: 'pending',
            priority: 'high',
            resolved_deps: ['task-b'],
            git_branch: 'feature/task-a',
            merge_target_branch: 'main',
            merge_policy: 'auto_merge',
            merge_strategy: 'squash',
            remote_branch_policy: 'keep',
            open_pr_before_merge: true,
            execution_mode: 'in_branch',

          },
          {
            id: 'task-b',
            path: 'projects/brain-api/task/task-b.md',
            title: 'Task B',
            status: 'in_progress',
            priority: 'medium',
          },
          {
            id: 'task-c',
            path: 'projects/brain-api/task/task-c.md',
            title: 'Task C',
            status: 'validated',
            priority: 'low',
          },
        ],
      }),
    });

    expect(event?.type).toBe('tasks_snapshot');
    if (!event || event.type !== 'tasks_snapshot') {
      throw new Error('Expected tasks_snapshot event');
    }

    expect(event.stats).toEqual({
      ready: 2,
      waiting: 1,
      blocked: 1,
      inProgress: 1,
      completed: 1,
    });
    expect(event.tasks).toHaveLength(3);
    expect(event.tasks[0]).toMatchObject({
      id: 'task-a',
      dependencies: ['task-b'],
      dependencyTitles: ['Task B'],
      dependents: [],
      dependentTitles: [],
      tags: [],
      gitBranch: 'feature/task-a',
      mergeTargetBranch: 'main',
      mergePolicy: 'auto_merge',
      mergeStrategy: 'squash',
      remoteBranchPolicy: 'keep',
      openPrBeforeMerge: true,
      executionMode: 'current_branch',

    });
  });

  it('uses fallback project id for events without projectId', () => {
    const event = normalizeTaskSSEEvent({
      event: 'heartbeat',
      data: JSON.stringify({
        type: 'heartbeat',
        transport: 'sse',
        timestamp: '2026-02-23T10:00:00.000Z',
      }),
      fallbackProjectId: 'brain-api',
    });

    expect(event).toEqual({
      type: 'heartbeat',
      transport: 'sse',
      timestamp: '2026-02-23T10:00:00.000Z',
      projectId: 'brain-api',
    });
  });

  it('derives dependents and indirect ancestor titles like poller shaping', () => {
    const event = normalizeTaskSSEEvent({
      event: 'tasks_snapshot',
      data: JSON.stringify({
        type: 'tasks_snapshot',
        timestamp: '2026-02-23T10:00:00.000Z',
        projectId: 'brain-api',
        tasks: [
          {
            id: 'task-a',
            path: 'projects/brain-api/task/task-a.md',
            title: 'Task A',
            status: 'pending',
            resolved_deps: ['task-b'],
          },
          {
            id: 'task-b',
            path: 'projects/brain-api/task/task-b.md',
            title: 'Task B',
            status: 'pending',
            resolved_deps: ['task-c'],
          },
          {
            id: 'task-c',
            path: 'projects/brain-api/task/task-c.md',
            title: 'Task C',
            status: 'pending',
          },
        ],
      }),
    });

    if (!event || event.type !== 'tasks_snapshot') {
      throw new Error('Expected tasks_snapshot event');
    }

    const taskA = event.tasks.find((task) => task.id === 'task-a');
    const taskB = event.tasks.find((task) => task.id === 'task-b');
    const taskC = event.tasks.find((task) => task.id === 'task-c');

    expect(taskA?.indirectAncestorTitles).toEqual(['Task C']);
    expect(taskA?.dependents).toEqual([]);
    expect(taskB?.dependents).toEqual(['task-a']);
    expect(taskB?.dependentTitles).toEqual(['Task A']);
    expect(taskC?.dependents).toEqual(['task-b']);
    expect(taskC?.dependentTitles).toEqual(['Task B']);
  });

  it('passes through task frontmatter in tasks_snapshot events', () => {
    const event = normalizeTaskSSEEvent({
      event: 'tasks_snapshot',
      data: JSON.stringify({
        type: 'tasks_snapshot',
        timestamp: '2026-02-23T10:00:00.000Z',
        projectId: 'brain-api',
        tasks: [
          {
            id: 'task-a',
            path: 'projects/brain-api/task/task-a.md',
            title: 'Task A',
            status: 'pending',
            frontmatter: {
              custom_string: 'keep-me',
              custom_nested: { enabled: true },
            },
          },
        ],
      }),
    });

    if (!event || event.type !== 'tasks_snapshot') {
      throw new Error('Expected tasks_snapshot event');
    }

    expect(event.tasks[0].frontmatter).toEqual({
      custom_string: 'keep-me',
      custom_nested: { enabled: true },
    });
  });

  it('returns null for unknown events or invalid payloads', () => {
    expect(
      normalizeTaskSSEEvent({
        event: 'unknown',
        data: '{}',
      })
    ).toBeNull();

    expect(
      normalizeTaskSSEEvent({
        event: 'connected',
        data: '{not-json}',
      })
    ).toBeNull();
  });

  it('keeps sync SSE debug logging disabled by default', () => {
    const originalValue = process.env.BRAIN_TUI_SSE_DEBUG;
    delete process.env.BRAIN_TUI_SSE_DEBUG;

    try {
      expect(isSseDebugLoggingEnabled()).toBe(false);

      process.env.BRAIN_TUI_SSE_DEBUG = '1';
      expect(isSseDebugLoggingEnabled()).toBe(true);
    } finally {
      if (originalValue === undefined) {
        delete process.env.BRAIN_TUI_SSE_DEBUG;
      } else {
        process.env.BRAIN_TUI_SSE_DEBUG = originalValue;
      }
    }
  });
});

import React, { useEffect } from 'react';
import { afterEach, beforeEach, describe, expect, it, mock, vi } from 'bun:test';
import { render } from 'ink-testing-library';
import { Text } from 'ink';
import { useTaskSse } from './useTaskSse';

type EventListenerMap = Map<string, Set<EventListener>>;

class MockEventSource {
  static instances: MockEventSource[] = [];

  readonly url: string;
  readonly listeners: EventListenerMap = new Map();
  onerror: ((this: EventSource, ev: Event) => any) | null = null;
  isClosed = false;

  constructor(url: string | URL) {
    this.url = String(url);
    MockEventSource.instances.push(this);
  }

  addEventListener(type: string, listener: EventListener): void {
    const listeners = this.listeners.get(type) ?? new Set<EventListener>();
    listeners.add(listener);
    this.listeners.set(type, listeners);
  }

  removeEventListener(type: string, listener: EventListener): void {
    this.listeners.get(type)?.delete(listener);
  }

  close(): void {
    this.isClosed = true;
  }

  emit(type: string, payload: unknown): void {
    const listeners = this.listeners.get(type);
    if (!listeners) {
      return;
    }

    const messageEvent = { data: JSON.stringify(payload) } as MessageEvent<string>;
    for (const listener of listeners) {
      listener(messageEvent as unknown as Event);
    }
  }

  static reset(): void {
    MockEventSource.instances = [];
  }
}

const originalEventSource = globalThis.EventSource;
const originalFetch = globalThis.fetch;

function createSnapshotPayload(projectId: string) {
  return {
    tasks: [
      {
        id: `${projectId}-task-1`,
        title: 'Task 1',
        status: 'pending',
      },
    ],
    stats: {
      ready: 1,
      waiting: 0,
      blocked: 0,
      inProgress: 0,
      completed: 0,
    },
  };
}

function flushPromises(): Promise<void> {
  return Promise.resolve().then(() => {});
}

function TaskSseProbe(props: {
  projectId: string;
  apiUrl: string;
  pollInterval: number;
  inactivityTimeoutMs: number;
  reconnectDelayMs: number;
  onState: (state: ReturnType<typeof useTaskSse>) => void;
}) {
  const state = useTaskSse({
    projectId: props.projectId,
    apiUrl: props.apiUrl,
    pollInterval: props.pollInterval,
    inactivityTimeoutMs: props.inactivityTimeoutMs,
    reconnectDelayMs: props.reconnectDelayMs,
  });

  useEffect(() => {
    props.onState(state);
  }, [props, state]);

  return React.createElement(Text, null, state.isConnected ? 'connected' : 'disconnected');
}

describe('useTaskSse - runtime fallback/reconnect', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    MockEventSource.reset();
    globalThis.EventSource = MockEventSource as unknown as typeof EventSource;
  });

  afterEach(() => {
    vi.useRealTimers();
    globalThis.fetch = originalFetch;
    globalThis.EventSource = originalEventSource;
    MockEventSource.reset();
  });

  it('falls back to polling on SSE error, reconnects, and stops fallback on healthy signal', async () => {
    let fetchCalls = 0;

    globalThis.fetch = mock(async () => {
      fetchCalls += 1;
      return {
        ok: true,
        status: 200,
        statusText: 'OK',
        json: async () => createSnapshotPayload('demo'),
      } as unknown as Response;
    }) as unknown as typeof fetch;

    const rendered = render(
      React.createElement(TaskSseProbe, {
        projectId: 'demo',
        apiUrl: 'http://localhost:3333',
        pollInterval: 1000,
        inactivityTimeoutMs: 10000,
        reconnectDelayMs: 2000,
        onState: () => {},
      })
    );

    const firstSource = MockEventSource.instances[0];
    expect(firstSource).toBeDefined();

    firstSource?.onerror?.call(firstSource as unknown as EventSource, new Event('error'));
    await flushPromises();

    expect(fetchCalls).toBe(1);

    vi.advanceTimersByTime(1000);
    await flushPromises();
    expect(fetchCalls).toBe(2);

    vi.advanceTimersByTime(2000);
    const reconnectedSource = MockEventSource.instances[1];
    expect(reconnectedSource).toBeDefined();

    reconnectedSource?.emit('connected', {
      type: 'connected',
      transport: 'sse',
      timestamp: '2026-02-23T12:00:00.000Z',
      projectId: 'demo',
    });
    await flushPromises();

    const callsAfterHealthySignal = fetchCalls;
    vi.advanceTimersByTime(3000);
    await flushPromises();

    expect(fetchCalls).toBe(callsAfterHealthySignal);

    rendered.unmount();
  });
});

describe('useTaskSse - reducer', () => {
  let taskSseReducer: typeof import('./useTaskSse').taskSseReducer;

  beforeEach(async () => {
    const mod = await import('./useTaskSse');
    taskSseReducer = mod.taskSseReducer;
  });

  const initialState = {
    tasks: [],
    stats: { total: 0, ready: 0, waiting: 0, blocked: 0, inProgress: 0, completed: 0 },
    isLoading: true,
    isConnected: false,
    error: null,
    isUsingPollingFallback: false,
  };

  it('applies snapshot updates and clears errors', () => {
    const tasks = [
      {
        id: 'task-1',
        path: 'projects/demo/task/task-1.md',
        title: 'Task 1',
        status: 'pending' as const,
        priority: 'medium' as const,
        tags: [],
        dependencies: [],
        dependents: [],
        dependencyTitles: [],
        dependentTitles: [],
      },
    ];

    const result = taskSseReducer(initialState, {
      type: 'SNAPSHOT_SUCCESS',
      tasks,
      stats: { total: 1, ready: 1, waiting: 0, blocked: 0, inProgress: 0, completed: 0 },
      fromPollingFallback: false,
    });

    expect(result.tasks).toEqual(tasks);
    expect(result.stats.total).toBe(1);
    expect(result.isLoading).toBe(false);
    expect(result.isConnected).toBe(true);
    expect(result.error).toBeNull();
    expect(result.isUsingPollingFallback).toBe(false);
  });

  it('preserves stale data on connection errors', () => {
    const stateWithData = {
      tasks: [
        {
          id: 'task-1',
          path: 'projects/demo/task/task-1.md',
          title: 'Task 1',
          status: 'pending' as const,
          priority: 'medium' as const,
          tags: [],
          dependencies: [],
          dependents: [],
          dependencyTitles: [],
          dependentTitles: [],
        },
      ],
      stats: { total: 1, ready: 1, waiting: 0, blocked: 0, inProgress: 0, completed: 0 },
      isLoading: false,
      isConnected: true,
      error: null,
      isUsingPollingFallback: false,
    };

    const result = taskSseReducer(stateWithData, {
      type: 'CONNECTION_ERROR',
      error: new Error('SSE disconnected'),
      usePollingFallback: true,
    });

    expect(result.tasks).toEqual(stateWithData.tasks);
    expect(result.stats).toEqual(stateWithData.stats);
    expect(result.isConnected).toBe(false);
    expect(result.isUsingPollingFallback).toBe(true);
    expect(result.error?.message).toContain('SSE disconnected');
  });
});

describe('useTaskSse - transport helpers', () => {
  let buildTaskStreamUrl: typeof import('./useTaskSse').buildTaskStreamUrl;
  let isSseInactive: typeof import('./useTaskSse').isSseInactive;
  let shouldUsePollingFallback: typeof import('./useTaskSse').shouldUsePollingFallback;

  beforeEach(async () => {
    const mod = await import('./useTaskSse');
    buildTaskStreamUrl = mod.buildTaskStreamUrl;
    isSseInactive = mod.isSseInactive;
    shouldUsePollingFallback = mod.shouldUsePollingFallback;
  });

  it('builds encoded task stream URL', () => {
    expect(buildTaskStreamUrl('http://localhost:3333', 'my project/test')).toBe(
      'http://localhost:3333/api/v1/tasks/my%20project%2Ftest/stream'
    );
  });

  it('detects inactivity only after threshold', () => {
    expect(isSseInactive(10_000, 14_999, 5_000)).toBe(false);
    expect(isSseInactive(10_000, 15_001, 5_000)).toBe(true);
  });

  it('enters polling fallback when SSE is unavailable', () => {
    expect(
      shouldUsePollingFallback({
        hasEventSource: false,
        hasConnectedEvent: false,
        hasRecentActivity: false,
      })
    ).toBe(true);
  });

  it('enters polling fallback when SSE becomes unhealthy', () => {
    expect(
      shouldUsePollingFallback({
        hasEventSource: true,
        hasConnectedEvent: true,
        hasRecentActivity: false,
      })
    ).toBe(true);
  });

  it('stays on SSE when connected and healthy', () => {
    expect(
      shouldUsePollingFallback({
        hasEventSource: true,
        hasConnectedEvent: true,
        hasRecentActivity: true,
      })
    ).toBe(false);
  });
});

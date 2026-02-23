import React, { useEffect } from 'react';
import { afterEach, beforeEach, describe, expect, it, mock, vi } from 'bun:test';
import { render } from 'ink-testing-library';
import { Text } from 'ink';
import { useMultiProjectSse } from './useMultiProjectSse';

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
        title: `${projectId} Task 1`,
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

function MultiProjectSseProbe(props: {
  projects: string[];
  apiUrl: string;
  pollInterval: number;
  inactivityTimeoutMs: number;
  reconnectDelayMs: number;
  onState: (state: ReturnType<typeof useMultiProjectSse>) => void;
}) {
  const state = useMultiProjectSse({
    projects: props.projects,
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

describe('useMultiProjectSse - runtime fallback/reconnect', () => {
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

  it('uses per-project polling fallback on inactivity and stops fallback after heartbeat', async () => {
    const fetchUrls: string[] = [];

    globalThis.fetch = mock(async (input: RequestInfo | URL) => {
      const url = String(input);
      fetchUrls.push(url);
      const projectId = decodeURIComponent(url.split('/api/v1/tasks/')[1]?.split('?')[0] ?? 'unknown');

      return {
        ok: true,
        status: 200,
        statusText: 'OK',
        json: async () => createSnapshotPayload(projectId),
      } as unknown as Response;
    }) as unknown as typeof fetch;

    const rendered = render(
      React.createElement(MultiProjectSseProbe, {
        projects: ['alpha', 'beta'],
        apiUrl: 'http://localhost:3333',
        pollInterval: 1000,
        inactivityTimeoutMs: 3000,
        reconnectDelayMs: 2000,
        onState: () => {},
      })
    );

    const alphaSource = MockEventSource.instances.find((source) => source.url.includes('/alpha/stream'));
    const betaSource = MockEventSource.instances.find((source) => source.url.includes('/beta/stream'));
    expect(alphaSource).toBeDefined();
    expect(betaSource).toBeDefined();

    alphaSource?.emit('connected', {
      type: 'connected',
      transport: 'sse',
      timestamp: '2026-02-23T12:00:00.000Z',
      projectId: 'alpha',
    });
    await flushPromises();

    vi.advanceTimersByTime(5000);
    await flushPromises();

    expect(fetchUrls.some((url) => url.includes('/api/v1/tasks/alpha'))).toBe(true);

    vi.advanceTimersByTime(2000);
    const reconnectedAlphaSource = MockEventSource.instances
      .filter((source) => source.url.includes('/alpha/stream'))
      .at(-1);
    expect(reconnectedAlphaSource).toBeDefined();

    reconnectedAlphaSource?.emit('heartbeat', {
      type: 'heartbeat',
      transport: 'sse',
      timestamp: '2026-02-23T12:00:08.000Z',
      projectId: 'alpha',
    });
    await flushPromises();

    const alphaPollCountAfterHeartbeat = fetchUrls.filter((url) => url.includes('/api/v1/tasks/alpha')).length;

    vi.advanceTimersByTime(900);
    await flushPromises();

    const alphaPollCountAfterWait = fetchUrls.filter((url) => url.includes('/api/v1/tasks/alpha')).length;
    expect(alphaPollCountAfterWait).toBe(alphaPollCountAfterHeartbeat);

    rendered.unmount();
  });
});

describe('useMultiProjectSse - reducer', () => {
  let multiProjectSseReducer: typeof import('./useMultiProjectSse').multiProjectSseReducer;

  beforeEach(async () => {
    const mod = await import('./useMultiProjectSse');
    multiProjectSseReducer = mod.multiProjectSseReducer;
  });

  const initialState = {
    tasksByProject: new Map(),
    statsByProject: new Map(),
    connectionByProject: new Map(),
    errorsByProject: new Map(),
    pollingFallbackByProject: new Map(),
    isLoading: true,
  };

  it('applies per-project snapshot updates and clears project errors', () => {
    const result = multiProjectSseReducer(initialState, {
      type: 'PROJECT_SNAPSHOT_SUCCESS',
      projectId: 'alpha',
      tasks: [
        {
          id: 'task-1',
          path: 'projects/alpha/task/task-1.md',
          title: 'Task 1',
          status: 'pending' as const,
          priority: 'medium' as const,
          tags: [],
          dependencies: [],
          dependents: [],
          dependencyTitles: [],
          dependentTitles: [],
          projectId: 'alpha',
        },
      ],
      stats: { total: 1, ready: 1, waiting: 0, blocked: 0, inProgress: 0, completed: 0 },
      fromPollingFallback: false,
    });

    expect(result.tasksByProject.get('alpha')).toHaveLength(1);
    expect(result.statsByProject.get('alpha')?.total).toBe(1);
    expect(result.connectionByProject.get('alpha')).toBe(true);
    expect(result.errorsByProject.has('alpha')).toBe(false);
    expect(result.pollingFallbackByProject.get('alpha')).toBe(false);
    expect(result.isLoading).toBe(false);
  });

  it('preserves stale data for one project when that stream fails', () => {
    const stateWithData = {
      ...initialState,
      isLoading: false,
      tasksByProject: new Map([
        [
          'beta',
          [
            {
              id: 'task-2',
              path: 'projects/beta/task/task-2.md',
              title: 'Task 2',
              status: 'pending' as const,
              priority: 'medium' as const,
              tags: [],
              dependencies: [],
              dependents: [],
              dependencyTitles: [],
              dependentTitles: [],
              projectId: 'beta',
            },
          ],
        ],
      ]),
      statsByProject: new Map([
        ['beta', { total: 1, ready: 1, waiting: 0, blocked: 0, inProgress: 0, completed: 0 }],
      ]),
      connectionByProject: new Map([['beta', true]]),
      errorsByProject: new Map(),
      pollingFallbackByProject: new Map([['beta', false]]),
    };

    const result = multiProjectSseReducer(stateWithData, {
      type: 'PROJECT_CONNECTION_ERROR',
      projectId: 'beta',
      error: new Error('SSE disconnected for beta'),
      usePollingFallback: true,
    });

    expect(result.tasksByProject.get('beta')).toEqual(stateWithData.tasksByProject.get('beta'));
    expect(result.statsByProject.get('beta')).toEqual(stateWithData.statsByProject.get('beta'));
    expect(result.connectionByProject.get('beta')).toBe(false);
    expect(result.errorsByProject.get('beta')?.message).toContain('SSE disconnected for beta');
    expect(result.pollingFallbackByProject.get('beta')).toBe(true);
  });
});

describe('useMultiProjectSse - transport helpers', () => {
  let buildProjectTaskStreamUrl: typeof import('./useMultiProjectSse').buildProjectTaskStreamUrl;
  let shouldUseProjectPollingFallback: typeof import('./useMultiProjectSse').shouldUseProjectPollingFallback;

  beforeEach(async () => {
    const mod = await import('./useMultiProjectSse');
    buildProjectTaskStreamUrl = mod.buildProjectTaskStreamUrl;
    shouldUseProjectPollingFallback = mod.shouldUseProjectPollingFallback;
  });

  it('builds encoded stream URL per project', () => {
    expect(buildProjectTaskStreamUrl('http://localhost:3333', 'my project/test')).toBe(
      'http://localhost:3333/api/v1/tasks/my%20project%2Ftest/stream'
    );
  });

  it('enters per-project fallback when EventSource is unavailable', () => {
    expect(
      shouldUseProjectPollingFallback({
        hasEventSource: false,
        hasConnectedEvent: false,
        hasRecentActivity: false,
      })
    ).toBe(true);
  });

  it('enters per-project fallback when stream is stale', () => {
    expect(
      shouldUseProjectPollingFallback({
        hasEventSource: true,
        hasConnectedEvent: true,
        hasRecentActivity: false,
      })
    ).toBe(true);
  });

  it('keeps per-project stream on SSE when healthy', () => {
    expect(
      shouldUseProjectPollingFallback({
        hasEventSource: true,
        hasConnectedEvent: true,
        hasRecentActivity: true,
      })
    ).toBe(false);
  });
});

import { useCallback, useEffect, useReducer, useRef } from 'react';
import type { ProjectStats, TaskDisplay, TUISSEEvent } from '../types';
import { normalizeTaskSSEEvent } from './taskSseEvents';
import type { TaskStats, UseTaskPollerResult } from './useTaskPoller';

export interface UseTaskSseOptions {
  projectId: string;
  apiUrl: string;
  pollInterval?: number;
  inactivityTimeoutMs?: number;
  reconnectDelayMs?: number;
  enabled?: boolean;
}

export interface TaskSseState {
  tasks: TaskDisplay[];
  stats: TaskStats;
  isLoading: boolean;
  isConnected: boolean;
  error: Error | null;
  isUsingPollingFallback: boolean;
}

export type TaskSseAction =
  | { type: 'FETCH_START' }
  | { type: 'SNAPSHOT_SUCCESS'; tasks: TaskDisplay[]; stats: TaskStats; fromPollingFallback: boolean }
  | { type: 'CONNECTION_ERROR'; error: Error; usePollingFallback: boolean }
  | { type: 'SSE_HEALTHY' };

const DEFAULT_POLL_INTERVAL = 1000;
const DEFAULT_INACTIVITY_TIMEOUT_MS = 65_000;
const DEFAULT_RECONNECT_DELAY_MS = 5_000;

const EMPTY_STATS: TaskStats = {
  total: 0,
  ready: 0,
  waiting: 0,
  blocked: 0,
  inProgress: 0,
  completed: 0,
};

const INITIAL_STATE: TaskSseState = {
  tasks: [],
  stats: EMPTY_STATS,
  isLoading: true,
  isConnected: false,
  error: null,
  isUsingPollingFallback: false,
};

export function taskSseReducer(state: TaskSseState, action: TaskSseAction): TaskSseState {
  switch (action.type) {
    case 'FETCH_START':
      return { ...state, isLoading: true };
    case 'SNAPSHOT_SUCCESS':
      return {
        tasks: action.tasks,
        stats: action.stats,
        isLoading: false,
        isConnected: true,
        error: null,
        isUsingPollingFallback: action.fromPollingFallback,
      };
    case 'CONNECTION_ERROR':
      return {
        ...state,
        isLoading: false,
        isConnected: false,
        error: action.error,
        isUsingPollingFallback: action.usePollingFallback,
      };
    case 'SSE_HEALTHY':
      return {
        ...state,
        isLoading: false,
        isConnected: true,
        error: null,
        isUsingPollingFallback: false,
      };
  }
}

export function buildTaskStreamUrl(apiUrl: string, projectId: string): string {
  return `${apiUrl}/api/v1/tasks/${encodeURIComponent(projectId)}/stream`;
}

export function isSseInactive(lastActivityAt: number, now: number, inactivityTimeoutMs: number): boolean {
  return now - lastActivityAt > inactivityTimeoutMs;
}

export function shouldUsePollingFallback(params: {
  hasEventSource: boolean;
  hasConnectedEvent: boolean;
  hasRecentActivity: boolean;
}): boolean {
  if (!params.hasEventSource) {
    return true;
  }

  if (params.hasConnectedEvent && !params.hasRecentActivity) {
    return true;
  }

  return false;
}

function toTaskStats(stats: ProjectStats['stats'], tasks: TaskDisplay[]): TaskStats {
  return {
    total: tasks.length,
    ready: stats.ready,
    waiting: stats.waiting,
    blocked: stats.blocked,
    inProgress: stats.inProgress,
    completed: stats.completed,
  };
}

function extractSnapshot(event: TUISSEEvent | null): { tasks: TaskDisplay[]; stats: TaskStats } | null {
  if (!event || event.type !== 'tasks_snapshot') {
    return null;
  }

  return {
    tasks: event.tasks,
    stats: toTaskStats(event.stats, event.tasks),
  };
}

function normalizeSnapshotEventFromApiResponse(projectId: string, payload: unknown): { tasks: TaskDisplay[]; stats: TaskStats } {
  const event = normalizeTaskSSEEvent({
    event: 'tasks_snapshot',
    data: JSON.stringify({
      type: 'tasks_snapshot',
      transport: 'sse',
      timestamp: new Date().toISOString(),
      projectId,
      ...(typeof payload === 'object' && payload !== null ? payload : {}),
    }),
    fallbackProjectId: projectId,
  });

  const snapshot = extractSnapshot(event);
  if (!snapshot) {
    throw new Error('Invalid task snapshot response');
  }
  return snapshot;
}

export function useTaskSse(options: UseTaskSseOptions): UseTaskPollerResult {
  const {
    projectId,
    apiUrl,
    pollInterval = DEFAULT_POLL_INTERVAL,
    inactivityTimeoutMs = DEFAULT_INACTIVITY_TIMEOUT_MS,
    reconnectDelayMs = DEFAULT_RECONNECT_DELAY_MS,
    enabled = true,
  } = options;

  const [state, dispatch] = useReducer(taskSseReducer, INITIAL_STATE);

  const isInitialFetchRef = useRef(true);
  const eventSourceRef = useRef<EventSource | null>(null);
  const pollingTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const inactivityTimerRef = useRef<ReturnType<typeof setInterval> | null>(null);
  const hasConnectedEventRef = useRef(false);
  const lastSseActivityAtRef = useRef<number | null>(null);
  const usingPollingFallbackRef = useRef(false);
  const isUnmountedRef = useRef(false);

  const clearPollingTimer = useCallback(() => {
    if (pollingTimerRef.current) {
      clearInterval(pollingTimerRef.current);
      pollingTimerRef.current = null;
    }
  }, []);

  const clearReconnectTimer = useCallback(() => {
    if (reconnectTimerRef.current) {
      clearTimeout(reconnectTimerRef.current);
      reconnectTimerRef.current = null;
    }
  }, []);

  const closeEventSource = useCallback(() => {
    if (eventSourceRef.current) {
      eventSourceRef.current.close();
      eventSourceRef.current = null;
    }
  }, []);

  const fetchSnapshotViaPolling = useCallback(
    async (fromPollingFallback: boolean) => {
      try {
        const response = await fetch(`${apiUrl}/api/v1/tasks/${encodeURIComponent(projectId)}`);
        if (!response.ok) {
          throw new Error(`API error: ${response.status} ${response.statusText}`);
        }

        const data = await response.json();
        const snapshot = normalizeSnapshotEventFromApiResponse(projectId, data);

        dispatch({
          type: 'SNAPSHOT_SUCCESS',
          tasks: snapshot.tasks,
          stats: snapshot.stats,
          fromPollingFallback,
        });
      } catch (err) {
        const error = err instanceof Error ? err : new Error('Unknown polling error');
        dispatch({ type: 'CONNECTION_ERROR', error, usePollingFallback: true });
      } finally {
        isInitialFetchRef.current = false;
      }
    },
    [apiUrl, projectId]
  );

  const startPollingFallback = useCallback(
    (reason: Error) => {
      usingPollingFallbackRef.current = true;
      dispatch({ type: 'CONNECTION_ERROR', error: reason, usePollingFallback: true });

      if (!pollingTimerRef.current) {
        void fetchSnapshotViaPolling(true);
        pollingTimerRef.current = setInterval(() => {
          void fetchSnapshotViaPolling(true);
        }, pollInterval);
      }
    },
    [fetchSnapshotViaPolling, pollInterval]
  );

  const stopPollingFallback = useCallback(() => {
    usingPollingFallbackRef.current = false;
    clearPollingTimer();
  }, [clearPollingTimer]);

  const scheduleReconnect = useCallback(
    (connect: () => void) => {
      if (reconnectTimerRef.current || isUnmountedRef.current) {
        return;
      }
      reconnectTimerRef.current = setTimeout(() => {
        reconnectTimerRef.current = null;
        connect();
      }, reconnectDelayMs);
    },
    [reconnectDelayMs]
  );

  useEffect(() => {
    if (!enabled) {
      return;
    }

    isUnmountedRef.current = false;
    isInitialFetchRef.current = true;
    hasConnectedEventRef.current = false;
    lastSseActivityAtRef.current = null;
    usingPollingFallbackRef.current = false;
    dispatch({ type: 'FETCH_START' });

    const connectSse = () => {
      if (isUnmountedRef.current) {
        return;
      }

      if (typeof EventSource === 'undefined') {
        startPollingFallback(new Error('EventSource is unavailable in this runtime'));
        return;
      }

      closeEventSource();

      const eventSource = new EventSource(buildTaskStreamUrl(apiUrl, projectId));
      eventSourceRef.current = eventSource;

      const onConnected = (messageEvent: MessageEvent<string>) => {
        const event = normalizeTaskSSEEvent({
          event: 'connected',
          data: messageEvent.data,
          fallbackProjectId: projectId,
        });
        if (!event || event.type !== 'connected') {
          return;
        }

        hasConnectedEventRef.current = true;
        lastSseActivityAtRef.current = Date.now();
        stopPollingFallback();
        dispatch({ type: 'SSE_HEALTHY' });
      };

      const onHeartbeat = (messageEvent: MessageEvent<string>) => {
        const event = normalizeTaskSSEEvent({
          event: 'heartbeat',
          data: messageEvent.data,
          fallbackProjectId: projectId,
        });
        if (!event || event.type !== 'heartbeat') {
          return;
        }

        lastSseActivityAtRef.current = Date.now();
        stopPollingFallback();
        dispatch({ type: 'SSE_HEALTHY' });
      };

      const onSnapshot = (messageEvent: MessageEvent<string>) => {
        const event = normalizeTaskSSEEvent({
          event: 'tasks_snapshot',
          data: messageEvent.data,
          fallbackProjectId: projectId,
        });

        const snapshot = extractSnapshot(event);
        if (!snapshot) {
          return;
        }

        lastSseActivityAtRef.current = Date.now();
        stopPollingFallback();
        dispatch({
          type: 'SNAPSHOT_SUCCESS',
          tasks: snapshot.tasks,
          stats: snapshot.stats,
          fromPollingFallback: false,
        });
      };

      const onServerErrorEvent = (messageEvent: MessageEvent<string>) => {
        const event = normalizeTaskSSEEvent({
          event: 'error',
          data: messageEvent.data,
          fallbackProjectId: projectId,
        });

        const message = event && event.type === 'error'
          ? event.message
          : 'SSE stream reported an error';

        startPollingFallback(new Error(message));
        scheduleReconnect(connectSse);
      };

      const onTransportError = () => {
        startPollingFallback(new Error('SSE connection error'));
        scheduleReconnect(connectSse);
      };

      eventSource.addEventListener('connected', onConnected as EventListener);
      eventSource.addEventListener('heartbeat', onHeartbeat as EventListener);
      eventSource.addEventListener('tasks_snapshot', onSnapshot as EventListener);
      eventSource.addEventListener('error', onServerErrorEvent as EventListener);
      eventSource.onerror = onTransportError;
    };

    connectSse();

    const inactivityCheckMs = Math.max(1000, Math.floor(inactivityTimeoutMs / 2));
    inactivityTimerRef.current = setInterval(() => {
      if (isUnmountedRef.current) {
        return;
      }

      const hasEventSource = typeof EventSource !== 'undefined';
      const lastActivityAt = lastSseActivityAtRef.current;
      const hasRecentActivity =
        typeof lastActivityAt === 'number'
          ? !isSseInactive(lastActivityAt, Date.now(), inactivityTimeoutMs)
          : false;

      if (
        shouldUsePollingFallback({
          hasEventSource,
          hasConnectedEvent: hasConnectedEventRef.current,
          hasRecentActivity,
        })
      ) {
        closeEventSource();
        startPollingFallback(new Error('SSE stream inactive; using polling fallback'));
        scheduleReconnect(connectSse);
      }
    }, inactivityCheckMs);

    return () => {
      isUnmountedRef.current = true;

      closeEventSource();
      clearPollingTimer();
      clearReconnectTimer();

      if (inactivityTimerRef.current) {
        clearInterval(inactivityTimerRef.current);
        inactivityTimerRef.current = null;
      }
    };
  }, [
    apiUrl,
    clearPollingTimer,
    clearReconnectTimer,
    closeEventSource,
    enabled,
    inactivityTimeoutMs,
    pollInterval,
    projectId,
    reconnectDelayMs,
    scheduleReconnect,
    startPollingFallback,
    stopPollingFallback,
  ]);

  const refetch = useCallback(async () => {
    await fetchSnapshotViaPolling(usingPollingFallbackRef.current);
  }, [fetchSnapshotViaPolling]);

  return {
    tasks: state.tasks,
    stats: state.stats,
    isLoading: state.isLoading,
    isConnected: state.isConnected,
    error: state.error,
    refetch,
  };
}

export default useTaskSse;

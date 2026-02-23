import { useCallback, useEffect, useReducer, useRef } from 'react';
import type { ProjectStats, TaskDisplay, TUISSEEvent } from '../types';
import { normalizeTaskSSEEvent } from './taskSseEvents';
import type { TaskStats, UseTaskResult } from './taskTypes';

export interface UseTaskSseOptions {
  projectId: string;
  apiUrl: string;
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
}

export type TaskSseAction =
  | { type: 'FETCH_START' }
  | { type: 'SNAPSHOT_SUCCESS'; tasks: TaskDisplay[]; stats: TaskStats }
  | { type: 'CONNECTION_ERROR'; error: Error }
  | { type: 'SSE_HEALTHY' };

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
      };
    case 'CONNECTION_ERROR':
      return {
        ...state,
        isLoading: false,
        isConnected: false,
        error: action.error,
      };
    case 'SSE_HEALTHY':
      return {
        ...state,
        isLoading: false,
        isConnected: true,
        error: null,
      };
  }
}

export function buildTaskStreamUrl(apiUrl: string, projectId: string): string {
  return `${apiUrl}/api/v1/tasks/${encodeURIComponent(projectId)}/stream`;
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

export function useTaskSse(options: UseTaskSseOptions): UseTaskResult {
  const {
    projectId,
    apiUrl,
    inactivityTimeoutMs = DEFAULT_INACTIVITY_TIMEOUT_MS,
    reconnectDelayMs = DEFAULT_RECONNECT_DELAY_MS,
    enabled = true,
  } = options;

  const [state, dispatch] = useReducer(taskSseReducer, INITIAL_STATE);

  const eventSourceRef = useRef<EventSource | null>(null);
  const reconnectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const isUnmountedRef = useRef(false);

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
    dispatch({ type: 'FETCH_START' });

    const connectSse = () => {
      if (isUnmountedRef.current) {
        return;
      }

      if (typeof EventSource === 'undefined') {
        dispatch({
          type: 'CONNECTION_ERROR',
          error: new Error('EventSource is unavailable in this runtime'),
        });
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

        dispatch({
          type: 'SNAPSHOT_SUCCESS',
          tasks: snapshot.tasks,
          stats: snapshot.stats,
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

        dispatch({ type: 'CONNECTION_ERROR', error: new Error(message) });
        scheduleReconnect(connectSse);
      };

      const onTransportError = () => {
        dispatch({ type: 'CONNECTION_ERROR', error: new Error('SSE connection error') });
        scheduleReconnect(connectSse);
      };

      eventSource.addEventListener('connected', onConnected as EventListener);
      eventSource.addEventListener('heartbeat', onHeartbeat as EventListener);
      eventSource.addEventListener('tasks_snapshot', onSnapshot as EventListener);
      eventSource.addEventListener('error', onServerErrorEvent as EventListener);
      eventSource.onerror = onTransportError;
    };

    connectSse();

    return () => {
      isUnmountedRef.current = true;
      closeEventSource();
      clearReconnectTimer();
    };
  }, [
    apiUrl,
    clearReconnectTimer,
    closeEventSource,
    enabled,
    projectId,
    scheduleReconnect,
  ]);

  // refetch is a no-op in pure SSE mode (no manual polling)
  const refetch = useCallback(async () => {
    // Pure SSE mode - no manual refetch needed
  }, []);

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

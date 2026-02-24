import { useCallback, useEffect, useReducer, useRef } from 'react';
import type { ProjectStats, TaskDisplay, TUISSEEvent } from '../types';
import { normalizeTaskSSEEvent } from './taskSseEvents';
import type { TaskStats, UseTaskResult } from './taskTypes';
import { appendFileSync } from 'fs';
import { EventSource as EventSourcePolyfill } from 'eventsource';

// Use polyfill in Node.js/Bun runtime, native EventSource in browser
const EventSourceImpl = (typeof EventSource !== 'undefined' ? EventSource : EventSourcePolyfill) as typeof EventSource;

const DEBUG_LOG = '/tmp/tui-sse-debug.log';

function debugLog(message: string) {
  try {
    appendFileSync(DEBUG_LOG, `[${new Date().toISOString()}] ${message}\n`);
  } catch {
    // Ignore errors
  }
}

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
  const url = `${apiUrl}/api/v1/tasks/${encodeURIComponent(projectId)}/stream`;
  debugLog(`[buildTaskStreamUrl] Built URL: ${url}`);
  return url;
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
  debugLog(`[extractSnapshot] Called with event: ${event ? event.type : 'NULL'}`);
  
  if (!event || event.type !== 'tasks_snapshot') {
    debugLog('[extractSnapshot] Event is null or not tasks_snapshot, returning null');
    return null;
  }

  debugLog(`[extractSnapshot] Extracting snapshot with ${event.tasks.length} tasks`);
  
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

  debugLog(`[useTaskSse] Hook called with projectId=${projectId}, apiUrl=${apiUrl}, enabled=${enabled}`);

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
    debugLog('[useTaskSse:closeEventSource] Closing EventSource');
    if (eventSourceRef.current) {
      const readyState = eventSourceRef.current.readyState;
      debugLog(`[useTaskSse:closeEventSource] Current readyState: ${readyState}`);
      
      // Only close if it's not already closed
      if (readyState !== 2) { // 2 = CLOSED
        eventSourceRef.current.close();
        debugLog('[useTaskSse:closeEventSource] EventSource closed');
      } else {
        debugLog('[useTaskSse:closeEventSource] EventSource already closed, skipping');
      }
      eventSourceRef.current = null;
    } else {
      debugLog('[useTaskSse:closeEventSource] No EventSource to close');
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
    debugLog(`[useTaskSse:useEffect] Effect triggered with enabled=${enabled}`);
    
    if (!enabled) {
      debugLog('[useTaskSse:useEffect] Hook is disabled, returning early');
      return;
    }

    let didCancel = false;  // Flag to track if effect was cancelled
    isUnmountedRef.current = false;
    dispatch({ type: 'FETCH_START' });

    const connectSse = () => {
      debugLog('[useTaskSse:connectSse] Function called');
      
      if (isUnmountedRef.current || didCancel) {
        debugLog('[useTaskSse:connectSse] Component unmounted or cancelled, returning');
        return;
      }

      if (typeof EventSourceImpl === 'undefined') {
        debugLog('[useTaskSse:connectSse] EventSource unavailable');
        dispatch({
          type: 'CONNECTION_ERROR',
          error: new Error('EventSource is unavailable in this runtime'),
        });
        return;
      }

      closeEventSource();

      const streamUrl = buildTaskStreamUrl(apiUrl, projectId);
      debugLog(`[useTaskSse:connectSse] Creating EventSource with URL: ${streamUrl}`);
      const eventSource = new EventSourceImpl(streamUrl);
      eventSourceRef.current = eventSource as EventSource;

      const onConnected = (messageEvent: MessageEvent<string>) => {
        debugLog('[useTaskSse:onConnected] Called');
        const event = normalizeTaskSSEEvent({
          event: 'connected',
          data: messageEvent.data,
          fallbackProjectId: projectId,
        });
        if (!event || event.type !== 'connected') {
          debugLog('[useTaskSse:onConnected] Event invalid');
          return;
        }

        debugLog('[useTaskSse:onConnected] SSE connected successfully');
        dispatch({ type: 'SSE_HEALTHY' });
      };

      const onHeartbeat = (messageEvent: MessageEvent<string>) => {
        debugLog('[useTaskSse:onHeartbeat] Handler called');
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
        debugLog('[useTaskSse:onSnapshot] Called');
        debugLog(`[useTaskSse:onSnapshot] Raw data (first 200 chars): ${messageEvent.data.substring(0, 200)}`);
        
        const event = normalizeTaskSSEEvent({
          event: 'tasks_snapshot',
          data: messageEvent.data,
          fallbackProjectId: projectId,
        });
        
        if (event && event.type === 'tasks_snapshot') {
          debugLog(`[useTaskSse:onSnapshot] Normalized event: type=${event.type}, projectId=${event.projectId}, taskCount=${event.tasks.length}`);
        } else {
          debugLog('[useTaskSse:onSnapshot] Normalized event: NULL or wrong type');
        }

        const snapshot = extractSnapshot(event);
        if (snapshot) {
          debugLog(`[useTaskSse:onSnapshot] Extracted snapshot: taskCount=${snapshot.tasks.length}, stats=${JSON.stringify(snapshot.stats)}`);
        } else {
          debugLog('[useTaskSse:onSnapshot] Extracted snapshot: NULL');
        }
        
        if (!snapshot) {
          debugLog('[useTaskSse:onSnapshot] Snapshot is null, returning without dispatch');
          return;
        }

        debugLog(`[useTaskSse:onSnapshot] Dispatching SNAPSHOT_SUCCESS with ${snapshot.tasks.length} tasks`);
        dispatch({
          type: 'SNAPSHOT_SUCCESS',
          tasks: snapshot.tasks,
          stats: snapshot.stats,
        });
      };

      const onServerErrorEvent = (messageEvent: MessageEvent<string>) => {
        debugLog('[useTaskSse:onServerErrorEvent] Handler called');
        const event = normalizeTaskSSEEvent({
          event: 'error',
          data: messageEvent.data,
          fallbackProjectId: projectId,
        });

        const message = event && event.type === 'error'
          ? event.message
          : 'SSE stream reported an error';

        debugLog(`[useTaskSse:onServerErrorEvent] Error message: ${message}`);
        dispatch({ type: 'CONNECTION_ERROR', error: new Error(message) });
        scheduleReconnect(connectSse);
      };

      const onTransportError = (e: Event) => {
        debugLog(`[useTaskSse:onTransportError] Transport error occurred, type=${e.type}`);
        dispatch({ type: 'CONNECTION_ERROR', error: new Error('SSE connection error') });
        scheduleReconnect(connectSse);
      };

      debugLog('[useTaskSse:connectSse] Attaching event listeners');
      eventSource.addEventListener('connected', onConnected as EventListener);
      eventSource.addEventListener('heartbeat', onHeartbeat as EventListener);
      eventSource.addEventListener('tasks_snapshot', onSnapshot as EventListener);
      eventSource.addEventListener('error', onServerErrorEvent as EventListener);
      eventSource.onerror = onTransportError;
      debugLog(`[useTaskSse:connectSse] Event listeners attached, readyState=${eventSource.readyState}`);
      
      // Schedule a check after 2 seconds to see if connection opened
      setTimeout(() => {
        debugLog(`[useTaskSse:connectSse:timeout] Connection check - readyState=${eventSource.readyState}`);
      }, 2000);
    };

    debugLog('[useTaskSse:useEffect] Calling connectSse()');
    connectSse();

    return () => {
      debugLog('[useTaskSse:useEffect:cleanup] Cleanup called');
      didCancel = true;  // Mark as cancelled
      isUnmountedRef.current = true;
      
      // Only close if EventSource exists and is not in CONNECTING state
      // This prevents React Strict Mode from closing connections prematurely
      if (eventSourceRef.current && eventSourceRef.current.readyState !== 0) {
        debugLog('[useTaskSse:useEffect:cleanup] Closing EventSource (not in CONNECTING state)');
        closeEventSource();
      } else {
        debugLog('[useTaskSse:useEffect:cleanup] Skipping close (CONNECTING or no connection)');
      }
      
      clearReconnectTimer();
    };
  }, [
    apiUrl,
    enabled,
    projectId,
    reconnectDelayMs,  // Only include reconnectDelayMs, not the callback itself
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

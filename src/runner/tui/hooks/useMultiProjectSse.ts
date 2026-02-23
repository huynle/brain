import { useCallback, useEffect, useMemo, useReducer, useRef } from 'react';
import type { ProjectStats, TaskDisplay, TUISSEEvent } from '../types';
import { normalizeTaskSSEEvent } from './taskSseEvents';
import type { TaskStats } from './useTaskPoller';
import {
  aggregateProjectStats,
  checkAnyConnected,
  getFirstError,
  mergeAllTasks,
  type MultiProjectPollerResult,
  type UseMultiProjectPollerOptions,
} from './useMultiProjectPoller';

interface UseMultiProjectSseOptions extends UseMultiProjectPollerOptions {
  inactivityTimeoutMs?: number;
  reconnectDelayMs?: number;
}

export interface MultiProjectSseState {
  tasksByProject: Map<string, TaskDisplay[]>;
  statsByProject: Map<string, TaskStats>;
  connectionByProject: Map<string, boolean>;
  errorsByProject: Map<string, Error>;
  pollingFallbackByProject: Map<string, boolean>;
  isLoading: boolean;
}

export type MultiProjectSseAction =
  | { type: 'FETCH_START' }
  | { type: 'RESET_PROJECTS'; projectIds: string[] }
  | {
      type: 'PROJECT_SNAPSHOT_SUCCESS';
      projectId: string;
      tasks: TaskDisplay[];
      stats: TaskStats;
      fromPollingFallback: boolean;
    }
  | {
      type: 'PROJECT_CONNECTION_ERROR';
      projectId: string;
      error: Error;
      usePollingFallback: boolean;
    }
  | {
      type: 'PROJECT_SSE_HEALTHY';
      projectId: string;
    };

const DEFAULT_POLL_INTERVAL = 1000;
const DEFAULT_INACTIVITY_TIMEOUT_MS = 65_000;
const DEFAULT_RECONNECT_DELAY_MS = 5_000;

const INITIAL_STATE: MultiProjectSseState = {
  tasksByProject: new Map(),
  statsByProject: new Map(),
  connectionByProject: new Map(),
  errorsByProject: new Map(),
  pollingFallbackByProject: new Map(),
  isLoading: true,
};

type ProjectRuntimeState = {
  eventSource: EventSource | null;
  pollingTimer: ReturnType<typeof setInterval> | null;
  reconnectTimer: ReturnType<typeof setTimeout> | null;
  inactivityTimer: ReturnType<typeof setInterval> | null;
  hasConnectedEvent: boolean;
  lastSseActivityAt: number | null;
  usingPollingFallback: boolean;
};

function getOrCreateProjectRuntimeState(
  runtimeByProject: Map<string, ProjectRuntimeState>,
  projectId: string
): ProjectRuntimeState {
  const existing = runtimeByProject.get(projectId);
  if (existing) {
    return existing;
  }

  const created: ProjectRuntimeState = {
    eventSource: null,
    pollingTimer: null,
    reconnectTimer: null,
    inactivityTimer: null,
    hasConnectedEvent: false,
    lastSseActivityAt: null,
    usingPollingFallback: false,
  };
  runtimeByProject.set(projectId, created);
  return created;
}

export function multiProjectSseReducer(state: MultiProjectSseState, action: MultiProjectSseAction): MultiProjectSseState {
  switch (action.type) {
    case 'FETCH_START':
      return { ...state, isLoading: true };
    case 'RESET_PROJECTS': {
      const projectSet = new Set(action.projectIds);
      const next = {
        tasksByProject: new Map<string, TaskDisplay[]>(),
        statsByProject: new Map<string, TaskStats>(),
        connectionByProject: new Map<string, boolean>(),
        errorsByProject: new Map<string, Error>(),
        pollingFallbackByProject: new Map<string, boolean>(),
      };

      for (const [projectId, tasks] of state.tasksByProject) {
        if (projectSet.has(projectId)) {
          next.tasksByProject.set(projectId, tasks);
        }
      }
      for (const [projectId, stats] of state.statsByProject) {
        if (projectSet.has(projectId)) {
          next.statsByProject.set(projectId, stats);
        }
      }
      for (const [projectId, connected] of state.connectionByProject) {
        if (projectSet.has(projectId)) {
          next.connectionByProject.set(projectId, connected);
        }
      }
      for (const [projectId, error] of state.errorsByProject) {
        if (projectSet.has(projectId)) {
          next.errorsByProject.set(projectId, error);
        }
      }
      for (const [projectId, usingFallback] of state.pollingFallbackByProject) {
        if (projectSet.has(projectId)) {
          next.pollingFallbackByProject.set(projectId, usingFallback);
        }
      }

      return {
        ...next,
        isLoading: true,
      };
    }
    case 'PROJECT_SNAPSHOT_SUCCESS': {
      const tasksByProject = new Map(state.tasksByProject);
      const statsByProject = new Map(state.statsByProject);
      const connectionByProject = new Map(state.connectionByProject);
      const errorsByProject = new Map(state.errorsByProject);
      const pollingFallbackByProject = new Map(state.pollingFallbackByProject);

      tasksByProject.set(action.projectId, action.tasks);
      statsByProject.set(action.projectId, action.stats);
      connectionByProject.set(action.projectId, true);
      errorsByProject.delete(action.projectId);
      pollingFallbackByProject.set(action.projectId, action.fromPollingFallback);

      return {
        tasksByProject,
        statsByProject,
        connectionByProject,
        errorsByProject,
        pollingFallbackByProject,
        isLoading: false,
      };
    }
    case 'PROJECT_CONNECTION_ERROR': {
      const connectionByProject = new Map(state.connectionByProject);
      const errorsByProject = new Map(state.errorsByProject);
      const pollingFallbackByProject = new Map(state.pollingFallbackByProject);

      connectionByProject.set(action.projectId, false);
      errorsByProject.set(action.projectId, action.error);
      pollingFallbackByProject.set(action.projectId, action.usePollingFallback);

      return {
        ...state,
        connectionByProject,
        errorsByProject,
        pollingFallbackByProject,
        isLoading: false,
      };
    }
    case 'PROJECT_SSE_HEALTHY': {
      const connectionByProject = new Map(state.connectionByProject);
      const errorsByProject = new Map(state.errorsByProject);
      const pollingFallbackByProject = new Map(state.pollingFallbackByProject);

      connectionByProject.set(action.projectId, true);
      errorsByProject.delete(action.projectId);
      pollingFallbackByProject.set(action.projectId, false);

      return {
        ...state,
        connectionByProject,
        errorsByProject,
        pollingFallbackByProject,
        isLoading: false,
      };
    }
  }
}

export function buildProjectTaskStreamUrl(apiUrl: string, projectId: string): string {
  return `${apiUrl}/api/v1/tasks/${encodeURIComponent(projectId)}/stream`;
}

export function isSseInactive(lastActivityAt: number, now: number, inactivityTimeoutMs: number): boolean {
  return now - lastActivityAt > inactivityTimeoutMs;
}

export function shouldUseProjectPollingFallback(params: {
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

export function useMultiProjectSse(options: UseMultiProjectSseOptions): MultiProjectPollerResult {
  const {
    projects,
    apiUrl,
    pollInterval = DEFAULT_POLL_INTERVAL,
    inactivityTimeoutMs = DEFAULT_INACTIVITY_TIMEOUT_MS,
    reconnectDelayMs = DEFAULT_RECONNECT_DELAY_MS,
    enabled = true,
  } = options;

  const [state, dispatch] = useReducer(multiProjectSseReducer, INITIAL_STATE);
  const runtimeByProjectRef = useRef<Map<string, ProjectRuntimeState>>(new Map());
  const isUnmountedRef = useRef(false);

  const clearProjectPollingTimer = useCallback((projectId: string) => {
    const runtime = runtimeByProjectRef.current.get(projectId);
    if (runtime?.pollingTimer) {
      clearInterval(runtime.pollingTimer);
      runtime.pollingTimer = null;
    }
  }, []);

  const clearProjectReconnectTimer = useCallback((projectId: string) => {
    const runtime = runtimeByProjectRef.current.get(projectId);
    if (runtime?.reconnectTimer) {
      clearTimeout(runtime.reconnectTimer);
      runtime.reconnectTimer = null;
    }
  }, []);

  const closeProjectEventSource = useCallback((projectId: string) => {
    const runtime = runtimeByProjectRef.current.get(projectId);
    if (runtime?.eventSource) {
      runtime.eventSource.close();
      runtime.eventSource = null;
    }
  }, []);

  const cleanupProjectRuntime = useCallback(
    (projectId: string) => {
      closeProjectEventSource(projectId);
      clearProjectPollingTimer(projectId);
      clearProjectReconnectTimer(projectId);

      const runtime = runtimeByProjectRef.current.get(projectId);
      if (runtime?.inactivityTimer) {
        clearInterval(runtime.inactivityTimer);
        runtime.inactivityTimer = null;
      }
    },
    [clearProjectPollingTimer, clearProjectReconnectTimer, closeProjectEventSource]
  );

  const fetchSnapshotViaPolling = useCallback(
    async (projectId: string, fromPollingFallback: boolean) => {
      try {
        const response = await fetch(`${apiUrl}/api/v1/tasks/${encodeURIComponent(projectId)}`);
        if (!response.ok) {
          throw new Error(`API error: ${response.status} ${response.statusText}`);
        }

        const data = await response.json();
        const snapshot = normalizeSnapshotEventFromApiResponse(projectId, data);

        dispatch({
          type: 'PROJECT_SNAPSHOT_SUCCESS',
          projectId,
          tasks: snapshot.tasks,
          stats: snapshot.stats,
          fromPollingFallback,
        });
      } catch (err) {
        const error = err instanceof Error ? err : new Error('Unknown polling error');
        dispatch({ type: 'PROJECT_CONNECTION_ERROR', projectId, error, usePollingFallback: true });
      }
    },
    [apiUrl]
  );

  const startProjectPollingFallback = useCallback(
    (projectId: string, reason: Error) => {
      const runtime = getOrCreateProjectRuntimeState(runtimeByProjectRef.current, projectId);
      runtime.usingPollingFallback = true;

      dispatch({
        type: 'PROJECT_CONNECTION_ERROR',
        projectId,
        error: reason,
        usePollingFallback: true,
      });

      if (!runtime.pollingTimer) {
        void fetchSnapshotViaPolling(projectId, true);
        runtime.pollingTimer = setInterval(() => {
          void fetchSnapshotViaPolling(projectId, true);
        }, pollInterval);
      }
    },
    [fetchSnapshotViaPolling, pollInterval]
  );

  const stopProjectPollingFallback = useCallback(
    (projectId: string) => {
      const runtime = getOrCreateProjectRuntimeState(runtimeByProjectRef.current, projectId);
      runtime.usingPollingFallback = false;
      clearProjectPollingTimer(projectId);
    },
    [clearProjectPollingTimer]
  );

  const scheduleProjectReconnect = useCallback(
    (projectId: string, reconnect: () => void) => {
      const runtime = getOrCreateProjectRuntimeState(runtimeByProjectRef.current, projectId);
      if (runtime.reconnectTimer || isUnmountedRef.current) {
        return;
      }

      runtime.reconnectTimer = setTimeout(() => {
        runtime.reconnectTimer = null;
        reconnect();
      }, reconnectDelayMs);
    },
    [reconnectDelayMs]
  );

  useEffect(() => {
    if (!enabled) {
      return;
    }

    isUnmountedRef.current = false;
    dispatch({ type: 'RESET_PROJECTS', projectIds: projects });

    for (const [projectId] of runtimeByProjectRef.current) {
      if (!projects.includes(projectId)) {
        cleanupProjectRuntime(projectId);
        runtimeByProjectRef.current.delete(projectId);
      }
    }

    dispatch({ type: 'FETCH_START' });

    for (const projectId of projects) {
      const runtime = getOrCreateProjectRuntimeState(runtimeByProjectRef.current, projectId);
      runtime.hasConnectedEvent = false;
      runtime.lastSseActivityAt = null;
      runtime.usingPollingFallback = false;

      const connectProjectSse = () => {
        if (isUnmountedRef.current) {
          return;
        }

        if (typeof EventSource === 'undefined') {
          startProjectPollingFallback(projectId, new Error('EventSource is unavailable in this runtime'));
          return;
        }

        closeProjectEventSource(projectId);
        const eventSource = new EventSource(buildProjectTaskStreamUrl(apiUrl, projectId));
        runtime.eventSource = eventSource;

        const onConnected = (messageEvent: MessageEvent<string>) => {
          const event = normalizeTaskSSEEvent({
            event: 'connected',
            data: messageEvent.data,
            fallbackProjectId: projectId,
          });
          if (!event || event.type !== 'connected') {
            return;
          }

          runtime.hasConnectedEvent = true;
          runtime.lastSseActivityAt = Date.now();
          stopProjectPollingFallback(projectId);
          dispatch({ type: 'PROJECT_SSE_HEALTHY', projectId });
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

          runtime.lastSseActivityAt = Date.now();
          stopProjectPollingFallback(projectId);
          dispatch({ type: 'PROJECT_SSE_HEALTHY', projectId });
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

          runtime.lastSseActivityAt = Date.now();
          stopProjectPollingFallback(projectId);
          dispatch({
            type: 'PROJECT_SNAPSHOT_SUCCESS',
            projectId,
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

          const message = event && event.type === 'error' ? event.message : 'SSE stream reported an error';
          startProjectPollingFallback(projectId, new Error(message));
          scheduleProjectReconnect(projectId, connectProjectSse);
        };

        const onTransportError = () => {
          startProjectPollingFallback(projectId, new Error('SSE connection error'));
          scheduleProjectReconnect(projectId, connectProjectSse);
        };

        eventSource.addEventListener('connected', onConnected as EventListener);
        eventSource.addEventListener('heartbeat', onHeartbeat as EventListener);
        eventSource.addEventListener('tasks_snapshot', onSnapshot as EventListener);
        eventSource.addEventListener('error', onServerErrorEvent as EventListener);
        eventSource.onerror = onTransportError;
      };

      connectProjectSse();

      const inactivityCheckMs = Math.max(1000, Math.floor(inactivityTimeoutMs / 2));
      runtime.inactivityTimer = setInterval(() => {
        if (isUnmountedRef.current) {
          return;
        }

        const hasEventSource = typeof EventSource !== 'undefined';
        const hasRecentActivity =
          typeof runtime.lastSseActivityAt === 'number'
            ? !isSseInactive(runtime.lastSseActivityAt, Date.now(), inactivityTimeoutMs)
            : false;

        if (
          shouldUseProjectPollingFallback({
            hasEventSource,
            hasConnectedEvent: runtime.hasConnectedEvent,
            hasRecentActivity,
          })
        ) {
          closeProjectEventSource(projectId);
          startProjectPollingFallback(projectId, new Error('SSE stream inactive; using polling fallback'));
          scheduleProjectReconnect(projectId, connectProjectSse);
        }
      }, inactivityCheckMs);
    }

    return () => {
      isUnmountedRef.current = true;
      for (const [projectId] of runtimeByProjectRef.current) {
        cleanupProjectRuntime(projectId);
      }
    };
  }, [
    apiUrl,
    cleanupProjectRuntime,
    closeProjectEventSource,
    enabled,
    inactivityTimeoutMs,
    projects,
    reconnectDelayMs,
    scheduleProjectReconnect,
    startProjectPollingFallback,
    stopProjectPollingFallback,
  ]);

  const refetch = useCallback(async () => {
    await Promise.all(
      projects.map(async (projectId) => {
        const runtime = getOrCreateProjectRuntimeState(runtimeByProjectRef.current, projectId);
        await fetchSnapshotViaPolling(projectId, runtime.usingPollingFallback);
      })
    );
  }, [fetchSnapshotViaPolling, projects]);

  const aggregateStats = useMemo(
    () => aggregateProjectStats(state.statsByProject),
    [state.statsByProject]
  );
  const allTasks = useMemo(
    () => mergeAllTasks(state.tasksByProject),
    [state.tasksByProject]
  );
  const isConnected = useMemo(
    () => checkAnyConnected(state.connectionByProject),
    [state.connectionByProject]
  );
  const error = useMemo(
    () => getFirstError(state.errorsByProject),
    [state.errorsByProject]
  );

  return {
    tasksByProject: state.tasksByProject,
    statsByProject: state.statsByProject,
    aggregateStats,
    allTasks,
    isLoading: state.isLoading,
    isConnected,
    error,
    refetch,
    connectionByProject: state.connectionByProject,
    errorsByProject: state.errorsByProject,
  };
}

export default useMultiProjectSse;

import { useCallback, useEffect, useMemo, useReducer, useRef } from 'react';
import type { ProjectStats, TaskDisplay, TUISSEEvent } from '../types';
import { normalizeTaskSSEEvent } from './taskSseEvents';
import type { TaskStats } from './taskTypes';
import {
  aggregateProjectStats,
  checkAnyConnected,
  getFirstError,
  mergeAllTasks,
  type MultiProjectResult,
  type UseMultiProjectOptions,
} from './multiProjectUtils';

interface UseMultiProjectSseOptions extends UseMultiProjectOptions {
  inactivityTimeoutMs?: number;
  reconnectDelayMs?: number;
}

export interface MultiProjectSseState {
  tasksByProject: Map<string, TaskDisplay[]>;
  statsByProject: Map<string, TaskStats>;
  connectionByProject: Map<string, boolean>;
  errorsByProject: Map<string, Error>;
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
    }
  | {
      type: 'PROJECT_CONNECTION_ERROR';
      projectId: string;
      error: Error;
    }
  | {
      type: 'PROJECT_SSE_HEALTHY';
      projectId: string;
    };

const DEFAULT_INACTIVITY_TIMEOUT_MS = 65_000;
const DEFAULT_RECONNECT_DELAY_MS = 5_000;

const INITIAL_STATE: MultiProjectSseState = {
  tasksByProject: new Map(),
  statsByProject: new Map(),
  connectionByProject: new Map(),
  errorsByProject: new Map(),
  isLoading: true,
};

type ProjectRuntimeState = {
  eventSource: EventSource | null;
  reconnectTimer: ReturnType<typeof setTimeout> | null;
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
    reconnectTimer: null,
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

      tasksByProject.set(action.projectId, action.tasks);
      statsByProject.set(action.projectId, action.stats);
      connectionByProject.set(action.projectId, true);
      errorsByProject.delete(action.projectId);

      return {
        tasksByProject,
        statsByProject,
        connectionByProject,
        errorsByProject,
        isLoading: false,
      };
    }
    case 'PROJECT_CONNECTION_ERROR': {
      const connectionByProject = new Map(state.connectionByProject);
      const errorsByProject = new Map(state.errorsByProject);

      connectionByProject.set(action.projectId, false);
      errorsByProject.set(action.projectId, action.error);

      return {
        ...state,
        connectionByProject,
        errorsByProject,
        isLoading: false,
      };
    }
    case 'PROJECT_SSE_HEALTHY': {
      const connectionByProject = new Map(state.connectionByProject);
      const errorsByProject = new Map(state.errorsByProject);

      connectionByProject.set(action.projectId, true);
      errorsByProject.delete(action.projectId);

      return {
        ...state,
        connectionByProject,
        errorsByProject,
        isLoading: false,
      };
    }
  }
}

export function buildProjectTaskStreamUrl(apiUrl: string, projectId: string): string {
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

export function useMultiProjectSse(options: UseMultiProjectSseOptions): MultiProjectResult {
  const {
    projects,
    apiUrl,
    inactivityTimeoutMs = DEFAULT_INACTIVITY_TIMEOUT_MS,
    reconnectDelayMs = DEFAULT_RECONNECT_DELAY_MS,
    enabled = true,
  } = options;

  const [state, dispatch] = useReducer(multiProjectSseReducer, INITIAL_STATE);
  const runtimeByProjectRef = useRef<Map<string, ProjectRuntimeState>>(new Map());
  const isUnmountedRef = useRef(false);

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
      clearProjectReconnectTimer(projectId);
    },
    [clearProjectReconnectTimer, closeProjectEventSource]
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

      const connectProjectSse = () => {
        if (isUnmountedRef.current) {
          return;
        }

        if (typeof EventSource === 'undefined') {
          dispatch({
            type: 'PROJECT_CONNECTION_ERROR',
            projectId,
            error: new Error('EventSource is unavailable in this runtime'),
          });
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

          dispatch({
            type: 'PROJECT_SNAPSHOT_SUCCESS',
            projectId,
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

          const message = event && event.type === 'error' ? event.message : 'SSE stream reported an error';
          dispatch({ type: 'PROJECT_CONNECTION_ERROR', projectId, error: new Error(message) });
          scheduleProjectReconnect(projectId, connectProjectSse);
        };

        const onTransportError = () => {
          dispatch({ type: 'PROJECT_CONNECTION_ERROR', projectId, error: new Error('SSE connection error') });
          scheduleProjectReconnect(projectId, connectProjectSse);
        };

        eventSource.addEventListener('connected', onConnected as EventListener);
        eventSource.addEventListener('heartbeat', onHeartbeat as EventListener);
        eventSource.addEventListener('tasks_snapshot', onSnapshot as EventListener);
        eventSource.addEventListener('error', onServerErrorEvent as EventListener);
        eventSource.onerror = onTransportError;
      };

      connectProjectSse();
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
    projects,
    scheduleProjectReconnect,
  ]);

  // refetch is a no-op in pure SSE mode (no manual polling)
  const refetch = useCallback(async () => {
    // Pure SSE mode - no manual refetch needed
  }, []);

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

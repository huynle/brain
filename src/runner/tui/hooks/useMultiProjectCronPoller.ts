/**
 * Hook for polling cron entries from multiple projects in parallel.
 */

import { useReducer, useEffect, useCallback, useMemo, useRef } from 'react';
import type { CronDisplay } from '../types';

interface CronListResponse {
  crons?: Array<{
    id: string;
    path: string;
    title: string;
    status: CronDisplay['status'];
    schedule?: string;
    next_run?: string;
    runs?: CronDisplay['runs'];
  }>;
}

export interface UseMultiProjectCronPollerOptions {
  projects: string[];
  apiUrl: string;
  pollInterval?: number;
  enabled?: boolean;
}

export interface MultiProjectCronPollerResult {
  cronsByProject: Map<string, CronDisplay[]>;
  allCrons: CronDisplay[];
  isLoading: boolean;
  isConnected: boolean;
  error: Error | null;
  refetch: () => Promise<void>;
  connectionByProject: Map<string, boolean>;
  errorsByProject: Map<string, Error>;
}

export interface MultiProjectCronPollerState {
  cronsByProject: Map<string, CronDisplay[]>;
  connectionByProject: Map<string, boolean>;
  errorsByProject: Map<string, Error>;
  isLoading: boolean;
}

export type MultiProjectCronPollerAction =
  | { type: 'FETCH_START' }
  | {
      type: 'FETCH_SUCCESS';
      cronsByProject: Map<string, CronDisplay[]>;
      connectionByProject: Map<string, boolean>;
      errorsByProject: Map<string, Error>;
    };

const DEFAULT_POLL_INTERVAL = 1000;

const INITIAL_STATE: MultiProjectCronPollerState = {
  cronsByProject: new Map(),
  connectionByProject: new Map(),
  errorsByProject: new Map(),
  isLoading: true,
};

function simpleHash(str: string): number {
  let hash = 2166136261;
  for (let i = 0; i < str.length; i++) {
    hash ^= str.charCodeAt(i);
    hash = (hash * 16777619) >>> 0;
  }
  return hash;
}

function toCronDisplay(raw: NonNullable<CronListResponse['crons']>[number]): CronDisplay {
  return {
    id: raw.id,
    path: raw.path,
    title: raw.title,
    schedule: raw.schedule ?? '',
    next_run: raw.next_run,
    status: raw.status,
    runs: raw.runs,
  };
}

function toCronDisplayWithProject(
  raw: NonNullable<CronListResponse['crons']>[number],
  projectId: string
): CronDisplay {
  return {
    ...toCronDisplay(raw),
    projectId,
  };
}

async function fetchProjectCrons(projectId: string, apiUrl: string): Promise<CronDisplay[]> {
  const response = await fetch(`${apiUrl}/api/v1/crons/${encodeURIComponent(projectId)}/crons`);
  if (!response.ok) {
    throw new Error(`API error: ${response.status} ${response.statusText}`);
  }

  const data = (await response.json()) as CronListResponse;
  return (data.crons ?? []).map((cron) => toCronDisplayWithProject(cron, projectId));
}

function createStateHash(
  cronsByProject: Map<string, CronDisplay[]>,
  connectionByProject: Map<string, boolean>,
  errorsByProject: Map<string, Error>
): number {
  const cronsData: Record<string, Array<{ id: string; status: string; schedule: string; next_run?: string }>> = {};
  for (const [projectId, crons] of cronsByProject) {
    cronsData[projectId] = crons.map((cron) => ({
      id: cron.id,
      status: cron.status,
      schedule: cron.schedule,
      next_run: cron.next_run,
    }));
  }

  const connData: Record<string, boolean> = {};
  for (const [projectId, connected] of connectionByProject) {
    connData[projectId] = connected;
  }

  const errData: Record<string, string> = {};
  for (const [projectId, error] of errorsByProject) {
    errData[projectId] = error.message;
  }

  return simpleHash(JSON.stringify({ crons: cronsData, conn: connData, err: errData }));
}

export function mergeAllCrons(cronsByProject: Map<string, CronDisplay[]>): CronDisplay[] {
  return Array.from(cronsByProject.values()).flat();
}

export function checkAnyConnected(connectionByProject: Map<string, boolean>): boolean {
  return Array.from(connectionByProject.values()).some(Boolean);
}

export function getFirstError(errorsByProject: Map<string, Error>): Error | null {
  return Array.from(errorsByProject.values())[0] ?? null;
}

export function multiProjectCronReducer(
  state: MultiProjectCronPollerState,
  action: MultiProjectCronPollerAction
): MultiProjectCronPollerState {
  switch (action.type) {
    case 'FETCH_START':
      return { ...state, isLoading: true };
    case 'FETCH_SUCCESS':
      return {
        cronsByProject: action.cronsByProject,
        connectionByProject: action.connectionByProject,
        errorsByProject: action.errorsByProject,
        isLoading: false,
      };
  }
}

export function useMultiProjectCronPoller(
  options: UseMultiProjectCronPollerOptions
): MultiProjectCronPollerResult {
  const {
    projects,
    apiUrl,
    pollInterval = DEFAULT_POLL_INTERVAL,
    enabled = true,
  } = options;

  const [state, dispatch] = useReducer(multiProjectCronReducer, INITIAL_STATE);
  const projectsKey = projects.join(',');
  const isInitialFetchRef = useRef(true);
  const prevHashRef = useRef<number | null>(null);

  const cronsByProjectRef = useRef(state.cronsByProject);
  cronsByProjectRef.current = state.cronsByProject;

  const allCrons = useMemo(() => mergeAllCrons(state.cronsByProject), [state.cronsByProject]);
  const isConnected = useMemo(
    () => checkAnyConnected(state.connectionByProject),
    [state.connectionByProject]
  );
  const error = useMemo(() => getFirstError(state.errorsByProject), [state.errorsByProject]);

  const fetchAllProjects = useCallback(async () => {
    if (projects.length === 0) {
      return;
    }

    if (isInitialFetchRef.current) {
      dispatch({ type: 'FETCH_START' });
    }

    const results = await Promise.allSettled(
      projects.map(async (projectId) => {
        const crons = await fetchProjectCrons(projectId, apiUrl);
        return { projectId, crons };
      })
    );

    const nextCronsByProject = new Map<string, CronDisplay[]>();
    const nextConnectionByProject = new Map<string, boolean>();
    const nextErrorsByProject = new Map<string, Error>();

    for (let i = 0; i < results.length; i++) {
      const projectId = projects[i];
      const result = results[i];

      if (result.status === 'fulfilled') {
        nextCronsByProject.set(projectId, result.value.crons);
        nextConnectionByProject.set(projectId, true);
      } else {
        const existingCrons = cronsByProjectRef.current.get(projectId);
        if (existingCrons) {
          nextCronsByProject.set(projectId, existingCrons);
        }
        nextConnectionByProject.set(projectId, false);
        nextErrorsByProject.set(projectId, result.reason as Error);
      }
    }

    const nextHash = createStateHash(nextCronsByProject, nextConnectionByProject, nextErrorsByProject);
    if (nextHash !== prevHashRef.current) {
      prevHashRef.current = nextHash;
      dispatch({
        type: 'FETCH_SUCCESS',
        cronsByProject: nextCronsByProject,
        connectionByProject: nextConnectionByProject,
        errorsByProject: nextErrorsByProject,
      });
    }

    isInitialFetchRef.current = false;
  }, [apiUrl, projectsKey]);

  const refetch = useCallback(async () => {
    await fetchAllProjects();
  }, [fetchAllProjects]);

  useEffect(() => {
    if (!enabled || projects.length === 0) {
      return;
    }

    isInitialFetchRef.current = true;
    fetchAllProjects();
    const intervalId = setInterval(fetchAllProjects, pollInterval);

    return () => {
      clearInterval(intervalId);
    };
  }, [enabled, fetchAllProjects, pollInterval, projectsKey]);

  return {
    cronsByProject: state.cronsByProject,
    allCrons,
    isLoading: state.isLoading,
    isConnected,
    error,
    refetch,
    connectionByProject: state.connectionByProject,
    errorsByProject: state.errorsByProject,
  };
}

export default useMultiProjectCronPoller;

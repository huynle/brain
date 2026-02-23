/**
 * Hook for polling cron entries for a single project.
 */

import { useReducer, useEffect, useCallback, useRef } from 'react';
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

export interface UseCronPollerOptions {
  projectId: string;
  apiUrl: string;
  pollInterval?: number;
  enabled?: boolean;
}

export interface UseCronPollerResult {
  crons: CronDisplay[];
  isLoading: boolean;
  isConnected: boolean;
  error: Error | null;
  refetch: () => Promise<void>;
}

export interface CronPollerState {
  crons: CronDisplay[];
  isLoading: boolean;
  isConnected: boolean;
  error: Error | null;
}

export type CronPollerAction =
  | { type: 'FETCH_START' }
  | { type: 'FETCH_SUCCESS'; crons: CronDisplay[] }
  | { type: 'FETCH_ERROR'; error: Error };

const DEFAULT_POLL_INTERVAL = 1000;

const INITIAL_STATE: CronPollerState = {
  crons: [],
  isLoading: true,
  isConnected: false,
  error: null,
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

export function cronPollerReducer(state: CronPollerState, action: CronPollerAction): CronPollerState {
  switch (action.type) {
    case 'FETCH_START':
      return { ...state, isLoading: true };
    case 'FETCH_SUCCESS':
      return {
        crons: action.crons,
        isLoading: false,
        isConnected: true,
        error: null,
      };
    case 'FETCH_ERROR':
      return {
        ...state,
        isLoading: false,
        isConnected: false,
        error: action.error,
      };
  }
}

export function useCronPoller(options: UseCronPollerOptions): UseCronPollerResult {
  const {
    projectId,
    apiUrl,
    pollInterval = DEFAULT_POLL_INTERVAL,
    enabled = true,
  } = options;

  const [state, dispatch] = useReducer(cronPollerReducer, INITIAL_STATE);
  const isInitialFetchRef = useRef(true);
  const prevHashRef = useRef<number | null>(null);

  const fetchCrons = useCallback(async () => {
    if (isInitialFetchRef.current) {
      dispatch({ type: 'FETCH_START' });
    }

    try {
      const response = await fetch(`${apiUrl}/api/v1/crons/${encodeURIComponent(projectId)}/crons`);
      if (!response.ok) {
        throw new Error(`API error: ${response.status} ${response.statusText}`);
      }

      const data = (await response.json()) as CronListResponse;
      const crons = (data.crons ?? []).map((cron) => toCronDisplayWithProject(cron, projectId));

      const hashSource = crons.map((cron) => ({
        id: cron.id,
        title: cron.title,
        status: cron.status,
        schedule: cron.schedule,
        next_run: cron.next_run,
        runCount: cron.runs?.length ?? 0,
      }));
      const nextHash = simpleHash(JSON.stringify(hashSource));

      if (nextHash !== prevHashRef.current) {
        prevHashRef.current = nextHash;
        dispatch({ type: 'FETCH_SUCCESS', crons });
      }
    } catch (err) {
      const error = err instanceof Error ? err : new Error('Unknown error');
      dispatch({ type: 'FETCH_ERROR', error });
    } finally {
      isInitialFetchRef.current = false;
    }
  }, [apiUrl, projectId]);

  const refetch = useCallback(async () => {
    await fetchCrons();
  }, [fetchCrons]);

  useEffect(() => {
    if (!enabled) {
      return;
    }

    isInitialFetchRef.current = true;
    fetchCrons();
    const intervalId = setInterval(fetchCrons, pollInterval);

    return () => {
      clearInterval(intervalId);
    };
  }, [enabled, fetchCrons, pollInterval]);

  return {
    crons: state.crons,
    isLoading: state.isLoading,
    isConnected: state.isConnected,
    error: state.error,
    refetch,
  };
}

export default useCronPoller;

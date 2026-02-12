/**
 * Hook for polling resource metrics (CPU/memory) of running OpenCode processes.
 * 
 * This hook polls the getResourceMetrics callback at a regular interval
 * to display real-time resource usage in the TUI StatusBar.
 */

import { useState, useEffect, useCallback, useRef } from 'react';
import type { ResourceMetrics } from '../types';

/**
 * Check if two ResourceMetrics objects are equal.
 * Returns true if they have the same values, avoiding unnecessary state updates.
 */
function metricsEqual(a: ResourceMetrics | null, b: ResourceMetrics | null): boolean {
  if (a === b) return true;
  if (a === null || b === null) return false;
  return (
    a.cpuPercent === b.cpuPercent &&
    a.memoryMB === b.memoryMB &&
    a.processCount === b.processCount
  );
}

export interface UseResourceMetricsOptions {
  /** Callback to get current resource metrics */
  getResourceMetrics?: () => ResourceMetrics;
  /** Polling interval in ms (default: 2000ms) */
  pollInterval?: number;
  /** Whether polling is enabled (default: true) */
  enabled?: boolean;
}

export interface UseResourceMetricsResult {
  /** Current resource metrics */
  metrics: ResourceMetrics | null;
  /** Whether we've received at least one successful measurement */
  isConnected: boolean;
}

const DEFAULT_POLL_INTERVAL = 2000;

const EMPTY_METRICS: ResourceMetrics = {
  cpuPercent: 0,
  memoryMB: "0",
  processCount: 0,
};

/**
 * Poll for resource metrics at a regular interval.
 * Returns null if no callback is provided.
 */
export function useResourceMetrics(options: UseResourceMetricsOptions): UseResourceMetricsResult {
  const {
    getResourceMetrics,
    pollInterval = DEFAULT_POLL_INTERVAL,
    enabled = true,
  } = options;

  const [metrics, setMetrics] = useState<ResourceMetrics | null>(null);
  const [isConnected, setIsConnected] = useState(false);
  
  // Track previous metrics to avoid unnecessary state updates
  const prevMetricsRef = useRef<ResourceMetrics | null>(null);

  const fetchMetrics = useCallback(() => {
    if (!getResourceMetrics) {
      return;
    }

    try {
      const result = getResourceMetrics();
      // Only update state if metrics actually changed (prevents flickering)
      if (!metricsEqual(result, prevMetricsRef.current)) {
        prevMetricsRef.current = result;
        setMetrics(result);
      }
      // Only update isConnected if it's changing from false to true
      setIsConnected(prev => prev ? prev : true);
    } catch {
      // If the callback fails, keep the last known metrics
      // but mark as disconnected if we haven't connected yet
      if (!isConnected) {
        const empty = EMPTY_METRICS;
        if (!metricsEqual(empty, prevMetricsRef.current)) {
          prevMetricsRef.current = empty;
          setMetrics(empty);
        }
      }
    }
  }, [getResourceMetrics, isConnected]);

  useEffect(() => {
    if (!enabled || !getResourceMetrics) {
      return;
    }

    // Initial fetch
    fetchMetrics();

    // Set up polling interval
    const intervalId = setInterval(fetchMetrics, pollInterval);

    return () => {
      clearInterval(intervalId);
    };
  }, [fetchMetrics, pollInterval, enabled, getResourceMetrics]);

  return {
    metrics,
    isConnected,
  };
}

export default useResourceMetrics;

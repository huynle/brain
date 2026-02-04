/**
 * Hook for streaming and managing logs
 *
 * Features:
 * - Circular buffer with configurable max entries
 * - Auto-timestamp new entries
 * - JSON log line parsing for file-based logs
 * - Integration with existing logger via callback
 */

import { useState, useCallback, useRef, useEffect } from 'react';
import type { LogEntry } from '../types';

// =============================================================================
// Types
// =============================================================================

export interface UseLogStreamOptions {
  /** Maximum number of log entries to keep (default: 100) */
  maxEntries?: number;
  /** Optional log file path to tail (not implemented - for future use) */
  logFile?: string;
}

export interface UseLogStreamResult {
  /** Current log entries */
  logs: LogEntry[];
  /** Add a log entry (timestamp is auto-added) */
  addLog: (entry: Omit<LogEntry, 'timestamp'>) => void;
  /** Clear all log entries */
  clearLogs: () => void;
  /** Parse and add a JSON log line */
  parseAndAddLog: (jsonLine: string) => boolean;
  /** Get a callback function that can be passed to external loggers */
  getLogCallback: () => (entry: Omit<LogEntry, 'timestamp'>) => void;
}

// =============================================================================
// JSON Log Parser
// =============================================================================

/**
 * JSON log line format from logger.ts:
 * {"timestamp":"2026-02-03T23:55:46.878Z","level":"info","message":"...","context":{...}}
 */
interface JsonLogLine {
  timestamp: string;
  level: 'debug' | 'info' | 'warn' | 'error';
  message: string;
  context?: Record<string, unknown>;
}

/**
 * Parse a JSON log line into a LogEntry
 * Returns null if parsing fails
 */
function parseJsonLogLine(line: string): LogEntry | null {
  try {
    const trimmed = line.trim();
    if (!trimmed || !trimmed.startsWith('{')) {
      return null;
    }

    const parsed: JsonLogLine = JSON.parse(trimmed);

    // Validate required fields
    if (!parsed.timestamp || !parsed.level || !parsed.message) {
      return null;
    }

    // Validate level
    const validLevels = ['debug', 'info', 'warn', 'error'];
    if (!validLevels.includes(parsed.level)) {
      return null;
    }

    return {
      timestamp: new Date(parsed.timestamp),
      level: parsed.level,
      message: parsed.message,
      context: parsed.context,
    };
  } catch {
    return null;
  }
}

// =============================================================================
// Hook Implementation
// =============================================================================

/**
 * Manage a stream of log entries with circular buffer
 *
 * @param options - Configuration options
 * @returns Log stream state and controls
 *
 * @example
 * ```tsx
 * const { logs, addLog, clearLogs } = useLogStream({ maxEntries: 50 });
 *
 * // Add a log entry
 * addLog({ level: 'info', message: 'Task started', taskId: 'abc123' });
 *
 * // Parse JSON log line (e.g., from file)
 * parseAndAddLog('{"timestamp":"...","level":"info","message":"..."}');
 *
 * // Clear all logs
 * clearLogs();
 * ```
 */
export function useLogStream(options?: UseLogStreamOptions): UseLogStreamResult {
  const maxEntries = options?.maxEntries ?? 100;

  const [logs, setLogs] = useState<LogEntry[]>([]);

  // Use ref for the callback to avoid stale closure issues
  const addLogRef = useRef<(entry: Omit<LogEntry, 'timestamp'>) => void>(undefined);

  /**
   * Add a new log entry with auto-generated timestamp
   * Maintains circular buffer by removing oldest entries when full
   */
  const addLog = useCallback(
    (entry: Omit<LogEntry, 'timestamp'>) => {
      setLogs((prevLogs) => {
        const newLog: LogEntry = {
          ...entry,
          timestamp: new Date(),
        };

        // Keep only the most recent logs up to maxEntries (FIFO)
        const updatedLogs = [...prevLogs, newLog];
        if (updatedLogs.length > maxEntries) {
          return updatedLogs.slice(-maxEntries);
        }
        return updatedLogs;
      });
    },
    [maxEntries]
  );

  // Keep ref updated
  useEffect(() => {
    addLogRef.current = addLog;
  }, [addLog]);

  /**
   * Clear all log entries
   */
  const clearLogs = useCallback(() => {
    setLogs([]);
  }, []);

  /**
   * Parse a JSON log line and add it to the stream
   * Returns true if parsing succeeded, false otherwise
   */
  const parseAndAddLog = useCallback(
    (jsonLine: string): boolean => {
      const entry = parseJsonLogLine(jsonLine);
      if (entry) {
        setLogs((prevLogs) => {
          const updatedLogs = [...prevLogs, entry];
          if (updatedLogs.length > maxEntries) {
            return updatedLogs.slice(-maxEntries);
          }
          return updatedLogs;
        });
        return true;
      }
      return false;
    },
    [maxEntries]
  );

  /**
   * Get a stable callback function for external logger integration
   * This callback can be passed to the Logger class or other log sources
   */
  const getLogCallback = useCallback(
    (): ((entry: Omit<LogEntry, 'timestamp'>) => void) => {
      return (entry: Omit<LogEntry, 'timestamp'>) => {
        addLogRef.current?.(entry);
      };
    },
    []
  );

  return {
    logs,
    addLog,
    clearLogs,
    parseAndAddLog,
    getLogCallback,
  };
}

export default useLogStream;

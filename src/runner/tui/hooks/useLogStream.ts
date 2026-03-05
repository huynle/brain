/**
 * Hook for streaming and managing logs
 *
 * Features:
 * - Circular buffer with configurable max entries
 * - Auto-timestamp new entries
 * - JSON log line parsing for file-based logs
 * - Integration with existing logger via callback
 * - File persistence: logs are written to disk and restored on restart
 */

import { useState, useCallback, useRef, useEffect } from 'react';
import { existsSync, mkdirSync, readFileSync, appendFileSync, writeFileSync } from 'fs';
import { dirname } from 'path';
import type { LogEntry } from '../types';

// =============================================================================
// Types
// =============================================================================

export interface UseLogStreamOptions {
  /** Maximum number of log entries to keep (default: 100) */
  maxEntries?: number;
  /** Optional log file path for persistence (JSONL format) */
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

// =============================================================================
// File Persistence Helpers
// =============================================================================

/**
 * Serialize a LogEntry to JSON line format for file storage.
 * Converts Date to ISO string for JSON compatibility.
 */
function serializeLogEntry(entry: LogEntry): string {
  return JSON.stringify({
    timestamp: entry.timestamp.toISOString(),
    level: entry.level,
    message: entry.message,
    taskId: entry.taskId,
    projectId: entry.projectId,
    context: entry.context,
  });
}

/**
 * Deserialize a JSON line to LogEntry.
 * Converts ISO string back to Date.
 */
function deserializeLogEntry(line: string): LogEntry | null {
  try {
    const trimmed = line.trim();
    if (!trimmed || !trimmed.startsWith('{')) {
      return null;
    }
    const parsed = JSON.parse(trimmed);
    if (!parsed.timestamp || !parsed.level || !parsed.message) {
      return null;
    }
    return {
      timestamp: new Date(parsed.timestamp),
      level: parsed.level,
      message: parsed.message,
      taskId: parsed.taskId,
      projectId: parsed.projectId,
      context: parsed.context,
    };
  } catch {
    return null;
  }
}

/**
 * Load existing logs from file.
 * Returns empty array if file doesn't exist.
 */
function loadLogsFromFile(filePath: string, maxEntries: number): LogEntry[] {
  try {
    if (!existsSync(filePath)) {
      return [];
    }
    const content = readFileSync(filePath, 'utf-8');
    const lines = content.split('\n').filter(line => line.trim());
    const entries: LogEntry[] = [];
    
    for (const line of lines) {
      const entry = deserializeLogEntry(line);
      if (entry) {
        entries.push(entry);
      }
    }
    
    // Return only the last maxEntries
    if (entries.length > maxEntries) {
      return entries.slice(-maxEntries);
    }
    return entries;
  } catch {
    return [];
  }
}

/**
 * Append a log entry to file.
 * Creates directory if needed.
 */
function appendLogToFile(filePath: string, entry: LogEntry): void {
  try {
    const dir = dirname(filePath);
    if (!existsSync(dir)) {
      mkdirSync(dir, { recursive: true });
    }
    appendFileSync(filePath, serializeLogEntry(entry) + '\n', 'utf-8');
  } catch {
    // Silently fail - don't disrupt TUI for file write errors
  }
}

/**
 * Truncate log file to keep only the last N entries.
 * Called periodically to prevent unbounded file growth.
 */
function truncateLogFile(filePath: string, maxEntries: number): void {
  try {
    if (!existsSync(filePath)) {
      return;
    }
    const content = readFileSync(filePath, 'utf-8');
    const lines = content.split('\n').filter(line => line.trim());
    
    if (lines.length > maxEntries * 2) {
      // Only truncate when we have 2x the max entries to avoid frequent rewrites
      const truncatedLines = lines.slice(-maxEntries);
      writeFileSync(filePath, truncatedLines.join('\n') + '\n', 'utf-8');
    }
  } catch {
    // Silently fail
  }
}

// =============================================================================
// Hook Implementation
// =============================================================================

/**
 * Manage a stream of log entries with circular buffer and optional file persistence
 *
 * @param options - Configuration options
 * @returns Log stream state and controls
 *
 * @example
 * ```tsx
 * const { logs, addLog, clearLogs } = useLogStream({ 
 *   maxEntries: 50,
 *   logFile: '/path/to/logs.jsonl'
 * });
 *
 * // Add a log entry (persisted to file if logFile is set)
 * addLog({ level: 'info', message: 'Task started', taskId: 'abc123' });
 *
 * // Parse JSON log line (e.g., from file)
 * parseAndAddLog('{"timestamp":"...","level":"info","message":"..."}');
 *
 * // Clear all logs (memory only, file not cleared)
 * clearLogs();
 * ```
 */
export function useLogStream(options?: UseLogStreamOptions): UseLogStreamResult {
  const maxEntries = options?.maxEntries ?? 100;
  const logFile = options?.logFile;

  // Load initial logs from file if path is provided
  const [logs, setLogs] = useState<LogEntry[]>(() => {
    if (logFile) {
      return loadLogsFromFile(logFile, maxEntries);
    }
    return [];
  });

  // Track log file path in ref for use in callbacks
  const logFileRef = useRef<string | undefined>(logFile);
  logFileRef.current = logFile;

  // Use ref for the callback to avoid stale closure issues
  const addLogRef = useRef<(entry: Omit<LogEntry, 'timestamp'>) => void>(undefined);

  /**
   * Add a new log entry with auto-generated timestamp
   * Maintains circular buffer by removing oldest entries when full
   * Persists to file if logFile is configured
   */
  const addLog = useCallback(
    (entry: Omit<LogEntry, 'timestamp'>) => {
      const newLog: LogEntry = {
        ...entry,
        timestamp: new Date(),
      };

      // Persist to file if configured
      if (logFileRef.current) {
        appendLogToFile(logFileRef.current, newLog);
      }

      setLogs((prevLogs) => {
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
   * Clear all log entries from memory
   * Note: Does not clear the log file - use for TUI reset only
   */
  const clearLogs = useCallback(() => {
    setLogs([]);
  }, []);

  // Periodically truncate log file to prevent unbounded growth
  useEffect(() => {
    if (!logFile) return;

    // Truncate on mount and every 5 minutes
    truncateLogFile(logFile, maxEntries);
    const interval = setInterval(() => {
      truncateLogFile(logFile, maxEntries);
    }, 5 * 60 * 1000);

    return () => clearInterval(interval);
  }, [logFile, maxEntries]);

  /**
   * Parse a JSON log line and add it to the stream
   * Returns true if parsing succeeded, false otherwise
   * Persists to file if logFile is configured
   */
  const parseAndAddLog = useCallback(
    (jsonLine: string): boolean => {
      const entry = parseJsonLogLine(jsonLine);
      if (entry) {
        // Persist to file if configured
        if (logFileRef.current) {
          appendLogToFile(logFileRef.current, entry);
        }

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

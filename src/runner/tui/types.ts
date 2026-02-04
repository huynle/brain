/**
 * TUI-specific types for the Ink-based dashboard
 */

import type { EntryStatus, Priority } from '../../core/types';

/**
 * Task display information for TUI rendering
 */
export interface TaskDisplay {
  id: string;
  title: string;
  status: EntryStatus;
  priority: Priority;
  dependencies: string[];
  dependents: string[];
  progress?: number;
  error?: string;
}

/**
 * Log entry for the log viewer
 */
export interface LogEntry {
  timestamp: Date;
  level: 'debug' | 'info' | 'warn' | 'error';
  message: string;
  taskId?: string;
  context?: Record<string, unknown>;
}

/**
 * TUI state
 */
export interface TUIState {
  tasks: TaskDisplay[];
  logs: LogEntry[];
  selectedTaskId: string | null;
  isPolling: boolean;
  lastUpdate: Date | null;
  error: string | null;
}

/**
 * TUI configuration
 */
export interface TUIConfig {
  apiUrl: string;
  project: string;
  pollInterval: number;
  maxLogs: number;
}

/**
 * Props for the main App component
 */
export interface AppProps {
  config: TUIConfig;
}

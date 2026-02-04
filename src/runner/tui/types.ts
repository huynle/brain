/**
 * TUI-specific types for the Ink-based dashboard
 */

import type { EntryStatus, Priority } from '../../core/types';

/**
 * Task display information for TUI rendering
 */
export interface TaskDisplay {
  id: string;
  path: string;
  title: string;
  status: EntryStatus;
  priority: Priority;
  dependencies: string[];
  dependents: string[];
  parent_id?: string | null;
  progress?: number;
  error?: string;
  projectId?: string;  // Which project this task belongs to (for multi-project mode)
}

/**
 * Log entry for the log viewer
 */
export interface LogEntry {
  timestamp: Date;
  level: 'debug' | 'info' | 'warn' | 'error';
  message: string;
  taskId?: string;
  projectId?: string;  // Which project this log entry belongs to (for multi-project mode)
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
  project: string;              // Legacy single project (kept for backward compatibility)
  projects?: string[];          // Multiple projects (Phase 2)
  activeProject?: string;       // Currently selected project (or 'all')
  pollInterval: number;
  maxLogs: number;
}

/**
 * Per-project stats tracking (for multi-project mode)
 */
export interface ProjectStats {
  projectId: string;
  stats: {
    ready: number;
    waiting: number;
    inProgress: number;
    completed: number;
    blocked: number;
  };
}

/**
 * Props for the main App component
 */
export interface AppProps {
  config: TUIConfig;
  /** Callback to receive the addLog function for external log integration */
  onLogCallback?: (addLog: (entry: Omit<LogEntry, 'timestamp'>) => void) => void;
  /** Callback to cancel a task by ID and path */
  onCancelTask?: (taskId: string, taskPath: string) => Promise<void>;
}

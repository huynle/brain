/**
 * TUI-specific types for the Ink-based dashboard
 */

import type { EntryStatus, Priority } from '../../core/types';

/**
 * Task classification for dependency resolution
 */
export type TaskClassification = 'ready' | 'waiting' | 'blocked' | 'not_pending';

/**
 * Task display information for TUI rendering
 * Includes all frontmatter fields from the brain entry
 */
export interface TaskDisplay {
  id: string;
  path: string;
  title: string;
  status: EntryStatus;
  priority: Priority;
  dependencies: string[];       // Raw IDs for tree building
  dependents: string[];         // Raw IDs for tree building
  dependencyTitles: string[];   // Titles for display in TaskDetail
  dependentTitles: string[];    // Titles for display in TaskDetail
  progress?: number;
  error?: string;
  projectId?: string;  // Which project this task belongs to (for multi-project mode)
  parent_id?: string;  // Parent task ID for hierarchy
  
  // Feature grouping
  feature_id?: string;           // Feature this task belongs to (e.g., "auth-system")
  feature_priority?: Priority;   // Priority for the feature
  feature_depends_on?: string[]; // Feature IDs this feature depends on
  
  // Frontmatter fields
  created?: string;                  // ISO timestamp when created
  modified?: string;                 // ISO timestamp when last modified
  workdir?: string | null;           // $HOME-relative working directory
  worktree?: string | null;          // Specific git worktree path
  gitRemote?: string | null;         // Git remote URL
  gitBranch?: string | null;         // Branch context
  userOriginalRequest?: string | null; // Original user request for validation
  
  // Dependency resolution fields
  resolvedDeps?: string[];           // IDs of resolved dependencies
  unresolvedDeps?: string[];         // References that couldn't be resolved
  classification?: TaskClassification; // "ready", "waiting", "blocked", "not_pending"
  blockedBy?: string[];              // IDs of blocking dependencies
  blockedByReason?: string;          // "circular_dependency" or "dependency_blocked"
  waitingOn?: string[];              // IDs of incomplete dependencies
  inCycle?: boolean;                 // Whether task is in a dependency cycle
  resolvedWorkdir?: string | null;   // Absolute path after resolution
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
  logDir?: string;              // Directory for log file persistence
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
 * Feature-level statistics for aggregated display
 */
export interface FeatureStats {
  total: number;
  pending: number;
  inProgress: number;
  completed: number;
  blocked: number;
}

/**
 * Feature display information for TUI rendering
 * Groups tasks by feature with aggregated stats
 */
export interface FeatureDisplay {
  id: string;                                         // Feature ID (e.g., "auth-system")
  priority: Priority;                                 // Feature priority (derived from feature_priority or highest task priority)
  status: 'pending' | 'in_progress' | 'completed' | 'blocked';  // Aggregated feature status
  classification: TaskClassification;                 // "ready", "waiting", "blocked" based on feature deps
  tasks: TaskDisplay[];                               // Tasks belonging to this feature
  taskStats: FeatureStats;                            // Aggregated task statistics
  blockedByFeatures: string[];                        // Feature IDs blocking this feature
  waitingOnFeatures: string[];                        // Feature IDs this feature is waiting on
}

/**
 * Resource metrics for running OpenCode processes
 */
export interface ResourceMetrics {
  /** Total CPU usage percentage across all processes */
  cpuPercent: number;
  /** Total memory usage in MB (formatted string) */
  memoryMB: string;
  /** Number of OpenCode processes being tracked */
  processCount: number;
}

/**
 * Per-project limit entry for settings display
 */
export interface ProjectLimitEntry {
  projectId: string;
  limit: number | undefined;  // undefined means "no limit"
  running: number;            // current running count for context
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
  /** Callback to pause a specific project */
  onPause?: (projectId: string) => void | Promise<void>;
  /** Callback to resume a specific project */
  onResume?: (projectId: string) => void | Promise<void>;
  /** Callback to pause all projects */
  onPauseAll?: () => void | Promise<void>;
  /** Callback to resume all projects */
  onResumeAll?: () => void | Promise<void>;
  /** Get current paused projects from TaskRunner */
  getPausedProjects?: () => string[];
  /** Callback to update a task's status */
  onUpdateStatus?: (taskId: string, taskPath: string, newStatus: EntryStatus) => Promise<void>;
  /** Callback to edit a task in external editor. Returns new content or null if cancelled. */
  onEditTask?: (taskId: string, taskPath: string) => Promise<string | null>;
  /** Callback to get the actual count of running OpenCode processes */
  getRunningProcessCount?: () => number;
  /** Callback to get resource metrics (CPU/memory) for running OpenCode processes */
  getResourceMetrics?: () => ResourceMetrics;
  /** Get per-project concurrent task limits */
  getProjectLimits?: () => ProjectLimitEntry[];
  /** Set per-project concurrent task limit (undefined to remove limit) */
  setProjectLimit?: (projectId: string, limit: number | undefined) => void;
}

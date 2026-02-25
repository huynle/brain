/**
 * TUI-specific types for the Ink-based dashboard
 */

import type { EntryStatus, Priority, SessionInfo } from '../../core/types';
import type {
  CronEntry,
  CronDetailResponse,
  CreateCronRequest,
  UpdateCronRequest,
  CronMutationResponse,
  DeleteCronResponse,
  CronRunsResponse,
  CronLinkedTasksResponse,
  CronLinkedTasksMutationResponse,
  CronTriggerResponse,
} from '../api-client';

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
  tags: string[];               // Tags for filtering and categorization
  cron_ids?: string[];          // Cron entry IDs that can trigger this task
  schedule?: string | null;     // Cron expression for periodic execution
  dependencies: string[];       // Raw IDs for tree building
  dependents: string[];         // Raw IDs for tree building
  dependencyTitles: string[];   // Direct dependency titles for display in TaskDetail
  dependentTitles: string[];    // Titles for display in TaskDetail
  indirectAncestorTitles?: string[];  // Transitive (indirect) dependency titles
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
  frontmatter?: Record<string, unknown>; // Raw task frontmatter passthrough
  workdir?: string | null;           // $HOME-relative working directory
  gitRemote?: string | null;         // Git remote URL
  gitBranch?: string | null;         // Branch context (worktree derived from this)
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
  
  // Execution override fields
  agent?: string | null;             // OpenCode agent override (bypasses config default)
  model?: string | null;             // LLM model override (bypasses config default)
  direct_prompt?: string | null;     // Direct prompt (bypasses do-work skill)
  
  // Session tracking
  sessions?: Record<string, SessionInfo>; // Map of session ID to session metadata
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
 * SSE event contract for future real-time transport support.
 */
export type TUISSEEvent =
  | {
      type: 'connected';
      transport: 'sse';
      timestamp: string;
      projectId?: string;
    }
  | {
      type: 'heartbeat';
      transport: 'sse';
      timestamp: string;
      projectId?: string;
    }
  | {
      type: 'tasks_snapshot';
      transport: 'sse';
      timestamp: string;
      projectId: string;
      tasks: TaskDisplay[];
      stats: ProjectStats['stats'];
    }
  | {
      type: 'log_entry';
      transport: 'sse';
      timestamp: string;
      projectId?: string;
      entry: Omit<LogEntry, 'timestamp'>;
    }
  | {
      type: 'error';
      transport: 'sse';
      timestamp: string;
      projectId?: string;
      message: string;
      code?: string;
    };

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
 * Cron display information for TUI rendering
 */
export interface CronDisplay {
  id: string;
  path: string;
  title: string;
  projectId?: string;
  schedule: string;
  next_run?: string;
  attempts_used?: number;
  remaining_runs?: number | null;
  completed_reason?: string;
  window_starts_at_utc?: string;
  window_expires_at_utc?: string;
  status: EntryStatus;
  runs?: Array<{
    run_id: string;
    status: 'completed' | 'failed' | 'skipped' | 'in_progress';
    started: string;
    completed?: string;
    duration?: number;
    tasks?: number;
    failed_task?: string;
    skip_reason?: string;
  }>;
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
 * Group visibility entry for settings display
 * Each status group can be toggled visible/hidden and expanded/collapsed
 */
export interface GroupVisibilityEntry {
  status: string;             // Status value (e.g., "completed", "draft")
  label: string;              // Display label (e.g., "Completed", "Draft")
  visible: boolean;           // Whether to show this group
  collapsed: boolean;         // Whether the group is collapsed
  taskCount: number;          // Number of tasks in this group
}

/**
 * Supported mouse buttons for TUI interactions.
 */
export type TUIMouseButton = 'left' | 'right' | 'middle' | 'none';

/**
 * Supported mouse buttons for press interactions.
 */
export type TUIPressMouseButton = Exclude<TUIMouseButton, 'none'>;

/**
 * Normalized mouse event contract used by App interaction routing.
 */
export type TUIMouseEvent =
  | {
      kind: 'press';
      button: TUIPressMouseButton;
      row: number;
      column: number;
    }
  | {
      kind: 'move';
      button: TUIMouseButton;
      row: number;
      column: number;
    };

/**
 * Logical row kinds rendered in the TaskTree.
 */
export type TaskTreeRowKind =
  | 'task'
  | 'feature_header'
  | 'project_header'
  | 'status_header'
  | 'status_feature_header'
  | 'ungrouped_header'
  | 'spacer'
  | 'unknown';

/**
 * Semantic target resolved from a visible TaskTree row.
 */
export interface TaskTreeRowTarget {
  kind: TaskTreeRowKind;
  id: string;
  taskId?: string;
  featureId?: string;
  projectId?: string;
  statusGroup?: 'completed' | 'draft' | 'cancelled' | 'superseded' | 'archived';
}

/**
 * Visible-row hit-test record for click routing.
 */
export interface TaskTreeVisibleRow {
  row: number;
  target: TaskTreeRowTarget;
}

/**
 * Settings popup section type
 */
export type SettingsSection = 'limits' | 'groups' | 'runtime';

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
  /** Callback to execute a task manually. Returns true if task was started, false otherwise. */
  onExecuteTask?: (taskId: string, taskPath: string) => Promise<boolean>;
  /** Callback to execute all ready tasks for a feature. Returns number of tasks started. */
  onExecuteFeature?: (featureId: string) => Promise<number>;
  /** Callback to get the actual count of running OpenCode processes */
  getRunningProcessCount?: () => number;
  /** Callback to get resource metrics (CPU/memory) for running OpenCode processes */
  getResourceMetrics?: () => ResourceMetrics;
  /** Get per-project concurrent task limits */
  getProjectLimits?: () => ProjectLimitEntry[];
  /** Set per-project concurrent task limit (undefined to remove limit) */
  setProjectLimit?: (projectId: string, limit: number | undefined) => void;
  /** Get in-memory runtime default model override */
  getRuntimeDefaultModel?: () => string | undefined;
  /** Set in-memory runtime default model override (undefined/empty clears override) */
  setRuntimeDefaultModel?: (model: string | undefined) => void;
  /** Enable a feature to run while project is paused (whitelist) */
  onEnableFeature?: (featureId: string) => void;
  /** Disable a feature from whitelist */
  onDisableFeature?: (featureId: string) => void;
  /** Get currently enabled features from TaskRunner */
  getEnabledFeatures?: () => string[];
  /** Callback to update entry metadata fields */
  onUpdateMetadata?: (
    taskPath: string,
    fields: {
      status?: EntryStatus;
      feature_id?: string;
      git_branch?: string;
      target_workdir?: string;
      schedule?: string;
      agent?: string;
      model?: string;
      direct_prompt?: string;
    }
  ) => Promise<void>;
  /** Callback to move a task to a different project */
  onMoveTask?: (
    taskPath: string,
    newProjectId: string
  ) => Promise<{ oldPath: string; newPath: string }>;
  /** Callback to list all available projects from the API (not just monitored projects) */
  onListProjects?: () => Promise<string[]>;
  /** Callback to delete tasks completely from the brain. Used by multi-select + backspace. */
  onDeleteTasks?: (taskPaths: string[]) => Promise<void>;
  /** Callback to open an OpenCode session in fullscreen mode. Used by 'o' key on tasks with session_ids. */
  onOpenSession?: (sessionId: string) => Promise<void>;
  /** Callback to open an OpenCode session in a new tmux window. Used by 'O' key on tasks with session_ids. */
  onOpenSessionTmux?: (sessionId: string, taskContext?: OpenSessionTaskContext) => Promise<void>;
  /** Callback to list cron entries for a project. */
  onListCrons?: (projectId: string) => Promise<CronEntry[]>;
  /** Callback to fetch one cron and its pipeline details. */
  onGetCron?: (projectId: string, cronId: string) => Promise<CronDetailResponse>;
  /** Callback to create a cron entry. */
  onCreateCron?: (projectId: string, request: CreateCronRequest) => Promise<CronMutationResponse>;
  /** Callback to update a cron entry. */
  onUpdateCron?: (
    projectId: string,
    cronId: string,
    request: UpdateCronRequest
  ) => Promise<CronMutationResponse>;
  /** Callback to delete a cron entry. */
  onDeleteCron?: (projectId: string, cronId: string) => Promise<DeleteCronResponse>;
  /** Callback to fetch cron run history. */
  onGetCronRuns?: (projectId: string, cronId: string) => Promise<CronRunsResponse>;
  /** Callback to fetch linked tasks for a cron. */
  onGetCronLinkedTasks?: (projectId: string, cronId: string) => Promise<CronLinkedTasksResponse>;
  /** Callback to replace linked tasks for a cron. */
  onSetCronLinkedTasks?: (
    projectId: string,
    cronId: string,
    taskIds: string[]
  ) => Promise<CronLinkedTasksMutationResponse>;
  /** Callback to add a linked task to a cron. */
  onAddCronLinkedTask?: (
    projectId: string,
    cronId: string,
    taskId: string
  ) => Promise<CronLinkedTasksMutationResponse>;
  /** Callback to remove a linked task from a cron. */
  onRemoveCronLinkedTask?: (
    projectId: string,
    cronId: string,
    taskId: string
  ) => Promise<CronLinkedTasksMutationResponse>;
  /** Callback to trigger a cron run immediately. */
  onTriggerCron?: (projectId: string, cronId: string) => Promise<CronTriggerResponse>;
}

/** Context needed to track a reopened session for idle monitoring */
export interface OpenSessionTaskContext {
  taskId: string;
  path: string;
  title: string;
  priority: Priority;
  projectId: string;
  workdir: string;
}

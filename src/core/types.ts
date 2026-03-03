/**
 * Brain API - Core Types
 *
 * Type definitions extracted from the OpenCode brain plugin
 */

// =============================================================================
// Entry Types
// =============================================================================

export const ENTRY_TYPES = [
  "summary",
  "report",
  "walkthrough",
  "plan",
  "pattern",
  "learning",
  "idea",
  "scratch",
  "decision",
  "exploration",
  "execution",
  "task",
] as const;

export type EntryType = (typeof ENTRY_TYPES)[number];

// =============================================================================
// Entry Statuses
// =============================================================================

export const ENTRY_STATUSES = [
  "draft", // Initial state, not ready
  "pending", // Queued, waiting to be worked on
  "active", // Ready/in use (default)
  "in_progress", // Actively being worked on
  "blocked", // Waiting on something
  "cancelled", // User-cancelled task
  "completed", // Done/implemented
  "validated", // Implementation verified working
  "superseded", // Replaced by another entry
  "archived", // No longer relevant
] as const;

export type EntryStatus = (typeof ENTRY_STATUSES)[number];

// =============================================================================
// Priority
// =============================================================================

export const PRIORITIES = ["high", "medium", "low"] as const;
export type Priority = (typeof PRIORITIES)[number];

export const GENERATED_KINDS = ["feature_checkout", "feature_review", "gap_task", "other"] as const;
export type GeneratedKind = (typeof GENERATED_KINDS)[number];

export const MERGE_POLICIES = ["prompt_only", "auto_pr", "auto_merge"] as const;
export type MergePolicy = (typeof MERGE_POLICIES)[number];

export const MERGE_STRATEGIES = ["squash", "merge", "rebase"] as const;
export type MergeStrategy = (typeof MERGE_STRATEGIES)[number];

export const REMOTE_BRANCH_POLICIES = ["keep", "delete"] as const;
export type RemoteBranchPolicy = (typeof REMOTE_BRANCH_POLICIES)[number];

export const EXECUTION_MODES = ["worktree", "current_branch"] as const;
export type ExecutionMode = (typeof EXECUTION_MODES)[number];

export type SessionInfo = {
  timestamp: string;
  cron_id?: string;
  run_id?: string;
};

export interface RunFinalization {
  status: EntryStatus;
  finalized_at: string; // ISO timestamp
  session_id?: string;
}

export interface CronRun {
  run_id: string; // "YYYYMMDD-HHmm" from scheduled trigger time
  status: "completed" | "failed" | "skipped" | "in_progress";
  started: string; // ISO timestamp
  completed?: string; // ISO timestamp
  duration?: number; // ms
  tasks?: number; // number of tasks in this run
  failed_task?: string; // task ID if a task failed
  skip_reason?: string; // reason if skipped (e.g., "task X in_progress")
}

// =============================================================================
// ZK Note (from zk CLI output)
// =============================================================================

export interface ZkNote {
  path: string;
  title: string;
  lead?: string;
  body?: string;
  rawContent?: string;
  wordCount?: number;
  tags?: string[];
  metadata?: Record<string, unknown>;
  created?: string;
  modified?: string;
}

// =============================================================================
// Entry Metadata (SQLite)
// =============================================================================

export interface EntryMeta {
  path: string;
  project_id: string;
  access_count: number;
  accessed_at: number | null;
  last_verified: number | null;
  created_at: number;
}

// =============================================================================
// Brain Entry (Full)
// =============================================================================

export interface BrainEntry {
  id: string; // 8-char alphanumeric
  path: string; // Full path in brain
  title: string;
  type: EntryType;
  status: EntryStatus;
  content: string; // Markdown body
  tags: string[];
  priority?: Priority;
  depends_on?: string[];
  project_id?: string;
  created?: string; // ISO timestamp
  modified?: string; // ISO timestamp
  access_count?: number;
  last_verified?: string;
  schedule?: string; // cron expression e.g. "0 2 * * *"
  schedule_enabled?: boolean; // whether the schedule is active (default true when schedule exists)
  next_run?: string; // ISO timestamp of next scheduled run
  max_runs?: number; // optional execution cap for this cron
  attempts_used?: number; // derived attempted run count for cron visibility
  remaining_runs?: number | null; // derived remaining attempts before max_runs is exhausted
  completed_reason?: string; // derived terminal reason when cron can no longer execute
  window_starts_at_utc?: string; // normalized UTC starts_at value for display contracts
  window_expires_at_utc?: string; // normalized UTC expires_at value for display contracts
  starts_at?: string; // optional ISO timestamp gate (inclusive lower bound)
  expires_at?: string; // optional ISO timestamp gate (inclusive upper bound)
  runs?: CronRun[]; // for cron entries: execution history

  // Execution context for tasks
  target_workdir?: string; // Explicit workdir override for task execution (absolute path)
  workdir?: string; // $HOME-relative path to main repo
  worktree?: string; // Specific git worktree path (legacy, prefer git_branch + ensureWorktree)
  git_remote?: string; // Git remote URL for verification
  git_branch?: string; // Branch context when task was created
  merge_target_branch?: string; // Branch to merge completed work into
  merge_policy?: MergePolicy; // Merge behavior at checkout completion
  merge_strategy?: MergeStrategy; // Git merge strategy used when auto-merging
  remote_branch_policy?: RemoteBranchPolicy; // Remote branch cleanup policy after successful auto-merge
  open_pr_before_merge?: boolean; // Whether to open PR before merge
  execution_mode?: ExecutionMode; // How task executes: worktree or current branch
  checkout_enabled?: boolean; // Whether dedicated checkout/worktree flow is enabled
  complete_on_idle?: boolean; // Mark task completed instead of blocked when agent goes idle

  // User intent for validation
  user_original_request?: string; // Verbatim user request for validation during task completion

  // Session traceability
  sessions?: Record<string, SessionInfo>; // Map of session ID to session metadata
  // Durable run completion markers keyed by run_id
  run_finalizations?: Record<string, RunFinalization>;

  // Generated task metadata
  generated?: boolean;
  generated_kind?: GeneratedKind;
  generated_key?: string;
  generated_by?: string;
}

// =============================================================================
// API Request/Response Types
// =============================================================================

export interface CreateEntryRequest {
  type: EntryType;
  title: string;
  content: string;
  tags?: string[];
  status?: EntryStatus;
  priority?: Priority;
  depends_on?: string[];
  global?: boolean;
  project?: string;
  relatedEntries?: string[];
  schedule?: string;
  schedule_enabled?: boolean;
  next_run?: string;
  max_runs?: number;
  starts_at?: string;
  expires_at?: string;
  run_once_at?: string; // loose datetime input; normalized to UTC by API layer
  runs?: CronRun[];

  // Execution context for tasks
  target_workdir?: string; // Explicit workdir override for task execution (absolute path)
  workdir?: string;
  git_remote?: string;
  git_branch?: string;
  merge_target_branch?: string;
  merge_policy?: MergePolicy;
  merge_strategy?: MergeStrategy;
  remote_branch_policy?: RemoteBranchPolicy;
  open_pr_before_merge?: boolean;
  execution_mode?: ExecutionMode;
  checkout_enabled?: boolean;
  complete_on_idle?: boolean;

  // User intent for validation
  user_original_request?: string; // Verbatim user request for validation during task completion

  // Feature grouping (for task organization)
  feature_id?: string;
  feature_priority?: Priority;
  feature_depends_on?: string[];

  // OpenCode execution options (task-specific)
  direct_prompt?: string;
  agent?: string;
  model?: string;

  // Session traceability
  sessions?: Record<string, SessionInfo>;
  run_finalizations?: Record<string, RunFinalization>;

  // Generated task metadata
  generated?: boolean;
  generated_kind?: GeneratedKind;
  generated_key?: string;
  generated_by?: string;
}

export interface CreateEntryResponse {
  id: string;
  path: string;
  title: string;
  type: EntryType;
  status: EntryStatus;
  link: string;
}

export interface UpdateEntryRequest {
  status?: EntryStatus;
  title?: string;
  content?: string; // Full content replacement (for external editor workflows)
  append?: string;
  note?: string;
  depends_on?: string[];
  tags?: string[];
  priority?: Priority;
  schedule?: string;
  schedule_enabled?: boolean;
  next_run?: string;
  max_runs?: number;
  starts_at?: string;
  expires_at?: string;
  run_once_at?: string; // loose datetime input; normalized to UTC by API layer
  runs?: CronRun[];
  target_workdir?: string;
  git_branch?: string;
  merge_target_branch?: string;
  merge_policy?: MergePolicy;
  merge_strategy?: MergeStrategy;
  remote_branch_policy?: RemoteBranchPolicy;
  open_pr_before_merge?: boolean;
  execution_mode?: ExecutionMode;
  checkout_enabled?: boolean;
  complete_on_idle?: boolean;
  // Feature grouping (for task organization)
  feature_id?: string;
  feature_priority?: Priority;
  feature_depends_on?: string[];
  // OpenCode execution options (task-specific)
  direct_prompt?: string;
  agent?: string;
  model?: string;

  // Session traceability (append semantics - new entries are merged by session ID)
  sessions?: Record<string, SessionInfo>;
  run_finalizations?: Record<string, RunFinalization>;

  // Generated task metadata
  generated?: boolean;
  generated_kind?: GeneratedKind;
  generated_key?: string;
  generated_by?: string;
}

export interface FeatureCheckoutRequest {
  execution_branch?: string;
  merge_target_branch?: string;
  merge_policy?: MergePolicy;
  merge_strategy?: MergeStrategy;
  remote_branch_policy?: RemoteBranchPolicy;
  open_pr_before_merge?: boolean;
  execution_mode?: ExecutionMode;
}

export interface ListEntriesRequest {
  type?: EntryType;
  status?: EntryStatus;
  feature_id?: string;
  filename?: string;
  tags?: string[];
  limit?: number;
  offset?: number;
  global?: boolean;
  sortBy?: "created" | "modified" | "priority";
}

export interface ListEntriesResponse {
  entries: BrainEntry[];
  total: number;
  limit: number;
  offset: number;
}

export interface SearchRequest {
  query: string;
  type?: EntryType;
  status?: EntryStatus;
  feature_id?: string;
  tags?: string[];
  limit?: number;
  global?: boolean;
}

export interface SearchResponse {
  results: BrainEntry[];
  total: number;
}

export interface InjectRequest {
  query: string;
  maxEntries?: number;
  type?: EntryType;
}

export interface InjectResponse {
  context: string;
  entries: BrainEntry[];
}

export interface LinkRequest {
  title?: string;
  path?: string;
  withTitle?: boolean;
}

export interface LinkResponse {
  link: string;
  id: string;
  path: string;
  title: string;
}

export interface StatsResponse {
  zkAvailable: boolean;
  zkVersion: string | null;
  notebookExists: boolean;
  brainDir: string;
  dbPath: string;
  totalEntries: number;
  globalEntries: number;
  projectEntries: number;
  byType: Record<string, number>;
  orphanCount: number;
  trackedEntries: number;
  staleCount: number;
}

export interface HealthResponse {
  status: "healthy" | "degraded" | "unhealthy";
  zkAvailable: boolean;
  dbAvailable: boolean;
  timestamp: string;
}

// =============================================================================
// Configuration
// =============================================================================

export interface BrainConfig {
  brainDir: string;
  dbPath: string;
  defaultProject: string;
}

export interface TlsConfig {
  enabled: boolean;
  keyPath?: string;
  certPath?: string;
}

export interface ServerConfig {
  port: number;
  host: string;
  logLevel: "debug" | "info" | "warn" | "error";
  enableAuth: boolean;
  apiKey?: string;
  /** PIN required on OAuth consent page (set via OAUTH_PIN env var) */
  oauthPin?: string;
  enableTenants: boolean;
  tls: TlsConfig;
}

export interface Config {
  brain: BrainConfig;
  server: ServerConfig;
}

// =============================================================================
// Task Classification (for dependency resolution)
// =============================================================================

export const TASK_CLASSIFICATIONS = [
  "ready", // Pending, all deps satisfied
  "waiting", // Pending, waiting on incomplete deps
  "blocked", // Blocked by blocked/cancelled deps
  "not_pending", // Task is not in pending status
] as const;

export type TaskClassification = (typeof TASK_CLASSIFICATIONS)[number];

// =============================================================================
// Task Types
// =============================================================================

// Raw task from zk query (before dependency resolution)
export interface Task {
  id: string;
  path: string;
  title: string;
  priority: Priority;
  status: EntryStatus;
  depends_on: string[];
  tags: string[]; // Tags for filtering and categorization
  created: string;
  modified?: string; // ISO timestamp when last modified
  target_workdir: string | null; // Explicit workdir override for task execution (absolute path)
  workdir: string | null;
  worktree: string | null; // Specific git worktree path (legacy, prefer git_branch + ensureWorktree)
  git_remote: string | null;
  git_branch: string | null;
  merge_target_branch?: string | null;
  merge_policy?: MergePolicy;
  merge_strategy?: MergeStrategy;
  remote_branch_policy?: RemoteBranchPolicy;
  open_pr_before_merge?: boolean;
  execution_mode?: ExecutionMode;
  checkout_enabled?: boolean;
  complete_on_idle?: boolean;
  user_original_request: string | null; // Verbatim user request for validation during task completion

  // Schedule fields (from cron/scheduled tasks)
  schedule?: string; // cron expression e.g. "0 2 * * *"
  schedule_enabled?: boolean; // whether the schedule is active (default true)
  next_run?: string; // ISO timestamp of next scheduled run
  max_runs?: number; // optional execution cap
  starts_at?: string; // optional ISO timestamp gate (inclusive lower bound)
  expires_at?: string; // optional ISO timestamp gate (inclusive upper bound)
  runs?: CronRun[]; // execution history

  // Feature grouping (optional)
  feature_id?: string; // e.g., "auth-system", "payment-flow"
  feature_priority?: Priority; // Priority for this feature
  feature_depends_on?: string[]; // Feature IDs this feature depends on

  // OpenCode execution options (optional)
  direct_prompt: string | null; // Direct prompt to execute, bypassing do-work skill workflow
  agent: string | null; // Override agent for this task (e.g., "explore", "tdd-dev", "build")
  model: string | null; // Override model for this task (e.g., "anthropic/claude-sonnet-4-20250514")

  // Session traceability
  sessions: Record<string, SessionInfo>; // Map of session ID to session metadata

  // Generated task metadata
  generated?: boolean;
  generated_kind?: GeneratedKind;
  generated_key?: string;
  generated_by?: string;

  // Raw task frontmatter for UI rendering/debugging
  frontmatter?: Record<string, unknown>;

  // Derived fields
  projectId?: string; // Derived from file path (e.g., "pwa" from "projects/pwa/task/...")
}

// Task with resolved dependencies
export interface ResolvedTask extends Task {
  resolved_deps: string[]; // IDs of resolved dependencies
  unresolved_deps: string[]; // References that couldn't be resolved
  classification: TaskClassification;
  blocked_by: string[]; // IDs of blocking deps
  blocked_by_reason?: string; // "circular_dependency" | "dependency_blocked"
  waiting_on: string[]; // IDs of incomplete deps
  in_cycle: boolean;
  resolved_workdir: string | null; // Absolute path after resolution
}

// Dependency resolution result
export interface DependencyResult {
  tasks: ResolvedTask[];
  cycles: string[][]; // Groups of task IDs in cycles
  stats: {
    total: number;
    ready: number;
    waiting: number;
    blocked: number;
    not_pending: number;
  };
}

// =============================================================================
// Task API Request/Response Types
// =============================================================================

export interface TaskListResponse {
  tasks: ResolvedTask[];
  count: number;
  stats?: DependencyResult["stats"];
}

export interface TaskNextResponse {
  task: ResolvedTask | null;
  message?: string;
}

// =============================================================================
// Task Claiming Types
// =============================================================================

export interface TaskClaim {
  runnerId: string;
  claimedAt: number; // Unix timestamp in milliseconds
}

export interface ClaimRequest {
  runnerId: string;
}

export interface ClaimResponse {
  success: boolean;
  taskId: string;
  runnerId: string;
  claimedAt?: string; // ISO timestamp
}

export interface ClaimConflictResponse {
  success: false;
  error: "conflict";
  message: string;
  taskId: string;
  claimedBy: string;
  claimedAt: string; // ISO timestamp
  isStale: boolean;
}

export interface ReleaseResponse {
  success: boolean;
  taskId?: string;
  message?: string;
}

export interface ClaimStatusResponse {
  claimed: boolean;
  claimedBy?: string;
  claimedAt?: string; // ISO timestamp
  isStale?: boolean;
}

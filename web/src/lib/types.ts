// Domain types mirroring the brain-api wire format. Field names match the Go
// json struct tags exactly (mostly snake_case, with a few camelCase fields in
// SSE/claim payloads — copied verbatim).

export type TaskStatus =
  | "draft"
  | "pending"
  | "active"
  | "in_progress"
  | "blocked"
  | "cancelled"
  | "completed"
  | "validated"
  | "superseded"
  | "archived";

export const ALL_STATUSES: TaskStatus[] = [
  "draft",
  "pending",
  "active",
  "in_progress",
  "blocked",
  "cancelled",
  "completed",
  "validated",
  "superseded",
  "archived",
];

export type Priority = "high" | "medium" | "low" | string;

export interface SessionInfo {
  timestamp: string;
  cron_id?: string;
  run_id?: string;
}

export interface AttachmentReference {
  id: string;
  type?: string;
  path?: string;
  filename?: string;
  provider?: string;
  model?: string;
  [k: string]: unknown;
}

/** ResolvedTask — the task object returned by the tasks endpoints + SSE. */
export interface Task {
  id: string;
  path: string;
  title: string;
  priority: Priority;
  status: TaskStatus;
  parent_id?: string;
  depends_on?: string[];
  created?: string;
  projectId?: string;

  workdir?: string;
  git_branch?: string;
  execution_mode?: string;
  merge_policy?: string;
  merge_strategy?: string;

  feature_id?: string;
  feature_priority?: string;
  feature_depends_on?: string[];

  schedule?: string;
  schedule_enabled?: boolean;
  next_run?: string;
  run_once_at?: string;

  user_original_request?: string;
  direct_prompt?: string;
  content?: string;
  agent?: string;
  model?: string;
  executor?: string;
  extensions?: string[];
  complete_on_idle?: boolean;
  target_workdir?: string;
  env?: Record<string, string>;

  sessions?: Record<string, SessionInfo>;
  tags?: string[];

  generated?: boolean;
  generated_kind?: string;

  // dependency resolution metadata
  resolved_deps?: string[];
  unresolved_deps?: string[];
  classification?: string;
  blocked_by?: string[];
  blocked_by_reason?: string;
  waiting_on?: string[];
  in_cycle?: boolean;
  resolved_workdir?: string;
}

export interface TaskStats {
  total: number;
  ready: number;
  waiting: number;
  blocked: number;
  not_pending: number;
}

export interface TaskListResponse {
  tasks: Task[];
  count: number;
  stats?: TaskStats;
  cycles?: string[][];
}

export interface ProjectListResponse {
  projects: string[];
}

export interface LogLine {
  timestamp: string;
  level: string;
  content: string;
}

export interface LogQueryResponse {
  lines: LogLine[];
  total: number;
  offset: number;
  limit: number;
}

export interface RunnerInfo {
  runner_id: string;
  hostname: string;
  labels?: Record<string, string>;
  executors?: string[];
  projects?: string[];
  capabilities?: string[];
  max_parallel: number;
  active_tasks?: number;
  feature_assignments?: FeatureAssignment[];
  registered_at: string;
  last_heartbeat: string;
  status: "online" | "stale" | "offline";
  version?: string;
}

export interface FeatureAssignment {
  feature_id: string;
  project_id?: string;
  executor?: string;
  [k: string]: unknown;
}

export interface RunnerListResponse {
  runners: RunnerInfo[];
  total: number;
}

export interface RunnerStatusResponse {
  running: boolean;
  paused: boolean;
  pausedProjects: string[];
  automationsPaused: boolean;
}

// ─── Brain entries ───────────────────────────────────────────────

export interface BrainEntry {
  id: string;
  path: string;
  title: string;
  type: string;
  status: string;
  content: string;
  tags?: string[];
  priority?: string;
  attachments?: AttachmentReference[];
  embedding_status?: string;
  parent_id?: string;
  project_id?: string;
  feature_id?: string;
  created?: string;
  modified?: string;
  agent?: string;
  model?: string;
}

export interface ListEntriesResponse {
  entries: BrainEntry[];
  total: number;
  limit: number;
  offset: number;
}

export type SearchStrategy = "semantic" | "fts" | "hybrid";

export interface SearchRequest {
  query: string;
  type?: string;
  status?: string;
  feature_id?: string;
  tags?: string[];
  include?: string[];
  limit?: number;
  global?: boolean;
  project?: string;
  strategy?: SearchStrategy;
}

export interface SearchResult {
  id: string;
  path: string;
  title: string;
  type: string;
  status: string;
  snippet: string;
  match_source?: string;
  attachments?: AttachmentReference[];
}

export interface SearchResponse {
  results: SearchResult[];
  total: number;
}

// ─── Goals / automations ─────────────────────────────────────────

export interface GoalConfig {
  id: string;
  criteria?: string;
  validation?: string;
  workdir?: string;
  trigger_source?: string;
  complete_statuses?: string[];
  blocked_statuses?: string[];
}

export interface AutomationAction {
  session_mode?: string;
  agent?: string;
  model?: string;
  executor?: string;
  [k: string]: unknown;
}

export interface TriggerConfig {
  event?: string;
  filter?: Record<string, unknown>;
  cooldown?: string;
  max_concurrent?: number;
  [k: string]: unknown;
}

export interface GoalSummary {
  entry_id: string;
  goal_id: string;
  title: string;
  project?: string;
  feature_id?: string;
  status: string;
  config?: GoalConfig;
  action?: AutomationAction;
  trigger?: TriggerConfig;
}

export interface GoalListResponse {
  goals: GoalSummary[];
  count: number;
}

export interface LinkedTaskSnapshot {
  task_id: string;
  title?: string;
  status?: string;
  [k: string]: unknown;
}

export interface GoalProgressResponse {
  goal_id: string;
  entry_id: string;
  project?: string;
  feature_id?: string;
  feature_status: string;
  total: number;
  pending: number;
  in_progress: number;
  completed: number;
  blocked: number;
  tasks: LinkedTaskSnapshot[];
}

export interface GoalReconcileAudit {
  timestamp: string;
  goal_id: string;
  project?: string;
  feature_id?: string;
  triggering_event: string;
  event_id?: string;
  decision: "complete" | "block" | "need_work" | "noop" | string;
  reason: string;
  linked_tasks?: LinkedTaskSnapshot[];
  generated_task_id?: string;
}

export interface GoalAuditResponse {
  audit: GoalReconcileAudit[];
  count: number;
}

export interface UpdateGoalRequest {
  title?: string;
  content?: string;
  status?: string;
  criteria?: string;
  validation?: string;
  workdir?: string;
  trigger_source?: string;
  complete_statuses?: string[];
  blocked_statuses?: string[];
  action?: AutomationAction;
}

export interface CreateGoalRequest {
  project: string;
  feature_id?: string;
  title: string;
  content?: string;
  config: GoalConfig; // requires a non-empty `id`
  action: AutomationAction; // requires a non-empty `type`
}

// ─── SSE payloads ────────────────────────────────────────────────

export interface SSEBase {
  type: string;
  transport: "sse";
  timestamp: string;
  projectId: string;
}

export interface SSETasksSnapshot extends SSEBase {
  type: "tasks_snapshot";
  tasks: Task[];
  count: number;
  stats?: TaskStats;
  cycles?: string[][];
}

export interface SSERunnerLog extends SSEBase {
  type: "runner_log";
  taskId: string;
  runnerId: string;
  lines: LogLine[];
}

export interface SSERunnersUpdate extends SSEBase {
  type: "runners_update";
  runners: RunnerInfo[];
  total: number;
}

export type Health = {
  status: string;
  embedding?: { enabled?: boolean; ready?: boolean; [k: string]: unknown };
  [k: string]: unknown;
};

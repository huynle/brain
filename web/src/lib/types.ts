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
  // Where the session lives — recorded at discovery so it can be re-opened
  // after the task's live instance is gone.
  runner_id?: string;
  machine_id?: string;
  hostname?: string;
  workdir?: string;
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

// A single HTTP request handled by the Brain server, for the global Logs tab.
export interface ServerRequest {
  seq: number;
  time: number; // unix ms
  method: string;
  path: string;
  status: number;
  duration_ms: number;
  actor_type: string; // "api_token" | "oauth" | "anonymous"
  actor_name: string;
  request_id?: string;
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
  machine_id?: string;
  bridge_connected?: boolean;
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

// ─── OpenCode instances (remote control) ─────────────────────────

export type InstanceKind = "task" | "adhoc";
export type InstanceStatus = "starting" | "idle" | "busy" | "exited";

export interface OpencodeInstance {
  instance_id: string;
  runner_id: string;
  hostname?: string;
  kind: InstanceKind;
  project_id?: string;
  task_id?: string;
  title?: string;
  workdir?: string;
  port?: number;
  pid?: number;
  session_ids?: string[];
  status: InstanceStatus;
  executor?: string;
  started_at?: number; // unix ms
  last_seen?: number; // unix ms
  // Live bridge decorations (present when the runner bridge is connected)
  pending_permissions?: number;
  bridge_connected?: boolean;
}

export interface InstanceListResponse {
  instances: OpencodeInstance[];
  total: number;
}

export interface SpawnInstanceSpec {
  workdir: string;
  agent?: string;
  model?: string;
  title?: string;
}

// ─── Remote control: OpenCode session/message shapes ─────────────
// Mirrors the subset of OpenCode's API the chat UI renders. Extra fields
// flow through untouched.

export interface OcSession {
  id: string;
  title?: string;
  slug?: string; // OpenCode's memorable auto-generated name (e.g. "hidden-engine")
  version?: string;
  time?: { created?: number; updated?: number };
  [k: string]: unknown;
}

// sessionName returns OpenCode's friendly session name: the slug, falling back
// to a meaningful title, then the raw id. OpenCode leaves `title` as the
// default "New session - <timestamp>" until/unless it summarizes, so the slug
// is the human-readable name shown in the OpenCode TUI.
export function sessionName(s: OcSession): string {
  if (s.slug) return s.slug;
  if (s.title && !/^New session - /.test(s.title)) return s.title;
  return s.id;
}

export interface OcMessageInfo {
  id: string;
  sessionID?: string;
  role: "user" | "assistant" | string;
  time?: { created?: number; completed?: number };
  error?: unknown;
  [k: string]: unknown;
}

export interface OcPart {
  id: string;
  messageID?: string;
  sessionID?: string;
  type: string; // "text" | "reasoning" | "tool" | "step-start" | "step-finish" | ...
  text?: string;
  tool?: string;
  callID?: string;
  state?: {
    status?: string; // "pending" | "running" | "completed" | "error"
    title?: string;
    input?: unknown;
    output?: string;
    error?: string;
    [k: string]: unknown;
  };
  [k: string]: unknown;
}

export interface OcMessage {
  info: OcMessageInfo;
  parts: OcPart[];
}

export interface OcPermission {
  id: string;
  sessionID?: string;
  messageID?: string;
  title?: string;
  type?: string;
  pattern?: string;
  metadata?: Record<string, unknown>;
  [k: string]: unknown;
}

export interface OcEvent {
  type: string;
  properties?: Record<string, unknown> & {
    info?: Record<string, unknown>;
    part?: OcPart;
    sessionID?: string;
  };
}

export interface OcAgent {
  name: string;
  description?: string;
  mode?: string;
  [k: string]: unknown;
}

export interface OcProvider {
  id: string;
  name?: string;
  models?: Record<string, { id?: string; name?: string }>;
  [k: string]: unknown;
}

export interface RunnerStatusResponse {
  running: boolean;
  paused: boolean;
  pausedProjects: string[];
  automationsPaused: boolean;
  automationPausedProjects: string[];
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
  // Automation / scheduling fields (present on type=automation and type=task).
  generated_by?: string;
  automation_run_id?: string;
  trigger?: TriggerConfig;
  schedule?: string;
  schedule_enabled?: boolean;
  run_once_at?: string;
  // Present on a GET of a single automation entry (used to execute it) and on
  // task entries (sessions discovered by the runner).
  action?: AutomationAction;
  executor?: string;
  execution_mode?: string;
  target_workdir?: string;
  complete_on_idle?: boolean;
  sessions?: Record<string, SessionInfo>;
}

// GoalGeneratedBy marks an automation entry as a goal automation.
export const GOAL_GENERATED_BY = "brain-goal";

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
  type?: string; // "event" | "cron" | "webhook" | "session"
  event?: string;
  schedule?: string;
  webhook?: string;
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

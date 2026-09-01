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
  content_type?: string;
  size?: number;
  sha256?: string;
  metadata?: Record<string, string>;
  download_url?: string;
  text_url?: string;
  role?: string;
  caption?: string;
  derived?: AttachmentDerived[];
  derived_text?: AttachmentDerivedText;
  provider?: string;
  model?: string;
}

export interface AttachmentDerived {
  id: string;
  kind: string;
  content_type?: string;
  size?: number;
  storage_key?: string;
  created?: string;
}

export interface AttachmentDerivedText {
  id?: string;
  kind?: string;
  status: "pending" | "ready" | "failed" | "skipped" | string;
  content_type?: string;
  text?: string;
  error?: string;
  metadata?: Record<string, string>;
  created?: string;
  modified?: string;
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
  modified?: string;
  /** RFC3339 stamp set when status entered completed/validated; empty for
   *  entries completed before the field existed (fall back to modified). */
  completed_at?: string;
  projectId?: string;

  workdir?: string;
  git_branch?: string;
  execution_mode?: string;
  merge_target_branch?: string;
  merge_policy?: string;
  merge_strategy?: string;
  open_pr_before_merge?: boolean;
  /**
   * Merge-request URL for the task's feature branch. Optional — the
   * server does not yet populate it in Phase 5. The in-flight
   * `mr-status` feature will introduce a first-class URL on
   * merge_request entries; until then, `lib/features.extractPrUrl`
   * also falls back to a regex scan of `content`.
   */
  mr_url?: string;
  /**
   * Compat alias for `mr_url`. Server code paths that use the longer
   * name are tolerated by the client.
   */
  merge_request_url?: string;

  feature_id?: string;
  feature_priority?: string;
  feature_depends_on?: string[];
  feature_schedule?: string;
  feature_starts_at?: string;
  feature_expires_at?: string;
  feature_run_once_at?: string;
  feature_timezone?: string;

  schedule?: string;
  schedule_enabled?: boolean;
  next_run?: string;
  run_once_at?: string;
  starts_at?: string;
  expires_at?: string;
  timezone?: string;

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
  generated_by?: string;

  // dependency resolution metadata
  resolved_deps?: string[];
  unresolved_deps?: string[];
  classification?: string;
  blocked_by?: string[];
  blocked_by_reason?: string;
  waiting_on?: string[];
  // Feature-level gating, emitted by applyFeatureGating (service/taskdeps.go).
  // Distinct from the task-level lists above: these name FEATURES, not tasks,
  // and the task tree has no row for a feature — so unless they are rendered
  // the task just sits at "waiting" with nothing explaining why.
  /** Features that must finish before this task's feature may start. */
  waiting_on_features?: string[];
  /** Features that are themselves blocked, blocking this one in turn. */
  blocked_by_features?: string[];
  /** feature_depends_on entries that match no known feature. These gate
   *  NOTHING — a typo silently orders nothing — so they are reported on every
   *  task in the feature regardless of classification. */
  unresolved_feature_deps?: string[];
  in_cycle?: boolean;
  resolved_workdir?: string;

  // Scheduler push-dispatch state. Populated by the task list endpoint
  // (`enrichDispatchDiagnostics` on the server). Lets the UI show *which*
  // runner is holding a task and what state the lease is in, instead of
  // generic "dispatched to a runner" text.
  dispatch_lease?: DispatchLease;
  placement_reasons?: PlacementReason[];
  last_placement_reason?: PlacementReason;

  // Abandonment surface for the resume-abandoned-tasks flow. Derived
  // server-side from task_claims + runners.status + reaper metadata by
  // enrichAbandonmentState — never written by clients. When is_abandoned
  // is true, abandon_reason names the underlying cause and the PWA
  // renders a Resume affordance.
  is_abandoned?: boolean;
  abandon_reason?: AbandonReason;

  // Runtime lifecycle flags for resume. resume_requested is set by
  // POST /resume and consumed by the runner at claim time.
  resume_requested?: boolean;
  resume_requested_at?: string;
}

/** Discriminant on the underlying signal that flagged the task as abandoned.
 *  Keep in sync with service.AbandonReason* constants in Go. */
export type AbandonReason =
  | "no_claim"
  | "claim_expired"
  | "runner_offline"
  | "orphan_reaped";

/** Body for POST /tasks/{project}/{task}/resume and
 *  POST /tasks/{project}/features/{feature}/resume. */
export interface ResumeTaskOptions {
  /** Bypass the is_abandoned gate. Does NOT override the live-claim safety
   *  check — a resume against a task claimed by an online runner is refused
   *  even with force=true. */
  force?: boolean;
}

/** Response from POST /resume (single task). Resumed=false means the call
 *  was a well-formed no-op — reason tells the user why. */
export interface ResumeTaskResult {
  task_id: string;
  resumed: boolean;
  prior_status?: string;
  prior_sessions_count?: number;
  abandon_reason?: AbandonReason | "";
  reason?: string;
}

/** Response from POST /features/{feature}/resume. Batch summary + per-task
 *  outcomes. total_resumed + total_skipped equals results.length. */
export interface ResumeFeatureResult {
  feature_id: string;
  total_resumed: number;
  total_skipped: number;
  results: ResumeTaskResult[];
}

export interface DispatchLease {
  leaseId: string;
  id?: string;
  project_id: string;
  task_id: string;
  assigned_runner_id: string;
  assigned_machine_id: string;
  state: string; // pushed | acked | rejected | expired
  pushed_at: number;
  acked_at?: number;
  rejected_at?: number;
  last_error?: string;
  expires_at: number;
}

export interface PlacementReason {
  task_id?: string;
  runner_id?: string;
  machine_id?: string;
  decision?: string;
  reason?: string;
  created_at?: number;
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

/**
 * Response from DELETE /tasks/{project} — erasing a whole project.
 *
 * No per-entry results list: a wipe routinely spans hundreds of entries and a
 * caller who asked to remove all of them has no use for a row per success.
 * Failures ARE enumerated (capped server-side) — those are the only entries
 * the user still has to deal with.
 */
export interface DeleteProjectResponse {
  project: string;
  deleted: number;
  failed: number;
  errors?: string[];
  index_rows_removed?: number;
  state_rows_removed?: Record<string, number>;
  directory_removed: boolean;
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
  // Runner-scoped pause dial (PUT /runners/{runnerId}/pause|resume).
  // Omitted by the API when false. A paused runner stays "online" but the
  // scheduler will not place any dispatch on it.
  paused?: boolean;
}

export interface FeatureAssignment {
  feature_id: string;
  project_id?: string;
  executor?: string;
  [k: string]: unknown;
}

/**
 * Response from PUT /runners/{runnerId}/pause | /resume.
 *
 * `paused` echoes the dial's new value, so a caller never has to infer it
 * from which endpoint it hit.
 */
export interface RunnerPauseResponse {
  runnerId: string;
  action: "pause" | "resume";
  paused: boolean;
  success: boolean;
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
  feature_id?: string;
  priority?: string;
  title?: string;
  workdir?: string;
  port?: number;
  pid?: number;
  session_ids?: string[];
  status: InstanceStatus;
  executor?: string;
  agent?: string;
  model?: string;
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
  agent?: string;
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

/**
 * How a session surface is addressed. `live` points at a running
 * instance (transcript + events reachable through it); `history`
 * addresses a session with no instance — the transcript comes from the
 * instance-independent history endpoint on the recorded runner.
 */
export type SessionRef =
  | {
      mode: "live";
      runner_id: string;
      instance_id: string;
      session_id?: string;
    }
  | {
      mode: "history";
      runner_id: string;
      session_id: string;
      task_id?: string;
      project_id?: string;
      workdir?: string;
    };

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
  // FOOTGUN: despite living under /tasks/runner/status, `paused` is
  // PROJECT scope, not runner scope. The server computes it as
  // `len(pausedProjects) > 0` (service.RunnerServiceImpl.GetStatus), so it
  // answers "is any project's task dial off?" and says nothing whatsoever
  // about whether a *runner* is paused. Runner-scoped pause is the
  // `paused` field on each RunnerInfo from GET /runners. Reading this one
  // as "the runner is paused" is how a paused runner stays invisible.
  paused: boolean;
  // Go's encoding/json emits nil slices as JSON `null`, not `[]`. Reflect
  // that in the type so callers must defend against null.
  pausedProjects: string[] | null;
  // Likewise project scope: true when ANY project has automations paused.
  automationsPaused: boolean;
  automationPausedProjects: string[] | null;
}

// ─── Scheduler status ────────────────────────────────────────────

// One scheduler pass over one project. `skipped` is the total; the four
// skipped_* counters break it down by cause and sum to it. The breakdown is
// the whole point — "held by a dial someone flipped" and "no runner will
// ever take this" are the two things an operator needs to tell apart, and a
// bare total conflates them. Mirrors types.SchedulerResult in Go.
export interface SchedulerResult {
  project_id: string;
  considered: number;
  dispatched: number;
  skipped: number;
  // Held by the project task dial (non-automation tasks only).
  skipped_tasks_paused?: number;
  // Held by the project automations dial (automation tasks only).
  skipped_automations_paused?: number;
  // No online runner would accept the task. Per-task detail lands in
  // placement_reasons on the task itself.
  skipped_no_candidate?: number;
  // Benign: a previous pass already dispatched it and the lease is live.
  skipped_already_leased?: number;
}

// Scheduler loop state from GET /scheduler/status. Mirrors
// types.SchedulerStatus in Go.
export interface SchedulerStatus {
  started: boolean;
  running: boolean;
  interval: string;
  last_tick_at?: string;
  last_success_at?: string;
  last_error?: string;
  total_ticks: number;
  last_project_results?: Record<string, SchedulerResult>;
  last_expired_leases: number;
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
  /** Multi-project scope; the reserved member "global" admits
   *  project-less entries. Supersedes `project` / `global`. */
  projects?: string[];
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

/**
 * Goal lifecycle statuses and what the PATCH verbs map them to:
 *   active    — reconciling (resume)
 *   blocked   — paused; events ignored until resumed (pause)
 *   completed — criteria met; reactivate by setting active
 *   archived  — hidden from the default list (archive)
 */
export type GoalStatus = "active" | "blocked" | "completed" | "archived";

/** Mirrors Go types.GoalSteering (internal/types/automation.go). */
export interface GoalSteering {
  /** Nil/omitted means enabled — steering is on by default. */
  enabled?: boolean;
  /** 0/omitted defaults to 15 minutes server-side. */
  cooldown_minutes?: number;
}

export interface GoalConfig {
  id: string;
  criteria?: string;
  validation?: string;
  workdir?: string;
  trigger_source?: string;
  /** Scopes the goal to a single task; takes precedence over feature scope. */
  task_id?: string;
  complete_statuses?: string[];
  blocked_statuses?: string[];
  steering?: GoalSteering;
}

/** Mirrors Go types.AutomationAction (internal/types/automation.go). */
export interface AutomationAction {
  type?: string; // "prompt" | "script" | "update" | "http"
  direct_prompt?: string;
  command?: string;
  agent?: string;
  model?: string;
  executor?: string;
  target_workdir?: string;
  execution_mode?: string;
  session_mode?: string;
  complete_on_idle?: boolean;
  timeout?: string;
  requires_capability?: string;
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

/** Mirrors Go types.LinkedTaskSnapshot: fields are `id`, not `task_id`. */
export interface LinkedTaskSnapshot {
  id: string;
  title: string;
  status: string;
}

/**
 * Counts are bucketed by the goal's own complete/blocked status sets, so they
 * match the reconciler's view. `goal_status` aggregates them under the same
 * rules and is always present; `feature_status` is feature semantics and is
 * only sent for a feature-scoped goal.
 */
export interface GoalProgressResponse {
  goal_id: string;
  entry_id: string;
  project?: string;
  feature_id?: string;
  task_id?: string;
  goal_status: string;
  feature_status?: string;
  total: number;
  pending: number;
  in_progress: number;
  completed: number;
  blocked: number;
  tasks: LinkedTaskSnapshot[];
}

/** Reconcile outcomes; "steer" means live sessions were nudged (noop+prompt). */
export type GoalReconcileDecision =
  | "complete"
  | "block"
  | "need_work"
  | "noop"
  | "steer";

export interface GoalReconcileAudit {
  timestamp: string;
  goal_id: string;
  project?: string;
  feature_id?: string;
  triggering_event: string;
  event_id?: string;
  decision: GoalReconcileDecision | string;
  reason: string;
  linked_tasks?: LinkedTaskSnapshot[];
  generated_task_id?: string;
  /** Populated when decision is "steer". */
  sessions_steered?: number;
  sessions_skipped?: number;
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
  task_id?: string;
  complete_statuses?: string[];
  blocked_statuses?: string[];
  steering?: GoalSteering;
  action?: AutomationAction;
}

export interface CreateGoalRequest {
  project: string;
  feature_id?: string;
  title: string;
  content?: string;
  // `config.id` may be empty — the server derives one from the title.
  config: GoalConfig;
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

// FeatureCheckoutOptions mirrors internal/types/types.go FeatureCheckoutOptions.
// Passed to POST /api/v1/tasks/{projectId}/features/{featureId}/checkout.
export interface FeatureCheckoutOptions {
  execution_branch?: string;
  merge_target_branch?: string;
  /** "prompt_only" | "auto_pr" | "auto_merge" */
  merge_policy?: string;
  /** "squash" | "merge" | "rebase" */
  merge_strategy?: string;
  /** "keep" | "delete" */
  remote_branch_policy?: string;
  open_pr_before_merge?: boolean;
  /** "worktree" | "current_branch" */
  execution_mode?: string;
  /** "ai" (default) | "simple" */
  checkout_mode?: string;
}

// CheckoutFeatureResult mirrors internal/types/types.go CheckoutFeatureResult.
export interface CheckoutFeatureResult {
  created: boolean;
  generatedKey: string;
  task?: {
    id?: string;
    path?: string;
    [k: string]: unknown;
  };
}

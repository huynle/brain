// Typed HTTP client for brain-api. Attaches the bearer token, transparently
// refreshes on 401 (once), and exposes thin wrappers for the endpoints the PWA
// uses. Task mutations go through the entries endpoint (PATCH/DELETE
// /api/v1/entries/{path}); trigger/pause/resume use the tasks endpoints.

import { API_V1 } from "./config";
import { useAuth } from "./auth";
import type {
  BrainEntry,
  CheckoutFeatureResult,
  DispatchLease,
  FeatureCheckoutOptions,
  GoalAuditResponse,
  GoalListResponse,
  GoalProgressResponse,
  Health,
  InstanceListResponse,
  ListEntriesResponse,
  OcAgent,
  OcMessage,
  OcPermission,
  OcProvider,
  OcSession,
  OpencodeInstance,
  SpawnInstanceSpec,
  ProjectListResponse,
  ResumeFeatureResult,
  ResumeTaskOptions,
  ResumeTaskResult,
  RunnerListResponse,
  RunnerStatusResponse,
  SearchRequest,
  SearchResponse,
  Task,
  TaskListResponse,
  UpdateGoalRequest,
} from "./types";

export class ApiError extends Error {
  status: number;
  body: string;
  constructor(status: number, message: string, body = "") {
    super(message);
    this.status = status;
    this.body = body;
  }
}

interface FetchOpts {
  method?: string;
  body?: unknown;
  rawBody?: string; // send this string verbatim (no JSON encoding)
  headers?: Record<string, string>; // extra request headers (e.g. Accept)
  query?: Record<string, string | number | boolean | undefined>;
  signal?: AbortSignal;
  raw?: boolean; // return Response instead of parsed JSON
}

function buildUrl(path: string, query?: FetchOpts["query"]): string {
  let url = path.startsWith("/") ? path : `${API_V1}/${path}`;
  if (query) {
    const qs = new URLSearchParams();
    for (const [k, v] of Object.entries(query)) {
      if (v !== undefined) qs.set(k, String(v));
    }
    const s = qs.toString();
    if (s) url += (url.includes("?") ? "&" : "?") + s;
  }
  return url;
}

async function doFetch(path: string, opts: FetchOpts): Promise<Response> {
  const auth = useAuth.getState();
  const headers: Record<string, string> = { ...auth.authHeader(), ...opts.headers };
  let body: BodyInit | undefined;
  if (opts.rawBody !== undefined) {
    // Caller supplies the Content-Type via opts.headers (e.g. full-file edits).
    body = opts.rawBody;
  } else if (opts.body !== undefined) {
    if (!headers["Content-Type"]) headers["Content-Type"] = "application/json";
    body = JSON.stringify(opts.body);
  }
  return fetch(buildUrl(path, opts.query), {
    method: opts.method || "GET",
    headers,
    body,
    signal: opts.signal,
  });
}

export async function api<T>(path: string, opts: FetchOpts = {}): Promise<T> {
  let res = await doFetch(path, opts);

  if (res.status === 401) {
    const refreshed = await useAuth.getState().onUnauthorized();
    if (refreshed) {
      res = await doFetch(path, opts);
    }
  }

  if (!res.ok) {
    const text = await res.text().catch(() => "");
    let message = `${res.status} ${res.statusText}`;
    try {
      const parsed = JSON.parse(text);
      message = parsed.message || parsed.error || parsed.detail || message;
    } catch {
      if (text) message = text.slice(0, 300);
    }
    throw new ApiError(res.status, message, text);
  }

  if (opts.raw) return res as unknown as T;
  if (res.status === 204) return undefined as T;
  const ct = res.headers.get("Content-Type") || "";
  if (ct.includes("application/json")) return (await res.json()) as T;
  return (await res.text()) as unknown as T;
}

/** Encode an entry path for use in /entries/* while preserving slashes. */
export function encodeEntryPath(p: string): string {
  return p
    .split("/")
    .map((seg) => encodeURIComponent(seg))
    .join("/");
}

// ─── Health / projects / tasks ───────────────────────────────────

export const getHealth = () => api<Health>("/api/v1/health");

export const getProjects = () =>
  api<ProjectListResponse>("/api/v1/tasks").then((r) => r.projects || []);

export const getTasks = (projectId: string, signal?: AbortSignal) =>
  api<TaskListResponse>(`/api/v1/tasks/${encodeURIComponent(projectId)}`, {
    signal,
  });

export const getTask = (projectId: string, taskId: string) =>
  api<Task>(
    `/api/v1/tasks/${encodeURIComponent(projectId)}/${encodeURIComponent(taskId)}`,
  );

export const getTaskLogs = (
  projectId: string,
  taskId: string,
  query?: { limit?: number; offset?: number },
) =>
  api<import("./types").LogQueryResponse>(
    `/api/v1/tasks/${encodeURIComponent(projectId)}/${encodeURIComponent(taskId)}/logs`,
    { query },
  );

// ─── Task actions (via entries endpoint) ─────────────────────────

export interface CreateEntryRequest {
  type: string; // "task" | "note" | "automation" | …
  title: string;
  content?: string;
  project?: string;
  status?: string;
  priority?: string;
  feature_id?: string;
  agent?: string;
  tags?: string[];
  [k: string]: unknown;
}

export interface CreateEntryResponse {
  id?: string;
  path: string;
  title?: string;
}

export const createEntry = (body: CreateEntryRequest) =>
  api<CreateEntryResponse>("/api/v1/entries", { method: "POST", body });

export const updateEntry = (path: string, patch: Record<string, unknown>) =>
  api<unknown>(`/api/v1/entries/${encodeEntryPath(path)}`, {
    method: "PATCH",
    body: patch,
  });

export const moveEntry = (path: string, project: string) =>
  api<unknown>(`/api/v1/entries/${encodeEntryPath(path)}/move`, {
    method: "POST",
    body: { project },
  });

// Full-file (frontmatter + body) get/update — mirrors the TUI's $EDITOR flow so
// the PWA can edit the entire entry, not just metadata or the body.
export const getEntryRaw = (path: string) =>
  api<Response>(`/api/v1/entries/${encodeEntryPath(path)}`, {
    headers: { Accept: "text/x-brain-full" },
    raw: true,
  }).then((r) => r.text());

export const updateEntryRaw = (path: string, content: string) =>
  api<unknown>(`/api/v1/entries/${encodeEntryPath(path)}`, {
    method: "PATCH",
    headers: { "Content-Type": "text/x-brain-full" },
    rawBody: content,
  });

export const deleteEntry = (path: string) =>
  api<unknown>(`/api/v1/entries/${encodeEntryPath(path)}`, {
    method: "DELETE",
    query: { confirm: "true" },
  });

export const setTaskStatus = (task: Task, status: string) =>
  updateEntry(task.path, { status });

export interface TriggerResponse {
  success: boolean;
  taskId: string;
  triggered: boolean;
  runId?: string;
  nextRun?: string;
  reason?: string;
  // Machine-readable reason from /run (e.g. "already_leased"). Surface in
  // toasts so callers can offer recovery actions (Force redispatch).
  reasonCode?: string;
  projectId?: string;
  // Lease metadata populated when reasonCode === "already_leased". The PWA
  // surfaces this on the toast and on the task detail pane so users can see
  // which runner is holding the task without digging through logs.
  lease?: {
    runnerId?: string;
    leaseId?: string;
    state?: string;
    expiresAt?: string;
  };
}

export const triggerTask = (projectId: string, taskId: string) =>
  api<TriggerResponse>(
    `/api/v1/tasks/${encodeURIComponent(projectId)}/${encodeURIComponent(taskId)}/trigger`,
    { method: "POST" },
  );

// /run is the user-explicit "execute this task now" endpoint. Unlike /trigger
// (which only flips status to pending and waits for the runner to poll),
// /run picks an eligible runner and pushes a dispatch command immediately.
// This matches the TUI's "x" key behaviour from the runner-controller path.
export interface RunTaskResponse {
  dispatched: boolean;
  taskId: string;
  projectId: string;
  runnerId?: string;
  leaseId?: string;
  leaseState?: string;
  expiresAt?: string;
  reason?: string;
  detail?: string;
}

export const runTask = (projectId: string, taskId: string, force = false) =>
  api<RunTaskResponse>(
    `/api/v1/tasks/${encodeURIComponent(projectId)}/${encodeURIComponent(taskId)}/run`,
    {
      method: "POST",
      body: { force },
    },
  );

// /features/{featureId}/run is the user-explicit "execute this whole feature
// now" endpoint. The server dispatches every ready task in the feature up to
// current runner capacity and queues the rest for a feature-scoped cascade
// that auto-dispatches as in-flight tasks complete — even when the project
// is paused. Mirrors runTask but operates on a whole feature in one click.
export interface RunFeatureResponse {
  dispatched: boolean;
  projectId: string;
  featureId: string;
  results?: RunTaskResponse[];
  queued?: string[];
  dispatchedCount: number;
  skippedCount: number;
  reason?: string;
  detail?: string;
  cascadeActive?: boolean;
}

export const runFeature = (projectId: string, featureId: string, force = false) =>
  api<RunFeatureResponse>(
    `/api/v1/tasks/${encodeURIComponent(projectId)}/features/${encodeURIComponent(featureId)}/run`,
    {
      method: "POST",
      body: { force },
    },
  );

// ─── Feature assignment (Phase 8) ─────────────────────────────────
//
// Backend handlers live at internal/api/tasks.go:
//   PUT  /api/v1/tasks/{projectId}/features/{featureId}/assignment
//        body: { runner_id: string, intent: "assign"|"reassign", force?: bool }
//   POST /api/v1/tasks/{projectId}/features/{featureId}/assignment/clear
//        body: { intent: "clear" }
//
// Assignment mutations emit `runners_update` SSE events on the runners
// lifecycle topic (see task ivwx9a8t), so successful writes reconcile
// automatically. Callers should still optimistically update local state
// for snappy UI and roll back on error.
//
// Intent semantics (from internal/api/runners_test.go):
//   - First-time assign     → intent: "assign"   → 200 OK
//   - Assign when already assigned to same runner → intent: "assign" → 200
//     (idempotent)
//   - Assign when already assigned to a *different* runner without
//     "reassign" intent → 409 Conflict
//   - To reassign, pass intent: "reassign"
//
// The client-side wrappers below default to `intent: "assign"` for the
// first call and let callers pass `reassign` explicitly. We do NOT try
// to guess the intent from local optimistic state — a 409 from the
// server is the signal to escalate.

export interface FeatureAssignmentResponse {
  project_id: string;
  feature_id: string;
  runner_id?: string;
  previous_runner?: string;
  source: string;
  status: string;
  assigned_at?: string;
  updated_at?: string;
}

export interface AssignFeatureOptions {
  /** "assign" (default) for first-time or same-runner idempotent writes,
   *  "reassign" when moving a feature from one runner to another. */
  intent?: "assign" | "reassign";
  /** Force reassignment past server-side conflicts. Rarely needed. */
  force?: boolean;
}

export const assignFeatureToRunner = (
  projectId: string,
  featureId: string,
  runnerId: string,
  options: AssignFeatureOptions = {},
) =>
  api<FeatureAssignmentResponse>(
    `/api/v1/tasks/${encodeURIComponent(projectId)}/features/${encodeURIComponent(featureId)}/assignment`,
    {
      method: "PUT",
      body: {
        runner_id: runnerId,
        intent: options.intent ?? "assign",
        ...(options.force ? { force: true } : {}),
      },
    },
  );

export const clearFeatureAssignment = (
  projectId: string,
  featureId: string,
) =>
  api<FeatureAssignmentResponse>(
    `/api/v1/tasks/${encodeURIComponent(projectId)}/features/${encodeURIComponent(featureId)}/assignment/clear`,
    {
      method: "POST",
      body: { intent: "clear" },
    },
  );

// Format a RunFeatureResponse into a toast message. Mirrors
// summarizeTriggerResults' shape so callers can drop this straight into the
// toast notification system.
export function summarizeRunFeatureResult(
  r: RunFeatureResponse,
): { message: string; kind: "info" | "success" } {
  if (r.dispatched && r.dispatchedCount > 0 && (r.queued?.length ?? 0) === 0) {
    return {
      message:
        r.dispatchedCount === 1
          ? "Dispatched 1 task"
          : `Dispatched ${r.dispatchedCount} tasks`,
      kind: "success",
    };
  }
  if (r.dispatched && (r.queued?.length ?? 0) > 0) {
    return {
      message: `Dispatched ${r.dispatchedCount}, queued ${r.queued?.length ?? 0} (auto-dispatch as slots free)`,
      kind: "success",
    };
  }
  // Nothing dispatched — surface the reason.
  const reasonText = humanizeRunFeatureReason(r);
  return { message: `Not triggered: ${reasonText}`, kind: "info" };
}

function humanizeRunFeatureReason(r: RunFeatureResponse): string {
  switch (r.reason) {
    case "feature_not_found":
      return "feature not found";
    case "no_ready_tasks":
      return "no ready tasks in this feature (check dependencies)";
    case "feature_in_progress":
      return "every ready task is already in flight";
    case "scheduler_not_configured":
      return "scheduler not configured on server";
    case "":
    case undefined:
      return "nothing to dispatch";
    default:
      return r.detail ? `${r.reason}: ${r.detail}` : r.reason;
  }
}

// Create a feature checkout task via POST /features/{featureId}/checkout.
// The server creates a task that runs the feature-checkout automation (AI or
// simple mode based on opts.checkout_mode). Empty opts uses server defaults.
export const checkoutFeature = (
  projectId: string,
  featureId: string,
  opts?: FeatureCheckoutOptions,
) =>
  api<CheckoutFeatureResult>(
    `/api/v1/tasks/${encodeURIComponent(projectId)}/features/${encodeURIComponent(featureId)}/checkout`,
    {
      method: "POST",
      body: opts ?? {},
    },
  );

// Resume an abandoned task via POST /tasks/{project}/{task}/resume. The server
// flips the task back to pending, stamps resume_requested, and the runner will
// re-spawn it with IsResume=true on its next poll. Returns Resumed=false with
// an explanatory Reason when the call is a well-formed no-op (task already
// resumed, terminal, or not abandoned without force).
export const resumeTask = (
  projectId: string,
  taskId: string,
  opts?: ResumeTaskOptions,
) =>
  api<ResumeTaskResult>(
    `/api/v1/tasks/${encodeURIComponent(projectId)}/${encodeURIComponent(taskId)}/resume`,
    {
      method: "POST",
      body: opts ?? {},
    },
  );

// Batch-resume every abandoned task in a feature. Per-task outcomes are in
// result.results — non-abandoned tasks appear as skipped entries with a
// reason string, so callers can render partial-failure state without a
// second round-trip.
export const resumeFeature = (
  projectId: string,
  featureId: string,
  opts?: ResumeTaskOptions,
) =>
  api<ResumeFeatureResult>(
    `/api/v1/tasks/${encodeURIComponent(projectId)}/features/${encodeURIComponent(featureId)}/resume`,
    {
      method: "POST",
      body: opts ?? {},
    },
  );

// One-line toast summary of a batch resume. Mirrors summarizeTriggerResults
// style so calling code can reuse the same UX pattern.
export const summarizeResumeResults = (r: ResumeFeatureResult): string => {
  if (r.total_resumed === 0 && r.total_skipped === 0) {
    return "No tasks in feature";
  }
  if (r.total_resumed === 0) {
    return `No tasks resumed (${r.total_skipped} skipped)`;
  }
  if (r.total_skipped === 0) {
    const s = r.total_resumed === 1 ? "" : "s";
    return `Resumed ${r.total_resumed} task${s}`;
  }
  const s = r.total_resumed === 1 ? "" : "s";
  return `Resumed ${r.total_resumed} task${s} · ${r.total_skipped} skipped`;
};

// Run the Blocked Task Inspector once for a feature without leaving a
// recurring monitor behind. Creates a one-shot task via POST /entries.
// The prompt mirrors the server-side blocked-inspector template for feature
// scope (see internal/service/monitor_prompts.go buildBlockedInspectorPrompt).
export const runBlockedInspectorNow = (projectId: string, featureId: string) => {
  const directPrompt = `You are the **Blocked Task Inspector** — an automated agent that checks for blocked tasks in feature ${featureId} of project ${projectId} and attempts to unblock them.

## Scope

feature ${featureId} in project ${projectId}

## Workflow

Call brain_tasks({ project: "${projectId}", feature_id: "${featureId}", status: "blocked" }) to find all blocked tasks for this feature.

For each blocked task found, follow these steps in order:

### Step 1: Read the Task
Call brain_task_get({ taskId: "<id>" }) to get the full task content, status, and any appended notes.

### Step 2: Check Session History
Use session tools to find error context from the agent that was working on this task.

### Step 3: Classify the Block

| Classification | Indicators |
|---|---|
| **Worktree setup failure** | Task never started, no session history, worktree errors |
| **Idle detection timeout** | Session shows agent went idle |
| **Process crash** | Session ends abruptly, exit codes in runner logs |
| **Agent self-block** | Task has a blocked note from brain_update |
| **Dependency block** | Task depends on blocked/incomplete tasks |

### Step 4: Attempt Resolution

**Worktree setup failure:** Reset to pending via brain_update({ path: "<task-path>", status: "pending" })
**Idle timeout (process dead):** Reset to pending, append context from session history
**Process crash:** Reset to pending, append crash context
**Agent self-block:** Do NOT auto-reset. Log analysis only.
**Dependency block:** Log analysis, check if upstream task can be unblocked first.

### Step 5: Log Actions
Append a summary of what you found and did to your own task via brain_update({ path: "<your-task-path>", append: "..." }).

## Safety Rules

1. **NEVER change the status of draft tasks**
2. **NEVER inspect or modify your own task's status**
3. **NEVER force-unblock agent self-blocks** — respect intentional blocks
4. **Limit actions per run to 5** — process at most 5 blocked tasks
5. **Be conservative** — when in doubt, log analysis but do NOT take action`;

  return createEntry({
    type: "task",
    title: `Blocked Task Inspector: ${featureId} (once)`,
    content: `## One-shot Blocked Task Inspector\n\nManually triggered from the PWA feature actions modal.\nScope: feature ${featureId} in project ${projectId}\n\nRuns once via direct_prompt then completes on idle.`,
    project: projectId,
    feature_id: featureId,
    status: "pending",
    execution_mode: "current_branch",
    complete_on_idle: true,
    direct_prompt: directPrompt,
    tags: [
      "inspector",
      "monitoring",
      "manual-oneshot",
      `monitor:blocked-inspector:feature:${featureId}:${projectId}:oneshot`,
    ],
  });
};

// Fetch the current dispatch lease for a task. Returns 404 when none is
// active — callers should treat that as "not currently dispatched" rather
// than an error. Used by the task detail pane to show which runner is
// holding a task without waiting for the next task-list refresh.
export async function getDispatchLease(
  projectId: string,
  taskId: string,
): Promise<DispatchLease | null> {
  try {
    return await api<DispatchLease>(
      `/api/v1/tasks/${encodeURIComponent(projectId)}/${encodeURIComponent(taskId)}/dispatch-lease`,
    );
  } catch (err) {
    if (err instanceof ApiError && err.status === 404) return null;
    throw err;
  }
}

// Force-release a dispatch lease owned by a specific runner. Used as the
// recovery action when a runner went silent without clearing its lease.
// Pairs with runTask(force=true) on the server, but explicit release is
// useful when the user just wants to clear the lease without redispatching.
export const releaseDispatchLease = (runnerId: string, projectId: string, taskId: string) =>
  api<{ success: boolean; taskId?: string; runnerId?: string }>(
    `/api/v1/tasks/runners/${encodeURIComponent(runnerId)}/dispatch/release`,
    {
      method: "POST",
      body: { projectId, taskId },
    },
  );

// runOrTriggerTask prefers /run (push dispatch to a runner) and silently
// falls back to /trigger when the server hasn't been upgraded — letting the
// PWA work against older brain-api builds. The fallback path returns a
// shape compatible with TriggerResponse so callers can keep using
// summarizeTriggerResults.
export async function runOrTriggerTask(
  projectId: string,
  taskId: string,
  force = false,
): Promise<TriggerResponse> {
  try {
    const r = await runTask(projectId, taskId, force);
    return runResponseToTrigger(r, projectId);
  } catch (err) {
    if (err instanceof ApiError && err.status === 501) {
      return triggerTask(projectId, taskId);
    }
    throw err;
  }
}

function runResponseToTrigger(r: RunTaskResponse, projectId: string): TriggerResponse {
  if (r.dispatched) {
    const detail = r.runnerId ? `dispatched to ${r.runnerId}` : "dispatched";
    return {
      success: true,
      taskId: r.taskId,
      projectId,
      triggered: true,
      reason: detail,
      reasonCode: "dispatched",
    };
  }
  // Map machine-readable reasons to user-facing copy. The shape matches
  // TriggerResponse so summarizeTriggerResults handles it transparently.
  const lease =
    r.reason === "already_leased" && (r.runnerId || r.leaseState)
      ? {
          runnerId: r.runnerId,
          leaseId: r.leaseId,
          state: r.leaseState,
          expiresAt: r.expiresAt,
        }
      : undefined;
  return {
    success: true,
    taskId: r.taskId,
    projectId,
    triggered: false,
    reason: humanizeRunReason(r),
    reasonCode: r.reason,
    lease,
  };
}

// Format an absolute ISO timestamp as a coarse "in 3m" / "5s ago" string so
// toast messages and detail panes can show lease freshness without dragging
// in date-fns. Keeps the output deliberately short (chip-sized).
function relativeFromNow(iso?: string): string | undefined {
  if (!iso) return undefined;
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return undefined;
  const deltaSec = Math.round((t - Date.now()) / 1000);
  const abs = Math.abs(deltaSec);
  const fmt =
    abs < 60 ? `${abs}s` :
    abs < 3600 ? `${Math.round(abs / 60)}m` :
    abs < 86400 ? `${Math.round(abs / 3600)}h` :
    `${Math.round(abs / 86400)}d`;
  return deltaSec >= 0 ? `in ${fmt}` : `${fmt} ago`;
}

function humanizeRunReason(r: RunTaskResponse): string {
  const reason = r.reason;
  const detail = r.detail;
  switch (reason) {
    case "no_online_runner":
      return "no runners are online";
    case "no_eligible_runner":
      return detail ? `no eligible runner (${detail})` : "no eligible runner for this task";
    case "all_runners_at_capacity":
      return "all runners are at capacity";
    case "task_not_ready":
      return "task is not ready (check dependencies)";
    case "already_leased": {
      // Surface the actual lease owner + state so users can tell at a
      // glance whether to wait, abort, or force-redispatch.
      if (!r.runnerId) return "task already dispatched to a runner";
      const parts: string[] = [r.runnerId];
      if (r.leaseState) parts.push(r.leaseState);
      const rel = relativeFromNow(r.expiresAt);
      if (rel) parts.push(`expires ${rel}`);
      return `already dispatched to ${parts[0]} (${parts.slice(1).join(", ")})`;
    }
    case "scheduler_not_configured":
      return "scheduler not configured on server";
    case "":
    case undefined:
      return "no eligible tasks to trigger";
    default:
      return detail ? `${reason}: ${detail}` : reason;
  }
}

// Summarize one-or-many TriggerResponse(s) into a user-friendly toast string
// and severity. The backend distinguishes between "actually triggered" and
// "no-op with reason" (e.g. task already pending), and we surface both.
export function summarizeTriggerResults(
  results: TriggerResponse[],
): { message: string; kind: "info" | "success" } {
  const triggered = results.filter((r) => r.triggered);
  const skipped = results.filter((r) => !r.triggered);

  if (triggered.length && !skipped.length) {
    return {
      message: triggered.length === 1 ? "Triggered" : `Triggered ${triggered.length}`,
      kind: "success",
    };
  }
  if (triggered.length && skipped.length) {
    return {
      message: `Triggered ${triggered.length}, skipped ${skipped.length}`,
      kind: "success",
    };
  }
  // All skipped — show the reason from the first one (or a generic message).
  const reason = skipped[0]?.reason || "no eligible tasks to trigger";
  return {
    message: skipped.length === 1 ? `Not triggered: ${reason}` : `Skipped ${skipped.length}: ${reason}`,
    kind: "info",
  };
}


// ─── Built-in Assistant ──────────────────────────────────────────

export interface AssistantStatusResponse {
  available: boolean;
  mode: "agentic" | "direct_llm" | "manual" | string;
  provider?: string;
  model?: string;
  capabilities: string[];
  reason?: string;
}

export interface AssistantAction {
  type: string;
  explicit: boolean;
  payload: Record<string, unknown>;
}

export interface AssistantChatResponse {
  reply: string;
  executed_actions: { type: string; status: string; result?: unknown; error?: string }[];
  proposed_actions: AssistantAction[];
}

// AssistantToolCall / AssistantToolResult are streamed for each tool the
// server-side agent loop invokes. The PWA renders these as collapsible chips
// above the assistant's final natural-language reply.
export interface AssistantToolCall {
  id: string;
  name: string;
  // Arguments are JSON-encoded (as sent to the model) so the UI can decode
  // and pretty-print them when a chip is expanded.
  args?: string | Record<string, unknown>;
  tier: "read" | "write" | "destructive" | string;
}

export interface AssistantToolResult {
  id: string;
  name: string;
  status: "completed" | "failed" | "proposed" | string;
  result?: unknown;
  error?: string;
  proposed?: boolean;
}

export interface AssistantGoalDraft {
  project?: string;
  feature_id?: string;
  title?: string;
  criteria?: string;
  validation?: string;
  workdir?: string;
  trigger_source?: string;
  agent?: string;
  model?: string;
  complete_statuses?: string[];
  blocked_statuses?: string[];
}

// AssistantHistoryMessage is the compact shape the PWA replays to the server
// so the agent loop has memory across HTTP turns. Tool result payloads are
// intentionally omitted — role="tool" entries carry only the tool_call_id +
// name + status. The server substitutes a placeholder body when replaying.
export interface AssistantHistoryMessage {
  role: "user" | "assistant" | "tool";
  content?: string;
  tool_calls?: { id: string; name: string; arguments?: string }[];
  tool_call_id?: string;
  name?: string;
  status?: string;
}

export const assistantStatus = () =>
  api<AssistantStatusResponse>("/api/v1/assistant/status");

export const assistantChat = (body: {
  project?: string;
  message: string;
  model?: string;
  attachments?: string[];
  context?: Record<string, string>;
  history?: AssistantHistoryMessage[];
}) => api<AssistantChatResponse>("/api/v1/assistant/chat", { method: "POST", body });

export interface AssistantStreamEvent {
  type: "delta" | "tool_call" | "tool_result" | "done" | "error" | string;
  delta?: string;
  reply?: string;
  executed_actions?: AssistantChatResponse["executed_actions"];
  proposed_actions?: AssistantChatResponse["proposed_actions"];
  tool_call?: AssistantToolCall;
  tool_result?: AssistantToolResult;
  error?: string;
}

export async function assistantChatStream(
  body: {
    project?: string;
    message: string;
    model?: string;
    attachments?: string[];
    context?: Record<string, string>;
    history?: AssistantHistoryMessage[];
  },
  onEvent: (event: AssistantStreamEvent) => void,
  signal?: AbortSignal,
): Promise<void> {
  const res = await api<Response>("/api/v1/assistant/chat/stream", {
    method: "POST",
    body,
    raw: true,
    headers: { Accept: "application/x-ndjson" },
    signal,
  });
  if (!res.body) throw new ApiError(0, "Streaming response body is unavailable");
  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let pending = "";
  for (;;) {
    const { value, done } = await reader.read();
    if (done) break;
    pending += decoder.decode(value, { stream: true });
    const lines = pending.split("\n");
    pending = lines.pop() || "";
    for (const line of lines) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      onEvent(JSON.parse(trimmed) as AssistantStreamEvent);
    }
  }
  pending += decoder.decode();
  const trimmed = pending.trim();
  if (trimmed) onEvent(JSON.parse(trimmed) as AssistantStreamEvent);
}

export const assistantGoalDraft = (body: {
  project?: string;
  message: string;
  current?: AssistantGoalDraft;
  attachments?: string[];
  context?: Record<string, string>;
}) =>
  api<{ reply: string; draft: AssistantGoalDraft }>("/api/v1/assistant/goal-draft", {
    method: "POST",
    body,
  });

export async function uploadAttachment(projectId: string, file: Blob, filename: string, metadata?: Record<string, unknown>) {
  const form = new FormData();
  form.set("project_id", projectId);
  form.set("file", file, filename);
  if (metadata) form.set("metadata", JSON.stringify(metadata));
  const token = useAuth.getState().token;
  const headers: Record<string, string> = {};
  if (token) headers.Authorization = `Bearer ${token}`;
  const res = await fetch(buildUrl("/api/v1/attachments"), {
    method: "POST",
    headers,
    body: form,
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new ApiError(res.status, text || res.statusText, text);
  }
  return (await res.json()) as { attachment: { id: string; filename: string; content_type: string } };
}

// ─── Runner control ──────────────────────────────────────────────

export const getRunners = () =>
  api<RunnerListResponse>("/api/v1/runners").then((r) => r.runners || []);

// getServerRequests fetches the global server-request log (all HTTP traffic in
// and out of the Brain server, annotated with the authenticated actor).
export const getServerRequests = (since = 0, limit = 500) =>
  api<{ requests: import("./types").ServerRequest[]; total: number }>(
    "/api/v1/server/requests/recent",
    { query: { since, limit } },
  ).then((r) => r.requests || []);

export const getRunnerStatus = () =>
  api<RunnerStatusResponse>("/api/v1/tasks/runner/status");

export const pauseProject = (projectId: string) =>
  api(`/api/v1/tasks/runner/pause/${encodeURIComponent(projectId)}`, {
    method: "POST",
  });
export const resumeProject = (projectId: string) =>
  api(`/api/v1/tasks/runner/resume/${encodeURIComponent(projectId)}`, {
    method: "POST",
  });
export const pauseAll = () =>
  api("/api/v1/tasks/runner/pause", { method: "POST" });
export const resumeAll = () =>
  api("/api/v1/tasks/runner/resume", { method: "POST" });
export const pauseAutomations = (projectId?: string) =>
  api(
    projectId
      ? `/api/v1/tasks/runner/automations/pause/${encodeURIComponent(projectId)}`
      : "/api/v1/tasks/runner/automations/pause",
    { method: "POST" },
  );
export const resumeAutomations = (projectId?: string) =>
  api(
    projectId
      ? `/api/v1/tasks/runner/automations/resume/${encodeURIComponent(projectId)}`
      : "/api/v1/tasks/runner/automations/resume",
    { method: "POST" },
  );

export const shutdownRunner = (runnerId: string, reason = "manual") =>
  api(`/api/v1/runners/${encodeURIComponent(runnerId)}/shutdown`, {
    method: "PUT",
    body: { reason },
  });

// ─── OpenCode instances (remote control) ─────────────────────────

export const listInstances = () =>
  api<InstanceListResponse>("/api/v1/instances").then((r) => r.instances || []);

export const listRunnerInstances = (runnerId: string) =>
  api<InstanceListResponse>(
    `/api/v1/runners/${encodeURIComponent(runnerId)}/instances`,
  ).then((r) => r.instances || []);

// ─── Remote control plane ────────────────────────────────────────

const controlBase = (runnerId: string, instanceId: string) =>
  `/api/v1/control/runners/${encodeURIComponent(runnerId)}/instances/${encodeURIComponent(instanceId)}`;

export const controlListSessions = (runnerId: string, instanceId: string) =>
  api<OcSession[]>(`${controlBase(runnerId, instanceId)}/sessions`);

export const controlCreateSession = (runnerId: string, instanceId: string, title?: string) =>
  api<OcSession>(`${controlBase(runnerId, instanceId)}/sessions`, {
    method: "POST",
    body: title ? { title } : {},
  });

export const controlListMessages = (
  runnerId: string,
  instanceId: string,
  sessionId: string,
  limit = 50,
) =>
  api<OcMessage[]>(
    `${controlBase(runnerId, instanceId)}/sessions/${encodeURIComponent(sessionId)}/messages`,
    { query: { limit: String(limit) } },
  );

export interface ControlFilePart {
  mime: string;
  url: string; // data: URL (base64) or file:// path on the runner
  filename?: string;
}

export const controlPrompt = (
  runnerId: string,
  instanceId: string,
  sessionId: string,
  body: {
    text: string;
    agent?: string;
    model?: { providerID: string; modelID: string };
    files?: ControlFilePart[];
  },
) =>
  api<unknown>(
    `${controlBase(runnerId, instanceId)}/sessions/${encodeURIComponent(sessionId)}/prompt`,
    { method: "POST", body },
  );

export const controlAbort = (runnerId: string, instanceId: string, sessionId: string) =>
  api<unknown>(
    `${controlBase(runnerId, instanceId)}/sessions/${encodeURIComponent(sessionId)}/abort`,
    { method: "POST" },
  );

export const controlRespondPermission = (
  runnerId: string,
  instanceId: string,
  sessionId: string,
  permissionId: string,
  response: "once" | "always" | "reject",
) =>
  api<unknown>(
    `${controlBase(runnerId, instanceId)}/sessions/${encodeURIComponent(sessionId)}/permissions/${encodeURIComponent(permissionId)}`,
    { method: "POST", body: { response } },
  );

export const controlPendingPermissions = (runnerId: string, instanceId: string) =>
  api<{ permissions: OcPermission[]; total: number }>(
    `${controlBase(runnerId, instanceId)}/permissions`,
  );

export const controlAgents = (runnerId: string, instanceId: string) =>
  api<OcAgent[]>(`${controlBase(runnerId, instanceId)}/agents`);

export const controlProviders = (runnerId: string, instanceId: string) =>
  api<{ providers?: OcProvider[] } | OcProvider[]>(
    `${controlBase(runnerId, instanceId)}/providers`,
  );

// controlSessionHistory fetches the transcript of a session by ID without a
// live instance — the runner serves it from a running instance if one hosts
// the session, otherwise from OpenCode's on-disk storage. Used to review a
// completed task's session in the Control tab.
export const controlSessionHistory = (runnerId: string, sessionId: string) =>
  api<OcMessage[]>(
    `/api/v1/control/runners/${encodeURIComponent(runnerId)}/sessions/${encodeURIComponent(sessionId)}/history`,
  );

export const controlSpawnInstance = (runnerId: string, spec: SpawnInstanceSpec) =>
  api<{ success: boolean; instance: OpencodeInstance }>(
    `/api/v1/control/runners/${encodeURIComponent(runnerId)}/instances`,
    { method: "POST", body: spec },
  );

export const controlKillInstance = (runnerId: string, instanceId: string) =>
  api<{ success: boolean }>(controlBase(runnerId, instanceId), { method: "DELETE" });

export const controlAbortTask = (runnerId: string, taskId: string) =>
  api<{ success: boolean }>(
    `/api/v1/control/runners/${encodeURIComponent(runnerId)}/tasks/${encodeURIComponent(taskId)}/abort`,
    { method: "POST" },
  );

/** EventSource URL for an instance's live event stream (?token= auth). */
export function controlEventsUrl(runnerId: string, instanceId: string): string {
  const base = `${controlBase(runnerId, instanceId)}/events`;
  const token = useAuth.getState().token;
  return token ? `${base}?token=${encodeURIComponent(token)}` : base;
}

// ─── Brain entries / search ──────────────────────────────────────

export const listEntries = (query?: {
  project?: string;
  type?: string;
  status?: string;
  limit?: number;
  global?: string;
  sortBy?: "created" | "modified" | "priority" | "completed" | "title";
  sortOrder?: "asc" | "desc";
}) => api<ListEntriesResponse>("/api/v1/entries", { query });

// ─── Automations (mirrors the TUI Automations tab) ───────────────
// Fetches all automation entries (project-scoped + global/built-in), the
// scheduled/generated task entries, and automation_run records so the PWA can
// render the same unified list the TUI does.
export async function listAutomationData(project?: string): Promise<{
  automations: BrainEntry[];
  tasks: BrainEntry[];
  runs: BrainEntry[];
}> {
  const [scoped, global, tasks, runs] = await Promise.all([
    listEntries({ type: "automation", limit: 500, ...(project ? { project } : {}) }).then(
      (r) => r.entries || [],
    ),
    // Built-in automations are global; always include them.
    listEntries({ type: "automation", global: "true", limit: 500 }).then((r) => r.entries || []),
    listEntries({ type: "task", limit: 1000, ...(project ? { project } : {}) }).then(
      (r) => r.entries || [],
    ),
    listEntries({ type: "automation_run", limit: 500, ...(project ? { project } : {}) })
      .then((r) => r.entries || [])
      .catch(() => [] as BrainEntry[]),
  ]);
  // Merge scoped + global automation entries, de-duped by id.
  const byId = new Map<string, BrainEntry>();
  for (const e of [...global, ...scoped]) byId.set(e.id, e);
  return { automations: [...byId.values()], tasks, runs };
}

// executeAutomation triggers a manual run of an automation entry SERVER-side
// (POST /automations/run), which reuses the exact task-generation code the
// cron/event dispatchers run. The previous client-side reimplementation
// drifted from the server (e.g. rejecting actions the scheduler happily
// runs with "automation action has no prompt or command").
export async function executeAutomation(
  path: string,
  _fallbackProject: string,
): Promise<{ task_id?: string; skipped?: boolean; message?: string }> {
  return api("/api/v1/automations/run", { method: "POST", body: { path } });
}

export const getEntry = (path: string) =>
  api<BrainEntry>(`/api/v1/entries/${encodeEntryPath(path)}`, {
    query: { include: "attachments" },
  });

export const search = (req: SearchRequest) =>
  api<SearchResponse>("/api/v1/search", { method: "POST", body: req });

export const embedBackfill = (body: {
  project?: string;
  force?: boolean;
  dry_run?: boolean;
}) => api<unknown>("/api/v1/embeddings/backfill", { method: "POST", body });

// ─── Goals ───────────────────────────────────────────────────────

export const listGoals = () =>
  api<GoalListResponse>("/api/v1/goals").then((r) => r.goals || []);

export const createGoal = (body: import("./types").CreateGoalRequest) =>
  api<import("./types").GoalSummary>("/api/v1/goals", {
    method: "POST",
    body,
  });

export const goalProgress = (goalId: string) =>
  api<GoalProgressResponse>(
    `/api/v1/goals/${encodeURIComponent(goalId)}/progress`,
  );

export const goalAudit = (goalId: string, limit = 10) =>
  api<GoalAuditResponse>(`/api/v1/goals/${encodeURIComponent(goalId)}/audit`, {
    query: { limit },
  });

export const updateGoal = (goalId: string, patch: UpdateGoalRequest) =>
  api(`/api/v1/goals/${encodeURIComponent(goalId)}`, {
    method: "PATCH",
    body: patch,
  });

export const runGoal = (goalId: string) =>
  api(`/api/v1/goals/${encodeURIComponent(goalId)}/run`, { method: "POST" });

// ─── Server configuration (~/.config/brain/config.yaml) ────────────

/**
 * ConfigField describes one editable field returned by
 * `GET /api/v1/config/schema`. See internal/api/config_schema.go for
 * the source of truth.
 */
export interface ConfigField {
  path: string;
  kind:
    | "string"
    | "int"
    | "bool"
    | "duration_ms"
    | "string_array"
    | "enum"
    | "secret"
    | "path"
    | "url";
  section: string;
  label: string;
  help?: string;
  enum?: string[];
  requires_restart?: boolean;
  secret?: boolean;
  required?: boolean;
}

/**
 * The full config document. Deliberately typed as a nested object of
 * unknowns because the file's structure is deep and changes over
 * time; the schema endpoint tells the UI what to render, and the
 * server validates on PUT.
 */
export type ServerConfig = Record<string, unknown>;

export interface ConfigGetResponse {
  config: ServerConfig;
  path: string;
}

export interface ConfigUpdateResponse {
  hot_reloaded: string[];
  requires_restart: string[];
  backup_path: string;
}

export const getServerConfig = () =>
  api<ConfigGetResponse>("/api/v1/config");

export const getConfigSchema = () =>
  api<{ fields: ConfigField[] }>("/api/v1/config/schema");

export const updateServerConfig = (cfg: ServerConfig) =>
  api<ConfigUpdateResponse>("/api/v1/config", {
    method: "PUT",
    body: { config: cfg },
  });

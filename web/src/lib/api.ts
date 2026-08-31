// Typed HTTP client for brain-api. Attaches the bearer token, transparently
// refreshes on 401 (once), and exposes thin wrappers for the endpoints the PWA
// uses. Task mutations go through the entries endpoint (PATCH/DELETE
// /api/v1/entries/{path}); trigger/pause/resume use the tasks endpoints.

import { API_V1 } from "./config";
import { useAuth } from "./auth";
import { parseSSEFrame } from "./sse";
import type {
  BrainEntry,
  CheckoutFeatureResult,
  CreateGoalRequest,
  DispatchLease,
  FeatureCheckoutOptions,
  GoalAuditResponse,
  GoalListResponse,
  GoalProgressResponse,
  GoalReconcileAudit,
  GoalSummary,
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
  RunnerPauseResponse,
  RunnerStatusResponse,
  SchedulerStatus,
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
  const headers: Record<string, string> = {
    ...auth.authHeader(),
    ...opts.headers,
  };
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

// Delete a single entry. `force` bypasses the server's live-claim guard,
// which refuses (409) to delete a task an online runner is executing.
export const deleteEntry = (path: string, force = false) =>
  api<unknown>(`/api/v1/entries/${encodeEntryPath(path)}`, {
    method: "DELETE",
    query: force ? { confirm: "true", force: "true" } : { confirm: "true" },
  });

export const setTaskStatus = (task: Task, status: string) =>
  updateEntry(task.path, { status });

// ─── Bulk operations ───────────────────────────────────────────────

export interface BulkFilter {
  feature_id?: string;
  project?: string;
  type?: string;
  status?: string;
  priority?: string;
  tags?: string[];
}

export interface BulkResultRow {
  path: string;
  id: string;
  title: string;
  /** "ok" | "error" */
  status: string;
  error?: string;
}

export interface BulkUpdateResponse {
  updated: number;
  failed: number;
  total: number;
  dry_run: boolean;
  results: BulkResultRow[];
  /** True when the filter matched more entries than the 100 cap allows. */
  truncated?: boolean;
  /** How many entries matched before the cap. */
  matched_total?: number;
}

export interface BulkDeleteResponse {
  deleted: number;
  failed: number;
  total: number;
  dry_run: boolean;
  results: BulkResultRow[];
  truncated?: boolean;
  matched_total?: number;
}

/**
 * Apply one set of updates to every entry matching a filter.
 *
 * Always run with `dry_run: true` first for anything user-initiated: the
 * response reports `truncated` when the filter matched more than the
 * server's 100-entry cap, which is the difference between "updated the
 * feature" and "updated the first 100 tasks of the feature and said
 * nothing".
 */
export const bulkUpdate = (
  filter: BulkFilter,
  updates: Record<string, unknown>,
  opts: { dryRun?: boolean; limit?: number; force?: boolean } = {},
) =>
  api<BulkUpdateResponse>("/api/v1/entries/bulk-update", {
    method: "POST",
    body: {
      filter,
      updates,
      ...(opts.dryRun ? { dry_run: true } : {}),
      ...(opts.limit ? { limit: opts.limit } : {}),
      // `force` bypasses the live-claim guard, which otherwise 409s the
      // request when any target is being executed by an online runner.
      ...(opts.force ? { force: true } : {}),
    },
  });

/**
 * Delete every entry matching a filter.
 *
 * The server rejects an unconstrained filter, so callers must always pin
 * at least project + feature_id. `force` bypasses the live-claim guard,
 * which otherwise fails the whole request (409) if any target is being
 * executed by an online runner.
 */
export const bulkDelete = (
  filter: BulkFilter,
  opts: { dryRun?: boolean; limit?: number; force?: boolean } = {},
) =>
  api<BulkDeleteResponse>("/api/v1/entries/bulk-delete", {
    method: "POST",
    // force travels in the body (current server) AND as a query param
    // (older builds read only the query) — harmless to send both.
    query: opts.force ? { force: "true" } : undefined,
    body: {
      filter,
      ...(opts.dryRun ? { dry_run: true } : {}),
      ...(opts.limit ? { limit: opts.limit } : {}),
      ...(opts.force ? { force: true } : {}),
    },
  });

/**
 * Delete an explicit list of entries by path (multi-select delete).
 *
 * The server treats `paths` as the exact work list — filter and paths are
 * mutually exclusive — and applies the same live-claim guard: any target
 * being executed by an online runner fails the request with 409 unless
 * `force`. Callers chunk to the 100-entry cap (`chunkPaths`).
 */
export const bulkDeletePaths = (
  paths: readonly string[],
  opts: { dryRun?: boolean; force?: boolean } = {},
) =>
  api<BulkDeleteResponse>("/api/v1/entries/bulk-delete", {
    method: "POST",
    query: opts.force ? { force: "true" } : undefined,
    body: {
      paths,
      ...(opts.dryRun ? { dry_run: true } : {}),
      ...(opts.force ? { force: true } : {}),
    },
  });

/** Set one status across every task in a feature. */
export const setFeatureStatus = (
  projectId: string,
  featureId: string,
  status: string,
  opts: { dryRun?: boolean; force?: boolean } = {},
) =>
  bulkUpdate(
    { project: projectId, feature_id: featureId, type: "task" },
    { status },
    opts,
  );

/** Delete every task in a feature. */
export const deleteFeatureTasks = (
  projectId: string,
  featureId: string,
  opts: { dryRun?: boolean; force?: boolean } = {},
) =>
  bulkDelete({ project: projectId, feature_id: featureId, type: "task" }, opts);

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
  /** Features this one is gated behind, from its tasks' feature-level
   *  dependency state. Present with reason "no_ready_tasks" when that is
   *  why — so the toast can name the blocker instead of telling the user to
   *  go check. waiting = the dependency is unfinished; blocked = it cannot
   *  finish (failed dependency or a cycle). */
  waitingOnFeatures?: string[];
  blockedByFeatures?: string[];
  /** Present only when the request asked for dependents. */
  dependents?: DependentQueue;
}

/** The chain enrolled by a run-with-dependents request. */
export interface DependentQueue {
  queued: string[];
  /** Reachable features deliberately NOT enrolled, mapped to why
   *  ("in_cycle", "already_settled"). Surfaced rather than dropped: a
   *  feature the user expected to run and which silently will not is the
   *  failure this surface exists to prevent. */
  skipped?: Record<string, string>;
  /** Features OUTSIDE the chain that a queued member still waits on. Under
   *  a paused project those never run, so the chain stalls there. */
  waitsOnExternal?: string[];
  truncated?: boolean;
}

/** A standing run-with-dependents request. Membership is derived server-side
 *  on every read, so it reflects edits to feature_depends_on. */
export interface DependentChain {
  projectId: string;
  rootFeatureId: string;
  requestedAt: number;
  /** Whether the project's task dial was already off when the user asked.
   *  Propagation crosses a pause that was already on; a pause applied later
   *  holds the chain. */
  pausedAtRequest: boolean;
  queued: string[];
  skipped?: Record<string, string>;
  waitsOnExternal?: string[];
  truncated?: boolean;
}

export const runFeature = (
  projectId: string,
  featureId: string,
  force = false,
  includeDependents = false,
) =>
  api<RunFeatureResponse>(
    `/api/v1/tasks/${encodeURIComponent(projectId)}/features/${encodeURIComponent(featureId)}/run`,
    {
      method: "POST",
      body: { force, includeDependents },
    },
  );

/**
 * Cancel a standing run-with-dependents chain rooted at this feature.
 *
 * Cancelling stops NEW features joining. Tasks already dispatched run to
 * completion — there is no un-dispatch, and the toast must not imply one.
 */
export const cancelDependentChain = (projectId: string, featureId: string) =>
  api<{ cancelled: boolean; detail: string }>(
    `/api/v1/tasks/${encodeURIComponent(projectId)}/features/${encodeURIComponent(featureId)}/run`,
    { method: "DELETE" },
  );

/** Standing chains for a project. Membership is derived server-side on every
 *  read, so it reflects edits to feature_depends_on. */
export const listDependentChains = (projectId: string) =>
  api<{ chains: DependentChain[] }>(
    `/api/v1/tasks/${encodeURIComponent(projectId)}/chains`,
  );

/**
 * One line describing what a run-with-dependents call actually set up.
 *
 * Reports refusals as prominently as successes: a feature the user expected
 * to be queued and which silently will not run is the failure this surface
 * exists to prevent.
 */
export function summarizeDependentQueue(q: DependentQueue | undefined): string {
  if (!q) return "";
  const parts: string[] = [];
  const n = q.queued?.length ?? 0;
  parts.push(
    n === 0
      ? "no dependent features to queue"
      : `queued ${n} dependent ${n === 1 ? "feature" : "features"}`,
  );
  if (q.truncated) parts.push("chain truncated at the server limit");
  const ext = q.waitsOnExternal ?? [];
  if (ext.length > 0) {
    // The chain will stall here: under a paused project nothing outside it
    // runs, so naming the blocker is the difference between "waiting its
    // turn" and "never going to run".
    parts.push(`also waits on ${ext.join(", ")}, not part of this run`);
  }
  const skipped = Object.entries(q.skipped ?? {});
  if (skipped.length > 0) {
    parts.push(
      skipped
        .map(([id, why]) => `${id} skipped (${why.replace(/_/g, " ")})`)
        .join("; "),
    );
  }
  return parts.join(" · ");
}

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

export const clearFeatureAssignment = (projectId: string, featureId: string) =>
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
export function summarizeRunFeatureResult(r: RunFeatureResponse): {
  message: string;
  kind: "info" | "success";
} {
  const queued = r.queued?.length ?? 0;
  if (r.dispatched && r.dispatchedCount > 0 && queued === 0) {
    return {
      message:
        r.dispatchedCount === 1
          ? "Dispatched 1 task"
          : `Dispatched ${r.dispatchedCount} tasks`,
      kind: "success",
    };
  }
  if (r.dispatched && queued > 0) {
    return {
      message: `Dispatched ${r.dispatchedCount}, queued ${queued} (auto-dispatch as slots free)`,
      kind: "success",
    };
  }
  // Nothing dispatched — surface the reason.
  const reasonText = humanizeRunFeatureReason(r);
  if (queued > 0) {
    // Queued-but-nothing-dispatched: the server parked every ready task
    // for the cascade because no runner would take it *right now*. This
    // used to read "Not triggered: nothing to dispatch", which is how a
    // whole feature could look like a dead menu item — the actual cause
    // ("no eligible runner: runner-a: project not allowed") was sitting
    // unread in `results`. Say what was queued and why nothing moved.
    return {
      message: `Queued ${queued} ${queued === 1 ? "task" : "tasks"}, nothing dispatched: ${reasonText}`,
      kind: "info",
    };
  }
  return { message: `Not triggered: ${reasonText}`, kind: "info" };
}

function humanizeRunFeatureReason(r: RunFeatureResponse): string {
  switch (r.reason) {
    case "feature_not_found":
      return "feature not found";
    case "no_ready_tasks": {
      // Name the blocker when the server knows it. "check dependencies"
      // sent the user off to look up something the response already
      // carried (waiting_on_features, added with feature-level gating).
      const blocked = r.blockedByFeatures ?? [];
      if (blocked.length) {
        return `no ready tasks — blocked by ${blocked.join(", ")}`;
      }
      const waiting = r.waitingOnFeatures ?? [];
      if (waiting.length) {
        return `no ready tasks — waiting on ${waiting.join(", ")}`;
      }
      return "no ready tasks in this feature (check dependencies)";
    }
    case "feature_in_progress":
      return "every ready task is already in flight";
    case "feature_dispatch_pending": {
      // The reason this exists: a lease stuck in "pushed" is NOT work in
      // flight. The dispatch went out and nothing came back, so nothing is
      // running and the hold clears itself when the lease expires. Saying
      // "already in flight" sent users hunting for a process that did not
      // exist; say what is true and when it resolves.
      const clears = relativeFromNow(latestPendingLeaseExpiry(r.results));
      const tail = clears ? ` (clears ${clears})` : "";
      return `a previous dispatch is still pending ack — nothing is running${tail}`;
    }
    case "runner_unreachable":
      return r.detail
        ? `dispatch not delivered (${r.detail})`
        : "the assigned runner's command stream is not connected";
    case "scheduler_not_configured":
      return "scheduler not configured on server";
    // Per-task placement reasons the server promotes to the feature level
    // when nothing dispatched (internal/service/run_feature.go).
    case "no_online_runner":
      return "no runners are online";
    case "no_eligible_runner":
      return r.detail
        ? `no eligible runner (${r.detail})`
        : "no eligible runner for these tasks";
    case "":
    case undefined:
      // Older servers leave the top-level reason empty when every task was
      // skipped, so read the cause out of the per-task results instead of
      // shrugging. Keeps the PWA honest against a backend that has not
      // been redeployed yet.
      return firstSkipReason(r.results) ?? "nothing to dispatch";
    default:
      return r.detail ? `${r.reason}: ${r.detail}` : r.reason;
  }
}

/**
 * Latest expiry among the leases that are holding this feature and have not
 * been acknowledged. It is what turns "wait" into "wait until when" — the
 * one piece of information that tells a user to sit still rather than go
 * looking for a runner to restart.
 */
function latestPendingLeaseExpiry(
  results?: RunTaskResponse[],
): string | undefined {
  let latest: string | undefined;
  for (const one of results ?? []) {
    if (one.dispatched || one.leaseState !== "pushed" || !one.expiresAt)
      continue;
    if (!latest || one.expiresAt > latest) latest = one.expiresAt;
  }
  return latest;
}

/**
 * Humanized reason from the first skipped task of a feature run, or
 * undefined when nothing in `results` explains itself. Reuses the per-task
 * humanizer so a feature-level toast reads exactly like the single-task one
 * — including lease owner and expiry for `already_leased`.
 */
function firstSkipReason(results?: RunTaskResponse[]): string | undefined {
  const skipped = results?.find((one) => !one.dispatched && one.reason);
  return skipped ? humanizeRunReason(skipped) : undefined;
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

/** Response from POST /tasks/{project}/run — fans out RunFeatureNow across
 *  every ready feature in the project. Skipped features (no ready tasks)
 *  show up in results with a reason and count in featuresSkipped. */
export interface RunProjectResponse {
  projectId: string;
  featuresConsidered: number;
  featuresDispatched: number;
  featuresSkipped: number;
  totalTasksDispatched: number;
  results?: RunFeatureResponse[];
  reason?: string;
}

// runProject dispatches every ready feature in the project in one call.
// Convenience over calling /features/{f}/run repeatedly from the client;
// server iterates and calls RunFeatureNow per feature. Returns 501 if the
// backend isn't wired for project-level runs — callers should gracefully
// fall back to per-feature dispatch.
export const runProject = (projectId: string, force = false) =>
  api<RunProjectResponse>(
    `/api/v1/tasks/${encodeURIComponent(projectId)}/run`,
    {
      method: "POST",
      body: { force },
    },
  );

// One-line toast summary of a project-scoped run. Mirrors
// summarizeRunFeatureResult style so calling code can reuse the pattern.
export const summarizeRunProjectResult = (r: RunProjectResponse): string => {
  if (r.featuresConsidered === 0) {
    return r.reason === "no_ready_tasks"
      ? "No ready tasks in project"
      : "No features to run";
  }
  if (r.featuresDispatched === 0) {
    return `Nothing dispatched (${r.featuresSkipped} feature${r.featuresSkipped === 1 ? "" : "s"} skipped)`;
  }
  const fs = r.featuresDispatched === 1 ? "" : "s";
  const ts = r.totalTasksDispatched === 1 ? "" : "s";
  const skipped =
    r.featuresSkipped > 0
      ? ` · ${r.featuresSkipped} feature${r.featuresSkipped === 1 ? "" : "s"} skipped`
      : "";
  return `Dispatched ${r.totalTasksDispatched} task${ts} across ${r.featuresDispatched} feature${fs}${skipped}`;
};

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
export const runBlockedInspectorNow = (
  projectId: string,
  featureId: string,
) => {
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
export const releaseDispatchLease = (
  runnerId: string,
  projectId: string,
  taskId: string,
) =>
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

function runResponseToTrigger(
  r: RunTaskResponse,
  projectId: string,
): TriggerResponse {
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
    abs < 60
      ? `${abs}s`
      : abs < 3600
        ? `${Math.round(abs / 60)}m`
        : abs < 86400
          ? `${Math.round(abs / 3600)}h`
          : `${Math.round(abs / 86400)}d`;
  return deltaSec >= 0 ? `in ${fmt}` : `${fmt} ago`;
}

function humanizeRunReason(r: RunTaskResponse): string {
  const reason = r.reason;
  const detail = r.detail;
  switch (reason) {
    case "no_online_runner":
      return "no runners are online";
    case "no_eligible_runner":
      return detail
        ? `no eligible runner (${detail})`
        : "no eligible runner for this task";
    case "all_runners_at_capacity":
      return "all runners are at capacity";
    case "task_not_ready":
      return "task is not ready (check dependencies)";
    case "already_leased": {
      // Surface the actual lease owner + state so users can tell at a
      // glance whether to wait, abort, or force-redispatch. The two states
      // are different situations: acked means a runner said it took the
      // work and is running it; pushed means the dispatch went out and
      // nothing came back, so nothing is running and the hold expires on
      // its own. One sentence for both read as "a process exists" either
      // way, which is exactly the hunt this avoids.
      if (!r.runnerId) return "task already dispatched to a runner";
      const rel = relativeFromNow(r.expiresAt);
      if (r.leaseState === "pushed") {
        const tail = rel ? ` (clears ${rel})` : "";
        return `pushed to ${r.runnerId}, not acknowledged yet — nothing running${tail}`;
      }
      const parts: string[] = [r.runnerId];
      if (r.leaseState) parts.push(r.leaseState);
      if (rel) parts.push(`expires ${rel}`);
      return `already dispatched to ${parts[0]} (${parts.slice(1).join(", ")})`;
    }
    case "runner_unreachable":
      return detail
        ? `dispatch not delivered (${detail})`
        : "the assigned runner's command stream is not connected";
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
export function summarizeTriggerResults(results: TriggerResponse[]): {
  message: string;
  kind: "info" | "success";
} {
  const triggered = results.filter((r) => r.triggered);
  const skipped = results.filter((r) => !r.triggered);

  if (triggered.length && !skipped.length) {
    return {
      message:
        triggered.length === 1 ? "Triggered" : `Triggered ${triggered.length}`,
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
    message:
      skipped.length === 1
        ? `Not triggered: ${reason}`
        : `Skipped ${skipped.length}: ${reason}`,
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
  executed_actions: {
    type: string;
    status: string;
    result?: unknown;
    error?: string;
  }[];
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
}) =>
  api<AssistantChatResponse>("/api/v1/assistant/chat", {
    method: "POST",
    body,
  });

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
  if (!res.body)
    throw new ApiError(0, "Streaming response body is unavailable");
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
  api<{ reply: string; draft: AssistantGoalDraft }>(
    "/api/v1/assistant/goal-draft",
    {
      method: "POST",
      body,
    },
  );

/**
 * Fetch attachment bytes as an object URL.
 *
 * `download_url` cannot be handed straight to <img src>: the API takes a
 * Bearer token in a header, which the browser will not attach to an image
 * request, so an auth-enabled server answers 401 and the picture silently
 * breaks. Fetching through `api()` and wrapping the blob keeps one auth
 * path for everything.
 *
 * The caller owns the returned URL and must revokeObjectURL it.
 */
export async function fetchAttachmentObjectURL(
  downloadUrl: string,
): Promise<string> {
  const res = await api<Response>(downloadUrl, { raw: true });
  return URL.createObjectURL(await res.blob());
}

export async function uploadAttachment(
  projectId: string,
  file: Blob,
  filename: string,
  metadata?: Record<string, unknown>,
) {
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
  return (await res.json()) as {
    attachment: { id: string; filename: string; content_type: string };
  };
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

// Project-scoped pause dials. Despite the /tasks/runner/ path this endpoint
// knows nothing about runners: `pausedProjects` is the project task dial and
// `automationPausedProjects` the project automations dial, and the top-level
// `paused` / `automationsPaused` booleans are just "is that list non-empty".
// Runner-scoped pause lives on RunnerInfo.paused from getRunners(). See the
// FOOTGUN note on RunnerStatusResponse in lib/types.
export const getRunnerStatus = () =>
  api<RunnerStatusResponse>("/api/v1/tasks/runner/status");

// Scheduler loop state, including per-project skip counts from the last pass.
// This is the only place the server explains *why* a ready task was not
// dispatched at project granularity (`last_project_results[project]`).
export const getSchedulerStatus = () =>
  api<SchedulerStatus>("/api/v1/scheduler/status");

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

// Runner-scoped pause dial — a THIRD dial, independent of the two project
// dials above. A paused runner accepts no dispatch for any project; a paused
// project stops dispatch on every runner. Neither implies the other, and
// neither is reported by the other's status endpoint: runner pause reads back
// as the `paused` field on GET /runners, never from /tasks/runner/status.
//
// Persisted server-side (runner_pause_state) before the SSE command is
// published, so the dial survives a runner restart or reconnect.
export const pauseRunner = (runnerId: string) =>
  api<RunnerPauseResponse>(
    `/api/v1/runners/${encodeURIComponent(runnerId)}/pause`,
    { method: "PUT", body: {} },
  );
export const resumeRunner = (runnerId: string) =>
  api<RunnerPauseResponse>(
    `/api/v1/runners/${encodeURIComponent(runnerId)}/resume`,
    { method: "PUT", body: {} },
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

export const controlCreateSession = (
  runnerId: string,
  instanceId: string,
  title?: string,
) =>
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

export const controlAbort = (
  runnerId: string,
  instanceId: string,
  sessionId: string,
) =>
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

export const controlPendingPermissions = (
  runnerId: string,
  instanceId: string,
) =>
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

export const controlSpawnInstance = (
  runnerId: string,
  spec: SpawnInstanceSpec,
) =>
  api<{ success: boolean; instance: OpencodeInstance }>(
    `/api/v1/control/runners/${encodeURIComponent(runnerId)}/instances`,
    { method: "POST", body: spec },
  );

export const controlKillInstance = (runnerId: string, instanceId: string) =>
  api<{ success: boolean }>(controlBase(runnerId, instanceId), {
    method: "DELETE",
  });

export const controlAbortTask = (runnerId: string, taskId: string) =>
  api<{ success: boolean }>(
    `/api/v1/control/runners/${encodeURIComponent(runnerId)}/tasks/${encodeURIComponent(taskId)}/abort`,
    { method: "POST" },
  );

// The live event stream is consumed by lib/instanceStream.ts — a
// fetch-based SSE client with header auth and explicit 401 refresh.
// (The old `controlEventsUrl` ?token= EventSource helper was removed so
// exactly one auth convention exists for the endpoint.)

// ─── Ad-hoc runner shell (streaming exec) ────────────────────────
//
// POST /api/v1/control/runners/{id}/exec answers with text/event-stream
// (started → exec_data* → exec_exit), so it can't go through api<T>(),
// which reads one JSON body to completion. It's hand-rolled on the same
// pattern as lib/instanceStream.ts:
//   • fetch, not EventSource — EventSource can't POST and can't set an
//     Authorization header (?token= is worse for auth and for logs)
//   • an explicit once-through 401 refresh, because api()'s refresh
//     wrapper doesn't cover hand-rolled streams
//   • a reader loop over response.body, frames split on the blank line
//     and parsed by the shared parseSSEFrame
//
// Errors raised BEFORE the stream opens (bad request, runner offline,
// missing scope) come back as normal JSON errors and are rethrown as
// ApiError. Once the stream is open, failures arrive in-band on the
// exec_exit frame's `error` field.
//
// Unlike the other streams in this app, exec does NOT reconnect: an exec
// is a single one-shot command, and a silent retry would re-run it.

export interface ExecStartedEvent {
  exec_id: string;
  runner_id: string;
  workdir: string;
}

export interface ExecDataEvent {
  exec_id: string;
  stream: "stdout" | "stderr";
  chunk: string;
}

export interface ExecExitEvent {
  exec_id: string;
  exit_code: number;
  error?: string;
}

/**
 * Output the server could not deliver. The fan-out from the runner is
 * non-blocking, so a browser that falls far enough behind loses chunks —
 * the server counts them and says so rather than letting the transcript go
 * quietly incomplete.
 */
export interface ExecTruncatedEvent {
  exec_id: string;
  dropped_chunks: number;
  dropped_bytes: number;
}

export interface ControlExecHandlers {
  onStarted?: (e: ExecStartedEvent) => void;
  onData?: (e: ExecDataEvent) => void;
  onTruncated?: (e: ExecTruncatedEvent) => void;
  onExit?: (e: ExecExitEvent) => void;
}

export interface ControlExecRequest {
  command: string;
  workdir?: string;
  timeout_ms?: number;
}

function dispatchExecFrame(
  frame: { event: string; data: string },
  handlers: ControlExecHandlers,
): void {
  let payload: unknown;
  try {
    payload = JSON.parse(frame.data);
  } catch {
    return; // malformed frame — drop it rather than killing the stream
  }
  if (!payload || typeof payload !== "object") return;

  switch (frame.event) {
    case "started":
      handlers.onStarted?.(payload as ExecStartedEvent);
      break;
    case "exec_data": {
      const d = payload as ExecDataEvent;
      if (typeof d.chunk === "string") {
        handlers.onData?.({
          exec_id: d.exec_id,
          stream: d.stream === "stderr" ? "stderr" : "stdout",
          chunk: d.chunk,
        });
      }
      break;
    }
    case "exec_truncated": {
      const d = payload as ExecTruncatedEvent;
      handlers.onTruncated?.({
        exec_id: d.exec_id,
        dropped_chunks: Number(d.dropped_chunks) || 0,
        dropped_bytes: Number(d.dropped_bytes) || 0,
      });
      break;
    }
    case "exec_exit": {
      const d = payload as ExecExitEvent;
      handlers.onExit?.({
        exec_id: d.exec_id,
        exit_code: typeof d.exit_code === "number" ? d.exit_code : 0,
        error: typeof d.error === "string" ? d.error : "",
      });
      break;
    }
    default:
      // heartbeat and anything unknown
      break;
  }
}

/**
 * Run one command on a runner host and stream its output.
 *
 * Resolves when the server closes the stream. Rejects with ApiError if
 * the request fails before the stream opens, and with the fetch's own
 * AbortError if `signal` fires — callers must catch both (an unhandled
 * rejection here would otherwise escape into the console).
 */
export async function controlExec(
  runnerId: string,
  req: ControlExecRequest,
  handlers: ControlExecHandlers,
  signal?: AbortSignal,
): Promise<void> {
  const url = `/api/v1/control/runners/${encodeURIComponent(runnerId)}/exec`;

  const open = (): Promise<Response> =>
    fetch(url, {
      method: "POST",
      headers: {
        ...useAuth.getState().authHeader(),
        "Content-Type": "application/json",
        Accept: "text/event-stream",
        "Cache-Control": "no-cache",
      },
      body: JSON.stringify(req),
      signal,
    });

  let res = await open();
  if (res.status === 401) {
    const refreshed = await useAuth.getState().onUnauthorized();
    if (refreshed) res = await open();
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
  if (!res.body) {
    throw new ApiError(res.status, "exec: server sent no stream body");
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buf = "";
  try {
    for (;;) {
      const { done, value } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      for (;;) {
        // Accept both LF and CRLF frame separators.
        const lf = buf.indexOf("\n\n");
        const crlf = buf.indexOf("\r\n\r\n");
        if (lf === -1 && crlf === -1) break;
        const useCrlf = crlf !== -1 && (lf === -1 || crlf < lf);
        const sep = useCrlf ? crlf : lf;
        const block = buf.slice(0, sep);
        buf = buf.slice(sep + (useCrlf ? 4 : 2));
        const frame = parseSSEFrame(block);
        if (frame) dispatchExecFrame(frame, handlers);
      }
    }
  } finally {
    void reader.cancel().catch(() => {});
  }
}

/**
 * Signal a running exec. "int" is Ctrl+C; "term"/"kill" escalate.
 */
export const controlExecSignal = (
  runnerId: string,
  execId: string,
  signal: "int" | "term" | "kill" = "int",
) =>
  api<{ success: boolean }>(
    `/api/v1/control/runners/${encodeURIComponent(runnerId)}/exec/${encodeURIComponent(execId)}/signal`,
    { method: "POST", body: { signal } },
  );

// ─── Brain entries / search ──────────────────────────────────────

export const listEntries = (query?: {
  project?: string;
  type?: string;
  status?: string;
  limit?: number;
  global?: string;
  /** Comma-separated multi-project scope, e.g. "hindsight,pwa,global".
   *  The reserved member "global" admits project-less entries. Supersedes
   *  `project` / `global` server-side. */
  projects?: string;
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
    listEntries({
      type: "automation",
      limit: 500,
      ...(project ? { project } : {}),
    }).then((r) => r.entries || []),
    // Built-in automations are global; always include them.
    listEntries({ type: "automation", global: "true", limit: 500 }).then(
      (r) => r.entries || [],
    ),
    listEntries({
      type: "task",
      limit: 1000,
      ...(project ? { project } : {}),
    }).then((r) => r.entries || []),
    listEntries({
      type: "automation_run",
      limit: 500,
      ...(project ? { project } : {}),
    })
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

// ─── Entry graph (backlinks / outlinks / related) ────────────────
// The graph routes address entries by 8-char short id (single path
// segment) and return bare BrainEntry arrays. Go serializes empty
// results as JSON null, so coerce.

export const getBacklinks = (id: string) =>
  api<BrainEntry[] | null>(
    `/api/v1/entries/${encodeURIComponent(id)}/backlinks`,
  ).then((r) => r || []);

export const getOutlinks = (id: string) =>
  api<BrainEntry[] | null>(
    `/api/v1/entries/${encodeURIComponent(id)}/outlinks`,
  ).then((r) => r || []);

export const getRelated = (id: string, limit = 10) =>
  api<BrainEntry[] | null>(
    `/api/v1/entries/${encodeURIComponent(id)}/related`,
    { query: { limit } },
  ).then((r) => r || []);

// ─── Brain stats (entry counts by type) ──────────────────────────

export interface BrainStats {
  totalEntries: number;
  globalEntries: number;
  projectEntries: number;
  byType: Record<string, number>;
  orphanCount?: number;
  staleCount?: number;
}

/** Entry counts. `projects` is the comma-separated multi-project scope
 *  (see listEntries) and supersedes the single-project / global forms. */
export const getBrainStats = (
  project?: string,
  global?: boolean,
  projects?: string,
) =>
  api<BrainStats>("/api/v1/stats", {
    query: projects
      ? { projects }
      : global
        ? { global: "true" }
        : project
          ? { project }
          : undefined,
  });

export const embedBackfill = (body: {
  project?: string;
  force?: boolean;
  dry_run?: boolean;
}) => api<unknown>("/api/v1/embeddings/backfill", { method: "POST", body });

// ─── Goals ───────────────────────────────────────────────────────

/**
 * List goal automations. Without `status` the server returns its default
 * set (active + blocked + completed; archived hidden). Pass
 * `status: "archived"` for archived only, `"all"` for everything, or an
 * exact status for a single-status match.
 */
export const listGoals = (params?: {
  project?: string;
  feature_id?: string;
  status?: string;
}) =>
  api<GoalListResponse>("/api/v1/goals", { query: params }).then(
    (r) => r.goals || [],
  );

export const createGoal = (body: CreateGoalRequest) =>
  api<GoalSummary>("/api/v1/goals", {
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
  api<GoalSummary>(`/api/v1/goals/${encodeURIComponent(goalId)}`, {
    method: "PATCH",
    body: patch,
  });

/** Manual reconcile; the response is the audit record it produced. */
export const runGoal = (goalId: string) =>
  api<GoalReconcileAudit>(`/api/v1/goals/${encodeURIComponent(goalId)}/run`, {
    method: "POST",
  });

export const deleteGoal = (goalId: string) =>
  api<{ success: boolean; goal_id: string }>(
    `/api/v1/goals/${encodeURIComponent(goalId)}`,
    { method: "DELETE" },
  );

// Lifecycle wrappers — status mapping mirrors the MCP goal_pause /
// goal_resume / goal_archive tools (internal/mcp/goal_tools.go):
// pause=blocked, resume=active, archive=archived.
export const pauseGoal = (goalId: string) =>
  updateGoal(goalId, { status: "blocked" });

export const resumeGoal = (goalId: string) =>
  updateGoal(goalId, { status: "active" });

export const archiveGoal = (goalId: string) =>
  updateGoal(goalId, { status: "archived" });

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

export const getServerConfig = () => api<ConfigGetResponse>("/api/v1/config");

export const getConfigSchema = () =>
  api<{ fields: ConfigField[] }>("/api/v1/config/schema");

export const updateServerConfig = (cfg: ServerConfig) =>
  api<ConfigUpdateResponse>("/api/v1/config", {
    method: "PUT",
    body: { config: cfg },
  });

// Typed HTTP client for brain-api. Attaches the bearer token, transparently
// refreshes on 401 (once), and exposes thin wrappers for the endpoints the PWA
// uses. Task mutations go through the entries endpoint (PATCH/DELETE
// /api/v1/entries/{path}); trigger/pause/resume use the tasks endpoints.

import { API_V1 } from "./config";
import { useAuth } from "./auth";
import type {
  BrainEntry,
  GoalAuditResponse,
  GoalListResponse,
  GoalProgressResponse,
  Health,
  ListEntriesResponse,
  ProjectListResponse,
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
  const headers: Record<string, string> = { ...auth.authHeader() };
  let body: BodyInit | undefined;
  if (opts.body !== undefined) {
    headers["Content-Type"] = "application/json";
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

export const deleteEntry = (path: string) =>
  api<unknown>(`/api/v1/entries/${encodeEntryPath(path)}`, {
    method: "DELETE",
    query: { confirm: "true" },
  });

export const setTaskStatus = (task: Task, status: string) =>
  updateEntry(task.path, { status });

export const triggerTask = (projectId: string, taskId: string) =>
  api<unknown>(
    `/api/v1/tasks/${encodeURIComponent(projectId)}/${encodeURIComponent(taskId)}/trigger`,
    { method: "POST" },
  );

// ─── Runner control ──────────────────────────────────────────────

export const getRunners = () =>
  api<RunnerListResponse>("/api/v1/runners").then((r) => r.runners || []);

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
export const pauseAutomations = () =>
  api("/api/v1/tasks/runner/automations/pause", { method: "POST" });
export const resumeAutomations = () =>
  api("/api/v1/tasks/runner/automations/resume", { method: "POST" });

export const shutdownRunner = (runnerId: string, reason = "manual") =>
  api(`/api/v1/runners/${encodeURIComponent(runnerId)}/shutdown`, {
    method: "PUT",
    body: { reason },
  });

// ─── Brain entries / search ──────────────────────────────────────

export const listEntries = (query?: {
  project?: string;
  type?: string;
  limit?: number;
}) => api<ListEntriesResponse>("/api/v1/entries", { query });

export const getEntry = (path: string) =>
  api<BrainEntry>(`/api/v1/entries/${encodeEntryPath(path)}`);

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

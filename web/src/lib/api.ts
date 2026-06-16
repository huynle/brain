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

export const triggerTask = (projectId: string, taskId: string) =>
  api<unknown>(
    `/api/v1/tasks/${encodeURIComponent(projectId)}/${encodeURIComponent(taskId)}/trigger`,
    { method: "POST" },
  );

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
  limit?: number;
  global?: string;
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
    listEntries({ type: "automation", ...(project ? { project } : {}) }).then(
      (r) => r.entries || [],
    ),
    // Built-in automations are global; always include them.
    listEntries({ type: "automation", global: "true" }).then((r) => r.entries || []),
    listEntries({ type: "task", ...(project ? { project } : {}) }).then(
      (r) => r.entries || [],
    ),
    listEntries({ type: "automation_run", ...(project ? { project } : {}) })
      .then((r) => r.entries || [])
      .catch(() => [] as BrainEntry[]),
  ]);
  // Merge scoped + global automation entries, de-duped by id.
  const byId = new Map<string, BrainEntry>();
  for (const e of [...global, ...scoped]) byId.set(e.id, e);
  return { automations: [...byId.values()], tasks, runs };
}

// executeAutomation triggers a manual run of an automation entry: it reads the
// entry's action and creates a generated task (generated_by automation:<id>),
// mirroring the TUI's runAutomationRowCmd. The runner then picks it up and the
// task appears under the automation via its generated_by linkage.
export async function executeAutomation(
  path: string,
  fallbackProject: string,
): Promise<CreateEntryResponse> {
  const entry = await getEntry(path);
  const action = entry.action;
  if (!action) throw new Error("automation has no action");
  const project = entry.project_id || fallbackProject;
  if (!project || project === "all") throw new Error("automation has no project");
  const prompt =
    (action.type === "script" ? (action.command as string) : (action.direct_prompt as string)) || "";
  if (!prompt) throw new Error("automation action has no prompt or command");

  const now = Date.now();
  const body: Record<string, unknown> = {
    type: "task",
    title: `Automation: ${entry.id}`,
    content: prompt,
    status: "pending",
    project,
    generated_by: `automation:${entry.id}`,
    generated_key: `automation:manual:${entry.id}:${now}`,
    direct_prompt: prompt,
    agent: entry.agent || action.agent,
    model: entry.model || action.model,
    executor: action.type === "script" ? "script" : entry.executor || action.executor,
    execution_mode: entry.execution_mode || (action.execution_mode as string),
    complete_on_idle: (action.complete_on_idle as boolean) ?? true,
    target_workdir: entry.target_workdir || (action.target_workdir as string),
  };
  return api<CreateEntryResponse>("/api/v1/entries", { method: "POST", body });
}

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

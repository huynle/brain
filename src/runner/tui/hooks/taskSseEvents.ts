import type { SessionInfo } from '../../../core/types';
import type { ProjectStats, TaskDisplay, TUISSEEvent } from '../types';
import { appendFileSync } from 'fs';

const DEBUG_LOG = '/tmp/tui-sse-debug.log';

export function isSseDebugLoggingEnabled(): boolean {
  return process.env.BRAIN_TUI_SSE_DEBUG === '1';
}

const SSE_DEBUG_ENABLED = isSseDebugLoggingEnabled();

function debugLog(message: string) {
  if (!SSE_DEBUG_ENABLED) {
    return;
  }

  try {
    appendFileSync(DEBUG_LOG, `[${new Date().toISOString()}] ${message}\n`);
  } catch {
    // Ignore errors
  }
}

type NormalizeTaskSSEEventParams = {
  event: string;
  data: string;
  fallbackProjectId?: string;
};

type LegacyTaskSessionShape = {
  sessions?: Record<string, SessionInfo>;
  session_ids?: unknown;
  session_timestamps?: unknown;
};

type RawTaskLike = Record<string, unknown>;

function isRecord(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value);
}

function asString(value: unknown): string | undefined {
  return typeof value === 'string' && value.length > 0 ? value : undefined;
}

function asStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.filter((item): item is string => typeof item === 'string' && item.length > 0);
}

function asNumber(value: unknown): number | undefined {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined;
}

function normalizeExecutionMode(value: unknown): TaskDisplay['executionMode'] {
  const mode = asString(value);
  if (mode === 'worktree' || mode === 'current_branch') {
    return mode;
  }
  if (mode === 'in_branch') {
    return 'current_branch';
  }
  return undefined;
}

function buildDependencyMap(tasks: RawTaskLike[]): Map<string, string[]> {
  const map = new Map<string, string[]>();
  for (const task of tasks) {
    const id = asString(task.id);
    if (!id) continue;
    map.set(id, asStringArray(task.resolved_deps ?? task.dependencies));
  }
  return map;
}

function computeDependents(tasks: RawTaskLike[], dependencyMap: Map<string, string[]>): Map<string, string[]> {
  const dependentsMap = new Map<string, string[]>();

  for (const task of tasks) {
    const id = asString(task.id);
    if (id) {
      dependentsMap.set(id, []);
    }
  }

  for (const task of tasks) {
    const taskId = asString(task.id);
    if (!taskId) continue;
    const deps = dependencyMap.get(taskId) ?? [];
    for (const depId of deps) {
      const existing = dependentsMap.get(depId);
      if (existing) {
        existing.push(taskId);
      }
    }
  }

  return dependentsMap;
}

function computeTransitiveAncestors(
  taskId: string,
  dependencyMap: Map<string, string[]>
): { directAncestors: string[]; indirectAncestors: string[] } {
  const directAncestors = dependencyMap.get(taskId) ?? [];
  const allAncestors = new Set<string>();
  const visited = new Set<string>();
  const queue = [...directAncestors];
  const directSet = new Set(directAncestors);

  while (queue.length > 0) {
    const currentId = queue.shift();
    if (!currentId || visited.has(currentId)) continue;
    visited.add(currentId);
    allAncestors.add(currentId);

    const currentDeps = dependencyMap.get(currentId) ?? [];
    for (const depId of currentDeps) {
      if (!visited.has(depId)) {
        queue.push(depId);
      }
    }
  }

  return {
    directAncestors,
    indirectAncestors: [...allAncestors].filter((id) => !directSet.has(id)),
  };
}

export function normalizeTaskSessions(task: LegacyTaskSessionShape): Record<string, SessionInfo> | undefined {
  if (isRecord(task.sessions) && Object.keys(task.sessions).length > 0) {
    return task.sessions;
  }

  const legacyIds = task.session_ids;
  const legacyTimestamps = isRecord(task.session_timestamps) ? task.session_timestamps : {};

  if (Array.isArray(legacyIds)) {
    const normalized: Record<string, SessionInfo> = {};
    for (const id of legacyIds) {
      if (typeof id !== 'string' || id.length === 0) continue;
      const timestamp = typeof legacyTimestamps[id] === 'string' ? (legacyTimestamps[id] as string) : '';
      normalized[id] = { timestamp };
    }
    return Object.keys(normalized).length > 0 ? normalized : undefined;
  }

  if (isRecord(legacyIds) && Object.keys(legacyIds).length > 0) {
    const normalized: Record<string, SessionInfo> = {};
    for (const [id, rawValue] of Object.entries(legacyIds)) {
      if (!id) continue;
      if (isRecord(rawValue) && typeof rawValue.timestamp === 'string') {
        normalized[id] = {
          timestamp: rawValue.timestamp,
          ...(typeof rawValue.cron_id === 'string' ? { cron_id: rawValue.cron_id } : {}),
          ...(typeof rawValue.run_id === 'string' ? { run_id: rawValue.run_id } : {}),
        };
        continue;
      }
      if (typeof rawValue === 'string') {
        normalized[id] = { timestamp: rawValue };
        continue;
      }
      normalized[id] = { timestamp: '' };
    }
    return Object.keys(normalized).length > 0 ? normalized : undefined;
  }

  return undefined;
}

function normalizeTask(
  rawTask: RawTaskLike,
  idToTitle: Map<string, string>,
  dependentsMap: Map<string, string[]>,
  dependencyMap: Map<string, string[]>,
  projectId?: string
): TaskDisplay {
  const taskId = asString(rawTask.id) ?? '';
  const depIds = asStringArray(rawTask.resolved_deps ?? rawTask.dependencies);
  const dependentIds = dependentsMap.get(taskId) ?? asStringArray(rawTask.dependents);
  const indirectAncestors = taskId
    ? computeTransitiveAncestors(taskId, dependencyMap).indirectAncestors
    : [];

  return {
    id: taskId,
    path: asString(rawTask.path) ?? '',
    title: asString(rawTask.title) ?? '',
    status: (asString(rawTask.status) ?? 'pending') as TaskDisplay['status'],
    priority: (asString(rawTask.priority) ?? 'medium') as TaskDisplay['priority'],
    tags: asStringArray(rawTask.tags),
    schedule: asString(rawTask.schedule) ?? null,
    scheduleEnabled:
      typeof rawTask.schedule_enabled === 'boolean' ? rawTask.schedule_enabled : undefined,
    dependencies: depIds,
    dependents: dependentIds,
    dependencyTitles: depIds.map((id) => idToTitle.get(id) ?? id),
    dependentTitles: dependentIds.map((id) => idToTitle.get(id) ?? id),
    indirectAncestorTitles: indirectAncestors.map((id) => idToTitle.get(id) ?? id),
    progress: asNumber(rawTask.progress),
    error: asString(rawTask.error),
    parent_id: asString(rawTask.parent_id),
    created: asString(rawTask.created),
    modified: asString(rawTask.modified),
    frontmatter: isRecord(rawTask.frontmatter) ? rawTask.frontmatter : undefined,
    workdir: asString(rawTask.workdir) ?? null,
    gitRemote: asString(rawTask.git_remote) ?? null,
    gitBranch: asString(rawTask.git_branch) ?? null,
    mergeTargetBranch: asString(rawTask.merge_target_branch) ?? null,
    mergePolicy: asString(rawTask.merge_policy) as TaskDisplay['mergePolicy'],
    mergeStrategy: asString(rawTask.merge_strategy) as TaskDisplay['mergeStrategy'],
    remoteBranchPolicy: asString(rawTask.remote_branch_policy) as TaskDisplay['remoteBranchPolicy'],
    openPrBeforeMerge:
      typeof rawTask.open_pr_before_merge === 'boolean'
        ? rawTask.open_pr_before_merge
        : undefined,
    executionMode: normalizeExecutionMode(rawTask.execution_mode),

    completeOnIdle:
      typeof rawTask.complete_on_idle === 'boolean' ? rawTask.complete_on_idle : undefined,
    userOriginalRequest: asString(rawTask.user_original_request) ?? null,
    resolvedDeps: asStringArray(rawTask.resolved_deps).map((id) => idToTitle.get(id) ?? id),
    unresolvedDeps: asStringArray(rawTask.unresolved_deps),
    classification: asString(rawTask.classification) as TaskDisplay['classification'],
    blockedBy: asStringArray(rawTask.blocked_by).map((id) => idToTitle.get(id) ?? id),
    blockedByReason: asString(rawTask.blocked_by_reason),
    waitingOn: asStringArray(rawTask.waiting_on).map((id) => idToTitle.get(id) ?? id),
    inCycle: typeof rawTask.in_cycle === 'boolean' ? rawTask.in_cycle : undefined,
    resolvedWorkdir: asString(rawTask.resolved_workdir) ?? null,
    feature_id: asString(rawTask.feature_id),
    feature_priority: asString(rawTask.feature_priority) as TaskDisplay['feature_priority'],
    feature_depends_on: asStringArray(rawTask.feature_depends_on),
    agent: asString(rawTask.agent) ?? null,
    model: asString(rawTask.model) ?? null,
    direct_prompt: asString(rawTask.direct_prompt) ?? null,
    sessions: normalizeTaskSessions(rawTask),
    projectId: asString(rawTask.projectId) ?? projectId,
  };
}

function normalizeSnapshotStats(rawStats: unknown, tasks: TaskDisplay[]): ProjectStats['stats'] {
  const stats = isRecord(rawStats) ? rawStats : {};
  const inProgress = tasks.filter((task) => task.status === 'in_progress').length;
  const completed = tasks.filter((task) => task.status === 'completed' || task.status === 'validated').length;

  return {
    ready: asNumber(stats.ready) ?? tasks.filter((task) => task.status === 'pending').length,
    waiting: asNumber(stats.waiting) ?? 0,
    blocked: asNumber(stats.blocked) ?? tasks.filter((task) => task.status === 'blocked').length,
    inProgress,
    completed,
  };
}

export function normalizeTaskSSEEvent(params: NormalizeTaskSSEEventParams): TUISSEEvent | null {
  const { event, data, fallbackProjectId } = params;

  debugLog(`[normalizeTaskSSEEvent] Called with event: ${event}, fallbackProjectId: ${fallbackProjectId}`);

  let parsed: unknown;
  try {
    parsed = JSON.parse(data);
    debugLog('[normalizeTaskSSEEvent] Parsed JSON successfully');
  } catch (err) {
    debugLog(`[normalizeTaskSSEEvent] JSON parse failed: ${err}`);
    return null;
  }

  if (!isRecord(parsed)) {
    debugLog('[normalizeTaskSSEEvent] Parsed data is not a record');
    return null;
  }

  const type = asString(parsed.type) ?? event;
  const timestamp = asString(parsed.timestamp) ?? new Date().toISOString();
  const projectId = asString(parsed.projectId) ?? fallbackProjectId;

  debugLog(`[normalizeTaskSSEEvent] Extracted type: ${type}, projectId: ${projectId}`);

  if (type === 'connected') {
    return {
      type: 'connected',
      transport: 'sse',
      timestamp,
      ...(projectId ? { projectId } : {}),
    };
  }

  if (type === 'heartbeat') {
    return {
      type: 'heartbeat',
      transport: 'sse',
      timestamp,
      ...(projectId ? { projectId } : {}),
    };
  }

  if (type === 'error') {
    return {
      type: 'error',
      transport: 'sse',
      timestamp,
      ...(projectId ? { projectId } : {}),
      message: asString(parsed.message) ?? 'Unknown SSE error',
      code: asString(parsed.code),
    };
  }

  if (type === 'tasks_snapshot') {
    if (!projectId) {
      debugLog('[normalizeTaskSSEEvent] tasks_snapshot missing projectId!');
      return null;
    }

    const rawTasks = Array.isArray(parsed.tasks) ? parsed.tasks.filter(isRecord) : [];
    debugLog(`[normalizeTaskSSEEvent] Raw tasks array length: ${Array.isArray(parsed.tasks) ? parsed.tasks.length : 'NOT_ARRAY'}`);
    debugLog(`[normalizeTaskSSEEvent] Filtered tasks (isRecord) count: ${rawTasks.length}`);
    
    const idToTitle = new Map<string, string>();
    for (const rawTask of rawTasks) {
      const id = asString(rawTask.id);
      const title = asString(rawTask.title);
      if (id && title) {
        idToTitle.set(id, title);
      }
    }

    const dependencyMap = buildDependencyMap(rawTasks);
    const dependentsMap = computeDependents(rawTasks, dependencyMap);

    const tasks = rawTasks.map((rawTask) =>
      normalizeTask(rawTask, idToTitle, dependentsMap, dependencyMap, projectId)
    );

    debugLog(`[normalizeTaskSSEEvent] Normalized tasks count: ${tasks.length}`);

    return {
      type: 'tasks_snapshot',
      transport: 'sse',
      timestamp,
      projectId,
      tasks,
      stats: normalizeSnapshotStats(parsed.stats, tasks),
    };
  }

  debugLog(`[normalizeTaskSSEEvent] Unknown event type: ${type}`);
  return null;
}

/**
 * Normalize a raw tasks snapshot from the REST API into TaskDisplay[].
 * Re-uses the same normalizeTask() pipeline as SSE events so field names
 * (depends_on → dependencies, etc.) are mapped consistently.
 */
export function normalizeTasksSnapshot(
  rawTasks: Record<string, unknown>[],
  projectId: string,
): { tasks: TaskDisplay[]; stats: ProjectStats['stats'] } {
  const idToTitle = new Map<string, string>();
  for (const rawTask of rawTasks) {
    const id = asString(rawTask.id);
    const title = asString(rawTask.title);
    if (id && title) {
      idToTitle.set(id, title);
    }
  }

  const dependencyMap = buildDependencyMap(rawTasks);
  const dependentsMap = computeDependents(rawTasks, dependencyMap);

  const tasks = rawTasks.map((rawTask) =>
    normalizeTask(rawTask, idToTitle, dependentsMap, dependencyMap, projectId)
  );

  return { tasks, stats: normalizeSnapshotStats(undefined, tasks) };
}

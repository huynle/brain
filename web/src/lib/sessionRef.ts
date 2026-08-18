// Task → session-address resolution.
//
// Live path: the task's running OpenCode instance from the instance
// registry (mirrors the goal steerer's lookup: kind "task", matching
// task id, not exited, last discovered session wins).
//
// Completed path: the `sessions` metadata the runner stamps on every
// task at discovery — entries accumulate across resume/retry and may
// point at different runners, so callers get the full newest-first list
// and treat "newest" only as the default.
//
// This module is pure so it stays unit-testable without pulling in
// zustand or react.

import type { OpencodeInstance, SessionRef, Task } from "./types";

/** The live task instance hosting this task, if any. */
export function findTaskInstance(
  task: Pick<Task, "id" | "projectId">,
  instances: readonly OpencodeInstance[],
): OpencodeInstance | undefined {
  return instances.find(
    (inst) =>
      inst.kind === "task" &&
      inst.task_id === task.id &&
      inst.status !== "exited" &&
      (!inst.project_id || !task.projectId || inst.project_id === task.projectId),
  );
}

/** Live session ref for a running task, if its instance is up. */
export function liveSessionRef(
  task: Pick<Task, "id" | "projectId">,
  instances: readonly OpencodeInstance[],
): SessionRef | undefined {
  const inst = findTaskInstance(task, instances);
  if (!inst) return undefined;
  const sessionIds = inst.session_ids || [];
  return {
    mode: "live",
    runner_id: inst.runner_id,
    instance_id: inst.instance_id,
    // Most recent discovered session wins; may be absent early in the
    // instance's life (discovery lag) — the surface shows "starting".
    session_id: sessionIds.length > 0 ? sessionIds[sessionIds.length - 1] : undefined,
  };
}

/**
 * History refs from the task's recorded sessions, newest first.
 * Entries without a runner_id are unaddressable (nothing can serve
 * their transcript) and are dropped.
 */
export function historySessionRefs(
  task: Pick<Task, "id" | "projectId" | "sessions">,
): SessionRef[] {
  const sessions = task.sessions || {};
  return Object.entries(sessions)
    .filter(([, info]) => !!info.runner_id)
    .sort(([, a], [, b]) => (b.timestamp || "").localeCompare(a.timestamp || ""))
    .map(([sid, info]) => ({
      mode: "history",
      runner_id: info.runner_id as string,
      session_id: sid,
      task_id: task.id,
      project_id: task.projectId,
      workdir: info.workdir,
    }));
}

/**
 * Default session ref for a task: the live instance when one is
 * running, else the newest recorded session, else undefined.
 */
export function resolveSessionRef(
  task: Pick<Task, "id" | "projectId" | "sessions">,
  instances: readonly OpencodeInstance[],
): SessionRef | undefined {
  return liveSessionRef(task, instances) ?? historySessionRefs(task)[0];
}

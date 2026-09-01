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

/**
 * Live session ref for one instance-registry row.
 *
 * THE single place a `{mode:"live", …}` ref is constructed — every
 * surface that addresses a running instance (task drill-downs, the
 * runner Processes tab, the full session view, docked leaves) goes
 * through here, so session selection has exactly one implementation.
 *
 * `pinnedSessionId` is the session the CALLER was already addressing —
 * a persisted leaf target or a history ref the user navigated from.
 * Pinning wins: a view that was opened on a specific session must not
 * silently jump to a newer one the instance discovers later. With
 * nothing pinned the newest discovered session wins.
 */
export function instanceSessionRef(
  inst: Pick<OpencodeInstance, "runner_id" | "instance_id" | "session_ids">,
  pinnedSessionId?: string,
): SessionRef {
  const sessionIds = inst.session_ids || [];
  const latest =
    sessionIds.length > 0 ? sessionIds[sessionIds.length - 1] : undefined;
  return {
    mode: "live",
    runner_id: inst.runner_id,
    instance_id: inst.instance_id,
    // May be absent early in the instance's life (discovery lag) — the
    // surface shows "starting".
    session_id: pinnedSessionId || latest,
  };
}

/**
 * Transcript ref for an instance row, honest about the process state:
 * live while the instance is up, history once it has exited (the
 * transcript outlives the process). Undefined when the instance exited
 * before any session was discovered — there is nothing to read.
 */
export function instanceTranscriptRef(
  inst: OpencodeInstance,
): SessionRef | undefined {
  if (inst.status !== "exited") return instanceSessionRef(inst);
  const sessionIds = inst.session_ids || [];
  const latest = sessionIds.length > 0 ? sessionIds[sessionIds.length - 1] : undefined;
  if (!latest) return undefined;
  return {
    mode: "history",
    runner_id: inst.runner_id,
    session_id: latest,
    task_id: inst.task_id,
    project_id: inst.project_id,
    workdir: inst.workdir,
  };
}

/** Live session ref for a running task, if its instance is up. */
export function liveSessionRef(
  task: Pick<Task, "id" | "projectId">,
  instances: readonly OpencodeInstance[],
): SessionRef | undefined {
  const inst = findTaskInstance(task, instances);
  return inst ? instanceSessionRef(inst) : undefined;
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

/**
 * Whether a session can be steered right now, and what to say when it
 * cannot.
 *
 * This is the rule the whole UI turns on — "can the user type into this
 * transcript" — so it lives here as a pure function rather than inside
 * the pane, next to the ref construction it reads. Three inputs, in the
 * order they disqualify:
 *
 *   • a HISTORY ref is a recording; its process is gone by definition.
 *   • a LIVE ref without a session id has nothing to address yet — the
 *     runner is still discovering it (the "starting" window).
 *   • delivery "ended" is the server telling us the instance exited
 *     while we were watching. The ref still says live; it isn't. Without
 *     this check the composer stays enabled over a dead process and
 *     every send fails.
 *
 * `hostNote` lets a caller that knows more (a registry row that already
 * reads "exited") explain it better than this function can.
 */
export function sessionSteerState(
  sref: SessionRef | undefined,
  delivery: "streaming" | "polling" | "ended" | "none",
  hostNote?: string,
): { canSteer: boolean; note: string } {
  const note = (fallback: string) => hostNote || fallback;
  if (!sref) return { canSteer: false, note: note("No session to show.") };
  if (sref.mode !== "live") {
    return {
      canSteer: false,
      note: note("This is a recorded transcript — the process is gone."),
    };
  }
  if (delivery === "ended") {
    return {
      canSteer: false,
      note: note("The session stream ended — the transcript is read-only."),
    };
  }
  if (!sref.session_id) {
    return {
      canSteer: false,
      note: note("Waiting for the session id before prompts can be delivered."),
    };
  }
  return { canSteer: true, note: "" };
}

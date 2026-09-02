/**
 * Run → openable work.
 *
 * A run audit records task IDS. Turning one into something a user can
 * look at walks two more hops, and each can be legitimately empty:
 *
 *   taskIds[0] ─┬─ absent      → the run generated nothing (a skip)
 *               └─ present ─┬─ not in the snapshot → deleted, or the
 *                           │   snapshot hasn't arrived yet
 *                           └─ task ─┬─ live instance  → live session
 *                                    ├─ recorded sessions → newest
 *                                    └─ none → script executor: log only
 *
 * Kept pure and separate from the row component because the difference
 * between those outcomes is the whole UX of the feature — a disabled
 * button that says why, versus one that silently does nothing — and it
 * cannot be exercised from live data on demand (it depends on a runner
 * having actually opened a session for a generated task).
 */
import { resolveSessionRef } from "./sessionRef";
import { outcomeLabel, type AutomationRun } from "./automationRuns";
import type { OpencodeInstance, SessionRef, Task } from "./types";

export interface RunTarget {
  /** The generated task, when it is in the project snapshot. */
  task?: Task;
  /** The session to open: live instance if one is up, else newest recorded. */
  sref?: SessionRef;
  /** Why there is nothing to open. Absent exactly when `sref` is set. */
  reason?: string;
}

export function resolveRunTarget(
  run: AutomationRun,
  taskById: ReadonlyMap<string, Task>,
  instances: readonly OpencodeInstance[],
  /** False before the project's task snapshot has arrived — "not loaded
   *  yet" must not be reported as "deleted". */
  snapshotLoaded: boolean,
): RunTarget {
  const taskId = run.taskIds[0];
  if (!taskId) {
    return { reason: `This run started no task — ${outcomeLabel(run)}.` };
  }
  const task = taskById.get(taskId);
  if (!task) {
    return {
      reason: snapshotLoaded
        ? `Task ${taskId} is no longer in this project's task list.`
        : "Waiting for this project's task snapshot.",
    };
  }
  const sref = resolveSessionRef(task, instances);
  if (!sref) {
    return {
      task,
      reason:
        "No session was recorded for this task — script-executor runs never open one. Its raw log is still there.",
    };
  }
  return { task, sref };
}

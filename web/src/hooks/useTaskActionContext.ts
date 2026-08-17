/**
 * useTaskActionContext — binds the pure task-action builders to real
 * effects (API calls, modal navigation, toasts).
 *
 * Kept separate from `lib/actions/taskActions` so the decision logic stays
 * unit-testable without mocking fetch or react, and separate from the
 * components so CardTasks, TaskModal and the command palette all drive the
 * identical set of effects.
 *
 * Mutating effects run through `withForceRetry`: the server's 409
 * live-claim refusal becomes a second confirmation quoting the server's
 * message, and an accepted confirmation retries once with force=true.
 *
 * Note there is no cache invalidation here. Task mutations reach the UI
 * through SSE: the server's `entry.*` events drive a fresh `tasks_snapshot`
 * (see internal/realtime/bridge.go), which the live store applies. Adding
 * a manual refetch would race that and produce a visible flicker.
 */
import { useMemo } from "react";

import { useModal } from "../store/modal";
import { useWorkspace } from "../store/workspace";
import { useUI } from "../store/ui";
import {
  controlAbortTask,
  deleteEntry,
  getDispatchLease,
  runOrTriggerTask,
  setTaskStatus,
  summarizeTriggerResults,
} from "../lib/api";
import {
  knownRunnerId,
  STATUS_LABELS,
  type TaskActionContext,
} from "../lib/actions/taskActions";
import { withForceRetry } from "../lib/actions/forceRetry";
import { forceConfirmFor } from "../lib/actions/forceConfirm";
import type { Task, TaskStatus } from "../lib/types";

/**
 * Factory form, for callers that span several projects at once (the
 * command palette). A hook cannot be called per project inside a loop, so
 * the project id becomes an argument instead.
 */
export function useTaskActionContextFactory(): (
  projectId: string,
) => TaskActionContext {
  const openModal = useModal((s) => s.open);
  const closeModal = useModal((s) => s.close);
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const toast = useUI((s) => s.toast);

  return useMemo(
    () => (projectId: string) => ({
      runTask: async (task: Task) => {
        const r = await withForceRetry(
          (force) => runOrTriggerTask(projectId, task.id, force),
          forceConfirmFor({
            title: "Runner conflict — force dispatch?",
            body: "Force pushes a fresh dispatch even though a runner already holds a lease on this task.",
            confirmLabel: "Force dispatch",
            danger: true,
          }),
          // /run reports lease conflicts in-band (200 + already_leased),
          // never as a 409 — force releases the stale lease and
          // re-dispatches (RunTaskNow's force path).
          (res) =>
            !res.triggered && res.reasonCode === "already_leased"
              ? res.reason ??
                "Task already holds a dispatch lease; force releases it and re-dispatches."
              : null,
        );
        const { message, kind } = summarizeTriggerResults([r]);
        toast(message, kind);
      },

      setStatus: async (task: Task, status: TaskStatus) => {
        await setTaskStatus(task, status);
        toast(
          `${task.title || task.id} → ${STATUS_LABELS[status] ?? status}`,
          "success",
        );
      },

      deleteTask: async (task: Task) => {
        await withForceRetry(
          (force) => deleteEntry(task.path, force),
          forceConfirmFor({
            title: "Runner online — force delete?",
            body:
              `Force delete removes "${task.title || task.id}" even though a runner ` +
              "appears to be executing it. This cannot be undone.",
            confirmLabel: "Force delete",
            danger: true,
          }),
        );
        // Close whatever modal was showing this task; leaving a detail
        // view open on a deleted entry produces a confusing "not found".
        closeModal();
        toast(`Deleted ${task.title || task.id}`, "success");
      },

      abortTask: async (task: Task) => {
        // The lease endpoint is authoritative and fresh; the enriched
        // snapshot (dispatch_lease / session runner) covers the case where
        // the lease record has already been cleaned up mid-flight.
        let runnerId: string | undefined;
        try {
          const lease = await getDispatchLease(projectId, task.id);
          runnerId = lease?.assigned_runner_id;
        } catch {
          // Lease lookup failing is not fatal — fall through to the
          // snapshot-derived runner below.
        }
        runnerId = runnerId || knownRunnerId(task);
        if (!runnerId) {
          throw new Error(
            "No runner is known to hold this task — it may have just finished",
          );
        }
        await controlAbortTask(runnerId, task.id);
        toast(
          `Abort signal sent to ${runnerId} for ${task.title || task.id}`,
          "success",
        );
      },

      openResume: (task: Task) =>
        openModal("task-actions", { projectId, taskId: task.id }),
      openStatusPicker: (task: Task) =>
        openModal("task-status", { projectId, taskId: task.id }),
      openMetadata: (task: Task) =>
        openModal("task-metadata", { projectId, taskId: task.id }),
      openGoalCreate: (task: Task) =>
        // Feature scope rides along when the task has one, so the goal
        // stays listed under its feature; task_id still narrows the goal.
        openModal("goal-create", {
          project: projectId,
          taskId: task.id,
          ...(task.feature_id ? { featureId: task.feature_id } : {}),
        }),

      openDetails: (task: Task) => {
        closeModal();
        openInFocus(
          "task-detail",
          { projectId, taskId: task.id },
          task.title || task.id,
        );
      },
      openLogs: (task: Task) => {
        closeModal();
        openInFocus(
          "logs",
          { projectId, taskId: task.id },
          `Logs ${task.id.slice(0, 8)}`,
        );
      },
    }),
    [openModal, closeModal, openInFocus, toast],
  );
}

export function useTaskActionContext(projectId: string): TaskActionContext {
  const factory = useTaskActionContextFactory();
  return useMemo(() => factory(projectId), [factory, projectId]);
}

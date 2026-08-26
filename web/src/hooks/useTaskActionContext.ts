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
import { useSelection } from "../store/selection";
import { useWorkspace } from "../store/workspace";
import { useUI } from "../store/ui";
import {
  controlAbortTask,
  controlSpawnInstance,
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
import { liveSessionRef } from "../lib/sessionRef";
import { forceDispatchNote, isAutomationTask, withForceNote } from "../lib/pause";
import { usePauseState } from "./usePauseState";
import { useSessions } from "./useSessions";
import type { SessionRef, Task, TaskStatus } from "../lib/types";

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
  const openSessionRef = useWorkspace((s) => s.openSessionRef);
  const setSteerIntent = useWorkspace((s) => s.setSteerIntent);
  const toast = useUI((s) => s.toast);
  // Instance registry for the live-session query. Poll-backed (8s); the
  // context memo refreshes with it so verb gates track instance state.
  const { allInstances } = useSessions();
  // Pause dials, so a manual run against a paused system can say what it
  // did to the pause rather than looking like pause was ignored.
  const { pause } = usePauseState();

  return useMemo(
    () => (projectId: string) => ({
      // The row's primary Open: the task modal. Distinct from openDetails
      // below, which opens the focus pane rather than the modal.
      openModal: (task: Task) =>
        openModal("task", { projectId, taskId: task.id }),

      // getState() (not a hook subscription): builders run on every row
      // render, and the row components subscribe to the selection store
      // themselves — the label stays fresh without a stale closure.
      toggleSelect: (task: Task) =>
        useSelection.getState().toggleTask(projectId, task.id),
      isSelected: (task: Task) => {
        const s = useSelection.getState();
        return s.projectId === projectId && s.taskIds.has(task.id);
      },

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
        // Run deliberately bypasses the project pause dials (see
        // SchedulerService.RunTaskNow, which skips shouldSkipTask on
        // purpose). Naming that keeps an intended override from reading as
        // a bug — and names the runner dial, which force cannot cross.
        const note = forceDispatchNote(pause, {
          projectId,
          automation: isAutomationTask(task),
        });
        toast(withForceNote(message, note), kind);
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

      liveSessionRef: (task: Task) =>
        liveSessionRef({ id: task.id, projectId }, allInstances),
      openSession: (_task: Task, ref: SessionRef) => {
        closeModal();
        openSessionRef(ref);
      },
      openTranscript: (_task: Task, ref: SessionRef) => {
        closeModal();
        openSessionRef(ref);
      },
      openSteer: (_task: Task, ref: SessionRef) => {
        closeModal();
        setSteerIntent(true);
        openSessionRef(ref);
      },

      continueSession: async (task: Task, ref: SessionRef) => {
        if (ref.mode !== "history") return;
        const title = `continue: ${task.title || task.id}`;
        const spawn = (workdir: string) =>
          controlSpawnInstance(ref.runner_id, { workdir, title });

        const primary = ref.workdir ?? task.workdir ?? "";
        let instance;
        try {
          instance = (await spawn(primary)).instance;
        } catch (err) {
          // Worktree-mode workdirs are force-removed at feature checkout —
          // the common case for merged tasks, so fall back to the task's
          // own configured workdir when the recorded one is gone.
          const fallback =
            task.workdir && task.workdir !== primary ? task.workdir : undefined;
          const msg = String((err as Error)?.message ?? err);
          if (fallback && /workdir/i.test(msg)) {
            instance = (await spawn(fallback)).instance;
            toast(
              `Original worktree was removed — continued from ${fallback} instead; ` +
                "file references in the old session may not resolve.",
              "warning",
            );
          } else {
            throw err;
          }
        }
        closeModal();
        openSessionRef({
          mode: "live",
          runner_id: ref.runner_id,
          instance_id: instance.instance_id,
          session_id: ref.session_id,
        });
        toast("Session reopened — send a message to continue.", "success");
      },
    }),
    [
      openModal,
      closeModal,
      openInFocus,
      openSessionRef,
      setSteerIntent,
      toast,
      allInstances,
      pause,
    ],
  );
}

export function useTaskActionContext(projectId: string): TaskActionContext {
  const factory = useTaskActionContextFactory();
  return useMemo(() => factory(projectId), [factory, projectId]);
}

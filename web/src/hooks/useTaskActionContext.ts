/**
 * useTaskActionContext — binds the pure task-action builders to real
 * effects (API calls, modal navigation, toasts).
 *
 * Kept separate from `lib/actions/taskActions` so the decision logic stays
 * unit-testable without mocking fetch or react, and separate from the
 * components so CardTasks, TaskModal and the command palette all drive the
 * identical set of effects.
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
  deleteEntry,
  runOrTriggerTask,
  setTaskStatus,
  summarizeTriggerResults,
} from "../lib/api";
import { STATUS_LABELS, type TaskActionContext } from "../lib/actions/taskActions";
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
        const r = await runOrTriggerTask(projectId, task.id, false);
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
        await deleteEntry(task.path);
        // Close whatever modal was showing this task; leaving a detail
        // view open on a deleted entry produces a confusing "not found".
        closeModal();
        toast(`Deleted ${task.title || task.id}`, "success");
      },

      openResume: (task: Task) =>
        openModal("task-actions", { projectId, taskId: task.id }),
      openStatusPicker: (task: Task) =>
        openModal("task-status", { projectId, taskId: task.id }),
      openMetadata: (task: Task) =>
        openModal("task-metadata", { projectId, taskId: task.id }),

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

import { useCallback } from "react";

import { snoozeUntil, type UseAttentionResult } from "./useAttention";
import type { Attention } from "../lib/types";
import { useWorkspace } from "../store/workspace";
import { useUI } from "../store/ui";
import type { AttentionRowActions } from "../components/Attention/AttentionRows";

/** Whether an attention item links to something we can open. */
export function attentionHasLink(item: Attention): boolean {
  return Boolean(
    item.feature_id ||
      item.task_id ||
      (item.session_id && item.runner_id),
  );
}

export function useAttentionRowActions(
  attention: Pick<
    UseAttentionResult,
    "markRead" | "resolve" | "dismiss" | "snooze"
  >,
): AttentionRowActions {
  const openInFocus = useWorkspace((state) => state.openInFocus);
  const openSessionRef = useWorkspace((state) => state.openSessionRef);
  const toast = useUI((state) => state.toast);

  const run = useCallback(
    async (label: string, action: () => Promise<void>) => {
      try {
        await action();
      } catch (error) {
        toast(
          `${label} failed: ${error instanceof Error ? error.message : String(error)}`,
          "error",
        );
      }
    },
    [toast],
  );

  const open = useCallback(
    (item: Attention) => {
      // Prefer the most specific link: a feature, then a task, then a
      // session. This matches how the reminder row opens its generated task.
      if (item.feature_id) {
        openInFocus(
          "feature-detail",
          { projectId: item.project, featureId: item.feature_id },
          item.feature_id,
        );
        return;
      }
      if (item.task_id) {
        openInFocus(
          "task-detail",
          { projectId: item.project, taskId: item.task_id },
          item.task_id,
        );
        return;
      }
      if (item.session_id && item.runner_id) {
        openSessionRef({
          mode: "history",
          runner_id: item.runner_id,
          session_id: item.session_id,
          task_id: item.task_id,
          project_id: item.project,
        });
      }
    },
    [openInFocus, openSessionRef],
  );

  return {
    onRead: (item: Attention) => {
      void run("Mark read", () => attention.markRead(item.id));
    },
    onResolve: (item: Attention) => {
      void run("Resolve", () => attention.resolve(item.id));
    },
    onDismiss: (item: Attention) => {
      void run("Dismiss", () => attention.dismiss(item.id));
    },
    onSnooze: (item: Attention) => {
      void run("Snooze", () => attention.snooze(item.id, snoozeUntil(60)));
    },
    onOpen: open,
    hasLink: attentionHasLink,
  };
}

/**
 * useAutomationActionContext — binds the pure automation-action
 * builders to real effects (API calls, modal navigation, toasts).
 *
 * Automations do not ride SSE — every mutating effect invalidates the
 * ["v2", "automations", project] query (see hooks/useAutomations) so
 * the 20s poll doesn't leave a stale row on screen after the user
 * just acted on it.
 *
 * Per-project factory like the task/feature contexts: executeAutomation
 * needs the project id alongside the entry path.
 */
import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { useModal } from "../store/modal";
import { useUI } from "../store/ui";
import { deleteEntry, executeAutomation, updateEntry } from "../lib/api";
import {
  automationName,
  type AutomationActionContext,
} from "../lib/actions/automationActions";
import type { BrainEntry } from "../lib/types";

export function useAutomationActionContext(
  projectId: string,
): AutomationActionContext {
  const openModal = useModal((s) => s.open);
  const closeModal = useModal((s) => s.close);
  const toast = useUI((s) => s.toast);
  const queryClient = useQueryClient();

  return useMemo(() => {
    const invalidate = () =>
      void queryClient.invalidateQueries({
        queryKey: ["v2", "automations", projectId],
      });

    return {
      runAutomation: async (a: BrainEntry) => {
        // executeAutomation expects the entry path (e.g.
        // "projects/x/automation/y.md"), not the short id.
        await executeAutomation(a.path, projectId);
        invalidate();
        toast(`Ran ${automationName(a)}`, "success");
      },

      enableAutomation: async (a: BrainEntry) => {
        await updateEntry(a.path, { status: "active" });
        invalidate();
        toast(`Enabled ${automationName(a)}`, "success");
      },

      pauseAutomation: async (a: BrainEntry) => {
        await updateEntry(a.path, { status: "archived" });
        invalidate();
        toast(`Paused ${automationName(a)}`, "success");
      },

      deleteAutomation: async (a: BrainEntry) => {
        await deleteEntry(a.path);
        invalidate();
        // Close whatever modal was showing this automation; leaving a
        // detail view open on a deleted entry shows "not found".
        closeModal();
        toast(`Deleted ${automationName(a)}`, "success");
      },

      openDetails: (a: BrainEntry) =>
        openModal("automation", { projectId, automationId: a.id }),
    };
  }, [projectId, openModal, closeModal, toast, queryClient]);
}

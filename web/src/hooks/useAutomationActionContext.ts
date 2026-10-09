import { reportBackgroundResult } from "../store/backgroundOperations";
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
import { useWorkspace } from "../store/workspace";
import {
  createEntry,
  deleteEntry,
  executeAutomation,
  updateEntry,
  type CreateEntryRequest,
} from "../lib/api";
import {
  automationName,
  type AutomationActionContext,
} from "../lib/actions/automationActions";
import {
  OPT_IN_STATUS,
  OPT_OUT_STATUS,
  bindingCreateBody,
  bindingPatchBody,
  bindingStatusPatch,
  emptyOverrides,
  planTurnOff,
  planTurnOn,
  type BindingPlan,
} from "../lib/automationBindings";
import type { BrainEntry } from "../lib/types";

/**
 * Carry out a binding plan from planTurnOn/planTurnOff. The parent is never
 * written: a plan names a binding path, or creates a binding of the parent.
 */
async function applyBindingPlan(
  parent: BrainEntry,
  project: string,
  plan: BindingPlan,
): Promise<void> {
  switch (plan.kind) {
    case "none":
      return;
    case "create":
      await createEntry(
        bindingCreateBody(parent, project, emptyOverrides(), plan.status) as CreateEntryRequest,
      );
      return;
    case "patch":
      await updateEntry(plan.path, bindingStatusPatch(plan.status));
      return;
    case "delete":
      await deleteEntry(plan.path);
      return;
  }
}

export function useAutomationActionContext(
  projectId: string,
): AutomationActionContext {
  const closeModal = useModal((s) => s.close);
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const openOrReuseInSidebar = useWorkspace((s) => s.openOrReuseInSidebar);
  const toast = useUI((s) => s.toast);
  const queryClient = useQueryClient();

  return useMemo(() => {
    const invalidate = () =>
      void queryClient.invalidateQueries({
        queryKey: ["v2", "automations", projectId],
      });

    // A manual run writes a fresh audit immediately. Without this the
    // run list the user is looking at keeps showing the pre-run history
    // for up to 30s — which reads as "Run now did nothing".
    const invalidateRuns = () =>
      void queryClient.invalidateQueries({
        queryKey: ["v2", "automation-runs", projectId],
      });

    // The effective view reads the binding, so every binding write makes it stale.
    const invalidateEffective = () =>
      void queryClient.invalidateQueries({
        queryKey: ["v2", "automation-effective", projectId],
      });

    return {
      runAutomation: async (a: BrainEntry) => {
        // executeAutomation expects the entry path (e.g.
        // "projects/x/automation/y.md"), not the short id.
        await executeAutomation(a.path, projectId);
        invalidate();
        invalidateRuns();
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

      // Project scope. These write this project's binding only; the global
      // entry is never archived from a project tab.
      turnOffHere: async (a, scope) => {
        await applyBindingPlan(a, scope.projectId, planTurnOff(scope.binding));
        invalidate();
        invalidateEffective();
        toast(`Turned ${automationName(a)} off here`, "success");
      },

      turnOnHere: async (a, scope) => {
        await applyBindingPlan(a, scope.projectId, planTurnOn(scope.binding));
        invalidate();
        invalidateEffective();
        toast(`Turned ${automationName(a)} on here`, "success");
      },

      saveOverrides: async (a, scope, overrides) => {
        if (scope.binding) {
          await updateEntry(
            scope.binding.path,
            bindingPatchBody(overrides, scope.binding.status),
          );
        } else {
          // A new binding keeps the project's current state: a project the
          // filter does not select starts opted out, so saving never opts it in.
          const status = scope.targeted ? OPT_IN_STATUS : OPT_OUT_STATUS;
          await createEntry(
            bindingCreateBody(a, scope.projectId, overrides, status) as CreateEntryRequest,
          );
        }
        invalidate();
        invalidateEffective();
      },

      deleteAutomation: async (a: BrainEntry) => {
        const originalModal = useModal.getState().target;
        await deleteEntry(a.path);
        invalidate();
        // Close whatever modal was showing this automation; leaving a
        // detail view open on a deleted entry shows "not found".
        if (useModal.getState().target === originalModal) closeModal();
        reportBackgroundResult(`Deleted ${automationName(a)}`);
        toast(`Deleted ${automationName(a)}`, "success");
      },

      // Details and history are the SAME surface now: the docked view
      // leads with the run list and folds the config away, so there is
      // nothing left for a second destination to show.
      openDetails: (a: BrainEntry) =>
        openOrReuseInSidebar(
          "automation-detail",
          { projectId, automationId: a.id },
          automationName(a),
        ),

      openHistory: (a: BrainEntry) =>
        openOrReuseInSidebar(
          "automation-detail",
          { projectId, automationId: a.id },
          automationName(a),
        ),

      openRunsPane: (a: BrainEntry) => {
        closeModal();
        openInFocus(
          "automation-runs",
          { projectId, automationId: a.id },
          `${automationName(a)} runs`,
        );
      },
    };
  }, [
    projectId,
    closeModal,
    openInFocus,
    openOrReuseInSidebar,
    toast,
    queryClient,
  ]);
}

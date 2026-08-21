/**
 * useRunnerActionContext — binds the pure runner-action builders to
 * real effects (API calls, modal navigation, toasts).
 *
 * Clear-assignments carries the optimistic feature→runner map from
 * the workspace store through the same unassign/rollback dance the
 * sidebar's drag-and-drop uses, so a clear that fails puts the chip
 * back instead of silently lying. Partial failures toast an aggregate
 * rather than failing the batch — mirroring the feature-resume API's
 * philosophy of reporting skips instead of aborting.
 *
 * Runner rows ride the runner SSE stream, but shutdown still
 * invalidates the ["v2", "runners"] REST fallback so a reconnecting
 * client doesn't repaint a runner the user just stopped.
 */
import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { useModal } from "../store/modal";
import { useUI } from "../store/ui";
import { useWorkspace } from "../store/workspace";
import {
  clearFeatureAssignment,
  shutdownRunner as apiShutdownRunner,
} from "../lib/api";
import {
  combineRunnerAssignments,
  type RunnerActionContext,
} from "../lib/actions/runnerActions";
import type { RunnerInfo } from "../lib/types";

export function useRunnerActionContext(): RunnerActionContext {
  const openModal = useModal((s) => s.open);
  const toast = useUI((s) => s.toast);
  const featureAssignments = useWorkspace((s) => s.featureAssignments);
  const assignFeature = useWorkspace((s) => s.assignFeature);
  const unassignFeature = useWorkspace((s) => s.unassignFeature);
  const queryClient = useQueryClient();

  return useMemo(
    () => ({
      openShell: (r: RunnerInfo) =>
        openModal("runner", { id: r.runner_id }, "shell"),

      openDetails: (r: RunnerInfo) =>
        openModal("runner", { id: r.runner_id }, "overview"),

      openProcesses: (r: RunnerInfo) =>
        openModal("runner", { id: r.runner_id }, "processes"),

      clearAssignments: async (r: RunnerInfo) => {
        const assignments = combineRunnerAssignments(r, featureAssignments);
        if (assignments.length === 0) return;
        // Assignments without a project id cannot be cleared — the
        // clear endpoint is project-scoped — so they are skipped
        // loudly rather than dropped from the count silently.
        const clearsWithProject = assignments.filter((a) => a.projectId);
        const clearsMissingProject = assignments.filter((a) => !a.projectId);
        if (clearsMissingProject.length > 0) {
          toast(
            `Skipped ${clearsMissingProject.length} assignments with unknown project`,
            "info",
          );
        }
        const results = await Promise.allSettled(
          clearsWithProject.map((a) => {
            unassignFeature(a.featureId);
            return clearFeatureAssignment(
              a.projectId as string,
              a.featureId,
            ).then(
              () => a,
              (err) => {
                assignFeature(a.featureId, r.runner_id);
                throw err;
              },
            );
          }),
        );
        const failed = results.filter(
          (res) => res.status === "rejected",
        ).length;
        if (failed > 0) {
          toast(
            `Cleared ${results.length - failed}/${results.length}; ${failed} failed`,
            "error",
          );
        }
      },

      shutdownRunner: async (r: RunnerInfo) => {
        await apiShutdownRunner(r.runner_id, "manual");
        void queryClient.invalidateQueries({ queryKey: ["v2", "runners"] });
        // The command is delivered asynchronously over SSE — the toast
        // reports the request landed, not that the process is gone.
        toast(`Shutdown requested for ${r.runner_id}`, "success");
      },
    }),
    [
      openModal,
      toast,
      featureAssignments,
      assignFeature,
      unassignFeature,
      queryClient,
    ],
  );
}

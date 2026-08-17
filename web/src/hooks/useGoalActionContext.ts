/**
 * useGoalActionContext — binds the pure goal-action builders to real
 * effects (API calls, modal navigation, toasts).
 *
 * Unlike tasks, goal mutations do NOT reach the UI through SSE — every
 * mutating effect here invalidates the ["goals"] query prefix (list,
 * per-goal progress, per-goal audit) so the 30s poll doesn't leave a
 * stale row on screen after the user just acted on it.
 *
 * A GoalSummary carries its own project, so unlike the task/feature
 * contexts there is no per-project factory: one context serves every
 * goal on every surface (card rows, modal footer, command palette).
 */
import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { useModal } from "../store/modal";
import { useUI } from "../store/ui";
import {
  archiveGoal as apiArchiveGoal,
  deleteGoal as apiDeleteGoal,
  pauseGoal as apiPauseGoal,
  resumeGoal as apiResumeGoal,
  runGoal as apiRunGoal,
} from "../lib/api";
import {
  summarizeGoalRun,
  type GoalActionContext,
} from "../lib/actions/goalActions";
import type { GoalSummary } from "../lib/types";

export function useGoalActionContext(): GoalActionContext {
  const openModal = useModal((s) => s.open);
  const closeModal = useModal((s) => s.close);
  const toast = useUI((s) => s.toast);
  const queryClient = useQueryClient();

  return useMemo(() => {
    const invalidate = () =>
      void queryClient.invalidateQueries({ queryKey: ["goals"] });
    const name = (goal: GoalSummary) => goal.title || goal.goal_id;

    return {
      runGoal: async (goal: GoalSummary) => {
        const audit = await apiRunGoal(goal.goal_id);
        invalidate();
        const { message, kind } = summarizeGoalRun(audit);
        toast(`${name(goal)}: ${message}`, kind);
      },

      pauseGoal: async (goal: GoalSummary) => {
        await apiPauseGoal(goal.goal_id);
        invalidate();
        toast(`Paused ${name(goal)}`, "success");
      },

      resumeGoal: async (goal: GoalSummary) => {
        await apiResumeGoal(goal.goal_id);
        invalidate();
        toast(`Resumed ${name(goal)}`, "success");
      },

      archiveGoal: async (goal: GoalSummary) => {
        await apiArchiveGoal(goal.goal_id);
        invalidate();
        toast(`Archived ${name(goal)}`, "success");
      },

      deleteGoal: async (goal: GoalSummary) => {
        await apiDeleteGoal(goal.goal_id);
        invalidate();
        // Close whatever modal was showing this goal; leaving a detail
        // view open on a deleted entry produces a confusing "not found".
        closeModal();
        toast(`Deleted ${name(goal)}`, "success");
      },

      openEdit: (goal: GoalSummary) =>
        openModal(
          "goal",
          { goalId: goal.goal_id, projectId: goal.project },
          "edit",
        ),
      openDetails: (goal: GoalSummary) =>
        openModal("goal", { goalId: goal.goal_id, projectId: goal.project }),
    };
  }, [openModal, closeModal, toast, queryClient]);
}

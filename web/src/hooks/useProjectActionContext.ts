/**
 * useProjectActionContext — binds the pure project-action builders to
 * real effects (API calls, focus-pane navigation, toasts).
 *
 * ProjectCard and the sidebar's ProjectsSection both render the project
 * verb list and previously hand-rolled the same three effects inline —
 * the drift lib/actions exists to prevent, one level up. With two pause
 * dials added the duplication stopped being harmless, so the effects
 * live here once.
 *
 * The dials do not ride SSE, so every mutating effect invalidates
 * ["v2", "runner-status"] (see useRunnerStatus) after writing.
 */
import { useMemo } from "react";

import { useUI } from "../store/ui";
import { useWorkspace } from "../store/workspace";
import {
  pauseAutomations as apiPauseAutomations,
  pauseProject as apiPauseProject,
  resumeAutomations as apiResumeAutomations,
  resumeProject as apiResumeProject,
  runProject,
  summarizeRunProjectResult,
} from "../lib/api";
import type { ProjectActionContext } from "../lib/actions/projectActions";
import { useInvalidateRunnerStatus } from "./useRunnerStatus";

export function useProjectActionContext(): ProjectActionContext {
  const toast = useUI((s) => s.toast);
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const hideProject = useWorkspace((s) => s.hideProject);
  const invalidateStatus = useInvalidateRunnerStatus();

  return useMemo(
    () => ({
      runProject: async (pid: string) => {
        const r = await runProject(pid, false);
        toast(
          summarizeRunProjectResult(r),
          r.totalTasksDispatched > 0 ? "success" : "info",
        );
      },

      openTaskList: (pid: string) =>
        openInFocus("task-detail", { projectId: pid }, pid),

      hideProject: (pid: string) => hideProject(pid),

      // ─── the two project dials ──────────────────────────────────
      // Toasts describe the actual effect: pause holds back NEW
      // dispatch, it does not interrupt a task already running, and
      // "Run now" / "Run feature now" still force-dispatch past it —
      // which is the whole point of pausing to isolate one feature.
      pauseProject: async (pid: string) => {
        await apiPauseProject(pid);
        invalidateStatus();
        toast(`${pid} paused — no new task dispatch`, "success");
      },

      resumeProject: async (pid: string) => {
        await apiResumeProject(pid);
        invalidateStatus();
        toast(`${pid} resumed`, "success");
      },

      pauseAutomations: async (pid: string) => {
        await apiPauseAutomations(pid);
        invalidateStatus();
        toast(`${pid} automations paused`, "success");
      },

      resumeAutomations: async (pid: string) => {
        await apiResumeAutomations(pid);
        invalidateStatus();
        toast(`${pid} automations resumed`, "success");
      },
    }),
    [toast, openInFocus, hideProject, invalidateStatus],
  );
}

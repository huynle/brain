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
 *
 * Project deletion additionally runs through `withForceRetry`: the server
 * refuses (409) while an online runner is executing one of the project's
 * tasks, and that refusal has to be answerable rather than a dead end.
 */
import { useMemo } from "react";
import { useQueryClient } from "@tanstack/react-query";

import { useUI } from "../store/ui";
import { useWorkspace } from "../store/workspace";
import {
  deleteProject as apiDeleteProject,
  pauseAutomations as apiPauseAutomations,
  pauseProject as apiPauseProject,
  resumeAutomations as apiResumeAutomations,
  resumeProject as apiResumeProject,
  runProject,
  summarizeRunProjectResult,
} from "../lib/api";
import {
  summarizeDeleteProjectResult,
  type ProjectActionContext,
} from "../lib/actions/projectActions";
import { forceConfirmFor } from "../lib/actions/forceConfirm";
import { withForceRetry } from "../lib/actions/forceRetry";
import { useInvalidateRunnerStatus } from "./useRunnerStatus";

export function useProjectActionContext(): ProjectActionContext {
  const toast = useUI((s) => s.toast);
  const openInFocus = useWorkspace((s) => s.openInFocus);
  const hideProject = useWorkspace((s) => s.hideProject);
  const forgetProject = useWorkspace((s) => s.forgetProject);
  const invalidateStatus = useInvalidateRunnerStatus();
  const queryClient = useQueryClient();

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

      // ─── destroy ────────────────────────────────────────────────
      // Four things have to be dropped after the wipe, and skipping
      // any one of them leaves the ghost of the project on screen:
      //
      //   ["projects"]  the sidebar's list — refetches on a 60s timer,
      //                 so without this the dead name lingers a minute
      //   ["entries"]   the Entries browser and the overview's "recently
      //                 updated" strip, which read entries directly and
      //                 would keep listing rows from a deleted project
      //   runner-status the pause dials, which are keyed by project id
      //                 and do not ride SSE
      //   workspace     any focus pane still pointed at the project
      //
      // The toast reports the server's own counts rather than assuming
      // success: a wipe is not transactional, and a partial one has to
      // read as a partial one.
      //
      // The 409 escalation keeps type-to-confirm on the SECOND pass as
      // well, matching feature delete. The first dialog asked "do you
      // mean this project"; the force dialog asks a different question —
      // "do you mean it while a runner is mid-task" — and answering it
      // with a bare click would make the first pass the only real gate.
      deleteProject: async (pid: string) => {
        const result = await withForceRetry(
          (force) => apiDeleteProject(pid, force ? { force: true } : {}),
          forceConfirmFor({
            title: "Runner online — force delete?",
            body:
              `Force erases ${pid} anyway; the runner's in-flight work will have ` +
              `nowhere to land. This cannot be undone.`,
            confirmLabel: "Force delete",
            danger: true,
            typeToConfirm: pid,
          }),
        );
        forgetProject(pid);
        await queryClient.invalidateQueries({ queryKey: ["projects"] });
        void queryClient.invalidateQueries({ queryKey: ["entries"] });
        invalidateStatus();
        toast(
          summarizeDeleteProjectResult(result),
          result.failed > 0 ? "error" : "success",
        );
      },
    }),
    [toast, openInFocus, hideProject, invalidateStatus, forgetProject, queryClient],
  );
}

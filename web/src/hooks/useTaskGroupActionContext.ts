import { submitBulkJob } from "../store/bulkJobs";
import { useMemo } from "react";

import { useUI } from "../store/ui";
import { useSelection } from "../store/selection";
import {
  runTask,
} from "../lib/api";
import type {
  TaskGroup,
  TaskGroupActionContext,
} from "../lib/actions/taskGroupActions";
import type { TaskStatus } from "../lib/types";

export interface UseTaskGroupActionContextOptions {
  /** Fold state lives in the workspace store, keyed by the group's key. */
  toggleCollapsed: (group: TaskGroup) => void;
}

export function useTaskGroupActionContext(
  projectId: string,
  opts: UseTaskGroupActionContextOptions,
): TaskGroupActionContext {
  const toast = useUI((s) => s.toast);
  const selectTasks = useSelection((s) => s.selectTasks);
  const { toggleCollapsed } = opts;

  return useMemo<TaskGroupActionContext>(
    () => ({
      toggleCollapsed,

      selectAll: (group) => {
        selectTasks(
          projectId,
          group.tasks.map((t) => t.id),
        );
        toast(
          `Selected ${group.tasks.length} task${group.tasks.length === 1 ? "" : "s"} in ${group.label}`,
          "info",
        );
      },

      // No endpoint dispatches an arbitrary task-id list — /run is
      // per-task and runProject iterates FEATURES, which by definition
      // skips this group. So this is an honest sequential fan-out, and
      // the toast reports what actually happened rather than assuming.
      runGroup: async (group) => {
        const runnable = group.tasks.filter((t) => t.status === "pending");
        if (runnable.length === 0) {
          toast("No pending tasks to dispatch", "warning");
          return;
        }
        let dispatched = 0;
        let skipped = 0;
        let firstReason = "";
        for (const t of runnable) {
          try {
            const r = await runTask(projectId, t.id);
            if (r.dispatched) dispatched++;
            else {
              skipped++;
              // The server's own words. `detail` is the sentence, `reason`
              // the code — either beats a generic "some failed".
              if (!firstReason) firstReason = r.detail || r.reason || "";
            }
          } catch (err) {
            skipped++;
            if (!firstReason) {
              firstReason = err instanceof Error ? err.message : String(err);
            }
          }
        }
        if (dispatched === 0) {
          toast(
            `${group.label}: nothing dispatched${firstReason ? ` — ${firstReason}` : ""}`,
            "warning",
          );
          return;
        }
        toast(
          `${group.label}: dispatched ${dispatched}` +
            (skipped > 0
              ? `, skipped ${skipped}${firstReason ? ` (${firstReason})` : ""}`
              : ""),
          skipped > 0 ? "warning" : "success",
        );
      },

      setStatusForAll: async (group, status: TaskStatus) => {
        await submitBulkJob({ operation: "set_status", status, paths: group.tasks.map(t => t.path) });
      },
      deleteGroup: async (group) => {
        await submitBulkJob({ operation: "delete", paths: group.tasks.map(t => t.path) });
      },
    }),
    [projectId, toast, selectTasks, toggleCollapsed],
  );
}

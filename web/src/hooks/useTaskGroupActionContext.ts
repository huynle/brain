/**
 * useTaskGroupActionContext — binds `buildTaskGroupActions` to real effects.
 *
 * Everything here addresses tasks by an EXPLICIT path list, never by a
 * bulk filter. See `lib/actions/taskGroupActions` for why: a group with no
 * `feature_id` cannot be named by any filter the server accepts, and the
 * empty-string attempt at one silently widens to the whole project.
 *
 * The explicit form is also why these effects are so much shorter than
 * their feature equivalents in `useFeatureActionContext`. A filter-mode
 * bulk call pages at 100 and can serve the SAME page again — the server
 * lists by modified DESC, so freshly-updated rows sort back to the front —
 * which is what the per-source-status baton exists to defeat. Disjoint
 * chunks of a path list cannot repeat, so one pass drains the group.
 *
 * The 100-entry cap still applies in explicit mode, and — unlike filter
 * mode — it is applied SILENTLY, with no `truncated` flag. `chunkPaths` is
 * therefore not an optimisation here; without it a 150-task group would
 * report success having touched 100.
 */
import { useMemo } from "react";

import { useUI } from "../store/ui";
import { useSelection } from "../store/selection";
import {
  bulkDeletePaths,
  bulkUpdateEntries,
  runTask,
} from "../lib/api";
import { chunkPaths } from "../lib/selection";
import { STATUS_LABELS } from "../lib/actions/taskActions";
import { forceConfirmFor } from "../lib/actions/forceConfirm";
import { withForceRetry } from "../lib/actions/forceRetry";
import type {
  TaskGroup,
  TaskGroupActionContext,
} from "../lib/actions/taskGroupActions";
import type { TaskStatus } from "../lib/types";

/** Tally of a chunked fan-out, so a partial result reads as partial. */
interface FanOut {
  ok: number;
  failed: number;
  firstError: string;
}

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
        // Skip rows already there. Not just an optimisation: it keeps the
        // reported count honest ("archived 3" when 3 changed, not 12) and
        // avoids re-writing files for no reason.
        const paths = group.tasks
          .filter((t) => t.status !== status)
          .map((t) => t.path);
        if (paths.length === 0) {
          toast(`Every task is already ${STATUS_LABELS[status] ?? status}`, "warning");
          return;
        }

        const commit = async (force: boolean): Promise<FanOut> => {
          const agg: FanOut = { ok: 0, failed: 0, firstError: "" };
          for (const chunk of chunkPaths(paths)) {
            const r = await bulkUpdateEntries(chunk, { status }, { force });
            agg.ok += r.updated;
            agg.failed += r.failed;
            if (!agg.firstError) {
              const bad = r.results?.find((row) => row.status !== "ok");
              if (bad) agg.firstError = bad.error ?? bad.title ?? bad.id;
            }
          }
          return agg;
        };

        const out = await withForceRetry(
          commit,
          forceConfirmFor({
            title: "Runner online — force update?",
            body:
              `Force applies "${STATUS_LABELS[status] ?? status}" even to tasks ` +
              `an online runner is still executing.`,
            confirmLabel: "Force update",
            danger: true,
          }),
        );

        const label = STATUS_LABELS[status] ?? status;
        toast(
          out.failed > 0
            ? `${group.label} → ${label}: ${out.ok} updated, ${out.failed} failed` +
                (out.firstError ? ` (${out.firstError})` : "")
            : `${group.label} → ${label}: ${out.ok} updated`,
          out.failed > 0 ? "warning" : "success",
        );
      },

      deleteGroup: async (group) => {
        const paths = group.tasks.map((t) => t.path);
        if (paths.length === 0) {
          toast("Nothing to delete", "warning");
          return;
        }

        const commit = async (force: boolean): Promise<FanOut> => {
          const agg: FanOut = { ok: 0, failed: 0, firstError: "" };
          for (const chunk of chunkPaths(paths)) {
            const r = await bulkDeletePaths(chunk, { force });
            agg.ok += r.deleted;
            agg.failed += r.failed;
            if (!agg.firstError) {
              const bad = r.results?.find((row) => row.status !== "ok");
              if (bad) agg.firstError = bad.error ?? bad.title ?? bad.id;
            }
          }
          return agg;
        };

        const out = await withForceRetry(
          commit,
          forceConfirmFor({
            title: "Runner online — force delete?",
            body:
              "Force delete removes the tasks anyway; the runner's in-flight " +
              "work will have nowhere to land. This cannot be undone.",
            confirmLabel: "Force delete",
            danger: true,
            // Same friction on the second pass as the first, matching
            // feature delete: the force dialog asks a different question
            // and must not be answerable with a bare click.
            typeToConfirm: group.label,
          }),
        );

        toast(
          out.failed > 0
            ? `${group.label}: deleted ${out.ok}, failed ${out.failed}` +
                (out.firstError ? ` (${out.firstError})` : "")
            : `${group.label}: deleted ${out.ok}`,
          out.failed > 0 ? "warning" : "success",
        );
      },
    }),
    [projectId, toast, selectTasks, toggleCollapsed],
  );
}

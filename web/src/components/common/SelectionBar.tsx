/**
 * SelectionBar — the floating command bar for multi-selected rows.
 *
 * Appears (fixed, bottom-center) whenever the selection store is
 * non-empty. Delete runs the same ladder every destructive verb uses:
 * dry-run preview → typed confirmation → commit with the 409 →
 * force-confirm recovery. Explicitly selected tasks delete by exact
 * path (chunked to the server's 100-entry cap — the server silently
 * clamps oversized path lists, so chunking here is correctness, not
 * politeness); selected features delete by feature filter with the
 * baton loop, so a >100-task feature still drains fully.
 */
import { useMemo, useState } from "react";

import { useLive } from "../../lib/sse";
import { useSelection } from "../../store/selection";
import { useUI } from "../../store/ui";
import {
  bulkDeletePaths,
  deleteFeatureTasks,
  type BulkDeleteResponse,
} from "../../lib/api";
import {
  buildDeletePlan,
  chunkPaths,
  describeSelection,
  type DeletePlan,
} from "../../lib/selection";
import { runBulkBaton } from "../../lib/actions/bulkBaton";
import { withForceRetry, ForceDeclinedError } from "../../lib/actions/forceRetry";
import { forceConfirmFor } from "../../lib/actions/forceConfirm";
import { ConfirmDialog } from "./ConfirmDialog";
import type { Task } from "../../lib/types";

const EMPTY_TASKS: readonly Task[] = Object.freeze([]);

interface PendingDelete {
  plan: DeletePlan;
  /** Total entries the dry-run says will go (tasks + feature members). */
  total: number;
  sampleTitles: string[];
}

export function SelectionBar(): JSX.Element | null {
  const projectId = useSelection((s) => s.projectId);
  const taskIds = useSelection((s) => s.taskIds);
  const featureIds = useSelection((s) => s.featureIds);
  const clear = useSelection((s) => s.clear);
  const toast = useUI((s) => s.toast);

  const tasks =
    useLive((s) => (projectId ? s.projects[projectId]?.tasks : undefined)) ??
    EMPTY_TASKS;

  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState<PendingDelete | null>(null);

  const count = taskIds.size + featureIds.size;
  const summary = useMemo(
    () => describeSelection(taskIds.size, featureIds.size),
    [taskIds, featureIds],
  );

  if (!projectId || count === 0) return null;

  const previewDelete = async () => {
    setBusy(true);
    try {
      const plan = buildDeletePlan(
        { projectId, taskIds, featureIds },
        tasks,
      );
      let total = 0;
      const sampleTitles = plan.taskTitles.slice(0, 5);
      for (const chunk of chunkPaths(plan.taskPaths)) {
        const r = await bulkDeletePaths(chunk, { dryRun: true });
        total += r.total;
      }
      for (const fid of plan.featureIds) {
        const r = await deleteFeatureTasks(projectId, fid, { dryRun: true });
        total += r.matched_total ?? r.total;
      }
      if (total === 0) {
        toast("Nothing to delete — selection is stale", "warning");
        clear();
        return;
      }
      if (plan.staleTaskIds.length > 0) {
        toast(
          `${plan.staleTaskIds.length} selected task(s) no longer exist and were skipped`,
          "info",
        );
      }
      setPending({ plan, total, sampleTitles });
    } catch (err) {
      toast(
        `Preview failed: ${err instanceof Error ? err.message : String(err)}`,
        "error",
      );
    } finally {
      setBusy(false);
    }
  };

  const commitDelete = async (pd: PendingDelete) => {
    const commit = async (force: boolean) => {
      let ok = 0;
      let failed = 0;
      const failedTitles: string[] = [];
      const absorb = (r: BulkDeleteResponse) => {
        ok += r.deleted;
        failed += r.failed;
        for (const row of r.results) {
          if (row.status !== "ok" && failedTitles.length < 3) {
            failedTitles.push(row.title || row.id);
          }
        }
      };
      for (const chunk of chunkPaths(pd.plan.taskPaths)) {
        absorb(await bulkDeletePaths(chunk, { force }));
      }
      for (const fid of pd.plan.featureIds) {
        const out = await runBulkBaton(
          () => deleteFeatureTasks(projectId, fid, { force }),
          (r: BulkDeleteResponse) => r.deleted,
        );
        ok += out.ok;
        failed += out.failed;
      }
      return { ok, failed, failedTitles };
    };

    const { ok, failed, failedTitles } = await withForceRetry(
      commit,
      forceConfirmFor({
        title: "Runner online — force delete?",
        body:
          "Some selected tasks are being executed by an online runner. " +
          "Force delete removes them anyway; in-flight work will have " +
          "nowhere to land. This cannot be undone.",
        confirmLabel: "Force delete",
        danger: true,
      }),
    );

    clear();
    if (failed > 0) {
      toast(
        `Deleted ${ok}, failed ${failed}${
          failedTitles.length > 0 ? ` (${failedTitles.join(", ")})` : ""
        }`,
        "warning",
      );
    } else {
      toast(`Deleted ${ok} entr${ok === 1 ? "y" : "ies"}`, "success");
    }
  };

  return (
    <>
      <div className="selection-bar" role="toolbar" aria-label="Selection">
        <span className="sel-count">{summary} selected</span>
        <button
          className="danger"
          disabled={busy}
          onClick={() => void previewDelete()}
        >
          {busy ? "Checking…" : "Delete"}
        </button>
        <button disabled={busy} onClick={clear}>
          Clear
        </button>
      </div>

      {pending && (
        <ConfirmDialog
          confirm={{
            title: `Delete ${pending.total} entr${pending.total === 1 ? "y" : "ies"}?`,
            body:
              `Deletes ${summary}` +
              `${pending.plan.featureIds.length > 0 ? " (feature selections delete all of their tasks)" : ""}.` +
              `${pending.sampleTitles.length > 0 ? ` Includes: ${pending.sampleTitles.join(", ")}${pending.plan.taskPaths.length > pending.sampleTitles.length ? ", …" : ""}.` : ""}` +
              " This cannot be undone.",
            confirmLabel: `Delete ${pending.total}`,
            // Same friction as single-feature delete: destructive fan-outs
            // require typing. A fixed word (not a name) because the target
            // is a heterogeneous selection.
            typeToConfirm: "delete",
          }}
          danger
          onCancel={() => setPending(null)}
          onConfirm={async () => {
            try {
              await commitDelete(pending);
              setPending(null);
            } catch (err) {
              if (err instanceof ForceDeclinedError) {
                // Declining the force escalation is a cancellation, not a
                // failure — close quietly, keep the selection for retry.
                setPending(null);
                toast(err.message, "info");
                return;
              }
              throw err; // ConfirmDialog renders it inline and stays open.
            }
          }}
        />
      )}
    </>
  );
}

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
 *
 * Archive is the reversible sibling: enabled only when every affected
 * task is already terminal, confirmed with plain counts (no typing),
 * committed as per-path status updates plus the per-source-status
 * feature baton.
 */
import { useEffect, useMemo, useRef, useState } from "react";

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
import { startArchive } from "../../store/archiveJob";
import { startBackgroundOperation, type ReportProgress } from "../../store/backgroundOperations";
import { TERMINAL_STATUSES } from "../../lib/actions/taskActions";
import { withForceRetry } from "../../lib/actions/forceRetry";
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

interface PendingArchive {
  /** Entry paths for explicitly selected, not-yet-archived tasks. */
  taskPaths: string[];
  /** Feature ids whose members archive via the per-status baton. */
  featureIds: string[];
  /** Client-side count of tasks the archive will actually flip. */
  total: number;
}

export function SelectionBar(): JSX.Element | null {
  const projectId = useSelection((s) => s.projectId);
  const taskIds = useSelection((s) => s.taskIds);
  const featureIds = useSelection((s) => s.featureIds);
  const clear = useSelection((s) => s.clear);
  const verbRequest = useSelection((s) => s.verbRequest);
  const consumeVerbRequest = useSelection((s) => s.consumeVerbRequest);
  const toast = useUI((s) => s.toast);

  const tasks =
    useLive((s) => (projectId ? s.projects[projectId]?.tasks : undefined)) ??
    EMPTY_TASKS;

  const [busy, setBusy] = useState(false);
  const [pending, setPending] = useState<PendingDelete | null>(null);
  const [pendingArchive, setPendingArchive] = useState<PendingArchive | null>(
    null,
  );

  const count = taskIds.size + featureIds.size;
  const summary = useMemo(
    () => describeSelection(taskIds.size, featureIds.size),
    [taskIds, featureIds],
  );

  // Archive gate: the verb only applies to settled work. Every task the
  // archive would touch — explicitly selected, or a member of a selected
  // feature — must already be terminal, or the button disables.
  const archiveGate = useMemo(() => {
    const affected = tasks.filter(
      (t) => taskIds.has(t.id) || (!!t.feature_id && featureIds.has(t.feature_id)),
    );
    const allSettled =
      affected.length > 0 &&
      affected.every((t) => TERMINAL_STATUSES.has(t.status));
    // Already-archived tasks pass the gate but need no write.
    const toArchive = affected.filter((t) => t.status !== "archived").length;
    return { allSettled, toArchive };
  }, [tasks, taskIds, featureIds]);

  // The selection context menu (lib/actions/selectionActions) posts
  // archive/delete requests into the store; this bar owns the preview
  // and confirm ladders, so it consumes them here. The ref carries the
  // current render's handlers across the early return below — the
  // request can only originate from a marked row, so the bar is
  // guaranteed to be mounted and rendered when one arrives.
  const verbHandlersRef = useRef<{
    archive: () => void;
    del: () => void;
  } | null>(null);
  useEffect(() => {
    if (!verbRequest) return;
    consumeVerbRequest();
    const h = verbHandlersRef.current;
    if (!h) return;
    if (verbRequest === "archive") h.archive();
    else h.del();
  }, [verbRequest, consumeVerbRequest]);

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

  const commitDelete = async (pd: PendingDelete, report: ReportProgress) => {
    const chunks = chunkPaths(pd.plan.taskPaths);
    let nextChunk = 0;
    let nextFeature = 0;
    let deleted = 0;
    let failures = 0;
    const titles: string[] = [];
    const absorb = (r: BulkDeleteResponse) => {
      deleted += r.deleted;
      failures += r.failed;
      report({ detail: `${deleted} deleted; ${failures} failed`, completed: deleted + failures, total: Math.max(pd.total, deleted + failures) });
      for (const row of r.results) {
        if (row.status !== "ok" && titles.length < 3) titles.push(row.title || row.id);
      }
    };
    // Keep cursors and counts across a force retry: already-deleted paths
    // must never be replayed, and completed feature pages stay counted.
    const commit = async (force: boolean) => {
      while (nextChunk < chunks.length) {
        absorb(await bulkDeletePaths(chunks[nextChunk], { force }));
        nextChunk++;
      }
      while (nextFeature < pd.plan.featureIds.length) {
        const fid = pd.plan.featureIds[nextFeature];
        const out = await runBulkBaton(async () => {
          const page = await deleteFeatureTasks(projectId, fid, { force });
          absorb(page);
          return page;
        }, (r) => r.deleted);
        if (out.stopped) throw new Error(`Stopped after ${deleted} deleted — more tasks remain`);
        nextFeature++;
      }
      return { ok: deleted, failed: failures, failedTitles: titles };
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

    if (failed > 0) {
      toast(
        `Deleted ${ok}, failed ${failed}${
          failedTitles.length > 0 ? ` (${failedTitles.join(", ")})` : ""
        }`,
        "warning",
      );
      throw new Error(`Deleted ${ok}; ${failed} failed`);
    } else {
      report({ detail: `Deleted ${ok} entries`, completed: ok, total: ok });
      toast(`Deleted ${ok} entr${ok === 1 ? "y" : "ies"}`, "success");
    }
  };

  // No dry-run round trip here: archive is reversible and the counts are
  // derivable client-side, so a plain ConfirmDialog with counts suffices.
  const previewArchive = () => {
    if (archiveGate.toArchive === 0) {
      toast("Nothing to archive — selection is already archived", "warning");
      return;
    }
    // Same resolution as delete: explicit tasks covered by a selected
    // feature ride that feature's baton instead of the path list.
    const plan = buildDeletePlan({ projectId, taskIds, featureIds }, tasks);
    const statusByPath = new Map(tasks.map((t) => [t.path, t.status]));
    setPendingArchive({
      taskPaths: plan.taskPaths.filter(
        (p) => statusByPath.get(p) !== "archived",
      ),
      featureIds: plan.featureIds,
      total: archiveGate.toArchive,
    });
  };

  // Current-render closures for the context-menu requests: the same
  // gate and flows as the bar's own buttons, including the disabled
  // Archive's explanation (a menu can't grey a verb it already fired,
  // so the gate answers with the tooltip's text as a toast).
  verbHandlersRef.current = {
    archive: () => {
      if (busy) return;
      if (!archiveGate.allSettled) {
        toast("Only settled tasks can be archived", "warning");
        return;
      }
      previewArchive();
    },
    del: () => {
      if (busy) return;
      void previewDelete();
    },
  };

  return (
    <>
      <div className="selection-bar" role="toolbar" aria-label="Selection">
        <span className="sel-count">{summary} selected</span>
        <button
          disabled={busy || !archiveGate.allSettled}
          title={
            archiveGate.allSettled
              ? undefined
              : "Only settled tasks can be archived"
          }
          onClick={previewArchive}
        >
          Archive
        </button>
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
              " This cannot be undone. Progress will appear in the background; keep this tab open.",
            confirmLabel: `Delete ${pending.total}`,
            // Same friction as single-feature delete: destructive fan-outs
            // require typing. A fixed word (not a name) because the target
            // is a heterogeneous selection.
            typeToConfirm: "delete",
          }}
          danger
          onCancel={() => setPending(null)}
          onConfirm={async () => {
            const operation = startBackgroundOperation(`Delete selection · ${projectId}`, (report) => commitDelete(pending, report));
            setPending(null);
            clear();
            void operation;
          }}
        />
      )}

      {pendingArchive && (
        <ConfirmDialog
          confirm={{
            title: `Archive ${pendingArchive.total} task${
              pendingArchive.total === 1 ? "" : "s"
            }?`,
            body:
              `Archives ${summary}` +
              `${pendingArchive.featureIds.length > 0 ? " (feature selections archive all of their tasks)" : ""}.` +
              " Archived tasks leave the default lists and stop counting toward progress." +
              " This is reversible — restore them later from the Archived filter. Progress will appear in the background; keep this tab open.",
            confirmLabel: `Archive ${pendingArchive.total}`,
          }}
          onCancel={() => setPendingArchive(null)}
          onConfirm={async () => {
            const operation = startArchive({ ...pendingArchive, projectId });
            setPendingArchive(null);
            clear();
            void operation;
          }}
        />
      )}
    </>
  );
}

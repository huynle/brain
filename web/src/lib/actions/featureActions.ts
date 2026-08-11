/**
 * lib/actions/featureActions — the verb matrix for a feature.
 *
 * A feature is not a stored entity: it is the set of tasks sharing a
 * `feature_id`. Every feature-level verb is therefore a fan-out, and the
 * UI has to be honest about that:
 *
 *   "Set status"    → bulk-update filtered by feature_id
 *   "Cancel"        → the same, pinned to cancelled
 *   "Delete"        → bulk-delete filtered by feature_id
 *
 * Two consequences shape the descriptors below.
 *
 * First, **cancel is the primary destructive verb, not delete.** Cancelling
 * is reversible and keeps the history; deleting removes the record of what
 * was attempted. Delete stays available but is gated behind typing the
 * feature name.
 *
 * Second, **every fan-out previews before it commits.** The bulk endpoints
 * take `dry_run`, and the confirm dialog shows the real count from that
 * preview — including whether the 100-entry cap truncated the match. A
 * feature-wide mutation that silently touched only the first 100 of 120
 * tasks is precisely the failure this avoids.
 */
import type { DerivedFeature } from "../features";
import type { TaskStatus } from "../types";
import { STATUS_LABELS } from "./taskActions";
import type { ActionDescriptor } from "./types";

export interface FeatureActionContext {
  runFeature: (feature: DerivedFeature) => Promise<void>;
  /** Opens the status picker for a feature-wide change. */
  openStatusPicker: (feature: DerivedFeature) => void;
  /** Applies a status to every task in the feature (preview then commit). */
  setStatusForAll: (
    feature: DerivedFeature,
    status: TaskStatus,
  ) => Promise<void>;
  /** Deletes every task in the feature. */
  deleteFeature: (feature: DerivedFeature) => Promise<void>;
  openCheckout: (feature: DerivedFeature) => void;
  openResume: (feature: DerivedFeature) => void;
  openPlan: (feature: DerivedFeature) => void;
  openDetails: (feature: DerivedFeature) => void;
  openMetadata: (feature: DerivedFeature) => void;
}

/** Statuses in which a feature's tasks are all finished, one way or another. */
const SETTLED_LIFECYCLES = new Set(["merged", "finished"]);

/** Why a feature cannot be run right now, or "" when it can. */
export function runFeatureBlockedReason(feature: DerivedFeature): string {
  if (feature.taskCount.total === 0) return "Feature has no tasks";
  if (SETTLED_LIFECYCLES.has(feature.lifecycle)) {
    return `Feature is ${feature.lifecycle} — nothing left to run`;
  }
  if (feature.taskCount.active === 0) {
    return "No runnable tasks — all are blocked or done";
  }
  return "";
}

/** Why a feature cannot be cancelled, or "" when it can. */
export function cancelFeatureBlockedReason(feature: DerivedFeature): string {
  if (feature.taskCount.total === 0) return "Feature has no tasks";
  if (feature.taskCount.completed === feature.taskCount.total) {
    return "Every task is already done";
  }
  return "";
}

/** Why a feature cannot be checked out, or "" when it can. */
export function checkoutBlockedReason(feature: DerivedFeature): string {
  if (feature.taskCount.total === 0) return "Feature has no tasks";
  return "";
}

/**
 * Count of tasks a feature-wide mutation would touch. Callers show this in
 * the confirm dialog; the server's dry run is the authority, but this gives
 * the dialog something accurate to render before the round trip.
 */
export function affectedTaskCount(feature: DerivedFeature): number {
  return feature.taskCount.total;
}

export function buildFeatureActions(
  feature: DerivedFeature,
  ctx: FeatureActionContext,
): ActionDescriptor[] {
  const actions: ActionDescriptor[] = [];
  const n = affectedTaskCount(feature);
  const plural = n === 1 ? "task" : "tasks";

  // ─── run ────────────────────────────────────────────────────────
  actions.push({
    id: "run",
    label: "Run feature now",
    group: "run",
    key: "x",
    disabledReason: runFeatureBlockedReason(feature),
    run: () => ctx.runFeature(feature),
  });

  if (feature.resumableCount > 0) {
    actions.push({
      id: "resume",
      label: `Resume ${feature.resumableCount} abandoned ${
        feature.resumableCount === 1 ? "task" : "tasks"
      }`,
      group: "run",
      key: "r",
      run: async () => ctx.openResume(feature),
    });
  }

  actions.push({
    id: "checkout",
    label: "Review & merge…",
    group: "run",
    key: "f",
    disabledReason: checkoutBlockedReason(feature),
    run: async () => ctx.openCheckout(feature),
  });

  // ─── state ──────────────────────────────────────────────────────
  actions.push({
    id: "status",
    label: "Set status for all tasks…",
    group: "state",
    key: "s",
    disabledReason: n === 0 ? "Feature has no tasks" : "",
    run: async () => ctx.openStatusPicker(feature),
  });

  actions.push({
    id: "cancel",
    label: "Cancel feature",
    group: "state",
    disabledReason: cancelFeatureBlockedReason(feature),
    confirm: {
      title: `Cancel ${feature.name}?`,
      body:
        `All ${n} ${plural} in this feature will be set to cancelled. ` +
        `Any runner already executing one of them will keep going until aborted separately. ` +
        `This is reversible — you can set the tasks back to pending afterwards.`,
      confirmLabel: "Cancel feature",
    },
    run: () => ctx.setStatusForAll(feature, "cancelled"),
  });

  // ─── edit ───────────────────────────────────────────────────────
  actions.push({
    id: "metadata",
    label: "Edit metadata…",
    group: "edit",
    key: "e",
    run: async () => ctx.openMetadata(feature),
  });

  // ─── navigate ───────────────────────────────────────────────────
  actions.push({
    id: "plan",
    label: "Open plan drawer",
    group: "navigate",
    run: async () => ctx.openPlan(feature),
  });
  actions.push({
    id: "details",
    label: "Feature details",
    group: "navigate",
    run: async () => ctx.openDetails(feature),
  });

  // ─── danger ─────────────────────────────────────────────────────
  actions.push({
    id: "delete",
    label: "Delete feature",
    group: "danger",
    key: "d",
    danger: true,
    disabledReason: n === 0 ? "Feature has no tasks" : "",
    confirm: {
      title: `Delete ${feature.name}?`,
      body:
        `This permanently removes all ${n} ${plural} in the feature and their history. ` +
        `It cannot be undone. If you only want to stop the work, cancel the feature instead.`,
      // The one place type-to-confirm earns its friction: irreversible,
      // and the blast radius is every task under the feature.
      typeToConfirm: feature.name,
      confirmLabel: "Delete permanently",
    },
    run: () => ctx.deleteFeature(feature),
  });

  return actions;
}

/**
 * Status entries for a feature-wide change. Unlike the task picker there
 * is no "already this status" case to disable — a feature's tasks can hold
 * a mix of statuses, so every target remains meaningful.
 */
export function buildFeatureStatusActions(
  feature: DerivedFeature,
  ctx: Pick<FeatureActionContext, "setStatusForAll">,
  statuses: readonly TaskStatus[],
): ActionDescriptor[] {
  const n = affectedTaskCount(feature);
  const plural = n === 1 ? "task" : "tasks";

  return statuses.map((status) => ({
    id: `status:${status}`,
    label: STATUS_LABELS[status] ?? status,
    group: "state" as const,
    confirm: {
      title: `Set ${n} ${plural} to ${STATUS_LABELS[status] ?? status}?`,
      body: `Every task in ${feature.name} will be set to "${status}".`,
      confirmLabel: "Apply to all",
    },
    run: () => ctx.setStatusForAll(feature, status),
  }));
}

/**
 * Summarise a bulk result for a toast. Partial failure must be legible —
 * "7 of 9 updated; 2 failed" beats a bare success message when the fan-out
 * was not atomic.
 */
export function summarizeBulkResult(r: {
  ok: number;
  failed: number;
  total: number;
  truncated?: boolean;
  matchedTotal?: number;
}): { message: string; kind: "success" | "warning" | "error" } {
  if (r.total === 0) {
    return { message: "Nothing matched — no tasks changed", kind: "warning" };
  }
  if (r.failed === 0) {
    const base = `${r.ok} of ${r.total} ${r.total === 1 ? "task" : "tasks"} updated`;
    if (r.truncated) {
      return {
        message: `${base}, but ${(r.matchedTotal ?? 0) - r.total} more matched than the 100-task limit allows — run again to continue`,
        kind: "warning",
      };
    }
    return { message: base, kind: "success" };
  }
  if (r.ok === 0) {
    return { message: `All ${r.failed} failed`, kind: "error" };
  }
  return {
    message: `${r.ok} of ${r.total} succeeded; ${r.failed} failed`,
    kind: "warning",
  };
}

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
import type { ResumeFeatureResult, TaskStatus } from "../types";
import { STATUS_LABELS } from "./taskActions";
import type { ActionDescriptor } from "./types";

export interface FeatureActionContext {
  /** Open the feature drawer — the row's double-click / Enter target and
   *  the context-menu "Open" verb. A distinct, clearly-named alias for the
   *  same drawer `openPlan` opens, so the primary Open verb reads plainly
   *  rather than as "Open plan drawer". */
  openDrawer: (feature: DerivedFeature) => void;
  /** Toggle this feature in the multi-select scope (SelectionBar verbs). */
  toggleSelect: (feature: DerivedFeature) => void;
  /** Whether the feature is currently in the multi-select scope. */
  isSelected: (feature: DerivedFeature) => boolean;
  runFeature: (feature: DerivedFeature) => Promise<void>;
  /** The FEATURE pause dial — holds this feature's tasks out of automatic
   *  dispatch while the rest of the project keeps running.
   *
   *  The `Dispatch` suffix is load-bearing: `resumeFeature` below is an
   *  UNRELATED operation that batch-resumes the feature's ABANDONED tasks
   *  by rewriting their status. One turns a dial, the other edits tasks —
   *  a bad pair to confuse. */
  pauseFeatureDispatch: (feature: DerivedFeature) => Promise<void>;
  resumeFeatureDispatch: (feature: DerivedFeature) => Promise<void>;
  /** Runs the feature AND enrols everything that transitively depends on it,
   *  so each runs as its gate opens. */
  runFeatureWithDependents: (feature: DerivedFeature) => Promise<void>;
  /** Cancels a standing chain rooted at this feature. */
  cancelDependentChain: (feature: DerivedFeature) => Promise<void>;
  /** Whether a chain rooted here is already running. */
  hasActiveChain: (feature: DerivedFeature) => boolean;
  /** Batch-resumes every abandoned task in the feature, directly. */
  resumeFeature: (feature: DerivedFeature) => Promise<void>;
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
  /**
   * Open one session pane per running task in this feature, side by
   * side in Focus. Returns how many it could actually address, so the
   * verb can say when it found nothing rather than switching the user
   * to an unchanged workspace.
   */
  watchInFocus: (feature: DerivedFeature) => number;
  openMetadata: (feature: DerivedFeature) => void;
  /** Opens the runner-assignment modal (ModalKind "feature-assign"). */
  openAssignRunner: (feature: DerivedFeature) => void;
  /** Clears the feature's runner assignment on the server. */
  clearRunnerAssignment: (feature: DerivedFeature) => Promise<void>;
  /**
   * Runner currently assigned to the feature, if the client knows of one.
   * Sync lookup — the builder uses it to disable "Clear assignment" with a
   * reason instead of letting it no-op.
   */
  assignedRunner: (feature: DerivedFeature) => string | undefined;
  /** Opens the goal-create modal prefilled with this feature's scope. */
  openGoalCreate: (feature: DerivedFeature) => void;
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

/** Why a feature cannot be archived, or "" when it can. */
export function archiveFeatureBlockedReason(feature: DerivedFeature): string {
  if (feature.taskCount.total === 0) return "Feature has no tasks";
  if (!SETTLED_LIFECYCLES.has(feature.lifecycle)) {
    return "Feature has active work — archive is for settled features";
  }
  return "";
}

/** Why a feature cannot be checked out, or "" when it can. */
export function checkoutBlockedReason(feature: DerivedFeature): string {
  if (feature.taskCount.total === 0) return "Feature has no tasks";
  return "";
}

/** Why a feature cannot be resumed, or "" when it can. */
export function resumeFeatureBlockedReason(feature: DerivedFeature): string {
  if (feature.taskCount.total === 0) return "Feature has no tasks";
  if (feature.resumableCount === 0) {
    return "No abandoned tasks — nothing to resume";
  }
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

/** Why the feature dial cannot be moved to `want`, or "" when it can.
 *  Mirrors `pauseDialBlockedReason` for the project dials: an unknown state
 *  (still loading) must not disable an idempotent verb. */
export function featurePauseBlockedReason(
  paused: boolean | undefined,
  want: boolean,
): string {
  if (paused === undefined) return "";
  if (paused === want) {
    return want ? "Feature is already paused" : "Feature is not paused";
  }
  return "";
}

export function buildFeatureActions(
  feature: DerivedFeature,
  ctx: FeatureActionContext,
  opts: {
    /** From `isFeaturePaused`; undefined while pause state is loading. */
    paused?: boolean;
  } = {},
): ActionDescriptor[] {
  const actions: ActionDescriptor[] = [];
  const n = affectedTaskCount(feature);
  const plural = n === 1 ? "task" : "tasks";

  // ─── select ─────────────────────────────────────────────────────
  // "Open" leads the menu: the row's primary gesture (double-click /
  // Enter open the drawer), surfaced first in the context menu too.
  // Distinct from the navigate-group "Open plan drawer" verb, which
  // stays put — this is the plain, top-of-menu Open.
  actions.push({
    id: "open",
    label: "Open",
    group: "select",
    run: async () => ctx.openDrawer(feature),
  });
  actions.push({
    id: "select",
    label: ctx.isSelected(feature) ? "Deselect" : "Select",
    group: "select",
    key: "v",
    run: async () => ctx.toggleSelect(feature),
  });

  // ─── run ────────────────────────────────────────────────────────
  actions.push({
    id: "run",
    label: "Run feature now",
    group: "run",
    key: "x",
    disabledReason: runFeatureBlockedReason(feature),
    run: () => ctx.runFeature(feature),
  });

  // Opt-in sibling of "Run feature now". A SEPARATE verb rather than a
  // modifier on the existing one: the default gesture must keep meaning
  // exactly what it means today, and a chain has a much wider blast radius
  // than one feature.
  //
  // Worth knowing: on an UNPAUSED project the scheduler already dispatches a
  // dependent the moment its gate opens, so this earns its keep while a
  // project is paused.
  // The label carries no count on purpose. The chain is derived server-side
  // from the CURRENT graph at click time, so any number computed here from
  // client state would be a guess that can disagree with what actually gets
  // queued. The toast reports the real figure the moment it is known.
  actions.push({
    id: "run-with-dependents",
    label: "Run feature + dependents",
    group: "run",
    key: "X",
    disabledReason: runFeatureBlockedReason(feature),
    run: () => ctx.runFeatureWithDependents(feature),
  });

  // Only offered when there is something to cancel, so the verb list does
  // not carry a permanently-dead entry for the common case.
  if (ctx.hasActiveChain(feature)) {
    actions.push({
      id: "cancel-chain",
      label: "Cancel queued dependents",
      group: "run",
      run: () => ctx.cancelDependentChain(feature),
    });
  }

  // Always present (disabled-never-hidden); executes directly rather than
  // detouring through FeatureActionsModal — the modal remains reachable via
  // "Review & merge…" for the checkout workflow, but resume is one gesture.
  actions.push({
    id: "resume",
    label:
      feature.resumableCount > 0
        ? `Resume ${feature.resumableCount} abandoned ${
            feature.resumableCount === 1 ? "task" : "tasks"
          }`
        : "Resume abandoned tasks",
    group: "run",
    key: "r",
    disabledReason: resumeFeatureBlockedReason(feature),
    run: () => ctx.resumeFeature(feature),
  });

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

  // ─── the feature dial ───────────────────────────────────────────
  // Placed beside cancel because that is the pair a user chooses
  // between, and the difference matters: pause is reversible and
  // touches no task, cancel rewrites every task's status and — since
  // a cancelled task counts as blocked — permanently hard-blocks every
  // feature that depends on this one.
  //
  // "Stop new dispatch" is spelled out for the same reason as the
  // project verb: pause does not interrupt a task a runner is already
  // executing, and a bare "Pause feature" would promise that it does.
  actions.push({
    id: "pause-dispatch",
    label: "Pause feature (stop new dispatch)",
    group: "state",
    disabledReason: featurePauseBlockedReason(opts.paused, true),
    run: () => ctx.pauseFeatureDispatch(feature),
  });
  actions.push({
    id: "resume-dispatch",
    label: "Resume feature",
    group: "state",
    disabledReason: featurePauseBlockedReason(opts.paused, false),
    run: () => ctx.resumeFeatureDispatch(feature),
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

  actions.push({
    id: "archive",
    label: "Archive feature",
    group: "state",
    disabledReason: archiveFeatureBlockedReason(feature),
    // Reversible (each task can be unarchived), so confirm without
    // type-to-confirm — same weight as cancel.
    confirm: {
      title: `Archive ${feature.name}?`,
      body:
        `All ${n} ${plural} in this feature will be set to archived. ` +
        `The feature leaves the lanes and its tasks stop counting toward ` +
        `progress. This is reversible — restore the tasks later from the ` +
        `Archived filter.`,
      confirmLabel: "Archive feature",
    },
    run: () => ctx.setStatusForAll(feature, "archived"),
  });

  // ─── edit ───────────────────────────────────────────────────────
  actions.push({
    id: "metadata",
    label: "Edit metadata…",
    group: "edit",
    key: "e",
    run: async () => ctx.openMetadata(feature),
  });

  // Runner assignment. Assign opens the picker modal (choosing a runner
  // needs a list, which a menu row cannot show); clear acts directly.
  const assigned = ctx.assignedRunner(feature);
  actions.push({
    id: "assign",
    label: assigned ? `Assign runner… (now ${assigned})` : "Assign runner…",
    group: "edit",
    key: "g",
    run: async () => ctx.openAssignRunner(feature),
  });
  actions.push({
    id: "unassign",
    label: "Clear runner assignment",
    group: "edit",
    disabledReason: assigned ? "" : "No runner assigned",
    run: () => ctx.clearRunnerAssignment(feature),
  });

  // Goals attach to a feature by scope, not by mutation — creating one is
  // always possible, even on a settled feature (a goal can watch a merged
  // feature for regressions).
  actions.push({
    id: "set-goal",
    label: "Set goal…",
    group: "edit",
    run: async () => ctx.openGoalCreate(feature),
  });

  // ─── navigate ───────────────────────────────────────────────────
  /*
   * The one thing no other surface can do: several agents on screen at
   * once. A feature is usually two or three tasks running in parallel,
   * and until now watching them meant clicking between sessions and
   * losing the thread of each.
   *
   * Gated on the feature having active tasks rather than on live
   * instances: `taskCount.active` is already on DerivedFeature, and the
   * builder stays pure. The effect reports what it actually found — a
   * task can be "active" with no addressable session (dispatch pending,
   * a pi executor) — so a run that opens nothing says so instead of
   * switching the user to an unchanged Focus tab.
   */
  actions.push({
    id: "watch-focus",
    label: "Watch tasks in Focus",
    group: "navigate",
    key: "w",
    disabledReason:
      feature.taskCount.active > 0
        ? ""
        : "No active tasks — nothing is running to watch",
    run: async () => void ctx.watchInFocus(feature),
  });
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
 * Summarise a batch resume for a toast: counts plus the top skip reasons,
 * so "Resumed 0 · 5 skipped" is never the whole story. Skip reasons come
 * back per task; we bucket identical strings and show the two most common.
 */
export function summarizeResumeOutcome(r: ResumeFeatureResult): {
  message: string;
  kind: "success" | "info" | "warning";
} {
  if (r.total_resumed === 0 && r.total_skipped === 0) {
    return { message: "No tasks in feature", kind: "info" };
  }

  const parts: string[] = [];
  if (r.total_resumed > 0) {
    parts.push(
      `Resumed ${r.total_resumed} task${r.total_resumed === 1 ? "" : "s"}`,
    );
  } else {
    parts.push("No tasks resumed");
  }
  if (r.total_skipped > 0) {
    parts.push(`${r.total_skipped} skipped`);
  }

  // Top skip reasons, most common first. Ties break on first-seen order,
  // which is deterministic on identical input.
  const counts = new Map<string, number>();
  for (const row of r.results ?? []) {
    if (row.resumed || !row.reason) continue;
    counts.set(row.reason, (counts.get(row.reason) ?? 0) + 1);
  }
  const top = [...counts.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, 2)
    .map(([reason, n]) => (n > 1 ? `${n}× ${reason}` : reason));
  if (top.length > 0) parts.push(`(${top.join("; ")})`);

  return {
    message: parts.join(" · "),
    kind:
      r.total_resumed === 0
        ? "info"
        : r.total_skipped > 0
          ? "warning"
          : "success",
  };
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

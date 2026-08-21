/**
 * lib/actions/runnerActions — the verb matrix for a runner row.
 *
 * Runner rows previously hand-rolled a ContextMenu in the sidebar
 * (RunnersSection) while the Focus-pane twin (RunnersLeaf) had no menu
 * at all — the exact drift lib/actions exists to prevent. The verbs
 * live here once; both surfaces render the same list.
 *
 * Runners are not mutated through CRUD like entries: the two mutating
 * verbs are "clear feature assignments" (bulk unpin, reversible by
 * dragging features back) and "shutdown" (a graceful-stop command
 * delivered over the runner's SSE stream — the process can be
 * restarted from its host, so the confirm is plain, no type-to-confirm).
 *
 * Pure: takes a RunnerInfo plus effect callbacks, returns descriptors.
 */
import type { RunnerInfo } from "../types";
import type { ActionDescriptor } from "./types";

/**
 * Effects a runner action can perform. The component supplies real
 * implementations; tests supply recorders.
 */
export interface RunnerActionContext {
  /** Opens the runner modal on the Shell tab. */
  openShell: (r: RunnerInfo) => void;
  /** Opens the runner modal on the Overview tab. */
  openDetails: (r: RunnerInfo) => void;
  /** Opens the runner modal on the Processes tab. */
  openProcesses: (r: RunnerInfo) => void;
  /** Clears every feature→runner assignment pinned to this runner. */
  clearAssignments: (r: RunnerInfo) => Promise<void>;
  /** PUT /runners/{id}/shutdown — graceful stop via SSE command. */
  shutdownRunner: (r: RunnerInfo) => Promise<void>;
}

/**
 * Merge server-known feature assignments with the optimistic map the
 * workspace store keeps during drag-and-drop, deduped, optimistic
 * first. An assignment the store has optimistically moved to another
 * runner is excluded even if the server still reports it here — the
 * UI must not offer to clear something the user just dragged away.
 */
export function combineRunnerAssignments(
  runner: RunnerInfo,
  optimistic: Record<string, string>,
): Array<{ featureId: string; projectId?: string }> {
  const seen = new Set<string>();
  const out: Array<{ featureId: string; projectId?: string }> = [];
  for (const [featureId, runnerId] of Object.entries(optimistic)) {
    if (runnerId !== runner.runner_id) continue;
    if (seen.has(featureId)) continue;
    seen.add(featureId);
    out.push({ featureId });
  }
  for (const a of runner.feature_assignments ?? []) {
    if (
      optimistic[a.feature_id] &&
      optimistic[a.feature_id] !== runner.runner_id
    ) {
      continue;
    }
    if (seen.has(a.feature_id)) continue;
    seen.add(a.feature_id);
    out.push({ featureId: a.feature_id, projectId: a.project_id });
  }
  return out;
}

/** Why assignments cannot be cleared right now, or "" when they can. */
export function clearAssignmentsBlockedReason(count: number): string {
  if (count === 0) return "No features are assigned to this runner";
  return "";
}

/** Why the runner cannot be shut down right now, or "" when it can. */
export function shutdownRunnerBlockedReason(r: RunnerInfo): string {
  // The shutdown command rides the runner's SSE stream — an offline
  // runner has no stream to receive it on. A "stale" runner may only
  // have a wedged heartbeat, so it stays eligible.
  if (r.status === "offline") {
    return "Runner is offline — there is no process to shut down";
  }
  return "";
}

/**
 * Build the full action list for a runner. Every action is always
 * present; unavailable ones carry a `disabledReason`. See ./types.
 *
 * `assignmentCount` should come from `combineRunnerAssignments` when
 * the caller tracks optimistic drag state; without it the builder
 * falls back to the server-reported assignments.
 */
export function buildRunnerActions(
  runner: RunnerInfo,
  ctx: RunnerActionContext,
  opts: { assignmentCount?: number } = {},
): ActionDescriptor[] {
  const count =
    opts.assignmentCount ?? runner.feature_assignments?.length ?? 0;
  const actions: ActionDescriptor[] = [];

  // ─── navigate ───────────────────────────────────────────────────
  actions.push({
    id: "shell",
    label: "Open runner shell",
    group: "navigate",
    key: "s",
    run: async () => ctx.openShell(runner),
  });

  actions.push({
    id: "details",
    label: "View details",
    group: "navigate",
    run: async () => ctx.openDetails(runner),
  });

  actions.push({
    id: "processes",
    label: "View processes",
    group: "navigate",
    run: async () => ctx.openProcesses(runner),
  });

  // ─── danger ─────────────────────────────────────────────────────
  // Clear-assignments lives in the danger group with the destructive
  // tone the old inline menu gave it, which also preserves that menu's
  // shape (navigation verbs, separator, destructive verbs). It stays
  // confirm-free: reversible by dragging features back, and the old
  // menu ran it in one click.
  actions.push({
    id: "clear-assignments",
    label:
      count > 1 ? `Clear all ${count} assignments` : "Clear assignment",
    group: "danger",
    danger: true,
    disabledReason: clearAssignmentsBlockedReason(count),
    run: () => ctx.clearAssignments(runner),
  });

  actions.push({
    id: "shutdown",
    label: "Shutdown runner",
    group: "danger",
    danger: true,
    disabledReason: shutdownRunnerBlockedReason(runner),
    // Reversible-ish (restart from the host), so confirm without
    // type-to-confirm — interrupted in-flight work is the consequence
    // worth a pause, not data loss.
    confirm: {
      title: `Shutdown ${runner.runner_id}?`,
      body:
        "The runner stops claiming work and exits. Tasks it is running " +
        "are interrupted and will surface as abandoned until resumed. " +
        "Restart the runner from its host to bring it back.",
      confirmLabel: "Shutdown runner",
    },
    run: () => ctx.shutdownRunner(runner),
  });

  return actions;
}

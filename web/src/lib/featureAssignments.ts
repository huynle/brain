/**
 * lib/featureAssignments — which runner a feature is pinned to.
 *
 * The SERVER owns this. `feature_assignments` is a real table
 * (internal/storage/feature_assignments.go), and every runner carries its
 * own rows on `RunnerInfo.feature_assignments` — attached by
 * `attachFeatureAssignments` on both the list and get paths, so the data
 * arrives on every `GET /runners` and inside every `runners_update` SSE
 * snapshot. The PWA's `RunnerInfo` type has always declared the field.
 *
 * It was never read. Three surfaces — the Tasks tab's feature header, the
 * overview grid and the feature detail pane — resolved the runner from
 * `useWorkspace().featureAssignments`, a browser-local map whose ONLY
 * writers are this app's own optimistic assign/unassign calls, persisted to
 * localStorage. Nothing hydrated it. So:
 *
 *   - a second browser, a phone, or cleared site data showed
 *     "· unassigned ·" for features the server had assigned;
 *   - an AUTO assignment was invisible everywhere, always — `task.go`
 *     auto-claims a feature for the first runner to pick up one of its
 *     tasks (source: "auto"), and no click ever writes that map. Live
 *     production had exactly one such assignment while this was written;
 *   - a stale local entry could name a runner the server had since cleared.
 *
 * `combineRunnerAssignments` in lib/actions/runnerActions.ts already had the
 * right shape for the RUNNER side of the same question — server rows merged
 * with the optimistic map as an overlay. This is that idea keyed by feature,
 * so both directions agree.
 */
import type { RunnerInfo } from "./types";

/**
 * Optimistic tombstone: the user cleared an assignment and the round-trip
 * has not landed. Distinct from "absent", which now means "no local opinion"
 * and defers to the server — without it, an optimistic clear would be
 * instantly overwritten by the server row it is in the process of deleting.
 */
export const CLEARED = "";

/**
 * feature id → runner id, server truth with the optimistic map layered on.
 *
 * Only `status: "active"` rows count. The table also holds released rows
 * (`ClearFeatureAssignmentsByRunner` marks rather than deletes), and
 * treating one of those as live would pin a feature to a runner that
 * released it.
 */
export function resolveFeatureAssignments(
  runners: readonly RunnerInfo[],
  optimistic: Record<string, string> = {},
): Record<string, string> {
  const out: Record<string, string> = {};

  for (const r of runners) {
    for (const a of r.feature_assignments ?? []) {
      if (a.status && a.status !== "active") continue;
      if (!a.feature_id) continue;
      // Prefer the runner that actually reports the row. `runner_id` on the
      // row and the runner carrying it are the same in practice; fall back
      // so a payload missing the field still resolves.
      out[a.feature_id] = a.runner_id || r.runner_id;
    }
  }

  // The overlay wins while a mutation is in flight, in BOTH directions.
  for (const [featureId, runnerId] of Object.entries(optimistic)) {
    if (runnerId === CLEARED) delete out[featureId];
    else out[featureId] = runnerId;
  }

  return out;
}

/**
 * Overlay entries the server has caught up with, which can be dropped.
 *
 * Without this the overlay is permanent for the session: an optimistic
 * assign keeps winning even after the server disagrees (say the runner went
 * offline and its assignments were released), which is the same "local map
 * outlives the truth" failure this module exists to end — just with a
 * shorter fuse.
 */
export function settledAssignments(
  runners: readonly RunnerInfo[],
  optimistic: Record<string, string>,
): string[] {
  const server = resolveFeatureAssignments(runners);
  const settled: string[] = [];
  for (const [featureId, runnerId] of Object.entries(optimistic)) {
    const actual = server[featureId];
    if (runnerId === CLEARED ? actual === undefined : actual === runnerId) {
      settled.push(featureId);
    }
  }
  return settled;
}

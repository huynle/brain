/**
 * lib/chains — prose for a standing "run feature + dependents" request.
 *
 * A chain is a queue the SERVER advances on its own: one click dispatches
 * a feature and enrols everything downstream of it, then the API drains
 * the queue as dependencies clear. Nothing in the UI would otherwise
 * mention those queued features again — the toast says "queued 2
 * features" and then they are invisible — which is why the feature head
 * carries a chip and the chip carries this tooltip.
 *
 * Pure so `node --test` can reach it without fetch or React. Lived inline
 * in CardFeatures until that view was folded into the Tasks tab.
 */
import type { DependentChain } from "./api";

/**
 * Tooltip for a chain ROOT — what it queued, and anything that will stall
 * it. An external wait is the difference between "waiting its turn" and
 * "never going to run", so it must not be left to inference.
 */
export function chainRootTitle(c: DependentChain | undefined): string {
  if (!c) return "Running with dependents";
  // Defensive ?? []: the server omits an empty `queued`, and an older build
  // could send null. Reading .length on that crashes the whole row rather
  // than degrading to "nothing queued".
  const queued = c.queued ?? [];
  const parts = [
    queued.length > 0
      ? `Running with ${queued.length} queued dependent ${
          queued.length === 1 ? "feature" : "features"
        }: ${queued.join(", ")}`
      : "Running with dependents; nothing queued behind it",
  ];
  if (c.waitsOnExternal?.length) {
    parts.push(
      `Stalls on ${c.waitsOnExternal.join(", ")} — not part of this run.`,
    );
  }
  if (!c.pausedAtRequest) {
    parts.push("Pausing the project will hold this chain.");
  }
  return parts.join("\n");
}

/** Tooltip for a queued MEMBER of a chain. */
export const CHAIN_QUEUED_TITLE =
  "Queued by a run-with-dependents chain. It dispatches on its own once " +
  "its dependencies finish — no second click needed.";

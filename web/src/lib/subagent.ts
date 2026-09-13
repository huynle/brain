/**
 * subagent — pure helpers for the recursive subagent session drill-down.
 *
 * A subagent invocation surfaces in a transcript as an OpenCode tool part
 * whose `tool === "task"`. These helpers identify such parts, resolve the
 * child session id they spawned, and build the SessionRef that addresses the
 * child's own transcript. All pure and unit-testable; the React wiring in
 * Transcript.tsx consumes them.
 */
import type { OcPart, SessionRef } from "./types";

/** A tool part is a subagent invocation iff its tool is OpenCode's "task" tool. */
export function isSubagentPart(part: OcPart): boolean {
  return part.type === "tool" && part.tool === "task";
}

/**
 * The child session id a subagent tool part spawned, or undefined.
 * Primary: parse `session_id: ses_…` out of state.output (OpenCode writes it
 * inside a <task_metadata> block). Secondary: state.metadata.sessionId.
 */
export function childSessionIdFromPart(part: OcPart): string | undefined {
  if (!isSubagentPart(part)) return undefined;
  const output = typeof part.state?.output === "string" ? part.state.output : "";
  const m = output.match(/session_id:\s*(ses_[A-Za-z0-9]+)/);
  if (m) return m[1];
  const meta = part.state?.metadata as { sessionId?: string } | undefined;
  if (meta && typeof meta.sessionId === "string" && meta.sessionId) return meta.sessionId;
  return undefined;
}

/**
 * Build the SessionRef for a subagent's child transcript given the parent
 * pane's ref and the resolved child session id. The child addresses itself by
 * its OWN session_id; parent_session_id is provenance. A live parent yields a
 * live child (same runner+instance, child session id) so it streams; a history
 * parent yields a history child by id (dead-instance safe).
 */
export function childSessionRef(parent: SessionRef, childSessionId: string): SessionRef {
  if (parent.mode === "live") {
    return {
      mode: "live",
      runner_id: parent.runner_id,
      instance_id: parent.instance_id,
      session_id: childSessionId,
      parent_session_id: parent.session_id,
    };
  }
  return {
    mode: "history",
    runner_id: parent.runner_id,
    session_id: childSessionId,
    parent_session_id: parent.session_id,
  };
}

/**
 * The gating decision for whether a tool part renders a subagent drill-down.
 * Single source of truth for logic PartView (Transcript.tsx) used to compute
 * inline. Pure so the cycle / depth / no-child / non-subagent branches are
 * unit-testable without a DOM.
 */
export interface SubagentDrilldownState {
  /** Resolved child session id, or undefined when none is addressable
   *  (no sessionRef, not a subagent, or Pi/empty output+metadata). */
  childId?: string;
  /** Whether the part itself is a subagent ("task") tool part. */
  isSubagent: boolean;
  /** Eligible to render an expand affordance + the recursive drill-down. */
  canDrill: boolean;
  /** A subagent WITH a resolvable child that is blocked by cycle/depth. */
  capped: boolean;
  /** Why it is capped — only set when `capped` is true. */
  cappedReason?: "cycle" | "max-depth";
}

/**
 * Decide the drill-down state for a tool part. Reproduces PartView's
 * `case "tool"` logic exactly:
 *   childId  = sessionRef ? childSessionIdFromPart(part) : undefined
 *   subagent = isSubagentPart(part) && !!childId        (the drill-down gate)
 *   canDrill = subagent && !!sessionRef
 *              && !ancestors.includes(childId) && depth < maxDepth
 *   capped   = subagent && !canDrill
 * `isSubagent` on the returned state is the RAW isSubagentPart(part): a
 * subagent with no child reads isSubagent:true, childId:undefined,
 * capped:false — it renders without a drill-down, not as "capped".
 */
export function subagentDrilldownState(
  part: OcPart,
  sessionRef: SessionRef | undefined,
  ancestors: string[],
  depth: number,
  maxDepth: number,
): SubagentDrilldownState {
  const isSubagent = isSubagentPart(part);
  const childId = sessionRef ? childSessionIdFromPart(part) : undefined;
  // The drill-down gate: a subagent part that actually resolved a child.
  const gated = isSubagent && !!childId;
  const canDrill =
    gated &&
    !!sessionRef &&
    !ancestors.includes(childId as string) &&
    depth < maxDepth;
  const capped = gated && !canDrill;
  const cappedReason = capped
    ? ancestors.includes(childId as string)
      ? "cycle"
      : "max-depth"
    : undefined;
  return { childId, isSubagent, canDrill, capped, cappedReason };
}

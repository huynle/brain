import test from "node:test";
import assert from "node:assert/strict";
import {
  isSubagentPart,
  childSessionIdFromPart,
  childSessionRef,
  subagentDrilldownState,
} from "./subagent";
import type { OcPart, SessionRef } from "./types";

/** A subagent ("task") tool part whose output carries a child session id. */
function taskPart(childId?: string): OcPart {
  return {
    id: "p",
    type: "tool",
    tool: "task",
    state: childId
      ? { output: `<task_metadata>\nsession_id: ${childId}\n</task_metadata>` }
      : { status: "running" },
  };
}

test("isSubagentPart: true only for the OpenCode task tool", () => {
  assert.equal(isSubagentPart({ id: "p", type: "tool", tool: "task" }), true);
  assert.equal(isSubagentPart({ id: "p", type: "tool", tool: "bash" }), false);
  assert.equal(isSubagentPart({ id: "p", type: "text" }), false);
  // tool undefined
  assert.equal(isSubagentPart({ id: "p", type: "tool" }), false);
});

test("childSessionIdFromPart: extracts ses_ from a <task_metadata> output block", () => {
  const part: OcPart = {
    id: "p",
    type: "tool",
    tool: "task",
    state: {
      output:
        "some preamble\n<task_metadata>\nsession_id: ses_46f60d3a8ffeHjjAlXvcGwSRVR\n</task_metadata>\ntrailing",
    },
  };
  assert.equal(
    childSessionIdFromPart(part),
    "ses_46f60d3a8ffeHjjAlXvcGwSRVR",
  );
});

test("childSessionIdFromPart: falls back to state.metadata.sessionId when output has no match", () => {
  const part: OcPart = {
    id: "p",
    type: "tool",
    tool: "task",
    state: {
      output: "no id in here",
      metadata: { sessionId: "ses_fromMetadata123" },
    },
  };
  assert.equal(childSessionIdFromPart(part), "ses_fromMetadata123");
});

test("childSessionIdFromPart: undefined when neither output nor metadata present", () => {
  const part: OcPart = {
    id: "p",
    type: "tool",
    tool: "task",
    state: { status: "running" },
  };
  assert.equal(childSessionIdFromPart(part), undefined);
});

test("childSessionIdFromPart: undefined when not a task part even if output has a ses_ id", () => {
  const part: OcPart = {
    id: "p",
    type: "tool",
    tool: "bash",
    state: { output: "session_id: ses_shouldBeIgnored" },
  };
  assert.equal(childSessionIdFromPart(part), undefined);
});

test("childSessionRef: live parent yields live child with child session id + provenance", () => {
  const parent: SessionRef = {
    mode: "live",
    runner_id: "run-1",
    instance_id: "inst-1",
    session_id: "ses_parent",
  };
  const ref = childSessionRef(parent, "ses_child");
  assert.deepEqual(ref, {
    mode: "live",
    runner_id: "run-1",
    instance_id: "inst-1",
    session_id: "ses_child",
    parent_session_id: "ses_parent",
  });
});

test("childSessionRef: history parent yields history child by id + provenance", () => {
  const parent: SessionRef = {
    mode: "history",
    runner_id: "run-2",
    session_id: "ses_parent2",
  };
  const ref = childSessionRef(parent, "ses_child2");
  assert.deepEqual(ref, {
    mode: "history",
    runner_id: "run-2",
    session_id: "ses_child2",
    parent_session_id: "ses_parent2",
  });
});

// ─── B. SessionRef parent/child round-trip addressing ────────────

test("childSessionRef: live round-trip addresses the parent again via parent_session_id", () => {
  const parent: SessionRef = {
    mode: "live",
    runner_id: "run-1",
    instance_id: "inst-1",
    session_id: "ses_parent",
  };
  const child = childSessionRef(parent, "ses_child");
  assert.equal(child.mode, "live");
  assert.equal(child.session_id, "ses_child");
  assert.equal(child.runner_id, "run-1");
  assert.equal(child.mode === "live" && child.instance_id, "inst-1");
  assert.equal(child.parent_session_id, "ses_parent");
  // The child carries enough provenance to re-address the parent: same
  // runner+instance, session_id = the recorded parent_session_id.
  assert.deepEqual(
    {
      mode: child.mode,
      runner_id: child.runner_id,
      instance_id: child.mode === "live" ? child.instance_id : undefined,
      session_id: child.parent_session_id,
    },
    {
      mode: "live",
      runner_id: "run-1",
      instance_id: "inst-1",
      session_id: "ses_parent",
    },
  );
});

test("childSessionRef: history parent preserves runner, carries no instance_id", () => {
  const parent: SessionRef = {
    mode: "history",
    runner_id: "run-2",
    session_id: "ses_parent",
  };
  const child = childSessionRef(parent, "ses_child");
  assert.equal(child.mode, "history");
  assert.equal(child.session_id, "ses_child");
  assert.equal(child.parent_session_id, "ses_parent");
  assert.equal(child.runner_id, "run-2");
  // A history ref has no instance_id in its shape.
  assert.equal((child as Record<string, unknown>).instance_id, undefined);
});

test("childSessionRef: subagent-of-subagent addressing round-trips", () => {
  const root: SessionRef = {
    mode: "live",
    runner_id: "run-1",
    instance_id: "inst-1",
    session_id: "ses_root",
  };
  const c1 = childSessionRef(root, "ses_c1");
  const c2 = childSessionRef(c1, "ses_c2");
  assert.equal(c2.session_id, "ses_c2");
  assert.equal(c2.parent_session_id, "ses_c1");
  assert.equal(c2.runner_id, "run-1");
  assert.equal(c2.mode === "live" && c2.instance_id, "inst-1");
  // c1's provenance still points at the root, so the whole chain is
  // walkable: c2.parent → c1.session, c1.parent → root.session.
  assert.equal(c1.parent_session_id, "ses_root");
});

// ─── C. subagentDrilldownState — pure gating decision ────────────

const LIVE_REF: SessionRef = {
  mode: "live",
  runner_id: "run-1",
  instance_id: "inst-1",
  session_id: "ses_parent",
};

test("subagentDrilldownState: eligible subagent renders the expand affordance", () => {
  const s = subagentDrilldownState(taskPart("ses_child"), LIVE_REF, [], 0, 8);
  assert.equal(s.childId, "ses_child");
  assert.equal(s.isSubagent, true);
  assert.equal(s.canDrill, true);
  assert.equal(s.capped, false);
  assert.equal(s.cappedReason, undefined);
});

test("subagentDrilldownState: child already in ancestors is capped as a cycle", () => {
  const s = subagentDrilldownState(
    taskPart("ses_child"),
    LIVE_REF,
    ["ses_child"],
    0,
    8,
  );
  assert.equal(s.canDrill, false);
  assert.equal(s.capped, true);
  assert.equal(s.cappedReason, "cycle");
});

test("subagentDrilldownState: depth === maxDepth is capped as max-depth", () => {
  const s = subagentDrilldownState(taskPart("ses_child"), LIVE_REF, [], 8, 8);
  assert.equal(s.canDrill, false);
  assert.equal(s.capped, true);
  assert.equal(s.cappedReason, "max-depth");
});

test("subagentDrilldownState: subagent with no resolvable child is not capped (Pi/no-child)", () => {
  // Empty output + no metadata.sessionId — a Pi task or a subagent whose
  // child id never surfaced. Renders without a drill-down, no error.
  const s = subagentDrilldownState(taskPart(undefined), LIVE_REF, [], 0, 8);
  assert.equal(s.isSubagent, true);
  assert.equal(s.childId, undefined);
  assert.equal(s.canDrill, false);
  assert.equal(s.capped, false);
  assert.equal(s.cappedReason, undefined);
});

test("subagentDrilldownState: a non-subagent tool part never drills", () => {
  const bash: OcPart = {
    id: "p",
    type: "tool",
    tool: "bash",
    state: { output: "session_id: ses_ignored" },
  };
  const s = subagentDrilldownState(bash, LIVE_REF, [], 0, 8);
  assert.equal(s.isSubagent, false);
  assert.equal(s.childId, undefined);
  assert.equal(s.canDrill, false);
  assert.equal(s.capped, false);
});

test("subagentDrilldownState: no sessionRef disables the drill-down entirely", () => {
  const s = subagentDrilldownState(taskPart("ses_child"), undefined, [], 0, 8);
  assert.equal(s.childId, undefined);
  assert.equal(s.canDrill, false);
  assert.equal(s.capped, false);
});

// ─── D. childSessionIdFromPart — targeted branch coverage ────────

test("childSessionIdFromPart: <task_metadata> block without a session_id line falls back to metadata", () => {
  const part: OcPart = {
    id: "p",
    type: "tool",
    tool: "task",
    state: {
      output: "<task_metadata>\nsummary: did stuff\n</task_metadata>",
      metadata: { sessionId: "ses_fallback" },
    },
  };
  assert.equal(childSessionIdFromPart(part), "ses_fallback");
});

test("childSessionIdFromPart: non-string output ignored, metadata still resolves", () => {
  const part = {
    id: "p",
    type: "tool",
    tool: "task",
    state: { output: 12345, metadata: { sessionId: "ses_meta" } },
  } as unknown as OcPart;
  assert.equal(childSessionIdFromPart(part), "ses_meta");
});

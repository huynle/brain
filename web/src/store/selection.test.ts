/**
 * store/selection — the "active row" state.
 *
 * The multi-select scope and its reducers are pinned in
 * lib/selection.test.ts. This file covers only the store-level
 * `active` field: the lightweight single-row highlight a plain click
 * sets, deliberately separate from the checkbox multi-select scope so
 * a single-click select never flips a checkbox when selection mode is
 * on. Invariants worth pinning:
 *   - setActive records the exact {projectId, kind, id}
 *   - setActive is idempotent (clicking the same row keeps it active —
 *     single-click must reliably SELECT, never toggle off)
 *   - setActive is independent of taskIds/featureIds/projectId scope
 *   - clear() drops the active row alongside the selection scope
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import { useSelection } from "./selection";

const INITIAL = useSelection.getState();

function resetStore() {
  useSelection.setState({
    projectId: INITIAL.projectId,
    taskIds: INITIAL.taskIds,
    featureIds: INITIAL.featureIds,
    anchor: INITIAL.anchor,
    verbRequest: INITIAL.verbRequest,
    active: INITIAL.active,
  });
}

// ─── initial ──────────────────────────────────────────────────────────

test("selection: active starts null", () => {
  resetStore();
  assert.equal(useSelection.getState().active, null);
});

// ─── setActive ────────────────────────────────────────────────────────

test("setActive records the exact projectId, kind, and id for a task", () => {
  resetStore();
  useSelection.getState().setActive("p1", "task", "t1");
  assert.deepEqual(useSelection.getState().active, {
    projectId: "p1",
    kind: "task",
    id: "t1",
  });
});

test("setActive records a feature active row", () => {
  resetStore();
  useSelection.getState().setActive("p1", "feature", "f1");
  assert.deepEqual(useSelection.getState().active, {
    projectId: "p1",
    kind: "feature",
    id: "f1",
  });
});

test("setActive is idempotent — the same row stays active, never toggles off", () => {
  // Single-click must reliably SELECT. Clicking the same active row
  // again keeps it active rather than clearing it.
  resetStore();
  const set = useSelection.getState().setActive;
  set("p1", "task", "t1");
  set("p1", "task", "t1");
  assert.deepEqual(useSelection.getState().active, {
    projectId: "p1",
    kind: "task",
    id: "t1",
  });
});

test("setActive replaces the previous active row (one active at a time)", () => {
  resetStore();
  const set = useSelection.getState().setActive;
  set("p1", "task", "t1");
  set("p1", "feature", "f2");
  assert.deepEqual(useSelection.getState().active, {
    projectId: "p1",
    kind: "feature",
    id: "f2",
  });
});

test("setActive does not touch the multi-select scope", () => {
  // The whole point of a separate field: single-click select must not
  // flip a checkbox or start selection mode.
  resetStore();
  useSelection.getState().toggleTask("p1", "t1"); // selection scope
  useSelection.getState().setActive("p1", "feature", "f9");
  const s = useSelection.getState();
  assert.equal(s.taskIds.has("t1"), true);
  assert.equal(s.featureIds.size, 0);
  assert.deepEqual(s.active, { projectId: "p1", kind: "feature", id: "f9" });
});

// ─── clear ────────────────────────────────────────────────────────────

test("clear() drops the active row along with the selection scope", () => {
  resetStore();
  const st = useSelection.getState();
  st.toggleTask("p1", "t1");
  st.setActive("p1", "task", "t1");
  useSelection.getState().clear();
  const s = useSelection.getState();
  assert.equal(s.active, null);
  assert.equal(s.projectId, null);
  assert.equal(s.taskIds.size, 0);
  assert.equal(s.featureIds.size, 0);
});

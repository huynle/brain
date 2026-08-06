/**
 * panes-v2 modal store — unit tests.
 *
 * The modal store is a thin state machine: one modal at a time,
 * carrying a `target` payload and an optional `tab`. Tests cover:
 *   - initial state (closed)
 *   - open(kind, target?, tab?)
 *   - close() clears everything
 *   - switchTab() only edits the tab field
 *   - open() while already open replaces the previous modal cleanly
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import { useModal } from "./modal";

const INITIAL = useModal.getState();

function resetStore() {
  useModal.setState({
    kind: INITIAL.kind,
    target: INITIAL.target,
    tab: INITIAL.tab,
  });
}

// ─── initial ──────────────────────────────────────────────────────────

test("modal: initial state is closed with no target or tab", () => {
  resetStore();
  const s = useModal.getState();
  assert.equal(s.kind, null);
  assert.equal(s.target, null);
  assert.equal(s.tab, null);
});

// ─── open ─────────────────────────────────────────────────────────────

test("modal: open() sets kind, target, and tab", () => {
  resetStore();
  useModal.getState().open("runner", { id: "r-1" }, "shell");
  const s = useModal.getState();
  assert.equal(s.kind, "runner");
  assert.deepEqual(s.target, { id: "r-1" });
  assert.equal(s.tab, "shell");
});

test("modal: open() without a tab leaves tab null", () => {
  resetStore();
  useModal.getState().open("task", { id: "t-6" });
  const s = useModal.getState();
  assert.equal(s.kind, "task");
  assert.deepEqual(s.target, { id: "t-6" });
  assert.equal(s.tab, null);
});

test("modal: open() without a target leaves target null", () => {
  resetStore();
  useModal.getState().open("settings");
  const s = useModal.getState();
  assert.equal(s.kind, "settings");
  assert.equal(s.target, null);
  assert.equal(s.tab, null);
});

// ─── open while open ──────────────────────────────────────────────────

test("modal: open() while already open replaces the previous modal", () => {
  resetStore();
  useModal.getState().open("runner", { id: "r-1" }, "shell");
  useModal.getState().open("task", { id: "t-6" }, "logs");
  const s = useModal.getState();
  assert.equal(s.kind, "task");
  assert.deepEqual(s.target, { id: "t-6" });
  assert.equal(s.tab, "logs");
});

// ─── close ────────────────────────────────────────────────────────────

test("modal: close() clears kind, target, and tab", () => {
  resetStore();
  useModal.getState().open("feature", { id: "f-1" }, "tasks");
  useModal.getState().close();
  const s = useModal.getState();
  assert.equal(s.kind, null);
  assert.equal(s.target, null);
  assert.equal(s.tab, null);
});

// ─── switchTab ────────────────────────────────────────────────────────

test("modal: switchTab() only edits tab field", () => {
  resetStore();
  useModal.getState().open("task", { id: "t-1" }, "detail");
  useModal.getState().switchTab("logs");
  const s = useModal.getState();
  assert.equal(s.kind, "task");
  assert.deepEqual(s.target, { id: "t-1" });
  assert.equal(s.tab, "logs");
});

test("modal: switchTab() on a closed modal is still allowed (records for a later open)", () => {
  // Deliberate: switching a tab while closed just records the tab field.
  // This is defensive — closed modals shouldn't accept a tab, but the
  // reducer doesn't enforce that; render layer already guards on kind.
  resetStore();
  useModal.getState().switchTab("orphan");
  const s = useModal.getState();
  assert.equal(s.kind, null);
  assert.equal(s.tab, "orphan");
});

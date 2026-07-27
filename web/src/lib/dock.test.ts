/**
 * panes-v2 dock tree — unit tests.
 *
 * Route T (TDD). These pure tree operations back the Focus workspace
 * (see Phase 7 spec). They must be side-effect-free and stable so the
 * React layer can call them in reducers without loops.
 *
 * All tests run under `node --test`, no DOM.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  addLeafAtEdge,
  findNodeInfo,
  firstLeaf,
  moveLeaf,
  newLeafNode,
  removeNode,
  replaceNode,
  setActiveTab,
  updateSplitRatio,
  walkLeaves,
  type DockLeaf,
  type DockNode,
} from "./dock";

// ─── helpers ──────────────────────────────────────────────────────────

function mkLeaf(title: string, url = "https://example.com"): DockLeaf {
  return { kind: "browser", target: { url }, title };
}

function assertLeaf(node: DockNode | undefined, title: string): void {
  assert.ok(node, "expected a node");
  assert.equal(node!.type, "leaf");
  if (node!.type === "leaf") {
    assert.equal(node.leaf.title, title);
  }
}

// ─── 1. newLeafNode ───────────────────────────────────────────────────

test("dock: newLeafNode produces a leaf with generated id", () => {
  const a = newLeafNode(mkLeaf("A"));
  const b = newLeafNode(mkLeaf("B"));
  assert.equal(a.type, "leaf");
  if (a.type === "leaf") {
    assert.equal(a.leaf.title, "A");
    assert.ok(a.id, "id should be non-empty");
  }
  // Fresh ids on distinct calls.
  assert.notEqual(a.id, b.id);
});

// ─── 2–5. addLeafAtEdge — edges around a leaf ─────────────────────────

test("dock: addLeafAtEdge — left on leaf produces split-row with new-left", () => {
  const target = newLeafNode(mkLeaf("Old"));
  const tree = addLeafAtEdge(target, target.id, "left", mkLeaf("New"));
  assert.equal(tree.type, "split");
  if (tree.type !== "split") return;
  assert.equal(tree.dir, "row");
  assert.equal(tree.ratio, 0.5);
  assert.equal(tree.children.length, 2);
  assertLeaf(tree.children[0], "New");
  assertLeaf(tree.children[1], "Old");
});

test("dock: addLeafAtEdge — right on leaf produces split-row with new-right", () => {
  const target = newLeafNode(mkLeaf("Old"));
  const tree = addLeafAtEdge(target, target.id, "right", mkLeaf("New"));
  assert.equal(tree.type, "split");
  if (tree.type !== "split") return;
  assert.equal(tree.dir, "row");
  assertLeaf(tree.children[0], "Old");
  assertLeaf(tree.children[1], "New");
});

test("dock: addLeafAtEdge — top on leaf produces split-col with new-top", () => {
  const target = newLeafNode(mkLeaf("Old"));
  const tree = addLeafAtEdge(target, target.id, "top", mkLeaf("New"));
  assert.equal(tree.type, "split");
  if (tree.type !== "split") return;
  assert.equal(tree.dir, "col");
  assertLeaf(tree.children[0], "New");
  assertLeaf(tree.children[1], "Old");
});

test("dock: addLeafAtEdge — bottom on leaf produces split-col with new-bottom", () => {
  const target = newLeafNode(mkLeaf("Old"));
  const tree = addLeafAtEdge(target, target.id, "bottom", mkLeaf("New"));
  assert.equal(tree.type, "split");
  if (tree.type !== "split") return;
  assert.equal(tree.dir, "col");
  assertLeaf(tree.children[0], "Old");
  assertLeaf(tree.children[1], "New");
});

// ─── 6–7. addLeafAtEdge — center (tabs) ───────────────────────────────

test("dock: addLeafAtEdge — center on leaf makes tabs with activeIdx=1", () => {
  const target = newLeafNode(mkLeaf("Old"));
  const tree = addLeafAtEdge(target, target.id, "center", mkLeaf("New"));
  assert.equal(tree.type, "tabs");
  if (tree.type !== "tabs") return;
  assert.equal(tree.activeIdx, 1);
  assert.equal(tree.children.length, 2);
  assert.equal(tree.children[0].leaf.title, "Old");
  assert.equal(tree.children[1].leaf.title, "New");
});

test("dock: addLeafAtEdge — center on tabs appends and focuses the new tab", () => {
  const a = newLeafNode(mkLeaf("A"));
  const withB = addLeafAtEdge(a, a.id, "center", mkLeaf("B")); // tabs [A, B]
  assert.equal(withB.type, "tabs");
  if (withB.type !== "tabs") return;

  const withC = addLeafAtEdge(withB, withB.id, "center", mkLeaf("C"));
  assert.equal(withC.type, "tabs");
  if (withC.type !== "tabs") return;
  assert.equal(withC.children.length, 3);
  assert.equal(withC.activeIdx, 2);
  assert.equal(withC.children[2].leaf.title, "C");
});

// ─── 8. removeNode — single-child split collapses ────────────────────

test("dock: removeNode collapses single-child split to remaining child", () => {
  const a = newLeafNode(mkLeaf("A"));
  const split = addLeafAtEdge(a, a.id, "right", mkLeaf("B"));
  assert.equal(split.type, "split");
  if (split.type !== "split") return;
  const bId = split.children[1].id;

  const next = removeNode(split, bId);
  assert.ok(next);
  // After removing B, only A remains — split should have been
  // collapsed into A itself.
  assert.equal(next!.type, "leaf");
  if (next!.type === "leaf") {
    assert.equal(next.leaf.title, "A");
  }
});

// ─── 9. removeNode — root ────────────────────────────────────────────

test("dock: removeNode on root leaf returns null", () => {
  const a = newLeafNode(mkLeaf("A"));
  const next = removeNode(a, a.id);
  assert.equal(next, null);
});

// ─── 10. removeNode — empty tabs cleaned up ─────────────────────────

test("dock: removeNode empties tabs down to the last leaf then removes tabs shell", () => {
  const a = newLeafNode(mkLeaf("A"));
  const withB = addLeafAtEdge(a, a.id, "center", mkLeaf("B")); // tabs [A, B]
  if (withB.type !== "tabs") throw new Error("expected tabs");

  // Remove B → tabs [A] should collapse to plain leaf A.
  const bId = withB.children[1].id;
  const step1 = removeNode(withB, bId);
  assert.ok(step1);
  assert.equal(step1!.type, "leaf");
  if (step1!.type === "leaf") assert.equal(step1.leaf.title, "A");

  // Remove the remaining A → whole tree gone.
  const step2 = removeNode(step1!, (step1! as { id: string }).id);
  assert.equal(step2, null);
});

// ─── 11. moveLeaf — cross-pane ───────────────────────────────────────

test("dock: moveLeaf removes from source and re-adds at target edge", () => {
  const a = newLeafNode(mkLeaf("A"));
  const abSplit = addLeafAtEdge(a, a.id, "right", mkLeaf("B")); // row [A, B]
  if (abSplit.type !== "split") throw new Error("expected split");
  const aId = abSplit.children[0].id;
  const bId = abSplit.children[1].id;

  // Move A to the bottom of B — resulting shape:
  // The single remaining sibling is B in a col-split [B, A].
  const moved = moveLeaf(abSplit, aId, bId, "bottom");
  assert.equal(moved.type, "split");
  if (moved.type !== "split") return;
  assert.equal(moved.dir, "col");
  assert.equal(moved.children.length, 2);
  assertLeaf(moved.children[0], "B");
  assertLeaf(moved.children[1], "A");
});

// ─── 12. moveLeaf — no-op guards ─────────────────────────────────────

test("dock: moveLeaf with source == target is a no-op", () => {
  const a = newLeafNode(mkLeaf("A"));
  const ab = addLeafAtEdge(a, a.id, "right", mkLeaf("B"));
  const bId = ab.type === "split" ? ab.children[1].id : "";

  const moved = moveLeaf(ab, bId, bId, "center");
  // Same shape, same title order.
  assert.equal(moved.type, "split");
  if (moved.type !== "split") return;
  assertLeaf(moved.children[0], "A");
  assertLeaf(moved.children[1], "B");
});

test("dock: moveLeaf into itself via center is a no-op (invalid drop)", () => {
  const a = newLeafNode(mkLeaf("A"));
  const same = moveLeaf(a, a.id, a.id, "center");
  assert.equal(same.type, "leaf");
  if (same.type !== "leaf") return;
  assert.equal(same.leaf.title, "A");
});

// ─── 13. updateSplitRatio — clamps ───────────────────────────────────

test("dock: updateSplitRatio clamps below 0.1 and above 0.9", () => {
  const a = newLeafNode(mkLeaf("A"));
  const split = addLeafAtEdge(a, a.id, "right", mkLeaf("B"));
  if (split.type !== "split") throw new Error("expected split");

  const low = updateSplitRatio(split, split.id, 0.01);
  const high = updateSplitRatio(split, split.id, 0.99);
  const mid = updateSplitRatio(split, split.id, 0.42);

  if (low.type !== "split" || high.type !== "split" || mid.type !== "split")
    throw new Error("expected split");
  assert.equal(low.ratio, 0.1);
  assert.equal(high.ratio, 0.9);
  assert.equal(mid.ratio, 0.42);
});

// ─── 14. setActiveTab — bounds ───────────────────────────────────────

test("dock: setActiveTab honors bounds (out-of-range is a no-op)", () => {
  const a = newLeafNode(mkLeaf("A"));
  const tabs = addLeafAtEdge(a, a.id, "center", mkLeaf("B"));
  if (tabs.type !== "tabs") throw new Error("expected tabs");

  const tooHigh = setActiveTab(tabs, tabs.id, 42);
  const negative = setActiveTab(tabs, tabs.id, -1);
  const ok = setActiveTab(tabs, tabs.id, 0);

  if (tooHigh.type !== "tabs" || negative.type !== "tabs" || ok.type !== "tabs")
    throw new Error("expected tabs");
  assert.equal(tooHigh.activeIdx, 1); // unchanged
  assert.equal(negative.activeIdx, 1); // unchanged
  assert.equal(ok.activeIdx, 0);
});

// ─── 15. walkLeaves — DFS order ──────────────────────────────────────

test("dock: walkLeaves visits leaves in DFS order", () => {
  // Build [ [A, B]  (row)  ,  C ] — outer row split.
  const a = newLeafNode(mkLeaf("A"));
  const ab = addLeafAtEdge(a, a.id, "right", mkLeaf("B"));
  const abc = addLeafAtEdge(ab, ab.id, "right", mkLeaf("C"));

  const order: string[] = [];
  walkLeaves(abc, (leaf) => {
    order.push(leaf.title);
  });
  assert.deepEqual(order, ["A", "B", "C"]);
});

// ─── extra: firstLeaf ────────────────────────────────────────────────

test("dock: firstLeaf returns the first leaf in DFS order", () => {
  const a = newLeafNode(mkLeaf("A"));
  const tree = addLeafAtEdge(a, a.id, "right", mkLeaf("B"));
  const first = firstLeaf(tree);
  assert.ok(first);
  assert.equal(first!.title, "A");
});

// ─── extra: findNodeInfo ─────────────────────────────────────────────

test("dock: findNodeInfo returns node + parent + index", () => {
  const a = newLeafNode(mkLeaf("A"));
  const tree = addLeafAtEdge(a, a.id, "right", mkLeaf("B"));
  if (tree.type !== "split") throw new Error("expected split");
  const bId = tree.children[1].id;

  const info = findNodeInfo(tree, bId);
  assert.ok(info);
  assert.equal(info!.node.id, bId);
  assert.equal(info!.parent?.id, tree.id);
  assert.equal(info!.index, 1);

  // Root has no parent.
  const rootInfo = findNodeInfo(tree, tree.id);
  assert.ok(rootInfo);
  assert.equal(rootInfo!.parent, null);
  assert.equal(rootInfo!.index, 0);
});

// ─── extra: replaceNode ──────────────────────────────────────────────

test("dock: replaceNode swaps subtree in place", () => {
  const a = newLeafNode(mkLeaf("A"));
  const tree = addLeafAtEdge(a, a.id, "right", mkLeaf("B"));
  if (tree.type !== "split") throw new Error("expected split");
  const bId = tree.children[1].id;
  const replacement = newLeafNode(mkLeaf("REPLACED"));

  const next = replaceNode(tree, bId, replacement);
  assert.equal(next.type, "split");
  if (next.type !== "split") return;
  assertLeaf(next.children[0], "A");
  assertLeaf(next.children[1], "REPLACED");
});

// ─── extra: nested add/remove smoke ──────────────────────────────────

test("dock: moving one of three siblings between panes keeps other two intact", () => {
  const a = newLeafNode(mkLeaf("A"));
  const ab = addLeafAtEdge(a, a.id, "right", mkLeaf("B"));
  if (ab.type !== "split") throw new Error("expected split");
  const abc = addLeafAtEdge(ab, ab.children[1].id, "right", mkLeaf("C"));
  // Tree shape: row [ A , row [ B , C ] ]

  // Move C onto A center → tabs [A, C]; B remains as sibling.
  if (abc.type !== "split") throw new Error("expected split");
  const outerA = abc.children[0];
  const innerSplit = abc.children[1];
  if (innerSplit.type !== "split") throw new Error("expected inner split");
  const cId = innerSplit.children[1].id;

  const moved = moveLeaf(abc, cId, outerA.id, "center");
  const titles: string[] = [];
  walkLeaves(moved, (l) => titles.push(l.title));
  // A is now merged with C into tabs; B still there.
  assert.deepEqual(titles.sort(), ["A", "B", "C"]);
});

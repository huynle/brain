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
  addNodeAtEdge,
  findNodeInfo,
  firstLeaf,
  isDockLeafKind,
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

function titles(tree: DockNode): string[] {
  const out: string[] = [];
  walkLeaves(tree, (l) => out.push(l.title));
  return out;
}

/**
 * Build `split(row)[ tabs[A, B], D ]` — the shape a dock reaches as
 * soon as it holds three things, and the one every drop used to be
 * silently discarded by (see the tabs-strip tests below). Returns the
 * tree plus the ids a drop target would actually carry: `PaneTabs`
 * renders `<PaneLeaf id={activeTab.id}/>`, so drop zones are stamped
 * with a LEAF id inside the strip, never the strip's own id.
 */
function tabsInSplit(): {
  tree: DockNode;
  tabsId: string;
  aId: string;
  bId: string;
  dId: string;
} {
  const a = newLeafNode(mkLeaf("A"));
  const tabs = addLeafAtEdge(a, a.id, "center", mkLeaf("B")); // tabs [A, B]
  const tree = addLeafAtEdge(tabs, tabs.id, "right", mkLeaf("D"));
  if (tree.type !== "split") throw new Error("expected split");
  const tabsNode = tree.children[0];
  if (tabsNode.type !== "tabs") throw new Error("expected tabs");
  return {
    tree,
    tabsId: tabsNode.id,
    aId: tabsNode.children[0].id,
    bId: tabsNode.children[1].id,
    dId: tree.children[1].id,
  };
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

// ─── regression: inserting AT a leaf that lives inside a tabs strip ───
// `replaceNode` can only substitute a tab child with another LEAF, but
// every shape `addNodeAtEdge` builds is a container. It used to hand
// one over anyway and the insertion was silently dropped — so once a
// dock held two items (i.e. was a tab strip) EVERY drop zone on it, and
// every "open in sidebar", stopped doing anything at all.

test("dock: addLeafAtEdge — center on a leaf inside tabs appends to that strip", () => {
  const { tree, aId, dId } = tabsInSplit();
  const next = addLeafAtEdge(tree, aId, "center", mkLeaf("NEW"));

  assert.deepEqual(titles(next), ["A", "B", "NEW", "D"]);
  if (next.type !== "split") return assert.fail("expected split root");
  const strip = next.children[0];
  assert.equal(strip.type, "tabs");
  if (strip.type === "tabs") {
    assert.equal(strip.children.length, 3);
    assert.equal(strip.children[2].leaf.title, "NEW");
    // The freshly added tab is the one you see.
    assert.equal(strip.activeIdx, 2);
  }
  // The sibling pane is untouched, id and all.
  assert.equal(next.children[1].id, dId);
});

test("dock: addLeafAtEdge — an edge on a leaf inside tabs splits the whole strip", () => {
  for (const edge of ["left", "right", "top", "bottom"] as const) {
    const { tree, aId, tabsId } = tabsInSplit();
    const next = addLeafAtEdge(tree, aId, edge, mkLeaf("NEW"));

    assert.equal(titles(next).length, 4, `${edge}: NEW must be present`);
    assert.ok(titles(next).includes("NEW"), `${edge}: NEW must be present`);
    if (next.type !== "split") return assert.fail("expected split root");
    const wrapper = next.children[0];
    assert.equal(wrapper.type, "split", `${edge}: strip should be wrapped`);
    if (wrapper.type !== "split") return;
    assert.equal(wrapper.dir, edge === "left" || edge === "right" ? "row" : "col");
    // The strip itself survives intact as one side of the new split —
    // "beside the tab I'm looking at" means beside its whole group.
    const newFirst = edge === "left" || edge === "top";
    const stripSide = wrapper.children[newFirst ? 1 : 0];
    const newSide = wrapper.children[newFirst ? 0 : 1];
    assert.equal(stripSide.id, tabsId, `${edge}: strip kept its identity`);
    assertLeaf(newSide, "NEW");
  }
});

// ─── addNodeAtEdge: the caller keeps the new node's id ───────────────

test("dock: addNodeAtEdge places the caller's own node, id intact", () => {
  const { tree, dId } = tabsInSplit();
  const node = newLeafNode(mkLeaf("NEW"));

  const next = addNodeAtEdge(tree, dId, "bottom", node);
  const found = findNodeInfo(next, node.id);
  assert.ok(found, "the caller's id must address the placed node");
  assert.equal(found!.node.type, "leaf");
  if (found!.node.type === "leaf") {
    assert.equal(found!.node.leaf.title, "NEW");
  }
});

test("dock: addNodeAtEdge on a missing target returns the identical tree", () => {
  const { tree } = tabsInSplit();
  const next = addNodeAtEdge(tree, "leaf_gone", "left", newLeafNode(mkLeaf("N")));
  assert.equal(next, tree, "no-op must be reference-equal so stores can detect it");
});

// ─── regression: moveLeaf must never lose the pane it is moving ───────
// moveLeaf is remove-then-add. Before the tabs fix its add step no-opped
// against any tabbed target, so the already-removed pane vanished for
// good — silent, unrecoverable data loss on a routine drag.

test("dock: moveLeaf onto a leaf inside tabs preserves the moved pane, every edge", () => {
  for (const edge of ["center", "left", "right", "top", "bottom"] as const) {
    const { tree, aId, dId } = tabsInSplit();
    const moved = moveLeaf(tree, dId, aId, edge);
    assert.deepEqual(
      titles(moved).sort(),
      ["A", "B", "D"],
      `${edge}: no pane may be created or destroyed`,
    );
    assert.ok(findNodeInfo(moved, dId), `${edge}: the moved pane keeps its id`);
  }
});

test("dock: moveLeaf preserves the leaf count across every edge and target", () => {
  const { tree, tabsId, aId, bId, dId } = tabsInSplit();
  for (const targetId of [tabsId, aId, bId, dId, tree.id]) {
    for (const source of [aId, bId, dId]) {
      for (const edge of ["center", "left", "right", "top", "bottom"] as const) {
        const moved = moveLeaf(tree, source, targetId, edge);
        assert.equal(
          titles(moved).length,
          3,
          `source=${source} target=${targetId} edge=${edge} changed the leaf count`,
        );
      }
    }
  }
});

test("dock: moveLeaf retargets to the survivor when the target collapses", () => {
  // PaneTabs' "Split right" verb: move a tab out of its own 2-tab strip,
  // targeting the STRIP. Removing the source collapses the strip into
  // its sibling, so the target id no longer exists. Falling back to the
  // root (the old behaviour) flung the pane to the far edge of the whole
  // dock and reparented everything else; it must land beside its former
  // tab-mate instead.
  const { tree, tabsId, aId, dId } = tabsInSplit();
  const moved = moveLeaf(tree, aId, tabsId, "right");

  assert.equal(moved.type, "split");
  if (moved.type !== "split") return;
  // D keeps its slot in the outer split — it was never involved.
  assert.equal(moved.children[1].id, dId);
  const inner = moved.children[0];
  assert.equal(inner.type, "split", "A should sit beside B, not beside the root");
  if (inner.type !== "split") return;
  assert.equal(inner.dir, "row");
  assertLeaf(inner.children[0], "B");
  assertLeaf(inner.children[1], "A");
});

// ─── isDockLeafKind ──────────────────────────────────────────────────

test("dock: isDockLeafKind accepts every leaf kind and rejects assign", () => {
  for (const kind of [
    "task-detail",
    "feature-detail",
    "logs",
    "session",
    "runners",
    "browser",
    "entry",
  ]) {
    assert.equal(isDockLeafKind(kind), true, `${kind} should be a leaf kind`);
  }
  // The feature→runner drag payload. Drop targets gate their zones on
  // this, so a mistake here means panes advertise a drop they refuse.
  assert.equal(isDockLeafKind("assign"), false);
  assert.equal(isDockLeafKind(""), false);
});

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

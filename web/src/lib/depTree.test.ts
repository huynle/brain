/**
 * Tests for lib/depTree — pure dependency-forest construction.
 *
 * These pin the semantics the PWA shares with `internal/tui/tasktree.go`:
 * dependency edges point dep → dependent, `parent_id` outranks a dep
 * edge for placement, diamonds render once, cycles degrade to flagged
 * roots, and dangling refs are ignored rather than dropping rows.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  buildDepForest,
  flattenDepForest,
  countDepNodes,
  hasDepEdges,
  type DepNode,
} from "./depTree";

interface Item {
  id: string;
  deps?: string[];
  parent?: string;
  rank?: number;
}

function forest(items: Item[]): DepNode<Item>[] {
  return buildDepForest(items, {
    id: (i) => i.id,
    deps: (i) => i.deps,
    parent: (i) => i.parent,
    rank: (i, idx) => i.rank ?? idx,
  });
}

/** "a > b > c" style shape dump — one line per row, prefix stripped. */
function shape(roots: DepNode<Item>[]): string[] {
  return flattenDepForest(roots).map(
    (r) => `${"  ".repeat(r.depth)}${r.node.id}${r.node.inCycle ? " ↺" : ""}`,
  );
}

test("empty input yields an empty forest", () => {
  assert.deepEqual(forest([]), []);
});

test("items with no deps are all roots, in rank order", () => {
  const roots = forest([{ id: "c" }, { id: "a" }, { id: "b" }]);
  assert.deepEqual(
    roots.map((r) => r.id),
    ["c", "a", "b"],
  );
  assert.equal(hasDepEdges(roots), false);
});

test("a dependency becomes the parent of its dependent", () => {
  // B depends on A → A is the root, B hangs under it.
  const roots = forest([{ id: "a" }, { id: "b", deps: ["a"] }]);
  assert.deepEqual(shape(roots), ["a", "  b"]);
  assert.equal(hasDepEdges(roots), true);
});

test("a dependency chain nests to full depth", () => {
  const roots = forest([
    { id: "a" },
    { id: "b", deps: ["a"] },
    { id: "c", deps: ["b"] },
    { id: "d", deps: ["c"] },
  ]);
  assert.deepEqual(shape(roots), ["a", "  b", "    c", "      d"]);
  assert.equal(countDepNodes(roots), 4);
});

test("input order does not matter — dependents find their parent", () => {
  const roots = forest([
    { id: "d", deps: ["c"] },
    { id: "b", deps: ["a"] },
    { id: "c", deps: ["b"] },
    { id: "a" },
  ]);
  assert.deepEqual(shape(roots), ["a", "  b", "    c", "      d"]);
});

test("a dangling dep ref is ignored and the item stays a root", () => {
  // "ghost" is not in the set (filtered out / deleted / other project).
  const roots = forest([{ id: "a" }, { id: "b", deps: ["ghost"] }]);
  assert.deepEqual(shape(roots), ["a", "b"]);
});

test("a self-dep is ignored rather than creating a loop", () => {
  const roots = forest([{ id: "a", deps: ["a"] }]);
  assert.deepEqual(shape(roots), ["a"]);
  assert.equal(roots[0].inCycle, false);
});

test("a diamond renders each node exactly once", () => {
  //   a
  //  / \
  // b   c
  //  \ /
  //   d      — d depends on both b and c.
  const roots = forest([
    { id: "a" },
    { id: "b", deps: ["a"] },
    { id: "c", deps: ["a"] },
    { id: "d", deps: ["b", "c"] },
  ]);
  const rows = flattenDepForest(roots);
  assert.equal(rows.length, 4, "one row per item, no duplication");
  assert.equal(countDepNodes(roots), 4);
  // d is placed under its first in-set dep (b); c is recorded as extra.
  const d = rows.find((r) => r.node.id === "d");
  assert.ok(d);
  assert.deepEqual(d.node.extraDeps, ["c"]);
  assert.deepEqual(shape(roots), ["a", "  b", "    d", "  c"]);
});

test("parent_id outranks a dep edge for placement", () => {
  // b depends on a, but is explicitly parented to p.
  const roots = forest([
    { id: "p" },
    { id: "a" },
    { id: "b", deps: ["a"], parent: "p" },
  ]);
  assert.deepEqual(shape(roots), ["p", "  b", "a"]);
  const b = flattenDepForest(roots).find((r) => r.node.id === "b");
  assert.deepEqual(b?.node.extraDeps, ["a"], "losing dep edge is retained");
});

test("an out-of-set parent_id falls back to the dep edge", () => {
  const roots = forest([{ id: "a" }, { id: "b", deps: ["a"], parent: "gone" }]);
  assert.deepEqual(shape(roots), ["a", "  b"]);
});

test("a 2-cycle degrades to flagged roots instead of hanging", () => {
  const roots = forest([
    { id: "a", deps: ["b"] },
    { id: "b", deps: ["a"] },
  ]);
  assert.equal(roots.length, 2);
  assert.ok(roots.every((r) => r.inCycle));
  assert.deepEqual(shape(roots).sort(), ["a ↺", "b ↺"]);
});

test("a 3-cycle flags every member and renders each once", () => {
  const roots = forest([
    { id: "a", deps: ["c"] },
    { id: "b", deps: ["a"] },
    { id: "c", deps: ["b"] },
  ]);
  assert.equal(countDepNodes(roots), 3);
  assert.ok(flattenDepForest(roots).every((r) => r.node.inCycle));
});

test("a clean subtree hanging off a cycle is still rendered", () => {
  const roots = forest([
    { id: "a", deps: ["b"] },
    { id: "b", deps: ["a"] },
    { id: "leaf", deps: ["a"] },
  ]);
  assert.equal(countDepNodes(roots), 3, "no item is lost to the cycle");
  const rows = flattenDepForest(roots);
  const leaf = rows.find((r) => r.node.id === "leaf");
  assert.ok(leaf);
  assert.equal(leaf.node.inCycle, false, "leaf is not part of the cycle");
  assert.equal(leaf.depth, 1, "leaf still hangs under its dep");
});

test("duplicate ids collapse to the first occurrence", () => {
  const roots = forest([{ id: "a" }, { id: "a" }]);
  assert.equal(countDepNodes(roots), 1);
});

test("siblings sort by rank", () => {
  const roots = forest([
    { id: "a" },
    { id: "hi", deps: ["a"], rank: 0 },
    { id: "lo", deps: ["a"], rank: 9 },
    { id: "mid", deps: ["a"], rank: 5 },
  ]);
  assert.deepEqual(
    roots[0].children.map((c) => c.id),
    ["hi", "mid", "lo"],
  );
});

test("a sibling that depends on a sibling still sorts after it", () => {
  // Both are parented to p, so the dep edge cannot place them; the
  // Kahn pass must still order first → second despite the ranks.
  const roots = forest([
    { id: "p" },
    { id: "second", parent: "p", deps: ["first"], rank: 0 },
    { id: "first", parent: "p", rank: 9 },
  ]);
  assert.deepEqual(
    roots[0].children.map((c) => c.id),
    ["first", "second"],
  );
});

test("box-drawing prefixes mark branch, last-branch and vertical guides", () => {
  const roots = forest([
    { id: "root" },
    { id: "x", deps: ["root"] },
    { id: "y", deps: ["root"] },
    { id: "x1", deps: ["x"] },
  ]);
  const rows = flattenDepForest(roots);
  const byId = Object.fromEntries(rows.map((r) => [r.node.id, r]));
  assert.equal(byId.root.prefix, "", "roots carry no guide");
  assert.equal(byId.x.prefix, "├─", "non-final child");
  assert.equal(byId.y.prefix, "└─", "final child");
  // x1 is x's only child; x is not last, so the guide continues.
  assert.equal(byId.x1.prefix, "│ └─");
});

test("deep chains do not overflow the stack", () => {
  const items: Item[] = [{ id: "n0" }];
  for (let i = 1; i < 5000; i++) {
    items.push({ id: `n${i}`, deps: [`n${i - 1}`] });
  }
  const roots = buildDepForest(items, {
    id: (i) => i.id,
    deps: (i) => i.deps,
  });
  assert.equal(roots.length, 1, "single chain has a single root");
  assert.equal(roots[0].inCycle, false);
});

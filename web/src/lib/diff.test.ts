/**
 * diff.ts — line diff tests (node --test, no DOM).
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import { diffLines, diffStats, type DiffRow } from "./diff";

function render(rows: DiffRow[]): string[] {
  return rows.map(
    (r) => `${r.kind === "add" ? "+" : r.kind === "del" ? "-" : " "}${r.text}`,
  );
}

test("diff: identical inputs are all same-rows with both line numbers", () => {
  const rows = diffLines("a\nb\nc", "a\nb\nc");
  assert.ok(rows.every((r) => r.kind === "same"));
  assert.deepEqual(
    rows.map((r) => [r.aLine, r.bLine]),
    [
      [1, 1],
      [2, 2],
      [3, 3],
    ],
  );
});

test("diff: single line change in the middle", () => {
  const rows = diffLines("a\nb\nc", "a\nX\nc");
  assert.deepEqual(render(rows), [" a", "-b", "+X", " c"]);
  const del = rows.find((r) => r.kind === "del")!;
  const add = rows.find((r) => r.kind === "add")!;
  assert.equal(del.aLine, 2);
  assert.equal(del.bLine, undefined);
  assert.equal(add.bLine, 2);
  assert.equal(add.aLine, undefined);
});

test("diff: pure insertion and pure deletion", () => {
  assert.deepEqual(render(diffLines("a\nc", "a\nb\nc")), [" a", "+b", " c"]);
  assert.deepEqual(render(diffLines("a\nb\nc", "a\nc")), [" a", "-b", " c"]);
});

test("diff: empty versus content", () => {
  // "" splits to [""], so empty-vs-x is a one-line replace.
  assert.deepEqual(render(diffLines("", "x")), ["-", "+x"]);
  assert.deepEqual(render(diffLines("", "")), [" "]);
});

test("diff: preserves correct absolute line numbers after prefix trim", () => {
  const a = ["p1", "p2", "old", "s1"].join("\n");
  const b = ["p1", "p2", "new", "extra", "s1"].join("\n");
  const rows = diffLines(a, b);
  assert.deepEqual(render(rows), [" p1", " p2", "-old", "+new", "+extra", " s1"]);
  const suffix = rows[rows.length - 1];
  assert.equal(suffix.aLine, 4);
  assert.equal(suffix.bLine, 5);
  const del = rows.find((r) => r.kind === "del")!;
  assert.equal(del.aLine, 3);
});

test("diff: LCS keeps the longest common structure", () => {
  const rows = diffLines("a\nb\nc\nd", "b\nc\nd\ne");
  assert.deepEqual(render(rows), ["-a", " b", " c", " d", "+e"]);
});

test("diffStats counts row kinds", () => {
  const rows = diffLines("a\nb\nc", "a\nX\nc\nd");
  assert.deepEqual(diffStats(rows), { added: 2, removed: 1, unchanged: 2 });
});

test("diff: oversize inputs degrade to replace block without hanging", () => {
  // 3000×3000 distinct middle lines exceeds the DP cap.
  const a = Array.from({ length: 3000 }, (_, i) => `a${i}`).join("\n");
  const b = Array.from({ length: 3000 }, (_, i) => `b${i}`).join("\n");
  const rows = diffLines(`same\n${a}\nsame`, `same\n${b}\nsame`);
  const stats = diffStats(rows);
  assert.equal(stats.unchanged, 2);
  assert.equal(stats.removed, 3000);
  assert.equal(stats.added, 3000);
});

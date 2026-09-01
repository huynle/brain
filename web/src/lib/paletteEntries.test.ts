import test from "node:test";
import assert from "node:assert/strict";
import {
  MAX_ENTRY_HITS,
  entryHitLabel,
  entryHitTitle,
  isExactEntryHit,
  rankEntryHits,
} from "./paletteEntries";
import type { SearchResult } from "./types";

function hit(over: Partial<SearchResult> = {}): SearchResult {
  return {
    id: "zg1ccngo",
    path: "global/automation/zg1ccngo.md",
    title: "Built-in feature checkout",
    type: "automation",
    status: "active",
    snippet: "",
    ...over,
  };
}

test("isExactEntryHit matches the short id, case-insensitively", () => {
  assert.equal(isExactEntryHit("zg1ccngo", hit()), true);
  assert.equal(isExactEntryHit("  ZG1CCNGO  ", hit()), true);
  assert.equal(isExactEntryHit("zg1ccng", hit()), false);
});

test("isExactEntryHit matches the path with or without .md", () => {
  const h = hit({ path: "projects/brain/plan/ab12cd34.md" });
  assert.equal(isExactEntryHit("projects/brain/plan/ab12cd34.md", h), true);
  assert.equal(isExactEntryHit("projects/brain/plan/ab12cd34", h), true);
  assert.equal(isExactEntryHit("projects/brain/plan", h), false);
});

test("isExactEntryHit matches an exact title", () => {
  assert.equal(isExactEntryHit("built-in feature checkout", hit()), true);
  // Substring of the title is a search hit, not an exact one.
  assert.equal(isExactEntryHit("feature checkout", hit()), false);
});

test("isExactEntryHit ignores an empty query", () => {
  assert.equal(isExactEntryHit("   ", hit()), false);
  // An empty title must not make every hit "exact".
  assert.equal(isExactEntryHit("", hit({ title: "" })), false);
});

test("rankEntryHits pins the named entry above better-scoring noise", () => {
  const wanted = hit({ id: "aaaaaaaa", path: "global/note/aaaaaaaa.md" });
  const noise = hit({ id: "bbbbbbbb", path: "global/note/bbbbbbbb.md" });
  const other = hit({ id: "cccccccc", path: "global/note/cccccccc.md" });

  // Server ranked the exact id third; the palette must not.
  const ranked = rankEntryHits("aaaaaaaa", [noise, other, wanted]);
  assert.deepEqual(
    ranked.map((r) => r.id),
    ["aaaaaaaa", "bbbbbbbb", "cccccccc"],
  );
});

test("rankEntryHits preserves server order among non-exact hits", () => {
  const hits = ["a", "b", "c"].map((c) =>
    hit({ id: c.repeat(8), path: `global/note/${c.repeat(8)}.md` }),
  );
  assert.deepEqual(
    rankEntryHits("checkout", hits).map((r) => r.id),
    ["aaaaaaaa", "bbbbbbbb", "cccccccc"],
  );
});

test("rankEntryHits caps the list", () => {
  const hits = Array.from({ length: MAX_ENTRY_HITS + 5 }, (_, i) =>
    hit({ id: `id${i}`, path: `global/note/id${i}.md` }),
  );
  assert.equal(rankEntryHits("note", hits).length, MAX_ENTRY_HITS);
});

test("rankEntryHits keeps the exact hit even when the cap would drop it", () => {
  const hits = Array.from({ length: MAX_ENTRY_HITS + 5 }, (_, i) =>
    hit({ id: `id${i}`, path: `global/note/id${i}.md` }),
  );
  const last = hits[hits.length - 1];
  const ranked = rankEntryHits(last.id, hits);
  assert.equal(ranked[0].id, last.id);
  assert.equal(ranked.length, MAX_ENTRY_HITS);
});

test("entryHitTitle falls back to the path basename", () => {
  assert.equal(entryHitTitle(hit({ title: "" })), "zg1ccngo");
  assert.equal(entryHitTitle(hit()), "Built-in feature checkout");
});

test("entryHitLabel names the project a hit lives in", () => {
  assert.equal(
    entryHitLabel(hit({ path: "projects/brain/plan/ab12cd34.md" })),
    "Entry: Built-in feature checkout (brain)",
  );
  assert.equal(
    entryHitLabel(hit()),
    "Entry: Built-in feature checkout (global)",
  );
});

test("entryHitLabel drops the parenthetical when the path names no project", () => {
  assert.equal(
    entryHitLabel(hit({ path: "loose.md", title: "Loose" })),
    "Entry: Loose",
  );
});

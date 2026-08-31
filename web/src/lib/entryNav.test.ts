/**
 * Entry ↔ URL helpers — unit tests (node --test, no DOM).
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import { entryHref, entryRefFromSearch, searchWithEntry } from "./entryNav";

test("entryNav: reads the entry ref out of a search string", () => {
  assert.equal(entryRefFromSearch("?entry=ab12cd34"), "ab12cd34");
  assert.equal(
    entryRefFromSearch("?entry=projects%2Fx%2Fplan%2Fab12cd34.md"),
    "projects/x/plan/ab12cd34.md",
  );
  assert.equal(entryRefFromSearch("?a=1&entry=ab12cd34&b=2"), "ab12cd34");
});

test("entryNav: no ref reads as null, never as an empty selection", () => {
  assert.equal(entryRefFromSearch(""), null);
  assert.equal(entryRefFromSearch("?other=1"), null);
  assert.equal(entryRefFromSearch("?entry="), null);
  assert.equal(entryRefFromSearch("?entry=%20"), null);
});

test("entryNav: writing a ref preserves the other params", () => {
  assert.equal(searchWithEntry("?tab=logs", "ab12cd34"), "?tab=logs&entry=ab12cd34");
  assert.equal(searchWithEntry("?entry=old&tab=logs", "new"), "?entry=new&tab=logs");
});

test("entryNav: clearing the selection drops the param, and the ? with it", () => {
  assert.equal(searchWithEntry("?entry=ab12cd34", null), "");
  assert.equal(searchWithEntry("?entry=ab12cd34&tab=logs", null), "?tab=logs");
  assert.equal(searchWithEntry("", null), "");
});

test("entryNav: a written ref round-trips, so the sync can't loop", () => {
  for (const ref of [
    "ab12cd34",
    "projects/x/plan/ab12cd34.md",
    "global/note/a b.md",
    "projects/x/note/é+&=?.md",
  ]) {
    assert.equal(entryRefFromSearch(searchWithEntry("", ref)), ref, ref);
  }
});

test("entryNav: link hrefs are a real URL for ⌘-click", () => {
  assert.equal(entryHref("ab12cd34"), "?entry=ab12cd34");
  assert.equal(
    entryHref("projects/x/plan/ab12cd34.md"),
    "?entry=projects%2Fx%2Fplan%2Fab12cd34.md",
  );
});

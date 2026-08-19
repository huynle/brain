/**
 * Entries store — unit tests (node --test, no DOM).
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import { useEntriesStore, ENTRIES_STORAGE_KEY } from "./entries";

const INITIAL = useEntriesStore.getState();

function resetStore() {
  useEntriesStore.setState({
    typeFilter: INITIAL.typeFilter,
    projectFilter: INITIAL.projectFilter,
    statusFilter: INITIAL.statusFilter,
    sortBy: INITIAL.sortBy,
    sortOrder: INITIAL.sortOrder,
    query: "",
    strategy: INITIAL.strategy,
    selectedPath: null,
    comparePins: [],
    compareOpen: false,
    diffMode: false,
  });
}

test("entries: persisted storage key is versioned", () => {
  assert.equal(ENTRIES_STORAGE_KEY, "panes-v2:entries:v1");
});

test("entries: defaults browse knowledge types, modified desc", () => {
  resetStore();
  const s = useEntriesStore.getState();
  assert.equal(s.typeFilter, "knowledge");
  assert.equal(s.sortBy, "modified");
  assert.equal(s.sortOrder, "desc");
  assert.equal(s.selectedPath, null);
});

test("entries: togglePin fills two slots then replaces the second", () => {
  resetStore();
  const st = () => useEntriesStore.getState();
  st().togglePin("a.md");
  st().togglePin("b.md");
  assert.deepEqual(st().comparePins, ["a.md", "b.md"]);
  st().togglePin("c.md");
  assert.deepEqual(st().comparePins, ["a.md", "c.md"]);
});

test("entries: togglePin unpins and closes an open compare", () => {
  resetStore();
  const st = () => useEntriesStore.getState();
  st().togglePin("a.md");
  st().togglePin("b.md");
  st().setCompareOpen(true);
  st().togglePin("a.md");
  assert.deepEqual(st().comparePins, ["b.md"]);
  assert.equal(st().compareOpen, false);
});

test("entries: clearPins closes compare", () => {
  resetStore();
  const st = () => useEntriesStore.getState();
  st().togglePin("a.md");
  st().togglePin("b.md");
  st().setCompareOpen(true);
  st().clearPins();
  assert.deepEqual(st().comparePins, []);
  assert.equal(st().compareOpen, false);
});

test("entries: filter and sort setters", () => {
  resetStore();
  const st = () => useEntriesStore.getState();
  st().setTypeFilter("walkthrough");
  st().setProjectFilter("hindsight");
  st().setStatusFilter("active");
  st().setSort("title", "asc");
  st().setStrategy("hybrid");
  const s = st();
  assert.equal(s.typeFilter, "walkthrough");
  assert.equal(s.projectFilter, "hindsight");
  assert.equal(s.statusFilter, "active");
  assert.equal(s.sortBy, "title");
  assert.equal(s.sortOrder, "asc");
  assert.equal(s.strategy, "hybrid");
});

test("entries: canonicalizeRef rewrites selection and collapses duplicate pins", () => {
  resetStore();
  const st = () => useEntriesStore.getState();
  // Entry opened via short-id link, then pinned by both id and path.
  st().selectEntry("ab12cd34");
  st().togglePin("ab12cd34");
  st().togglePin("projects/x/plan/ab12cd34.md");
  st().setCompareOpen(true);
  st().canonicalizeRef("ab12cd34", "projects/x/plan/ab12cd34.md");
  const s = st();
  assert.equal(s.selectedPath, "projects/x/plan/ab12cd34.md");
  assert.deepEqual(s.comparePins, ["projects/x/plan/ab12cd34.md"]);
  assert.equal(s.compareOpen, false);
});

test("entries: canonicalizeRef is a no-op for unrelated refs", () => {
  resetStore();
  const st = () => useEntriesStore.getState();
  st().selectEntry("p/a.md");
  st().togglePin("p/b.md");
  st().canonicalizeRef("zzzzzzzz", "p/c.md");
  assert.equal(st().selectedPath, "p/a.md");
  assert.deepEqual(st().comparePins, ["p/b.md"]);
});

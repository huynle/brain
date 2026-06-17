import { strict as assert } from "node:assert";
import { test } from "node:test";
import { deserializeHiddenEntryTypes, filterEntriesByHiddenTypes, serializeHiddenEntryTypes, toggleHiddenEntryType } from "./entryFilters";
function entry(id: string, type: string) {
  return { id, type };
}

test("filterEntriesByHiddenTypes hides only selected entry types", () => {
  const entries = [entry("task-1", "task"), entry("auto-1", "automation"), entry("note-1", "note")];

  const visible = filterEntriesByHiddenTypes(entries, new Set(["task", "automation"]));

  assert.deepEqual(visible.map((e) => e.id), ["note-1"]);
});

test("filterEntriesByHiddenTypes preserves all entries when no types are hidden", () => {
  const entries = [entry("task-1", "task"), entry("auto-1", "automation")];

  assert.deepEqual(filterEntriesByHiddenTypes(entries, new Set()), entries);
});

test("toggleHiddenEntryType toggles a type without mutating the original set", () => {
  const hidden = new Set(["task"]);

  assert.deepEqual([...toggleHiddenEntryType(hidden, "task")], []);
  assert.deepEqual([...toggleHiddenEntryType(hidden, "automation")].sort(), ["automation", "task"]);
  assert.deepEqual([...hidden], ["task"]);
});


test("serializeHiddenEntryTypes sorts hidden types for stable storage", () => {
  const encoded = serializeHiddenEntryTypes(new Set(["summary", "plan"]));

  assert.equal(encoded, '["plan","summary"]');
});

test("deserializeHiddenEntryTypes ignores invalid cached values", () => {
  assert.deepEqual([...deserializeHiddenEntryTypes('not-json')], []);
  assert.deepEqual([...deserializeHiddenEntryTypes('["plan", 42, "summary"]')], ["plan", "summary"]);
});

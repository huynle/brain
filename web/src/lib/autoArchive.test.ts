import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  AUTO_ARCHIVE_TAG,
  autoArchiveEntry,
  findAutoArchive,
  isAutoArchiveAutomation,
  isAutoArchiveOn,
} from "./autoArchive";
import type { BrainEntry } from "./types";

const entry = (over: Partial<BrainEntry> = {}): BrainEntry =>
  ({
    id: "a1",
    path: "projects/shop/automation/a1.md",
    title: "Auto-archive completed features",
    type: "automation",
    status: "active",
    tags: [AUTO_ARCHIVE_TAG],
    action: { type: "update", set_status: "archived" },
    ...over,
  }) as BrainEntry;

test("isAutoArchiveAutomation matches the tag AND the action", () => {
  assert.equal(isAutoArchiveAutomation(entry()), true);
});

// The tag alone is not enough: someone tagging an unrelated automation by
// hand would otherwise make the checkbox claim to be on while nothing
// archives anything.
test("isAutoArchiveAutomation rejects a tagged entry with the wrong action", () => {
  assert.equal(
    isAutoArchiveAutomation(entry({ action: { type: "prompt" } })),
    false,
  );
  assert.equal(
    isAutoArchiveAutomation(
      entry({ action: { type: "update", set_status: "cancelled" } }),
    ),
    false,
  );
  assert.equal(isAutoArchiveAutomation(entry({ action: undefined })), false);
});

test("isAutoArchiveAutomation rejects an untagged entry and a non-automation", () => {
  assert.equal(isAutoArchiveAutomation(entry({ tags: [] })), false);
  assert.equal(isAutoArchiveAutomation(entry({ tags: undefined })), false);
  assert.equal(isAutoArchiveAutomation(entry({ type: "task" })), false);
});

// A paused automation is still an entry. Reporting it as on would tick a
// box while nothing happens.
test("isAutoArchiveOn requires the automation to be active", () => {
  assert.equal(isAutoArchiveOn([entry()]), true);
  assert.equal(isAutoArchiveOn([entry({ status: "blocked" })]), false);
  assert.equal(isAutoArchiveOn([]), false);
});

test("findAutoArchive picks it out of a mixed list", () => {
  const other = entry({ id: "a2", tags: [], action: { type: "prompt" } });
  assert.equal(findAutoArchive([other, entry()])?.id, "a1");
  assert.equal(findAutoArchive([other]), undefined);
});

test("autoArchiveEntry scopes to the project and asks for the update action", () => {
  const e = autoArchiveEntry("shop");
  assert.equal(e.type, "automation");
  assert.equal(e.project, "shop");
  assert.equal(e.status, "active");
  assert.deepEqual(e.tags, [AUTO_ARCHIVE_TAG]);
  assert.deepEqual(e.trigger, { type: "event", event: "feature.completed" });
  assert.deepEqual(e.action, { type: "update", set_status: "archived" });
});

// `once_per: "feature_id"` is the dedup every other action type uses, and
// here it would be a bug: it fires a feature exactly once forever, so a
// task added after the first pass would never be archived. The update
// action guards its own loop by writing only what is not already there.
test("autoArchiveEntry does NOT set once_per", () => {
  const trigger = autoArchiveEntry("shop").trigger as Record<string, unknown>;
  assert.equal(trigger.once_per, undefined);
});

test("autoArchiveEntry explains itself and how to turn it off", () => {
  const e = autoArchiveEntry("shop");
  assert.match(String(e.content), /untick/i);
  assert.match(String(e.content), /Archived tab/);
});

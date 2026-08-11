/**
 * Tests for lib/mergeRequests — attributing Brain-native merge requests to
 * features.
 *
 * The entry is written by an LLM agent following SKILL.md, and observed
 * drift is real: the first live sonnet run set none of the structured
 * fields. These tests pin the three-level fallback (field → content line →
 * title convention) using the EXACT shapes seen in that live run.
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  isOpenMergeRequest,
  mrFeatureId,
  openMRFeatureIds,
} from "./mergeRequests";
import type { BrainEntry } from "./types";

function mkMR(over: Partial<BrainEntry> = {}): BrainEntry {
  return {
    id: "mr1",
    path: "projects/p/merge_request/mr1.md",
    title: "Merge request: feat-x -> main",
    type: "merge_request",
    status: "pending",
    content: "",
    ...over,
  } as BrainEntry;
}

// ─── attribution fallbacks ─────────────────────────────────────────

test("structured feature_id wins when present", () => {
  const e = mkMR({ feature_id: "checkout-flow", title: "Merge request: other -> main" });
  assert.equal(mrFeatureId(e), "checkout-flow");
});

test("falls back to the content block's feature_id line", () => {
  // Exact shape from the live sonnet run (no structured fields at all).
  const e = mkMR({
    title: "Merge request: ai-feat2 -> main",
    content:
      "## Brain Merge Request\n\n- feature_id: ai-feat2\n- source_branch: ai-feat2\n- target_branch: main\n",
  });
  // Remove title match to prove the content line is what resolves…
  e.title = "unconventional title";
  assert.equal(mrFeatureId(e), "ai-feat2");
});

test("falls back to the mandated title convention last", () => {
  const e = mkMR({ title: "Merge request: ai-feat2 -> main", content: "prose only" });
  assert.equal(mrFeatureId(e), "ai-feat2");
});

test("returns empty for an unattributable entry", () => {
  const e = mkMR({ title: "some note", content: "nothing structured here" });
  assert.equal(mrFeatureId(e), "");
});

test("content line matching is anchored — a mention in prose does not count", () => {
  const e = mkMR({
    title: "x",
    content: "we discussed feature_id: bogus in passing\nbut no block line exists",
  });
  // "feature_id: bogus" is not in the `- feature_id:` list-item shape.
  assert.equal(mrFeatureId(e), "");
});

// ─── open/closed statuses ──────────────────────────────────────────

test("pending is open — the skill's contract state", () => {
  assert.equal(isOpenMergeRequest(mkMR({ status: "pending" })), true);
});

for (const s of ["completed", "validated", "cancelled", "superseded", "archived", "draft"]) {
  test(`${s} is not open`, () => {
    assert.equal(isOpenMergeRequest(mkMR({ status: s })), false);
  });
}

// ─── folding ───────────────────────────────────────────────────────

test("openMRFeatureIds folds only open, attributable entries", () => {
  const set = openMRFeatureIds([
    mkMR({ id: "a", feature_id: "feat-open" }),
    mkMR({ id: "b", feature_id: "feat-done", status: "completed" }),
    mkMR({ id: "c", title: "unattributable", content: "" }),
    mkMR({ id: "d", title: "Merge request: feat-title -> main" }),
  ]);
  assert.deepEqual([...set].sort(), ["feat-open", "feat-title"]);
});

test("openMRFeatureIds of nothing is an empty set", () => {
  assert.equal(openMRFeatureIds([]).size, 0);
});

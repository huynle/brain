/**
 * entries.ts — pure helper tests (node --test, no DOM).
 */
import { strict as assert } from "node:assert";
import { test } from "node:test";

import {
  ALL_ENTRY_TYPES,
  KNOWLEDGE_TYPES,
  buildListPlan,
  classifyEntryHref,
  entryBasename,
  entryProject,
  excerptOf,
  extractHeadings,
  mergeEntryLists,
  slugifyHeading,
  FANOUT_LIMIT_PER_TYPE,
  SINGLE_TYPE_LIMIT,
} from "./entries";
import type { BrainEntry } from "./types";

function entry(over: Partial<BrainEntry>): BrainEntry {
  return {
    id: "aaaaaaaa",
    path: "projects/p/plan/aaaaaaaa.md",
    title: "t",
    type: "plan",
    status: "active",
    content: "",
    ...over,
  };
}

// ─── buildListPlan ────────────────────────────────────────────────────

test("plan: knowledge filter fans out per knowledge type", () => {
  const plan = buildListPlan({
    typeFilter: "knowledge",
    projectFilter: "",
    statusFilter: "",
    sortBy: "modified",
    sortOrder: "desc",
  });
  assert.equal(plan.length, KNOWLEDGE_TYPES.length);
  assert.deepEqual(
    plan.map((c) => c.type),
    [...KNOWLEDGE_TYPES],
  );
  assert.ok(plan.every((c) => c.limit === FANOUT_LIMIT_PER_TYPE));
  assert.ok(plan.every((c) => !("project" in c) && !("global" in c)));
});

test("plan: all filter fans out across every type", () => {
  const plan = buildListPlan({
    typeFilter: "all",
    projectFilter: "",
    statusFilter: "",
    sortBy: "modified",
    sortOrder: "desc",
  });
  assert.equal(plan.length, ALL_ENTRY_TYPES.length);
});

test("plan: single type is one bigger call with filters applied", () => {
  const plan = buildListPlan({
    typeFilter: "walkthrough",
    projectFilter: "hindsight",
    statusFilter: "active",
    sortBy: "created",
    sortOrder: "asc",
  });
  assert.equal(plan.length, 1);
  assert.deepEqual(plan[0], {
    type: "walkthrough",
    project: "hindsight",
    status: "active",
    sortBy: "created",
    sortOrder: "asc",
    limit: SINGLE_TYPE_LIMIT,
  });
});

test("plan: global project filter maps to global=true", () => {
  const plan = buildListPlan({
    typeFilter: "summary",
    projectFilter: "global",
    statusFilter: "",
    sortBy: "modified",
    sortOrder: "desc",
  });
  assert.equal(plan[0].global, "true");
  assert.equal(plan[0].project, undefined);
});

// ─── mergeEntryLists ──────────────────────────────────────────────────

test("merge: dedupes by path, sorts by modified desc, caps", () => {
  const a = entry({ path: "p/a.md", modified: "2026-01-01T00:00:00Z" });
  const b = entry({ path: "p/b.md", modified: "2026-03-01T00:00:00Z" });
  const c = entry({ path: "p/c.md", modified: "2026-02-01T00:00:00Z" });
  const merged = mergeEntryLists([[a, b], [b, c]], "modified", "desc");
  assert.deepEqual(
    merged.map((e) => e.path),
    ["p/b.md", "p/c.md", "p/a.md"],
  );
  const capped = mergeEntryLists([[a, b, c]], "modified", "desc", 2);
  assert.equal(capped.length, 2);
});

test("merge: title sort ascending, missing dates sort stably", () => {
  const a = entry({ path: "p/a.md", title: "beta" });
  const b = entry({ path: "p/b.md", title: "Alpha" });
  const byTitle = mergeEntryLists([[a, b]], "title", "asc");
  assert.deepEqual(
    byTitle.map((e) => e.title),
    ["Alpha", "beta"],
  );
  const noDates = mergeEntryLists([[a, b]], "modified", "desc");
  assert.equal(noDates.length, 2);
});

// ─── markdown helpers ─────────────────────────────────────────────────

test("headings: extracts levels/text, skips fenced code", () => {
  const md = [
    "# Top",
    "text",
    "```bash",
    "# not a heading",
    "```",
    "## Sub section!",
    "## Sub section!",
  ].join("\n");
  const hs = extractHeadings(md);
  assert.deepEqual(
    hs.map((h) => [h.level, h.text, h.slug]),
    [
      [1, "Top", "top"],
      [2, "Sub section!", "sub-section"],
      [2, "Sub section!", "sub-section-2"],
    ],
  );
});

test("slugify: strips markdown and punctuation", () => {
  assert.equal(slugifyHeading("Fix `runner` — v2 (final)"), "fix-runner-v2-final");
});

test("classifyEntryHref: entry paths, ids, anchors, external", () => {
  assert.deepEqual(classifyEntryHref("projects/x/plan/ab12cd34.md"), {
    kind: "entry",
    ref: "projects/x/plan/ab12cd34.md",
  });
  assert.deepEqual(classifyEntryHref("./global/learning/zz99yy88.md"), {
    kind: "entry",
    ref: "global/learning/zz99yy88.md",
  });
  assert.deepEqual(classifyEntryHref("ab12cd34"), {
    kind: "entry",
    ref: "ab12cd34",
  });
  assert.deepEqual(classifyEntryHref("#some-heading"), { kind: "anchor" });
  assert.deepEqual(classifyEntryHref("https://example.com/a.md"), {
    kind: "external",
  });
  assert.deepEqual(classifyEntryHref("mailto:x@y.z"), { kind: "external" });
  assert.deepEqual(classifyEntryHref("docs/readme.md"), { kind: "external" });
});

test("excerpt: skips headings/fences, strips inline markdown, truncates", () => {
  const md = [
    "# Title",
    "",
    "```",
    "code",
    "```",
    "Some **bold** intro with a [link](projects/x/plan/a.md) here.",
  ].join("\n");
  assert.equal(excerptOf(md), "Some bold intro with a link here.");
  const long = excerptOf("word ".repeat(100), 40);
  assert.ok(long.length <= 41);
  assert.ok(long.endsWith("…"));
});

test("path helpers: basename and project extraction", () => {
  assert.equal(entryBasename("projects/x/plan/ab12cd34.md"), "ab12cd34");
  assert.equal(entryProject(entry({ path: "global/learning/a.md", project_id: undefined })), "global");
  assert.equal(entryProject(entry({ path: "projects/hindsight/plan/a.md", project_id: undefined })), "hindsight");
  assert.equal(entryProject(entry({ project_id: "explicit" })), "explicit");
});

// ─── review-driven regression tests ──────────────────────────────────

test("headings: records 1-based source lines and allows ≤3-space indent", () => {
  const md = ["intro", "# One", "", "   ## Indented"].join("\n");
  const hs = extractHeadings(md);
  assert.deepEqual(
    hs.map((h) => [h.text, h.line]),
    [
      ["One", 2],
      ["Indented", 4],
    ],
  );
});

test("headings: links/images/entities are cleaned out of text and slug", () => {
  const md = "## Fix [runner docs](projects/x/plan/ab12cd34.md) &amp; more";
  const hs = extractHeadings(md);
  assert.equal(hs[0].text, "Fix runner docs & more");
  assert.equal(hs[0].slug, "fix-runner-docs-more");
});

test("headings: a shorter or info-string fence line does not close a fence", () => {
  const md = [
    "````text",
    "# not a heading",
    "```",
    "# still inside the 4-tick fence",
    "````",
    "# real heading",
  ].join("\n");
  assert.deepEqual(
    extractHeadings(md).map((h) => h.text),
    ["real heading"],
  );
});

test("excerpt: tilde fence is not closed by a backtick line", () => {
  const md = ["~~~", "```", "inner code line", "```", "~~~", "Real text."].join(
    "\n",
  );
  assert.equal(excerptOf(md), "Real text.");
});

test("classifyEntryHref: relative entry links resolve via basename short id", () => {
  assert.deepEqual(classifyEntryHref("../plan/ab12cd34.md"), {
    kind: "entry",
    ref: "ab12cd34",
  });
  assert.deepEqual(classifyEntryHref("walkthrough/xy12zw34.md"), {
    kind: "entry",
    ref: "xy12zw34",
  });
  assert.deepEqual(classifyEntryHref("docs/readme.md"), { kind: "external" });
});

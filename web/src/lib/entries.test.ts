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
  resolveProjectScope,
  scopeKey,
  scopeProjectsParam,
  PROJECT_FILTER_ALL,
  PROJECT_FILTER_GLOBAL,
  PROJECT_FILTER_SIDEBAR,
  entryBasename,
  entryProject,
  excerptOf,
  extractHeadings,
  isLoneImageParagraph,
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
    scope: { kind: "all" },
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
    scope: { kind: "all" },
    statusFilter: "",
    sortBy: "modified",
    sortOrder: "desc",
  });
  assert.equal(plan.length, ALL_ENTRY_TYPES.length);
});

test("plan: single type is one bigger call with filters applied", () => {
  const plan = buildListPlan({
    typeFilter: "walkthrough",
    scope: { kind: "project", project: "hindsight" },
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

test("plan: a project set becomes one projects= call per type", () => {
  const plan = buildListPlan({
    typeFilter: "knowledge",
    scope: { kind: "set", projects: ["hindsight", "pwa", "global"] },
    statusFilter: "",
    sortBy: "modified",
    sortOrder: "desc",
  });
  // Still one call per type — NOT one per (type × project), which is what
  // makes following a six-project sidebar affordable.
  assert.equal(plan.length, KNOWLEDGE_TYPES.length);
  assert.ok(
    plan.every((c) => c.projects === "hindsight,pwa,global"),
    "every call carries the whole scope",
  );
  assert.ok(plan.every((c) => c.project === undefined && c.global === undefined));
});

test("plan: global project filter maps to global=true", () => {
  const plan = buildListPlan({
    typeFilter: "summary",
    scope: { kind: "global" },
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

// ─── resolveProjectScope ──────────────────────────────────────────────

test("scope: the sidebar sentinel follows the visible projects, plus global", () => {
  const scope = resolveProjectScope(PROJECT_FILTER_SIDEBAR, {
    projects: ["hindsight", "pwa"],
    unfiltered: false,
  });
  // Global entries belong to no project, so the sidebar can neither show
  // nor hide them — they ride along rather than vanishing.
  assert.deepEqual(scope, {
    kind: "set",
    projects: ["hindsight", "pwa", "global"],
  });
  assert.equal(scopeProjectsParam(scope), "hindsight,pwa,global");
});

test("scope: an unfiltered sidebar asks for everything, not a 44-item list", () => {
  const scope = resolveProjectScope(PROJECT_FILTER_SIDEBAR, {
    projects: ["a", "b", "c"],
    unfiltered: true,
  });
  assert.deepEqual(scope, { kind: "all" });
  assert.equal(scopeProjectsParam(scope), undefined);
});

test("scope: explicit picker values override the sidebar", () => {
  const sidebar = { projects: ["hindsight"], unfiltered: false };
  assert.deepEqual(resolveProjectScope(PROJECT_FILTER_ALL, sidebar), {
    kind: "all",
  });
  assert.deepEqual(resolveProjectScope(PROJECT_FILTER_GLOBAL, sidebar), {
    kind: "global",
  });
  assert.deepEqual(resolveProjectScope("supernote", sidebar), {
    kind: "project",
    project: "supernote",
  });
});

test("scope: keys distinguish every scope, and track set membership", () => {
  const keys = [
    scopeKey({ kind: "all" }),
    scopeKey({ kind: "global" }),
    scopeKey({ kind: "project", project: "a" }),
    scopeKey({ kind: "set", projects: ["a"] }),
    scopeKey({ kind: "set", projects: ["a", "b"] }),
  ];
  assert.equal(new Set(keys).size, keys.length);
});

// ─── isLoneImageParagraph ─────────────────────────────────────────────

const text = (value: string) => ({ type: "text", value });
const el = (tagName: string, children: unknown[] = []) => ({
  type: "element",
  tagName,
  children: children as never[],
});

test("figure: a paragraph holding only an image is a figure", () => {
  assert.equal(isLoneImageParagraph(el("p", [el("img")])), true);
});

test("figure: surrounding whitespace doesn't disqualify a figure", () => {
  // remark leaves newline text nodes around a lone image.
  assert.equal(
    isLoneImageParagraph(el("p", [text("\n"), el("img"), text("\n")])),
    true,
  );
});

test("figure: a linked lone image is still a figure", () => {
  assert.equal(
    isLoneImageParagraph(el("p", [el("a", [el("img")])])),
    true,
  );
});

test("figure: an image among words is NOT a figure", () => {
  // The case a CSS :only-child rule got wrong: :only-child counts element
  // children, so it matched this and broke the sentence onto three lines.
  assert.equal(
    isLoneImageParagraph(
      el("p", [text("a sentence with an "), el("img"), text(" in it")]),
    ),
    false,
  );
});

test("figure: two adjacent images are not a figure — they flow inline", () => {
  assert.equal(
    isLoneImageParagraph(el("p", [el("img"), text("\n"), el("img")])),
    false,
  );
});

test("figure: a text-only paragraph and an empty/absent node are not figures", () => {
  assert.equal(isLoneImageParagraph(el("p", [text("just words")])), false);
  assert.equal(isLoneImageParagraph(el("p", [])), false);
  assert.equal(isLoneImageParagraph(undefined), false);
  // A link that wraps more than the image keeps its inline flow.
  assert.equal(
    isLoneImageParagraph(el("p", [el("a", [el("img"), text(" caption")])])),
    false,
  );
});

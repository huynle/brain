/**
 * Brain entries browser — pure helpers.
 *
 * The Entries view browses *knowledge* entries (summaries, walkthroughs,
 * reports, plans…). The store also holds tens of thousands of
 * system-generated `automation_run` / `task` records, so the default
 * browse mode fans out one list request per knowledge type instead of
 * fetching "everything" and drowning in run records.
 *
 * Everything here is pure and unit-tested in `entries.test.ts`; the
 * react-query hook (`hooks/useEntries.ts`) and the components stay thin.
 */
import type { BrainEntry } from "./types";

/** Human-authored / documentation types shown by the default filter. */
export const KNOWLEDGE_TYPES = [
  "summary",
  "report",
  "walkthrough",
  "plan",
  "decision",
  "exploration",
  "pattern",
  "learning",
  "quirk",
  "idea",
  "scratch",
  "dream",
] as const;

/** System-generated record types, hidden unless explicitly selected. */
export const SYSTEM_TYPES = [
  "task",
  "execution",
  "automation",
  "automation_run",
  "merge_request",
] as const;

export const ALL_ENTRY_TYPES = [...KNOWLEDGE_TYPES, ...SYSTEM_TYPES];

/** "knowledge" (default fan-out), "all", or one concrete entry type. */
export type EntryTypeFilter = "knowledge" | "all" | string;

export type EntrySortBy = "modified" | "created" | "title";
export type EntrySortOrder = "asc" | "desc";

export interface EntryListFilters {
  typeFilter: EntryTypeFilter;
  /** "" = all projects, "global" = only global/ entries, else a project id. */
  projectFilter: string;
  /** "" = any status. */
  statusFilter: string;
  sortBy: EntrySortBy;
  sortOrder: EntrySortOrder;
}

export interface EntryListCall {
  type?: string;
  project?: string;
  global?: string;
  status?: string;
  sortBy: EntrySortBy;
  sortOrder: EntrySortOrder;
  limit: number;
}

/** Per-type request size for fan-out modes. */
export const FANOUT_LIMIT_PER_TYPE = 150;
/** Request size when a single concrete type is selected. */
export const SINGLE_TYPE_LIMIT = 400;
/** Cap on the merged, rendered list. */
export const MERGED_LIST_CAP = 400;

/**
 * Translate UI filters into the concrete `GET /entries` calls to make.
 * "knowledge" and "all" fan out one call per type because the API's
 * `type` param accepts a single value and an unfiltered list would be
 * dominated by automation_run records.
 */
export function buildListPlan(filters: EntryListFilters): EntryListCall[] {
  const base: Omit<EntryListCall, "type"> = {
    sortBy: filters.sortBy,
    sortOrder: filters.sortOrder,
    limit: FANOUT_LIMIT_PER_TYPE,
    ...(filters.statusFilter ? { status: filters.statusFilter } : {}),
    ...(filters.projectFilter === "global"
      ? { global: "true" }
      : filters.projectFilter
        ? { project: filters.projectFilter }
        : {}),
  };
  if (filters.typeFilter === "knowledge") {
    return KNOWLEDGE_TYPES.map((type) => ({ ...base, type }));
  }
  if (filters.typeFilter === "all") {
    return ALL_ENTRY_TYPES.map((type) => ({ ...base, type }));
  }
  return [{ ...base, type: filters.typeFilter, limit: SINGLE_TYPE_LIMIT }];
}

/**
 * Merge fan-out results into one list: dedupe by path, sort by the
 * requested key, cap the result. String compare is correct for the
 * RFC3339 timestamps the API emits.
 */
export function mergeEntryLists(
  lists: BrainEntry[][],
  sortBy: EntrySortBy,
  sortOrder: EntrySortOrder,
  cap: number = MERGED_LIST_CAP,
): BrainEntry[] {
  const byPath = new Map<string, BrainEntry>();
  for (const list of lists) {
    for (const e of list) {
      if (e && e.path && !byPath.has(e.path)) byPath.set(e.path, e);
    }
  }
  const merged = [...byPath.values()];
  const dir = sortOrder === "asc" ? 1 : -1;
  merged.sort((a, b) => {
    let cmp: number;
    if (sortBy === "title") {
      cmp = (a.title || "").localeCompare(b.title || "");
    } else {
      const av = (sortBy === "created" ? a.created : a.modified) || "";
      const bv = (sortBy === "created" ? b.created : b.modified) || "";
      cmp = av < bv ? -1 : av > bv ? 1 : 0;
    }
    if (cmp === 0) cmp = a.path.localeCompare(b.path);
    return cmp * dir;
  });
  return merged.slice(0, cap);
}

// ─── markdown helpers ────────────────────────────────────────────────

export interface Heading {
  level: number;
  text: string;
  slug: string;
  /** 1-based source line of the heading. EntryMarkdown keys the DOM ids
   *  off remark's `node.position.start.line`, so this is the join key
   *  that keeps TOC slugs and rendered heading ids identical. */
  line: number;
}

/** GitHub-flavoured-ish anchor slug. */
export function slugifyHeading(text: string): string {
  return text
    .toLowerCase()
    .replace(/[`*_~[\]()]/g, "")
    .replace(/[^\p{L}\p{N}\s-]/gu, "")
    .trim()
    .replace(/\s+/g, "-");
}

/** Strip inline markdown that would leak URLs/entities into slugs:
 *  images → alt text, links → link text, common HTML entities → chars. */
function cleanHeadingText(raw: string): string {
  return raw
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/&amp;/g, "&")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .trim();
}

/**
 * CommonMark-ish fence tracking shared by extractHeadings/excerptOf:
 * a fence opens on ``` or ~~~ (≤3 spaces indent); it only closes on a
 * same-character fence at least as long as the opener with no info
 * string. Returns true when the line is a fence delimiter (caller
 * skips it either way).
 */
function makeFenceTracker(): {
  handleFenceLine(line: string): boolean;
  inFence(): boolean;
} {
  let marker = "";
  let openLen = 0;
  return {
    handleFenceLine(line: string): boolean {
      const m = line.match(/^ {0,3}(`{3,}|~{3,})(.*)$/);
      if (!m) return false;
      const ch = m[1][0];
      const len = m[1].length;
      const info = m[2].trim();
      if (marker === "") {
        marker = ch;
        openLen = len;
        return true;
      }
      if (ch === marker && len >= openLen && info === "") {
        marker = "";
        openLen = 0;
      }
      // A non-closing fence-looking line inside a fence is content;
      // both cases are skipped by callers, so just report "fence-ish".
      return true;
    },
    inFence: () => marker !== "",
  };
}

/**
 * Extract markdown headings for a table of contents, skipping fenced
 * code blocks. Duplicate slugs get `-2`, `-3`… suffixes so TOC anchors
 * stay unique. Only column-≤3 ATX headings are detected (setext and
 * blockquoted headings render but don't appear in the TOC).
 */
export function extractHeadings(md: string): Heading[] {
  const out: Heading[] = [];
  const used = new Map<string, number>();
  const fence = makeFenceTracker();
  const lines = md.split("\n");
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (fence.handleFenceLine(line)) continue;
    if (fence.inFence()) continue;
    const m = line.match(/^ {0,3}(#{1,6})\s+(.+?)\s*#*\s*$/);
    if (!m) continue;
    const text = cleanHeadingText(m[2]);
    let slug = slugifyHeading(text) || "section";
    const n = used.get(slug) ?? 0;
    used.set(slug, n + 1);
    if (n > 0) slug = `${slug}-${n + 1}`;
    out.push({ level: m[1].length, text, slug, line: i + 1 });
  }
  return out;
}

/**
 * Classify a markdown link href from an entry body.
 *  - "entry": a Brain entry path (projects/…/x.md, global/…/x.md) or a
 *    bare 8-char short id — openable in the reader.
 *  - "anchor": an in-document #fragment.
 *  - "external": everything else (http, mailto, …).
 * Returns the normalized entry ref for "entry" kinds.
 */
export function classifyEntryHref(
  href: string,
): { kind: "entry"; ref: string } | { kind: "anchor" } | { kind: "external" } {
  if (!href) return { kind: "external" };
  if (href.startsWith("#")) return { kind: "anchor" };
  if (/^[a-z]+:/i.test(href) || href.startsWith("//")) {
    return { kind: "external" };
  }
  let p = href.replace(/^\.\//, "").replace(/^\/+/, "");
  try {
    p = decodeURI(p);
  } catch {
    // keep the raw path if it isn't valid percent-encoding
  }
  if (/^(projects|global)\/[^\s]+\.md$/.test(p)) {
    return { kind: "entry", ref: p };
  }
  if (/^[a-z0-9]{8}(\.md)?$/.test(p)) {
    return { kind: "entry", ref: p.replace(/\.md$/, "") };
  }
  // Relative links between entries ("../plan/ab12cd34.md",
  // "walkthrough/ab12cd34.md"): the basename short ID resolves globally.
  const rel = p.match(/(?:^|\/)([a-z0-9]{8})\.md$/);
  if (rel) {
    return { kind: "entry", ref: rel[1] };
  }
  return { kind: "external" };
}

/**
 * First-paragraph excerpt for list rows / cards: skips headings and
 * blank lines, strips light markdown syntax, truncates on a word.
 */
export function excerptOf(content: string, maxLen = 160): string {
  const fence = makeFenceTracker();
  for (const raw of (content || "").split("\n")) {
    if (fence.handleFenceLine(raw)) continue;
    if (fence.inFence()) continue;
    const line = raw.trim();
    if (!line) continue;
    if (/^#{1,6}\s/.test(line)) continue;
    if (/^(---+$|\|)/.test(line)) continue;
    const text = line
      .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
      .replace(/\[([^\]]*)\]\([^)]*\)/g, "$1")
      .replace(/[`*_~]/g, "")
      .replace(/^[-+>]\s+/, "")
      .replace(/^\d+\.\s+/, "")
      .trim();
    if (!text) continue;
    if (text.length <= maxLen) return text;
    const cut = text.slice(0, maxLen);
    return `${cut.slice(0, Math.max(cut.lastIndexOf(" "), maxLen - 20))}…`;
  }
  return "";
}

/** Short display name for an entry path: "projects/foo/plan/abc.md" → "abc". */
export function entryBasename(path: string): string {
  const base = path.split("/").pop() || path;
  return base.replace(/\.md$/, "");
}

/** Project id from an entry path ("global" for global entries). */
export function entryProject(e: Pick<BrainEntry, "path" | "project_id">): string {
  if (e.project_id) return e.project_id;
  if (e.path.startsWith("global/")) return "global";
  const m = e.path.match(/^projects\/([^/]+)\//);
  return m ? m[1] : "";
}

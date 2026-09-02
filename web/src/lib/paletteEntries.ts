/**
 * Command-palette entry lookup — the pure half.
 *
 * Every other palette command is built synchronously out of a store
 * that is already in memory. Entries cannot be: a brain holds
 * thousands of them and none are loaded, so the palette asks the
 * server (POST /search) and folds the hits in. What lives here is the
 * part worth testing without a DOM — how a hit is labelled, and how
 * the hits are ordered once they land.
 */
import { entryBasename, entryProject } from "./entries";
import type { SearchResult } from "./types";

/** Shortest query that earns a round trip. */
export const MIN_ENTRY_QUERY = 2;

/** How many entry hits the palette lists. */
export const MAX_ENTRY_HITS = 8;

/** Drop the extension the API tolerates on either side of a compare. */
const stripMd = (s: string): string => s.replace(/\.md$/, "");

/**
 * Whether a hit is the thing the query named outright — same short id,
 * same path (with or without `.md`), or same title.
 *
 * Note this is never used to decide *whether* to search. An 8-char
 * lowercase word ("checkout", "features") has the exact shape of a
 * short id, so the shape alone proves nothing; the server settles what
 * exists and this only says which of the hits was named verbatim.
 */
export function isExactEntryHit(query: string, hit: SearchResult): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return false;
  if (hit.id.toLowerCase() === q) return true;
  if (stripMd(hit.path.toLowerCase()) === stripMd(q)) return true;
  return !!hit.title && hit.title.toLowerCase() === q;
}

/**
 * Order the server's hits for the palette: anything the query named
 * outright first, everything else left in the server's own ranking,
 * capped at `MAX_ENTRY_HITS`.
 *
 * FTS already ranks an exact short id first in practice — the id is a
 * token of the indexed path, which bm25 weights at 5.0. The pin is
 * here so that a lookup by id, the whole point of typing one, cannot
 * be pushed below a body-text match that happens to score better.
 */
export function rankEntryHits(
  query: string,
  hits: SearchResult[],
): SearchResult[] {
  const exact: SearchResult[] = [];
  const rest: SearchResult[] = [];
  for (const h of hits) {
    (isExactEntryHit(query, h) ? exact : rest).push(h);
  }
  return [...exact, ...rest].slice(0, MAX_ENTRY_HITS);
}

/** Display title for a hit, falling back to the path's basename. */
export function entryHitTitle(hit: SearchResult): string {
  return hit.title || entryBasename(hit.path);
}

/**
 * Palette label for an entry hit.
 *
 * Shaped like its siblings ("Task: X (project)") so the list reads as
 * one thing. The short id is deliberately NOT in here — it goes in the
 * command's `hint`, where the palette renders it as a right-aligned
 * `<kbd>`, which is what makes an id lookup legible at a glance.
 */
export function entryHitLabel(hit: SearchResult): string {
  const project = entryProject({ path: hit.path });
  const title = entryHitTitle(hit);
  return project ? `Entry: ${title} (${project})` : `Entry: ${title}`;
}

/**
 * Entry navigation ↔ URL glue.
 *
 * The reader's current entry lives in the URL as `?entry=<ref>` so that
 * following an entry link is a real browser navigation: Back returns to
 * the entry you came from, Forward walks in again, and the URL is
 * shareable. `hooks/useEntryNavHistory` owns the two-way sync; the
 * functions here are the pure half of it.
 *
 * The ref is whatever the reader accepts as a path — a canonical
 * `projects/x/plan/ab12cd34.md`, or the 8-char short id an entry link
 * carries. Storing it verbatim keeps a shared link resolving the same
 * way the click did (see `classifyEntryHref`).
 */

/** Query-string key holding the open entry. */
export const ENTRY_PARAM = "entry";

/** The entry ref in a `location.search`, or null when none is set. */
export function entryRefFromSearch(search: string): string | null {
  const raw = new URLSearchParams(search).get(ENTRY_PARAM);
  const ref = raw?.trim();
  return ref ? ref : null;
}

/**
 * `search` with the entry ref set (or removed, for `null`). Every other
 * parameter is preserved and left in place — the entries view is one
 * surface in a shell that may grow others.
 */
export function searchWithEntry(search: string, ref: string | null): string {
  const params = new URLSearchParams(search);
  if (ref) params.set(ENTRY_PARAM, ref);
  else params.delete(ENTRY_PARAM);
  const next = params.toString();
  return next ? `?${next}` : "";
}

/**
 * Href for an entry link in rendered markdown. The click is intercepted
 * (the reader navigates in place), but the href has to be a URL that
 * actually resolves so ⌘/middle-click opens the entry in a new tab.
 */
export function entryHref(ref: string): string {
  return searchWithEntry("", ref);
}
